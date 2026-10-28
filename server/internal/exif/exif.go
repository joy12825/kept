// Package exif reads the few EXIF tags the details page shows (v1.3.7):
// when a photo was taken, the camera, the software, and whether it carries
// a location. JPEG (APP1), TIFF, and since v1.5.5 HEIC and WebP files are
// read; anything else, or anything malformed, yields no tags. Coordinates
// are never decoded.
package exif

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"strings"
	"time"
)

// Info is what was found; zero values mean “not present”.
type Info struct {
	Taken       time.Time // DateTimeOriginal, else DateTime; local time without zone
	Make, Model string
	Software    string
	HasLocation bool // a GPS IFD with a latitude
	// Orientation (v1.11.2) is how the picture is to be turned to show
	// upright, 1–8 as EXIF has it; 0 when not given. Not shown, so Empty
	// leaves it out.
	Orientation int
}

// Empty reports whether nothing was found.
func (i Info) Empty() bool {
	return i.Taken.IsZero() && i.Make == "" && i.Model == "" && i.Software == "" && !i.HasLocation
}

// maxRead bounds how much of a file is read: EXIF sits at the start of a
// JPEG (APP1 is at most 64 KB) and the IFDs of a TIFF are usually near it.
const maxRead = 1 << 20

// ReadFile reads the EXIF of a JPEG or TIFF file.
func ReadFile(path string) Info {
	f, err := os.Open(path)
	if err != nil {
		return Info{}
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, maxRead))
	return Parse(b)
}

// Parse reads the EXIF of a JPEG, TIFF, HEIC or WebP file's leading bytes.
func Parse(b []byte) Info {
	switch {
	case len(b) > 2 && b[0] == 0xFF && b[1] == 0xD8:
		return parseTIFF(jpegAPP1(b))
	case len(b) > 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return parseTIFF(webpEXIF(b))
	case len(b) > 12 && string(b[4:8]) == "ftyp":
		return parseTIFF(heifEXIF(b))
	}
	return parseTIFF(b)
}

// webpEXIF returns the TIFF data of a WebP's EXIF chunk (some writers put
// the JPEG-style "Exif\0\0" in front of it).
func webpEXIF(b []byte) []byte {
	for i := 12; i+8 <= len(b); {
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		end := i + 8 + n
		if n < 0 || end > len(b) {
			return nil
		}
		if string(b[i:i+4]) == "EXIF" {
			return bytes.TrimPrefix(b[i+8:end], []byte("Exif\x00\x00"))
		}
		i = end + n%2 // chunks are padded to an even size
	}
	return nil
}

// heifEXIF finds the EXIF of a HEIC/HEIF file. The Exif item is stored as
// a 4-byte offset, "Exif\0\0" and the TIFF data; iPhones write it near the
// start of the file. Rather than walking the item boxes, look for the
// marker followed by a TIFF header.
func heifEXIF(b []byte) []byte {
	for i := 0; ; {
		j := bytes.Index(b[i:], []byte("Exif\x00\x00"))
		if j < 0 {
			return nil
		}
		t := b[i+j+6:]
		if len(t) >= 4 && (string(t[:4]) == "II*\x00" || string(t[:4]) == "MM\x00*") {
			return t
		}
		i += j + 6
	}
}

// jpegAPP1 returns the TIFF data of a JPEG's EXIF segment.
func jpegAPP1(b []byte) []byte {
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return nil
		}
		marker := b[i+1]
		if marker == 0xD9 || marker == 0xDA { // end of image, start of scan
			return nil
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			return nil
		}
		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return seg[6:]
		}
		i += 2 + n
	}
	return nil
}

const (
	tagMake             = 0x010F
	tagModel            = 0x0110
	tagSoftware         = 0x0131
	tagOrientation      = 0x0112
	tagDateTime         = 0x0132
	tagExifIFD          = 0x8769
	tagGPSIFD           = 0x8825
	tagDateTimeOriginal = 0x9003
	tagGPSLatitude      = 0x0002
	typeASCII           = 2
	typeShort           = 3
	typeLong            = 4
)

type tiff struct {
	b  []byte
	bo binary.ByteOrder
}

func parseTIFF(b []byte) Info {
	if len(b) < 8 {
		return Info{}
	}
	var t tiff
	switch string(b[:2]) {
	case "II":
		t = tiff{b, binary.LittleEndian}
	case "MM":
		t = tiff{b, binary.BigEndian}
	default:
		return Info{}
	}
	if t.bo.Uint16(b[2:]) != 42 {
		return Info{}
	}
	var out Info
	var dateTime, original string
	ifd0 := t.entries(int(t.bo.Uint32(b[4:])))
	for _, e := range ifd0 {
		switch e.tag {
		case tagMake:
			out.Make = t.ascii(e)
		case tagModel:
			out.Model = t.ascii(e)
		case tagSoftware:
			out.Software = t.ascii(e)
		case tagOrientation:
			if e.typ == typeShort {
				if v := int(t.bo.Uint16(e.value)); v >= 1 && v <= 8 {
					out.Orientation = v
				}
			}
		case tagDateTime:
			dateTime = t.ascii(e)
		case tagExifIFD:
			for _, x := range t.entries(t.offset(e)) {
				if x.tag == tagDateTimeOriginal {
					original = t.ascii(x)
				}
			}
		case tagGPSIFD:
			for _, x := range t.entries(t.offset(e)) {
				if x.tag == tagGPSLatitude {
					out.HasLocation = true
				}
			}
		}
	}
	for _, s := range []string{original, dateTime} {
		if tm, err := time.Parse("2006:01:02 15:04:05", s); err == nil && tm.Year() > 1900 {
			out.Taken = tm
			break
		}
	}
	// a camera's make is often repeated at the start of its model
	if out.Make != "" && strings.HasPrefix(strings.ToLower(out.Model), strings.ToLower(out.Make)) {
		out.Make = ""
	}
	return out
}

type entry struct {
	tag, typ uint16
	count    uint32
	value    []byte // the 4-byte value field
}

// entries reads an IFD at off; a bad offset yields none.
func (t tiff) entries(off int) []entry {
	if off <= 0 || off+2 > len(t.b) {
		return nil
	}
	n := int(t.bo.Uint16(t.b[off:]))
	var out []entry
	for i := range min(n, 512) {
		p := off + 2 + i*12
		if p+12 > len(t.b) {
			break
		}
		out = append(out, entry{t.bo.Uint16(t.b[p:]), t.bo.Uint16(t.b[p+2:]), t.bo.Uint32(t.b[p+4:]), t.b[p+8 : p+12]})
	}
	return out
}

func (t tiff) offset(e entry) int {
	if e.typ != typeLong {
		return 0
	}
	return int(t.bo.Uint32(e.value))
}

func (t tiff) ascii(e entry) string {
	if e.typ != typeASCII || e.count == 0 || e.count > 4096 {
		return ""
	}
	var raw []byte
	if e.count <= 4 {
		raw = e.value[:e.count]
	} else {
		off := int(t.bo.Uint32(e.value))
		if off < 0 || off+int(e.count) > len(t.b) {
			return ""
		}
		raw = t.b[off : off+int(e.count)]
	}
	return strings.TrimSpace(strings.TrimRight(string(raw), "\x00"))
}
