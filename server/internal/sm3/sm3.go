// Package sm3 is the SM3 hash (GB/T 32905-2016), the digest of China's
// commercial cryptography, used by OFD signatures (v1.11.3). Written here
// from the standard.
package sm3

import (
	"encoding/binary"
	"hash"
	"math/bits"
)

// Size is the size of a digest, BlockSize of a block.
const (
	Size      = 32
	BlockSize = 64
)

var iv = [8]uint32{0x7380166f, 0x4914b2b9, 0x172442d7, 0xda8a0600, 0xa96f30bc, 0x163138aa, 0xe38dee4d, 0xb0fb0e4e}

type digest struct {
	v   [8]uint32
	buf [BlockSize]byte
	n   int    // bytes in buf
	len uint64 // bytes written
}

// New returns a new SM3 hash.
func New() hash.Hash {
	d := &digest{}
	d.Reset()
	return d
}

// Sum returns the SM3 digest of data.
func Sum(data []byte) [Size]byte {
	d := &digest{}
	d.Reset()
	d.Write(data)
	var out [Size]byte
	copy(out[:], d.Sum(nil))
	return out
}

func (d *digest) Reset()         { d.v, d.n, d.len = iv, 0, 0 }
func (d *digest) Size() int      { return Size }
func (d *digest) BlockSize() int { return BlockSize }

func (d *digest) Write(p []byte) (int, error) {
	n := len(p)
	d.len += uint64(n)
	if d.n > 0 {
		k := copy(d.buf[d.n:], p)
		d.n += k
		p = p[k:]
		if d.n == BlockSize {
			d.block(d.buf[:])
			d.n = 0
		}
	}
	for len(p) >= BlockSize {
		d.block(p[:BlockSize])
		p = p[BlockSize:]
	}
	d.n += copy(d.buf[:], p)
	return n, nil
}

func (d *digest) Sum(in []byte) []byte {
	c := *d // the hash goes on after Sum
	var pad [BlockSize + 8]byte
	pad[0] = 0x80
	k := (BlockSize + 56 - int(c.len%BlockSize)) % BlockSize
	if k == 0 {
		k = BlockSize
	}
	binary.BigEndian.PutUint64(pad[k:], c.len*8)
	c.Write(pad[:k+8])
	out := make([]byte, Size)
	for i, v := range c.v {
		binary.BigEndian.PutUint32(out[4*i:], v)
	}
	return append(in, out...)
}

func p0(x uint32) uint32 { return x ^ bits.RotateLeft32(x, 9) ^ bits.RotateLeft32(x, 17) }
func p1(x uint32) uint32 { return x ^ bits.RotateLeft32(x, 15) ^ bits.RotateLeft32(x, 23) }

// block compresses one block into the state.
func (d *digest) block(b []byte) {
	var w [68]uint32
	var w1 [64]uint32
	for j := 0; j < 16; j++ {
		w[j] = binary.BigEndian.Uint32(b[4*j:])
	}
	for j := 16; j < 68; j++ {
		w[j] = p1(w[j-16]^w[j-9]^bits.RotateLeft32(w[j-3], 15)) ^ bits.RotateLeft32(w[j-13], 7) ^ w[j-6]
	}
	for j := 0; j < 64; j++ {
		w1[j] = w[j] ^ w[j+4]
	}
	a, bb, c, dd, e, f, g, h := d.v[0], d.v[1], d.v[2], d.v[3], d.v[4], d.v[5], d.v[6], d.v[7]
	for j := 0; j < 64; j++ {
		t := uint32(0x79cc4519)
		if j >= 16 {
			t = 0x7a879d8a
		}
		ss1 := bits.RotateLeft32(bits.RotateLeft32(a, 12)+e+bits.RotateLeft32(t, j%32), 7)
		ss2 := ss1 ^ bits.RotateLeft32(a, 12)
		var ff, gg uint32
		if j < 16 {
			ff, gg = a^bb^c, e^f^g
		} else {
			ff, gg = (a&bb)|(a&c)|(bb&c), (e&f)|(^e&g)
		}
		tt1 := ff + dd + ss2 + w1[j]
		tt2 := gg + h + ss1 + w[j]
		dd, c, bb, a = c, bits.RotateLeft32(bb, 9), a, tt1
		h, g, f, e = g, bits.RotateLeft32(f, 19), e, p0(tt2)
	}
	d.v[0] ^= a
	d.v[1] ^= bb
	d.v[2] ^= c
	d.v[3] ^= dd
	d.v[4] ^= e
	d.v[5] ^= f
	d.v[6] ^= g
	d.v[7] ^= h
}
