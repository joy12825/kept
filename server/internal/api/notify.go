package api

import (
	"context"

	"kept/server/internal/api/gen"
	"kept/server/internal/audit"
	"kept/server/internal/notify"
	"kept/server/internal/tenant"
)

func (s *Server) smtpEnabled() bool { return s.notify != nil && s.notify.Enabled() }

func (s *Server) EmailDocuments(ctx context.Context, req gen.EmailDocumentsRequestObject) (gen.EmailDocumentsResponseObject, error) {
	if !s.smtpEnabled() {
		return nil, notify.ErrNotConfigured
	}
	p := mustPrincipal(ctx)
	u, err := s.auth.GetUser(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	b := req.Body
	if err := s.refuseSensitive(ctx, tenant.MustFrom(ctx), b.Ids...); err != nil {
		return nil, err
	}
	in := notify.EmailInput{IDs: b.Ids, To: b.To, Subject: deref(b.Subject), Message: deref(b.Message),
		Original: b.File != nil && *b.File == gen.EmailDocumentsRequestFileOriginal}
	id, err := s.notify.EmailDocuments(ctx, tenant.MustFrom(ctx), notify.Sender{Name: audit.ActorFrom(ctx).Name, Email: u.Email}, in)
	if err != nil {
		return nil, err
	}
	return gen.EmailDocuments202JSONResponse{JobId: id}, nil
}

func (s *Server) AdminTestSmtp(ctx context.Context, req gen.AdminTestSmtpRequestObject) (gen.AdminTestSmtpResponseObject, error) {
	if !s.smtpEnabled() {
		return nil, notify.ErrNotConfigured
	}
	if err := s.notify.SendTest(ctx, req.Body.To); err != nil {
		return nil, err
	}
	return gen.AdminTestSmtp204Response{}, nil
}

func (s *Server) notificationPrefs(p notify.Prefs) gen.NotificationPrefs {
	return gen.NotificationPrefs{EmailAlerts: p.EmailAlerts, Email: strPtr(p.Email), Smtp: s.smtpEnabled()}
}

func (s *Server) GetNotificationPrefs(ctx context.Context, _ gen.GetNotificationPrefsRequestObject) (gen.GetNotificationPrefsResponseObject, error) {
	p, err := s.notify.Prefs(ctx, tenant.MustFrom(ctx), mustPrincipal(ctx).UserID)
	if err != nil {
		return nil, err
	}
	return gen.GetNotificationPrefs200JSONResponse(s.notificationPrefs(p)), nil
}

func (s *Server) SetNotificationPrefs(ctx context.Context, req gen.SetNotificationPrefsRequestObject) (gen.SetNotificationPrefsResponseObject, error) {
	p, err := s.notify.SetPrefs(ctx, tenant.MustFrom(ctx), mustPrincipal(ctx).UserID, req.Body.EmailAlerts)
	if err != nil {
		return nil, err
	}
	return gen.SetNotificationPrefs200JSONResponse(s.notificationPrefs(p)), nil
}
