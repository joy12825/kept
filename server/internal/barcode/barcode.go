// Package barcode finds barcodes on PDF pages: separator sheets that split
// one scan into several documents, and ASN labels (archive serial numbers)
// stuck on paper originals (v0.2).
package barcode

import (
	"context"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// MaxPages: longer PDFs are not scanned (rendering every page is slow).
const MaxPages = 200

// Options configure what the codes mean.
type Options struct {
	Separator string // a page with this code separates documents; default PATCHT
	ASNPrefix string // codes like ASN00123 carry an archive serial number; default ASN
	// DPI pages are rendered at (v1.6.5, KEPT_BARCODE_DPI); default 300.
	// A page where nothing is found is tried once more at twice that
	// (at most MaxDPI): small ASN labels on low-resolution scans.
	DPI int
}

const (
	DefaultDPI = 300
	MinDPI     = 72
	MaxDPI     = 600
)

func (o Options) withDefaults() Options {
	if o.Separator == "" {
		o.Separator = "PATCHT"
	}
	if o.ASNPrefix == "" {
		o.ASNPrefix = "ASN"
	}
	if o.DPI == 0 {
		o.DPI = DefaultDPI
	}
	return o
}

// Part is a run of pages (1-based, inclusive) that makes one document.
type Part struct {
	From, To int
	ASN      *int64 // from an ASN code on one of its pages
}

// Plan is what the codes in a PDF ask for: the documents to make.
type Plan struct {
	Parts      []Part
	Separators int // separator pages found (and dropped)
}

// Split reports whether the PDF should become more than one document.
func (p Plan) Split() bool { return len(p.Parts) > 1 }

// Scan renders each page of pdf (pages long) into workDir at opt.DPI and
// reads the barcodes on it; pages with none are rendered again at twice
// the resolution. It returns the codes per page (index 0 is page 1).
func Scan(ctx context.Context, pdf string, pages int, workDir string, opt Options) ([][]string, error) {
	opt = opt.withDefaults()
	if pages <= 0 || pages > MaxPages {
		return nil, nil
	}
	dir, err := os.MkdirTemp(workDir, "barcodes-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(10+pages*3)*time.Second)
	defer cancel()
	prefix := filepath.Join(dir, "p")
	if out, err := exec.CommandContext(ctx, "pdftoppm", "-gray", "-r", strconv.Itoa(opt.DPI), "-png", pdf, prefix).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %v: %s", err, strings.TrimSpace(string(out)))
	}
	files, err := filepath.Glob(prefix + "-*.png")
	if err != nil {
		return nil, err
	}
	// pdftoppm pads page numbers to the width of the page count.
	sort.Slice(files, func(i, j int) bool { return pageNumber(files[i]) < pageNumber(files[j]) })
	codes := make([][]string, pages)
	for _, f := range files {
		n := pageNumber(f)
		if n < 1 || n > pages {
			continue
		}
		img, err := load(f)
		if err != nil {
			continue
		}
		codes[n-1] = Read(img)
	}
	if again := min(opt.DPI*2, MaxDPI); again > opt.DPI {
		for i := range codes {
			if len(codes[i]) > 0 || ctx.Err() != nil {
				continue
			}
			codes[i] = readPage(ctx, pdf, i+1, again, filepath.Join(dir, "again"))
		}
	}
	return codes, nil
}

// readPage renders one page at dpi and reads it; nothing on failure.
func readPage(ctx context.Context, pdf string, page, dpi int, prefix string) []string {
	n := strconv.Itoa(page)
	if err := exec.CommandContext(ctx, "pdftoppm", "-gray", "-r", strconv.Itoa(dpi), "-f", n, "-l", n, "-singlefile", "-png", pdf, prefix).Run(); err != nil {
		return nil
	}
	defer os.Remove(prefix + ".png")
	img, err := load(prefix + ".png")
	if err != nil {
		return nil
	}
	return Read(img)
}

var numRE = regexp.MustCompile(`-(\d+)\.png$`)

func pageNumber(path string) int {
	m := numRE.FindStringSubmatch(path)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func load(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// Read returns the QR and Code 128 codes found on an image.
func Read(img image.Image) []string {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return nil
	}
	hints := map[gozxing.DecodeHintType]interface{}{gozxing.DecodeHintType_TRY_HARDER: true}
	var out []string
	for _, r := range []gozxing.Reader{qrcode.NewQRCodeReader(), oned.NewCode128Reader()} {
		res, err := r.Decode(bmp, hints)
		if err == nil && res.GetText() != "" {
			out = append(out, res.GetText())
		}
	}
	return out
}

// Analyze turns the codes per page into a plan: separator pages split the
// PDF and are dropped (empty runs are skipped); an ASN code gives its run
// an archive serial number.
func Analyze(codes [][]string, pages int, opt Options) Plan {
	opt = opt.withDefaults()
	var plan Plan
	cur := Part{From: 1}
	flush := func(to int) {
		if to >= cur.From {
			cur.To = to
			plan.Parts = append(plan.Parts, cur)
		}
	}
	for i := 0; i < pages; i++ {
		page := i + 1
		var pc []string
		if i < len(codes) {
			pc = codes[i]
		}
		separator := false
		for _, c := range pc {
			c = strings.TrimSpace(c)
			if strings.EqualFold(c, opt.Separator) {
				separator = true
			} else if n, ok := asn(c, opt.ASNPrefix); ok && cur.ASN == nil {
				cur.ASN = &n
			}
		}
		if separator {
			plan.Separators++
			flush(page - 1)
			cur = Part{From: page + 1}
		}
	}
	flush(pages)
	if len(plan.Parts) == 0 { // nothing but separators: keep the file whole
		plan.Parts = []Part{{From: 1, To: pages}}
	}
	return plan
}

// asn reads an archive serial number from a code like ASN00123.
func asn(code, prefix string) (int64, bool) {
	if !strings.HasPrefix(strings.ToUpper(code), strings.ToUpper(prefix)) {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimLeft(code[len(prefix):], " -:"), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
