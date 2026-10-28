package quota

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o640); err != nil {
		t.Fatal(err)
	}
}
