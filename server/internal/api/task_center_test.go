package api

import (
	"net/http"
	"testing"
	"time"
)

// v1.7.14: the task list sorts tasks into groups, shows who started them
// and lists moving documents between storages, whose last result the
// storage page keeps.
func TestTaskList(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, _, homeID := home(t, e)
	uploadedDocument(alice, "a.txt")
	uploadedDocument(bob, "b.txt")

	byKind := func(list []any, kind string) []map[string]any {
		var out []map[string]any
		for _, x := range list {
			if x := x.(map[string]any); x["kind"] == kind {
				out = append(out, x)
			}
		}
		return out
	}
	all := alice.mustDo(http.MethodGet, "/api/v1/tasks", nil, http.StatusOK).list
	ups := byKind(all, "ingest.document")
	if len(ups) != 2 || ups[0]["group"] != "upload" || ups[0]["creatorName"] != "bob" || ups[1]["creatorName"] != "alice" {
		t.Fatalf("uploads: %v", ups)
	}
	mine := bob.mustDo(http.MethodGet, "/api/v1/tasks?mine=true", nil, http.StatusOK).list
	if len(byKind(mine, "ingest.document")) != 1 {
		t.Fatalf("bob's: %v", mine)
	}
	for _, x := range mine {
		if x.(map[string]any)["creatorName"] != "bob" {
			t.Fatalf("not bob's: %v", x)
		}
	}
	for _, x := range alice.mustDo(http.MethodGet, "/api/v1/tasks?group=upload", nil, http.StatusOK).list {
		if x.(map[string]any)["group"] != "upload" {
			t.Fatalf("group upload: %v", x)
		}
	}
	if l := alice.mustDo(http.MethodGet, "/api/v1/tasks?limit=1", nil, http.StatusOK).list; len(l) != 1 {
		t.Fatalf("limit: %d", len(l))
	}
	// A scheduled check has no person behind it: the top bar's panel leaves it out (v1.7.16).
	if _, err := e.db.Exec(`INSERT INTO jobs (workspace_id, kind, payload, status, run_after, created_at, finished_at) VALUES (?, 'mail.fetch', '{}', 'succeeded', 1, ?, ?)`,
		int64(homeID), time.Now().UnixMilli(), time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if len(byKind(alice.mustDo(http.MethodGet, "/api/v1/tasks", nil, http.StatusOK).list, "mail.fetch")) != 1 {
		t.Fatal("the mail check is not listed")
	}
	for _, x := range alice.mustDo(http.MethodGet, "/api/v1/tasks?people=true", nil, http.StatusOK).list {
		if x.(map[string]any)["creatorName"] == nil {
			t.Fatalf("people: %v", x)
		}
	}
	// Unlisted kinds (the semantic index runs every five minutes) do not
	// push the listed ones past the limit (v1.7.19).
	for i := 0; i < 520; i++ {
		if _, err := e.db.Exec(`INSERT INTO jobs (workspace_id, kind, payload, status, run_after, created_at) VALUES (?, 'embed', '{}', 'succeeded', 1, ?)`,
			int64(homeID), time.Now().UnixMilli()); err != nil {
			t.Fatal(err)
		}
	}
	if ups := byKind(alice.mustDo(http.MethodGet, "/api/v1/tasks", nil, http.StatusOK).list, "ingest.document"); len(ups) != 2 {
		t.Fatalf("uploads behind the index runs: %v", ups)
	}
	alice.mustDo(http.MethodGet, "/api/v1/tasks?group=bogus", nil, http.StatusBadRequest)
	alice.mustDo(http.MethodGet, "/api/v1/tasks?limit=501", nil, http.StatusBadRequest)

	// Moving the documents to another storage is a storage task.
	job := alice.mustDo(http.MethodPut, "/api/v1/storage/location", map[string]any{"storage": "nas", "move": true}, http.StatusOK).body["jobId"].(float64)
	done := alice.waitTask(job)
	if done["status"] != "succeeded" || done["group"] != "storage" || done["label"] != "移动文档" || done["summary"] != "移了 2 份" || done["creatorName"] != "alice" {
		t.Fatalf("move task: %v", done)
	}
	if l := alice.mustDo(http.MethodGet, "/api/v1/tasks?group=storage", nil, http.StatusOK).list; len(l) != 1 {
		t.Fatalf("storage group: %v", l)
	}
	if l := alice.mustDo(http.MethodGet, "/api/v1/tasks?status=active", nil, http.StatusOK).list; len(l) != 0 {
		t.Fatalf("active: %v", l)
	}
	cur := alice.mustDo(http.MethodGet, "/api/v1/storage/location", nil, http.StatusOK).body["current"].(map[string]any)
	last, _ := cur["last"].(map[string]any)
	if last == nil || last["jobId"] != job || last["moved"] != float64(2) || last["total"] != float64(2) || last["finishedAt"] == nil {
		t.Fatalf("last move: %v", cur)
	}
}
