package i18n

// English for OFD signatures (v1.11.3).
func init() {
	add(En, map[string]string{
		"签名以后，被签的文件有改动":           "A signed file was changed after signing",
		"被签的文件不见了":                "A signed file is missing",
		"签名的原文（Signature.xml）有改动": "The signed text (Signature.xml) was changed",
		"签名与证书对不上，签名值可能被改过":       "The signature doesn’t match the certificate; it may have been changed",
		"找不到签名值":                  "The signature value is missing",
		"签名值读不出来":                 "The signature value can’t be read",
		"签名算法不是 SM2，暂时不能验证":       "The signature algorithm isn’t SM2, so it can’t be verified yet",
		"摘要算法不认识，暂时不能验证":          "The digest algorithm isn’t known, so it can’t be verified yet",
		"这是不带印章的数字签名，暂时不能验证":      "This is a digital signature without a seal, which can’t be verified yet",
		"签章人的证书有改动":               "The signer’s certificate was changed",
		"签章人的证书不是印章登记的证书":         "The signer’s certificate isn’t one registered with the seal",
		"只有 OFD 文件有电子签章":          "Only OFD files have electronic seals",
		"这份 OFD 读不出来，可能已损坏":       "This OFD can’t be read and may be damaged",
		"这个签章没有能显示的印章图片":          "This signature has no seal picture to show",
	})
}
