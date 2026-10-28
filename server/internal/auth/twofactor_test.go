package auth

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// validCode decodes a base32 TOTP secret and computes a code valid at c's
// current time.
func validCode(t *testing.T, secret string, c *clock) string {
	t.Helper()
	b, err := totpSecretEncoding.DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	step := c.Now().Unix() / 30
	return totpCode(b, step)
}

func TestTwoFactorFullFlow(t *testing.T) {
	s, c := newService(t)
	u := mustCreate(t, s, newUser("alice"))

	status, err := s.TwoFactorStatus(bg(), u.ID)
	if err != nil || status.Enabled {
		t.Fatalf("TwoFactorStatus() = %+v, %v, want disabled", status, err)
	}

	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	if setup.Secret == "" || !strings.Contains(setup.OtpauthURI, setup.Secret) {
		t.Fatalf("BeginTwoFactor() = %+v, want non-empty secret embedded in the URI", setup)
	}

	if _, err := s.EnableTwoFactor(bg(), u.ID, "000000"); !isKind(err, KindInvalid) {
		t.Fatalf("EnableTwoFactor(wrong code) = %v, want ErrWrongCode", err)
	}

	codes, err := s.EnableTwoFactor(bg(), u.ID, validCode(t, setup.Secret, c))
	if err != nil {
		t.Fatalf("EnableTwoFactor: %v", err)
	}
	if len(codes) != 10 {
		t.Fatalf("EnableTwoFactor() returned %d recovery codes, want 10", len(codes))
	}

	status, err = s.TwoFactorStatus(bg(), u.ID)
	if err != nil || !status.Enabled || status.RecoveryCodesLeft != 10 {
		t.Fatalf("TwoFactorStatus() = %+v, %v, want enabled with 10 codes left", status, err)
	}
}

func TestBeginTwoFactorConflict(t *testing.T) {
	s, c := newService(t)
	u := mustCreate(t, s, newUser("bob"))
	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	if _, err := s.EnableTwoFactor(bg(), u.ID, validCode(t, setup.Secret, c)); err != nil {
		t.Fatalf("EnableTwoFactor: %v", err)
	}
	if _, err := s.BeginTwoFactor(bg(), u.ID, u.Username); !isKind(err, KindConflict) {
		t.Fatalf("BeginTwoFactor(already enabled) = %v, want ErrTwoFactorEnabled", err)
	}
}

func TestDisableTwoFactor(t *testing.T) {
	s, c := newService(t)
	u := mustCreate(t, s, newUser("carol"))
	_, p := mustLogin(t, s, "carol")
	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	if _, err := s.EnableTwoFactor(bg(), u.ID, validCode(t, setup.Secret, c)); err != nil {
		t.Fatalf("EnableTwoFactor: %v", err)
	}

	if err := s.DisableTwoFactor(bg(), p, "wrong-password"); !isKind(err, KindInvalid) || err != ErrWrongPassword {
		t.Fatalf("DisableTwoFactor(wrong password) = %v, want ErrWrongPassword", err)
	}

	if err := s.DisableTwoFactor(bg(), p, "password-123"); err != nil {
		t.Fatalf("DisableTwoFactor: %v", err)
	}
	status, err := s.TwoFactorStatus(bg(), u.ID)
	if err != nil || status.Enabled || status.RecoveryCodesLeft != 0 {
		t.Fatalf("TwoFactorStatus() after disable = %+v, %v, want disabled with no codes", status, err)
	}
}

func TestDisableTwoFactorWhenNotEnabled(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newUser("gina"))
	_, p := mustLogin(t, s, "gina")
	if err := s.DisableTwoFactor(bg(), p, "password-123"); err != ErrTwoFactorNotEnabled {
		t.Fatalf("DisableTwoFactor(not enabled) = %v, want ErrTwoFactorNotEnabled", err)
	}
	if _, err := s.RegenerateRecoveryCodes(bg(), p, "password-123"); err != ErrTwoFactorNotEnabled {
		t.Fatalf("RegenerateRecoveryCodes(not enabled) = %v, want ErrTwoFactorNotEnabled", err)
	}
}

func TestRegenerateRecoveryCodes(t *testing.T) {
	s, c := newService(t)
	u := mustCreate(t, s, newUser("heidi"))
	_, p := mustLogin(t, s, "heidi")
	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	oldCodes, err := s.EnableTwoFactor(bg(), u.ID, validCode(t, setup.Secret, c))
	if err != nil {
		t.Fatalf("EnableTwoFactor: %v", err)
	}

	newCodes, err := s.RegenerateRecoveryCodes(bg(), p, "password-123")
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodes: %v", err)
	}
	if len(newCodes) != 10 {
		t.Fatalf("RegenerateRecoveryCodes() returned %d codes, want 10", len(newCodes))
	}

	// The old codes must no longer work.
	ok, err := s.checkSecondFactor(bg(), s.q, u.ID, "", oldCodes[0], c.Now())
	if err != nil || ok {
		t.Fatalf("checkSecondFactor(old recovery code after regeneration) = %v, %v, want (false, nil)", ok, err)
	}
	ok, err = s.checkSecondFactor(bg(), s.q, u.ID, "", newCodes[0], c.Now())
	if err != nil || !ok {
		t.Fatalf("checkSecondFactor(new recovery code) = %v, %v, want (true, nil)", ok, err)
	}
}

func TestAdminDisableTwoFactorIsIdempotent(t *testing.T) {
	s, _ := newService(t)
	u := mustCreate(t, s, newUser("dave"))
	if err := s.AdminDisableTwoFactor(bg(), u.ID); err != nil {
		t.Fatalf("AdminDisableTwoFactor (not enabled): %v", err)
	}
	if err := s.AdminDisableTwoFactor(bg(), u.ID); err != nil {
		t.Fatalf("AdminDisableTwoFactor (again): %v", err)
	}
	if err := s.AdminDisableTwoFactor(bg(), 999999); !isKind(err, KindNotFound) {
		t.Fatalf("AdminDisableTwoFactor(unknown user) = %v, want ErrUserNotFound", err)
	}
}

// ---------- checkSecondFactor ----------

func TestCheckSecondFactorCode(t *testing.T) {
	s, c := newService(t)
	u := mustCreate(t, s, newUser("erin"))
	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	code := validCode(t, setup.Secret, c)
	if _, err := s.EnableTwoFactor(bg(), u.ID, code); err != nil {
		t.Fatalf("EnableTwoFactor: %v", err)
	}

	// Advance to the next time step so a fresh code is used (the one from
	// setup is now the last-used step and would be rejected as a replay).
	c.Add(30 * time.Second)
	nextCode := validCode(t, setup.Secret, c)

	if ok, err := s.checkSecondFactor(bg(), s.q, u.ID, "000000", "", c.Now()); err != nil || ok {
		t.Fatalf("checkSecondFactor(wrong code) = %v, %v, want (false, nil)", ok, err)
	}
	if ok, err := s.checkSecondFactor(bg(), s.q, u.ID, nextCode, "", c.Now()); err != nil || !ok {
		t.Fatalf("checkSecondFactor(valid code) = %v, %v, want (true, nil)", ok, err)
	}
	// Replay of the same code must now fail.
	if ok, err := s.checkSecondFactor(bg(), s.q, u.ID, nextCode, "", c.Now()); err != nil || ok {
		t.Fatalf("checkSecondFactor(replayed code) = %v, %v, want (false, nil)", ok, err)
	}
}

func TestCheckSecondFactorRecoveryCodeIsOneTime(t *testing.T) {
	s, c := newService(t)
	u := mustCreate(t, s, newUser("frank"))
	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	codes, err := s.EnableTwoFactor(bg(), u.ID, validCode(t, setup.Secret, c))
	if err != nil {
		t.Fatalf("EnableTwoFactor: %v", err)
	}
	rc := codes[0]

	if ok, err := s.checkSecondFactor(bg(), s.q, u.ID, "", "not-a-real-code", c.Now()); err != nil || ok {
		t.Fatalf("checkSecondFactor(bad recovery code) = %v, %v, want (false, nil)", ok, err)
	}
	// Case and formatting (spaces, hyphen) should not matter.
	mangled := strings.ToUpper(strings.ReplaceAll(rc, "-", " "))
	if ok, err := s.checkSecondFactor(bg(), s.q, u.ID, "", mangled, c.Now()); err != nil || !ok {
		t.Fatalf("checkSecondFactor(recovery code) = %v, %v, want (true, nil)", ok, err)
	}
	if ok, err := s.checkSecondFactor(bg(), s.q, u.ID, "", rc, c.Now()); err != nil || ok {
		t.Fatalf("checkSecondFactor(reused recovery code) = %v, %v, want (false, nil)", ok, err)
	}
}

// TestEnableTwoFactorIsAtomic: two concurrent confirmations of the same
// pending secret must not both succeed (the loser's recovery codes would be
// silently replaced); reading the pending secret, checking the code and
// switching two-factor on happen in one transaction.
func TestEnableTwoFactorIsAtomic(t *testing.T) {
	s, c := newService(t)
	u := mustCreate(t, s, newUser("alice"))
	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	code := validCode(t, setup.Secret, c)

	for round := range 5 {
		var wg sync.WaitGroup
		errs := make([]error, 4)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = s.EnableTwoFactor(bg(), u.ID, code)
			}()
		}
		wg.Wait()
		successes := 0
		for _, err := range errs {
			if err == nil {
				successes++
			}
		}
		if successes != 1 {
			t.Fatalf("round %d: %d concurrent EnableTwoFactor calls succeeded, want exactly 1 (errs %v)", round, successes, errs)
		}
		// Start over for the next round.
		if err := s.AdminDisableTwoFactor(bg(), u.ID); err != nil {
			t.Fatal(err)
		}
		c.Add(30 * time.Second)
		if setup, err = s.BeginTwoFactor(bg(), u.ID, u.Username); err != nil {
			t.Fatal(err)
		}
		code = validCode(t, setup.Secret, c)
	}
}

// Authenticator apps show the instance's name.
func TestTwoFactorUsesTheInstanceName(t *testing.T) {
	s, _ := newService(t)
	s.name = func(context.Context) string { return "家庭档案" }
	u := mustCreate(t, s, newUser("wang"))
	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(setup.OtpauthURI, "otpauth://totp/"+url.PathEscape("家庭档案:wang")) || !strings.Contains(setup.OtpauthURI, "issuer="+url.QueryEscape("家庭档案")) {
		t.Errorf("uri = %s", setup.OtpauthURI)
	}
}
