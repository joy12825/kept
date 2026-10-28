package workspace

import "kept/server/internal/apperr"

var (
	// ErrNotFound covers a missing workspace, a deleted one, and one the
	// caller is not a member of: the three must be indistinguishable.
	ErrNotFound        = apperr.New(apperr.KindNotFound, "找不到这个空间")
	ErrNotOwner        = apperr.New(apperr.KindForbidden, "需要空间管理员权限")
	ErrPersonalDelete  = apperr.New(apperr.KindConflict, "个人空间不能删除")
	ErrNameMismatch    = apperr.New(apperr.KindInvalid, "输入的名称与空间名称不一致")
	ErrPersonalMembers = apperr.New(apperr.KindConflict, "个人空间不能添加成员")
	ErrPersonalLeave   = apperr.New(apperr.KindConflict, "不能退出个人空间")
	// ErrUserNotFound covers an unknown login and a disabled account alike.
	ErrUserNotFound   = apperr.New(apperr.KindNotFound, "找不到这个账号。账号需要先由管理员创建")
	ErrAlreadyMember  = apperr.New(apperr.KindConflict, "对方已经是这个空间的成员")
	ErrMemberNotFound = apperr.New(apperr.KindNotFound, "对方不是这个空间的成员")
	ErrLastOwner      = apperr.New(apperr.KindConflict, "至少需要保留一位管理员")
	errRole           = apperr.New(apperr.KindInvalid, "角色只能是管理员、成员或只看")
	errName           = apperr.New(apperr.KindInvalid, "空间名称需要 1 到 32 个字符")
	errKind           = apperr.New(apperr.KindInvalid, "只能新建家庭空间或团队空间")
	errColor          = apperr.New(apperr.KindInvalid, "不支持这个颜色")
)
