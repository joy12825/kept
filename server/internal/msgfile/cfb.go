// Package msgfile reads an Outlook message saved as a file (.msg, v1.9.7):
// an OLE compound file (MS-CFB) whose streams hold the message's properties
// (MS-OXMSG). Written here from the two specifications, no library.
package msgfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

// Magic starts every compound file.
var Magic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

const (
	endOfChain = 0xFFFFFFFE
	noStream   = 0xFFFFFFFF
	headerSize = 512
	entrySize  = 128

	typeStorage = 1
	typeStream  = 2
	typeRoot    = 5
)

var errBroken = errors.New("msgfile: the compound file is damaged")

type entry struct {
	name               string
	typ                byte
	left, right, child uint32
	start              uint32
	size               uint64
}

// cfb is a compound file read into memory.
type cfb struct {
	data     []byte
	secSize  int
	miniSize int
	cutoff   uint64
	fat      []uint32
	minifat  []uint32
	dirs     []entry
	mini     []byte // the mini stream: where the small streams are
}

func u16(b []byte, off int) uint16 { return binary.LittleEndian.Uint16(b[off:]) }
func u32(b []byte, off int) uint32 { return binary.LittleEndian.Uint32(b[off:]) }

func openCFB(data []byte) (*cfb, error) {
	if len(data) < headerSize || !bytes.HasPrefix(data, Magic) {
		return nil, errBroken
	}
	shift, miniShift := u16(data, 0x1E), u16(data, 0x20)
	if (shift != 9 && shift != 12) || miniShift != 6 {
		return nil, errBroken
	}
	c := &cfb{data: data, secSize: 1 << shift, miniSize: 1 << miniShift, cutoff: uint64(u32(data, 0x38))}
	major := u16(data, 0x1A)

	// the sectors of the allocation table: 109 listed in the header, the
	// rest in a chain of DIFAT sectors
	nFAT := int(u32(data, 0x2C))
	if nFAT > c.sectors() {
		return nil, errBroken
	}
	var fatSecs []uint32
	for i := 0; i < 109 && len(fatSecs) < nFAT; i++ {
		fatSecs = append(fatSecs, u32(data, 0x4C+4*i))
	}
	per := c.secSize/4 - 1
	for s, n := u32(data, 0x44), 0; len(fatSecs) < nFAT && s != endOfChain && s != noStream; n++ {
		b, ok := c.sector(s)
		if !ok || n > c.sectors() {
			return nil, errBroken
		}
		for i := 0; i < per && len(fatSecs) < nFAT; i++ {
			fatSecs = append(fatSecs, u32(b, 4*i))
		}
		s = u32(b, 4*per)
	}
	for _, s := range fatSecs {
		b, ok := c.sector(s)
		if !ok {
			return nil, errBroken
		}
		for i := 0; i < len(b); i += 4 {
			c.fat = append(c.fat, u32(b, i))
		}
	}

	dir, err := c.chain(c.fat, u32(data, 0x30), c.secSize, func(s uint32) ([]byte, bool) { return c.sector(s) })
	if err != nil {
		return nil, err
	}
	for off := 0; off+entrySize <= len(dir); off += entrySize {
		e := dir[off : off+entrySize]
		n := int(u16(e, 0x40))
		if n > 64 {
			n = 64
		}
		units := make([]uint16, 0, n/2)
		for i := 0; i+1 < n; i += 2 {
			if v := u16(e, i); v != 0 {
				units = append(units, v)
			}
		}
		size := binary.LittleEndian.Uint64(e[0x78:])
		if major == 3 {
			size &= 0xFFFFFFFF // version 3 files may leave junk in the high half
		}
		c.dirs = append(c.dirs, entry{
			name: string(utf16.Decode(units)), typ: e[0x42],
			left: u32(e, 0x44), right: u32(e, 0x48), child: u32(e, 0x4C),
			start: u32(e, 0x74), size: size,
		})
	}
	if len(c.dirs) == 0 || c.dirs[0].typ != typeRoot {
		return nil, errBroken
	}

	if mf := u32(data, 0x3C); mf != endOfChain && mf != noStream {
		b, err := c.chain(c.fat, mf, c.secSize, func(s uint32) ([]byte, bool) { return c.sector(s) })
		if err != nil {
			return nil, err
		}
		for i := 0; i+4 <= len(b); i += 4 {
			c.minifat = append(c.minifat, u32(b, i))
		}
	}
	root := c.dirs[0]
	if root.size > 0 {
		if c.mini, err = c.regular(root.start, root.size); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// sectors is how many sectors the file has after its header.
func (c *cfb) sectors() int { return (len(c.data) - c.secSize) / c.secSize }

func (c *cfb) sector(s uint32) ([]byte, bool) {
	off := (int64(s) + 1) * int64(c.secSize)
	if s >= endOfChain-3 || off+int64(c.secSize) > int64(len(c.data)) {
		return nil, false
	}
	return c.data[off : off+int64(c.secSize)], true
}

func (c *cfb) miniSector(s uint32) ([]byte, bool) {
	off := int64(s) * int64(c.miniSize)
	if off+int64(c.miniSize) > int64(len(c.mini)) {
		return nil, false
	}
	return c.mini[off : off+int64(c.miniSize)], true
}

// chain joins the sectors of a chain in table, from start. A chain that
// loops or leaves the file is an error.
func (c *cfb) chain(table []uint32, start uint32, size int, get func(uint32) ([]byte, bool)) ([]byte, error) {
	var out []byte
	for s, n := start, 0; s != endOfChain; n++ {
		if n > len(table) || int(s) >= len(table) {
			return nil, errBroken
		}
		b, ok := get(s)
		if !ok {
			return nil, errBroken
		}
		out = append(out, b[:size]...)
		s = table[s]
	}
	return out, nil
}

func (c *cfb) regular(start uint32, size uint64) ([]byte, error) {
	if size > uint64(len(c.data)) {
		return nil, errBroken
	}
	b, err := c.chain(c.fat, start, c.secSize, func(s uint32) ([]byte, bool) { return c.sector(s) })
	if err != nil {
		return nil, err
	}
	if uint64(len(b)) < size {
		return nil, errBroken
	}
	return b[:size], nil
}

// read returns a stream's bytes: small streams live in the mini stream.
func (c *cfb) read(e entry) ([]byte, error) {
	if e.size == 0 {
		return nil, nil
	}
	if e.size >= c.cutoff {
		return c.regular(e.start, e.size)
	}
	if e.size > uint64(len(c.mini)) {
		return nil, errBroken
	}
	b, err := c.chain(c.minifat, e.start, c.miniSize, c.miniSector)
	if err != nil {
		return nil, err
	}
	if uint64(len(b)) < e.size {
		return nil, errBroken
	}
	return b[:e.size], nil
}

// children lists the entries directly inside storage i: its child and,
// from there, every left and right sibling.
func (c *cfb) children(i int) []int {
	var out []int
	seen := map[uint32]bool{}
	stack := []uint32{c.dirs[i].child}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if s == noStream || int(s) >= len(c.dirs) || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, int(s))
		stack = append(stack, c.dirs[s].left, c.dirs[s].right)
	}
	return out
}
