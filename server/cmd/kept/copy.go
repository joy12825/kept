package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"kept/server/internal/config"
	"kept/server/internal/edition"
	"kept/server/internal/store"
)

const copyUsage = `用法：
  kept copy-to-postgres [<地址>]   把数据目录里的 SQLite 数据库复制到一个空的 Postgres 数据库

地址形如 postgres://kept:密码@db:5432/kept；不写时用 KEPT_DATABASE_URL。
先停止服务。复制在一个事务里完成，失败时目标数据库不留下任何数据；
文件和全文索引留在数据目录里，不用重建。完成后设置 KEPT_DATABASE_URL 再启动，
app.db 不会被修改，可以留作备份。
`

// runCopy implements `kept copy-to-postgres`.
func runCopy(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	target := cfg.DatabaseURL
	switch {
	case len(args) == 1 && store.IsURL(args[0]):
		target = args[0]
	case len(args) == 0 && target != "":
	default:
		fmt.Fprint(stderr, copyUsage)
		return 2
	}
	if _, err := os.Stat(cfg.DBPath()); errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(stderr, "数据目录里没有 SQLite 数据库（%s）\n", cfg.DBPath())
		return 1
	}
	start := time.Now()
	src, err := store.OpenSQLite(ctx, cfg.DBPath())
	if err != nil {
		fmt.Fprintf(stderr, "打开 SQLite 数据库：%v\n", err)
		return 1
	}
	defer src.Close()
	// PostgreSQL is a Business feature (v1.12)
	ed := edition.NewService(src.DB, edition.Options{Override: editionOverride(), OpenSource: edition.OpenSourceBuild})
	if err := ed.Init(ctx); err != nil {
		fmt.Fprintf(stderr, "读取授权失败：%v\n", err)
		return 1
	}
	if !ed.Allows(edition.Team) {
		fmt.Fprintln(stderr, "改用 Postgres 是企业版的功能。请先在“设置 → 实例管理 → 版本与授权”导入企业版许可证，或用 kept license install。")
		return 1
	}
	dst, err := store.Open(ctx, target)
	if err != nil {
		fmt.Fprintf(stderr, "打开 Postgres 数据库：%v\n", err)
		return 1
	}
	defer dst.Close()
	tables, err := store.CopyToPostgres(ctx, src.DB, dst.DB, func(table string, rows int) {
		if rows > 0 {
			fmt.Fprintf(stdout, "  %-28s %d 行\n", table, rows)
		}
	})
	if err != nil {
		fmt.Fprintf(stderr, "复制失败，目标数据库没有改动：%v\n", err)
		return 1
	}
	total := 0
	for _, t := range tables {
		total += t.Rows
	}
	fmt.Fprintf(stdout, "已复制 %d 张表、%d 行，行数核对一致，用时 %s。\n", len(tables), total, time.Since(start).Round(time.Millisecond))
	fmt.Fprintf(stdout, "下一步：设置 KEPT_DATABASE_URL=%s 后启动服务（数据目录不变）。%s 没有改动，可以留作备份。\n",
		config.Config{DatabaseURL: target}.DatabaseLabel(), cfg.DBPath())
	return 0
}
