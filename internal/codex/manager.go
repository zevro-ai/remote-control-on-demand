package codex

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
	defaultSandbox                  = "workspace-write"
	defaultDangerouslyBypassSandbox = false
	defaultSystemPATH               = "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin"
)

var runCodexFn = runCodex

type state struct {
	ActiveSessionID string          `json:"active_session_id"`
	Sessions        []*chat.Session `json:"sessions"`
}

type Manager struct {
	mu                       sync.Mutex
	baseFolder               string
	statePath                string
	sessions                 map[string]*chat.Session
	activeSessionID          string
	model                    string
	sandbox                  string
	dangerouslyBypassSandbox bool
	subMu                    sync.Mutex
	subscribers              []func(chat.Event)
	wg                       sync.WaitGroup
}

func NewManager(baseFolder, statePath string) *Manager {
	return &Manager{
		baseFolder:               baseFolder,
		statePath:                statePath,
		sessions:                 make(map[string]*chat.Session),
		sandbox:                  defaultSandbox,
		dangerouslyBypassSandbox: defaultDangerouslyBypassSandbox,
	}
}

func (m *Manager) ID() string {
	return "codex"
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
		return fmt.Errorf("reading codex state: %w", err)
	}

	var saved state
	if err := json.Unmarshal(data, &saved); err != nil {
		return fmt.Errorf("parsing codex state: %w", err)
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

func (m *Manager) SetSandbox(sandbox string) {
	sandbox = strings.TrimSpace(sandbox)
	if sandbox == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sandbox = sandbox
}

func (m *Manager) SetDangerouslyBypassSandbox(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dangerouslyBypassSandbox = enabled
}

func (m *Manager) ConfigurePermissionMode(permissionMode string) {
	permissionMode = config.NormalizeCodexPermissionMode(permissionMode)
	if permissionMode == config.PermissionModeBypass {
		m.SetDangerouslyBypassSandbox(true)
		m.SetSandbox(defaultSandbox)
		return
	}

	m.SetDangerouslyBypassSandbox(false)
	m.SetSandbox(permissionMode)
}

func (m *Manager) BaseFolder() string {
	return m.baseFolder
}

// Subscribe registers a callback for codex events.
// fn MUST be non-blocking (push to channel or spawn goroutine).
// Returns an unsubscribe function.
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

// Shutdown waits for in-flight Send() calls to finish.
func (m *Manager) Shutdown() {
	m.wg.Wait()
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
	id, err := m.generateUniqueCodexID()
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

func (m *Manager) Active() (*chat.Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeSessionID == "" {
		return nil, false
	}
	sess, ok := m.sessions[m.activeSessionID]
	if !ok {
		return nil, false
	}
	return cloneSession(sess), true
}

func (m *Manager) SetActive(id string) (*chat.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %q not found", id)
	}

	m.activeSessionID = id
	sess.UpdatedAt = time.Now()
	if err := m.saveLocked(); err != nil {
		return nil, err
	}

	return cloneSession(sess), nil
}

func (m *Manager) DeleteSession(id string) error {
	m.mu.Lock()

	if _, ok := m.sessions[id]; !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}

	delete(m.sessions, id)
	if m.activeSessionID == id {
		m.activeSessionID = latestCodexSessionIDLocked(m.sessions)
	}

	err := m.saveLocked()
	m.mu.Unlock()

	if err == nil {
		m.emit(chat.Event{Type: chat.EventSessionClosed, SessionID: id})
	}
	return err
}

func latestCodexSessionIDLocked(sessions map[string]*chat.Session) string {
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

func (m *Manager) ResolveActive() (*chat.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.activeSessionID != "" {
		sess, ok := m.sessions[m.activeSessionID]
		if ok {
			return cloneSession(sess), nil
		}
		m.activeSessionID = ""
	}

	if len(m.sessions) == 1 {
		for id, sess := range m.sessions {
			m.activeSessionID = id
			_ = m.saveLocked()
			return cloneSession(sess), nil
		}
	}

	if len(m.sessions) == 0 {
		return nil, fmt.Errorf("no Codex session yet; use /new or /folders first")
	}

	return nil, fmt.Errorf("no active session selected; use /use or /sessions")
}

const maxMessages = 500

func (m *Manager) SendMessage(ctx context.Context, id, prompt string, attachments []chat.Attachment) error {
	_, _, err := m.Send(ctx, id, prompt, attachments)
	return err
}

func (m *Manager) RunCommand(ctx context.Context, id, command string) error {
	_, _, err := m.Run(ctx, id, command)
	return err
}

func (m *Manager) Send(ctx context.Context, id, prompt string, attachments []chat.Attachment) (*chat.Session, string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" && len(attachments) == 0 {
		return nil, "", fmt.Errorf("message cannot be empty")
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
	sandbox := m.sandbox
	dangerouslyBypassSandbox := m.dangerouslyBypassSandbox
	model := m.model
	snapshot := cloneSession(sess)
	m.mu.Unlock()

	m.emit(chat.Event{Type: chat.EventMessageReceived, SessionID: id, Message: cloneMessage(&userMessage)})
	m.emit(chat.Event{Type: chat.EventBusyChanged, SessionID: id, Busy: true})
	m.wg.Add(1)
	defer m.wg.Done()

	threadID, reply, err := runCodexFn(ctx, snapshot, prompt, attachments, sandbox, model, dangerouslyBypassSandbox)

	m.mu.Lock()
	current, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		m.emit(chat.Event{Type: chat.EventBusyChanged, SessionID: id, Busy: false})
		return nil, "", fmt.Errorf("session %q disappeared while processing", id)
	}

	current.Busy = false
	current.UpdatedAt = time.Now()
	if threadID != "" {
		current.ThreadID = threadID
	}
	var emitted *chat.Message
	if err == nil && reply != "" {
		assistantMessage := chat.Message{
			Role:      "assistant",
			Kind:      "text",
			Content:   reply,
			Timestamp: time.Now(),
		}
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

func (m *Manager) Run(ctx context.Context, id, command string) (*chat.Session, bashcmd.Result, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, bashcmd.Result{}, fmt.Errorf("command cannot be empty")
	}

	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return nil, bashcmd.Result{}, fmt.Errorf("session %q not found", id)
	}
	if sess.Busy {
		m.mu.Unlock()
		return nil, bashcmd.Result{}, fmt.Errorf("session %q is already processing another request", id)
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
		return nil, bashcmd.Result{}, fmt.Errorf("session %q disappeared while processing", id)
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
	clone := cloneSession(current)
	m.mu.Unlock()

	m.emit(chat.Event{Type: chat.EventBusyChanged, SessionID: id, Busy: false})
	if emitted != nil {
		m.emit(chat.Event{Type: chat.EventMessageReceived, SessionID: id, Message: emitted})
	}

	if err != nil {
		if saveErr != nil {
			return nil, bashcmd.Result{}, fmt.Errorf("%w (state save failed: %v)", err, saveErr)
		}
		return nil, bashcmd.Result{}, err
	}
	if saveErr != nil {
		return nil, bashcmd.Result{}, saveErr
	}

	return clone, result, nil
}

func runCodex(
	ctx context.Context,
	sess *chat.Session,
	prompt string,
	attachments []chat.Attachment,
	sandbox,
	model string,
	dangerouslyBypassSandbox bool,
) (string, string, error) {
	codexBin, cmdEnv, err := resolveCodexBinaryEnv()
	if err != nil {
		return "", "", fmt.Errorf("starting codex: %w", err)
	}

	args := buildCodexArgs(sess, prompt, attachments, sandbox, model, dangerouslyBypassSandbox)
	cmd := exec.CommandContext(ctx, codexBin, args...)
	cmd.Dir = sess.Folder
	cmd.Env = cmdEnv

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("starting codex: %w", err)
	}

	var wg sync.WaitGroup
	var result execResult
	var stdoutBuf strings.Builder
	var stderrBuf strings.Builder

	wg.Add(2)
	go func() {
		defer wg.Done()
		parseExecOutput(stdout, &result, &stdoutBuf)
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
		return result.ThreadID, strings.TrimSpace(result.Response), fmt.Errorf("codex command failed: %s", detail)
	}

	reply := strings.TrimSpace(result.Response)
	if reply == "" {
		reply = strings.TrimSpace(stdoutBuf.String())
	}
	if reply == "" {
		return result.ThreadID, "", fmt.Errorf("codex returned an empty response")
	}

	return result.ThreadID, reply, nil
}

func buildCodexArgs(
	sess *chat.Session,
	prompt string,
	attachments []chat.Attachment,
	sandbox,
	model string,
	dangerouslyBypassSandbox bool,
) []string {
	args := []string{"exec"}
	if dangerouslyBypassSandbox {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}

	if sess.ThreadID == "" {
		args = append(args, "--json")
		if !dangerouslyBypassSandbox {
			args = append(args, "--sandbox", sandbox)
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		// For `codex exec`, `--image` is variadic, so the prompt must come first.
		args = append(args, initialPrompt(sess, prompt))
		args = appendImageArgs(args, attachments)
		return args
	}

	args = append(args, "resume", "--json")
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, sess.ThreadID, prompt)
	args = appendImageArgs(args, attachments)
	return args
}

func appendImageArgs(args []string, attachments []chat.Attachment) []string {
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.Path) == "" {
			continue
		}
		args = append(args, "--image", attachment.Path)
	}
	return args
}

func initialPrompt(sess *chat.Session, prompt string) string {
	return fmt.Sprintf(
		"You are Codex talking to the user through Telegram.\nKeep replies concise unless the user asks for depth.\nThis chat session is attached to repository %q.\n\nUser message:\n%s",
		sess.RelName,
		prompt,
	)
}

type execResult struct {
	ThreadID string
	Response string
}

type execEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id,omitempty"`
	Item     struct {
		Type string `json:"type,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"item,omitempty"`
}

func parseExecOutput(r io.Reader, result *execResult, raw *strings.Builder) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		raw.WriteString(line)
		raw.WriteByte('\n')

		var event execEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.ThreadID != "" {
			result.ThreadID = event.ThreadID
		}
		if event.Type == "item.completed" && event.Item.Type == "agent_message" && event.Item.Text != "" {
			result.Response = event.Item.Text
		}
	}
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
		return fmt.Errorf("marshaling codex state: %w", err)
	}

	if err := os.WriteFile(m.statePath, data, 0600); err != nil {
		return fmt.Errorf("writing codex state: %w", err)
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

// generateUniqueCodexID generates a codex session ID that does not collide with existing sessions.
// Must be called with m.mu held.
func (m *Manager) generateUniqueCodexID() (string, error) {
	for i := 0; i < 100; i++ {
		id, err := generateID()
		if err != nil {
			return "", err
		}
		if _, exists := m.sessions[id]; !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique codex session ID after 100 attempts")
}

func resolveCodexBinaryEnv() (string, []string, error) {
	codexBin, err := resolveCodexBinary()
	if err != nil {
		return "", nil, err
	}

	env := withPATH(os.Environ(), filepath.Dir(codexBin))
	env = ensureHome(env)
	return codexBin, env, nil
}

func resolveCodexBinary() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("CODEX_BIN")); configured != "" {
		path, err := validateExecutable(configured)
		if err != nil {
			return "", fmt.Errorf("CODEX_BIN=%q is invalid: %w", configured, err)
		}
		return path, nil
	}

	if path, err := exec.LookPath("codex"); err == nil {
		return path, nil
	}

	for _, candidate := range codexCandidatePaths() {
		if path, err := validateExecutable(candidate); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("could not find codex in PATH or common install locations")
}

func codexCandidatePaths() []string {
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
		addCandidate(filepath.Join(dir, "codex"))
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, pattern := range []string{
			filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "codex"),
			filepath.Join(home, ".volta", "bin", "codex"),
			filepath.Join(home, ".local", "bin", "codex"),
		} {
			matches, _ := filepath.Glob(pattern)
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			for _, match := range matches {
				addCandidate(match)
			}
		}
	}

	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
		addCandidate(filepath.Join(dir, "codex"))
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

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return env
	}
	return setEnv(env, "HOME", home)
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
	replaced := false
	next := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			if !replaced {
				next = append(next, prefix+value)
				replaced = true
			}
			continue
		}
		next = append(next, entry)
	}
	if !replaced {
		next = append(next, prefix+value)
	}
	return next
}

func joinUniquePath(entries []string) string {
	seen := make(map[string]bool)
	unique := make([]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" || seen[entry] {
			continue
		}
		seen[entry] = true
		unique = append(unique, entry)
	}
	return strings.Join(unique, string(os.PathListSeparator))
}
