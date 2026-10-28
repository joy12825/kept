package i18n

// English for what the health group of packages says (v1.10.1); see
// docs/plans/2026-10-17-v1.10.1-服务端英文.md for the glossary.
//
// The health, privacy, emergency and checklist packages, and the api's
// health, privacy, persons, emergency, checklists and scenes files. The
// metrics' names, aliases and units (the built-in dictionary and the
// scenes' records) are data and stay as they are.
func init() {
	add(En, map[string]string{
		// health: measurements, lab reports and entries
		"找不到这份文档":            "Document not found",
		"日期的格式应为 2024-03-05": "Write the date as 2024-03-05",
		"这位家人不存在":            "This person doesn’t exist",
		"找不到这位家人":            "Person not found",

		// privacy: private documents, keys and emergency access of a person
		"只能选择这个空间的成员":                       "Choose a member of this space",
		"权限只能是本人、照看人、代管或只读":                 "Access must be self, caretaker, manager or read-only",
		"只有这位家人本人（没有本人时是照看人）可以管理谁能看":        "Only this person (or their caretaker, if they have no account) can manage who can see",
		"这位家人已经关联了账号，先解除再换":                 "This person is already linked to an account. Unlink it first.",
		"这个账号已经关联了空间里的另一位家人":                "This account is already linked to another person in this space",
		"本人的权限不能改，只能由本人解除关联":                "The person’s own access can’t be changed. Only they can unlink themselves.",
		"只有本人可以解除自己和这位家人的关联":                "Only the person themselves can unlink their account",
		"这位家人还有私密文档，先指定照看人或取消这些文档的私密，再解除":   "This person still has private documents. Choose a caretaker or make those documents not private first, then unlink.",
		"这位家人有私密文档但没有人负责，只有空间拥有者可以指定本人或照看人": "This person has private documents but no one in charge. Only the space’s owner can choose the person or a caretaker.",
		"你的角色只能查看":                          "Your role can only view",
		"这位家人还没有关联账号或照看人，先在家人页指定，再设为私密":     "This person has no linked account or caretaker yet. Choose one on the person’s page first, then make it private.",
		"私密文档要属于一位家人，请先选择家人":                "A private document must belong to a person. Choose a person first.",
		"要合并的家人有私密文档：只有同时负责这几位家人的人可以合并":     "The people to merge have private documents. Only someone in charge of all of them can merge them.",
		"只有这位家人本人（没有本人时是照看人）可以设置或取消私密":      "Only this person (or their caretaker, if they have no account) can make documents private or not",
		"这是家人的私密文档，你只能查看":                   "This is a person’s private document. You can only view it.",
		"这位家人还有私密文档，先取消私密或把文档换给别的家人，再删除":    "This person still has private documents. Make them not private or move them to another person first, then delete.",
		"这位家人已经有钥匙了；忘了恢复码可以重新生成":            "This person already has a key. If the recovery code is lost, you can make a new one.",
		"这位家人还没有设钥匙":                        "This person has no key yet",
		"请输入登录密码或现在的恢复码":                    "Enter your password or the current recovery code",
		"密码或恢复码不对":                          "The password or recovery code is incorrect",
		"只有这位家人本人（没有本人时是照看人）可以决定":           "Only this person (or their caretaker, if they have no account) can decide",
		"已经能看这位家人私密文档的人不需要设为紧急联系人":          "Someone who can already see this person’s private documents doesn’t need to be an emergency contact",
		"一位家人最多 5 位紧急联系人":                   "A person can have at most 5 emergency contacts",
		"你不是这位家人的紧急联系人":                     "You’re not an emergency contact of this person",

		// emergency access of a space (and of a person)
		"等待期应在 1 到 30 天之间": "The waiting period must be between 1 and 30 days",
		"说明最多 500 个字":      "The note can be at most 500 characters",
		"状态已经变了，请刷新后再试":    "The status has changed. Refresh and try again.",
		// activity summaries; the first value is who did it

		// checklists
		"找不到这份清单":                "Checklist not found",
		"清单名称需要 1 到 64 个字":       "Checklist names must be 1 to 64 characters",
		"说明最多 1000 个字":           "The description can be at most 1000 characters",
		"清单最多 50 项":              "A checklist can have at most 50 items",
		"每一项需要 1 到 64 个字的名称":     "Each item needs a name of 1 to 64 characters",
		"每一项要的份数在 1 到 24 之间":     "Each item must ask for 1 to 24 documents",
		"每一项需要找文档的条件":            "Each item needs conditions to find documents",
		"清单里没有这一项":               "This item isn’t in the checklist",
		"上传的内容格式不正确":             "The upload is not in the right format",
		"预设的标题不正确":               "The preset title isn’t valid",
		"%s 新建了清单“%s”":           "%s created checklist “%s”",
		"%s 修改了清单“%s”":           "%s edited checklist “%s”",
		"%s 删除了清单“%s”":           "%s deleted checklist “%s”",
		"%s 把《%s》放进了清单“%s”的“%s”": "%[1]s added “%[2]s” to “%[4]s” in checklist “%[3]s”",
		"%s 把《%s》移出了清单“%s”的“%s”": "%[1]s removed “%[2]s” from “%[4]s” in checklist “%[3]s”",
		// v1.11.5: readings and service reminders
	})
}
