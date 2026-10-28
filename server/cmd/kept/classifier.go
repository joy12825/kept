package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"

	"kept/server/internal/classify"
	"kept/server/internal/config"
	"kept/server/internal/search"
	"kept/server/internal/store"
)

const classifierUsage = `用法：
  kept classifier eval [-k 5] <空间 ID>   用 k 折交叉验证评估自动分类的效果

评估只读取数据，不改动模型，服务运行时也可以执行。
`

// runClassifier implements `kept classifier eval`.
func runClassifier(ctx context.Context, args []string, cfg config.Config, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "eval" {
		fmt.Fprint(stderr, classifierUsage)
		return 2
	}
	fs := flag.NewFlagSet("classifier eval", flag.ContinueOnError)
	fs.SetOutput(stderr)
	k := fs.Int("k", 5, "折数")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 1 {
		fmt.Fprint(stderr, classifierUsage)
		return 2
	}
	ws, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil {
		fmt.Fprint(stderr, classifierUsage)
		return 2
	}
	st, err := store.Open(ctx, cfg.Database())
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	defer st.Close()
	search.LoadDictionary()
	svc := classify.NewService(st.DB, classify.Options{})
	rep, err := svc.Eval(ctx, ws, *k)
	if err != nil {
		fmt.Fprintf(stderr, "评估失败：%v\n", err)
		return 1
	}
	names, err := svc.Names(ctx, ws)
	if err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "空间 %d：%d 份已整理的文档，%d 折交叉验证\n", ws, rep.Documents, rep.Folds)
	if rep.Skipped != "" {
		fmt.Fprintf(stdout, "无法训练：%s\n", skipReason(rep.Skipped))
		return 0
	}
	fmt.Fprintf(stdout, "类型准确率 %.1f%%，往来方准确率 %.1f%%\n\n", rep.TypeAccuracy*100, rep.CorrespondentAccuracy*100)
	fmt.Fprintf(stdout, "%-6s %-24s %6s %6s %6s %8s\n", "种类", "名称", "样本", "找到", "误判", "召回率")
	kinds := map[string]string{"type": "类型", "correspondent": "往来方", "tag": "标签"}
	for _, s := range rep.Scores {
		recall := "-"
		if s.Samples > 0 {
			recall = fmt.Sprintf("%.1f%%", float64(s.Found)/float64(s.Samples)*100)
		}
		fmt.Fprintf(stdout, "%-6s %-24s %6d %6d %6d %8s\n", kinds[s.Kind], names[s.Kind][s.ID], s.Samples, s.Found, s.Wrong, recall)
	}
	return 0
}

func skipReason(code string) string {
	switch code {
	case "too_few_documents":
		return fmt.Sprintf("已整理的文档少于 %d 份", classify.MinDocs)
	case "nothing_to_learn":
		return "没有足够样本的“自动学习”对象"
	case "no_words":
		return "文档里没有可用的文字"
	}
	return code
}
