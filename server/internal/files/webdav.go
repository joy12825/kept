package files

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"kept/server/internal/i18n"
)

// ---------- WebDAV (v1.7.5): Nextcloud, ownCloud, 坚果云, Synology, QNAP ----------

// WebDAVConfig is a folder on a WebDAV server; the password comes from the
// environment or is sealed in the database, never shown.
type WebDAVConfig struct {
	URL      string // the folder, e.g. https://cloud.example.com/remote.php/dav/files/wang/kept
	Username string
	Password string
	// Transport replaces the HTTP transport (tests).
	Transport http.RoundTripper
}

// WebDAVBackend keeps objects as files under a folder of a WebDAV server,
// in the same layout as the data directory.
type WebDAVBackend struct {
	base     *url.URL // ends with /
	user     string
	pass     string
	c        *http.Client
	baseMade atomic.Bool // the storage's own folder is there (made when missing)
}

func NewWebDAV(cfg WebDAVConfig) (*WebDAVBackend, error) {
	u, err := url.Parse(strings.TrimSpace(cfg.URL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("a WebDAV address is http(s)://host/folder, got %q", cfg.URL)
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/"
	u.RawQuery, u.Fragment = "", ""
	if u.User != nil { // credentials go in the fields, never in what is shown
		if cfg.Username == "" {
			cfg.Username = u.User.Username()
		}
		if p, ok := u.User.Password(); ok && cfg.Password == "" {
			cfg.Password = p
		}
		u.User = nil
	}
	tr := cfg.Transport
	if tr == nil {
		t := http.DefaultTransport.(*http.Transport).Clone()
		t.ResponseHeaderTimeout = 2 * time.Minute
		tr = t
	}
	return &WebDAVBackend{base: u, user: cfg.Username, pass: cfg.Password, c: &http.Client{Transport: tr}}, nil
}

func (b *WebDAVBackend) url(key string) (string, error) {
	clean := path.Clean("/" + key)[1:]
	if clean == "" || clean != key {
		return "", fmt.Errorf("bad key %q", key)
	}
	u := *b.base
	u.Path += clean
	return u.String(), nil
}

func (b *WebDAVBackend) do(ctx context.Context, method, target string, body io.Reader, header map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, err
	}
	if b.user != "" || b.pass != "" {
		req.SetBasicAuth(b.user, b.pass)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	return b.c.Do(req)
}

// statusErr turns an unexpected answer into an error; 404 is fs.ErrNotExist.
func statusErr(op string, resp *http.Response) error {
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", fs.ErrNotExist, op)
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf(i18n.N("%s: 用户名或密码不对，或没有权限（%s）"), op, resp.Status)
	}
	return fmt.Errorf("%s: %s", op, resp.Status)
}

type propResponse struct {
	Href string `xml:"href"`
	Prop struct {
		Length   string `xml:"getcontentlength"`
		Modified string `xml:"getlastmodified"`
		Type     struct {
			Collection *struct{} `xml:"collection"`
		} `xml:"resourcetype"`
	} `xml:"propstat>prop"`
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?><d:propfind xmlns:d="DAV:"><d:prop><d:getcontentlength/><d:getlastmodified/><d:resourcetype/></d:prop></d:propfind>`

func (b *WebDAVBackend) propfind(ctx context.Context, target, depth string) ([]propResponse, error) {
	resp, err := b.do(ctx, "PROPFIND", target, strings.NewReader(propfindBody), map[string]string{"Depth": depth, "Content-Type": "application/xml"})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, statusErr("PROPFIND", resp)
	}
	defer resp.Body.Close()
	var ms struct {
		Responses []propResponse `xml:"response"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, err
	}
	return ms.Responses, nil
}

func infoOf(p propResponse) Info {
	n, _ := strconv.ParseInt(p.Prop.Length, 10, 64)
	t, _ := http.ParseTime(p.Prop.Modified)
	return Info{Size: n, ModTime: t}
}

func (b *WebDAVBackend) Stat(ctx context.Context, key string) (Info, error) {
	target, err := b.url(key)
	if err != nil {
		return Info{}, err
	}
	rs, err := b.propfind(ctx, target, "0")
	if err != nil {
		return Info{}, err
	}
	if len(rs) == 0 || rs[0].Prop.Type.Collection != nil {
		return Info{}, fmt.Errorf("%w: %s", fs.ErrNotExist, key)
	}
	return infoOf(rs[0]), nil
}

// davFile reads a remote file; seeking starts a ranged GET at the new
// offset, so a viewer reading a large PDF in pieces only fetches those.
type davFile struct {
	b      *WebDAVBackend
	ctx    context.Context
	target string
	info   Info
	off    int64
	body   io.ReadCloser
}

func (f *davFile) Info() Info { return f.info }

func (f *davFile) Read(p []byte) (int, error) {
	if f.off >= f.info.Size {
		return 0, io.EOF
	}
	if f.body == nil {
		resp, err := f.b.do(f.ctx, http.MethodGet, f.target, nil, map[string]string{"Range": fmt.Sprintf("bytes=%d-", f.off)})
		if err != nil {
			return 0, err
		}
		switch {
		case resp.StatusCode == http.StatusPartialContent:
		case resp.StatusCode == http.StatusOK: // no ranges: skip to the offset
			if _, err := io.CopyN(io.Discard, resp.Body, f.off); err != nil {
				resp.Body.Close()
				return 0, err
			}
		default:
			return 0, statusErr("GET", resp)
		}
		f.body = resp.Body
	}
	n, err := f.body.Read(p)
	f.off += int64(n)
	return n, err
}

func (f *davFile) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = f.off + offset
	case io.SeekEnd:
		abs = f.info.Size + offset
	}
	if abs < 0 {
		return 0, errors.New("seek before the start")
	}
	if abs != f.off && f.body != nil {
		f.body.Close()
		f.body = nil
	}
	f.off = abs
	return abs, nil
}

func (f *davFile) Close() error {
	if f.body != nil {
		return f.body.Close()
	}
	return nil
}

func (b *WebDAVBackend) Open(ctx context.Context, key string) (File, error) {
	info, err := b.Stat(ctx, key)
	if err != nil {
		return nil, err
	}
	target, _ := b.url(key)
	return &davFile{b: b, ctx: ctx, target: target, info: info}, nil
}

// mkdirs makes the folders above key, from the storage's own folder
// down; existing ones are fine.
func (b *WebDAVBackend) mkdirs(ctx context.Context, key string) error {
	mk := func(u url.URL, what string) error {
		resp, err := b.do(ctx, "MKCOL", u.String(), nil, nil)
		if err != nil {
			return err
		}
		switch resp.StatusCode {
		case http.StatusCreated, http.StatusMethodNotAllowed: // made, or already there
			resp.Body.Close()
			return nil
		case http.StatusConflict:
			resp.Body.Close()
			return fmt.Errorf(i18n.N("建不了文件夹 %s：上一级文件夹不存在"), what)
		}
		return statusErr("MKCOL "+what, resp)
	}
	if !b.baseMade.Load() {
		if err := mk(*b.base, b.base.Path); err != nil {
			return err
		}
		b.baseMade.Store(true)
	}
	cur := ""
	for _, d := range strings.Split(path.Dir(key), "/") {
		if d == "." || d == "" {
			continue
		}
		cur = path.Join(cur, d)
		u := *b.base
		u.Path += cur + "/"
		if err := mk(u, cur); err != nil {
			return err
		}
	}
	return nil
}

func (b *WebDAVBackend) Put(ctx context.Context, key, src string) error {
	target, err := b.url(key)
	if err != nil {
		return err
	}
	if err := b.mkdirs(ctx, key); err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target, f)
	if err != nil {
		return err
	}
	req.ContentLength = st.Size()
	if b.user != "" || b.pass != "" {
		req.SetBasicAuth(b.user, b.pass)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := b.c.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return statusErr("PUT", resp)
	}
	resp.Body.Close()
	return nil
}

func (b *WebDAVBackend) Rename(ctx context.Context, from, to string) error {
	src, err := b.url(from)
	if err != nil {
		return err
	}
	dst, err := b.url(to)
	if err != nil {
		return err
	}
	if err := b.mkdirs(ctx, to); err != nil {
		return err
	}
	resp, err := b.do(ctx, "MOVE", src, nil, map[string]string{"Destination": dst, "Overwrite": "T"})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return statusErr("MOVE", resp)
	}
	resp.Body.Close()
	return nil
}

func (b *WebDAVBackend) Remove(ctx context.Context, key string) error {
	target, err := b.url(key)
	if err != nil {
		return err
	}
	resp, err := b.do(ctx, http.MethodDelete, target, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil
	}
	if resp.StatusCode >= 300 {
		return statusErr("DELETE", resp)
	}
	resp.Body.Close()
	return nil
}

// List lists one folder: the files under the folder part of prefix whose
// names start with the rest (the layout has no deeper folders there).
func (b *WebDAVBackend) List(ctx context.Context, prefix string) ([]string, error) {
	dir, base := path.Split(prefix)
	u := *b.base
	u.Path += dir
	rs, err := b.propfind(ctx, u.String(), "1")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rs {
		if r.Prop.Type.Collection != nil {
			continue
		}
		href, err := url.PathUnescape(r.Href)
		if err != nil {
			continue
		}
		name := path.Base(strings.TrimSuffix(href, "/"))
		if strings.HasPrefix(name, base) {
			out = append(out, dir+name)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (b *WebDAVBackend) LocalPath(string) string { return "" }

func (b *WebDAVBackend) Describe() string { return b.base.String() }
