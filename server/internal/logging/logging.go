// Package logging builds the process-wide slog logger.
package logging

import (
	"io"
	"log/slog"
)

// New returns a logger writing to w and keeping recent records in Recent.
// level is debug|info|warn|error and format is text|json; both are
// validated by the config package.
func New(w io.Writer, level, format string) *slog.Logger {
	var lv slog.Level
	_ = lv.UnmarshalText([]byte(level))
	opts := &slog.HandlerOptions{Level: lv}
	var h slog.Handler = slog.NewTextHandler(w, opts)
	if format == "json" {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(&teeHandler{inner: h, ring: Recent})
}
