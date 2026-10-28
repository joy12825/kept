package auth

import (
	"errors"
	"testing"
)

func refusal(err error) string {
	var r *SSORefusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}

func TestLoginSSO(t *testing.T) {
	s, _ := newService(t)
	wang := mustCreate(t, s, newUser("wang"))
	li := mustCreate(t, s, newUser("lili"))
	const iss = "https://id.example.com"

	// First time: bound by the verified email (any case); kept's 2FA is not asked.
	tok, p, err := s.LoginSSO(bg(), SSOIdentity{Issuer: iss, Subject: "s-wang", Email: "WANG@example.com", EmailVerified: true}, true, info())
	if err != nil || tok == "" || p.UserID != wang.ID || !p.Persistent {
		t.Fatalf("first = %v %+v", err, p)
	}
	if u, _ := s.GetUser(bg(), wang.ID); !u.SSO {
		t.Error("not bound")
	}
	// Then by subject, whatever the email says now.
	if _, p, err := s.LoginSSO(bg(), SSOIdentity{Issuer: iss, Subject: "s-wang", Email: "new@example.com"}, false, info()); err != nil || p.UserID != wang.ID || p.Persistent {
		t.Fatalf("by subject = %v %+v", err, p)
	}

	for _, c := range []struct {
		id   SSOIdentity
		want string
	}{
		{SSOIdentity{Issuer: iss, Subject: "s-x", Email: "nobody@example.com", EmailVerified: true}, SSONoAccount},
		{SSOIdentity{Issuer: iss, Subject: "s-x", Email: "lili@example.com"}, SSOUnverified},
		{SSOIdentity{Issuer: iss, Subject: "s-x", EmailVerified: true}, SSONoAccount},
		// wang is bound to s-wang: another person there with wang's email is refused.
		{SSOIdentity{Issuer: iss, Subject: "s-other", Email: "wang@example.com", EmailVerified: true}, SSOTaken},
	} {
		if _, _, err := s.LoginSSO(bg(), c.id, false, info()); refusal(err) != c.want {
			t.Errorf("%+v: %v, want %s", c.id, err, c.want)
		}
	}

	// Disabled accounts cannot sign in this way either.
	if err := s.SetDisabled(bg(), li.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LoginSSO(bg(), SSOIdentity{Issuer: iss, Subject: "s-li", Email: "lili@example.com", EmailVerified: true}, false, info()); refusal(err) != SSODisabled {
		t.Errorf("disabled: %v", err)
	}

	// Unbinding: the next sign-in binds again by email.
	if err := s.UnlinkSSO(bg(), wang.ID); err != nil {
		t.Fatal(err)
	}
	if u, _ := s.GetUser(bg(), wang.ID); u.SSO {
		t.Error("still bound")
	}
	if _, p, err := s.LoginSSO(bg(), SSOIdentity{Issuer: iss, Subject: "s-other", Email: "wang@example.com", EmailVerified: true}, false, info()); err != nil || p.UserID != wang.ID {
		t.Errorf("after unbinding = %v", err)
	}
	if err := s.UnlinkSSO(bg(), 999); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user: %v", err)
	}
}
