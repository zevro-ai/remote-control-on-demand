package ralph

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultRunner_Success(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	r := &DefaultRunner{}
	stdout, stderr, exitCode, err := r.Run(context.Background(), "", nil, "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if strings.TrimSpace(stdout) != "hello" {
		t.Fatalf("expected stdout 'hello', got %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
}

func TestDefaultRunner_NonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	r := &DefaultRunner{}
	_, _, exitCode, err := r.Run(context.Background(), "", nil, "bash", "-c", "exit 42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 42 {
		t.Fatalf("expected exit code 42, got %d", exitCode)
	}
}

func TestDefaultRunner_WithEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	r := &DefaultRunner{}
	stdout, _, exitCode, err := r.Run(context.Background(), "", []string{"MY_TEST_VAR=works"}, "bash", "-c", "echo $MY_TEST_VAR")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if strings.TrimSpace(stdout) != "works" {
		t.Fatalf("expected 'works', got %q", stdout)
	}
}

func TestDefaultRunner_ContextCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := &DefaultRunner{}
	_, _, _, err := r.Run(ctx, "", nil, "sleep", "10")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestDefaultRunner_WithDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	r := &DefaultRunner{}
	stdout, _, exitCode, err := r.Run(context.Background(), "/tmp", nil, "pwd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	// /tmp may resolve to /private/tmp on macOS
	got := strings.TrimSpace(stdout)
	if got != "/tmp" && got != "/private/tmp" {
		t.Fatalf("expected /tmp or /private/tmp, got %q", got)
	}
}
