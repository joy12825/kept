package mailpage

import (
	"strings"
	"testing"
	"time"
)

func TestPageKeepsTheMessageAndDropsWhatLoadsOrRuns(t *testing.T) {
	m := Message{
		From: "招商银行 <cc@cmbchina.com>", To: "小王 <wang@example.com>", Subject: "信用卡电子账单 <2024年3月>",
		Date: time.Date(2024, 3, 5, 10, 0, 0, 0, time.Local),
		HTML: `<html><head><link rel="stylesheet" href="https://x.example/a.css"><style>@import url(https://x.example/b.css); .a{background:url('https://t.example/p.gif')} .b{background:url(data:image/png;base64,AAAA)}</style>
<script>alert(1)</script><meta http-equiv="refresh" content="0;url=https://evil.example"></head>
<body onload="steal()"><h1 style="background-image:url(http://t.example/q.png);color:red">本期应还 ¥1,280.00</h1>
<img src="cid:logo@cmb" alt="logo"><img src="https://track.example/open.gif?u=1"><img src="data:image/png;base64,iVBORw0KGgo=">
<a href="javascript:alert(1)">x</a><a href="https://www.cmbchina.com">官网</a><iframe src="https://x.example"></iframe>
<form action="https://x.example"><input name="p"></form><svg><image href="https://x.example/s.png"/></svg><!-- tracking --></body></html>`,
		Inline: map[string]Inline{"logo@cmb": {MIME: "image/png", Data: []byte("PNGDATA")}},
	}
	index, assets := Page(m)
	out := string(index)
	for _, bad := range []string{"<script", "alert(1)", "https://x.example", "t.example", "track.example", "onload", "javascript:", "<iframe", "<form", "<input", "<svg", "evil.example", "tracking", "@import"} {
		if strings.Contains(out, bad) {
			t.Errorf("page still has %q:\n%s", bad, out)
		}
	}
	for _, good := range []string{"本期应还 ¥1,280.00", `href="https://www.cmbchina.com"`, "data:image/png;base64,iVBORw0KGgo=", "url(data:image/png;base64,AAAA)",
		"color:red", "Content-Security-Policy", "招商银行 &lt;cc@cmbchina.com&gt;", "信用卡电子账单 &lt;2024年3月&gt;", "2024-03-05 10:00", "kept-header"} {
		if !strings.Contains(out, good) {
			t.Errorf("page lacks %q:\n%s", good, out)
		}
	}
	if len(assets) != 1 {
		t.Fatalf("assets = %v", assets)
	}
	for name, data := range assets {
		if string(data) != "PNGDATA" || !strings.Contains(out, `src="`+name+`"`) {
			t.Errorf("inline image %s not referenced:\n%s", name, out)
		}
	}
}

func TestPagePlainTextIsEscaped(t *testing.T) {
	index, _ := Page(Message{Subject: "水费", Text: "本月水费 <b>36</b> 元\n请按时缴纳"})
	out := string(index)
	if !strings.Contains(out, "&lt;b&gt;36&lt;/b&gt;") || !strings.Contains(out, `class="kept-text"`) || strings.Contains(out, "<b>36") {
		t.Errorf("plain text page:\n%s", out)
	}
	if got := Title(Message{From: "a@b.com"}); got != "邮件（a@b.com）" {
		t.Errorf("title without a subject = %q", got)
	}
}

func TestPlainText(t *testing.T) {
	got := PlainText(`<html><head><title>标题</title><style>p{color:red}</style></head><body><p>本月<b>物业费</b></p><script>var x="隐藏"</script><div>320 元</div></body></html>`)
	if !strings.Contains(got, "本月物业费") || !strings.Contains(got, "320 元") || strings.Contains(got, "隐藏") || strings.Contains(got, "color") || strings.Contains(got, "标题") {
		t.Errorf("got %q", got)
	}
	if PlainText("  ") != "" {
		t.Error("empty")
	}
}
