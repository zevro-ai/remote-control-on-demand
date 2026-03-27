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
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/claudechat"
	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/config"
	"github.com/zevro-ai/remote-control-on-demand/internal/httpauth"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
)

func setupTestServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()

	runner := &testRunner{}
	sessionMgr := session.NewManager(runner, t.TempDir(), "", false, 0, 0, nil)
	claudeMgr := claudechat.NewManager(t.TempDir(), "")
	codexMgr := codex.NewManager(t.TempDir(), "")

	return setupTestServerWithManagers(t, sessionMgr, claudeMgr, codexMgr)
}

func setupTestServerWithManagers(t *testing.T, sessionMgr *session.Manager, claudeMgr *claudechat.Manager, codexMgr *codex.Manager) (*Server, *http.ServeMux) {
	t.Helper()

	_, registry := testProviders(t, sessionMgr, claudeMgr, codexMgr)
	srv := NewServer(config.APIConfig{Port: 0, Token: "test-token"}, "claude", registry)
	srv.uploadDir = t.TempDir()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/status", srv.handleAuthStatus)
	mux.HandleFunc("GET /api/auth/login", srv.handleAuthLogin)
	mux.HandleFunc("GET /api/auth/callback", srv.handleAuthCallback)
	mux.HandleFunc("GET /api/auth/logout", srv.handleAuthLogout)
	mux.HandleFunc("POST /api/auth/logout", srv.handleAuthLogout)
	mux.HandleFunc("GET /api/sessions", srv.handleListSessions)
	mux.HandleFunc("POST /api/sessions", srv.handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", srv.handleDeleteSession)
	mux.HandleFunc("POST /api/sessions/{id}/restart", srv.handleRestartSession)
	mux.HandleFunc("GET /api/runtime/providers", srv.handleListRuntimeProviderMetadata)
	mux.HandleFunc("GET /api/runtime/{provider}/sessions", srv.handleListProviderRuntimeSessions)
	mux.HandleFunc("POST /api/runtime/{provider}/sessions", srv.handleCreateProviderRuntimeSession)
	mux.HandleFunc("DELETE /api/runtime/{provider}/sessions/{id}", srv.handleDeleteProviderRuntimeSession)
	mux.HandleFunc("POST /api/runtime/{provider}/sessions/{id}/restart", srv.handleRestartProviderRuntimeSession)
	mux.HandleFunc("GET /api/runtime/{provider}/sessions/{id}/logs", srv.handleProviderRuntimeSessionLogs)
	mux.HandleFunc("GET /api/runtime/{provider}/folders", srv.handleListProviderFolders)
	mux.HandleFunc("GET /api/folders", srv.handleListFolders)

	// Generic Chat Provider API
	mux.HandleFunc("GET /api/providers", srv.handleListProviderMetadata)
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

func testProviders(t *testing.T, sessionMgr *session.Manager, claudeMgr *claudechat.Manager, codexMgr *codex.Manager) (provider.RuntimeProvider, *provider.Registry) {
	t.Helper()

	registry := provider.NewRegistry()

	runtimeProvider, err := provider.NewRuntimeAdapter(provider.Metadata{
		ID:          "claude",
		DisplayName: "Claude",
		Runtime: &provider.RuntimeCapabilities{
			LongRunningProcesses: true,
			ExternalURLDetection: true,
		},
	}, sessionMgr)
	if err != nil {
		t.Fatalf("NewRuntimeAdapter(): %v", err)
	}
	if err := registry.RegisterRuntime(runtimeProvider); err != nil {
		t.Fatalf("RegisterRuntime(): %v", err)
	}

	if claudeMgr != nil {
		if err := registry.RegisterChat(claudeMgr); err != nil {
			t.Fatalf("RegisterChat(claude): %v", err)
		}
	}

	if codexMgr != nil {
		if err := registry.RegisterChat(codexMgr); err != nil {
			t.Fatalf("RegisterChat(codex): %v", err)
		}
	}

	return runtimeProvider, registry
}

func requireDefaultRuntimeProvider(t *testing.T, srv *Server) provider.RuntimeProvider {
	t.Helper()

	runtimeProvider, ok := srv.defaultRuntimeProvider()
	if !ok {
		t.Fatal("expected default runtime provider")
	}
	return runtimeProvider
}

type testRunner struct{}

type stubRuntimeSession struct {
	snapshot provider.RuntimeSessionSnapshot
	logs     []string
}

func (s stubRuntimeSession) Snapshot() provider.RuntimeSessionSnapshot {
	return s.snapshot
}

func (s stubRuntimeSession) SnapshotLogs(lines int) []string {
	if lines <= 0 || len(s.logs) <= lines {
		return append([]string(nil), s.logs...)
	}
	return append([]string(nil), s.logs[len(s.logs)-lines:]...)
}

func (s stubRuntimeSession) SubscribeLogs(fn func(string)) func() {
	return func() {}
}

type stubRuntimeProvider struct {
	metadata provider.Metadata
	sessions map[string]provider.RuntimeSession
}

func (p stubRuntimeProvider) Metadata() provider.Metadata { return p.metadata }
func (p stubRuntimeProvider) BaseFolder() string          { return "" }
func (p stubRuntimeProvider) ListFolders() []string       { return nil }
func (p stubRuntimeProvider) ListSessions() []provider.RuntimeSession {
	out := make([]provider.RuntimeSession, 0, len(p.sessions))
	for _, sess := range p.sessions {
		out = append(out, sess)
	}
	return out
}
func (p stubRuntimeProvider) GetSession(id string) (provider.RuntimeSession, bool) {
	sess, ok := p.sessions[id]
	return sess, ok
}
func (p stubRuntimeProvider) CreateSession(folder string) (provider.RuntimeSession, error) {
	return nil, errors.New("not implemented")
}
func (p stubRuntimeProvider) DeleteSession(id string) error  { return errors.New("not implemented") }
func (p stubRuntimeProvider) RestartSession(id string) error { return errors.New("not implemented") }
func (p stubRuntimeProvider) Subscribe(fn func(provider.RuntimeNotification)) func() {
	return func() {}
}

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

func waitForRuntimeSessionStopped(t *testing.T, runtimeProvider provider.RuntimeProvider, sessionID string) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		sess, ok := runtimeProvider.GetSession(sessionID)
		if !ok {
			return
		}
		if sess.Snapshot().Status != string(session.StatusRunning) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatalf("runtime session %s did not stop before cleanup", sessionID)
}

func TestAuthMiddleware_NoToken(t *testing.T) {
	handler := authMiddleware(httpauth.NewService(config.APIConfig{Token: "secret"}), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := authMiddleware(httpauth.NewService(config.APIConfig{Token: "secret"}), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := authMiddleware(httpauth.NewService(config.APIConfig{Token: "secret"}), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := authMiddleware(httpauth.NewService(config.APIConfig{Token: "secret"}), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := authMiddleware(httpauth.NewService(config.APIConfig{Token: "secret"}), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := authMiddleware(httpauth.NewService(config.APIConfig{}), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with no token configured, got %d", rr.Code)
	}
}

func TestAuthStatus_TokenMode(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp authStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if resp.Mode != "token" || !resp.TokenEnabled {
		t.Fatalf("auth status = %#v", resp)
	}
}

func TestAuthStatus_ExternalMode(t *testing.T) {
	srv := NewServer(config.APIConfig{
		Auth: &config.APIAuthConfig{
			SessionSecret: strings.Repeat("x", 32),
			GitHub: &config.GitHubAuthConfig{
				ClientID:     "client",
				ClientSecret: "secret",
				RedirectURL:  "https://rcod.example/api/auth/callback",
			},
		},
	}, "claude", provider.NewRegistry())

	req := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	rr := httptest.NewRecorder()
	srv.handleAuthStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp authStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if resp.Mode != "external" || resp.Provider == nil || resp.Provider.ID != "github" {
		t.Fatalf("auth status = %#v", resp)
	}
}

func TestAuthLogin_RedirectsToConfiguredProvider(t *testing.T) {
	srv := NewServer(config.APIConfig{
		Auth: &config.APIAuthConfig{
			SessionSecret: strings.Repeat("x", 32),
			GitHub: &config.GitHubAuthConfig{
				ClientID:     "client",
				ClientSecret: "secret",
				RedirectURL:  "https://rcod.example/api/auth/callback",
			},
		},
	}, "claude", provider.NewRegistry())

	req := httptest.NewRequest(http.MethodGet, "/api/auth/login?redirect=%2F", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	srv.handleAuthLogin(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}
	location := rr.Header().Get("Location")
	if !strings.HasPrefix(location, "https://github.com/login/oauth/authorize?") {
		t.Fatalf("login redirect = %q", location)
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

func TestListProviderRuntimeSessions_Empty(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/runtime/claude/sessions", nil)
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

func TestListRuntimeProviderMetadata_ReturnsRuntimeProviders(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/runtime/providers", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var providers []providerMetadataResponse
	if err := json.NewDecoder(rr.Body).Decode(&providers); err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 {
		t.Fatalf("expected 1 runtime provider, got %d", len(providers))
	}
	if providers[0].ID != "claude" || providers[0].Runtime == nil {
		t.Fatalf("runtime provider payload = %#v", providers[0])
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

func TestListProviderFolders(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/runtime/claude/folders", nil)
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

func TestListChatProviders_ReturnsLegacyProviderIDs(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/chat/providers", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var providers []string
	if err := json.NewDecoder(rr.Body).Decode(&providers); err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(providers) != 2 || providers[0] != "claude" || providers[1] != "codex" {
		t.Fatalf("providers = %#v, want [claude codex]", providers)
	}
}

func TestListProviderMetadata_ReturnsProviderMetadata(t *testing.T) {
	_, mux := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/providers", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var providers []providerMetadataResponse
	if err := json.NewDecoder(rr.Body).Decode(&providers); err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("providers len = %d, want 2", len(providers))
	}

	if providers[0].ID != "claude" || providers[0].DisplayName != "Claude" {
		t.Fatalf("first provider = %#v", providers[0])
	}
	if providers[0].Chat == nil || providers[0].Runtime == nil {
		t.Fatalf("claude metadata = %#v, want chat+runtime capabilities", providers[0])
	}

	if providers[1].ID != "codex" || providers[1].DisplayName != "Codex" {
		t.Fatalf("second provider = %#v", providers[1])
	}
	if providers[1].Chat == nil {
		t.Fatalf("codex metadata = %#v, want chat capabilities", providers[1])
	}
	if providers[1].Runtime != nil {
		t.Fatalf("codex runtime capabilities = %#v, want nil", providers[1].Runtime)
	}
}

func TestCreateSession_ResponseIncludesProvider(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	runner := &testRunner{}
	sessionMgr := session.NewManager(runner, baseDir, "", false, 0, 0, nil)
	srv, mux := setupTestServerWithManagers(t, sessionMgr, claudechat.NewManager(baseDir, ""), codex.NewManager(baseDir, ""))

	req := httptest.NewRequest("POST", "/api/sessions", bytes.NewBufferString(`{"folder":"demo"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.Code)
	}

	var resp sessionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if resp.Provider != "claude" || resp.Agent != "claude" {
		t.Fatalf("runtime session provider = %q agent = %q, want claude/claude", resp.Provider, resp.Agent)
	}
	if resp.ProviderMeta.ID != "claude" || resp.ProviderMeta.Runtime == nil {
		t.Fatalf("runtime provider metadata = %#v", resp.ProviderMeta)
	}

	runtimeProvider := requireDefaultRuntimeProvider(t, srv)
	if err := runtimeProvider.DeleteSession(resp.ID); err == nil {
		waitForRuntimeSessionStopped(t, runtimeProvider, resp.ID)
	}
}

func TestCreateChatSession_ResponseIncludesProvider(t *testing.T) {
	baseDir := t.TempDir()
	projectDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	runner := &testRunner{}
	sessionMgr := session.NewManager(runner, baseDir, "", false, 0, 0, nil)
	claudeMgr := claudechat.NewManager(baseDir, "")
	codexMgr := codex.NewManager(baseDir, "")
	_, mux := setupTestServerWithManagers(t, sessionMgr, claudeMgr, codexMgr)

	req := httptest.NewRequest("POST", "/api/chat/codex/sessions", bytes.NewBufferString(`{"folder":"demo"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.Code)
	}

	var resp chatSessionResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if resp.Provider != "codex" || resp.Agent != "codex" {
		t.Fatalf("chat session provider = %q agent = %q, want codex/codex", resp.Provider, resp.Agent)
	}
	if resp.ProviderMeta.ID != "codex" || resp.ProviderMeta.Chat == nil {
		t.Fatalf("chat provider metadata = %#v", resp.ProviderMeta)
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
	runner := &testRunner{}
	sessionMgr := session.NewManager(runner, t.TempDir(), "", false, 0, 0, nil)
	srv, mux = setupTestServerWithManagers(t, sessionMgr, claudechat.NewManager(t.TempDir(), ""), codexMgr)
	srv.uploadDir = filepath.Dir(attachmentPath)

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
	runner := &testRunner{}
	sessionMgr := session.NewManager(runner, t.TempDir(), "", false, 0, 0, nil)
	srv, mux = setupTestServerWithManagers(t, sessionMgr, claudeMgr, codex.NewManager(t.TempDir(), ""))
	srv.uploadDir = t.TempDir()

	repoDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(filepath.Join(repoDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	claudeProvider, ok := srv.registry.ChatProvider("claude")
	if !ok {
		t.Fatal("expected claude provider in registry")
	}
	sess, err := claudeProvider.CreateSession("demo")
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
	runner := &testRunner{}
	sessionMgr := session.NewManager(runner, t.TempDir(), "", false, 0, 0, nil)
	srv, mux = setupTestServerWithManagers(t, sessionMgr, claudeMgr, codex.NewManager(t.TempDir(), ""))
	srv.uploadDir = filepath.Dir(attachmentPath)

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
	runtimeProvider := requireDefaultRuntimeProvider(t, srv)

	projectDir := filepath.Join(runtimeProvider.BaseFolder(), "demo")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	sess, err := runtimeProvider.CreateSession("demo")
	if err != nil {
		t.Fatalf("Start(): %v", err)
	}
	t.Cleanup(func() {
		sessionID := sess.Snapshot().ID
		if err := runtimeProvider.DeleteSession(sessionID); err == nil {
			waitForRuntimeSessionStopped(t, runtimeProvider, sessionID)
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
	runtimeProvider := requireDefaultRuntimeProvider(t, srv)

	projectDir := filepath.Join(runtimeProvider.BaseFolder(), "demo")
	if err := os.MkdirAll(filepath.Join(projectDir, ".git"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}

	sess, err := runtimeProvider.CreateSession("demo")
	if err != nil {
		t.Fatalf("Start(): %v", err)
	}
	sessionID := sess.Snapshot().ID
	if err := runtimeProvider.DeleteSession(sessionID); err != nil {
		t.Fatalf("Kill(): %v", err)
	}
	waitForRuntimeSessionStopped(t, runtimeProvider, sessionID)

	req := httptest.NewRequest("DELETE", "/api/sessions/"+sessionID, nil)
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

	_, registry := testProviders(t, sessionMgr, claudeMgr, codexMgr)
	srv := NewServer(config.APIConfig{}, "claude", registry)
	srv.hub.start(registry)
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

	_, registry := testProviders(t, sessionMgr, claudeMgr, codexMgr)
	srv := NewServer(config.APIConfig{}, "claude", registry)
	srv.hub.start(registry)
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

func TestHubHandleChatEvent_EmbedsProviderInSessionPayload(t *testing.T) {
	hub := newHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := &wsClient{
		send:   make(chan []byte, 1),
		subs:   make(map[string]bool),
		ctx:    ctx,
		cancel: cancel,
	}
	hub.addClient(client)
	defer hub.removeClient(client)

	hub.handleChatEvent(provider.Metadata{
		ID:          "codex",
		DisplayName: "Codex",
		Chat: &provider.ChatCapabilities{
			StreamingDeltas:   true,
			ToolCallStreaming: true,
			ShellCommandExec:  true,
			ThreadResume:      true,
			ImageAttachments:  true,
		},
	}, chat.Event{
		Type:      chat.EventSessionCreated,
		SessionID: "codex-1",
		Session: &chat.Session{
			ID:        "codex-1",
			Folder:    "/tmp/demo",
			RelName:   "demo",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		},
	})

	select {
	case data := <-client.send:
		var msg struct {
			Type         string                   `json:"type"`
			Provider     string                   `json:"provider"`
			ProviderMeta providerMetadataResponse `json:"provider_meta"`
			Session      chatSessionResponse      `json:"session"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Unmarshal(): %v", err)
		}
		if msg.Type != "chat_session_added" {
			t.Fatalf("type = %q, want %q", msg.Type, "chat_session_added")
		}
		if msg.Provider != "codex" {
			t.Fatalf("provider = %q, want %q", msg.Provider, "codex")
		}
		if msg.ProviderMeta.ID != "codex" || msg.ProviderMeta.Chat == nil {
			t.Fatalf("provider_meta = %#v", msg.ProviderMeta)
		}
		if msg.Session.Provider != "codex" || msg.Session.Agent != "codex" {
			t.Fatalf("session payload = %#v", msg.Session)
		}
		if msg.Session.ProviderMeta.ID != "codex" || msg.Session.ProviderMeta.Chat == nil {
			t.Fatalf("session provider metadata = %#v", msg.Session.ProviderMeta)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for websocket payload")
	}
}

func TestSubscribeClientToSession_LogPayloadIncludesProvider(t *testing.T) {
	registry := provider.NewRegistry()
	runtimeProvider := stubRuntimeProvider{
		metadata: provider.Metadata{
			ID:          "claude",
			DisplayName: "Claude",
			Runtime: &provider.RuntimeCapabilities{
				LongRunningProcesses: true,
			},
		},
		sessions: map[string]provider.RuntimeSession{
			"runtime-1": stubRuntimeSession{
				snapshot: provider.RuntimeSessionSnapshot{ID: "runtime-1"},
				logs:     []string{"line one"},
			},
		},
	}
	if err := registry.RegisterRuntime(runtimeProvider); err != nil {
		t.Fatalf("RegisterRuntime(): %v", err)
	}

	srv := NewServer(config.APIConfig{}, "claude", registry)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &wsClient{
		send:   make(chan []byte, 1),
		subs:   map[string]bool{"runtime-1": true},
		ctx:    ctx,
		cancel: cancel,
	}

	srv.subscribeClientToSession(client, "runtime-1")

	select {
	case data := <-client.send:
		var msg wsMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("Unmarshal(): %v", err)
		}
		if msg.Type != "log" {
			t.Fatalf("type = %q, want %q", msg.Type, "log")
		}
		if msg.Provider != "claude" {
			t.Fatalf("provider = %q, want %q", msg.Provider, "claude")
		}
		if msg.ProviderMeta == nil || msg.ProviderMeta.ID != "claude" || msg.ProviderMeta.Runtime == nil {
			t.Fatalf("provider_meta = %#v", msg.ProviderMeta)
		}
		if msg.SessionID != "runtime-1" {
			t.Fatalf("session_id = %q, want %q", msg.SessionID, "runtime-1")
		}
		if msg.Line != "line one" {
			t.Fatalf("line = %q, want %q", msg.Line, "line one")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for log payload")
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := corsMiddleware("", false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	handler := corsMiddleware("secret", false, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestCORSMiddleware_ExternalAuthReflectsSameOriginOnly(t *testing.T) {
	handler := corsMiddleware("", true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.Host = "example.test"
	req.Header.Set("Origin", "https://example.test")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://example.test" {
		t.Fatalf("same-origin allow header = %q", got)
	}

	req = httptest.NewRequest("GET", "/api/auth/status", nil)
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
