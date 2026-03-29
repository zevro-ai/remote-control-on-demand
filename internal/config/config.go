package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type APIConfig struct {
	Port  int            `yaml:"port"`            // default 0 = disabled
	Token string         `yaml:"token,omitempty"` // optional bearer token
	Auth  *APIAuthConfig `yaml:"auth,omitempty"`
}

type APIAuthConfig struct {
	SessionSecret string            `yaml:"session_secret,omitempty"`
	OIDC          *OIDCAuthConfig   `yaml:"oidc,omitempty"`
	GitHub        *GitHubAuthConfig `yaml:"github,omitempty"`
}

type OIDCAuthConfig struct {
	IssuerURL     string   `yaml:"issuer_url,omitempty"`
	ClientID      string   `yaml:"client_id,omitempty"`
	ClientSecret  string   `yaml:"client_secret,omitempty"`
	RedirectURL   string   `yaml:"redirect_url,omitempty"`
	Scopes        []string `yaml:"scopes,omitempty"`
	AllowedUsers  []string `yaml:"allowed_users,omitempty"`
	AllowedEmails []string `yaml:"allowed_emails,omitempty"`
	AllowedGroups []string `yaml:"allowed_groups,omitempty"`
}

type GitHubAuthConfig struct {
	ClientID     string   `yaml:"client_id,omitempty"`
	ClientSecret string   `yaml:"client_secret,omitempty"`
	RedirectURL  string   `yaml:"redirect_url,omitempty"`
	AllowedUsers []string `yaml:"allowed_users,omitempty"`
	AllowedOrgs  []string `yaml:"allowed_orgs,omitempty"`
}

type Config struct {
	Telegram  TelegramConfig  `yaml:"telegram"`
	RC        RCConfig        `yaml:"rc"`
	Providers ProvidersConfig `yaml:"providers,omitempty"`
	API       APIConfig       `yaml:"api,omitempty"`
}

type TelegramConfig struct {
	Token         string `yaml:"token"`
	AllowedUserID int64  `yaml:"allowed_user_id"`
}

type RCConfig struct {
	BaseFolder          string               `yaml:"base_folder"`
	PermissionMode      string               `yaml:"permission_mode,omitempty"`
	AutoRestart         bool                 `yaml:"auto_restart,omitempty"`
	MaxRestarts         int                  `yaml:"max_restarts,omitempty"`
	RestartDelaySeconds int                  `yaml:"restart_delay_seconds,omitempty"`
	Notifications       *NotificationsConfig `yaml:"notifications,omitempty"`
}

type ProvidersConfig struct {
	Claude ClaudeProviderConfig `yaml:"claude,omitempty"`
	Codex  CodexProviderConfig  `yaml:"codex,omitempty"`
	Gemini GeminiProviderConfig `yaml:"gemini,omitempty"`
}

type ClaudeProviderConfig struct {
	Chat    ProviderChatConfig    `yaml:"chat,omitempty"`
	Runtime ProviderRuntimeConfig `yaml:"runtime,omitempty"`
}

type CodexProviderConfig struct {
	Chat ProviderChatConfig `yaml:"chat,omitempty"`
}

type GeminiProviderConfig struct {
	Chat ProviderChatConfig `yaml:"chat,omitempty"`
}

type ProviderChatConfig struct {
	PermissionMode string `yaml:"permission_mode,omitempty"`
}

type ProviderRuntimeConfig struct {
	AutoRestart         *bool                `yaml:"auto_restart,omitempty"`
	MaxRestarts         *int                 `yaml:"max_restarts,omitempty"`
	RestartDelaySeconds *int                 `yaml:"restart_delay_seconds,omitempty"`
	Notifications       *NotificationsConfig `yaml:"notifications,omitempty"`
}

type RuntimeSettings struct {
	BaseFolder    string
	AutoRestart   bool
	MaxRestarts   int
	RestartDelay  time.Duration
	Notifications *NotificationsConfig
}

const (
	DefaultCodexPermissionMode = "workspace-write"
	PermissionModeBypass       = "bypassPermissions"
	PermissionModeReadOnly     = "read-only"
	PermissionModeWorkspace    = "workspace-write"
	PermissionModeDangerFull   = "danger-full-access"

	// Gemini CLI specific modes
	PermissionModeGeminiAutoEdit = "auto_edit"
	PermissionModeGeminiPlan     = "plan"
	PermissionModeGeminiYolo     = "yolo"
)

// ProjectConfig represents per-project settings in .rcod.yaml
type ProjectConfig struct {
	AutoRestart   *ProjectAutoRestart  `yaml:"auto_restart,omitempty"`
	Prompt        string               `yaml:"prompt,omitempty"`
	MaxDuration   Duration             `yaml:"max_duration,omitempty"`
	Notifications *NotificationsConfig `yaml:"notifications,omitempty"`
}

// PatternConfig defines a single notification pattern to match in session output.
type PatternConfig struct {
	Name  string `yaml:"name"`
	Regex string `yaml:"regex"`
	Once  bool   `yaml:"once,omitempty"`
}

// NotificationsConfig defines session event notification settings.
type NotificationsConfig struct {
	IdleTimeout            Duration        `yaml:"idle_timeout,omitempty"`
	ProgressUpdateInterval Duration        `yaml:"progress_update_interval,omitempty"`
	Patterns               []PatternConfig `yaml:"patterns,omitempty"`
}

// ResolveNotifications returns the effective notifications config.
// Project-level config takes precedence over global when non-nil.
func ResolveNotifications(global, project *NotificationsConfig) *NotificationsConfig {
	if project != nil {
		return project
	}
	return global
}

type ProjectAutoRestart struct {
	Enabled     bool     `yaml:"enabled"`
	MaxAttempts int      `yaml:"max_attempts"`
	Delay       Duration `yaml:"delay"`
}

// Duration is a wrapper around time.Duration for YAML marshaling/unmarshaling
type Duration time.Duration

func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	var s string
	if err := value.Decode(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(dur)
	return nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &cfg, nil
}

func (c *Config) Validate() error {
	hasTelegram := c.Telegram.Token != "" && c.Telegram.AllowedUserID != 0
	hasAPI := c.API.Port > 0

	if !hasTelegram && !hasAPI {
		return fmt.Errorf("at least one of telegram or api must be configured")
	}
	if c.Telegram.Token != "" && c.Telegram.AllowedUserID == 0 {
		return fmt.Errorf("telegram.allowed_user_id is required when telegram.token is set")
	}
	if c.Telegram.AllowedUserID != 0 && c.Telegram.Token == "" {
		return fmt.Errorf("telegram.token is required when telegram.allowed_user_id is set")
	}
	if c.RC.BaseFolder == "" {
		return fmt.Errorf("rc.base_folder is required")
	}

	info, err := os.Stat(c.RC.BaseFolder)
	if err != nil {
		return fmt.Errorf("rc.base_folder %q: %w", c.RC.BaseFolder, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("rc.base_folder %q is not a directory", c.RC.BaseFolder)
	}

	if c.RC.MaxRestarts < 0 {
		return fmt.Errorf("rc.max_restarts must be >= 0")
	}
	if c.RC.RestartDelaySeconds < 0 {
		return fmt.Errorf("rc.restart_delay_seconds must be >= 0")
	}
	if err := ValidateCodexPermissionMode(c.RC.PermissionMode); err != nil {
		return fmt.Errorf("rc.permission_mode: %w", err)
	}

	if c.RC.Notifications != nil {
		if err := c.RC.Notifications.Validate(); err != nil {
			return fmt.Errorf("rc.notifications: %w", err)
		}
	}
	if c.API.Auth != nil {
		if err := c.API.Auth.Validate(); err != nil {
			return fmt.Errorf("api.auth: %w", err)
		}
	}
	if err := c.Providers.Validate(); err != nil {
		return fmt.Errorf("providers: %w", err)
	}

	return nil
}

func NormalizeCodexPermissionMode(permissionMode string) string {
	permissionMode = strings.TrimSpace(permissionMode)
	if permissionMode == "" {
		return DefaultCodexPermissionMode
	}
	return permissionMode
}

func ValidateCodexPermissionMode(permissionMode string) error {
	switch NormalizeCodexPermissionMode(permissionMode) {
	case PermissionModeBypass, PermissionModeReadOnly, PermissionModeWorkspace, PermissionModeDangerFull:
		return nil
	default:
		return fmt.Errorf("must be one of %q, %q, %q, or %q", PermissionModeBypass, PermissionModeReadOnly, PermissionModeWorkspace, PermissionModeDangerFull)
	}
}

func ValidateGeminiPermissionMode(permissionMode string) error {
	switch NormalizeCodexPermissionMode(permissionMode) {
	case PermissionModeBypass, PermissionModeReadOnly, PermissionModeWorkspace, PermissionModeDangerFull,
		PermissionModeGeminiAutoEdit, PermissionModeGeminiPlan, PermissionModeGeminiYolo:
		return nil
	default:
		return fmt.Errorf("must be one of %q, %q, %q, %q, %q, %q, or %q",
			PermissionModeBypass, PermissionModeReadOnly, PermissionModeWorkspace, PermissionModeDangerFull,
			PermissionModeGeminiAutoEdit, PermissionModeGeminiPlan, PermissionModeGeminiYolo)
	}
}

func (c APIConfig) HasExternalAuth() bool {
	return c.Auth != nil && (c.Auth.OIDC != nil || c.Auth.GitHub != nil)
}

func (c *APIAuthConfig) Validate() error {
	if c == nil {
		return nil
	}
	if len(strings.TrimSpace(c.SessionSecret)) < 32 {
		return fmt.Errorf("session_secret must be at least 32 characters")
	}

	providers := 0
	if c.OIDC != nil {
		providers++
		if err := c.OIDC.Validate(); err != nil {
			return fmt.Errorf("oidc: %w", err)
		}
	}
	if c.GitHub != nil {
		providers++
		if err := c.GitHub.Validate(); err != nil {
			return fmt.Errorf("github: %w", err)
		}
	}
	if providers == 0 {
		return fmt.Errorf("one auth provider must be configured")
	}
	if providers > 1 {
		return fmt.Errorf("configure only one auth provider at a time")
	}
	return nil
}

func (c *OIDCAuthConfig) Validate() error {
	if c == nil {
		return nil
	}
	if strings.TrimSpace(c.IssuerURL) == "" {
		return fmt.Errorf("issuer_url is required")
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("client_id is required")
	}
	if strings.TrimSpace(c.ClientSecret) == "" {
		return fmt.Errorf("client_secret is required")
	}
	if strings.TrimSpace(c.RedirectURL) == "" {
		return fmt.Errorf("redirect_url is required")
	}
	return nil
}

func (c *GitHubAuthConfig) Validate() error {
	if c == nil {
		return nil
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("client_id is required")
	}
	if strings.TrimSpace(c.ClientSecret) == "" {
		return fmt.Errorf("client_secret is required")
	}
	if strings.TrimSpace(c.RedirectURL) == "" {
		return fmt.Errorf("redirect_url is required")
	}
	return nil
}

func (p ProvidersConfig) Validate() error {
	if err := p.Claude.Validate(); err != nil {
		return fmt.Errorf("claude: %w", err)
	}
	if err := p.Codex.Validate(); err != nil {
		return fmt.Errorf("codex: %w", err)
	}
	if err := p.Gemini.Validate(); err != nil {
		return fmt.Errorf("gemini: %w", err)
	}
	return nil
}

func (p ClaudeProviderConfig) Validate() error {
	if err := p.Chat.validateWith(ValidateCodexPermissionMode); err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	if err := p.Runtime.Validate(); err != nil {
		return fmt.Errorf("runtime: %w", err)
	}
	return nil
}

func (p CodexProviderConfig) Validate() error {
	if err := p.Chat.validateWith(ValidateCodexPermissionMode); err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	return nil
}

func (p GeminiProviderConfig) Validate() error {
	if err := p.Chat.validateWith(ValidateGeminiPermissionMode); err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	return nil
}

func (p ProviderChatConfig) validateWith(validator func(string) error) error {
	if strings.TrimSpace(p.PermissionMode) == "" {
		return nil
	}
	return validator(p.PermissionMode)
}

func (p ProviderRuntimeConfig) Validate() error {
	if p.MaxRestarts != nil && *p.MaxRestarts < 0 {
		return fmt.Errorf("max_restarts must be >= 0")
	}
	if p.RestartDelaySeconds != nil && *p.RestartDelaySeconds < 0 {
		return fmt.Errorf("restart_delay_seconds must be >= 0")
	}
	if p.Notifications != nil {
		if err := p.Notifications.Validate(); err != nil {
			return fmt.Errorf("notifications: %w", err)
		}
	}
	return nil
}

func (c *Config) ClaudeRuntimeSettings() RuntimeSettings {
	runtimeCfg := c.Providers.Claude.Runtime

	autoRestart := c.RC.AutoRestart
	if runtimeCfg.AutoRestart != nil {
		autoRestart = *runtimeCfg.AutoRestart
	}

	maxRestarts := c.RC.MaxRestarts
	if runtimeCfg.MaxRestarts != nil {
		maxRestarts = *runtimeCfg.MaxRestarts
	}

	restartDelaySeconds := c.RC.RestartDelaySeconds
	if runtimeCfg.RestartDelaySeconds != nil {
		restartDelaySeconds = *runtimeCfg.RestartDelaySeconds
	}

	notifications := c.RC.Notifications
	if runtimeCfg.Notifications != nil {
		notifications = runtimeCfg.Notifications
	}

	return RuntimeSettings{
		BaseFolder:    c.RC.BaseFolder,
		AutoRestart:   autoRestart,
		MaxRestarts:   maxRestarts,
		RestartDelay:  time.Duration(restartDelaySeconds) * time.Second,
		Notifications: notifications,
	}
}

func (c *Config) ClaudeChatPermissionMode() string {
	if mode := strings.TrimSpace(c.Providers.Claude.Chat.PermissionMode); mode != "" {
		return NormalizeCodexPermissionMode(mode)
	}
	return NormalizeCodexPermissionMode(c.RC.PermissionMode)
}

func (c *Config) CodexChatPermissionMode() string {
	if mode := strings.TrimSpace(c.Providers.Codex.Chat.PermissionMode); mode != "" {
		return NormalizeCodexPermissionMode(mode)
	}
	return NormalizeCodexPermissionMode(c.RC.PermissionMode)
}

func (c *Config) GeminiChatPermissionMode() string {
	if mode := strings.TrimSpace(c.Providers.Gemini.Chat.PermissionMode); mode != "" {
		return NormalizeCodexPermissionMode(mode)
	}
	return NormalizeCodexPermissionMode(c.RC.PermissionMode)
}

func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// LoadProjectConfig loads .rcod.yaml from the given directory if it exists
func LoadProjectConfig(dir string) (*ProjectConfig, error) {
	path := filepath.Join(dir, ".rcod.yaml")
	if !Exists(path) {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading project config: %w", err)
	}

	var cfg ProjectConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing project config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid project config: %w", err)
	}

	return &cfg, nil
}

func (p *ProjectConfig) Validate() error {
	if p.AutoRestart != nil {
		if p.AutoRestart.MaxAttempts < 0 {
			return fmt.Errorf("auto_restart.max_attempts must be >= 0")
		}
		if p.AutoRestart.Delay < 0 {
			return fmt.Errorf("auto_restart.delay must be >= 0")
		}
	}
	if p.MaxDuration < 0 {
		return fmt.Errorf("max_duration must be >= 0")
	}
	if p.Notifications != nil {
		if err := p.Notifications.Validate(); err != nil {
			return fmt.Errorf("notifications: %w", err)
		}
	}
	return nil
}

func (n *NotificationsConfig) Validate() error {
	if n.IdleTimeout < 0 {
		return fmt.Errorf("idle_timeout must be >= 0")
	}
	if n.ProgressUpdateInterval < 0 {
		return fmt.Errorf("progress_update_interval must be >= 0")
	}
	seen := make(map[string]bool)
	for i, p := range n.Patterns {
		if p.Name == "" {
			return fmt.Errorf("patterns[%d].name is required", i)
		}
		if p.Regex == "" {
			return fmt.Errorf("patterns[%d].regex is required", i)
		}
		if _, err := regexp.Compile(p.Regex); err != nil {
			return fmt.Errorf("patterns[%d].regex is invalid: %w", i, err)
		}
		if seen[p.Name] {
			return fmt.Errorf("patterns[%d].name %q is duplicated", i, p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}
