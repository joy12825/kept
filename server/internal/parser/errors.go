package parser

import "errors"

// Error is a parse failure the user can act on. Permanent ones (the file
// itself is the problem) are not worth retrying.
type Error struct {
	Message   string
	Permanent bool
	Err       error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Err }

var (
	ErrUnsupported = &Error{Message: "不支持这种文件格式，请上传 PDF、图片（JPG、PNG、TIFF、HEIC、WebP、GIF、BMP）、文本文件、Office 文档或 EPUB 电子书", Permanent: true}
	ErrEncrypted   = &Error{Message: "PDF 已加密，请先移除密码再上传", Permanent: true}
	ErrCorrupt     = &Error{Message: "文件已损坏，无法读取", Permanent: true}
	ErrEmpty       = &Error{Message: "文件是空的", Permanent: true}
)

// IsPermanent reports whether err should fail the job without retrying.
func IsPermanent(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Permanent
}
