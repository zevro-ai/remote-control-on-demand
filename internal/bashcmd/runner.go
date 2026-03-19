package bashcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	MaxOutputBytes    = 64 * 1024
	DefaultTimeout    = 60 * time.Second
	defaultSystemPATH = "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin"
)

type Result struct {
	Command    string `json:"command"`
	Output     string `json:"output"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

func Run(ctx context.Context, dir, command string) (Result, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return Result{}, fmt.Errorf("command cannot be empty")
	}

	startedAt := time.Now()
	timeout := DefaultTimeout
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = dir
	cmd.Env = ensureSystemPATH(os.Environ())

	output, err := cmd.CombinedOutput()
	result := Result{
		Command:    command,
		DurationMs: time.Since(startedAt).Milliseconds(),
	}

	if len(output) > MaxOutputBytes {
		output = output[:MaxOutputBytes]
		result.Truncated = true
	}
	result.Output = strings.TrimRight(string(output), "\n")

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		result.TimedOut = true
		if strings.TrimSpace(result.Output) == "" {
			result.Output = fmt.Sprintf("Command timed out after %s.", timeout.Round(time.Millisecond))
		}
		return result, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("starting bash: %w", err)
	}

	result.ExitCode = 0
	if strings.TrimSpace(result.Output) == "" {
		result.Output = "(no output)"
	}
	return result, nil
}

func ensureSystemPATH(env []string) []string {
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			return env
		}
	}
	return append(env, "PATH="+defaultSystemPATH)
}
