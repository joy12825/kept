package notify

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/audit"
	"kept/server/internal/config"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/files"
	"kept/server/internal/i18n"
	"kept/server/internal/jobs"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/tenant"
)

const (
	// KindEmail sends documents as attachments.
	KindEmail = "notify.email"
	// KindAlerts sends a workspace's new reminders to the members who
	// asked for them (daily).
	KindAlerts = "notify.alerts"

	MaxRecipients = 10
	MaxDocuments  = 50
)

var (
	ErrNotConfigured = apperr.WithCode(apperr.KindInvalid, "smtp_not_configured", "还没有配置发信（KEPT_SMTP_HOST），不能发送邮件")
	ErrNoDocuments   = apperr.New(apperr.KindInvalid, "请选择要发送的文档")
)

type Options struct {
	SMTP config.SMTP
	// Mailer delivers mail; nil means SMTP when a host is configured.
	Mailer    Mailer
	Queue     *jobs.Queue
	Files     *files.Store
	Documents *document.Service
	Drawers   *drawer.Service
	// Health, when set, adds health reminders to the reminder emails.
	Health HealthReminders
	// PublicURL, when set, is put in reminder emails as a link.
	PublicURL string
	// Name is the instance's name (branding) used in mail; nil means 收好.
	Name func(context.Context) string
	Log  *slog.Logger
	Now  func() time.Time
}

// SetHealth adds health reminders to the reminder emails.
func (s *Service) SetHealth(h HealthReminders) { s.opt.Health = h }

// HealthReminder is a health reminder in an email (v1.8.3).
type HealthReminder struct {
	ID   int64
	Text string
}

// HealthReminders are the health reminders to mail (package health).
type HealthReminders interface {
	Unemailed(ctx context.Context, ws int64) ([]int64, error)
	// Visible are the open reminders a member may see.
	Visible(ctx context.Context, scope tenant.Scope) ([]HealthReminder, error)
	MarkEmailed(ctx context.Context, ws int64, ids []int64) error
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	opt Options
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if opt.Mailer == nil && opt.SMTP.Host != "" {
		opt.Mailer = NewSMTP(opt.SMTP)
	}
	return &Service{db: db, q: sqlc.New(db), opt: opt}
}

// name is the instance's name for mail.
func (s *Service) name(ctx context.Context) string {
	if s.opt.Name != nil {
		if n := s.opt.Name(ctx); n != "" {
			return n
		}
	}
	return "收好"
}

// Enabled reports whether mail can be sent.
func (s *Service) Enabled() bool { return s.opt.SMTP.Host != "" && s.opt.Mailer != nil }

// Settings is the configuration without the password (system status).
func (s *Service) Settings() config.SMTP {
	c := s.opt.SMTP
	c.Password = ""
	return c
}

func (s *Service) send(ctx context.Context, m Message) error {
	if !s.Enabled() {
		return ErrNotConfigured
	}
	return s.opt.Mailer.Send(ctx, s.opt.SMTP.From, m.To, func(w io.Writer) error {
		return Write(w, s.opt.SMTP.From, m, s.opt.Now())
	})
}

// SendTest sends a short message at once; the error says what went wrong.
func (s *Service) SendTest(ctx context.Context, to string) error {
	addr, err := parseAddress(to)
	if err != nil {
		return err
	}
	name := s.name(ctx)
	l := i18n.From(ctx) // the administrator testing it reads it
	err = s.send(ctx, Message{To: []string{addr}, Subject: localize(l, fmt.Sprintf(i18n.N("%s：测试邮件"), name)),
		Text: localize(l, fmt.Sprintf(i18n.N("这是一封测试邮件。收到它，说明%s的发信设置可以正常使用。"), name)) + "\n"})
	var e *Error
	if errors.As(err, &e) {
		// what went wrong in the person's language, then what the mail
		// server said, as it said it (v1.10.2)
		sep := "："
		if l == i18n.En {
			sep = ": "
		}
		return apperr.WithCode(apperr.KindInvalid, "smtp_failed", i18n.Message(l, e.Message)+sep+e.Err.Error())
	}
	return err
}

func parseAddress(v string) (string, error) {
	a, err := mail.ParseAddress(strings.TrimSpace(v))
	if err != nil || !strings.Contains(a.Address, "@") {
		return "", apperr.New(apperr.KindInvalid, fmt.Sprintf("“%s”不是有效的邮箱地址", strings.TrimSpace(v)))
	}
	return a.Address, nil
}

// Sender is the member sending documents.
type Sender struct {
	Name  string
	Email string
}

// EmailInput sends documents as attachments.
type EmailInput struct {
	IDs      []int64
	To       []string
	Subject  string
	Message  string
	Original bool // the original files instead of the searchable PDFs
}

type emailPayload struct {
	To        []string `json:"to"`
	ReplyTo   string   `json:"replyTo,omitempty"`
	Subject   string   `json:"subject"`
	Text      string   `json:"text"`
	Documents []int64  `json:"documents"`
	Original  bool     `json:"original"`
	// Notice: the server wrote it; each recipient gets it in their language
	Notice bool `json:"notice,omitempty"`
}

// EmailResult is the job's result.
type EmailResult struct {
	Sent    int     `json:"sent"`
	Skipped []int64 `json:"skipped,omitempty"` // moved to the trash before sending
}

// attachment picks a document's file for mailing, and where it is.
func (s *Service) attachment(ctx context.Context, d document.Document, original bool, used map[string]int) (Attachment, files.Ref) {
	ref, ext := files.Original(d.WorkspaceID, d.ID, d.OriginalExt), d.OriginalExt
	if !original && d.HasArchive {
		ref, ext = files.Archive(d.WorkspaceID, d.ID), "pdf"
	}
	return Attachment{Name: files.FileName(d.Title, ext, used), Open: func() (io.ReadCloser, error) {
		return s.opt.Files.Open(ctx, d.Storage, ref)
	}}, ref
}

// EmailDocuments checks the request and queues the message; the documents
// are read when it is sent.
func (s *Service) EmailDocuments(ctx context.Context, scope tenant.Scope, from Sender, in EmailInput) (int64, error) {
	if !s.Enabled() {
		return 0, ErrNotConfigured
	}
	to := []string{}
	seen := map[string]bool{}
	for _, v := range in.To {
		if strings.TrimSpace(v) == "" {
			continue
		}
		a, err := parseAddress(v)
		if err != nil {
			return 0, err
		}
		if !seen[strings.ToLower(a)] {
			seen[strings.ToLower(a)] = true
			to = append(to, a)
		}
	}
	switch {
	case len(to) == 0:
		return 0, apperr.New(apperr.KindInvalid, "请填写收件人")
	case len(to) > MaxRecipients:
		return 0, apperr.New(apperr.KindInvalid, fmt.Sprintf("一次最多发给 %d 个收件人", MaxRecipients))
	case len(in.IDs) == 0:
		return 0, ErrNoDocuments
	case len(in.IDs) > MaxDocuments:
		return 0, apperr.New(apperr.KindInvalid, fmt.Sprintf("一次最多发送 %d 份文档", MaxDocuments))
	}
	docs := make([]document.Document, 0, len(in.IDs))
	ids := make([]int64, 0, len(in.IDs))
	var total int64
	used := map[string]int{}
	dup := map[int64]bool{}
	for _, id := range in.IDs {
		if dup[id] {
			continue
		}
		dup[id] = true
		d, err := s.opt.Documents.Get(ctx, scope, id)
		if err != nil {
			return 0, err
		}
		_, ref := s.attachment(ctx, d, in.Original, used)
		fi, err := s.opt.Files.Stat(ctx, d.Storage, ref)
		if err != nil {
			return 0, fmt.Errorf("document %d: %w", id, err)
		}
		total += fi.Size
		docs = append(docs, d)
		ids = append(ids, id)
	}
	if limit := s.opt.SMTP.MaxAttachments; total > limit {
		return 0, apperr.WithCode(apperr.KindInvalid, "attachments_too_large",
			fmt.Sprintf("附件共 %s，超过了一封邮件允许的 %s，请减少文档", sizeText(total), sizeText(limit)))
	}
	subject := strings.TrimSpace(in.Subject)
	if subject == "" {
		subject = docs[0].Title
		if len(docs) > 1 { // in the sender's language: the recipients may not have an account
			subject = i18n.T(ctx, fmt.Sprintf(i18n.N("%s 等 %d 份文档"), docs[0].Title, len(docs)))
		}
	}
	text := strings.TrimSpace(in.Message)
	if text != "" {
		text += "\n\n"
	}
	text += i18n.T(ctx, fmt.Sprintf(i18n.N("—— 由 %s 通过%s发送，附件 %d 份"), from.Name, s.name(ctx), len(docs))) + "\n"
	p := emailPayload{To: to, ReplyTo: from.Email, Subject: subject, Text: text, Documents: ids, Original: in.Original}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	q := s.q.WithTx(tx)
	ws := scope.WorkspaceID()
	jobID, err := s.opt.Queue.EnqueueWith(ctx, q, KindEmail, p, jobs.EnqueueOptions{WorkspaceID: &ws, MaxAttempts: 3})
	if err != nil {
		return 0, err
	}
	who := to[0]
	if len(to) > 1 {
		who = fmt.Sprintf(i18n.N("%s 等 %d 人"), who, len(to))
	}
	for _, d := range docs {
		if err := audit.Record(ctx, q, audit.Event{
			WorkspaceID: ws, Action: "document.emailed", TargetType: "document", TargetID: strconv.FormatInt(d.ID, 10),
			Summary: fmt.Sprintf(i18n.N("%s 通过邮件把“%s”发给 %s"), audit.ActorFrom(ctx).Name, d.Title, who), At: s.opt.Now(),
		}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.opt.Queue.Wake()
	return jobID, nil
}

// QueueEmail queues a message from an automatic sender (workflows): the
// documents' searchable PDFs, or originals, are attached when sending.
func (s *Service) QueueEmail(ctx context.Context, ws int64, to []string, subject, text string, docs []int64) (int64, error) {
	if !s.Enabled() {
		return 0, ErrNotConfigured
	}
	return s.opt.Queue.Enqueue(ctx, KindEmail, emailPayload{To: to, Subject: subject, Text: text, Documents: docs},
		jobs.EnqueueOptions{WorkspaceID: &ws, MaxAttempts: 3})
}

// QueueNotice queues a notice the server wrote (emergency access, private
// documents): written in Chinese, each recipient gets it in their language
// (v1.10.2). Each line of text is one sentence of the catalog.
func (s *Service) QueueNotice(ctx context.Context, ws int64, to []string, subject, text string) (int64, error) {
	if !s.Enabled() {
		return 0, ErrNotConfigured
	}
	return s.opt.Queue.Enqueue(ctx, KindEmail, emailPayload{To: to, Subject: subject, Text: text, Notice: true},
		jobs.EnqueueOptions{WorkspaceID: &ws, MaxAttempts: 3})
}

func sizeText(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d KB", max(1, n>>10))
}

func (s *Service) runEmail(ctx context.Context, job jobs.Job) (any, error) {
	if job.WorkspaceID == nil {
		return nil, jobs.Permanent(errors.New("notify.email needs a workspace"))
	}
	var p emailPayload
	if err := json.Unmarshal(job.Payload, &p); err != nil {
		return nil, jobs.Permanent(err)
	}
	scope := tenant.New(*job.WorkspaceID, tenant.RoleOwner)
	m := Message{To: p.To, ReplyTo: p.ReplyTo, Subject: p.Subject, Text: p.Text}
	used := map[string]int{}
	var res EmailResult
	for _, id := range p.Documents {
		d, err := s.opt.Documents.Get(ctx, scope, id)
		if errors.Is(err, document.ErrNotFound) {
			res.Skipped = append(res.Skipped, id)
			continue
		}
		if err != nil {
			return nil, err
		}
		a, _ := s.attachment(ctx, d, p.Original, used)
		m.Attachments = append(m.Attachments, a)
	}
	if len(p.Documents) > 0 && len(m.Attachments) == 0 {
		return nil, jobs.Permanent(errors.New("要发送的文档都已移到回收站"))
	}
	if p.Notice { // one copy per language
		order, groups := s.byLang(ctx, p.To)
		for _, l := range order {
			c := m
			c.To, c.Subject, c.Text = groups[l], localize(l, p.Subject), localize(l, p.Text)
			if err := s.send(ctx, c); err != nil {
				return nil, retryable(err)
			}
		}
	} else if err := s.send(ctx, m); err != nil {
		return nil, retryable(err)
	}
	res.Sent = len(m.Attachments)
	return res, nil
}

// AlertsResult is the reminder digest job's result.
type AlertsResult struct {
	Alerts     int `json:"alerts"`
	Recipients int `json:"recipients"`
}

// SendAlerts emails the workspace's open reminders that were not emailed
// yet to the members who turned reminder emails on, then marks them.
// Without a mail server it does nothing, so the reminders wait.
func (s *Service) SendAlerts(ctx context.Context, ws int64) (AlertsResult, error) {
	if !s.Enabled() {
		return AlertsResult{}, nil
	}
	ids, err := s.q.ListUnemailedAlertIDs(ctx, ws)
	if err != nil {
		return AlertsResult{}, err
	}
	var healthIDs []int64
	if s.opt.Health != nil {
		if healthIDs, err = s.opt.Health.Unemailed(ctx, ws); err != nil {
			return AlertsResult{}, err
		}
	}
	if len(ids) == 0 && len(healthIDs) == 0 {
		return AlertsResult{}, nil
	}
	rows, err := s.q.ListAlertRecipients(ctx, ws)
	if err != nil {
		return AlertsResult{}, err
	}
	if len(rows) == 0 {
		return AlertsResult{Alerts: len(ids) + len(healthIDs)}, nil
	}
	fresh := map[int64]bool{}
	for _, id := range ids {
		fresh[id] = true
	}
	freshHealth := map[int64]bool{}
	for _, id := range healthIDs {
		freshHealth[id] = true
	}
	sentHealth := map[int64]bool{}
	wsName, err := s.q.GetWorkspaceName(ctx, ws)
	if err != nil {
		return AlertsResult{}, err
	}
	names, err := s.q.ListDrawerNames(ctx, ws)
	if err != nil {
		return AlertsResult{}, err
	}
	drawerName := map[int64]string{}
	for _, n := range names {
		drawerName[n.ID] = n.Name
	}
	name := s.name(ctx)
	sent := map[int64]bool{}
	recipients := 0
	// Each member gets their own copy, so addresses are not shared, with
	// only the reminders they may see: those about a family member's
	// private document go to the people who may see it (v1.8.0).
	for _, r := range rows {
		all, err := s.opt.Drawers.Alerts(ctx, tenant.ForUser(ws, tenant.RoleViewer, r.ID))
		if err != nil {
			return AlertsResult{}, err
		}
		var list []drawer.Alert
		for _, a := range all {
			if fresh[a.ID] {
				list = append(list, a)
				sent[a.ID] = true
			}
		}
		var hlist []HealthReminder
		if len(freshHealth) > 0 {
			hall, err := s.opt.Health.Visible(ctx, tenant.ForUser(ws, tenant.RoleViewer, r.ID))
			if err != nil {
				return AlertsResult{}, err
			}
			for _, h := range hall {
				if freshHealth[h.ID] {
					hlist = append(hlist, h)
					sentHealth[h.ID] = true
				}
			}
		}
		if len(list)+len(hlist) == 0 {
			continue
		}
		// one sentence a line, put into the recipient's language (v1.10.2)
		var b bytes.Buffer
		fmt.Fprintf(&b, i18n.N("“%s”有 %d 条新提醒：")+"\n\n", wsName, len(list)+len(hlist))
		for _, a := range list {
			fmt.Fprintf(&b, i18n.N("· %s")+"\n", a.Text)
			fmt.Fprintf(&b, "  "+i18n.N("抽屉：“%s”")+"\n", drawerName[a.DrawerID])
			if s.opt.PublicURL != "" {
				fmt.Fprintf(&b, "  %s/drawers/%d\n", s.opt.PublicURL, a.DrawerID)
			}
		}
		if len(hlist) > 0 {
			if len(list) > 0 {
				b.WriteString("\n")
			}
			b.WriteString(i18n.N("健康：") + "\n")
			for _, h := range hlist {
				fmt.Fprintf(&b, i18n.N("· %s")+"\n", h.Text)
			}
			if s.opt.PublicURL != "" {
				fmt.Fprintf(&b, "  %s/health\n", s.opt.PublicURL)
			}
			b.WriteString("  " + i18n.N("仅供记录参考，不构成医疗建议。") + "\n")
		}
		b.WriteString("\n" + fmt.Sprintf(i18n.N("不想再收到这类邮件，可以在%s的“设置 → 通知”中关闭邮件提醒。"), name) + "\n")
		l := s.LangFor(ctx, r.Email.String, i18n.Zh)
		m := Message{To: []string{r.Email.String}, Subject: localize(l, fmt.Sprintf(i18n.N("%s提醒：%d 条新提醒（%s）"), name, len(list)+len(hlist), wsName)), Text: localize(l, b.String())}
		if err := s.send(ctx, m); err != nil {
			return AlertsResult{}, retryable(err)
		}
		recipients++
	}
	// every fresh reminder counts as emailed, even one nobody may see
	now := s.opt.Now().UnixMilli()
	for id := range fresh {
		if err := s.q.MarkAlertEmailed(ctx, sqlc.MarkAlertEmailedParams{Now: sql.NullInt64{Int64: now, Valid: true}, WorkspaceID: ws, ID: id}); err != nil {
			return AlertsResult{}, err
		}
	}
	if len(healthIDs) > 0 {
		if err := s.opt.Health.MarkEmailed(ctx, ws, healthIDs); err != nil {
			return AlertsResult{}, err
		}
	}
	return AlertsResult{Alerts: len(sent) + len(sentHealth), Recipients: recipients}, nil
}

// SendQuotaWarning emails the workspace's administrators (those who take
// reminder emails) that its storage is nearly full or full (v1.7.4). It
// reports whether anything was sent; without a mail server nothing is.
func (s *Service) SendQuotaWarning(ctx context.Context, ws int64, full bool, used, limit int64) (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	rows, err := s.q.ListOwnerRecipients(ctx, ws)
	if err != nil || len(rows) == 0 {
		return false, err
	}
	wsName, err := s.q.GetWorkspaceName(ctx, ws)
	if err != nil {
		return false, err
	}
	name := s.name(ctx)
	subject := fmt.Sprintf(i18n.N("%s：空间“%s”快满了"), name, wsName)
	// one sentence a line, put into each owner's language (v1.10.2)
	var b bytes.Buffer
	if full {
		subject = fmt.Sprintf(i18n.N("%s：空间“%s”已满，不能再上传"), name, wsName)
		fmt.Fprintf(&b, i18n.N("空间“%s”已用 %s，达到了 %s 的配额，新的文件暂时传不上来（邮件、监听目录收到的也会失败）。")+"\n\n", wsName, quotaText(used), quotaText(limit))
	} else {
		fmt.Fprintf(&b, i18n.N("空间“%s”已用 %s，配额是 %s，用了 %d%%。")+"\n\n", wsName, quotaText(used), quotaText(limit), used*100/limit)
	}
	b.WriteString(i18n.N("可以清空回收站、删除不需要的文档或旧版本腾出空间，或请实例管理员提高配额。"))
	if s.opt.PublicURL != "" {
		fmt.Fprintf(&b, "\n\n"+i18n.N("查看用量：%s"), s.opt.PublicURL+"/settings/storage")
	}
	b.WriteString("\n\n" + fmt.Sprintf(i18n.N("不想再收到这类邮件，可以在%s的“设置 → 通知”中关闭邮件提醒。"), name) + "\n")
	for _, r := range rows {
		l := s.LangFor(ctx, r.String, i18n.Zh)
		if err := s.send(ctx, Message{To: []string{r.String}, Subject: localize(l, subject), Text: localize(l, b.String())}); err != nil {
			return false, retryable(err)
		}
	}
	return true, nil
}

func quotaText(n int64) string {
	if n >= 1<<30 {
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	return sizeText(n)
}

// Prefs are a member's notification settings in a workspace.
type Prefs struct {
	EmailAlerts bool
	Email       string // the account's address; reminders need one
}

func (s *Service) Prefs(ctx context.Context, scope tenant.Scope, userID int64) (Prefs, error) {
	r, err := s.q.GetMemberNotify(ctx, sqlc.GetMemberNotifyParams{WorkspaceID: scope.WorkspaceID(), UserID: userID})
	if err != nil {
		return Prefs{}, err
	}
	return Prefs{EmailAlerts: r.NotifyEmail == 1, Email: r.Email}, nil
}

func (s *Service) SetPrefs(ctx context.Context, scope tenant.Scope, userID int64, emailAlerts bool) (Prefs, error) {
	v := int64(0)
	if emailAlerts {
		v = 1
	}
	if _, err := s.q.SetMemberNotify(ctx, sqlc.SetMemberNotifyParams{NotifyEmail: v, WorkspaceID: scope.WorkspaceID(), UserID: userID}); err != nil {
		return Prefs{}, err
	}
	return s.Prefs(ctx, scope, userID)
}

// Register adds the job handlers.
func (s *Service) Register(p *jobs.Pool) {
	p.Handle(KindEmail, s.runEmail)
	p.Handle(KindAlerts, func(ctx context.Context, job jobs.Job) (any, error) {
		if job.WorkspaceID == nil {
			return nil, jobs.Permanent(errors.New("notify.alerts needs a workspace"))
		}
		return s.SendAlerts(ctx, *job.WorkspaceID)
	})
}
