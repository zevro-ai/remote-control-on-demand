package httpapi

import "time"

type sessionResponse struct {
	ID        string `json:"id"`
	Folder    string `json:"folder"`
	RelName   string `json:"rel_name"`
	Status    string `json:"status"`
	Agent     string `json:"agent"`
	URL       string `json:"url,omitempty"`
	PID       int    `json:"pid,omitempty"`
	StartedAt string `json:"started_at"`
	Restarts  int    `json:"restarts"`
	Uptime    string `json:"uptime"`
}

type codexSessionResponse struct {
	ID        string           `json:"id"`
	Folder    string           `json:"folder"`
	RelName   string           `json:"rel_name"`
	Agent     string           `json:"agent"`
	ThreadID  string           `json:"thread_id,omitempty"`
	Busy      bool             `json:"busy"`
	CreatedAt string           `json:"created_at"`
	UpdatedAt string           `json:"updated_at"`
	Messages  []messagePayload `json:"messages,omitempty"`
}

type claudeSessionResponse struct {
	ID        string           `json:"id"`
	Folder    string           `json:"folder"`
	RelName   string           `json:"rel_name"`
	Agent     string           `json:"agent"`
	ThreadID  string           `json:"thread_id,omitempty"`
	Busy      bool             `json:"busy"`
	CreatedAt string           `json:"created_at"`
	UpdatedAt string           `json:"updated_at"`
	Messages  []messagePayload `json:"messages,omitempty"`
}

type messagePayload struct {
	Role        string              `json:"role"`
	Kind        string              `json:"kind,omitempty"`
	Content     string              `json:"content"`
	Timestamp   string              `json:"timestamp"`
	Attachments []attachmentPayload `json:"attachments,omitempty"`
	Command     *commandPayload     `json:"command,omitempty"`
}

type attachmentPayload struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type,omitempty"`
	Size        int64  `json:"size,omitempty"`
	URL         string `json:"url,omitempty"`
}

type commandPayload struct {
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	TimedOut   bool   `json:"timed_out,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type createSessionRequest struct {
	Folder string `json:"folder"`
}

type sendMessageRequest struct {
	Message string `json:"message"`
}

type runCommandRequest struct {
	Command string `json:"command"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type toolCallPayload struct {
	Index       int    `json:"index"`
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Command     string `json:"command,omitempty"`
	OutputText  string `json:"output_text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

type wsMessage struct {
	Type      string           `json:"type"`
	SessionID string           `json:"session_id,omitempty"`
	Line      string           `json:"line,omitempty"`
	Status    string           `json:"status,omitempty"`
	Restarts  int              `json:"restarts,omitempty"`
	Session   interface{}      `json:"session,omitempty"`
	Message   interface{}      `json:"message,omitempty"`
	Delta     string           `json:"delta,omitempty"`
	Busy      *bool            `json:"busy,omitempty"`
	ToolCall  *toolCallPayload `json:"tool_call,omitempty"`
}

type wsClientMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
