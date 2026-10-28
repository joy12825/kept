package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"kept/server/internal/store"
)

func TestBackupFolderChecks(t *testing.T) {
	data := t.TempDir()
	disk := t.TempDir()
	file := filepath.Join(disk, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		dir  string
		want error
	}{
		{"usb/kept-backup", ErrDirNotAbsolute},
		{filepath.Join(disk, "nothing-here"), ErrDirMissing},
		{file, ErrDirNotDir},
		{data, ErrDirInData},
		{filepath.Dir(data), ErrDirInData}, // contains the data directory
		{disk, nil},
	} {
		if err := CheckDir(c.dir, data); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.dir, err, c.want)
		}
	}
	inside := filepath.Join(data, "backups")
	if err := os.Mkdir(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := CheckDir(inside, data); !errors.Is(err, ErrDirInData) {
		t.Errorf("inside the data directory: %v", err)
	}
	if os.Geteuid() != 0 { // root writes anywhere
		ro := t.TempDir()
		if err := os.Chmod(ro, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		if err := CheckDir(ro, data); !errors.Is(err, ErrDirNotWritable) {
			t.Errorf("read-only: %v", err)
		}
	}
	// the write check leaves nothing behind
	if left, _ := os.ReadDir(disk); len(left) != 1 {
		t.Errorf("left in the folder: %v", left)
	}
}

func TestBackupFolderOnThePageAndInTheDeployment(t *testing.T) {
	ctx := context.Background()
	data := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(data, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	disk := t.TempDir()

	page := NewDir(st.DB, "", data)
	if page.Get(ctx) != "" || page.FromEnv() {
		t.Fatal("a new instance has no backup folder")
	}
	if err := page.Set(ctx, "relative"); !errors.Is(err, ErrDirNotAbsolute) || page.Get(ctx) != "" {
		t.Fatalf("a bad folder is not kept: %v", err)
	}
	if err := page.Set(ctx, " "+disk+"/ "); err != nil || page.Get(ctx) != disk {
		t.Fatalf("chosen: %v %q", err, page.Get(ctx))
	}
	if err := page.Set(ctx, ""); err != nil || page.Get(ctx) != "" {
		t.Fatalf("turned off: %v %q", err, page.Get(ctx))
	}

	// KEPT_BACKUP_DIR decides, and the page cannot change it
	_ = page.Set(ctx, disk)
	env := NewDir(st.DB, "/srv/kept-backup", data)
	if env.Get(ctx) != "/srv/kept-backup" || !env.FromEnv() {
		t.Errorf("deployment: %q", env.Get(ctx))
	}
	if err := env.Set(ctx, ""); !errors.Is(err, ErrDirFromEnv) {
		t.Errorf("the page changes the deployment's folder: %v", err)
	}
	var nothing *Dir
	if nothing.Get(ctx) != "" || nothing.FromEnv() {
		t.Error("no source is no folder")
	}
}
