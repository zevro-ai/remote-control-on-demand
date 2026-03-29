package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type persistedState struct {
	ActiveSessionID string     `json:"active_session_id"`
	Sessions        []*Session `json:"sessions"`
}

type EventBus struct {
	mu          sync.Mutex
	nextID      int
	subscribers map[int]func(Event)
}

func (b *EventBus) Subscribe(fn func(Event)) func() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subscribers == nil {
		b.subscribers = make(map[int]func(Event))
	}
	id := b.nextID
	b.nextID++
	b.subscribers[id] = fn
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subscribers, id)
	}
}

func (b *EventBus) Emit(event Event) {
	b.mu.Lock()
	subs := make([]func(Event), 0, len(b.subscribers))
	for _, fn := range b.subscribers {
		subs = append(subs, fn)
	}
	b.mu.Unlock()
	for _, fn := range subs {
		if fn != nil {
			fn(event)
		}
	}
}

type Core struct {
	mu              sync.Mutex
	baseFolder      string
	statePath       string
	maxMessages     int
	sessions        map[string]*Session
	activeSessionID string
	bus             EventBus
	wg              sync.WaitGroup
}

type Request struct {
	core      *Core
	sessionID string
	mu        sync.Mutex
	done      bool
}

func NewCore(baseFolder, statePath string, maxMessages int) *Core {
	if maxMessages <= 0 {
		maxMessages = DefaultMaxMessages
	}

	return &Core{
		baseFolder:  baseFolder,
		statePath:   statePath,
		maxMessages: maxMessages,
		sessions:    make(map[string]*Session),
	}
}

func (c *Core) BaseFolder() string {
	return c.baseFolder
}

func (c *Core) MaxMessages() int {
	return c.maxMessages
}

func (c *Core) Subscribe(fn func(Event)) func() {
	return c.bus.Subscribe(fn)
}

func (c *Core) Emit(event Event) {
	c.bus.Emit(event)
}

func (c *Core) Shutdown() {
	c.wg.Wait()
}

func (c *Core) Restore() error {
	if c.statePath == "" {
		return nil
	}

	data, err := os.ReadFile(c.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading chat state: %w", err)
	}

	var saved persistedState
	if err := json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("parsing chat state: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.activeSessionID = saved.ActiveSessionID
	c.sessions = make(map[string]*Session, len(saved.Sessions))
	for _, sess := range saved.Sessions {
		if sess == nil || sess.ID == "" {
			continue
		}
		sess.Busy = false
		if !sess.ThreadReady && sess.ThreadID != "" && sessionHasAssistantTextReply(sess) {
			sess.ThreadReady = true
		}
		c.sessions[sess.ID] = sess
	}
	if _, ok := c.sessions[c.activeSessionID]; !ok {
		c.activeSessionID = ""
	}

	return nil
}

func sessionHasAssistantTextReply(sess *Session) bool {
	for _, msg := range sess.Messages {
		if msg.Role == "assistant" && msg.Kind == "text" {
			return true
		}
	}
	return false
}

func (c *Core) CreateSession(folder string) (*Session, error) {
	return c.createSession(folder, "", false)
}

func (c *Core) CreateSessionWithThread(folder, threadID string, threadReady bool) (*Session, error) {
	return c.createSession(folder, threadID, threadReady)
}

func (c *Core) createSession(folder, threadID string, threadReady bool) (*Session, error) {
	var err error
	if threadID == "" {
		threadID, err = GenerateUUID()
		if err != nil {
			return nil, fmt.Errorf("generating thread ID: %w", err)
		}
	}
	fullPath, relName, err := ResolveProjectPath(c.baseFolder, folder)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(fullPath)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("folder %q does not exist", relName)
	}

	if _, err := os.Stat(filepath.Join(fullPath, ".git")); err != nil {
		return nil, fmt.Errorf("folder %q is not a git repository", relName)
	}

	c.mu.Lock()
	id, err := generateUniqueSessionID(c.sessions)
	if err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("generating session ID: %w", err)
	}

	now := time.Now()
	sess := &Session{
		ID:          id,
		Folder:      fullPath,
		RelName:     relName,
		ThreadID:    threadID,
		ThreadReady: threadReady,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	previousActive := c.activeSessionID
	c.sessions[id] = sess
	c.activeSessionID = id
	if err := c.saveLocked(); err != nil {
		delete(c.sessions, id)
		c.activeSessionID = previousActive
		c.mu.Unlock()
		return nil, err
	}
	clone := CloneSession(sess)
	c.mu.Unlock()

	c.Emit(Event{Type: EventSessionCreated, SessionID: id, Session: clone})
	return clone, nil
}

func (c *Core) ListSessions() []*Session {
	c.mu.Lock()
	defer c.mu.Unlock()

	list := make([]*Session, 0, len(c.sessions))
	for _, sess := range c.sessions {
		list = append(list, CloneSession(sess))
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].ID == c.activeSessionID {
			return true
		}
		if list[j].ID == c.activeSessionID {
			return false
		}
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})

	return list
}

func (c *Core) GetSession(id string) (*Session, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	sess, ok := c.sessions[id]
	if !ok {
		return nil, false
	}
	return CloneSession(sess), true
}

func (c *Core) Active() (*Session, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.activeSessionID == "" {
		return nil, false
	}
	sess, ok := c.sessions[c.activeSessionID]
	if !ok {
		return nil, false
	}
	return CloneSession(sess), true
}

func (c *Core) SetActive(id string) (*Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	sess, ok := c.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %q not found", id)
	}

	previousActive := c.activeSessionID
	previousUpdatedAt := sess.UpdatedAt
	c.activeSessionID = id
	sess.UpdatedAt = time.Now()
	if err := c.saveLocked(); err != nil {
		c.activeSessionID = previousActive
		sess.UpdatedAt = previousUpdatedAt
		return nil, err
	}

	return CloneSession(sess), nil
}

func (c *Core) ResolveActive(noSessionsError, noActiveError string) (*Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.activeSessionID != "" {
		sess, ok := c.sessions[c.activeSessionID]
		if ok {
			return CloneSession(sess), nil
		}
		c.activeSessionID = ""
	}

	if len(c.sessions) == 1 {
		previousActive := c.activeSessionID
		for id, sess := range c.sessions {
			c.activeSessionID = id
			if err := c.saveLocked(); err != nil {
				c.activeSessionID = previousActive
				return nil, fmt.Errorf("persisting active session: %w", err)
			}
			return CloneSession(sess), nil
		}
	}

	if len(c.sessions) == 0 {
		return nil, errors.New(noSessionsError)
	}

	return nil, errors.New(noActiveError)
}

func (c *Core) DeleteSession(id string) error {
	c.mu.Lock()

	if _, ok := c.sessions[id]; !ok {
		c.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}

	deleted := c.sessions[id]
	previousActive := c.activeSessionID
	delete(c.sessions, id)
	if c.activeSessionID == id {
		c.activeSessionID = latestSessionIDLocked(c.sessions)
	}

	err := c.saveLocked()
	if err != nil {
		c.sessions[id] = deleted
		c.activeSessionID = previousActive
	}
	c.mu.Unlock()

	if err == nil {
		c.Emit(Event{Type: EventSessionClosed, SessionID: id})
	}
	return err
}

func (c *Core) BeginRequest(id string, userMessage Message) (*Request, *Session, error) {
	c.mu.Lock()
	sess, ok := c.sessions[id]
	if !ok {
		c.mu.Unlock()
		return nil, nil, fmt.Errorf("session %q not found", id)
	}
	if sess.Busy {
		c.mu.Unlock()
		return nil, nil, fmt.Errorf("session %q is already processing another request", id)
	}

	sess.Busy = true
	sess.Messages = AppendMessageWithLimit(sess.Messages, userMessage, c.maxMessages)
	snapshot := CloneSession(sess)
	c.wg.Add(1)
	c.mu.Unlock()

	c.Emit(Event{Type: EventMessageReceived, SessionID: id, Message: CloneMessage(&userMessage)})
	c.Emit(Event{Type: EventBusyChanged, SessionID: id, Busy: true})

	return &Request{core: c, sessionID: id}, snapshot, nil
}

func (r *Request) Complete(update func(*Session) *Message) (*Session, error) {
	r.mu.Lock()
	if r.done {
		r.mu.Unlock()
		return nil, fmt.Errorf("request already completed")
	}
	r.done = true
	r.mu.Unlock()

	defer r.core.wg.Done()

	r.core.mu.Lock()
	current, ok := r.core.sessions[r.sessionID]
	if !ok {
		r.core.mu.Unlock()
		r.core.Emit(Event{Type: EventBusyChanged, SessionID: r.sessionID, Busy: false})
		return nil, fmt.Errorf("session %q disappeared while processing", r.sessionID)
	}

	current.Busy = false
	current.UpdatedAt = time.Now()

	var emitted *Message
	if update != nil {
		emitted = CloneMessage(update(current))
	}

	saveErr := r.core.saveLocked()
	clone := CloneSession(current)
	r.core.mu.Unlock()

	r.core.Emit(Event{Type: EventBusyChanged, SessionID: r.sessionID, Busy: false})
	if emitted != nil {
		r.core.Emit(Event{Type: EventMessageReceived, SessionID: r.sessionID, Message: emitted})
	}

	return clone, saveErr
}

func (c *Core) saveLocked() error {
	if c.statePath == "" {
		return nil
	}

	sessions := make([]*Session, 0, len(c.sessions))
	for _, sess := range c.sessions {
		cloned := CloneSession(sess)
		cloned.Busy = false
		sessions = append(sessions, cloned)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})

	data, err := json.MarshalIndent(persistedState{
		ActiveSessionID: c.activeSessionID,
		Sessions:        sessions,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling chat state: %w", err)
	}

	if err := os.WriteFile(c.statePath, data, 0600); err != nil {
		return fmt.Errorf("writing chat state: %w", err)
	}

	return nil
}
