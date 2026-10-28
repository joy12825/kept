package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"kept/server/internal/i18n"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gomail "github.com/emersion/go-message/mail"

	"kept/server/internal/apperr"
	"kept/server/internal/config"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/files"
	"kept/server/internal/jobs"
	"kept/server/internal/notify/smtptest"
	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

type env struct {
	t     *testing.T
	ctx   context.Context
	db    *sql.DB
	queue *jobs.Queue
	smtp  *smtptest.Server
	s     *Service
	scope tenant.Scope
}

func newEnv(t *testing.T, mutate func(*config.SMTP)) *env {
	t.Helper()
	data := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(data, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, '王家', 'family', 1), (2, '别人', 'family', 1)`,
		`INSERT INTO users (id, username, display_name, email, password_hash, password_changed_at, created_at, updated_at, disabled_at) VALUES
		 (1, 'wang', '小王', 'wang@example.com', 'x', 1, 1, 1, NULL), (2, 'li', '小李', 'li@example.com', 'x', 1, 1, 1, NULL),
		 (3, 'zhao', '小赵', 'zhao@example.com', 'x', 1, 1, 1, 5), (4, 'qian', '小钱', NULL, 'x', 1, 1, 1, NULL)`,
		`INSERT INTO workspace_members (workspace_id, user_id, role, created_at) VALUES (1, 1, 'owner', 1), (1, 2, 'viewer', 1), (1, 3, 'editor', 1), (1, 4, 'editor', 1), (2, 2, 'owner', 1)`,
		`INSERT INTO documents (id, workspace_id, title, created_date, added_at, modified_at, mime_type, original_filename, original_ext, checksum, has_archive) VALUES
		 (10, 1, '电费账单', '2026-09-01', 1, 1, 'application/pdf', 'a.pdf', 'pdf', 'c10', 1),
		 (11, 1, '体检报告/2026', '2026-09-02', 1, 1, 'image/jpeg', 'b.jpg', 'jpg', 'c11', 0),
		 (20, 2, '别人的', '2026-09-03', 1, 1, 'application/pdf', 'c.pdf', 'pdf', 'c20', 0)`,
	} {
		if _, err := st.DB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	fs := files.NewStore(data)
	for _, f := range []struct {
		path, body string
	}{
		{fs.Path(files.Original(1, 10, "pdf")), "%PDF original 10"},
		{fs.Path(files.Archive(1, 10)), "%PDF archive 10"},
		{fs.Path(files.Original(1, 11, "jpg")), "jpeg 11"},
		{fs.Path(files.Original(2, 20, "pdf")), "%PDF 20"},
	} {
		if err := os.MkdirAll(filepath.Dir(f.path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.path, []byte(f.body), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	srv := smtptest.Start(t, smtptest.Config{Username: "kept", Password: "secret", Reject: map[string]bool{"nobody@example.com": true}})
	cfg := config.SMTP{Host: srv.Host, Port: srv.Port, Security: "none", Username: "kept", Password: "secret",
		From: "收好 <kept@example.com>", MaxAttachments: 1 << 20}
	if mutate != nil {
		mutate(&cfg)
	}
	queue := jobs.NewQueue(st.DB)
	s := NewService(st.DB, Options{SMTP: cfg, Queue: queue, Files: fs,
		Documents: document.NewService(st.DB, document.Options{Files: fs}),
		Drawers:   drawer.NewService(st.DB, drawer.Options{}), PublicURL: "https://docs.example.com",
		Now: func() time.Time { return time.Date(2026, 10, 3, 8, 0, 0, 0, time.Local) }})
	return &env{t: t, ctx: context.Background(), db: st.DB, queue: queue, smtp: srv, s: s, scope: tenant.New(1, tenant.RoleEditor)}
}

// parse reads a delivered message: subject, headers, text and attachments.
type parsed struct {
	subject, replyTo, text string
	files                  map[string]string
}

func parse(t *testing.T, raw string) parsed {
	t.Helper()
	r, err := gomail.CreateReader(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	p := parsed{files: map[string]string{}}
	p.subject, _ = r.Header.Subject()
	if l, _ := r.Header.AddressList("Reply-To"); len(l) > 0 {
		p.replyTo = l[0].Address
	}
	for {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(part.Body)
		switch h := part.Header.(type) {
		case *gomail.AttachmentHeader:
			name, _ := h.Filename()
			p.files[name] = string(body)
		default:
			p.text += string(body)
		}
	}
	return p
}

func TestSendTest(t *testing.T) {
	e := newEnv(t, nil)
	if err := e.s.SendTest(e.ctx, "not an address"); !isKind(err, apperr.KindInvalid) {
		t.Fatalf("bad address: %v", err)
	}
	if err := e.s.SendTest(e.ctx, "wang@example.com"); err != nil {
		t.Fatal(err)
	}
	msgs := e.smtp.Messages()
	if len(msgs) != 1 || msgs[0].From != "kept@example.com" || msgs[0].To[0] != "wang@example.com" {
		t.Fatalf("messages: %+v", msgs)
	}
	if p := parse(t, msgs[0].Data); p.subject != "收好：测试邮件" || !strings.Contains(p.text, "发信设置可以正常使用") {
		t.Fatalf("message: %+v", p)
	}

	bad := newEnv(t, func(c *config.SMTP) { c.Password = "wrong" })
	err := bad.s.SendTest(bad.ctx, "wang@example.com")
	if !isKind(err, apperr.KindInvalid) || !strings.Contains(err.Error(), "登录邮件服务器失败") {
		t.Fatalf("wrong password: %v", err)
	}
	tls := newEnv(t, func(c *config.SMTP) { c.Security = "starttls" })
	if err := tls.s.SendTest(tls.ctx, "wang@example.com"); err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Fatalf("starttls: %v", err)
	}
	off := newEnv(t, func(c *config.SMTP) { c.Host = "" })
	if off.s.Enabled() || !errors.Is(off.s.SendTest(off.ctx, "wang@example.com"), ErrNotConfigured) {
		t.Fatal("sending without a server")
	}
}

func TestRejectedRecipientIsPermanent(t *testing.T) {
	e := newEnv(t, nil)
	err := e.s.send(e.ctx, Message{To: []string{"nobody@example.com"}, Subject: "x", Text: "x"})
	var me *Error
	if !errors.As(err, &me) || !me.Permanent || !jobs.IsPermanent(retryable(err)) {
		t.Fatalf("rejected recipient: %v", err)
	}
}

func (e *env) run(id int64) (any, error) {
	e.t.Helper()
	j, err := e.queue.Get(e.ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	ws := j.WorkspaceID.Int64
	return e.s.runEmail(e.ctx, jobs.Job{ID: j.ID, WorkspaceID: &ws, Kind: j.Kind, Payload: []byte(j.Payload), Attempt: 1, MaxAttempts: 3})
}

func TestEmailDocuments(t *testing.T) {
	e := newEnv(t, nil)
	from := Sender{Name: "小王", Email: "wang@example.com"}
	for name, in := range map[string]EmailInput{
		"no recipient":   {IDs: []int64{10}},
		"bad address":    {IDs: []int64{10}, To: []string{"li@"}},
		"no document":    {To: []string{"li@example.com"}},
		"other's":        {IDs: []int64{20}, To: []string{"li@example.com"}},
		"too many to":    {IDs: []int64{10}, To: manyAddresses(11)},
		"missing at all": {IDs: []int64{99}, To: []string{"li@example.com"}},
	} {
		if _, err := e.s.EmailDocuments(e.ctx, e.scope, from, in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	small := newEnv(t, func(c *config.SMTP) { c.MaxAttachments = 20 })
	if _, err := small.s.EmailDocuments(small.ctx, small.scope, from, EmailInput{IDs: []int64{10, 11}, To: []string{"li@example.com"}}); !isCode(err, "attachments_too_large") {
		t.Fatalf("too large: %v", err)
	}

	id, err := e.s.EmailDocuments(e.ctx, e.scope, from, EmailInput{IDs: []int64{10, 11, 10}, To: []string{"Li <li@example.com>", "LI@example.com", "zhang@example.com"}, Message: "请查收"})
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT count(*) FROM audit_events WHERE action = 'document.emailed'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("audit events: %d %v", n, err)
	}
	res, err := e.run(id)
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(EmailResult); r.Sent != 2 {
		t.Fatalf("result: %+v", r)
	}
	msgs := e.smtp.Messages()
	if len(msgs) != 1 || strings.Join(msgs[0].To, ",") != "li@example.com,zhang@example.com" {
		t.Fatalf("messages: %+v", msgs)
	}
	p := parse(t, msgs[0].Data)
	if p.subject != "电费账单 等 2 份文档" || p.replyTo != "wang@example.com" || !strings.Contains(p.text, "请查收") || !strings.Contains(p.text, "由 小王 通过收好发送") {
		t.Fatalf("message: %+v", p)
	}
	if p.files["电费账单.pdf"] != "%PDF archive 10" || p.files["体检报告_2026.jpg"] != "jpeg 11" {
		t.Fatalf("attachments: %v", p.files)
	}

	// The originals; a document trashed before sending is left out.
	id, err = e.s.EmailDocuments(e.ctx, e.scope, from, EmailInput{IDs: []int64{10, 11}, To: []string{"li@example.com"}, Subject: "原件", Original: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE documents SET deleted_at = 1 WHERE id = 11`); err != nil {
		t.Fatal(err)
	}
	res, err = e.run(id)
	if err != nil {
		t.Fatal(err)
	}
	if r := res.(EmailResult); r.Sent != 1 || len(r.Skipped) != 1 {
		t.Fatalf("result: %+v", r)
	}
	p = parse(t, e.smtp.Messages()[1].Data)
	if p.subject != "原件" || p.files["电费账单.pdf"] != "%PDF original 10" || len(p.files) != 1 {
		t.Fatalf("originals: %+v", p)
	}
}

func TestSendAlerts(t *testing.T) {
	e := newEnv(t, nil)
	for _, q := range []string{
		`INSERT INTO drawers (id, workspace_id, name, created_at, updated_at) VALUES (5, 1, '水电燃气', 1, 1)`,
		`INSERT INTO drawer_facts (id, drawer_id, kind, label, config, created_at) VALUES (8, 5, 'expect', '电费账单', '{"period":"month"}', 1)`,
		`INSERT INTO alerts (id, workspace_id, drawer_id, fact_id, kind, period_key, period_label, status, created_at, updated_at) VALUES
		 (1, 1, 5, 8, 'missing', 'expect:2026-09', '9 月', 'open', 1, 1),
		 (2, 1, 5, 8, 'missing', 'expect:2026-10', '10 月', 'open', 1, 1),
		 (3, 1, 5, 8, 'missing', 'expect:2026-08', '8 月', 'dismissed', 1, 1)`,
	} {
		if _, err := e.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	// Nobody asked for reminder emails yet.
	if r, err := e.s.SendAlerts(e.ctx, 1); err != nil || r.Recipients != 0 || len(e.smtp.Messages()) != 0 {
		t.Fatalf("no recipients: %+v %v", r, err)
	}
	for _, u := range []int64{1, 3, 4} { // 3 is disabled, 4 has no address
		if _, err := e.s.SetPrefs(e.ctx, e.scope, u, true); err != nil {
			t.Fatal(err)
		}
	}
	p, err := e.s.Prefs(e.ctx, e.scope, 1)
	if err != nil || !p.EmailAlerts || p.Email != "wang@example.com" {
		t.Fatalf("prefs: %+v %v", p, err)
	}
	r, err := e.s.SendAlerts(e.ctx, 1)
	if err != nil || r.Alerts != 2 || r.Recipients != 1 {
		t.Fatalf("send: %+v %v", r, err)
	}
	msgs := e.smtp.Messages()
	if len(msgs) != 1 || msgs[0].To[0] != "wang@example.com" {
		t.Fatalf("messages: %+v", msgs)
	}
	m := parse(t, msgs[0].Data)
	if m.subject != "收好提醒：2 条新提醒（王家）" || !strings.Contains(m.text, "电费账单：9 月的还没有存入") ||
		!strings.Contains(m.text, "https://docs.example.com/drawers/5") || strings.Contains(m.text, "8 月") {
		t.Fatalf("digest: %+v", m)
	}
	// Each reminder is emailed once.
	if r, err := e.s.SendAlerts(e.ctx, 1); err != nil || r.Alerts != 0 || len(e.smtp.Messages()) != 1 {
		t.Fatalf("again: %+v %v", r, err)
	}

	// Without a mail server the reminders wait.
	off := newEnv(t, func(c *config.SMTP) { c.Host = "" })
	if r, err := off.s.SendAlerts(off.ctx, 1); err != nil || r != (AlertsResult{}) {
		t.Fatalf("off: %+v %v", r, err)
	}
}

func isKind(err error, k apperr.Kind) bool {
	var e *apperr.Error
	return errors.As(err, &e) && e.Kind == k
}

func isCode(err error, code string) bool {
	var e *apperr.Error
	return errors.As(err, &e) && e.Code == code
}

func manyAddresses(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("u%d@example.com", i)
	}
	return out
}

// Mail names the instance as its administrator renamed it.
func TestMailUsesTheInstanceName(t *testing.T) {
	e := newEnv(t, nil)
	e.s.opt.Name = func(context.Context) string { return "家庭档案" }
	if err := e.s.SendTest(e.ctx, "wang@example.com"); err != nil {
		t.Fatal(err)
	}
	p := parse(t, e.smtp.Messages()[0].Data)
	if p.subject != "家庭档案：测试邮件" || !strings.Contains(p.text, "家庭档案的发信设置") || strings.Contains(p.text, "收好") {
		t.Fatalf("message: %+v", p)
	}
}

// TestSendQuotaWarning (v1.7.4): only the workspace's administrators who
// take reminder emails are told the storage is filling up.
func TestSendQuotaWarning(t *testing.T) {
	e := newEnv(t, nil)
	if sent, err := e.s.SendQuotaWarning(e.ctx, 1, false, 920<<20, 1<<30); err != nil || sent {
		t.Fatalf("nobody takes emails yet: %v %v", sent, err)
	}
	for _, u := range []int64{1, 2} { // 1 owns 王家, 2 only views it
		if _, err := e.s.SetPrefs(e.ctx, e.scope, u, true); err != nil {
			t.Fatal(err)
		}
	}
	sent, err := e.s.SendQuotaWarning(e.ctx, 1, false, 920<<20, 1<<30)
	msgs := e.smtp.Messages()
	if err != nil || !sent || len(msgs) != 1 || msgs[0].To[0] != "wang@example.com" {
		t.Fatalf("nearly full: %v %v %+v", sent, err, msgs)
	}
	m := parse(t, msgs[0].Data)
	if m.subject != "收好：空间“王家”快满了" || !strings.Contains(m.text, "920.0 MB") || !strings.Contains(m.text, "1.0 GB") ||
		!strings.Contains(m.text, "89%") || !strings.Contains(m.text, "https://docs.example.com/settings/storage") {
		t.Fatalf("nearly full: %+v", m)
	}
	if _, err := e.s.SendQuotaWarning(e.ctx, 1, true, 1<<30, 1<<30); err != nil {
		t.Fatal(err)
	}
	if m := parse(t, e.smtp.Messages()[1].Data); m.subject != "收好：空间“王家”已满，不能再上传" {
		t.Fatalf("full: %+v", m)
	}
	off := newEnv(t, func(c *config.SMTP) { c.Host = "" })
	if sent, err := off.s.SendQuotaWarning(off.ctx, 1, true, 1, 1); err != nil || sent {
		t.Fatalf("off: %v %v", sent, err)
	}
}

// v1.10.2: mail the server writes goes in each recipient's language: their
// account's, or the one they last used when it follows the browser.
func TestMailInTheRecipientsLanguage(t *testing.T) {
	e := newEnv(t, nil)
	for _, q := range []string{
		`UPDATE users SET locale = 'en' WHERE id = 1`,
		`UPDATE users SET locale = '', seen_lang = 'zh' WHERE id = 2`,
		`INSERT INTO drawers (id, workspace_id, name, created_at, updated_at) VALUES (5, 1, '水电燃气', 1, 1)`,
		`INSERT INTO drawer_facts (id, drawer_id, kind, label, config, created_at) VALUES (8, 5, 'expect', '电费账单', '{"period":"month"}', 1)`,
		`INSERT INTO alerts (id, workspace_id, drawer_id, fact_id, kind, period_key, period_label, status, created_at, updated_at) VALUES
		 (1, 1, 5, 8, 'missing', 'expect:2026-09', '9 月', 'open', 1, 1)`,
	} {
		if _, err := e.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.s.SetPrefs(e.ctx, e.scope, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.SendAlerts(e.ctx, 1); err != nil {
		t.Fatal(err)
	}
	m := parse(t, e.smtp.Messages()[0].Data)
	if m.subject != "Kept reminders: 1 new (王家)" || !strings.Contains(m.text, "Drawer: “水电燃气”") ||
		!strings.Contains(m.text, "To stop these emails, turn off email reminders in Kept under Settings → Notifications.") ||
		!strings.Contains(m.text, "https://docs.example.com/drawers/5") {
		t.Fatalf("English digest: %+v", m)
	}

	// a notice to two people: one copy each, in their own language
	id, err := e.s.QueueNotice(e.ctx, 1, []string{"wang@example.com", "li@example.com"}, "【收好】紧急查看已生效", "等待期已过，你现在可以只读查看家人的私密文档了。")
	if err != nil {
		t.Fatal(err)
	}
	before := len(e.smtp.Messages())
	if _, err := e.run(id); err != nil {
		t.Fatal(err)
	}
	got := map[string]parsed{}
	for _, raw := range e.smtp.Messages()[before:] {
		got[raw.To[0]] = parse(t, raw.Data)
	}
	if en := got["wang@example.com"]; en.subject != "[Kept] Emergency access is now active" || !strings.Contains(en.text, "The waiting period is over.") {
		t.Errorf("English notice: %+v", en)
	}
	if zh := got["li@example.com"]; zh.subject != "【收好】紧急查看已生效" || !strings.Contains(zh.text, "等待期已过") {
		t.Errorf("Chinese notice: %+v", zh)
	}

	// someone the server does not know, or who never said: Chinese
	if l := e.s.LangFor(e.ctx, "stranger@example.com", i18n.Zh); l != i18n.Zh {
		t.Errorf("stranger: %s", l)
	}
	if _, err := e.db.Exec(`UPDATE users SET seen_lang = 'en' WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	if l := e.s.LangFor(e.ctx, "LI@example.com", i18n.Zh); l != i18n.En {
		t.Errorf("following the browser, last seen in English: %s", l)
	}
}
