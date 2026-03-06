package config

import (
	"os"
	"path/filepath"
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
