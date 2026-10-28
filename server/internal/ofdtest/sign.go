package ofdtest

import (
	"bytes"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/big"
	"strings"
	"time"

	"kept/server/internal/sm2"
	"kept/server/internal/sm3"
)

// Electronic seals (v1.11.3): an invoice signed as the tax system's are,
// with a seal (GB/T 38540, or GM/T 0031's version 1), an SM2 key and a
// self-signed certificate, the files' SM3 digests in Signature.xml. What
// it signs can be changed afterwards, to test that the change shows.

// Signer is who signs, and how.
type Signer struct {
	Key      *sm2.PrivateKey
	Cert     []byte // DER
	SealName string
	Vector   bool // the seal's picture is an OFD of its own, else a PNG
	Version  int  // of the seal's data: 4 (the default) or 1
	Time     time.Time
	// Tamper changes the signed file afterwards: "amount" (the page),
	// "signature-xml", "signature-value", "extra-file", "missing-file".
	Tamper string
}

var oidSM2SM3 = asn1.ObjectIdentifier{1, 2, 156, 10197, 1, 501}

// NewSigner makes a key and a certificate for name of org, valid from
// 2020 to 2040.
func NewSigner(name, org, seal string) (*Signer, error) {
	k, err := sm2.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	cert, err := certificate(k, name, org)
	if err != nil {
		return nil, err
	}
	return &Signer{Key: k, Cert: cert, SealName: seal, Version: 4, Time: time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)}, nil
}

type spki struct {
	Algorithm pkix.AlgorithmIdentifier
	Key       asn1.BitString
}

type tbsCert struct {
	Version   int `asn1:"optional,explicit,default:0,tag:0"`
	Serial    *big.Int
	Algorithm pkix.AlgorithmIdentifier
	Issuer    asn1.RawValue
	Validity  struct{ NotBefore, NotAfter time.Time }
	Subject   asn1.RawValue
	PublicKey spki
}

type cert struct {
	TBS       asn1.RawValue
	Algorithm pkix.AlgorithmIdentifier
	Signature asn1.BitString
}

// certificate is a self-signed X.509 certificate of the SM2 key.
func certificate(k *sm2.PrivateKey, name, org string) ([]byte, error) {
	subject, err := asn1.Marshal(pkix.Name{CommonName: name, Organization: []string{org}}.ToRDNSequence())
	if err != nil {
		return nil, err
	}
	curve, _ := asn1.Marshal(asn1.ObjectIdentifier{1, 2, 156, 10197, 1, 301})
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	t := tbsCert{Version: 2, Serial: serial, Algorithm: pkix.AlgorithmIdentifier{Algorithm: oidSM2SM3},
		Issuer: asn1.RawValue{FullBytes: subject}, Subject: asn1.RawValue{FullBytes: subject},
		PublicKey: spki{pkix.AlgorithmIdentifier{Algorithm: asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}, Parameters: asn1.RawValue{FullBytes: curve}},
			asn1.BitString{Bytes: k.Bytes(), BitLength: 520}}}
	t.Validity.NotBefore = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	t.Validity.NotAfter = time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	tbs, err := asn1.Marshal(t)
	if err != nil {
		return nil, err
	}
	sig, err := sm2.SignASN1(nil, k, sm2.DefaultID, tbs)
	if err != nil {
		return nil, err
	}
	return asn1.Marshal(cert{asn1.RawValue{FullBytes: tbs}, pkix.AlgorithmIdentifier{Algorithm: oidSM2SM3}, asn1.BitString{Bytes: sig, BitLength: 8 * len(sig)}})
}

// the seal's data (GB/T 38540)
type sesHeader struct {
	ID      string `asn1:"ia5"`
	Version int
	Vid     string `asn1:"ia5"`
}

type sesProperty struct {
	Type         int
	Name         string `asn1:"utf8"`
	CertListType int
	CertList     [][]byte
	Created      time.Time `asn1:"generalized"`
	ValidStart   time.Time `asn1:"generalized"`
	ValidEnd     time.Time `asn1:"generalized"`
}

type sesPicture struct {
	Type   string `asn1:"ia5"`
	Data   []byte
	Width  int
	Height int
}

type sesSealInfo struct {
	Header   sesHeader
	ID       string `asn1:"ia5"`
	Property sesProperty
	Picture  sesPicture
}

type seSeal struct {
	Info      sesSealInfo
	Cert      []byte
	Algorithm asn1.ObjectIdentifier
	Signed    asn1.BitString
}

type tbsSign4 struct {
	Version  int
	Seal     seSeal
	Time     time.Time `asn1:"generalized"`
	DataHash asn1.BitString
	Property string `asn1:"ia5"`
}

type sesSignature4 struct {
	ToSign    asn1.RawValue
	Cert      []byte
	Algorithm asn1.ObjectIdentifier
	Signature asn1.BitString
}

type tbsSign1 struct {
	Version   int
	Seal      seSeal
	Time      asn1.BitString
	DataHash  asn1.BitString
	Property  string `asn1:"ia5"`
	Cert      []byte
	Algorithm asn1.ObjectIdentifier
}

type sesSignature1 struct {
	ToSign    asn1.RawValue
	Signature asn1.BitString
}

func bits(b []byte) asn1.BitString { return asn1.BitString{Bytes: b, BitLength: 8 * len(b)} }

// sealPicture is a red round seal: a PNG, or a one-page OFD of paths.
func (s *Signer) sealPicture() (string, []byte) {
	if !s.Vector {
		img := image.NewNRGBA(image.Rect(0, 0, 160, 160))
		for y := 0; y < 160; y++ {
			for x := 0; x < 160; x++ {
				dx, dy := float64(x-80), float64(y-80)
				r := dx*dx + dy*dy
				star := dx > -20 && dx < 20 && dy > -20 && dy < 20
				if r <= 78*78 && r >= 70*70 || star {
					img.Set(x, y, color.NRGBA{220, 20, 20, 255})
				}
			}
		}
		var b bytes.Buffer
		_ = png.Encode(&b, img)
		return "png", b.Bytes()
	}
	ring := `<ofd:PathObject ID="2" Boundary="0 0 40 40" LineWidth="1.5"><ofd:StrokeColor Value="220 20 20"/>` +
		`<ofd:AbbreviatedData>M 20 1 B 30.5 1 39 9.5 39 20 B 39 30.5 30.5 39 20 39 B 9.5 39 1 30.5 1 20 B 1 9.5 9.5 1 20 1 C</ofd:AbbreviatedData></ofd:PathObject>` +
		`<ofd:PathObject ID="3" Boundary="0 0 40 40" Fill="true" Stroke="false"><ofd:FillColor Value="220 20 20"/>` +
		`<ofd:AbbreviatedData>M 20 12 L 22.5 18 L 29 18 L 24 22 L 26 28 L 20 24.5 L 14 28 L 16 22 L 11 18 L 17.5 18 C</ofd:AbbreviatedData></ofd:PathObject>`
	files := []entry{
		{"OFD.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><ofd:OFD ` + ns + ` Version="1.1" DocType="OFD"><ofd:DocBody><ofd:DocInfo><ofd:DocID>seal</ofd:DocID></ofd:DocInfo><ofd:DocRoot>Doc_0/Document.xml</ofd:DocRoot></ofd:DocBody></ofd:OFD>`)},
		{"Doc_0/Document.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><ofd:Document ` + ns + `><ofd:CommonData><ofd:MaxUnitID>9</ofd:MaxUnitID><ofd:PageArea><ofd:PhysicalBox>0 0 40 40</ofd:PhysicalBox></ofd:PageArea></ofd:CommonData><ofd:Pages><ofd:Page ID="1" BaseLoc="Pages/Page_0/Content.xml"/></ofd:Pages></ofd:Document>`)},
		{"Doc_0/Pages/Page_0/Content.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><ofd:Page ` + ns + `><ofd:Content><ofd:Layer ID="1">` + ring + `</ofd:Layer></ofd:Content></ofd:Page>`)},
	}
	var b bytes.Buffer
	z := newZip(&b)
	for _, e := range files {
		z.add(e.name, e.data)
	}
	z.close()
	return "ofd", b.Bytes()
}

// seal is the electronic seal, signed by its maker (here the signer too).
func (s *Signer) seal() (seSeal, error) {
	typ, pic := s.sealPicture()
	info := sesSealInfo{Header: sesHeader{"ES", 4, "kept"}, ID: "kept-demo-seal",
		Property: sesProperty{Type: 1, Name: s.SealName, CertListType: 1, CertList: [][]byte{s.Cert},
			Created: s.Time.AddDate(-1, 0, 0), ValidStart: s.Time.AddDate(-1, 0, 0), ValidEnd: s.Time.AddDate(5, 0, 0)},
		Picture: sesPicture{typ, pic, 40, 40}}
	der, err := asn1.Marshal(info)
	if err != nil {
		return seSeal{}, err
	}
	sig, err := sm2.SignASN1(nil, s.Key, sm2.DefaultID, append(der, s.Cert...))
	if err != nil {
		return seSeal{}, err
	}
	return seSeal{info, s.Cert, oidSM2SM3, bits(sig)}, nil
}

// signedValue is SignedValue.dat: the signature of Signature.xml.
func (s *Signer) signedValue(signatureXML []byte) ([]byte, error) {
	seal, err := s.seal()
	if err != nil {
		return nil, err
	}
	hash := sm3.Sum(signatureXML)
	var tbs []byte
	if s.Version == 1 {
		tbs, err = asn1.Marshal(tbsSign1{1, seal, bits([]byte(s.Time.Format("20060102150405Z"))), bits(hash[:]), "/Doc_0/Signs/Sign_0/Signature.xml", s.Cert, oidSM2SM3})
	} else {
		tbs, err = asn1.Marshal(tbsSign4{4, seal, s.Time, bits(hash[:]), "/Doc_0/Signs/Sign_0/Signature.xml"})
	}
	if err != nil {
		return nil, err
	}
	sig, err := sm2.SignASN1(nil, s.Key, sm2.DefaultID, tbs)
	if err != nil {
		return nil, err
	}
	if s.Tamper == "signature-value" {
		sig[len(sig)-1] ^= 1
	}
	if s.Version == 1 {
		return asn1.Marshal(sesSignature1{asn1.RawValue{FullBytes: tbs}, bits(sig)})
	}
	return asn1.Marshal(sesSignature4{asn1.RawValue{FullBytes: tbs}, s.Cert, oidSM2SM3, bits(sig)})
}

// apply signs the package's files: Signature.xml lists their digests and
// where the seal goes; the seal's signature covers Signature.xml.
func (s *Signer) apply(files []entry, amount string) []entry {
	var refs strings.Builder
	for _, f := range files {
		if f.name == "OFD.xml" {
			continue // it names the signatures: not signed
		}
		sum := sm3.Sum(f.data)
		fmt.Fprintf(&refs, `<ofd:Reference FileRef="/%s"><ofd:CheckValue>%s</ofd:CheckValue></ofd:Reference>`, f.name, base64.StdEncoding.EncodeToString(sum[:]))
	}
	sigXML := []byte(`<?xml version="1.0" encoding="UTF-8"?><ofd:Signature ` + ns + `><ofd:SignedInfo>` +
		`<ofd:Provider ProviderName="收好演示签章" Company="收好" Version="1.0"/><ofd:SignatureMethod>1.2.156.10197.1.501</ofd:SignatureMethod>` +
		`<ofd:SignatureDateTime>` + s.Time.Format("20060102150405") + `</ofd:SignatureDateTime>` +
		`<ofd:References CheckMethod="1.2.156.10197.1.401">` + refs.String() + `</ofd:References>` +
		`<ofd:StampAnnot ID="1" PageRef="10" Boundary="150 92 40 40"/></ofd:SignedInfo>` +
		`<ofd:SignedValue>/Doc_0/Signs/Sign_0/SignedValue.dat</ofd:SignedValue></ofd:Signature>`)
	value, err := s.signedValue(sigXML)
	if err != nil {
		panic(err) // tests and the demo only
	}
	for i := range files {
		switch {
		case files[i].name == "OFD.xml":
			files[i].data = bytes.Replace(files[i].data, []byte("</ofd:DocRoot>"), []byte("</ofd:DocRoot><ofd:Signatures>Doc_0/Signs/Signatures.xml</ofd:Signatures>"), 1)
		case s.Tamper == "amount" && strings.HasPrefix(files[i].name, "Doc_0/Pages/Page_0/"):
			files[i].data = bytes.ReplaceAll(files[i].data, []byte(amount), []byte(amount+"0"))
		}
	}
	if s.Tamper == "signature-xml" {
		sigXML = bytes.Replace(sigXML, []byte("收好演示签章"), []byte("别人的签章"), 1)
	}
	files = append(files,
		entry{"Doc_0/Signs/Signatures.xml", []byte(`<?xml version="1.0" encoding="UTF-8"?><ofd:Signatures ` + ns + `><ofd:MaxSignId>1</ofd:MaxSignId><ofd:Signature ID="1" Type="Seal" BaseLoc="Sign_0/Signature.xml"/></ofd:Signatures>`)},
		entry{"Doc_0/Signs/Sign_0/Signature.xml", sigXML},
		entry{"Doc_0/Signs/Sign_0/SignedValue.dat", value})
	switch s.Tamper {
	case "extra-file":
		files = append(files, entry{"Doc_0/Res/added.png", stamp()})
	case "missing-file":
		for i, f := range files {
			if f.name == "Doc_0/Res/stamp.png" {
				files = append(files[:i], files[i+1:]...)
				break
			}
		}
	}
	return files
}
