package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Entry is one log record kept for the system log page (design doc 6.1,
// `system`: 最近日志).
type Entry struct {
	Time    time.Time
	Level   string // DEBUG, INFO, WARN, ERROR
	Message string
	Attrs   [][2]string // key, value; groups are joined with dots
}

// Ring keeps the newest records in memory. It is safe for concurrent use.
type Ring struct {
	mu   sync.Mutex
	buf  []Entry
	next int
	full bool
}

func NewRing(size int) *Ring { return &Ring{buf: make([]Entry, size)} }

// Recent holds the process's latest log records.
var Recent = NewRing(2000)

func (r *Ring) add(e Entry) {
	r.mu.Lock()
	r.buf[r.next] = e
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// Entries returns the kept records newest first, at minLevel or above and
// containing q (in the message or an attribute), at most limit of them.
func (r *Ring) Entries(minLevel slog.Level, q string, limit int) []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.next
	if r.full {
		n = len(r.buf)
	}
	out := []Entry{}
	for i := 0; i < n && len(out) < limit; i++ {
		e := r.buf[(r.next-1-i+len(r.buf))%len(r.buf)]
		var lv slog.Level
		_ = lv.UnmarshalText([]byte(e.Level))
		if lv < minLevel || (q != "" && !e.contains(q)) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (e Entry) contains(q string) bool {
	q = strings.ToLower(q)
	if strings.Contains(strings.ToLower(e.Message), q) {
		return true
	}
	for _, a := range e.Attrs {
		if strings.Contains(strings.ToLower(a[0]+"="+a[1]), q) {
			return true
		}
	}
	return false
}

// teeHandler passes records to the real handler and keeps a copy in a Ring.
// Successful HTTP access lines are not kept: they would push everything
// else out of the buffer.
type teeHandler struct {
	inner  slog.Handler
	ring   *Ring
	attrs  [][2]string
	groups string
}

func (h *teeHandler) Enabled(ctx context.Context, l slog.Level) bool { return h.inner.Enabled(ctx, l) }

func (h *teeHandler) Handle(ctx context.Context, rec slog.Record) error {
	keep := true
	attrs := append([][2]string{}, h.attrs...)
	rec.Attrs(func(a slog.Attr) bool {
		attrs = flatten(attrs, h.groups, a)
		if rec.Message == "http request" && a.Key == "status" && a.Value.Kind() == slog.KindInt64 && a.Value.Int64() < 400 {
			keep = false
		}
		return true
	})
	if keep {
		h.ring.add(Entry{Time: rec.Time, Level: rec.Level.String(), Message: rec.Message, Attrs: attrs})
	}
	return h.inner.Handle(ctx, rec)
}

func (h *teeHandler) WithAttrs(as []slog.Attr) slog.Handler {
	c := *h
	c.inner = h.inner.WithAttrs(as)
	c.attrs = append([][2]string{}, h.attrs...)
	for _, a := range as {
		c.attrs = flatten(c.attrs, h.groups, a)
	}
	return &c
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	c := *h
	c.inner = h.inner.WithGroup(name)
	c.groups = h.groups + name + "."
	return &c
}

func flatten(out [][2]string, prefix string, a slog.Attr) [][2]string {
	v := a.Value.Resolve()
	if v.Kind() == slog.KindGroup {
		for _, g := range v.Group() {
			out = flatten(out, prefix+a.Key+".", g)
		}
		return out
	}
	return append(out, [2]string{prefix + a.Key, fmt.Sprint(v.Any())})
}

// NewHandlerForTest returns a handler that only records into Recent.
func NewHandlerForTest() slog.Handler {
	return &teeHandler{inner: slog.NewTextHandler(io.Discard, nil), ring: Recent}
}
