package auth

import (
	"testing"
	"time"
)

// RFC 6238 Appendix B test vectors (SHA1 mode), secret is the ASCII string
// "12345678901234567890"; the RFC's 8-digit truncation is trimmed to the
// last 6 digits kept by this implementation.
func TestTotpCodeRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	cases := []struct {
		t    int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, c := range cases {
		step := c.t / 30
		got := totpCode(secret, step)
		if got != c.want {
			t.Errorf("totpCode(T=%d, step=%d) = %q, want %q", c.t, step, got, c.want)
		}
	}
}

func TestVerifyTOTPWindowAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	// T=1111111109 -> step 37037036, code 081804.
	step := int64(1111111109) / 30
	now := time.Unix(1111111109, 0).UTC()

	t.Run("current step matches", func(t *testing.T) {
		got, ok := verifyTOTP(secret, "081804", now, 0)
		if !ok || got != step {
			t.Fatalf("verifyTOTP() = (%d, %v), want (%d, true)", got, ok, step)
		}
	})

	t.Run("one step behind is accepted", func(t *testing.T) {
		late := now.Add(30 * time.Second) // now is one step behind "late"
		got, ok := verifyTOTP(secret, "081804", late, 0)
		if !ok || got != step {
			t.Fatalf("verifyTOTP() = (%d, %v), want (%d, true)", got, ok, step)
		}
	})

	t.Run("one step ahead is accepted", func(t *testing.T) {
		early := now.Add(-30 * time.Second)
		got, ok := verifyTOTP(secret, "081804", early, 0)
		if !ok || got != step {
			t.Fatalf("verifyTOTP() = (%d, %v), want (%d, true)", got, ok, step)
		}
	})

	t.Run("two steps away is rejected", func(t *testing.T) {
		tooLate := now.Add(60 * time.Second)
		if _, ok := verifyTOTP(secret, "081804", tooLate, 0); ok {
			t.Fatal("verifyTOTP() = true, want false for a two-step drift")
		}
	})

	t.Run("step must be greater than lastStep", func(t *testing.T) {
		if _, ok := verifyTOTP(secret, "081804", now, step); ok {
			t.Fatal("verifyTOTP() = true, want false when step <= lastStep")
		}
		if _, ok := verifyTOTP(secret, "081804", now, step+1); ok {
			t.Fatal("verifyTOTP() = true, want false when step <= lastStep")
		}
	})

	t.Run("wrong code is rejected", func(t *testing.T) {
		if _, ok := verifyTOTP(secret, "000000", now, 0); ok {
			t.Fatal("verifyTOTP() = true, want false for a wrong code")
		}
	})
}
