package msgfile_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"kept/server/internal/msgfile"
	"kept/server/internal/msgtest"
)

func sample(gbk bool) msgtest.Message {
	return msgtest.Message{
		Subject: "物业费缴费通知", FromName: "物业服务中心", FromEmail: "wuye@example.com",
		To:   []msgtest.Recipient{{Name: "王小明", Email: "wang@example.com"}, {Name: "李华", Email: "li@example.com"}},
		Date: time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC),
		Body: "您好，2026 年第四季度物业费 1,260 元，请于 10 月 31 日前缴纳。", HTML: `<p>您好，物业费 <b>1,260</b> 元。</p><img src="cid:logo@wuye">`,
		GBK: gbk,
		Attachments: []msgtest.Attachment{
			{Name: "缴费单.pdf", MIME: "application/pdf", Data: bytes.Repeat([]byte("%PDF-1.4 "), 1000)}, // over 4096 bytes: in sectors
			{Name: "logo.png", CID: "logo@wuye", MIME: "image/png", Data: []byte("\x89PNG small")},
		},
	}
}

func TestParse(t *testing.T) {
	for _, gbk := range []bool{false, true} {
		m, err := msgfile.Parse(msgtest.Build(sample(gbk)))
		if err != nil {
			t.Fatalf("gbk=%v: %v", gbk, err)
		}
		if m.Subject != "物业费缴费通知" || m.From != "物业服务中心 <wuye@example.com>" {
			t.Errorf("gbk=%v: subject %q from %q", gbk, m.Subject, m.From)
		}
		if m.To != "王小明 <wang@example.com>, 李华 <li@example.com>" {
			t.Errorf("gbk=%v: to %q", gbk, m.To)
		}
		if !m.Date.Equal(time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC)) {
			t.Errorf("date %v", m.Date)
		}
		if !strings.Contains(m.Text, "1,260 元") || !strings.Contains(m.HTML, "<b>1,260</b>") {
			t.Errorf("gbk=%v: text %q html %q", gbk, m.Text, m.HTML)
		}
		if len(m.Attachments) != 2 {
			t.Fatalf("attachments %d", len(m.Attachments))
		}
		a, b := m.Attachments[0], m.Attachments[1]
		if a.Name != "缴费单.pdf" || len(a.Data) != 9000 || a.MIME != "application/pdf" {
			t.Errorf("big attachment %q %d %q", a.Name, len(a.Data), a.MIME)
		}
		if b.CID != "logo@wuye" || string(b.Data) != "\x89PNG small" {
			t.Errorf("inline %q %q", b.CID, b.Data)
		}
	}
}

func TestParseBroken(t *testing.T) {
	good := msgtest.Build(sample(false))
	cases := map[string]func([]byte){
		"not a compound file":              func(b []byte) { b[0] = 'X' },
		"directory sector out of the file": func(b []byte) { binary.LittleEndian.PutUint32(b[0x30:], 99999) },
		"allocation table loops": func(b []byte) {
			dir := binary.LittleEndian.Uint32(b[0x30:])
			binary.LittleEndian.PutUint32(b[512+4*int(dir):], dir) // the directory's chain points at itself
		},
	}
	for name, spoil := range cases {
		b := append([]byte(nil), good...)
		spoil(b)
		if _, err := msgfile.Parse(b); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := msgfile.Parse(good[:300]); err == nil {
		t.Error("truncated: no error")
	}
}
