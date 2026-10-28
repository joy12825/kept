package edition

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/auth"
	"kept/server/internal/store"
)

var ctx = context.Background()

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newService(t *testing.T) (*Service, *store.Store, *clock, ed25519.PrivateKey) {
	t.Helper()
	return newServiceWith(t, Options{})
}

func newServiceWith(t *testing.T, opt Options) (*Service, *store.Store, *clock, ed25519.PrivateKey) {
	t.Helper()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	pub, key, _ := GenerateKey()
	c := &clock{t: time.Date(2026, 10, 1, 12, 0, 0, 0, time.Local)}
	opt.Now, opt.PublicKey = c.now, pub
	s := NewService(st.DB, opt)
	if err := s.Init(ctx); err != nil {
		t.Fatal(err)
	}
	return s, st, c, key
}

func issue(t *testing.T, key ed25519.PrivateKey, l License) string {
	t.Helper()
	text, err := Sign(l, key)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestANewInstanceHasATrialThenTheFreeEdition(t *testing.T) {
	s, st, c, _ := newService(t)
	if s.Current() != Pro {
		t.Fatalf("new instance: %v, want the trial (pro)", s.Current())
	}
	status, _ := s.Status(ctx)
	if !status.Trial || status.DaysLeft != TrialDays || status.Warn || status.Instance == "" {
		t.Errorf("status = %+v", status)
	}
	c.t = c.t.AddDate(0, 0, TrialDays-3)
	if status, _ := s.Status(ctx); !status.Warn || status.DaysLeft != 3 {
		t.Errorf("3 days left: %+v", status)
	}
	c.t = c.t.AddDate(0, 0, 3)
	if s.Current() != Free || s.Allows(Pro) {
		t.Errorf("after the trial: %v", s.Current())
	}
	// starting again keeps the same instance and trial
	id := s.Instance()
	again := NewService(st.DB, Options{Now: c.now, PublicKey: s.pub})
	if err := again.Init(ctx); err != nil || again.Instance() != id || again.Current() != Free {
		t.Errorf("restart: %v %s %v", err, again.Instance(), again.Current())
	}
}

func TestALicenseSetsTheEditionUntilItsLastDay(t *testing.T) {
	s, _, c, key := newService(t)
	text := issue(t, key, License{ID: "L-1", Instance: s.Instance(), Edition: "team", Holder: "某公司", Issued: "2026-10-01", Expires: "2026-12-31"})
	// line breaks from copying do not matter
	if _, err := s.Install(ctx, text[:20]+"\n"+text[20:]+"\n"); err != nil {
		t.Fatal(err)
	}
	if s.Current() != Team {
		t.Fatalf("with a team license: %v", s.Current())
	}
	c.t = time.Date(2026, 12, 31, 23, 0, 0, 0, time.Local)
	if s.Current() != Team {
		t.Error("the last day still counts")
	}
	c.t = time.Date(2027, 1, 1, 0, 0, 1, 0, time.Local)
	status, _ := s.Status(ctx)
	if s.Current() != Free || !status.Expired || status.License == nil {
		t.Errorf("after it expired: %v %+v", s.Current(), status)
	}
	if err := s.Remove(ctx); err != nil || s.Current() != Free {
		t.Errorf("removed: %v %v", err, s.Current())
	}
}

func TestLicensesThatDoNotHold(t *testing.T) {
	s, _, _, key := newService(t)
	good := License{ID: "L-2", Instance: s.Instance(), Edition: "pro", Holder: "王家", Issued: "2026-10-01"}
	_, otherKey, _ := GenerateKey()
	cases := map[string]string{
		"another issuer":   issue(t, otherKey, good),
		"another instance": issue(t, key, License{ID: "L-3", Instance: "0000", Edition: "pro", Holder: "王家"}),
		"not a license":    "hello",
	}
	// changed after signing: a longer member count in the same envelope
	parts := strings.Split(issue(t, key, good), ".")
	cases["changed"] = parts[0] + "." + b64.EncodeToString([]byte(`{"id":"L-2","instance":"`+s.Instance()+`","edition":"team","holder":"王家"}`)) + "." + parts[2]
	for name, text := range cases {
		if _, err := s.Install(ctx, text); err == nil {
			t.Errorf("%s: installed", name)
		} else if var_ := (*apperr.Error)(nil); !errors.As(err, &var_) {
			t.Errorf("%s: %v is not for the user", name, err)
		}
	}
	if _, err := Sign(License{Edition: "free"}, key); err == nil {
		t.Error("signed a free license")
	}
	// a license read with another key at start counts as none
	if _, err := s.Install(ctx, issue(t, key, good)); err != nil {
		t.Fatal(err)
	}
	pub2, _, _ := GenerateKey()
	other := NewService(s.db, Options{Now: s.now, PublicKey: pub2})
	if err := other.Init(ctx); err != nil || other.Current() != Pro { // the trial, not the license
		t.Errorf("%v %v", err, other.Current())
	}
	if st, _ := other.Status(ctx); st.License != nil {
		t.Error("an unreadable license shows as installed")
	}
}

func TestLimitsKeepWhoIsThereAndStopOneMore(t *testing.T) {
	s, st, c, key := newService(t)
	users := auth.NewService(st.DB, auth.Options{})
	for i := 0; i < 6; i++ {
		if _, err := users.CreateUser(ctx, auth.NewUser{Username: fmt.Sprintf("user%d", i), Email: fmt.Sprintf("u%d@example.com", i), Password: "correct horse battery"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CanAddMember(ctx); err != nil {
		t.Errorf("the trial (pro) takes 15: %v", err)
	}
	c.t = c.t.AddDate(0, 1, 0) // the trial is over: free, six people
	err := s.CanAddMember(ctx)
	if !errors.Is(err, ErrMembers) {
		t.Fatalf("a seventh in free: %v", err)
	}
	status, _ := s.Status(ctx)
	if status.Members != (Usage{Used: 6, Limit: 6}) || status.SharedWorkspaces.Limit != 1 {
		t.Errorf("usage = %+v %+v", status.Members, status.SharedWorkspaces)
	}
	// a license with more people
	if _, err := s.Install(ctx, issue(t, key, License{ID: "L-4", Instance: s.Instance(), Edition: "pro", Holder: "大家庭", Members: 20})); err != nil {
		t.Fatal(err)
	}
	if status, _ := s.Status(ctx); status.Members.Limit != 20 || status.SharedWorkspaces.Limit != 0 {
		t.Errorf("pro license: %+v", status)
	}
}

func TestRequiredSaysWhichEdition(t *testing.T) {
	var e *apperr.Error
	if !errors.As(Required(Team), &e) || e.Code != "edition_required" || e.Details["edition"] != "team" || e.Kind != apperr.KindForbidden {
		t.Errorf("%+v", e)
	}
	if got := Features(Free); len(got) != 0 {
		t.Errorf("free features %v", got)
	}
	if got := Features(Pro); len(got) != 8 || got[0] != FeatureAI {
		t.Errorf("pro features %v", got)
	}
	if got := Features(Team); len(got) != 12 {
		t.Errorf("team features %v", got)
	}
	fixed := Free
	s := NewService(nil, Options{Override: &fixed})
	if s.Current() != Free || s.Require(Pro) == nil {
		t.Error("override ignored")
	}
}

// The open-source build (v1.13.1) is always the free edition: no trial, and a
// license neither installs nor counts if one was kept by an official build.
func TestTheOpenSourceBuildIsAlwaysFree(t *testing.T) {
	official, st, c, key := newService(t)
	text := issue(t, key, License{ID: "L-1", Instance: official.Instance(), Edition: "team", Holder: "王家", Issued: "2026-10-01"})
	if _, err := official.Install(ctx, text); err != nil {
		t.Fatal(err)
	}
	s := NewService(st.DB, Options{Now: c.now, PublicKey: official.pub, OpenSource: true})
	if err := s.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if s.Current() != Free || s.Allows(Pro) {
		t.Errorf("open source: %v", s.Current())
	}
	status, _ := s.Status(ctx)
	if !status.OpenSource || status.Trial || status.License != nil || !status.TrialEndsAt.IsZero() || status.DaysLeft != -1 || len(status.Features) != 0 || status.Members.Limit != 6 {
		t.Errorf("status = %+v", status)
	}
	if _, err := s.Install(ctx, text); !errors.Is(err, ErrOpenSource) {
		t.Errorf("install: %v", err)
	}

	// a new open-source instance starts no trial; an official build later does
	_, st2, c2, _ := newServiceWith(t, Options{OpenSource: true})
	var trial int
	_ = st2.DB.QueryRow(`SELECT count(*) FROM settings WHERE key = ?`, keyTrial).Scan(&trial)
	if trial != 0 {
		t.Error("the open-source build started a trial")
	}
	later := NewService(st2.DB, Options{Now: c2.now})
	if err := later.Init(ctx); err != nil || later.Current() != Pro {
		t.Errorf("official build afterwards: %v %v", later.Current(), err)
	}
}

func TestRequiredSaysWhichBuild(t *testing.T) {
	var e *apperr.Error
	if !errors.As(Required(Pro), &e) {
		t.Fatal(e)
	}
	if open := strings.Contains(e.Message, "开源"); open != OpenSourceBuild {
		t.Errorf("open-source build %v, message %q", OpenSourceBuild, e.Message)
	}
}
