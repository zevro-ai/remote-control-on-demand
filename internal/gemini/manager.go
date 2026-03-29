package gemini

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
	defaultPermissionMode = "auto_edit"
	defaultSystemPATH     = "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin"
)

var runGeminiFn = runGemini

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
	case config.PermissionModeBypass, config.PermissionModeDangerFull, config.PermissionModeGeminiYolo:
		return "yolo"
	case config.PermissionModeReadOnly, config.PermissionModeGeminiPlan:
		return "plan"
	case config.PermissionModeGeminiAutoEdit, config.PermissionModeWorkspace:
		return "auto_edit"
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

func (m *Manager) BaseFolder() string {
	return m.core.BaseFolder()
}

func (m *Manager) Active() (*chat.Session, bool) {
	return m.core.Active()
}

func (m *Manager) SetActive(id string) (*chat.Session, error) {
	return m.core.SetActive(id)
}

func (m *Manager) ResolveActive() (*chat.Session, error) {
	return m.core.ResolveActive("no Gemini session yet; use /new or /folders first", "no active session selected; use /use or /sessions")
}

func (m *Manager) Metadata() provider.Metadata {
	return provider.Metadata{
		ID:          "gemini",
		DisplayName: "Gemini",
		Chat: &provider.ChatCapabilities{
			StreamingDeltas:  true,
			ShellCommandExec: true,
			ThreadResume:     true,
			ImageAttachments: false, // gemini-cli supports images but RCOD doesn't yet handle it for Gemini
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
	if len(attachments) > 0 {
		return nil, "", fmt.Errorf("Gemini chat provider does not support attachments")
	}

	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, "", fmt.Errorf("message cannot be empty")
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

	threadID, reply, err := runGeminiFn(ctx, snapshot, prompt, permissionMode, model, StreamCallback{
		OnTextDelta: func(delta string) {
			m.emit(chat.Event{Type: chat.EventMessageDelta, SessionID: id, Delta: delta})
		},
		OnToolStart: func(index int, toolID, name string, parameters json.RawMessage) {
			m.emit(chat.Event{Type: chat.EventToolUseStart, SessionID: id, ToolCall: &chat.ToolCallEvent{Index: index, ID: toolID, Name: name}})
			if len(parameters) > 0 {
				m.emit(chat.Event{Type: chat.EventToolUseDelta, SessionID: id, ToolCall: &chat.ToolCallEvent{Index: index, PartialJSON: string(parameters)}})
			}
		},
		OnToolFinish: func(index int) {
			m.emit(chat.Event{Type: chat.EventToolUseFinish, SessionID: id, ToolCall: &chat.ToolCallEvent{Index: index}})
		},
	})

	clone, saveErr := request.Complete(func(current *chat.Session) *chat.Message {
		if threadID != "" {
			current.ThreadID = threadID
			current.ThreadReady = true
		}
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

func runGemini(ctx context.Context, sess *chat.Session, prompt, permissionMode, model string, cb StreamCallback) (string, string, error) {
	geminiBin, cmdEnv, err := resolveGeminiCommandEnv()
	if err != nil {
		return "", "", fmt.Errorf("starting gemini: %w", err)
	}

	args := buildGeminiArgs(sess, permissionMode, model)
	cmd := exec.CommandContext(ctx, geminiBin, args...)
	cmd.Dir = sess.Folder
	cmd.Env = cmdEnv
	cmd.Stdin = strings.NewReader(prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("starting gemini: %w", err)
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
		return result.SessionID, strings.TrimSpace(result.Response), fmt.Errorf("gemini command failed: %s", detail)
	}

	reply := strings.TrimSpace(result.Response)
	if reply == "" {
		reply = strings.TrimSpace(stdoutBuf.String())
	}
	if reply == "" {
		return result.SessionID, "", fmt.Errorf("gemini returned an empty response")
	}

	return result.SessionID, reply, nil
}

func buildGeminiArgs(sess *chat.Session, permissionMode, model string) []string {
	args := []string{
		"--output-format", "stream-json",
		"--approval-mode", permissionMode,
	}

	if model != "" {
		args = append(args, "--model", model)
	}

	if sess.ThreadID != "" && sess.ThreadReady {
		args = append(args, "--resume", sess.ThreadID)
	}

	return args
}

type execResult struct {
	SessionID string
	Response  string
}

type streamEnvelope struct {
	Type       string          `json:"type"`
	SessionID  string          `json:"session_id,omitempty"`
	Role       string          `json:"role,omitempty"`
	Content    string          `json:"content,omitempty"`
	Delta      bool            `json:"delta,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolID     string          `json:"tool_id,omitempty"`
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

type StreamCallback struct {
	OnTextDelta  func(delta string)
	OnToolStart  func(index int, toolID, name string, parameters json.RawMessage)
	OnToolFinish func(index int)
}

func parseExecOutput(r io.Reader, result *execResult, raw *strings.Builder, cb StreamCallback) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var partial strings.Builder
	var nextToolIndex int
	toolMap := make(map[string]int)

	for scanner.Scan() {
		line := scanner.Bytes()
		raw.Write(line)
		raw.WriteByte('\n')

		var event streamEnvelope
		if err := json.Unmarshal(line, &event); err == nil {
			switch event.Type {
			case "init":
				if event.SessionID != "" {
					result.SessionID = event.SessionID
				}
			case "message":
				if event.Role == "assistant" && event.Content != "" {
					if event.Delta {
						partial.WriteString(event.Content)
						if cb.OnTextDelta != nil {
							cb.OnTextDelta(event.Content)
						}
					} else {
						if partial.Len() == 0 {
							partial.WriteString(event.Content)
							if cb.OnTextDelta != nil {
								cb.OnTextDelta(event.Content)
							}
						}
					}
				}
			case "tool_use":
				idx := nextToolIndex
				nextToolIndex++
				if event.ToolID != "" {
					toolMap[event.ToolID] = idx
				}
				if cb.OnToolStart != nil {
					cb.OnToolStart(idx, event.ToolID, event.ToolName, event.Parameters)
				}
			case "tool_result":
				if idx, ok := toolMap[event.ToolID]; ok {
					if cb.OnToolFinish != nil {
						cb.OnToolFinish(idx)
					}
					delete(toolMap, event.ToolID)
				}
			}
		}
	}

	// Drain remaining output if scanner failed (e.g. line too long)
	if err := scanner.Err(); err != nil {
		_, _ = io.Copy(io.Discard, r)
	}

	result.Response = partial.String()
}

var (
	geminiBinCache string
	geminiEnvCache []string
	geminiErrCache error
	geminiOnce     sync.Once
)

func resolveGeminiCommandEnv() (string, []string, error) {
	geminiOnce.Do(func() {
		geminiBinCache, geminiErrCache = resolveGeminiBinary()
		if geminiErrCache == nil {
			var cleanEnv []string
			for _, key := range []string{"GEMINI_API_KEY", "USER", "LOGNAME", "HOME", "PATH"} {
				if val := os.Getenv(key); val != "" {
					cleanEnv = append(cleanEnv, key+"="+val)
				}
			}
			env := withPATH(cleanEnv, filepath.Dir(geminiBinCache))
			geminiEnvCache = ensureHome(env)
		}
	})

	if geminiErrCache != nil {
		return "", nil, geminiErrCache
	}
	return geminiBinCache, geminiEnvCache, nil
}

func resolveGeminiBinary() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("GEMINI_BIN")); configured != "" {
		absPath, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolving absolute path for GEMINI_BIN=%q: %w", configured, err)
		}
		path, err := validateExecutable(absPath)
		if err != nil {
			return "", fmt.Errorf("GEMINI_BIN=%q is invalid: %w", configured, err)
		}
		return path, nil
	}

	if path, err := exec.LookPath("gemini"); err == nil {
		return path, nil
	}

	for _, candidate := range geminiCandidatePaths() {
		if path, err := validateExecutable(candidate); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("could not find gemini in PATH or common install locations")
}

func geminiCandidatePaths() []string {
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
		addCandidate(filepath.Join(dir, "gemini"))
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, pattern := range []string{
			filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "gemini"),
			filepath.Join(home, ".volta", "bin", "gemini"),
			filepath.Join(home, ".local", "bin", "gemini"),
		} {
			matches, _ := filepath.Glob(pattern)
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			for _, match := range matches {
				addCandidate(match)
			}
		}
	}

	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
		addCandidate(filepath.Join(dir, "gemini"))
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
