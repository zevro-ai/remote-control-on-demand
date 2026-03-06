package process

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const defaultPermissionMode = "bypassPermissions"

type Runner struct{}

func NewRunner() *Runner {
	return &Runner{}
}

func (r *Runner) IsClaudeProcess(pid int) bool {
	return isClaudeProcess(pid)
}

func (r *Runner) Start(ctx context.Context, dir string, stdout, stderr io.Writer) (*exec.Cmd, error) {
	// RCOD is intentionally opinionated here: remote sessions should always
	// run without interactive permission prompts.
	args := []string{"rc", "--permission-mode", defaultPermissionMode}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = cleanEnv()

	// Start in own process group so we can kill all children (on Unix)
	setProcessGroup(cmd)

	// On context cancel, kill the entire process group (on Unix) or just the process (on Windows)
	cmd.Cancel = func() error {
		return killProcessGroup(cmd)
	}
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

// cleanEnv returns the current environment with Claude Code session
// variables removed, so spawned claude rc processes don't think they're
// nested inside another Claude Code session.
func cleanEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		if strings.HasPrefix(key, "CLAUDE_CODE_") || key == "CLAUDECODE" {
			continue
		}
		env = append(env, e)
	}
	return env
}
