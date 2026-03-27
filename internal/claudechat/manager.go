package claudechat

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
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

const (
	defaultPermissionMode = "acceptEdits"
	defaultSystemPATH     = "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin"
)

var runClaudeFn = runClaude

type Manager struct {
	core           *chat.Core
	mu             sync.Mutex
	model          string
	permissionMode string
}

func NewManager(baseFolder, statePath string) *Manager {
	return &Manager{
		core:           chat.NewCore(baseFolder, statePath, chat.DefaultMaxMessages),
		permissionMode: defaultPermissionMode,
	}
}

func (m *Manager) Restore() error {
	return m.core.Restore()
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
	return m.core.Subscribe(fn)
}

func (m *Manager) emit(e chat.Event) {
	m.core.Emit(e)
}

func (m *Manager) Shutdown() {
	m.core.Shutdown()
}

func (m *Manager) Metadata() provider.Metadata {
	return provider.Metadata{
		ID:          "claude",
		DisplayName: "Claude",
		Chat: &provider.ChatCapabilities{
			StreamingDeltas:  true,
			ShellCommandExec: true,
			ThreadResume:     true,
			ImageAttachments: true,
		},
	}
}

func (m *Manager) ID() string {
	return m.Metadata().ID
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

func (m *Manager) DeleteSession(id string) error {
	return m.core.DeleteSession(id)
}

func (m *Manager) SendMessage(ctx context.Context, id, prompt string, attachments []chat.Attachment) error {
	_, _, err := m.Send(ctx, id, prompt, attachments)
	return err
}

func (m *Manager) RunCommand(ctx context.Context, id, command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return fmt.Errorf("command cannot be empty")
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
		return err
	}

	result, err := bashcmd.Run(ctx, snapshot.Folder, command)

	_, saveErr := request.Complete(func(current *chat.Session) *chat.Message {
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

	userMessage := chat.Message{
		Role:        "user",
		Kind:        "text",
		Content:     prompt,
		Timestamp:   time.Now(),
		Attachments: chat.CloneAttachments(attachments),
	}

	m.mu.Lock()
	model := m.model
	permissionMode := m.permissionMode
	m.mu.Unlock()

	request, snapshot, err := m.core.BeginRequest(id, userMessage)
	if err != nil {
		return nil, "", err
	}

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

	clone, saveErr := request.Complete(func(current *chat.Session) *chat.Message {
		if err != nil || reply == "" {
			return nil
		}
		assistantMessage := &chat.Message{Role: "assistant", Kind: "text", Content: reply, Timestamp: time.Now()}
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
