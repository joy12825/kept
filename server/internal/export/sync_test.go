package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// v1.6.5: an updated export writes what changed, leaves the rest, and
// removes only what it wrote before and no longer has.
func TestSyncSink(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC)
	run := func(files map[string]string, mod time.Time) (written []string, unchanged int, removed int) {
		t.Helper()
		s, err := NewSyncSink(dir)
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range files {
			if s.Unchanged(name, mod, int64(len(body))) {
				unchanged++
				continue
			}
			if err := s.Add(name, mod, strings.NewReader(body), false); err != nil {
				t.Fatal(err)
			}
			written = append(written, name)
		}
		removed, err = s.Finish()
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	w, u, r := run(map[string]string{"房子/2024/a.pdf": "aaa", "车/2023/b.pdf": "bb", "清单.csv": "x"}, t0)
	if len(w) != 3 || u != 0 || r != 0 {
		t.Fatalf("first: %v %d %d", w, u, r)
	}
	// someone's own file in the folder is never touched
	if err := os.WriteFile(filepath.Join(dir, "我的笔记.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	// b.pdf is gone, a.pdf is unchanged, c.pdf is new
	w, u, r = run(map[string]string{"房子/2024/a.pdf": "aaa", "房子/2024/c.pdf": "c", "清单.csv": "y"}, t0)
	if u != 2 || r != 1 || len(w) != 1 || w[0] != "房子/2024/c.pdf" {
		t.Errorf("second: written %v, unchanged %d, removed %d", w, u, r)
	}
	if _, err := os.Stat(filepath.Join(dir, "车")); !os.IsNotExist(err) {
		t.Errorf("an emptied folder stays: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "我的笔记.txt")); err != nil || string(b) != "mine" {
		t.Errorf("someone's file: %q %v", b, err)
	}
	// a changed document (newer time) is written again
	w, _, _ = run(map[string]string{"房子/2024/a.pdf": "aaa", "房子/2024/c.pdf": "c", "清单.csv": "y"}, t0.Add(time.Hour))
	if len(w) != 3 {
		t.Errorf("after a change: %v", w)
	}
	// a folder that is not an export is refused
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSyncSink(other); err == nil {
		t.Error("a folder that is not an export should be refused")
	}
}
