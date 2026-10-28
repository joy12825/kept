package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGzipJSONCompressesOnlyJSON(t *testing.T) {
	big := strings.Repeat("收好的识别文字。", 2000)
	h := gzipJSON(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"content":"`+big+`"}`)
		case "/pdf":
			w.Header().Set("Content-Type", "application/pdf")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = io.WriteString(w, "%PDF-1.7")
		case "/events":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: x\n\n")
			http.NewResponseController(w).Flush()
		}
	}))
	get := func(path, enc string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if enc != "" {
			req.Header.Set("Accept-Encoding", enc)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get("/json", "gzip, deflate, br")
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("json not compressed: %v", rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(zr)
	if !strings.Contains(string(body), big) || rec.Body.Len() > len(body)/4 {
		t.Errorf("round trip: %d compressed of %d", rec.Body.Len(), len(body))
	}

	if rec := get("/json", ""); rec.Header().Get("Content-Encoding") != "" || !strings.Contains(rec.Body.String(), big) {
		t.Error("a client without gzip gets plain JSON")
	}
	if rec := get("/pdf", "gzip"); rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != "%PDF-1.7" || rec.Code != http.StatusPartialContent {
		t.Error("files with Range stay as they are")
	}
	if rec := get("/events", "gzip"); rec.Header().Get("Content-Encoding") != "" || !rec.Flushed {
		t.Error("the event stream is not compressed and still flushes")
	}
}
