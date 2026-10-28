package parser

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// maxHTMLPDF bounds a converted page set (a mail body is far smaller).
const maxHTMLPDF = 64 << 20

// HTMLToPDF asks Gotenberg's Chromium to print index.html (with the files
// it refers to by name) as an A4 PDF (v1.4.0, mail bodies). The HTML must
// already be free of anything that loads from the network: Chromium would
// fetch it.
func HTMLToPDF(ctx context.Context, baseURL string, index []byte, assets map[string][]byte) ([]byte, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	add := func(name string, data []byte) error {
		part, err := mw.CreateFormFile("files", name)
		if err != nil {
			return err
		}
		_, err = part.Write(data)
		return err
	}
	if err := add("index.html", index); err != nil {
		return nil, err
	}
	for name, data := range assets {
		if err := add(name, data); err != nil {
			return nil, err
		}
	}
	// A4 with modest margins, in inches; backgrounds kept (coloured bills)
	for k, v := range map[string]string{
		"paperWidth": "8.27", "paperHeight": "11.7",
		"marginTop": "0.4", "marginBottom": "0.4", "marginLeft": "0.4", "marginRight": "0.4",
		"printBackground": "true",
	} {
		if err := mw.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, convertTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, baseURL+"/forms/chromium/convert/html", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 Gotenberg 失败：%w", err) // temporary: retried
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if resp.StatusCode == http.StatusBadRequest {
			return nil, &Error{Message: "邮件正文无法转换成 PDF", Permanent: true, Err: fmt.Errorf("%s", bytes.TrimSpace(msg))}
		}
		return nil, fmt.Errorf("Gotenberg 返回 %d：%s", resp.StatusCode, bytes.TrimSpace(msg))
	}
	out, err := io.ReadAll(io.LimitReader(resp.Body, maxHTMLPDF+1))
	if err != nil {
		return nil, err
	}
	if len(out) > maxHTMLPDF {
		return nil, &Error{Message: "邮件正文转换出的 PDF 太大", Permanent: true}
	}
	return out, nil
}
