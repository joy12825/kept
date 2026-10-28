package ingest

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kept/server/internal/events"
	"kept/server/internal/jobs"
)

// v1.5.1: an .eml file is kept as the original; its body, printed by
// Gotenberg, is what is shown and searched; its subject and date name and
// date the document; its files become documents of their own.
func TestEmailFileIsIngested(t *testing.T) {
	e := newEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/forms/chromium/convert/html" {
			http.NotFound(w, r)
			return
		}
		w.Write(pdfBytes(t, "Statement 2024-03", "Total due 1280.00"))
	}))
	defer srv.Close()
	e.s.opt.Parser.GotenbergURL = srv.URL
	sub, cancel := e.bus.Subscribe(1)
	defer cancel()

	subject := "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("信用卡电子账单（2024年3月）")) + "?="
	eml := "From: Bank <cc@bank.example>\r\nTo: me@example.com\r\nSubject: " + subject + "\r\nDate: Tue, 05 Mar 2024 10:00:00 +0800\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=B\r\n\r\n--B\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<p>本期应还 1280.00 元</p>\r\n" +
		"--B\r\nContent-Type: text/plain\r\nContent-Disposition: attachment; filename=\"明细.txt\"\r\n\r\n消费明细：餐饮 300 元\r\n--B--\r\n"
	job, err := e.s.Submit(e.ctx, e.scope, "账单.eml", strings.NewReader(eml), Overrides{DocumentTypeID: 5}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	je := e.wait(sub, job)
	if je.Status != "succeeded" {
		t.Fatalf("job: %+v", je)
	}
	d, err := e.docs.GetDetail(e.ctx, e.scope, je.DocumentID)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != "信用卡电子账单（2024年3月）" || d.CreatedDate != "2024-03-05" || d.MimeType != "message/rfc822" || d.OriginalExt != "eml" ||
		!d.HasArchive || !strings.Contains(d.Content, "Total due 1280.00") || d.DocumentTypeID != 5 {
		t.Fatalf("e-mail document: %+v", d.Document)
	}
	if d.FieldSources.Date == nil || d.FieldSources.Date.Source != "file" || !strings.Contains(d.FieldSources.Date.Basis, "邮件发送时间") {
		t.Errorf("date source: %+v", d.FieldSources.Date)
	}
	// its file, in the inbox with the same presets
	var att JobEvent
	for att.DocumentID == 0 || att.DocumentID == je.DocumentID {
		select {
		case ev := <-sub:
			if x, ok := ev.Data.(JobEvent); ok && x.Status == "succeeded" && x.DocumentID != je.DocumentID {
				att = x
			}
		case <-t.Context().Done():
			t.Fatal("no document for the attachment")
		}
	}
	a, _ := e.docs.GetDetail(e.ctx, e.scope, att.DocumentID)
	if a.Title != "明细" || !strings.Contains(a.Content, "餐饮 300 元") || a.DocumentTypeID != 5 {
		t.Errorf("attachment document: %+v", a.Document)
	}
	_ = events.JobUpdated
}

// v1.9.1: without Gotenberg an e-mail is kept with its text, and waits;
// once Gotenberg is set up the scheduled job converts it.
func TestEmailWaitsForGotenberg(t *testing.T) {
	e := newEnv(t)
	sub, cancel := e.bus.Subscribe(1)
	defer cancel()
	job, err := e.s.Submit(e.ctx, e.scope, "a.eml", strings.NewReader("From: a@b.com\r\nSubject: hi\r\n\r\nhello there\r\n"), Overrides{}, Upload{})
	if err != nil {
		t.Fatal(err)
	}
	je := e.wait(sub, job)
	if je.Status != "succeeded" {
		t.Fatalf("without Gotenberg: %+v", je)
	}
	d, _ := e.docs.GetDetail(e.ctx, e.scope, je.DocumentID)
	if d.HasArchive || !strings.Contains(d.Content, "hello there") || !e.s.ConversionPending(e.ctx, 1, je.DocumentID) {
		t.Fatalf("kept waiting: %+v", d.Document)
	}
	// no Gotenberg yet: nothing queued
	ws := int64(1)
	if r, err := e.s.handleConvertPending(e.ctx, jobs.Job{WorkspaceID: &ws}); err != nil || r.(map[string]int)["queued"] != 0 {
		t.Fatalf("queued without Gotenberg: %v %v", r, err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(pdfBytes(t, "hello there, printed"))
	}))
	defer srv.Close()
	e.s.opt.Parser.GotenbergURL = srv.URL
	r, err := e.s.handleConvertPending(e.ctx, jobs.Job{WorkspaceID: &ws})
	if err != nil || r.(map[string]int)["queued"] != 1 {
		t.Fatalf("queued: %v %v", r, err)
	}
	// queued once: not again until it has had its chance
	if r, _ := e.s.handleConvertPending(e.ctx, jobs.Job{WorkspaceID: &ws}); r.(map[string]int)["queued"] != 0 {
		t.Errorf("queued twice: %v", r)
	}
	deadline := time.Now().Add(60 * time.Second)
	for e.s.ConversionPending(e.ctx, 1, je.DocumentID) {
		if time.Now().After(deadline) {
			t.Fatal("still waiting")
		}
		time.Sleep(50 * time.Millisecond)
	}
	d, _ = e.docs.GetDetail(e.ctx, e.scope, je.DocumentID)
	if !d.HasArchive || !strings.Contains(d.Content, "printed") {
		t.Errorf("converted: %+v", d.Document)
	}
}
