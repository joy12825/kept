//go:build !ee

package main

import (
	"fmt"
	"io"
)

// The open-source build only installs licenses; it does not issue them (v1.13).

const issuerUsage = ""

func licenseKeygen(_ []string, _, stderr io.Writer) int { return noIssuing(stderr) }

func licenseIssue(_ []string, _, stderr io.Writer) int { return noIssuing(stderr) }

func noIssuing(stderr io.Writer) int {
	fmt.Fprintln(stderr, "这是开源的免费版，不能签发许可证")
	return 1
}
