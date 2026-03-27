package provider

import (
	"context"
	"testing"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
)

func TestRegistryMergesChatAndRuntimeByToolID(t *testing.T) {
	registry := NewRegistry()

	claudeChat, err := NewChatAdapter(Metadata{
		ID:          "claude",
		DisplayName: "Claude",
		Chat: &ChatCapabilities{
			StreamingDeltas:      true,
			ShellCommandExec:     true,
			ThreadResume:         true,
			ExternalURLDetection: true,
		},
	}, stubChatProvider{id: "claude"})
	if err != nil {
		t.Fatalf("NewChatAdapter(): %v", err)
	}

	claudeRuntime, err := NewRuntimeAdapter(Metadata{
		ID:          "claude",
		DisplayName: "Claude",
		Runtime: &RuntimeCapabilities{
			LongRunningProcesses: true,
			AutoRestart:          true,
			ExternalURLDetection: true,
		},
	}, stubRuntimeManager{})
	if err != nil {
		t.Fatalf("NewRuntimeAdapter(): %v", err)
	}

	if err := registry.RegisterChat(claudeChat); err != nil {
		t.Fatalf("RegisterChat(): %v", err)
	}
	if err := registry.RegisterRuntime(claudeRuntime); err != nil {
		t.Fatalf("RegisterRuntime(): %v", err)
	}

	tools := registry.Tools()
	if len(tools) != 1 {
		t.Fatalf("Tools() len = %d, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Metadata.ID != "claude" {
		t.Fatalf("tool ID = %q", tool.Metadata.ID)
	}
	if tool.Metadata.Chat == nil || !tool.Metadata.Chat.ThreadResume {
		t.Fatalf("chat capabilities not preserved: %+v", tool.Metadata.Chat)
	}
	if tool.Metadata.Runtime == nil || !tool.Metadata.Runtime.LongRunningProcesses {
		t.Fatalf("runtime capabilities not preserved: %+v", tool.Metadata.Runtime)
	}
}

func TestRegistryRejectsDuplicateChatRegistration(t *testing.T) {
	registry := NewRegistry()

	provider, err := NewChatAdapter(Metadata{
		ID:          "codex",
		DisplayName: "Codex",
		Chat:        &ChatCapabilities{},
	}, stubChatProvider{id: "codex"})
	if err != nil {
		t.Fatalf("NewChatAdapter(): %v", err)
	}

	if err := registry.RegisterChat(provider); err != nil {
		t.Fatalf("RegisterChat(): %v", err)
	}
	if err := registry.RegisterChat(provider); err == nil {
		t.Fatal("expected duplicate chat registration to fail")
	}
}

func TestRegistrySortsProvidersByID(t *testing.T) {
	registry := NewRegistry()

	for _, item := range []struct {
		id   string
		name string
	}{
		{id: "codex", name: "Codex"},
		{id: "claude", name: "Claude"},
	} {
		provider, err := NewChatAdapter(Metadata{
			ID:          item.id,
			DisplayName: item.name,
			Chat:        &ChatCapabilities{},
		}, stubChatProvider{id: item.id})
		if err != nil {
			t.Fatalf("NewChatAdapter(%s): %v", item.id, err)
		}
		if err := registry.RegisterChat(provider); err != nil {
			t.Fatalf("RegisterChat(%s): %v", item.id, err)
		}
	}

	providers := registry.ChatProviders()
	if len(providers) != 2 {
		t.Fatalf("ChatProviders() len = %d, want 2", len(providers))
	}
	if providers[0].Metadata().ID != "claude" || providers[1].Metadata().ID != "codex" {
		t.Fatalf("provider order = %q, %q", providers[0].Metadata().ID, providers[1].Metadata().ID)
	}
}

func TestRuntimeSessionSnapshotUsesLivePIDAndURL(t *testing.T) {
	sess := &session.Session{
		ID:        "runtime-1",
		Folder:    "/tmp/demo",
		RelName:   "demo",
		URL:       "https://example.test",
		Status:    session.StatusRunning,
		StartedAt: time.Unix(123, 0).UTC(),
		Restarts:  2,
	}

	snapshot := runtimeSessionAdapter{session: sess}.Snapshot()
	if snapshot.ID != "runtime-1" || snapshot.URL != "https://example.test" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.Status != "running" || snapshot.Restarts != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

type stubChatProvider struct {
	id string
}

func (p stubChatProvider) ID() string { return p.id }

func (stubChatProvider) ListSessions() []*chat.Session { return nil }

func (stubChatProvider) GetSession(string) (*chat.Session, bool) { return nil, false }

func (stubChatProvider) CreateSession(string) (*chat.Session, error) { return nil, nil }

func (stubChatProvider) DeleteSession(string) error { return nil }

func (stubChatProvider) SendMessage(context.Context, string, string, []chat.Attachment) error {
	return nil
}

func (stubChatProvider) RunCommand(context.Context, string, string) error { return nil }

func (stubChatProvider) Subscribe(func(chat.Event)) func() { return func() {} }

type stubRuntimeManager struct{}

func (stubRuntimeManager) BaseFolder() string { return "" }

func (stubRuntimeManager) ListFolders() []string { return nil }

func (stubRuntimeManager) List() []*session.Session { return nil }

func (stubRuntimeManager) Get(string) (*session.Session, bool) { return nil, false }

func (stubRuntimeManager) Start(string) (*session.Session, error) { return nil, nil }

func (stubRuntimeManager) Kill(string) error { return nil }

func (stubRuntimeManager) Restart(string) error { return nil }

func (stubRuntimeManager) Subscribe(func(session.Notification)) func() { return func() {} }
