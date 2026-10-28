package parser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// office are the Office formats Gotenberg's LibreOffice route converts
// (design doc 4.2): the original is kept, the converted PDF is the preview
// and goes through the PDF steps (text layer, OCR, thumbnail).
var officeTypes = map[string]string{
	"application/msword": "doc",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "docx",
	"application/vnd.ms-excel": "xls",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "xlsx",
	"application/vnd.ms-powerpoint":                                             "ppt",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": "pptx",
	"application/vnd.oasis.opendocument.text":                                   "odt",
	"application/vnd.oasis.opendocument.spreadsheet":                            "ods",
	"application/vnd.oasis.opendocument.presentation":                           "odp",
	"text/rtf": "rtf",
}

// AcceptedTypes lists the MIME types the parser takes. Office documents
// and e-mails are taken without a conversion service too (v1.9.1), and wait
// for it.
func AcceptedTypes() []string {
	out := make([]string, 0, len(supported)+len(officeTypes)+len(mediaTypes)+1)
	for _, m := range []map[string]string{supported, officeTypes, mediaTypes} {
		for t := range m {
			out = append(out, t)
		}
	}
	out = append(out, emailType, msgType, ofdType, zipType)
	slices.Sort(out)
	return slices.Compact(out)
}

// IsOffice reports whether mime is an Office format.
func IsOffice(mime string) bool { _, ok := officeTypes[mime]; return ok }

// convertTimeout bounds one conversion; big spreadsheets take a while.
const convertTimeout = 3 * time.Minute

// convertOffice asks Gotenberg to turn src into a PDF in workDir.
func convertOffice(ctx context.Context, baseURL, src, ext, workDir string) (string, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("files", "document."+ext)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}
	cctx, cancel := context.WithTimeout(ctx, convertTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, baseURL+"/forms/libreoffice/convert", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("连接 Gotenberg 失败：%w", err) // temporary: retried
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusBadRequest:
		return "", &Error{Message: "Office 文档无法转换，文件可能已损坏或加密", Permanent: true}
	case resp.StatusCode != http.StatusOK:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("Gotenberg 返回 %d：%s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	dst := filepath.Join(workDir, "converted.pdf")
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return "", err
	}
	return dst, out.Close()
}

// OfficeTypes lists the Office MIME types (v1.6.1: the file format filter).
func OfficeTypes() []string {
	out := make([]string, 0, len(officeTypes))
	for m := range officeTypes {
		out = append(out, m)
	}
	slices.Sort(out)
	return out
}
