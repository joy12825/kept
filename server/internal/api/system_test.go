package api

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"kept/server/internal/logging"
)

func TestTasksSystemAndLogs(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin()
	alice := e.user("alice")

	r := alice.upload("电费.txt", "本期电费", nil)
	alice.waitJob(r.body["jobId"].(float64))
	tasks := alice.mustDo(http.MethodGet, "/api/v1/tasks", nil, http.StatusOK).list
	if len(tasks) == 0 {
		t.Fatal("no tasks")
	}
	first := tasks[len(tasks)-1].(map[string]any)
	if first["label"] != "上传" || first["summary"] != "电费.txt" || first["status"] != "succeeded" {
		t.Errorf("task = %v", first)
	}
	if failed := alice.mustDo(http.MethodGet, "/api/v1/tasks?status=failed", nil, http.StatusOK).list; len(failed) != 0 {
		t.Errorf("failed tasks = %v", failed)
	}
	if r := admin.do(http.MethodGet, "/api/v1/tasks?days=0", nil); r.code != http.StatusBadRequest {
		t.Errorf("days=0: %d", r.code)
	}

	// Administrators only.
	for _, p := range []string{"/api/v1/admin/system", "/api/v1/admin/logs"} {
		if r := alice.do(http.MethodGet, p, nil); r.code != http.StatusForbidden {
			t.Errorf("alice %s: %d", p, r.code)
		}
	}
	st := admin.mustDo(http.MethodGet, "/api/v1/admin/system", nil, http.StatusOK).body
	if st["documents"].(float64) < 1 || st["users"].(float64) < 2 || len(st["tools"].([]any)) == 0 || st["integrity"] == nil {
		t.Errorf("system = %v", st)
	}

	slog.New(logging.NewHandlerForTest()).Warn("测试日志", "who", "system_test")
	logs := admin.mustDo(http.MethodGet, "/api/v1/admin/logs?level=warn&q=system_test", nil, http.StatusOK).list
	if len(logs) == 0 || logs[0].(map[string]any)["message"] != "测试日志" || logs[0].(map[string]any)["attrs"].(map[string]any)["who"] != "system_test" {
		t.Errorf("logs = %v", logs)
	}
}

func TestBackupNowNeedsADirectory(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin()
	alice := e.user("alice")
	if r := alice.do(http.MethodPost, "/api/v1/admin/backup", nil); r.code != http.StatusForbidden {
		t.Errorf("alice backs up: %d", r.code)
	}
	if r := admin.do(http.MethodPost, "/api/v1/admin/backup", nil); r.code != http.StatusConflict || code(r) != "backup_not_configured" {
		t.Errorf("backup without a directory: %d %s", r.code, r.raw)
	}
	b := admin.mustDo(http.MethodGet, "/api/v1/admin/system", nil, http.StatusOK).body["backup"].(map[string]any)
	if b["dir"] != nil || b["snapshots"] != float64(0) || b["last"] != nil {
		t.Errorf("backup status = %v", b)
	}
}

// The daily backup folder chosen on the page (v1.13.9): every edition, the
// instance administrator only; then "back up now" writes there.
func TestBackupFolderChosenOnThePage(t *testing.T) {
	e := newEnv(t, "auto", nil)
	admin := e.setupAdmin()
	alice := e.user("alice")
	disk := t.TempDir()
	if r := alice.do(http.MethodPut, "/api/v1/admin/backup", map[string]any{"dir": disk}); r.code != http.StatusForbidden {
		t.Errorf("alice chooses the folder: %d", r.code)
	}
	if r := admin.do(http.MethodPut, "/api/v1/admin/backup", map[string]any{"dir": "usb/kept"}); r.code != http.StatusBadRequest || code(r) != "invalid_backup_dir" {
		t.Errorf("a relative path: %d %s", r.code, r.raw)
	}
	admin.mustDo(http.MethodPut, "/api/v1/admin/backup", map[string]any{"dir": disk}, http.StatusNoContent)
	b := admin.mustDo(http.MethodGet, "/api/v1/admin/system", nil, http.StatusOK).body["backup"].(map[string]any)
	if b["dir"] != disk || b["dirFromEnv"] == true {
		t.Errorf("backup status = %v", b)
	}
	if tg := admin.mustDo(http.MethodGet, "/api/v1/admin/backup-target", nil, http.StatusOK).body; tg["dir"] != disk {
		t.Errorf("backup target = %v", tg)
	}
	admin.mustDo(http.MethodPost, "/api/v1/admin/backup", nil, http.StatusAccepted)
	deadline := time.Now().Add(10 * time.Second)
	for {
		last, _ := admin.mustDo(http.MethodGet, "/api/v1/admin/system", nil, http.StatusOK).body["backup"].(map[string]any)["last"].(map[string]any)
		if last != nil && last["status"] == "succeeded" {
			break
		}
		if last != nil && last["status"] == "failed" || time.Now().After(deadline) {
			t.Fatalf("backup: %v", last)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, err := os.Stat(filepath.Join(disk, "kept-backup.json")); err != nil {
		t.Errorf("nothing in the folder: %v", err)
	}
	// turned off: back to needing a folder
	admin.mustDo(http.MethodPut, "/api/v1/admin/backup", map[string]any{"dir": ""}, http.StatusNoContent)
	if r := admin.do(http.MethodPost, "/api/v1/admin/backup", nil); r.code != http.StatusConflict || code(r) != "backup_not_configured" {
		t.Errorf("after turning off: %d", r.code)
	}
}
