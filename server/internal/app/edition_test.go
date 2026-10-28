package app

import (
	"context"
	"testing"

	"kept/server/internal/jobs"
)

// A paid entry (wired by wireEE, v1.13) runs only in its edition, and its
// own condition still counts.
func TestPaidEntriesKeepTheirCondition(t *testing.T) {
	own := false
	e := onlyIn(jobs.Entry{Name: "x", When: func(context.Context) bool { return own }}, func() bool { return true })
	if e.When(context.Background()) {
		t.Error("its own condition was dropped")
	}
	own = true
	if !e.When(context.Background()) || onlyIn(e, func() bool { return false }).When(context.Background()) {
		t.Error("the edition's check is not applied")
	}
}
