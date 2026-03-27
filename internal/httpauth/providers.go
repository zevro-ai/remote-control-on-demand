package httpauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/zevro-ai/remote-control-on-demand/internal/config"
)

type oidcProvider struct {
	cfg    config.OIDCAuthConfig
	client *http.Client
}

type githubProvider struct {
	cfg    config.GitHubAuthConfig
	client *http.Client
}

type oidcDiscovery struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserInfoEndpoint      string `json:"userinfo_endpoint"`
}

type oauthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type oidcUserInfo struct {
	Subject           string        `json:"sub"`
	Email             string        `json:"email"`
	Name              string        `json:"name"`
	PreferredUsername string        `json:"preferred_username"`
	Groups            []string      `json:"groups"`
	RawGroups         []interface{} `json:"-"`
}

type githubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

type githubOrg struct {
	Login string `json:"login"`
}

func newOIDCProvider(cfg config.OIDCAuthConfig) externalProvider {
	return &oidcProvider{cfg: cfg, client: http.DefaultClient}
}

func newGitHubProvider(cfg config.GitHubAuthConfig) externalProvider {
	return &githubProvider{cfg: cfg, client: http.DefaultClient}
}

func (p *oidcProvider) Metadata() ProviderMetadata {
	return ProviderMetadata{ID: "oidc", DisplayName: "OIDC"}
}

func (p *oidcProvider) AuthorizeURL(state string) (string, error) {
	discovery, err := p.discover(context.Background())
	if err != nil {
		return "", err
	}

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", p.cfg.ClientID)
	values.Set("redirect_uri", p.cfg.RedirectURL)
	values.Set("scope", strings.Join(p.scopes(), " "))
	values.Set("state", state)

	return discovery.AuthorizationEndpoint + "?" + values.Encode(), nil
}

func (p *oidcProvider) Exchange(ctx context.Context, code string) (*Identity, error) {
	discovery, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}

	token, err := exchangeOAuthCode(ctx, p.client, discovery.TokenEndpoint, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {p.cfg.ClientID},
		"client_secret": {p.cfg.ClientSecret},
		"redirect_uri":  {p.cfg.RedirectURL},
	})
	if err != nil {
		return nil, err
	}

	var userInfo map[string]any
	if err := getJSONWithBearer(ctx, p.client, discovery.UserInfoEndpoint, token.AccessToken, &userInfo); err != nil {
		return nil, err
	}

	identity := &Identity{
		Provider: "oidc",
		Subject:  stringValue(userInfo["sub"]),
		Login:    firstNonEmpty(stringValue(userInfo["preferred_username"]), stringValue(userInfo["nickname"])),
		Name:     stringValue(userInfo["name"]),
		Email:    stringValue(userInfo["email"]),
	}
	groups := stringSliceValue(userInfo["groups"])
	if identity.Subject == "" {
		return nil, fmt.Errorf("OIDC userinfo missing subject")
	}
	if err := authorizeOIDCIdentity(identity, groups, p.cfg); err != nil {
		return nil, err
	}
	return identity, nil
}

func (p *oidcProvider) scopes() []string {
	scopes := []string{"openid", "profile", "email"}
	seen := map[string]bool{}
	out := make([]string, 0, len(scopes)+len(p.cfg.Scopes))
	for _, scope := range append(scopes, p.cfg.Scopes...) {
		scope = strings.TrimSpace(scope)
		if scope == "" || seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out
}

func (p *oidcProvider) discover(ctx context.Context) (*oidcDiscovery, error) {
	base := strings.TrimRight(strings.TrimSpace(p.cfg.IssuerURL), "/")
	if base == "" {
		return nil, fmt.Errorf("OIDC issuer URL is required")
	}
	url := base + "/.well-known/openid-configuration"
	var discovery oidcDiscovery
	if err := getJSON(ctx, p.client, url, &discovery); err != nil {
		return nil, err
	}
	if discovery.AuthorizationEndpoint == "" || discovery.TokenEndpoint == "" || discovery.UserInfoEndpoint == "" {
		return nil, fmt.Errorf("OIDC discovery document is missing required endpoints")
	}
	return &discovery, nil
}

func authorizeOIDCIdentity(identity *Identity, groups []string, cfg config.OIDCAuthConfig) error {
	if len(cfg.AllowedUsers) > 0 && !containsFold(cfg.AllowedUsers, identity.Login) {
		return fmt.Errorf("OIDC user %q is not allowed", identity.Login)
	}
	if len(cfg.AllowedEmails) > 0 && !containsFold(cfg.AllowedEmails, identity.Email) {
		return fmt.Errorf("OIDC email %q is not allowed", identity.Email)
	}
	if len(cfg.AllowedGroups) > 0 && !intersectsFold(cfg.AllowedGroups, groups) {
		return fmt.Errorf("OIDC groups do not satisfy allowlist")
	}
	return nil
}

func (p *githubProvider) Metadata() ProviderMetadata {
	return ProviderMetadata{ID: "github", DisplayName: "GitHub"}
}

func (p *githubProvider) AuthorizeURL(state string) (string, error) {
	values := url.Values{}
	values.Set("client_id", p.cfg.ClientID)
	values.Set("redirect_uri", p.cfg.RedirectURL)
	values.Set("scope", strings.Join(p.scopes(), " "))
	values.Set("state", state)
	return "https://github.com/login/oauth/authorize?" + values.Encode(), nil
}

func (p *githubProvider) Exchange(ctx context.Context, code string) (*Identity, error) {
	token, err := exchangeOAuthCode(ctx, p.client, "https://github.com/login/oauth/access_token", url.Values{
		"code":          {code},
		"client_id":     {p.cfg.ClientID},
		"client_secret": {p.cfg.ClientSecret},
		"redirect_uri":  {p.cfg.RedirectURL},
	})
	if err != nil {
		return nil, err
	}

	var user githubUser
	if err := getJSONWithBearer(ctx, p.client, "https://api.github.com/user", token.AccessToken, &user); err != nil {
		return nil, err
	}

	email := strings.TrimSpace(user.Email)
	if email == "" {
		var emails []githubEmail
		if err := getJSONWithBearer(ctx, p.client, "https://api.github.com/user/emails", token.AccessToken, &emails); err == nil {
			for _, candidate := range emails {
				if candidate.Primary && candidate.Verified {
					email = candidate.Email
					break
				}
			}
		}
	}

	identity := &Identity{
		Provider: "github",
		Subject:  strconv.FormatInt(user.ID, 10),
		Login:    user.Login,
		Name:     firstNonEmpty(user.Name, user.Login),
		Email:    email,
	}
	if identity.Subject == "0" {
		return nil, fmt.Errorf("GitHub user profile is missing id")
	}
	if len(p.cfg.AllowedUsers) > 0 && !containsFold(p.cfg.AllowedUsers, identity.Login) {
		return nil, fmt.Errorf("GitHub user %q is not allowed", identity.Login)
	}
	if len(p.cfg.AllowedOrgs) > 0 {
		var orgs []githubOrg
		if err := getJSONWithBearer(ctx, p.client, "https://api.github.com/user/orgs", token.AccessToken, &orgs); err != nil {
			return nil, err
		}
		orgLogins := make([]string, 0, len(orgs))
		for _, org := range orgs {
			orgLogins = append(orgLogins, org.Login)
		}
		if !intersectsFold(p.cfg.AllowedOrgs, orgLogins) {
			return nil, fmt.Errorf("GitHub organizations do not satisfy allowlist")
		}
	}

	return identity, nil
}

func (p *githubProvider) scopes() []string {
	scopes := []string{"read:user", "user:email"}
	if len(p.cfg.AllowedOrgs) > 0 {
		scopes = append(scopes, "read:org")
	}
	return scopes
}

func exchangeOAuthCode(ctx context.Context, client *http.Client, endpoint string, form url.Values) (*oauthTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OAuth token exchange failed with status %d", resp.StatusCode)
	}
	var token oauthTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, err
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return nil, fmt.Errorf("OAuth token exchange returned no access token")
	}
	return &token, nil
}

func getJSON(ctx context.Context, client *http.Client, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s failed with status %d", endpoint, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func getJSONWithBearer(ctx context.Context, client *http.Client, endpoint, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s failed with status %d", endpoint, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func containsFold(values []string, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), candidate) {
			return true
		}
	}
	return false
}

func intersectsFold(allowed []string, values []string) bool {
	for _, value := range values {
		if containsFold(allowed, value) {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func stringSliceValue(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := stringValue(item); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
