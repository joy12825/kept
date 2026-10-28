// Package pdftest makes small PDFs for tests.
package pdftest

import (
	"fmt"
	"strings"
)

// Text builds a PDF with one page per label, each showing its label in
// Helvetica: a born-digital PDF with a text layer, no tools needed.
func Text(labels ...string) []byte {
	n := len(labels)
	objs := []string{"<< /Type /Catalog /Pages 2 0 R >>", ""}
	var kids []string
	for i, l := range labels {
		page, content := 3+2*i, 4+2*i
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
		objs = append(objs, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents %d 0 R /Resources << /Font << /F1 %d 0 R >> >> >>", content, 3+2*n))
		s := fmt.Sprintf("BT /F1 24 Tf 50 100 Td (%s) Tj ET", l)
		objs = append(objs, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(s), s))
	}
	objs = append(objs, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n)
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return []byte(b.String())
}
