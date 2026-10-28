package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"kept/server/internal/app"

	"kept/server/internal/audit"
	"kept/server/internal/config"
	"kept/server/internal/drawer"
	"kept/server/internal/paperless"
	"kept/server/internal/store"
	"kept/server/internal/taxonomy"
	"kept/server/internal/tenant"
)

const importUsage = `用法：
  kept import paperless --workspace <空间名称或 ID> <导出目录>

导入 Paperless-ngx 的导出（document_exporter 生成的目录，含 manifest.json）：
往来方、类型、标签（含上下级）、自定义字段、文档（原件与可搜索版原样复制，不重新识别）、备注。
同名的往来方、类型、标签、字段会沿用已有的；工作区里已有的文件（按 SHA-256）跳过，可以重复执行。
备注记在这个空间的管理员名下。服务运行时也可以执行，导入的文档会在半分钟内进入全文索引。
同名的空间不止一个时，请改用空间 ID（出错时会列出）。
`

// runImport implements `kept import paperless`.
func runImport(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "paperless" {
		fmt.Fprint(stderr, importUsage)
		return 2
	}
	fl := flag.NewFlagSet("import", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	which := fl.String("workspace", "", "")
	if err := fl.Parse(args[1:]); err != nil || fl.NArg() != 1 || *which == "" {
		fmt.Fprint(stderr, importUsage)
		return 2
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
	var name string
	var owner sql.NullInt64
	var ownerName sql.NullString
	err = st.DB.QueryRowContext(ctx, `SELECT w.name, (SELECT m.user_id FROM workspace_members m WHERE m.workspace_id = w.id AND m.role = 'owner' ORDER BY m.user_id LIMIT 1),
		(SELECT u.display_name FROM workspace_members m JOIN users u ON u.id = m.user_id WHERE m.workspace_id = w.id AND m.role = 'owner' ORDER BY m.user_id LIMIT 1)
		FROM workspaces w WHERE w.id = ? AND w.deleted_at IS NULL`, ws).Scan(&name, &owner, &ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		fmt.Fprintf(stderr, "没有 ID 为 %d 的空间\n", ws)
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	ctx = audit.WithActor(ctx, audit.Actor{UserID: owner.Int64, Name: ownerName.String + "（导入）", Via: audit.ViaSystem})
	fileStore, err := app.OpenFileStore(ctx, cfg, st.DB)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	rep, err := paperless.Import(ctx, paperless.Deps{
		DB: st.DB, Taxonomy: taxonomy.NewService(st.DB, taxonomy.Options{}), Files: fileStore,
		Placer: drawer.NewService(st.DB, drawer.Options{}),
	}, tenant.New(ws, tenant.RoleOwner), owner.Int64, fl.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "导入失败：%v\n（已经导入的内容保留，修正后可以重新执行，已有的会跳过）\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "已导入到空间“%s”：文档 %d 份（跳过已有的 %d 份），新建往来方 %d 个、类型 %d 个、标签 %d 个、字段 %d 个，备注 %d 条\n",
		name, rep.Documents, rep.Skipped, rep.Correspondents, rep.Types, rep.Tags, rep.Fields, rep.Notes)
	if len(rep.Missing) > 0 {
		fmt.Fprintf(stdout, "有 %d 份文档在导出目录里找不到文件，没有导入：\n", len(rep.Missing))
		for _, m := range first(rep.Missing, 20) {
			fmt.Fprintf(stdout, "  %s\n", m)
		}
	}
	for _, w := range first(rep.Warnings, 50) {
		fmt.Fprintf(stdout, "提示：%s\n", w)
	}
	return 0
}

// findWorkspace resolves a workspace by ID or by its (unique) name.
func findWorkspace(ctx context.Context, db *sql.DB, which string) (int64, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, name FROM workspaces WHERE deleted_at IS NULL AND (CAST(id AS TEXT) = ? OR name = ?) ORDER BY id`, which, which)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []int64
	var names []string
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			return 0, err
		}
		ids, names = append(ids, id), append(names, fmt.Sprintf("%d（%s）", id, n))
	}
	switch len(ids) {
	case 0:
		return 0, fmt.Errorf("没有找到空间“%s”", which)
	case 1:
		return ids[0], nil
	}
	return 0, fmt.Errorf("有多个空间叫“%s”，请改用 ID：%v", which, names)
}
