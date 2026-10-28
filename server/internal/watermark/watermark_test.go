package watermark

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
)

func TestApply(t *testing.T) {
	if !Available() {
		t.Skip("no Chinese font, qpdf or pdfinfo")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "in.pdf")
	p := fpdf.New("P", "pt", "A4", "")
	p.SetFont("Helvetica", "", 14)
	p.AddPage()
	p.Text(50, 80, "Page one")
	p.AddPageFormat("L", fpdf.SizeType{Wd: 595, Ht: 842})
	p.Text(50, 80, "Page two")
	if err := p.OutputFileAndClose(src); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "out.pdf")
	if err := Apply(context.Background(), src, dst, dir, "仅供办理房贷使用", "仅供办理房贷使用 · 分享于 2026-10-15"); err != nil {
		t.Fatal(err)
	}
	sizes, err := pageSizes(context.Background(), dst)
	if err != nil || len(sizes) != 2 || sizes[1].w <= sizes[1].h {
		t.Fatalf("pages = %v %v", sizes, err)
	}
	for _, page := range []string{"1", "2"} {
		out, err := exec.Command("pdftotext", "-f", page, "-l", page, dst, "-").Output()
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(string(out), " ", "")
		if !strings.Contains(text, "仅供办理房贷使用") || !strings.Contains(text, "分享于2026-10-15") || !strings.Contains(text, "Page") {
			t.Errorf("page %s text = %q", page, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "watermark-overlay.pdf")); !os.IsNotExist(err) {
		t.Error("overlay left behind")
	}
}

func TestFontSetting(t *testing.T) {
	defer SetFont("")
	SetFont("/nonexistent/font.ttf")
	if Font() != "" || Available() {
		t.Error("a missing font is not available")
	}
}
