package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

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
		cb StreamCallback,
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

func TestParseExecOutputStreaming(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-abc"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"bash -lc ls","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"bash -lc ls","status":"completed"}}`,
		`{"type":"item.started","item":{"id":"item_2","type":"reasoning","status":"in_progress"}}`,
		`{"type":"item.completed","item":{"id":"item_2","type":"reasoning","text":"thinking...","status":"completed"}}`,
		`{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Here is the result."}}`,
		`{"type":"turn.completed"}`,
	}, "\n")

	var result execResult
	var raw strings.Builder

	var started []ItemEvent
	var completed []ItemEvent
	cb := StreamCallback{
		OnItemStarted: func(item ItemEvent) {
			started = append(started, item)
		},
		OnItemCompleted: func(item ItemEvent) {
			completed = append(completed, item)
		},
	}

	parseExecOutput(strings.NewReader(input), &result, &raw, cb)

	if result.ThreadID != "thread-abc" {
		t.Fatalf("ThreadID = %q, want %q", result.ThreadID, "thread-abc")
	}
	if result.Response != "Here is the result." {
		t.Fatalf("Response = %q, want %q", result.Response, "Here is the result.")
	}

	if len(started) != 2 {
		t.Fatalf("started events = %d, want 2", len(started))
	}
	if started[0].Type != "command_execution" || started[0].Command != "bash -lc ls" || started[0].Index != 0 {
		t.Fatalf("started[0] = %+v", started[0])
	}
	if started[1].Type != "reasoning" || started[1].Index != 1 {
		t.Fatalf("started[1] = %+v", started[1])
	}

	if len(completed) != 3 {
		t.Fatalf("completed events = %d, want 3", len(completed))
	}
	if completed[0].Type != "command_execution" || completed[0].Index != 0 {
		t.Fatalf("completed[0] = %+v", completed[0])
	}
	if completed[1].Type != "reasoning" || completed[1].Index != 1 {
		t.Fatalf("completed[1] = %+v", completed[1])
	}
	if completed[2].Type != "agent_message" || completed[2].Text != "Here is the result." {
		t.Fatalf("completed[2] = %+v", completed[2])
	}
}

func TestSendEmitsItemStreamingEvents(t *testing.T) {
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
		cb StreamCallback,
	) (string, string, error) {
		if cb.OnItemStarted != nil {
			cb.OnItemStarted(ItemEvent{Index: 0, ID: "item_1", Type: "command_execution", Command: "ls"})
		}
		if cb.OnItemCompleted != nil {
			cb.OnItemCompleted(ItemEvent{Index: 0, ID: "item_1", Type: "command_execution", Command: "ls"})
		}
		return "thread-456", "done", nil
	}
	t.Cleanup(func() {
		runCodexFn = previousRunCodexFn
	})

	var itemStarted []ItemEvent
	var itemCompleted []ItemEvent
	mgr.Subscribe(func(event Event) {
		switch event.Type {
		case EventItemStarted:
			if event.Item != nil {
				itemStarted = append(itemStarted, *event.Item)
			}
		case EventItemCompleted:
			if event.Item != nil {
				itemCompleted = append(itemCompleted, *event.Item)
			}
		}
	})

	_, reply, err := mgr.Send(context.Background(), sess.ID, "do stuff", nil)
	if err != nil {
		t.Fatalf("Send(): %v", err)
	}
	if reply != "done" {
		t.Fatalf("reply = %q, want %q", reply, "done")
	}

	if len(itemStarted) != 1 {
		t.Fatalf("itemStarted = %d, want 1", len(itemStarted))
	}
	if itemStarted[0].Type != "command_execution" || itemStarted[0].Command != "ls" {
		t.Fatalf("itemStarted[0] = %+v", itemStarted[0])
	}

	if len(itemCompleted) != 1 {
		t.Fatalf("itemCompleted = %d, want 1", len(itemCompleted))
	}
	if itemCompleted[0].Type != "command_execution" {
		t.Fatalf("itemCompleted[0] = %+v", itemCompleted[0])
	}
}

func TestSendEmitsTextDeltaEvents(t *testing.T) {
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
		cb StreamCallback,
	) (string, string, error) {
		if cb.OnTextDelta != nil {
			cb.OnTextDelta("hello")
			cb.OnTextDelta(" world")
		}
		return "thread-789", "hello world", nil
	}
	t.Cleanup(func() {
		runCodexFn = previousRunCodexFn
	})

	var deltas []string
	mgr.Subscribe(func(event Event) {
		if event.Type == EventMessageDelta {
			deltas = append(deltas, event.Delta)
		}
	})

	_, reply, err := mgr.Send(context.Background(), sess.ID, "ping", nil)
	if err != nil {
		t.Fatalf("Send(): %v", err)
	}
	if reply != "hello world" {
		t.Fatalf("reply = %q, want %q", reply, "hello world")
	}
	if !reflect.DeepEqual(deltas, []string{"hello", " world"}) {
		t.Fatalf("deltas = %#v", deltas)
	}
}

func TestHandleAppServerNotificationStreamsAgentText(t *testing.T) {
	state := &appServerTurnState{}
	var reply strings.Builder
	var deltas []string

	err := handleAppServerNotification(
		"item/agentMessage/delta",
		json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"hel"}`),
		state,
		&reply,
		StreamCallback{
			OnTextDelta: func(delta string) {
				deltas = append(deltas, delta)
			},
		},
	)
	if err != nil {
		t.Fatalf("handleAppServerNotification(delta 1): %v", err)
	}

	err = handleAppServerNotification(
		"item/agentMessage/delta",
		json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"lo"}`),
		state,
		&reply,
		StreamCallback{
			OnTextDelta: func(delta string) {
				deltas = append(deltas, delta)
			},
		},
	)
	if err != nil {
		t.Fatalf("handleAppServerNotification(delta 2): %v", err)
	}

	err = handleAppServerNotification(
		"item/completed",
		json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","item":{"type":"agentMessage","id":"item-1","text":"hello"}}`),
		state,
		&reply,
		StreamCallback{},
	)
	if err != nil {
		t.Fatalf("handleAppServerNotification(completed): %v", err)
	}

	if reply.String() != "hello" {
		t.Fatalf("reply = %q, want %q", reply.String(), "hello")
	}
	if !reflect.DeepEqual(deltas, []string{"hel", "lo"}) {
		t.Fatalf("deltas = %#v", deltas)
	}
}

func TestNormalizeAppServerItemCommandExecution(t *testing.T) {
	item, ok := normalizeAppServerItem(appServerItem{
		ID:      "cmd-1",
		Type:    "commandExecution",
		Command: "ls -la",
		Status:  "completed",
	})
	if !ok {
		t.Fatal("expected normalized item")
	}
	if item.Type != "command_execution" {
		t.Fatalf("Type = %q", item.Type)
	}
	if item.Command != "ls -la" {
		t.Fatalf("Command = %q", item.Command)
	}
}

func TestAppServerClientBuffersNotificationsBeforeResponse(t *testing.T) {
	events := make(chan appServerEnvelope, 2)
	scanErr := make(chan error)
	client := &appServerClient{
		stdin:   nopWriteCloser{},
		events:  events,
		scanErr: scanErr,
	}

	responseID := 1
	notification := appServerEnvelope{
		Method: "item/agentMessage/delta",
		Params: json.RawMessage(`{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"hello"}`),
	}
	response := appServerEnvelope{
		ID:     &responseID,
		Result: json.RawMessage(`{"turn":{"id":"turn-1"}}`),
	}

	events <- notification
	events <- response

	if _, err := client.sendRequest("turn/start", map[string]any{"threadId": "thread-1"}); err != nil {
		t.Fatalf("sendRequest(): %v", err)
	}

	next, err := client.nextEnvelope(context.Background())
	if err != nil {
		t.Fatalf("nextEnvelope(): %v", err)
	}
	if next.Method != "item/agentMessage/delta" {
		t.Fatalf("next.Method = %q", next.Method)
	}
}

func TestParseExecOutputNilCallbacks(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"thread.started","thread_id":"t-1"}`,
		`{"type":"item.started","item":{"id":"i1","type":"command_execution","command":"ls"}}`,
		`{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"hello"}}`,
	}, "\n")

	var result execResult
	var raw strings.Builder
	parseExecOutput(strings.NewReader(input), &result, &raw, StreamCallback{})

	if result.ThreadID != "t-1" {
		t.Fatalf("ThreadID = %q", result.ThreadID)
	}
	if result.Response != "hello" {
		t.Fatalf("Response = %q", result.Response)
	}
}

func TestInactivityReaderTimesOutOnNoData(t *testing.T) {
	pr, _ := io.Pipe()
	ctx, cancelCause := context.WithCancelCause(context.Background())
	defer cancelCause(nil)

	ir := newInactivityReader(pr, 50*time.Millisecond, cancelCause)
	defer ir.Stop()

	time.Sleep(100 * time.Millisecond)

	cause := context.Cause(ctx)
	if !errors.Is(cause, errExecInactivityTimeout) {
		t.Fatalf("expected errExecInactivityTimeout, got %v", cause)
	}
}

func TestInactivityReaderResetsTimerOnData(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancelCause := context.WithCancelCause(context.Background())
	defer cancelCause(nil)

	ir := newInactivityReader(pr, 100*time.Millisecond, cancelCause)
	defer ir.Stop()

	go func() {
		for i := 0; i < 6; i++ {
			time.Sleep(50 * time.Millisecond)
			pw.Write([]byte("data\n"))
		}
		pw.Close()
	}()

	buf := make([]byte, 1024)
	for {
		_, err := ir.Read(buf)
		if err != nil {
			break
		}
	}

	if errors.Is(context.Cause(ctx), errExecInactivityTimeout) {
		t.Fatal("context was cancelled by inactivity, but data was flowing")
	}
}

func TestInactivityReaderTimesOutAfterDataStops(t *testing.T) {
	pr, pw := io.Pipe()
	ctx, cancelCause := context.WithCancelCause(context.Background())
	defer cancelCause(nil)

	ir := newInactivityReader(pr, 80*time.Millisecond, cancelCause)
	defer ir.Stop()

	go func() {
		pw.Write([]byte("initial data\n"))
	}()

	buf := make([]byte, 1024)
	ir.Read(buf)

	time.Sleep(150 * time.Millisecond)

	cause := context.Cause(ctx)
	if !errors.Is(cause, errExecInactivityTimeout) {
		t.Fatalf("expected errExecInactivityTimeout after data stopped, got %v", cause)
	}
}

func TestCancelAbortsRunningSession(t *testing.T) {
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
		sandbox, model string,
		dangerouslyBypassSandbox bool,
		cb StreamCallback,
	) (string, string, error) {
		<-ctx.Done()
		cause := context.Cause(ctx)
		if errors.Is(cause, errCancelledByUser) {
			return "", "", fmt.Errorf("codex exec cancelled by user")
		}
		return "", "", ctx.Err()
	}
	t.Cleanup(func() { runCodexFn = previousRunCodexFn })

	errCh := make(chan error, 1)
	go func() {
		_, _, err := mgr.Send(context.Background(), sess.ID, "hello", nil)
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)

	if err := mgr.Cancel(sess.ID); err != nil {
		t.Fatalf("Cancel(): %v", err)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error from Send()")
		}
		if !strings.Contains(err.Error(), "cancelled by user") {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Send() did not return after Cancel()")
	}
}

func TestSendEmitsErrorEvent(t *testing.T) {
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
		sandbox, model string,
		dangerouslyBypassSandbox bool,
		cb StreamCallback,
	) (string, string, error) {
		return "", "", fmt.Errorf("something went wrong")
	}
	t.Cleanup(func() { runCodexFn = previousRunCodexFn })

	var errorEvents []Event
	mgr.Subscribe(func(event Event) {
		if event.Type == EventError {
			errorEvents = append(errorEvents, event)
		}
	})

	_, _, err = mgr.Send(context.Background(), sess.ID, "hello", nil)
	if err == nil {
		t.Fatal("expected error from Send()")
	}

	if len(errorEvents) != 1 {
		t.Fatalf("error events = %d, want 1", len(errorEvents))
	}
	if errorEvents[0].Error != "something went wrong" {
		t.Fatalf("error = %q", errorEvents[0].Error)
	}
}
