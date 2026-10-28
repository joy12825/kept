// Package apperr is the error type business packages return for failures
// the API reports to the user as-is. Business packages never deal in HTTP
// status codes: they pick a Kind, and api.respondError maps it.
package apperr

// Error is a failure the API reports to the user as-is. Kind decides the
// HTTP status; Message is user-facing Chinese. Code overrides the status
// code's default machine-readable code when set. Details is extra
// machine-readable context (e.g. remainingAttempts, retryAfterSeconds).
type Error struct {
	Kind    Kind
	Message string
	Code    string
	Details map[string]any
	base    *Error // the sentinel this was derived from, for errors.Is
}

func (e *Error) Error() string { return e.Message }

// Is reports whether e was derived from target (or is target itself), so
// errors.Is(err, ErrSomething) still matches a copy carrying Details from
// With.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	if e.base != nil {
		return e.base == t
	}
	return e == t
}

// With returns a copy of e carrying details, leaving e itself untouched so
// shared sentinel errors are never mutated.
func (e *Error) With(details map[string]any) *Error {
	c := *e
	c.Details = details
	if c.base == nil {
		c.base = e
	}
	return &c
}

type Kind int

const (
	KindInvalid      Kind = iota + 1 // 400: input fails validation
	KindUnauthorized                 // 401: wrong credentials, expired session
	KindForbidden                    // 403: not allowed (not an administrator, role too low)
	KindNotFound                     // 404: missing, or belongs to another account or workspace
	KindConflict                     // 409: name taken, last administrator, already done
	KindRateLimited                  // 429
	KindUnavailable                  // 503: a part of the service is not ready yet (the search index)
)

// New builds an error whose machine-readable code is the Kind's default.
func New(k Kind, msg string) *Error { return &Error{Kind: k, Message: msg} }

// WithCode builds an error with its own machine-readable code.
func WithCode(k Kind, code, msg string) *Error { return &Error{Kind: k, Code: code, Message: msg} }
