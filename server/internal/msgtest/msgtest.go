// Package msgtest makes small Outlook .msg files (v1.9.7) for tests and the
// demo: a compound file (MS-CFB, version 3) laid out as Outlook does, with
// the small streams in the mini stream and the large ones in sectors.
package msgtest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// Message describes the .msg file.
type Message struct {
	Subject             string
	FromName, FromEmail string
	To                  []Recipient
	Date                time.Time
	Body                string
	HTML                string // stored as bytes in the internet code page (UTF-8 here)
	// GBK stores the strings as 8-bit text in code page 936, as older
	// Chinese Outlook does; otherwise they are UTF-16.
	GBK         bool
	Attachments []Attachment
}

type Recipient struct{ Name, Email string }

type Attachment struct {
	Name, CID, MIME string
	Data            []byte
}

// node is a storage or a stream in the file.
type node struct {
	name     string
	storage  bool
	data     []byte
	children []*node
}

func stream(name string, data []byte) *node { return &node{name: name, data: data} }

// props is one storage's properties: variable ones as streams, fixed ones
// in the property stream.
type props struct {
	streams []*node
	fixed   bytes.Buffer
	gbk     bool
}

func (p *props) str(id uint16, s string) {
	if s == "" {
		return
	}
	if p.gbk {
		b, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(s))
		if err != nil {
			panic(err)
		}
		p.streams = append(p.streams, stream(fmt.Sprintf("__substg1.0_%04X001E", id), append(b, 0)))
		p.fixedEntry(id, 0x001E, uint64(len(b)+1))
		return
	}
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u)+2)
	for i, v := range u {
		binary.LittleEndian.PutUint16(b[2*i:], v)
	}
	p.streams = append(p.streams, stream(fmt.Sprintf("__substg1.0_%04X001F", id), b))
	p.fixedEntry(id, 0x001F, uint64(len(b)))
}

func (p *props) bin(id uint16, b []byte) {
	p.streams = append(p.streams, stream(fmt.Sprintf("__substg1.0_%04X0102", id), b))
	p.fixedEntry(id, 0x0102, uint64(len(b)))
}

func (p *props) long(id uint16, v int32) { p.fixedEntry(id, 0x0003, uint64(uint32(v))) }

func (p *props) time(id uint16, t time.Time) {
	p.fixedEntry(id, 0x0040, uint64(t.UnixNano()/100+116444736000000000))
}

func (p *props) fixedEntry(id, typ uint16, v uint64) {
	var e [16]byte
	binary.LittleEndian.PutUint32(e[0:], uint32(id)<<16|uint32(typ))
	binary.LittleEndian.PutUint32(e[4:], 6) // readable, writable
	binary.LittleEndian.PutUint64(e[8:], v)
	p.fixed.Write(e[:])
}

func (p *props) nodes(header int) []*node {
	h := make([]byte, header)
	return append(p.streams, stream("__properties_version1.0", append(h, p.fixed.Bytes()...)))
}

// Build writes m as a .msg file.
func Build(m Message) []byte {
	top := &props{gbk: m.GBK}
	if m.GBK {
		top.long(0x3FFD, 936)
	}
	top.str(0x0037, m.Subject)
	top.str(0x0C1A, m.FromName)
	top.str(0x5D01, m.FromEmail)
	top.str(0x1000, m.Body)
	if m.HTML != "" {
		top.long(0x3FDE, 65001)
		top.bin(0x1013, []byte(m.HTML))
	}
	if !m.Date.IsZero() {
		top.time(0x0039, m.Date)
	}
	root := &node{name: "Root Entry", storage: true}
	var names []string
	for i, r := range m.To {
		p := &props{gbk: m.GBK}
		p.long(0x0C15, 1)
		p.str(0x3001, r.Name)
		p.str(0x39FE, r.Email)
		root.children = append(root.children, &node{name: fmt.Sprintf("__recip_version1.0_#%08X", i), storage: true, children: p.nodes(8)})
		names = append(names, r.Name)
	}
	for i, a := range m.Attachments {
		p := &props{gbk: m.GBK}
		p.long(0x3705, 1)
		p.str(0x3707, a.Name)
		p.str(0x3712, a.CID)
		p.str(0x370E, a.MIME)
		p.bin(0x3701, a.Data)
		root.children = append(root.children, &node{name: fmt.Sprintf("__attach_version1.0_#%08X", i), storage: true, children: p.nodes(8)})
	}
	top.str(0x0E04, strings.Join(names, "; "))
	root.children = append(top.nodes(32), root.children...)
	return write(root)
}

const (
	secSize    = 512
	miniSize   = 64
	cutoff     = 4096
	endOfChain = 0xFFFFFFFE
	freeSect   = 0xFFFFFFFF
	fatSect    = 0xFFFFFFFD
	noStream   = 0xFFFFFFFF
)

// msgCLSID marks the root storage as an Outlook message.
var msgCLSID = []byte{0x0B, 0x0D, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}

func pad(b []byte, n int) []byte {
	if r := len(b) % n; r != 0 {
		b = append(b, make([]byte, n-r)...)
	}
	return b
}

// write lays the tree out as a version 3 compound file.
func write(root *node) []byte {
	// the directory, in order: each storage's children chained as right
	// siblings
	var all []*node
	var walk func(n *node)
	walk = func(n *node) {
		all = append(all, n)
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	index := map[*node]int{}
	for i, n := range all {
		index[n] = i
	}

	// small streams into the mini stream
	var mini []byte
	var minifat []uint32
	start := make([]uint32, len(all))
	big := map[int]bool{}
	for i, n := range all {
		start[i] = endOfChain
		if n.storage || len(n.data) == 0 {
			continue
		}
		if len(n.data) >= cutoff {
			big[i] = true
			continue
		}
		first := uint32(len(minifat))
		cnt := (len(n.data) + miniSize - 1) / miniSize
		for k := 0; k < cnt; k++ {
			if k == cnt-1 {
				minifat = append(minifat, endOfChain)
			} else {
				minifat = append(minifat, uint32(len(minifat)+1))
			}
		}
		mini = append(mini, pad(append([]byte(nil), n.data...), miniSize)...)
		start[i] = first
	}

	dirBytes := len(all) * 128
	nDir := (dirBytes + secSize - 1) / secSize
	nMiniFAT := (len(minifat)*4 + secSize - 1) / secSize
	nMini := (len(mini) + secSize - 1) / secSize
	nBig := 0
	for i := range all {
		if big[i] {
			nBig += (len(all[i].data) + secSize - 1) / secSize
		}
	}
	rest := nDir + nMiniFAT + nMini + nBig
	nFAT := 1
	for (nFAT+rest+127)/128 > nFAT {
		nFAT++
	}
	if nFAT > 109 {
		panic("msgtest: file too big")
	}

	fat := make([]uint32, nFAT*128)
	for i := range fat {
		fat[i] = freeSect
	}
	next := uint32(0)
	alloc := func(n int, mark uint32) uint32 {
		if n == 0 {
			return endOfChain
		}
		first := next
		for k := 0; k < n; k++ {
			switch {
			case mark != 0:
				fat[next] = mark
			case k == n-1:
				fat[next] = endOfChain
			default:
				fat[next] = next + 1
			}
			next++
		}
		return first
	}
	alloc(nFAT, fatSect)
	dirStart := alloc(nDir, 0)
	miniFATStart := alloc(nMiniFAT, 0)
	miniStart := alloc(nMini, 0)
	for i := range all {
		if big[i] {
			start[i] = alloc((len(all[i].data)+secSize-1)/secSize, 0)
		}
	}

	// directory entries
	dir := make([]byte, nDir*secSize)
	for i := 0; i < nDir*secSize/128; i++ {
		e := dir[i*128:]
		binary.LittleEndian.PutUint32(e[0x44:], noStream)
		binary.LittleEndian.PutUint32(e[0x48:], noStream)
		binary.LittleEndian.PutUint32(e[0x4C:], noStream)
	}
	for i, n := range all {
		e := dir[i*128 : i*128+128]
		u := utf16.Encode([]rune(n.name))
		for k, v := range u {
			binary.LittleEndian.PutUint16(e[2*k:], v)
		}
		binary.LittleEndian.PutUint16(e[0x40:], uint16(2*len(u)+2))
		switch {
		case i == 0:
			e[0x42] = 5
			copy(e[0x50:], msgCLSID)
		case n.storage:
			e[0x42] = 1
		default:
			e[0x42] = 2
		}
		e[0x43] = 1 // black
		if len(n.children) > 0 {
			binary.LittleEndian.PutUint32(e[0x4C:], uint32(index[n.children[0]]))
			for k := 0; k+1 < len(n.children); k++ {
				c := index[n.children[k]]
				binary.LittleEndian.PutUint32(dir[c*128+0x48:], uint32(index[n.children[k+1]]))
			}
		}
		switch {
		case i == 0:
			binary.LittleEndian.PutUint32(e[0x74:], miniStart)
			binary.LittleEndian.PutUint64(e[0x78:], uint64(len(mini)))
		case !n.storage:
			binary.LittleEndian.PutUint32(e[0x74:], start[i])
			binary.LittleEndian.PutUint64(e[0x78:], uint64(len(n.data)))
		}
	}

	var out bytes.Buffer
	h := make([]byte, 512)
	copy(h, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	binary.LittleEndian.PutUint16(h[0x18:], 0x3E)
	binary.LittleEndian.PutUint16(h[0x1A:], 3)
	binary.LittleEndian.PutUint16(h[0x1C:], 0xFFFE)
	binary.LittleEndian.PutUint16(h[0x1E:], 9)
	binary.LittleEndian.PutUint16(h[0x20:], 6)
	binary.LittleEndian.PutUint32(h[0x2C:], uint32(nFAT))
	binary.LittleEndian.PutUint32(h[0x30:], dirStart)
	binary.LittleEndian.PutUint32(h[0x38:], cutoff)
	binary.LittleEndian.PutUint32(h[0x3C:], miniFATStart)
	binary.LittleEndian.PutUint32(h[0x40:], uint32(nMiniFAT))
	binary.LittleEndian.PutUint32(h[0x44:], endOfChain)
	for i := 0; i < 109; i++ {
		v := uint32(freeSect)
		if i < nFAT {
			v = uint32(i)
		}
		binary.LittleEndian.PutUint32(h[0x4C+4*i:], v)
	}
	out.Write(h)
	for _, v := range fat {
		_ = binary.Write(&out, binary.LittleEndian, v)
	}
	out.Write(dir)
	mf := make([]byte, nMiniFAT*secSize)
	for i := range mf {
		mf[i] = 0xFF
	}
	for i, v := range minifat {
		binary.LittleEndian.PutUint32(mf[4*i:], v)
	}
	out.Write(mf)
	out.Write(pad(mini, secSize))
	for i := range all {
		if big[i] {
			out.Write(pad(append([]byte(nil), all[i].data...), secSize))
		}
	}
	return out.Bytes()
}
