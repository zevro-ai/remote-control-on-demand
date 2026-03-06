//go:build windows

package process

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

func setProcessGroup(cmd *exec.Cmd) {
	// Windows doesn't use Setpgid in syscall.SysProcAttr
}

func isClaudeProcess(pid int) bool {
	// Use tasklist /FI "PID eq <pid>"
	b, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid)).Output()
	if err != nil {
		return false
	}
	output := string(b)
	// Check if the output contains the PID and a relevant process name
	return strings.Contains(output, strconv.Itoa(pid)) &&
		(strings.Contains(output, "node") || strings.Contains(output, "claude"))
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// On Windows, terminate the process. For killing the whole tree,
	// one would usually use Job Objects or taskkill /F /T /PID,
	// but for now, simple Kill() will satisfy the build and basic functionality.
	return cmd.Process.Kill()
}
