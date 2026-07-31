// Package antigravity integrates RCOD with the headless Antigravity CLI (agy).
package antigravity

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/zevro-ai/remote-control-on-demand/internal/bashcmd"
	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

const (
	defaultPermissionMode = "accept-edits"
	defaultSystemPATH     = "/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:/usr/local/bin"
)

var runAntigravityFn = runAntigravity

// Manager owns the RCOD-local state for Antigravity conversations. The actual
// conversation history remains owned by agy and is resumed by conversation ID.
type Manager struct {
	core            *chat.Core
	mu              sync.Mutex
	model           string
	reasoningEffort string
	permissionMode  string
}

func NewManager(baseFolder, statePath string) *Manager {
	return &Manager{
		core:           chat.NewCore(baseFolder, statePath, chat.DefaultMaxMessages),
		permissionMode: defaultPermissionMode,
	}
}

func (m *Manager) ID() string { return m.Metadata().ID }

func (m *Manager) Metadata() provider.Metadata {
	return provider.Metadata{
		ID:          "antigravity",
		DisplayName: "Antigravity",
		Chat: &provider.ChatCapabilities{
			StreamingDeltas:       true,
			ToolCallStreaming:     true,
			ShellCommandExec:      true,
			ThreadResume:          true,
			AdoptExistingSessions: true,
			ImageAttachments:      false,
			History:               true,
		},
	}
}

func (m *Manager) Restore() error { return m.core.Restore() }

func (m *Manager) Shutdown() { m.core.Shutdown() }

func (m *Manager) BaseFolder() string { return m.core.BaseFolder() }

func (m *Manager) Subscribe(fn func(chat.Event)) func() { return m.core.Subscribe(fn) }

func (m *Manager) SetModel(model string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.model = strings.TrimSpace(model)
}

func (m *Manager) SetDefaultModel(model string) { m.SetModel(model) }

func (m *Manager) SetReasoningEffort(effort string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reasoningEffort = strings.TrimSpace(effort)
}

// SetSessionModel overrides the default model for one RCOD session.
func (m *Manager) SetSessionModel(id, model string) (*chat.Session, error) {
	sess, ok := m.core.GetSession(id)
	if !ok {
		return nil, fmt.Errorf("session %q not found", id)
	}
	return m.SetSessionOptions(id, chat.SessionOptions{Model: strings.TrimSpace(model), Reasoning: sess.Reasoning})
}

func (m *Manager) ConfigurePermissionMode(mode string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.permissionMode = normalizePermissionMode(mode)
}

func normalizePermissionMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "bypass", "bypasspermissions", "dangerfull", "danger-full-access", "yolo", "dangerously-skip-permissions":
		return "bypass"
	case "plan", "readonly", "read-only":
		return "plan"
	case "accept-edits", "acceptedits", "auto_edit", "auto-edit", "workspace", "":
		return defaultPermissionMode
	default:
		return defaultPermissionMode
	}
}

func (m *Manager) CreateSession(folder string) (*chat.Session, error) {
	m.mu.Lock()
	options := chat.SessionOptions{Model: m.model, Reasoning: m.reasoningEffort}
	m.mu.Unlock()
	return m.core.CreateSessionWithOptions(folder, "", false, options)
}

func (m *Manager) CreateSessionWithOptions(folder string, options chat.SessionOptions) (*chat.Session, error) {
	options.Model = strings.TrimSpace(options.Model)
	options.Reasoning = strings.TrimSpace(options.Reasoning)
	return m.core.CreateSessionWithOptions(folder, "", false, options)
}

func (m *Manager) SetSessionOptions(id string, options chat.SessionOptions) (*chat.Session, error) {
	options.Model = strings.TrimSpace(options.Model)
	options.Reasoning = strings.TrimSpace(options.Reasoning)
	return m.core.SetSessionOptions(id, options)
}

func (m *Manager) ListModels() ([]provider.Model, error) {
	bin, env, err := resolveAGYCommandEnv()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "models")
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("listing Antigravity models: %w: %s", err, strings.TrimSpace(string(output)))
	}
	m.mu.Lock()
	defaultModel := m.model
	m.mu.Unlock()
	return parseModelOutput(output, defaultModel), nil
}

func parseModelOutput(output []byte, defaultModel string) []provider.Model {
	type modelJSON struct {
		Slug             string   `json:"slug"`
		Name             string   `json:"name"`
		DisplayName      string   `json:"display_name"`
		Description      string   `json:"description"`
		ReasoningLevels  []string `json:"reasoning_levels"`
		DefaultReasoning string   `json:"default_reasoning"`
	}
	var jsonModels []modelJSON
	if json.Unmarshal(output, &jsonModels) != nil {
		var wrapped struct {
			Models []modelJSON `json:"models"`
		}
		if json.Unmarshal(output, &wrapped) == nil {
			jsonModels = wrapped.Models
		}
	}
	models := make([]provider.Model, 0, len(jsonModels))
	seen := make(map[string]struct{})
	for _, item := range jsonModels {
		slug := strings.TrimSpace(item.Slug)
		if slug == "" {
			slug = strings.TrimSpace(item.Name)
		}
		if slug == "" {
			continue
		}
		models = append(models, provider.Model{Slug: slug, DisplayName: firstNonEmpty(item.DisplayName, item.Name, slug), Description: strings.TrimSpace(item.Description), ReasoningLevels: item.ReasoningLevels, DefaultReasoning: strings.TrimSpace(item.DefaultReasoning)})
		seen[slug] = struct{}{}
	}
	if len(models) == 0 {
		scanner := bufio.NewScanner(strings.NewReader(string(output)))
		for scanner.Scan() {
			slug := strings.TrimSpace(scanner.Text())
			if slug == "" || strings.HasPrefix(slug, "Usage:") || strings.HasPrefix(slug, "Available ") {
				continue
			}
			if _, ok := seen[slug]; ok {
				continue
			}
			seen[slug] = struct{}{}
			models = append(models, provider.Model{Slug: slug, DisplayName: slug})
		}
	}
	defaultModel = strings.TrimSpace(defaultModel)
	if defaultModel != "" {
		if _, ok := seen[defaultModel]; !ok {
			models = append(models, provider.Model{Slug: defaultModel, DisplayName: defaultModel})
		}
	}
	sort.SliceStable(models, func(i, j int) bool {
		if models[i].Slug == defaultModel {
			return true
		}
		if models[j].Slug == defaultModel {
			return false
		}
		return strings.ToLower(models[i].DisplayName) < strings.ToLower(models[j].DisplayName)
	})
	return models
}

func (m *Manager) ListSessions() []*chat.Session { return m.core.ListSessions() }

func (m *Manager) GetSession(id string) (*chat.Session, bool) { return m.core.GetSession(id) }

func (m *Manager) Active() (*chat.Session, bool) { return m.core.Active() }

func (m *Manager) SetActive(id string) (*chat.Session, error) { return m.core.SetActive(id) }

func (m *Manager) ResolveActive() (*chat.Session, error) {
	return m.core.ResolveActive("no Antigravity session yet; use /new or /folders first", "no active session selected; use /use or /sessions")
}

func (m *Manager) DeleteSession(id string) error { return m.core.DeleteSession(id) }

func (m *Manager) SendMessage(ctx context.Context, id, message string, attachments []chat.Attachment) error {
	_, _, err := m.Send(ctx, id, message, attachments)
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
		return nil, "", fmt.Errorf("Antigravity chat provider does not support attachments")
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil, "", fmt.Errorf("message cannot be empty")
	}

	userMessage := chat.Message{Role: "user", Kind: "text", Content: prompt, Timestamp: time.Now()}
	m.mu.Lock()
	model, reasoning, permissionMode := m.model, m.reasoningEffort, m.permissionMode
	m.mu.Unlock()

	request, snapshot, err := m.core.BeginRequest(id, userMessage)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(snapshot.Model) != "" {
		model = snapshot.Model
	}
	if strings.TrimSpace(snapshot.Reasoning) != "" {
		reasoning = snapshot.Reasoning
	}
	snapshot.Reasoning = reasoning

	conversationID, reply, err := runAntigravityFn(ctx, snapshot, prompt, permissionMode, model, StreamCallback{
		OnTextDelta: func(delta string) {
			m.core.Emit(chat.Event{Type: chat.EventMessageDelta, SessionID: id, Delta: delta})
		},
		OnToolStart: func(index int, toolID, name string, parameters json.RawMessage) {
			m.core.Emit(chat.Event{Type: chat.EventToolUseStart, SessionID: id, ToolCall: &chat.ToolCallEvent{Index: index, ID: toolID, Name: name}})
			if len(parameters) > 0 {
				m.core.Emit(chat.Event{Type: chat.EventToolUseDelta, SessionID: id, ToolCall: &chat.ToolCallEvent{Index: index, PartialJSON: string(parameters)}})
			}
		},
		OnToolDelta: func(index int, partialJSON string) {
			m.core.Emit(chat.Event{Type: chat.EventToolUseDelta, SessionID: id, ToolCall: &chat.ToolCallEvent{Index: index, PartialJSON: partialJSON}})
		},
		OnToolFinish: func(index int) {
			m.core.Emit(chat.Event{Type: chat.EventToolUseFinish, SessionID: id, ToolCall: &chat.ToolCallEvent{Index: index}})
		},
	})

	clone, saveErr := request.Complete(func(current *chat.Session) *chat.Message {
		if conversationID != "" {
			current.ThreadID = conversationID
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

func runAntigravity(ctx context.Context, sess *chat.Session, prompt, permissionMode, model string, cb StreamCallback) (string, string, error) {
	agyBin, cmdEnv, err := resolveAGYCommandEnv()
	if err != nil {
		return "", "", fmt.Errorf("starting agy: %w", err)
	}

	cmd := exec.CommandContext(ctx, agyBin, buildAGYArgs(sess, prompt, permissionMode, model)...)
	cmd.Dir = sess.Folder
	cmd.Env = cmdEnv
	// Keep prompts out of the process list. agy accepts the prompt from stdin in
	// print mode, and this also avoids shell quoting issues for multiline input.
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
		return "", "", fmt.Errorf("starting agy: %w", err)
	}

	var wg sync.WaitGroup
	var result execResult
	var stdoutBuf, stderrBuf strings.Builder
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
		return result.ConversationID, strings.TrimSpace(result.Response), fmt.Errorf("agy command failed: %s", detail)
	}

	reply := strings.TrimSpace(result.Response)
	if reply == "" {
		reply = strings.TrimSpace(stdoutBuf.String())
	}
	if reply == "" {
		return result.ConversationID, "", fmt.Errorf("agy returned an empty response")
	}
	return result.ConversationID, reply, nil
}

func buildAGYArgs(sess *chat.Session, _ string, permissionMode, model string) []string {
	args := []string{"--print", "--output-format", "stream-json"}
	if normalizePermissionMode(permissionMode) == "bypass" {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		args = append(args, "--mode", normalizePermissionMode(permissionMode))
	}
	if strings.TrimSpace(model) != "" {
		args = append(args, "--model", strings.TrimSpace(model))
	}
	if sess != nil && strings.TrimSpace(sess.Reasoning) != "" {
		args = append(args, "--effort", strings.TrimSpace(sess.Reasoning))
	}
	if sess != nil && sess.ThreadReady && strings.TrimSpace(sess.ThreadID) != "" {
		args = append(args, "--conversation", strings.TrimSpace(sess.ThreadID))
	}
	return args
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type execResult struct {
	ConversationID string
	Response       string
}

type StreamCallback struct {
	OnTextDelta  func(delta string)
	OnToolStart  func(index int, toolID, name string, parameters json.RawMessage)
	OnToolDelta  func(index int, partialJSON string)
	OnToolFinish func(index int)
}

// parseExecOutput accepts agy's documented stream-json shape as well as the
// simpler JSON/text shapes emitted by older CLI builds. Unknown JSON events are
// retained in raw so a future CLI change cannot silently lose diagnostics.
func parseExecOutput(r io.Reader, result *execResult, raw *strings.Builder, cb StreamCallback) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var streamed, final string
	var nextToolIndex int
	toolMap := make(map[string]int)
	parsedJSON := false

	for scanner.Scan() {
		line := scanner.Text()
		raw.WriteString(line)
		raw.WriteByte('\n')
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		parsedJSON = true
		if id := conversationIDFromEvent(event); id != "" {
			result.ConversationID = id
		}

		typeName := strings.ToLower(stringValue(event["type"]))
		if isToolStart(typeName, event) {
			id := firstString(event, "tool_id", "tool_call_id", "call_id", "id")
			name := firstString(event, "tool_name", "name")
			idx := intValue(event["index"])
			if idx < 0 {
				idx = nextToolIndex
			}
			if idx >= nextToolIndex {
				nextToolIndex = idx + 1
			}
			if id != "" {
				toolMap[id] = idx
			}
			params := rawValue(event, "parameters", "input", "arguments")
			if cb.OnToolStart != nil {
				cb.OnToolStart(idx, id, name, params)
			}
		}
		if isToolFinish(typeName, event) {
			id := firstString(event, "tool_id", "tool_call_id", "call_id", "id")
			if idx, ok := toolMap[id]; ok {
				if cb.OnToolFinish != nil {
					cb.OnToolFinish(idx)
				}
				delete(toolMap, id)
			}
		}
		if idx, partial := toolDelta(event); partial != "" && cb.OnToolDelta != nil {
			cb.OnToolDelta(idx, partial)
		}

		text, delta, ok := assistantText(event, typeName)
		if !ok || text == "" {
			continue
		}
		if delta {
			streamed += text
			if cb.OnTextDelta != nil {
				cb.OnTextDelta(text)
			}
		} else {
			final = text
		}
	}
	if !parsedJSON {
		// Some agy versions emit one pretty-printed JSON document instead of
		// JSONL. Try the complete raw document before treating it as text.
		var document map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(raw.String())), &document); err == nil {
			parsedJSON = true
			if id := conversationIDFromEvent(document); id != "" {
				result.ConversationID = id
			}
			if text, _, ok := assistantText(document, strings.ToLower(stringValue(document["type"]))); ok {
				final = text
			}
		}
	}

	if streamed != "" {
		result.Response = streamed
	} else if final != "" {
		result.Response = final
	} else if !parsedJSON {
		result.Response = strings.TrimSpace(raw.String())
	}
}

func conversationIDFromEvent(event map[string]any) string {
	if id := firstString(event, "conversation_id", "conversationId", "session_id", "sessionId", "thread_id", "threadId"); id != "" {
		return id
	}
	typeName := strings.ToLower(stringValue(event["type"]))
	if typeName == "init" || typeName == "session" || typeName == "conversation" {
		return firstString(event, "id")
	}
	return ""
}

func assistantText(event map[string]any, typeName string) (string, bool, bool) {
	role := strings.ToLower(firstString(event, "role", "author"))
	if role != "" && role != "assistant" && role != "model" {
		return "", false, false
	}
	if role == "" && (typeName == "user" || typeName == "tool_result" || typeName == "error") {
		return "", false, false
	}

	delta := boolValue(event["delta"]) || strings.Contains(typeName, "delta") || typeName == "chunk"
	var text string
	for _, key := range []string{"text", "content", "response", "result", "output"} {
		if value, ok := event[key]; ok {
			text = textValue(value)
			if text != "" {
				break
			}
		}
	}
	if text == "" {
		if nested, ok := event["message"].(map[string]any); ok {
			text = textValue(nested["content"])
			delta = delta || boolValue(nested["delta"])
		}
	}
	if text == "" {
		if nested, ok := event["delta"].(map[string]any); ok {
			text = textValue(nested["text"])
			if text == "" {
				text = textValue(nested["content"])
			}
			delta = delta || strings.Contains(strings.ToLower(stringValue(nested["type"])), "delta")
		}
	}
	if text == "" {
		return "", false, false
	}
	return text, delta, true
}

func textValue(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case []any:
		var parts []string
		for _, item := range value {
			if part := textValue(item); part != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		for _, key := range []string{"text", "value", "content"} {
			if part := textValue(value[key]); part != "" {
				return part
			}
		}
	}
	return ""
}

func isToolStart(typeName string, event map[string]any) bool {
	return typeName == "tool_use" || typeName == "tool_call" || typeName == "function_call" || typeName == "content_block_start" && strings.EqualFold(firstStringFromMap(event, "content_block", "type"), "tool_use")
}

func isToolFinish(typeName string, event map[string]any) bool {
	return typeName == "tool_result" || typeName == "tool_call_result" || typeName == "function_result" || typeName == "content_block_stop"
}

func toolDelta(event map[string]any) (int, string) {
	idx := intValue(event["index"])
	for _, key := range []string{"partial_json", "partialJSON"} {
		if value := stringValue(event[key]); value != "" {
			return idx, value
		}
	}
	if nested, ok := event["delta"].(map[string]any); ok {
		if value := firstString(nested, "partial_json", "partialJSON", "input"); value != "" {
			return idx, value
		}
	}
	return idx, ""
}

func firstString(event map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(event[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstStringFromMap(event map[string]any, parent, key string) string {
	nested, _ := event[parent].(map[string]any)
	return stringValue(nested[key])
}

func rawValue(event map[string]any, keys ...string) json.RawMessage {
	for _, key := range keys {
		if value, ok := event[key]; ok && value != nil {
			encoded, err := json.Marshal(value)
			if err == nil {
				return encoded
			}
		}
	}
	return nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func intValue(value any) int {
	switch value := value.(type) {
	case float64:
		return int(value)
	case int:
		return value
	}
	return -1
}

func resolveAGYCommandEnv() (string, []string, error) {
	bin, err := resolveAGYBinary()
	if err != nil {
		return "", nil, err
	}
	allowed := []string{
		"HOME", "USER", "LOGNAME", "PATH", "LANG", "LC_ALL", "LC_CTYPE", "TERM", "NO_COLOR",
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
		"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "ANTIGRAVITY_HOME", "AGY_HOME",
	}
	env := make([]string, 0, len(allowed)+1)
	for _, key := range allowed {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	env = withPATH(env, filepath.Dir(bin))
	if strings.TrimSpace(envValue(env, "HOME")) == "" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			env = setEnv(env, "HOME", home)
		}
	}
	return bin, env, nil
}

func resolveAGYBinary() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("AGY_BIN")); configured != "" {
		path, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolving AGY_BIN=%q: %w", configured, err)
		}
		if _, err := validateExecutable(path); err != nil {
			return "", fmt.Errorf("AGY_BIN=%q is invalid: %w", configured, err)
		}
		return path, nil
	}
	if path, err := exec.LookPath("agy"); err == nil {
		return path, nil
	}
	for _, candidate := range agyCandidatePaths() {
		if path, err := validateExecutable(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("could not find agy in PATH or common install locations")
}

func agyCandidatePaths() []string {
	var candidates []string
	seen := make(map[string]bool)
	add := func(path string) {
		if path != "" && !seen[path] {
			seen[path] = true
			candidates = append(candidates, path)
		}
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		add(filepath.Join(dir, "agy"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".local", "bin", "agy"))
		add(filepath.Join(home, ".volta", "bin", "agy"))
		matches, _ := filepath.Glob(filepath.Join(home, ".nvm", "versions", "node", "*", "bin", "agy"))
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		for _, match := range matches {
			add(match)
		}
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin"} {
		add(filepath.Join(dir, "agy"))
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
	entries := []string{preferredDir}
	entries = append(entries, filepath.SplitList(envValue(env, "PATH"))...)
	entries = append(entries, filepath.SplitList(defaultSystemPATH)...)
	return setEnv(env, "PATH", joinUniquePath(entries))
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

type conversationMetadata struct {
	ID           string
	CWD          string
	Model        string
	Title        string
	Preview      string
	MessageCount int
	UpdatedAt    time.Time
}

func (m *Manager) ListAdoptableSessions() ([]provider.AdoptableSession, error) {
	home, err := resolveAntigravityHome()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, "conversations")
	paths, err := filepath.Glob(filepath.Join(dir, "*.db"))
	if err != nil {
		return nil, fmt.Errorf("locating Antigravity conversations: %w", err)
	}
	existing := make(map[string]struct{})
	for _, sess := range m.core.ListSessions() {
		if sess != nil && strings.TrimSpace(sess.ThreadID) != "" {
			existing[sess.ThreadID] = struct{}{}
		}
	}
	var result []provider.AdoptableSession
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if _, ok := existing[id]; ok {
			continue
		}
		metadata, err := readConversationMetadata(path, id)
		if err != nil {
			continue
		}
		_, relName, relCWD, err := resolveRepoForConversation(m.core.BaseFolder(), metadata.CWD)
		if err != nil {
			continue
		}
		result = append(result, provider.AdoptableSession{ThreadID: id, RelName: relName, RelCWD: relCWD, Title: metadata.Title, Model: metadata.Model, UpdatedAt: metadata.UpdatedAt})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].UpdatedAt.After(result[j].UpdatedAt) })
	return result, nil
}

// ListHistory returns the conversations that Antigravity has persisted for a
// workspace. Antigravity keeps the authoritative transcript in its own
// conversation database; RCOD can still show and resume the workspace-level
// index without copying that private database into its state file.
func (m *Manager) ListHistory() ([]provider.HistorySession, error) {
	home, err := resolveAntigravityHome()
	if err != nil {
		return nil, err
	}
	paths, err := filepath.Glob(filepath.Join(home, "conversations", "*.db"))
	if err != nil {
		return nil, fmt.Errorf("locating Antigravity history: %w", err)
	}
	history := make([]provider.HistorySession, 0, len(paths))
	for _, path := range paths {
		id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		metadata, err := readConversationMetadata(path, id)
		if err != nil {
			continue
		}
		_, relName, relCWD, err := resolveRepoForConversation(m.core.BaseFolder(), metadata.CWD)
		if err != nil {
			continue
		}
		preview := ""
		if sess, ok := m.findSessionByThread(id); ok {
			preview = lastMessagePreview(sess.Messages)
		}
		history = append(history, provider.HistorySession{
			ThreadID: id, RelName: relName, RelCWD: relCWD, Title: metadata.Title,
			Model: metadata.Model, Preview: firstNonEmpty(metadata.Preview, preview), UpdatedAt: metadata.UpdatedAt,
			MessageCount: metadata.MessageCount,
		})
	}
	sort.Slice(history, func(i, j int) bool { return history[i].UpdatedAt.After(history[j].UpdatedAt) })
	return history, nil
}

func (m *Manager) GetHistory(threadID string) ([]chat.Message, error) {
	if sess, ok := m.findSessionByThread(strings.TrimSpace(threadID)); ok {
		return append([]chat.Message(nil), sess.Messages...), nil
	}
	return nil, fmt.Errorf("Antigravity transcript %q is owned by agy; adopt it before loading messages", strings.TrimSpace(threadID))
}

func (m *Manager) findSessionByThread(threadID string) (*chat.Session, bool) {
	for _, sess := range m.core.ListSessions() {
		if sess != nil && sess.ThreadID == threadID {
			return sess, true
		}
	}
	return nil, false
}

func lastMessagePreview(messages []chat.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if text := strings.TrimSpace(messages[i].Content); text != "" {
			if len(text) > 180 {
				return text[:180] + "..."
			}
			return text
		}
	}
	return ""
}

func (m *Manager) AdoptSession(threadID string) (*chat.Session, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, fmt.Errorf("conversation ID is required")
	}
	for _, sess := range m.core.ListSessions() {
		if sess != nil && sess.ThreadID == threadID {
			return nil, fmt.Errorf("conversation %q is already adopted by session %q", threadID, sess.ID)
		}
	}
	adoptable, err := m.ListAdoptableSessions()
	if err != nil {
		return nil, err
	}
	for _, item := range adoptable {
		if item.ThreadID != threadID {
			continue
		}
		folder := item.RelName
		if item.RelCWD != "" {
			folder = filepath.Join(folder, item.RelCWD)
		}
		return m.core.CreateSessionWithOptions(folder, threadID, true, chat.SessionOptions{Model: item.Model})
	}
	return nil, fmt.Errorf("adoptable Antigravity conversation %q not found", threadID)
}

func resolveAntigravityHome() (string, error) {
	for _, key := range []string{"ANTIGRAVITY_HOME", "AGY_HOME"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return filepath.Clean(value), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".gemini", "antigravity-cli"), nil
}

func readConversationMetadata(path, id string) (conversationMetadata, error) {
	info, err := os.Stat(path)
	if err != nil {
		return conversationMetadata{}, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	if err != nil {
		return conversationMetadata{}, fmt.Errorf("opening Antigravity conversation: %w", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT data FROM trajectory_metadata_blob WHERE data IS NOT NULL`)
	if err != nil {
		return conversationMetadata{}, fmt.Errorf("reading Antigravity conversation metadata: %w", err)
	}
	defer rows.Close()
	var stringsFound []string
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return conversationMetadata{}, err
		}
		stringsFound = append(stringsFound, printableStrings(blob)...)
	}
	if err := rows.Err(); err != nil {
		return conversationMetadata{}, err
	}
	metadata := conversationMetadata{ID: id, Title: "Antigravity conversation " + shortID(id), UpdatedAt: info.ModTime()}
	if indexed, ok := readIndexedConversation(id); ok {
		metadata.CWD = fileURLPath(indexFirstString(indexed.WorkspaceURIs...))
		metadata.Title = firstNonEmpty(indexed.Title, indexed.Preview, metadata.Title)
		metadata.Preview = indexed.Preview
		metadata.MessageCount = indexed.NumSteps
		if updated := firstNonEmpty(indexed.UpdatedAt, indexed.LastModifiedTime); updated != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, updated); err == nil {
				metadata.UpdatedAt = parsed
			}
		}
	}
	for _, value := range stringsFound {
		if metadata.CWD == "" {
			metadata.CWD = fileURLPath(value)
		}
		if metadata.Model == "" && looksLikeModel(value) {
			metadata.Model = value
		}
	}
	if metadata.CWD == "" {
		return conversationMetadata{}, errors.New("conversation metadata has no workspace path")
	}
	return metadata, nil
}

type indexedConversation struct {
	Title            string
	Preview          string
	NumSteps         int
	UpdatedAt        string
	WorkspaceURIs    []string
	LastModifiedTime string
}

func readIndexedConversation(id string) (indexedConversation, bool) {
	home, err := resolveAntigravityHome()
	if err != nil {
		return indexedConversation{}, false
	}
	data, err := os.ReadFile(filepath.Join(home, "cache", "conversation_metadata.json"))
	if err != nil {
		return indexedConversation{}, false
	}
	var index struct {
		Conversations map[string]struct {
			Summary struct {
				Title         string   `json:"Title"`
				Preview       string   `json:"Preview"`
				NumSteps      int      `json:"NumSteps"`
				UpdatedAt     string   `json:"UpdatedAt"`
				WorkspaceURIs []string `json:"WorkspaceURIs"`
			} `json:"summary"`
			LastModifiedTime string `json:"last_modified_time"`
		} `json:"conversations"`
	}
	if json.Unmarshal(data, &index) != nil {
		return indexedConversation{}, false
	}
	entry, ok := index.Conversations[id]
	if !ok {
		return indexedConversation{}, false
	}
	return indexedConversation{
		Title: entry.Summary.Title, Preview: entry.Summary.Preview, NumSteps: entry.Summary.NumSteps,
		UpdatedAt: entry.Summary.UpdatedAt, WorkspaceURIs: entry.Summary.WorkspaceURIs,
		LastModifiedTime: entry.LastModifiedTime,
	}, true
}

func indexFirstString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var printableStringRE = regexp.MustCompile(`[ -~]{4,}`)

func printableStrings(blob []byte) []string {
	matches := printableStringRE.FindAll(blob, -1)
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		result = append(result, string(match))
	}
	return result
}

func fileURLPath(value string) string {
	idx := strings.Index(value, "file://")
	if idx < 0 {
		return ""
	}
	parsed, err := url.Parse(value[idx:])
	if err != nil || parsed.Scheme != "file" {
		return ""
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return ""
	}
	return filepath.Clean(path)
}

func looksLikeModel(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if strings.Contains(value, " ") || strings.Contains(value, "/") || strings.Contains(value, "file:") {
		return false
	}
	for _, prefix := range []string{"gemini-", "claude-", "gpt-", "o1", "o3", "o4"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func resolveRepoForConversation(baseFolder, cwd string) (string, string, string, error) {
	if strings.TrimSpace(baseFolder) == "" || strings.TrimSpace(cwd) == "" {
		return "", "", "", errors.New("base folder and conversation workspace are required")
	}
	base, err := filepath.EvalSymlinks(baseFolder)
	if err != nil {
		return "", "", "", fmt.Errorf("resolving base folder: %w", err)
	}
	current, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		return "", "", "", fmt.Errorf("resolving conversation workspace: %w", err)
	}
	workspace := current
	var repo string
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			repo = current
			break
		}
		if current == base {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if repo == "" {
		return "", "", "", errors.New("conversation workspace is not inside a git repository")
	}
	relRepo, err := filepath.Rel(base, repo)
	if err != nil || relRepo == ".." || strings.HasPrefix(relRepo, ".."+string(os.PathSeparator)) {
		return "", "", "", errors.New("conversation repository is outside base folder")
	}
	relCWD, err := filepath.Rel(repo, workspace)
	if err != nil || relCWD == ".." || strings.HasPrefix(relCWD, ".."+string(os.PathSeparator)) {
		return "", "", "", errors.New("conversation workspace is outside repository")
	}
	if relCWD == "." {
		relCWD = ""
	}
	return repo, relRepo, relCWD, nil
}
