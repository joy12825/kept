package apperr

import (
	"errors"
	"fmt"
	"testing"
)

func TestWithKeepsTheSentinelUntouchedAndStillMatchesIt(t *testing.T) {
	sentinel := New(KindRateLimited, "稍后再试")
	other := New(KindRateLimited, "稍后再试")

	derived := sentinel.With(map[string]any{"retryAfterSeconds": 30})
	if sentinel.Details != nil {
		t.Error("With mutated the shared sentinel")
	}
	if derived.Details["retryAfterSeconds"] != 30 || derived.Kind != KindRateLimited || derived.Message != "稍后再试" {
		t.Errorf("derived = %+v", derived)
	}
	if !errors.Is(derived, sentinel) {
		t.Error("a derived error should match its sentinel")
	}
	if errors.Is(derived, other) {
		t.Error("a derived error must not match a different sentinel with the same text")
	}
	if !errors.Is(derived.With(map[string]any{"x": 1}), sentinel) {
		t.Error("deriving twice should still match the original sentinel")
	}
	if !errors.Is(fmt.Errorf("wrapped: %w", derived), sentinel) {
		t.Error("wrapping should not hide the sentinel")
	}
	if errors.Is(derived, errors.New("稍后再试")) {
		t.Error("an unrelated error type must not match")
	}
}

func TestWithCode(t *testing.T) {
	e := WithCode(KindNotFound, "workspace_not_found", "找不到这个空间")
	if e.Kind != KindNotFound || e.Code != "workspace_not_found" || e.Error() != "找不到这个空间" {
		t.Errorf("e = %+v", e)
	}
	if New(KindInvalid, "x").Code != "" {
		t.Error("New should leave Code empty so the Kind's default applies")
	}
}
