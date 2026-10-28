package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"kept/server/internal/i18n"
	"kept/server/internal/ingest"
	"kept/server/internal/parser"
)

// v1.10.1: uploads, failed uploads and what the ingest packages write, in
// the request's language.
func TestUploadTextsInEnglish(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")

	// without the header: Chinese
	if r := alice.upload("empty.txt", "", nil); r.code != http.StatusBadRequest || r.body["message"] != "文件是空的" {
		t.Fatalf("empty upload: %d %s", r.code, r.raw)
	}
	alice.headers["Accept-Language"] = "en"
	if r := alice.upload("empty.txt", "", nil); r.code != http.StatusBadRequest || r.body["message"] != "The file is empty" {
		t.Fatalf("empty upload in English: %d %s", r.code, r.raw)
	}
	// a mailbox's settings (a paid module, v1.13)
	if r := alice.do(http.MethodPost, "/api/v1/mail-accounts", map[string]any{"host": "", "username": "a"}); eeBuild && (r.code != http.StatusBadRequest ||
		r.body["message"] != "Enter the mail server address") {
		t.Fatalf("mail account in English: %d %s", r.code, r.raw)
	}
}

func TestJobErrorsInEnglish(t *testing.T) {
	for stored, want := range map[string]string{
		parser.ErrUnsupported.Message:       "This file format is not supported. Upload a PDF, an image (JPG, PNG, TIFF, HEIC, WebP, GIF, BMP), a text file, an Office document or an EPUB ebook.",
		parser.ErrEncrypted.Message + "（x）": "The PDF is encrypted. Remove the password before uploading.",
		parser.ErrCorrupt.Message:           "The file is damaged and cannot be read",
		"context deadline 超时":               "Processing timed out. Please try again later.",
		"something else":                    "Processing failed. Please try again later.",
	} {
		if got := i18n.Message(i18n.En, ingest.JobErrorMessage(stored)); got != want {
			t.Errorf("%q: %q, want %q", stored, got, want)
		}
	}
	// parser errors made with values, and messages built of messages
	var pe *parser.Error
	if err := parser.ErrDRM; !errors.As(err, &pe) || i18n.Message(i18n.En, pe.Message) != "This EPUB has digital rights management (DRM) and cannot be read" {
		t.Errorf("DRM: %v", err)
	}
	for zh, want := range map[string]string{
		"EPUB 文件已损坏，无法读取（OPF 无法解析）":              "The EPUB file is damaged and cannot be read (OPF 无法解析)",
		"文件超过 200 MB 的上限":                        "File exceeds the 200 MB limit",
		"2 份文档没能移过去，稍后会再试。例如：读取 a/b.pdf：EOF":     "2 documents could not be moved and will be tried again later. For example: Reading a/b.pdf: EOF",
		"张三 上传了“租房协议”的第 3 版":                     "张三 uploaded version 3 of “租房协议”",
		"Joy Dong 旋转了第 1、3 页，删除了第 2 页（“租房协议”）":   "Joy Dong rotated page 1、3 and deleted page 2 of “租房协议”",
		"空间“家”里已经有同一个文件：《租房协议》（在回收站中）":           "The space “家” already has the same file: “租房协议” (in Trash)",
		"按空间的设置（最多保留 2 个旧版本）删除了“租房协议”最早的 1 个旧版本": "Following the space setting (keep at most 2 earlier versions), deleted the oldest 1 earlier versions of “租房协议”",
		"210 × 297 毫米（A4）":                       "210 × 297 mm (A4)",
		"发件人不含“bank”":                            "Sender does not contain “bank”", // mail rules: a paid module (v1.13)
	} {
		if !eeBuild && strings.HasPrefix(zh, "发件人") {
			continue
		}
		if got := i18n.Message(i18n.En, zh); got != want {
			t.Errorf("%q: %q, want %q", zh, got, want)
		}
	}
}
