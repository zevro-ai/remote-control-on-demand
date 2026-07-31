package antigravity

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
	_ "modernc.org/sqlite"
)

func TestMetadata(t *testing.T) {
	t.Parallel()

	metadata := NewManager(t.TempDir(), "").Metadata()
	if metadata.ID != "antigravity" || metadata.DisplayName != "Antigravity" {
		t.Fatalf("metadata identity = %q/%q", metadata.ID, metadata.DisplayName)
	}
	want := provider.ChatCapabilities{
		StreamingDeltas:       true,
		ToolCallStreaming:     true,
		ShellCommandExec:      true,
		ThreadResume:          true,
		AdoptExistingSessions: true,
		ImageAttachments:      false,
		History:               true,
	}
	if metadata.Chat == nil || *metadata.Chat != want {
		t.Fatalf("metadata.Chat = %#v, want %#v", metadata.Chat, want)
	}
}

func TestBuildAGYArgs(t *testing.T) {
	t.Parallel()

	newArgs := buildAGYArgs(&chat.Session{}, "do not put me in argv", "acceptEdits", "gemini-3.6-flash-high")
	got := strings.Join(newArgs, " ")
	if !strings.Contains(got, "--print --output-format stream-json --mode accept-edits --model gemini-3.6-flash-high") {
		t.Fatalf("new conversation args = %q", got)
	}
	if strings.Contains(got, "do not put me in argv") {
		t.Fatal("prompt leaked into process arguments")
	}

	resumeArgs := buildAGYArgs(&chat.Session{ThreadID: "conversation-123", ThreadReady: true}, "", "bypassPermissions", "")
	if got := strings.Join(resumeArgs, " "); got != "--print --output-format stream-json --dangerously-skip-permissions --conversation conversation-123" {
		t.Fatalf("resume args = %q", got)
	}
	effortArgs := buildAGYArgs(&chat.Session{Reasoning: "high"}, "", "accept-edits", "")
	if got := strings.Join(effortArgs, " "); !strings.Contains(got, "--effort high") {
		t.Fatalf("reasoning args = %q", got)
	}
}

func TestParseExecOutputStreamsTextAndToolEvents(t *testing.T) {
	t.Parallel()

	input := strings.Join([]string{
		`{"type":"init","conversation_id":"conv-123"}`,
		`{"type":"message","role":"assistant","content":"hel","delta":true}`,
		`{"type":"tool_use","tool_id":"call-1","tool_name":"ls","parameters":{"path":"."}}`,
		`{"type":"tool_result","tool_id":"call-1"}`,
		`{"type":"message","role":"assistant","content":"lo","delta":true}`,
		`{"type":"result","result":"hello"}`,
		"",
	}, "\n")

	var result execResult
	var raw strings.Builder
	var deltas strings.Builder
	var starts, finishes int
	parseExecOutput(strings.NewReader(input), &result, &raw, StreamCallback{
		OnTextDelta: func(delta string) { deltas.WriteString(delta) },
		OnToolStart: func(index int, id, name string, parameters json.RawMessage) {
			starts++
			if index != 0 || id != "call-1" || name != "ls" || string(parameters) != `{"path":"."}` {
				t.Fatalf("tool start = %d/%q/%q/%s", index, id, name, parameters)
			}
		},
		OnToolFinish: func(index int) {
			finishes++
			if index != 0 {
				t.Fatalf("tool finish index = %d", index)
			}
		},
	})

	if result.ConversationID != "conv-123" || result.Response != "hello" || deltas.String() != "hello" {
		t.Fatalf("parsed result = %#v, deltas=%q", result, deltas.String())
	}
	if starts != 1 || finishes != 1 {
		t.Fatalf("tool events = %d/%d", starts, finishes)
	}
}

func TestParseExecOutputFallsBackToJSONAndText(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, input, want string
	}{
		{name: "json", input: `{"conversation_id":"c-1","response":"json reply"}` + "\n", want: "json reply"},
		{name: "text", input: "plain reply\n", want: "plain reply"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var result execResult
			var raw strings.Builder
			parseExecOutput(strings.NewReader(tc.input), &result, &raw, StreamCallback{})
			if result.Response != tc.want {
				t.Fatalf("response = %q, want %q", result.Response, tc.want)
			}
		})
	}
}

func TestParseExecOutputHandlesNestedDelta(t *testing.T) {
	t.Parallel()

	var result execResult
	var raw strings.Builder
	var deltas strings.Builder
	parseExecOutput(strings.NewReader(`{"type":"content_block_delta","delta":{"type":"text_delta","text":"nested"}}`+"\n"), &result, &raw, StreamCallback{
		OnTextDelta: func(delta string) { deltas.WriteString(delta) },
	})
	if result.Response != "nested" || deltas.String() != "nested" {
		t.Fatalf("nested delta response = %q, deltas = %q", result.Response, deltas.String())
	}
}

func TestResolveAGYCommandEnvUsesConfiguredBinaryAndSafeEnvironment(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "agy")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGY_BIN", bin)
	t.Setenv("HOME", "/tmp/agy-home")
	t.Setenv("PATH", "/custom/bin")
	t.Setenv("AGY_TEST_SECRET", "must-not-pass")

	gotBin, env, err := resolveAGYCommandEnv()
	if err != nil {
		t.Fatal(err)
	}
	if gotBin != bin {
		t.Fatalf("binary = %q, want %q", gotBin, bin)
	}
	envText := strings.Join(env, "\n")
	if strings.Contains(envText, "AGY_TEST_SECRET") {
		t.Fatal("unsafe environment variable was forwarded")
	}
	if !strings.Contains(envText, "HOME=/tmp/agy-home") || !strings.Contains(envText, "PATH="+filepath.Dir(bin)) {
		t.Fatalf("safe environment = %q", envText)
	}
}

func TestListAndAdoptSessionsFromConversationMetadata(t *testing.T) {
	base := t.TempDir()
	repo := filepath.Join(base, "demo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	dataHome := t.TempDir()
	conversationDir := filepath.Join(dataHome, "conversations")
	if err := os.MkdirAll(conversationDir, 0755); err != nil {
		t.Fatal(err)
	}
	id := "11111111-2222-3333-4444-555555555555"
	dbPath := filepath.Join(conversationDir, id+".db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE trajectory_metadata_blob (data BLOB);`)
	if err == nil {
		workspaceURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(repo)}).String()
		blob := appendProtoString(1, workspaceURL)
		blob = append(blob, appendProtoString(2, "gemini-3.6-flash-high")...)
		_, err = db.Exec(`INSERT INTO trajectory_metadata_blob(data) VALUES (?)`, blob)
	}
	if closeErr := db.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", dataHome)

	mgr := NewManager(base, "")
	items, err := mgr.ListAdoptableSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ThreadID != id || items[0].RelName != "demo" || items[0].Model != "gemini-3.6-flash-high" {
		t.Fatalf("adoptable sessions = %#v", items)
	}
	sess, err := mgr.AdoptSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if sess.ThreadID != id || !sess.ThreadReady || sess.RelName != "demo" || sess.Model != "gemini-3.6-flash-high" {
		t.Fatalf("adopted session = %#v", sess)
	}
}

func appendProtoString(field byte, value string) []byte {
	return append([]byte{field<<3 | 2, byte(len(value))}, []byte(value)...)
}
