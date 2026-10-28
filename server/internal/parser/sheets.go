package parser

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"io"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"kept/server/internal/i18n"
)

// Tables (v1.9.3): the cells of an Excel workbook (xlsx) or a CSV/TSV file,
// for the table view. Values are shown as stored: a formula's last result,
// dates as dates; nothing is recalculated.

// Limits of what is sent to the browser.
const (
	SheetMaxRows = 10000
	SheetMaxCols = 200
)

// Sheet is one table.
type Sheet struct {
	Name      string
	Rows      [][]string
	Merges    [][4]int // first row, first column, last row, last column (from 0)
	Truncated bool
}

// ErrNoSheets: the file has no table to show.
var ErrNoSheets = &Error{Message: "这份文档没有可以按表格显示的内容", Permanent: true}

// CSVSheetName names the one table of a CSV or text table (translated where it is sent).
var CSVSheetName = i18n.N("表格")

// ReadSheets reads the tables of the file at src: ext is the document's
// (xlsx, csv, tsv; a text file named .csv is a CSV).
func ReadSheets(src, ext string) ([]Sheet, error) {
	switch strings.ToLower(ext) {
	case "xlsx":
		return readXLSX(src)
	case "csv", "tsv", "txt":
		return readCSV(src, strings.ToLower(ext) == "tsv")
	}
	return nil, ErrNoSheets
}

func readCSV(src string, tabs bool) ([]Sheet, error) {
	text, err := readText(src) // UTF-8 or GBK
	if err != nil {
		return nil, err
	}
	first, _, _ := strings.Cut(text, "\n")
	r := csv.NewReader(strings.NewReader(text))
	r.LazyQuotes, r.FieldsPerRecord, r.ReuseRecord = true, -1, false
	if tabs || strings.Count(first, "\t") > strings.Count(first, ",") {
		r.Comma = '\t'
	} else if strings.Count(first, ";") > strings.Count(first, ",") {
		r.Comma = ';'
	}
	sh := Sheet{Name: CSVSheetName}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if len(sh.Rows) == 0 {
				return nil, ErrNoSheets
			}
			break
		}
		if len(sh.Rows) == SheetMaxRows {
			sh.Truncated = true
			break
		}
		if len(rec) > SheetMaxCols {
			rec, sh.Truncated = rec[:SheetMaxCols], true
		}
		sh.Rows = append(sh.Rows, rec)
	}
	return []Sheet{sh}, nil
}

// xlsx parts -------------------------------------------------------------

type xlsx struct {
	files    map[string]*zip.File
	strings  []string
	dateFmt  map[int]string // style index → "date", "datetime" or "time"
	date1904 bool
}

func (x *xlsx) read(name string) ([]byte, error) {
	f := x.files[strings.TrimPrefix(name, "/")]
	if f == nil {
		return nil, io.EOF
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, officePartMax))
}

func readXLSX(src string) ([]Sheet, error) {
	z, err := zip.OpenReader(src)
	if err != nil {
		return nil, ErrNoSheets
	}
	defer z.Close()
	x := &xlsx{files: map[string]*zip.File{}, dateFmt: map[int]string{}}
	for _, f := range z.File {
		x.files[f.Name] = f
	}
	wb, err := x.read("xl/workbook.xml")
	if err != nil {
		return nil, ErrNoSheets
	}
	var book struct {
		Pr struct {
			Date1904 string `xml:"date1904,attr"`
		} `xml:"workbookPr"`
		Sheets []struct {
			Name  string `xml:"name,attr"`
			State string `xml:"state,attr"`
			RID   string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(wb, &book); err != nil {
		return nil, ErrNoSheets
	}
	x.date1904 = book.Pr.Date1904 == "1" || book.Pr.Date1904 == "true"
	targets := map[string]string{}
	if rels, err := x.read("xl/_rels/workbook.xml.rels"); err == nil {
		var r struct {
			Rel []struct {
				ID     string `xml:"Id,attr"`
				Target string `xml:"Target,attr"`
			} `xml:"Relationship"`
		}
		if xml.Unmarshal(rels, &r) == nil {
			for _, rel := range r.Rel {
				t := rel.Target
				if strings.HasPrefix(t, "/") {
					t = strings.TrimPrefix(t, "/")
				} else {
					t = path.Join("xl", t)
				}
				targets[rel.ID] = t
			}
		}
	}
	x.readStrings()
	x.readStyles()
	var out []Sheet
	for i, s := range book.Sheets {
		if s.State == "hidden" || s.State == "veryHidden" {
			continue
		}
		part := targets[s.RID]
		if part == "" {
			part = "xl/worksheets/sheet" + strconv.Itoa(i+1) + ".xml"
		}
		sh, err := x.sheet(part)
		if err != nil {
			continue
		}
		sh.Name = s.Name
		out = append(out, sh)
	}
	if len(out) == 0 {
		return nil, ErrNoSheets
	}
	return out, nil
}

func (x *xlsx) readStrings() {
	b, err := x.read("xl/sharedStrings.xml")
	if err != nil {
		return
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	var cur strings.Builder
	in, inT, phonetic := false, false, false
	for {
		tok, err := d.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				in = true
				cur.Reset()
			case "t":
				inT = in && !phonetic
			case "rPh":
				phonetic = true // the reading of Japanese text, not shown
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				x.strings = append(x.strings, cur.String())
				in = false
			case "t":
				inT = false
			case "rPh":
				phonetic = false
			}
		case xml.CharData:
			if inT {
				cur.Write(t)
			}
		}
	}
}

var builtinDates = map[int]string{14: "date", 15: "date", 16: "date", 17: "date", 18: "time", 19: "time", 20: "time", 21: "time", 22: "datetime",
	27: "date", 30: "date", 31: "date", 36: "date", 45: "time", 46: "time", 47: "time", 50: "date", 57: "date", 58: "date"}

var quotedRE = regexp.MustCompile(`"[^"]*"|\[[^\]]*\]|\\.`)

// fmtKind says whether a number format shows a date, a time or both.
func fmtKind(code string) string {
	c := strings.ToLower(quotedRE.ReplaceAllString(code, ""))
	date := strings.ContainsAny(c, "yd") || strings.Contains(c, "mmm")
	tm := strings.ContainsAny(c, "hs")
	switch {
	case date && tm:
		return "datetime"
	case date:
		return "date"
	case tm:
		return "time"
	}
	return ""
}

func (x *xlsx) readStyles() {
	b, err := x.read("xl/styles.xml")
	if err != nil {
		return
	}
	var st struct {
		NumFmts []struct {
			ID   int    `xml:"numFmtId,attr"`
			Code string `xml:"formatCode,attr"`
		} `xml:"numFmts>numFmt"`
		Xfs []struct {
			NumFmt int `xml:"numFmtId,attr"`
		} `xml:"cellXfs>xf"`
	}
	if xml.Unmarshal(b, &st) != nil {
		return
	}
	custom := map[int]string{}
	for _, f := range st.NumFmts {
		if k := fmtKind(f.Code); k != "" {
			custom[f.ID] = k
		}
	}
	for i, xf := range st.Xfs {
		if k, ok := builtinDates[xf.NumFmt]; ok {
			x.dateFmt[i] = k
		} else if k, ok := custom[xf.NumFmt]; ok {
			x.dateFmt[i] = k
		}
	}
}

// cellRef splits "AB12" into column 27 (from 0) and row 11 (from 0).
func cellRef(ref string) (row, col int, ok bool) {
	i := 0
	for i < len(ref) && ref[i] >= 'A' && ref[i] <= 'Z' {
		col = col*26 + int(ref[i]-'A'+1)
		i++
	}
	if i == 0 || i == len(ref) {
		return 0, 0, false
	}
	r, err := strconv.Atoi(ref[i:])
	if err != nil || r < 1 {
		return 0, 0, false
	}
	return r - 1, col - 1, true
}

func (x *xlsx) number(v string, style int) string {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return v
	}
	if kind, ok := x.dateFmt[style]; ok && f >= 0 && f < 2958466 {
		base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
		if x.date1904 {
			base = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		t := base.Add(time.Duration(math.Round(f*86400)) * time.Second)
		switch kind {
		case "date":
			return t.Format("2006-01-02")
		case "time":
			return t.Format("15:04:05")
		default:
			return t.Format("2006-01-02 15:04")
		}
	}
	// as Excel shows a general number: no float noise
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func (x *xlsx) sheet(part string) (Sheet, error) {
	b, err := x.read(part)
	if err != nil {
		return Sheet{}, err
	}
	var sh Sheet
	d := xml.NewDecoder(bytes.NewReader(b))
	var (
		row, col, style int
		typ, val        string
		inV, inIs       bool
		haveCell        bool
		nextRow         int
	)
	set := func(r, c int, v string) {
		if r >= SheetMaxRows || c >= SheetMaxCols {
			sh.Truncated = true
			return
		}
		for len(sh.Rows) <= r {
			sh.Rows = append(sh.Rows, nil)
		}
		for len(sh.Rows[r]) <= c {
			sh.Rows[r] = append(sh.Rows[r], "")
		}
		sh.Rows[r][c] = v
	}
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				row, col = nextRow, 0
				for _, a := range t.Attr {
					if a.Name.Local == "r" {
						if n, err := strconv.Atoi(a.Value); err == nil && n >= 1 {
							row = n - 1
						}
					}
				}
				nextRow = row + 1
			case "c":
				haveCell, typ, val, style = true, "", "", 0
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						if r, c, ok := cellRef(a.Value); ok {
							row, col = r, c
						}
					case "t":
						typ = a.Value
					case "s":
						style, _ = strconv.Atoi(a.Value)
					}
				}
			case "v":
				inV = true
			case "is":
				inIs = true
			case "t":
				// inline string text
			case "mergeCell":
				for _, a := range t.Attr {
					if a.Name.Local == "ref" {
						from, to, ok := strings.Cut(a.Value, ":")
						r1, c1, ok1 := cellRef(from)
						r2, c2, ok2 := cellRef(to)
						if ok && ok1 && ok2 && r1 < SheetMaxRows && c1 < SheetMaxCols {
							sh.Merges = append(sh.Merges, [4]int{r1, c1, min(r2, SheetMaxRows-1), min(c2, SheetMaxCols-1)})
						}
					}
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "v":
				inV = false
			case "is":
				inIs = false
			case "c":
				if !haveCell {
					break
				}
				haveCell = false
				var out string
				switch typ {
				case "s":
					if i, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && i >= 0 && i < len(x.strings) {
						out = x.strings[i]
					}
				case "b":
					out = map[string]string{"1": "TRUE", "0": "FALSE"}[strings.TrimSpace(val)]
				case "inlineStr", "str", "e":
					out = val
				default:
					if strings.TrimSpace(val) != "" {
						out = x.number(strings.TrimSpace(val), style)
					}
				}
				if out != "" {
					set(row, col, out)
				}
				col++
			}
		case xml.CharData:
			if inV || inIs {
				val += string(t)
			}
		}
	}
	// a sheet often ends in empty rows that only carried a style
	for len(sh.Rows) > 0 && len(sh.Rows[len(sh.Rows)-1]) == 0 {
		sh.Rows = sh.Rows[:len(sh.Rows)-1]
	}
	return sh, nil
}
