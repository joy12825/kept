// Package sm2 verifies (and, for tests and the demo, makes) SM2
// signatures (GB/T 32918-2016) on the recommended curve, as the seals of
// OFD signatures use (v1.11.3). Written here from the standard with
// math/big: verifying a seal is rare, so it is plain rather than fast.
package sm2

import (
	"crypto/rand"
	"encoding/asn1"
	"errors"
	"io"
	"math/big"

	"kept/server/internal/sm3"
)

func hexInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("sm2: bad constant")
	}
	return n
}

// the recommended curve, y² = x³ + ax + b over Fp
var (
	p  = hexInt("FFFFFFFEFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF00000000FFFFFFFFFFFFFFFF")
	a  = hexInt("FFFFFFFEFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF00000000FFFFFFFFFFFFFFFC")
	b  = hexInt("28E9FA9E9D9F5E344D5A9E4BCF6509A7F39789F515AB8F92DDBCBD414D940E93")
	n  = hexInt("FFFFFFFEFFFFFFFFFFFFFFFFFFFFFFFF7203DF6B21C6052B53BBF40939D54123")
	gx = hexInt("32C4AE2C1F1981195F9904466A39C9948FE30BBFF2660BE1715A4589334C74C7")
	gy = hexInt("BC3736A2F4F6779C59BDCEE36B692153D0A9877CC62A474002DF32E52139F0A0")
)

// DefaultID is the signer's ID when none was agreed (GB/T 35276).
var DefaultID = []byte("1234567812345678")

// PublicKey is a point on the curve.
type PublicKey struct{ X, Y *big.Int }

// PrivateKey is a private key and its public key.
type PrivateKey struct {
	PublicKey
	D *big.Int
}

var errKey = errors.New("sm2: not a public key on the curve")

// ParsePublicKey reads an uncompressed point: 04 ‖ x ‖ y.
func ParsePublicKey(b []byte) (*PublicKey, error) {
	if len(b) != 65 || b[0] != 4 {
		return nil, errKey
	}
	k := &PublicKey{X: new(big.Int).SetBytes(b[1:33]), Y: new(big.Int).SetBytes(b[33:])}
	if !onCurve(k.X, k.Y) {
		return nil, errKey
	}
	return k, nil
}

// Bytes is the uncompressed point.
func (k *PublicKey) Bytes() []byte {
	out := []byte{4}
	out = append(out, pad(k.X)...)
	return append(out, pad(k.Y)...)
}

func pad(x *big.Int) []byte {
	out := make([]byte, 32)
	return x.FillBytes(out)
}

func onCurve(x, y *big.Int) bool {
	if x.Sign() < 0 || x.Cmp(p) >= 0 || y.Sign() < 0 || y.Cmp(p) >= 0 {
		return false
	}
	l := new(big.Int).Mul(y, y)
	r := new(big.Int).Mul(x, x)
	r.Mul(r, x)
	r.Add(r, new(big.Int).Mul(a, x))
	r.Add(r, b)
	return l.Mod(l, p).Cmp(r.Mod(r, p)) == 0
}

// point arithmetic in affine coordinates; nil is the point at infinity
type point struct{ x, y *big.Int }

func add(P, Q *point) *point {
	switch {
	case P == nil:
		return Q
	case Q == nil:
		return P
	}
	var l *big.Int
	if P.x.Cmp(Q.x) == 0 {
		if new(big.Int).Add(P.y, Q.y).Mod(new(big.Int).Add(P.y, Q.y), p).Sign() == 0 {
			return nil
		}
		// tangent: (3x² + a) / 2y
		num := new(big.Int).Mul(P.x, P.x)
		num.Mul(num, big.NewInt(3)).Add(num, a)
		den := new(big.Int).Lsh(P.y, 1)
		l = num.Mul(num, den.ModInverse(den.Mod(den, p), p))
	} else {
		num := new(big.Int).Sub(Q.y, P.y)
		den := new(big.Int).Sub(Q.x, P.x)
		l = num.Mul(num, den.ModInverse(den.Mod(den, p), p))
	}
	l.Mod(l, p)
	x := new(big.Int).Mul(l, l)
	x.Sub(x, P.x).Sub(x, Q.x).Mod(x, p)
	y := new(big.Int).Sub(P.x, x)
	y.Mul(y, l).Sub(y, P.y).Mod(y, p)
	return &point{x, y}
}

func mul(k *big.Int, P *point) *point {
	var R *point
	for i := k.BitLen() - 1; i >= 0; i-- {
		R = add(R, R)
		if k.Bit(i) == 1 {
			R = add(R, P)
		}
	}
	return R
}

var g = &point{gx, gy}

// Z is the signer's identity digest: SM3(ENTL ‖ ID ‖ a ‖ b ‖ G ‖ P).
func Z(pub *PublicKey, id []byte) []byte {
	h := sm3.New()
	bitLen := len(id) * 8
	h.Write([]byte{byte(bitLen >> 8), byte(bitLen)})
	h.Write(id)
	for _, v := range []*big.Int{a, b, gx, gy, pub.X, pub.Y} {
		h.Write(pad(v))
	}
	return h.Sum(nil)
}

func digest(pub *PublicKey, id, msg []byte) *big.Int {
	h := sm3.New()
	h.Write(Z(pub, id))
	h.Write(msg)
	return new(big.Int).SetBytes(h.Sum(nil))
}

// Verify checks the signature (r, s) of msg by pub, with the signer's id.
func Verify(pub *PublicKey, id, msg []byte, r, s *big.Int) bool {
	one := big.NewInt(1)
	if r.Cmp(one) < 0 || r.Cmp(n) >= 0 || s.Cmp(one) < 0 || s.Cmp(n) >= 0 || !onCurve(pub.X, pub.Y) {
		return false
	}
	t := new(big.Int).Add(r, s)
	t.Mod(t, n)
	if t.Sign() == 0 {
		return false
	}
	pt := add(mul(s, g), mul(t, &point{pub.X, pub.Y}))
	if pt == nil {
		return false
	}
	R := new(big.Int).Add(digest(pub, id, msg), pt.x)
	return R.Mod(R, n).Cmp(r) == 0
}

type sig struct{ R, S *big.Int }

// VerifyASN1 checks a signature encoded as DER SEQUENCE { r, s }, or as
// the 64 bytes r ‖ s some seals hold.
func VerifyASN1(pub *PublicKey, id, msg, signature []byte) bool {
	var v sig
	if rest, err := asn1.Unmarshal(signature, &v); err == nil && len(rest) == 0 && v.R != nil && v.S != nil {
		return Verify(pub, id, msg, v.R, v.S)
	}
	if len(signature) == 64 {
		return Verify(pub, id, msg, new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:]))
	}
	return false
}

// GenerateKey makes a key pair (tests and the demo).
func GenerateKey(rnd io.Reader) (*PrivateKey, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	max := new(big.Int).Sub(n, big.NewInt(2))
	d, err := rand.Int(rnd, max)
	if err != nil {
		return nil, err
	}
	d.Add(d, big.NewInt(1))
	pt := mul(d, g)
	return &PrivateKey{PublicKey: PublicKey{pt.x, pt.y}, D: d}, nil
}

// SignASN1 signs msg with the signer's id, DER encoded (tests and the demo).
func SignASN1(rnd io.Reader, k *PrivateKey, id, msg []byte) ([]byte, error) {
	if rnd == nil {
		rnd = rand.Reader
	}
	e := digest(&k.PublicKey, id, msg)
	for {
		kk, err := rand.Int(rnd, new(big.Int).Sub(n, big.NewInt(1)))
		if err != nil {
			return nil, err
		}
		kk.Add(kk, big.NewInt(1))
		x1 := mul(kk, g).x
		r := new(big.Int).Add(e, x1)
		r.Mod(r, n)
		if r.Sign() == 0 || new(big.Int).Add(r, kk).Cmp(n) == 0 {
			continue
		}
		// s = (1 + d)⁻¹ (k − r d) mod n
		inv := new(big.Int).Add(k.D, big.NewInt(1))
		inv.ModInverse(inv, n)
		s := new(big.Int).Mul(r, k.D)
		s.Sub(kk, s).Mul(s, inv).Mod(s, n)
		if s.Sign() == 0 {
			continue
		}
		return asn1.Marshal(sig{r, s})
	}
}
