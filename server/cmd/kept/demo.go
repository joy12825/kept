package main

import (
	"context"
	"fmt"
	"io"
	"kept/server/internal/app"
	"os"

	"kept/server/internal/auth"
	"kept/server/internal/classify"
	"kept/server/internal/config"
	"kept/server/internal/demo"
	"kept/server/internal/document"
	"kept/server/internal/drawer"
	"kept/server/internal/events"
	"kept/server/internal/ingest"
	"kept/server/internal/jobs"
	"kept/server/internal/parser"
	"kept/server/internal/store"
	"kept/server/internal/taxonomy"
	"kept/server/internal/workspace"
)

const demoUsage = `用法：
  kept demo load <用户名或邮箱>   为该账号建立演示空间“王家”“某某工作室”，写入原型的示例数据

账号需要先存在（kept user create 或“设置 → 账号管理”）。可以重复执行，已有的数据不会重复写入。
`

// runDemo implements `kept demo ...`: sample data for demonstrations.
func runDemo(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	if len(args) != 2 || args[0] != "load" {
		fmt.Fprint(stderr, demoUsage)
		return 2
	}
	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		fmt.Fprintf(stderr, "创建数据目录失败：%v\n", err)
		return 1
	}
	st, err := store.Open(ctx, cfg.Database())
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	defer st.Close()
	docs := document.NewService(st.DB, document.Options{})
	fileStore, err := app.OpenFileStore(ctx, cfg, st.DB)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	drawers := drawer.NewService(st.DB, drawer.Options{})
	tax := taxonomy.NewService(st.DB, taxonomy.Options{})
	reports, err := demo.Load(ctx, demo.Deps{
		Files:      fileStore,
		DB:         st.DB,
		Classifier: classify.NewService(st.DB, classify.Options{Documents: docs}),
		Drawers:    drawers,
		Auth:       auth.NewService(st.DB, auth.Options{SessionTTL: cfg.SessionTTL}),
		Workspaces: workspace.NewService(st.DB, workspace.Options{}),
		Taxonomy:   tax,
		EE:         demoEE(st, fileStore, docs, tax, drawers),
		Ingest: ingest.NewService(st.DB, jobs.NewQueue(st.DB), fileStore, events.NewBus(), ingest.Options{
			Parser: parser.Config{OCRLanguages: cfg.OCRLanguages, OCRMode: cfg.OCRMode, Rotate: cfg.OCRRotate, Deskew: cfg.OCRDeskew, RotateThreshold: cfg.OCRRotateThreshold}, MaxUploadSize: cfg.MaxUploadSize,
		}),
	}, args[1])
	for _, r := range reports {
		verb := "已有"
		if r.Created {
			verb = "新建"
		}
		fmt.Fprintf(stdout, "%s空间“%s”：新增往来方 %d 个、类型 %d 个、标签 %d 个、文档 %d 份、常用筛选 %d 个、抽屉 %d 个、已连接的手机 %d 台、工作流 %d 个、分享链接 %d 个、新版本 %d 份、近似重复 %d 对、AI 记录 %d 条、书籍 %d 本；自动分类学会了 %d 个对象\n", verb, r.Name, r.Correspondents, r.Types, r.Tags, r.Documents, r.Views, r.Drawers, r.Devices, r.Workflows, r.Shares, r.Versions, r.Duplicates, r.AgentRuns, r.Books, r.Learned)
		if r.DocumentsSkipped != "" {
			fmt.Fprintf(stdout, "  %s\n", r.DocumentsSkipped)
		}
		if r.LabReports > 0 {
			fmt.Fprintf(stdout, "  化验单 %d 份（“爸爸”的血常规与生化，两份已核对，一份待核对）\n", r.LabReports)
		}
		if r.Findings > 0 {
			fmt.Fprintf(stdout, "  检查报告、病历和票据 %d 份（“爸爸”的甲状腺彩超两份、门诊病历一份，就诊里附了收费票据）\n", r.Findings)
		}
		if r.Scenes > 0 {
			fmt.Fprintf(stdout, "  场景：启用了孩子成长、水电燃气、车辆保养，建了“血压”；记录和用药共 %d 条（“乐乐”的身高体重、每月读数、里程、“爸爸”的血压、处方和长期用药）；电费通知单、保养结算单各一张，读数和里程等你核对\n", r.Scenes)
		}
		if r.Previewed > 0 {
			fmt.Fprintf(stdout, "  预览器：Markdown 笔记、录音、照片、OFD 电子发票、Excel 账本、压缩包、Outlook 邮件、两版 Word 合同、幻灯片、应急手册（.docx、.doc、.odt 各一份）、带电子签章的发票两张（一张签章后被改）共 %d 份\n", r.Previewed)
		}
		if r.RecoveryCode != "" {
			fmt.Fprintf(stdout, "  “爸爸”的私密文档有了钥匙（演示），恢复码：%s。只显示这一次，从远程备份恢复时要用到。\n", r.RecoveryCode)
		}
		if r.StorageRecoveryCode != "" {
			fmt.Fprintf(stdout, "  存储加密的恢复码（演示）：%s。只显示这一次；在“设置 → 存储”给云端存储开启加密后，主密钥丢失时用它解开。\n", r.StorageRecoveryCode)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "写入演示数据失败：%v\n", err)
		return 1
	}
	return 0
}
