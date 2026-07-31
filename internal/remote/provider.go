package remote

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

// ProxyProvider presents one provider on one SSH host as a normal RCOD chat
// provider. The dashboard can therefore use its existing sessions/messages
// UI while the actual work remains on the target machine.
type ProxyProvider struct {
	client     *AgentClient
	providerID string
	metadata   provider.Metadata

	mu       sync.Mutex
	sessions map[string]*chat.Session
	subs     map[int]func(chat.Event)
	nextSub  int
}

func NewProxyProvider(hostID, hostName, providerID string, client *AgentClient) (*ProxyProvider, error) {
	if client == nil {
		return nil, fmt.Errorf("agent client is required")
	}
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	if providerID != "codex" && providerID != "antigravity" {
		return nil, fmt.Errorf("unsupported remote provider %q", providerID)
	}
	displayHost := firstNonEmpty(hostName, hostID)
	displayProvider := strings.ToUpper(providerID[:1]) + providerID[1:]
	proxy := &ProxyProvider{
		client:     client,
		providerID: providerID,
		metadata: provider.Metadata{
			ID:          fmt.Sprintf("ssh-%s-%s", hostID, providerID),
			DisplayName: fmt.Sprintf("%s / %s", displayHost, displayProvider),
			Chat: &provider.ChatCapabilities{
				StreamingDeltas:       true,
				ToolCallStreaming:     true,
				ShellCommandExec:      true,
				ThreadResume:          true,
				AdoptExistingSessions: true,
				History:               true,
			},
		},
		sessions: make(map[string]*chat.Session),
		subs:     make(map[int]func(chat.Event)),
	}
	client.SubscribeEvents(func(event AgentEvent) {
		if event.Provider != providerID || event.SessionID == "" {
			return
		}
		proxy.emit(chat.Event{
			Type: event.EventType, SessionID: event.SessionID, Delta: event.Delta,
			Message: event.Message, Busy: valueOrFalse(event.Busy), ToolCall: event.ToolCall,
		})
	})
	return proxy, nil
}

func (p *ProxyProvider) Metadata() provider.Metadata { return p.metadata }

func (p *ProxyProvider) ListSessions() []*chat.Session {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	sessions, err := p.client.ListSessions(ctx, p.providerID)
	if err != nil {
		return p.cachedSessions()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sessions = make(map[string]*chat.Session, len(sessions))
	for _, sess := range sessions {
		if sess != nil {
			p.sessions[sess.ID] = sess
		}
	}
	return cloneSessions(sessions)
}

func (p *ProxyProvider) GetSession(id string) (*chat.Session, bool) {
	p.mu.Lock()
	sess, ok := p.sessions[id]
	p.mu.Unlock()
	if ok {
		return cloneSession(sess), true
	}
	for _, candidate := range p.ListSessions() {
		if candidate != nil && candidate.ID == id {
			return candidate, true
		}
	}
	return nil, false
}

func (p *ProxyProvider) CreateSession(folder string) (*chat.Session, error) {
	return p.CreateSessionWithOptions(folder, chat.SessionOptions{})
}

func (p *ProxyProvider) CreateSessionWithOptions(folder string, options chat.SessionOptions) (*chat.Session, error) {
	sess, err := p.client.CreateSession(context.Background(), p.providerID, folder, options)
	if err != nil {
		return nil, err
	}
	p.cache(sess)
	p.emit(chat.Event{Type: chat.EventSessionCreated, SessionID: sess.ID, Session: sess})
	return cloneSession(sess), nil
}

func (p *ProxyProvider) SetSessionOptions(id string, options chat.SessionOptions) (*chat.Session, error) {
	sess, err := p.client.SetSessionOptions(context.Background(), p.providerID, id, options)
	if err != nil {
		return nil, err
	}
	p.cache(sess)
	return cloneSession(sess), nil
}

func (p *ProxyProvider) DeleteSession(id string) error {
	if err := p.client.DeleteSession(context.Background(), p.providerID, id); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.sessions, id)
	p.mu.Unlock()
	p.emit(chat.Event{Type: chat.EventSessionClosed, SessionID: id})
	return nil
}

func (p *ProxyProvider) SendMessage(ctx context.Context, id, message string, attachments []chat.Attachment) error {
	if len(attachments) > 0 {
		return fmt.Errorf("remote %s does not support attachments", p.providerID)
	}
	sess, err := p.client.SendMessage(ctx, p.providerID, id, message)
	if err != nil {
		return err
	}
	p.cache(sess)
	p.emitLatest(sess)
	return nil
}

func (p *ProxyProvider) RunCommand(ctx context.Context, id, command string) error {
	sess, err := p.client.RunCommand(ctx, p.providerID, id, command)
	if err != nil {
		return err
	}
	p.cache(sess)
	p.emitLatest(sess)
	return nil
}

func (p *ProxyProvider) Subscribe(fn func(chat.Event)) func() {
	if fn == nil {
		return func() {}
	}
	p.mu.Lock()
	id := p.nextSub
	p.nextSub++
	p.subs[id] = fn
	p.mu.Unlock()
	return func() {
		p.mu.Lock()
		delete(p.subs, id)
		p.mu.Unlock()
	}
}

func (p *ProxyProvider) ListAdoptableSessions() ([]provider.AdoptableSession, error) {
	return p.client.ListAdoptableSessions(context.Background(), p.providerID)
}

func (p *ProxyProvider) AdoptSession(threadID string) (*chat.Session, error) {
	sess, err := p.client.AdoptSession(context.Background(), p.providerID, threadID)
	if err != nil {
		return nil, err
	}
	p.cache(sess)
	return cloneSession(sess), nil
}

func (p *ProxyProvider) ListModels() ([]provider.Model, error) {
	return p.client.ListModels(context.Background(), p.providerID)
}

func (p *ProxyProvider) ListFolders() ([]string, error) {
	return p.client.ListFolders(context.Background(), p.providerID)
}

func (p *ProxyProvider) ListHistory() ([]provider.HistorySession, error) {
	return p.client.ListHistory(context.Background(), p.providerID)
}

func (p *ProxyProvider) GetHistory(threadID string) ([]chat.Message, error) {
	return p.client.GetHistory(context.Background(), p.providerID, threadID)
}

func (p *ProxyProvider) cache(sess *chat.Session) {
	if sess == nil {
		return
	}
	p.mu.Lock()
	p.sessions[sess.ID] = cloneSession(sess)
	p.mu.Unlock()
}

func (p *ProxyProvider) cachedSessions() []*chat.Session {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]*chat.Session, 0, len(p.sessions))
	for _, sess := range p.sessions {
		result = append(result, cloneSession(sess))
	}
	return result
}

func (p *ProxyProvider) emitLatest(sess *chat.Session) {
	if sess == nil {
		return
	}
	var latest *chat.Message
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		if sess.Messages[i].Role == "assistant" {
			message := sess.Messages[i]
			latest = &message
			break
		}
	}
	p.emit(chat.Event{Type: chat.EventBusyChanged, SessionID: sess.ID, Busy: false})
	if latest != nil {
		p.emit(chat.Event{Type: chat.EventMessageReceived, SessionID: sess.ID, Message: latest})
	}
}

func (p *ProxyProvider) emit(event chat.Event) {
	p.mu.Lock()
	subs := make([]func(chat.Event), 0, len(p.subs))
	for _, fn := range p.subs {
		subs = append(subs, fn)
	}
	p.mu.Unlock()
	for _, fn := range subs {
		fn(event)
	}
}

func valueOrFalse(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func cloneSessions(sessions []*chat.Session) []*chat.Session {
	result := make([]*chat.Session, 0, len(sessions))
	for _, sess := range sessions {
		if sess != nil {
			result = append(result, cloneSession(sess))
		}
	}
	return result
}

func cloneSession(sess *chat.Session) *chat.Session {
	if sess == nil {
		return nil
	}
	clone := *sess
	clone.Messages = append([]chat.Message(nil), sess.Messages...)
	return &clone
}
