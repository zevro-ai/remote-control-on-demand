package httpauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/config"
)

const (
	sessionCookieName = "rcod_session"
	stateCookieName   = "rcod_auth_state"
	sessionTTL        = 12 * time.Hour
	stateTTL          = 10 * time.Minute
)

type ProviderMetadata struct {
	ID          string
	DisplayName string
}

type Identity struct {
	Provider string
	Subject  string
	Login    string
	Name     string
	Email    string
}

type PublicStatus struct {
	Mode          string
	TokenEnabled  bool
	Provider      *ProviderMetadata
	Authenticated bool
	User          *Identity
	LoginURL      string
	LogoutURL     string
}

type externalProvider interface {
	Metadata() ProviderMetadata
	AuthorizeURL(state string) (string, error)
	Exchange(ctx context.Context, code string) (*Identity, error)
}

type Service struct {
	token         string
	sessionSecret []byte
	provider      externalProvider
	now           func() time.Time
	randomBytes   func(int) ([]byte, error)
}

type sessionClaims struct {
	Provider  string `json:"provider"`
	Subject   string `json:"subject"`
	Login     string `json:"login,omitempty"`
	Name      string `json:"name,omitempty"`
	Email     string `json:"email,omitempty"`
	IssuedAt  int64  `json:"issued_at"`
	ExpiresAt int64  `json:"expires_at"`
}

type authState struct {
	Value     string `json:"value"`
	Redirect  string `json:"redirect"`
	ExpiresAt int64  `json:"expires_at"`
}

func NewService(cfg config.APIConfig) *Service {
	service := &Service{
		token:       strings.TrimSpace(cfg.Token),
		now:         time.Now,
		randomBytes: randomTokenBytes,
	}

	if cfg.Auth == nil {
		return service
	}

	service.sessionSecret = []byte(cfg.Auth.SessionSecret)
	switch {
	case cfg.Auth.OIDC != nil:
		service.provider = newOIDCProvider(*cfg.Auth.OIDC)
	case cfg.Auth.GitHub != nil:
		service.provider = newGitHubProvider(*cfg.Auth.GitHub)
	}
	return service
}

func (s *Service) HasExternalAuth() bool {
	return s != nil && s.provider != nil
}

func (s *Service) Enabled() bool {
	return s != nil && (strings.TrimSpace(s.token) != "" || s.provider != nil)
}

func (s *Service) Status(r *http.Request) PublicStatus {
	status := PublicStatus{
		Mode:         "none",
		TokenEnabled: strings.TrimSpace(s.token) != "",
	}
	if status.TokenEnabled {
		status.Mode = "token"
	}
	if s.HasExternalAuth() {
		metadata := s.provider.Metadata()
		status.Mode = "external"
		status.Provider = &metadata
		status.LoginURL = "/api/auth/login"
		status.LogoutURL = "/api/auth/logout"
	}

	if identity, ok := s.AuthenticateRequest(r); ok {
		status.Authenticated = true
		status.User = identity
	}

	return status
}

func (s *Service) AuthenticateRequest(r *http.Request) (*Identity, bool) {
	if strings.TrimSpace(s.token) != "" {
		auth := strings.TrimSpace(r.Header.Get("Authorization"))
		if auth == "Bearer "+s.token {
			return nil, true
		}
		if allowsQueryToken(r.URL.Path) && requestToken(r) == s.token {
			return nil, true
		}
	}

	if !s.HasExternalAuth() {
		return nil, false
	}

	claims, err := s.readSession(r)
	if err != nil {
		return nil, false
	}
	return &Identity{
		Provider: claims.Provider,
		Subject:  claims.Subject,
		Login:    claims.Login,
		Name:     claims.Name,
		Email:    claims.Email,
	}, true
}

func (s *Service) HandleLogin(w http.ResponseWriter, r *http.Request) error {
	if !s.HasExternalAuth() {
		return ErrExternalAuthDisabled
	}

	stateBytes, err := s.randomBytes(24)
	if err != nil {
		return fmt.Errorf("generating auth state: %w", err)
	}
	stateValue := base64.RawURLEncoding.EncodeToString(stateBytes)
	state := authState{
		Value:     stateValue,
		Redirect:  sanitizeRedirect(r.URL.Query().Get("redirect")),
		ExpiresAt: s.now().Add(stateTTL).Unix(),
	}
	if err := s.writeSignedCookie(w, r, stateCookieName, state, stateTTL); err != nil {
		return err
	}

	authURL, err := s.provider.AuthorizeURL(stateValue)
	if err != nil {
		return err
	}
	http.Redirect(w, r, authURL, http.StatusFound)
	return nil
}

func (s *Service) HandleCallback(w http.ResponseWriter, r *http.Request) error {
	if !s.HasExternalAuth() {
		return ErrExternalAuthDisabled
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	stateValue := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || stateValue == "" {
		return fmt.Errorf("missing OAuth callback parameters")
	}

	state, err := s.readState(r)
	if err != nil {
		return err
	}
	s.clearCookie(w, r, stateCookieName)
	if state.Value != stateValue {
		return ErrInvalidAuthState
	}

	identity, err := s.provider.Exchange(r.Context(), code)
	if err != nil {
		return err
	}
	if err := s.writeSession(w, r, identity); err != nil {
		return err
	}

	http.Redirect(w, r, state.Redirect, http.StatusFound)
	return nil
}

func (s *Service) HandleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearCookie(w, r, sessionCookieName)
	if r.Method == http.MethodGet {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) writeSession(w http.ResponseWriter, r *http.Request, identity *Identity) error {
	if identity == nil {
		return fmt.Errorf("identity is required")
	}
	claims := sessionClaims{
		Provider:  identity.Provider,
		Subject:   identity.Subject,
		Login:     identity.Login,
		Name:      identity.Name,
		Email:     identity.Email,
		IssuedAt:  s.now().Unix(),
		ExpiresAt: s.now().Add(sessionTTL).Unix(),
	}
	return s.writeSignedCookie(w, r, sessionCookieName, claims, sessionTTL)
}

func (s *Service) readSession(r *http.Request) (*sessionClaims, error) {
	var claims sessionClaims
	if err := s.readSignedCookie(r, sessionCookieName, &claims); err != nil {
		return nil, err
	}
	if claims.ExpiresAt <= s.now().Unix() {
		return nil, ErrExpiredSession
	}
	return &claims, nil
}

func (s *Service) readState(r *http.Request) (*authState, error) {
	var state authState
	if err := s.readSignedCookie(r, stateCookieName, &state); err != nil {
		return nil, err
	}
	if state.ExpiresAt <= s.now().Unix() {
		return nil, ErrExpiredAuthState
	}
	if strings.TrimSpace(state.Redirect) == "" {
		state.Redirect = "/"
	}
	return &state, nil
}

func (s *Service) writeSignedCookie(w http.ResponseWriter, r *http.Request, name string, value any, ttl time.Duration) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encoding %s cookie: %w", name, err)
	}
	signed, err := s.sign(payload)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    signed,
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl / time.Second),
	})
	return nil
}

func (s *Service) clearCookie(w http.ResponseWriter, r *http.Request, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (s *Service) readSignedCookie(r *http.Request, name string, out any) error {
	cookie, err := r.Cookie(name)
	if err != nil {
		return err
	}
	payload, err := s.verify(cookie.Value)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decoding %s cookie: %w", name, err)
	}
	return nil
}

func (s *Service) sign(payload []byte) (string, error) {
	if len(s.sessionSecret) == 0 {
		return "", ErrMissingSessionSecret
	}
	mac := hmac.New(sha256.New, s.sessionSecret)
	mac.Write(payload)
	signature := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (s *Service) verify(value string) ([]byte, error) {
	if len(s.sessionSecret) == 0 {
		return nil, ErrMissingSessionSecret
	}
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return nil, ErrInvalidSignature
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidSignature
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidSignature
	}
	mac := hmac.New(sha256.New, s.sessionSecret)
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, ErrInvalidSignature
	}
	return payload, nil
}

func sanitizeRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || strings.HasPrefix(raw, "//") || !strings.HasPrefix(raw, "/") {
		return "/"
	}
	return raw
}

func cookieSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	for _, value := range strings.Split(r.Header.Get("X-Forwarded-Proto"), ",") {
		if strings.EqualFold(strings.TrimSpace(value), "https") {
			return true
		}
	}
	return false
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

func randomTokenBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

var (
	ErrExternalAuthDisabled = errors.New("external auth is not configured")
	ErrMissingSessionSecret = errors.New("session secret is required")
	ErrInvalidSignature     = errors.New("invalid session signature")
	ErrInvalidAuthState     = errors.New("invalid auth state")
	ErrExpiredAuthState     = errors.New("expired auth state")
	ErrExpiredSession       = errors.New("expired session")
)
