package drawer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kept/server/internal/i18n"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/events"
	"kept/server/internal/jobs"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

// KindAlerts is the daily alert check of one workspace (design doc 11.7).
const KindAlerts = "drawer.alerts"

// AlertsChanged tells clients to reload the alerts.
const AlertsChanged = "alerts.changed"

// Alert is an open alert, ready to show.
type Alert struct {
	ID          int64
	DrawerID    int64
	FactID      int64
	Kind        string // due | missing
	DocumentID  int64
	DueDate     string
	DaysLeft    int
	PeriodLabel string
	Text        string
	Note        string // the steward's plain-language note (v0.5); "" when none
	Warn        bool
	CreatedAt   time.Time
}

var ErrAlertNotFound = apperr.New(apperr.KindNotFound, "找不到这条提醒")

// RefreshAlerts checks the workspace's due and expect facts: alerts whose
// condition holds are opened (a dismissed one stays dismissed for its
// period), the others are resolved. It reports whether anything changed.
func (s *Service) RefreshAlerts(ctx context.Context, scope tenant.Scope) (bool, error) {
	ws := scope.WorkspaceID()
	e, err := s.env(ctx, scope)
	if err != nil {
		return false, err
	}
	rows, err := s.q.ListWorkspaceFacts(ctx, ws)
	if err != nil {
		return false, err
	}
	type key struct {
		fact   int64
		period string
	}
	want := map[key]sqlc.UpsertAlertParams{}
	now := s.opt.Now().UnixMilli()
	for _, r := range rows {
		f := toFact(r)
		switch f.Kind {
		case KindDue:
			docs, dates, err := e.dueDates(ctx, f)
			if err != nil {
				return false, err
			}
			limit := e.today.AddDate(0, 0, f.Config.AdvanceDays)
			for i, d := range docs {
				if e.day(dates[i]).After(limit) {
					break
				}
				k := key{f.ID, fmt.Sprintf("due:%d:%s", d.ID, dates[i])}
				want[k] = sqlc.UpsertAlertParams{WorkspaceID: ws, DrawerID: f.DrawerID, FactID: f.ID, Kind: "due", PeriodKey: k.period,
					DocumentID: sql.NullInt64{Int64: d.ID, Valid: true}, DueDate: sql.NullString{String: dates[i], Valid: true}, Now: now}
			}
		case KindExpect:
			v, err := e.expect(ctx, f)
			if err != nil {
				return false, err
			}
			if v.Present {
				continue
			}
			_, _, pk, label := expectPeriod(e.today, f.Config.Period, f.Config.GraceDays)
			k := key{f.ID, "expect:" + pk}
			want[k] = sqlc.UpsertAlertParams{WorkspaceID: ws, DrawerID: f.DrawerID, FactID: f.ID, Kind: "missing", PeriodKey: k.period, PeriodLabel: label, Now: now}
		}
	}
	changed := false
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		have, err := q.ListUnresolvedAlertKeys(ctx, ws)
		if err != nil {
			return err
		}
		known := map[key]bool{}
		for _, h := range have {
			k := key{h.FactID, h.PeriodKey}
			known[k] = true
			if _, ok := want[k]; !ok {
				if err := q.ResolveAlert(ctx, sqlc.ResolveAlertParams{Now: now, WorkspaceID: ws, ID: h.ID}); err != nil {
					return err
				}
				changed = true
			}
		}
		for k, p := range want {
			if !known[k] {
				changed = true
			}
			if err := q.UpsertAlert(ctx, p); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && changed && s.opt.Bus != nil {
		s.opt.Bus.Publish(events.Event{Type: AlertsChanged, WorkspaceID: ws})
	}
	return changed, err
}

// Alerts lists the open alerts, most urgent first: missing documents and
// dates already past, then the nearest dates.
func (s *Service) Alerts(ctx context.Context, scope tenant.Scope) ([]Alert, error) {
	rows, err := s.q.ListOpenAlerts(ctx, sqlc.ListOpenAlertsParams{WorkspaceID: scope.WorkspaceID(), Viewer: scope.Viewer()})
	if err != nil {
		return nil, err
	}
	now := s.opt.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	out := make([]Alert, 0, len(rows))
	for _, r := range rows {
		a := Alert{ID: r.ID, DrawerID: r.DrawerID, FactID: r.FactID, Kind: r.Kind, DocumentID: r.DocumentID.Int64,
			DueDate: r.DueDate.String, PeriodLabel: r.PeriodLabel, Note: r.Note, CreatedAt: time.UnixMilli(r.CreatedAt)}
		if a.Kind == "due" {
			d, _ := time.ParseInLocation("2006-01-02", a.DueDate, now.Location())
			a.DaysLeft = int(d.Sub(today).Hours() / 24)
			field := r.FieldName
			if field == "" {
				field = r.FactLabel
			}
			// whose it is (v1.6.3): “小明的护照”, unless the title says so
			whose := r.PersonName != "" && !strings.Contains(r.DocumentTitle, r.PersonName)
			a.Text = dueText(whose, r.PersonName, r.DocumentTitle, field, a.DueDate, a.DaysLeft)
			a.Warn = a.DaysLeft <= defaultAdvance
		} else {
			a.Text = fmt.Sprintf(i18n.N("%s：%s的还没有存入"), r.FactLabel, a.PeriodLabel)
			a.Warn = true
		}
		out = append(out, a)
	}
	rank := func(a Alert) int {
		if a.Kind == "missing" {
			return 0
		}
		return a.DaysLeft
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out, nil
}

// dueText is a due alert's text. Each case has its own format (whose,
// one day) so the English reads well (v1.10.1); the text is translated
// where it is sent.
func dueText(whose bool, person, title, field, date string, days int) string {
	var format string
	n := days
	switch {
	case days < -1:
		format, n = i18n.N("%s：%s %s，已过期 %d 天"), -days
	case days == -1:
		format = i18n.N("%s：%s %s，已过期 1 天")
	case days == 0:
		format = i18n.N("%s：%s %s，就是今天")
	case days == 1:
		format = i18n.N("%s：%s %s，还剩 1 天")
	default:
		format = i18n.N("%s：%s %s，还剩 %d 天")
	}
	if whose {
		format = map[string]string{
			i18n.N("%s：%s %s，已过期 %d 天"): i18n.N("%s的%s：%s %s，已过期 %d 天"),
			i18n.N("%s：%s %s，已过期 1 天"):  i18n.N("%s的%s：%s %s，已过期 1 天"),
			i18n.N("%s：%s %s，就是今天"):     i18n.N("%s的%s：%s %s，就是今天"),
			i18n.N("%s：%s %s，还剩 1 天"):   i18n.N("%s的%s：%s %s，还剩 1 天"),
			i18n.N("%s：%s %s，还剩 %d 天"):  i18n.N("%s的%s：%s %s，还剩 %d 天"),
		}[format]
	}
	args := []any{title, field, date}
	if whose {
		args = append([]any{person}, args...)
	}
	if days < -1 || days > 1 {
		args = append(args, n)
	}
	return fmt.Sprintf(format, args...)
}

// SetAlertDismissed dismisses an alert for its period, or undoes that.
func (s *Service) SetAlertDismissed(ctx context.Context, scope tenant.Scope, id int64, dismissed bool) error {
	status := "open"
	if dismissed {
		status = "dismissed"
	}
	n, err := s.q.SetAlertStatus(ctx, sqlc.SetAlertStatusParams{Status: status, Now: s.opt.Now().UnixMilli(), WorkspaceID: scope.WorkspaceID(), ID: id})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrAlertNotFound
	}
	if s.opt.Bus != nil {
		s.opt.Bus.Publish(events.Event{Type: AlertsChanged, WorkspaceID: scope.WorkspaceID()})
	}
	return nil
}

// RegisterAlerts adds the daily alert check's handler.
func (s *Service) RegisterAlerts(p *jobs.Pool) {
	p.Handle(KindAlerts, func(ctx context.Context, job jobs.Job) (any, error) {
		if job.WorkspaceID == nil {
			return nil, jobs.Permanent(errors.New("drawer.alerts needs a workspace"))
		}
		changed, err := s.RefreshAlerts(ctx, tenant.New(*job.WorkspaceID, tenant.RoleOwner))
		return map[string]bool{"changed": changed}, err
	})
}

// RunAlertRefresher rechecks a workspace's alerts shortly after its
// documents or drawers change, so that filing the missing bill clears its
// alert at once. Changes within delay are handled together.
func (s *Service) RunAlertRefresher(ctx context.Context, bus *events.Bus, delay time.Duration, log *slog.Logger) {
	sub, cancel := bus.SubscribeAll()
	defer cancel()
	var mu sync.Mutex
	pending := map[int64]*time.Timer{}
	for {
		select {
		case <-ctx.Done():
			mu.Lock()
			for _, t := range pending {
				t.Stop()
			}
			mu.Unlock()
			return
		case ev, ok := <-sub:
			if !ok {
				return
			}
			switch ev.Type {
			case events.DocumentUpdated, events.InboxChanged, DrawersChanged, events.JobUpdated:
			default:
				continue
			}
			ws := ev.WorkspaceID
			mu.Lock()
			if _, waiting := pending[ws]; !waiting {
				pending[ws] = time.AfterFunc(delay, func() {
					mu.Lock()
					delete(pending, ws)
					mu.Unlock()
					if _, err := s.RefreshAlerts(ctx, tenant.New(ws, tenant.RoleOwner)); err != nil && ctx.Err() == nil {
						log.Warn("refresh alerts", "workspace", ws, "err", err)
					}
				})
			}
			mu.Unlock()
		}
	}
}
