package remote

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/zevro-ai/remote-control-on-demand/internal/antigravity"
	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

// AgentRequest and AgentResponse form the small JSON-lines protocol used over
// SSH. Keeping the protocol independent from HTTP means the remote machine
// never needs to expose a listening port.
type AgentRequest struct {
	ID        string `json:"id"`
	Method    string `json:"method"`
	Provider  string `json:"provider,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	Folder    string `json:"folder,omitempty"`
	Message   string `json:"message,omitempty"`
	Command   string `json:"command,omitempty"`
	Model     string `json:"model,omitempty"`
	Reasoning string `json:"reasoning_effort,omitempty"`
}

type AgentResponse struct {
	ID        string              `json:"id"`
	OK        bool                `json:"ok"`
	Error     string              `json:"error,omitempty"`
	Result    json.RawMessage     `json:"result,omitempty"`
	Event     string              `json:"event,omitempty"`
	Provider  string              `json:"provider,omitempty"`
	EventType chat.EventType      `json:"event_type,omitempty"`
	SessionID string              `json:"session_id,omitempty"`
	Delta     string              `json:"delta,omitempty"`
	Busy      *bool               `json:"busy,omitempty"`
	Message   *chat.Message       `json:"message,omitempty"`
	ToolCall  *chat.ToolCallEvent `json:"tool_call,omitempty"`
}

type AgentEvent struct {
	Provider  string
	EventType chat.EventType
	SessionID string
	Delta     string
	Busy      *bool
	Message   *chat.Message
	ToolCall  *chat.ToolCallEvent
}

type AgentClient struct {
	executor   *Executor
	workingDir string
	command    []string

	mu        sync.Mutex
	transport *Transport
	decoder   *json.Decoder
	encoder   *json.Encoder
	nextID    uint64
	eventSubs map[int]func(AgentEvent)
	nextEvent int
}

func NewAgentClient(executor *Executor, workingDir, command string) (*AgentClient, error) {
	return NewAgentClientArgs(executor, workingDir, strings.Fields(strings.TrimSpace(command)))
}

func NewAgentClientArgs(executor *Executor, workingDir string, command []string) (*AgentClient, error) {
	if executor == nil {
		return nil, errors.New("SSH executor is required")
	}
	parts := append([]string(nil), command...)
	if len(parts) == 0 {
		parts = []string{"rcod-agent"}
	}
	hasStdio := false
	for _, part := range parts[1:] {
		if part == "--stdio" {
			hasStdio = true
		}
	}
	if !hasStdio {
		parts = append(parts, "--stdio")
	}
	return &AgentClient{executor: executor, workingDir: workingDir, command: parts, eventSubs: make(map[int]func(AgentEvent))}, nil
}

func (c *AgentClient) SubscribeEvents(fn func(AgentEvent)) func() {
	if fn == nil {
		return func() {}
	}
	c.mu.Lock()
	id := c.nextEvent
	c.nextEvent++
	c.eventSubs[id] = fn
	c.mu.Unlock()
	return func() {
		c.mu.Lock()
		delete(c.eventSubs, id)
		c.mu.Unlock()
	}
}

func (c *AgentClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.transport == nil {
		return nil
	}
	err := c.transport.Close()
	c.transport = nil
	c.decoder = nil
	c.encoder = nil
	return err
}

func (c *AgentClient) call(ctx context.Context, request AgentRequest, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.transport == nil {
		transport, err := c.executor.Open(ctx, ExecRequest{WorkingDirectory: c.workingDir, Command: c.command})
		if err != nil {
			return err
		}
		c.transport = transport
		c.decoder = json.NewDecoder(bufio.NewReader(transport.Stdout()))
		c.encoder = json.NewEncoder(transport)
		go func(stderr io.Reader) { _, _ = io.Copy(io.Discard, stderr) }(transport.Stderr())
	}
	request.ID = fmt.Sprintf("%d", atomic.AddUint64(&c.nextID, 1))
	if err := c.encoder.Encode(request); err != nil {
		_ = c.transport.Close()
		c.transport = nil
		return fmt.Errorf("sending remote request: %w", err)
	}
	type decodeResult struct {
		response AgentResponse
		err      error
	}
	decodeNext := func() chan decodeResult {
		ch := make(chan decodeResult, 1)
		decoder := c.decoder
		go func() {
			var response AgentResponse
			err := decoder.Decode(&response)
			ch <- decodeResult{response: response, err: err}
		}()
		return ch
	}
	responseCh := decodeNext()
	for {
		var response AgentResponse
		select {
		case result := <-responseCh:
			response = result.response
			if result.err != nil {
				_ = c.transport.Close()
				c.transport = nil
				c.decoder = nil
				c.encoder = nil
				return fmt.Errorf("reading remote response: %w", result.err)
			}
		case <-ctx.Done():
			_ = c.transport.Close()
			c.transport = nil
			c.decoder = nil
			c.encoder = nil
			return ctx.Err()
		}
		if response.Event != "" {
			c.emitEventLocked(AgentEvent{Provider: response.Provider, EventType: response.EventType, SessionID: response.SessionID, Delta: response.Delta, Busy: response.Busy, Message: response.Message, ToolCall: response.ToolCall})
			responseCh = decodeNext()
			continue
		}
		if response.ID != request.ID {
			return fmt.Errorf("remote response ID %q did not match request %q", response.ID, request.ID)
		}
		if !response.OK {
			return errors.New(firstNonEmpty(response.Error, "remote agent request failed"))
		}
		if result == nil || len(response.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decoding remote response: %w", err)
		}
		return nil
	}
}

func (c *AgentClient) emitEventLocked(event AgentEvent) {
	subs := make([]func(AgentEvent), 0, len(c.eventSubs))
	for _, fn := range c.eventSubs {
		subs = append(subs, fn)
	}
	for _, fn := range subs {
		fn(event)
	}
}

func (c *AgentClient) Ping(ctx context.Context) error {
	return c.call(ctx, AgentRequest{Method: "ping"}, nil)
}

func (c *AgentClient) ListSessions(ctx context.Context, providerID string) ([]*chat.Session, error) {
	var sessions []*chat.Session
	err := c.call(ctx, AgentRequest{Method: "list_sessions", Provider: providerID}, &sessions)
	return sessions, err
}

func (c *AgentClient) CreateSession(ctx context.Context, providerID, folder string, options chat.SessionOptions) (*chat.Session, error) {
	var sess chat.Session
	err := c.call(ctx, AgentRequest{Method: "create_session", Provider: providerID, Folder: folder, Model: options.Model, Reasoning: options.Reasoning}, &sess)
	return &sess, err
}

func (c *AgentClient) SetSessionOptions(ctx context.Context, providerID, sessionID string, options chat.SessionOptions) (*chat.Session, error) {
	var sess chat.Session
	err := c.call(ctx, AgentRequest{Method: "set_session_options", Provider: providerID, SessionID: sessionID, Model: options.Model, Reasoning: options.Reasoning}, &sess)
	return &sess, err
}

func (c *AgentClient) SendMessage(ctx context.Context, providerID, sessionID, message string) (*chat.Session, error) {
	var sess chat.Session
	err := c.call(ctx, AgentRequest{Method: "send_message", Provider: providerID, SessionID: sessionID, Message: message}, &sess)
	return &sess, err
}

func (c *AgentClient) RunCommand(ctx context.Context, providerID, sessionID, command string) (*chat.Session, error) {
	var sess chat.Session
	err := c.call(ctx, AgentRequest{Method: "run_command", Provider: providerID, SessionID: sessionID, Command: command}, &sess)
	return &sess, err
}

func (c *AgentClient) DeleteSession(ctx context.Context, providerID, sessionID string) error {
	return c.call(ctx, AgentRequest{Method: "delete_session", Provider: providerID, SessionID: sessionID}, nil)
}

func (c *AgentClient) AdoptSession(ctx context.Context, providerID, threadID string) (*chat.Session, error) {
	var sess chat.Session
	err := c.call(ctx, AgentRequest{Method: "adopt_session", Provider: providerID, ThreadID: threadID}, &sess)
	return &sess, err
}

func (c *AgentClient) ListAdoptableSessions(ctx context.Context, providerID string) ([]provider.AdoptableSession, error) {
	var sessions []provider.AdoptableSession
	err := c.call(ctx, AgentRequest{Method: "list_adoptable", Provider: providerID}, &sessions)
	return sessions, err
}

func (c *AgentClient) ListModels(ctx context.Context, providerID string) ([]provider.Model, error) {
	var models []provider.Model
	err := c.call(ctx, AgentRequest{Method: "list_models", Provider: providerID}, &models)
	return models, err
}

func (c *AgentClient) ListFolders(ctx context.Context, providerID string) ([]string, error) {
	var folders []string
	err := c.call(ctx, AgentRequest{Method: "list_folders", Provider: providerID}, &folders)
	return folders, err
}

func (c *AgentClient) ListHistory(ctx context.Context, providerID string) ([]provider.HistorySession, error) {
	var history []provider.HistorySession
	err := c.call(ctx, AgentRequest{Method: "list_history", Provider: providerID}, &history)
	return history, err
}

func (c *AgentClient) GetHistory(ctx context.Context, providerID, threadID string) ([]chat.Message, error) {
	var messages []chat.Message
	err := c.call(ctx, AgentRequest{Method: "get_history", Provider: providerID, ThreadID: threadID}, &messages)
	return messages, err
}

// AgentServer serves the protocol on stdin/stdout. It is used by the small
// rcod-agent binary on the target machine.
type AgentServer struct {
	Codex       *codex.Manager
	Antigravity *antigravity.Manager
}

func (s *AgentServer) Serve(ctx context.Context, input io.Reader, output io.Writer) error {
	decoder := json.NewDecoder(bufio.NewReader(input))
	encoder := json.NewEncoder(output)
	var writeMu sync.Mutex
	write := func(response AgentResponse) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return encoder.Encode(response)
	}
	var unsubscribers []func()
	if s.Codex != nil {
		unsubscribers = append(unsubscribers, s.Codex.Subscribe(func(event chat.Event) {
			_ = write(agentEventResponse("codex", event))
		}))
	}
	if s.Antigravity != nil {
		unsubscribers = append(unsubscribers, s.Antigravity.Subscribe(func(event chat.Event) {
			_ = write(agentEventResponse("antigravity", event))
		}))
	}
	defer func() {
		for _, unsubscribe := range unsubscribers {
			unsubscribe()
		}
	}()
	var requests sync.WaitGroup
	for {
		var request AgentRequest
		if err := decoder.Decode(&request); err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				requests.Wait()
				return nil
			}
			requests.Wait()
			return err
		}
		requests.Add(1)
		go func(request AgentRequest) {
			defer requests.Done()
			_ = write(s.dispatch(ctx, request))
		}(request)
	}
}

func agentEventResponse(providerID string, event chat.Event) AgentResponse {
	busy := event.Busy
	return AgentResponse{
		OK: true, Event: "chat", Provider: providerID, EventType: event.Type,
		SessionID: event.SessionID, Delta: event.Delta, Message: event.Message,
		Busy: &busy, ToolCall: event.ToolCall,
	}
}

func (s *AgentServer) dispatch(ctx context.Context, request AgentRequest) AgentResponse {
	response := AgentResponse{ID: request.ID, OK: true}
	if request.Method == "ping" {
		return response
	}
	manager, err := s.manager(request.Provider)
	if err != nil {
		return failedResponse(request.ID, err)
	}

	var result any
	switch request.Method {
	case "list_sessions":
		result = manager.ListSessions()
	case "create_session":
		result, err = createRemoteSession(manager, request.Folder, chat.SessionOptions{Model: request.Model, Reasoning: request.Reasoning})
	case "set_session_options":
		configurator, ok := manager.(provider.SessionConfigurator)
		if !ok {
			err = errors.New("provider does not support session options")
		} else {
			result, err = configurator.SetSessionOptions(request.SessionID, chat.SessionOptions{Model: request.Model, Reasoning: request.Reasoning})
		}
	case "send_message":
		result, _, err = manager.Send(ctx, request.SessionID, request.Message, nil)
	case "run_command":
		err = manager.RunCommand(ctx, request.SessionID, request.Command)
		if err == nil {
			result, _ = manager.GetSession(request.SessionID)
		}
	case "delete_session":
		err = manager.DeleteSession(request.SessionID)
	case "adopt_session":
		adopter, ok := manager.(provider.SessionAdopter)
		if !ok {
			err = errors.New("provider does not support adoption")
		} else {
			result, err = adopter.AdoptSession(request.ThreadID)
		}
	case "list_adoptable":
		adopter, ok := manager.(provider.SessionAdopter)
		if !ok {
			err = errors.New("provider does not support adoption")
		} else {
			result, err = adopter.ListAdoptableSessions()
		}
	case "list_models":
		lister, ok := manager.(provider.ModelLister)
		if !ok {
			err = errors.New("provider does not expose models")
		} else {
			result, err = lister.ListModels()
		}
	case "list_folders":
		baseFolderProvider, ok := manager.(interface{ BaseFolder() string })
		if !ok {
			err = errors.New("provider does not expose a base folder")
		} else {
			result, err = listRemoteFolders(baseFolderProvider.BaseFolder())
		}
	case "list_history":
		historian, ok := manager.(provider.SessionHistorian)
		if !ok {
			err = errors.New("provider does not expose history")
		} else {
			result, err = historian.ListHistory()
		}
	case "get_history":
		historian, ok := manager.(provider.SessionHistorian)
		if !ok {
			err = errors.New("provider does not expose history")
		} else {
			result, err = historian.GetHistory(request.ThreadID)
		}
	default:
		err = fmt.Errorf("unknown agent method %q", request.Method)
	}
	if err != nil {
		return failedResponse(request.ID, err)
	}
	if result != nil {
		response.Result, err = json.Marshal(result)
		if err != nil {
			return failedResponse(request.ID, err)
		}
	}
	return response
}

func listRemoteFolders(baseFolder string) ([]string, error) {
	entries, err := os.ReadDir(baseFolder)
	if err != nil {
		return nil, fmt.Errorf("reading remote base folder: %w", err)
	}
	folders := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == ".git" || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(baseFolder, entry.Name())
		if info, err := os.Stat(filepath.Join(path, ".git")); err == nil && info != nil {
			folders = append(folders, entry.Name())
		}
	}
	sort.Strings(folders)
	return folders, nil
}

type remoteChatManager interface {
	ListSessions() []*chat.Session
	GetSession(string) (*chat.Session, bool)
	DeleteSession(string) error
	RunCommand(context.Context, string, string) error
	Send(context.Context, string, string, []chat.Attachment) (*chat.Session, string, error)
}

func createRemoteSession(manager remoteChatManager, folder string, options chat.SessionOptions) (*chat.Session, error) {
	if creator, ok := manager.(provider.SessionOptionCreator); ok {
		return creator.CreateSessionWithOptions(folder, options)
	}
	return nil, errors.New("provider does not support session options")
}

func (s *AgentServer) manager(id string) (remoteChatManager, error) {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "codex":
		if s.Codex != nil {
			return s.Codex, nil
		}
	case "antigravity":
		if s.Antigravity != nil {
			return s.Antigravity, nil
		}
	}
	return nil, fmt.Errorf("remote provider %q is not configured", id)
}

func failedResponse(id string, err error) AgentResponse {
	return AgentResponse{ID: id, Error: err.Error()}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
