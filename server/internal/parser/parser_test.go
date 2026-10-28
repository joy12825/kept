package parser

import (
	"context"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"

	"kept/server/internal/msgtest"
)

func needTools(t *testing.T) {
	t.Helper()
	if m := MissingTools(); len(m) > 0 {
		t.Skipf("missing tools: %v", m)
	}
}

var cfg = Config{OCRLanguages: "eng", OCRMode: "skip"}

// textPDF writes a one-page PDF with a real text layer.
func textPDF(t *testing.T, dir string, lines ...string) string {
	t.Helper()
	p := fpdf.New("P", "mm", "A4", "")
	p.AddPage()
	p.SetFont("Helvetica", "", 20)
	for _, l := range lines {
		p.Cell(0, 14, l)
		p.Ln(16)
	}
	path := filepath.Join(dir, "text.pdf")
	if err := p.OutputFileAndClose(path); err != nil {
		t.Fatal(err)
	}
	return path
}

var invoiceLines = []string{"ACME POWER COMPANY", "Electricity bill for March", "Issued 2024-03-05", "Total due 128.50", "Account 0012345678"}

func TestFindDate(t *testing.T) {
	now := time.Date(2024, 10, 8, 0, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"开票日期：2024年3月5日":              "2024-03-05",
		"日期 2024 年 03 月 05 日":         "2024-03-05",
		"Issued 2024-03-05":           "2024-03-05",
		"2024/3/5 打印":                 "2024-03-05",
		"2024.03.05":                  "2024-03-05",
		"账号 2024-13-40，日期 2023-01-02": "2023-01-02", // invalid first, valid second
		"1985-01-01 then 2020-02-29":  "2020-02-29",
		"2月30日 2024-02-30":            "",
		"nothing here":                "",
		"2030-01-01":                  "",
		"先 2022/1/1 再 2023年1月1日":      "2022-01-01", // the earliest wins
	}
	for in, want := range cases {
		got, ok := FindDate(in, now)
		if got != want || ok != (want != "") {
			t.Errorf("FindDate(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if m, e, err := Detect(write("a.bin", "hello 你好"), "notes.txt"); err != nil || m != "text/plain" || e != "txt" {
		t.Errorf("text = %s %s %v", m, e, err)
	}
	if m, e, _ := Detect(write("b", "# 标题"), "readme.md"); m != "text/markdown" || e != "md" {
		t.Errorf("markdown = %s %s", m, e)
	}
	if _, _, err := Detect(write("c", "PK\x03\x04\x14\x00\x06\x00"), "x.docx"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("zip err = %v", err)
	}
	for name, markup := range map[string]string{
		"page.html": "<!DOCTYPE html><html><body><script>alert(1)</script></body></html>",
		"pic.svg":   `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"data.xml":  `<?xml version="1.0"?><a>b</a>`,
	} {
		if _, _, err := Detect(write(name, markup), name+".txt"); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: err = %v, want unsupported", name, err)
		}
	}
	if _, _, err := Detect(write("d", ""), "empty.pdf"); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty err = %v", err)
	}
	// A PDF named .txt is still a PDF: the content decides.
	pdf := textPDF(t, dir, "hello")
	if m, e, _ := Detect(pdf, "disguised.txt"); m != "application/pdf" || e != "pdf" {
		t.Errorf("pdf = %s %s", m, e)
	}
}

func TestTextFiles(t *testing.T) {
	dir := t.TempDir()
	gbk := []byte{0xc4, 0xe3, 0xba, 0xc3} // 你好 in GB18030
	p := filepath.Join(dir, "gbk.txt")
	_ = os.WriteFile(p, gbk, 0o644)
	res, err := Parse(context.Background(), cfg, p, "gbk.txt", dir, nil)
	if err != nil || res.Content != "你好" || res.ArchivePath != "" || res.ThumbnailPath != "" {
		t.Errorf("gbk text = %+v, %v", res, err)
	}
}

func TestPDFWithTextLayerSkipsOCR(t *testing.T) {
	needTools(t)
	dir := t.TempDir()
	src := textPDF(t, dir, invoiceLines...)
	var stages []string
	res, err := Parse(context.Background(), cfg, src, "bill.pdf", dir, func(s string, _ int) { stages = append(stages, s) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Content, "ACME POWER COMPANY") || res.PageCount != 1 || res.MimeType != "application/pdf" {
		t.Errorf("result = %+v", res)
	}
	if res.ArchivePath != "" {
		t.Error("a PDF that already has text needs no archive copy in skip mode")
	}
	if strings.Contains(strings.Join(stages, ","), "ocr") {
		t.Errorf("OCR ran on a text PDF: stages %v", stages)
	}
	if st, err := os.Stat(res.ThumbnailPath); err != nil || st.Size() == 0 {
		t.Errorf("thumbnail: %v", err)
	}
}

// A scanned page: render the text PDF to an image, then OCR the image.
func TestImageIsOCRed(t *testing.T) {
	needTools(t)
	dir := t.TempDir()
	src := textPDF(t, dir, invoiceLines...)
	if out, err := exec.Command("pdftoppm", "-png", "-r", "200", "-singlefile", src, filepath.Join(dir, "scan")).CombinedOutput(); err != nil {
		t.Fatalf("render: %v %s", err, out)
	}
	res, err := Parse(context.Background(), cfg, filepath.Join(dir, "scan.png"), "scan.png", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(res.Content), "ACME") || res.ArchivePath == "" || res.PageCount != 1 || res.Ext != "png" {
		t.Errorf("OCR result = %+v", res)
	}
	if d, ok := FindDate(res.Content, time.Now()); !ok || d != "2024-03-05" {
		t.Errorf("date from OCR text = %q, %v (text %q)", d, ok, res.Content)
	}
}

// Screenshots are usually RGBA PNGs, which ocrmypdf rejects unless the
// alpha channel is removed first.
func TestImageWithAlphaIsOCRed(t *testing.T) {
	needTools(t)
	dir := t.TempDir()
	src := textPDF(t, dir, invoiceLines...)
	if out, err := exec.Command("pdftoppm", "-png", "-r", "200", "-singlefile", src, filepath.Join(dir, "scan")).CombinedOutput(); err != nil {
		t.Fatalf("render: %v %s", err, out)
	}
	f, _ := os.Open(filepath.Join(dir, "scan.png"))
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	rgba := image.NewNRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)
	rgba.Pix[3] = 0 // one transparent pixel, so the encoder keeps the alpha channel
	shot := filepath.Join(dir, "shot.png")
	out, _ := os.Create(shot)
	_ = png.Encode(out, rgba)
	out.Close()
	if b, _ := os.ReadFile(shot); !pngHasAlpha(b) {
		t.Fatal("test image should have an alpha channel")
	}

	res, err := Parse(context.Background(), cfg, shot, "shot.png", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(res.Content), "ACME") || res.ArchivePath == "" {
		t.Errorf("OCR result = %+v", res)
	}
}

func TestEncryptedAndCorruptPDFs(t *testing.T) {
	needTools(t)
	dir := t.TempDir()
	p := fpdf.New("P", "mm", "A4", "")
	p.SetProtection(fpdf.CnProtectPrint, "user-secret", "owner-secret")
	p.AddPage()
	p.SetFont("Helvetica", "", 12)
	p.Cell(0, 10, "secret")
	enc := filepath.Join(dir, "enc.pdf")
	if err := p.OutputFileAndClose(enc); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(context.Background(), cfg, enc, "enc.pdf", dir, nil); !errors.Is(err, ErrEncrypted) || !IsPermanent(err) {
		t.Errorf("encrypted err = %v", err)
	}
	bad := filepath.Join(dir, "bad.pdf")
	_ = os.WriteFile(bad, []byte("%PDF-1.4\n garbage garbage garbage"), 0o644)
	if _, err := Parse(context.Background(), cfg, bad, "bad.pdf", dir, nil); !IsPermanent(err) {
		t.Errorf("corrupt err = %v, want a permanent error", err)
	}
}

func TestRunKillsOnTimeout(t *testing.T) {
	start := time.Now()
	_, err := run(context.Background(), 300*time.Millisecond, "sh", "-c", "sleep 30 & sleep 30")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("err = %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Error("the process group was not killed in time")
	}
}

func TestTextQuality(t *testing.T) {
	for s, want := range map[string]bool{
		"国家电网 电费通知单 本期应缴 128.50 元。":       true,
		"ACME POWER COMPANY, Invoice #42": true,
		" ab":                         false,
		"¤¦¬©®¤¦¬©® ¤¦¬©®":                false,
		"�����abc":                        false,
	} {
		if got := TextQuality(s) >= garbledBelow; got != want {
			t.Errorf("TextQuality(%q) = %.2f", s, TextQuality(s))
		}
	}
	if TextQuality("   ") != 0 {
		t.Error("empty text should score 0")
	}
}

// A PDF whose visible page is readable but whose text layer is junk (a font
// without a usable encoding): the text layer is read again by OCR.
func TestGarbledTextLayerIsOCRed(t *testing.T) {
	needTools(t)
	dir := t.TempDir()
	src := textPDF(t, dir, invoiceLines...)
	if out, err := exec.Command("pdftoppm", "-png", "-r", "200", "-singlefile", src, filepath.Join(dir, "page")).CombinedOutput(); err != nil {
		t.Fatalf("render: %v %s", err, out)
	}
	p := fpdf.New("P", "mm", "A4", "")
	p.AddPage()
	p.ImageOptions(filepath.Join(dir, "page.png"), 0, 0, 210, 297, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	tr := p.UnicodeTranslatorFromDescriptor("")
	p.SetFont("Helvetica", "", 12)
	p.SetTextRenderingMode(3) // invisible: only the text layer
	for i := 0; i < 6; i++ {
		p.Text(20, float64(30+10*i), tr("¤¦¬©®¤¦¬©®¤¦¬©®¤¦¬©®"))
	}
	bad := filepath.Join(dir, "garbled.pdf")
	if err := p.OutputFileAndClose(bad); err != nil {
		t.Fatal(err)
	}
	res, err := Parse(context.Background(), cfg, bad, "garbled.pdf", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToUpper(res.Content), "ACME") || res.ArchivePath == "" || !strings.Contains(strings.Join(res.Warnings, ""), "乱码") {
		t.Errorf("result = %q archive %q warnings %v", res.Content, res.ArchivePath, res.Warnings)
	}
}

// v1.9.1: sound and video are kept as they are; JSON and CSV are text.
func TestMediaAndDataFiles(t *testing.T) {
	dir := t.TempDir()
	// a tiny WAV: the RIFF header and a few silent samples
	wav := []byte("RIFF\x2c\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x40\x1f\x00\x00\x80\x3e\x00\x00\x02\x00\x10\x00data\x08\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	p := filepath.Join(dir, "rec")
	_ = os.WriteFile(p, wav, 0o644)
	res, err := Parse(context.Background(), cfg, p, "录音.wav", dir, nil)
	if err != nil || res.MimeType != "audio/wav" || res.Ext != "wav" || res.Content != "" || res.ThumbnailPath != "" || res.ArchivePath != "" {
		t.Errorf("wav = %+v, %v", res, err)
	}
	for name, body := range map[string]string{"a.json": `{"电表": 5230}`, "b.csv": "日期,读数\n2026-10-01,5230\n"} {
		q := filepath.Join(dir, name)
		_ = os.WriteFile(q, []byte(body), 0o644)
		res, err := Parse(context.Background(), cfg, q, name, dir, nil)
		if err != nil || !strings.Contains(res.Content, "5230") {
			t.Errorf("%s = %+v, %v", name, res, err)
		}
	}
	types := AcceptedTypes()
	for _, m := range []string{"audio/mpeg", "video/mp4", "message/rfc822", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"} {
		if !slices.Contains(types, m) {
			t.Errorf("accepted types lack %s", m)
		}
	}
}

// v1.9.1: an e-mail without Gotenberg: its text and files, the PDF waits.
func TestEmailWithoutGotenberg(t *testing.T) {
	dir := t.TempDir()
	eml := "From: Bank <cc@bank.example>\r\nTo: me@example.com\r\nSubject: =?UTF-8?B?" + "5L+h55So5Y2h6LSm5Y2V" + "?=\r\nDate: Tue, 05 Mar 2024 10:00:00 +0800\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n本期应还 1,234.00 元\r\n--b\r\n" +
		"Content-Type: text/plain; name=\"明细.txt\"\r\nContent-Disposition: attachment; filename=\"detail.txt\"\r\n\r\n明细\r\n--b--\r\n"
	p := filepath.Join(dir, "bill.eml")
	_ = os.WriteFile(p, []byte(eml), 0o644)
	res, err := Parse(context.Background(), cfg, p, "bill.eml", dir, nil)
	if err != nil || res.Pending != PendingEmail || res.ArchivePath != "" || res.Email == nil {
		t.Fatalf("eml = %+v, %v", res, err)
	}
	if !strings.Contains(res.Content, "本期应还 1,234.00 元") || !strings.Contains(res.Content, "主题：信用卡账单") || len(res.Email.Attachments) != 1 {
		t.Errorf("content %q, files %d", res.Content, len(res.Email.Attachments))
	}
}

// v1.9.7: an Outlook .msg goes the .eml's way — an e-mail, its text, its
// files (not the logo its body shows), the PDF waiting without Gotenberg.
func TestOutlookMessage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "物业费.msg")
	_ = os.WriteFile(p, msgtest.Build(msgtest.Message{
		Subject: "物业费缴费通知", FromName: "物业服务中心", FromEmail: "wuye@example.com",
		To: []msgtest.Recipient{{Name: "王小明", Email: "wang@example.com"}}, GBK: true,
		Date: time.Date(2026, 10, 12, 9, 30, 0, 0, time.UTC),
		Body: "第四季度物业费 1,260 元", HTML: `<p>物业费 1,260 元</p><img src="cid:logo@wuye">`,
		Attachments: []msgtest.Attachment{
			{Name: "缴费单.txt", MIME: "text/plain", Data: []byte("缴费单")},
			{Name: "logo.png", CID: "logo@wuye", MIME: "image/png", Data: []byte("\x89PNG")},
		},
	}), 0o644)
	mime, ext, err := Detect(p, "物业费.msg")
	if err != nil || mime != "application/vnd.ms-outlook" || ext != "msg" || !IsEmail(mime) {
		t.Fatalf("detect = %q %q %v", mime, ext, err)
	}
	res, err := Parse(context.Background(), cfg, p, "物业费.msg", dir, nil)
	if err != nil || res.Pending != PendingEmail || res.Email == nil {
		t.Fatalf("msg = %+v, %v", res, err)
	}
	if !strings.Contains(res.Content, "主题：物业费缴费通知") || !strings.Contains(res.Content, "发件人：物业服务中心 <wuye@example.com>") || !strings.Contains(res.Content, "1,260 元") {
		t.Errorf("content %q", res.Content)
	}
	if len(res.Email.Attachments) != 1 || res.Email.Attachments[0].Name != "缴费单.txt" || res.Email.Subject != "物业费缴费通知" {
		t.Errorf("email %+v", res.Email)
	}
	if text, ok, err := ReadText(context.Background(), p, "物业费.msg"); err != nil || !ok || !strings.Contains(text, "1,260 元") {
		t.Errorf("read text %q %v %v", text, ok, err)
	}
	if !slices.Contains(AcceptedTypes(), "application/vnd.ms-outlook") {
		t.Error("accepted types lack .msg")
	}
}
