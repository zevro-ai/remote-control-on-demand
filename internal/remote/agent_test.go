package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestAgentServerPingAndUnknownMethod(t *testing.T) {
	input := strings.NewReader("{\"id\":\"one\",\"method\":\"ping\"}\n{\"id\":\"two\",\"method\":\"missing\",\"provider\":\"codex\"}\n")
	var output bytes.Buffer
	server := AgentServer{}
	if err := server.Serve(context.Background(), input, &output); err != nil {
		t.Fatalf("Serve(): %v", err)
	}
	decoder := json.NewDecoder(&output)
	responses := make(map[string]AgentResponse)
	for len(responses) < 2 {
		var response AgentResponse
		if err := decoder.Decode(&response); err != nil {
			t.Fatalf("response: %v", err)
		}
		responses[response.ID] = response
	}
	first, second := responses["one"], responses["two"]
	if first.ID != "one" || !first.OK {
		t.Fatalf("first response = %#v", first)
	}
	if second.ID != "two" || second.OK || !strings.Contains(second.Error, "remote provider") {
		t.Fatalf("second response = %#v", second)
	}
}

func TestNewAgentClientAddsStdioFlag(t *testing.T) {
	executor, err := NewExecutor(validHost())
	if err != nil {
		t.Fatalf("NewExecutor(): %v", err)
	}
	client, err := NewAgentClient(executor, "/srv/projects", "rcod-agent --base-folder /srv/projects")
	if err != nil {
		t.Fatalf("NewAgentClient(): %v", err)
	}
	if got := strings.Join(client.command, " "); got != "rcod-agent --base-folder /srv/projects --stdio" {
		t.Fatalf("command = %q", got)
	}
}

func TestAgentServerStopsAtEOF(t *testing.T) {
	server := AgentServer{}
	if err := server.Serve(context.Background(), strings.NewReader(""), io.Discard); err != nil {
		t.Fatalf("Serve(EOF): %v", err)
	}
}
