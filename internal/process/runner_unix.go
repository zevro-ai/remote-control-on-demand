//go:build !windows

package process

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func isClaudeProcess(pid int) bool {
	// Use ps -p <pid> -o args= to get the command line
	b, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "args=").Output()
	if err != nil {
		return false
	}
	cmdline := string(b)
	// Claude Code often runs as 'node ... claude' or just 'claude'
	return strings.Contains(cmdline, "claude") || strings.Contains(cmdline, "node")
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid

	// Send SIGINT first (mimics Ctrl+C) for graceful shutdown —
	// claude rc uses this to cleanly disconnect from the Anthropic relay API.
	if err := syscall.Kill(-pid, syscall.SIGINT); err != nil {
		return err
	}

	// Escalate to SIGTERM after 3s if the process is still alive.
	// WaitDelay (5s) handles the final force-kill via SIGKILL.
	go func() {
		time.Sleep(3 * time.Second)
		_ = syscall.Kill(-pid, syscall.SIGTERM)
	}()

	return nil
}
