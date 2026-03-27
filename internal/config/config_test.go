package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadProjectConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rcod-project-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	t.Run("no config file", func(t *testing.T) {
		cfg, err := LoadProjectConfig(tmpDir)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if cfg != nil {
			t.Error("expected nil config")
		}
	})

	t.Run("valid config file", func(t *testing.T) {
		yamlContent := `
auto_restart:
  enabled: true
  max_attempts: 5
  delay: 10s
prompt: "Test prompt"
max_duration: 1h
`
		err := os.WriteFile(filepath.Join(tmpDir, ".rcod.yaml"), []byte(yamlContent), 0644)
		if err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadProjectConfig(tmpDir)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if cfg.AutoRestart == nil || !cfg.AutoRestart.Enabled {
			t.Error("expected auto_restart enabled")
		}
		if cfg.AutoRestart.MaxAttempts != 5 {
			t.Errorf("expected max_attempts 5, got %d", cfg.AutoRestart.MaxAttempts)
		}
		if time.Duration(cfg.AutoRestart.Delay) != 10*time.Second {
			t.Errorf("expected delay 10s, got %v", cfg.AutoRestart.Delay)
		}
		if cfg.Prompt != "Test prompt" {
			t.Errorf("expected prompt 'Test prompt', got %q", cfg.Prompt)
		}
		if time.Duration(cfg.MaxDuration) != time.Hour {
			t.Errorf("expected max_duration 1h, got %v", cfg.MaxDuration)
		}
	})
}

func TestNotificationsConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rcod-notif-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	t.Run("valid notifications config", func(t *testing.T) {
		yamlContent := `
notifications:
  idle_timeout: 5m
  progress_update_interval: 10m
  patterns:
    - name: "done"
      regex: "(?i)task completed"
      once: true
    - name: "error"
      regex: "ERROR:"
`
		err := os.WriteFile(filepath.Join(tmpDir, ".rcod.yaml"), []byte(yamlContent), 0644)
		if err != nil {
			t.Fatal(err)
		}

		cfg, err := LoadProjectConfig(tmpDir)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if cfg.Notifications == nil {
			t.Fatal("expected notifications config")
		}
		if time.Duration(cfg.Notifications.IdleTimeout) != 5*time.Minute {
			t.Errorf("expected idle_timeout 5m, got %v", cfg.Notifications.IdleTimeout)
		}
		if time.Duration(cfg.Notifications.ProgressUpdateInterval) != 10*time.Minute {
			t.Errorf("expected progress_update_interval 10m, got %v", cfg.Notifications.ProgressUpdateInterval)
		}
		if len(cfg.Notifications.Patterns) != 2 {
			t.Fatalf("expected 2 patterns, got %d", len(cfg.Notifications.Patterns))
		}
		if cfg.Notifications.Patterns[0].Name != "done" {
			t.Errorf("expected pattern name 'done', got %q", cfg.Notifications.Patterns[0].Name)
		}
		if !cfg.Notifications.Patterns[0].Once {
			t.Error("expected pattern 'done' to have once=true")
		}
		if cfg.Notifications.Patterns[1].Once {
			t.Error("expected pattern 'error' to have once=false")
		}
	})

	t.Run("invalid regex", func(t *testing.T) {
		yamlContent := `
notifications:
  patterns:
    - name: "bad"
      regex: "[invalid"
`
		err := os.WriteFile(filepath.Join(tmpDir, ".rcod.yaml"), []byte(yamlContent), 0644)
		if err != nil {
			t.Fatal(err)
		}
		_, err = LoadProjectConfig(tmpDir)
		if err == nil {
			t.Error("expected error for invalid regex")
		}
	})

	t.Run("duplicate pattern names", func(t *testing.T) {
		yamlContent := `
notifications:
  patterns:
    - name: "dup"
      regex: "a"
    - name: "dup"
      regex: "b"
`
		err := os.WriteFile(filepath.Join(tmpDir, ".rcod.yaml"), []byte(yamlContent), 0644)
		if err != nil {
			t.Fatal(err)
		}
		_, err = LoadProjectConfig(tmpDir)
		if err == nil {
			t.Error("expected error for duplicate pattern names")
		}
	})

	t.Run("empty pattern name", func(t *testing.T) {
		yamlContent := `
notifications:
  patterns:
    - name: ""
      regex: "test"
`
		err := os.WriteFile(filepath.Join(tmpDir, ".rcod.yaml"), []byte(yamlContent), 0644)
		if err != nil {
			t.Fatal(err)
		}
		_, err = LoadProjectConfig(tmpDir)
		if err == nil {
			t.Error("expected error for empty pattern name")
		}
	})
}

func TestResolveNotifications(t *testing.T) {
	global := &NotificationsConfig{
		IdleTimeout: Duration(5 * time.Minute),
	}
	project := &NotificationsConfig{
		IdleTimeout: Duration(10 * time.Minute),
	}

	t.Run("project overrides global", func(t *testing.T) {
		result := ResolveNotifications(global, project)
		if result != project {
			t.Error("expected project config to take precedence")
		}
	})

	t.Run("nil project falls through to global", func(t *testing.T) {
		result := ResolveNotifications(global, nil)
		if result != global {
			t.Error("expected global config when project is nil")
		}
	})

	t.Run("both nil returns nil", func(t *testing.T) {
		result := ResolveNotifications(nil, nil)
		if result != nil {
			t.Error("expected nil when both are nil")
		}
	})

	t.Run("progress interval preserved", func(t *testing.T) {
		globalWithProgress := &NotificationsConfig{
			ProgressUpdateInterval: Duration(10 * time.Minute),
		}
		result := ResolveNotifications(globalWithProgress, nil)
		if time.Duration(result.ProgressUpdateInterval) != 10*time.Minute {
			t.Fatalf("expected 10m progress interval, got %v", result.ProgressUpdateInterval)
		}
	})
}

func TestDuration_YAML(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		d := Duration(2 * time.Hour)
		data, err := d.MarshalYAML()
		if err != nil {
			t.Fatal(err)
		}
		if data.(string) != "2h0m0s" {
			t.Errorf("expected '2h0m0s', got %v", data)
		}
	})
}

func TestRedactToken(t *testing.T) {
	t.Run("short token", func(t *testing.T) {
		if got := redactToken("short"); got != "*****" {
			t.Fatalf("expected masked short token, got %q", got)
		}
	})

	t.Run("long token", func(t *testing.T) {
		if got := redactToken("1234567890"); got != "1234...7890" {
			t.Fatalf("expected redacted token, got %q", got)
		}
	})
}

func TestValidateCodexPermissionMode(t *testing.T) {
	validModes := []string{
		"",
		PermissionModeBypass,
		PermissionModeReadOnly,
		PermissionModeWorkspace,
		PermissionModeDangerFull,
	}

	for _, mode := range validModes {
		t.Run("valid "+NormalizeCodexPermissionMode(mode), func(t *testing.T) {
			if err := ValidateCodexPermissionMode(mode); err != nil {
				t.Fatalf("expected mode %q to be valid, got %v", mode, err)
			}
		})
	}

	t.Run("invalid mode", func(t *testing.T) {
		err := ValidateCodexPermissionMode("plan")
		if err == nil {
			t.Fatal("expected invalid mode error")
		}
		if !strings.Contains(err.Error(), PermissionModeWorkspace) {
			t.Fatalf("expected allowed values in error, got %v", err)
		}
	})
}

func TestConfigValidatePermissionMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rcod-config-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &Config{
		Telegram: TelegramConfig{
			Token:         "token",
			AllowedUserID: 123,
		},
		RC: RCConfig{
			BaseFolder:          tmpDir,
			PermissionMode:      "plan",
			AutoRestart:         true,
			MaxRestarts:         3,
			RestartDelaySeconds: 5,
		},
	}

	err = cfg.Validate()
	if err == nil {
		t.Fatal("expected invalid permission mode error")
	}
	if !strings.Contains(err.Error(), "rc.permission_mode") {
		t.Fatalf("expected rc.permission_mode error, got %v", err)
	}
}

func TestAPIAuthConfigValidation(t *testing.T) {
	t.Run("requires long session secret", func(t *testing.T) {
		cfg := &APIAuthConfig{
			SessionSecret: "short",
			GitHub: &GitHubAuthConfig{
				ClientID:     "client",
				ClientSecret: "secret",
				RedirectURL:  "http://localhost:3001/api/auth/callback",
			},
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "session_secret") {
			t.Fatalf("expected session_secret validation error, got %v", err)
		}
	})

	t.Run("requires exactly one provider", func(t *testing.T) {
		cfg := &APIAuthConfig{
			SessionSecret: strings.Repeat("a", 32),
			OIDC: &OIDCAuthConfig{
				IssuerURL:    "https://auth.example.com/application/o/demo/",
				ClientID:     "oidc-client",
				ClientSecret: "oidc-secret",
				RedirectURL:  "http://localhost:3001/api/auth/callback",
			},
			GitHub: &GitHubAuthConfig{
				ClientID:     "github-client",
				ClientSecret: "github-secret",
				RedirectURL:  "http://localhost:3001/api/auth/callback",
			},
		}
		if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "only one auth provider") {
			t.Fatalf("expected single-provider validation error, got %v", err)
		}
	})

	t.Run("validates provider-specific required fields", func(t *testing.T) {
		cfg := &Config{
			Telegram: TelegramConfig{
				Token:         "token",
				AllowedUserID: 123,
			},
			RC: RCConfig{
				BaseFolder: t.TempDir(),
			},
			API: APIConfig{
				Port: 8080,
				Auth: &APIAuthConfig{
					SessionSecret: strings.Repeat("b", 32),
					OIDC: &OIDCAuthConfig{
						IssuerURL: "https://auth.example.com",
					},
				},
			},
		}
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "api.auth: oidc: client_id is required") {
			t.Fatalf("expected oidc validation error, got %v", err)
		}
	})
}

func TestConfigValidateProviderSpecificSettings(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &Config{
		Telegram: TelegramConfig{
			Token:         "token",
			AllowedUserID: 123,
		},
		RC: RCConfig{
			BaseFolder: tmpDir,
		},
		Providers: ProvidersConfig{
			Claude: ClaudeProviderConfig{
				Chat: ProviderChatConfig{
					PermissionMode: "plan",
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected invalid provider-specific permission mode error")
	}
	if !strings.Contains(err.Error(), "providers: claude: chat") {
		t.Fatalf("expected providers claude chat error, got %v", err)
	}
}

func TestConfigResolvedProviderSettings(t *testing.T) {
	cfg := &Config{
		RC: RCConfig{
			BaseFolder:          "/tmp/projects",
			PermissionMode:      PermissionModeReadOnly,
			AutoRestart:         true,
			MaxRestarts:         3,
			RestartDelaySeconds: 5,
			Notifications: &NotificationsConfig{
				IdleTimeout: Duration(5 * time.Minute),
			},
		},
		Providers: ProvidersConfig{
			Claude: ClaudeProviderConfig{
				Chat: ProviderChatConfig{
					PermissionMode: PermissionModeDangerFull,
				},
				Runtime: ProviderRuntimeConfig{
					AutoRestart:         boolPtr(false),
					MaxRestarts:         intPtr(9),
					RestartDelaySeconds: intPtr(12),
				},
			},
			Codex: CodexProviderConfig{
				Chat: ProviderChatConfig{
					PermissionMode: PermissionModeWorkspace,
				},
			},
		},
	}

	runtimeSettings := cfg.ClaudeRuntimeSettings()
	if runtimeSettings.BaseFolder != "/tmp/projects" {
		t.Fatalf("BaseFolder = %q", runtimeSettings.BaseFolder)
	}
	if runtimeSettings.AutoRestart {
		t.Fatal("expected provider runtime auto_restart override to disable restarts")
	}
	if runtimeSettings.MaxRestarts != 9 {
		t.Fatalf("MaxRestarts = %d, want 9", runtimeSettings.MaxRestarts)
	}
	if runtimeSettings.RestartDelay != 12*time.Second {
		t.Fatalf("RestartDelay = %v, want 12s", runtimeSettings.RestartDelay)
	}
	if runtimeSettings.Notifications == nil || time.Duration(runtimeSettings.Notifications.IdleTimeout) != 5*time.Minute {
		t.Fatalf("Notifications = %#v", runtimeSettings.Notifications)
	}
	if cfg.ClaudeChatPermissionMode() != PermissionModeDangerFull {
		t.Fatalf("ClaudeChatPermissionMode() = %q", cfg.ClaudeChatPermissionMode())
	}
	if cfg.CodexChatPermissionMode() != PermissionModeWorkspace {
		t.Fatalf("CodexChatPermissionMode() = %q", cfg.CodexChatPermissionMode())
	}
}

func TestConfigResolvedProviderSettingsFallbackToLegacyRC(t *testing.T) {
	cfg := &Config{
		RC: RCConfig{
			BaseFolder:          "/tmp/projects",
			PermissionMode:      PermissionModeWorkspace,
			AutoRestart:         true,
			MaxRestarts:         2,
			RestartDelaySeconds: 7,
		},
	}

	runtimeSettings := cfg.ClaudeRuntimeSettings()
	if !runtimeSettings.AutoRestart || runtimeSettings.MaxRestarts != 2 || runtimeSettings.RestartDelay != 7*time.Second {
		t.Fatalf("runtime settings = %#v", runtimeSettings)
	}
	if cfg.ClaudeChatPermissionMode() != PermissionModeWorkspace {
		t.Fatalf("ClaudeChatPermissionMode() = %q", cfg.ClaudeChatPermissionMode())
	}
	if cfg.CodexChatPermissionMode() != PermissionModeWorkspace {
		t.Fatalf("CodexChatPermissionMode() = %q", cfg.CodexChatPermissionMode())
	}
}
