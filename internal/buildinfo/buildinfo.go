package buildinfo

import (
	"fmt"
	"runtime/debug"
	"strings"
	"time"
)

var (
	Version   = "dev"
	Commit    = ""
	BuildTime = ""
	startedAt = time.Now().UTC()
)

type Info struct {
	Version   string
	Commit    string
	BuildTime string
	BuildID   string
	StartedAt time.Time
}

func Current() Info {
	info := Info{
		Version:   strings.TrimSpace(Version),
		Commit:    shortCommit(strings.TrimSpace(Commit)),
		BuildTime: strings.TrimSpace(BuildTime),
		StartedAt: startedAt,
	}

	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		if info.Version == "" || info.Version == "dev" {
			if version := strings.TrimSpace(buildInfo.Main.Version); version != "" && version != "(devel)" {
				info.Version = version
			}
		}

		for _, setting := range buildInfo.Settings {
			switch setting.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = shortCommit(strings.TrimSpace(setting.Value))
				}
			case "vcs.time":
				if info.BuildTime == "" {
					info.BuildTime = strings.TrimSpace(setting.Value)
				}
			}
		}
	}

	if info.Version == "" {
		info.Version = "dev"
	}

	info.BuildID = info.Version
	if info.Commit != "" {
		info.BuildID = fmt.Sprintf("%s+%s", info.Version, info.Commit)
	}

	return info
}

func shortCommit(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
