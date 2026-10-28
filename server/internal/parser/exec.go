package parser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Timeout for external tools: a base plus a share per page, capped
// (design doc 4.3).
func Timeout(pages int) time.Duration {
	return min(60*time.Second+time.Duration(max(pages, 1))*30*time.Second, 30*time.Minute)
}

// runResult is a finished command's output.
type runResult struct {
	stdout, stderr string
	exitCode       int
}

// run executes name with a timeout. On timeout or cancellation the whole
// process group is killed, so helpers a tool spawned (ocrmypdf runs
// tesseract and ghostscript) do not linger.
func run(ctx context.Context, timeout time.Duration, name string, args ...string) (runResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	res := runResult{stdout: out.String(), stderr: errOut.String()}
	var exitErr *exec.ExitError
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return res, fmt.Errorf("%s timed out after %s", name, timeout)
	case errors.As(err, &exitErr):
		res.exitCode = exitErr.ExitCode()
		return res, fmt.Errorf("%s exited with %d: %s", name, res.exitCode, lastLine(res.stderr))
	case err != nil:
		return res, fmt.Errorf("%s: %w", name, err)
	}
	return res, nil
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// Tools lists the external programs the parser needs.
var Tools = []string{"pdfinfo", "pdftotext", "pdftoppm", "ocrmypdf"}

// MissingTools reports which of Tools are not on PATH.
func MissingTools() []string {
	var missing []string
	for _, t := range Tools {
		if _, err := exec.LookPath(t); err != nil {
			missing = append(missing, t)
		}
	}
	return missing
}
