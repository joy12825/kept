package edition

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/i18n"
)

// License is what a license file says. It is signed as a whole: changing
// any of it breaks the signature.
type License struct {
	ID       string `json:"id"`
	Instance string `json:"instance"` // the instance it is for
	Edition  string `json:"edition"`  // pro or team
	Holder   string `json:"holder"`   // who it was issued to
	// Members raises the edition's member limit; 0 keeps it (a team's: no limit).
	Members int    `json:"members,omitempty"`
	Issued  string `json:"issued"`            // YYYY-MM-DD
	Expires string `json:"expires,omitempty"` // YYYY-MM-DD, the last day it holds; "" never
}

// prefix starts every license text; the version is in it.
const prefix = "KEPT1"

var b64 = base64.RawURLEncoding

var (
	errFormat    = apperr.WithCode(apperr.KindInvalid, "license_invalid", i18n.N("这不是收好的许可证，请完整复制许可证文件的内容"))
	errSignature = apperr.WithCode(apperr.KindInvalid, "license_invalid", i18n.N("许可证校验没有通过：内容被改过，或不是收好签发的"))
	errInstance  = apperr.WithCode(apperr.KindInvalid, "license_instance", i18n.N("这份许可证是签给另一台服务器的，请用版本与授权页上的实例编号重新申请"))
	errEdition   = apperr.WithCode(apperr.KindInvalid, "license_invalid", i18n.N("许可证里的版本不对"))
)

// Sign writes l as license text with the issuer's key.
func Sign(l License, key ed25519.PrivateKey) (string, error) {
	if _, err := Parse(l.Edition); err != nil || l.Edition == "free" {
		return "", errors.New("license: edition must be pro or team")
	}
	body, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	payload := b64.EncodeToString(body)
	sig := ed25519.Sign(key, []byte(prefix+"."+payload))
	return prefix + "." + payload + "." + b64.EncodeToString(sig), nil
}

// Read checks text's signature with pub and returns what it says. Spaces
// and line breaks from copying are ignored.
func Read(text string, pub ed25519.PublicKey) (License, error) {
	text = strings.Join(strings.Fields(text), "")
	parts := strings.Split(text, ".")
	if len(parts) != 3 || parts[0] != prefix {
		return License{}, errFormat
	}
	body, err1 := b64.DecodeString(parts[1])
	sig, err2 := b64.DecodeString(parts[2])
	if err1 != nil || err2 != nil {
		return License{}, errFormat
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, []byte(prefix+"."+parts[1]), sig) {
		return License{}, errSignature
	}
	var l License
	if err := json.Unmarshal(body, &l); err != nil {
		return License{}, errFormat
	}
	if e, err := Parse(l.Edition); err != nil || e == Free {
		return License{}, errEdition
	}
	return l, nil
}

// Tier is the license's edition.
func (l License) Tier() Edition {
	e, _ := Parse(l.Edition)
	return e
}

// ExpiresAt is the moment after its last day, in loc; zero when it never expires.
func (l License) ExpiresAt(loc *time.Location) time.Time {
	if l.Expires == "" {
		return time.Time{}
	}
	d, err := time.ParseInLocation("2006-01-02", l.Expires, loc)
	if err != nil {
		return time.Time{}.Add(1) // unreadable: already over
	}
	return d.AddDate(0, 0, 1)
}

// Limits are the edition's, with the license's member count when it gives one.
func (l License) Limits() Limits {
	lim := DefaultLimits(l.Tier())
	if l.Members > 0 {
		lim.Members = l.Members
	}
	return lim
}

// GenerateKey makes an issuer's key pair.
func GenerateKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(nil)
}

// EncodeKey and DecodeKey write keys as text (base64url).
func EncodeKey(k []byte) string { return b64.EncodeToString(k) }

func DecodeKey(s string) ([]byte, error) { return b64.DecodeString(strings.TrimSpace(s)) }
