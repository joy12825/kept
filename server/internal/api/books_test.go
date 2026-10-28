package api

import (
	"bytes"
	"image"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"kept/server/internal/bookmeta"
)

func TestBooksAndLibraryAPI(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, _ := home(t, e)
	a := uploadedDocument(alice, "huozhe.txt")
	doc := "/api/v1/documents/" + itoa(a)

	carol.mustDo(http.MethodGet, doc+"/book", nil, http.StatusNotFound)
	carol.mustDo(http.MethodPut, doc+"/book", map[string]any{}, http.StatusForbidden)

	lib := bob.mustDo(http.MethodPost, "/api/v1/library", nil, http.StatusOK).body["drawer"].(map[string]any)
	if lib["kind"] != "library" || lib["name"] != "书房" || lib["icon"] != "book" {
		t.Fatalf("library = %v", lib)
	}
	if again := bob.mustDo(http.MethodPost, "/api/v1/library", nil, http.StatusOK).body["drawer"].(map[string]any); again["id"] != lib["id"] {
		t.Errorf("enabled twice: %v", again)
	}
	bob.mustDo(http.MethodDelete, "/api/v1/drawers/"+itoa(lib["id"].(float64)), nil, http.StatusConflict)
	bob.mustDo(http.MethodPatch, "/api/v1/drawers/"+itoa(lib["id"].(float64)), map[string]any{"clearRule": true}, http.StatusConflict)
	bob.mustDo(http.MethodPatch, "/api/v1/drawers/"+itoa(lib["id"].(float64)), map[string]any{"name": "藏书"}, http.StatusOK)

	bob.mustDo(http.MethodPut, doc+"/book", map[string]any{"isbn": "978-7-5063-6543-8"}, http.StatusBadRequest)
	b := bob.mustDo(http.MethodPut, doc+"/book", map[string]any{"isbn": "7-5063-6543-X", "authors": []string{"余华"}, "publisher": "作家出版社", "pages": 191}, http.StatusOK).body
	if b["isbn"] != "9787506365437" || b["publisher"] != "作家出版社" || b["pages"] != float64(191) {
		t.Fatalf("book = %v", b)
	}
	d := carol.mustDo(http.MethodGet, doc, nil, http.StatusOK).body
	if d["isBook"] != true || d["book"].(map[string]any)["authors"].([]any)[0] != "余华" || d["drawerId"] != lib["id"] {
		t.Errorf("document = isBook %v, book %v, drawer %v", d["isBook"], d["book"], d["drawerId"])
	}
	list := carol.mustDo(http.MethodGet, "/api/v1/documents?q=作家出版社", nil, http.StatusOK).body["items"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["isBook"] != true {
		t.Errorf("search by publisher: %v", list)
	}

	bob.mustDo(http.MethodDelete, doc+"/book", nil, http.StatusNoContent)
	bob.mustDo(http.MethodDelete, doc+"/book", nil, http.StatusNotFound)
	if d := carol.mustDo(http.MethodGet, doc, nil, http.StatusOK).body; d["isBook"] != false || d["book"] != nil || d["drawerId"] != nil {
		t.Errorf("after delete: %v %v %v", d["isBook"], d["book"], d["drawerId"])
	}

	bob.mustDo(http.MethodDelete, "/api/v1/library", nil, http.StatusOK)
	for _, dr := range carol.mustDo(http.MethodGet, "/api/v1/drawers", nil, http.StatusOK).body["items"].([]any) {
		if dr.(map[string]any)["kind"] == "library" {
			t.Errorf("library still there")
		}
	}
}

// fakeBookLookup answers for 活着 (9787506365437) through the custom
// source, with a 900×1200 PNG cover; Open Library is unreachable.
func fakeBookLookup(t *testing.T) *bookmeta.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/isbn/9787506365437":
			_, _ = w.Write([]byte(`{"title": "活着", "subtitle": "修订版", "authors": ["余华"], "publisher": "作家出版社", "pages": 191, "language": "zh",
				"cover": "http://` + r.Host + `/cover.png"}`))
		case "/cover.png":
			img := image.NewRGBA(image.Rect(0, 0, 900, 1200))
			_ = png.Encode(w, img)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return bookmeta.New(bookmeta.Options{Sources: []string{"custom", "openlibrary"}, CustomURL: srv.URL + "/isbn/{isbn}", OpenLibrary: "http://127.0.0.1:1", Timeout: 2 * time.Second})
}

func TestBookLookupAndCover(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, _ := home(t, e)
	if me := bob.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body; me["features"].(map[string]any)["bookLookup"] != true {
		t.Errorf("features = %v", me["features"])
	}
	carol.mustDo(http.MethodGet, "/api/v1/books/lookup?isbn=9787506365437", nil, http.StatusForbidden)
	bob.mustDo(http.MethodGet, "/api/v1/books/lookup?isbn=123", nil, http.StatusBadRequest)
	r := bob.mustDo(http.MethodGet, "/api/v1/books/lookup?isbn=7-5063-6543-X", nil, http.StatusOK).body
	res := r["results"].([]any)
	fails := r["failures"].([]any)
	if r["isbn"] != "9787506365437" || len(res) != 1 || len(fails) != 1 {
		t.Fatalf("lookup = %v", r)
	}
	if got := res[0].(map[string]any); got["title"] != "活着：修订版" || got["language"] != "中文" || got["hasCover"] != true || got["sourceName"] != "自定义书目接口" {
		t.Errorf("result = %v", got)
	}
	if f := fails[0].(map[string]any); f["source"] != "openlibrary" || !strings.Contains(f["message"].(string), "连不上") {
		t.Errorf("failure = %v", f)
	}
	if r := bob.mustDo(http.MethodGet, "/api/v1/books/lookup?isbn=9780306406157", nil, http.StatusOK).body; len(r["results"].([]any)) != 0 {
		t.Errorf("unknown book = %v", r)
	}

	a := uploadedDocument(alice, "huozhe.txt")
	doc := "/api/v1/documents/" + itoa(a)
	fetch := map[string]any{"isbn": "9787506365437", "source": "custom"}
	bob.mustDo(http.MethodPost, doc+"/book/cover/fetch", fetch, http.StatusNotFound) // not a book yet
	bob.mustDo(http.MethodPut, doc+"/book", map[string]any{"isbn": "9787506365437"}, http.StatusOK)
	bob.mustDo(http.MethodPost, doc+"/book/cover/fetch", map[string]any{"isbn": "9787506365437", "source": "openlibrary"}, http.StatusNotFound)
	if b := bob.mustDo(http.MethodPost, doc+"/book/cover/fetch", fetch, http.StatusOK).body; b["hasCover"] != true {
		t.Fatalf("book = %v", b)
	}
	cover := carol.mustDo(http.MethodGet, doc+"/cover", nil, http.StatusOK)
	img, format, err := image.Decode(strings.NewReader(cover.raw))
	if err != nil || format != "jpeg" || img.Bounds().Dx() != 600 || img.Bounds().Dy() != 800 {
		t.Fatalf("cover: %v %s %v", err, format, img.Bounds())
	}
	list := carol.mustDo(http.MethodGet, "/api/v1/documents", nil, http.StatusOK).body["items"].([]any)
	if list[0].(map[string]any)["hasCover"] != true {
		t.Errorf("list item = %v", list[0])
	}

	// Uploading: PNG or JPEG only.
	if r := bob.doRaw(http.MethodPut, doc+"/book/cover", "image/gif", []byte("GIF89a....")); r.code != http.StatusBadRequest {
		t.Errorf("gif: %d %s", r.code, r.raw)
	}
	var small bytes.Buffer
	_ = png.Encode(&small, image.NewRGBA(image.Rect(0, 0, 200, 300)))
	if r := bob.doRaw(http.MethodPut, doc+"/book/cover", "image/png", small.Bytes()); r.code != http.StatusOK {
		t.Errorf("png: %d %s", r.code, r.raw)
	}
	img, _, _ = image.Decode(strings.NewReader(carol.mustDo(http.MethodGet, doc+"/cover", nil, http.StatusOK).raw))
	if img.Bounds().Dx() != 200 {
		t.Errorf("small covers keep their size: %v", img.Bounds())
	}
	bob.mustDo(http.MethodDelete, doc+"/book/cover", nil, http.StatusOK)
	bob.mustDo(http.MethodDelete, doc+"/book/cover", nil, http.StatusNotFound)
	carol.mustDo(http.MethodGet, doc+"/cover", nil, http.StatusNotFound)
}

func TestReadingProgressAPI(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, homeID := home(t, e)
	a := uploadedDocument(alice, "book.txt")
	path := "/api/v1/documents/" + itoa(a) + "/reading"
	if r := carol.mustDo(http.MethodGet, path, nil, http.StatusOK).body; r["position"] != "" || r["percent"] != float64(0) || r["updatedAt"] != nil {
		t.Errorf("never read = %v", r)
	}
	// A viewer keeps their own place; each person has their own.
	carol.mustDo(http.MethodPut, path, map[string]any{"position": "page:12", "percent": 0.25}, http.StatusOK)
	bob.mustDo(http.MethodPut, path, map[string]any{"position": "epubcfi(/6/4!/4/2/1:0)", "percent": 0.5}, http.StatusOK)
	if r := carol.mustDo(http.MethodGet, path, nil, http.StatusOK).body; r["position"] != "page:12" || r["percent"] != 0.25 || r["updatedAt"] == nil {
		t.Errorf("carol = %v", r)
	}
	if r := bob.mustDo(http.MethodGet, path, nil, http.StatusOK).body; r["position"] != "epubcfi(/6/4!/4/2/1:0)" {
		t.Errorf("bob = %v", r)
	}
	bob.mustDo(http.MethodPut, path, map[string]any{"position": "page:1", "percent": 1.5}, http.StatusBadRequest)
	bob.mustDo(http.MethodPut, path, map[string]any{"position": strings.Repeat("x", 2001), "percent": 0.1}, http.StatusBadRequest)
	outsider := e.user("dave").inSpace(homeID)
	outsider.mustDo(http.MethodGet, path, nil, http.StatusNotFound)
	alice.mustDo(http.MethodDelete, "/api/v1/documents/"+itoa(a), nil, http.StatusNoContent)
	carol.mustDo(http.MethodGet, path, nil, http.StatusNotFound)
}

func TestReadingStateAndHighlightsAPI(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, _ := home(t, e)
	a := uploadedDocument(alice, "a.txt")
	b := uploadedDocument(alice, "b.txt")
	doc := "/api/v1/documents/" + itoa(a)

	// A viewer's own status, rating and dates.
	r := carol.mustDo(http.MethodPatch, doc+"/reading", map[string]any{"status": "finished", "rating": 4}, http.StatusOK).body
	if r["status"] != "finished" || r["rating"] != float64(4) || r["finishedOn"] == "" || r["startedOn"] == "" {
		t.Fatalf("state = %v", r)
	}
	carol.mustDo(http.MethodPatch, doc+"/reading", map[string]any{"status": "done"}, http.StatusBadRequest)
	if r := bob.mustDo(http.MethodGet, doc+"/reading", nil, http.StatusOK).body; r["status"] != "" {
		t.Errorf("bob sees carol's status: %v", r)
	}
	bob.mustDo(http.MethodPut, "/api/v1/documents/"+itoa(b)+"/reading", map[string]any{"position": "page:2", "percent": 0.5}, http.StatusOK)

	// The list carries the caller's own reading, and filters by it.
	items := func(c *client, filter string) map[float64]map[string]any {
		out := map[float64]map[string]any{}
		for _, it := range c.mustDo(http.MethodGet, "/api/v1/documents?filter="+url.QueryEscape(filter), nil, http.StatusOK).body["items"].([]any) {
			m := it.(map[string]any)
			out[m["id"].(float64)] = m
		}
		return out
	}
	if got := items(carol, ""); got[a]["reading"].(map[string]any)["status"] != "finished" || got[b]["reading"] != nil {
		t.Errorf("carol's list = %v", got)
	}
	if got := items(bob, `{"field":"reading","op":"in","value":["reading"]}`); len(got) != 1 || got[b] == nil || got[b]["reading"].(map[string]any)["percent"] != 0.5 {
		t.Errorf("bob reading = %v", got)
	}
	if got := items(bob, `{"field":"reading","op":"in","value":["none"]}`); len(got) != 1 || got[a] == nil {
		t.Errorf("bob unmarked = %v", got)
	}
	// Not in shared rules.
	if rr := bob.do(http.MethodPost, "/api/v1/drawers", map[string]any{"name": "在读", "rule": map[string]any{"field": "reading", "op": "in", "value": []string{"reading"}}}); rr.code != http.StatusBadRequest || !strings.Contains(rr.raw, "个人数据") {
		t.Errorf("drawer rule: %d %s", rr.code, rr.raw)
	}

	// Highlights: private, searchable.
	h := carol.mustDo(http.MethodPost, doc+"/highlights", map[string]any{"locator": "epubcfi(/6/4!/4/4,/1:0,/1:5)", "quote": "国破山河在", "color": "pink"}, http.StatusCreated).body
	carol.mustDo(http.MethodPost, doc+"/highlights", map[string]any{"locator": "page:3"}, http.StatusBadRequest)
	carol.mustDo(http.MethodPost, doc+"/highlights", map[string]any{"locator": "page:3", "note": "这一页"}, http.StatusCreated)
	hl := "/api/v1/highlights/" + itoa(h["id"].(float64))
	carol.mustDo(http.MethodPatch, hl, map[string]any{"note": "感时花溅泪"}, http.StatusOK)
	bob.mustDo(http.MethodPatch, hl, map[string]any{"note": "x"}, http.StatusNotFound)
	if list := bob.mustDo(http.MethodGet, doc+"/highlights", nil, http.StatusOK).list; len(list) != 0 {
		t.Errorf("bob sees carol's highlights: %v", list)
	}
	page := carol.mustDo(http.MethodGet, "/api/v1/highlights?q="+url.QueryEscape("花溅"), nil, http.StatusOK).body["items"].([]any)
	if len(page) != 1 || page[0].(map[string]any)["documentTitle"] != "a" || page[0].(map[string]any)["color"] != "pink" {
		t.Errorf("search = %v", page)
	}
	if r := carol.mustDo(http.MethodGet, doc+"/reading", nil, http.StatusOK).body; r["highlightCount"] != float64(2) {
		t.Errorf("count = %v", r)
	}
	carol.mustDo(http.MethodDelete, hl, nil, http.StatusNoContent)
	carol.mustDo(http.MethodDelete, hl, nil, http.StatusNotFound)
}

func TestBookListsAndFinishedBooksAPI(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, carol, _ := home(t, e)
	a := uploadedDocument(alice, "a.txt")
	b := uploadedDocument(alice, "b.txt")
	for _, d := range []float64{a, b} {
		bob.mustDo(http.MethodPut, "/api/v1/documents/"+itoa(d)+"/book", map[string]any{"authors": []string{"佚名"}}, http.StatusOK)
	}
	carol.mustDo(http.MethodPost, "/api/v1/book-lists", map[string]any{"name": "暑假"}, http.StatusForbidden)
	l := bob.mustDo(http.MethodPost, "/api/v1/book-lists", map[string]any{"name": "暑假", "description": "孩子读"}, http.StatusCreated).body
	path := "/api/v1/book-lists/" + itoa(l["id"].(float64))
	bob.mustDo(http.MethodPost, path+"/items", map[string]any{"documentId": a, "note": "先读"}, http.StatusNoContent)
	bob.mustDo(http.MethodPost, path+"/items", map[string]any{"documentId": b}, http.StatusNoContent)
	bob.mustDo(http.MethodPost, path+"/items", map[string]any{"documentId": b}, http.StatusConflict)
	bob.mustDo(http.MethodPut, path+"/order", map[string]any{"ids": []float64{b, a}}, http.StatusNoContent)
	bob.mustDo(http.MethodPatch, path+"/items/"+itoa(b), map[string]any{"note": "图多"}, http.StatusNoContent)
	carol.mustDo(http.MethodPut, "/api/v1/documents/"+itoa(a)+"/reading", map[string]any{"position": "page:1", "percent": 0.2}, http.StatusOK)

	d := carol.mustDo(http.MethodGet, path, nil, http.StatusOK).body
	items := d["items"].([]any)
	if d["count"] != float64(2) || len(items) != 2 || items[0].(map[string]any)["note"] != "图多" || items[1].(map[string]any)["reading"].(map[string]any)["status"] != "reading" {
		t.Fatalf("list = %v", d)
	}
	if ids := carol.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(a)+"/book-lists", nil, http.StatusOK).list; len(ids) != 1 {
		t.Errorf("lists of a = %v", ids)
	}
	if lists := carol.mustDo(http.MethodGet, "/api/v1/book-lists", nil, http.StatusOK).list; len(lists) != 1 || len(lists[0].(map[string]any)["preview"].([]any)) != 2 {
		t.Errorf("lists = %v", lists)
	}
	bob.mustDo(http.MethodDelete, path+"/items/"+itoa(a), nil, http.StatusNoContent)
	bob.mustDo(http.MethodPatch, path, map[string]any{"name": "寒假"}, http.StatusOK)
	bob.mustDo(http.MethodDelete, path, nil, http.StatusNoContent)
	bob.mustDo(http.MethodGet, path, nil, http.StatusNotFound)

	// The yearbook lists the books the caller finished that year, and only theirs.
	carol.mustDo(http.MethodPatch, "/api/v1/documents/"+itoa(b)+"/reading", map[string]any{"status": "finished", "finishedOn": "2023-07-01", "rating": 5}, http.StatusOK)
	y := carol.mustDo(http.MethodGet, "/api/v1/yearbook/2023", nil, http.StatusOK).body
	books := y["books"].([]any)
	if y["booksFinished"] != float64(1) || len(books) != 1 || books[0].(map[string]any)["rating"] != float64(5) || books[0].(map[string]any)["finishedOn"] != "2023-07-01" {
		t.Errorf("carol's 2023 = %v / %v", y["booksFinished"], books)
	}
	if y := bob.mustDo(http.MethodGet, "/api/v1/yearbook/2023", nil, http.StatusOK).body; y["booksFinished"] != float64(0) {
		t.Errorf("bob's 2023 = %v", y["books"])
	}
}
