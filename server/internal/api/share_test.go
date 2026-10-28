package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestShareLinks(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	bob := e.user("bob")
	doc := alice.waitJob(alice.upload("租房合同.txt", "甲方 乙方 房租 3000 元", nil).body["jobId"].(float64))["documentId"].(float64)
	path := "/api/v1/documents/" + itoa(doc) + "/share-links"

	if r := alice.do(http.MethodPost, path, map[string]any{"days": 0, "file": "archive"}); r.code != http.StatusBadRequest {
		t.Fatalf("0 days: %d", r.code)
	}
	bob.mustDo(http.MethodPost, path, map[string]any{"days": 7, "file": "archive"}, http.StatusNotFound) // another workspace
	l := alice.mustDo(http.MethodPost, path, map[string]any{"days": 7, "file": "archive"}, http.StatusCreated).body
	token := l["token"].(string)
	if len(token) != 32 || l["file"] != "original" || l["creatorName"] == "" { // a text file has no searchable PDF
		t.Fatalf("link: %v", l)
	}

	anon := e.anon()
	pub := anon.mustDo(http.MethodGet, "/api/v1/public/shares/"+token, nil, http.StatusOK)
	if pub.body["title"] != "租房合同" || pub.body["filename"] != "租房合同.txt" || pub.body["mimeType"] != "text/plain" || strings.Contains(pub.raw, "alice") {
		t.Fatalf("public: %s", pub.raw)
	}
	f := anon.mustDo(http.MethodGet, "/api/v1/public/shares/"+token+"/file?download=true", nil, http.StatusOK)
	if !strings.Contains(f.raw, "房租 3000 元") || !strings.HasPrefix(f.header.Get("Content-Disposition"), "attachment") || f.header.Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("file: %v %q", f.header, f.raw)
	}
	links := alice.mustDo(http.MethodGet, path, nil, http.StatusOK)
	if len(links.list) != 1 || links.list[0].(map[string]any)["accessCount"] != float64(1) {
		t.Fatalf("links: %s", links.raw)
	}
	hist := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(doc)+"/history", nil, http.StatusOK)
	if !strings.Contains(hist.raw, "分享了“租房合同”的原件，7 天有效") {
		t.Fatalf("history: %s", hist.raw)
	}

	// A wrong token and a revoked link look the same.
	if r := anon.do(http.MethodGet, "/api/v1/public/shares/"+strings.Repeat("x", 32), nil); r.code != http.StatusNotFound || code(r) != "share_expired" {
		t.Fatalf("unknown: %d %s", r.code, r.raw)
	}
	bob.mustDo(http.MethodDelete, "/api/v1/share-links/"+itoa(l["id"].(float64)), nil, http.StatusNotFound)
	alice.mustDo(http.MethodDelete, "/api/v1/share-links/"+itoa(l["id"].(float64)), nil, http.StatusNoContent)
	if r := anon.do(http.MethodGet, "/api/v1/public/shares/"+token+"/file", nil); r.code != http.StatusNotFound || code(r) != "share_expired" {
		t.Fatalf("revoked: %d %s", r.code, r.raw)
	}
	if links := alice.mustDo(http.MethodGet, path, nil, http.StatusOK); len(links.list) != 0 {
		t.Fatalf("after revoke: %s", links.raw)
	}

	// Trashing the document ends its links.
	l2 := alice.mustDo(http.MethodPost, path, map[string]any{"days": 1, "file": "original"}, http.StatusCreated).body
	alice.mustDo(http.MethodDelete, "/api/v1/documents/"+itoa(doc), nil, http.StatusNoContent)
	anon.mustDo(http.MethodGet, "/api/v1/public/shares/"+l2["token"].(string), nil, http.StatusNotFound)
}
