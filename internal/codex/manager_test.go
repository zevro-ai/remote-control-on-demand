package codex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
)

func TestBuildCodexArgsNewSessionDangerousBypass(t *testing.T) {
	sess := &chat.Session{RelName: "remote-control-on-demand"}

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
	sess := &chat.Session{RelName: "remote-control-on-demand"}

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
	sess := &chat.Session{RelName: "remote-control-on-demand"}

	got := buildCodexArgs(sess, "Opisz obrazek", []chat.Attachment{
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
	sess := &chat.Session{
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
	sess := &chat.Session{
		RelName:  "remote-control-on-demand",
		ThreadID: "thread-123",
	}

	got := buildCodexArgs(sess, "Opisz obrazek", []chat.Attachment{
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
	sess, err := mgr.CreateSession("demo")
	if err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}

	// We test mgr.Run because it returns the session, RunCommand only returns error in PoC
	updated, result, err := mgr.Run(context.Background(), sess.ID, "printf 'hello from bash'")
	if err != nil {
		t.Fatalf("Run(): %v", err)
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
	sess, err := mgr.CreateSession("demo")
	if err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}

	previousRunCodexFn := runCodexFn
	runCodexFn = func(
		ctx context.Context,
		sess *chat.Session,
		prompt string,
		attachments []chat.Attachment,
		sandbox,
		model string,
		dangerouslyBypassSandbox bool,
		cb StreamCallback,
	) (string, string, error) {
		return "thread-123", "assistant reply", nil
	}
	t.Cleanup(func() {
		runCodexFn = previousRunCodexFn
	})

	var events []chat.Message
	mgr.Subscribe(func(event chat.Event) {
		if event.Type == chat.EventMessageReceived && event.Message != nil {
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

func TestParseExecOutputEmitsStreamingToolAndTextEvents(t *testing.T) {
	input := strings.NewReader(strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-123"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"I will inspect README.md."}}`,
		`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"sed -n '1,20p' README.md","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"sed -n '1,20p' README.md","aggregated_output":"# RCOD","status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"RCOD: Remote Control On Demand"}}`,
		`{"type":"turn.completed"}`,
	}, "\n"))

	var raw strings.Builder
	var result execResult
	var deltas []string
	var events []chat.ToolCallEvent

	parseExecOutput(input, &result, &raw, StreamCallback{
		OnTextDelta: func(delta string) {
			deltas = append(deltas, delta)
		},
		OnToolStart: func(index int, id, name string) {
			events = append(events, chat.ToolCallEvent{Index: index, ID: id, Name: name})
		},
		OnToolDelta: func(index int, partialJSON string) {
			events = append(events, chat.ToolCallEvent{Index: index, PartialJSON: partialJSON})
		},
		OnToolFinish: func(index int) {
			events = append(events, chat.ToolCallEvent{Index: index})
		},
	})

	if result.ThreadID != "thread-123" {
		t.Fatalf("ThreadID = %q", result.ThreadID)
	}

	wantReply := "I will inspect README.md.\n\nRCOD: Remote Control On Demand"
	if result.Response != wantReply {
		t.Fatalf("Response = %q, want %q", result.Response, wantReply)
	}

	if !reflect.DeepEqual(deltas, []string{
		"I will inspect README.md.",
		"\n\nRCOD: Remote Control On Demand",
	}) {
		t.Fatalf("deltas = %#v", deltas)
	}

	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}

	if events[0].Index != 0 || events[0].ID != "item_1" || events[0].Name != "Bash" {
		t.Fatalf("tool start = %#v", events[0])
	}

	if events[1].Index != 0 || events[1].PartialJSON != `{"command":"sed -n '1,20p' README.md"}` {
		t.Fatalf("tool delta = %#v", events[1])
	}

	if events[2].Index != 0 {
		t.Fatalf("tool finish = %#v", events[2])
	}
}

func TestRunCommandEmitsUserAndAssistantEvents(t *testing.T) {
	baseDir := t.TempDir()
	repoDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	mgr := NewManager(baseDir, "")
	sess, err := mgr.CreateSession("demo")
	if err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}

	var events []chat.Message
	mgr.Subscribe(func(event chat.Event) {
		if event.Type == chat.EventMessageReceived && event.Message != nil {
			events = append(events, *event.Message)
		}
	})

	_, result, err := mgr.Run(context.Background(), sess.ID, "printf 'hello from bash'")
	if err != nil {
		t.Fatalf("Run(): %v", err)
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
	first, err := mgr.CreateSession("one")
	if err != nil {
		t.Fatalf("CreateSession(one): %v", err)
	}
	second, err := mgr.CreateSession("two")
	if err != nil {
		t.Fatalf("CreateSession(two): %v", err)
	}
	third, err := mgr.CreateSession("three")
	if err != nil {
		t.Fatalf("CreateSession(three): %v", err)
	}

	mgr.mu.Lock()
	mgr.sessions[first.ID].UpdatedAt = time.Unix(10, 0)
	mgr.sessions[second.ID].UpdatedAt = time.Unix(30, 0)
	mgr.sessions[third.ID].UpdatedAt = time.Unix(20, 0)
	mgr.activeSessionID = third.ID
	mgr.mu.Unlock()

	if err := mgr.DeleteSession(third.ID); err != nil {
		t.Fatalf("DeleteSession(): %v", err)
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
