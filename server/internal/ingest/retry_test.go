package ingest

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"kept/server/internal/jobs"
	"kept/server/internal/parser"
)

// v1.9.9: the phone's "重试" after the processing failed sends the same
// upload ID again. The failed job gives it up and the upload starts over;
// an upload that is queued or done stays the same job.
func TestRetryAfterFailedProcessing(t *testing.T) {
	if m := parser.MissingTools(); len(m) > 0 {
		t.Skipf("missing tools: %v", m)
	}
	e := newEnv(t)
	sub, cancel := e.bus.Subscribe(1)
	defer cancel()
	up := Upload{ClientUploadID: "phone-1"}
	bad, err := e.s.Submit(e.ctx, e.scope, "scan.pdf", strings.NewReader("%PDF-1.4\n not really a pdf"), Overrides{}, up)
	if err != nil {
		t.Fatal(err)
	}
	if je := e.wait(sub, bad); je.Status != "failed" {
		t.Fatalf("first try: %+v", je)
	}
	again, err := e.s.Submit(e.ctx, e.scope, "scan.pdf", bytes.NewReader(pdfBytes(t, "Retried scan", "Water bill 58.00")), Overrides{}, up)
	if err != nil || again == bad {
		t.Fatalf("retry: job %d (first %d), %v", again, bad, err)
	}
	je := e.wait(sub, again)
	if je.Status != "succeeded" {
		t.Fatalf("retry: %+v", je)
	}
	st, err := e.s.Statuses(e.ctx, e.scope, []string{"phone-1"})
	if err != nil || len(st) != 1 || st[0].Status != "succeeded" || st[0].DocumentID != je.DocumentID {
		t.Errorf("status by upload ID: %+v %v", st, err)
	}
	// done: the same upload again is the same job
	if same, err := e.s.Submit(e.ctx, e.scope, "scan.pdf", bytes.NewReader(pdfBytes(t, "x")), Overrides{}, up); err != nil || same != again {
		t.Errorf("after success: job %d, want %d (%v)", same, again, err)
	}
}

// v1.9.12: a failed upload's file is kept for a retry, unless retrying ends
// the same way (a duplicate) or there is no file.
func TestKeepFailed(t *testing.T) {
	for _, c := range []struct {
		err  error
		keep bool
	}{
		{jobs.Permanent(parser.ErrUnsupported), true},
		{errors.New("ocrmypdf: timed out"), true},
		{jobs.Permanent(errDuplicate), false},
		{jobs.Permanent(errDuplicateInTrash), false},
		{jobs.Permanent(errUploadGone), false},
		{nil, false},
	} {
		if got := keepFailed(c.err); got != c.keep {
			t.Errorf("keepFailed(%v) = %v", c.err, got)
		}
	}
	if KeptForRetry(errDuplicate.Error()) || !KeptForRetry("处理失败") {
		t.Error("KeptForRetry")
	}
}
