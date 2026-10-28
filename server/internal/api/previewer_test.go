package api

import (
	"net/http"
	"strings"
	"testing"
)

// v1.9.1: an e-mail taken without Gotenberg says it waits for its PDF; the
// original can be shown in the page; sound is taken as it is.
func TestPreviewerFiles(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	r := alice.upload("账单.eml", "From: bank@example.com\r\nSubject: bill\r\n\r\nTotal due 1280.00\r\n", nil)
	if r.code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", r.code, r.raw)
	}
	job := alice.waitJob(r.body["jobId"].(float64))
	if job["status"] != "succeeded" {
		t.Fatalf("job = %v", job)
	}
	id := itoa(job["documentId"].(float64))
	d := alice.mustDo(http.MethodGet, "/api/v1/documents/"+id, nil, http.StatusOK).body
	if d["conversionPending"] != true || d["hasArchive"] != false || !strings.Contains(d["content"].(string), "Total due 1280.00") {
		t.Errorf("e-mail = %v", d)
	}
	p := alice.do(http.MethodGet, "/api/v1/documents/"+id+"/preview?original=true", nil)
	if p.code != http.StatusOK || !strings.HasPrefix(p.header.Get("Content-Disposition"), "inline;") || p.header.Get("Content-Type") != "message/rfc822" {
		t.Errorf("original inline: %d %v", p.code, p.header)
	}

	wav := "RIFF\x2c\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x40\x1f\x00\x00\x80\x3e\x00\x00\x02\x00\x10\x00data\x08\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"
	r = alice.upload("录音.wav", wav, nil)
	if r.code != http.StatusAccepted {
		t.Fatalf("upload wav: %d %s", r.code, r.raw)
	}
	job = alice.waitJob(r.body["jobId"].(float64))
	if job["status"] != "succeeded" {
		t.Fatalf("wav job = %v", job)
	}
	id = itoa(job["documentId"].(float64))
	d = alice.mustDo(http.MethodGet, "/api/v1/documents/"+id, nil, http.StatusOK).body
	if d["mimeType"] != "audio/wav" || d["conversionPending"] != nil {
		t.Errorf("wav = %v", d)
	}
	if p := alice.do(http.MethodGet, "/api/v1/documents/"+id+"/preview", nil); p.code != http.StatusOK || p.header.Get("Content-Type") != "audio/wav" {
		t.Errorf("wav preview: %d %v", p.code, p.header.Get("Content-Type"))
	}
}
