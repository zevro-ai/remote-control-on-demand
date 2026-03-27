package httpauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type stubProvider struct {
	metadata ProviderMetadata
	authURL  string
	identity *Identity
	err      error
}

func (p stubProvider) Metadata() ProviderMetadata {
	return p.metadata
}

func (p stubProvider) AuthorizeURL(state string) (string, error) {
	if p.err != nil {
		return "", p.err
	}
	return p.authURL + state, nil
}

func (p stubProvider) Exchange(context.Context, string) (*Identity, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.identity, nil
}

func TestServiceStatusTokenMode(t *testing.T) {
	service := &Service{token: "secret"}
	status := service.Status(httptest.NewRequest(http.MethodGet, "/api/auth/status", nil))
	if status.Mode != "token" {
		t.Fatalf("Mode = %q, want token", status.Mode)
	}
	if !status.TokenEnabled {
		t.Fatal("expected token to be enabled")
	}
}

func TestServiceLoginAndCallbackFlow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	service := &Service{
		sessionSecret: []byte(strings.Repeat("x", 32)),
		provider: stubProvider{
			metadata: ProviderMetadata{ID: "github", DisplayName: "GitHub"},
			authURL:  "https://auth.example.test/login?state=",
			identity: &Identity{
				Provider: "github",
				Subject:  "123",
				Login:    "krecik",
				Name:     "Tomasz",
				Email:    "tomasz@example.com",
			},
		},
		now: func() time.Time { return now },
		randomBytes: func(int) ([]byte, error) {
			return []byte("state-12345678901234567890"), nil
		},
	}

	loginReq := httptest.NewRequest(http.MethodGet, "/api/auth/login?redirect=%2Fdashboard", nil)
	loginReq.Header.Set("X-Forwarded-Proto", "https")
	loginRecorder := httptest.NewRecorder()

	if err := service.HandleLogin(loginRecorder, loginReq); err != nil {
		t.Fatalf("HandleLogin(): %v", err)
	}
	if loginRecorder.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", loginRecorder.Code)
	}
	stateCookie := loginRecorder.Result().Cookies()[0]
	if stateCookie.Name != stateCookieName {
		t.Fatalf("cookie name = %q", stateCookie.Name)
	}

	callbackReq := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=code-1&state="+service.mustReadStateValue(t, stateCookie), nil)
	callbackReq.AddCookie(stateCookie)
	callbackReq.Header.Set("X-Forwarded-Proto", "https")
	callbackRecorder := httptest.NewRecorder()

	if err := service.HandleCallback(callbackRecorder, callbackReq); err != nil {
		t.Fatalf("HandleCallback(): %v", err)
	}
	if callbackRecorder.Code != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", callbackRecorder.Code)
	}
	if location := callbackRecorder.Result().Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("redirect = %q, want /dashboard", location)
	}

	var sessionCookie *http.Cookie
	for _, cookie := range callbackRecorder.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			sessionCookie = cookie
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}

	authReq := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	authReq.AddCookie(sessionCookie)
	identity, ok := service.AuthenticateRequest(authReq)
	if !ok || identity == nil {
		t.Fatal("expected session authentication to succeed")
	}
	if identity.Login != "krecik" {
		t.Fatalf("identity login = %q", identity.Login)
	}
}

func TestServiceAuthenticateRequestSupportsBearerAndQueryToken(t *testing.T) {
	service := &Service{token: "secret"}

	headerReq := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	headerReq.Header.Set("Authorization", "Bearer secret")
	if _, ok := service.AuthenticateRequest(headerReq); !ok {
		t.Fatal("expected bearer auth to succeed")
	}

	queryReq := httptest.NewRequest(http.MethodGet, "/ws?access_token=secret", nil)
	if _, ok := service.AuthenticateRequest(queryReq); !ok {
		t.Fatal("expected query auth to succeed for websocket")
	}
}

func (s *Service) mustReadStateValue(t *testing.T, cookie *http.Cookie) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/callback", nil)
	req.AddCookie(cookie)
	state, err := s.readState(req)
	if err != nil {
		t.Fatalf("readState(): %v", err)
	}
	return state.Value
}
