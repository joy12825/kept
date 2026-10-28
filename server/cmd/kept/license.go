package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"kept/server/internal/config"
	"kept/server/internal/edition"
	"kept/server/internal/store"
)

const licenseUsage = `用法：
  kept license status                 显示当前版本、试用或到期日、成员和共享空间用量、实例编号
  kept license install <文件>          导入许可证（文件写 - 表示从标准输入读取）；服务运行中时重启后生效
  kept license remove                 移除许可证，回到免费版（试用期内仍是试用）
`

func runLicense(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, licenseUsage+issuerUsage)
		return 2
	}
	switch args[0] {
	case "keygen":
		return licenseKeygen(args[1:], stdout, stderr)
	case "issue":
		return licenseIssue(args[1:], stdout, stderr)
	case "status", "install", "remove":
	default:
		fmt.Fprintf(stderr, "未知子命令：%s\n\n%s", args[0], licenseUsage)
		return 2
	}
	if args[0] == "install" && len(args) != 2 || args[0] != "install" && len(args) != 1 {
		fmt.Fprint(stderr, licenseUsage)
		return 2
	}
	st, err := store.Open(ctx, cfg.Database())
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	defer st.Close()
	svc := edition.NewService(st.DB, edition.Options{Override: editionOverride(), OpenSource: edition.OpenSourceBuild})
	if err := svc.Init(ctx); err != nil {
		fmt.Fprintf(stderr, "读取授权失败：%v\n", err)
		return 1
	}
	switch args[0] {
	case "install":
		var text []byte
		if args[1] == "-" {
			text, err = io.ReadAll(os.Stdin)
		} else {
			text, err = os.ReadFile(args[1])
		}
		if err != nil {
			fmt.Fprintf(stderr, "读取许可证失败：%v\n", err)
			return 1
		}
		l, err := svc.Install(ctx, string(text))
		if err != nil {
			fmt.Fprintf(stderr, "导入失败：%v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "已导入%s许可证（%s）。服务运行中时请重启。\n", l.Tier().Name(), l.Holder)
		return 0
	case "remove":
		if err := svc.Remove(ctx); err != nil {
			fmt.Fprintf(stderr, "移除失败：%v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "已移除许可证。服务运行中时请重启。")
		return 0
	}
	s, err := svc.Status(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "读取状态失败：%v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "版本：%s\n", s.Edition.Name())
	switch {
	case s.Overridden:
		fmt.Fprintln(stdout, "（开发构建：由 KEPT_EDITION 指定）")
	case s.OpenSource:
		fmt.Fprintln(stdout, "（开源构建：不含收费模块，不试用，不能导入许可证）")
	case s.Trial:
		fmt.Fprintf(stdout, "试用：还剩 %d 天（到 %s）\n", s.DaysLeft, s.TrialEndsAt.Format("2006-01-02"))
	case s.License != nil && s.Expired:
		fmt.Fprintf(stdout, "许可证：%s，已于 %s 到期\n", s.License.Holder, s.License.Expires)
	case s.License != nil && s.License.Expires == "":
		fmt.Fprintf(stdout, "许可证：%s，长期有效\n", s.License.Holder)
	case s.License != nil:
		fmt.Fprintf(stdout, "许可证：%s，有效期到 %s（还剩 %d 天）\n", s.License.Holder, s.License.Expires, s.DaysLeft)
	}
	fmt.Fprintf(stdout, "成员：%s\n共享空间：%s\n实例编号：%s\n", usageText(s.Members), usageText(s.SharedWorkspaces), s.Instance)
	return 0
}

// testEdition, set by tests, stands for the edition a license would give.
var testEdition *edition.Edition

// editionOverride is the edition fixed by a development build or a test.
func editionOverride() *edition.Edition {
	if testEdition != nil {
		return testEdition
	}
	return edition.DevOverride()
}

func usageText(u edition.Usage) string {
	if u.Limit == 0 {
		return fmt.Sprintf("%d（不限）", u.Used)
	}
	return fmt.Sprintf("%d / %d", u.Used, u.Limit)
}
