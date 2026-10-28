package api

import (
	"context"
	"encoding/json"

	"kept/server/internal/events"
	"kept/server/internal/ingest"
	"kept/server/internal/tenant"
)

// Who is told what (v1.9.10): an event about a document goes only to those
// who see the document — a family member's private document stays out of
// the others' streams. An upload is the uploader's until it is a document:
// whether it is private is decided while it is processed.

// documentOf is the document an event is about (0: none).
func documentOf(ev events.Event) int64 {
	switch d := ev.Data.(type) {
	case ingest.JobEvent:
		return d.DocumentID
	case map[string]any:
		switch v := d["documentId"].(type) {
		case int64:
			return v
		case int:
			return int64(v)
		case float64:
			return int64(v)
		}
	}
	return 0
}

// jobPayload is the part of an ingest job's payload that says who started it.
type jobPayload struct {
	Actor struct {
		UserID int64 `json:"UserID"`
	} `json:"actor"`
}

// mayKnow reports whether the stream's viewer is told ev.
func (s *Server) mayKnow(ctx context.Context, scope tenant.Scope, ev events.Event) bool {
	if scope.Viewer() == tenant.ViewerSystem {
		return true
	}
	if doc := documentOf(ev); doc != 0 {
		_, err := s.documents.Get(ctx, scope, doc)
		return err == nil
	}
	if je, ok := ev.Data.(ingest.JobEvent); ok {
		return s.ownJob(ctx, scope, je.ID)
	}
	return true
}

// ownJob reports whether the viewer started the job.
func (s *Server) ownJob(ctx context.Context, scope tenant.Scope, id int64) bool {
	j, err := s.jobs.Get(ctx, id)
	if err != nil {
		return false
	}
	return mine(j.Payload, scope)
}

func mine(payload string, scope tenant.Scope) bool {
	var p jobPayload
	return json.Unmarshal([]byte(payload), &p) == nil && p.Actor.UserID != 0 && p.Actor.UserID == scope.Viewer()
}
