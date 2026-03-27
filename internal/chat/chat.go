package chat

import (
	"context"
	"time"
)

// Common types used across all chat/agent providers.

type EventType int

const (
	EventSessionCreated EventType = iota
	EventSessionClosed
	EventMessageReceived
	EventMessageDelta
	EventBusyChanged
	EventToolUseStart
	EventToolUseDelta
	EventToolUseFinish
)

type Attachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size,omitempty"`
	URL         string `json:"url,omitempty"`
	Path        string `json:"path,omitempty"`
}

type CommandMeta struct {
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type Message struct {
	Role        string       `json:"role"`
	Kind        string       `json:"kind,omitempty"`
	Content     string       `json:"content"`
	Timestamp   time.Time    `json:"timestamp"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Command     *CommandMeta `json:"command,omitempty"`
}

type Session struct {
	ID          string    `json:"id"`
	Folder      string    `json:"folder"`
	RelName     string    `json:"rel_name"`
	ThreadID    string    `json:"thread_id,omitempty"`
	ThreadReady bool      `json:"thread_ready,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Busy        bool      `json:"busy"`
	Messages    []Message `json:"messages,omitempty"`
}

type ToolCallEvent struct {
	Index       int    `json:"index"`
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

type Event struct {
	Type      EventType
	SessionID string
	Session   *Session
	Message   *Message
	Delta     string
	Busy      bool
	ToolCall  *ToolCallEvent
}

// Provider defines the interface for any chat-based control agent.
type Provider interface {
	ID() string
	ListSessions() []*Session
	GetSession(id string) (*Session, bool)
	CreateSession(folder string) (*Session, error)
	DeleteSession(id string) error
	SendMessage(ctx context.Context, sessionID, message string, attachments []Attachment) error
	RunCommand(ctx context.Context, sessionID, command string) error
	Subscribe(fn func(Event)) func()
}
