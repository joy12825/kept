package i18n

// English for encrypted storages (v1.11.0).
func init() {
	add(En, map[string]string{
		"这个存储的加密钥匙解不开（主密钥变了），请在“设置 → 存储”里输入恢复码": "This storage’s encryption key can’t be opened (the master key changed). Enter the recovery code under Settings → Storage.",
		"存储“%s”的钥匙解不开，请先用原来的恢复码解开":              "The key of the storage “%s” can’t be opened. Unlock it with the current recovery code first.",
		"%s 生成了存储加密的恢复码":                        "%s made the recovery code for storage encryption",
		"数据目录就在这台服务器上，不需要加密":                    "The data directory is on this server and doesn’t need encryption",
		"恢复码不对": "The recovery code is wrong",
		"%s 用恢复码解开了存储“%s”的钥匙":       "%s unlocked the key of the storage “%s” with the recovery code",
		"还没有生成恢复码":                  "No recovery code has been made yet",
		"加密设置改了，稍后按新的设置继续":          "The encryption setting changed. It will continue with the new setting later.",
		"%d 个文件没能转换，稍后会再试。例如：%w":    "%d files couldn’t be converted and will be tried again later. For example: %w",
		"%d 个文件所在的文档正在处理或刚改过，稍后会再试": "%d files belong to documents being processed or just changed, and will be tried again later",
		"转换后校验值不符":                  "The checksum doesn’t match after converting",
	})
}
