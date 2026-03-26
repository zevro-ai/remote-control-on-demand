package runtimepaths

import (
	"fmt"
	"path/filepath"
)

type LaunchdMode string

const (
	LaunchdModeAgent  LaunchdMode = "agent"
	LaunchdModeDaemon LaunchdMode = "daemon"
)

type MacOSLaunchdLayout struct {
	Label      string
	PlistPath  string
	BinPath    string
	ConfigPath string
	StateDir   string
	LogDir     string
}

// DefaultMacOSPrefix returns the preferred install prefix for macOS binaries and config.
// Apple Silicon defaults to /opt/homebrew; Intel defaults to /usr/local.
func DefaultMacOSPrefix(machine string, optHomebrewExists bool) string {
	if optHomebrewExists || machine == "arm64" {
		return "/opt/homebrew"
	}

	return "/usr/local"
}

// DefaultMacOSLaunchdLayout returns the recommended macOS paths for launchd installs.
func DefaultMacOSLaunchdLayout(mode LaunchdMode, machine, userHome string, optHomebrewExists, skipBuild bool) MacOSLaunchdLayout {
	prefix := DefaultMacOSPrefix(machine, optHomebrewExists)
	layout := MacOSLaunchdLayout{
		StateDir: filepath.Join(userHome, "Library", "Application Support", "rcod"),
		LogDir:   filepath.Join(userHome, "Library", "Logs", "rcod"),
	}

	switch mode {
	case LaunchdModeDaemon:
		layout.Label = "ai.zevro.rcod"
		layout.PlistPath = filepath.Join("/Library", "LaunchDaemons", "ai.zevro.rcod.plist")
		layout.BinPath = filepath.Join(prefix, "bin", "rcod")
		layout.ConfigPath = filepath.Join(prefix, "etc", "rcod", "config.yaml")
	case LaunchdModeAgent:
		layout.Label = "ai.zevro.rcod.agent"
		layout.PlistPath = filepath.Join(userHome, "Library", "LaunchAgents", "ai.zevro.rcod.agent.plist")
		layout.ConfigPath = filepath.Join(userHome, ".config", "rcod", "config.yaml")
		if skipBuild {
			layout.BinPath = filepath.Join(prefix, "bin", "rcod")
		} else {
			layout.BinPath = filepath.Join(userHome, ".local", "bin", "rcod")
		}
	default:
		panic(fmt.Sprintf("runtimepaths: unknown LaunchdMode %q", mode))
	}

	return layout
}
