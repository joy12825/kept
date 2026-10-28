package parser

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"unicode/utf16"
)

// A compound file ([MS-CFB]): the container of the old Office formats
// (.doc). A FAT links fixed-size sectors into streams; small streams live
// in a mini stream with a FAT of its own. Written here from the
// specification, for reading named streams of the root storage.

var errCFB = errors.New("parser: not a compound file, or damaged")

const (
	cfbMaxFile    = 200 << 20 // the whole file
	cfbFreeSect   = 0xFFFFFFFF
	cfbEndOfChain = 0xFFFFFFFE
	cfbFATSect    = 0xFFFFFFFD
	cfbDIFSect    = 0xFFFFFFFC
)

type cfbEntry struct {
	name  string
	kind  byte // 1 storage, 2 stream, 5 root
	start uint32
	size  uint64
	left  uint32
	right uint32
	child uint32
}

type cfbFile struct {
	data       []byte
	sectorSize int
	miniSize   int
	miniCutoff uint64
	fat        []uint32
	miniFAT    []uint32
	dir        []cfbEntry
	mini       []byte // the mini stream
}

// readCFB reads the compound file at src.
func readCFB(src string) (*cfbFile, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > cfbMaxFile {
		return nil, ErrWordTooBig
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	return parseCFB(data)
}

func parseCFB(data []byte) (*cfbFile, error) {
	if len(data) < 512 || string(data[:8]) != "\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1" {
		return nil, errCFB
	}
	le := binary.LittleEndian
	shift := le.Uint16(data[30:])
	miniShift := le.Uint16(data[32:])
	if (shift != 9 && shift != 12) || miniShift != 6 {
		return nil, errCFB
	}
	c := &cfbFile{data: data, sectorSize: 1 << shift, miniSize: 1 << miniShift, miniCutoff: uint64(le.Uint32(data[56:]))}
	nFAT := le.Uint32(data[44:])
	dirStart := le.Uint32(data[48:])
	miniFATStart := le.Uint32(data[60:])
	nMiniFAT := le.Uint32(data[64:])
	difStart := le.Uint32(data[68:])
	nDIF := le.Uint32(data[72:])
	nSectors := uint32((len(data) - c.sectorSize) / c.sectorSize)
	if nFAT > nSectors || nMiniFAT > nSectors || nDIF > nSectors {
		return nil, errCFB
	}
	// The FAT's sectors: 109 in the header, the rest in the DIFAT chain.
	var fatSects []uint32
	for i := 0; i < 109 && uint32(len(fatSects)) < nFAT; i++ {
		fatSects = append(fatSects, le.Uint32(data[76+4*i:]))
	}
	per := c.sectorSize/4 - 1
	seen := map[uint32]bool{}
	for s := difStart; uint32(len(fatSects)) < nFAT; {
		if s >= nSectors || seen[s] {
			return nil, errCFB
		}
		seen[s] = true
		sec := c.sector(s)
		for i := 0; i < per && uint32(len(fatSects)) < nFAT; i++ {
			fatSects = append(fatSects, le.Uint32(sec[4*i:]))
		}
		s = le.Uint32(sec[4*per:])
	}
	for _, s := range fatSects {
		if s >= nSectors {
			return nil, errCFB
		}
		sec := c.sector(s)
		for i := 0; i < c.sectorSize/4; i++ {
			c.fat = append(c.fat, le.Uint32(sec[4*i:]))
		}
	}
	dir, err := c.chain(dirStart, -1)
	if err != nil {
		return nil, err
	}
	for i := 0; i+128 <= len(dir); i += 128 {
		e := dir[i : i+128]
		n := int(le.Uint16(e[64:]))
		if n > 64 || n%2 != 0 {
			n = 0
		}
		u := make([]uint16, 0, 32)
		for j := 0; j+2 <= n-2; j += 2 {
			u = append(u, le.Uint16(e[j:]))
		}
		size := le.Uint64(e[120:])
		if c.sectorSize == 512 { // version 3: the high half may be anything
			size &= 0xFFFFFFFF
		}
		c.dir = append(c.dir, cfbEntry{name: string(utf16.Decode(u)), kind: e[66], left: le.Uint32(e[68:]), right: le.Uint32(e[72:]),
			child: le.Uint32(e[76:]), start: le.Uint32(e[116:]), size: size})
	}
	if len(c.dir) == 0 || c.dir[0].kind != 5 {
		return nil, errCFB
	}
	if nMiniFAT > 0 {
		mf, err := c.chain(miniFATStart, -1)
		if err != nil {
			return nil, err
		}
		for i := 0; i+4 <= len(mf); i += 4 {
			c.miniFAT = append(c.miniFAT, le.Uint32(mf[i:]))
		}
		root := c.dir[0]
		if c.mini, err = c.chain(root.start, int64(root.size)); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func (c *cfbFile) sector(s uint32) []byte {
	off := (int(s) + 1) * c.sectorSize
	return c.data[off : off+c.sectorSize]
}

// chain reads the sectors from start; size < 0 reads the whole chain.
func (c *cfbFile) chain(start uint32, size int64) ([]byte, error) {
	if size > cfbMaxFile {
		return nil, errCFB
	}
	nSectors := uint32((len(c.data) - c.sectorSize) / c.sectorSize)
	var out []byte
	steps := 0
	for s := start; s != cfbEndOfChain; s = c.fat[s] {
		if size >= 0 && int64(len(out)) >= size {
			break
		}
		if s >= nSectors || int(s) >= len(c.fat) || steps > len(c.fat) {
			return nil, errCFB // out of range, or a loop
		}
		steps++
		out = append(out, c.sector(s)...)
	}
	if size >= 0 {
		if int64(len(out)) < size {
			return nil, errCFB
		}
		out = out[:size]
	}
	return out, nil
}

func (c *cfbFile) miniChain(start uint32, size int64) ([]byte, error) {
	var out []byte
	steps := 0
	for s := start; s != cfbEndOfChain && int64(len(out)) < size; s = c.miniFAT[s] {
		off := int(s) * c.miniSize
		if int(s) >= len(c.miniFAT) || off+c.miniSize > len(c.mini) || steps > len(c.miniFAT) {
			return nil, errCFB
		}
		steps++
		out = append(out, c.mini[off:off+c.miniSize]...)
	}
	if int64(len(out)) < size {
		return nil, errCFB
	}
	return out[:size], nil
}

// stream reads a stream of the root storage by name; a missing one is nil.
func (c *cfbFile) stream(name string) ([]byte, error) {
	root := c.dir[0]
	// The children form a red-black tree; walk it all (it is small).
	var e *cfbEntry
	seen := map[uint32]bool{}
	var walk func(i uint32)
	walk = func(i uint32) {
		if i == cfbFreeSect || int(i) >= len(c.dir) || seen[i] || e != nil {
			return
		}
		seen[i] = true
		d := &c.dir[i]
		if d.name == name && d.kind == 2 {
			e = d
			return
		}
		walk(d.left)
		walk(d.right)
	}
	walk(root.child)
	if e == nil {
		return nil, nil
	}
	if e.size < c.miniCutoff {
		return c.miniChain(e.start, int64(e.size))
	}
	return c.chain(e.start, int64(e.size))
}

// isWordCFB reports whether the compound file at src holds a Word
// document (a WordDocument stream that starts like one).
func isWordCFB(src string) bool {
	c, err := readCFB(src)
	if err != nil {
		return false
	}
	wd, err := c.stream("WordDocument")
	return err == nil && len(wd) >= 2 && binary.LittleEndian.Uint16(wd) == 0xA5EC
}
