package parser

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"io"
	"path"
	"strings"
)

// A small XML tree for the Word reader (v1.9.8): the parts are read whole
// and walked, as the reading view needs most of what is in them.

type xnode struct {
	space, local string
	attr         []xml.Attr
	kids         []*xnode
	text         string // the character data directly inside
}

const (
	maxXMLDepth = 256
	maxXMLNodes = 3_000_000
)

// ErrWordTooBig: a part of the Word document is too large to read.
var ErrWordTooBig = errors.New("parser: the Word document is too large to show")

func parseXML(r io.Reader) (*xnode, error) { return parseXMLMixed(r, false) }

// textNode names the character data kept as children by parseXMLMixed.
const textNode = "#text"

// parseXMLMixed is parseXML that, with mixed, also keeps each piece of
// character data as a child (OpenDocument interleaves text and elements).
func parseXMLMixed(r io.Reader, mixed bool) (*xnode, error) {
	d := xml.NewDecoder(r)
	d.Strict = false
	root := &xnode{}
	stack := []*xnode{root}
	nodes := 0
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if nodes++; nodes > maxXMLNodes || len(stack) > maxXMLDepth {
				return nil, ErrWordTooBig
			}
			n := &xnode{space: t.Name.Space, local: t.Name.Local, attr: t.Attr}
			top := stack[len(stack)-1]
			top.kids = append(top.kids, n)
			stack = append(stack, n)
		case xml.EndElement:
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case xml.CharData:
			top := stack[len(stack)-1]
			top.text += string(t)
			if mixed {
				if nodes++; nodes > maxXMLNodes {
					return nil, ErrWordTooBig
				}
				top.kids = append(top.kids, &xnode{local: textNode, text: string(t)})
			}
		}
	}
	return root, nil
}

// readPart parses a part of the package, or returns nil when it is not
// there. A part larger than limit when unpacked is refused.
func readPart(z *zip.Reader, name string, limit int64) (*xnode, error) {
	f := zipFile(z, name)
	if f == nil {
		return nil, nil
	}
	if f.UncompressedSize64 > uint64(limit) {
		return nil, ErrWordTooBig
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return parseXML(io.LimitReader(rc, limit))
}

func zipFile(z *zip.Reader, name string) *zip.File {
	for _, f := range z.File {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return nil
}

// isW: an element of WordprocessingML (transitional or strict).
func (n *xnode) isW() bool {
	return strings.HasSuffix(n.space, "/wordprocessingml/2006/main") || strings.HasSuffix(n.space, "/wordprocessingml/main")
}

// w is the first child named local in the Word namespace.
func (n *xnode) w(local string) *xnode {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.local == local && k.isW() {
			return k
		}
	}
	return nil
}

// at is the attribute named local, whatever its namespace.
func (n *xnode) at(local string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

func (n *xnode) has(local string) bool {
	if n == nil {
		return false
	}
	for _, a := range n.attr {
		if a.Name.Local == local {
			return true
		}
	}
	return false
}

// val is the w:val of the child named local; ok when the child is there.
func (n *xnode) val(local string) (string, bool) {
	k := n.w(local)
	if k == nil {
		return "", false
	}
	return k.at("val"), true
}

// on reads a toggle property (w:b, w:i …): there and not false.
func (n *xnode) on(local string) (bool, bool) {
	v, ok := n.val(local)
	if !ok {
		return false, false
	}
	return v != "0" && v != "false" && v != "off" && v != "none", true
}

// find is the first descendant named local (any namespace), depth first.
func (n *xnode) find(local string) *xnode {
	for _, k := range n.kids {
		if k.local == local {
			return k
		}
		if f := k.find(local); f != nil {
			return f
		}
	}
	return nil
}

// all lists the descendants named local, in document order.
func (n *xnode) all(local string, out []*xnode) []*xnode {
	for _, k := range n.kids {
		if k.local == local {
			out = append(out, k)
			continue
		}
		out = k.all(local, out)
	}
	return out
}

// partPath resolves a relationship's target against the part it is from.
func partPath(from, target string) (string, bool) {
	if strings.HasPrefix(target, "/") {
		target = strings.TrimPrefix(target, "/")
	} else {
		target = path.Join(path.Dir(from), target)
	}
	target = path.Clean(target)
	if strings.HasPrefix(target, "../") || target == ".." {
		return "", false
	}
	return target, true
}

type wordRel struct {
	target   string
	external bool
}

func readRels(z *zip.Reader, part string) map[string]wordRel {
	out := map[string]wordRel{}
	n, err := readPart(z, path.Join(path.Dir(part), "_rels", path.Base(part)+".rels"), 8<<20)
	if err != nil || n == nil {
		return out
	}
	for _, r := range n.all("Relationship", nil) {
		ext := strings.EqualFold(r.at("TargetMode"), "External")
		t := r.at("Target")
		if !ext {
			p, ok := partPath(part, t)
			if !ok {
				continue
			}
			t = p
		}
		out[r.at("Id")] = wordRel{target: t, external: ext}
	}
	return out
}
