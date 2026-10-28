package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"kept/server/internal/config"
	"kept/server/internal/system"
	"kept/server/internal/version"
	"kept/server/internal/watermark"
)

// runDoctor checks what a binary installation needs (v1.3.1): the
// external tools, the OCR languages, the data directory, and optional
// services. It exits 1 when something required is missing.
func runDoctor(ctx context.Context, cfg config.Config, stdout io.Writer) int {
	ok := true
	fail := func(format string, a ...any) { ok = false; fmt.Fprintf(stdout, "  ✗ "+format+"\n", a...) }
	pass := func(format string, a ...any) { fmt.Fprintf(stdout, "  ✓ "+format+"\n", a...) }

	fmt.Fprintf(stdout, "kept %s\n\n外部工具\n", version.Version)
	for _, t := range system.ExternalTools() {
		if path, err := exec.LookPath(t.Name); err == nil {
			pass("%-10s %s（%s）", t.Name, t.Purpose, path)
		} else if t.Optional {
			fmt.Fprintf(stdout, "  - %-10s %s：没有找到\n", t.Name, t.Purpose)
		} else {
			fail("%-10s %s：没有找到，见部署文档“二进制部署”的依赖安装", t.Name, t.Purpose)
		}
	}

	fmt.Fprintf(stdout, "\nOCR 语言（KEPT_OCR_LANGUAGES=%s）\n", cfg.OCRLanguages)
	if path, err := exec.LookPath("tesseract"); err == nil {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		out, _ := exec.CommandContext(cctx, path, "--list-langs").CombinedOutput()
		cancel()
		have := map[string]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			have[strings.TrimSpace(line)] = true
		}
		for _, lang := range strings.Split(cfg.OCRLanguages, "+") {
			if have[lang] {
				pass("%s", lang)
			} else {
				fail("%s：tesseract 没有这个语言包（Debian/Ubuntu：tesseract-ocr-%s）", lang, strings.ReplaceAll(lang, "_", "-"))
			}
		}
		// 自动转正（v1.3.9）要用方向检测的数据
		if cfg.OCRRotate {
			if have["osd"] {
				pass("osd（自动转正）")
			} else {
				fail("osd：没有方向检测数据，自动转正时识别会失败（Debian/Ubuntu：tesseract-ocr-osd；或设 KEPT_OCR_ROTATE=false）")
			}
		}
	} else {
		fail("没有 tesseract，无法检查")
	}

	fmt.Fprintf(stdout, "\n数据目录（KEPT_DATA_DIR=%s）\n", cfg.DataDir)
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		fail("无法创建：%v", err)
	} else if f, err := os.CreateTemp(cfg.DataDir, ".doctor-*"); err != nil {
		fail("不能写入：%v（检查目录的属主与权限）", err)
	} else {
		f.Close()
		os.Remove(f.Name())
		pass("可以写入")
	}

	fmt.Fprintln(stdout, "\n可选服务")
	watermark.SetFont(cfg.WatermarkFont)
	if p := watermark.Font(); p != "" {
		pass("分享水印的中文字体（%s）", p)
	} else {
		fmt.Fprintln(stdout, "  - 分享水印：没有找到中文字体，分享不能加水印（Debian/Ubuntu：fonts-droid-fallback；或设 KEPT_WATERMARK_FONT）")
	}
	if cfg.GotenbergURL == "" {
		fmt.Fprintln(stdout, "  - Office 文档转换：未配置（KEPT_GOTENBERG_URL）")
	} else {
		cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		req, _ := http.NewRequestWithContext(cctx, http.MethodGet, strings.TrimRight(cfg.GotenbergURL, "/")+"/health", nil)
		resp, err := http.DefaultClient.Do(req)
		cancel()
		if err == nil {
			resp.Body.Close()
		}
		if err == nil && resp.StatusCode == http.StatusOK {
			pass("Office 文档转换（%s）", cfg.GotenbergURL)
		} else {
			fail("Office 文档转换：连不上 %s", cfg.GotenbergURL)
		}
	}

	fmt.Fprintln(stdout)
	if !ok {
		fmt.Fprintln(stdout, "有缺少的项目，按上面的提示处理后再运行 kept doctor。")
		return 1
	}
	fmt.Fprintln(stdout, "一切就绪，可以运行 kept serve。")
	return 0
}
