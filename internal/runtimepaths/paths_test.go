package runtimepaths

import (
	"path/filepath"
	"testing"
)

func TestResolveStateDirDefaultsToConfigDirectory(t *testing.T) {
	configPath := filepath.Join("/etc", "rcod", "config.yaml")

	got := ResolveStateDir(configPath, "")

	if got != filepath.Join("/etc", "rcod") {
		t.Fatalf("ResolveStateDir() = %q", got)
	}
}

func TestResolveStateDirUsesOverride(t *testing.T) {
	got := ResolveStateDir("/etc/rcod/config.yaml", "/var/lib/rcod")

	if got != filepath.Join("/var", "lib", "rcod") {
		t.Fatalf("ResolveStateDir() = %q", got)
	}
}

func TestResolveStateDirDefaultsToCurrentDirectoryForBareConfigFilename(t *testing.T) {
	got := ResolveStateDir("config.yaml", "")

	if got != "." {
		t.Fatalf("ResolveStateDir() = %q", got)
	}
}

func TestResolveStatePathUsesResolvedDirectory(t *testing.T) {
	got := ResolveStatePath("/etc/rcod/config.yaml", "/var/lib/rcod", "sessions.json")

	if got != filepath.Join("/var", "lib", "rcod", "sessions.json") {
		t.Fatalf("ResolveStatePath() = %q", got)
	}
}
