package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// v1.6.4: sensitive documents need a recently confirmed password; nothing
// that leaves the instance carries them.
func TestSensitiveDocuments(t *testing.T) {
	e, clock := newEnvWithClock(t, "auto")
	alice := e.user("alice")
	up := func(name, text string) float64 {
		r := alice.upload(name, text, nil)
		return alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	}
	card := up("身份证.txt", "居民身份证 公民身份号码 110101199001011234")
	bill := up("电费.txt", "国家电网 电费 128 元")
	doc := "/api/v1/documents/" + itoa(card)

	// a link made before the document was marked stops working
	old := alice.mustDo(http.MethodPost, doc+"/share-links", map[string]any{"days": 7, "file": "original"}, http.StatusCreated).body["token"].(string)
	anon := e.anon()
	anon.mustDo(http.MethodGet, "/api/v1/public/shares/"+old, nil, http.StatusOK)

	alice.mustDo(http.MethodPatch, doc, map[string]any{"sensitive": true}, http.StatusOK)
	if r := anon.do(http.MethodGet, "/api/v1/public/shares/"+old, nil); r.code == http.StatusOK {
		t.Errorf("an old link still opens: %s", r.raw)
	}
	// signing in counts as confirming: within 15 minutes everything opens
	d := alice.mustDo(http.MethodGet, doc, nil, http.StatusOK).body
	if d["sensitive"] != true || d["sensitiveMarked"] != true || d["locked"] != nil || !strings.Contains(d["content"].(string), "身份证") {
		t.Fatalf("fresh session: %v", d)
	}
	alice.mustDo(http.MethodGet, doc+"/original", nil, http.StatusOK)
	if me := alice.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body; me["verifiedUntil"] == nil {
		t.Errorf("me lacks verifiedUntil: %v", me)
	}

	// what leaves the instance refuses it, confirmed or not
	leaving := []resp{
		alice.do(http.MethodPost, doc+"/share-links", map[string]any{"days": 7, "file": "original"}),
		alice.do(http.MethodPost, "/api/v1/documents/email", map[string]any{"ids": []any{card}, "to": []any{"x@example.com"}}),
	}
	if eeBuild { // share bundles are paid (v1.13.2)
		leaving = append(leaving, alice.do(http.MethodPost, "/api/v1/share-bundles", map[string]any{"ids": []any{bill, card}, "days": 7, "file": "original"}))
	}
	for _, r := range leaving {
		if r.code != http.StatusBadRequest || code(r) != "sensitive" {
			t.Errorf("sharing a sensitive document: %d %s", r.code, r.raw)
		}
	}

	// after 15 minutes: titles only until the password is confirmed again
	clock.Add(16 * time.Minute)
	d = alice.mustDo(http.MethodGet, doc, nil, http.StatusOK).body
	if d["locked"] != true || d["content"] != "" || d["title"] != "身份证" {
		t.Errorf("locked document: %v", d)
	}
	for _, path := range []string{doc + "/original", doc + "/preview", doc + "/thumbnail", "/api/v1/documents/download?ids=" + itoa(bill) + "," + itoa(card)} {
		if r := alice.do(http.MethodGet, path, nil); r.code != http.StatusForbidden || code(r) != "reverify_required" {
			if !(strings.HasSuffix(path, "/thumbnail") && r.code == http.StatusNotFound) { // a text file has no thumbnail
				t.Errorf("%s: %d %s", path, r.code, r.raw)
			}
		}
	}
	alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(bill)+"/original", nil, http.StatusOK) // others open as ever
	if r := alice.do(http.MethodGet, "/api/v1/export", nil); r.code != http.StatusForbidden {
		t.Errorf("export while locked: %d", r.code)
	}
	if me := alice.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body; me["verifiedUntil"] != nil {
		t.Errorf("me still verified: %v", me["verifiedUntil"])
	}
	// search finds it by title, without a snippet of its text
	e.waitIndexed()
	hits := alice.mustDo(http.MethodGet, "/api/v1/documents?filter="+url.QueryEscape(`{"text":"身份证"}`), nil, http.StatusOK).body["items"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["highlight"] != nil || hits[0].(map[string]any)["sensitive"] != true {
		t.Errorf("search: %v", hits)
	}
	if n := alice.mustDo(http.MethodGet, "/api/v1/documents?filter="+url.QueryEscape(`{"field":"sensitive","op":"eq","value":true}`), nil, http.StatusOK).body["total"]; n != float64(1) {
		t.Errorf("sensitive filter: %v", n)
	}

	// confirming: a wrong password is refused, the right one unlocks
	if r := alice.do(http.MethodPost, "/api/v1/auth/verify", map[string]any{"password": "wrong"}); r.code != http.StatusBadRequest {
		t.Errorf("wrong password: %d %s", r.code, r.raw)
	}
	alice.mustDo(http.MethodPost, "/api/v1/auth/verify", map[string]any{}, http.StatusBadRequest)
	until := alice.mustDo(http.MethodPost, "/api/v1/auth/verify", map[string]any{"password": "password-123"}, http.StatusOK).body["verifiedUntil"]
	if until == nil {
		t.Fatal("no verifiedUntil")
	}
	alice.mustDo(http.MethodGet, doc+"/original", nil, http.StatusOK)

	// a sensitive drawer makes its documents sensitive; unmarking the
	// document leaves it sensitive while it is in the drawer
	drw := alice.mustDo(http.MethodPost, "/api/v1/drawers", map[string]any{"name": "证件"}, http.StatusCreated).body["drawer"].(map[string]any)["id"].(float64)
	alice.mustDo(http.MethodPatch, "/api/v1/drawers/"+itoa(drw), map[string]any{"sensitive": true}, http.StatusOK)
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(bill), map[string]any{"drawerId": drw}, http.StatusOK)
	b := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(bill), nil, http.StatusOK).body
	if b["sensitive"] != true || b["sensitiveMarked"] != nil {
		t.Errorf("in a sensitive drawer: %v %v", b["sensitive"], b["sensitiveMarked"])
	}
	alice.mustDo(http.MethodPost, "/api/v1/documents/"+itoa(bill)+"/share-links", map[string]any{"days": 7, "file": "original"}, http.StatusBadRequest)
	alice.mustDo(http.MethodPatch, "/api/v1/drawers/"+itoa(drw), map[string]any{"sensitive": false}, http.StatusOK)
	alice.mustDo(http.MethodPost, "/api/v1/documents/"+itoa(bill)+"/share-links", map[string]any{"days": 7, "file": "original"}, http.StatusCreated)

	// the AI never gets it
	if r := alice.do(http.MethodPost, doc+"/ask", map[string]any{"question": "号码是多少"}); r.code == http.StatusOK && !strings.Contains(r.raw, "敏感") {
		t.Errorf("asking about a sensitive document: %d %s", r.code, r.raw)
	}
	hist := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(card)+"/history", nil, http.StatusOK).raw
	if !strings.Contains(hist, "敏感") {
		t.Errorf("history: %s", hist)
	}
}

// Wrong passwords when confirming are limited per account, on a counter of
// their own: the account can still sign in.
func TestVerifySessionIsRateLimited(t *testing.T) {
	e := newEnv(t, "auto", nil)
	bob := e.user("bob")
	last := 0
	for i := 0; i < 6; i++ {
		last = bob.do(http.MethodPost, "/api/v1/auth/verify", map[string]any{"password": "nope"}).code
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("after 6 wrong passwords: %d", last)
	}
	bob.mustDo(http.MethodPost, "/api/v1/auth/verify", map[string]any{"password": "password-123"}, http.StatusTooManyRequests)
	e.anon().mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": "bob", "password": "password-123"}, http.StatusOK)
}

// v1.7.20: a paired phone with full access confirms the password (or a
// two-factor code) itself and then opens sensitive documents for 15
// minutes; phones that only upload, and API tokens, cannot.
func TestPhoneConfirmsForSensitiveDocuments(t *testing.T) {
	e, clock := newEnvWithClock(t, "auto")
	alice := e.user("alice")
	card := alice.waitJob(alice.upload("身份证.txt", "居民身份证 公民身份号码 110101199001011234", nil).body["jobId"].(float64))["documentId"].(float64)
	doc := "/api/v1/documents/" + itoa(card)
	alice.mustDo(http.MethodPatch, doc, map[string]any{"sensitive": true}, http.StatusOK)

	phone := pairFull(t, alice, "小王的 iPhone")
	if r := phone.do(http.MethodGet, doc+"/original", nil); r.code != http.StatusForbidden || code(r) != "reverify_required" {
		t.Fatalf("before confirming: %d %s", r.code, r.raw)
	}
	if d := phone.mustDo(http.MethodGet, doc, nil, http.StatusOK).body; d["locked"] != true || d["sensitive"] != true {
		t.Fatalf("locked on the phone: %v", d)
	}
	if r := phone.do(http.MethodPost, "/api/v1/auth/verify", map[string]any{"password": "wrong"}); r.code != http.StatusBadRequest {
		t.Fatalf("wrong password: %d %s", r.code, r.raw)
	}
	if until := phone.mustDo(http.MethodPost, "/api/v1/auth/verify", map[string]any{"password": "password-123"}, http.StatusOK).body["verifiedUntil"]; until == nil {
		t.Fatal("no verifiedUntil")
	}
	if got := phone.mustDo(http.MethodGet, doc+"/original", nil, http.StatusOK).raw; !strings.Contains(got, "身份证") {
		t.Fatalf("original: %q", got)
	}
	if d := phone.mustDo(http.MethodGet, doc, nil, http.StatusOK).body; d["locked"] != nil {
		t.Fatalf("still locked: %v", d)
	}
	// each phone confirms for itself: another one is still locked
	other := pairFull(t, alice, "iPad")
	if r := other.do(http.MethodGet, doc+"/original", nil); r.code != http.StatusForbidden {
		t.Fatalf("another phone: %d", r.code)
	}
	// 15 minutes, as on the web
	clock.Add(16 * time.Minute)
	if r := phone.do(http.MethodGet, doc+"/original", nil); r.code != http.StatusForbidden || code(r) != "reverify_required" {
		t.Fatalf("after 15 minutes: %d %s", r.code, r.raw)
	}
	// an upload-only phone (App 1.x) cannot confirm
	if r := pair(t, alice, "旧手机").do(http.MethodPost, "/api/v1/auth/verify", map[string]any{"password": "password-123"}); r.code != http.StatusForbidden {
		t.Fatalf("upload-only phone: %d %s", r.code, r.raw)
	}
	// nor can an API token
	script := e.anon()
	script.bearer = alice.mustDo(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "脚本"}, http.StatusCreated).body["token"].(string)
	if r := script.do(http.MethodPost, "/api/v1/auth/verify", map[string]any{"password": "password-123"}); r.code != http.StatusForbidden {
		t.Fatalf("API token: %d %s", r.code, r.raw)
	}
}
