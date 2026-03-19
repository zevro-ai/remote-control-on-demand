package httpapi

import (
	"net/http"
	"net/url"
	"strings"
)

func authMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || !requiresAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "Bearer "+token {
			next.ServeHTTP(w, r)
			return
		}

		if allowsQueryToken(r.URL.Path) && requestToken(r) == token {
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	})
}

func requiresAuth(path string) bool {
	return path == "/api" ||
		strings.HasPrefix(path, "/api/") ||
		path == "/ws"
}

func allowsQueryToken(path string) bool {
	return path == "/ws" || strings.HasPrefix(path, "/api/uploads/")
}

func requestToken(r *http.Request) string {
	if token := strings.TrimSpace(r.URL.Query().Get("access_token")); token != "" {
		return token
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

func corsMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := allowedCORSOrigin(token, r); origin != "" {
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

func allowedCORSOrigin(token string, r *http.Request) string {
	if strings.TrimSpace(token) == "" {
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
