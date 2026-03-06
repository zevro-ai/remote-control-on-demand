package session

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/config"
)

type mockRunner struct {
	startCalled chan bool
}

func (m *mockRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer) (*exec.Cmd, error) {
	if m.startCalled != nil {
		m.startCalled <- true
	}

	cmdName := "sleep"
	args := []string{"100"}
	if runtime.GOOS == "windows" {
		cmdName = "waitfor"
		args = []string{"SomethingThatNeverHappens", "/t", "100"}
	}

	cmd := exec.CommandContext(ctx, cmdName, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func (m *mockRunner) IsClaudeProcess(pid int) bool {
	// Simple existence check for test runner
	return pid > 0
}

type mockCrashingRunner struct {
	startCalled chan bool
}

func (m *mockCrashingRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer) (*exec.Cmd, error) {
	if m.startCalled != nil {
		m.startCalled <- true
	}
	// Command that exits immediately with error
	cmd := exec.CommandContext(ctx, "go", "invalid-command")
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func (m *mockCrashingRunner) IsClaudeProcess(pid int) bool {
	return false
}

func TestManager_ProjectConfigOverrides(t *testing.T) {
	tmpBase, err := os.MkdirTemp("", "rcod-mgr-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpBase)

	projDir := filepath.Join(tmpBase, "my-project")
	err = os.MkdirAll(filepath.Join(projDir, ".git"), 0755)
	if err != nil {
		t.Fatal(err)
	}

	yamlContent := `
max_duration: 100ms
`
	err = os.WriteFile(filepath.Join(projDir, ".rcod.yaml"), []byte(yamlContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	runner := &mockRunner{startCalled: make(chan bool, 1)}
	mgr := NewManager(runner, tmpBase, "", false, 0, 0, nil)

	sess, err := mgr.Start("my-project")
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	if sess.Config == nil {
		t.Fatal("expected project config to be loaded")
	}

	// Test max duration auto-kill
	timeout := time.After(2 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("session was not killed after max_duration")
		case <-ticker.C:
			if sess.Status == StatusStopped {
				return // Success
			}
			t.Logf("Session %s status: %s", sess.ID, sess.Status)
		}
	}
}

func TestManager_ProjectConfigAutoRestart(t *testing.T) {
	tmpBase, err := os.MkdirTemp("", "rcod-restart-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpBase)

	projDir := filepath.Join(tmpBase, "my-project")
	err = os.MkdirAll(filepath.Join(projDir, ".git"), 0755)
	if err != nil {
		t.Fatal(err)
	}

	yamlContent := `
auto_restart:
  enabled: true
  max_attempts: 2
  delay: 10ms
`
	err = os.WriteFile(filepath.Join(projDir, ".rcod.yaml"), []byte(yamlContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	runner := &mockCrashingRunner{startCalled: make(chan bool, 10)}
	// Global auto-restart is OFF
	mgr := NewManager(runner, tmpBase, "", false, 0, 0, nil)

	_, err = mgr.Start("my-project")
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}

	// Wait for first start
	select {
	case <-runner.startCalled:
	case <-time.After(1 * time.Second):
		t.Fatal("session did not start")
	}

	// It should restart 2 times because of project config, even though global is OFF
	restarts := 0
	timeout := time.After(2 * time.Second)
	for restarts < 2 {
		select {
		case <-runner.startCalled:
			restarts++
		case <-timeout:
			t.Fatalf("expected 2 restarts, got %d", restarts)
		}
	}
}

func TestManager_Persistence(t *testing.T) {
	tmpBase, err := os.MkdirTemp("", "rcod-persistence-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpBase)

	projDir := filepath.Join(tmpBase, "my-project")
	err = os.MkdirAll(filepath.Join(projDir, ".git"), 0755)
	if err != nil {
		t.Fatal(err)
	}

	stateFile := filepath.Join(tmpBase, "sessions.json")
	runner := &mockRunner{startCalled: make(chan bool, 10)}
	mgr := NewManager(runner, tmpBase, stateFile, false, 0, 0, nil)

	sess, err := mgr.Start("my-project")
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// Manually set a URL to test URL persistence
	sess.URL = "https://claude.ai/code/test"
	mgr.saveState()

	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file not created: %v", err)
	}

	// Create a new manager instance and Restore
	mgr2 := NewManager(runner, tmpBase, stateFile, false, 0, 0, nil)
	if err := mgr2.Restore(); err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	sess2, ok := mgr2.Get(sess.ID)
	if !ok {
		t.Fatal("session not restored")
	}

	if sess2.PID != sess.PID {
		t.Errorf("expected PID %d, got %d", sess.PID, sess2.PID)
	}
	if sess2.URL != "https://claude.ai/code/test" {
		t.Errorf("expected URL to be restored")
	}
	if sess2.Status != StatusRunning {
		t.Errorf("expected status running, got %s", sess2.Status)
	}

	mgr2.Kill(sess.ID)
}

func TestManager_RejectsPathTraversalOutsideBaseFolder(t *testing.T) {
	tmpBase, err := os.MkdirTemp("", "rcod-traversal-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpBase)

	outsideRepo := filepath.Join(filepath.Dir(tmpBase), "outside-repo")
	if err := os.MkdirAll(filepath.Join(outsideRepo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outsideRepo)

	mgr := NewManager(&mockRunner{}, tmpBase, "", false, 0, 0, nil)

	if _, err := mgr.Start("../outside-repo"); err == nil {
		t.Fatal("expected path traversal to be rejected")
	}
}

func TestManager_SendsProgressUpdates(t *testing.T) {
	mgr := NewManager(&mockRunner{}, t.TempDir(), "", false, 0, 0, &config.NotificationsConfig{
		ProgressUpdateInterval: config.Duration(20 * time.Millisecond),
	})

	sess := &Session{
		ID:           "abcd",
		RelName:      "my-project",
		Status:       StatusRunning,
		StartedAt:    time.Now().Add(-2 * time.Minute),
		LastOutputAt: time.Now().Add(-5 * time.Second),
		ClaudeURL:    "https://claude.ai/code/test",
		OutputBuf:    NewRingBuffer(10),
	}
	if _, err := sess.OutputBuf.Write([]byte("latest output line\n")); err != nil {
		t.Fatalf("failed to seed ring buffer: %v", err)
	}

	mgr.notifyProgress(sess)

	select {
	case notif := <-mgr.Notifications():
		if !strings.Contains(notif.Message, "Progress update") {
			t.Fatalf("expected progress notification, got %q", notif.Message)
		}
		if !strings.Contains(notif.Message, "latest output line") {
			t.Fatalf("expected latest log line in notification, got %q", notif.Message)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected progress notification")
	}
}
