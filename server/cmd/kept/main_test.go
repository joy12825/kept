package main

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/auth"
	"kept/server/internal/store"
)

func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String() + errOut.String()
}

func TestUserCreateAndSetPassword(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEPT_DATA_DIR", dir)
	t.Setenv("KEPT_PASSWORD", "password-123")

	if code, out := runCLI(t, "user", "create", "--admin", "--email", "boss@example.com", "Boss"); code != 0 || !strings.Contains(out, "boss") {
		t.Fatalf("create: exit %d, output %q", code, out)
	}
	if code, out := runCLI(t, "user", "create", "--email", "boss2@example.com", "boss"); code != 1 || !strings.Contains(out, "已经被使用") {
		t.Errorf("duplicate create: exit %d, output %q", code, out)
	}

	t.Setenv("KEPT_PASSWORD", "brand-new-pass")
	if code, out := runCLI(t, "user", "set-password", "boss"); code != 0 {
		t.Fatalf("set-password: exit %d, output %q", code, out)
	}
	if code, _ := runCLI(t, "user", "set-password", "nobody"); code != 1 {
		t.Errorf("set-password for a missing user: exit %d, want 1", code)
	}

	st, err := store.Open(context.Background(), filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := auth.NewService(st.DB, auth.Options{})
	res, err := svc.Login(context.Background(), "boss", "brand-new-pass", true, auth.ClientInfo{})
	if err != nil {
		t.Fatalf("login with the new password: %v", err)
	}
	if !res.Principal.IsAdmin {
		t.Error("--admin did not make an administrator")
	}
}

func TestUserCommandUsage(t *testing.T) {
	t.Setenv("KEPT_DATA_DIR", t.TempDir())
	for _, args := range [][]string{
		{"user"}, {"user", "frobnicate"}, {"user", "create"}, {"user", "create", "boss"},
		{"user", "disable-2fa"}, {"user", "disable-2fa", "a", "b"},
	} {
		if code, _ := runCLI(t, args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestUserDisableTwoFactor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEPT_DATA_DIR", dir)
	t.Setenv("KEPT_PASSWORD", "password-123")
	if code, out := runCLI(t, "user", "create", "--email", "lost@example.com", "lost"); code != 0 {
		t.Fatalf("create: exit %d, output %q", code, out)
	}

	st, err := store.Open(context.Background(), filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(st.DB, auth.Options{})
	u, err := svc.FindUser(context.Background(), "lost")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginTwoFactor(context.Background(), u.ID, u.Username); err != nil {
		t.Fatal(err)
	}
	st.Close()

	if code, out := runCLI(t, "user", "disable-2fa", "lost"); code != 0 || !strings.Contains(out, "lost") {
		t.Fatalf("disable-2fa: exit %d, output %q", code, out)
	}
	if code, _ := runCLI(t, "user", "disable-2fa", "nobody"); code != 1 {
		t.Errorf("disable-2fa for a missing user: exit %d, want 1", code)
	}

	st2, err := store.Open(context.Background(), filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	svc2 := auth.NewService(st2.DB, auth.Options{})
	status, err := svc2.TwoFactorStatus(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if status.Enabled {
		t.Error("two-factor authentication is still reported as enabled")
	}
}

func TestPasswordFromStdinWhenNotATerminal(t *testing.T) {
	got, err := readPassword(strings.NewReader("from-stdin-pw\n"), io.Discard)
	if err != nil || got != "from-stdin-pw" {
		t.Errorf("readPassword = %q, %v", got, err)
	}
}

func TestDemoLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEPT_DATA_DIR", dir)
	t.Setenv("KEPT_PASSWORD", "password-123")
	if code, out := runCLI(t, "demo", "load", "wang"); code != 1 {
		t.Errorf("demo load for a missing account: exit %d, output %q", code, out)
	}
	if code, out := runCLI(t, "user", "create", "--email", "wang@example.com", "wang"); code != 0 {
		t.Fatalf("create: exit %d, %q", code, out)
	}
	code, out := runCLI(t, "demo", "load", "wang")
	if code != 0 || !strings.Contains(out, "新建空间“王家”：新增往来方 17 个、类型 7 个、标签 6 个、文档") {
		t.Errorf("demo load: exit %d, output %q", code, out)
	}
	if code, out := runCLI(t, "demo", "load", "wang"); code != 0 || !strings.Contains(out, "已有空间“王家”：新增往来方 0 个") {
		t.Errorf("second demo load: exit %d, output %q", code, out)
	}
	if code, _ := runCLI(t, "demo"); code != 2 {
		t.Errorf("demo without arguments: exit %d, want 2", code)
	}
}

func TestDoctorReportsMissingToolsAndAWritableDataDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KEPT_DATA_DIR", filepath.Join(dir, "data"))
	t.Setenv("PATH", t.TempDir()) // no tools at all
	code, out := runCLI(t, "doctor")
	if code != 1 {
		t.Errorf("exit %d, want 1 when tools are missing", code)
	}
	for _, want := range []string{"✗ ocrmypdf", "✗ pdftotext", "没有 tesseract", "✓ 可以写入", "有缺少的项目"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
