package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"kept/server/internal/api/gen"
	"kept/server/internal/events"
	"kept/server/internal/i18n"
	"kept/server/internal/ingest"
	"kept/server/internal/tenant"
)

// RetryJob processes a failed upload again (v1.9.12).
func (s *Server) RetryJob(ctx context.Context, req gen.RetryJobRequestObject) (gen.RetryJobResponseObject, error) {
	if err := s.ingest.Retry(ctx, tenant.MustFrom(ctx), req.Id); err != nil {
		return nil, err
	}
	return gen.RetryJob204Response{}, nil
}

func (s *Server) ListJobs(ctx context.Context, _ gen.ListJobsRequestObject) (gen.ListJobsResponseObject, error) {
	rows, err := s.jobs.ListRecent(ctx, tenant.MustFrom(ctx), ingest.KindPrefix, time.Now().Add(-24*time.Hour), 100)
	if err != nil {
		return nil, err
	}
	scope := tenant.MustFrom(ctx)
	now := time.Now()
	out := gen.ListJobs200JSONResponse{}
	for _, r := range rows {
		// someone else's upload shows once it is a document the viewer sees
		// (v1.9.10): until then whether it is private is not decided
		if !r.DocumentID.Valid && scope.Viewer() != tenant.ViewerSystem && !mine(r.Payload, scope) {
			continue
		}
		var pl struct {
			Filename string `json:"filename"`
		}
		_ = json.Unmarshal([]byte(r.Payload), &pl)
		var p ingest.Progress
		if r.Result.Valid {
			_ = json.Unmarshal([]byte(r.Result.String), &p)
		}
		j := gen.Job{
			Id: r.ID, Kind: gen.JobKind(r.Kind), Status: gen.JobStatus(r.Status), Stage: p.Stage, Progress: p.Progress,
			Filename: pl.Filename, CreatedAt: time.UnixMilli(r.CreatedAt),
		}
		if j.Stage == "" {
			j.Stage = "queued"
		}
		if r.Status == "succeeded" {
			j.Stage, j.Progress = "done", 100
		}
		if r.Status == "failed" && r.Error.Valid {
			msg := i18n.T(ctx, ingest.JobErrorMessage(r.Error.String))
			j.Error = &msg
		}
		if r.DocumentID.Valid {
			id := r.DocumentID.Int64
			j.DocumentId = &id
		}
		if ingest.MayRetry(scope, r.CreatedBy) && s.ingest.Retryable(r.Kind, r.Status, r.Payload, r.FinishedAt, now) {
			yes := true
			j.Retryable = &yes
		}
		out = append(out, j)
	}
	return out, nil
}

func (s *Server) GetEvents(ctx context.Context, _ gen.GetEventsRequestObject) (gen.GetEventsResponseObject, error) {
	return eventStream{ctx: ctx, s: s, bus: s.events, ws: tenant.MustFrom(ctx).WorkspaceID(), scope: tenant.MustFrom(ctx)}, nil
}

// eventStream writes Server-Sent Events until the client goes away.
type eventStream struct {
	ctx   context.Context
	s     *Server
	bus   *events.Bus
	ws    int64
	scope tenant.Scope
}

// keepAlive is how often a comment line keeps proxies from closing an idle stream.
var keepAlive = 25 * time.Second

func (e eventStream) VisitGetEventsResponse(w http.ResponseWriter) error {
	ctx, stop := e.s.streamContext(e.ctx)
	defer stop()
	ch, cancel := e.bus.Subscribe(e.ws)
	defer cancel()
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // nginx: do not buffer the stream
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprint(w, ": connected\n\n"); err != nil {
		return nil
	}
	_ = rc.Flush()
	tick := time.NewTicker(keepAlive)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil // the client left, or the server is shutting down (browsers reconnect)
		case <-tick.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if !e.s.mayKnow(ctx, e.scope, ev) {
				continue
			}
			if je, ok := ev.Data.(ingest.JobEvent); ok && je.Error != "" {
				je.Error = i18n.T(ctx, je.Error) // a failed upload's reason, in the stream's language
				ev.Data = je
			}
			data, err := json.Marshal(ev.Data)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data); err != nil {
				return nil
			}
		}
		if err := rc.Flush(); err != nil {
			return nil
		}
	}
}
