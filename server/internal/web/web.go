// Package web serves the embedded web app (built from ../app by `make web`)
// with a single-page-app fallback, cache headers and security headers.
package web

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

//go:embed all:dist
var dist embed.FS

// The EPUB reader (v0.4) renders books in blob: iframes, with the book's
// styles and fonts as blob: URLs; its scripts stay blocked (script-src).
const csp = "default-src 'self'; img-src 'self' data: blob:; worker-src 'self' blob:; frame-src 'self' blob:; " +
	"style-src 'self' 'unsafe-inline' blob:; font-src 'self' data: blob:; frame-ancestors 'none'; base-uri 'self'"

// Handler serves the embedded build output.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // the embed pattern guarantees the directory exists
	}
	return handlerFS(sub)
}

func handlerFS(files fs.FS) http.Handler {
	fileServer := http.FileServerFS(files)
	gz := &gzipCache{files: files}
	index := "index.html"
	if _, err := fs.Stat(files, index); err != nil {
		index = "placeholder.html"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", csp)

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if info, err := fs.Stat(files, name); err == nil && !info.IsDir() && name != "index.html" && name != "placeholder.html" {
			if strings.HasPrefix(name, "assets/") {
				h.Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				h.Set("Cache-Control", "no-cache")
			}
			if gz.serve(w, r, name) {
				return
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		// Front-end routes (/files, /docs/42 …) and "/" get the app shell.
		body, err := fs.ReadFile(files, index)
		if err != nil {
			http.Error(w, "web app missing", http.StatusInternalServerError)
			return
		}
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	})
}

// gzipCache compresses text assets once, on first request, and keeps them
// in memory (v1.3.2): the app's scripts are about 3 MB and a third of that
// compressed, which is seconds on a slow link the first time.
type gzipCache struct {
	files fs.FS
	m     sync.Map // name → []byte (nil: not worth compressing)
}

var compressible = map[string]string{
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8", ".svg": "image/svg+xml", ".json": "application/json",
	".txt": "text/plain; charset=utf-8", ".map": "application/json", ".wasm": "application/wasm",
}

func (c *gzipCache) serve(w http.ResponseWriter, r *http.Request, name string) bool {
	ct, ok := compressible[path.Ext(name)]
	if !ok || r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	w.Header().Add("Vary", "Accept-Encoding")
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		return false
	}
	v, done := c.m.Load(name)
	if !done {
		v = c.compress(name)
		c.m.Store(name, v)
	}
	body, _ := v.([]byte)
	if body == nil {
		return false
	}
	h := w.Header()
	if t := mime.TypeByExtension(path.Ext(name)); t != "" && path.Ext(name) != ".mjs" {
		ct = t
	}
	h.Set("Content-Type", ct)
	h.Set("Content-Encoding", "gzip")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		return true
	}
	_, _ = w.Write(body)
	return true
}

// compress returns the gzipped file, or nil when it is small or does not shrink.
func (c *gzipCache) compress(name string) []byte {
	raw, err := fs.ReadFile(c.files, name)
	if err != nil || len(raw) < 1024 {
		return nil
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	if buf.Len() >= len(raw)*9/10 {
		return nil
	}
	return buf.Bytes()
}
