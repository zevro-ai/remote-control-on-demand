package session

import (
	"context"
	"fmt"
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

func startLongRunningTestCommand(ctx context.Context, dir string, stdout, stderr io.Writer) (*exec.Cmd, error) {
	cmdName := "sleep"
	args := []string{"100"}
	if runtime.GOOS == "windows" {
		cmdName = "powershell"
		args = []string{"-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 100"}
	}

	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	return cmd, nil
}

func waitForSessionExit(t *testing.T, sess *Session) {
	t.Helper()

	if sess == nil || sess.Cmd == nil {
		return
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sess.Cmd.ProcessState != nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("session %s process did not exit before cleanup", sess.ID)
}

func (m *mockRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer) (*exec.Cmd, error) {
	cmd, err := startLongRunningTestCommand(ctx, dir, stdout, stderr)
	if err != nil {
		return nil, err
	}

	if m.startCalled != nil {
		m.startCalled <- true
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
	// Command that exits immediately with error
	cmd := exec.CommandContext(ctx, "go", "invalid-command")
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	if m.startCalled != nil {
		m.startCalled <- true
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
				waitForSessionExit(t, sess)
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

	if err := mgr2.Kill(sess.ID); err != nil {
		t.Fatalf("mgr2.Kill(): %v", err)
	}
	if err := mgr.Kill(sess.ID); err != nil {
		t.Fatalf("mgr.Kill(): %v", err)
	}
	waitForSessionExit(t, sess)
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
	mgr.SetNotifications(true)

	notifCh := make(chan Notification, 10)
	mgr.Subscribe(func(n Notification) {
		notifCh <- n
	})

	sess := &Session{
		ID:           "abcd1234",
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
	case notif := <-notifCh:
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

func TestManager_ConcurrentStart_SameFolder(t *testing.T) {
	tmpBase, err := os.MkdirTemp("", "rcod-concurrent-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpBase)

	projDir := filepath.Join(tmpBase, "my-project")
	if err := os.MkdirAll(filepath.Join(projDir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	runner := &mockRunner{startCalled: make(chan bool, 100)}
	mgr := NewManager(runner, tmpBase, "", false, 0, 0, nil)

	const goroutines = 10
	results := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			_, err := mgr.Start("my-project")
			results <- err
		}()
	}

	var successes, duplicateErrors int
	for i := 0; i < goroutines; i++ {
		err := <-results
		if err == nil {
			successes++
		} else if strings.Contains(err.Error(), "session already running") {
			duplicateErrors++
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}

	if successes != 1 {
		t.Fatalf("expected exactly 1 success, got %d (duplicates: %d)", successes, duplicateErrors)
	}
	if duplicateErrors != goroutines-1 {
		t.Fatalf("expected %d duplicate errors, got %d", goroutines-1, duplicateErrors)
	}

	// Verify only 1 session exists
	sessions := mgr.List()
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session in map, got %d", len(sessions))
	}

	// Kill the single session
	for _, s := range sessions {
		if err := mgr.Kill(s.ID); err != nil {
			t.Fatalf("Kill(%s): %v", s.ID, err)
		}
		waitForSessionExit(t, s)
	}
}

func TestManager_UniqueIDs(t *testing.T) {
	tmpBase, err := os.MkdirTemp("", "rcod-uniqueid-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpBase)

	// Create multiple project dirs
	for i := 0; i < 5; i++ {
		projDir := filepath.Join(tmpBase, fmt.Sprintf("project-%d", i))
		if err := os.MkdirAll(filepath.Join(projDir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	runner := &mockRunner{startCalled: make(chan bool, 100)}
	mgr := NewManager(runner, tmpBase, "", false, 0, 0, nil)

	ids := make(map[string]bool)
	for i := 0; i < 5; i++ {
		sess, err := mgr.Start(fmt.Sprintf("project-%d", i))
		if err != nil {
			t.Fatalf("Start project-%d: %v", i, err)
		}
		if ids[sess.ID] {
			t.Fatalf("duplicate ID generated: %s", sess.ID)
		}
		ids[sess.ID] = true
	}

	for id := range ids {
		sess, ok := mgr.Get(id)
		if !ok {
			t.Fatalf("session %s not found during cleanup", id)
		}
		if err := mgr.Kill(id); err != nil {
			t.Fatalf("Kill(%s): %v", id, err)
		}
		waitForSessionExit(t, sess)
	}
}

func TestManager_SubscribeFanOut(t *testing.T) {
	mgr := NewManager(&mockRunner{}, t.TempDir(), "", false, 0, 0, nil)
	mgr.SetNotifications(true)

	ch1 := make(chan Notification, 10)
	ch2 := make(chan Notification, 10)

	mgr.Subscribe(func(n Notification) { ch1 <- n })
	mgr.Subscribe(func(n Notification) { ch2 <- n })

	mgr.sendNotification("test fan-out")

	select {
	case n := <-ch1:
		if n.Message != "test fan-out" {
			t.Fatalf("subscriber 1: got %q", n.Message)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber 1 did not receive notification")
	}

	select {
	case n := <-ch2:
		if n.Message != "test fan-out" {
			t.Fatalf("subscriber 2: got %q", n.Message)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscriber 2 did not receive notification")
	}
}

func TestManager_NotificationsEnabledByDefault(t *testing.T) {
	mgr := NewManager(&mockRunner{}, t.TempDir(), "", false, 0, 0, nil)

	notifCh := make(chan Notification, 1)
	mgr.Subscribe(func(n Notification) {
		notifCh <- n
	})

	mgr.sendNotification("default on")

	select {
	case notif := <-notifCh:
		if notif.Message != "default on" {
			t.Fatalf("unexpected message %q", notif.Message)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected notification delivery without explicit enable")
	}
}
