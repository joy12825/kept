package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"kept/server/internal/i18n"
	"net/http"
	"strconv"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/document"
	"kept/server/internal/filter"

	"kept/server/internal/api/gen"
	"kept/server/internal/tenant"
)

// Sensitive documents (v1.6.4): their preview, files and recognised text
// need the viewer to have confirmed the password within the last 15
// minutes (signing in counts). A paired phone with full access confirms
// for itself (v1.7.20); API tokens never can, so they get titles only. Share links, bundles and email refuse them altogether.

var (
	errLocked = &apiError{http.StatusForbidden, "reverify_required", "这是敏感文档，请先确认密码"}
	// errSensitiveOut: what leaves the instance (links, email) never
	// carries a sensitive document; unmark it first.
	errSensitiveOut = &apiError{http.StatusBadRequest, "sensitive", "敏感文档不能分享或发送，请先取消“敏感”标记"}
)

func (s *Server) VerifySession(ctx context.Context, req gen.VerifySessionRequestObject) (gen.VerifySessionResponseObject, error) {
	var password, code string
	if req.Body.Password != nil {
		password = *req.Body.Password
	}
	if req.Body.Code != nil {
		code = *req.Body.Code
	}
	until, err := s.auth.VerifySession(ctx, mustPrincipal(ctx), password, code)
	if err != nil {
		return nil, err
	}
	return gen.VerifySession200JSONResponse{VerifiedUntil: until}, nil
}

// unlocked: the caller may open sensitive documents now.
func (s *Server) unlocked(ctx context.Context) bool {
	p, ok := principalFrom(ctx)
	return ok && s.auth.Verified(p)
}

// guardSensitive refuses when any of the documents (of scope) is
// sensitive and the caller has not confirmed the password.
func (s *Server) guardSensitive(ctx context.Context, scope tenant.Scope, ids ...int64) error {
	if s.unlocked(ctx) {
		return nil
	}
	sens, err := s.documents.Sensitive(ctx, scope, ids...)
	if err != nil {
		return err
	}
	for _, v := range sens {
		if v {
			return errLocked
		}
	}
	return nil
}

// refuseSensitive refuses when any of the documents is sensitive,
// whatever the caller confirmed (what would leave the instance).
func (s *Server) refuseSensitive(ctx context.Context, scope tenant.Scope, ids ...int64) error {
	sens, err := s.documents.Sensitive(ctx, scope, ids...)
	if err != nil {
		return err
	}
	for _, v := range sens {
		if v {
			return errSensitiveOut
		}
	}
	// a family member's private document goes out only by the person, a
	// guardian or a manager: read access and uploaders only look (v1.8.0)
	for _, id := range ids {
		d, err := s.documents.Get(ctx, scope, id)
		if err != nil {
			continue // not there: the operation reports it
		}
		if d.Private {
			ok, err := s.privacy.CanEdit(ctx, nil, scope, true, sql.NullInt64{Int64: d.PersonID, Valid: d.PersonID != 0})
			if err != nil {
				return err
			}
			if !ok {
				return errPrivateOut
			}
		}
	}
	return nil
}

var errPrivateOut = apperr.WithCode(apperr.KindForbidden, "private_out", "这是家人的私密文档：只有本人、照看人或代管的人可以分享、发送")

// lockSensitive withholds a sensitive document's recognised text from a
// caller who has not confirmed the password.
func (s *Server) lockSensitive(ctx context.Context, d gen.Document) gen.Document {
	if d.Sensitive != nil && *d.Sensitive && !s.unlocked(ctx) {
		d.Content, d.ContentEditedAt = "", nil
		t := true
		d.Locked = &t
	}
	return d
}

// optTrue is a boolean that the JSON leaves out when false.
func optTrue(v bool) *bool {
	if !v {
		return nil
	}
	return &v
}

// countSensitive is how many sensitive documents the workspace has.
func (s *Server) countSensitive(ctx context.Context, scope tenant.Scope) (int64, error) {
	page, err := s.documents.List(ctx, scope, document.Query{Filter: filter.Cond{Field: "sensitive", Op: "eq", Value: json.RawMessage("true")}, Limit: 1})
	return page.Total, err
}

// purposeOf is why a file is fetched: "download", "print", or "" to show it.
func purposeOf[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}

// recordTaken notes in a sensitive document's history that someone
// downloaded or printed it (v1.9.7). Other documents are not noted, so
// their history stays about changes; showing a file is never noted.
// what is 原件 or 可搜索版, marked for the catalog by the callers.
func (s *Server) recordTaken(ctx context.Context, scope tenant.Scope, id int64, title, purpose, what string) {
	if purpose != "download" && purpose != "print" {
		return
	}
	sens, err := s.documents.Sensitive(ctx, scope, id)
	if err != nil || !sens[id] {
		return
	}
	name := audit.ActorFrom(ctx).Name
	summary := fmt.Sprintf(i18n.N("%s 下载了“%s”的%s"), name, title, what)
	if purpose == "print" {
		summary = fmt.Sprintf(i18n.N("%s 打印了“%s”"), name, title)
	}
	if err := s.audit.Add(ctx, audit.Event{WorkspaceID: scope.WorkspaceID(), Action: "document." + purpose, TargetType: "document",
		TargetID: strconv.FormatInt(id, 10), Summary: summary}); err != nil {
		s.log.ErrorContext(ctx, "record sensitive document taken", "document", id, "err", err)
	}
}
