package mailpage

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"strings"

	_ "github.com/emersion/go-message/charset" // GBK, GB2312, Big5 … in Chinese mail
	gomail "github.com/emersion/go-message/mail"
)

// Attachment is a file in a message: an attachment, or an inline part with
// a name.
type Attachment struct {
	Name string
	CID  string // Content-ID without <>, when it has one
	Data []byte
}

// maxParts bounds how many parts of one message are read.
const maxParts = 200

// Parse reads a raw message (RFC 5322, an .eml file): who, when, what, the
// body (HTML and plain text), the images it shows and its files.
func Parse(raw []byte) (Message, []Attachment, error) {
	r, err := gomail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return Message{}, nil, err
	}
	m := Message{Inline: map[string]Inline{}}
	m.Subject, _ = r.Header.Subject()
	m.From, m.To = addresses(r.Header, "From"), addresses(r.Header, "To")
	m.Date, _ = r.Header.Date()
	var files []Attachment
	for i := 0; i < maxParts; i++ {
		p, err := r.NextPart()
		if errors.Is(err, io.EOF) || err != nil {
			break
		}
		data, err := io.ReadAll(p.Body)
		if err != nil {
			continue
		}
		name := partFilename(p.Header)
		ct, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		cid := strings.Trim(p.Header.Get("Content-Id"), "<> ")
		if _, ok := p.Header.(*gomail.InlineHeader); ok && name == "" {
			switch {
			case ct == "text/html" && m.HTML == "":
				m.HTML = string(data)
			case (ct == "text/plain" || ct == "") && m.Text == "":
				m.Text = string(data)
			}
		}
		if strings.HasPrefix(ct, "image/") && cid != "" {
			m.Inline[cid] = Inline{MIME: ct, Data: data}
		}
		if name != "" {
			files = append(files, Attachment{Name: name, CID: cid, Data: data})
		}
	}
	return m, files, nil
}

// Shows reports whether the body shows the attachment (a logo, a
// signature): such an image is part of the body, not a file of its own.
func (m Message) Shows(a Attachment) bool {
	return a.CID != "" && strings.Contains(strings.ToLower(m.HTML), "cid:"+strings.ToLower(a.CID))
}

// Sender is who sent the message, as a name: the display name, else the
// address.
func (m Message) Sender() string {
	from := m.From
	if i := strings.Index(from, ","); i >= 0 {
		from = from[:i]
	}
	if i := strings.Index(from, " <"); i > 0 {
		return strings.TrimSpace(from[:i])
	}
	return strings.Trim(strings.TrimSpace(from), "<>")
}

// partFilename is an attachment's (or a named inline part's) file name.
func partFilename(h gomail.PartHeader) string {
	switch h := h.(type) {
	case *gomail.AttachmentHeader:
		name, _ := h.Filename()
		return name
	case *gomail.InlineHeader:
		if _, params, err := h.ContentType(); err == nil && params["name"] != "" {
			return params["name"]
		}
		if _, params, err := mime.ParseMediaType(h.Get("Content-Disposition")); err == nil {
			return params["filename"]
		}
	}
	return ""
}

// addresses renders a header's address list: “张三 <a@b.com>, …”.
func addresses(h gomail.Header, key string) string {
	list, err := h.AddressList(key)
	if err != nil {
		return h.Get(key)
	}
	parts := make([]string, len(list))
	for i, a := range list {
		if a.Name != "" {
			parts[i] = a.Name + " <" + a.Address + ">"
		} else {
			parts[i] = a.Address
		}
	}
	return strings.Join(parts, ", ")
}
