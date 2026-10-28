package barcode

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"kept/server/internal/barcode/barcodetest"
)

func p64(n int64) *int64 { return &n }

func TestAnalyze(t *testing.T) {
	cases := []struct {
		name  string
		codes [][]string
		pages int
		want  Plan
	}{
		{"no codes", nil, 3, Plan{Parts: []Part{{From: 1, To: 3}}}},
		{"one separator", [][]string{nil, {"PATCHT"}, nil, nil}, 4, Plan{Parts: []Part{{From: 1, To: 1}, {From: 3, To: 4}}, Separators: 1}},
		{"leading and doubled separators leave no empty parts", [][]string{{"PATCHT"}, nil, {"patcht"}, {"PATCHT"}, nil}, 5, Plan{Parts: []Part{{From: 2, To: 2}, {From: 5, To: 5}}, Separators: 3}},
		{"asn per part", [][]string{{"ASN00123"}, {"PATCHT"}, {"ASN-7"}, {"ASN99"}}, 4, Plan{Parts: []Part{{From: 1, To: 1, ASN: p64(123)}, {From: 3, To: 4, ASN: p64(7)}}, Separators: 1}},
		{"only separators keep the file", [][]string{{"PATCHT"}}, 1, Plan{Parts: []Part{{From: 1, To: 1}}, Separators: 1}},
		{"other codes are ignored", [][]string{{"https://example.com"}, {"ASNx"}}, 2, Plan{Parts: []Part{{From: 1, To: 2}}}},
	}
	for _, c := range cases {
		if got := Analyze(c.codes, c.pages, Options{}); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
	if got := Analyze([][]string{nil, {"SPLIT"}, {"NO.42"}}, 3, Options{Separator: "SPLIT", ASNPrefix: "NO."}); len(got.Parts) != 2 || got.Parts[1].ASN == nil || *got.Parts[1].ASN != 42 {
		t.Errorf("custom codes: %+v", got)
	}
}

func TestScanFindsSeparatorAndASN(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("needs pdftoppm")
	}
	pdf := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(pdf, barcodetest.PDF(barcodetest.Blank(), barcodetest.QR("PATCHT"), barcodetest.Code128("ASN00123"), barcodetest.Blank()), 0o644); err != nil {
		t.Fatal(err)
	}
	codes, err := Scan(context.Background(), pdf, 4, t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 4 || len(codes[0]) != 0 || len(codes[1]) != 1 || codes[1][0] != "PATCHT" || len(codes[2]) != 1 || codes[2][0] != "ASN00123" {
		t.Fatalf("codes = %q", codes)
	}
	plan := Analyze(codes, 4, Options{})
	if !plan.Split() || plan.Parts[0] != (Part{From: 1, To: 1}) || plan.Parts[1].From != 3 || *plan.Parts[1].ASN != 123 {
		t.Errorf("plan = %+v", plan)
	}
}

// v1.6.5: a low resolution still finds the codes, rendering the pages
// where nothing was found again at twice the resolution.
func TestScanRetriesAtHigherResolution(t *testing.T) {
	if _, err := exec.LookPath("pdftoppm"); err != nil {
		t.Skip("needs pdftoppm")
	}
	pdf := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(pdf, barcodetest.PDF(barcodetest.Code128("ASN00042"), barcodetest.Blank()), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, dpi := range []int{MinDPI, 150, DefaultDPI} {
		codes, err := Scan(context.Background(), pdf, 2, t.TempDir(), Options{DPI: dpi})
		if err != nil {
			t.Fatal(err)
		}
		if len(codes) != 2 || len(codes[0]) != 1 || codes[0][0] != "ASN00042" || len(codes[1]) != 0 {
			t.Errorf("at %d dpi: codes = %q", dpi, codes)
		}
	}
	if got := readPage(context.Background(), pdf, 1, 144, filepath.Join(t.TempDir(), "p")); len(got) != 1 {
		t.Errorf("readPage = %q", got)
	}
}
