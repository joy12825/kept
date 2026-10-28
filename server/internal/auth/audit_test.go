package auth

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"kept/server/internal/audit"
)

type auditRow struct {
	action, via, target, summary, changes string
	actor                                 int64
	instanceLevel                         bool
}

func auditRows(t *testing.T, s *Service) []auditRow {
	t.Helper()
	rows, err := s.db.QueryContext(bg(), `SELECT action, actor_via, target_type || '/' || target_id, summary, changes,
		coalesce(actor_user_id, 0), workspace_id IS NULL FROM audit_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.action, &r.via, &r.target, &r.summary, &r.changes, &r.actor, &r.instanceLevel); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func as(u User) context.Context {
	return audit.WithActor(bg(), audit.Actor{UserID: u.ID, Name: u.DisplayName, Via: audit.ViaSession, RequestID: "req-1"})
}

func TestAccountManagementIsAudited(t *testing.T) {
	s, c := newService(t)
	admin, err := s.Setup(bg(), NewUser{Password: "password-123", DisplayName: "管理员", Email: "admin@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := as(admin)

	wang, err := s.CreateUser(ctx, NewUser{Username: "wang", Password: "password-123", Email: "wang@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		name string
		run  func() error
	}{
		{"disable", func() error { return s.SetDisabled(ctx, wang.ID, true) }},
		{"enable", func() error { return s.SetDisabled(ctx, wang.ID, false) }},
		{"grant admin", func() error { return s.SetAdmin(ctx, wang.ID, true) }},
		{"revoke admin", func() error { return s.SetAdmin(ctx, wang.ID, false) }},
		{"reset password", func() error { return s.SetPassword(ctx, wang.ID, "another-secret-9") }},
	}
	for _, st := range steps {
		if err := st.run(); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
	}
	setup, err := s.BeginTwoFactor(bg(), wang.ID, wang.Username)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnableTwoFactor(bg(), wang.ID, validCode(t, setup.Secret, c)); err != nil {
		t.Fatal(err)
	}
	if err := s.AdminDisableTwoFactor(ctx, wang.ID); err != nil {
		t.Fatal(err)
	}

	rows := auditRows(t, s)
	want := []string{"user.setup", "user.create", "user.disable", "user.enable", "user.admin_change", "user.admin_change",
		"user.password_reset", "user.2fa_disable"}
	if len(rows) != len(want) {
		t.Fatalf("recorded %d events, want %d: %+v", len(rows), len(want), rows)
	}
	for i, r := range rows {
		if r.action != want[i] {
			t.Errorf("event %d action = %s, want %s", i, r.action, want[i])
		}
		if !r.instanceLevel {
			t.Errorf("%s should be an instance-level event", r.action)
		}
		if r.actor != admin.ID || r.via != "session" {
			t.Errorf("%s actor = %d via %s, want the administrator's session", r.action, r.actor, r.via)
		}
		for _, secret := range []string{"password-123", "another-secret-9"} {
			if strings.Contains(r.summary, secret) || strings.Contains(r.changes, secret) {
				t.Errorf("%s leaks a password: %s %s", r.action, r.summary, r.changes)
			}
		}
	}
	if rows[0].target != "user/"+strconv.FormatInt(admin.ID, 10) || !strings.Contains(rows[0].summary, "管理员") {
		t.Errorf("setup event = %+v", rows[0])
	}
	if rows[1].target != "user/"+strconv.FormatInt(wang.ID, 10) || rows[1].summary != "管理员 创建了账号 wang" {
		t.Errorf("create event = %+v", rows[1])
	}
	if rows[4].changes != `{"isAdmin":[false,true]}` || rows[5].changes != `{"isAdmin":[true,false]}` {
		t.Errorf("admin changes = %s, %s", rows[4].changes, rows[5].changes)
	}
}

func TestNothingIsAuditedWhenNothingChanges(t *testing.T) {
	s, _ := newService(t)
	admin, err := s.Setup(bg(), NewUser{Password: "password-123", DisplayName: "管理员", Email: "admin@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := as(admin)
	before := len(auditRows(t, s))

	// Refused: the last administrator cannot be disabled or demoted.
	if err := s.SetDisabled(ctx, admin.ID, true); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("err = %v", err)
	}
	if err := s.SetAdmin(ctx, admin.ID, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("err = %v", err)
	}
	// Refused before anything is written.
	if _, err := s.CreateUser(ctx, NewUser{Username: "dup", Password: "password-123", Email: "admin@example.com"}); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("err = %v", err)
	}
	if err := s.SetPassword(ctx, 9999, "another-secret-9"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("err = %v", err)
	}
	// No-ops: already in the requested state.
	if err := s.SetDisabled(ctx, admin.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAdmin(ctx, admin.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.AdminDisableTwoFactor(ctx, admin.ID); err != nil {
		t.Fatal(err)
	}
	if rows := auditRows(t, s); len(rows) != before {
		t.Errorf("recorded %d new events, want none: %+v", len(rows)-before, rows[before:])
	}
}

// The command line has no caller: its actions are attributed to the system.
func TestCommandLineActionsAreAttributedToTheSystem(t *testing.T) {
	s, _ := newService(t)
	if _, err := s.CreateUser(bg(), newUser("wang")); err != nil {
		t.Fatal(err)
	}
	rows := auditRows(t, s)
	if len(rows) != 1 || rows[0].action != "user.create" || rows[0].via != "system" || rows[0].actor != 0 {
		t.Errorf("rows = %+v", rows)
	}
}
