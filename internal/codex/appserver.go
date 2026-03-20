package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

type appServerEnvelope struct {
	ID     *int            `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *appServerError `json:"error,omitempty"`
}

type appServerError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type appServerThreadResponse struct {
	Thread struct {
		ID string `json:"id"`
	} `json:"thread"`
}

type appServerTurnState struct {
	reply    strings.Builder
	turnDone bool
	turnErr  string
}

type appServerItemNotification struct {
	ThreadID string        `json:"threadId"`
	TurnID   string        `json:"turnId"`
	Item     appServerItem `json:"item"`
}

type appServerDeltaNotification struct {
	ThreadID string `json:"threadId"`
	TurnID   string `json:"turnId"`
	ItemID   string `json:"itemId"`
	Delta    string `json:"delta"`
}

type appServerTurnNotification struct {
	ThreadID string `json:"threadId"`
	Turn     struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"turn"`
}

type appServerErrorNotification struct {
	Message string `json:"message"`
}

type appServerItem struct {
	ID               string          `json:"id,omitempty"`
	Type             string          `json:"type,omitempty"`
	Text             string          `json:"text,omitempty"`
	Command          string          `json:"command,omitempty"`
	Status           string          `json:"status,omitempty"`
	Summary          []string        `json:"summary,omitempty"`
	Content          json.RawMessage `json:"content,omitempty"`
	AggregatedOutput string          `json:"aggregatedOutput,omitempty"`
}

// extractContentText extracts text strings from the Content field,
// which may be a JSON array of strings or array of objects with a "text" field.
func extractContentText(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	// Try []string first.
	var strs []string
	if err := json.Unmarshal(raw, &strs); err == nil {
		return strs
	}
	// Try []object with text field.
	var objs []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &objs); err == nil {
		out := make([]string, 0, len(objs))
		for _, o := range objs {
			if o.Text != "" {
				out = append(out, o.Text)
			}
		}
		return out
	}
	return nil
}

func runCodex(
	ctx context.Context,
	sess *Session,
	prompt string,
	attachments []Attachment,
	sandbox,
	model string,
	dangerouslyBypassSandbox bool,
	cb StreamCallback,
) (string, string, error) {
	return runCodexAppServer(ctx, sess, prompt, attachments, sandbox, model, dangerouslyBypassSandbox, cb)
}

func runCodexAppServer(
	ctx context.Context,
	sess *Session,
	prompt string,
	attachments []Attachment,
	sandbox,
	model string,
	dangerouslyBypassSandbox bool,
	cb StreamCallback,
) (string, string, error) {
	codexBin, cmdEnv, err := resolveCodexCommandEnv()
	if err != nil {
		return "", "", fmt.Errorf("starting codex: %w", err)
	}

	cmd := exec.CommandContext(ctx, codexBin, "app-server", "--session-source", "exec")
	cmd.Dir = sess.Folder
	cmd.Env = cmdEnv

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", "", fmt.Errorf("creating stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("creating stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", "", fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("starting codex app-server: %w", err)
	}

	var stderrBuf strings.Builder
	var stderrWG sync.WaitGroup
	stderrWG.Add(1)
	go func() {
		defer stderrWG.Done()
		_, _ = io.Copy(&stderrBuf, stderr)
	}()

	events := make(chan appServerEnvelope, 128)
	scanErr := make(chan error, 1)
	go scanAppServer(stdout, events, scanErr)

	client := &appServerClient{
		stdin:   stdin,
		events:  events,
		scanErr: scanErr,
		cb:      cb,
	}

	threadID, err := client.run(ctx, sess, prompt, attachments, sandbox, model, dangerouslyBypassSandbox)
	_ = stdin.Close()
	waitErr := cmd.Wait()
	stderrWG.Wait()
	if err != nil {
		return threadID, "", err
	}
	if waitErr != nil && ctx.Err() == nil {
		detail := strings.TrimSpace(stderrBuf.String())
		if detail == "" {
			detail = waitErr.Error()
		}
		return threadID, "", fmt.Errorf("codex app-server failed: %s", detail)
	}

	reply := strings.TrimSpace(client.reply.String())
	if reply == "" {
		return threadID, "", fmt.Errorf("codex returned an empty response")
	}

	return threadID, reply, nil
}

type appServerClient struct {
	stdin   io.WriteCloser
	events  <-chan appServerEnvelope
	scanErr <-chan error
	nextID  int
	reply   strings.Builder
	cb      StreamCallback
	pending []appServerEnvelope
}

func (c *appServerClient) run(
	ctx context.Context,
	sess *Session,
	prompt string,
	attachments []Attachment,
	sandbox,
	model string,
	dangerouslyBypassSandbox bool,
) (string, error) {
	if err := c.initialize(ctx); err != nil {
		return "", err
	}

	threadID, err := c.ensureThread(ctx, sess, sandbox, model, dangerouslyBypassSandbox)
	if err != nil {
		return "", err
	}
	if err := c.startTurn(ctx, threadID, sess.Folder, prompt, attachments, model); err != nil {
		return threadID, err
	}

	state, err := c.consumeTurn(ctx)
	if err != nil {
		return threadID, err
	}
	if state.turnErr != "" {
		return threadID, fmt.Errorf("codex turn failed: %s", state.turnErr)
	}
	return threadID, nil
}

func (c *appServerClient) initialize(ctx context.Context) error {
	resp, err := c.sendRequest(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{
			"name":    "rcod",
			"version": "dev",
		},
		"capabilities": map[string]any{
			"experimentalApi": false,
		},
	})
	if err != nil {
		return fmt.Errorf("initializing codex app-server: %w", err)
	}
	if len(resp.Result) == 0 {
		return fmt.Errorf("initializing codex app-server: empty response")
	}
	if err := c.sendNotification("initialized", nil); err != nil {
		return fmt.Errorf("sending initialized notification: %w", err)
	}
	return nil
}

func (c *appServerClient) ensureThread(
	ctx context.Context,
	sess *Session,
	sandbox,
	model string,
	dangerouslyBypassSandbox bool,
) (string, error) {
	params := map[string]any{
		"cwd":                    sess.Folder,
		"persistExtendedHistory": false,
		"approvalPolicy":         appServerApprovalPolicy(dangerouslyBypassSandbox),
		"sandbox":                appServerSandboxMode(sandbox, dangerouslyBypassSandbox),
	}
	if model != "" {
		params["model"] = model
	}

	if sess.ThreadID == "" {
		params["experimentalRawEvents"] = false
		params["developerInstructions"] = developerInstructions(sess)

		resp, err := c.sendRequest(ctx, "thread/start", params)
		if err != nil {
			return "", fmt.Errorf("starting codex thread: %w", err)
		}
		var parsed appServerThreadResponse
		if err := json.Unmarshal(resp.Result, &parsed); err != nil {
			return "", fmt.Errorf("decoding thread/start response: %w", err)
		}
		if parsed.Thread.ID == "" {
			return "", fmt.Errorf("thread/start returned empty thread id")
		}
		return parsed.Thread.ID, nil
	}

	params["threadId"] = sess.ThreadID
	resp, err := c.sendRequest(ctx, "thread/resume", params)
	if err != nil {
		return "", fmt.Errorf("resuming codex thread %q: %w", sess.ThreadID, err)
	}
	var parsed appServerThreadResponse
	if err := json.Unmarshal(resp.Result, &parsed); err != nil {
		return "", fmt.Errorf("decoding thread/resume response: %w", err)
	}
	if parsed.Thread.ID == "" {
		return "", fmt.Errorf("thread/resume returned empty thread id")
	}
	return parsed.Thread.ID, nil
}

func (c *appServerClient) startTurn(
	ctx context.Context,
	threadID, cwd, prompt string,
	attachments []Attachment,
	model string,
) error {
	input := make([]map[string]any, 0, 1+len(attachments))
	if trimmed := strings.TrimSpace(prompt); trimmed != "" {
		input = append(input, map[string]any{
			"type":          "text",
			"text":          trimmed,
			"text_elements": []any{},
		})
	}
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.Path) == "" {
			continue
		}
		input = append(input, map[string]any{
			"type": "localImage",
			"path": attachment.Path,
		})
	}
	if len(input) == 0 {
		return fmt.Errorf("message cannot be empty")
	}

	params := map[string]any{
		"threadId": threadID,
		"input":    input,
		"cwd":      cwd,
	}
	if model != "" {
		params["model"] = model
	}
	if _, err := c.sendRequest(ctx, "turn/start", params); err != nil {
		return fmt.Errorf("starting codex turn: %w", err)
	}
	return nil
}

func (c *appServerClient) consumeTurn(ctx context.Context) (*appServerTurnState, error) {
	state := &appServerTurnState{}
	for !state.turnDone {
		env, err := c.nextEnvelope(ctx)
		if err != nil {
			return nil, err
		}
		if env.ID != nil && env.Method != "" && len(env.Result) == 0 && env.Error == nil {
			if err := c.respondToServerRequest(*env.ID, env.Method); err != nil {
				return nil, err
			}
			continue
		}
		if env.Method == "" {
			continue
		}
		if err := handleAppServerNotification(env.Method, env.Params, state, &c.reply, c.cb); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func (c *appServerClient) sendRequest(ctx context.Context, method string, params any) (appServerEnvelope, error) {
	c.nextID++
	id := c.nextID
	if err := c.writeEnvelope(map[string]any{
		"id":     id,
		"method": method,
		"params": params,
	}); err != nil {
		return appServerEnvelope{}, err
	}

	for {
		env, err := c.readEnvelope(ctx)
		if err != nil {
			return appServerEnvelope{}, err
		}
		if env.ID == nil || *env.ID != id {
			if env.ID != nil && env.Method != "" && len(env.Result) == 0 && env.Error == nil {
				if err := c.respondToServerRequest(*env.ID, env.Method); err != nil {
					return appServerEnvelope{}, err
				}
			} else {
				c.pending = append(c.pending, env)
			}
			continue
		}
		if env.Error != nil {
			return appServerEnvelope{}, fmt.Errorf("%s", env.Error.Message)
		}
		return env, nil
	}
}

func (c *appServerClient) sendNotification(method string, params any) error {
	envelope := map[string]any{"method": method}
	if params != nil {
		envelope["params"] = params
	}
	return c.writeEnvelope(envelope)
}

func (c *appServerClient) respondToServerRequest(id int, method string) error {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval", "applyPatchApproval", "execCommandApproval":
		return c.writeEnvelope(map[string]any{
			"id":     id,
			"result": map[string]any{"decision": "denied"},
		})
	default:
		return c.writeEnvelope(map[string]any{
			"id":    id,
			"error": map[string]any{"code": -32601, "message": fmt.Sprintf("unsupported server request: %s", method)},
		})
	}
}

func (c *appServerClient) writeEnvelope(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding app-server envelope: %w", err)
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing app-server envelope: %w", err)
	}
	return nil
}

func (c *appServerClient) nextEnvelope(ctx context.Context) (appServerEnvelope, error) {
	if len(c.pending) > 0 {
		env := c.pending[0]
		c.pending = c.pending[1:]
		return env, nil
	}

	return c.readEnvelope(ctx)
}

func (c *appServerClient) readEnvelope(ctx context.Context) (appServerEnvelope, error) {
	select {
	case <-ctx.Done():
		return appServerEnvelope{}, ctx.Err()
	case err := <-c.scanErr:
		if err != nil {
			return appServerEnvelope{}, err
		}
		return appServerEnvelope{}, io.EOF
	case env, ok := <-c.events:
		if !ok {
			return appServerEnvelope{}, io.EOF
		}
		return env, nil
	}
}

func scanAppServer(r io.Reader, out chan<- appServerEnvelope, errCh chan<- error) {
	defer close(out)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		var env appServerEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
			continue
		}
		out <- env
	}

	if err := scanner.Err(); err != nil {
		errCh <- err
	}
}

func handleAppServerNotification(
	method string,
	params json.RawMessage,
	state *appServerTurnState,
	reply *strings.Builder,
	cb StreamCallback,
) error {
	switch method {
	case "item/agentMessage/delta":
		var delta appServerDeltaNotification
		if err := json.Unmarshal(params, &delta); err != nil {
			return fmt.Errorf("decoding item/agentMessage/delta: %w", err)
		}
		reply.WriteString(delta.Delta)
		if cb.OnTextDelta != nil && delta.Delta != "" {
			cb.OnTextDelta(delta.Delta)
		}
	case "item/started":
		var item appServerItemNotification
		if err := json.Unmarshal(params, &item); err != nil {
			return fmt.Errorf("decoding item/started: %w", err)
		}
		if normalized, ok := normalizeAppServerItem(item.Item); ok && cb.OnItemStarted != nil {
			cb.OnItemStarted(normalized)
		}
	case "item/completed":
		var item appServerItemNotification
		if err := json.Unmarshal(params, &item); err != nil {
			return fmt.Errorf("decoding item/completed: %w", err)
		}
		if item.Item.Type == "agentMessage" && item.Item.Text != "" && reply.Len() == 0 {
			reply.WriteString(item.Item.Text)
		}
		if normalized, ok := normalizeAppServerItem(item.Item); ok && cb.OnItemCompleted != nil {
			cb.OnItemCompleted(normalized)
		}
	case "turn/completed":
		var turn appServerTurnNotification
		if err := json.Unmarshal(params, &turn); err != nil {
			return fmt.Errorf("decoding turn/completed: %w", err)
		}
		state.turnDone = true
		if turn.Turn.Error != nil {
			state.turnErr = strings.TrimSpace(turn.Turn.Error.Message)
		}
	case "error":
		var appErr appServerErrorNotification
		if err := json.Unmarshal(params, &appErr); err != nil {
			return fmt.Errorf("decoding error notification: %w", err)
		}
		state.turnDone = true
		state.turnErr = strings.TrimSpace(appErr.Message)
	}
	return nil
}

func normalizeAppServerItem(item appServerItem) (ItemEvent, bool) {
	switch item.Type {
	case "userMessage", "agentMessage":
		return ItemEvent{}, false
	case "commandExecution":
		return ItemEvent{
			ID:      item.ID,
			Type:    "command_execution",
			Command: item.Command,
			Text:    item.AggregatedOutput,
			Status:  item.Status,
		}, true
	case "fileChange":
		return ItemEvent{
			ID:     item.ID,
			Type:   "file_changes",
			Status: item.Status,
		}, true
	case "reasoning":
		text := strings.TrimSpace(strings.Join(append(item.Summary, extractContentText(item.Content)...), "\n"))
		return ItemEvent{
			ID:     item.ID,
			Type:   "reasoning",
			Text:   text,
			Status: item.Status,
		}, true
	default:
		return ItemEvent{
			ID:     item.ID,
			Type:   item.Type,
			Text:   item.Text,
			Status: item.Status,
		}, true
	}
}

func appServerSandboxMode(sandbox string, dangerouslyBypassSandbox bool) string {
	if dangerouslyBypassSandbox {
		return "danger-full-access"
	}
	switch strings.TrimSpace(sandbox) {
	case "read-only", "workspace-write", "danger-full-access":
		return sandbox
	default:
		return defaultSandbox
	}
}

func appServerApprovalPolicy(dangerouslyBypassSandbox bool) string {
	if dangerouslyBypassSandbox {
		return "never"
	}
	return "on-request"
}

func developerInstructions(sess *Session) string {
	return fmt.Sprintf(
		"You are Codex talking to the user through Telegram.\nKeep replies concise unless the user asks for depth.\nThis chat session is attached to repository %q.",
		sess.RelName,
	)
}
