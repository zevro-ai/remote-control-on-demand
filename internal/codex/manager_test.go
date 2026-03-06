package codex

import (
	"reflect"
	"testing"
)

func TestBuildCodexArgsNewSessionDangerousBypass(t *testing.T) {
	sess := &Session{RelName: "remote-control-on-demand"}

	got := buildCodexArgs(sess, "Wystaw PR", "workspace-write", "gpt-5", true)
	want := []string{
		"exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"--json",
		"--model",
		"gpt-5",
		initialPrompt(sess, "Wystaw PR"),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildCodexArgs() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBuildCodexArgsNewSessionSandboxed(t *testing.T) {
	sess := &Session{RelName: "remote-control-on-demand"}

	got := buildCodexArgs(sess, "Wystaw PR", "workspace-write", "gpt-5", false)
	want := []string{
		"exec",
		"--json",
		"--sandbox",
		"workspace-write",
		"--model",
		"gpt-5",
		initialPrompt(sess, "Wystaw PR"),
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildCodexArgs() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBuildCodexArgsResumeDangerousBypass(t *testing.T) {
	sess := &Session{
		RelName:  "remote-control-on-demand",
		ThreadID: "thread-123",
	}

	got := buildCodexArgs(sess, "Działaj", "workspace-write", "gpt-5", true)
	want := []string{
		"exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"resume",
		"--json",
		"--model",
		"gpt-5",
		"thread-123",
		"Działaj",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildCodexArgs() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestConfigurePermissionModeDangerousBypass(t *testing.T) {
	mgr := NewManager("/tmp/projects", "")
	mgr.ConfigurePermissionMode("bypassPermissions")

	if !mgr.dangerouslyBypassSandbox {
		t.Fatal("expected dangerouslyBypassSandbox to stay enabled")
	}
	if mgr.sandbox != defaultSandbox {
		t.Fatalf("expected sandbox to remain %q, got %q", defaultSandbox, mgr.sandbox)
	}
}

func TestConfigurePermissionModeDefaultSandbox(t *testing.T) {
	mgr := NewManager("/tmp/projects", "")
	mgr.ConfigurePermissionMode("")

	if mgr.dangerouslyBypassSandbox {
		t.Fatal("expected dangerouslyBypassSandbox to stay disabled")
	}
	if mgr.sandbox != defaultSandbox {
		t.Fatalf("expected sandbox %q, got %q", defaultSandbox, mgr.sandbox)
	}
}

func TestConfigurePermissionModeSandbox(t *testing.T) {
	mgr := NewManager("/tmp/projects", "")
	mgr.ConfigurePermissionMode("read-only")

	if mgr.dangerouslyBypassSandbox {
		t.Fatal("expected dangerouslyBypassSandbox to be disabled")
	}
	if mgr.sandbox != "read-only" {
		t.Fatalf("expected sandbox %q, got %q", "read-only", mgr.sandbox)
	}
}
