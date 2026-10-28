// Package watermark stamps a line of text on every page of a PDF (v1.9.2,
// shares): fpdf draws a page of watermark for each page, at its size, and
// qpdf lays them over the original. The original file is not changed.
package watermark

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/go-pdf/fpdf"
)

// fontCandidates are TrueType fonts with Chinese: the Docker image installs
// the first (fonts-droid-fallback); the others are where a desktop has one.
var fontCandidates = []string{
	"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf",
	"/usr/share/fonts/truetype/arphic-gbsn00lp/gbsn00lp.ttf",
	"/usr/share/fonts/truetype/wqy/wqy-microhei.ttf",
	"/Library/Fonts/Arial Unicode.ttf",
	"/System/Library/Fonts/Supplementary/Arial Unicode.ttf",
}

var (
	fontMu   sync.Mutex
	fontPath string // set by SetFont, or found
	fontData []byte
)

// SetFont uses the font at path (KEPT_WATERMARK_FONT); "" looks for one.
func SetFont(path string) {
	fontMu.Lock()
	defer fontMu.Unlock()
	fontPath, fontData = path, nil
}

// Font is the font file used, or "" when there is none.
func Font() string {
	fontMu.Lock()
	defer fontMu.Unlock()
	return findFont()
}

func findFont() string {
	if fontPath != "" {
		if _, err := os.Stat(fontPath); err == nil {
			return fontPath
		}
		return ""
	}
	for _, p := range fontCandidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func font() ([]byte, error) {
	fontMu.Lock()
	defer fontMu.Unlock()
	if fontData != nil {
		return fontData, nil
	}
	p := findFont()
	if p == "" {
		return nil, ErrUnavailable
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	fontData = b
	return b, nil
}

// ErrUnavailable: no font with Chinese, or no qpdf or pdfinfo.
var ErrUnavailable = errors.New("watermark: no Chinese font or no qpdf")

// Available reports whether watermarks can be made here.
func Available() bool {
	if Font() == "" {
		return false
	}
	for _, tool := range []string{"qpdf", "pdfinfo"} {
		if _, err := exec.LookPath(tool); err != nil {
			return false
		}
	}
	return true
}

// MaxText is the longest watermark, in characters.
const MaxText = 40

type pageSize struct{ w, h float64 }

var sizeRE = regexp.MustCompile(`(?m)^Page\s+(\d+) size:\s+([\d.]+) x ([\d.]+) pts`)

// pageSizes reads every page's size from pdfinfo.
func pageSizes(ctx context.Context, src string) ([]pageSize, error) {
	cctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := exec.CommandContext(cctx, "pdfinfo", "-f", "1", "-l", "100000", src).Output()
	if err != nil {
		return nil, fmt.Errorf("pdfinfo: %w", err)
	}
	var sizes []pageSize
	for _, m := range sizeRE.FindAllSubmatch(out, -1) {
		w, _ := strconv.ParseFloat(string(m[2]), 64)
		h, _ := strconv.ParseFloat(string(m[3]), 64)
		sizes = append(sizes, pageSize{w, h})
	}
	if len(sizes) == 0 {
		return nil, errors.New("pdfinfo: no pages")
	}
	return sizes, nil
}

// overlay draws one page of watermark per page size: the text tiled
// diagonally in light grey, and a small line at the foot with footer.
func overlay(sizes []pageSize, text, footer string) ([]byte, error) {
	fb, err := font()
	if err != nil {
		return nil, err
	}
	pdf := fpdf.NewCustom(&fpdf.InitType{UnitStr: "pt", Size: fpdf.SizeType{Wd: sizes[0].w, Ht: sizes[0].h}})
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetCompression(true)
	pdf.AddUTF8FontFromBytes("wm", "", fb)
	for _, s := range sizes {
		pdf.AddPageFormat("P", fpdf.SizeType{Wd: s.w, Ht: s.h})
		size := math.Max(14, math.Min(s.w, s.h)/18)
		pdf.SetFont("wm", "", size)
		pdf.SetTextColor(110, 110, 110)
		pdf.SetAlpha(0.13, "Normal")
		tw := pdf.GetStringWidth(text)
		cx, cy := s.w/2, s.h/2
		diag := math.Hypot(s.w, s.h)
		stepX, stepY := tw+size*3, size*5
		pdf.TransformBegin()
		pdf.TransformRotate(30, cx, cy)
		row := 0
		for y := -diag / 2; y <= diag/2; y += stepY {
			shift := float64(row%2) * stepX / 2 // brick pattern
			for x := -diag/2 - shift; x <= diag/2; x += stepX {
				pdf.Text(cx+x, cy+y, text)
			}
			row++
		}
		pdf.TransformEnd()
		pdf.SetAlpha(0.6, "Normal")
		pdf.SetFont("wm", "", 8)
		fw := pdf.GetStringWidth(footer)
		pdf.Text((s.w-fw)/2, s.h-12, footer)
		pdf.SetAlpha(1, "Normal")
	}
	var b bytes.Buffer
	if err := pdf.Output(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Apply writes src with the watermark text (and footer, at the foot of
// each page) to dst. workDir holds the overlay.
func Apply(ctx context.Context, src, dst, workDir, text, footer string) error {
	if !Available() {
		return ErrUnavailable
	}
	sizes, err := pageSizes(ctx, src)
	if err != nil {
		return err
	}
	ov, err := overlay(sizes, text, footer)
	if err != nil {
		return err
	}
	ovPath := filepath.Join(workDir, "watermark-overlay.pdf")
	if err := os.WriteFile(ovPath, ov, 0o600); err != nil {
		return err
	}
	defer os.Remove(ovPath)
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(cctx, "qpdf", src, "--overlay", ovPath, "--to=1-z", "--from=1-z", "--", dst)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 3) { // 3: warnings only
		return fmt.Errorf("qpdf: %w: %s", err, bytes.TrimSpace(out))
	}
	return nil
}

// FontData is the Chinese font's file (v1.9.3: OFD pages are drawn with
// it too); ErrUnavailable when there is none.
func FontData() ([]byte, error) { return font() }
