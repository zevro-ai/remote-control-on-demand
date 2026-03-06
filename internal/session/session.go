package session

import (
	"context"
	"os/exec"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/config"
)

type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusCrashed Status = "crashed"
)

type Session struct {
	ID           string
	Folder       string // absolute path
	RelName      string // relative path from base folder (e.g. "work/my-app")
	ClaudeURL    string
	Config       *config.ProjectConfig
	Cmd          *exec.Cmd
	PID          int
	URL          string
	Status       Status
	StartedAt    time.Time
	LastOutputAt time.Time
	Restarts     int
	OutputBuf    *RingBuffer
	cancel       context.CancelFunc
	eventScanner *eventScanner
	stopProgress context.CancelFunc
}
