package main

import (
	"os"
	"path/filepath"

	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"kept/server/internal/secret"
	"strings"

	"kept/server/internal/app"
	"kept/server/internal/backup"
	"kept/server/internal/config"
	"kept/server/internal/storage"
	"kept/server/internal/store"
)

const backupUsage = `用法：
  kept backup [<目录>]          把数据库和文件备份到目录（服务运行时也可以执行）；不写目录时用 KEPT_BACKUP_DIR，或页面上“文件安全”里设置的目录
  kept backup --storage <名字>  备份到一个存储（S3、WebDAV 等）的 kept-backup/ 下（v1.7.6）

备份包括数据库快照（保留最近 7 份）和它引用的原件、可搜索版、缩略图、Logo，
放在其他存储里的文档也一起备份；再次备份时只复制新增或变化的文件。
全文索引不备份，恢复后会自动重建。备份目标不能是放着文档的存储。
数据库在 Postgres 上时，快照同样是一个 SQLite 文件（v1.9.10），恢复后用 kept copy-to-postgres 放回 Postgres。
`

const restoreUsage = `用法：
  kept restore [--snapshot <文件名>] [--force] <目录>           把备份恢复到 KEPT_DATA_DIR（必须是空的数据目录，服务需要先停止）
  kept restore [--snapshot <文件名>] [--force] --storage <名字>  从存储里的备份恢复（v1.7.6）；这个存储要写在部署配置（KEPT_STORAGES）里

默认使用最新的快照；--snapshot 指定 db/ 下的某一份。恢复前会校验原件，
有缺失或损坏时列出并退出；--force 跳过有问题的文件继续恢复。
恢复出来的数据库总是数据目录里的 app.db；原来用 Postgres 的，再用 kept copy-to-postgres 复制到一个空的 Postgres 数据库。
`

// runBackup implements `kept backup`.
func runBackup(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	dir, target := cfg.BackupDir, ""
	switch {
	case len(args) == 2 && args[0] == "--storage":
		target = args[1]
	case len(args) == 1 && !strings.HasPrefix(args[0], "-"):
		dir = args[0]
	case len(args) == 0:
	default:
		fmt.Fprint(stderr, backupUsage)
		return 2
	}
	st, err := store.Open(ctx, cfg.Database())
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	defer st.Close()
	// no directory given: KEPT_BACKUP_DIR, or the one chosen on the page (v1.13.9)
	if target == "" && dir == "" {
		if dir = backup.NewDir(st.DB, "", cfg.DataDir).Get(ctx); dir == "" {
			fmt.Fprint(stderr, backupUsage)
			return 2
		}
	}
	fs, err := app.OpenFileStore(ctx, cfg, st.DB)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	var res backup.Result
	if target != "" {
		res, err = backupToStorage(ctx, st.DB, target, backup.Options{DataDir: cfg.DataDir, Files: fs})
	} else {
		res, err = backup.Run(ctx, st.DB, dir, backup.Options{DataDir: cfg.DataDir, Files: fs})
	}
	if err != nil {
		fmt.Fprintf(stderr, "备份失败：%v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "已备份到 %s：快照 %s，%d 份文档、%d 个文件（这次复制 %d 个，%s），用时 %s\n",
		res.Dir, res.Snapshot, res.Documents, res.Files, res.Copied, humanBytes(res.Bytes), res.Took)
	if res.Pruned > 0 {
		fmt.Fprintf(stdout, "清理了 %d 个不再需要的旧文件\n", res.Pruned)
	}
	if res.External > 0 {
		fmt.Fprintf(stdout, "另有 %d 个文件放在读不到的存储里（没有配置，或连不上），不在这份备份里\n", res.External)
	}
	if len(res.Missing) > 0 {
		fmt.Fprintf(stdout, "有 %d 个文件在数据目录中不存在，没有备份（运行 kept check 查看）：\n", len(res.Missing))
		for _, m := range first(res.Missing, 20) {
			fmt.Fprintf(stdout, "  %s\n", m)
		}
	}
	return 0
}

// runRestore implements `kept restore`.
func runRestore(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("restore", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	snapshot := fl.String("snapshot", "", "")
	force := fl.Bool("force", false, "")
	target := fl.String("storage", "", "")
	if err := fl.Parse(args); err != nil || (*target == "") != (fl.NArg() == 1) {
		fmt.Fprint(stderr, restoreUsage)
		return 2
	}
	// The data directory is empty: only the storages of the deployment are known.
	fs, err := app.NewFileStore(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	opt := backup.RestoreOptions{DataDir: cfg.DataDir, Snapshot: *snapshot, Force: *force, Files: fs}
	var res backup.RestoreResult
	encrypted := false
	if *target != "" {
		// An encrypted storage (v1.11.0): its key is in the storage, opened
		// with the master key or else the recovery code.
		if b, err := fs.Backend(*target); err == nil {
			var box *secret.Box
			if _, serr := os.Stat(filepath.Join(cfg.DataDir, secret.FileName)); cfg.SecretKey != "" || serr == nil {
				box, _ = secret.Load(cfg.DataDir, cfg.SecretKey)
			}
			k, err := storage.KeyFromStorage(ctx, b, box, func() (string, error) {
				fmt.Fprintln(stderr, "这个存储里的备份是加密的，主密钥打不开它的钥匙。")
				return readRecoveryCode(stderr)
			})
			switch {
			case err == nil:
				storage.UseKey(fs, *target, k)
				encrypted = true
			case !errors.Is(err, os.ErrNotExist):
				fmt.Fprintf(stderr, "打不开这个存储的钥匙：%v\n", err)
				return 1
			}
		}
		res, err = backup.RestoreFrom(ctx, *target, opt)
	} else {
		res, err = backup.Restore(ctx, fl.Arg(0), opt)
	}
	var dmg *backup.ErrDamaged
	if errors.As(err, &dmg) {
		fmt.Fprintf(stderr, "%v，没有恢复任何内容：\n", err)
		for _, p := range first(dmg.Problems, 20) {
			fmt.Fprintf(stderr, "  %s\n", p)
		}
		fmt.Fprintln(stderr, "可以用 --snapshot 换一份快照，或加 --force 跳过这些文件。")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "恢复失败：%v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "已从快照 %s 恢复 %d 份文档、%d 个文件到 %s。启动服务后会在后台重建全文索引。\n", res.Snapshot, res.Documents, res.Files, cfg.DataDir)
	if encrypted {
		fmt.Fprintln(stdout, "这份备份是加密的，已经解开。启动服务后如果存储显示“需要恢复码”（主密钥和原来不同），在“设置 → 存储”里输入恢复码。")
	}
	if res.SealedKeys {
		fmt.Fprintln(stdout, "页面上添加的存储的密钥用主密钥加密，备份里没有主密钥：请把原数据目录里的 secret.key 拷到新的数据目录，或设置相同的 KEPT_SECRET_KEY；否则要在页面上重新填写这些存储的 Secret Key。")
	}
	if res.Sealed > 0 {
		fmt.Fprintf(stdout, "家人的私密文档有 %d 个文件是加密的，标题和文字也是加密的：请对每位家人运行 kept private restore，用他的恢复码打开。\n", res.Sealed)
	}
	if res.External > 0 {
		fmt.Fprintf(stdout, "另有 %d 个文件在其他存储里，这次没有放回（那些存储不在部署配置里）。它们若仍在原处，启动后照常可用；页面上添加的存储启动后才能用。\n", res.External)
	}
	if len(res.Problems) > 0 {
		fmt.Fprintf(stdout, "跳过了 %d 个缺失或损坏的文件，启动后可以用 kept check 查看。\n", len(res.Problems))
	}
	if cfg.DatabaseURL != "" {
		fmt.Fprintln(stdout, "部署配置里的数据库在 Postgres 上：恢复出来的是 app.db。要继续用 Postgres，先做完上面的步骤（kept private restore 时不设 KEPT_DATABASE_URL），再运行 kept copy-to-postgres 复制到一个空的 Postgres 数据库。")
	}
	return 0
}

func first(xs []string, n int) []string {
	if len(xs) > n {
		return append(xs[:n:n], fmt.Sprintf("……还有 %d 个", len(xs)-n))
	}
	return xs
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// runSecretKey implements `kept secret-key` (v1.7.6): prints the master key
// that seals the storage keys entered on the page, to keep somewhere safe;
// backups leave it out.
func runSecretKey(cfg config.Config, stdout, stderr io.Writer) int {
	if cfg.SecretKey != "" {
		fmt.Fprintln(stdout, cfg.SecretKey)
		fmt.Fprintln(stderr, "（来自 KEPT_SECRET_KEY）")
		return 0
	}
	b, err := os.ReadFile(filepath.Join(cfg.DataDir, secret.FileName))
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(stderr, "还没有主密钥：页面上添加带密钥的存储后才会生成")
		return 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	fmt.Fprint(stdout, string(b))
	fmt.Fprintln(stderr, "请把它保存在备份以外的安全地方；恢复到新机器时设为 KEPT_SECRET_KEY，或放回数据目录的 secret.key。")
	return 0
}
