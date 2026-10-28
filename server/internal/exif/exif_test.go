package exif

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// builder writes a little TIFF block: IFD0 → Exif IFD and GPS IFD.
type builder struct {
	bo   binary.ByteOrder
	head []byte
}

type tag struct {
	id    uint16
	ascii string // ASCII value; empty for a pointer
	ptr   uint32
}

// tiffBlock lays out header, IFD0, Exif IFD, GPS IFD and string data.
func tiffBlock(bo binary.ByteOrder, ifd0, exifIFD, gps []tag) []byte {
	ifdSize := func(n int) int { return 2 + n*12 + 4 }
	off0 := 8
	offExif := off0 + ifdSize(len(ifd0)+2)
	offGPS := offExif + ifdSize(len(exifIFD))
	data := offGPS + ifdSize(len(gps))
	var strs bytes.Buffer
	b := make([]byte, data)
	if bo == binary.LittleEndian {
		copy(b, "II")
	} else {
		copy(b, "MM")
	}
	bo.PutUint16(b[2:], 42)
	bo.PutUint32(b[4:], uint32(off0))
	write := func(off int, tags []tag) {
		bo.PutUint16(b[off:], uint16(len(tags)))
		for i, t := range tags {
			p := off + 2 + i*12
			bo.PutUint16(b[p:], t.id)
			if t.ascii != "" {
				s := t.ascii + "\x00"
				bo.PutUint16(b[p+2:], typeASCII)
				bo.PutUint32(b[p+4:], uint32(len(s)))
				if len(s) <= 4 {
					copy(b[p+8:], s)
				} else {
					bo.PutUint32(b[p+8:], uint32(data+strs.Len()))
					strs.WriteString(s)
				}
			} else {
				bo.PutUint16(b[p+2:], typeLong)
				bo.PutUint32(b[p+4:], 1)
				bo.PutUint32(b[p+8:], t.ptr)
			}
		}
	}
	write(off0, append(ifd0, tag{id: tagExifIFD, ptr: uint32(offExif)}, tag{id: tagGPSIFD, ptr: uint32(offGPS)}))
	write(offExif, exifIFD)
	write(offGPS, gps)
	return append(b, strs.Bytes()...)
}

func jpeg(tiffData []byte) []byte {
	seg := append([]byte("Exif\x00\x00"), tiffData...)
	b := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00} // SOI, a small APP0
	b = append(b, 0xFF, 0xE1, byte((len(seg)+2)>>8), byte(len(seg)+2))
	b = append(b, seg...)
	return append(b, 0xFF, 0xDA, 0x00, 0x02, 0xFF, 0xD9)
}

func TestParseJPEG(t *testing.T) {
	for _, bo := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		data := jpeg(tiffBlock(bo,
			[]tag{{id: tagMake, ascii: "Apple"}, {id: tagModel, ascii: "iPhone 15"}, {id: tagSoftware, ascii: "17.2"}, {id: tagDateTime, ascii: "2024:03:06 09:00:00"}},
			[]tag{{id: tagDateTimeOriginal, ascii: "2024:03:05 18:42:07"}},
			[]tag{{id: tagGPSLatitude, ascii: "x"}}))
		got := Parse(data)
		want := Info{Taken: time.Date(2024, 3, 5, 18, 42, 7, 0, time.UTC), Make: "Apple", Model: "iPhone 15", Software: "17.2", HasLocation: true}
		if got != want {
			t.Errorf("%v: got %+v, want %+v", bo, got, want)
		}
	}
}

func TestParseDropsRepeatedMakeAndFallsBackToDateTime(t *testing.T) {
	got := Parse(tiffBlock(binary.LittleEndian,
		[]tag{{id: tagMake, ascii: "Canon"}, {id: tagModel, ascii: "Canon EOS R6"}, {id: tagDateTime, ascii: "2023:12:31 23:59:59"}}, nil, nil))
	if got.Make != "" || got.Model != "Canon EOS R6" || got.Taken.Format("2006-01-02") != "2023-12-31" || got.HasLocation {
		t.Errorf("got %+v", got)
	}
}

func TestParseIgnoresGarbage(t *testing.T) {
	for _, b := range [][]byte{nil, []byte("hello world"), {0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF}, {'I', 'I', 42, 0, 0xFF, 0xFF, 0xFF, 0x7F}} {
		if got := Parse(b); !got.Empty() {
			t.Errorf("Parse(%q) = %+v", b, got)
		}
	}
	// a pointer past the end is not followed
	b := tiffBlock(binary.BigEndian, []tag{{id: tagModel, ascii: "X100V"}}, nil, nil)
	if got := Parse(b[:20]); got.Model != "" {
		t.Errorf("truncated: %+v", got)
	}
}

func TestReadFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jpg")
	if err := os.WriteFile(p, jpeg(tiffBlock(binary.LittleEndian, []tag{{id: tagModel, ascii: "Pixel 8"}}, nil, nil)), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadFile(p); got.Model != "Pixel 8" {
		t.Errorf("got %+v", got)
	}
	if got := ReadFile(p + ".missing"); !got.Empty() {
		t.Errorf("missing: %+v", got)
	}
}

func TestParseHEICAndWebP(t *testing.T) {
	block := tiffBlock(binary.BigEndian, []tag{{id: tagModel, ascii: "iPhone 15"}}, []tag{{id: tagDateTimeOriginal, ascii: "2024:03:05 18:42:07"}}, nil)
	// HEIC: ftyp box, then somewhere the Exif item (offset, "Exif\0\0", TIFF);
	// an earlier "Exif\0\0" not followed by TIFF is skipped.
	heic := append([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"), []byte("....Exif\x00\x00junk....")...)
	heic = append(append(heic, 0, 0, 0, 6), append([]byte("Exif\x00\x00"), block...)...)
	// WebP: RIFF, a VP8 chunk of odd size (padded), then EXIF with the prefix.
	chunk := func(id string, data []byte) []byte {
		out := append([]byte(id), byte(len(data)), byte(len(data)>>8), byte(len(data)>>16), byte(len(data)>>24))
		out = append(out, data...)
		if len(data)%2 == 1 {
			out = append(out, 0)
		}
		return out
	}
	body := append([]byte("WEBP"), chunk("VP8 ", []byte{1, 2, 3})...)
	body = append(body, chunk("EXIF", append([]byte("Exif\x00\x00"), block...))...)
	webp := append([]byte("RIFF\x00\x00\x00\x00"), body...)
	for name, b := range map[string][]byte{"heic": heic, "webp": webp} {
		if got := Parse(b); got.Model != "iPhone 15" || got.Taken.Format("2006-01-02 15:04") != "2024-03-05 18:42" {
			t.Errorf("%s: got %+v", name, got)
		}
	}
}
