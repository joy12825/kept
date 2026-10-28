package parser

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
)

// Why a file has no PDF to show (v1.9.7, versions side by side).
var (
	ErrNoConverter = errors.New("parser: Office and e-mail need the conversion service")
	ErrNoPDFForm   = errors.New("parser: this kind of file is not made into a PDF")
)

// ToPDF makes the PDF a file is shown as, in workDir: a PDF is itself; an
// Office file or an e-mail is converted by Gotenberg, an OFD drawn here. It
// is what the current version's searchable PDF is made from, without the
// recognition.
func ToPDF(ctx context.Context, gotenberg, src, filename, workDir string) (string, error) {
	mime, ext, err := Detect(src, filename)
	if err != nil {
		return "", err
	}
	switch {
	case mime == "application/pdf":
		return src, nil
	case mime == ofdType:
		d, err := readOFD(src)
		if err != nil {
			return "", err
		}
		defer d.Close()
		var b bytes.Buffer
		if err := d.PDF(&b); err != nil {
			if errors.Is(err, errNoFont) {
				return "", ErrNoPDFForm
			}
			return "", err
		}
		dst := filepath.Join(workDir, "ofd.pdf")
		return dst, os.WriteFile(dst, b.Bytes(), 0o600)
	case IsEmail(mime), IsOffice(mime):
		if gotenberg == "" {
			return "", ErrNoConverter
		}
		if IsEmail(mime) {
			pdf, _, err := convertEmail(ctx, gotenberg, src, workDir)
			return pdf, err
		}
		return convertOffice(ctx, gotenberg, src, ext, workDir)
	}
	return "", ErrNoPDFForm
}
