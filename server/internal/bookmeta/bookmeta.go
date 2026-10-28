// Package bookmeta looks up a book's catalog data by ISBN (v0.4): Open
// Library, Google Books and a deployer's own endpoint, queried in
// parallel and listed in the configured order. Results are cached for an
// hour so a form that asks twice costs one request.
package bookmeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"kept/server/internal/i18n"
)

// ErrNotFound means the source has no record of the ISBN.
var ErrNotFound = errors.New("not found")

// Meta is what a source knows about a book. Empty fields are unknown.
type Meta struct {
	Title       string
	Subtitle    string
	Authors     []string
	Publisher   string
	Published   string
	Pages       int
	Language    string
	Description string
	Series      string
	CoverURL    string // "" when the source has no cover
}

// Source is one place to look books up.
type Source interface {
	Name() string
	Lookup(ctx context.Context, isbn string) (*Meta, error)
}

// Names are the sources a deployer can list in KEPT_BOOK_SOURCES.
var Names = []string{"openlibrary", "googlebooks", "custom"}

// Label is a source's name for people.
func Label(name string) string {
	switch name {
	case "openlibrary":
		return "Open Library"
	case "googlebooks":
		return "Google Books"
	case "custom":
		return i18n.N("自定义书目接口")
	}
	return name
}

// Result is one source's answer.
type Result struct {
	Source string
	Meta   *Meta // nil when Err is set or the book is unknown
	Err    error // nil, ErrNotFound or why the source failed
}

// Client queries its sources.
type Client struct {
	sources []Source
	http    *http.Client
	timeout time.Duration
	ttl     time.Duration
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at      time.Time
	results []Result
}

// Options configures New. Base URLs are for tests.
type Options struct {
	Sources       []string // in order; empty means none
	CustomURL     string   // with {isbn}
	GoogleKey     string
	OpenLibrary   string // base URL, default https://openlibrary.org
	GoogleBooks   string // base URL, default https://www.googleapis.com
	HTTP          *http.Client
	Timeout       time.Duration // per source, default 8s
	CacheDuration time.Duration // default 1h
}

func New(o Options) *Client {
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{Transport: http.DefaultTransport} // honours HTTPS_PROXY
	}
	c := &Client{http: hc, timeout: o.Timeout, ttl: o.CacheDuration, now: time.Now, cache: map[string]cached{}}
	if c.timeout == 0 {
		c.timeout = 8 * time.Second
	}
	if c.ttl == 0 {
		c.ttl = time.Hour
	}
	for _, name := range o.Sources {
		switch name {
		case "openlibrary":
			c.sources = append(c.sources, &openLibrary{c: c, base: or(o.OpenLibrary, "https://openlibrary.org")})
		case "googlebooks":
			c.sources = append(c.sources, &googleBooks{c: c, base: or(o.GoogleBooks, "https://www.googleapis.com"), key: o.GoogleKey})
		case "custom":
			if o.CustomURL != "" {
				c.sources = append(c.sources, &custom{c: c, url: o.CustomURL})
			}
		}
	}
	return c
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return strings.TrimRight(s, "/")
}

// Enabled reports whether any source is configured.
func (c *Client) Enabled() bool { return c != nil && len(c.sources) > 0 }

// Lookup asks every source about a 13-digit ISBN, in parallel; the results
// are in the configured order.
func (c *Client) Lookup(ctx context.Context, isbn string) []Result {
	c.mu.Lock()
	if e, ok := c.cache[isbn]; ok && c.now().Sub(e.at) < c.ttl {
		c.mu.Unlock()
		return e.results
	}
	c.mu.Unlock()
	out := make([]Result, len(c.sources))
	var wg sync.WaitGroup
	for i, s := range c.sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sctx, cancel := context.WithTimeout(ctx, c.timeout)
			defer cancel()
			m, err := s.Lookup(sctx, isbn)
			if err == nil && m != nil {
				clean(m)
			}
			out[i] = Result{Source: s.Name(), Meta: m, Err: err}
		}()
	}
	wg.Wait()
	// Failures (other than "not found") are not cached: the network may be back soon.
	keep := true
	for _, r := range out {
		if r.Err != nil && !errors.Is(r.Err, ErrNotFound) {
			keep = false
		}
	}
	if keep && ctx.Err() == nil {
		c.mu.Lock()
		for k, e := range c.cache { // a small cache: drop what expired
			if c.now().Sub(e.at) >= c.ttl {
				delete(c.cache, k)
			}
		}
		c.cache[isbn] = cached{at: c.now(), results: out}
		c.mu.Unlock()
	}
	return out
}

// Cover downloads the cover a source reported for the ISBN (from the
// cache, or by looking it up again). It returns the image bytes, at most
// max+1 of them so the caller can tell it was too big.
func (c *Client) Cover(ctx context.Context, isbn, source string, max int64) ([]byte, error) {
	var url string
	for _, r := range c.Lookup(ctx, isbn) {
		if r.Source == source && r.Meta != nil {
			url = r.Meta.CoverURL
		}
	}
	if url == "" {
		return nil, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 2*c.timeout)
	defer cancel()
	body, err := c.get(ctx, url, nil)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	return io.ReadAll(io.LimitReader(body, max+1))
}

// get fetches url; a 404 is ErrNotFound, other non-2xx answers errors.
func (c *Client) get(ctx context.Context, url string, header http.Header) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range header {
		req.Header[k] = v
	}
	req.Header.Set("User-Agent", "kept (self-hosted document manager)")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// getJSON decodes a JSON answer of at most 2 MB.
func (c *Client) getJSON(ctx context.Context, url string, v any) error {
	body, err := c.get(ctx, url, http.Header{"Accept": {"application/json"}})
	if err != nil {
		return err
	}
	defer body.Close()
	if err := json.NewDecoder(io.LimitReader(body, 2<<20)).Decode(v); err != nil {
		return fmt.Errorf("the answer is not the expected JSON: %w", err)
	}
	return nil
}

// clean trims fields and drops empty authors; it does not invent values.
func clean(m *Meta) {
	for _, p := range []*string{&m.Title, &m.Subtitle, &m.Publisher, &m.Published, &m.Language, &m.Description, &m.Series, &m.CoverURL} {
		*p = strings.TrimSpace(*p)
	}
	var authors []string
	for _, a := range m.Authors {
		if a = strings.TrimSpace(a); a != "" {
			authors = append(authors, a)
		}
	}
	m.Authors = authors
	m.Published = published(m.Published)
	if m.Pages < 0 || m.Pages > 100000 {
		m.Pages = 0
	}
	if r := []rune(m.Description); len(r) > 5000 {
		m.Description = string(r[:5000])
	}
	if !strings.HasPrefix(m.CoverURL, "https://") && !strings.HasPrefix(m.CoverURL, "http://") {
		m.CoverURL = ""
	}
}

// published turns the dates sources give ("October 1, 1988", "1988-10-01",
// "1988") into the year and month people write ("1988-10"), or the year.
func published(s string) string {
	for _, layout := range []string{"2006-01-02", "2006-01", "January 2, 2006", "Jan 2, 2006", "2 January 2006", "January 2006", "Jan 2006", "2006/1/2", "2006/1", "2006.1"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01")
		}
	}
	if m := yearRe.FindStringSubmatch(s); m != nil && len(s) <= 12 {
		return m[1]
	}
	return s
}

var yearRe = regexp.MustCompile(`(?:^|\D)((?:1[5-9]|20)\d{2})(?:\D|$)`)

// languages maps the codes sources use to names people read (translated
// where a lookup is sent, v1.10.1).
var languages = map[string]string{
	"zh": i18n.N("中文"), "chi": i18n.N("中文"), "zho": i18n.N("中文"), "zh-cn": i18n.N("中文"), "zh-tw": i18n.N("中文"),
	"en": i18n.N("英文"), "eng": i18n.N("英文"), "ja": i18n.N("日文"), "jpn": i18n.N("日文"), "fr": i18n.N("法文"), "fre": i18n.N("法文"), "fra": i18n.N("法文"),
	"de": i18n.N("德文"), "ger": i18n.N("德文"), "deu": i18n.N("德文"), "ru": i18n.N("俄文"), "rus": i18n.N("俄文"), "es": i18n.N("西班牙文"), "spa": i18n.N("西班牙文"), "ko": i18n.N("韩文"), "kor": i18n.N("韩文"),
}

func language(code string) string {
	if n, ok := languages[strings.ToLower(code)]; ok {
		return n
	}
	return code
}
