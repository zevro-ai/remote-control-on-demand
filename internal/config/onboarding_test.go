package config

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunOnboardingEnablesDashboardAndWritesConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, output, exitCode, err := runOnboardingForTest(t, onboardingTestCase{
		configPath:     configPath,
		cwd:            tmpDir,
		stdin:          "telegram-token\n123456\n\n\n\n",
		selections:     []int{1, 1, 0, 0, 0},
		generatedToken: "generated-dashboard-token",
	})
	if err != nil {
		t.Fatalf("RunOnboarding returned error: %v", err)
	}
	if exitCode != -1 {
		t.Fatalf("expected onboarding not to exit, got exit code %d", exitCode)
	}

	if cfg.API.Port != defaultDashboardPort {
		t.Fatalf("expected dashboard port %d, got %d", defaultDashboardPort, cfg.API.Port)
	}
	if cfg.API.Token != "generated-dashboard-token" {
		t.Fatalf("expected generated dashboard token to be saved, got %q", cfg.API.Token)
	}
	if cfg.CodexChatPermissionMode() != DefaultCodexPermissionMode {
		t.Fatalf("expected codex permission mode %q, got %q", DefaultCodexPermissionMode, cfg.CodexChatPermissionMode())
	}
	if cfg.ClaudeRuntimeSettings().AutoRestart {
		t.Fatal("expected onboarding fixture to keep claude runtime auto_restart disabled")
	}

	if !strings.Contains(output, "Dashboard URL: http://127.0.0.1:3001/") {
		t.Fatalf("expected output to include dashboard URL, got %q", output)
	}
	if !strings.Contains(output, "Bearer token saved to config.yaml.") {
		t.Fatalf("expected output to mention saved bearer token, got %q", output)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading saved config: %v", err)
	}
	configYAML := string(data)
	if !strings.Contains(configYAML, "api:\n") {
		t.Fatalf("expected saved config to include api section, got %q", configYAML)
	}
	if !strings.Contains(configYAML, "port: 3001") {
		t.Fatalf("expected saved config to include dashboard port, got %q", configYAML)
	}
	if !strings.Contains(configYAML, "token: generated-dashboard-token") {
		t.Fatalf("expected saved config to include dashboard token, got %q", configYAML)
	}
	if !strings.Contains(configYAML, "providers:\n") || !strings.Contains(configYAML, "claude:\n") || !strings.Contains(configYAML, "codex:\n") {
		t.Fatalf("expected saved config to include provider sections, got %q", configYAML)
	}
}

func TestRunOnboardingCanLeaveDashboardDisabled(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, output, exitCode, err := runOnboardingForTest(t, onboardingTestCase{
		configPath: configPath,
		cwd:        tmpDir,
		stdin:      "telegram-token\n123456\n\n",
		selections: []int{1, 1, 1, 0},
	})
	if err != nil {
		t.Fatalf("RunOnboarding returned error: %v", err)
	}
	if exitCode != -1 {
		t.Fatalf("expected onboarding not to exit, got exit code %d", exitCode)
	}

	if cfg.API.Port != 0 {
		t.Fatalf("expected dashboard to remain disabled, got port %d", cfg.API.Port)
	}
	if cfg.API.Token != "" {
		t.Fatalf("expected no dashboard token when disabled, got %q", cfg.API.Token)
	}

	if strings.Contains(output, "Dashboard URL:") {
		t.Fatalf("did not expect dashboard URL in output when disabled, got %q", output)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading saved config: %v", err)
	}
	if strings.Contains(string(data), "api:\n") {
		t.Fatalf("expected saved config to omit api section when disabled, got %q", string(data))
	}
}

func TestRunOnboardingEnablesDashboardWithoutToken(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, output, exitCode, err := runOnboardingForTest(t, onboardingTestCase{
		configPath: configPath,
		cwd:        tmpDir,
		stdin:      "telegram-token\n123456\n\n\n",
		selections: []int{1, 1, 0, 1, 0},
	})
	if err != nil {
		t.Fatalf("RunOnboarding returned error: %v", err)
	}
	if exitCode != -1 {
		t.Fatalf("expected onboarding not to exit, got exit code %d", exitCode)
	}

	if cfg.API.Port != defaultDashboardPort {
		t.Fatalf("expected dashboard port %d, got %d", defaultDashboardPort, cfg.API.Port)
	}
	if cfg.API.Token != "" {
		t.Fatalf("expected no dashboard token, got %q", cfg.API.Token)
	}

	if !strings.Contains(output, "Dashboard URL: http://127.0.0.1:3001/") {
		t.Fatalf("expected output to include dashboard URL, got %q", output)
	}
	if strings.Contains(output, "token protected") {
		t.Fatalf("did not expect token-protected output, got %q", output)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading saved config: %v", err)
	}
	configYAML := string(data)
	if !strings.Contains(configYAML, "api:\n") {
		t.Fatalf("expected saved config to include api section, got %q", configYAML)
	}
	if !strings.Contains(configYAML, "port: 3001") {
		t.Fatalf("expected saved config to include dashboard port, got %q", configYAML)
	}
	if strings.Contains(configYAML, "api:\n    port: 3001\n    token:") {
		t.Fatalf("expected saved config to omit dashboard token, got %q", configYAML)
	}
}

func TestRunOnboardingRejectsShortManualDashboardToken(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, output, exitCode, err := runOnboardingForTest(t, onboardingTestCase{
		configPath: configPath,
		cwd:        tmpDir,
		stdin:      "telegram-token\n123456\n\n\nshort-token\n",
		selections: []int{1, 1, 0, 0},
	})
	if err == nil {
		t.Fatal("expected onboarding to reject short manual dashboard token")
	}
	if !strings.Contains(err.Error(), "dashboard token must be at least 32 characters") {
		t.Fatalf("expected dashboard token validation error, got %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config on validation error, got %#v", cfg)
	}
	if exitCode != -1 {
		t.Fatalf("expected onboarding not to exit, got exit code %d", exitCode)
	}
	if strings.Contains(output, "Config saved to") {
		t.Fatalf("did not expect config to be saved on validation error, got %q", output)
	}
	if _, statErr := os.Stat(configPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected config file not to be created, got stat err %v", statErr)
	}
}

func TestRunOnboardingWarnsForPrivilegedDashboardPort(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg, output, exitCode, err := runOnboardingForTest(t, onboardingTestCase{
		configPath: configPath,
		cwd:        tmpDir,
		stdin:      "telegram-token\n123456\n\n80\n\n",
		selections: []int{1, 1, 0, 1, 0},
	})
	if err != nil {
		t.Fatalf("RunOnboarding returned error: %v", err)
	}
	if exitCode != -1 {
		t.Fatalf("expected onboarding not to exit, got exit code %d", exitCode)
	}
	if cfg.API.Port != 80 {
		t.Fatalf("expected dashboard port 80, got %d", cfg.API.Port)
	}
	if !strings.Contains(output, "Warning: port 80 is privileged and may require root on Linux/macOS.") {
		t.Fatalf("expected privileged port warning, got %q", output)
	}
}

type onboardingTestCase struct {
	configPath     string
	cwd            string
	stdin          string
	selections     []int
	generatedToken string
}

type onboardingExitPanic struct {
	code int
}

func runOnboardingForTest(t *testing.T, tc onboardingTestCase) (*Config, string, int, error) {
	t.Helper()

	oldStdin := os.Stdin
	oldStdout := os.Stdout
	oldSelect := onboardingSelectOption
	oldGetwd := onboardingGetwd
	oldExit := onboardingExit
	oldGenerateToken := onboardingGenerateToken

	t.Cleanup(func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
		onboardingSelectOption = oldSelect
		onboardingGetwd = oldGetwd
		onboardingExit = oldExit
		onboardingGenerateToken = oldGenerateToken
	})

	inputPath := filepath.Join(t.TempDir(), "stdin.txt")
	if err := os.WriteFile(inputPath, []byte(tc.stdin), 0600); err != nil {
		t.Fatalf("writing stdin fixture: %v", err)
	}
	inputFile, err := os.Open(inputPath)
	if err != nil {
		t.Fatalf("opening stdin fixture: %v", err)
	}
	defer inputFile.Close()
	os.Stdin = inputFile

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}
	os.Stdout = stdoutWriter

	selections := append([]int(nil), tc.selections...)
	onboardingSelectOption = func(labels []string, defaultIdx int) (int, error) {
		if len(selections) == 0 {
			return 0, errors.New("unexpected onboarding selection")
		}
		choice := selections[0]
		selections = selections[1:]
		return choice, nil
	}
	onboardingGetwd = func() (string, error) {
		return tc.cwd, nil
	}
	onboardingGenerateToken = func() (string, error) {
		return tc.generatedToken, nil
	}

	exitCode := -1
	onboardingExit = func(code int) {
		exitCode = code
		panic(onboardingExitPanic{code: code})
	}

	var cfg *Config
	callErr := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				exitPanic, ok := r.(onboardingExitPanic)
				if !ok {
					panic(r)
				}
				exitCode = exitPanic.code
			}
		}()
		cfg, err = RunOnboarding(tc.configPath)
		return err
	}()

	if err := stdoutWriter.Close(); err != nil {
		t.Fatalf("closing stdout writer: %v", err)
	}
	output, err := io.ReadAll(stdoutReader)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}

	if len(selections) != 0 {
		t.Fatalf("unused onboarding selections left: %v", selections)
	}

	return cfg, string(output), exitCode, callErr
}
