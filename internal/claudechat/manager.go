package claudechat

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/bashcmd"
	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/config"
)

const (
	defaultPermissionMode = "acceptEdits"
	defaultSystemPATH     = "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin"
	maxMessages           = 500
)

var runClaudeFn = runClaude

type state struct {
	ActiveSessionID string          `json:"active_session_id"`
	Sessions        []*chat.Session `json:"sessions"`
}

type Manager struct {
	mu              sync.Mutex
	baseFolder      string
	statePath       string
	sessions        map[string]*chat.Session
	activeSessionID string
	model           string
	permissionMode  string
	subMu           sync.Mutex
	subscribers     []func(chat.Event)
	wg              sync.WaitGroup
}

func NewManager(baseFolder, statePath string) *Manager {
	return &Manager{
		baseFolder:     baseFolder,
		statePath:      statePath,
		sessions:       make(map[string]*chat.Session),
		permissionMode: defaultPermissionMode,
	}
}

func (m *Manager) Restore() error {
	if m.statePath == "" {
		return nil
	}

	data, err := os.ReadFile(m.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading claude state: %w", err)
	}

	var saved state
	if err := json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("parsing claude state: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.activeSessionID = saved.ActiveSessionID
	m.sessions = make(map[string]*chat.Session, len(saved.Sessions))
	for _, sess := range saved.Sessions {
		if sess == nil || sess.ID == "" {
			continue
		}
		sess.Busy = false
		m.sessions[sess.ID] = sess
	}
	if _, ok := m.sessions[m.activeSessionID]; !ok {
		m.activeSessionID = ""
	}

	return nil
}

func (m *Manager) SetModel(model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = strings.TrimSpace(model)
}

func (m *Manager) ConfigurePermissionMode(mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.permissionMode = normalizePermissionMode(mode)
}

func normalizePermissionMode(mode string) string {
	switch config.NormalizeCodexPermissionMode(mode) {
	case config.PermissionModeBypass, config.PermissionModeDangerFull:
		return "bypassPermissions"
	case config.PermissionModeReadOnly:
		return "plan"
	default:
		return defaultPermissionMode
	}
}

func (m *Manager) Subscribe(fn func(chat.Event)) func() {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	m.subscribers = append(m.subscribers, fn)
	idx := len(m.subscribers) - 1
	return func() {
		m.subMu.Lock()
		defer m.subMu.Unlock()
		m.subscribers[idx] = nil
	}
}

func (m *Manager) emit(e chat.Event) {
	m.subMu.Lock()
	subs := make([]func(chat.Event), len(m.subscribers))
	copy(subs, m.subscribers)
	m.subMu.Unlock()
	for _, fn := range subs {
		if fn != nil {
			fn(e)
		}
	}
}

func (m *Manager) Shutdown() {
	m.wg.Wait()
}

func (m *Manager) ID() string {
	return "claude"
}

func (m *Manager) CreateSession(folder string) (*chat.Session, error) {
	fullPath, relName, err := resolveProjectPath(m.baseFolder, folder)
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

	m.mu.Lock()
	id, err := m.generateUniqueIDLocked()
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("generating session ID: %w", err)
	}

	threadID, err := generateUUID()
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("generating thread ID: %w", err)
	}

	now := time.Now()
	sess := &chat.Session{
		ID:        id,
		Folder:    fullPath,
		RelName:   relName,
		ThreadID:  threadID,
		CreatedAt: now,
		UpdatedAt: now,
	}

	m.sessions[id] = sess
	m.activeSessionID = id
	if err := m.saveLocked(); err != nil {
		m.mu.Unlock()
		return nil, err
	}
	clone := cloneSession(sess)
	m.mu.Unlock()

	m.emit(chat.Event{Type: chat.EventSessionCreated, SessionID: id, Session: clone})
	return clone, nil
}

func (m *Manager) ListSessions() []*chat.Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	list := make([]*chat.Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		list = append(list, cloneSession(sess))
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].ID == m.activeSessionID {
			return true
		}
		if list[j].ID == m.activeSessionID {
			return false
		}
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})

	return list
}

func (m *Manager) GetSession(id string) (*chat.Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[id]
	if !ok {
		return nil, false
	}
	return cloneSession(sess), true
}

func (m *Manager) DeleteSession(id string) error {
	m.mu.Lock()

	if _, ok := m.sessions[id]; !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}

	delete(m.sessions, id)
	if m.activeSessionID == id {
		m.activeSessionID = latestSessionIDLocked(m.sessions)
	}

	err := m.saveLocked()
	m.mu.Unlock()

	if err == nil {
		m.emit(chat.Event{Type: chat.EventSessionClosed, SessionID: id})
	}
	return err
}

func latestSessionIDLocked(sessions map[string]*chat.Session) string {
	var latestID string
	var latestTime time.Time

	for sessionID, sess := range sessions {
		if latestID == "" || sess.UpdatedAt.After(latestTime) {
			latestID = sessionID
			latestTime = sess.UpdatedAt
		}
	}

	return latestID
}

func (m *Manager) SendMessage(ctx context.Context, id, prompt string) error {
	_, _, err := m.Send(ctx, id, prompt, nil)
	return err
}

func (m *Manager) RunCommand(ctx context.Context, id, command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("command cannot be empty")
	}

	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	if sess.Busy {
		m.mu.Unlock()
		return fmt.Errorf("session %q is already processing another request", id)
	}

	now := time.Now()
	userMessage := chat.Message{
		Role:      "user",
		Kind:      "bash",
		Content:   command,
		Timestamp: now,
		Command:   &chat.CommandMeta{Command: command},
	}
	sess.Busy = true
	sess.Messages = append(sess.Messages, userMessage)
	if len(sess.Messages) > maxMessages {
		sess.Messages = sess.Messages[len(sess.Messages)-maxMessages:]
	}
	snapshot := cloneSession(sess)
	m.mu.Unlock()

	m.emit(chat.Event{Type: chat.EventMessageReceived, SessionID: id, Message: cloneMessage(&userMessage)})
	m.emit(chat.Event{Type: chat.EventBusyChanged, SessionID: id, Busy: true})
	m.wg.Add(1)
	defer m.wg.Done()

	result, err := bashcmd.Run(ctx, snapshot.Folder, command)

	m.mu.Lock()
	current, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		m.emit(chat.Event{Type: chat.EventBusyChanged, SessionID: id, Busy: false})
		return fmt.Errorf("session %q disappeared while processing", id)
	}

	current.Busy = false
	current.UpdatedAt = time.Now()

	var emitted *chat.Message
	if err == nil {
		reply := chat.Message{
			Role:      "assistant",
			Kind:      "bash_result",
			Content:   result.Output,
			Timestamp: time.Now(),
			Command: &chat.CommandMeta{
				Command:    result.Command,
				ExitCode:   result.ExitCode,
				DurationMs: result.DurationMs,
				TimedOut:   result.TimedOut,
				Truncated:  result.Truncated,
			},
		}
		current.Messages = append(current.Messages, reply)
		if len(current.Messages) > maxMessages {
			current.Messages = current.Messages[len(current.Messages)-maxMessages:]
		}
		emitted = &reply
	}

	saveErr := m.saveLocked()
	m.mu.Unlock()

	m.emit(chat.Event{Type: chat.EventBusyChanged, SessionID: id, Busy: false})
	if emitted != nil {
		m.emit(chat.Event{Type: chat.EventMessageReceived, SessionID: id, Message: emitted})
	}

	if err != nil {
		if saveErr != nil {
			return fmt.Errorf("%w (state save failed: %v)", err, saveErr)
		}
		return err
	}
	return saveErr
}

func (m *Manager) Send(ctx context.Context, id, prompt string, attachments []chat.Attachment) (*chat.Session, string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" && len(attachments) == 0 {
		return nil, "", fmt.Errorf("message cannot be empty")
	}
	if len(attachments) > 0 {
		return nil, "", fmt.Errorf("image attachments are not supported for Claude sessions in the current CLI mode")
	}

	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return nil, "", fmt.Errorf("session %q not found", id)
	}
	if sess.Busy {
		m.mu.Unlock()
		return nil, "", fmt.Errorf("session %q is already processing another message", id)
	}

	now := time.Now()
	userMessage := chat.Message{
		Role:        "user",
		Kind:        "text",
		Content:     prompt,
		Timestamp:   now,
		Attachments: cloneAttachments(attachments),
	}
	sess.Busy = true
	sess.Messages = append(sess.Messages, userMessage)
	if len(sess.Messages) > maxMessages {
		sess.Messages = sess.Messages[len(sess.Messages)-maxMessages:]
	}
	model := m.model
	permissionMode := m.permissionMode
	snapshot := cloneSession(sess)
	m.mu.Unlock()

	m.emit(chat.Event{Type: chat.EventMessageReceived, SessionID: id, Message: cloneMessage(&userMessage)})
	m.emit(chat.Event{Type: chat.EventBusyChanged, SessionID: id, Busy: true})
	m.wg.Add(1)
	defer m.wg.Done()

	reply, err := runClaudeFn(ctx, snapshot, prompt, permissionMode, model, StreamCallback{
		OnTextDelta: func(delta string) {
			m.emit(chat.Event{Type: chat.EventMessageDelta, SessionID: id, Delta: delta})
		},
		OnToolStart: func(index int, toolID, name string) {
			m.emit(chat.Event{Type: chat.EventToolUseStart, SessionID: id, ToolCall: &chat.ToolCallEvent{Index: index, ID: toolID, Name: name}})
		},
		OnToolDelta: func(index int, partialJSON string) {
			m.emit(chat.Event{Type: chat.EventToolUseDelta, SessionID: id, ToolCall: &chat.ToolCallEvent{Index: index, PartialJSON: partialJSON}})
		},
		OnToolFinish: func(index int) {
			m.emit(chat.Event{Type: chat.EventToolUseFinish, SessionID: id, ToolCall: &chat.ToolCallEvent{Index: index}})
		},
	})

	m.mu.Lock()
	current, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		m.emit(chat.Event{Type: chat.EventBusyChanged, SessionID: id, Busy: false})
		return nil, "", fmt.Errorf("session %q disappeared while processing", id)
	}

	current.Busy = false
	current.UpdatedAt = time.Now()
	var emitted *chat.Message
	if err == nil && reply != "" {
		assistantMessage := chat.Message{Role: "assistant", Kind: "text", Content: reply, Timestamp: time.Now()}
		current.Messages = append(current.Messages, assistantMessage)
		if len(current.Messages) > maxMessages {
			current.Messages = current.Messages[len(current.Messages)-maxMessages:]
		}
		emitted = cloneMessage(&assistantMessage)
	}
	saveErr := m.saveLocked()
	clone := cloneSession(current)
	m.mu.Unlock()

	m.emit(chat.Event{Type: chat.EventBusyChanged, SessionID: id, Busy: false})
	if emitted != nil {
		m.emit(chat.Event{Type: chat.EventMessageReceived, SessionID: id, Message: emitted})
	}

	if err != nil {
		if saveErr != nil {
			return nil, "", fmt.Errorf("%w (state save failed: %v)", err, saveErr)
		}
		return nil, "", err
	}
	if saveErr != nil {
		return nil, "", saveErr
	}

	return clone, reply, nil
}

func runClaude(ctx context.Context, sess *chat.Session, prompt, permissionMode, model string, cb StreamCallback) (string, error) {
	claudeBin, cmdEnv, err := resolveClaudeCommandEnv()
	if err != nil {
		return "", fmt.Errorf("starting claude: %w", err)
	}

	args := buildClaudeArgs(sess, prompt, permissionMode, model)
	cmd := exec.CommandContext(ctx, claudeBin, args...)
	cmd.Dir = sess.Folder
	cmd.Env = cmdEnv

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting claude: %w", err)
	}

	var wg sync.WaitGroup
	var result execResult
	var stdoutBuf strings.Builder
	var stderrBuf strings.Builder

	wg.Add(2)
	go func() {
		defer wg.Done()
		parseExecOutput(stdout, &result, &stdoutBuf, cb)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stderrBuf, stderr)
	}()

	waitErr := cmd.Wait()
	wg.Wait()

	if waitErr != nil {
		detail := strings.TrimSpace(stderrBuf.String())
		if detail == "" {
			detail = strings.TrimSpace(stdoutBuf.String())
		}
		if detail == "" {
			detail = waitErr.Error()
		}
		return strings.TrimSpace(result.Response), fmt.Errorf("claude command failed: %s", detail)
	}

	reply := strings.TrimSpace(result.Response)
	if reply == "" {
		reply = strings.TrimSpace(stdoutBuf.String())
	}
	if reply == "" {
		return "", fmt.Errorf("claude returned an empty response")
	}

	return reply, nil
}

func buildClaudeArgs(sess *chat.Session, prompt, permissionMode, model string) []string {
	args := []string{
		"-p",
		"--verbose",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--permission-mode", permissionMode,
		"--append-system-prompt", systemPrompt(sess),
	}

	if model != "" {
		args = append(args, "--model", model)
	}

	if hasAssistantReply(sess) {
		args = append(args, "--resume", sess.ThreadID)
	} else {
		args = append(args, "--session-id", sess.ThreadID)
	}

	args = append(args, prompt)
	return args
}

func hasAssistantReply(sess *chat.Session) bool {
	for _, msg := range sess.Messages {
		if msg.Role == "assistant" && msg.Kind == "text" {
			return true
		}
	}
	return false
}

func systemPrompt(sess *chat.Session) string {
	return fmt.Sprintf(
		"You are Claude helping a developer through the RCOD dashboard.\nAdapt the level of detail to the user's request.\nThis chat session is attached to repository %q.",
		sess.RelName,
	)
}

type execResult struct {
	Response string
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

type streamEnvelope struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	Result    string `json:"result,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Message   struct {
		Content []contentBlock `json:"content"`
	} `json:"message,omitempty"`
	Event struct {
		Type         string       `json:"type"`
		Index        int          `json:"index"`
		ContentBlock contentBlock `json:"content_block,omitempty"`
		Delta        struct {
			Type        string `json:"type"`
			Text        string `json:"text,omitempty"`
			PartialJSON string `json:"partial_json,omitempty"`
		} `json:"delta,omitempty"`
	} `json:"event,omitempty"`
}

// StreamCallback receives structured events from Claude CLI stream-json output.
type StreamCallback struct {
	OnTextDelta  func(delta string)
	OnToolStart  func(index int, id, name string)
	OnToolDelta  func(index int, partialJSON string)
	OnToolFinish func(index int)
}

// toolBlockTracker tracks which content block indices are tool_use blocks.
type toolBlockTracker map[int]bool

func parseExecOutput(r io.Reader, result *execResult, raw *strings.Builder, cb StreamCallback) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var partial strings.Builder
	toolBlocks := make(toolBlockTracker)

	for scanner.Scan() {
		line := scanner.Text()
		raw.WriteString(line)
		raw.WriteByte('\n')

		var event streamEnvelope
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		switch event.Type {
		case "stream_event":
			switch event.Event.Type {
			case "content_block_start":
				if event.Event.ContentBlock.Type == "tool_use" {
					toolBlocks[event.Event.Index] = true
					if cb.OnToolStart != nil {
						cb.OnToolStart(event.Event.Index, event.Event.ContentBlock.ID, event.Event.ContentBlock.Name)
					}
				}
			case "content_block_delta":
				if event.Event.Delta.Type == "text_delta" {
					partial.WriteString(event.Event.Delta.Text)
					if cb.OnTextDelta != nil {
						cb.OnTextDelta(event.Event.Delta.Text)
					}
				} else if event.Event.Delta.Type == "input_json_delta" {
					if cb.OnToolDelta != nil {
						cb.OnToolDelta(event.Event.Index, event.Event.Delta.PartialJSON)
					}
				}
			case "content_block_stop":
				if toolBlocks[event.Event.Index] {
					delete(toolBlocks, event.Event.Index)
					if cb.OnToolFinish != nil {
						cb.OnToolFinish(event.Event.Index)
					}
				}
			}
		case "assistant":
			text := extractText(event.Message.Content)
			if text != "" {
				result.Response = text
			}
		case "result":
			if result.Response == "" && event.Result != "" {
				result.Response = event.Result
			}
		}
	}

	if result.Response == "" {
		result.Response = partial.String()
	}
}

func extractText(content []contentBlock) string {
	var parts []string
	for _, block := range content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (m *Manager) saveLocked() error {
	if m.statePath == "" {
		return nil
	}

	sessions := make([]*chat.Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		copy := cloneSession(sess)
		copy.Busy = false
		sessions = append(sessions, copy)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.Before(sessions[j].CreatedAt)
	})

	data, err := json.MarshalIndent(state{
		ActiveSessionID: m.activeSessionID,
		Sessions:        sessions,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling claude state: %w", err)
	}

	if err := os.WriteFile(m.statePath, data, 0600); err != nil {
		return fmt.Errorf("writing claude state: %w", err)
	}

	return nil
}

func cloneSession(sess *chat.Session) *chat.Session {
	if sess == nil {
		return nil
	}
	c := *sess
	if sess.Messages != nil {
		c.Messages = make([]chat.Message, len(sess.Messages))
		for i, msg := range sess.Messages {
			c.Messages[i] = msg
			c.Messages[i].Attachments = cloneAttachments(msg.Attachments)
			c.Messages[i].Command = cloneCommand(msg.Command)
		}
	}
	return &c
}

func cloneAttachments(attachments []chat.Attachment) []chat.Attachment {
	if attachments == nil {
		return nil
	}
	cloned := make([]chat.Attachment, len(attachments))
	copy(cloned, attachments)
	return cloned
}

func cloneCommand(command *chat.CommandMeta) *chat.CommandMeta {
	if command == nil {
		return nil
	}
	cloned := *command
	return &cloned
}

func cloneMessage(message *chat.Message) *chat.Message {
	if message == nil {
		return nil
	}
	cloned := *message
	cloned.Attachments = cloneAttachments(message.Attachments)
	cloned.Command = cloneCommand(message.Command)
	return &cloned
}

func resolveProjectPath(baseFolder, folder string) (string, string, error) {
	if strings.TrimSpace(folder) == "" {
		return "", "", fmt.Errorf("folder is required")
	}

	baseAbs, err := filepath.Abs(baseFolder)
	if err != nil {
		return "", "", fmt.Errorf("resolving base folder: %w", err)
	}

	targetAbs, err := filepath.Abs(filepath.Join(baseAbs, filepath.Clean(folder)))
	if err != nil {
		return "", "", fmt.Errorf("resolving folder %q: %w", folder, err)
	}

	relPath, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolving folder %q: %w", folder, err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("folder %q must stay within rc.base_folder", folder)
	}

	return targetAbs, relPath, nil
}

func generateID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		b[0:4],
		b[4:6],
		b[6:8],
		b[8:10],
		b[10:16],
	), nil
}

func (m *Manager) generateUniqueIDLocked() (string, error) {
	for i := 0; i < 100; i++ {
		id, err := generateID()
		if err != nil {
			return "", err
		}
		if _, exists := m.sessions[id]; !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique claude session ID after 100 attempts")
}

func resolveClaudeCommandEnv() (string, []string, error) {
	claudeBin, err := resolveClaudeBinary()
	if err != nil {
		return "", nil, err
	}

	env := withPATH(os.Environ(), filepath.Dir(claudeBin))
	env = ensureHome(env)
	return claudeBin, env, nil
}

func resolveClaudeBinary() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("CLAUDE_BIN")); configured != "" {
		path, err := validateExecutable(configured)
		if err != nil {
			return "", fmt.Errorf("CLAUDE_BIN=%q is invalid: %w", configured, err)
		}
		return path, nil
	}

	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}

	for _, candidate := range claudeCandidatePaths() {
		if path, err := validateExecutable(candidate); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("could not find claude in PATH or common install locations")
}

func claudeCandidatePaths() []string {
	var candidates []string
	seen := make(map[string]bool)

	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		candidates = append(candidates, path)
	}

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		addCandidate(filepath.Join(dir, "claude"))
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, pattern := range []string{
			filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "claude"),
			filepath.Join(home, ".volta", "bin", "claude"),
			filepath.Join(home, ".local", "bin", "claude"),
		} {
			matches, _ := filepath.Glob(pattern)
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			for _, match := range matches {
				addCandidate(match)
			}
		}
	}

	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
		addCandidate(filepath.Join(dir, "claude"))
	}

	return candidates
}

func validateExecutable(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory")
	}
	if info.Mode()&0111 == 0 {
		return "", fmt.Errorf("path is not executable")
	}
	return path, nil
}

func withPATH(env []string, preferredDir string) []string {
	pathEntries := []string{preferredDir}
	pathEntries = append(pathEntries, filepath.SplitList(envValue(env, "PATH"))...)
	pathEntries = append(pathEntries, filepath.SplitList(defaultSystemPATH)...)
	return setEnv(env, "PATH", joinUniquePath(pathEntries))
}

func ensureHome(env []string) []string {
	if strings.TrimSpace(envValue(env, "HOME")) != "" {
		return env
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return setEnv(env, "HOME", home)
	}
	return env
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue
		}
		out = append(out, entry)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}

func joinUniquePath(entries []string) string {
	seen := make(map[string]bool)
	unique := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry) == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		unique = append(unique, entry)
	}
	return strings.Join(unique, string(os.PathListSeparator))
}
