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

	"github.com/zevro-ai/remote-control-on-demand/internal/config"
	"github.com/zevro-ai/remote-control-on-demand/internal/httpapi/dashboard"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

type Server struct {
	cfg             config.APIConfig
	runtimeProvider provider.RuntimeProvider
	registry        *provider.Registry
	hub             *Hub
	httpServer      *http.Server
	uploadDir       string
	spaFS           fs.FS
}

func NewServer(cfg config.APIConfig, runtimeProvider provider.RuntimeProvider, registry *provider.Registry) *Server {
	spaFS := dashboard.FS()
	if registry == nil {
		registry = provider.NewRegistry()
	}

	return &Server{
		cfg:             cfg,
		runtimeProvider: runtimeProvider,
		registry:        registry,
		hub:             newHub(),
		uploadDir:       filepath.Join(".rcodbot", "uploads"),
		spaFS:           spaFS,
	}
}

func (s *Server) Start() {
	mux := http.NewServeMux()

	// Remote Control sessions (legacy/generic)
	mux.HandleFunc("GET /api/sessions", s.handleListSessions)
	mux.HandleFunc("POST /api/sessions", s.handleCreateSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", s.handleDeleteSession)
	mux.HandleFunc("POST /api/sessions/{id}/restart", s.handleRestartSession)
	mux.HandleFunc("GET /api/sessions/{id}/logs", s.handleSessionLogs)

	// Generic Chat Provider API
	mux.HandleFunc("GET /api/chat/providers", s.handleListProviders)
	mux.HandleFunc("GET /api/chat/{provider}/sessions", s.handleListChatSessions)
	mux.HandleFunc("POST /api/chat/{provider}/sessions", s.handleCreateChatSession)
	mux.HandleFunc("GET /api/chat/{provider}/sessions/{id}/messages", s.handleGetChatMessages)
	mux.HandleFunc("POST /api/chat/{provider}/sessions/{id}/send", s.handleSendChatMessage)
	mux.HandleFunc("POST /api/chat/{provider}/sessions/{id}/command", s.handleRunChatCommand)
	mux.HandleFunc("DELETE /api/chat/{provider}/sessions/{id}", s.handleDeleteChatSession)

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

	s.hub.start(s.runtimeProvider, s.registry)

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
