package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/claudechat"
	"github.com/zevro-ai/remote-control-on-demand/internal/codex"
	"github.com/zevro-ai/remote-control-on-demand/internal/config"
	"github.com/zevro-ai/remote-control-on-demand/internal/httpapi/dashboard"
	"github.com/zevro-ai/remote-control-on-demand/internal/session"
)

type Server struct {
	cfg        config.APIConfig
	sessionMgr *session.Manager
	claudeMgr  *claudechat.Manager
	codexMgr   *codex.Manager
	hub        *Hub
	httpServer *http.Server
	uploadDir  string
	spaFS      fs.FS
}

func NewServer(cfg config.APIConfig, sessionMgr *session.Manager, claudeMgr *claudechat.Manager, codexMgr *codex.Manager) *Server {
	spaFS := dashboard.FS()
	return &Server{
		cfg:        cfg,
		sessionMgr: sessionMgr,
		claudeMgr:  claudeMgr,
		codexMgr:   codexMgr,
		hub:        newHub(),
		uploadDir:  filepath.Join(".codexbot", "uploads"),
		spaFS:      spaFS,
	}
}

func (s *Server) Start() {
	mux := http.NewServeMux()

	// Claude RC sessions
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /api/sessions/{id}/restart", s.handleRestartSession)
	mux.HandleFunc("GET /api/sessions/{id}/logs", s.handleSessionLogs)

	// Claude chat sessions
	mux.HandleFunc("GET /api/claude/sessions", s.handleListClaudeSessions)
	mux.HandleFunc("POST /api/claude/sessions", s.handleCreateClaudeSession)
	mux.HandleFunc("POST /api/claude/sessions/{id}/send", s.handleSendClaudeMessage)
	mux.HandleFunc("POST /api/claude/sessions/{id}/command", s.handleRunClaudeCommand)
	mux.HandleFunc("GET /api/claude/sessions/{id}/messages", s.handleClaudeMessages)
	mux.HandleFunc("DELETE /api/claude/sessions/{id}", s.handleDeleteClaudeSession)

	// Codex sessions
	mux.HandleFunc("GET /api/codex/sessions", s.handleListCodexSessions)
	mux.HandleFunc("POST /api/codex/sessions", s.handleCreateCodexSession)
	mux.HandleFunc("POST /api/codex/sessions/{id}/send", s.handleSendCodexMessage)
	mux.HandleFunc("POST /api/codex/sessions/{id}/cancel", s.handleCancelCodexSession)
	mux.HandleFunc("POST /api/codex/sessions/{id}/command", s.handleRunCodexCommand)
	mux.HandleFunc("GET /api/codex/sessions/{id}/messages", s.handleCodexMessages)
	mux.HandleFunc("DELETE /api/codex/sessions/{id}", s.handleDeleteCodexSession)

	// Folders
	mux.HandleFunc("GET /api/folders", s.handleListFolders)
	mux.HandleFunc("GET /api/uploads/{name}", s.handleUpload)

	// WebSocket
	mux.HandleFunc("GET /ws", s.handleWS)

	// SPA fallback
	mux.HandleFunc("/", s.handleSPA)

	var handler http.Handler = mux
	handler = authMiddleware(s.cfg.Token, handler)
	handler = corsMiddleware(s.cfg.Token, handler)

	s.hub.start(s.sessionMgr, s.claudeMgr, s.codexMgr)

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("HTTP API server listening on :%d", s.cfg.Port)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("HTTP server error: %v", err)
	}
}

func (s *Server) Shutdown(ctx context.Context) {
	s.hub.stop()
	if s.httpServer != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx)
	}
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if s.spaFS == nil {
		http.NotFound(w, r)
		return
	}

	requestPath := normalizeSPAPath(r.URL.Path)
	if info, err := fs.Stat(s.spaFS, requestPath); err == nil && !info.IsDir() {
		s.serveSPAFile(w, r, requestPath)
		return
	}

	if _, err := fs.Stat(s.spaFS, "index.html"); err != nil {
		http.NotFound(w, r)
		return
	}

	s.serveSPAFile(w, r, "index.html")
}

func normalizeSPAPath(rawPath string) string {
	cleaned := path.Clean("/" + strings.TrimSpace(rawPath))
	if cleaned == "/" {
		return "index.html"
	}
	return strings.TrimPrefix(cleaned, "/")
}

func (s *Server) serveSPAFile(w http.ResponseWriter, r *http.Request, name string) {
	data, err := fs.ReadFile(s.spaFS, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if contentType := mime.TypeByExtension(path.Ext(name)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
