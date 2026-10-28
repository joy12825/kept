// Package auth manages accounts, personal workspaces, login sessions, API
// tokens.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/audit"
	"kept/server/internal/i18n"
	"kept/server/internal/privacy"
	"kept/server/internal/store/sqlc"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

// shortSessionTTL is the idle lifetime of a non-persistent ("remember me"
// off) session; it uses a browser-session cookie so it normally ends much
// sooner than this when the browser closes.
const shortSessionTTL = 12 * time.Hour

type Options struct {
	SessionTTL time.Duration    // idle lifetime of a login session; default 30 days
	Now        func() time.Time // default time.Now
	// Name is the instance's name (branding), shown in authenticator apps;
	// nil means 收好.
	Name func(context.Context) string
}

type Service struct {
	db         *sql.DB
	q          *sqlc.Queries
	now        func() time.Time
	sessionTTL time.Duration
	name       func(context.Context) string
	userLimit  *limiter // failed logins per username
	ipLimit    *limiter // failed logins per client IP (looser: may be a shared proxy)
	pairLimit  *limiter // wrong pairing codes per client IP
	challenges *challengeStore

	// verifyPassword is the password check used by Login; a field only so
	// tests can count how many checks actually run.
	verifyPassword func(hash, password string) bool
	// beforeCodeCheck, when set (tests only), runs in CompleteTwoFactor after
	// the attempt is reserved and before the code is checked.
	beforeCodeCheck func(code string)
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.SessionTTL <= 0 {
		opt.SessionTTL = 30 * 24 * time.Hour
	}
	return &Service{
		db:         db,
		q:          sqlc.New(db),
		now:        opt.Now,
		sessionTTL: opt.SessionTTL,
		name:       opt.Name,
		userLimit:  newLimiter(5, 15*time.Minute, opt.Now),
		ipLimit:    newLimiter(50, 15*time.Minute, opt.Now),
		pairLimit:  newLimiter(10, 10*time.Minute, opt.Now),
		challenges: newChallengeStore(opt.Now),

		verifyPassword: VerifyPassword,
	}
}

// ---------- types ----------

type User struct {
	ID               int64
	Username         string
	DisplayName      string
	Email            string
	IsAdmin          bool
	Disabled         bool
	CreatedAt        time.Time
	TwoFactorEnabled bool
	SSO              bool   // bound to a person at the single sign-on provider (v1.0)
	Locale           string // the web's language: "" follows the browser, "zh", "en" (v1.10.0)
}

type Workspace struct {
	ID    int64
	Name  string
	Kind  string
	Color string
	Role  tenant.Role
}

type Token struct {
	ID         int64
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

type NewUser struct {
	Username    string
	Password    string
	DisplayName string
	Email       string
	IsAdmin     bool
}

type ClientInfo struct {
	IP        string
	UserAgent string
}

type Via int

const (
	ViaSession Via = iota + 1
	ViaToken
	ViaDevice // a paired phone's token: one workspace; DeviceAccess says what it may do
)

// Principal is the authenticated caller of a request.
type Principal struct {
	UserID       int64
	Username     string
	DisplayName  string
	IsAdmin      bool
	Scope        tenant.Scope
	Via          Via
	DeviceAccess string // ViaDevice only: AccessUpload or AccessFull
	SessionID    string // hashed session id (ViaSession only)
	CSRFToken    string // ViaSession only
	Refreshed    bool   // the session's expiry was just extended; re-issue the cookie
	Persistent   bool   // ViaSession only: cookie carries Max-Age and survives browser restarts
	TokenID      int64  // ViaToken and ViaDevice: the api_tokens row
	// VerifiedAt is when the session last confirmed the password or a
	// two-factor code (signing in counts), or the paired phone did (v1.7.20);
	// zero for API tokens (v1.6.4).
	VerifiedAt time.Time
}

// LoginResult is what Login returns. When Challenge is non-empty, the
// password was correct but the account has two-factor authentication
// enabled: no session was created, and the caller must complete the second
// step with CompleteTwoFactor before ChallengeExpiresAt. Otherwise Token and
// Principal are the newly created session, exactly as CompleteTwoFactor
// returns them.
type LoginResult struct {
	Token              string
	Principal          Principal
	Challenge          string
	ChallengeExpiresAt time.Time
}

// ---------- setup, users ----------

func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	n, err := s.q.CountUsers(ctx)
	return n == 0, err
}

// Setup creates the first administrator. It fails with ErrSetupDone once any
// account exists; the check and insert share one write transaction.
func (s *Service) Setup(ctx context.Context, u NewUser) (User, error) {
	u.IsAdmin = true
	return s.createUser(ctx, u, "user.setup", func(q *sqlc.Queries) error {
		n, err := q.CountUsers(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrSetupDone
		}
		return nil
	})
}

// CreateUser is used by administrators and the command line.
func (s *Service) CreateUser(ctx context.Context, u NewUser) (User, error) {
	return s.createUser(ctx, u, "user.create", nil)
}

var usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{2,31}$`)

func normalizeUsername(name string) (string, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if !usernameRe.MatchString(n) {
		return "", newErr(KindInvalid, "用户名需为 3–32 个字符，只能包含英文字母、数字、下划线、点和短横线，并以字母或数字开头")
	}
	return n, nil
}

var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// normalizeEmail trims whitespace, lower-cases and validates an email
// address. It is stored lower-cased so lookups are case-insensitive.
func normalizeEmail(email string) (string, error) {
	e := strings.ToLower(strings.TrimSpace(email))
	if len(e) > 254 || !emailRe.MatchString(e) {
		return "", newErr(KindInvalid, "请输入有效的邮箱地址")
	}
	return e, nil
}

var usernameDisallowedRe = regexp.MustCompile(`[^a-z0-9_.-]`)

// usernameFromEmail derives a username candidate from the local part of an
// email address and picks the first one that is not already taken.
func usernameFromEmail(ctx context.Context, q *sqlc.Queries, email string) (string, error) {
	local := email
	if i := strings.IndexByte(email, '@'); i >= 0 {
		local = email[:i]
	}
	local = usernameDisallowedRe.ReplaceAllString(local, "")
	for len(local) > 0 {
		c := local[0]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			break
		}
		local = local[1:]
	}
	if len(local) > 28 {
		local = local[:28]
	}
	base := local
	if len(base) < 3 {
		base = "user"
	}
	for i := 1; i <= 99; i++ {
		candidate := base
		if i > 1 {
			candidate = fmt.Sprintf("%s%d", base, i)
		}
		if _, err := q.GetUserByUsername(ctx, candidate); errors.Is(err, sql.ErrNoRows) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	suffix, err := randomDigits(6)
	if err != nil {
		return "", err
	}
	return base + suffix, nil
}

func randomDigits(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i, v := range b {
		out[i] = '0' + v%10
	}
	return string(out), nil
}

// createUser inserts the account and its personal workspace and records
// action ("user.setup" or "user.create") in the audit log, all in one
// transaction.
func (s *Service) createUser(ctx context.Context, u NewUser, action string, precheck func(*sqlc.Queries) error) (User, error) {
	email, err := normalizeEmail(u.Email)
	if err != nil {
		return User{}, err
	}
	wantUsername := strings.TrimSpace(u.Username) != ""
	var username string
	if wantUsername {
		username, err = normalizeUsername(u.Username)
		if err != nil {
			return User{}, err
		}
	}
	if err := ValidatePassword(u.Password); err != nil {
		return User{}, err
	}
	display := strings.TrimSpace(u.DisplayName)
	hash, err := HashPassword(u.Password)
	if err != nil {
		return User{}, err
	}

	var created sqlc.User
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		if precheck != nil {
			if err := precheck(q); err != nil {
				return err
			}
		}
		if _, err := q.GetUserByEmail(ctx, nullString(email)); err == nil {
			return ErrEmailTaken
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if wantUsername {
			if _, err := q.GetUserByUsername(ctx, username); err == nil {
				return ErrUsernameTaken
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		} else {
			username, err = usernameFromEmail(ctx, q, email)
			if err != nil {
				return err
			}
		}
		displayName := display
		if displayName == "" {
			displayName = username
		}
		now := s.now().UnixMilli()
		created, err = q.InsertUser(ctx, sqlc.InsertUserParams{
			Username:          username,
			DisplayName:       displayName,
			Email:             nullString(email),
			PasswordHash:      hash,
			IsAdmin:           boolInt(u.IsAdmin),
			PasswordChangedAt: now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
		if err != nil {
			return err
		}
		wsID, err := q.InsertWorkspace(ctx, sqlc.InsertWorkspaceParams{
			// named in the language of the request creating it (v1.10.1)
			Name: i18n.T(ctx, fmt.Sprintf(i18n.N("%s 的空间"), displayName)), Kind: "personal", Color: "blue",
			CreatedBy: sql.NullInt64{Int64: created.ID, Valid: true}, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		if err := q.InsertWorkspaceMember(ctx, sqlc.InsertWorkspaceMemberParams{
			WorkspaceID: wsID, UserID: created.ID, Role: string(tenant.RoleOwner), CreatedAt: now,
		}); err != nil {
			return err
		}
		if err := taxonomy.SeedWorkspace(ctx, q, wsID, s.now()); err != nil {
			return err
		}
		ev := audit.Event{Action: action, TargetType: "user", TargetID: strconv.FormatInt(created.ID, 10), At: s.now()}
		if action == "user.setup" {
			// Nobody is logged in yet: the new administrator is the actor.
			ev.Actor = &audit.Actor{UserID: created.ID, Name: displayName, Via: audit.ViaSession, RequestID: audit.ActorFrom(ctx).RequestID}
			ev.Summary = fmt.Sprintf(i18n.N("%s 完成了首次初始化，创建了管理员账号 %s"), displayName, username)
		} else {
			ev.Summary = fmt.Sprintf(i18n.N("%s 创建了账号 %s"), audit.ActorFrom(ctx).Name, username)
			if u.IsAdmin {
				ev.Summary = fmt.Sprintf(i18n.N("%s 创建了账号 %s（管理员）"), audit.ActorFrom(ctx).Name, username)
			}
		}
		return audit.Record(ctx, q, ev)
	})
	if err != nil {
		return User{}, err
	}
	return toNewUser(created), nil
}

func (s *Service) GetUser(ctx context.Context, id int64) (User, error) {
	u, err := s.q.GetUserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return toUser(u), nil
}

// FindUser looks an account up by username or email (case-insensitive);
// login is treated as an email when it contains "@".
func (s *Service) FindUser(ctx context.Context, login string) (User, error) {
	u, err := s.lookupByLogin(ctx, login)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return toUser(u), nil
}

// lookupByLogin resolves a username-or-email login (case-insensitive).
func (s *Service) lookupByLogin(ctx context.Context, login string) (sqlc.GetUserByIDRow, error) {
	name := strings.ToLower(strings.TrimSpace(login))
	if strings.Contains(name, "@") {
		r, err := s.q.GetUserByEmail(ctx, nullString(name))
		return sqlc.GetUserByIDRow(r), err
	}
	r, err := s.q.GetUserByUsername(ctx, name)
	return sqlc.GetUserByIDRow(r), err
}

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]User, len(rows))
	for i, r := range rows {
		users[i] = toUser(sqlc.GetUserByIDRow(r))
	}
	return users, nil
}

func (s *Service) PersonalWorkspace(ctx context.Context, userID int64) (Workspace, error) {
	w, err := s.q.GetPersonalWorkspace(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, ErrUserNotFound
	}
	if err != nil {
		return Workspace{}, err
	}
	return Workspace{ID: w.ID, Name: w.Name, Kind: w.Kind, Color: w.Color, Role: tenant.Role(w.Role)}, nil
}

// SetLocale sets the language of the web for an account (v1.10.0): ""
// follows the browser.
func (s *Service) SetLocale(ctx context.Context, userID int64, locale string) error {
	switch locale {
	case "", "zh", "en":
	default:
		return newErr(KindInvalid, "语言只能是 zh、en 或空（跟随浏览器）")
	}
	return s.q.SetUserLocale(ctx, sqlc.SetUserLocaleParams{Locale: locale, Now: time.Now().UnixMilli(), ID: userID})
}

// SeenLang records the language a person last used the web or the app in
// (v1.10.2): emails to someone whose account follows the browser use it.
func (s *Service) SeenLang(ctx context.Context, userID int64, lang string) error {
	return s.q.SetUserSeenLang(ctx, sqlc.SetUserSeenLangParams{Lang: lang, ID: userID})
}

func (s *Service) UpdateProfile(ctx context.Context, userID int64, displayName, email string) (User, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" || len([]rune(displayName)) > 64 {
		return User{}, newErr(KindInvalid, "显示名称需为 1–64 个字符")
	}
	email, err := normalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	err = s.inTx(ctx, func(q *sqlc.Queries) error {
		if existing, err := q.GetUserByEmail(ctx, nullString(email)); err == nil {
			if existing.ID != userID {
				return ErrEmailTaken
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return q.UpdateUserProfile(ctx, sqlc.UpdateUserProfileParams{
			DisplayName: displayName, Email: nullString(email), Now: s.now().UnixMilli(), ID: userID,
		})
	})
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, userID)
}

// SetDisabled enables or disables an account. Disabling ends its sessions;
// the last active administrator cannot be disabled.
func (s *Service) SetDisabled(ctx context.Context, userID int64, disabled bool) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		u, err := q.GetUserByID(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		if err != nil {
			return err
		}
		if disabled == u.DisabledAt.Valid {
			return nil // already in the requested state
		}
		now := s.now().UnixMilli()
		if disabled {
			if u.IsAdmin == 1 {
				if err := ensureAnotherAdmin(ctx, q); err != nil {
					return err
				}
			}
			if err := q.DeleteUserSessions(ctx, userID); err != nil {
				return err
			}
			if err := q.SetUserDisabled(ctx, sqlc.SetUserDisabledParams{
				DisabledAt: sql.NullInt64{Int64: now, Valid: true}, Now: now, ID: userID,
			}); err != nil {
				return err
			}
			return s.recordUser(ctx, q, "user.disable", userID, fmt.Sprintf(i18n.N("%s 停用了账号 %s"), audit.ActorFrom(ctx).Name, u.Username), nil)
		}
		if err := q.SetUserDisabled(ctx, sqlc.SetUserDisabledParams{Now: now, ID: userID}); err != nil {
			return err
		}
		return s.recordUser(ctx, q, "user.enable", userID, fmt.Sprintf(i18n.N("%s 启用了账号 %s"), audit.ActorFrom(ctx).Name, u.Username), nil)
	})
}

// SetAdmin grants or revokes administrator rights, keeping at least one.
func (s *Service) SetAdmin(ctx context.Context, userID int64, isAdmin bool) error {
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		u, err := q.GetUserByID(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		if err != nil {
			return err
		}
		if isAdmin == (u.IsAdmin == 1) {
			return nil // already in the requested state
		}
		if !isAdmin && !u.DisabledAt.Valid {
			if err := ensureAnotherAdmin(ctx, q); err != nil {
				return err
			}
		}
		if err := q.SetUserAdmin(ctx, sqlc.SetUserAdminParams{IsAdmin: boolInt(isAdmin), Now: s.now().UnixMilli(), ID: userID}); err != nil {
			return err
		}
		summary := fmt.Sprintf(i18n.N("%s 将 %s 设为管理员"), audit.ActorFrom(ctx).Name, u.Username)
		if !isAdmin {
			summary = fmt.Sprintf(i18n.N("%s 取消了 %s 的管理员权限"), audit.ActorFrom(ctx).Name, u.Username)
		}
		return s.recordUser(ctx, q, "user.admin_change", userID, summary, map[string][2]any{"isAdmin": {!isAdmin, isAdmin}})
	})
}

// recordUser writes an instance-level audit event about an account; the
// summary is a whole sentence, made with a format marked i18n.N so it can
// be translated where the activity is listed.
func (s *Service) recordUser(ctx context.Context, q *sqlc.Queries, action string, userID int64, summary string, changes map[string][2]any) error {
	return audit.Record(ctx, q, audit.Event{
		Action: action, TargetType: "user", TargetID: strconv.FormatInt(userID, 10),
		Summary: summary, Changes: changes, At: s.now(),
	})
}

func ensureAnotherAdmin(ctx context.Context, q *sqlc.Queries) error {
	n, err := q.CountActiveAdmins(ctx)
	if err != nil {
		return err
	}
	if n <= 1 {
		return ErrLastAdmin
	}
	return nil
}

// SetPassword is an administrator reset: all of the user's sessions end,
// and their API tokens and paired phones stop working.
func (s *Service) SetPassword(ctx context.Context, userID int64, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		u, err := q.GetUserByID(ctx, userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		} else if err != nil {
			return err
		}
		if err := q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
			PasswordHash: hash, Now: s.now().UnixMilli(), ID: userID,
		}); err != nil {
			return err
		}
		if err := q.DeleteUserSessions(ctx, userID); err != nil {
			return err
		}
		// family members' keys wrapped by the old password no longer open
		// (v1.8.1); their recovery codes still do
		if err := privacy.PasswordReset(ctx, q, userID); err != nil {
			return err
		}
		revoked, err := q.DeleteUserTokensExcept(ctx, sqlc.DeleteUserTokensExceptParams{UserID: userID})
		if err != nil {
			return err
		}
		what := fmt.Sprintf(i18n.N("%s 重置了 %s 的密码"), audit.ActorFrom(ctx).Name, u.Username)
		if revoked > 0 {
			what = fmt.Sprintf(i18n.N("%s 重置了 %s 的密码，%d 个 API Token 与手机随之失效"), audit.ActorFrom(ctx).Name, u.Username, revoked)
		}
		return s.recordUser(ctx, q, "user.password_reset", userID, what, nil)
	})
}

// ChangePassword is the user changing their own password: every other
// login ends — sessions, API tokens and paired phones — and the one making
// the change stays.
func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next string) error {
	u, err := s.q.GetUserByID(ctx, p.UserID)
	if err != nil {
		return err
	}
	if !VerifyPassword(u.PasswordHash, current) {
		return ErrWrongPassword
	}
	if err := ValidatePassword(next); err != nil {
		return err
	}
	hash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(q *sqlc.Queries) error {
		if err := q.UpdateUserPassword(ctx, sqlc.UpdateUserPasswordParams{
			PasswordHash: hash, Now: s.now().UnixMilli(), ID: p.UserID,
		}); err != nil {
			return err
		}
		// family members' keys wrapped by the password follow it (v1.8.1)
		if err := privacy.PasswordChanged(ctx, q, p.UserID, current, next, s.now()); err != nil {
			return err
		}
		keepToken := int64(0)
		if p.Via == ViaToken {
			keepToken = p.TokenID
		}
		if _, err := q.DeleteUserTokensExcept(ctx, sqlc.DeleteUserTokensExceptParams{UserID: p.UserID, KeepID: keepToken}); err != nil {
			return err
		}
		if p.Via == ViaSession {
			return q.DeleteUserSessionsExcept(ctx, sqlc.DeleteUserSessionsExceptParams{UserID: p.UserID, KeepID: p.SessionID})
		}
		return q.DeleteUserSessions(ctx, p.UserID)
	})
}

// instanceName is the name authenticator apps show for this instance.
func (s *Service) instanceName(ctx context.Context) string {
	if s.name != nil {
		if n := s.name(ctx); n != "" {
			return n
		}
	}
	return "收好"
}

// ---------- sessions ----------

// Login verifies the password and, for an account without two-factor
// authentication, starts a session (remember decides whether it survives
// browser restarts; see Options and shortSessionTTL). For an account with
// two-factor authentication enabled it instead issues a challenge the caller
// must complete with CompleteTwoFactor; see LoginResult.
//
// Failed logins are counted per account: the key is "u:"+accountID when the
// login resolves to an account (so a username and its email share one
// counter), or "n:"+normalizedLogin when it does not. The account is looked
// up before the limiter is consulted so the right key is used; the lookup
// itself is cheap. A shared IP is also counted, with a much looser limit.
//
// Every attempt reserves a failure (limiter.Reserve: check and count under
// one lock) before the password is checked, so concurrent requests cannot
// get more than the limit's worth of password checks. A correct password
// takes its reservation back: Reset for the account on a completed login,
// Release otherwise (the IP counter, and the account counter while the
// second step is pending, keep their earlier failures). A wrong password
// Settles its reservation, so it stays counted even if a concurrent success
// Reset the counter while the password was being checked.
func (s *Service) Login(ctx context.Context, login, password string, remember bool, ci ClientInfo) (LoginResult, error) {
	name := strings.ToLower(strings.TrimSpace(login))
	u, err := s.lookupByLogin(ctx, name)
	found := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return LoginResult{}, err
	}
	key := "n:" + name
	if found {
		key = "u:" + strconv.FormatInt(u.ID, 10)
	}
	gen, ok := s.userLimit.Reserve(key)
	if !ok {
		return LoginResult{}, s.rateLimitedErr(s.userLimit.RetryAfter(key))
	}
	var ipGen uint64
	if ci.IP != "" {
		if ipGen, ok = s.ipLimit.Reserve(ci.IP); !ok {
			s.userLimit.Release(key, gen) // refused before any check: not an attempt
			return LoginResult{}, s.rateLimitedErr(s.ipLimit.RetryAfter(ci.IP))
		}
	}
	if !found {
		s.verifyPassword(dummyHash, password) // equalize timing for unknown usernames
	}
	if !found || !s.verifyPassword(u.PasswordHash, password) || u.DisabledAt.Valid {
		// The failure is already counted by the reservations above (or
		// counted again, if a concurrent success cleared them meanwhile).
		s.userLimit.Settle(key, gen)
		if ci.IP != "" {
			s.ipLimit.Settle(ci.IP, ipGen)
		}
		if s.userLimit.Remaining(key) == 0 {
			return LoginResult{}, s.rateLimitedErr(s.userLimit.RetryAfter(key))
		}
		return LoginResult{}, ErrInvalidCredentials.With(map[string]any{"remainingAttempts": s.userLimit.Remaining(key)})
	}

	// The password is correct: this attempt was not a failure.
	if ci.IP != "" {
		s.ipLimit.Release(ci.IP, ipGen)
	}

	// When two-factor authentication is enabled, no session is created and
	// the account's earlier failures are kept (they still guard the second
	// step): only this attempt's reservation is taken back and a challenge
	// is issued.
	if u.TwoFactorEnabled {
		s.userLimit.Release(key, gen)
		token, expiresAt := s.challenges.insert(u.ID, remember)
		return LoginResult{Challenge: token, ChallengeExpiresAt: expiresAt}, nil
	}

	s.userLimit.Reset(key)
	token, p, err := s.createSession(ctx, u.ID, remember, ci)
	return LoginResult{Token: token, Principal: p}, err
}

// CompleteTwoFactor finishes a two-step login started by Login: it verifies
// the challenge and, exactly once, either a TOTP code or a recovery code,
// then creates the session Login deferred.
func (s *Service) CompleteTwoFactor(ctx context.Context, challenge, code, recoveryCode string, ci ClientInfo) (string, Principal, error) {
	if (code == "") == (recoveryCode == "") {
		return "", Principal{}, newErr(KindInvalid, "请输入验证码或恢复码")
	}
	entry, ok := s.challenges.get(challenge)
	if !ok {
		return "", Principal{}, ErrChallengeExpired
	}

	// An account disabled between the two steps can no longer complete the
	// login, regardless of what the code check below would say.
	u, err := s.q.GetUserByID(ctx, entry.userID)
	if errors.Is(err, sql.ErrNoRows) {
		s.challenges.delete(challenge)
		return "", Principal{}, ErrChallengeExpired
	}
	if err != nil {
		return "", Principal{}, err
	}
	if u.DisabledAt.Valid {
		s.challenges.delete(challenge)
		return "", Principal{}, ErrChallengeExpired
	}

	// Reserve an attempt for the account and the client IP before the code
	// is checked, atomically (see Login), so concurrent guesses cannot get
	// past the account's limit; a wrong code keeps the reservations.
	key := "u:" + strconv.FormatInt(entry.userID, 10)
	gen, ok := s.userLimit.Reserve(key)
	if !ok {
		s.challenges.delete(challenge)
		return "", Principal{}, s.rateLimitedErr(s.userLimit.RetryAfter(key))
	}
	var ipGen uint64
	if ci.IP != "" {
		if ipGen, ok = s.ipLimit.Reserve(ci.IP); !ok {
			s.userLimit.Release(key, gen) // refused before any check: not an attempt
			return "", Principal{}, s.rateLimitedErr(s.ipLimit.RetryAfter(ci.IP))
		}
	}
	if s.beforeCodeCheck != nil {
		s.beforeCodeCheck(code)
	}

	// checkSecondFactor runs inside a write transaction (BEGIN IMMEDIATE; see
	// store.Open) so the "is this code/recovery code still unused" read and
	// the "mark it used" write are atomic: two concurrent requests racing on
	// the same code or recovery code serialize here, and only the first one
	// commits before the second's read observes it as already used.
	now := s.now()
	var valid bool
	if err := s.inTx(ctx, func(q *sqlc.Queries) error {
		var cerr error
		valid, cerr = s.checkSecondFactor(ctx, q, entry.userID, code, recoveryCode, now)
		return cerr
	}); err != nil {
		return "", Principal{}, err
	}
	if !valid {
		s.userLimit.Settle(key, gen)
		if ci.IP != "" {
			s.ipLimit.Settle(ci.IP, ipGen)
		}
		attempts := s.challenges.fail(challenge)
		if s.userLimit.Remaining(key) == 0 {
			s.challenges.delete(challenge)
			return "", Principal{}, s.rateLimitedErr(s.userLimit.RetryAfter(key))
		}
		if attempts >= 5 {
			s.challenges.delete(challenge)
			return "", Principal{}, ErrChallengeExpired
		}
		return "", Principal{}, (&Error{Kind: KindUnauthorized, Code: "invalid_code", Message: "验证码不正确或已过期"}).
			With(map[string]any{"remainingAttempts": s.userLimit.Remaining(key)})
	}

	s.challenges.delete(challenge)
	s.userLimit.Reset(key)
	if ci.IP != "" {
		s.ipLimit.Release(ci.IP, ipGen)
	}
	return s.createSession(ctx, entry.userID, entry.remember, ci)
}

// createSession starts a new login session for userID; the returned token is
// the cookie value (only its hash is stored). remember decides whether the
// session survives browser restarts (see Options and shortSessionTTL).
func (s *Service) createSession(ctx context.Context, userID int64, remember bool, ci ClientInfo) (string, Principal, error) {
	token := newSecret()
	id := hashToken(token)
	csrf := newSecret()
	now := s.now()
	ttl := s.sessionTTL
	if !remember {
		ttl = shortSessionTTL
	}
	if err := s.q.InsertSession(ctx, sqlc.InsertSessionParams{
		ID: id, UserID: userID, CsrfToken: csrf,
		Ip: ci.IP, UserAgent: truncate(ci.UserAgent, 256),
		CreatedAt: now.UnixMilli(), LastSeenAt: now.UnixMilli(), ExpiresAt: now.Add(ttl).UnixMilli(),
		Persistent: boolInt(remember), VerifiedAt: now.UnixMilli(),
	}); err != nil {
		return "", Principal{}, err
	}
	p, err := s.Authenticate(ctx, token)
	return token, p, err
}

// rateLimitedErr reports how long the caller must wait, rounded up to a
// whole second.
func (s *Service) rateLimitedErr(retryAfter time.Duration) *Error {
	secs := int((retryAfter + time.Second - 1) / time.Second)
	if secs < 0 {
		secs = 0
	}
	return ErrRateLimited.With(map[string]any{"retryAfterSeconds": secs})
}

// Authenticate resolves a session cookie value. Sessions slide: any use
// more than an hour after the last one extends the expiry by SessionTTL.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}
	now := s.now()
	r, err := s.q.GetSessionPrincipal(ctx, sqlc.GetSessionPrincipalParams{ID: hashToken(token), Now: now.UnixMilli()})
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	persistent := r.Persistent == 1
	ttl := s.sessionTTL
	if !persistent {
		ttl = shortSessionTTL
	}
	refreshed := now.UnixMilli()-r.LastSeenAt > time.Hour.Milliseconds()
	if refreshed {
		if err := s.q.TouchSession(ctx, sqlc.TouchSessionParams{
			Now: now.UnixMilli(), ExpiresAt: now.Add(ttl).UnixMilli(), ID: r.SessionID,
		}); err != nil {
			return Principal{}, err
		}
	}
	return Principal{
		UserID: r.UserID, Username: r.Username, DisplayName: r.DisplayName, IsAdmin: r.IsAdmin == 1,
		Scope: tenant.ForUser(r.WorkspaceID, tenant.Role(r.Role), r.UserID),
		Via:   ViaSession, SessionID: r.SessionID, CSRFToken: r.CsrfToken, Refreshed: refreshed,
		Persistent: persistent, VerifiedAt: time.UnixMilli(r.VerifiedAt),
	}, nil
}

// SessionTTL is the idle lifetime of a session (used for cookie Max-Age).
func (s *Service) SessionTTL() time.Duration { return s.sessionTTL }

func (s *Service) Logout(ctx context.Context, p Principal) error {
	if p.Via != ViaSession {
		return nil
	}
	return s.q.DeleteSession(ctx, p.SessionID)
}

func (s *Service) PurgeExpiredSessions(ctx context.Context) (int64, error) {
	return s.q.DeleteExpiredSessions(ctx, s.now().UnixMilli())
}

// ---------- API tokens ----------

var tokenEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// CreateToken returns the plaintext token (shown to the user once) and its record.
func (s *Service) CreateToken(ctx context.Context, userID int64, name string) (string, Token, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return "", Token{}, newErr(KindInvalid, "Token 名称需为 1–64 个字符")
	}
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", Token{}, err
	}
	plain := "kept_" + strings.ToLower(tokenEncoding.EncodeToString(b))
	row, err := s.q.InsertAPIToken(ctx, sqlc.InsertAPITokenParams{
		UserID: userID, Name: name, TokenHash: hashToken(plain), Prefix: plain[:12], CreatedAt: s.now().UnixMilli(),
	})
	if err != nil {
		return "", Token{}, err
	}
	return plain, toToken(row), nil
}

func (s *Service) ListTokens(ctx context.Context, userID int64) ([]Token, error) {
	rows, err := s.q.ListAPITokens(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Token, len(rows))
	for i, r := range rows {
		out[i] = toToken(r)
	}
	return out, nil
}

// RevokeToken deletes one of the user's tokens; another user's token is
// reported as not found.
func (s *Service) RevokeToken(ctx context.Context, userID, tokenID int64) error {
	n, err := s.q.DeleteAPIToken(ctx, sqlc.DeleteAPITokenParams{ID: tokenID, UserID: userID})
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrTokenNotFound
	}
	return nil
}

func (s *Service) AuthenticateToken(ctx context.Context, token string) (Principal, error) {
	if !strings.HasPrefix(token, "kept_") {
		return Principal{}, ErrUnauthenticated
	}
	r, err := s.q.GetTokenPrincipal(ctx, hashToken(token))
	if errors.Is(err, sql.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	now := s.now().UnixMilli()
	if !r.LastUsedAt.Valid || now-r.LastUsedAt.Int64 > time.Minute.Milliseconds() {
		if err := s.q.TouchAPIToken(ctx, sqlc.TouchAPITokenParams{Now: sql.NullInt64{Int64: now, Valid: true}, ID: r.TokenID}); err != nil {
			return Principal{}, err
		}
	}
	via, access := ViaToken, ""
	var verified time.Time
	if r.TokenKind == "device" {
		via, access = ViaDevice, r.TokenAccess
		if r.TokenVerifiedAt > 0 {
			verified = time.UnixMilli(r.TokenVerifiedAt) // v1.7.20
		}
	}
	return Principal{
		UserID: r.UserID, Username: r.Username, DisplayName: r.DisplayName, IsAdmin: r.IsAdmin == 1,
		Scope: tenant.ForUser(r.WorkspaceID, tenant.Role(r.Role), r.UserID),
		Via:   via, DeviceAccess: access, TokenID: r.TokenID, VerifiedAt: verified,
	}, nil
}

// ---------- helpers ----------

func (s *Service) inTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE (see store.Open)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func newSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func toUser(u sqlc.GetUserByIDRow) User {
	return User{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email.String,
		IsAdmin: u.IsAdmin == 1, Disabled: u.DisabledAt.Valid, CreatedAt: time.UnixMilli(u.CreatedAt),
		TwoFactorEnabled: u.TwoFactorEnabled, SSO: u.SsoSubject.Valid, Locale: u.Locale,
	}
}

// toNewUser converts the row InsertUser returns. A brand new account can
// never already have two-factor authentication enabled.
func toNewUser(u sqlc.User) User {
	return User{
		ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email.String,
		IsAdmin: u.IsAdmin == 1, Disabled: u.DisabledAt.Valid, CreatedAt: time.UnixMilli(u.CreatedAt),
	}
}

func toToken(t sqlc.ApiToken) Token {
	tok := Token{ID: t.ID, Name: t.Name, Prefix: t.Prefix, CreatedAt: time.UnixMilli(t.CreatedAt)}
	if t.LastUsedAt.Valid {
		at := time.UnixMilli(t.LastUsedAt.Int64)
		tok.LastUsedAt = &at
	}
	return tok
}

func nullString(s string) sql.NullString {
	s = strings.TrimSpace(s)
	return sql.NullString{String: s, Valid: s != ""}
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
