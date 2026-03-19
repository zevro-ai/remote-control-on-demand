package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/claudechat"
	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
)

const claudeAttachmentsUnsupportedMessage = "image attachments are not supported for Claude sessions in the current CLI mode"

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.sessionMgr.List()
	resp := make([]sessionResponse, 0, len(sessions))
	for _, sess := range sessions {
		resp = append(resp, toSessionResponse(sess))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	sess, err := s.sessionMgr.Start(req.Folder)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSessionResponse(sess))
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.sessionMgr.Kill(id); err != nil {
		writeManagerError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRestartSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.sessionMgr.Restart(id); err != nil {
		writeManagerError(w, err)
		return
	}
	sess, ok := s.sessionMgr.Get(id)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]string{"status": "restarted"})
		return
	}
	writeJSON(w, http.StatusOK, toSessionResponse(sess))
}

func (s *Server) handleSessionLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, ok := s.sessionMgr.Get(id)
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

	lines := sess.OutputBuf.Lines(n)
	writeJSON(w, http.StatusOK, map[string]interface{}{"lines": lines})
}

// Codex handlers

func (s *Server) handleListClaudeSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.claudeMgr.List()
	resp := make([]claudeSessionResponse, 0, len(sessions))
	for _, sess := range sessions {
		resp = append(resp, toClaudeSessionResponse(sess))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateClaudeSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	sess, err := s.claudeMgr.Create(req.Folder)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toClaudeSessionResponse(sess))
}

func (s *Server) handleSendClaudeMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if _, ok := s.claudeMgr.Get(id); !ok {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found"})
			return
		}
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: claudeAttachmentsUnsupportedMessage})
		return
	}

	message, attachments, err := s.parseSendMessageRequest(r, id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	sess, reply, err := s.claudeMgr.Send(r.Context(), id, message, toClaudeAttachments(attachments))
	if err != nil {
		cleanupStoredAttachments(attachments)
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session": toClaudeSessionResponse(sess),
		"reply":   reply,
	})
}

func (s *Server) handleRunClaudeCommand(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req runCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}

	sess, result, err := s.claudeMgr.RunCommand(r.Context(), id, req.Command)
	if err != nil {
		writeManagerError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session": toClaudeSessionResponse(sess),
		"result":  toCommandPayload(result.Command, result.ExitCode, result.DurationMs, result.TimedOut, result.Truncated),
	})
}

func (s *Server) handleClaudeMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, ok := s.claudeMgr.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found"})
		return
	}

	msgs := make([]messagePayload, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		msgs = append(msgs, messagePayload{
			Role:        m.Role,
			Kind:        m.Kind,
			Content:     m.Content,
			Timestamp:   formatTime(m.Timestamp),
			Attachments: toClaudeAttachmentPayloads(m.Attachments),
			Command:     toClaudeCommandPayload(m.Command),
		})
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleDeleteClaudeSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	attachments := sessionAttachmentFilesFromClaudeSession(s.claudeMgr, id)
	if err := s.claudeMgr.Close(id); err != nil {
		writeManagerError(w, err)
		return
	}
	cleanupStoredAttachments(attachments)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListCodexSessions(w http.ResponseWriter, r *http.Request) {
	sessions := s.codexMgr.List()
	resp := make([]codexSessionResponse, 0, len(sessions))
	for _, sess := range sessions {
		resp = append(resp, toCodexSessionResponse(sess))
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateCodexSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}
	sess, err := s.codexMgr.Create(req.Folder)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCodexSessionResponse(sess))
}

func (s *Server) handleSendCodexMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	message, attachments, err := s.parseSendMessageRequest(r, id)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: err.Error()})
		return
	}
	sess, reply, err := s.codexMgr.Send(r.Context(), id, message, toCodexAttachments(attachments))
	if err != nil {
		cleanupStoredAttachments(attachments)
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session": toCodexSessionResponse(sess),
		"reply":   reply,
	})
}

func (s *Server) handleRunCodexCommand(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var req runCommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid request body"})
		return
	}

	sess, result, err := s.codexMgr.RunCommand(r.Context(), id, req.Command)
	if err != nil {
		writeManagerError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session": toCodexSessionResponse(sess),
		"result":  toCommandPayload(result.Command, result.ExitCode, result.DurationMs, result.TimedOut, result.Truncated),
	})
}

func (s *Server) handleCodexMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess, ok := s.codexMgr.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "session not found"})
		return
	}

	msgs := make([]messagePayload, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		msgs = append(msgs, toCodexMessagePayload(m))
	}
	writeJSON(w, http.StatusOK, msgs)
}

func (s *Server) handleDeleteCodexSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	attachments := sessionAttachmentFilesFromCodexSession(s.codexMgr, id)
	if err := s.codexMgr.Close(id); err != nil {
		writeManagerError(w, err)
		return
	}
	cleanupStoredAttachments(attachments)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListFolders(w http.ResponseWriter, r *http.Request) {
	folders := s.sessionMgr.ListFolders()
	writeJSON(w, http.StatusOK, folders)
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

func sessionAttachmentFilesFromClaudeSession(manager *claudechat.Manager, id string) []storedAttachment {
	sess, ok := manager.Get(id)
	if !ok {
		return nil
	}
	return storedAttachmentsFromClaudeMessages(sess.Messages)
}

func sessionAttachmentFilesFromCodexSession(manager *codex.Manager, id string) []storedAttachment {
	sess, ok := manager.Get(id)
	if !ok {
		return nil
	}
	return storedAttachmentsFromCodexMessages(sess.Messages)
}

func storedAttachmentsFromClaudeMessages(messages []claudechat.Message) []storedAttachment {
	attachments := make([]storedAttachment, 0)
	for _, message := range messages {
		for _, attachment := range message.Attachments {
			attachments = append(attachments, storedAttachment{Path: attachment.Path})
		}
	}
	return attachments
}

func storedAttachmentsFromCodexMessages(messages []codex.Message) []storedAttachment {
	attachments := make([]storedAttachment, 0)
	for _, message := range messages {
		for _, attachment := range message.Attachments {
			attachments = append(attachments, storedAttachment{Path: attachment.Path})
		}
	}
	return attachments
}

// Helpers

func toSessionResponse(sess *session.Session) sessionResponse {
	pid := 0
	if sess.Cmd != nil && sess.Cmd.Process != nil {
		pid = sess.Cmd.Process.Pid
	}
	url := sess.ClaudeURL
	if url == "" {
		url = sess.URL
	}
	return sessionResponse{
		ID:        sess.ID,
		Folder:    sess.Folder,
		RelName:   sess.RelName,
		Status:    string(sess.Status),
		Agent:     "claude",
		URL:       url,
		PID:       pid,
		StartedAt: formatTime(sess.StartedAt),
		Restarts:  sess.Restarts,
		Uptime:    time.Since(sess.StartedAt).Truncate(time.Second).String(),
	}
}

func toCodexSessionResponse(sess *codex.Session) codexSessionResponse {
	msgs := make([]messagePayload, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		msgs = append(msgs, toCodexMessagePayload(m))
	}
	return codexSessionResponse{
		ID:        sess.ID,
		Folder:    sess.Folder,
		RelName:   sess.RelName,
		Agent:     "codex",
		ThreadID:  sess.ThreadID,
		Busy:      sess.Busy,
		CreatedAt: formatTime(sess.CreatedAt),
		UpdatedAt: formatTime(sess.UpdatedAt),
		Messages:  msgs,
	}
}

func toClaudeSessionResponse(sess *claudechat.Session) claudeSessionResponse {
	msgs := make([]messagePayload, 0, len(sess.Messages))
	for _, m := range sess.Messages {
		msgs = append(msgs, toClaudeMessagePayload(m))
	}
	return claudeSessionResponse{
		ID:        sess.ID,
		Folder:    sess.Folder,
		RelName:   sess.RelName,
		Agent:     "claude",
		ThreadID:  sess.ThreadID,
		Busy:      sess.Busy,
		CreatedAt: formatTime(sess.CreatedAt),
		UpdatedAt: formatTime(sess.UpdatedAt),
		Messages:  msgs,
	}
}

func toCodexMessagePayload(message codex.Message) messagePayload {
	return messagePayload{
		Role:        message.Role,
		Kind:        message.Kind,
		Content:     message.Content,
		Timestamp:   formatTime(message.Timestamp),
		Attachments: toCodexAttachmentPayloads(message.Attachments),
		Command:     toCodexCommandPayload(message.Command),
	}
}

func toClaudeMessagePayload(message claudechat.Message) messagePayload {
	return messagePayload{
		Role:        message.Role,
		Kind:        message.Kind,
		Content:     message.Content,
		Timestamp:   formatTime(message.Timestamp),
		Attachments: toClaudeAttachmentPayloads(message.Attachments),
		Command:     toClaudeCommandPayload(message.Command),
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func toCodexAttachments(attachments []storedAttachment) []codex.Attachment {
	if attachments == nil {
		return nil
	}
	out := make([]codex.Attachment, len(attachments))
	for i, attachment := range attachments {
		out[i] = codex.Attachment{
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

func toClaudeAttachments(attachments []storedAttachment) []claudechat.Attachment {
	if attachments == nil {
		return nil
	}
	out := make([]claudechat.Attachment, len(attachments))
	for i, attachment := range attachments {
		out[i] = claudechat.Attachment{
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

func toCodexAttachmentPayloads(attachments []codex.Attachment) []attachmentPayload {
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

func toClaudeAttachmentPayloads(attachments []claudechat.Attachment) []attachmentPayload {
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

func toCodexCommandPayload(command *codex.CommandMeta) *commandPayload {
	if command == nil {
		return nil
	}
	return toCommandPayload(command.Command, command.ExitCode, command.DurationMs, command.TimedOut, command.Truncated)
}

func toClaudeCommandPayload(command *claudechat.CommandMeta) *commandPayload {
	if command == nil {
		return nil
	}
	return toCommandPayload(command.Command, command.ExitCode, command.DurationMs, command.TimedOut, command.Truncated)
}

func toCommandPayload(command string, exitCode int, durationMs int64, timedOut, truncated bool) *commandPayload {
	return &commandPayload{
		Command:    command,
		ExitCode:   exitCode,
		DurationMs: durationMs,
		TimedOut:   timedOut,
		Truncated:  truncated,
	}
}
