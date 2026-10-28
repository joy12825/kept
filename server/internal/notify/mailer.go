// Package notify sends mail (v0.3): documents as attachments, the daily
// reminder digest, and the administrator's test message. Outgoing mail
// goes through one SMTP server configured with KEPT_SMTP_*.
package notify

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"kept/server/internal/i18n"
	"mime"
	"net"
	"net/smtp"
	"net/textproto"
	"path/filepath"
	"strconv"
	"time"

	gomail "github.com/emersion/go-message/mail"

	"kept/server/internal/config"
	"kept/server/internal/jobs"
)

// Message is one email: plain text with optional attachments.
type Message struct {
	To          []string
	ReplyTo     string
	Subject     string
	Text        string
	Attachments []Attachment
}

// Attachment is a file read when the message is written.
type Attachment struct {
	Name string
	Open func() (io.ReadCloser, error)
}

// Mailer delivers a message. Tests use an in-memory one.
type Mailer interface {
	Send(ctx context.Context, from string, to []string, write func(io.Writer) error) error
}

// Write renders m as MIME: UTF-8 headers, a text part, then base64
// attachments.
func Write(w io.Writer, from string, m Message, now time.Time) error {
	var h gomail.Header
	f, err := gomail.ParseAddress(from)
	if err != nil {
		return fmt.Errorf("sender %q: %w", from, err)
	}
	h.SetAddressList("From", []*gomail.Address{f})
	to := make([]*gomail.Address, len(m.To))
	for i, a := range m.To {
		to[i] = &gomail.Address{Address: a}
	}
	h.SetAddressList("To", to)
	if m.ReplyTo != "" {
		h.SetAddressList("Reply-To", []*gomail.Address{{Address: m.ReplyTo}})
	}
	h.SetSubject(m.Subject)
	h.SetDate(now)
	if err := h.GenerateMessageID(); err != nil {
		return err
	}
	var text gomail.InlineHeader
	text.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
	if len(m.Attachments) == 0 {
		h.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		h.Set("Content-Transfer-Encoding", "quoted-printable")
		body, err := gomail.CreateSingleInlineWriter(w, h)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(body, m.Text); err != nil {
			return err
		}
		return body.Close()
	}
	mw, err := gomail.CreateWriter(w, h)
	if err != nil {
		return err
	}
	tw, err := mw.CreateSingleInline(text)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(tw, m.Text); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	for _, a := range m.Attachments {
		if err := attach(mw, a); err != nil {
			return err
		}
	}
	return mw.Close()
}

func attach(mw *gomail.Writer, a Attachment) error {
	f, err := a.Open()
	if err != nil {
		return fmt.Errorf("read the attachment %s: %w", a.Name, err)
	}
	defer f.Close()
	var ah gomail.AttachmentHeader
	ct := mime.TypeByExtension(filepath.Ext(a.Name))
	if ct == "" {
		ct = "application/octet-stream"
	}
	ah.SetContentType(ct, nil)
	ah.SetFilename(a.Name)
	aw, err := mw.CreateAttachment(ah)
	if err != nil {
		return err
	}
	if _, err := io.Copy(aw, f); err != nil {
		return err
	}
	return aw.Close()
}

// SMTP sends through the configured server.
type SMTP struct {
	cfg     config.SMTP
	timeout time.Duration
}

func NewSMTP(cfg config.SMTP) *SMTP { return &SMTP{cfg: cfg, timeout: 2 * time.Minute} }

// Error is a failed delivery explained in Chinese; Permanent errors (5xx
// replies) are not retried.
type Error struct {
	Message   string
	Permanent bool
	Err       error
}

func (e *Error) Error() string { return e.Message + "：" + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

func fail(msg string, err error) error {
	var tp *textproto.Error
	return &Error{Message: msg, Permanent: errors.As(err, &tp) && tp.Code >= 500, Err: err}
}

func (s *SMTP) Send(ctx context.Context, from string, to []string, write func(io.Writer) error) error {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	d := net.Dialer{Timeout: 30 * time.Second}
	var conn net.Conn
	var err error
	if s.cfg.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: s.cfg.Host}}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fail(fmt.Sprintf(i18n.N("连不上邮件服务器 %s"), addr), err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(s.timeout))
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		return fail(i18n.N("邮件服务器没有正常应答"), err)
	}
	defer c.Close()
	if s.cfg.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return &Error{Message: "邮件服务器不支持 STARTTLS", Permanent: true, Err: errors.New("no STARTTLS; set KEPT_SMTP_SECURITY to tls or none")}
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.cfg.Host}); err != nil {
			return fail(i18n.N("与邮件服务器建立加密连接失败"), err)
		}
	}
	if s.cfg.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fail(i18n.N("登录邮件服务器失败，请检查用户名和密码"), err)
		}
	}
	sender, err := gomail.ParseAddress(from)
	if err != nil {
		return &Error{Message: "发件人地址不正确", Permanent: true, Err: err}
	}
	if err := c.Mail(sender.Address); err != nil {
		return fail(fmt.Sprintf(i18n.N("邮件服务器拒绝了发件人 %s"), sender.Address), err)
	}
	for _, r := range to {
		if err := c.Rcpt(r); err != nil {
			return fail(fmt.Sprintf(i18n.N("邮件服务器拒绝了收件人 %s"), r), err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fail(i18n.N("邮件服务器拒绝接收邮件"), err)
	}
	if err := write(w); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return fail(i18n.N("邮件服务器拒绝了这封邮件"), err)
	}
	return c.Quit()
}

// retryable marks permanent delivery errors so the job queue gives up.
func retryable(err error) error {
	var e *Error
	if errors.As(err, &e) && e.Permanent {
		return jobs.Permanent(err)
	}
	return err
}
