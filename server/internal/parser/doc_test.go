package parser

import (
	"encoding/binary"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kept/server/internal/doctest"
)

func writeFile(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// pngBytes is a small PNG (1×1).
var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde\x00\x00\x00\x0cIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

// TestDocFromAnotherApplication: a .doc textutil wrote (files/handbook.html).
func TestDocFromAnotherApplication(t *testing.T) {
	d, err := ReadWord(writeFile(t, "a.doc", doctest.HandbookDoc()))
	if err != nil {
		t.Fatal(err)
	}
	b := d.Blocks
	if texts(b[0]) != "家庭应急手册" || !b[0].Runs[0].Bold || b[0].Runs[0].Size != 24 {
		t.Errorf("title = %+v", b[0])
	}
	var fmts []string
	for _, r := range b[1].Runs {
		switch {
		case r.Bold:
			fmts = append(fmts, "b:"+r.Text)
		case r.Italic:
			fmts = append(fmts, "i:"+r.Text)
		case r.Underline:
			fmts = append(fmts, "u:"+r.Text)
		case r.Strike:
			fmts = append(fmts, "s:"+r.Text)
		}
	}
	if strings.Join(fmts, ",") != "b:先保证人身安全,i:请每年核对一次,u:药品,s:旧电话" {
		t.Errorf("formats = %v", fmts)
	}
	if b[2].Align != "center" || b[2].Runs[0].Color != "B00004" {
		t.Errorf("centred, red = %+v", b[2])
	}
	var vert []string
	for _, r := range b[2].Runs {
		if r.Vert != "" {
			vert = append(vert, r.Vert+r.Text)
		}
	}
	if strings.Join(vert, ",") != "sub2,super3" {
		t.Errorf("sub and superscript = %v", vert)
	}
	tbl := b[4]
	if tbl.Kind != "table" || len(tbl.Rows) != 4 || len(tbl.Rows[0]) != 3 {
		t.Fatalf("table = %+v", tbl)
	}
	if c := tbl.Rows[1][2]; c.RowSpan != 2 || texts(c.Blocks[0]) != "白天优先" || len(tbl.Rows[2]) != 2 {
		t.Errorf("merged down = %+v / %+v", c, tbl.Rows[2])
	}
	if c := tbl.Rows[3][0]; c.ColSpan != 2 || texts(c.Blocks[0]) != "物业服务中心" {
		t.Errorf("merged across = %+v", c)
	}
	last := b[len(b)-1]
	if strings.ContainsRune(texts(last), 0xFFFC) {
		t.Errorf("the object character shows: %q", texts(last))
	}
}

func richDoc() doctest.Doc {
	p := func(runs ...doctest.Run) doctest.Para { return doctest.Para{Runs: runs} }
	cell := func(merge byte, ps ...doctest.Para) doctest.Cell { return doctest.Cell{Merge: merge, Paras: ps} }
	w := []int{2000, 3000}
	return doctest.Doc{Blocks: []doctest.Block{
		{Para: &doctest.Para{Style: 1, Runs: []doctest.Run{{Text: "家庭应急手册"}}}},
		{Para: &doctest.Para{Runs: []doctest.Run{{Text: "先保证"}, {Text: "人身安全", Bold: true, Color: "C00000"}, {Text: "。", Footnote: "每年核对一次"}}}},
		{Para: &doctest.Para{List: 1, Runs: []doctest.Run{{Text: "查看停电范围"}}}},
		{Para: &doctest.Para{List: 1, Level: 1, Runs: []doctest.Run{{Text: "整栋楼"}}}},
		{Para: &doctest.Para{List: 1, Level: 1, Runs: []doctest.Run{{Text: "只有家里"}}}},
		{Para: &doctest.Para{List: 1, Runs: []doctest.Run{{Text: "合上总闸"}}}},
		{Para: &doctest.Para{List: 1, Level: 2, Runs: []doctest.Run{{Text: "注意安全"}}}},
		{Para: &doctest.Para{List: 2, Runs: []doctest.Run{{Text: "第五项"}}}},
		{Table: []doctest.Row{
			{Header: true, Widths: w, Cells: []doctest.Cell{cell(0, p(doctest.Run{Text: "称呼"})), cell(0, p(doctest.Run{Text: "电话"}))}},
			{Widths: w, Cells: []doctest.Cell{cell(3, p(doctest.Run{Text: "家人"})), cell(0, p(doctest.Run{Text: "爸爸 138"}), p(doctest.Run{Text: "妈妈 139"}))}},
			{Widths: w, Cells: []doctest.Cell{cell(1, p()), cell(0, p(doctest.Run{Text: "奶奶 137"}))}},
			{Widths: []int{5000}, Cells: []doctest.Cell{cell(0, p(doctest.Run{Text: "物业 24 小时"}))}},
		}},
		{Para: &doctest.Para{Runs: []doctest.Run{{Text: "安全指南", Link: "https://example.com/safety"}, {Text: "；"}, {Text: "别点", Link: "javascript:alert(1)"},
			{Text: "看不见", Hidden: true}, {Text: "删掉的", Deleted: true}, {Text: "醒目", Style: 4}}}},
		{Para: &doctest.Para{Style: 3, Align: 1, Runs: []doctest.Run{{Text: "总阀"}, {Picture: pngBytes, Width: 40, Height: 20}, {Text: "在厨房", Endnote: "以物业图纸为准"}}}},
		{Para: &doctest.Para{Style: 2, Align: 3, Runs: []doctest.Run{{Text: "附录", Italic: true}}}},
	}}
}

func TestDocParagraphsListsTablesNotes(t *testing.T) {
	src := writeFile(t, "rich.doc", richDoc().Build())
	d, err := ReadWord(src)
	if err != nil {
		t.Fatal(err)
	}
	b := d.Blocks
	if d.BaseSize != 12 {
		t.Errorf("base size = %v", d.BaseSize)
	}
	if b[0].Heading != 1 || texts(b[0]) != "家庭应急手册" || !b[0].Runs[0].Bold || b[0].Runs[0].Size != 16 {
		t.Errorf("heading = %+v", b[0])
	}
	if r := b[1].Runs[1]; !r.Bold || r.Color != "C00000" || r.Text != "人身安全" {
		t.Errorf("formatted = %+v", r)
	}
	if r := b[1].Runs[len(b[1].Runs)-1]; r.Note != 1 || r.Vert != "super" {
		t.Errorf("footnote reference = %+v", r)
	}
	var markers []string
	for _, x := range b[2:8] {
		if x.List == nil {
			t.Fatalf("not a list: %+v", x)
		}
		markers = append(markers, x.List.Marker)
	}
	if got := strings.Join(markers, " "); got != "1. （一） （二） 2. • 5." {
		t.Errorf("markers = %s", got)
	}
	if b[3].List.Level != 1 {
		t.Errorf("level = %d", b[3].List.Level)
	}
	tbl := b[8]
	if tbl.Kind != "table" || tbl.HeaderRows != 1 || len(tbl.Rows) != 4 {
		t.Fatalf("table = %+v", tbl)
	}
	if c := tbl.Rows[1][0]; c.RowSpan != 2 || texts(c.Blocks[0]) != "家人" {
		t.Errorf("merged down = %+v", c)
	}
	if c := tbl.Rows[1][1]; len(c.Blocks) != 2 || texts(c.Blocks[1]) != "妈妈 139" {
		t.Errorf("two paragraphs in a cell = %+v", c)
	}
	if len(tbl.Rows[2]) != 1 || texts(tbl.Rows[2][0].Blocks[0]) != "奶奶 137" {
		t.Errorf("the row under the merge = %+v", tbl.Rows[2])
	}
	if c := tbl.Rows[3][0]; c.ColSpan != 2 {
		t.Errorf("a wide cell spans the grid = %+v", c)
	}
	links := b[9].Runs
	if texts(b[9]) != "安全指南；别点醒目" {
		t.Errorf("hidden and deleted text shows: %q", texts(b[9]))
	}
	if links[0].Link != "https://example.com/safety" || links[2].Link != "" {
		t.Errorf("links = %+v", links)
	}
	if r := links[len(links)-1]; !r.Bold || r.Color != "C00000" {
		t.Errorf("character style = %+v", r)
	}
	if b[10].Heading != 3 || b[10].Align != "center" {
		t.Errorf("heading by its style's name = %+v", b[10])
	}
	if r := b[10].Runs[1]; r.Image != 1 || len(d.Images) != 1 || d.Images[0].Width != 40 || d.Images[0].Height != 20 {
		t.Errorf("picture = %+v %+v", r, d.Images)
	}
	if b[11].Heading != 2 || b[11].Align != "justify" || !b[11].Runs[0].Italic {
		t.Errorf("heading 2 = %+v", b[11])
	}
	if len(d.Notes) != 2 || strings.TrimSpace(texts(d.Notes[0].Blocks[0])) != "每年核对一次" || strings.TrimSpace(texts(d.Notes[1].Blocks[0])) != "以物业图纸为准" {
		t.Errorf("notes = %+v", d.Notes)
	}
	img, mime, err := ReadWordImage(src, 1)
	if err != nil || mime != "image/png" || string(img) != string(pngBytes) {
		t.Errorf("picture = %q %s %v", img, mime, err)
	}
	if _, _, err := ReadWordImage(src, 2); !errors.Is(err, ErrNotWord) {
		t.Errorf("no second picture: %v", err)
	}
}

func TestDocRefused(t *testing.T) {
	good := richDoc().Build()
	wordDoc := func(b []byte) []byte {
		c, err := parseCFB(b)
		if err != nil {
			t.Fatal(err)
		}
		wd, _ := c.stream("WordDocument")
		return wd
	}
	// patch changes the WordDocument stream in place (it starts at a known sector)
	patch := func(fn func(wd []byte)) []byte {
		b := append([]byte(nil), good...)
		wd := wordDoc(b)
		at := strings.Index(string(b), string(wd[:64]))
		fn(b[at:])
		return b
	}
	cases := map[string]struct {
		file []byte
		want error
	}{
		"encrypted":  {patch(func(wd []byte) { binary.LittleEndian.PutUint16(wd[10:], binary.LittleEndian.Uint16(wd[10:])|0x0100) }), ErrWordEncrypted},
		"word 95":    {patch(func(wd []byte) { binary.LittleEndian.PutUint16(wd[2:], 0x0065) }), ErrWordOld},
		"not word":   {patch(func(wd []byte) { wd[0] = 0 }), ErrNotWord},
		"cut short":  {good[:1500], ErrNotWord},
		"not a file": {[]byte("\xD0\xCF\x11\xE0\xA1\xB1\x1A\xE1 and nothing else"), ErrNotWord},
	}
	// a FAT whose chain loops back on itself
	loop := append([]byte(nil), good...)
	binary.LittleEndian.PutUint32(loop[512+4*1:], 1) // the directory's sector points to itself
	cases["a loop"] = struct {
		file []byte
		want error
	}{loop, ErrNotWord}
	for name, c := range cases {
		if _, err := ReadWord(writeFile(t, "x.doc", c.file)); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
}

// TestDocDamaged: no damage makes the reader fail other than with an error.
func TestDocDamaged(t *testing.T) {
	sources := [][]byte{richDoc().Build(), doctest.HandbookDoc()}
	rnd := rand.New(rand.NewSource(1))
	dir := t.TempDir()
	for i := 0; i < 1500; i++ {
		b := append([]byte(nil), sources[i%2]...)
		for k := 0; k < 1+rnd.Intn(8); k++ {
			at := rnd.Intn(len(b))
			if rnd.Intn(3) == 0 && at > 512 {
				at = 512 + rnd.Intn(min(len(b)-512, 4096))
			}
			b[at] = byte(rnd.Intn(256))
		}
		p := filepath.Join(dir, "d.doc")
		os.WriteFile(p, b, 0o644)
		func() {
			defer func() {
				if r := recover(); r != nil {
					os.WriteFile(filepath.Join(os.TempDir(), "kept-doc-crash.doc"), b, 0o644)
					t.Fatalf("damaged file %d: panic %v", i, r)
				}
			}()
			if d, err := ReadWord(p); err == nil {
				for n := range d.Images {
					ReadWordImage(p, n+1)
				}
			}
		}()
	}
}

// A .doc without Word's class ID in its compound file is still a .doc.
func TestDetectDocWithoutClassID(t *testing.T) {
	for _, b := range [][]byte{doctest.HandbookDoc(), richDoc().Build()} {
		if m, ext, err := Detect(writeFile(t, "a.bin", b), "a.bin"); m != DocMIME || ext != "doc" || err != nil {
			t.Errorf("detected %s %s %v", m, ext, err)
		}
	}
	if m, _, _ := Detect(writeFile(t, "a.odt", doctest.HandbookOdt()), "a.odt"); m != OdtMIME {
		t.Errorf("odt detected as %s", m)
	}
}
