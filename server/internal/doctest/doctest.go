// Package doctest makes Word 97–2003 documents (.doc) and OpenDocument
// texts (.odt) for tests and the demo (v1.11.1).
//
// HandbookDoc and HandbookOdt are real files, written by macOS's textutil
// from files/handbook.html: what an application other than the reader
// makes. Doc and Odt build documents with what textutil does not write —
// heading styles, lists, notes, pictures, merged cells — the .doc from
// [MS-DOC] and [MS-CFB], the .odt from OpenDocument 1.2.
package doctest

import (
	_ "embed"
)

//go:embed files/handbook.doc
var handbookDoc []byte

//go:embed files/handbook.odt
var handbookOdt []byte

// HandbookDoc is 家庭应急手册 saved as a .doc by textutil.
func HandbookDoc() []byte { return append([]byte(nil), handbookDoc...) }

// HandbookOdt is the same saved as an .odt.
func HandbookOdt() []byte { return append([]byte(nil), handbookOdt...) }
