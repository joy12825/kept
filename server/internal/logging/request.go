package logging

import "context"

type requestIDKey struct{}

// WithRequestID returns ctx carrying the ID of the HTTP request it serves.
// A job enqueued under ctx keeps the ID, so its logs can be matched with
// the request's (v1.0.1).
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the request ID in ctx, "" when none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}
