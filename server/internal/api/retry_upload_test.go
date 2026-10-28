package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// v1.9.12: a failed upload keeps its file for a week and can be processed
// again from the web, by the person who uploaded it (by an owner for one
// that came through a link).

func validZip(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, _ := w.Create("收据.txt")
	_, _ = f.Write([]byte("报销收据 128 元"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// keptFile is where a job's upload is kept.
func (e *testEnv) keptFile(job float64) string {
	e.t.Helper()
	var raw string
	if err := e.db.QueryRow(`SELECT payload FROM jobs WHERE id = ?`, int64(job)).Scan(&raw); err != nil {
		e.t.Fatal(err)
	}
	var pl struct{ Dir string }
	_ = json.Unmarshal([]byte(raw), &pl)
	return filepath.Join(e.fs.TmpRoot(), filepath.Base(pl.Dir), "original")
}

func taskOf(c *client, job float64) map[string]any {
	for _, it := range c.mustDo(http.MethodGet, "/api/v1/tasks?group=upload", nil, http.StatusOK).list {
		if it.(map[string]any)["id"] == job {
			return it.(map[string]any)
		}
	}
	return nil
}

func TestRetryFailedUpload(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, _ := home(t, e)
	tag := alice.mustDo(http.MethodPost, "/api/v1/tags", map[string]any{"name": "报销"}, http.StatusCreated).body["id"].(float64)

	// bob's upload fails; the file stays, with the presets
	job := bob.upload("收据.zip", "PK\x03\x04\x14\x00\x06\x00 broken", map[string]string{"tagIds": itoa(tag)}).body["jobId"].(float64)
	if j := bob.waitJob(job); j["status"] != "failed" || j["retryable"] != true {
		t.Fatalf("failed upload: %v", j)
	}
	kept := e.keptFile(job)
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("the file is not kept: %v", err)
	}
	if tk := taskOf(bob, job); tk == nil || tk["retryable"] != true || tk["expired"] == true {
		t.Errorf("bob's task: %v", tk)
	}
	// nobody else hears of it, nor may retry it
	if tk := taskOf(alice, job); tk != nil {
		t.Errorf("the owner sees bob's failed upload: %v", tk)
	}
	alice.mustDo(http.MethodPost, "/api/v1/jobs/"+itoa(job)+"/retry", nil, http.StatusNotFound)
	carol.mustDo(http.MethodPost, "/api/v1/jobs/"+itoa(job)+"/retry", nil, http.StatusForbidden) // viewers upload nothing
	// the start-up clean-up leaves it
	if _, err := e.ing.CleanTmp(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("cleaned at start-up: %v", err)
	}

	// fixed (here: the file mended), retried: the same job, the presets kept
	if err := os.WriteFile(kept, validZip(t), 0o644); err != nil {
		t.Fatal(err)
	}
	bob.mustDo(http.MethodPost, "/api/v1/jobs/"+itoa(job)+"/retry", nil, http.StatusNoContent)
	j := bob.waitJob(job)
	if j["status"] != "succeeded" || j["documentId"] == nil || j["retryable"] != nil {
		t.Fatalf("retried: %v", j)
	}
	d := bob.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(j["documentId"].(float64)), nil, http.StatusOK).body
	if tags := d["tagIds"].([]any); len(tags) != 1 || tags[0] != tag {
		t.Errorf("presets lost: %v", d["tagIds"])
	}
	if _, err := os.Stat(kept); !os.IsNotExist(err) {
		t.Errorf("the file stays after success: %v", err)
	}
	if r := bob.do(http.MethodPost, "/api/v1/jobs/"+itoa(job)+"/retry", nil); r.code != http.StatusConflict || r.body["code"] != "not_failed" {
		t.Errorf("retrying a done job: %d %v", r.code, r.body)
	}

	// a duplicate (found while processing) is not kept: retrying it would
	// end the same way
	dup := bob.upload("重复.zip", "PK\x03\x04 broken twice", nil).body["jobId"].(float64)
	bob.waitJob(dup)
	if _, err := e.db.Exec(`UPDATE jobs SET error = '这份文件已经存在' WHERE id = ?`, int64(dup)); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Dir(e.keptFile(dup)))
	if r := bob.do(http.MethodPost, "/api/v1/jobs/"+itoa(dup)+"/retry", nil); r.code != http.StatusConflict || r.body["code"] != "not_retryable" {
		t.Errorf("retrying a duplicate: %d %v", r.code, r.body)
	}
	if tk := taskOf(bob, dup); tk == nil || tk["retryable"] != false || tk["expired"] == true {
		t.Errorf("a duplicate shows as expired: %v", tk)
	}

	// a week on, the file goes
	old := bob.upload("旧.zip", "PK\x03\x04 broken again", nil).body["jobId"].(float64)
	bob.waitJob(old)
	if _, err := e.db.Exec(`UPDATE jobs SET finished_at = ? WHERE id = ?`, time.Now().Add(-8*24*time.Hour).UnixMilli(), int64(old)); err != nil {
		t.Fatal(err)
	}
	if tk := taskOf(bob, old); tk == nil || tk["retryable"] != false || tk["expired"] != true {
		t.Errorf("expired task: %v", tk)
	}
	if r := bob.do(http.MethodPost, "/api/v1/jobs/"+itoa(old)+"/retry", nil); r.code != http.StatusConflict || r.body["code"] != "upload_expired" {
		t.Errorf("retrying an expired upload: %d %v", r.code, r.body)
	}
	if n, err := e.ing.PurgeFailed(t.Context(), time.Now()); err != nil || n != 1 { // the duplicate's is gone already
		t.Errorf("purged %d, %v", n, err)
	}
	if _, err := os.Stat(e.keptFile(old)); !os.IsNotExist(err) {
		t.Errorf("an expired file stays: %v", err)
	}

	checkLinkUploadRetry(t, e, alice, bob) // the official build only (v1.13)
}

// v1.9.12: the task list, like the tray (v1.9.10), does not show someone
// else's upload before it is a document the viewer sees.
func TestTaskListHidesOthersUploads(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, _, homeID := home(t, e)
	// bob's upload, waiting in the queue (run later: no worker takes it now)
	var id int64
	if err := e.db.QueryRow(`INSERT INTO jobs (workspace_id, kind, payload, status, run_after, created_at, created_by)
		VALUES (?, 'ingest.document', '{"filename":"便条.txt"}', 'queued', ?, ?, ?) RETURNING id`,
		int64(homeID), time.Now().Add(time.Hour).UnixMilli(), time.Now().UnixMilli(), int64(userID(bob))).Scan(&id); err != nil {
		t.Fatal(err)
	}
	job := float64(id)
	if tk := taskOf(alice, job); tk != nil {
		t.Errorf("the owner sees bob's upload in progress: %v", tk)
	}
	if tk := taskOf(bob, job); tk == nil {
		t.Error("bob does not see his own upload")
	}
}
