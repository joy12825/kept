package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"kept/server/internal/auth"
	"kept/server/internal/config"
	"kept/server/internal/edition"
	"kept/server/internal/store"
)

const userUsage = `用法：
  kept user create [--admin] --email <邮箱> [<用户名>]   创建账号（--admin 表示实例管理员；用户名留空时根据邮箱自动生成）
  kept user set-password <用户名或邮箱>                   重置密码，并结束该账号的所有会话
  kept user disable-2fa <用户名或邮箱>                    关闭两步验证（用于用户丢失手机的情况）

密码取自环境变量 KEPT_PASSWORD；未设置时从终端读取（或从标准输入读取一行）。
`

// runUser implements `kept user ...`, the recovery path when nobody can log
// in to the web UI (e.g. the only administrator forgot the password, or lost
// the device generating their two-factor codes).
func runUser(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, userUsage)
		return 2
	}
	var username, email string
	isAdmin := false
	needsPassword := true
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.BoolVar(&isAdmin, "admin", false, "实例管理员")
		fs.StringVar(&email, "email", "", "登录邮箱")
		if err := fs.Parse(args[1:]); err != nil || email == "" || fs.NArg() > 1 {
			fmt.Fprint(stderr, userUsage)
			return 2
		}
		username = fs.Arg(0)
	case "set-password":
		if len(args) != 2 {
			fmt.Fprint(stderr, userUsage)
			return 2
		}
		username = args[1]
	case "disable-2fa":
		if len(args) != 2 {
			fmt.Fprint(stderr, userUsage)
			return 2
		}
		username = args[1]
		needsPassword = false
	default:
		fmt.Fprintf(stderr, "未知子命令：%s\n\n%s", args[0], userUsage)
		return 2
	}

	var password string
	if needsPassword {
		password = os.Getenv("KEPT_PASSWORD")
		if password == "" {
			var err error
			if password, err = readPassword(os.Stdin, stderr); err != nil {
				fmt.Fprintf(stderr, "读取密码失败：%v\n", err)
				return 1
			}
		}
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
	svc := auth.NewService(st.DB, auth.Options{SessionTTL: cfg.SessionTTL})

	switch args[0] {
	case "create":
		// the edition's member limit holds here too (v1.12)
		ed := edition.NewService(st.DB, edition.Options{Override: editionOverride(), OpenSource: edition.OpenSourceBuild})
		if err := ed.Init(ctx); err == nil {
			err = ed.CanAddMember(ctx)
		}
		if err != nil {
			fmt.Fprintf(stderr, "创建失败：%v\n", err)
			return 1
		}
		u, err := svc.CreateUser(ctx, auth.NewUser{Username: username, Password: password, Email: email, IsAdmin: isAdmin})
		if err != nil {
			fmt.Fprintf(stderr, "创建失败：%v\n", err)
			return 1
		}
		role := "普通账号"
		if u.IsAdmin {
			role = "管理员"
		}
		fmt.Fprintf(stdout, "已创建%s %s\n", role, u.Username)
		return 0
	case "disable-2fa":
		u, err := svc.FindUser(ctx, username)
		if err == nil {
			err = svc.AdminDisableTwoFactor(ctx, u.ID)
		}
		if err != nil {
			fmt.Fprintf(stderr, "关闭失败：%v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "已关闭 %s 的两步验证\n", u.Username)
		return 0
	default: // set-password
		u, err := svc.FindUser(ctx, username)
		if err == nil {
			err = svc.SetPassword(ctx, u.ID, password)
		}
		if err != nil {
			fmt.Fprintf(stderr, "重置失败：%v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "已重置 %s 的密码，该账号的所有会话已结束\n", u.Username)
		return 0
	}
}

// readPassword prompts twice without echo on a terminal; otherwise it reads
// one line (for scripts: `echo pw | kept user create admin`).
func readPassword(r io.Reader, prompt io.Writer) (string, error) {
	if f, ok := r.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(prompt, "密码：")
		first, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		fmt.Fprint(prompt, "再输入一次：")
		second, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(prompt)
		if err != nil {
			return "", err
		}
		if string(first) != string(second) {
			return "", errors.New("两次输入的密码不一致")
		}
		return string(first), nil
	}
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
