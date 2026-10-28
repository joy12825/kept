package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"kept/server/internal/app"
	"kept/server/internal/config"
	"kept/server/internal/privacy"
	"kept/server/internal/store"
)

const privateUsage = `用法：
  kept private restore --workspace <空间名称或 ID> <家人名称>

用这位家人的恢复码打开他的私密文档（v1.8.1）：
  · 从远程备份恢复后，数据库里被加密的标题、识别文字、备注等恢复成原文；
  · 本机缺少的文件，从备份里带回来的加密文件（<文件>.kept）或云端存储上的加密副本解密放回。
恢复码从环境变量 KEPT_RECOVERY_CODE 读取；没有设置时从终端读取一行。
可以重复执行：已经恢复的不会再动。完成后启动服务，会在后台重建全文索引和语义索引。
`

// runPrivate implements `kept private`.
func runPrivate(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "restore" {
		fmt.Fprint(stderr, privateUsage)
		return 2
	}
	fl := flag.NewFlagSet("private restore", flag.ContinueOnError)
	fl.SetOutput(io.Discard)
	which := fl.String("workspace", "", "")
	if err := fl.Parse(args[1:]); err != nil || fl.NArg() != 1 || *which == "" {
		fmt.Fprint(stderr, privateUsage)
		return 2
	}
	code, err := readRecoveryCode(stderr)
	if err != nil {
		fmt.Fprintf(stderr, "\n%v\n", err)
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
	var person int64
	if err := st.DB.QueryRowContext(ctx, `SELECT id FROM persons WHERE workspace_id = ? AND name = ?`, ws, fl.Arg(0)).Scan(&person); err != nil {
		fmt.Fprintf(stderr, "空间里没有叫“%s”的家人\n", fl.Arg(0))
		return 1
	}
	fileStore, err := app.OpenFileStore(ctx, cfg, st.DB)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	rep, err := privacy.RestorePerson(ctx, st.DB, fileStore, person, code)
	if err != nil {
		fmt.Fprintf(stderr, "恢复失败：%v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s 有 %d 份私密文档：恢复了 %d 处文字、%d 个文件。\n", fl.Arg(0), rep.Documents, rep.Values, rep.Files)
	for _, m := range rep.Missing {
		fmt.Fprintf(stdout, "  找不到：%s（本机没有，也没有加密副本）\n", m)
	}
	return 0
}

// readRecoveryCode reads a recovery code from KEPT_RECOVERY_CODE, or else
// a line of the terminal.
func readRecoveryCode(stderr io.Writer) (string, error) {
	if code := os.Getenv("KEPT_RECOVERY_CODE"); code != "" {
		return code, nil
	}
	fmt.Fprint(stderr, "恢复码：")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("没有读到恢复码")
	}
	return line, nil
}
