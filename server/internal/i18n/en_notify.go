package i18n

// English for the emails the server writes (v1.10.2): reminders, quota
// warnings, the test email, the line under documents sent by email, and
// the notices about emergency access and private documents. Each line of a
// mail is one entry.
func init() {
	add(En, map[string]string{
		// subjects
		"【收好】%s":            "[Kept] %s",
		"%s：测试邮件":           "%s: test email",
		"%s提醒：%d 条新提醒（%s）":  "%s reminders: %d new (%s)",
		"%s：空间“%s”快满了":      "%s: the space “%s” is nearly full",
		"%s：空间“%s”已满，不能再上传": "%s: the space “%s” is full and can’t take new uploads",
		"有人申请紧急查看私密文档":      "Someone asked for emergency access to private documents",
		"紧急查看的申请被拒绝了":       "Your emergency access request was denied",
		"紧急查看已生效":           "Emergency access is now active",

		// the test email and documents sent by email
		"这是一封测试邮件。收到它，说明%s的发信设置可以正常使用。": "This is a test email. If you got it, %s can send email.",
		"—— 由 %s 通过%s发送，附件 %d 份":        "— Sent by %s with %s. Attachments: %d",

		// reminders
		"“%s”有 %d 条新提醒：":  "New reminders in “%s”: %d",
		"抽屉：“%s”":         "Drawer: “%s”",
		"健康：":             "Health:",
		"仅供记录参考，不构成医疗建议。": "For your records only, not medical advice.",
		"不想再收到这类邮件，可以在%s的“设置 → 通知”中关闭邮件提醒。": "To stop these emails, turn off email reminders in %s under Settings → Notifications.",

		// quota
		"空间“%s”已用 %s，达到了 %s 的配额，新的文件暂时传不上来（邮件、监听目录收到的也会失败）。": "The space “%s” uses %s and has reached its %s quota. New files can’t be uploaded for now (files from email and the consume folder will fail too).",
		"空间“%s”已用 %s，配额是 %s，用了 %d%%。":                        "The space “%s” uses %s of its %s quota (%d%%).",
		"可以清空回收站、删除不需要的文档或旧版本腾出空间，或请实例管理员提高配额。":              "To make room, empty the Trash, delete documents or old versions you don’t need, or ask the instance admin for a larger quota.",
		"查看用量：%s": "See usage: %s",

		// when the mail server says no (the test email shows it)
		"连不上邮件服务器 %s":         "Can’t reach the mail server %s",
		"邮件服务器没有正常应答":         "The mail server didn’t answer properly",
		"与邮件服务器建立加密连接失败":      "Couldn’t make an encrypted connection to the mail server",
		"登录邮件服务器失败，请检查用户名和密码": "Couldn’t sign in to the mail server. Check the username and password.",
		"邮件服务器拒绝了发件人 %s":      "The mail server refused the sender %s",
		"邮件服务器拒绝了收件人 %s":      "The mail server refused the recipient %s",
		"邮件服务器拒绝接收邮件":         "The mail server refused to take the email",
		"邮件服务器拒绝了这封邮件":        "The mail server refused this email",

		// emergency access to a space's drawers

		// emergency access to a person's private documents
		"有人申请紧急查看家人的私密文档。如果不同意，请在 %s 之前登录，在“设置 → 我的私密”里拒绝；到时没有拒绝，对方就能只读查看。": "Someone asked for emergency access to a family member’s private documents. If you don’t agree, sign in before %s and deny it under Settings → My privacy. If it isn’t denied by then, they can view the documents read-only.",
		"对方的说明：%s": "Their reason: %s",
		"你申请紧急查看家人私密文档，对方拒绝了。":     "Your request for emergency access to a family member’s private documents was denied.",
		"等待期已过，你现在可以只读查看家人的私密文档了。": "The waiting period is over. You can now view the family member’s private documents, read-only.",
	})
}
