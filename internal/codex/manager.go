package codex

import (
	"bufio"
	"context"
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

type Manager struct {
	core                     *chat.Core
	mu                       sync.Mutex
	model                    string
	sandbox                  string
	dangerouslyBypassSandbox bool
}

func NewManager(baseFolder, statePath string) *Manager {
	return &Manager{
		core:                     chat.NewCore(baseFolder, statePath, chat.DefaultMaxMessages),
		sandbox:                  defaultSandbox,
		dangerouslyBypassSandbox: defaultDangerouslyBypassSandbox,
	}
}

func (m *Manager) ID() string {
	return "codex"
}

func (m *Manager) Restore() error {
	return m.core.Restore()
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
	return m.core.BaseFolder()
}

// Subscribe registers a callback for codex events.
// fn MUST be non-blocking (push to channel or spawn goroutine).
// Returns an unsubscribe function.
func (m *Manager) Subscribe(fn func(chat.Event)) func() {
	return m.core.Subscribe(fn)
}

func (m *Manager) emit(e chat.Event) {
	m.core.Emit(e)
}

// Shutdown waits for in-flight Send() calls to finish.
func (m *Manager) Shutdown() {
	m.core.Shutdown()
}

func (m *Manager) CreateSession(folder string) (*chat.Session, error) {
	return m.core.CreateSession(folder)
}

func (m *Manager) ListSessions() []*chat.Session {
	return m.core.ListSessions()
}

func (m *Manager) GetSession(id string) (*chat.Session, bool) {
	return m.core.GetSession(id)
}

func (m *Manager) Active() (*chat.Session, bool) {
	return m.core.Active()
}

func (m *Manager) SetActive(id string) (*chat.Session, error) {
	return m.core.SetActive(id)
}

func (m *Manager) DeleteSession(id string) error {
	return m.core.DeleteSession(id)
}

func (m *Manager) ResolveActive() (*chat.Session, error) {
	return m.core.ResolveActive("no Codex session yet; use /new or /folders first", "no active session selected; use /use or /sessions")
}

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

	userMessage := chat.Message{
		Role:        "user",
		Kind:        "text",
		Content:     prompt,
		Timestamp:   time.Now(),
		Attachments: chat.CloneAttachments(attachments),
	}

	request, snapshot, err := m.core.BeginRequest(id, userMessage)
	if err != nil {
		return nil, "", err
	}

	m.mu.Lock()
	sandbox := m.sandbox
	dangerouslyBypassSandbox := m.dangerouslyBypassSandbox
	model := m.model
	m.mu.Unlock()

	threadID, reply, err := runCodexFn(ctx, snapshot, prompt, attachments, sandbox, model, dangerouslyBypassSandbox, StreamCallback{
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

	clone, saveErr := request.Complete(func(current *chat.Session) *chat.Message {
		if threadID != "" {
			current.ThreadID = threadID
		}
		if err != nil || reply == "" {
			return nil
		}
		assistantMessage := &chat.Message{
			Role:      "assistant",
			Kind:      "text",
			Content:   reply,
			Timestamp: time.Now(),
		}
		current.Messages = chat.AppendMessageWithLimit(current.Messages, *assistantMessage, m.core.MaxMessages())
		return assistantMessage
	})

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

	userMessage := chat.Message{
		Role:      "user",
		Kind:      "bash",
		Content:   command,
		Timestamp: time.Now(),
		Command:   &chat.CommandMeta{Command: command},
	}
	request, snapshot, err := m.core.BeginRequest(id, userMessage)
	if err != nil {
		return nil, bashcmd.Result{}, err
	}

	result, err := bashcmd.Run(ctx, snapshot.Folder, command)

	clone, saveErr := request.Complete(func(current *chat.Session) *chat.Message {
		if err != nil {
			return nil
		}
		reply := &chat.Message{
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
		current.Messages = chat.AppendMessageWithLimit(current.Messages, *reply, m.core.MaxMessages())
		return reply
	})

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
	cb StreamCallback,
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

// StreamCallback receives structured events from Codex exec JSONL output.
type StreamCallback struct {
	OnTextDelta  func(delta string)
	OnToolStart  func(index int, id, name string)
	OnToolDelta  func(index int, partialJSON string)
	OnToolFinish func(index int)
}

type execEvent struct {
	Type     string   `json:"type"`
	ThreadID string   `json:"thread_id,omitempty"`
	Item     execItem `json:"item,omitempty"`
}

type execItem struct {
	ID               string `json:"id,omitempty"`
	Type             string `json:"type,omitempty"`
	Text             string `json:"text,omitempty"`
	Command          string `json:"command,omitempty"`
	AggregatedOutput string `json:"aggregated_output,omitempty"`
	Status           string `json:"status,omitempty"`
	Items            []struct {
		Text      string `json:"text,omitempty"`
		Completed bool   `json:"completed,omitempty"`
	} `json:"items,omitempty"`
}

func parseExecOutput(r io.Reader, result *execResult, raw *strings.Builder, cb StreamCallback) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	toolEntries := make(map[string]*toolEntry)
	var nextToolIndex int
	var nextAnonymousTool int
	var anonymousQueue []string
	var agentMessages []string

	resolveToolKey := func(eventType string, item execItem) string {
		if strings.TrimSpace(item.ID) != "" {
			return item.ID
		}

		if eventType == "item.completed" && len(anonymousQueue) > 0 {
			key := anonymousQueue[0]
			anonymousQueue = anonymousQueue[1:]
			return key
		}

		key := fmt.Sprintf("anonymous-%d", nextAnonymousTool)
		nextAnonymousTool++
		if eventType == "item.started" {
			anonymousQueue = append(anonymousQueue, key)
		}
		return key
	}

	ensureTool := func(eventType string, item execItem) *toolEntry {
		key := resolveToolKey(eventType, item)
		if entry, ok := toolEntries[key]; ok {
			return entry
		}

		entry := &toolEntry{index: nextToolIndex}
		nextToolIndex++
		toolEntries[key] = entry
		if cb.OnToolStart != nil {
			cb.OnToolStart(entry.index, item.ID, toolNameForExecItem(item))
		}
		return entry
	}

	emitToolDelta := func(entry *toolEntry, item execItem) {
		if entry == nil || entry.deltaSent || cb.OnToolDelta == nil {
			return
		}
		payload := marshalToolInput(item)
		if payload == "" {
			return
		}
		cb.OnToolDelta(entry.index, payload)
		entry.deltaSent = true
	}

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

		switch event.Type {
		case "item.started":
			if isStreamingToolItem(event.Item) {
				entry := ensureTool(event.Type, event.Item)
				emitToolDelta(entry, event.Item)
			}
		case "item.completed":
			switch event.Item.Type {
			case "agent_message":
				text := strings.TrimSpace(event.Item.Text)
				if text == "" {
					continue
				}
				delta := text
				if len(agentMessages) > 0 {
					delta = "\n\n" + text
				}
				agentMessages = append(agentMessages, text)
				result.Response = strings.Join(agentMessages, "\n\n")
				if cb.OnTextDelta != nil {
					cb.OnTextDelta(delta)
				}
			case "command_execution":
				entry := ensureTool(event.Type, event.Item)
				emitToolDelta(entry, event.Item)
				if cb.OnToolFinish != nil {
					cb.OnToolFinish(entry.index)
				}
			case "todo_list":
				entry := ensureTool(event.Type, event.Item)
				emitToolDelta(entry, event.Item)
				if cb.OnToolFinish != nil {
					cb.OnToolFinish(entry.index)
				}
			}
		}
	}
}

type toolEntry struct {
	index     int
	deltaSent bool
}

func toolNameForExecItem(item execItem) string {
	switch item.Type {
	case "command_execution":
		// Codex currently models shell invocations as command_execution items.
		return "Bash"
	case "todo_list":
		// Normalize Codex todo_list items to the TodoWrite tool semantics used in the dashboard.
		return "TodoWrite"
	case "":
		return "tool"
	default:
		return item.Type
	}
}

func isStreamingToolItem(item execItem) bool {
	switch item.Type {
	case "command_execution", "todo_list":
		return true
	default:
		return false
	}
}

func marshalToolInput(item execItem) string {
	var payload any

	switch item.Type {
	case "command_execution":
		if item.Command == "" {
			return ""
		}
		payload = struct {
			Command string `json:"command"`
		}{
			Command: item.Command,
		}
	case "todo_list":
		if len(item.Items) == 0 {
			return ""
		}
		payload = struct {
			Todos []struct {
				Text      string `json:"text,omitempty"`
				Completed bool   `json:"completed,omitempty"`
			} `json:"todos"`
		}{
			Todos: item.Items,
		}
	default:
		return ""
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
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
