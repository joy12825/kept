package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"kept/server/internal/ofdtest"
)

// v1.11.3: the signatures of an OFD, checked when asked for.
func TestOFDSignatures(t *testing.T) {
	e, clock := newEnvWithClock(t, "auto")
	alice := e.user("alice")
	bob := e.user("bob")
	signer, err := ofdtest.NewSigner("王小明", "上海某某餐饮有限公司", "上海某某餐饮有限公司发票专用章")
	if err != nil {
		t.Fatal(err)
	}
	inv := ofdtest.Invoice{Number: "2431200001", Date: "2024年03月05日", Seller: "上海某某餐饮有限公司", Buyer: "王小明", Item: "餐费", Amount: "286.00", Sign: signer}
	good := alice.waitJob(alice.uploadBytes("发票.ofd", inv.Build(), nil).body["jobId"].(float64))["documentId"].(float64)
	tampered := *signer
	tampered.Tamper = "amount"
	inv.Sign, inv.Number = &tampered, "2431200002"
	bad := alice.waitJob(alice.uploadBytes("改过的发票.ofd", inv.Build(), nil).body["jobId"].(float64))["documentId"].(float64)

	path := func(id float64) string { return "/api/v1/documents/" + itoa(id) + "/signatures" }
	items := alice.mustDo(http.MethodGet, path(good), nil, http.StatusOK).body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("signatures: %v", items)
	}
	s := items[0].(map[string]any)
	if s["status"] != "valid" || s["sealName"] != "上海某某餐饮有限公司发票专用章" || s["signer"] != "王小明" || s["hasSeal"] != true ||
		s["certInSeal"] != true || s["page"] != float64(1) || s["reason"] != nil || len(s["files"].([]any)) != 6 {
		t.Fatalf("valid: %v", s)
	}
	seal := alice.mustDo(http.MethodGet, path(good)+"/1/seal", nil, http.StatusOK)
	if seal.header.Get("Content-Type") != "image/png" || seal.header.Get("Content-Security-Policy") != "sandbox" || !strings.HasPrefix(seal.raw, "\x89PNG") {
		t.Errorf("seal: %v", seal.header)
	}
	alice.mustDo(http.MethodGet, path(good)+"/2/seal", nil, http.StatusNotFound)

	alice.headers["Accept-Language"] = "en"
	s = alice.mustDo(http.MethodGet, path(bad), nil, http.StatusOK).body["items"].([]any)[0].(map[string]any)
	if s["status"] != "changed" || s["reason"] != "A signed file was changed after signing" {
		t.Errorf("changed: %v", s)
	}
	delete(alice.headers, "Accept-Language")

	// not an OFD; another's document
	txt := alice.waitJob(alice.upload("便条.txt", "买菜", nil).body["jobId"].(float64))["documentId"].(float64)
	if r := alice.do(http.MethodGet, path(txt), nil); r.code != http.StatusBadRequest || code(r) != "not_ofd" {
		t.Errorf("text: %d %s", r.code, r.raw)
	}
	bob.mustDo(http.MethodGet, path(good), nil, http.StatusNotFound)
	bob.mustDo(http.MethodGet, path(good)+"/1/seal", nil, http.StatusNotFound)

	// a sensitive document needs the password confirmed
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(good), map[string]any{"sensitive": true}, http.StatusOK)
	clock.Add(16 * time.Minute)
	if r := alice.do(http.MethodGet, path(good), nil); r.code != http.StatusForbidden || code(r) != "reverify_required" {
		t.Errorf("sensitive: %d %s", r.code, r.raw)
	}
}
