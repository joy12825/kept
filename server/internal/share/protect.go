package share

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"kept/server/internal/apperr"
	"kept/server/internal/exif"
	"kept/server/internal/files"
	"kept/server/internal/watermark"
)

// Protecting what is shared (v1.9.2): a line of watermark stamped on every
// PDF page served, and view-only shares (no download). Since v1.11.2 a
// picture shared as itself has the watermark drawn on it, and stays a
// picture.

// Protect is how a share is protected.
type Protect struct {
	Watermark     string // "" for none
	AllowDownload bool
}

// Settings is how a link or bundle is made.
type Settings struct {
	Days int
	File string // FileArchive or FileOriginal
	Protect
}

// ErrViewOnly: a view-only share refuses downloads.
var ErrViewOnly = apperr.WithCode(apperr.KindForbidden, "view_only", "这个分享只能在线查看，不能下载")

var errNoWatermark = apperr.New(apperr.KindInvalid, "服务器还不能加水印：缺少中文字体，请管理员安装或设置 KEPT_WATERMARK_FONT")

// cleanWatermark trims the text and checks it.
func cleanWatermark(text string) (string, error) {
	text = strings.Join(strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	if text == "" {
		return "", nil
	}
	if n := len([]rune(text)); n > watermark.MaxText {
		return "", apperr.New(apperr.KindInvalid, fmt.Sprintf("水印文字最多 %d 个字", watermark.MaxText))
	}
	if !watermark.ImageAvailable() {
		return "", errNoWatermark
	}
	return text, nil
}

// CleanWatermark trims a watermark and checks it: "" for none.
func CleanWatermark(text string) (string, error) { return cleanWatermark(text) }

// stampFile picks the file a watermark goes on for a document, given the
// file asked for: a picture asked for as itself, else its PDF (the
// searchable copy when asked for, or when the original is not a PDF or is
// encrypted), else a picture without a PDF. ok is false when there is
// nothing to put a watermark on.
func stampFile(file, mime string, archive, encrypted bool) (string, bool) {
	picture := watermark.ImageType(mime) != ""
	switch {
	case file == FileOriginal && picture:
		return FileOriginal, true
	case hasPDF(mime, archive):
		if encrypted && archive {
			return FileArchive, true
		}
		return pdfFile(file, mime, archive), true
	case picture:
		return FileOriginal, true
	}
	return "", false
}

// StampFile is stampFile for the callers outside: which file a document's
// watermarked copy is made from.
func StampFile(file, mime string, archive, encrypted bool) (string, bool) {
	return stampFile(file, mime, archive, encrypted)
}

// checkStamp says whether this server can put a watermark on a file of
// type mime: a PDF needs qpdf too.
func checkStamp(mime string) error {
	if mime == "application/pdf" && !watermark.Available() || !watermark.ImageAvailable() {
		return errNoWatermark
	}
	return nil
}

// servedName is a file's name once stamped: a picture may change type.
func servedName(title, ext, mime string) string {
	switch watermark.ImageType(mime) {
	case "image/jpeg":
		ext = "jpg"
	case "image/png":
		ext = "png"
	}
	return files.FileName(title, ext, map[string]int{})
}

// hasPDF reports whether a document has a PDF to stamp: its original, or
// its searchable copy.
func hasPDF(mime string, archive bool) bool { return archive || mime == "application/pdf" }

// pdfFile picks the file a watermarked share serves: the searchable copy
// when asked for or when the original is not a PDF.
func pdfFile(file, mime string, archive bool) string {
	if archive && (file == FileArchive || mime != "application/pdf") {
		return FileArchive
	}
	return FileOriginal
}

// footer is the small line at the foot of each stamped page.
func footer(text string, created time.Time) string {
	return text + " · 分享于 " + created.Format("2006-01-02")
}

var stampMu sync.Mutex

// Stamp returns the watermarked copy of loc, a file of type mime (a PDF or
// a picture), made once and kept in the cache under key (which changes
// when the file or the watermark does), and the type of the copy.
func (s *Service) Stamp(ctx context.Context, loc files.Loc, mime, key, text, foot string) (files.Loc, string, error) {
	out := mime
	ext := ".pdf"
	if mime != "application/pdf" {
		out = watermark.ImageType(mime)
		if out == "" {
			return files.Loc{}, "", apperr.New(apperr.KindInvalid, "这份文档没有 PDF 或图片，不能加水印")
		}
		ext = map[string]string{"image/jpeg": ".jpg", "image/png": ".png"}[out]
	}
	if err := checkStamp(mime); err != nil {
		return files.Loc{}, "", err
	}
	l, err := s.stamped(ctx, loc, mime, key+ext, text, foot)
	return l, out, err
}

func (s *Service) stamped(ctx context.Context, loc files.Loc, mime, name, text, foot string) (files.Loc, error) {
	dir := s.opt.Files.CacheDir("watermark")
	dst := filepath.Join(dir, name)
	stampMu.Lock()
	defer stampMu.Unlock()
	if _, err := os.Stat(dst); err == nil {
		now := time.Now()
		_ = os.Chtimes(dst, now, now) // still in use: not cleaned
		return files.LocalFile(dst), nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return files.Loc{}, err
	}
	work, err := s.opt.Files.NewTmpDir("watermark-")
	if err != nil {
		return files.Loc{}, err
	}
	defer os.RemoveAll(work)
	src := filepath.Join(work, "in")
	f, err := s.opt.Files.OpenLoc(ctx, loc)
	if err != nil {
		return files.Loc{}, err
	}
	out, err := os.Create(src)
	if err == nil {
		_, err = out.ReadFrom(f)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}
	f.Close()
	if err != nil {
		return files.Loc{}, err
	}
	tmp := filepath.Join(work, "out")
	if mime == "application/pdf" {
		err = watermark.Apply(ctx, src, tmp, work, text, foot)
	} else {
		err = watermark.ApplyImage(src, tmp, mime, exif.ReadFile(src).Orientation, text, foot)
	}
	if errors.Is(err, watermark.ErrTooLarge) {
		return files.Loc{}, apperr.New(apperr.KindInvalid, "图片太大，不能加水印")
	}
	if err != nil {
		return files.Loc{}, err
	}
	if err := files.Move(tmp, dst); err != nil {
		return files.Loc{}, err
	}
	return files.LocalFile(dst), nil
}

// TextKey is a short name for a watermark's text, for cache keys.
func TextKey(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:6])
}

// cacheKey names a share's stamped copy of a file.
func cacheKey(kind string, shareID, docID int64, sum string) string {
	if len(sum) > 16 {
		sum = sum[:16]
	}
	return fmt.Sprintf("%s-%d-%d-%s", kind, shareID, docID, sum)
}
