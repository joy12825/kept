package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	h, err := HashPassword("correct horse 电池")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("unexpected hash format: %s", h)
	}
	if !VerifyPassword(h, "correct horse 电池") {
		t.Error("correct password rejected")
	}
	if VerifyPassword(h, "correct horse") {
		t.Error("wrong password accepted")
	}
}

func TestHashesAreSalted(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Error("two hashes of the same password must differ")
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	for _, h := range []string{"", "plain", "$argon2id$v=19$m=x$salt$hash", "$bcrypt$whatever"} {
		if VerifyPassword(h, "anything") {
			t.Errorf("malformed hash %q verified", h)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short"); err == nil {
		t.Error("7-char password accepted")
	}
	if err := ValidatePassword(strings.Repeat("a", 257)); err == nil {
		t.Error("257-char password accepted")
	}
	if err := ValidatePassword("长度足够的中文密码"); err != nil {
		t.Errorf("valid password rejected: %v", err)
	}
}
