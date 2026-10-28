package i18n

// English for the editions and licenses (v1.12); the paid edition for a
// family is “Plus”, the one for a team “Business”.
func init() {
	add(En, map[string]string{
		"收好":  "Kept",
		"免费版": "Free",
		"收费版": "Plus",
		"企业版": "Business",
		"这是收费版的功能，请管理员在“设置 → 实例管理 → 版本与授权”导入许可证": "This is a Plus feature. An administrator can add a license in Settings → Instance → Edition & license.",
		"这是企业版的功能，请管理员在“设置 → 实例管理 → 版本与授权”导入许可证": "This is a Business feature. An administrator can add a license in Settings → Instance → Edition & license.",
		"成员已经到了当前版本的上限，已有的成员不受影响；要再加人，请管理员升级版本":  "This edition’s member limit is reached. Existing members aren’t affected; to add more, an administrator can upgrade.",
		"共享空间已经到了当前版本的上限，已有的空间不受影响；要再建，请管理员升级版本": "This edition’s shared-space limit is reached. Existing spaces aren’t affected; to add more, an administrator can upgrade.",
		"这不是收好的许可证，请完整复制许可证文件的内容":                "This isn’t a Kept license. Copy the license file’s whole content.",
		"许可证校验没有通过：内容被改过，或不是收好签发的":               "The license didn’t verify: it was changed, or wasn’t issued by Kept.",
		"这份许可证是签给另一台服务器的，请用版本与授权页上的实例编号重新申请":     "This license is for another server. Request one with the instance ID shown on the Edition & license page.",
		"许可证里的版本不对":        "The license names an unknown edition.",
		"%s 导入了%s许可证（%s）":  "%s added a %s license (%s)",
		"%s 移除了许可证":        "%s removed the license",
		"这是开源的免费版，不包含收费功能": "This is the open-source Free edition; it has no paid features.",
		"这是开源的免费版，不能导入许可证；收费功能请使用官方发布的版本":                "This is the open-source Free edition; it can’t take a license. For paid features, use an official release.",
		"把外部存储（S3、WebDAV）用作文档存储是收费版的功能；服务器上的目录在免费版里也可以用": "Using external storage (S3, WebDAV) is a Plus feature; a directory on the server can be used in Free too.",
	})
}
