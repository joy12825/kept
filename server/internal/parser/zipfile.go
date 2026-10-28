package parser

import (
	"archive/zip"
	"io"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Zip files (v1.9.3): kept as they are; the names of their files are the
// text to search, and the files can be imported one by one. Nothing is
// unpacked to disk: each file is read as a stream when imported.

const zipType = "application/zip"

// Limits on what a zip may make us do.
const (
	ZipMaxEntries = 5000
	zipTextMax    = 256 << 10
)

// ZipEntry is a file (or folder) in a zip.
type ZipEntry struct {
	Path      string // cleaned, with / between folders; never .. or absolute
	Size      int64  // uncompressed
	Dir       bool
	Encrypted bool
	index     int
}

// ErrZip: the file is not a zip we can read.
var ErrZip = &Error{Message: "压缩包读不出来，可能已损坏", Permanent: true}

// zipName is an entry's name as text: UTF-8 when the zip says so or it is
// valid UTF-8, else GBK (zips made on Chinese Windows).
func zipName(f *zip.File) string {
	name := f.Name
	if utf8.ValidString(name) && (!f.NonUTF8 || isASCII(name)) {
		return name
	}
	if d, err := simplifiedchinese.GB18030.NewDecoder().String(name); err == nil {
		return d
	}
	return strings.ToValidUTF8(name, "?")
}

// cleanZipPath makes an entry's path safe to show: forward slashes, no
// leading slash or drive, no . or .. parts.
func cleanZipPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	var parts []string
	for _, part := range strings.Split(p, "/") {
		switch part {
		case "", ".", "..":
			continue
		}
		if len(parts) == 0 && strings.HasSuffix(part, ":") {
			continue // C:
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/")
}

// ListZip lists the entries of the zip at src, folders first then by
// path; truncated when there are more than ZipMaxEntries.
func ListZip(src string) (entries []ZipEntry, truncated bool, err error) {
	z, err := zip.OpenReader(src)
	if err != nil {
		return nil, false, ErrZip
	}
	defer z.Close()
	seen := map[string]bool{}
	for i, f := range z.File {
		if len(entries) >= ZipMaxEntries {
			truncated = true
			break
		}
		p := cleanZipPath(zipName(f))
		if p == "" || seen[p] || strings.HasPrefix(path.Base(p), "._") || strings.HasPrefix(p, "__MACOSX/") {
			continue // Mac's resource forks are not files of their own
		}
		seen[p] = true
		entries = append(entries, ZipEntry{Path: p, Size: int64(f.UncompressedSize64), Dir: f.FileInfo().IsDir(), Encrypted: f.Flags&0x1 != 0, index: i})
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, truncated, nil
}

// zipText is the names of a zip's files, one per line.
func zipText(src string) (string, error) {
	entries, _, err := ListZip(src)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		if e.Dir {
			continue
		}
		if b.Len()+len(e.Path) > zipTextMax {
			break
		}
		b.WriteString(e.Path + "\n")
	}
	return strings.TrimSpace(b.String()), nil
}

// OpenZipEntry opens the file at p (as ListZip shows it) for reading, at
// most limit bytes; close the returned closer when done.
func OpenZipEntry(src, p string, limit int64) (io.ReadCloser, ZipEntry, error) {
	z, err := zip.OpenReader(src)
	if err != nil {
		return nil, ZipEntry{}, ErrZip
	}
	for _, f := range z.File {
		if cleanZipPath(zipName(f)) != p || f.FileInfo().IsDir() {
			continue
		}
		e := ZipEntry{Path: p, Size: int64(f.UncompressedSize64), Encrypted: f.Flags&0x1 != 0}
		if e.Encrypted {
			z.Close()
			return nil, e, &Error{Message: "压缩包里的这个文件有密码", Permanent: true}
		}
		if limit > 0 && e.Size > limit {
			z.Close()
			return nil, e, &Error{Message: "文件太大", Permanent: true}
		}
		rc, err := f.Open()
		if err != nil {
			z.Close()
			return nil, e, ErrZip
		}
		r := io.Reader(rc)
		if limit > 0 {
			r = io.LimitReader(rc, limit+1) // a size that lies is caught by the reader
		}
		return struct {
			io.Reader
			io.Closer
		}{r, closers{rc, z}}, e, nil
	}
	z.Close()
	return nil, ZipEntry{}, &Error{Message: "压缩包里没有这个文件", Permanent: true}
}

type closers []io.Closer

func (c closers) Close() error {
	var first error
	for _, x := range c {
		if err := x.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// isZip reports whether src opens as a zip.
func isZip(src string) bool {
	z, err := zip.OpenReader(src)
	if err != nil {
		return false
	}
	z.Close()
	return true
}
