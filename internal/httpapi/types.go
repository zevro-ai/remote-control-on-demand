package httpapi

import "github.com/zevro-ai/remote-control-on-demand/internal/provider"

type authProviderResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type authUserResponse struct {
	Provider string `json:"provider"`
	Subject  string `json:"subject"`
	Login    string `json:"login,omitempty"`
	Name     string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
}

type authStatusResponse struct {
	Mode          string                `json:"mode"`
	TokenEnabled  bool                  `json:"token_enabled"`
	Provider      *authProviderResponse `json:"provider,omitempty"`
	Authenticated bool                  `json:"authenticated"`
	User          *authUserResponse     `json:"user,omitempty"`
	LoginURL      string                `json:"login_url,omitempty"`
	LogoutURL     string                `json:"logout_url,omitempty"`
}

type providerMetadataResponse struct {
	ID          string                        `json:"id"`
	DisplayName string                        `json:"display_name"`
	Chat        *provider.ChatCapabilities    `json:"chat,omitempty"`
	Runtime     *provider.RuntimeCapabilities `json:"runtime,omitempty"`
}

type sessionResponse struct {
	ID           string                   `json:"id"`
	Folder       string                   `json:"folder"`
	RelName      string                   `json:"rel_name"`
	Status       string                   `json:"status"`
	Provider     string                   `json:"provider"`
	ProviderMeta providerMetadataResponse `json:"provider_meta"`
	Agent        string                   `json:"agent"`
	URL          string                   `json:"url,omitempty"`
	PID          int                      `json:"pid,omitempty"`
	StartedAt    string                   `json:"started_at"`
	Restarts     int                      `json:"restarts"`
	Uptime       string                   `json:"uptime"`
}

type chatSessionResponse struct {
	ID           string                   `json:"id"`
	Folder       string                   `json:"folder"`
	RelName      string                   `json:"rel_name"`
	Provider     string                   `json:"provider"`
	ProviderMeta providerMetadataResponse `json:"provider_meta"`
	Agent        string                   `json:"agent"` // Provider ID
	ThreadID     string                   `json:"thread_id,omitempty"`
	Busy         bool                     `json:"busy"`
	CreatedAt    string                   `json:"created_at"`
	UpdatedAt    string                   `json:"updated_at"`
	Messages     []messagePayload         `json:"messages,omitempty"`
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
	PartialJSON string `json:"partial_json,omitempty"`
}

type wsMessage struct {
	Type         string                    `json:"type"`
	Provider     string                    `json:"provider,omitempty"`
	ProviderMeta *providerMetadataResponse `json:"provider_meta,omitempty"`
	SessionID    string                    `json:"session_id,omitempty"`
	Line         string                    `json:"line,omitempty"`
	Status       string                    `json:"status,omitempty"`
	Restarts     int                       `json:"restarts,omitempty"`
	Session      interface{}               `json:"session,omitempty"`
	Message      interface{}               `json:"message,omitempty"`
	Delta        string                    `json:"delta,omitempty"`
	Busy         *bool                     `json:"busy,omitempty"`
	ToolCall     *toolCallPayload          `json:"tool_call,omitempty"`
}

type wsClientMessage struct {
	Type      string `json:"type"`
	Provider  string `json:"provider,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}
