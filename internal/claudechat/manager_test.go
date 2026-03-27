package claudechat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
)

func TestParseExecOutputStreamsAndFinalizesMessage(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"123"}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`,
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
	if !strings.Contains(raw.String(), `"type":"assistant"`) {
		t.Fatalf("raw output missing assistant event: %q", raw.String())
	}
}

func TestParseExecOutputHandlesToolUse(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Let me read the file."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_abc123","name":"Read","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"/src/main.ts\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"Let me read the file."},{"type":"tool_use","id":"toolu_abc123","name":"Read","input":{"file_path":"/src/main.ts"}}]}}`,
		"",
	}, "\n")

	var result execResult
	var raw strings.Builder
	var deltas strings.Builder

	var toolStarts []struct {
		index    int
		id, name string
	}
	var toolDeltas []struct {
		index int
		json  string
	}
	var toolFinishes []int

	parseExecOutput(strings.NewReader(input), &result, &raw, StreamCallback{
		OnTextDelta: func(delta string) {
			deltas.WriteString(delta)
		},
		OnToolStart: func(index int, id, name string) {
			toolStarts = append(toolStarts, struct {
				index    int
				id, name string
			}{index, id, name})
		},
		OnToolDelta: func(index int, partialJSON string) {
			toolDeltas = append(toolDeltas, struct {
				index int
				json  string
			}{index, partialJSON})
		},
		OnToolFinish: func(index int) {
			toolFinishes = append(toolFinishes, index)
		},
	})

	// Text deltas should still work
	if got := deltas.String(); got != "Let me read the file." {
		t.Fatalf("text deltas = %q, want %q", got, "Let me read the file.")
	}

	// Tool start
	if len(toolStarts) != 1 {
		t.Fatalf("expected 1 tool start, got %d", len(toolStarts))
	}
	if toolStarts[0].name != "Read" {
		t.Fatalf("tool name = %q, want %q", toolStarts[0].name, "Read")
	}
	if toolStarts[0].id != "toolu_abc123" {
		t.Fatalf("tool id = %q, want %q", toolStarts[0].id, "toolu_abc123")
	}
	if toolStarts[0].index != 1 {
		t.Fatalf("tool index = %d, want 1", toolStarts[0].index)
	}

	// Tool deltas
	if len(toolDeltas) != 2 {
		t.Fatalf("expected 2 tool deltas, got %d", len(toolDeltas))
	}
	combinedJSON := toolDeltas[0].json + toolDeltas[1].json
	if !strings.Contains(combinedJSON, "/src/main.ts") {
		t.Fatalf("tool delta JSON missing file path: %q", combinedJSON)
	}

	// Tool finish
	if len(toolFinishes) != 1 || toolFinishes[0] != 1 {
		t.Fatalf("expected tool finish at index 1, got %v", toolFinishes)
	}

	// Final response
	if result.Response != "Let me read the file." {
		t.Fatalf("response = %q, want %q", result.Response, "Let me read the file.")
	}
}

func TestParseExecOutputMultipleTools(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"I'll check two files."}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":0}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_001","name":"Read","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"file_path\":\"/a.ts\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":1}}`,
		`{"type":"stream_event","event":{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"toolu_002","name":"Bash","input":{}}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"command\":\"ls\"}"}}}`,
		`{"type":"stream_event","event":{"type":"content_block_stop","index":2}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"I'll check two files."}]}}`,
		"",
	}, "\n")

	var result execResult
	var raw strings.Builder
	var toolNames []string

	parseExecOutput(strings.NewReader(input), &result, &raw, StreamCallback{
		OnToolStart: func(index int, id, name string) {
			toolNames = append(toolNames, name)
		},
	})

	if len(toolNames) != 2 {
		t.Fatalf("expected 2 tool starts, got %d", len(toolNames))
	}
	if toolNames[0] != "Read" || toolNames[1] != "Bash" {
		t.Fatalf("tool names = %v, want [Read, Bash]", toolNames)
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

	previousRunClaudeFn := runClaudeFn
	runClaudeFn = func(
		ctx context.Context,
		sess *chat.Session,
		prompt,
		permissionMode,
		model string,
		cb StreamCallback,
	) (string, error) {
		return "assistant reply", nil
	}
	t.Cleanup(func() {
		runClaudeFn = previousRunClaudeFn
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

	err = mgr.RunCommand(context.Background(), sess.ID, "printf 'hello from bash'")
	if err != nil {
		t.Fatalf("RunCommand(): %v", err)
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
	if events[1].Content != "hello from bash" {
		t.Fatalf("content = %q, want 'hello from bash'", events[1].Content)
	}
}

func TestDeleteSessionPromotesMostRecentRemaining(t *testing.T) {
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
	_ = first

	if _, err := mgr.core.SetActive(second.ID); err != nil {
		t.Fatalf("SetActive(second): %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := mgr.core.SetActive(third.ID); err != nil {
		t.Fatalf("SetActive(third): %v", err)
	}

	if err := mgr.DeleteSession(third.ID); err != nil {
		t.Fatalf("DeleteSession(): %v", err)
	}

	active, ok := mgr.core.Active()
	if !ok {
		t.Fatal("expected active session after delete")
	}
	if active.ID != second.ID {
		t.Fatalf("active session = %q, want %q", active.ID, second.ID)
	}
}

func TestHasAssistantReplyIgnoresBashResults(t *testing.T) {
	sess := &chat.Session{
		Messages: []chat.Message{
			{Role: "user", Kind: "bash", Content: "pwd"},
			{Role: "assistant", Kind: "bash_result", Content: "/tmp"},
		},
	}

	if hasAssistantReply(sess) {
		t.Fatal("expected bash results to not count as assistant replies")
	}

	sess.Messages = append(sess.Messages, chat.Message{Role: "assistant", Kind: "text", Content: "Hello"})
	if !hasAssistantReply(sess) {
		t.Fatal("expected text replies to count as assistant replies")
	}
}

func TestSystemPromptTargetsDashboard(t *testing.T) {
	prompt := systemPrompt(&chat.Session{RelName: "demo"})

	if !strings.Contains(prompt, "RCOD dashboard") {
		t.Fatalf("prompt = %q, want dashboard context", prompt)
	}
	if strings.Contains(prompt, "Telegram") {
		t.Fatalf("prompt = %q, should not mention Telegram", prompt)
	}
}
