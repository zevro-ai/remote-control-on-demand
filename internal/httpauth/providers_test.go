package httpauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zevro-ai/remote-control-on-demand/internal/config"
)

func TestOIDCProviderRevalidateRefreshesExpiredAccessToken(t *testing.T) {
	var issuerURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			writeJSONResponse(t, w, map[string]string{
				"authorization_endpoint": issuerURL + "/authorize",
				"token_endpoint":         issuerURL + "/token",
				"userinfo_endpoint":      issuerURL + "/userinfo",
			})
		case "/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm(): %v", err)
			}
			switch r.Form.Get("grant_type") {
			case "refresh_token":
				writeJSONResponse(t, w, map[string]string{
					"access_token":  "refreshed-token",
					"refresh_token": "refresh-2",
				})
			default:
				t.Fatalf("unexpected grant_type %q", r.Form.Get("grant_type"))
			}
		case "/userinfo":
			switch r.Header.Get("Authorization") {
			case "Bearer expired-token":
				http.Error(w, "expired", http.StatusUnauthorized)
			case "Bearer refreshed-token":
				writeJSONResponse(t, w, map[string]string{
					"sub":                "user-1",
					"preferred_username": "krecik",
					"name":               "Tomasz",
					"email":              "tomasz@example.com",
				})
			default:
				t.Fatalf("unexpected authorization header %q", r.Header.Get("Authorization"))
			}
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()
	issuerURL = server.URL

	provider := newOIDCProvider(config.OIDCAuthConfig{
		IssuerURL:    issuerURL,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://rcod.example/api/auth/callback",
	}).(*oidcProvider)

	session, err := provider.Revalidate(context.Background(), &providerSession{
		AccessToken:  "expired-token",
		RefreshToken: "refresh-1",
	})
	if err != nil {
		t.Fatalf("Revalidate(): %v", err)
	}
	if session.AccessToken != "refreshed-token" {
		t.Fatalf("AccessToken = %q, want refreshed-token", session.AccessToken)
	}
	if session.RefreshToken != "refresh-2" {
		t.Fatalf("RefreshToken = %q, want refresh-2", session.RefreshToken)
	}
	if session.Identity == nil || session.Identity.Login != "krecik" {
		t.Fatalf("Identity = %#v", session.Identity)
	}
}

func writeJSONResponse(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatalf("Encode(): %v", err)
	}
}
