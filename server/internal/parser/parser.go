// Package parser turns an uploaded file into text, a searchable archive PDF,
// a thumbnail and a page count (design doc 4.2, steps 3 to 6), using
// poppler and ocrmypdf.
package parser

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gabriel-vasile/mimetype"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type Config struct {
	OCRLanguages string // tesseract languages, e.g. chi_sim+eng
	OCRMode      string // skip (only pages without text) | redo | force
	// Rotate turns pages upright by the text's detected orientation, and
	// Deskew straightens crooked scans (v1.3.9); both only touch the pages
	// that are recognised.
	Rotate, Deskew bool
	// RotateThreshold is the orientation confidence needed to turn a page;
	// 0 uses ocrmypdf's own (14).
	RotateThreshold float64
	MinTextChars    int    // a PDF with fewer characters of text counts as a scan; default 50
	GotenbergURL    string // Office conversion; "" keeps Office documents and e-mails waiting for it
}

// Result describes a parsed file. ArchivePath and ThumbnailPath are files
// in the work directory, or "" when there is none.
type Result struct {
	MimeType      string
	Ext           string // canonical extension of the original, without the dot
	Content       string
	PageCount     int
	ArchivePath   string
	ThumbnailPath string
	Warnings      []string
	// E-books (v0.4): what the file says about itself, and its cover as a
	// JPEG in the work directory ("" when it has none).
	Book      *BookMeta
	CoverPath string
	// Email (v1.5.1): an .eml file's subject, date and files; the files
	// become documents of their own.
	Email *EmailMeta
	// Pending (v1.9.1) is what an Office document or an e-mail taken
	// without Gotenberg waits for: PendingOffice or PendingEmail. Its text
	// is read; the PDF comes when it is reprocessed with Gotenberg.
	Pending string
}

// What a document kept without its converted PDF waits for.
const (
	PendingOffice = "office"
	PendingEmail  = "email"
)

// Progress reports a stage name and a percentage within parsing (0-100).
type Progress func(stage string, percent int)

var supported = map[string]string{
	"application/pdf":      "pdf",
	"image/jpeg":           "jpg",
	"image/png":            "png",
	"image/tiff":           "tif",
	"text/plain":           "txt",
	"application/epub+zip": "epub",
}

// mediaTypes are sound and video (v1.9.1): kept as they are and played in
// the browser; nothing to read or recognise. Matched on the detected type
// itself, not its parents (an AVIF picture is a kind of MP4 to mimetype).
var mediaTypes = map[string]string{
	"audio/mpeg":      "mp3",
	"audio/x-m4a":     "m4a",
	"audio/mp4":       "m4a",
	"audio/aac":       "aac",
	"audio/wav":       "wav",
	"audio/ogg":       "ogg",
	"audio/flac":      "flac",
	"video/mp4":       "mp4",
	"video/quicktime": "mov",
	"video/webm":      "webm",
}

// IsMedia reports whether mime is sound or video.
func IsMedia(mime string) bool { _, ok := mediaTypes[mime]; return ok }

var markup = map[string]bool{"text/html": true, "image/svg+xml": true, "text/xml": true, "application/xml": true, "application/xhtml+xml": true}

// Detect identifies a file by its content (magic numbers), not its name.
// Markdown is plain text named *.md.
func Detect(path, filename string) (mime, ext string, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", "", err
	}
	if st.Size() == 0 {
		return "", "", ErrEmpty
	}
	m, err := mimetype.DetectFile(path)
	if err != nil {
		return "", "", err
	}
	// Markup is text to mimetype (HTML → text/plain), but an HTML or SVG
	// page is not a document to keep, and could carry scripts.
	for mt := m; mt != nil; mt = mt.Parent() {
		if markup[strings.SplitN(mt.String(), ";", 2)[0]] {
			return "", "", ErrUnsupported
		}
	}
	if base := strings.SplitN(m.String(), ";", 2)[0]; mediaTypes[base] != "" {
		return base, mediaTypes[base], nil
	}
	for mt := m; mt != nil; mt = mt.Parent() {
		base := strings.SplitN(mt.String(), ";", 2)[0]
		if base == emailType {
			return base, "eml", nil
		}
		if base == msgType {
			return base, "msg", nil
		}
		if ext, ok := officeTypes[base]; ok {
			return base, ext, nil
		}
		if ext, ok := convertTypes[base]; ok {
			return base, ext, nil
		}
		if base == "application/x-ole-storage" && isWordCFB(path) { // v1.11.1: a .doc without Word's class ID (textutil, WPS)
			return DocMIME, "doc", nil
		}
		if base == "application/zip" { // v1.9.3: an OFD, or a zip of files
			if !isZip(path) {
				return "", "", ErrUnsupported
			}
			if isOFD(path) {
				return ofdType, "ofd", nil
			}
			return zipType, "zip", nil
		}
		if ext, ok := supported[base]; ok {
			if base == "text/plain" && strings.EqualFold(filepath.Ext(filename), ".md") {
				return "text/markdown", "md", nil
			}
			return base, ext, nil
		}
	}
	return "", "", ErrUnsupported
}

// Parse processes the file at src inside workDir.
func Parse(ctx context.Context, cfg Config, src, filename, workDir string, progress Progress) (Result, error) {
	if progress == nil {
		progress = func(string, int) {}
	}
	if cfg.MinTextChars == 0 {
		cfg.MinTextChars = 50
	}
	mime, ext, err := Detect(src, filename)
	if err != nil {
		return Result{}, err
	}
	res := Result{MimeType: mime, Ext: ext}
	switch {
	case mime == "text/plain" || mime == "text/markdown":
		res.Content, err = readText(src)
		return res, err
	case mime == "application/epub+zip":
		err = parseEPUB(src, workDir, &res, progress)
		return res, err // the thumbnail is the cover
	case mime == "application/pdf":
		err = parsePDF(ctx, cfg, src, workDir, &res, progress)
	case IsMedia(mime):
		return res, nil // played as it is: no text, no thumbnail
	case mime == zipType:
		res.Content, err = zipText(src)
		return res, err // its files are listed, and imported on request
	case mime == ofdType:
		progress("convert", 15)
		if err = parseOFD(src, workDir, &res); err != nil || res.ArchivePath == "" {
			return res, err // no Chinese font: the text only
		}
	case IsEmail(mime):
		if cfg.GotenbergURL == "" {
			msg, meta, rerr := readEmail(src)
			if rerr != nil {
				return res, rerr
			}
			res.Email, res.Content, res.Pending = meta, emailText(msg), PendingEmail
			return res, nil
		}
		progress("convert", 15)
		pdf, meta, cerr := convertEmail(ctx, cfg.GotenbergURL, src, workDir)
		if cerr != nil {
			return res, cerr
		}
		res.Email = meta
		if err = parsePDF(ctx, cfg, pdf, workDir, &res, progress); err == nil && res.ArchivePath == "" {
			res.ArchivePath = pdf // the preview of an e-mail is its printed body
		}
	case IsOffice(mime):
		if cfg.GotenbergURL == "" {
			res.Content, res.Pending = officeText(src, ext), PendingOffice
			return res, nil
		}
		progress("convert", 15)
		pdf, cerr := convertOffice(ctx, cfg.GotenbergURL, src, ext, workDir)
		if cerr != nil {
			return res, cerr
		}
		if err = parsePDF(ctx, cfg, pdf, workDir, &res, progress); err == nil && res.ArchivePath == "" {
			res.ArchivePath = pdf // the preview of an Office file is its PDF
		}
	case IsConverted(mime):
		progress("convert", 15)
		img, cerr := toPNG(ctx, mime, src, workDir)
		if cerr != nil {
			return res, cerr
		}
		err = parseImage(ctx, cfg, img, workDir, &res, progress)
	default: // images
		err = parseImage(ctx, cfg, src, workDir, &res, progress)
	}
	if err != nil {
		return res, err
	}
	progress("thumbnail", 90)
	preview := src
	if res.ArchivePath != "" {
		preview = res.ArchivePath
	}
	thumb, err := thumbnail(ctx, preview, workDir, res.PageCount)
	if err != nil {
		res.Warnings = append(res.Warnings, "缩略图生成失败："+err.Error())
	} else {
		res.ThumbnailPath = thumb
	}
	return res, nil
}

func parsePDF(ctx context.Context, cfg Config, src, workDir string, res *Result, progress Progress) error {
	progress("inspect", 20)
	pages, err := pageCount(ctx, src)
	if err != nil {
		return err
	}
	res.PageCount = pages
	text, err := pdfText(ctx, src, pages, false)
	if err != nil {
		return err
	}
	hasText := countChars(text) >= cfg.MinTextChars
	// A text layer that comes out garbled (fonts without a usable encoding)
	// is read again by OCR, once, and the better text kept (design doc 10.5).
	garbled := hasText && cfg.OCRMode != "force" && TextQuality(text) < garbledBelow
	if hasText && cfg.OCRMode == "skip" && !garbled {
		res.Content = text // the original already is searchable: no archive copy
		return nil
	}
	progress("ocr", 30)
	archive := filepath.Join(workDir, "archive.pdf")
	modeFlag := map[string]string{"skip": "--skip-text", "redo": "--redo-ocr", "force": "--force-ocr"}[cfg.OCRMode]
	if garbled {
		modeFlag = "--force-ocr"
	} else if modeFlag == "" {
		modeFlag = "--skip-text"
	}
	if err := ocr(ctx, cfg, pages, src, archive, modeFlag); err != nil {
		if hasText && !IsPermanent(err) {
			// Degrade: keep the text layer, no archive (design doc 4.3).
			res.Content = text
			res.Warnings = append(res.Warnings, "文字识别失败，已保留原有的文字层："+err.Error())
			return nil
		}
		return err
	}
	progress("ocr", 80)
	if res.Content, err = pdfText(ctx, archive, pages, true); err != nil {
		return err
	}
	if garbled {
		if TextQuality(res.Content) <= TextQuality(text) {
			// OCR did no better: keep the original and its text layer.
			os.Remove(archive)
			res.Content = text
			return nil
		}
		res.Warnings = append(res.Warnings, "原有的文字层是乱码，已重新识别文字")
	}
	res.ArchivePath = archive
	return nil
}

// garbledBelow is the TextQuality under which a text layer counts as garbled.
const garbledBelow = 0.8

// TextQuality is the share of a text's visible characters that are letters,
// digits or punctuation, as opposed to private-use characters, replacement
// characters, control codes and stray symbols that broken PDF fonts produce.
// An empty text scores 0.
func TextQuality(s string) float64 {
	good, all := 0, 0
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		all++
		switch {
		case r == utf8.RuneError, unicode.Is(unicode.Co, r), unicode.IsControl(r):
		case unicode.IsLetter(r), unicode.IsDigit(r), unicode.IsPunct(r), strings.ContainsRune("¥$€£%+-=<>×÷~|^`·°", r):
			good++
		}
	}
	if all == 0 {
		return 0
	}
	return float64(good) / float64(all)
}

func parseImage(ctx context.Context, cfg Config, src, workDir string, res *Result, progress Progress) error {
	progress("ocr", 30)
	archive := filepath.Join(workDir, "archive.pdf")
	input, err := flattenAlpha(src, workDir)
	if err != nil {
		return err
	}
	// --image-dpi overrides the resolution stored in the file, and OCR of a
	// 144 DPI screenshot read as 300 DPI comes out garbled; so only guess
	// when the image has no usable resolution.
	err = ocr(ctx, cfg, 1, input, archive)
	if errors.Is(err, errNoDPI) {
		err = ocr(ctx, cfg, 1, input, archive, "--image-dpi", "300")
	}
	if err != nil {
		return err
	}
	progress("ocr", 80)
	pages, err := pageCount(ctx, archive)
	if err != nil {
		return err
	}
	res.PageCount = pages
	if res.Content, err = pdfText(ctx, archive, pages, true); err != nil {
		return err
	}
	res.ArchivePath = archive
	return nil
}

var pagesRE = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)

func pageCount(ctx context.Context, pdf string) (int, error) {
	out, err := run(ctx, Timeout(1), "pdfinfo", pdf)
	if err != nil {
		if strings.Contains(out.stderr, "Incorrect password") {
			return 0, ErrEncrypted
		}
		return 0, &Error{Message: ErrCorrupt.Message, Permanent: true, Err: err}
	}
	m := pagesRE.FindStringSubmatch(out.stdout)
	if m == nil {
		return 0, ErrCorrupt
	}
	n, _ := strconv.Atoi(m[1])
	return n, nil
}

// pdfText extracts the text layer. Born-digital PDFs keep their layout;
// OCR output is read in content order (raw), because tesseract writes it in
// reading order and -layout scatters the per-word boxes of Chinese text
// ("缴费日期: 2024年5月18日" came out as "缴费日期 : 2024" plus fragments).
func pdfText(ctx context.Context, pdf string, pages int, ocred bool) (string, error) {
	mode := "-layout"
	if ocred {
		mode = "-raw"
	}
	out, err := run(ctx, Timeout(pages), "pdftotext", mode, "-enc", "UTF-8", pdf, "-")
	if err != nil {
		if strings.Contains(out.stderr, "Incorrect password") {
			return "", ErrEncrypted
		}
		return "", err
	}
	return strings.TrimSpace(out.stdout), nil
}

// layoutFlags turns and straightens pages while recognising them.
// ocrmypdf cannot deskew when it keeps an existing text layer (--redo-ocr).
func layoutFlags(cfg Config, mode []string) []string {
	var out []string
	if cfg.Rotate {
		out = append(out, "--rotate-pages")
		if cfg.RotateThreshold > 0 {
			out = append(out, "--rotate-pages-threshold", strconv.FormatFloat(cfg.RotateThreshold, 'f', -1, 64))
		}
	}
	if cfg.Deskew && !slices.Contains(mode, "--redo-ocr") {
		out = append(out, "--deskew")
	}
	return out
}

// errNoDPI: ocrmypdf refused an image without a (credible) resolution.
var errNoDPI = errors.New("image has no usable resolution")

func ocr(ctx context.Context, cfg Config, pages int, src, dst string, flags ...string) error {
	args := append([]string{"-l", cfg.OCRLanguages, "--output-type", "pdfa", "--quiet"}, flags...)
	args = append(args, layoutFlags(cfg, flags)...)
	args = append(args, src, dst)
	out, err := run(ctx, Timeout(pages)*2, "ocrmypdf", args...)
	if err == nil {
		return nil
	}
	switch out.exitCode { // ocrmypdf exit codes
	case 8:
		return ErrEncrypted
	case 2:
		if strings.Contains(out.stderr, "DpiError") {
			return errNoDPI
		}
		return &Error{Message: ErrCorrupt.Message, Permanent: true, Err: err}
	}
	return err
}

func thumbnail(ctx context.Context, pdf, workDir string, pages int) (string, error) {
	prefix := filepath.Join(workDir, "thumb")
	if _, err := run(ctx, Timeout(1), "pdftoppm", "-jpeg", "-f", "1", "-l", "1", "-scale-to-x", "400", "-scale-to-y", "-1", "-singlefile", pdf, prefix); err != nil {
		return "", err
	}
	return prefix + ".jpg", nil
}

// readText reads a text file as UTF-8, falling back to GB18030 (common for
// Chinese text saved on Windows).
func readText(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if utf8.Valid(b) {
		return string(b), nil
	}
	decoded, err := io.ReadAll(simplifiedchinese.GB18030.NewDecoder().Reader(bytes.NewReader(b)))
	if err != nil {
		return "", errors.Join(ErrCorrupt, err)
	}
	return string(decoded), nil
}

func countChars(s string) int {
	n := 0
	for _, r := range s {
		if r > ' ' {
			n++
		}
	}
	return n
}

// PDFPages counts a PDF's pages (pdfinfo).
func PDFPages(ctx context.Context, pdf string) (int, error) { return pageCount(ctx, pdf) }

// PDFText extracts a PDF's text layer; ocred reads OCR output in content
// order (see pdfText).
func PDFText(ctx context.Context, pdf string, pages int, ocred bool) (string, error) {
	return pdfText(ctx, pdf, pages, ocred)
}

// Thumbnail renders the first page of pdf as workDir/thumb.jpg.
func Thumbnail(ctx context.Context, pdf, workDir string) (string, error) {
	return thumbnail(ctx, pdf, workDir, 1)
}

// ErrWrongPassword: DecryptPDF was given the wrong password.
var ErrWrongPassword = errors.New("wrong PDF password")

// IsEncryptedPDF reports whether path is a password-protected PDF (false
// when qpdf is missing or the file is not a PDF; parsing then decides).
func IsEncryptedPDF(ctx context.Context, path string) bool {
	if _, err := exec.LookPath("qpdf"); err != nil {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	head := make([]byte, 1024)
	n, _ := f.Read(head)
	f.Close()
	if !bytes.Contains(head[:n], []byte("%PDF-")) {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err = exec.CommandContext(ctx, "qpdf", "--is-encrypted", path).Run()
	return err == nil // exit 0: encrypted, 2: not
}

// DecryptPDF writes an unprotected copy of src to dst. The password goes
// to qpdf on standard input, never on the command line.
func DecryptPDF(ctx context.Context, src, dst, password string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "qpdf", "--password-file=-", "--decrypt", src, dst)
	cmd.Stdin = strings.NewReader(password + "\n")
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &ee) && ee.ExitCode() == 3: // warnings only
		return nil
	case strings.Contains(strings.ToLower(string(out)), "password"):
		return ErrWrongPassword
	}
	return &Error{Message: ErrCorrupt.Message, Permanent: true, Err: err}
}

// ReadText is the text a file carries, read without recognising anything
// (v1.9.2, comparing versions): a PDF's text layer, a text file, the text
// of an Office file or an e-mail. ok is false when there is none to read
// (a scan, a photo, sound).
func ReadText(ctx context.Context, src, filename string) (text string, ok bool, err error) {
	mime, ext, err := Detect(src, filename)
	if err != nil {
		return "", false, err
	}
	switch {
	case mime == "text/plain" || mime == "text/markdown":
		text, err = readText(src)
	case mime == "application/pdf":
		pages, perr := pageCount(ctx, src)
		if perr != nil {
			return "", false, perr
		}
		if text, err = pdfText(ctx, src, pages, false); err == nil && countChars(text) < 20 {
			return "", false, nil // a scan
		}
	case IsOffice(mime):
		text = officeText(src, ext)
	case IsEmail(mime):
		msg, _, rerr := readEmail(src)
		if rerr != nil {
			return "", false, rerr
		}
		text = emailText(msg)
	default:
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return text, strings.TrimSpace(text) != "", nil
}
