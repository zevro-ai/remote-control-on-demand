package provider

import (
	"context"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
)

// Metadata describes a tool/provider in a transport-neutral way.
// Chat and runtime capabilities are split so callers do not need to infer
// behavior from provider IDs or hard-coded product branches.
type Metadata struct {
	ID          string               `json:"id"`
	DisplayName string               `json:"display_name"`
	Chat        *ChatCapabilities    `json:"chat,omitempty"`
	Runtime     *RuntimeCapabilities `json:"runtime,omitempty"`
}

type ChatCapabilities struct {
	StreamingDeltas       bool `json:"streaming_deltas"`
	ToolCallStreaming     bool `json:"tool_call_streaming"`
	ImageAttachments      bool `json:"image_attachments"`
	ShellCommandExec      bool `json:"shell_command_exec"`
	ThreadResume          bool `json:"thread_resume"`
	AdoptExistingSessions bool `json:"adopt_existing_sessions"`
	ExternalURLDetection  bool `json:"external_url_detection"`
	History               bool `json:"history"`
}

type AdoptableSession struct {
	ThreadID  string    `json:"thread_id"`
	RelName   string    `json:"rel_name"`
	RelCWD    string    `json:"rel_cwd"`
	Folder    string    `json:"-"`
	Title     string    `json:"title"`
	Model     string    `json:"model,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Model struct {
	Slug             string   `json:"slug"`
	DisplayName      string   `json:"display_name"`
	Description      string   `json:"description,omitempty"`
	ReasoningLevels  []string `json:"reasoning_levels,omitempty"`
	DefaultReasoning string   `json:"default_reasoning,omitempty"`
}

type ModelLister interface {
	ListModels() ([]Model, error)
}

type SessionOptionCreator interface {
	CreateSessionWithOptions(folder string, options chat.SessionOptions) (*chat.Session, error)
}

type SessionConfigurator interface {
	SetSessionOptions(id string, options chat.SessionOptions) (*chat.Session, error)
}

type HistorySession struct {
	ThreadID     string    `json:"thread_id"`
	RelName      string    `json:"rel_name"`
	RelCWD       string    `json:"rel_cwd,omitempty"`
	Folder       string    `json:"-"`
	Title        string    `json:"title,omitempty"`
	Model        string    `json:"model,omitempty"`
	Preview      string    `json:"preview,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
	Archived     bool      `json:"archived,omitempty"`
	MessageCount int       `json:"message_count,omitempty"`
}

type SessionHistorian interface {
	ListHistory() ([]HistorySession, error)
	GetHistory(threadID string) ([]chat.Message, error)
}

type FolderLister interface {
	ListFolders() ([]string, error)
}

type RuntimeCapabilities struct {
	LongRunningProcesses bool `json:"long_running_processes"`
	AutoRestart          bool `json:"auto_restart"`
	ExternalURLDetection bool `json:"external_url_detection"`
}

// ChatProvider exposes conversational/thread-style operations.
type ChatProvider interface {
	Metadata() Metadata
	ListSessions() []*chat.Session
	GetSession(id string) (*chat.Session, bool)
	CreateSession(folder string) (*chat.Session, error)
	DeleteSession(id string) error
	SendMessage(ctx context.Context, sessionID, message string, attachments []chat.Attachment) error
	RunCommand(ctx context.Context, sessionID, command string) error
	Subscribe(fn func(chat.Event)) func()
}

// SessionAdopter is an optional chat-provider capability for discovering and
// adopting sessions created outside RCOD.
type SessionAdopter interface {
	ListAdoptableSessions() ([]AdoptableSession, error)
	AdoptSession(threadID string) (*chat.Session, error)
}

type RuntimeNotification struct {
	Provider string
	Message  string
}

type RuntimeSessionSnapshot struct {
	ID        string
	Folder    string
	RelName   string
	Status    string
	URL       string
	PID       int
	StartedAt time.Time
	Restarts  int
}

// RuntimeSession exposes the state and log stream of a long-running process.
type RuntimeSession interface {
	Snapshot() RuntimeSessionSnapshot
	SnapshotLogs(lines int) []string
	SubscribeLogs(fn func(line string)) func()
}

// RuntimeProvider exposes long-running managed process operations.
type RuntimeProvider interface {
	Metadata() Metadata
	BaseFolder() string
	ListFolders() []string
	ListSessions() []RuntimeSession
	GetSession(id string) (RuntimeSession, bool)
	CreateSession(folder string) (RuntimeSession, error)
	DeleteSession(id string) error
	RestartSession(id string) error
	Subscribe(fn func(RuntimeNotification)) func()
}
