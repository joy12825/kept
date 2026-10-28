package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"golang.org/x/net/webdav"

	"kept/server/internal/pdftest"
)

// TestStorages: a workspace switched to another storage writes new
// documents there and, with move, moves the ones it has; reading them
// goes on as before.
func TestStorages(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, _, homeID := home(t, e)
	root := e.user("root")
	if err := e.auth.SetAdmin(t.Context(), 4, true); err != nil {
		t.Fatal(err)
	}
	before := uploadedDocument(bob, "a.txt")

	alice.mustDo(http.MethodGet, "/api/v1/admin/storages", nil, http.StatusForbidden)
	list := root.mustDo(http.MethodGet, "/api/v1/admin/storages", nil, http.StatusOK).body["items"].([]any)
	if len(list) != 3 || list[2].(map[string]any)["kind"] != "s3" {
		t.Fatalf("storages = %v", list)
	}
	local, nas := list[0].(map[string]any), list[1].(map[string]any)
	if local["name"] != "local" || local["kind"] != "local" || local["documents"] != float64(1) || nas["name"] != "nas" || nas["kind"] != "dir" || nas["documents"] != float64(0) {
		t.Fatalf("storages = %v", list)
	}
	nasDir := nas["location"].(string)
	if r := root.mustDo(http.MethodPost, "/api/v1/admin/storages/nas/test", nil, http.StatusOK).body; r["ok"] != true {
		t.Fatalf("test nas = %v", r)
	}
	root.mustDo(http.MethodPost, "/api/v1/admin/storages/nope/test", nil, http.StatusNotFound)

	path := "/api/v1/admin/workspaces/" + itoa(homeID) + "/storage"
	alice.mustDo(http.MethodPut, path, map[string]any{"storage": "nas", "move": true}, http.StatusForbidden)
	root.mustDo(http.MethodPut, path, map[string]any{"storage": "nope", "move": false}, http.StatusBadRequest)
	// Only new documents: the old one stays.
	if r := root.mustDo(http.MethodPut, path, map[string]any{"storage": "nas", "move": false}, http.StatusOK).body; r["jobId"] != nil {
		t.Fatalf("set without move = %v", r)
	}
	after := uploadedDocument(bob, "b.txt")
	ws := storageOf(t, root, homeID)
	if ws["name"] != "nas" || ws["pending"] != float64(1) || ws["moving"] != false {
		t.Fatalf("workspace storage = %v", ws)
	}
	var where string
	if err := e.db.QueryRow(`SELECT storage FROM documents WHERE id = ?`, int64(after)).Scan(&where); err != nil || where != "nas" {
		t.Fatalf("new document in %q, %v", where, err)
	}
	if _, err := os.Stat(filepath.Join(nasDir, "originals", itoa(homeID), itoa(after)+".txt")); err != nil {
		t.Fatalf("new original not in nas: %v", err)
	}
	if got := bob.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(after)+"/original", nil, http.StatusOK).raw; got != "content of b.txt" {
		t.Errorf("original from nas = %q", got)
	}

	// An earlier version moves with its document.
	r := bob.addVersion(before, "a2.txt", []byte("second of a"), "")
	if r.code != http.StatusAccepted {
		t.Fatalf("add a version: %d %s", r.code, r.raw)
	}
	bob.waitJob(r.body["jobId"].(float64))

	// Move the rest.
	if r := root.mustDo(http.MethodPut, path, map[string]any{"storage": "nas", "move": true}, http.StatusOK).body; r["jobId"] == nil {
		t.Fatalf("set with move = %v", r)
	}
	ws = waitMoved(t, root, homeID)
	if ws["pending"] != float64(0) || ws["error"] != nil {
		t.Fatalf("after the move = %v", ws)
	}
	if err := e.db.QueryRow(`SELECT storage FROM documents WHERE id = ?`, int64(before)).Scan(&where); err != nil || where != "nas" {
		t.Fatalf("old document in %q, %v", where, err)
	}
	if got := bob.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(before)+"/original", nil, http.StatusOK).raw; got != "second of a" {
		t.Errorf("moved original = %q", got)
	}
	var first float64
	for _, v := range bob.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(before)+"/versions", nil, http.StatusOK).list {
		if v := v.(map[string]any); v["current"] == false {
			first = v["id"].(float64)
		}
	}
	if got := bob.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(before)+"/versions/"+itoa(first)+"/file", nil, http.StatusOK).raw; got != "content of a.txt" {
		t.Errorf("moved earlier version = %q", got)
	}
	for _, kind := range []string{"originals", "versions"} {
		if left, _ := os.ReadDir(filepath.Join(local["location"].(string), kind, itoa(homeID))); len(left) != 0 {
			t.Errorf("left in the data directory's %s: %v", kind, left)
		}
	}
	if list := root.mustDo(http.MethodGet, "/api/v1/admin/storages", nil, http.StatusOK).body["items"].([]any); list[1].(map[string]any)["documents"] != float64(2) {
		t.Errorf("storages after the move = %v", list)
	}

	// And back.
	root.mustDo(http.MethodPut, path, map[string]any{"storage": "local", "move": true}, http.StatusOK)
	if ws := waitMoved(t, root, homeID); ws["name"] != "local" || ws["pending"] != float64(0) {
		t.Fatalf("back to local = %v", ws)
	}
	if got := bob.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(after)+"/original", nil, http.StatusOK).raw; got != "content of b.txt" {
		t.Errorf("original back in local = %q", got)
	}
}

func storageOf(t *testing.T, admin *client, ws float64) map[string]any {
	t.Helper()
	for _, w := range admin.mustDo(http.MethodGet, "/api/v1/admin/workspaces", nil, http.StatusOK).body["items"].([]any) {
		if wm := w.(map[string]any); wm["id"] == ws {
			return wm["storage"].(map[string]any)
		}
	}
	t.Fatalf("no workspace %v", ws)
	return nil
}

func waitMoved(t *testing.T, admin *client, ws float64) map[string]any {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if st := storageOf(t, admin, ws); st["moving"] == false {
			return st
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("the move did not finish")
	return nil
}

// TestStorageOnS3 (v1.7.1): a workspace on an S3 storage uploads there,
// previews with ranges, edits pages, keeps versions, and moves back.
func TestStorageOnS3(t *testing.T) {
	needPDFTools(t)
	e := newEnv(t, "auto", nil)
	_, bob, _, homeID := home(t, e)
	root := e.user("root")
	if err := e.auth.SetAdmin(t.Context(), 4, true); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/workspaces/" + itoa(homeID) + "/storage"
	root.mustDo(http.MethodPut, path, map[string]any{"storage": "s3", "move": false}, http.StatusOK)
	if r := root.mustDo(http.MethodPost, "/api/v1/admin/storages/s3/test", nil, http.StatusOK).body; r["ok"] != true {
		t.Fatalf("test s3 = %v", r)
	}

	r := bob.uploadBytes("合同.pdf", pdftest.Text("PAGE-ONE", "PAGE-TWO", "PAGE-THREE"), nil)
	if r.code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", r.code, r.raw)
	}
	id := bob.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	var where string
	if err := e.db.QueryRow(`SELECT storage FROM documents WHERE id = ?`, int64(id)).Scan(&where); err != nil || where != "s3" {
		t.Fatalf("document in %q, %v", where, err)
	}
	doc := "/api/v1/documents/" + itoa(id)

	// A range of the original, as pdf.js asks.
	req := httptest.NewRequest(http.MethodGet, doc+"/original", nil)
	req.Header.Set("Range", "bytes=0-3")
	if got := bob.send(req); got.code != http.StatusPartialContent || got.raw != "%PDF" {
		t.Fatalf("range = %d %q", got.code, got.raw)
	}

	// Page edits read and write the stored PDFs.
	d := bob.mustDo(http.MethodPut, doc+"/pages", map[string]any{"delete": []int{2}}, http.StatusOK).body
	if d["pageCount"] != float64(2) || strings.Contains(d["content"].(string), "PAGE-TWO") {
		t.Fatalf("after editing: %v", d)
	}
	if unedited := bob.mustDo(http.MethodGet, doc+"/preview?unedited=true", nil, http.StatusOK).raw; !strings.HasPrefix(unedited, "%PDF") {
		t.Error("no unedited PDF")
	}

	// A new version: the old original is kept in the storage.
	first := bob.mustDo(http.MethodGet, doc+"/original", nil, http.StatusOK).raw
	if v := bob.addVersion(id, "合同-二稿.pdf", pdftest.Text("SECOND"), ""); v.code != http.StatusAccepted {
		t.Fatalf("add a version: %d %s", v.code, v.raw)
	} else {
		bob.waitJob(v.body["jobId"].(float64))
	}
	var older float64
	for _, v := range bob.mustDo(http.MethodGet, doc+"/versions", nil, http.StatusOK).list {
		if v := v.(map[string]any); v["current"] == false {
			older = v["id"].(float64)
		}
	}
	if got := bob.mustDo(http.MethodGet, doc+"/versions/"+itoa(older)+"/file", nil, http.StatusOK).raw; got != first {
		t.Error("earlier version differs from the first original")
	}
	b, _ := e.fs.Backend("s3")
	if left, _ := b.List(t.Context(), "incoming/"); len(left) != 0 {
		t.Errorf("uploads left in incoming: %v", left)
	}

	// And back to the data directory.
	root.mustDo(http.MethodPut, path, map[string]any{"storage": "local", "move": true}, http.StatusOK)
	if ws := waitMoved(t, root, homeID); ws["pending"] != float64(0) || ws["error"] != nil {
		t.Fatalf("back to local = %v", ws)
	}
	if got := bob.mustDo(http.MethodGet, doc+"/versions/"+itoa(older)+"/file", nil, http.StatusOK).raw; got != first {
		t.Error("earlier version lost in the move")
	}
	if keys, _ := b.List(t.Context(), ""); len(keys) != 0 {
		t.Errorf("left in s3 after moving back: %v", keys)
	}
}

// TestStoragesOnThePage (v1.7.2): the instance administrator adds, tests,
// changes and deletes storages on the page; a workspace administrator
// picks one for the workspace.
func TestStoragesOnThePage(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, homeID := home(t, e)
	root := e.user("root")
	if err := e.auth.SetAdmin(t.Context(), 4, true); err != nil {
		t.Fatal(err)
	}
	mem := s3mem.New()
	if err := mem.CreateBucket("family"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(gofakes3.New(mem).Server())
	t.Cleanup(srv.Close)
	const secretKey = "very-secret-key-123"
	cloud := map[string]any{"name": "cloud", "kind": "s3", "description": "家里的 MinIO", "selectable": true, "secretKey": secretKey,
		"settings": map[string]any{"endpoint": srv.URL, "bucket": "family", "prefix": "docs", "pathStyle": true, "accessKey": "ak"}}

	alice.mustDo(http.MethodPost, "/api/v1/admin/storages", cloud, http.StatusForbidden)
	// Try before saving.
	if r := root.mustDo(http.MethodPost, "/api/v1/admin/storage-test", map[string]any{"storage": cloud}, http.StatusOK).body; r["ok"] != true {
		t.Fatalf("try = %v", r)
	}
	root.mustDo(http.MethodPost, "/api/v1/admin/storage-test", map[string]any{"storage": map[string]any{"kind": "s3", "selectable": true,
		"settings": map[string]any{"bucket": "family"}}}, http.StatusBadRequest) // no keys
	for _, bad := range []map[string]any{
		{"name": "Bad Name", "kind": "dir", "selectable": true, "settings": map[string]any{"path": t.TempDir()}},
		{"name": "nas", "kind": "dir", "selectable": true, "settings": map[string]any{"path": t.TempDir()}},   // taken
		{"name": "local", "kind": "dir", "selectable": true, "settings": map[string]any{"path": t.TempDir()}}, // the data directory
		{"name": "rel", "kind": "dir", "selectable": true, "settings": map[string]any{"path": "relative/dir"}},
	} {
		if r := root.do(http.MethodPost, "/api/v1/admin/storages", bad); r.code != http.StatusBadRequest && r.code != http.StatusConflict {
			t.Errorf("%v: %d %s", bad, r.code, r.raw)
		}
	}
	root.mustDo(http.MethodPost, "/api/v1/admin/storages", cloud, http.StatusNoContent)
	root.mustDo(http.MethodPost, "/api/v1/admin/storages", cloud, http.StatusConflict)
	root.mustDo(http.MethodPost, "/api/v1/admin/storages", map[string]any{"name": "hidden", "kind": "dir", "selectable": false,
		"settings": map[string]any{"path": t.TempDir()}}, http.StatusNoContent)

	// The key is sealed, and never comes back.
	list := root.mustDo(http.MethodGet, "/api/v1/admin/storages", nil, http.StatusOK)
	if strings.Contains(list.raw, secretKey) {
		t.Fatal("the secret key is listed")
	}
	var sealed string
	if err := e.db.QueryRow(`SELECT secret FROM storages WHERE name = 'cloud'`).Scan(&sealed); err != nil || sealed == "" || strings.Contains(sealed, secretKey) {
		t.Fatalf("stored secret = %q %v", sealed, err)
	}
	var cloudInfo map[string]any
	for _, s := range list.body["items"].([]any) {
		if s := s.(map[string]any); s["name"] == "cloud" {
			cloudInfo = s
		} else if s["name"] == "nas" && s["source"] != "env" {
			t.Errorf("nas = %v", s)
		}
	}
	if cloudInfo == nil || cloudInfo["source"] != "page" || cloudInfo["kind"] != "s3" || cloudInfo["description"] != "家里的 MinIO" ||
		cloudInfo["settings"].(map[string]any)["accessKey"] != "ak" || cloudInfo["settings"].(map[string]any)["bucket"] != "family" {
		t.Fatalf("cloud = %v", cloudInfo)
	}
	if r := root.mustDo(http.MethodPost, "/api/v1/admin/storages/cloud/test", nil, http.StatusOK).body; r["ok"] != true {
		t.Fatalf("test saved cloud = %v", r)
	}

	// The workspace administrator picks it; others cannot.
	carol.mustDo(http.MethodGet, "/api/v1/storage/location", nil, http.StatusForbidden)
	loc := alice.mustDo(http.MethodGet, "/api/v1/storage/location", nil, http.StatusOK).body
	var names []string
	for _, c := range loc["choices"].([]any) {
		names = append(names, c.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "local,cloud,nas,s3" || loc["current"].(map[string]any)["name"] != "local" {
		t.Fatalf("location = %v", loc)
	}
	alice.mustDo(http.MethodPut, "/api/v1/storage/location", map[string]any{"storage": "hidden", "move": false}, http.StatusBadRequest)
	alice.mustDo(http.MethodPut, "/api/v1/storage/location", map[string]any{"storage": "cloud", "move": false}, http.StatusOK)
	id := uploadedDocument(bob, "rent.txt")
	var where string
	if err := e.db.QueryRow(`SELECT storage FROM documents WHERE id = ?`, int64(id)).Scan(&where); err != nil || where != "cloud" {
		t.Fatalf("new document in %q, %v", where, err)
	}
	if obj, err := mem.HeadObject("family", "docs/originals/"+itoa(homeID)+"/"+itoa(id)+".txt"); err != nil || obj == nil {
		t.Fatalf("not in the bucket: %v", err)
	}

	// Changing: where files go is fixed once documents are there; the
	// description, the endpoint and the keys can change (empty key: kept).
	moved := map[string]any{"kind": "s3", "selectable": true, "settings": map[string]any{"endpoint": srv.URL, "bucket": "family", "prefix": "other", "pathStyle": true, "accessKey": "ak"}}
	root.mustDo(http.MethodPut, "/api/v1/admin/storages/cloud", moved, http.StatusConflict)
	root.mustDo(http.MethodPut, "/api/v1/admin/storages/cloud", map[string]any{"kind": "s3", "description": "NAS", "selectable": true,
		"settings": map[string]any{"endpoint": srv.URL, "bucket": "family", "prefix": "docs", "pathStyle": true, "accessKey": "ak"}}, http.StatusNoContent)
	if got := bob.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(id)+"/original", nil, http.StatusOK).raw; got != "content of rent.txt" {
		t.Errorf("original after the change = %q", got)
	}
	root.mustDo(http.MethodPut, "/api/v1/admin/storages/nas", moved, http.StatusConflict) // the deployment's

	// Deleting: not while used.
	root.mustDo(http.MethodDelete, "/api/v1/admin/storages/cloud", nil, http.StatusConflict)
	alice.mustDo(http.MethodPut, "/api/v1/storage/location", map[string]any{"storage": "local", "move": true}, http.StatusOK)
	if ws := waitMoved(t, root, homeID); ws["pending"] != float64(0) {
		t.Fatalf("back to local = %v", ws)
	}
	root.mustDo(http.MethodDelete, "/api/v1/admin/storages/cloud", nil, http.StatusNoContent)
	root.mustDo(http.MethodDelete, "/api/v1/admin/storages/nas", nil, http.StatusConflict)
	if got := root.mustDo(http.MethodGet, "/api/v1/admin/storages", nil, http.StatusOK).raw; strings.Contains(got, `"cloud"`) {
		t.Error("cloud still listed")
	}
	if got := bob.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(id)+"/original", nil, http.StatusOK).raw; got != "content of rent.txt" {
		t.Errorf("original back home = %q", got)
	}
}

// TestWebDAVStorageOnThePage (v1.7.5): a WebDAV folder added on the page,
// tested, picked by a workspace administrator, written to and read from.
func TestWebDAVStorageOnThePage(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, _, homeID := home(t, e)
	root := e.user("root")
	if err := e.auth.SetAdmin(t.Context(), 4, true); err != nil {
		t.Fatal(err)
	}
	mem := webdav.NewMemFS()
	h := &webdav.Handler{Prefix: "/dav", FileSystem: mem, LockSystem: webdav.NewMemLS()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "wang" || p != "app-pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	dav := map[string]any{"name": "jgy", "kind": "webdav", "description": "坚果云", "selectable": true, "secretKey": "app-pass",
		"settings": map[string]any{"url": srv.URL + "/dav/kept", "username": "wang"}}

	wrong := map[string]any{"storage": map[string]any{"kind": "webdav", "selectable": true, "secretKey": "nope", "settings": dav["settings"]}}
	if r := root.mustDo(http.MethodPost, "/api/v1/admin/storage-test", wrong, http.StatusOK).body; r["ok"] != false || !strings.Contains(r["error"].(string), "用户名或密码") {
		t.Fatalf("wrong password = %v", r)
	}
	root.mustDo(http.MethodPost, "/api/v1/admin/storages", map[string]any{"name": "bad", "kind": "webdav", "selectable": true,
		"settings": map[string]any{"url": "ftp://x/y"}}, http.StatusBadRequest)
	root.mustDo(http.MethodPost, "/api/v1/admin/storages", dav, http.StatusNoContent) // makes the kept folder
	if r := root.mustDo(http.MethodPost, "/api/v1/admin/storages/jgy/test", nil, http.StatusOK).body; r["ok"] != true {
		t.Fatalf("test = %v", r)
	}
	list := root.mustDo(http.MethodGet, "/api/v1/admin/storages", nil, http.StatusOK)
	if strings.Contains(list.raw, "app-pass") || !strings.Contains(list.raw, `"kind":"webdav"`) || !strings.Contains(list.raw, `"username":"wang"`) {
		t.Fatalf("list = %s", list.raw)
	}

	alice.mustDo(http.MethodPut, "/api/v1/storage/location", map[string]any{"storage": "jgy", "move": false}, http.StatusOK)
	id := uploadedDocument(bob, "water.txt")
	if f, err := mem.OpenFile(t.Context(), "/kept/originals/"+itoa(homeID)+"/"+itoa(id)+".txt", 0, 0); err != nil {
		t.Fatalf("not on the WebDAV server: %v", err)
	} else {
		f.Close()
	}
	if got := bob.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(id)+"/original", nil, http.StatusOK).raw; got != "content of water.txt" {
		t.Errorf("original = %q", got)
	}
	// The password can change without retyping the rest; the folder cannot once used.
	root.mustDo(http.MethodPut, "/api/v1/admin/storages/jgy", map[string]any{"kind": "webdav", "selectable": true,
		"settings": map[string]any{"url": srv.URL + "/dav/other", "username": "wang"}}, http.StatusConflict)
	root.mustDo(http.MethodPut, "/api/v1/admin/storages/jgy", map[string]any{"kind": "webdav", "description": "坚果云网盘", "selectable": true,
		"settings": map[string]any{"url": srv.URL + "/dav/kept", "username": "wang"}}, http.StatusNoContent)
	if got := bob.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(id)+"/original", nil, http.StatusOK).raw; got != "content of water.txt" {
		t.Errorf("original after the change = %q", got)
	}
}

// TestStorageCapacity (v1.7.10): a storage with a capacity takes no
// uploads, and no moves, past it.
func TestStorageCapacity(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, _, homeID := home(t, e)
	root := e.user("root")
	if err := e.auth.SetAdmin(t.Context(), 4, true); err != nil {
		t.Fatal(err)
	}
	root.mustDo(http.MethodPost, "/api/v1/admin/storages", map[string]any{"name": "small", "kind": "dir", "selectable": true, "capacity": 40,
		"settings": map[string]any{"path": t.TempDir()}}, http.StatusNoContent)
	root.mustDo(http.MethodPost, "/api/v1/admin/storages", map[string]any{"name": "tiny", "kind": "dir", "selectable": true, "capacity": 5,
		"settings": map[string]any{"path": t.TempDir()}}, http.StatusNoContent)
	alice.mustDo(http.MethodPut, "/api/v1/storage/location", map[string]any{"storage": "small", "move": false}, http.StatusOK)
	first := uploadedDocument(bob, "a.txt") // 16 bytes, room held for 32
	r := bob.upload("b.txt", "content of b.txt", nil)
	if r.code != http.StatusConflict || code(r) != "storage_full" || !strings.Contains(r.raw, "存储“small”已满") {
		t.Fatalf("over capacity = %d %s", r.code, r.raw)
	}
	var small map[string]any
	for _, s := range root.mustDo(http.MethodGet, "/api/v1/admin/storages", nil, http.StatusOK).body["items"].([]any) {
		if s := s.(map[string]any); s["name"] == "small" {
			small = s
		}
	}
	if small["used"] != float64(16) || small["capacity"] != float64(40) {
		t.Errorf("small = %v", small)
	}
	// Moving into a storage too small stops; the documents stay.
	root.mustDo(http.MethodPut, "/api/v1/admin/workspaces/"+itoa(homeID)+"/storage", map[string]any{"storage": "tiny", "move": true}, http.StatusOK)
	ws := waitMoved(t, root, homeID)
	if !strings.Contains(fmt.Sprint(ws["error"]), "存储“tiny”已满") || ws["pending"] != float64(1) {
		t.Errorf("move into tiny = %v", ws)
	}
	var where string
	if err := e.db.QueryRow(`SELECT storage FROM documents WHERE id = ?`, int64(first)).Scan(&where); err != nil || where != "small" {
		t.Errorf("document in %q, %v", where, err)
	}
}
