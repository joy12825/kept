package parser

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"kept/server/internal/ofdtest"
)

func signedInvoice(t *testing.T, tweak func(s *ofdtest.Signer)) string {
	t.Helper()
	s, err := ofdtest.NewSigner("王小明", "上海某某餐饮有限公司", "上海某某餐饮有限公司发票专用章")
	if err != nil {
		t.Fatal(err)
	}
	if tweak != nil {
		tweak(s)
	}
	in := invoice
	in.Sign = s
	return writeFile(t, "signed.ofd", in.Build())
}

func TestOFDSignatureValid(t *testing.T) {
	for _, version := range []int{4, 1} {
		src := signedInvoice(t, func(s *ofdtest.Signer) { s.Version = version })
		sigs, err := ReadOFDSignatures(src)
		if err != nil || len(sigs) != 1 {
			t.Fatalf("v%d: %v %v", version, sigs, err)
		}
		s := sigs[0]
		if s.Status != SignValid || s.Reason != "" || s.Version != version || !s.SelfSigned || !s.CertInSeal {
			t.Fatalf("v%d: %s %s (version %d)", version, s.Status, s.Reason, s.Version)
		}
		if s.SealName != "上海某某餐饮有限公司发票专用章" || s.Signer != "王小明" || s.Org != "上海某某餐饮有限公司" || s.Issuer != "王小明" {
			t.Errorf("v%d: seal %q signer %q org %q issuer %q", version, s.SealName, s.Signer, s.Org, s.Issuer)
		}
		if !s.Time.Equal(time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)) || s.OutsideValidity || s.ValidTo.Year() != 2040 {
			t.Errorf("v%d: time %v, outside %v, valid to %v", version, s.Time, s.OutsideValidity, s.ValidTo)
		}
		if s.Page != 1 || s.Boundary != [4]float64{150, 92, 40, 40} || len(s.Files) != 7 || len(s.Unsigned) != 0 {
			t.Errorf("v%d: page %d %v, files %d, unsigned %v", version, s.Page, s.Boundary, len(s.Files), s.Unsigned)
		}
		for _, f := range s.Files {
			if !f.OK {
				t.Errorf("v%d: %s", version, f.Path)
			}
		}
		img, mime, err := OFDSeal(src, 1)
		if err != nil || mime != "image/png" || !bytes.HasPrefix(img, []byte("\x89PNG")) {
			t.Errorf("v%d: seal picture %s %v", version, mime, err)
		}
	}
}

func TestOFDSignatureChanged(t *testing.T) {
	for tamper, want := range map[string]struct{ status, reason string }{
		"amount":          {SignChanged, signFileChanged},
		"signature-xml":   {SignChanged, signTextChanged},
		"signature-value": {SignChanged, signBad},
		"missing-file":    {SignChanged, signFileMissing},
		"extra-file":      {SignValid, ""},
	} {
		src := signedInvoice(t, func(s *ofdtest.Signer) { s.Tamper = tamper })
		sigs, err := ReadOFDSignatures(src)
		if err != nil || len(sigs) != 1 {
			t.Fatalf("%s: %v", tamper, err)
		}
		if s := sigs[0]; s.Status != want.status || s.Reason != want.reason {
			t.Errorf("%s: %s %q", tamper, s.Status, s.Reason)
		}
		if tamper == "amount" {
			changed := 0
			for _, f := range sigs[0].Files {
				if !f.OK {
					changed++
					if !strings.Contains(f.Path, "Page_0") {
						t.Errorf("changed: %s", f.Path)
					}
				}
			}
			if changed != 1 {
				t.Errorf("changed files: %d", changed)
			}
		}
		if tamper == "extra-file" && (len(sigs[0].Unsigned) != 1 || sigs[0].Unsigned[0] != "Doc_0/Res/added.png") {
			t.Errorf("unsigned: %v", sigs[0].Unsigned)
		}
	}
}

func TestOFDWithoutSignatures(t *testing.T) {
	sigs, err := ReadOFDSignatures(writeFile(t, "a.ofd", invoice.Build()))
	if err != nil || len(sigs) != 0 {
		t.Errorf("%v %v", sigs, err)
	}
}

// rewrite changes one file of a package.
func rewrite(t *testing.T, src, name string, fn func([]byte) []byte) string {
	t.Helper()
	z, err := zip.OpenReader(src)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, f := range z.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		if f.Name == name {
			data = fn(data)
		}
		x, _ := w.Create(f.Name)
		x.Write(data)
	}
	w.Close()
	return writeFile(t, "rewritten.ofd", b.Bytes())
}

func TestOFDSignatureUnknownAndDamaged(t *testing.T) {
	src := signedInvoice(t, nil)
	// a signature algorithm other than SM2
	other := rewrite(t, src, "Doc_0/Signs/Sign_0/SignedValue.dat", func(b []byte) []byte {
		return bytes.Replace(b, []byte{0x2A, 0x81, 0x1C, 0xCF, 0x55, 0x01, 0x83, 0x75}, []byte{0x2A, 0x86, 0x48, 0x86, 0xF7, 0x0D, 0x01, 0x01}, -1) // 1.2.156.10197.1.501 → 1.2.840.113549.1.1
	})
	if s, _ := ReadOFDSignatures(other); len(s) != 1 || s[0].Status != SignUnknown || s[0].Reason != signAlgorithm {
		t.Errorf("RSA: %+v", s)
	}
	// the signer's name changed in a self-signed certificate
	renamed := rewrite(t, src, "Doc_0/Signs/Sign_0/SignedValue.dat", func(b []byte) []byte {
		i := bytes.LastIndex(b, []byte("王小明"))
		return bytes.Join([][]byte{b[:i], []byte("李小明"), b[i+len("王小明"):]}, nil)
	})
	if s, _ := ReadOFDSignatures(renamed); s[0].Status != SignChanged || s[0].Reason != signCertList {
		t.Errorf("renamed: %s %q", s[0].Status, s[0].Reason)
	}
	// both names changed: the self-signed certificate's signature fails too
	both := rewrite(t, src, "Doc_0/Signs/Sign_0/SignedValue.dat", func(b []byte) []byte {
		i := bytes.LastIndex(b, []byte("王小明"))
		j := bytes.LastIndex(b[:i], []byte("王小明"))
		b = bytes.Clone(b)
		copy(b[i:], "李小明")
		copy(b[j:], "李小明")
		return b
	})
	if s, _ := ReadOFDSignatures(both); s[0].Status != SignChanged || s[0].Reason != signCert {
		t.Errorf("both names: %s %q", s[0].Status, s[0].Reason)
	}
	// a digest not known
	digest := rewrite(t, src, "Doc_0/Signs/Sign_0/Signature.xml", func(b []byte) []byte {
		return bytes.Replace(b, []byte(`CheckMethod="1.2.156.10197.1.401"`), []byte(`CheckMethod="1.2.3.4"`), 1)
	})
	if s, _ := ReadOFDSignatures(digest); len(s) != 1 || s[0].Status != SignChanged { // Signature.xml changed too
		t.Errorf("digest: %+v", s[0].Status)
	}
	// a signature without a seal
	pkcs7 := rewrite(t, src, "Doc_0/Signs/Signatures.xml", func(b []byte) []byte { return bytes.Replace(b, []byte(`Type="Seal"`), []byte(`Type="Sign"`), 1) })
	if s, _ := ReadOFDSignatures(pkcs7); s[0].Status != SignUnknown || s[0].Reason != signPKCS7 {
		t.Errorf("pkcs7: %+v", s[0])
	}
	// damaged signature values never crash the reader
	rnd := rand.New(rand.NewSource(2))
	for i := 0; i < 400; i++ {
		bad := rewrite(t, src, "Doc_0/Signs/Sign_0/SignedValue.dat", func(b []byte) []byte {
			b = bytes.Clone(b)
			// within what is signed: the certificate after it (version 4) is
			// protected by its issuer, which is not checked here
			for k := 0; k < 1+rnd.Intn(6); k++ {
				b[rnd.Intn(len(b)-700)] = byte(rnd.Intn(256))
			}
			if rnd.Intn(4) == 0 {
				b = b[:rnd.Intn(len(b))]
			}
			return b
		})
		s, err := ReadOFDSignatures(bad)
		if err != nil || len(s) != 1 || s[0].Status == SignValid {
			t.Fatalf("damaged %d: %+v %v", i, s, err)
		}
	}
}

// The seal is drawn where it was stamped: a picture, or a vector seal.
func TestOFDSealDrawn(t *testing.T) {
	if len(MissingTools()) > 0 {
		t.Skip("needs poppler")
	}
	for _, vector := range []bool{false, true} {
		src := signedInvoice(t, func(s *ofdtest.Signer) { s.Vector = vector })
		dir := t.TempDir()
		var res Result
		if err := parseOFD(src, dir, &res); err != nil || res.ArchivePath == "" {
			t.Skipf("no archive (no Chinese font?): %v %v", err, res.Warnings)
		}
		out := filepath.Join(dir, "page")
		if err := exec.Command("pdftoppm", "-r", "40", "-png", "-f", "1", "-l", "1", res.ArchivePath, out).Run(); err != nil {
			t.Fatal(err)
		}
		matches, _ := filepath.Glob(out + "*.png")
		if len(matches) == 0 {
			t.Fatal("no page")
		}
		img := readPNG(t, matches[0])
		// red pixels inside the seal's boundary (150 92 40 40 mm of 210 × 140)
		b := img.Bounds()
		red := 0
		for y := b.Dy() * 92 / 140; y < b.Dy()*132/140; y++ {
			for x := b.Dx() * 150 / 210; x < b.Dx()*190/210; x++ {
				r, g, bb, _ := img.At(x, y).RGBA()
				if r>>8 > 180 && g>>8 < 90 && bb>>8 < 90 {
					red++
				}
			}
		}
		if red < 20 {
			t.Errorf("vector %v: the seal is not there (%d red pixels)", vector, red)
		}
		if os.Getenv("SEAL_OUT") != "" {
			data, _ := os.ReadFile(matches[0])
			os.WriteFile(os.Getenv("SEAL_OUT")+map[bool]string{false: "-png.png", true: "-ofd.png"}[vector], data, 0o644)
		}
	}
}

func readPNG(t *testing.T, p string) image.Image {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}
