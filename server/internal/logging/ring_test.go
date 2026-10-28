package logging

import (
	"io"
	"log/slog"
	"testing"
)

func TestRingKeepsNewestFirstAndFilters(t *testing.T) {
	r := NewRing(3)
	log := slog.New(&teeHandler{inner: slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}), ring: r})
	log.Info("http request", "status", 200) // not kept
	log.Warn("http request", "status", 404) // kept
	log.With("component", "jobs").Error("job failed", slog.Group("job", "id", 7))
	log.Debug("one")
	log.Info("two")
	es := r.Entries(slog.LevelDebug, "", 10)
	if len(es) != 3 || es[0].Message != "two" || es[2].Message != "job failed" {
		t.Fatalf("entries = %+v", es)
	}
	if got := es[2].Attrs; len(got) != 2 || got[0] != [2]string{"component", "jobs"} || got[1] != [2]string{"job.id", "7"} {
		t.Errorf("attrs = %v", got)
	}
	if es := r.Entries(slog.LevelWarn, "", 10); len(es) != 1 || es[0].Level != "ERROR" {
		t.Errorf("warn and above = %+v", es)
	}
	if es := r.Entries(slog.LevelDebug, "JOBS", 10); len(es) != 1 {
		t.Errorf("search = %+v", es)
	}
}
