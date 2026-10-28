package auth

import (
	"sync"
	"time"
)

// limiter counts failed logins per key (username or client IP) within a
// fixed window. In memory: a restart clears it, which is acceptable for a
// single-process self-hosted server.
type limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	now    func() time.Time
	fails  map[string]*failures
	seq    uint64 // last generation handed out
}

// failures is one key's live counting window. gen identifies it: every new
// entry (after a Reset, or once a window expires) gets a fresh generation,
// so a caller holding a reservation can tell whether its failure is still
// part of the current count.
type failures struct {
	count int
	since time.Time
	gen   uint64
}

func newLimiter(max int, window time.Duration, now func() time.Time) *limiter {
	return &limiter{max: max, window: window, now: now, fails: map[string]*failures{}}
}

func (l *limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.current(key)
	return f == nil || f.count < l.max
}

func (l *limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.record(key, l.current(key))
}

// record adds one failure to key's live entry f (nil: start a new window
// with a fresh generation) and returns the entry. The caller holds l.mu.
func (l *limiter) record(key string, f *failures) *failures {
	if f == nil {
		l.seq++
		f = &failures{since: l.now(), gen: l.seq}
		l.fails[key] = f
	}
	f.count++
	if len(l.fails) > 10000 {
		l.prune()
	}
	return f
}

// Reserve checks and counts in one step, under one lock: when key is not
// blocked it records one failure up front and returns the generation of
// the window it was recorded in and true; when key is blocked it records
// nothing and returns false. Callers reserve before the expensive check
// (password or second-factor code) so that concurrent requests cannot all
// pass a separate Allow before any of them calls Fail: at most max checks
// run per generation, however many requests race.
//
// The reservation then has to be resolved: Settle when the attempt failed
// (it stays counted, or is counted again if a Reset cleared it meanwhile),
// Release or Reset when it did not.
func (l *limiter) Reserve(key string) (uint64, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.current(key)
	if f != nil && f.count >= l.max {
		return 0, false
	}
	f = l.record(key, f)
	return f.gen, true
}

// Settle confirms a failed attempt reserved in generation gen. Normally the
// reservation already is the failure and nothing changes. If the counter
// was cleared since (a concurrent successful login called Reset, or the
// window expired), the failure is recorded again in the current window,
// unless that window is already at the limit.
func (l *limiter) Settle(key string, gen uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.current(key)
	if f != nil && f.gen == gen {
		return
	}
	if f != nil && f.count >= l.max {
		return
	}
	l.record(key, f)
}

// Release takes back a reservation from generation gen when the attempt
// did not fail (for example a correct password that still needs the
// second step), leaving any other failures in place. It does nothing if
// the reservation was already cleared (the generation changed), and never
// goes below zero.
func (l *limiter) Release(key string, gen uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.current(key)
	if f == nil || f.gen != gen {
		return
	}
	f.count--
	if f.count <= 0 {
		delete(l.fails, key)
	}
}

func (l *limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
}

// Remaining returns how many more failures key may have before it is
// blocked.
func (l *limiter) Remaining(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.current(key)
	if f == nil {
		return l.max
	}
	n := l.max - f.count
	if n < 0 {
		n = 0
	}
	return n
}

// RetryAfter returns how long key must wait before its window clears. It is
// zero when key is not currently blocked.
func (l *limiter) RetryAfter(key string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.current(key)
	if f == nil {
		return 0
	}
	remaining := l.window - l.now().Sub(f.since)
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// current returns the live entry for key, dropping it if its window expired.
func (l *limiter) current(key string) *failures {
	f, ok := l.fails[key]
	if !ok {
		return nil
	}
	if l.now().Sub(f.since) > l.window {
		delete(l.fails, key)
		return nil
	}
	return f
}

func (l *limiter) prune() {
	for k := range l.fails {
		l.current(k)
	}
}
