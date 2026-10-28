package i18n

// English for what the misc group of packages says (v1.10.1); see
// docs/plans/2026-10-17-v1.10.1-服务端英文.md for the glossary.
func init() {
	// The AI agents (internal/agent): errors, run summaries, suggestion
	// reasons, the activity log. Prompts and the model's answers stay as
	// they are.
	add(En, map[string]string{
		// settings and the model

		// what an agent changed, as a list ("类型、往来方")
		"类型":  "Document type",
		"往来方": "Correspondent",
		"日期":  "Date",
		"标题":  "Title",
		"标签":  "Tag",
		"抽屉":  "Drawer",

		// the intake agent and the organizing assistant

		// the drawer steward
		"年份不正确": "Invalid year",

		// the library assistant
		"这份文档不是书籍": "This document isn’t a book",

		// lab reports

		// Ask
	})
}
