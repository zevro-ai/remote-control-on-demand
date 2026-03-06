package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Telegram TelegramConfig `yaml:"telegram"`
	RC       RCConfig       `yaml:"rc"`
}

type TelegramConfig struct {
	Token         string `yaml:"token"`
	AllowedUserID int64  `yaml:"allowed_user_id"`
}

type RCConfig struct {
	BaseFolder          string               `yaml:"base_folder"`
	AutoRestart         bool                 `yaml:"auto_restart"`
	MaxRestarts         int                  `yaml:"max_restarts"`
	RestartDelaySeconds int                  `yaml:"restart_delay_seconds"`
	Notifications       *NotificationsConfig `yaml:"notifications,omitempty"`
}

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
	if c.Telegram.Token == "" {
		return fmt.Errorf("telegram.token is required")
	}
	if c.Telegram.AllowedUserID == 0 {
		return fmt.Errorf("telegram.allowed_user_id is required")
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

	if c.RC.Notifications != nil {
		if err := c.RC.Notifications.Validate(); err != nil {
			return fmt.Errorf("rc.notifications: %w", err)
		}
	}

	return nil
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
