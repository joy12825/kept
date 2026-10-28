package edition

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"kept/server/internal/audit"
	"kept/server/internal/i18n"
	"kept/server/internal/store/sqlc"
)

// TrialDays is the paid edition's trial on a new instance.
const TrialDays = 30

// WarnDays before a trial or license ends, administrators are told.
const WarnDays = 7

const (
	keyInstance = "instance_id"
	keyTrial    = "trial_started_at" // unix seconds
	keyLicense  = "license"
)

type Options struct {
	Now func() time.Time // default time.Now
	// PublicKey checks licenses; default the issuer's (publicKey).
	PublicKey ed25519.PublicKey
	// Override, when set, is the edition regardless of license and trial:
	// development builds only (KEPT_EDITION), for demos and tests.
	Override *Edition
	// OpenSource is the open-source build (v1.13.1, OpenSourceBuild): it has
	// no paid modules, so it is always the free edition, without a trial or
	// a license.
	OpenSource bool
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	now func() time.Time
	pub ed25519.PublicKey
	fix *Edition
	oss bool

	mu       sync.RWMutex
	loaded   bool
	instance string
	trial    time.Time // when the trial started
	license  *License
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.PublicKey == nil {
		opt.PublicKey = publicKey
	}
	return &Service{db: db, q: sqlc.New(db), now: opt.Now, pub: opt.PublicKey, fix: opt.Override, oss: opt.OpenSource}
}

// Init gives a new instance its ID and starts its trial; afterwards it
// reads what is kept. Call once at start.
func (s *Service) Init(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.setting(ctx, keyInstance)
	if err != nil {
		return err
	}
	if id == "" {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		id = hex.EncodeToString(b)
		if err := s.q.UpsertSetting(ctx, sqlc.UpsertSettingParams{Key: keyInstance, Value: id}); err != nil {
			return err
		}
	}
	started, err := s.setting(ctx, keyTrial)
	if err != nil {
		return err
	}
	if started == "" && !s.oss { // the open-source build starts no trial
		started = strconv.FormatInt(s.now().Unix(), 10)
		if err := s.q.UpsertSetting(ctx, sqlc.UpsertSettingParams{Key: keyTrial, Value: started}); err != nil {
			return err
		}
	}
	secs, _ := strconv.ParseInt(started, 10, 64)
	s.instance, s.trial = id, time.Unix(secs, 0)
	if started == "" {
		s.trial = time.Time{}
	}
	s.license = nil
	if text, err := s.setting(ctx, keyLicense); err != nil {
		return err
	} else if text != "" {
		// a license that no longer reads (another key, another instance)
		// counts as none; the page says so
		if l, err := Read(text, s.pub); err == nil && l.Instance == id {
			s.license = &l
		}
	}
	s.loaded = true
	return nil
}

func (s *Service) setting(ctx context.Context, key string) (string, error) {
	v, err := s.q.GetSetting(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// Instance is this instance's ID, written into its licenses.
func (s *Service) Instance() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.instance
}

// Current is the edition in force now.
func (s *Service) Current() Edition {
	if s.fix != nil {
		return *s.fix
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, _, _ := s.state(s.now())
	return e
}

// Allows reports whether need is within the current edition.
func (s *Service) Allows(need Edition) bool { return s.Current().AtLeast(need) }

// Has reports whether the current edition includes f.
func (s *Service) Has(f Feature) bool { return s.Allows(EditionOf(f)) }

// Require is nil when need is within the current edition, else the error to return.
func (s *Service) Require(need Edition) error {
	if s.Allows(need) {
		return nil
	}
	return Required(need)
}

// state is the edition in force at now, the limits, and whether that is
// the trial; s.mu held.
func (s *Service) state(now time.Time) (Edition, Limits, bool) {
	if s.oss {
		return Free, DefaultLimits(Free), false
	}
	if l := s.license; l != nil {
		if end := l.ExpiresAt(now.Location()); end.IsZero() || now.Before(end) {
			return l.Tier(), l.Limits(), false
		}
	}
	if s.loaded && now.Before(s.trialEnd()) {
		return Pro, DefaultLimits(Pro), true
	}
	return Free, DefaultLimits(Free), false
}

func (s *Service) trialEnd() time.Time { return s.trial.AddDate(0, 0, TrialDays) }

// Usage is how much of a limit is used; Limit 0 is no limit.
type Usage struct {
	Used  int
	Limit int
}

// Full reports whether one more would go over the limit.
func (u Usage) Full() bool { return u.Limit > 0 && u.Used >= u.Limit }

// Status is what the 版本与授权 page shows.
type Status struct {
	Edition  Edition
	Instance string
	Trial    bool
	// TrialEndsAt: the trial's end, also after it ended (zero with a license
	// or an override).
	TrialEndsAt time.Time
	// License is the one installed, nil without one; Expired when its last
	// day has passed.
	License          *License
	LicenseExpiresAt time.Time
	Expired          bool
	// DaysLeft of the trial or license in force; -1 when it does not end.
	DaysLeft         int
	Warn             bool // ends within WarnDays
	Members          Usage
	SharedWorkspaces Usage
	Features         []Feature
	Overridden       bool // development builds: KEPT_EDITION
	// OpenSource: the open-source build, always the free edition (v1.13.1)
	OpenSource bool
}

// Status counts members and shared workspaces now.
func (s *Service) Status(ctx context.Context) (Status, error) {
	s.mu.RLock()
	now := s.now()
	e, lim, trial := s.state(now)
	st := Status{Edition: e, Instance: s.instance, Trial: trial, DaysLeft: -1, OpenSource: s.oss}
	if s.license != nil && !s.oss {
		l := *s.license
		st.License = &l
		st.LicenseExpiresAt = l.ExpiresAt(now.Location())
		st.Expired = !st.LicenseExpiresAt.IsZero() && !now.Before(st.LicenseExpiresAt)
	}
	if (st.License == nil || st.Expired) && !s.oss {
		st.TrialEndsAt = s.trialEnd()
	}
	s.mu.RUnlock()
	if s.fix != nil {
		st.Edition, lim, st.Trial, st.Overridden, st.TrialEndsAt = *s.fix, DefaultLimits(*s.fix), false, true, time.Time{}
	}
	switch {
	case st.Overridden:
	case st.Trial:
		st.DaysLeft = daysUntil(now, st.TrialEndsAt)
	case st.License != nil && !st.Expired && !st.LicenseExpiresAt.IsZero():
		st.DaysLeft = daysUntil(now, st.LicenseExpiresAt)
	}
	st.Warn = st.DaysLeft >= 0 && st.DaysLeft <= WarnDays
	st.Features = Features(st.Edition)
	members, shared, err := s.counts(ctx)
	if err != nil {
		return st, err
	}
	st.Members = Usage{Used: members, Limit: lim.Members}
	st.SharedWorkspaces = Usage{Used: shared, Limit: lim.SharedWorkspaces}
	return st, nil
}

// daysUntil counts whole days left, rounding up (a few hours left is 1).
func daysUntil(now, end time.Time) int {
	if !now.Before(end) {
		return 0
	}
	return int((end.Sub(now) + 24*time.Hour - 1) / (24 * time.Hour))
}

// counts are the enabled accounts and the shared workspaces not deleted.
func (s *Service) counts(ctx context.Context) (members, shared int, err error) {
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE disabled_at IS NULL`).Scan(&members); err != nil {
		return
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM workspaces WHERE kind <> 'personal' AND deleted_at IS NULL`).Scan(&shared)
	return
}

// CanAddMember is nil when one more enabled account fits.
func (s *Service) CanAddMember(ctx context.Context) error {
	st, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if st.Members.Full() {
		return ErrMembers.With(map[string]any{"limit": st.Members.Limit, "edition": st.Edition.String()})
	}
	return nil
}

// CanAddSharedWorkspace is nil when one more shared workspace fits.
func (s *Service) CanAddSharedWorkspace(ctx context.Context) error {
	st, err := s.Status(ctx)
	if err != nil {
		return err
	}
	if st.SharedWorkspaces.Full() {
		return ErrSharedWorkspaces.With(map[string]any{"limit": st.SharedWorkspaces.Limit, "edition": st.Edition.String()})
	}
	return nil
}

// Install checks text and keeps it as this instance's license.
func (s *Service) Install(ctx context.Context, text string) (License, error) {
	if s.oss {
		return License{}, ErrOpenSource
	}
	l, err := Read(text, s.pub)
	if err != nil {
		return License{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if l.Instance != s.instance {
		return License{}, errInstance.With(map[string]any{"instance": s.instance})
	}
	if err := s.q.UpsertSetting(ctx, sqlc.UpsertSettingParams{Key: keyLicense, Value: strings.Join(strings.Fields(text), "")}); err != nil {
		return License{}, err
	}
	s.license = &l
	err = audit.Record(ctx, s.q, audit.Event{
		Action: "settings.license_install", TargetType: "setting", TargetID: keyLicense,
		Summary: fmt.Sprintf(i18n.N("%s 导入了%s许可证（%s）"), audit.ActorFrom(ctx).Name, l.Tier().Name(), l.Holder), At: s.now(),
	})
	return l, err
}

// Remove drops the license: the instance goes back to the free edition
// (or the trial, if it has not ended).
func (s *Service) Remove(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.q.DeleteSetting(ctx, keyLicense); err != nil {
		return err
	}
	s.license = nil
	return audit.Record(ctx, s.q, audit.Event{
		Action: "settings.license_remove", TargetType: "setting", TargetID: keyLicense,
		Summary: fmt.Sprintf(i18n.N("%s 移除了许可证"), audit.ActorFrom(ctx).Name), At: s.now(),
	})
}
