package api

import (
	"context"
	"strconv"
	"time"

	"kept/server/internal/api/gen"
	"kept/server/internal/audit"
	"kept/server/internal/i18n"
	"kept/server/internal/tenant"
)

// ListAudit is the current workspace's activity log. authorize has already
// checked the caller is one of its administrators (x-kept-min-role: owner).
func (s *Server) ListAudit(ctx context.Context, req gen.ListAuditRequestObject) (gen.ListAuditResponseObject, error) {
	p := req.Params
	f, err := auditFilter(p.Action, p.ActorId, p.From, p.To, p.Limit, p.Cursor)
	if err != nil {
		return nil, err
	}
	entries, next, err := s.audit.List(ctx, tenant.MustFrom(ctx), f)
	if err != nil {
		return nil, err
	}
	return gen.ListAudit200JSONResponse(auditPageIn(ctx, entries, next)), nil
}

// AdminListAudit is the instance-level log (account management), for
// instance administrators. It never includes events inside a workspace.
func (s *Server) AdminListAudit(ctx context.Context, req gen.AdminListAuditRequestObject) (gen.AdminListAuditResponseObject, error) {
	p := req.Params
	f, err := auditFilter(p.Action, p.ActorId, p.From, p.To, p.Limit, p.Cursor)
	if err != nil {
		return nil, err
	}
	entries, next, err := s.audit.ListInstance(ctx, f)
	if err != nil {
		return nil, err
	}
	return gen.AdminListAudit200JSONResponse(auditPageIn(ctx, entries, next)), nil
}

func auditFilter(action *string, actorID *int64, from, to *time.Time, limit *int, cursor *string) (audit.Filter, error) {
	var f audit.Filter
	if action != nil {
		f.ActionPrefix = *action
	}
	if actorID != nil {
		f.ActorID = *actorID
	}
	if from != nil {
		f.From = *from
	}
	if to != nil {
		f.To = *to
	}
	if limit != nil {
		if *limit < 1 {
			return f, invalid("limit 必须大于 0")
		}
		f.Limit = *limit
	}
	if cursor != nil && *cursor != "" {
		id, err := strconv.ParseInt(*cursor, 10, 64)
		if err != nil || id <= 0 {
			return f, invalid("cursor 无效")
		}
		f.BeforeID = id
	}
	return f, nil
}

func auditPageIn(ctx context.Context, entries []audit.Entry, next int64) gen.AuditPage {
	// the summaries are stored in Chinese; translated here, so older events
	// read in the request's language too (v1.10.1)
	page := gen.AuditPage{Items: make([]gen.AuditEvent, len(entries))}
	for i, e := range entries {
		item := gen.AuditEvent{
			Id: e.ID, Action: e.Action, ActorName: i18n.T(ctx, e.ActorName), ActorVia: gen.AuditEventActorVia(e.ActorVia),
			TargetType: e.TargetType, TargetId: e.TargetID, Summary: i18n.T(ctx, e.Summary), Changes: e.Changes, CreatedAt: e.CreatedAt,
		}
		if e.ActorID != 0 {
			id := e.ActorID
			item.ActorId = &id
		}
		page.Items[i] = item
	}
	if next != 0 {
		c := strconv.FormatInt(next, 10)
		page.NextCursor = &c
	}
	return page
}
