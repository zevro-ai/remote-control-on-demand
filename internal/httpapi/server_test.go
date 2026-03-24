package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"github.com/zevro-ai/remote-control-on-demand/internal/claudechat"
	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/config"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
)

func setupTestServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()

	runner := &testRunner{}
	sessionMgr := session.NewManager(runner, t.TempDir(), "", false, 0, 0, nil)
	claudeMgr := claudechat.NewManager(t.TempDir(), "")
	codexMgr := codex.NewManager(t.TempDir(), "")

	srv := NewServer(config.APIConfig{Port: 0, Token: "test-token"}, sessionMgr, claudeMgr, codexMgr)
	srv.uploadDir = t.TempDir()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions", srv.handleListSessions)
	mux.HandleFunc("POST /api/sessions", srv.handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", srv.handleDeleteSession)
	mux.HandleFunc("POST /api/sessions/{id}/restart", srv.handleRestartSession)
	mux.HandleFunc("GET /api/folders", srv.handleListFolders)

	// Generic Chat Provider API
	mux.HandleFunc("GET /api/chat/providers", srv.handleListProviders)
	mux.HandleFunc("GET /api/chat/{provider}/sessions", srv.handleListChatSessions)
	mux.HandleFunc("POST /api/chat/{provider}/sessions", srv.handleCreateChatSession)
	mux.HandleFunc("GET /api/chat/{provider}/sessions/{id}/messages", srv.handleGetChatMessages)
	mux.HandleFunc("POST /api/chat/{provider}/sessions/{id}/send", srv.handleSendChatMessage)
	mux.HandleFunc("POST /api/chat/{provider}/sessions/{id}/command", srv.handleRunChatCommand)
	mux.HandleFunc("DELETE /api/chat/{provider}/sessions/{id}", srv.handleDeleteChatSession)

	mux.HandleFunc("GET /api/uploads/{name}", srv.handleUpload)

	return srv, mux
}

type testRunner struct{}

func startLongRunningTestCommand(ctx context.Context, dir string, stdout, stderr io.Writer) (*exec.Cmd, error) {
	cmdName := "sleep"
	args := []string{"100"}
	if runtime.GOOS == "windows" {
		cmdName = "powershell"
		args = []string{"-NoProfile", "-NonInteractive", "-Command", "Start-Sleep -Seconds 100"}
	}

	cmd := exec.CommandContext(ctx, cmdName, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func (r *testRunner) Start(ctx context.Context, dir string, stdout, stderr io.Writer) (*exec.Cmd, error) {
	return startLongRunningTestCommand(ctx, dir, stdout, stderr)
}

func (r *testRunner) IsClaudeProcess(pid int) bool { return false }

func waitForSessionExit(t *testing.T, sess *session.Session) {
	t.Helper()

	if sess == nil || sess.Cmd == nil {
		return
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if sess.Cmd.ProcessState != nil {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("session %s process did not exit before cleanup", sess.ID)
}

func TestAuthMiddleware_NoToken(t *testing.T) {
	handler := authMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	handler := authMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_UploadQueryToken(t *testing.T) {
	handler := authMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/uploads/file.png?access_token=secret", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_WebSocketQueryToken(t *testing.T) {
	handler := authMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/ws?access_token=secret", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_SPABypassesToken(t *testing.T) {
	handler := authMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA route, got %d", rr.Code)
	}
}

func TestAuthMiddleware_Empty(t *testing.T) {
	handler := authMiddleware("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with no token configured, got %d", rr.Code)
	}
}

func TestListSessions_Empty(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var sessions []sessionResponse
	if err := json.NewDecoder(rr.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestListFolders(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/folders", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestListCodexSessions_Empty(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/chat/codex/sessions", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var sessions []chatSessionResponse
	if err := json.NewDecoder(rr.Body).Decode(&sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected 0, got %d", len(sessions))
	}
}

func TestDeleteCodexSession_NotFound(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("DELETE", "/api/chat/codex/sessions/nonexistent", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestDeleteCodexSession_CleansUpAttachments(t *testing.T) {
	srv, mux := setupTestServer(t)

	baseDir := t.TempDir()
	repoDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	attachmentPath := filepath.Join(srv.uploadDir, "codex-delete.png")
	if err := os.WriteFile(attachmentPath, []byte("png"), 0644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	statePath := filepath.Join(t.TempDir(), "codex_sessions.json")
	now := time.Now().UTC()
	saved := map[string]interface{}{
		"active_session_id": "codex-1",
		"sessions": []map[string]interface{}{
			{
				"id":         "codex-1",
				"folder":     repoDir,
				"rel_name":   "demo",
				"thread_id":  "thread-1",
				"created_at": now,
				"updated_at": now,
				"messages": []map[string]interface{}{
					{
						"role":      "user",
						"kind":      "text",
						"content":   "look",
						"timestamp": now,
						"attachments": []map[string]interface{}{
							{"id": "att-1", "name": "codex-delete.png", "path": attachmentPath},
						},
					},
				},
			},
		},
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatalf("WriteFile(state): %v", err)
	}

	codexMgr := codex.NewManager(baseDir, statePath)
	if err := codexMgr.Restore(); err != nil {
		t.Fatalf("Restore(): %v", err)
	}
	srv.providers["codex"] = codexMgr

	req := httptest.NewRequest("DELETE", "/api/chat/codex/sessions/codex-1", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if _, err := os.Stat(attachmentPath); !os.IsNotExist(err) {
		t.Fatalf("expected attachment cleanup on close, got err=%v", err)
	}
}

func TestStatusCodeForManagerError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "not found becomes 404",
			err:  errors.New(`session "abc" not found`),
			want: http.StatusNotFound,
		},
		{
			name: "already processing becomes 409",
			err:  errors.New(`session "abc" is already processing another message`),
			want: http.StatusConflict,
		},
		{
			name: "already running becomes 409",
			err:  errors.New(`session already running in "demo" (ID: abc123)`),
			want: http.StatusConflict,
		},
		{
			name: "not running becomes 409",
			err:  errors.New(`session "abc" not running (status: stopped)`),
			want: http.StatusConflict,
		},
		{
			name: "validation stays 400",
			err:  errors.New("message cannot be empty"),
			want: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := statusCodeForManagerError(tt.err); got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestSendClaudeMessage_NotFoundReturns404(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest(
		"POST",
		"/api/chat/claude/sessions/missing/send",
		bytes.NewBufferString(`{"message":"hello"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestSendClaudeMessage_RejectsMultipartWithoutWritingUploads(t *testing.T) {
	srv, mux := setupTestServer(t)

	baseDir := t.TempDir()
	claudeMgr := claudechat.NewManager(baseDir, "")
	srv.providers["claude"] = claudeMgr

	repoDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	sess, err := srv.providers["claude"].CreateSession("demo")
	if err != nil {
		t.Fatalf("Create(): %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("message", "describe this"); err != nil {
		t.Fatal(err)
	}

	part, err := writer.CreateFormFile("images", "wall.png")
	if err != nil {
		t.Fatal(err)
	}
	imageBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wn1x1EAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(imageBytes); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/chat/claude/sessions/"+sess.ID+"/send", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}

	var resp errorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if resp.Error != "image attachments are not supported for Claude sessions in the current CLI mode" {
		t.Fatalf("error = %q", resp.Error)
	}

	entries, err := os.ReadDir(srv.uploadDir)
	if err != nil {
		t.Fatalf("ReadDir(): %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no uploaded files, found %d", len(entries))
	}
}

func TestDeleteClaudeSession_CleansUpAttachments(t *testing.T) {
	srv, mux := setupTestServer(t)

	baseDir := t.TempDir()
	repoDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	attachmentPath := filepath.Join(srv.uploadDir, "claude-delete.png")
	if err := os.WriteFile(attachmentPath, []byte("png"), 0644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	statePath := filepath.Join(t.TempDir(), "claude_sessions.json")
	now := time.Now().UTC()
	saved := map[string]interface{}{
		"active_session_id": "claude-1",
		"sessions": []map[string]interface{}{
			{
				"id":         "claude-1",
				"folder":     repoDir,
				"rel_name":   "demo",
				"thread_id":  "thread-1",
				"created_at": now,
				"updated_at": now,
				"messages": []map[string]interface{}{
					{
						"role":      "user",
						"kind":      "text",
						"content":   "look",
						"timestamp": now,
						"attachments": []map[string]interface{}{
							{"id": "att-1", "name": "claude-delete.png", "path": attachmentPath},
						},
					},
				},
			},
		},
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatalf("Marshal(): %v", err)
	}
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatalf("WriteFile(state): %v", err)
	}

	claudeMgr := claudechat.NewManager(baseDir, statePath)
	if err := claudeMgr.Restore(); err != nil {
		t.Fatalf("Restore(): %v", err)
	}
	srv.providers["claude"] = claudeMgr

	req := httptest.NewRequest("DELETE", "/api/chat/claude/sessions/claude-1", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if _, err := os.Stat(attachmentPath); !os.IsNotExist(err) {
		t.Fatalf("expected attachment cleanup on close, got err=%v", err)
	}
}

func TestSendCodexMessage_NotFoundReturns404(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest(
		"POST",
		"/api/chat/codex/sessions/missing/send",
		bytes.NewBufferString(`{"message":"hello"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestCreateSession_AlreadyRunningReturns409(t *testing.T) {
	srv, mux := setupTestServer(t)

	projectDir := filepath.Join(srv.sessionMgr.BaseFolder(), "demo")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	sess, err := srv.sessionMgr.Start("demo")
	if err != nil {
		t.Fatalf("Start(): %v", err)
	}
	t.Cleanup(func() {
		if err := srv.sessionMgr.Kill(sess.ID); err == nil {
			waitForSessionExit(t, sess)
		}
	})

	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBufferString(`{"folder":"demo"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}
}

func TestDeleteSession_NotRunningReturns409(t *testing.T) {
	srv, mux := setupTestServer(t)

	projectDir := filepath.Join(srv.sessionMgr.BaseFolder(), "demo")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	sess, err := srv.sessionMgr.Start("demo")
	if err != nil {
		t.Fatalf("Start(): %v", err)
	}
	if err := srv.sessionMgr.Kill(sess.ID); err != nil {
		t.Fatalf("Kill(): %v", err)
	}
	waitForSessionExit(t, sess)

	req := httptest.NewRequest("DELETE", "/api/sessions/"+sess.ID, nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rr.Code)
	}
}

func TestRestartSession_NotFoundReturns404(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("POST", "/api/sessions/missing/restart", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestWebSocket_Connect(t *testing.T) {
	runner := &testRunner{}
	sessionMgr := session.NewManager(runner, t.TempDir(), "", false, 0, 0, nil)
	claudeMgr := claudechat.NewManager(t.TempDir(), "")
	codexMgr := codex.NewManager(t.TempDir(), "")

	srv := NewServer(config.APIConfig{}, sessionMgr, claudeMgr, codexMgr)
	srv.hub.start(sessionMgr, srv.providers)
	defer srv.hub.stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", srv.handleWS)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	ctx := context.Background()
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	conn.Close(websocket.StatusNormalClosure, "")
}

func TestWebSocket_RejectsCrossOrigin(t *testing.T) {
	runner := &testRunner{}
	sessionMgr := session.NewManager(runner, t.TempDir(), "", false, 0, 0, nil)
	claudeMgr := claudechat.NewManager(t.TempDir(), "")
	codexMgr := codex.NewManager(t.TempDir(), "")

	srv := NewServer(config.APIConfig{}, sessionMgr, claudeMgr, codexMgr)
	srv.hub.start(sessionMgr, srv.providers)
	defer srv.hub.stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws", srv.handleWS)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:] + "/ws"
	ctx := context.Background()
	_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Origin": []string{"http://evil.example"},
		},
	})
	if err == nil {
		t.Fatal("expected cross-origin websocket dial to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 response, got %#v", resp)
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := corsMiddleware("", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Test OPTIONS preflight
	req := httptest.NewRequest("OPTIONS", "/api/sessions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("missing CORS header")
	}
}

func TestCORSMiddleware_TokenReflectsSameOriginOnly(t *testing.T) {
	handler := corsMiddleware("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Host = "example.test"
	req.Header.Set("Origin", "https://example.test")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.test" {
		t.Fatalf("same-origin allow header = %q", got)
	}

	req = httptest.NewRequest("GET", "/api/sessions", nil)
	req.Host = "example.test"
	req.Header.Set("Origin", "https://evil.test")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("cross-origin allow header = %q, want empty", got)
	}
}

func TestCreateSession_InvalidBody(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBufferString("invalid"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestParseSendMessageRequestMultipart(t *testing.T) {
	srv, _ := setupTestServer(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("message", "describe this screenshot"); err != nil {
		t.Fatal(err)
	}

	part, err := writer.CreateFormFile("images", "wall.png")
	if err != nil {
		t.Fatal(err)
	}
	imageBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wn1x1EAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(imageBytes); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/chat/codex/sessions/test/send", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	message, attachments, err := srv.parseSendMessageRequest(req, "sess-1")
	if err != nil {
		t.Fatalf("parseSendMessageRequest: %v", err)
	}
	if message != "describe this screenshot" {
		t.Fatalf("message = %q", message)
	}
	if len(attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(attachments))
	}
	if attachments[0].ContentType == "" || attachments[0].URL == "" || attachments[0].Path == "" {
		t.Fatalf("attachment metadata incomplete: %+v", attachments[0])
	}
}

func TestParseMultipartSendMessage_CleansUpEarlierFilesOnLaterFailure(t *testing.T) {
	srv, _ := setupTestServer(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("message", "describe both"); err != nil {
		t.Fatal(err)
	}

	validPart, err := writer.CreateFormFile("images", "wall.png")
	if err != nil {
		t.Fatal(err)
	}
	imageBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wn1x1EAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := validPart.Write(imageBytes); err != nil {
		t.Fatal(err)
	}

	invalidPart, err := writer.CreateFormFile("images", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := invalidPart.Write([]byte("not an image")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/chat/codex/sessions/test/send", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	_, _, err = srv.parseSendMessageRequest(req, "sess-1")
	if err == nil {
		t.Fatal("expected multipart parse to fail on non-image attachment")
	}

	entries, err := os.ReadDir(srv.uploadDir)
	if err != nil {
		t.Fatalf("ReadDir(): %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected partial uploads to be cleaned up, found %d files", len(entries))
	}
}

func TestSaveAttachmentRejectsOversizeFile(t *testing.T) {
	srv, _ := setupTestServer(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("images", "huge.png")
	if err != nil {
		t.Fatal(err)
	}

	header := make([]byte, 8)
	copy(header, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	if _, err := part.Write(header); err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(bytes.Repeat([]byte("a"), maxAttachmentBytes+1)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/chat/codex/sessions/test/send", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(maxUploadBytes); err != nil {
		t.Fatalf("ParseMultipartForm(): %v", err)
	}

	fileHeader := req.MultipartForm.File["images"][0]
	fileHeader.Size = 1

	_, err = srv.saveAttachment("sess-1", fileHeader)
	if err == nil {
		t.Fatal("expected oversize attachment to be rejected")
	}

	entries, err := os.ReadDir(srv.uploadDir)
	if err != nil {
		t.Fatalf("ReadDir(): %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected oversize upload cleanup, found %d files", len(entries))
	}
}

func TestCleanupStoredAttachmentsRemovesFiles(t *testing.T) {
	srv, _ := setupTestServer(t)

	path := filepath.Join(srv.uploadDir, "orphan.png")
	if err := os.WriteFile(path, []byte("png"), 0644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	cleanupStoredAttachments([]storedAttachment{{Path: path}})

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected file to be removed, got err=%v", err)
	}
}

func TestSendCodexMessage_CleansUpAttachmentsOnManagerError(t *testing.T) {
	srv, mux := setupTestServer(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("message", "describe this"); err != nil {
		t.Fatal(err)
	}

	part, err := writer.CreateFormFile("images", "wall.png")
	if err != nil {
		t.Fatal(err)
	}
	imageBytes, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wn1x1EAAAAASUVORK5CYII=",
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(imageBytes); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/chat/codex/sessions/missing/send", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}

	entries, err := os.ReadDir(srv.uploadDir)
	if err != nil {
		t.Fatalf("ReadDir(): %v", err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) == ".png" {
			t.Fatalf("expected uploaded attachment to be cleaned up, found %q", entry.Name())
		}
	}
}

func TestCreateClaudeSession_InvalidFolderReturnsBadRequest(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("POST", "/api/chat/claude/sessions", bytes.NewBufferString(`{"folder":"missing"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDeleteClaudeSession_NotFound(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("DELETE", "/api/chat/claude/sessions/nonexistent", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestHandleSPAServesEmbeddedAssetAndFallsBackToIndex(t *testing.T) {
	srv, _ := setupTestServer(t)
	srv.spaFS = fstest.MapFS{
		"index.html":         {Data: []byte("<html>app</html>")},
		"assets/app.js":      {Data: []byte("console.log('ok')")},
		"assets/styles.css":  {Data: []byte("body{}")},
		"nested/feature.txt": {Data: []byte("feature")},
	}

	t.Run("serves existing asset", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/assets/app.js", nil)
		rr := httptest.NewRecorder()

		srv.handleSPA(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		if body := rr.Body.String(); body != "console.log('ok')" {
			t.Fatalf("body = %q", body)
		}
	})

	t.Run("falls back to index for spa route", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/sessions/focus/123", nil)
		rr := httptest.NewRecorder()

		srv.handleSPA(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		if body := rr.Body.String(); body != "<html>app</html>" {
			t.Fatalf("body = %q", body)
		}
	})
}
