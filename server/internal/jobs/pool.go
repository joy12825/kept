package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"kept/server/internal/logging"
)

type PoolConfig struct {
	Workers      int
	PollInterval time.Duration // idle polling when nothing wakes the pool; default 2s
	Heartbeat    time.Duration // default 15s
	StaleAfter   time.Duration // a running job without heartbeat for this long is recovered; default 2m
	JobTimeout   time.Duration // per-job deadline; default 30m
	Grace        time.Duration // after shutdown starts, running jobs get this long before cancellation; default 25s
}

func (c PoolConfig) withDefaults() PoolConfig {
	if c.Workers < 1 {
		c.Workers = 1
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 2 * time.Second
	}
	if c.Heartbeat <= 0 {
		c.Heartbeat = 15 * time.Second
	}
	if c.StaleAfter <= 0 {
		c.StaleAfter = 2 * time.Minute
	}
	if c.JobTimeout <= 0 {
		c.JobTimeout = 30 * time.Minute
	}
	if c.Grace <= 0 {
		c.Grace = 25 * time.Second
	}
	return c
}

// Pool runs jobs from a Queue with a fixed number of workers.
type Pool struct {
	queue    *Queue
	cfg      PoolConfig
	log      *slog.Logger
	mu       sync.RWMutex
	handlers map[string]Handler
	onFinish []func(Job, Outcome)
}

func NewPool(q *Queue, cfg PoolConfig, log *slog.Logger) *Pool {
	return &Pool{queue: q, cfg: cfg.withDefaults(), log: log, handlers: map[string]Handler{}}
}

// Handle registers the handler for a job kind. Register before Run.
// Outcome is how a job execution ended, for OnFinish.
type Outcome struct {
	Status string // succeeded, retrying or failed
	Result any
	Err    error
}

// OnFinish registers f to run after each execution has been recorded
// (e.g. to tell clients). Register before Run.
func (p *Pool) OnFinish(f func(job Job, o Outcome)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onFinish = append(p.onFinish, f)
}

func (p *Pool) finished(job Job, o Outcome) {
	p.mu.RLock()
	hooks := p.onFinish
	p.mu.RUnlock()
	for _, f := range hooks {
		f(job, o)
	}
}

func (p *Pool) Handle(kind string, h Handler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[kind] = h
}

func (p *Pool) handler(kind string) (Handler, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	h, ok := p.handlers[kind]
	return h, ok
}

// Run blocks until ctx is cancelled and all running jobs have returned.
// Cancelling ctx stops claiming new jobs; running jobs keep going for
// cfg.Grace and are then cancelled.
func (p *Pool) Run(ctx context.Context) error {
	if n, f, err := p.queue.RecoverStale(ctx, p.cfg.StaleAfter); err != nil {
		return fmt.Errorf("recover stale jobs: %w", err)
	} else if n+f > 0 {
		p.log.Warn("recovered interrupted jobs", "requeued", n, "failed", f)
	}

	jobCtx, cancelJobs := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelJobs()
	go func() {
		<-ctx.Done()
		t := time.NewTimer(p.cfg.Grace)
		defer t.Stop()
		select {
		case <-t.C:
			cancelJobs()
		case <-jobCtx.Done():
		}
	}()

	var wg sync.WaitGroup
	for range p.cfg.Workers {
		wg.Go(func() { p.worker(ctx, jobCtx) })
	}
	wg.Go(func() { p.recoverLoop(ctx) })
	wg.Wait()
	return nil
}

func (p *Pool) recoverLoop(ctx context.Context) {
	t := time.NewTicker(p.cfg.StaleAfter / 2)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, f, err := p.queue.RecoverStale(ctx, p.cfg.StaleAfter); err != nil && ctx.Err() == nil {
				p.log.Error("recover stale jobs", "err", err)
			} else if n+f > 0 {
				p.log.Warn("recovered interrupted jobs", "requeued", n, "failed", f)
			}
		}
	}
}

func (p *Pool) worker(ctx, jobCtx context.Context) {
	for ctx.Err() == nil {
		row, err := p.queue.claim(ctx)
		if err != nil {
			if ctx.Err() == nil {
				p.log.Error("claim job", "err", err)
			}
			p.sleep(ctx)
			continue
		}
		if row == nil {
			p.sleep(ctx)
			continue
		}
		p.execute(jobCtx, toJob(row))
	}
}

// sleep waits for the poll interval, a wake-up from Enqueue, or shutdown.
func (p *Pool) sleep(ctx context.Context) {
	t := time.NewTimer(p.cfg.PollInterval)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	case <-p.queue.wake:
	}
}

func (p *Pool) execute(parent context.Context, job Job) {
	log := p.log.With("job", job.ID, "kind", job.Kind, "attempt", job.Attempt)
	if job.RequestID != "" {
		log = log.With("request_id", job.RequestID)
		parent = logging.WithRequestID(parent, job.RequestID) // jobs it enqueues keep the ID
	}
	ctx, cancel := context.WithTimeout(parent, p.cfg.JobTimeout)
	defer cancel()

	stopBeat := p.startHeartbeat(ctx, job.ID, log)
	started := time.Now()
	result, err := p.run(ctx, job)
	stopBeat()

	// Record the outcome even if the job context was cancelled.
	db, done := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer done()
	if err == nil {
		if cerr := p.queue.complete(db, job.ID, result); cerr != nil {
			log.Error("record job success", "err", cerr)
		}
		log.Info("job succeeded", "took", time.Since(started))
		p.finished(job, Outcome{Status: "succeeded", Result: result})
		return
	}
	if ferr := p.queue.finishWithError(db, job.ID, job.Attempt, job.MaxAttempts, err); ferr != nil {
		log.Error("record job failure", "err", ferr)
	}
	log.Warn("job failed", "err", err, "permanent", IsPermanent(err), "took", time.Since(started))
	status := "retrying"
	if IsPermanent(err) || job.Attempt >= job.MaxAttempts {
		status = "failed"
	}
	p.finished(job, Outcome{Status: status, Err: err})
}

func (p *Pool) run(ctx context.Context, job Job) (result any, err error) {
	h, ok := p.handler(job.Kind)
	if !ok {
		return nil, Permanent(fmt.Errorf("没有处理 %q 类任务的程序", job.Kind))
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()
	result, err = h(ctx, job)
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		err = fmt.Errorf("任务超时（%s）：%w", p.cfg.JobTimeout, err)
	}
	return result, err
}

func (p *Pool) startHeartbeat(ctx context.Context, id int64, log *slog.Logger) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(p.cfg.Heartbeat)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := p.queue.heartbeat(ctx, id); err != nil && ctx.Err() == nil {
					log.Error("job heartbeat", "err", err)
				}
			}
		}
	}()
	return func() { cancel(); <-done }
}
