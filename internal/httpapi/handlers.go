package httpapi

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if s.runtimeProvider == nil {
		writeJSON(w, http.StatusOK, []sessionResponse{})
		return
	}

	sessions := s.runtimeProvider.ListSessions()
	resp := make([]sessionResponse, 0, len(sessions))
	for _, sess := range sessions {
		resp = append(resp, toSessionResponse(sess))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if s.runtimeProvider == nil {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "runtime provider not configured"})
		return
	}

	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	sess, err := s.runtimeProvider.CreateSession(req.Folder)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSessionResponse(sess))
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	if s.runtimeProvider == nil {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "runtime provider not configured"})
		return
	}

	id := r.PathValue("id")
	if err := s.runtimeProvider.DeleteSession(id); err != nil {
		writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestartSession(w http.ResponseWriter, r *http.Request) {
	if s.runtimeProvider == nil {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "runtime provider not configured"})
		return
	}

	id := r.PathValue("id")
	if err := s.runtimeProvider.RestartSession(id); err != nil {
		writeManagerError(w, err)
		return
	}
	sess, ok := s.runtimeProvider.GetSession(id)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
		return
	}
	writeJSON(w, http.StatusOK, toSessionResponse(sess))
}

func (s *Server) handleSessionLogs(w http.ResponseWriter, r *http.Request) {
	if s.runtimeProvider == nil {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "runtime provider not configured"})
		return
	}

	id := r.PathValue("id")
	sess, ok := s.runtimeProvider.GetSession(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found"})
		return
	}

	n := 50
	if qn := r.URL.Query().Get("lines"); qn != "" {
		if parsed, err := strconv.Atoi(qn); err == nil && parsed > 0 {
			n = parsed
		}
	}

	lines := sess.SnapshotLogs(n)
	writeJSON(w, http.StatusOK, map[string]interface{}{"lines": lines})
}

// Generic Chat Provider Handlers

func (s *Server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	chatProviders := s.registry.ChatProviders()
	providers := make([]string, 0, len(chatProviders))
	for _, p := range chatProviders {
		providers = append(providers, p.Metadata().ID)
	}
	sort.Strings(providers)
	writeJSON(w, http.StatusOK, providers)
}

func (s *Server) handleListChatSessions(w http.ResponseWriter, r *http.Request) {
	p, ok := s.getProvider(w, r)
	if !ok {
		return
	}
	providerID := p.Metadata().ID

	sessions := p.ListSessions()
	resp := make([]chatSessionResponse, 0, len(sessions))
	for _, sess := range sessions {
		resp = append(resp, toChatSessionResponse(sess, providerID))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateChatSession(w http.ResponseWriter, r *http.Request) {
	p, ok := s.getProvider(w, r)
	if !ok {
		return
	}
	providerID := p.Metadata().ID

	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}

	sess, err := p.CreateSession(req.Folder)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toChatSessionResponse(sess, providerID))
}

func (s *Server) handleGetChatMessages(w http.ResponseWriter, r *http.Request) {
	p, ok := s.getProvider(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	sess, ok := p.GetSession(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found"})
		return
	}

	msgs := make([]messagePayload, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		msgs = append(msgs, toMessagePayload(m))
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleSendChatMessage(w http.ResponseWriter, r *http.Request) {
	p, ok := s.getProvider(w, r)
	if !ok {
		return
	}
	providerID := p.Metadata().ID

	id := r.PathValue("id")
	message, attachments, err := s.parseSendMessageRequest(r, id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}

	err = p.SendMessage(r.Context(), id, message, toChatAttachments(attachments))
	if err != nil {
		cleanupStoredAttachments(attachments)
		writeManagerError(w, err)
		return
	}

	sess, ok := p.GetSession(id)
	if !ok {
		cleanupStoredAttachments(attachments)
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found after message sent"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session": toChatSessionResponse(sess, providerID),
	})
}

func toChatAttachments(attachments []storedAttachment) []chat.Attachment {
	if attachments == nil {
		return nil
	}
	out := make([]chat.Attachment, len(attachments))
	for i, attachment := range attachments {
		out[i] = chat.Attachment{
			ID:          attachment.ID,
			Name:        attachment.Name,
			ContentType: attachment.ContentType,
			Size:        attachment.Size,
			URL:         attachment.URL,
			Path:        attachment.Path,
		}
	}
	return out
}

func (s *Server) handleRunChatCommand(w http.ResponseWriter, r *http.Request) {
	p, ok := s.getProvider(w, r)
	if !ok {
		return
	}
	providerID := p.Metadata().ID

	id := r.PathValue("id")
	var req runCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}

	err := p.RunCommand(r.Context(), id, req.Command)
	if err != nil {
		writeManagerError(w, err)
		return
	}

	sess, ok := p.GetSession(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found after command sent"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session": toChatSessionResponse(sess, providerID),
	})
}

func (s *Server) handleDeleteChatSession(w http.ResponseWriter, r *http.Request) {
	p, ok := s.getProvider(w, r)
	if !ok {
		return
	}

	id := r.PathValue("id")
	// Cleanup attachments before deleting
	sess, ok := p.GetSession(id)
	if ok {
		attachments := storedAttachmentsFromMessages(sess.Messages)
		cleanupStoredAttachments(attachments)
	}

	if err := p.DeleteSession(id); err != nil {
		writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getProvider(w http.ResponseWriter, r *http.Request) (provider.ChatProvider, bool) {
	id := r.PathValue("provider")
	p, ok := s.registry.ChatProvider(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: fmt.Sprintf("provider %q not found", id)})
		return nil, false
	}
	return p, true
}

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	if s.runtimeProvider == nil {
		writeJSON(w, http.StatusOK, []string{})
		return
	}
	folders := listFolders(s.runtimeProvider.BaseFolder())
	writeJSON(w, http.StatusOK, folders)
}

func listFolders(baseFolder string) []string {
	info, err := os.Stat(baseFolder)
	if err != nil || !info.IsDir() {
		return nil
	}

	var folders []string
	_ = filepath.WalkDir(baseFolder, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || path == baseFolder {
			return nil
		}
		if !d.IsDir() {
			return nil
		}

		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		switch name {
		case "node_modules", "vendor", "dist", "build", "tmp":
			return filepath.SkipDir
		}

		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			rel, err := filepath.Rel(baseFolder, path)
			if err == nil && rel != "." {
				folders = append(folders, rel)
			}
			return filepath.SkipDir
		}
		return nil
	})

	sort.Strings(folders)
	return folders
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("name"))
	if name == "." || name == "" {
		http.NotFound(w, r)
		return
	}

	path := filepath.Join(s.uploadDir, name)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}

func writeManagerError(w http.ResponseWriter, err error) {
	writeJSON(w, statusCodeForManagerError(err), errorResponse{Error: err.Error()})
}

func statusCodeForManagerError(err error) int {
	if err == nil {
		return http.StatusBadRequest
	}

	message := err.Error()
	switch {
	case strings.Contains(message, "not found"):
		return http.StatusNotFound
	case strings.Contains(message, "already processing"),
		strings.Contains(message, "already running"),
		strings.Contains(message, "not running"):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func cleanupStoredAttachments(attachments []storedAttachment) {
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.Path) == "" {
			continue
		}
		_ = os.Remove(attachment.Path)
	}
}

func storedAttachmentsFromMessages(messages []chat.Message) []storedAttachment {
	attachments := make([]storedAttachment, 0)
	for _, message := range messages {
		for _, attachment := range message.Attachments {
			attachments = append(attachments, storedAttachment{Path: attachment.Path})
		}
	}
	return attachments
}

// Helpers

func toSessionResponse(sess provider.RuntimeSession) sessionResponse {
	if sess == nil {
		return sessionResponse{}
	}
	snapshot := sess.Snapshot()
	return sessionResponse{
		ID:        snapshot.ID,
		Folder:    snapshot.Folder,
		RelName:   snapshot.RelName,
		Status:    snapshot.Status,
		Agent:     "claude",
		URL:       snapshot.URL,
		PID:       snapshot.PID,
		StartedAt: formatTime(snapshot.StartedAt),
		Restarts:  snapshot.Restarts,
		Uptime:    time.Since(snapshot.StartedAt).Truncate(time.Second).String(),
	}
}

func toChatSessionResponse(sess *chat.Session, provider string) chatSessionResponse {
	if sess == nil {
		return chatSessionResponse{}
	}
	msgs := make([]messagePayload, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		msgs = append(msgs, toMessagePayload(m))
	}
	return chatSessionResponse{
		ID:        sess.ID,
		Folder:    sess.Folder,
		RelName:   sess.RelName,
		Agent:     provider,
		ThreadID:  sess.ThreadID,
		Busy:      sess.Busy,
		CreatedAt: formatTime(sess.CreatedAt),
		UpdatedAt: formatTime(sess.UpdatedAt),
		Messages:  msgs,
	}
}

func toMessagePayload(message chat.Message) messagePayload {
	return messagePayload{
		Role:        message.Role,
		Kind:        message.Kind,
		Content:     message.Content,
		Timestamp:   formatTime(message.Timestamp),
		Attachments: toAttachmentPayloads(message.Attachments),
		Command:     toCommandMetaPayload(message.Command),
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func toAttachmentPayloads(attachments []chat.Attachment) []attachmentPayload {
	if attachments == nil {
		return nil
	}
	out := make([]attachmentPayload, len(attachments))
	for i, attachment := range attachments {
		out[i] = attachmentPayload{
			ID:          attachment.ID,
			Name:        attachment.Name,
			ContentType: attachment.ContentType,
			Size:        attachment.Size,
			URL:         attachment.URL,
		}
	}
	return out
}

func toCommandMetaPayload(command *chat.CommandMeta) *commandPayload {
	if command == nil {
		return nil
	}
	return &commandPayload{
		Command:    command.Command,
		ExitCode:   command.ExitCode,
		DurationMs: command.DurationMs,
		TimedOut:   command.TimedOut,
		Truncated:  command.Truncated,
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
