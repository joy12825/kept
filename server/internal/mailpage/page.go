// Package mailpage turns an e-mail into a page to print (v1.4.0, moved
// here in v1.5.1): the message's own text with who sent it, when and why on
// top, with nothing in it that loads from the network or runs. Mail fetching
// and .eml uploads both use it.
package mailpage

import (
	"bytes"
	"fmt"
	"html"
	"mime"
	"regexp"
	"strconv"
	"strings"
	"time"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Message is what a page is made of.
type Message struct {
	From, To, Subject string
	Date              time.Time
	HTML, Text        string
	Inline            map[string]Inline // images by Content-ID, without <>
}

// Inline is an image the body shows.
type Inline struct {
	MIME string
	Data []byte
}

const maxInline = 10 << 20 // images inside one message

// Nothing in it loads from the network: remote images, styles, scripts and
// frames are dropped, and a content security policy forbids the rest.
// Page renders m as an HTML page for printing, with the message's header
// on top, and returns it with the images it refers to by file name.
func Page(m Message) (index []byte, assets map[string][]byte) {
	assets = map[string][]byte{}
	names := map[string]string{} // cid → file name
	total := 0
	for cid, p := range m.Inline {
		if total+len(p.Data) > maxInline {
			continue
		}
		exts, _ := mime.ExtensionsByType(p.MIME)
		ext := ".img"
		if len(exts) > 0 {
			ext = exts[len(exts)-1]
		}
		name := "inline-" + strconv.Itoa(len(names)+1) + ext
		names[strings.ToLower(cid)] = name
		assets[name] = p.Data
		total += len(p.Data)
	}

	var doc *nethtml.Node
	if strings.TrimSpace(m.HTML) != "" {
		doc, _ = nethtml.Parse(strings.NewReader(m.HTML))
	}
	if doc == nil {
		doc, _ = nethtml.Parse(strings.NewReader(`<pre class="kept-text"></pre>`))
		if pre := find(doc, atom.Pre); pre != nil {
			pre.AppendChild(&nethtml.Node{Type: nethtml.TextNode, Data: m.Text})
		}
	}
	clean(doc, names)
	head, body := find(doc, atom.Head), find(doc, atom.Body)
	if head != nil {
		for _, n := range fragment(head, headHTML) {
			head.InsertBefore(n, head.FirstChild)
		}
	}
	if body != nil {
		for _, n := range fragment(body, headerHTML(m)) {
			body.InsertBefore(n, body.FirstChild)
		}
	}
	var out bytes.Buffer
	_ = nethtml.Render(&out, doc)
	return out.Bytes(), assets
}

const headHTML = `<meta charset="utf-8">` +
	`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: file: 'self'; style-src 'unsafe-inline'; font-src data:">` +
	`<style>body{font-family:"PingFang SC","Noto Sans CJK SC","Microsoft YaHei",sans-serif}.kept-header{font:13px/1.6 "PingFang SC","Noto Sans CJK SC","Microsoft YaHei",sans-serif;color:#333;border-bottom:1px solid #ccc;margin:0 0 16px;padding:0 0 8px}` +
	`.kept-header td{padding:1px 8px 1px 0;vertical-align:top}.kept-header td:first-child{color:#777;white-space:nowrap}` +
	`.kept-text{white-space:pre-wrap;font:14px/1.7 "PingFang SC","Noto Sans CJK SC",sans-serif}</style>`

// headerHTML is the block of who, when and what on top of the page.
func headerHTML(m Message) string {
	row := func(k, v string) string {
		if strings.TrimSpace(v) == "" {
			return ""
		}
		return "<tr><td>" + k + "</td><td>" + html.EscapeString(v) + "</td></tr>"
	}
	when := ""
	if !m.Date.IsZero() {
		when = m.Date.Format("2006-01-02 15:04")
	}
	return `<div class="kept-header"><table>` + row("发件人", m.From) + row("收件人", m.To) + row("时间", when) + row("主题", m.Subject) + `</table></div>`
}

func fragment(ctx *nethtml.Node, s string) []*nethtml.Node {
	nodes, _ := nethtml.ParseFragment(strings.NewReader(s), ctx)
	// inserted before the first child one after another: keep the order
	for i, j := 0, len(nodes)-1; i < j; i, j = i+1, j-1 {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	}
	return nodes
}

func find(n *nethtml.Node, a atom.Atom) *nethtml.Node {
	if n.Type == nethtml.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := find(c, a); f != nil {
			return f
		}
	}
	return nil
}

// dropped are elements removed with their content: they run code, embed
// other documents, load from elsewhere or take input.
var dropped = map[atom.Atom]bool{
	atom.Script: true, atom.Noscript: true, atom.Iframe: true, atom.Frame: true, atom.Frameset: true, atom.Object: true,
	atom.Embed: true, atom.Applet: true, atom.Link: true, atom.Meta: true, atom.Base: true, atom.Audio: true, atom.Video: true,
	atom.Source: true, atom.Track: true, atom.Svg: true, atom.Math: true, atom.Input: true, atom.Button: true, atom.Select: true,
	atom.Textarea: true, atom.Template: true, atom.Form: true,
}

// urlAttrs may make the browser load something.
var urlAttrs = map[string]bool{"src": true, "srcset": true, "background": true, "poster": true, "href": true, "action": true,
	"formaction": true, "data": true, "xlink:href": true, "lowsrc": true, "dynsrc": true, "longdesc": true, "cite": true, "ping": true}

// clean removes what could load or run anything, keeping links as text
// targets (printing does not follow them) and images from the message
// itself (cid: → file name) or data: URIs.
func clean(n *nethtml.Node, names map[string]string) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		switch {
		case c.Type == nethtml.CommentNode:
			n.RemoveChild(c)
		case c.Type == nethtml.ElementNode && (dropped[c.DataAtom] || c.DataAtom == 0 && strings.Contains(c.Data, ":")):
			n.RemoveChild(c)
		case c.Type == nethtml.ElementNode && c.DataAtom == atom.Style:
			for t := c.FirstChild; t != nil; t = t.NextSibling {
				if t.Type == nethtml.TextNode {
					t.Data = cleanCSS(t.Data)
				}
			}
		case c.Type == nethtml.ElementNode:
			cleanAttrs(c, names)
			clean(c, names)
		}
		c = next
	}
}

func cleanAttrs(e *nethtml.Node, names map[string]string) {
	kept := e.Attr[:0]
	for _, a := range e.Attr {
		key := strings.ToLower(a.Key)
		if a.Namespace != "" {
			key = strings.ToLower(a.Namespace) + ":" + key
		}
		val := strings.TrimSpace(a.Val)
		switch {
		case strings.HasPrefix(key, "on"):
			continue
		case key == "style":
			a.Val = cleanCSS(a.Val)
		case key == "href" && e.DataAtom == atom.A:
			if l := strings.ToLower(val); !strings.HasPrefix(l, "http:") && !strings.HasPrefix(l, "https:") && !strings.HasPrefix(l, "mailto:") {
				continue
			}
		case key == "src" && e.DataAtom == atom.Img:
			if name, ok := inlineRef(val, names); ok {
				a.Val = name
			} else if !strings.HasPrefix(strings.ToLower(val), "data:image/") {
				continue // a remote image: not fetched (tracking pixels, “read” receipts)
			}
		case urlAttrs[key]:
			continue
		}
		kept = append(kept, a)
	}
	e.Attr = kept
}

func inlineRef(src string, names map[string]string) (string, bool) {
	if len(src) < 4 || !strings.EqualFold(src[:4], "cid:") {
		return "", false
	}
	name, ok := names[strings.ToLower(strings.Trim(src[4:], "<>"))]
	return name, ok
}

var (
	cssImport = regexp.MustCompile(`(?i)@import[^;]*;?`)
	cssURL    = regexp.MustCompile(`(?i)url\s*\(\s*(['"]?)\s*([^)'"]*)['"]?\s*\)`)
	cssExpr   = regexp.MustCompile(`(?i)expression\s*\(|behavior\s*:|-moz-binding`)
)

// cleanCSS keeps a style but lets it load nothing but data: URIs.
func cleanCSS(s string) string {
	s = cssImport.ReplaceAllString(s, "")
	s = cssExpr.ReplaceAllString(s, "x:")
	return cssURL.ReplaceAllStringFunc(s, func(u string) string {
		m := cssURL.FindStringSubmatch(u)
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(m[2])), "data:") {
			return u
		}
		return "none"
	})
}

// Title is a page's title: the subject, else who sent it.
func Title(m Message) string {
	t := strings.Join(strings.Fields(m.Subject), " ")
	if t == "" {
		t = fmt.Sprintf("邮件（%s）", strings.TrimSpace(m.From))
	}
	if r := []rune(t); len(r) > 200 {
		t = string(r[:200])
	}
	return t
}

// PlainText is the visible text of an HTML body (v1.5.7, for the mail
// rule on words in the body): text nodes outside script and style, with
// the line breaks of block elements.
func PlainText(html string) string {
	if strings.TrimSpace(html) == "" {
		return ""
	}
	doc, err := nethtml.Parse(strings.NewReader(html))
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(n *nethtml.Node)
	walk = func(n *nethtml.Node) {
		switch {
		case n.Type == nethtml.ElementNode && (n.DataAtom == atom.Script || n.DataAtom == atom.Style || n.DataAtom == atom.Head):
			return
		case n.Type == nethtml.TextNode:
			b.WriteString(n.Data)
		case n.Type == nethtml.ElementNode && (n.DataAtom == atom.Br || n.DataAtom == atom.P || n.DataAtom == atom.Div || n.DataAtom == atom.Tr || n.DataAtom == atom.Li):
			b.WriteByte('\n')
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return b.String()
}
