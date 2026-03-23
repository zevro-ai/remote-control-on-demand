package ralph

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// CmdRunner abstracts command execution for testing.
type CmdRunner interface {
	Run(ctx context.Context, dir string, env []string, name string, args ...string) (stdout, stderr string, exitCode int, err error)
	RunWithStdin(ctx context.Context, dir string, env []string, stdin string, name string, args ...string) (stdout, stderr string, exitCode int, err error)
}

// DefaultRunner executes commands via os/exec.
type DefaultRunner struct{}

func (r *DefaultRunner) Run(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, int, error) {
	return r.RunWithStdin(ctx, dir, env, "", name, args...)
}

func (r *DefaultRunner) RunWithStdin(ctx context.Context, dir string, env []string, stdin string, name string, args ...string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			err = nil
		}
	}

	return stdoutBuf.String(), stderrBuf.String(), exitCode, err
}
