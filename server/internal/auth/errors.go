package auth

import "kept/server/internal/apperr"

// Error and Kind are the shared application error type; the aliases keep
// auth's own code and its callers reading as before.
type (
	Error = apperr.Error
	Kind  = apperr.Kind
)

const (
	KindInvalid      = apperr.KindInvalid
	KindUnauthorized = apperr.KindUnauthorized
	KindForbidden    = apperr.KindForbidden
	KindNotFound     = apperr.KindNotFound
	KindConflict     = apperr.KindConflict
	KindRateLimited  = apperr.KindRateLimited
)

func newErr(k Kind, msg string) *Error { return apperr.New(k, msg) }

var (
	ErrInvalidCredentials = newErr(KindUnauthorized, "用户名或密码不正确")
	ErrUnauthenticated    = newErr(KindUnauthorized, "请先登录")
	ErrRateLimited        = newErr(KindRateLimited, "登录失败次数过多，请稍后再试")
	ErrSetupDone          = newErr(KindConflict, "系统已经初始化，请直接登录")
	ErrUsernameTaken      = newErr(KindConflict, "这个用户名已经被使用")
	ErrEmailTaken         = newErr(KindConflict, "这个邮箱已经被使用")
	ErrUserNotFound       = newErr(KindNotFound, "找不到这个账号")
	ErrTokenNotFound      = newErr(KindNotFound, "找不到这个 API Token")
	ErrLastAdmin          = newErr(KindConflict, "至少需要保留一个可用的管理员账号")
	ErrWrongPassword      = newErr(KindInvalid, "当前密码不正确")

	ErrTwoFactorEnabled    = newErr(KindConflict, "两步验证已经开启")
	ErrTwoFactorNotStarted = newErr(KindInvalid, "请先扫描二维码")
	ErrTwoFactorNotEnabled = newErr(KindInvalid, "两步验证尚未开启")
	ErrWrongCode           = newErr(KindInvalid, "验证码不正确或已过期")

	ErrChallengeExpired = apperr.WithCode(KindUnauthorized, "challenge_expired", "验证已过期，请重新登录")
)
