package bashcmd

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunSuccess(t *testing.T) {
	result, err := Run(context.Background(), t.TempDir(), "printf 'hello'")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if result.Output != "hello" {
		t.Fatalf("Output = %q, want %q", result.Output, "hello")
	}
	if result.DurationMs < 0 {
		t.Fatalf("DurationMs = %d, want >= 0", result.DurationMs)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	result, err := Run(context.Background(), t.TempDir(), "printf 'boom'; exit 7")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("ExitCode = %d, want 7", result.ExitCode)
	}
	if result.Output != "boom" {
		t.Fatalf("Output = %q, want %q", result.Output, "boom")
	}
}

func TestRunTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	result, err := Run(ctx, t.TempDir(), "sleep 1")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.TimedOut {
		t.Fatal("expected TimedOut to be true")
	}
	if result.ExitCode != -1 {
		t.Fatalf("ExitCode = %d, want -1", result.ExitCode)
	}
}

func TestRunTruncatesLargeOutput(t *testing.T) {
	command := "python3 - <<'PY'\nprint('a' * 70000)\nPY"
	result, err := Run(context.Background(), t.TempDir(), command)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Truncated {
		t.Fatal("expected Truncated to be true")
	}
	if len(result.Output) > MaxOutputBytes {
		t.Fatalf("len(Output) = %d, want <= %d", len(result.Output), MaxOutputBytes)
	}
	if !strings.HasPrefix(result.Output, "aaaa") {
		t.Fatalf("unexpected Output prefix: %q", result.Output[:min(len(result.Output), 12)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
