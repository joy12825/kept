package auth

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"kept/server/internal/store"
	"kept/server/internal/tenant"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }
func newClock() *clock               { return &clock{t: time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC)} }
func info() ClientInfo               { return ClientInfo{IP: "192.0.2.1", UserAgent: "test"} }
func bg() context.Context            { return context.Background() }
func isKind(err error, k Kind) bool  { var e *Error; return errors.As(err, &e) && e.Kind == k }
func newUser(name string) NewUser {
	return NewUser{Username: name, Password: "password-123", Email: name + "@example.com"}
}
func newAdmin(name string) NewUser { u := newUser(name); u.IsAdmin = true; return u }

func newService(t *testing.T) (*Service, *clock) {
	t.Helper()
	st, err := store.Open(bg(), filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	c := newClock()
	return NewService(st.DB, Options{SessionTTL: 30 * 24 * time.Hour, Now: c.Now}), c
}

func mustCreate(t *testing.T, s *Service, u NewUser) User {
	t.Helper()
	created, err := s.CreateUser(bg(), u)
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", u.Username, err)
	}
	return created
}

func mustLogin(t *testing.T, s *Service, name string) (string, Principal) {
	t.Helper()
	res, err := s.Login(bg(), name, "password-123", true, info())
	if err != nil {
		t.Fatalf("Login(%s): %v", name, err)
	}
	if res.Challenge != "" {
		t.Fatalf("Login(%s) returned a two-factor challenge, want a session", name)
	}
	return res.Token, res.Principal
}

// detailsOf extracts the Details map of an *Error, failing the test if err
// is not one.
func detailsOf(t *testing.T, err error) map[string]any {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v (%T), want *auth.Error", err, err)
	}
	return e.Details
}

// codeOf extracts the Code of an *Error, failing the test if err is not one.
func codeOf(t *testing.T, err error) string {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v (%T), want *auth.Error", err, err)
	}
	return e.Code
}

// countSessions is a white-box helper: the number of session rows currently
// stored, used to confirm a two-factor challenge does not create one.
func countSessions(t *testing.T, s *Service) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// enableTwoFactorFor creates a user and turns two-factor authentication on
// for it, returning the account and its TOTP secret.
func enableTwoFactorFor(t *testing.T, s *Service, c *clock, name string) (User, string) {
	t.Helper()
	u := mustCreate(t, s, newUser(name))
	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	if _, err := s.EnableTwoFactor(bg(), u.ID, validCode(t, setup.Secret, c)); err != nil {
		t.Fatalf("EnableTwoFactor: %v", err)
	}
	return u, setup.Secret
}

// --- setup & users ---

func TestSetupCreatesFirstAdminOnlyOnce(t *testing.T) {
	s, _ := newService(t)
	if need, _ := s.NeedsSetup(bg()); !need {
		t.Fatal("empty instance should need setup")
	}
	u, err := s.Setup(bg(), newUser("Admin"))
	if err != nil {
		t.Fatal(err)
	}
	if !u.IsAdmin || u.Username != "admin" {
		t.Errorf("setup user = %+v, want lower-cased admin", u)
	}
	if need, _ := s.NeedsSetup(bg()); need {
		t.Error("still needs setup after creating the admin")
	}
	if _, err := s.Setup(bg(), newUser("second")); !errors.Is(err, ErrSetupDone) {
		t.Errorf("second Setup err = %v, want ErrSetupDone", err)
	}
}

func TestSetupIsSafeUnderConcurrency(t *testing.T) {
	s, _ := newService(t)
	var wg sync.WaitGroup
	var ok sync.Map
	for i := range 5 {
		wg.Go(func() {
			if _, err := s.Setup(bg(), newUser("admin"+string(rune('a'+i)))); err == nil {
				ok.Store(i, true)
			}
		})
	}
	wg.Wait()
	n := 0
	ok.Range(func(any, any) bool { n++; return true })
	if n != 1 {
		t.Errorf("%d concurrent setups succeeded, want exactly 1", n)
	}
}

func TestCreateUserMakesPersonalWorkspace(t *testing.T) {
	s, _ := newService(t)
	u := mustCreate(t, s, NewUser{Username: "xiaowang", Password: "password-123", DisplayName: "小王", Email: "xiaowang@example.com"})
	ws, err := s.PersonalWorkspace(bg(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Kind != "personal" || ws.Role != tenant.RoleOwner || !strings.Contains(ws.Name, "小王") || ws.Color != "blue" {
		t.Errorf("workspace = %+v", ws)
	}
	var createdBy sql.NullInt64
	if err := s.db.QueryRowContext(bg(), `SELECT created_by FROM workspaces WHERE id = ?`, ws.ID).Scan(&createdBy); err != nil {
		t.Fatal(err)
	}
	if createdBy.Int64 != u.ID {
		t.Errorf("created_by = %v, want the new user %d", createdBy, u.ID)
	}
	var builtIns int
	if err := s.db.QueryRowContext(bg(), `SELECT count(*) FROM custom_fields WHERE workspace_id = ? AND key IN ('amount', 'due_date')`, ws.ID).Scan(&builtIns); err != nil {
		t.Fatal(err)
	}
	if builtIns != 2 {
		t.Errorf("the personal workspace has %d built-in fields, want 2", builtIns)
	}
}

func TestUsernameRules(t *testing.T) {
	s, _ := newService(t)
	for _, bad := range []string{"", "ab", "has space", "中文名", strings.Repeat("a", 33), "-dash", "a/b"} {
		if _, err := s.CreateUser(bg(), newUser(bad)); !isKind(err, KindInvalid) {
			t.Errorf("username %q: err = %v, want invalid", bad, err)
		}
	}
	mustCreate(t, s, newUser("Wang.Li_2"))
	dup := newUser("wang.li_2")
	dup.Email = "someone-else@example.com"
	if _, err := s.CreateUser(bg(), dup); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("case-insensitive duplicate: err = %v, want ErrUsernameTaken", err)
	}
}

func TestCreateUserRejectsWeakPassword(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.CreateUser(bg(), NewUser{Username: "wang", Password: "short", Email: "wang@example.com"}); !isKind(err, KindInvalid) {
		t.Errorf("err = %v, want invalid", err)
	}
}

// --- email as the second login identifier ---

func TestEmailIsRequired(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.CreateUser(bg(), NewUser{Username: "wang", Password: "password-123"}); !isKind(err, KindInvalid) {
		t.Errorf("missing email: err = %v, want invalid", err)
	}
	if _, err := s.CreateUser(bg(), NewUser{Username: "wang", Password: "password-123", Email: "not-an-email"}); !isKind(err, KindInvalid) {
		t.Errorf("malformed email: err = %v, want invalid", err)
	}
}

func TestEmailMustBeUnique(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, NewUser{Username: "wang", Password: "password-123", Email: "Wang@Example.com"})
	if _, err := s.CreateUser(bg(), NewUser{Username: "wang2", Password: "password-123", Email: "wang@example.com"}); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("duplicate email err = %v, want ErrEmailTaken", err)
	}
}

func TestUsernameIsGeneratedFromEmailWhenBlank(t *testing.T) {
	s, _ := newService(t)
	a := mustCreate(t, s, NewUser{Password: "password-123", Email: "Wang.Xiao@Example.com"})
	if a.Username != "wang.xiao" {
		t.Errorf("username = %q, want wang.xiao", a.Username)
	}
	b := mustCreate(t, s, NewUser{Password: "password-123", Email: "wang.xiao@other.com"})
	if b.Username != "wang.xiao2" {
		t.Errorf("username = %q, want wang.xiao2", b.Username)
	}
	c := mustCreate(t, s, NewUser{Password: "password-123", Email: "ab@x.com"})
	if c.Username != "user" {
		t.Errorf("username = %q, want user", c.Username)
	}
}

func TestLoginByEmail(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, NewUser{Username: "wang", Password: "password-123", Email: "wang@example.com"})
	if _, err := s.Login(bg(), "wang@example.com", "password-123", true, info()); err != nil {
		t.Errorf("login by email: %v", err)
	}
	if _, err := s.Login(bg(), "WANG@EXAMPLE.COM", "password-123", true, info()); err != nil {
		t.Errorf("login by upper-case email: %v", err)
	}
}

func TestUpdateProfileEmail(t *testing.T) {
	s, _ := newService(t)
	a := mustCreate(t, s, NewUser{Username: "alice", Password: "password-123", Email: "alice@example.com"})
	mustCreate(t, s, NewUser{Username: "bob", Password: "password-123", Email: "bob@example.com"})

	if _, err := s.UpdateProfile(bg(), a.ID, "小艾", ""); !isKind(err, KindInvalid) {
		t.Errorf("clearing email: err = %v, want invalid", err)
	}
	if _, err := s.UpdateProfile(bg(), a.ID, "小艾", "bob@example.com"); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("taking someone else's email: err = %v, want ErrEmailTaken", err)
	}
	got, err := s.UpdateProfile(bg(), a.ID, "小艾", "Alice2@Example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "alice2@example.com" {
		t.Errorf("email = %q, want alice2@example.com", got.Email)
	}
}

// --- sessions ---

func TestLoginAndAuthenticate(t *testing.T) {
	s, _ := newService(t)
	u := mustCreate(t, s, newUser("wang"))
	tok, p := mustLogin(t, s, "WANG")
	if p.UserID != u.ID || p.Via != ViaSession || p.CSRFToken == "" {
		t.Errorf("login principal = %+v", p)
	}
	got, err := s.Authenticate(bg(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != u.ID || got.Scope.WorkspaceID() == 0 || got.CSRFToken != p.CSRFToken {
		t.Errorf("authenticated principal = %+v", got)
	}
	if _, err := s.Authenticate(bg(), tok+"x"); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("tampered token err = %v", err)
	}
}

func TestLoginFailures(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newUser("wang"))
	if _, err := s.Login(bg(), "wang", "wrong-password", true, info()); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password err = %v", err)
	}
	if _, err := s.Login(bg(), "nobody", "password-123", true, info()); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user err = %v, want the same error as a wrong password", err)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	s, c := newService(t)
	mustCreate(t, s, newUser("wang"))
	for range 5 {
		s.Login(bg(), "wang", "wrong-password", true, info())
	}
	if _, err := s.Login(bg(), "wang", "password-123", true, info()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("after 5 failures err = %v, want ErrRateLimited", err)
	}
	c.Add(16 * time.Minute)
	mustLogin(t, s, "wang")
}

// TestLoginTracksRemainingAttemptsPerAccount covers the brief's rule that
// failures are counted per account (shared by username and email), locking
// after the 5th with a 900s retry-after, and that a locked account rejects
// even a correct password without checking it.
func TestLoginTracksRemainingAttemptsPerAccount(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, NewUser{Username: "wang", Password: "password-123", Email: "wang@example.com"})

	// 4 failures by username: remainingAttempts counts down 4, 3, 2, 1.
	for want := 4; want >= 1; want-- {
		_, err := s.Login(bg(), "wang", "wrong-password", true, info())
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("failure err = %v, want ErrInvalidCredentials", err)
		}
		got := detailsOf(t, err)["remainingAttempts"]
		if got != want {
			t.Errorf("remainingAttempts = %v, want %d", got, want)
		}
	}

	// The 5th failure (via the email, proving the counter is shared) locks
	// the account for 900s.
	_, err := s.Login(bg(), "WANG@EXAMPLE.COM", "wrong-password", true, info())
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindRateLimited {
		t.Fatalf("5th failure err = %v, want KindRateLimited", err)
	}
	if secs := detailsOf(t, err)["retryAfterSeconds"]; secs != 900 {
		t.Errorf("retryAfterSeconds = %v, want 900", secs)
	}

	// Locked out: even the correct password is rejected, without resetting
	// the lockout.
	if _, err := s.Login(bg(), "wang", "password-123", true, info()); !errors.Is(err, ErrRateLimited) {
		t.Errorf("login during lockout err = %v, want ErrRateLimited", err)
	}
}

// TestLoginRemainingAttemptsForUnknownAccount covers the "n:"+login keying
// used when the login does not resolve to any account.
func TestLoginRemainingAttemptsForUnknownAccount(t *testing.T) {
	s, _ := newService(t)
	for want := 4; want >= 1; want-- {
		_, err := s.Login(bg(), "nobody", "wrong-password", true, info())
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("failure err = %v, want ErrInvalidCredentials", err)
		}
		got := detailsOf(t, err)["remainingAttempts"]
		if got != want {
			t.Errorf("remainingAttempts = %v, want %d", got, want)
		}
	}
}

func TestSessionExpiresAndSlides(t *testing.T) {
	s, c := newService(t)
	mustCreate(t, s, newUser("wang"))
	tok, _ := mustLogin(t, s, "wang")

	if p, _ := s.Authenticate(bg(), tok); p.Refreshed {
		t.Error("a session used right after login should not be refreshed")
	}
	c.Add(29 * 24 * time.Hour)
	p, err := s.Authenticate(bg(), tok)
	if err != nil {
		t.Fatalf("session expired early: %v", err)
	}
	if !p.Refreshed {
		t.Error("expiry was extended but Refreshed is false, so the cookie would not be re-issued")
	}
	// Use on day 29 extended it by another 30 days.
	c.Add(29 * 24 * time.Hour)
	if _, err := s.Authenticate(bg(), tok); err != nil {
		t.Fatalf("sliding expiry did not extend the session: %v", err)
	}
	c.Add(31 * 24 * time.Hour)
	if _, err := s.Authenticate(bg(), tok); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("idle session still valid after 31 days: %v", err)
	}
	if n, _ := s.PurgeExpiredSessions(bg()); n != 1 {
		t.Errorf("purged %d sessions, want 1", n)
	}
}

func TestLogout(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newUser("wang"))
	tok, p := mustLogin(t, s, "wang")
	if err := s.Logout(bg(), p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(bg(), tok); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("session valid after logout: %v", err)
	}
}

func TestChangePasswordKeepsOnlyCurrentSession(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newUser("wang"))
	current, p := mustLogin(t, s, "wang")
	other, _ := mustLogin(t, s, "wang")

	if err := s.ChangePassword(bg(), p, "wrong-password", "new-password-1"); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("wrong current password err = %v", err)
	}
	if err := s.ChangePassword(bg(), p, "password-123", "new-password-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(bg(), current); err != nil {
		t.Errorf("current session revoked: %v", err)
	}
	if _, err := s.Authenticate(bg(), other); err == nil {
		t.Error("other session survived a password change")
	}
	if _, err := s.Login(bg(), "wang", "new-password-1", true, info()); err != nil {
		t.Errorf("new password rejected: %v", err)
	}
}

// A password change also ends API tokens (but the one making the change)
// and paired phones; an administrator reset ends all of them.
func TestPasswordChangesRevokeTokens(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newAdmin("admin"))
	u := mustCreate(t, s, newUser("wang"))
	script, _, _ := s.CreateToken(bg(), u.ID, "脚本")
	other, _, _ := s.CreateToken(bg(), u.ID, "另一个")
	p, err := s.AuthenticateToken(bg(), script)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePassword(bg(), p, "password-123", "new-password-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateToken(bg(), script); err != nil {
		t.Errorf("the token that changed the password was revoked: %v", err)
	}
	if _, err := s.AuthenticateToken(bg(), other); err == nil {
		t.Error("another API token survived a password change")
	}

	if err := s.SetPassword(bg(), u.ID, "reset-password-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateToken(bg(), script); err == nil {
		t.Error("an API token survived an administrator reset")
	}
}

// --- admin operations ---

func TestDisablingUserBlocksLoginAndSessions(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newAdmin("admin"))
	u := mustCreate(t, s, newUser("wang"))
	tok, _ := mustLogin(t, s, "wang")

	if err := s.SetDisabled(bg(), u.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(bg(), tok); err == nil {
		t.Error("disabled user's session still valid")
	}
	if _, err := s.Login(bg(), "wang", "password-123", true, info()); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("disabled user login err = %v", err)
	}
	if err := s.SetDisabled(bg(), u.ID, false); err != nil {
		t.Fatal(err)
	}
	mustLogin(t, s, "wang")
}

func TestLastAdminIsProtected(t *testing.T) {
	s, _ := newService(t)
	a := mustCreate(t, s, newAdmin("admin"))
	if err := s.SetDisabled(bg(), a.ID, true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable last admin err = %v", err)
	}
	if err := s.SetAdmin(bg(), a.ID, false); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demote last admin err = %v", err)
	}
	b := mustCreate(t, s, newAdmin("admin2"))
	if err := s.SetAdmin(bg(), a.ID, false); err != nil {
		t.Errorf("demote with another admin present: %v", err)
	}
	if err := s.SetDisabled(bg(), b.ID, true); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable the remaining admin err = %v", err)
	}
}

func TestAdminSetPasswordRevokesAllSessions(t *testing.T) {
	s, _ := newService(t)
	u := mustCreate(t, s, newUser("wang"))
	tok, _ := mustLogin(t, s, "wang")
	if err := s.SetPassword(bg(), u.ID, "reset-password-9"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(bg(), tok); err == nil {
		t.Error("session survived an admin password reset")
	}
	if _, err := s.Login(bg(), "wang", "reset-password-9", true, info()); err != nil {
		t.Errorf("reset password rejected: %v", err)
	}
	if err := s.SetPassword(bg(), 9999, "reset-password-9"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user err = %v", err)
	}
}

// --- API tokens ---

func TestAPITokenLifecycle(t *testing.T) {
	s, _ := newService(t)
	u := mustCreate(t, s, newUser("wang"))
	plain, tok, err := s.CreateToken(bg(), u.ID, "扫描仪脚本")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, "kept_") || !strings.HasPrefix(plain, tok.Prefix) {
		t.Errorf("token %q / prefix %q", plain, tok.Prefix)
	}
	p, err := s.AuthenticateToken(bg(), plain)
	if err != nil {
		t.Fatal(err)
	}
	if p.UserID != u.ID || p.Via != ViaToken || p.Scope.WorkspaceID() == 0 {
		t.Errorf("token principal = %+v", p)
	}
	list, _ := s.ListTokens(bg(), u.ID)
	if len(list) != 1 || list[0].LastUsedAt == nil {
		t.Errorf("list = %+v, want one token with last-used time", list)
	}
	if err := s.RevokeToken(bg(), u.ID, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AuthenticateToken(bg(), plain); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("revoked token err = %v", err)
	}
}

func TestUsersCannotTouchEachOthersTokens(t *testing.T) {
	s, _ := newService(t)
	a := mustCreate(t, s, newUser("alice"))
	b := mustCreate(t, s, newUser("bob"))
	_, tok, _ := s.CreateToken(bg(), a.ID, "a")
	if err := s.RevokeToken(bg(), b.ID, tok.ID); !errors.Is(err, ErrTokenNotFound) {
		t.Errorf("revoking another user's token err = %v, want not found", err)
	}
	if list, _ := s.ListTokens(bg(), b.ID); len(list) != 0 {
		t.Errorf("bob sees alice's tokens: %+v", list)
	}
}

func TestUsersGetSeparateWorkspaces(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newUser("alice"))
	mustCreate(t, s, newUser("bob"))
	_, pa := mustLogin(t, s, "alice")
	_, pb := mustLogin(t, s, "bob")
	if pa.Scope.WorkspaceID() == pb.Scope.WorkspaceID() {
		t.Error("two users share a workspace")
	}
}

func TestFindUserByUsername(t *testing.T) {
	s, _ := newService(t)
	u := mustCreate(t, s, newUser("wang"))
	got, err := s.FindUser(bg(), " WANG ")
	if err != nil || got.ID != u.ID {
		t.Errorf("FindUser = %+v, %v", got, err)
	}
	if _, err := s.FindUser(bg(), "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("missing user err = %v", err)
	}
}

// Behind a reverse proxy every user shares one IP, so the per-IP limit must
// be much looser than the per-username one.
func TestSharedIPIsNotLockedOutByAFewTypos(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newUser("wang"))
	for i := range 20 {
		s.Login(bg(), "typo"+string(rune('a'+i)), "wrong-password", true, info())
	}
	if _, err := s.Login(bg(), "wang", "password-123", true, info()); err != nil {
		t.Fatalf("20 failures from a shared IP locked out another user: %v", err)
	}
	for i := range 60 {
		s.Login(bg(), "spray"+string(rune('a'+i)), "wrong-password", true, info())
	}
	if _, err := s.Login(bg(), "wang", "password-123", true, info()); !errors.Is(err, ErrRateLimited) {
		t.Errorf("password spraying from one IP was not limited: %v", err)
	}
}

// --- two-step login (task 4) ---

func TestLoginWithTwoFactorReturnsChallengeInsteadOfASession(t *testing.T) {
	s, c := newService(t)
	enableTwoFactorFor(t, s, c, "alice")
	before := countSessions(t, s)

	res, err := s.Login(bg(), "alice", "password-123", true, info())
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if res.Challenge == "" || res.Token != "" {
		t.Fatalf("Login() = %+v, want a challenge and no token", res)
	}
	if want := c.Now().Add(5 * time.Minute); !res.ChallengeExpiresAt.Equal(want) {
		t.Errorf("ChallengeExpiresAt = %v, want %v", res.ChallengeExpiresAt, want)
	}
	if got := countSessions(t, s); got != before {
		t.Errorf("session count = %d after a two-factor login, want unchanged %d", got, before)
	}
}

func TestCompleteTwoFactorWithCodeAndRejectsReplay(t *testing.T) {
	s, c := newService(t)
	u, secret := enableTwoFactorFor(t, s, c, "bob")
	c.Add(30 * time.Second) // past EnableTwoFactor's own step, so this code is fresh
	code := validCode(t, secret, c)

	res, err := s.Login(bg(), "bob", "password-123", true, info())
	if err != nil || res.Challenge == "" {
		t.Fatalf("Login() = %+v, %v", res, err)
	}
	tok, p, err := s.CompleteTwoFactor(bg(), res.Challenge, code, "", info())
	if err != nil {
		t.Fatalf("CompleteTwoFactor: %v", err)
	}
	if p.UserID != u.ID || tok == "" {
		t.Fatalf("CompleteTwoFactor() = %q, %+v", tok, p)
	}
	if _, err := s.Authenticate(bg(), tok); err != nil {
		t.Errorf("session from CompleteTwoFactor is not valid: %v", err)
	}

	// The same TOTP code cannot be replayed against a fresh challenge.
	res2, err := s.Login(bg(), "bob", "password-123", true, info())
	if err != nil || res2.Challenge == "" {
		t.Fatalf("second Login() = %+v, %v", res2, err)
	}
	if _, _, err := s.CompleteTwoFactor(bg(), res2.Challenge, code, "", info()); codeOf(t, err) != "invalid_code" {
		t.Errorf("replayed code err = %v, want invalid_code", err)
	}
}

func TestCompleteTwoFactorWithRecoveryCodeIsOneTime(t *testing.T) {
	s, c := newService(t)
	u := mustCreate(t, s, newUser("carol"))
	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	codes, err := s.EnableTwoFactor(bg(), u.ID, validCode(t, setup.Secret, c))
	if err != nil {
		t.Fatalf("EnableTwoFactor: %v", err)
	}
	rc := codes[0]

	res, err := s.Login(bg(), "carol", "password-123", true, info())
	if err != nil || res.Challenge == "" {
		t.Fatalf("Login() = %+v, %v", res, err)
	}
	if _, _, err := s.CompleteTwoFactor(bg(), res.Challenge, "", rc, info()); err != nil {
		t.Fatalf("CompleteTwoFactor(recovery code): %v", err)
	}

	res2, err := s.Login(bg(), "carol", "password-123", true, info())
	if err != nil || res2.Challenge == "" {
		t.Fatalf("second Login() = %+v, %v", res2, err)
	}
	if _, _, err := s.CompleteTwoFactor(bg(), res2.Challenge, "", rc, info()); codeOf(t, err) != "invalid_code" {
		t.Errorf("reused recovery code err = %v, want invalid_code", err)
	}
}

func TestCompleteTwoFactorRequiresExactlyOneOfCodeOrRecoveryCode(t *testing.T) {
	s, c := newService(t)
	enableTwoFactorFor(t, s, c, "dana")
	res, err := s.Login(bg(), "dana", "password-123", true, info())
	if err != nil || res.Challenge == "" {
		t.Fatalf("Login() = %+v, %v", res, err)
	}
	if _, _, err := s.CompleteTwoFactor(bg(), res.Challenge, "", "", info()); !isKind(err, KindInvalid) {
		t.Errorf("neither code nor recoveryCode: err = %v, want invalid", err)
	}
	if _, _, err := s.CompleteTwoFactor(bg(), res.Challenge, "123456", "some-code", info()); !isKind(err, KindInvalid) {
		t.Errorf("both code and recoveryCode: err = %v, want invalid", err)
	}
}

func TestCompleteTwoFactorWrongCodeTracksRemainingAttempts(t *testing.T) {
	s, c := newService(t)
	_, secret := enableTwoFactorFor(t, s, c, "erin")
	res, err := s.Login(bg(), "erin", "password-123", true, info())
	if err != nil || res.Challenge == "" {
		t.Fatalf("Login() = %+v, %v", res, err)
	}

	for want := 4; want >= 1; want-- {
		_, _, err := s.CompleteTwoFactor(bg(), res.Challenge, "000000", "", info())
		if codeOf(t, err) != "invalid_code" {
			t.Fatalf("wrong code err = %v, want invalid_code", err)
		}
		if got := detailsOf(t, err)["remainingAttempts"]; got != want {
			t.Errorf("remainingAttempts = %v, want %d", got, want)
		}
	}

	// The 5th wrong attempt locks the account and consumes the challenge.
	if _, _, err := s.CompleteTwoFactor(bg(), res.Challenge, "000000", "", info()); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("5th wrong attempt err = %v, want ErrRateLimited", err)
	}

	// The challenge is gone, even to a now-correct code.
	c.Add(30 * time.Second)
	code := validCode(t, secret, c)
	if _, _, err := s.CompleteTwoFactor(bg(), res.Challenge, code, "", info()); !errors.Is(err, ErrChallengeExpired) {
		t.Errorf("challenge after lockout err = %v, want ErrChallengeExpired", err)
	}
}

func TestCompleteTwoFactorChallengeExpiresAfterFiveMinutes(t *testing.T) {
	s, c := newService(t)
	enableTwoFactorFor(t, s, c, "frank")
	res, err := s.Login(bg(), "frank", "password-123", true, info())
	if err != nil || res.Challenge == "" {
		t.Fatalf("Login() = %+v, %v", res, err)
	}
	c.Add(5*time.Minute + time.Second)
	if _, _, err := s.CompleteTwoFactor(bg(), res.Challenge, "000000", "", info()); !errors.Is(err, ErrChallengeExpired) {
		t.Errorf("expired challenge err = %v, want ErrChallengeExpired", err)
	}
}

func TestCompleteTwoFactorRememberFalseIsNotPersistent(t *testing.T) {
	s, c := newService(t)
	_, secret := enableTwoFactorFor(t, s, c, "gina")
	c.Add(30 * time.Second)
	code := validCode(t, secret, c)

	res, err := s.Login(bg(), "gina", "password-123", false, info())
	if err != nil || res.Challenge == "" {
		t.Fatalf("Login() = %+v, %v", res, err)
	}
	_, p, err := s.CompleteTwoFactor(bg(), res.Challenge, code, "", info())
	if err != nil {
		t.Fatalf("CompleteTwoFactor: %v", err)
	}
	if p.Persistent {
		t.Errorf("remember=false session is Persistent")
	}
}

func TestCompleteTwoFactorAccountDisabledBetweenSteps(t *testing.T) {
	s, c := newService(t)
	u, secret := enableTwoFactorFor(t, s, c, "harry")
	res, err := s.Login(bg(), "harry", "password-123", true, info())
	if err != nil || res.Challenge == "" {
		t.Fatalf("Login() = %+v, %v", res, err)
	}
	if err := s.SetDisabled(bg(), u.ID, true); err != nil {
		t.Fatal(err)
	}
	c.Add(30 * time.Second)
	code := validCode(t, secret, c)
	if _, _, err := s.CompleteTwoFactor(bg(), res.Challenge, code, "", info()); !errors.Is(err, ErrChallengeExpired) {
		t.Errorf("disabled account err = %v, want ErrChallengeExpired", err)
	}
}

func TestCompleteTwoFactorUnknownChallenge(t *testing.T) {
	s, _ := newService(t)
	if _, _, err := s.CompleteTwoFactor(bg(), "not-a-real-challenge", "123456", "", info()); !errors.Is(err, ErrChallengeExpired) {
		t.Errorf("unknown challenge err = %v, want ErrChallengeExpired", err)
	}
}

// TestCompleteTwoFactorAttemptsCapIsIsolatedFromAccountLockout exercises the
// "attempts达到5时删除挑战并返回ErrChallengeExpired" branch on its own. Under
// normal operation a challenge's own attempts counter and the account
// limiter increment in lockstep from zero, so the account-lockout branch
// (checked first) wins the moment both reach 5 and this branch is
// unreachable; here the challenge's counter is advanced directly (white-box)
// while the account limiter is left untouched, so the 5th wrong attempt made
// through CompleteTwoFactor only trips the challenge's own cap.
func TestCompleteTwoFactorAttemptsCapIsIsolatedFromAccountLockout(t *testing.T) {
	s, c := newService(t)
	enableTwoFactorFor(t, s, c, "iris")
	res, err := s.Login(bg(), "iris", "password-123", true, info())
	if err != nil || res.Challenge == "" {
		t.Fatalf("Login() = %+v, %v", res, err)
	}

	for range 4 {
		s.challenges.fail(res.Challenge)
	}

	if _, _, err := s.CompleteTwoFactor(bg(), res.Challenge, "000000", "", info()); !errors.Is(err, ErrChallengeExpired) {
		t.Fatalf("5th challenge attempt err = %v, want ErrChallengeExpired (not ErrRateLimited)", err)
	}
}

// TestCompleteTwoFactorRecoveryCodeConcurrentUseOnlyOneSucceeds guards the
// atomicity of checkSecondFactor: two requests racing to redeem the same
// recovery code (each holding its own challenge from an independent Login)
// must not both succeed.
func TestCompleteTwoFactorRecoveryCodeConcurrentUseOnlyOneSucceeds(t *testing.T) {
	s, c := newService(t)
	u := mustCreate(t, s, newUser("jill"))
	setup, err := s.BeginTwoFactor(bg(), u.ID, u.Username)
	if err != nil {
		t.Fatalf("BeginTwoFactor: %v", err)
	}
	codes, err := s.EnableTwoFactor(bg(), u.ID, validCode(t, setup.Secret, c))
	if err != nil {
		t.Fatalf("EnableTwoFactor: %v", err)
	}
	rc := codes[0]

	res1, err := s.Login(bg(), "jill", "password-123", true, info())
	if err != nil || res1.Challenge == "" {
		t.Fatalf("Login() = %+v, %v", res1, err)
	}
	res2, err := s.Login(bg(), "jill", "password-123", true, info())
	if err != nil || res2.Challenge == "" {
		t.Fatalf("second Login() = %+v, %v", res2, err)
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, results[0] = s.CompleteTwoFactor(bg(), res1.Challenge, "", rc, info())
	}()
	go func() {
		defer wg.Done()
		_, _, results[1] = s.CompleteTwoFactor(bg(), res2.Challenge, "", rc, info())
	}()
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("results = %v, want exactly one success", results)
	}
}

// --- lockout under concurrency ---

// TestLoginLockoutHoldsUnderConcurrency: 50 concurrent wrong-password logins
// against one account. The limiter's check-and-count must be atomic, so at
// most 5 of them reach the password check (each answered with 401 or, for
// the one that uses up the budget, 429); the rest are refused up front
// with 429.
func TestLoginLockoutHoldsUnderConcurrency(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newUser("wang"))
	var checks atomic.Int32
	s.verifyPassword = func(hash, password string) bool {
		checks.Add(1)
		return VerifyPassword(hash, password)
	}

	var wg sync.WaitGroup
	errs := make([]error, 50)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = s.Login(bg(), "wang", "wrong-password", true, info())
		}()
	}
	wg.Wait()

	unauthorized := 0
	for _, err := range errs {
		switch {
		case errors.Is(err, ErrInvalidCredentials):
			unauthorized++
		case isKind(err, KindRateLimited):
		default:
			t.Fatalf("unexpected err = %v", err)
		}
	}
	if unauthorized > 5 {
		t.Errorf("401 responses = %d, want at most 5 (lockout bypassed by concurrency)", unauthorized)
	}
	if got := checks.Load(); got > 5 {
		t.Errorf("password checks = %d, want at most 5 (lockout bypassed by concurrency)", got)
	}
	if got := s.userLimit.Remaining("u:1"); got != 0 {
		t.Errorf("Remaining = %d after 50 wrong attempts, want 0", got)
	}
}

// TestLoginCorrectPasswordAfterConcurrentLockoutIsRejected: 40 wrong
// passwords and 1 correct one race. A successful login clears the earlier
// failures (Reset), so the strict bound is per generation: at most 5
// attempts reach the password check before the success clears the counter
// (the success among them) and at most 5 after it, i.e. at most
// 5*(successes+1) checks in all; with no success, at most 5. In particular
// a correct password arriving after the lockout is refused.
func TestLoginCorrectPasswordAfterConcurrentLockoutIsRejected(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newUser("wang"))
	var checks atomic.Int32
	s.verifyPassword = func(hash, password string) bool {
		checks.Add(1)
		return VerifyPassword(hash, password)
	}

	var wg sync.WaitGroup
	errs := make([]error, 41)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pw := "wrong-password"
			if i == 20 {
				pw = "password-123"
			}
			_, errs[i] = s.Login(bg(), "wang", pw, true, info())
		}()
	}
	wg.Wait()

	successes := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrInvalidCredentials), isKind(err, KindRateLimited):
		default:
			t.Fatalf("unexpected err = %v", err)
		}
	}
	if got, limit := int(checks.Load()), 5*(successes+1); got > limit {
		t.Errorf("password checks = %d with %d success(es), want at most %d", got, successes, limit)
	}
}

// TestCompleteTwoFactorLockoutHoldsUnderConcurrency: the same bound for the
// second step. 10 challenges get 4 wrong codes each (so no challenge reaches
// its own 5-attempt cap) plus one correct code, all concurrently. As for
// passwords, the success clears earlier failures, so at most 5 requests
// reach the code check before it and at most 5 after it: at most
// 5*(successes+1) checks in all.
func TestCompleteTwoFactorLockoutHoldsUnderConcurrency(t *testing.T) {
	s, c := newService(t)
	_, secret := enableTwoFactorFor(t, s, c, "erin")
	c.Add(30 * time.Second)
	code := validCode(t, secret, c)

	challenges := make([]string, 11)
	for i := range challenges {
		res, err := s.Login(bg(), "erin", "password-123", true, info())
		if err != nil || res.Challenge == "" {
			t.Fatalf("Login() = %+v, %v", res, err)
		}
		challenges[i] = res.Challenge
	}
	var checks atomic.Int32
	s.beforeCodeCheck = func(string) { checks.Add(1) }

	var wg sync.WaitGroup
	errs := make([]error, 41)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i == 40 {
				_, _, errs[i] = s.CompleteTwoFactor(bg(), challenges[10], code, "", info())
				return
			}
			_, _, errs[i] = s.CompleteTwoFactor(bg(), challenges[i/4], "000000", "", info())
		}()
	}
	wg.Wait()

	successes := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case isKind(err, KindRateLimited), errors.Is(err, ErrChallengeExpired):
		default:
			if codeOf(t, err) != "invalid_code" {
				t.Fatalf("unexpected err = %v", err)
			}
		}
	}
	if got, limit := int(checks.Load()), 5*(successes+1); got > limit {
		t.Errorf("code checks = %d with %d success(es), want at most %d", got, successes, limit)
	}
}

// TestLoginFailureOverlappingASuccessIsStillCounted: a wrong password whose
// check is still running when another request logs in successfully (and
// clears the counter) must still be counted once it fails.
func TestLoginFailureOverlappingASuccessIsStillCounted(t *testing.T) {
	s, _ := newService(t)
	mustCreate(t, s, newUser("wang"))
	started, proceed := make(chan struct{}), make(chan struct{})
	s.verifyPassword = func(hash, password string) bool {
		if password == "wrong-password" {
			close(started)
			<-proceed
		}
		return VerifyPassword(hash, password)
	}

	errc := make(chan error, 1)
	go func() {
		_, err := s.Login(bg(), "wang", "wrong-password", true, info())
		errc <- err
	}()
	<-started
	mustLogin(t, s, "wang") // clears the counter while the wrong attempt is in flight
	close(proceed)
	err := <-errc

	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("overlapping wrong password err = %v, want ErrInvalidCredentials", err)
	}
	if got := detailsOf(t, err)["remainingAttempts"]; got != 4 {
		t.Errorf("remainingAttempts = %v, want 4 (the failure counted after the reset)", got)
	}
	if got := s.userLimit.Remaining("u:1"); got != 4 {
		t.Errorf("Remaining = %d, want 4", got)
	}
}

// TestCompleteTwoFactorFailureOverlappingASuccessIsStillCounted: the same
// for the second step.
func TestCompleteTwoFactorFailureOverlappingASuccessIsStillCounted(t *testing.T) {
	s, c := newService(t)
	_, secret := enableTwoFactorFor(t, s, c, "erin")
	c.Add(30 * time.Second)
	code := validCode(t, secret, c)
	var challenges [2]string
	for i := range challenges {
		res, err := s.Login(bg(), "erin", "password-123", true, info())
		if err != nil || res.Challenge == "" {
			t.Fatalf("Login() = %+v, %v", res, err)
		}
		challenges[i] = res.Challenge
	}
	started, proceed := make(chan struct{}), make(chan struct{})
	s.beforeCodeCheck = func(c string) {
		if c == "000000" {
			close(started)
			<-proceed
		}
	}

	errc := make(chan error, 1)
	go func() {
		_, _, err := s.CompleteTwoFactor(bg(), challenges[0], "000000", "", info())
		errc <- err
	}()
	<-started
	if _, _, err := s.CompleteTwoFactor(bg(), challenges[1], code, "", info()); err != nil {
		t.Fatalf("correct code: %v", err)
	}
	close(proceed)
	err := <-errc

	if codeOf(t, err) != "invalid_code" {
		t.Fatalf("overlapping wrong code err = %v, want invalid_code", err)
	}
	if got := detailsOf(t, err)["remainingAttempts"]; got != 4 {
		t.Errorf("remainingAttempts = %v, want 4 (the failure counted after the reset)", got)
	}
}

// TestSuccessfulLoginsDoNotUseUpTheIPBudget: reserving an attempt before the
// password check must not turn successful logins into IP failures (an
// office behind one NAT address logs in far more than 50 times).
func TestSuccessfulLoginsDoNotUseUpTheIPBudget(t *testing.T) {
	s, c := newService(t)
	mustCreate(t, s, newUser("wang"))
	enableTwoFactorFor(t, s, c, "erin")
	for range 30 {
		mustLogin(t, s, "wang")
		if res, err := s.Login(bg(), "erin", "password-123", true, info()); err != nil || res.Challenge == "" {
			t.Fatalf("Login(erin) = %+v, %v", res, err)
		}
	}
	if got := s.ipLimit.Remaining(info().IP); got != 50 {
		t.Errorf("IP Remaining = %d after 60 successful logins, want 50", got)
	}
	if got := s.userLimit.Remaining("u:2"); got != 5 {
		t.Errorf("two-factor account Remaining = %d after correct passwords, want 5", got)
	}
}

// TestSecondStepFailuresCountAgainstTheIP: a wrong second-step code is also
// counted for the client IP.
func TestSecondStepFailuresCountAgainstTheIP(t *testing.T) {
	s, c := newService(t)
	enableTwoFactorFor(t, s, c, "erin")
	res, err := s.Login(bg(), "erin", "password-123", true, info())
	if err != nil || res.Challenge == "" {
		t.Fatalf("Login() = %+v, %v", res, err)
	}
	s.CompleteTwoFactor(bg(), res.Challenge, "000000", "", info())
	if got := s.ipLimit.Remaining(info().IP); got != 49 {
		t.Errorf("IP Remaining = %d after one wrong code, want 49", got)
	}
}
