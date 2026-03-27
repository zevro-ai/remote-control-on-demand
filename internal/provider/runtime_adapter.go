package provider

import (
	"fmt"

	"github.com/zevro-ai/remote-control-on-demand/internal/session"
)

type sessionManager interface {
	BaseFolder() string
	List() []*session.Session
	Get(id string) (*session.Session, bool)
	Start(folder string) (*session.Session, error)
	Kill(id string) error
	Restart(id string) error
	Subscribe(fn func(session.Notification)) func()
}

type RuntimeAdapter struct {
	metadata Metadata
	manager  sessionManager
}

type runtimeSessionAdapter struct {
	session *session.Session
}

func NewRuntimeAdapter(metadata Metadata, manager sessionManager) (*RuntimeAdapter, error) {
	if manager == nil {
		return nil, fmt.Errorf("runtime provider is required")
	}
	if err := validateMetadata(metadata); err != nil {
		return nil, err
	}
	if metadata.Runtime == nil {
		return nil, fmt.Errorf("runtime capabilities are required for %q", metadata.ID)
	}
	return &RuntimeAdapter{
		metadata: metadata,
		manager:  manager,
	}, nil
}

func (a *RuntimeAdapter) Metadata() Metadata {
	return cloneMetadata(a.metadata)
}

func (a *RuntimeAdapter) BaseFolder() string {
	return a.manager.BaseFolder()
}

func (a *RuntimeAdapter) ListSessions() []RuntimeSession {
	sessions := a.manager.List()
	out := make([]RuntimeSession, 0, len(sessions))
	for _, sess := range sessions {
		if sess == nil {
			continue
		}
		out = append(out, runtimeSessionAdapter{session: sess})
	}
	return out
}

func (a *RuntimeAdapter) GetSession(id string) (RuntimeSession, bool) {
	sess, ok := a.manager.Get(id)
	if !ok || sess == nil {
		return nil, false
	}
	return runtimeSessionAdapter{session: sess}, true
}

func (a *RuntimeAdapter) CreateSession(folder string) (RuntimeSession, error) {
	sess, err := a.manager.Start(folder)
	if err != nil {
		return nil, err
	}
	return runtimeSessionAdapter{session: sess}, nil
}

func (a *RuntimeAdapter) DeleteSession(id string) error {
	return a.manager.Kill(id)
}

func (a *RuntimeAdapter) RestartSession(id string) error {
	return a.manager.Restart(id)
}

func (a *RuntimeAdapter) Subscribe(fn func(RuntimeNotification)) func() {
	return a.manager.Subscribe(func(notification session.Notification) {
		fn(RuntimeNotification{Message: notification.Message})
	})
}

func (s runtimeSessionAdapter) Snapshot() RuntimeSessionSnapshot {
	if s.session == nil {
		return RuntimeSessionSnapshot{}
	}

	pid := s.session.PID
	if pid == 0 && s.session.Cmd != nil && s.session.Cmd.Process != nil {
		pid = s.session.Cmd.Process.Pid
	}

	url := s.session.ClaudeURL
	if url == "" {
		url = s.session.URL
	}

	return RuntimeSessionSnapshot{
		ID:        s.session.ID,
		Folder:    s.session.Folder,
		RelName:   s.session.RelName,
		Status:    string(s.session.Status),
		URL:       url,
		PID:       pid,
		StartedAt: s.session.StartedAt,
		Restarts:  s.session.Restarts,
	}
}

func (s runtimeSessionAdapter) SnapshotLogs(lines int) []string {
	if s.session == nil || s.session.OutputBuf == nil {
		return nil
	}
	return s.session.OutputBuf.Lines(lines)
}

func (s runtimeSessionAdapter) SubscribeLogs(fn func(line string)) func() {
	if s.session == nil || s.session.OutputBuf == nil {
		return func() {}
	}
	return s.session.OutputBuf.Subscribe(fn)
}
