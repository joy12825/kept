package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

var built = fstest.MapFS{
	"index.html":         {Data: []byte("<!doctype html><title>收好</title>")},
	"assets/app-3f2a.js": {Data: []byte("console.log(1)")},
	"placeholder.html":   {Data: []byte("placeholder")},
}

func TestServesIndexForFrontendRoutes(t *testing.T) {
	h := handlerFS(built)
	for _, p := range []string{"/", "/files", "/docs/42", "/settings/profile"} {
		rec := get(h, p)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>收好</title>") {
			t.Errorf("%s: %d %q", p, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: index must not be cached, got %q", p, rec.Header().Get("Cache-Control"))
		}
	}
}

func TestHashedAssetsAreCachedForever(t *testing.T) {
	rec := get(handlerFS(built), "/assets/app-3f2a.js")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %d %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
}

func TestSecurityHeaders(t *testing.T) {
	rec := get(handlerFS(built), "/")
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
	if !strings.Contains(rec.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Errorf("CSP: %q", rec.Header().Get("Content-Security-Policy"))
	}
}

func TestPlaceholderWhenFrontendNotBuilt(t *testing.T) {
	rec := get(handlerFS(fstest.MapFS{"placeholder.html": {Data: []byte("请先执行 make web")}}), "/files")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "make web") {
		t.Errorf("placeholder: %d %q", rec.Code, rec.Body.String())
	}
}

func TestScriptsAreGzippedForClientsThatAcceptIt(t *testing.T) {
	js := strings.Repeat("function kept(){return '收好';}\n", 400)
	files := fstest.MapFS{
		"index.html":            {Data: []byte("<!doctype html>")},
		"assets/app-9.js":       {Data: []byte(js)},
		"assets/pdf.worker.mjs": {Data: []byte(js)},
		"assets/logo-1.png":     {Data: []byte(strings.Repeat("x", 4096))},
	}
	h := handlerFS(files)
	req := func(p, enc string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, p, nil)
		if enc != "" {
			r.Header.Set("Accept-Encoding", enc)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}
	for _, p := range []string{"/assets/app-9.js", "/assets/pdf.worker.mjs"} {
		rec := req(p, "gzip, br")
		if rec.Header().Get("Content-Encoding") != "gzip" || rec.Body.Len() >= len(js)/4 {
			t.Errorf("%s: %v, %d bytes", p, rec.Header(), rec.Body.Len())
		}
		if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
			t.Errorf("%s headers: %v", p, rec.Header())
		}
		zr, _ := gzip.NewReader(rec.Body)
		if b, _ := io.ReadAll(zr); string(b) != js {
			t.Errorf("%s: round trip differs", p)
		}
	}
	if rec := req("/assets/app-9.js", ""); rec.Header().Get("Content-Encoding") != "" || rec.Body.String() != js {
		t.Error("without gzip the file is sent as is")
	}
	if rec := req("/assets/logo-1.png", "gzip"); rec.Header().Get("Content-Encoding") != "" {
		t.Error("images are not compressed")
	}
}
