package codex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBuildCodexArgsNewSessionDangerousBypass(t *testing.T) {
	sess := &Session{RelName: "remote-control-on-demand"}

	got := buildCodexArgs(sess, "Wystaw PR", nil, "workspace-write", "gpt-5", true)
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

	got := buildCodexArgs(sess, "Wystaw PR", nil, "workspace-write", "gpt-5", false)
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

func TestBuildCodexArgsNewSessionWithImages(t *testing.T) {
	sess := &Session{RelName: "remote-control-on-demand"}

	got := buildCodexArgs(sess, "Opisz obrazek", []Attachment{
		{Path: "/tmp/one.png"},
		{Path: "/tmp/two.jpg"},
	}, "workspace-write", "gpt-5", false)
	want := []string{
		"exec",
		"--json",
		"--sandbox",
		"workspace-write",
		"--model",
		"gpt-5",
		initialPrompt(sess, "Opisz obrazek"),
		"--image",
		"/tmp/one.png",
		"--image",
		"/tmp/two.jpg",
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

	got := buildCodexArgs(sess, "Działaj", nil, "workspace-write", "gpt-5", true)
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

func TestBuildCodexArgsResumeWithImages(t *testing.T) {
	sess := &Session{
		RelName:  "remote-control-on-demand",
		ThreadID: "thread-123",
	}

	got := buildCodexArgs(sess, "Opisz obrazek", []Attachment{
		{Path: "/tmp/one.png"},
		{Path: "/tmp/two.jpg"},
	}, "workspace-write", "gpt-5", false)
	want := []string{
		"exec",
		"resume",
		"--json",
		"--model",
		"gpt-5",
		"thread-123",
		"Opisz obrazek",
		"--image",
		"/tmp/one.png",
		"--image",
		"/tmp/two.jpg",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildCodexArgs() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestRunCommandPersistsBashMessages(t *testing.T) {
	baseDir := t.TempDir()
	repoDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	mgr := NewManager(baseDir, "")
	sess, err := mgr.Create("demo")
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}

	updated, result, err := mgr.RunCommand(context.Background(), sess.ID, "printf 'hello from bash'")
	if err != nil {
		t.Fatalf("RunCommand(): %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("ExitCode = %d, want 0", result.ExitCode)
	}
	if len(updated.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(updated.Messages))
	}
	if updated.Messages[0].Kind != "bash" || updated.Messages[0].Command == nil {
		t.Fatalf("user message = %#v", updated.Messages[0])
	}
	if updated.Messages[1].Kind != "bash_result" || updated.Messages[1].Command == nil {
		t.Fatalf("result message = %#v", updated.Messages[1])
	}
	if updated.Messages[1].Content != "hello from bash" {
		t.Fatalf("result content = %q", updated.Messages[1].Content)
	}
}

func TestSendEmitsUserAndAssistantEvents(t *testing.T) {
	baseDir := t.TempDir()
	repoDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	mgr := NewManager(baseDir, "")
	sess, err := mgr.Create("demo")
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}

	previousRunCodexFn := runCodexFn
	runCodexFn = func(
		ctx context.Context,
		sess *Session,
		prompt string,
		attachments []Attachment,
		sandbox,
		model string,
		dangerouslyBypassSandbox bool,
	) (string, string, error) {
		return "thread-123", "assistant reply", nil
	}
	t.Cleanup(func() {
		runCodexFn = previousRunCodexFn
	})

	var events []Message
	mgr.Subscribe(func(event Event) {
		if event.Type == EventMessageReceived && event.Message != nil {
			events = append(events, *event.Message)
		}
	})

	updated, reply, err := mgr.Send(context.Background(), sess.ID, "ping", nil)
	if err != nil {
		t.Fatalf("Send(): %v", err)
	}
	if reply != "assistant reply" {
		t.Fatalf("reply = %q", reply)
	}
	if updated.ThreadID != "thread-123" {
		t.Fatalf("thread_id = %q", updated.ThreadID)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Role != "user" || events[0].Content != "ping" {
		t.Fatalf("user event = %#v", events[0])
	}
	if events[1].Role != "assistant" || events[1].Content != "assistant reply" {
		t.Fatalf("assistant event = %#v", events[1])
	}
}

func TestRunCommandEmitsUserAndAssistantEvents(t *testing.T) {
	baseDir := t.TempDir()
	repoDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	mgr := NewManager(baseDir, "")
	sess, err := mgr.Create("demo")
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}

	var events []Message
	mgr.Subscribe(func(event Event) {
		if event.Type == EventMessageReceived && event.Message != nil {
			events = append(events, *event.Message)
		}
	})

	_, result, err := mgr.RunCommand(context.Background(), sess.ID, "printf 'hello from bash'")
	if err != nil {
		t.Fatalf("RunCommand(): %v", err)
	}
	if result.Output != "hello from bash" {
		t.Fatalf("output = %q", result.Output)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Role != "user" || events[0].Kind != "bash" {
		t.Fatalf("user event = %#v", events[0])
	}
	if events[1].Role != "assistant" || events[1].Kind != "bash_result" {
		t.Fatalf("assistant event = %#v", events[1])
	}
}

func TestClosePromotesMostRecentRemainingSession(t *testing.T) {
	baseDir := t.TempDir()
	for _, name := range []string{"one", "two", "three"} {
		repoDir := filepath.Join(baseDir, name)
		if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", name, err)
		}
	}

	mgr := NewManager(baseDir, "")
	first, err := mgr.Create("one")
	if err != nil {
		t.Fatalf("Create(one): %v", err)
	}
	second, err := mgr.Create("two")
	if err != nil {
		t.Fatalf("Create(two): %v", err)
	}
	third, err := mgr.Create("three")
	if err != nil {
		t.Fatalf("Create(three): %v", err)
	}

	mgr.mu.Lock()
	mgr.sessions[first.ID].UpdatedAt = time.Unix(10, 0)
	mgr.sessions[second.ID].UpdatedAt = time.Unix(30, 0)
	mgr.sessions[third.ID].UpdatedAt = time.Unix(20, 0)
	mgr.activeSessionID = third.ID
	mgr.mu.Unlock()

	if err := mgr.Close(third.ID); err != nil {
		t.Fatalf("Close(): %v", err)
	}

	if mgr.activeSessionID != second.ID {
		t.Fatalf("activeSessionID = %q, want %q", mgr.activeSessionID, second.ID)
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
