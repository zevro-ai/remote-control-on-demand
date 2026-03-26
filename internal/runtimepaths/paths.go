package runtimepaths

import (
	"path/filepath"
	"strings"
)

// ResolveStateDir returns the directory used to persist runtime state files.
// When no override is provided, state files live next to the config file.
func ResolveStateDir(configPath, override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return filepath.Clean(override)
	}

	configDir := filepath.Dir(strings.TrimSpace(configPath))
	if configDir == "." {
		return "."
	}

	return filepath.Clean(configDir)
}

func ResolveStatePath(configPath, override, filename string) string {
	return filepath.Join(ResolveStateDir(configPath, override), filename)
}
