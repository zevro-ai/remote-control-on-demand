package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/zevro-ai/remote-control-on-demand/internal/httpauth"
)

func authMiddleware(auth *httpauth.Service, next http.Handler) http.Handler {
	if auth == nil || !auth.Enabled() {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || !requiresAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		if _, ok := auth.AuthenticateRequest(r); ok {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	})
}

func requiresAuth(path string) bool {
	if strings.HasPrefix(path, "/api/auth/") {
		return false
	}
	return path == "/api" ||
		strings.HasPrefix(path, "/api/") ||
		path == "/ws"
}

func corsMiddleware(token string, externalAuth bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := allowedCORSOrigin(token, externalAuth, r); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedCORSOrigin(token string, externalAuth bool, r *http.Request) string {
	if strings.TrimSpace(token) == "" && !externalAuth {
		return "*"
	}

	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return ""
	}

	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	if !strings.EqualFold(parsed.Host, r.Host) {
		return ""
	}

	return origin
}
