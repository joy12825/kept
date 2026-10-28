package parser

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kept/server/internal/mailpage"
	"kept/server/internal/msgfile"
)

// emailType is an e-mail saved as a file (.eml, v1.5.1). Like an Office
// file its body is printed to a PDF by Gotenberg, which is what is shown and
// searched; the .eml stays as the original. Without Gotenberg (v1.9.1) its
// text and files are read all the same, and the PDF waits.
const emailType = "message/rfc822"

// msgType is an Outlook message saved as a file (.msg, v1.9.7): read into
// the same shape as an .eml, and kept the same way.
const msgType = "application/vnd.ms-outlook"

// IsEmail reports whether mime is an e-mail saved as a file.
func IsEmail(mime string) bool { return mime == emailType || mime == msgType }

// EmailTypes lists the e-mail types (for the type filter).
func EmailTypes() []string { return []string{emailType, msgType} }

// EmailMeta is what an .eml file says about itself.
type EmailMeta struct {
	Subject     string
	Date        time.Time
	Attachments []mailpage.Attachment
}

// maxEmailFiles bounds the files taken from one e-mail.
const maxEmailFiles = 25

// readEmail reads the message at src: what it says about itself, with its
// files (not the images its body shows).
func readEmail(src string) (mailpage.Message, *EmailMeta, error) {
	raw, err := os.ReadFile(src)
	if err != nil {
		return mailpage.Message{}, nil, err
	}
	var msg mailpage.Message
	var files []mailpage.Attachment
	if bytes.HasPrefix(raw, msgfile.Magic) {
		msg, files, err = fromMSG(raw)
	} else {
		msg, files, err = mailpage.Parse(raw)
	}
	if err != nil {
		return mailpage.Message{}, nil, &Error{Message: "邮件文件读不出来，可能已损坏", Permanent: true, Err: err}
	}
	meta := &EmailMeta{Subject: mailpage.Title(msg), Date: msg.Date}
	for _, f := range files {
		if !msg.Shows(f) && len(meta.Attachments) < maxEmailFiles {
			meta.Attachments = append(meta.Attachments, f)
		}
	}
	return msg, meta, nil
}

// convertEmail prints the message at src to a PDF in workDir.
func convertEmail(ctx context.Context, gotenberg, src, workDir string) (string, *EmailMeta, error) {
	msg, meta, err := readEmail(src)
	if err != nil {
		return "", nil, err
	}
	index, assets := mailpage.Page(msg)
	pdf, err := HTMLToPDF(ctx, gotenberg, index, assets)
	if err != nil {
		return "", nil, err
	}
	dst := filepath.Join(workDir, "email.pdf")
	if err := os.WriteFile(dst, pdf, 0o600); err != nil {
		return "", nil, err
	}
	return dst, meta, nil
}

// emailText is the searchable text of a message kept without its printed
// PDF (v1.9.1): the header lines its page shows, then the body.
func emailText(msg mailpage.Message) string {
	var b strings.Builder
	line := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			b.WriteString(k + "：" + v + "\n")
		}
	}
	line("发件人", msg.From)
	line("收件人", msg.To)
	if !msg.Date.IsZero() {
		line("时间", msg.Date.Format("2006-01-02 15:04"))
	}
	line("主题", msg.Subject)
	body := msg.Text
	if strings.TrimSpace(body) == "" {
		body = mailpage.PlainText(msg.HTML)
	}
	b.WriteString("\n" + strings.TrimSpace(body))
	return strings.TrimSpace(b.String())
}

// fromMSG reads an Outlook message as the .eml reader would: the images the
// HTML body shows by Content-ID go inline.
func fromMSG(raw []byte) (mailpage.Message, []mailpage.Attachment, error) {
	m, err := msgfile.Parse(raw)
	if err != nil {
		return mailpage.Message{}, nil, err
	}
	msg := mailpage.Message{From: m.From, To: m.To, Subject: m.Subject, Date: m.Date, HTML: m.HTML, Text: m.Text, Inline: map[string]mailpage.Inline{}}
	var files []mailpage.Attachment
	for _, a := range m.Attachments {
		if a.CID != "" && strings.HasPrefix(a.MIME, "image/") {
			msg.Inline[a.CID] = mailpage.Inline{MIME: a.MIME, Data: a.Data}
		}
		if a.Name != "" {
			files = append(files, mailpage.Attachment{Name: a.Name, CID: a.CID, Data: a.Data})
		}
	}
	return msg, files, nil
}
