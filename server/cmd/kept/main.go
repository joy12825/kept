// Command kept is the Kept server.
//
//	kept serve     run the HTTP server and background workers
//	kept migrate   apply database migrations and exit
//	kept user      create accounts / reset passwords from the command line
//	kept demo      load the prototype's sample data for demonstrations
//	kept index     rebuild the full-text index
//	kept classifier  evaluate automatic classification
//	kept agent     evaluate the AI agents
//	kept check     check documents, files and the index agree
//	kept copy-to-postgres  copy the SQLite database into an empty Postgres one
//	kept doctor    check the tools and directories a binary install needs
//	kept license   the edition: status, install a license; keygen and issue for the issuer
//	kept version   print version information
//
// Configuration comes from KEPT_* environment variables (see internal/config).
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"kept/server/internal/app"
	"kept/server/internal/config"
	"kept/server/internal/logging"
	"kept/server/internal/version"
)

const usage = `用法：kept <命令>

命令：
  serve     启动服务（HTTP 接口和后台任务）
  migrate   执行数据库迁移后退出
  user      创建账号、重置密码（忘记管理员密码时使用），详见 kept user
  demo      写入原型的示例数据，用于演示，详见 kept demo
  index     重建全文索引，详见 kept index
  classifier  评估自动分类的效果，详见 kept classifier
  agent     评估 AI 识别员，详见 kept agent
  check     检查文档、文件和全文索引是否一致
  backup    备份数据库和文件（服务运行时也可以执行），详见 kept backup -h
  restore   把备份恢复到空的数据目录，详见 kept restore -h
  private   用恢复码打开家人的私密文档（从远程备份恢复后），详见 kept private -h
  secret-key 显示加密页面上存储密钥的主密钥，请另外妥善保存
  import    导入 Paperless-ngx 的导出，详见 kept import
  export    把一个空间导出为普通文件夹和清单（不装 kept 也能看），详见 kept export
  copy-to-postgres  把 SQLite 数据库复制到空的 Postgres 数据库，详见 kept copy-to-postgres -h
  doctor    检查二进制部署所需的工具、OCR 语言和数据目录
  license   版本与授权：查看状态、导入或移除许可证，详见 kept license
  version   显示版本信息

配置通过环境变量设置：KEPT_DATA_DIR、KEPT_DATABASE_URL、KEPT_HTTP_ADDR、KEPT_LOG_LEVEL、
KEPT_LOG_FORMAT、KEPT_WORKERS、KEPT_SHUTDOWN_TIMEOUT、KEPT_SESSION_TTL、
KEPT_SECURE_COOKIE、KEPT_AUDIT_RETENTION、KEPT_MAX_UPLOAD_SIZE、KEPT_DEFAULT_QUOTA、
KEPT_OCR_LANGUAGES、KEPT_OCR_MODE、KEPT_OCR_ROTATE、KEPT_OCR_DESKEW、KEPT_SEARCH_LIMIT、KEPT_GOTENBERG_URL、KEPT_PUBLIC_URLS、
KEPT_BACKUP_DIR、KEPT_CONSUME_DIR、KEPT_BARCODES、KEPT_BARCODE_SEPARATOR、
KEPT_BARCODE_ASN_PREFIX、KEPT_SMTP_*、KEPT_AI_*、KEPT_BOOK_*、KEPT_GOOGLE_BOOKS_KEY、
KEPT_OIDC_*。详见部署文档。
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintf(stdout, "kept %s (commit %s, built %s)\n", version.Version, version.Commit, version.BuildTime)
		return 0
	case "serve", "migrate", "user", "demo", "index", "classifier", "agent", "check", "backup", "restore", "import", "export", "copy-to-postgres", "doctor", "secret-key", "private", "license":
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "未知命令：%s\n\n%s", args[0], usage)
		return 2
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintf(stderr, "配置有误：\n%v\n", err)
		return 2
	}
	log := logging.New(stderr, cfg.LogLevel, cfg.LogFormat)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if args[0] == "doctor" {
		return runDoctor(ctx, cfg, stdout)
	}
	if args[0] == "license" {
		return runLicense(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "user" {
		return runUser(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "demo" {
		return runDemo(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "index" {
		return runIndex(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "check" {
		return runCheck(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "export" {
		return runExport(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "import" {
		return runImport(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "secret-key" {
		return runSecretKey(cfg, stdout, stderr)
	}
	if args[0] == "backup" {
		return runBackup(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "copy-to-postgres" {
		return runCopy(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "restore" {
		return runRestore(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "private" {
		return runPrivate(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "classifier" {
		return runClassifier(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "agent" {
		return runAgent(ctx, args[1:], cfg, stdout, stderr)
	}
	if args[0] == "migrate" {
		err = app.Migrate(ctx, cfg, log)
	} else {
		err = app.Serve(ctx, cfg, log, nil)
	}
	if err != nil {
		log.Error("exiting with error", "err", err)
		return 1
	}
	return 0
}
