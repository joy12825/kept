package msgfile

import (
	"encoding/binary"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// Message is what an Outlook message says.
type Message struct {
	Subject, From, To string
	Date              time.Time
	Text, HTML        string
	Attachments       []Attachment
}

// Attachment is a file in a message.
type Attachment struct {
	Name, CID, MIME string
	Data            []byte
}

// Bounds on one message.
const (
	MaxSize        = 200 << 20
	maxAttachments = 200
	maxRecipients  = 500
)

// Property types (MS-OXCDATA 2.11.1).
const (
	ptLong    = 0x0003
	ptString8 = 0x001E
	ptUnicode = 0x001F
	ptSysTime = 0x0040
	ptBinary  = 0x0102
)

// Property IDs (MS-OXPROPS).
const (
	pidSubject          = 0x0037
	pidClientSubmitTime = 0x0039
	pidSenderName       = 0x0C1A
	pidSenderEmail      = 0x0C1F
	pidRecipientType    = 0x0C15
	pidDisplayTo        = 0x0E04
	pidDeliveryTime     = 0x0E06
	pidBody             = 0x1000
	pidHTML             = 0x1013
	pidDisplayName      = 0x3001
	pidEmailAddress     = 0x3003
	pidAttachData       = 0x3701
	pidAttachFilename   = 0x3704
	pidAttachLongName   = 0x3707
	pidAttachMIME       = 0x370E
	pidAttachContentID  = 0x3712
	pidSMTPAddress      = 0x39FE
	pidInternetCPID     = 0x3FDE
	pidMessageCodepage  = 0x3FFD
	pidSenderSMTP       = 0x5D01
)

// storage is one storage of the message: the message itself, a recipient
// or an attachment, with its property streams by name.
type storage struct {
	c        *cfb
	idx      int
	streams  map[string]entry
	fixed    map[uint16][8]byte // fixed-size values from the property stream
	codepage int
}

func (c *cfb) storage(i, header int, codepage int) storage {
	s := storage{c: c, idx: i, streams: map[string]entry{}, fixed: map[uint16][8]byte{}, codepage: codepage}
	for _, k := range c.children(i) {
		if e := c.dirs[k]; e.typ == typeStream {
			s.streams[e.name] = e
		}
	}
	if e, ok := s.streams["__properties_version1.0"]; ok {
		if b, err := c.read(e); err == nil && len(b) >= header {
			for off := header; off+16 <= len(b); off += 16 {
				tag := binary.LittleEndian.Uint32(b[off:])
				var v [8]byte
				copy(v[:], b[off+8:off+16])
				s.fixed[uint16(tag>>16)] = v
			}
		}
	}
	if cp, ok := s.long(pidMessageCodepage); ok && cp > 0 {
		s.codepage = cp
	}
	return s
}

func (s storage) long(id uint16) (int, bool) {
	v, ok := s.fixed[id]
	if !ok {
		return 0, false
	}
	return int(int32(binary.LittleEndian.Uint32(v[:]))), true
}

func (s storage) time(id uint16) time.Time {
	v, ok := s.fixed[id]
	if !ok {
		return time.Time{}
	}
	ft := binary.LittleEndian.Uint64(v[:])
	if ft == 0 {
		return time.Time{}
	}
	// FILETIME: 100 ns since 1601-01-01
	const epochDiff = 116444736000000000
	if ft < epochDiff {
		return time.Time{}
	}
	return time.Unix(0, int64(ft-epochDiff)*100).UTC()
}

func (s storage) raw(id, typ uint16) ([]byte, bool) {
	e, ok := s.streams[fmt.Sprintf("__substg1.0_%04X%04X", id, typ)]
	if !ok {
		return nil, false
	}
	b, err := s.c.read(e)
	return b, err == nil
}

// str reads a string property, stored as UTF-16 or in the message's code page.
func (s storage) str(id uint16) string {
	if b, ok := s.raw(id, ptUnicode); ok {
		return strings.TrimRight(utf16le(b), "\x00")
	}
	if b, ok := s.raw(id, ptString8); ok {
		return strings.TrimRight(decode(b, s.codepage), "\x00")
	}
	return ""
}

func utf16le(b []byte) string {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

var codepages = map[int]encoding.Encoding{
	936: simplifiedchinese.GBK, 54936: simplifiedchinese.GB18030, 20936: simplifiedchinese.HZGB2312,
	950: traditionalchinese.Big5, 932: japanese.ShiftJIS, 50220: japanese.ISO2022JP, 51932: japanese.EUCJP,
	949: korean.EUCKR, 1250: charmap.Windows1250, 1251: charmap.Windows1251, 1252: charmap.Windows1252,
	1253: charmap.Windows1253, 1254: charmap.Windows1254, 1255: charmap.Windows1255, 1256: charmap.Windows1256,
	1257: charmap.Windows1257, 1258: charmap.Windows1258, 28591: charmap.ISO8859_1, 28592: charmap.ISO8859_2,
	28605: charmap.ISO8859_15, 866: charmap.CodePage866, 437: charmap.CodePage437,
}

// decode turns bytes in a Windows code page into text. Without a known code
// page, valid UTF-8 is taken as it is, and anything else as GB18030 — the
// likeliest for mail kept by a Chinese family.
func decode(b []byte, cp int) string {
	if cp == 65001 || cp == 20127 {
		return string(b)
	}
	enc, ok := codepages[cp]
	if !ok {
		if utf8.Valid(b) {
			return string(b)
		}
		enc = simplifiedchinese.GB18030
	}
	out, err := enc.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

// Parse reads a .msg file.
func Parse(data []byte) (Message, error) {
	if len(data) > MaxSize {
		return Message{}, fmt.Errorf("msgfile: the message is over %d MB", MaxSize>>20)
	}
	c, err := openCFB(data)
	if err != nil {
		return Message{}, err
	}
	top := c.storage(0, 32, 0)
	m := Message{Subject: top.str(pidSubject), Text: top.str(pidBody)}
	m.Date = top.time(pidClientSubmitTime)
	if m.Date.IsZero() {
		m.Date = top.time(pidDeliveryTime)
	}
	m.From = address(top.str(pidSenderName), top.str(pidSenderSMTP), top.str(pidSenderEmail))

	if b, ok := top.raw(pidHTML, ptBinary); ok {
		cp, _ := top.long(pidInternetCPID)
		if cp == 0 {
			cp = top.codepage
		}
		m.HTML = decode(b, cp)
	} else {
		m.HTML = top.str(pidHTML)
	}

	var to []string
	for _, k := range c.children(0) {
		e := c.dirs[k]
		if e.typ != typeStorage {
			continue
		}
		switch {
		case strings.HasPrefix(e.name, "__recip_version1.0_") && len(to) < maxRecipients:
			r := c.storage(k, 8, top.codepage)
			if t, ok := r.long(pidRecipientType); ok && t != 1 {
				continue // Cc and Bcc
			}
			if a := address(r.str(pidDisplayName), r.str(pidSMTPAddress), r.str(pidEmailAddress)); a != "" {
				to = append(to, a)
			}
		case strings.HasPrefix(e.name, "__attach_version1.0_") && len(m.Attachments) < maxAttachments:
			a := c.storage(k, 8, top.codepage)
			data, ok := a.raw(pidAttachData, ptBinary)
			if !ok {
				continue // an attached message or OLE object: not a plain file
			}
			name := a.str(pidAttachLongName)
			if name == "" {
				name = a.str(pidAttachFilename)
			}
			if name == "" {
				name = a.str(pidDisplayName)
			}
			m.Attachments = append(m.Attachments, Attachment{
				Name: name, CID: strings.Trim(a.str(pidAttachContentID), "<> "), MIME: strings.ToLower(a.str(pidAttachMIME)), Data: data,
			})
		}
	}
	if len(to) > 0 {
		m.To = strings.Join(to, ", ")
	} else {
		m.To = top.str(pidDisplayTo)
	}
	return m, nil
}

// address writes a person as the .eml reader does: "Name <a@b>", or
// whichever of the two there is. Exchange's internal addresses ("/O=…")
// are left out.
func address(name, smtp, other string) string {
	addr := smtp
	if addr == "" && strings.Contains(other, "@") {
		addr = other
	}
	name = strings.TrimSpace(name)
	switch {
	case addr == "":
		return name
	case name == "" || name == addr:
		return addr
	default:
		return name + " <" + addr + ">"
	}
}
