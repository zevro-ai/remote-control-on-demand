package gemini

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

func TestManagerMetadata(t *testing.T) {
	t.Parallel()

	mgr := NewManager(t.TempDir(), "")
	metadata := mgr.Metadata()

	if metadata.ID != "gemini" {
		t.Fatalf("metadata.ID = %q, want %q", metadata.ID, "gemini")
	}
	if metadata.DisplayName != "Gemini" {
		t.Fatalf("metadata.DisplayName = %q, want %q", metadata.DisplayName, "Gemini")
	}
	if metadata.Chat == nil {
		t.Fatal("metadata.Chat = nil, want chat capabilities")
	}
	want := provider.ChatCapabilities{
		StreamingDeltas:  true,
		ShellCommandExec: true,
		ThreadResume:     true,
		ImageAttachments: false,
	}
	if *metadata.Chat != want {
		t.Fatalf("metadata.Chat = %#v, want %#v", *metadata.Chat, want)
	}
}

func TestParseExecOutputStreamsAndFinalizesMessage(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"type":"init","session_id":"sess-123"}`,
		`{"type":"message","role":"user","content":"hi"}`,
		`{"type":"message","role":"assistant","content":"hel","delta":true}`,
		`{"type":"message","role":"assistant","content":"lo","delta":true}`,
		`{"type":"result","status":"success"}`,
		"",
	}, "\n")

	var result execResult
	var raw strings.Builder
	var deltas strings.Builder

	parseExecOutput(strings.NewReader(input), &result, &raw, StreamCallback{
		OnTextDelta: func(delta string) {
			deltas.WriteString(delta)
		},
	})

	if got := deltas.String(); got != "hello" {
		t.Fatalf("delta stream = %q, want %q", got, "hello")
	}
	if got := result.Response; got != "hello" {
		t.Fatalf("response = %q, want %q", got, "hello")
	}
	if result.SessionID != "sess-123" {
		t.Fatalf("session_id = %q, want %q", result.SessionID, "sess-123")
	}
}

func TestParseExecOutputHandlesToolUse(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"type":"message","role":"assistant","content":"Let me check.","delta":true}`,
		`{"type":"tool_use","tool_name":"ls","tool_id":"call-1","parameters":{"path":"."}}`,
		`{"type":"tool_result","tool_id":"call-1","status":"success","output":"file.txt"}`,
		`{"type":"message","role":"assistant","content":" Done.","delta":true}`,
		"",
	}, "\n")

	var result execResult
	var raw strings.Builder
	var deltas strings.Builder

	var toolStarts []struct {
		index    int
		id, name string
	}
	var toolFinishes []int

	parseExecOutput(strings.NewReader(input), &result, &raw, StreamCallback{
		OnTextDelta: func(delta string) {
			deltas.WriteString(delta)
		},
		OnToolStart: func(index int, id, name string, params json.RawMessage) {
			toolStarts = append(toolStarts, struct {
				index    int
				id, name string
			}{index, id, name})
		},
		OnToolFinish: func(index int) {
			toolFinishes = append(toolFinishes, index)
		},
	})

	if got := deltas.String(); got != "Let me check. Done." {
		t.Fatalf("text deltas = %q", got)
	}

	if len(toolStarts) != 1 {
		t.Fatalf("expected 1 tool start, got %d", len(toolStarts))
	}
	if toolStarts[0].name != "ls" || toolStarts[0].id != "call-1" {
		t.Fatalf("tool start = %#v", toolStarts[0])
	}

	if len(toolFinishes) != 1 || toolFinishes[0] != 0 {
		t.Fatalf("expected tool finish at index 0, got %v", toolFinishes)
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

	previousRunGeminiFn := runGeminiFn
	runGeminiFn = func(
		ctx context.Context,
		sess *chat.Session,
		prompt,
		permissionMode,
		model string,
		cb StreamCallback,
	) (string, string, error) {
		return "sess-abc", "assistant reply", nil
	}
	t.Cleanup(func() {
		runGeminiFn = previousRunGeminiFn
	})

	var events []chat.Message
	mgr.Subscribe(func(event chat.Event) {
		if event.Type == chat.EventMessageReceived && event.Message != nil {
			events = append(events, *event.Message)
		}
	})

	_, _, err = mgr.Send(context.Background(), sess.ID, "ping", nil)
	if err != nil {
		t.Fatalf("Send(): %v", err)
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
