package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
)

type ChatAdapter struct {
	metadata Metadata
	provider chat.Provider
}

func NewChatAdapter(metadata Metadata, provider chat.Provider) (*ChatAdapter, error) {
	if provider == nil {
		return nil, fmt.Errorf("chat provider is required")
	}
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	if metadata.Chat == nil {
		return nil, fmt.Errorf("chat capabilities are required for %q", metadata.ID)
	}
	if provider.ID() != "" && metadata.ID != provider.ID() {
		return nil, fmt.Errorf("chat provider ID mismatch: metadata=%q provider=%q", metadata.ID, provider.ID())
	}
	return &ChatAdapter{
		metadata: metadata,
		provider: provider,
	}, nil
}

func (a *ChatAdapter) Metadata() Metadata {
	return cloneMetadata(a.metadata)
}

func (a *ChatAdapter) ListSessions() []*chat.Session {
	return a.provider.ListSessions()
}

func (a *ChatAdapter) GetSession(id string) (*chat.Session, bool) {
	return a.provider.GetSession(id)
}

func (a *ChatAdapter) CreateSession(folder string) (*chat.Session, error) {
	return a.provider.CreateSession(folder)
}

func (a *ChatAdapter) DeleteSession(id string) error {
	return a.provider.DeleteSession(id)
}

func (a *ChatAdapter) SendMessage(ctx context.Context, sessionID, message string, attachments []chat.Attachment) error {
	return a.provider.SendMessage(ctx, sessionID, message, attachments)
}

func (a *ChatAdapter) RunCommand(ctx context.Context, sessionID, command string) error {
	return a.provider.RunCommand(ctx, sessionID, command)
}

func (a *ChatAdapter) Subscribe(fn func(chat.Event)) func() {
	return a.provider.Subscribe(fn)
}

func validateMetadata(metadata Metadata) error {
	if strings.TrimSpace(metadata.ID) == "" {
		return fmt.Errorf("provider ID is required")
	}
	if strings.TrimSpace(metadata.DisplayName) == "" {
		return fmt.Errorf("display name is required for %q", metadata.ID)
	}
	return nil
}

func cloneMetadata(metadata Metadata) Metadata {
	clone := metadata
	if metadata.Chat != nil {
		capabilities := *metadata.Chat
		clone.Chat = &capabilities
	}
	if metadata.Runtime != nil {
		capabilities := *metadata.Runtime
		clone.Runtime = &capabilities
	}
	return clone
}
