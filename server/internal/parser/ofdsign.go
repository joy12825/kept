package parser

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"errors"
	"hash"
	"math/big"
	"path"
	"sort"
	"strings"
	"time"

	"kept/server/internal/i18n"
	"kept/server/internal/sm2"
	"kept/server/internal/sm3"
)

// The signatures of an OFD (v1.11.3): GB/T 33190 chapter 18 for where they
// are, GB/T 38540 (version 4) and GM/T 0031 (version 1) for the electronic
// seal's data, SM2 and SM3 for the cryptography. Each is checked: the
// files it covers are as signed, the signed text (Signature.xml) is the
// one hashed, and the SM2 signature holds for the signer's certificate.
// Who issued the certificate is not checked: there are no root
// certificates here.

// Signature statuses.
const (
	SignValid   = "valid"   // as signed
	SignChanged = "changed" // a file or the signature changed since
	SignUnknown = "unknown" // cannot be checked here
)

// OFDSignature is one signature of an OFD and what checking it found.
type OFDSignature struct {
	Type   string // Seal, or Sign (a signature without a seal)
	Status string
	Reason string // why it is not valid, a sentence of the catalog
	// where the seal is: a page (from 1; 0 for none) and its boundary, mm
	Page     int
	Boundary [4]float64
	Time     time.Time
	Provider string
	SealName string
	Signer   string // the certificate's subject: common name
	Org      string // and organisation
	Issuer   string
	// the certificate's validity, and whether the signing time is outside it
	ValidFrom, ValidTo time.Time
	OutsideValidity    bool
	// SelfSigned: the certificate is issued by its own key, and that was
	// checked; other issuers' signatures are not.
	SelfSigned bool
	// CertInSeal: the certificate is one the seal lists (in what is
	// signed), so it is the one the signer used.
	CertInSeal bool
	Version    int // of the seal's data: 1 or 4; 0 not read
	Files      []SignedFile
	Unsigned   []string // files of the package the signatures do not cover

	seal     []byte
	sealType string // png, jpg, ofd …
}

// SignedFile is a file a signature covers.
type SignedFile struct {
	Path    string
	OK      bool
	Missing bool
}

// Reasons a signature is not valid (shown to people).
var (
	signFileChanged = i18n.N("签名以后，被签的文件有改动")
	signFileMissing = i18n.N("被签的文件不见了")
	signTextChanged = i18n.N("签名的原文（Signature.xml）有改动")
	signBad         = i18n.N("签名与证书对不上，签名值可能被改过")
	signNoValue     = i18n.N("找不到签名值")
	signUnreadable  = i18n.N("签名值读不出来")
	signAlgorithm   = i18n.N("签名算法不是 SM2，暂时不能验证")
	signDigest      = i18n.N("摘要算法不认识，暂时不能验证")
	signPKCS7       = i18n.N("这是不带印章的数字签名，暂时不能验证")
	signCert        = i18n.N("签章人的证书有改动")
	signCertList    = i18n.N("签章人的证书不是印章登记的证书")
)

var oidSM2SM3 = asn1.ObjectIdentifier{1, 2, 156, 10197, 1, 501}

// checkHash is the hash a Reference's CheckMethod names.
func checkHash(method string) hash.Hash {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "", "SM3", "1.2.156.10197.1.401":
		return sm3.New()
	case "SHA256", "SHA-256", "2.16.840.1.101.3.4.2.1":
		return sha256.New()
	case "SHA1", "SHA-1", "1.3.14.3.2.26":
		return sha1.New()
	case "MD5", "1.2.840.113549.2.5":
		return md5.New()
	}
	return nil
}

// ReadOFDSignatures checks the signatures of the OFD at src (none when
// it has none).
func ReadOFDSignatures(src string) ([]OFDSignature, error) {
	d, err := readOFD(src)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	return d.signatures(), nil
}

// OFDSeal returns the picture of the n-th signature's seal (from 1) and
// its type, when a browser shows it (PNG or JPEG).
func OFDSeal(src string, n int) ([]byte, string, error) {
	sigs, err := ReadOFDSignatures(src)
	if err != nil {
		return nil, "", err
	}
	if n < 1 || n > len(sigs) {
		return nil, "", ErrOFD
	}
	s := sigs[n-1]
	switch sealFormat(s.seal, s.sealType) {
	case "png":
		return s.seal, "image/png", nil
	case "jpg":
		return s.seal, "image/jpeg", nil
	}
	return nil, "", ErrOFD
}

// sealFormat tells a seal picture's format by its content, else its type.
func sealFormat(b []byte, typ string) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG")):
		return "png"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8}):
		return "jpg"
	case bytes.HasPrefix(b, []byte("PK\x03\x04")):
		return "ofd"
	}
	return strings.ToLower(typ)
}

func (d *ofdDoc) signatures() []OFDSignature {
	if d.signs == "" {
		return nil
	}
	list, err := d.xml(d.signs)
	if err != nil {
		return nil
	}
	var out []OFDSignature
	covered := map[string]bool{}
	signDir := path.Dir(d.signs) + "/"
	for _, s := range list.all("Signature") {
		if len(out) >= 50 {
			break
		}
		loc := strings.TrimPrefix(resolve(d.signs, s.attr("BaseLoc")), "/")
		sig := d.signature(loc, s.attr("Type"))
		for _, f := range sig.Files {
			covered[f.Path] = true
		}
		out = append(out, sig)
	}
	// files added after signing (outside the signatures' own folder)
	var extra []string
	for name := range d.files {
		if name == "OFD.xml" || strings.HasPrefix(name, signDir) || strings.HasSuffix(name, "/") || covered[name] {
			continue
		}
		extra = append(extra, name)
	}
	sort.Strings(extra)
	if len(extra) > 0 && len(covered) > 0 {
		for i := range out {
			out[i].Unsigned = extra
		}
	}
	return out
}

// signature reads and checks one signature, its Signature.xml at loc.
func (d *ofdDoc) signature(loc, typ string) OFDSignature {
	out := OFDSignature{Type: typ, Status: SignValid}
	if out.Type == "" {
		out.Type = "Seal"
	}
	fail := func(status, reason string) {
		if out.Status == SignValid || status == SignChanged && out.Status == SignUnknown {
			out.Status, out.Reason = status, reason
		}
	}
	raw, err := d.open(loc)
	if err != nil {
		fail(SignChanged, signNoValue)
		return out
	}
	x, err := d.xml(loc)
	if err != nil {
		fail(SignUnknown, signUnreadable)
		return out
	}
	si := x.kid("SignedInfo")
	if si == nil {
		fail(SignUnknown, signUnreadable)
		return out
	}
	if p := si.kid("Provider"); p != nil {
		out.Provider = strings.TrimSpace(p.attr("Company") + " " + p.attr("ProviderName"))
	}
	if t := si.kid("SignatureDateTime"); t != nil {
		out.Time = signTime(t.Text)
	}
	// the files it covers
	if refs := si.kid("References"); refs != nil {
		method := refs.attr("CheckMethod")
		for _, r := range refs.all("Reference") {
			name := strings.TrimPrefix(resolve(loc, r.attr("FileRef")), "/")
			f := SignedFile{Path: name}
			h := checkHash(method)
			want, werr := base64.StdEncoding.DecodeString(strings.TrimSpace(textOf(r.kid("CheckValue"))))
			b, err := d.open(name)
			switch {
			case err != nil:
				f.Missing = true
				fail(SignChanged, signFileMissing)
			case h == nil:
				fail(SignUnknown, signDigest)
			default:
				h.Write(b)
				f.OK = werr == nil && bytes.Equal(h.Sum(nil), want)
				if !f.OK {
					fail(SignChanged, signFileChanged)
				}
			}
			out.Files = append(out.Files, f)
		}
	}
	// where the seal is
	if sa := si.kid("StampAnnot"); sa != nil {
		for i, id := range d.pageIDs {
			if id == strings.TrimSpace(sa.attr("PageRef")) {
				out.Page = i + 1
			}
		}
		out.Boundary, _ = box(sa.attr("Boundary"))
	}
	// the signature value
	sv := x.kid("SignedValue")
	if sv == nil {
		fail(SignChanged, signNoValue)
		return out
	}
	value, err := d.open(strings.TrimPrefix(resolve(loc, sv.Text), "/"))
	if err != nil {
		fail(SignChanged, signNoValue)
		return out
	}
	if out.Type == "Sign" {
		fail(SignUnknown, signPKCS7)
		return out
	}
	d.checkSeal(&out, value, raw, fail)
	return out
}

func textOf(e *el) string {
	if e == nil {
		return ""
	}
	return e.Text
}

// signTime reads a SignatureDateTime: 20240101120000(Z), or RFC 3339.
func signTime(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"20060102150405Z", "20060102150405", time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// der splits the contents of a constructed DER value into its parts.
func der(v asn1.RawValue) []asn1.RawValue {
	var out []asn1.RawValue
	rest := v.Bytes
	for len(rest) > 0 && len(out) < 64 {
		var k asn1.RawValue
		r, err := asn1.Unmarshal(rest, &k)
		if err != nil {
			return out
		}
		out = append(out, k)
		rest = r
	}
	return out
}

func isSeq(v asn1.RawValue) bool { return v.Class == asn1.ClassUniversal && v.Tag == asn1.TagSequence }

func is(v asn1.RawValue, tag int) bool { return v.Class == asn1.ClassUniversal && v.Tag == tag }

// bitString is a BIT STRING's bytes.
func bitString(v asn1.RawValue) []byte {
	if !is(v, asn1.TagBitString) || len(v.Bytes) < 1 {
		return nil
	}
	return v.Bytes[1:]
}

// checkSeal reads an electronic seal's signature value (SES_Signature) and
// checks it: the signed text's hash and the SM2 signature.
func (d *ofdDoc) checkSeal(out *OFDSignature, value, signedText []byte, fail func(status, reason string)) {
	var top asn1.RawValue
	if _, err := asn1.Unmarshal(value, &top); err != nil || !isSeq(top) {
		fail(SignUnknown, signUnreadable)
		return
	}
	parts := der(top)
	if len(parts) < 2 || !isSeq(parts[0]) {
		fail(SignUnknown, signUnreadable)
		return
	}
	tbs := parts[0]
	tbsParts := der(tbs)
	if len(tbsParts) < 5 {
		fail(SignUnknown, signUnreadable)
		return
	}
	var cert, signature []byte
	var alg asn1.ObjectIdentifier
	switch {
	case len(parts) >= 4 && is(parts[1], asn1.TagOctetString): // version 4: the certificate after what is signed
		out.Version = 4
		cert, signature = parts[1].Bytes, bitString(parts[3])
		_, _ = asn1.Unmarshal(parts[2].FullBytes, &alg)
		if is(tbsParts[2], asn1.TagGeneralizedTime) {
			var t time.Time
			if _, err := asn1.Unmarshal(tbsParts[2].FullBytes, &t); err == nil {
				out.Time = t
			}
		}
	case len(tbsParts) >= 7 && is(tbsParts[5], asn1.TagOctetString): // version 1: inside it
		out.Version = 1
		cert, signature = tbsParts[5].Bytes, bitString(parts[1])
		_, _ = asn1.Unmarshal(tbsParts[6].FullBytes, &alg)
		if t := signTime(string(bitString(tbsParts[2]))); !t.IsZero() {
			out.Time = t
		}
	default:
		fail(SignUnknown, signUnreadable)
		return
	}
	// the seal: its name and picture
	if isSeq(tbsParts[1]) {
		out.SealName, out.seal, out.sealType = sealInfo(tbsParts[1], 0)
	}
	c, err := readCert(cert)
	if err != nil {
		fail(SignUnknown, signUnreadable)
		return
	}
	out.Signer, out.Org, out.Issuer, out.ValidFrom, out.ValidTo = c.signer, c.org, c.issuer, c.from, c.to
	if !out.Time.IsZero() && (out.Time.Before(c.from) || out.Time.After(c.to)) {
		out.OutsideValidity = true
	}
	if !alg.Equal(oidSM2SM3) || c.key == nil {
		fail(SignUnknown, signAlgorithm)
		return
	}
	if c.selfSigned {
		out.SelfSigned = true
		if !c.selfOK {
			fail(SignChanged, signCert)
		}
	}
	if list, ok := sealCerts(tbsParts[1], 0); ok {
		for _, x := range list {
			if certMatches(x, cert) {
				out.CertInSeal = true
			}
		}
		if !out.CertInSeal {
			fail(SignChanged, signCertList)
		}
	}
	// the signed text: Signature.xml, hashed with SM3
	if hash := bitString(tbsParts[3]); hash == nil || !bytes.Equal(hash, sm3sum(signedText)) {
		fail(SignChanged, signTextChanged)
	}
	if !sm2.VerifyASN1(c.key, sm2.DefaultID, tbs.FullBytes, signature) {
		fail(SignChanged, signBad)
	}
}

func sm3sum(b []byte) []byte {
	s := sm3.Sum(b)
	return s[:]
}

// sealInfo finds a seal's name (its first UTF8String) and picture (a
// SEQUENCE of a type, the data, width and height) in its data.
func sealInfo(v asn1.RawValue, depth int) (name string, pic []byte, typ string) {
	if depth > 8 {
		return
	}
	kids := der(v)
	if len(kids) >= 2 && is(kids[0], asn1.TagIA5String) && is(kids[1], asn1.TagOctetString) {
		t := strings.ToLower(string(kids[0].Bytes))
		switch t {
		case "png", "jpg", "jpeg", "bmp", "gif", "ofd", "svg":
			return "", kids[1].Bytes, t
		}
	}
	for _, k := range kids {
		switch {
		case is(k, asn1.TagUTF8String) && name == "":
			name = string(k.Bytes)
		case k.IsCompound && (isSeq(k) || k.Class == asn1.ClassContextSpecific):
			n, p, t := sealInfo(k, depth+1)
			if name == "" {
				name = n
			}
			if pic == nil && p != nil {
				pic, typ = p, t
			}
		}
	}
	return
}

// sealCerts finds the certificates a seal lists (or their digests): in
// its property, the first SEQUENCE after the name. ok is false when the
// seal lists none.
func sealCerts(v asn1.RawValue, depth int) (list [][]byte, ok bool) {
	if depth > 8 {
		return nil, false
	}
	kids := der(v)
	if len(kids) >= 3 && is(kids[0], asn1.TagInteger) && is(kids[1], asn1.TagUTF8String) {
		for _, k := range kids[2:] {
			if !isSeq(k) {
				continue
			}
			for _, c := range der(k) {
				switch {
				case is(c, asn1.TagOctetString):
					list = append(list, c.Bytes)
				case isSeq(c):
					for _, x := range der(c) {
						if is(x, asn1.TagOctetString) {
							list = append(list, x.Bytes)
						}
					}
				}
			}
			return list, len(list) > 0
		}
		return nil, false
	}
	for _, k := range kids {
		if k.IsCompound && (isSeq(k) || k.Class == asn1.ClassContextSpecific) {
			if l, ok := sealCerts(k, depth+1); ok {
				return l, true
			}
		}
	}
	return nil, false
}

// certMatches: x is the certificate, or its SM3, SHA-256 or SHA-1 digest.
func certMatches(x, cert []byte) bool {
	if bytes.Equal(x, cert) {
		return true
	}
	s3, s256, s1 := sm3.Sum(cert), sha256.Sum256(cert), sha1.Sum(cert)
	return bytes.Equal(x, s3[:]) || bytes.Equal(x, s256[:]) || bytes.Equal(x, s1[:])
}

type certInfo struct {
	signer, org, issuer string
	from, to            time.Time
	key                 *sm2.PublicKey
	selfSigned          bool // the issuer is the subject
	selfOK              bool // and its signature holds
}

// x509 parts read here: Go's own parser refuses SM2 keys
type tbsCertificate struct {
	Raw       asn1.RawContent
	Version   int `asn1:"optional,explicit,default:0,tag:0"`
	Serial    *big.Int
	Algorithm pkix.AlgorithmIdentifier
	Issuer    asn1.RawValue
	Validity  struct{ NotBefore, NotAfter time.Time }
	Subject   asn1.RawValue
	PublicKey struct {
		Algorithm pkix.AlgorithmIdentifier
		Key       asn1.BitString
	}
	IssuerUID  asn1.BitString `asn1:"optional,tag:1"`
	SubjectUID asn1.BitString `asn1:"optional,tag:2"`
	Extensions asn1.RawValue  `asn1:"optional,explicit,tag:3"`
}

type certificate struct {
	TBS       tbsCertificate
	Algorithm pkix.AlgorithmIdentifier
	Signature asn1.BitString
}

var errCert = errors.New("parser: not a certificate")

func readCert(b []byte) (certInfo, error) {
	var c certificate
	if _, err := asn1.Unmarshal(b, &c); err != nil {
		return certInfo{}, errCert
	}
	name := func(raw asn1.RawValue) pkix.Name {
		var rdn pkix.RDNSequence
		var n pkix.Name
		if _, err := asn1.Unmarshal(raw.FullBytes, &rdn); err == nil {
			n.FillFromRDNSequence(&rdn)
		}
		return n
	}
	sub, iss := name(c.TBS.Subject), name(c.TBS.Issuer)
	out := certInfo{signer: sub.CommonName, issuer: iss.CommonName, from: c.TBS.Validity.NotBefore, to: c.TBS.Validity.NotAfter}
	if len(sub.Organization) > 0 {
		out.org = sub.Organization[0]
	}
	if out.issuer == "" && len(iss.Organization) > 0 {
		out.issuer = iss.Organization[0]
	}
	out.key, _ = sm2.ParsePublicKey(c.TBS.PublicKey.Key.Bytes)
	if bytes.Equal(c.TBS.Issuer.FullBytes, c.TBS.Subject.FullBytes) && out.key != nil {
		out.selfSigned = true
		out.selfOK = c.Algorithm.Algorithm.Equal(oidSM2SM3) && sm2.VerifyASN1(out.key, sm2.DefaultID, c.TBS.Raw, c.Signature.Bytes)
	}
	return out, nil
}

// sealPictures are the seals to draw on the pages, read from the
// signatures.
func (d *ofdDoc) sealPictures() {
	for _, s := range d.signatures() {
		if s.Page < 1 || s.Page > len(d.pages) || s.seal == nil || s.Boundary[2] <= 0 || s.Boundary[3] <= 0 {
			continue
		}
		p := &d.pages[s.Page-1]
		p.objs = append(p.objs, ofdObj{kind: "seal", box: s.Boundary, ctm: [6]float64{1, 0, 0, 1, 0, 0}, sealData: s.seal, format: sealFormat(s.seal, s.sealType)})
	}
}

// sealDoc reads a vector seal: an OFD of its own.
func sealDoc(b []byte) (*ofdDoc, error) {
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, ErrOFD
	}
	return readOFDFrom(z, nil)
}

// HasSealPicture: the seal has a picture a browser shows (OFDSeal).
func (s OFDSignature) HasSealPicture() bool {
	f := sealFormat(s.seal, s.sealType)
	return f == "png" || f == "jpg"
}
