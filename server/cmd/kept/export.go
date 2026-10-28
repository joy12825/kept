package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"kept/server/internal/app"
	"os"

	"kept/server/internal/config"
	"kept/server/internal/document"
	"kept/server/internal/export"
	"kept/server/internal/store"
	"kept/server/internal/taxonomy"
)

const exportUsage = `用法：
  kept export --workspace <空间名称或 ID> [--original] [--update] <目录>

把一个空间导出为普通文件（v1.4.1），不装 kept 也能翻看：
  <目录>/<抽屉>/<年份>/<日期> <标题>.pdf   没放入抽屉的在“未入抽屉”
  <目录>/清单.csv、清单.json              每份文档的标题、日期、分类、字段和备注
默认导出可搜索版（识别过文字的 PDF），没有时导出原件；--original 一律导出原件。
目录须为空或不存在；--update 更新以前导出的目录：只写新的和变了的文件，删掉已经不在的
（只删收好自己写的，目录里别的文件不动），清单照常重写。
服务运行时也可以执行。这不是备份：完整备份请用 kept backup。
`

// runExport implements `kept export`.
func runExport(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("export", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	which := fl.String("workspace", "", "")
	original := fl.Bool("original", false, "")
	update := fl.Bool("update", false, "")
	if err := fl.Parse(args); err != nil || fl.NArg() != 1 || *which == "" {
		fmt.Fprint(stderr, exportUsage)
		return 2
	}
	dir := fl.Arg(0)
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 && !*update {
		fmt.Fprintf(stderr, "目录 %s 不是空的，请换一个空目录（更新以前导出的目录请加 --update）\n", dir)
		return 1
	}
	sink, err := export.NewSyncSink(dir)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	st, err := store.Open(ctx, cfg.Database())
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	defer st.Close()
	ws, err := findWorkspace(ctx, st.DB, *which)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	fileStore, err := app.OpenFileStore(ctx, cfg, st.DB)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	rep, err := export.Export(ctx, export.Deps{
		DB: st.DB, Documents: document.NewService(st.DB, document.Options{}), Taxonomy: taxonomy.NewService(st.DB, taxonomy.Options{}), Files: fileStore,
	}, ws, export.Options{Original: *original}, sink)
	if err != nil {
		fmt.Fprintf(stderr, "导出失败：%v\n", err)
		return 1
	}
	if *update {
		fmt.Fprintf(stdout, "已更新 %s：写入 %d 份（%s），%d 份没有变化，删掉 %d 个已经不在的文件\n", dir, rep.Documents, humanBytes(rep.Bytes), rep.Unchanged, rep.Removed)
	} else {
		fmt.Fprintf(stdout, "已导出 %d 份文档（%s）到 %s，清单见 清单.csv\n", rep.Documents, humanBytes(rep.Bytes), dir)
	}
	for _, m := range rep.Missing {
		fmt.Fprintf(stdout, "  缺少文件：%s（已记在清单里，可运行 kept check 检查）\n", m)
	}
	return 0
}
