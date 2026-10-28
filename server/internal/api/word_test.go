package api

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"kept/server/internal/doctest"
	"kept/server/internal/docxtest"
)

// v1.9.8: the Word reading view and its pictures.
func TestWordReadingView(t *testing.T) {
	e, clock := newEnvWithClock(t, "auto")
	alice := e.user("alice")
	bob := e.user("bob")
	docx := docxtest.Doc{
		Rels:  []docxtest.Rel{{ID: "rImg", Type: "image", Target: "media/a.png"}},
		Media: map[string][]byte{"a.png": []byte("\x89PNG\r\n\x1a\nfake")},
		Body:  docxtest.P("租房合同") + `<w:p>` + docxtest.Image("rImg", 200, 100) + `</w:p>`,
	}.Build()
	doc := alice.waitJob(alice.uploadBytes("合同.docx", docx, nil).body["jobId"].(float64))["documentId"].(float64)
	base := "/api/v1/documents/" + itoa(doc) + "/word"
	w := alice.mustDo(http.MethodGet, base, nil, http.StatusOK).body
	blocks := w["blocks"].([]any)
	if len(blocks) != 2 || blocks[0].(map[string]any)["runs"].([]any)[0].(map[string]any)["text"] != "租房合同" {
		t.Fatalf("blocks: %v", blocks)
	}
	if im := w["images"].([]any); len(im) != 1 || im[0].(map[string]any)["width"] != float64(200) {
		t.Errorf("images: %v", w["images"])
	}
	if w["notes"] == nil || w["baseSize"] == nil {
		t.Errorf("notes and baseSize are always there: %v", w)
	}
	img := alice.mustDo(http.MethodGet, base+"/images/1", nil, http.StatusOK)
	if img.header.Get("Content-Type") != "image/png" || img.header.Get("Content-Security-Policy") != "sandbox" || img.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("image headers: %v", img.header)
	}
	alice.mustDo(http.MethodGet, base+"/images/2", nil, http.StatusNotFound)
	bob.mustDo(http.MethodGet, base, nil, http.StatusNotFound)
	bob.mustDo(http.MethodGet, base+"/images/1", nil, http.StatusNotFound)

	// not a Word document
	txt := alice.waitJob(alice.upload("笔记.txt", "不是 Word", nil).body["jobId"].(float64))["documentId"].(float64)
	if r := alice.do(http.MethodGet, "/api/v1/documents/"+itoa(txt)+"/word", nil); r.code != http.StatusBadRequest || r.body["code"] != "not_word" {
		t.Errorf("text: %d %s", r.code, r.raw)
	}

	// a sensitive document needs the password confirmed (signing in counts for 15 minutes)
	alice.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(doc), map[string]any{"sensitive": true}, http.StatusOK)
	clock.Add(16 * time.Minute)
	for _, p := range []string{base, base + "/images/1"} {
		if r := alice.do(http.MethodGet, p, nil); r.code != http.StatusForbidden || code(r) != "reverify_required" {
			t.Errorf("sensitive, locked %s: %d %s", p, r.code, r.raw)
		}
	}
	alice.mustDo(http.MethodPost, "/api/v1/auth/verify", map[string]any{"password": "password-123"}, http.StatusOK)
	alice.mustDo(http.MethodGet, base, nil, http.StatusOK)
}

// v1.11.1: a .doc and an .odt have the reading view too.
func TestWordReadingViewOldAndOpenFormats(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	png := []byte("\x89PNG\r\n\x1a\nfake")
	doc := doctest.Doc{Blocks: []doctest.Block{
		{Para: &doctest.Para{Style: 1, Runs: []doctest.Run{{Text: "旧版手册"}}}},
		{Para: &doctest.Para{Runs: []doctest.Run{{Text: "总阀"}, {Picture: png, Width: 30, Height: 20}}}},
	}}.Build()
	odt := doctest.Odt{Body: `<text:h text:outline-level="1">开放格式</text:h><text:p>正文</text:p>`}.Build()
	for name, data := range map[string][]byte{"手册.doc": doc, "手册.odt": odt, "textutil.doc": doctest.HandbookDoc(), "textutil.odt": doctest.HandbookOdt()} {
		job := alice.waitJob(alice.uploadBytes(name, data, nil).body["jobId"].(float64))
		if job["documentId"] == nil {
			t.Fatalf("%s: %v", name, job)
		}
		id := job["documentId"].(float64)
		w := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(id)+"/word", nil, http.StatusOK).body
		if w["blocks"] == nil {
			t.Fatalf("%s: %v", name, w)
		}
		first := w["blocks"].([]any)[0].(map[string]any)
		text := first["runs"].([]any)[0].(map[string]any)["text"]
		switch name {
		case "手册.doc":
			if text != "旧版手册" || first["heading"] != float64(1) {
				t.Errorf("%s: %v", name, first)
			}
			img := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(id)+"/word/images/1", nil, http.StatusOK)
			if img.header.Get("Content-Type") != "image/png" || img.raw != string(png) {
				t.Errorf("%s picture: %v %q", name, img.header, img.raw)
			}
		case "手册.odt":
			if text != "开放格式" || first["heading"] != float64(1) {
				t.Errorf("%s: %v", name, first)
			}
		default:
			if text != "家庭应急手册" {
				t.Errorf("%s: %v", name, first)
			}
		}
	}
	// a .doc with an opening password
	locked := doctest.Doc{Blocks: []doctest.Block{{Para: &doctest.Para{Runs: []doctest.Run{{Text: "密"}}}}}}.Build()
	at := bytes.Index(locked, []byte{0xEC, 0xA5, 0xC1, 0x00})
	locked[at+11] |= 0x01 // fEncrypted, bit 8 of the flags at 10
	id := alice.waitJob(alice.uploadBytes("加密.doc", locked, nil).body["jobId"].(float64))["documentId"]
	if id == nil {
		return // refused at upload: nothing to view
	}
	if r := alice.do(http.MethodGet, "/api/v1/documents/"+itoa(id.(float64))+"/word", nil); r.code != http.StatusBadRequest || r.body["code"] != "word_encrypted" {
		t.Errorf("encrypted: %d %s", r.code, r.raw)
	}
}
