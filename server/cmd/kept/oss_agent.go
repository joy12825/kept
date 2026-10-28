//go:build !ee

package main

import (
	"context"
	"fmt"
	"io"

	"kept/server/internal/config"
)

// runAgent: the open-source build has no AI helpers to evaluate (v1.13).
func runAgent(_ context.Context, _ []string, _ config.Config, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "这是开源的免费版，不包含 AI 助手")
	return 1
}
