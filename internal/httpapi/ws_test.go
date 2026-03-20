package httpapi

import (
	"testing"

	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
)

func TestCodexEventMessageUsesDedicatedToolFields(t *testing.T) {
	started, ok := codexEventMessage(codex.Event{
		Type:      codex.EventItemStarted,
		SessionID: "codex-1",
		Item: &codex.ItemEvent{
			Index:   3,
			ID:      "item-1",
			Type:    "command_execution",
			Command: "ls -la",
		},
	})
	if !ok {
		t.Fatal("expected started message")
	}
	if started.Delta != "" {
		t.Fatalf("started.Delta = %q, want empty", started.Delta)
	}
	if started.ToolCall == nil || started.ToolCall.Command != "ls -la" {
		t.Fatalf("started.ToolCall = %#v", started.ToolCall)
	}

	completed, ok := codexEventMessage(codex.Event{
		Type:      codex.EventItemCompleted,
		SessionID: "codex-1",
		Item: &codex.ItemEvent{
			Index: 3,
			ID:    "item-1",
			Type:  "command_execution",
			Text:  "total 0",
		},
	})
	if !ok {
		t.Fatal("expected completed message")
	}
	if completed.Delta != "" {
		t.Fatalf("completed.Delta = %q, want empty", completed.Delta)
	}
	if completed.ToolCall == nil || completed.ToolCall.OutputText != "total 0" {
		t.Fatalf("completed.ToolCall = %#v", completed.ToolCall)
	}
}
