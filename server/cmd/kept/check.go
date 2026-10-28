package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"kept/server/internal/app"

	"kept/server/internal/config"
	"kept/server/internal/integrity"
	"kept/server/internal/search"
	"kept/server/internal/store"
)

const checkUsage = `用法：
  kept check   检查每个空间的文档、文件和全文索引是否一致

报告“有记录没有文件”和“有文件没有记录”（不删除任何文件）；服务没有运行时，
还会检查全文索引，把缺少的文档交给下次启动时的索引器补建。服务运行时每周自动检查一次。
`

// runCheck implements `kept check`.
func runCheck(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprint(stderr, checkUsage)
		return 2
	}
	st, err := store.Open(ctx, cfg.Database())
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	defer st.Close()
	ix, _, err := search.Open(cfg.IndexDir())
	switch {
	case errors.Is(err, search.ErrInUse):
		fmt.Fprintln(stdout, "全文索引正被运行中的服务使用，这次不检查索引（服务会自动保持索引最新）。")
		ix = nil
	case err != nil:
		fmt.Fprintf(stderr, "打开全文索引失败：%v\n", err)
		return 1
	default:
		defer ix.Close()
	}
	rows, err := st.DB.QueryContext(ctx, `SELECT id, name FROM workspaces WHERE deleted_at IS NULL ORDER BY id`)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	type ws struct {
		id   int64
		name string
	}
	var list []ws
	for rows.Next() {
		var w ws
		if err := rows.Scan(&w.id, &w.name); err != nil {
			rows.Close()
			fmt.Fprintf(stderr, "%v\n", err)
			return 1
		}
		list = append(list, w)
	}
	rows.Close()
	fileStore, err := app.OpenFileStore(ctx, cfg, st.DB)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	checker := integrity.NewChecker(st.DB, fileStore, cfg.DataDir, ix)
	problems := 0
	for _, w := range list {
		rep, err := checker.Check(ctx, w.id)
		if err != nil {
			fmt.Fprintf(stderr, "检查空间“%s”失败：%v\n", w.name, err)
			return 1
		}
		fmt.Fprintf(stdout, "空间“%s”：%d 份文档", w.name, rep.Documents)
		if rep.OK() {
			fmt.Fprint(stdout, "，文件完整")
		}
		if rep.IndexChecked {
			fmt.Fprintf(stdout, "；索引补建 %d 份、移除 %d 条", rep.Reindexed, rep.RemovedFromIndex)
		}
		fmt.Fprintln(stdout)
		for _, m := range rep.MissingFiles {
			fmt.Fprintf(stdout, "  缺少文件：#%d《%s》的%s\n", m.DocumentID, m.Title, map[string]string{"original": "原件", "archive": "可搜索版", "thumbnail": "缩略图"}[m.File])
		}
		if rep.MissingCount > len(rep.MissingFiles) {
			fmt.Fprintf(stdout, "  ……共缺少 %d 个文件\n", rep.MissingCount)
		}
		for _, f := range rep.OrphanFiles {
			fmt.Fprintf(stdout, "  没有记录的文件：%s\n", f)
		}
		if rep.OrphanCount > len(rep.OrphanFiles) {
			fmt.Fprintf(stdout, "  ……共 %d 个没有记录的文件\n", rep.OrphanCount)
		}
		problems += rep.MissingCount + rep.OrphanCount
	}
	if problems > 0 {
		return 1
	}
	return 0
}
