package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"kept/server/internal/config"
	"kept/server/internal/search"
	"kept/server/internal/store"
)

const indexUsage = `用法：
  kept index rebuild   删除全文索引并按数据库重新建立（服务需要先停止）

全文索引可以随时重建，不需要备份。服务启动时发现索引的格式变了也会自动在后台重建。
`

// runIndex implements `kept index rebuild`.
func runIndex(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "rebuild" {
		fmt.Fprint(stderr, indexUsage)
		return 2
	}
	st, err := store.Open(ctx, cfg.Database())
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	defer st.Close()
	// Open first: it fails rather than deleting an index a running server holds.
	ix, _, err := search.Open(cfg.IndexDir())
	if errors.Is(err, search.ErrInUse) {
		fmt.Fprintln(stderr, "全文索引正被运行中的服务使用，请先停止 kept serve 再重建（服务运行时会自动保持索引最新）。")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "打开全文索引失败：%v\n", err)
		return 1
	}
	ix.Close()
	if err := os.RemoveAll(cfg.IndexDir()); err != nil {
		fmt.Fprintf(stderr, "删除旧索引失败：%v\n", err)
		return 1
	}
	ix, _, err = search.Open(cfg.IndexDir())
	if err != nil {
		fmt.Fprintf(stderr, "创建全文索引失败：%v\n", err)
		return 1
	}
	defer ix.Close()
	x := search.NewIndexer(st.DB, ix, nil, nil)
	if err := x.MarkAll(ctx); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	start, total := time.Now(), 0
	for {
		n, err := x.Pass(ctx)
		if err != nil {
			fmt.Fprintf(stderr, "建立索引时出错：%v\n", err)
			return 1
		}
		total += n
		if n == 0 {
			break
		}
		fmt.Fprintf(stdout, "\r已处理 %d 份", total)
	}
	fmt.Fprintf(stdout, "\r全文索引已重建：%d 份文档，用时 %s\n", total, time.Since(start).Round(time.Millisecond))
	return 0
}
