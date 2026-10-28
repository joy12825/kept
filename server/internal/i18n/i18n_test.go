package i18n

import (
	"context"
	"testing"
)

func TestFromHeader(t *testing.T) {
	for h, want := range map[string]Lang{
		"":                         Zh,
		"zh-CN":                    Zh,
		"zh-Hans-US;q=1, en;q=0.9": Zh,
		"en":                       En,
		"en-US,en;q=0.9,zh;q=0.8":  En,
		"fr-FR":                    En,
		"*":                        Zh,
	} {
		if got := FromHeader(h); got != want {
			t.Errorf("FromHeader(%q) = %s, want %s", h, got, want)
		}
	}
}

func TestMessage(t *testing.T) {
	add("xx", map[string]string{
		"需要管理员权限":        "Admin access required",
		"文件超过 %d MB 的上限": "File exceeds the %d MB limit",
		"复制 %s：%v":       "Copy %s: %v",
		"文件已存在":          "The file already exists",
		"“%s”和“%s”重名":    "“%[2]s” has the same name as “%[1]s”",
		"%d%% 已完成":       "%d%% done",
		"标签“%s”":         "Tag “%s”",
		"标签“%s”已存在":      "Tag “%s” already exists",
		"合同":             "Contracts",
		"上传了《%s》":        "Uploaded “%s”",
		"移动失败：%w":        "Move failed: %w",
	})
	l := Lang("xx")
	for in, want := range map[string]string{
		"需要管理员权限":         "Admin access required",
		"文件超过 200 MB 的上限": "File exceeds the 200 MB limit",
		"复制 a.pdf：文件已存在":  "Copy a.pdf: The file already exists", // a value that is a message
		"“甲”和“乙”重名":       "“乙” has the same name as “甲”",
		"50% 已完成":         "50% done",
		"标签“税务”已存在":       "Tag “税务” already exists", // the more specific format wins
		"没有翻译的":           "没有翻译的",
		"上传了《合同》":         "Uploaded “合同”", // a quoted name is never translated
		"移动失败：文件已存在":      "Move failed: The file already exists",
	} {
		if got := Message(l, in); got != want {
			t.Errorf("Message(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Message(Zh, "需要管理员权限"); got != "需要管理员权限" {
		t.Errorf("Chinese changed: %q", got)
	}
	ctx := With(context.Background(), l)
	if got := T(ctx, "需要管理员权限"); got != "Admin access required" || From(context.Background()) != Zh {
		t.Errorf("T = %q", got)
	}
}
