package api

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"net/http"
	"testing"

	"kept/server/internal/edition"
	"kept/server/internal/store"
)

// newEditionEnv is the real handler with the edition fixed (fixed != nil),
// or read from licenses signed with key.
func newEditionEnv(t *testing.T, fixed *edition.Edition, pub ed25519.PublicKey) (*testEnv, *edition.Service) {
	t.Helper()
	var svc *edition.Service
	env := newEnvDeps(t, "auto", nil, nil, func(d *Deps) {
		svc = edition.NewService(d.DB.(*store.Store).DB, edition.Options{Override: fixed, PublicKey: pub})
		if err := svc.Init(context.Background()); err != nil {
			t.Fatal(err)
		}
		d.Edition = svc
	})
	return env, svc
}

func ed(e edition.Edition) *edition.Edition { return &e }

func TestTheFreeEditionStopsPaidFeaturesButNotReading(t *testing.T) {
	env, _ := newEditionEnv(t, ed(edition.Free), nil)
	admin := env.setupAdmin()

	r := admin.do(http.MethodPost, "/api/v1/workflows", map[string]any{"name": "x"})
	if r.code != http.StatusForbidden || code(r) != "edition_required" {
		t.Fatalf("make a workflow in free: %d %s", r.code, r.raw)
	}
	if det, _ := r.body["details"].(map[string]any); det["edition"] != "pro" {
		t.Errorf("details = %v", r.body["details"])
	}
	if eeBuild { // the open-source build has no workflows at all (v1.13)
		admin.mustDo(http.MethodGet, "/api/v1/workflows", nil, http.StatusOK)
	}
	if r := admin.do(http.MethodPost, "/api/v1/qa/conversations", map[string]any{}); code(r) != "edition_required" {
		t.Errorf("ask AI in free: %d %s", r.code, r.raw)
	}
	if r := admin.do(http.MethodPatch, "/api/v1/admin/branding", map[string]any{"name": "我家"}); code(r) != "edition_required" {
		t.Errorf("branding in free: %d %s", r.code, r.raw)
	}
	// an external storage is paid; a directory on the server is not
	if r := admin.do(http.MethodPost, "/api/v1/admin/storages", map[string]any{"name": "oss", "kind": "s3", "selectable": true,
		"settings": map[string]any{"endpoint": "https://oss.example.com", "bucket": "b"}}); code(r) != "edition_required" {
		t.Errorf("S3 in free: %d %s", r.code, r.raw)
	}
	if r := admin.do(http.MethodPost, "/api/v1/admin/storages", map[string]any{"name": "disk2", "kind": "dir", "selectable": true,
		"settings": map[string]any{"path": t.TempDir()}}); code(r) == "edition_required" {
		t.Errorf("a directory in free: %d %s", r.code, r.raw)
	}
	// a share link is free; its watermark is paid (v1.13.3)
	doc := admin.waitJob(admin.upload("笔记.txt", "只有文字", nil).body["jobId"].(float64))["documentId"].(float64)
	if r := admin.do(http.MethodPost, "/api/v1/documents/"+itoa(doc)+"/share-links", map[string]any{"days": 3, "file": "original", "watermark": "仅供查看"}); code(r) != "edition_required" {
		t.Errorf("a watermarked link in free: %d %s", r.code, r.raw)
	}
	admin.mustDo(http.MethodPost, "/api/v1/documents/"+itoa(doc)+"/share-links", map[string]any{"days": 3, "file": "original", "watermark": "  "}, http.StatusCreated)

	r = admin.mustDo(http.MethodGet, "/api/v1/edition", nil, http.StatusOK)
	if r.body["edition"] != "free" || r.body["instance"] == nil {
		t.Errorf("edition = %v", r.body)
	}
	if feats, _ := r.body["features"].([]any); len(feats) != 0 {
		t.Errorf("free features %v", feats)
	}
	// someone who is not an administrator sees no instance ID
	member := env.user("member")
	if r := member.mustDo(http.MethodGet, "/api/v1/edition", nil, http.StatusOK); r.body["instance"] != nil {
		t.Errorf("instance shown to a member: %v", r.body)
	}
}

func TestTheFreeEditionsLimitsStopOneMore(t *testing.T) {
	env, _ := newEditionEnv(t, ed(edition.Free), nil)
	admin := env.setupAdmin()
	for i := 1; i <= 5; i++ {
		admin.mustDo(http.MethodPost, "/api/v1/admin/users", map[string]any{"email": fmt.Sprintf("p%d@example.com", i), "password": "password-123"}, http.StatusCreated)
	}
	r := admin.do(http.MethodPost, "/api/v1/admin/users", map[string]any{"email": "p6@example.com", "password": "password-123"})
	if r.code != http.StatusForbidden || code(r) != "edition_limit" {
		t.Fatalf("a seventh person in free: %d %s", r.code, r.raw)
	}
	// one shared workspace
	shared := 0
	for _, w := range admin.mustDo(http.MethodGet, "/api/v1/workspaces", nil, http.StatusOK).list {
		if w.(map[string]any)["kind"] != "personal" {
			shared++
		}
	}
	if shared == 0 {
		admin.mustDo(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "我家", "kind": "family"}, http.StatusCreated)
	}
	if r := admin.do(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": "爸妈家", "kind": "family"}); code(r) != "edition_limit" {
		t.Errorf("a second shared workspace in free: %d %s", r.code, r.raw)
	}
}

func TestALicenseInstalledOverTheAPI(t *testing.T) {
	pub, key, _ := edition.GenerateKey()
	env, svc := newEditionEnv(t, nil, pub)
	admin := env.setupAdmin()
	r := admin.mustDo(http.MethodGet, "/api/v1/edition", nil, http.StatusOK)
	if r.body["edition"] != "pro" || r.body["trial"] != true {
		t.Fatalf("a new instance: %v", r.body)
	}
	if r := admin.do(http.MethodPut, "/api/v1/admin/license", map[string]any{"license": "KEPT1.bad.bad"}); r.code != http.StatusBadRequest || code(r) != "license_invalid" {
		t.Errorf("a bad license: %d %s", r.code, r.raw)
	}
	other, _ := edition.Sign(edition.License{ID: "L-9", Instance: "elsewhere", Edition: "team", Holder: "某公司"}, key)
	if r := admin.do(http.MethodPut, "/api/v1/admin/license", map[string]any{"license": other}); code(r) != "license_instance" {
		t.Errorf("another instance's license: %d %s", r.code, r.raw)
	}
	text, _ := edition.Sign(edition.License{ID: "L-10", Instance: svc.Instance(), Edition: "team", Holder: "某公司", Issued: "2026-10-24", Expires: "2099-12-31"}, key)
	r = admin.mustDo(http.MethodPut, "/api/v1/admin/license", map[string]any{"license": text}, http.StatusOK)
	if r.body["edition"] != "team" || r.body["trial"] != false {
		t.Errorf("after installing: %v", r.body)
	}
	if lic, _ := r.body["license"].(map[string]any); lic["holder"] != "某公司" || lic["expires"] != "2099-12-31" {
		t.Errorf("license = %v", r.body["license"])
	}
	if eeBuild { // the open-source build has no branding to change (v1.13)
		admin.mustDo(http.MethodPatch, "/api/v1/admin/branding", map[string]any{"name": "某公司文档"}, http.StatusOK)
	}
	// a member may not install one
	member := env.user("member")
	if r := member.do(http.MethodPut, "/api/v1/admin/license", map[string]any{"license": text}); r.code != http.StatusForbidden {
		t.Errorf("a member installed a license: %d", r.code)
	}
	r = admin.mustDo(http.MethodDelete, "/api/v1/admin/license", nil, http.StatusOK)
	if r.body["edition"] != "pro" || r.body["license"] != nil { // back to the trial
		t.Errorf("after removing: %v", r.body)
	}
}

// A phone's bootstrap always says the edition and what it includes: App
// 4.10 shows only that, and takes a bootstrap without it for an old server.
func TestThePhoneIsAlwaysToldTheEdition(t *testing.T) {
	features := func(boot map[string]any) []any { f, _ := boot["features"].([]any); return f }

	free, _ := newEditionEnv(t, ed(edition.Free), nil)
	boot := pair(t, free.setupAdmin(), "iPhone").mustDo(http.MethodGet, "/api/v1/mobile/bootstrap", nil, http.StatusOK).body
	if boot["edition"] != "free" || boot["features"] == nil || len(features(boot)) != 0 {
		t.Errorf("free: edition %v, features %v", boot["edition"], boot["features"])
	}

	// without an edition service everything is allowed, and listed
	all := newEnv(t, "auto", nil)
	boot = pair(t, all.setupAdmin(), "iPhone").mustDo(http.MethodGet, "/api/v1/mobile/bootstrap", nil, http.StatusOK).body
	if boot["edition"] != "team" || len(features(boot)) != len(edition.Features(edition.Team)) {
		t.Errorf("no service: edition %v, features %v", boot["edition"], boot["features"])
	}
}
