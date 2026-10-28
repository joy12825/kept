package taxonomy

import (
	"fmt"

	"kept/server/internal/apperr"
)

var (
	// ErrNotFound covers a missing item and one in another workspace alike.
	ErrNotFound = apperr.New(apperr.KindNotFound, "找不到这一项，它可能已被删除")
	errName     = nameError(apperr.New(apperr.KindInvalid, "名称需要 1 到 64 个字符"))
	errRule     = apperr.New(apperr.KindInvalid, "不支持这种匹配方式")
	errKeywords = apperr.New(apperr.KindInvalid, "请填写要匹配的关键词")
)

// errTaken is each kind's name conflict; its details name the field, so a
// form shows it under the name whatever the language (v1.10.1).
var errTaken = map[Kind]*apperr.Error{
	Correspondent: nameError(apperr.New(apperr.KindConflict, "已经有同名的往来方")),
	DocumentType:  nameError(apperr.New(apperr.KindConflict, "已经有同名的类型")),
	Tag:           nameError(apperr.New(apperr.KindConflict, "已经有同名的标签")),
	CustomField:   nameError(apperr.New(apperr.KindConflict, "已经有同名的自定义字段")),
	Person:        nameError(apperr.New(apperr.KindConflict, "已经有同名的家人")),
	locationKind:  nameError(apperr.New(apperr.KindConflict, "已经有同名的存放位置")),
}

// nameError marks an error as being about the name field (details
// {field: "name"}).
func nameError(e *apperr.Error) *apperr.Error {
	e.Details = map[string]any{"field": "name"}
	return e
}

func errRegex(detail string) error {
	return apperr.New(apperr.KindInvalid, fmt.Sprintf("正则表达式无效：%s", detail))
}
