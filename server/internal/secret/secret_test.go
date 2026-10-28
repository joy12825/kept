package secret

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSealOpen(t *testing.T) {
	dir := t.TempDir()
	b, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", st, err)
	}
	sealed, err := b.Seal("s3-secret")
	if err != nil || sealed == "s3-secret" {
		t.Fatalf("sealed = %q %v", sealed, err)
	}
	again, _ := b.Seal("s3-secret")
	if again == sealed {
		t.Error("the same secret sealed twice looks the same")
	}
	// The key file is read back on the next start.
	b2, err := Load(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := b2.Open(sealed); err != nil || got != "s3-secret" {
		t.Errorf("open = %q %v", got, err)
	}
	if got, err := b.Seal(""); err != nil || got != "" {
		t.Errorf("empty = %q %v", got, err)
	}
	// Another key cannot open it.
	other, err := Load(t.TempDir(), "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(sealed); !errors.Is(err, ErrDecrypt) {
		t.Errorf("other key: %v", err)
	}
	if _, err := b.Open("garbage"); !errors.Is(err, ErrDecrypt) {
		t.Errorf("garbage: %v", err)
	}
	if _, err := Load(dir, "short"); err == nil {
		t.Error("a short KEPT_SECRET_KEY accepted")
	}
}
