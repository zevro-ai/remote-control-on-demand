package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoreRestoreResetsBusyState(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "sessions.json")
	now := time.Now().UTC()
	data, err := json.Marshal(persistedState{
		ActiveSessionID: "one",
		Sessions: []*Session{
			{
				ID:        "one",
				Folder:    "/tmp/demo",
				RelName:   "demo",
				ThreadID:  "thread-1",
				CreatedAt: now,
				UpdatedAt: now,
				Busy:      true,
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	core := NewCore(t.TempDir(), statePath, 10)
	if err := core.Restore(); err != nil {
		t.Fatalf("Restore(): %v", err)
	}

	sess, ok := core.GetSession("one")
	if !ok {
		t.Fatal("expected restored session")
	}
	if sess.Busy {
		t.Fatal("expected restored session to be non-busy")
	}
}

func TestCoreCreateDeleteAndResolveActive(t *testing.T) {
	baseDir := t.TempDir()
	for _, name := range []string{"one", "two", "three"} {
		if err := os.MkdirAll(filepath.Join(baseDir, name, ".git"), 0755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", name, err)
		}
	}

	core := NewCore(baseDir, "", 10)
	first, err := core.CreateSession("one")
	if err != nil {
		t.Fatalf("CreateSession(one): %v", err)
	}
	second, err := core.CreateSession("two")
	if err != nil {
		t.Fatalf("CreateSession(two): %v", err)
	}
	third, err := core.CreateSession("three")
	if err != nil {
		t.Fatalf("CreateSession(three): %v", err)
	}

	core.mu.Lock()
	core.sessions[first.ID].UpdatedAt = time.Unix(10, 0)
	core.sessions[second.ID].UpdatedAt = time.Unix(30, 0)
	core.sessions[third.ID].UpdatedAt = time.Unix(20, 0)
	core.activeSessionID = third.ID
	core.mu.Unlock()

	if err := core.DeleteSession(third.ID); err != nil {
		t.Fatalf("DeleteSession(): %v", err)
	}

	active, err := core.ResolveActive("no sessions", "no active")
	if err != nil {
		t.Fatalf("ResolveActive(): %v", err)
	}
	if active.ID != second.ID {
		t.Fatalf("active session = %q, want %q", active.ID, second.ID)
	}
}

func TestCoreBeginRequestCompletePersistsAndEmits(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "demo", ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}
	statePath := filepath.Join(t.TempDir(), "sessions.json")

	core := NewCore(baseDir, statePath, 3)
	sess, err := core.CreateSession("demo")
	if err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}

	var events []Event
	core.Subscribe(func(event Event) {
		events = append(events, event)
	})

	req, _, err := core.BeginRequest(sess.ID, Message{
		Role:      "user",
		Kind:      "text",
		Content:   "ping",
		Timestamp: time.Now(),
	})
	if err != nil {
		t.Fatalf("BeginRequest(): %v", err)
	}

	updated, err := req.Complete(func(current *Session) *Message {
		reply := Message{
			Role:      "assistant",
			Kind:      "text",
			Content:   "pong",
			Timestamp: time.Now(),
		}
		current.Messages = AppendMessageWithLimit(current.Messages, reply, core.MaxMessages())
		return &reply
	})
	if err != nil {
		t.Fatalf("Complete(): %v", err)
	}

	if updated.Busy {
		t.Fatal("expected completed session to be non-busy")
	}
	if len(updated.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(updated.Messages))
	}
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}

	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("ReadFile(): %v", err)
	}
	var saved persistedState
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("Unmarshal(): %v", err)
	}
	if len(saved.Sessions) != 1 || len(saved.Sessions[0].Messages) != 2 {
		t.Fatalf("saved state = %+v", saved)
	}
}

func TestCoreCreateSessionWithThreadIDAllowsEmptyThreadID(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "demo", ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	core := NewCore(baseDir, "", 10)
	sess, err := core.CreateSessionWithThreadID("demo", "")
	if err != nil {
		t.Fatalf("CreateSessionWithThreadID(): %v", err)
	}

	if sess.ThreadID != "" {
		t.Fatalf("ThreadID = %q, want empty for a new session", sess.ThreadID)
	}
}

func TestCoreCreateSessionRollsBackOnSaveError(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "demo", ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	core := NewCore(baseDir, t.TempDir(), 10)
	if _, err := core.CreateSession("demo"); err == nil {
		t.Fatal("expected CreateSession() to fail when state path is a directory")
	}

	if sessions := core.ListSessions(); len(sessions) != 0 {
		t.Fatalf("sessions = %d, want 0", len(sessions))
	}
	if _, ok := core.Active(); ok {
		t.Fatal("expected no active session after rollback")
	}
}

func TestCoreSetActiveRollsBackOnSaveError(t *testing.T) {
	baseDir := t.TempDir()
	for _, name := range []string{"one", "two"} {
		if err := os.MkdirAll(filepath.Join(baseDir, name, ".git"), 0755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", name, err)
		}
	}

	core := NewCore(baseDir, "", 10)
	first, err := core.CreateSession("one")
	if err != nil {
		t.Fatalf("CreateSession(one): %v", err)
	}
	second, err := core.CreateSession("two")
	if err != nil {
		t.Fatalf("CreateSession(two): %v", err)
	}

	core.statePath = t.TempDir()
	if _, err := core.SetActive(first.ID); err == nil {
		t.Fatal("expected SetActive() to fail when state path is a directory")
	}

	active, ok := core.Active()
	if !ok {
		t.Fatal("expected active session after rollback")
	}
	if active.ID != second.ID {
		t.Fatalf("active session = %q, want %q", active.ID, second.ID)
	}
}

func TestCoreDeleteSessionRollsBackOnSaveError(t *testing.T) {
	baseDir := t.TempDir()
	for _, name := range []string{"one", "two"} {
		if err := os.MkdirAll(filepath.Join(baseDir, name, ".git"), 0755); err != nil {
			t.Fatalf("MkdirAll(%s): %v", name, err)
		}
	}

	core := NewCore(baseDir, "", 10)
	first, err := core.CreateSession("one")
	if err != nil {
		t.Fatalf("CreateSession(one): %v", err)
	}
	second, err := core.CreateSession("two")
	if err != nil {
		t.Fatalf("CreateSession(two): %v", err)
	}

	core.statePath = t.TempDir()
	if err := core.DeleteSession(second.ID); err == nil {
		t.Fatal("expected DeleteSession() to fail when state path is a directory")
	}

	if _, ok := core.GetSession(second.ID); !ok {
		t.Fatal("expected deleted session to be restored after rollback")
	}
	active, ok := core.Active()
	if !ok {
		t.Fatal("expected active session after rollback")
	}
	if active.ID != second.ID {
		t.Fatalf("active session = %q, want %q", active.ID, second.ID)
	}
	if _, ok := core.GetSession(first.ID); !ok {
		t.Fatal("expected untouched session to remain present")
	}
}

func TestCoreResolveActiveReturnsSaveError(t *testing.T) {
	baseDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(baseDir, "demo", ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	core := NewCore(baseDir, "", 10)
	sess, err := core.CreateSession("demo")
	if err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}

	core.mu.Lock()
	core.activeSessionID = ""
	core.statePath = t.TempDir()
	core.mu.Unlock()

	if _, err := core.ResolveActive("no sessions", "no active"); err == nil {
		t.Fatal("expected ResolveActive() to return a save error")
	}

	active, ok := core.Active()
	if ok {
		t.Fatalf("expected no active session after rollback, got %q", active.ID)
	}
	if restored, ok := core.GetSession(sess.ID); !ok || restored.ID != sess.ID {
		t.Fatal("expected session to remain available after failed promotion")
	}
}

func TestEventBusUnsubscribeRemovesSubscriber(t *testing.T) {
	var bus EventBus
	unsubscribe := bus.Subscribe(func(Event) {})
	unsubscribe()

	if got := len(bus.subscribers); got != 0 {
		t.Fatalf("subscriber count = %d, want 0", got)
	}
}
