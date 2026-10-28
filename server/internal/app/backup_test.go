package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/backup"
	"kept/server/internal/config"
	"kept/server/internal/store"
)

// "Back up now" on the system page runs a real backup into KEPT_BACKUP_DIR
// and the page then shows its result.
func TestServeBacksUpOnRequest(t *testing.T) {
	if store.TestingPostgres() {
		t.Skip("Postgres databases are backed up with pg_dump")
	}
	dir, bdir := t.TempDir(), filepath.Join(t.TempDir(), "backups")
	cfg, err := config.Load(func(k string) string {
		return map[string]string{"KEPT_DATA_DIR": dir, "KEPT_HTTP_ADDR": "127.0.0.1:0", "KEPT_SHUTDOWN_TIMEOUT": "6s", "KEPT_BACKUP_DIR": bdir}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), ready) }()
	defer func() { cancel(); <-done }()
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("Serve exited early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not start")
	}
	base := "http://" + addr
	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar}
	resp, err := cl.Post(base+"/api/v1/setup", "application/json", strings.NewReader(`{"username":"admin","email":"admin@example.com","password":"password-123"}`))
	if err != nil {
		t.Fatal(err)
	}
	var me struct{ CSRFToken string }
	_ = json.NewDecoder(resp.Body).Decode(&me)
	resp.Body.Close()

	req, _ := http.NewRequest(http.MethodPost, base+"/api/v1/admin/backup", nil)
	req.Header.Set("X-CSRF-Token", me.CSRFToken)
	req.Header.Set("Origin", base)
	resp, err = cl.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("back up now: %d", resp.StatusCode)
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		resp, err := cl.Get(base + "/api/v1/admin/system")
		if err != nil {
			t.Fatal(err)
		}
		var st struct {
			Backup struct {
				Dir       string
				Snapshots int
				Last      *struct{ Status, Snapshot, Error string }
			}
		}
		_ = json.NewDecoder(resp.Body).Decode(&st)
		resp.Body.Close()
		if l := st.Backup.Last; l != nil && l.Status == "succeeded" {
			// The first scheduler check also backs up once (a new entry runs
			// at once), so there may be two snapshots.
			if st.Backup.Dir != bdir || st.Backup.Snapshots < 1 || l.Snapshot == "" {
				t.Errorf("backup status = %+v %+v", st.Backup, *l)
			}
			break
		}
		if l := st.Backup.Last; l != nil && l.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("backup did not succeed: %+v", st.Backup)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if m, err := backup.ReadManifest(bdir); err != nil || m.Snapshot == "" {
		t.Errorf("manifest = %+v, %v", m, err)
	}
}
