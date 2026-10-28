package bookmeta

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const isbn = "9787506365437"

func servers(t *testing.T) (ol, gb, cu *httptest.Server, hits *atomic.Int32) {
	hits = &atomic.Int32{}
	ol = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/api/books" || r.URL.Query().Get("bibkeys") != "ISBN:"+isbn || r.URL.Query().Get("jscmd") != "data" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"ISBN:` + isbn + `": {"title": "活着", "authors": [{"name": "余华"}], "number_of_pages": 191,
			"publishers": [{"name": "作家出版社"}], "publish_date": "August 1, 2012", "notes": {"type": "/type/text", "value": "福贵的一生"},
			"cover": {"medium": "` + "http://" + r.Host + `/m.jpg", "large": "` + "http://" + r.Host + `/l.jpg"}}}`))
	}))
	gb = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Query().Get("q") != "isbn:"+isbn {
			_, _ = w.Write([]byte(`{"totalItems": 0}`))
			return
		}
		if r.URL.Query().Get("key") != "k1" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"totalItems": 1, "items": [{"volumeInfo": {"title": "活着", "authors": ["余华"], "publisher": "作家出版社",
			"publishedDate": "2012-08-01", "pageCount": 191, "language": "zh", "description": "长篇小说",
			"imageLinks": {"thumbnail": "http://books.google.com/books/content?id=x&printsec=frontcover&img=1&zoom=1&edge=curl&source=gbs_api"}}}]}`))
	}))
	cu = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/isbn/"+isbn {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"title": " 活着 ", "authors": ["余华", " "], "series": "余华作品", "language": "chi", "pages": 191, "cover": "ftp://x"}`))
	}))
	t.Cleanup(func() { ol.Close(); gb.Close(); cu.Close() })
	return
}

func TestLookup(t *testing.T) {
	ol, gb, cu, hits := servers(t)
	c := New(Options{Sources: []string{"custom", "openlibrary", "googlebooks"}, CustomURL: cu.URL + "/isbn/{isbn}", GoogleKey: "k1",
		OpenLibrary: ol.URL, GoogleBooks: gb.URL})
	rs := c.Lookup(context.Background(), isbn)
	if len(rs) != 3 || rs[0].Source != "custom" || rs[1].Source != "openlibrary" || rs[2].Source != "googlebooks" {
		t.Fatalf("results = %+v", rs)
	}
	for _, r := range rs {
		if r.Err != nil || r.Meta == nil || r.Meta.Title != "活着" || len(r.Meta.Authors) != 1 || r.Meta.Authors[0] != "余华" || r.Meta.Pages != 191 {
			t.Errorf("%s: %+v %v", r.Source, r.Meta, r.Err)
		}
	}
	if m := rs[0].Meta; m.Series != "余华作品" || m.Language != "中文" || m.CoverURL != "" {
		t.Errorf("custom = %+v", m)
	}
	if m := rs[1].Meta; m.Publisher != "作家出版社" || m.Published != "2012-08" || m.Description != "福贵的一生" || !strings.HasSuffix(m.CoverURL, "/l.jpg") {
		t.Errorf("openlibrary = %+v", m)
	}
	if m := rs[2].Meta; m.Language != "中文" || m.Published != "2012-08" || !strings.HasPrefix(m.CoverURL, "https://") || strings.Contains(m.CoverURL, "edge=curl") {
		t.Errorf("googlebooks = %+v", m)
	}
	n := hits.Load()
	c.Lookup(context.Background(), isbn)
	if hits.Load() != n {
		t.Errorf("not cached")
	}

	// Unknown books, a failing source and an unreachable one.
	c = New(Options{Sources: []string{"openlibrary", "googlebooks", "custom"}, OpenLibrary: ol.URL, GoogleBooks: gb.URL, CustomURL: "http://127.0.0.1:1/{isbn}"})
	rs = c.Lookup(context.Background(), isbn)
	if !errors.Is(c.Lookup(context.Background(), "9780306406157")[0].Err, ErrNotFound) {
		t.Errorf("unknown book")
	}
	if rs[1].Err == nil || !strings.Contains(rs[1].Err.Error(), "429") || rs[2].Err == nil || errors.Is(rs[2].Err, ErrNotFound) {
		t.Errorf("failures = %v / %v", rs[1].Err, rs[2].Err)
	}
	n = hits.Load()
	c.Lookup(context.Background(), isbn)
	if hits.Load() == n {
		t.Errorf("failures should not be cached")
	}
	if New(Options{}).Enabled() || New(Options{Sources: []string{"custom"}}).Enabled() {
		t.Errorf("no sources should mean disabled")
	}
}

func TestCover(t *testing.T) {
	ol, _, _, _ := servers(t)
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 100))) }))
	defer img.Close()
	c := New(Options{Sources: []string{"openlibrary"}, OpenLibrary: ol.URL, Timeout: time.Second})
	c.Lookup(context.Background(), isbn)
	// Point the cached cover at the image server.
	c.cache[isbn].results[0].Meta.CoverURL = img.URL + "/l.jpg"
	b, err := c.Cover(context.Background(), isbn, "openlibrary", 1000)
	if err != nil || len(b) != 100 {
		t.Fatalf("cover = %d bytes, %v", len(b), err)
	}
	if b, _ := c.Cover(context.Background(), isbn, "openlibrary", 10); len(b) != 11 {
		t.Errorf("limit: %d bytes", len(b))
	}
	if _, err := c.Cover(context.Background(), isbn, "googlebooks", 1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("other source: %v", err)
	}
}

func TestPublished(t *testing.T) {
	for in, want := range map[string]string{"October 1, 1988": "1988-10", "1988": "1988", "1988-10-01": "1988-10", "2019-05": "2019-05", "c1999": "1999", "Spring 1999 edition, revised": "Spring 1999 edition, revised", "": ""} {
		if got := published(in); got != want {
			t.Errorf("%q = %q, want %q", in, got, want)
		}
	}
}
