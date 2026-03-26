package runtimepaths

import (
	"path/filepath"
	"testing"
)

func TestDefaultMacOSPrefixPrefersHomebrewOnAppleSilicon(t *testing.T) {
	got := DefaultMacOSPrefix("arm64", false)

	if got != "/opt/homebrew" {
		t.Fatalf("DefaultMacOSPrefix() = %q", got)
	}
}

func TestDefaultMacOSPrefixFallsBackToUsrLocalOnIntel(t *testing.T) {
	got := DefaultMacOSPrefix("x86_64", false)

	if got != "/usr/local" {
		t.Fatalf("DefaultMacOSPrefix() = %q", got)
	}
}

func TestDefaultMacOSLaunchdLayoutForDaemon(t *testing.T) {
	got := DefaultMacOSLaunchdLayout(LaunchdModeDaemon, "x86_64", "/Users/alice", false, false)

	if got.Label != "ai.zevro.rcod" {
		t.Fatalf("Label = %q", got.Label)
	}
	if got.PlistPath != filepath.Join("/Library", "LaunchDaemons", "ai.zevro.rcod.plist") {
		t.Fatalf("PlistPath = %q", got.PlistPath)
	}
	if got.BinPath != filepath.Join("/usr", "local", "bin", "rcod") {
		t.Fatalf("BinPath = %q", got.BinPath)
	}
	if got.ConfigPath != filepath.Join("/usr", "local", "etc", "rcod", "config.yaml") {
		t.Fatalf("ConfigPath = %q", got.ConfigPath)
	}
	if got.StateDir != filepath.Join("/Users", "alice", "Library", "Application Support", "rcod") {
		t.Fatalf("StateDir = %q", got.StateDir)
	}
	if got.LogDir != filepath.Join("/Users", "alice", "Library", "Logs", "rcod") {
		t.Fatalf("LogDir = %q", got.LogDir)
	}
}

func TestDefaultMacOSLaunchdLayoutForDaemonOnAppleSilicon(t *testing.T) {
	got := DefaultMacOSLaunchdLayout(LaunchdModeDaemon, "arm64", "/Users/alice", false, false)

	if got.BinPath != filepath.Join("/opt", "homebrew", "bin", "rcod") {
		t.Fatalf("BinPath = %q", got.BinPath)
	}
	if got.ConfigPath != filepath.Join("/opt", "homebrew", "etc", "rcod", "config.yaml") {
		t.Fatalf("ConfigPath = %q", got.ConfigPath)
	}
}

func TestDefaultMacOSLaunchdLayoutForAgentBuildsIntoLocalBin(t *testing.T) {
	got := DefaultMacOSLaunchdLayout(LaunchdModeAgent, "arm64", "/Users/alice", false, false)

	if got.Label != "ai.zevro.rcod.agent" {
		t.Fatalf("Label = %q", got.Label)
	}
	if got.PlistPath != filepath.Join("/Users", "alice", "Library", "LaunchAgents", "ai.zevro.rcod.agent.plist") {
		t.Fatalf("PlistPath = %q", got.PlistPath)
	}
	if got.BinPath != filepath.Join("/Users", "alice", ".local", "bin", "rcod") {
		t.Fatalf("BinPath = %q", got.BinPath)
	}
	if got.ConfigPath != filepath.Join("/Users", "alice", ".config", "rcod", "config.yaml") {
		t.Fatalf("ConfigPath = %q", got.ConfigPath)
	}
}

func TestDefaultMacOSLaunchdLayoutForAgentSkipBuildUsesInstalledBinary(t *testing.T) {
	got := DefaultMacOSLaunchdLayout(LaunchdModeAgent, "arm64", "/Users/alice", false, true)

	if got.BinPath != filepath.Join("/opt", "homebrew", "bin", "rcod") {
		t.Fatalf("BinPath = %q", got.BinPath)
	}
}
