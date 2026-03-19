package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/config"
)

type Notification struct {
	Message string
}

type sessionState struct {
	ID        string    `json:"id"`
	Folder    string    `json:"folder"`
	RelName   string    `json:"rel_name"`
	PID       int       `json:"pid"`
	URL       string    `json:"url"`
	Status    Status    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	Restarts  int       `json:"restarts"`
}

type Runner interface {
	Start(ctx context.Context, dir string, stdout, stderr io.Writer) (*exec.Cmd, error)
	IsClaudeProcess(pid int) bool
}

type Manager struct {
	mu                   sync.Mutex
	sessions             map[string]*Session
	runner               Runner
	baseFolder           string
	statePath            string
	autoRestart          bool
	maxRestarts          int
	restartDelay         time.Duration
	notificationsCfg     *config.NotificationsConfig
	notificationsEnabled atomic.Bool
	subMu                sync.Mutex
	subscribers          []func(Notification)
}

func NewManager(runner Runner, baseFolder, statePath string, autoRestart bool, maxRestarts int, restartDelay time.Duration, notifCfg *config.NotificationsConfig) *Manager {
	mgr := &Manager{
		sessions:         make(map[string]*Session),
		runner:           runner,
		baseFolder:       baseFolder,
		statePath:        statePath,
		autoRestart:      autoRestart,
		maxRestarts:      maxRestarts,
		restartDelay:     restartDelay,
		notificationsCfg: notifCfg,
	}
	mgr.notificationsEnabled.Store(true)
	return mgr
}

// Subscribe registers a callback for notification events.
// fn MUST be non-blocking (push to channel or spawn goroutine).
// Returns an unsubscribe function.
func (m *Manager) Subscribe(fn func(Notification)) func() {
	m.subMu.Lock()
	defer m.subMu.Unlock()
	m.subscribers = append(m.subscribers, fn)
	idx := len(m.subscribers) - 1
	return func() {
		m.subMu.Lock()
		defer m.subMu.Unlock()
		m.subscribers[idx] = nil
	}
}

// SetNotifications enables or disables notifications.
// When disabled, notifications are silently dropped.
func (m *Manager) SetNotifications(enabled bool) {
	m.notificationsEnabled.Store(enabled)
}

func (m *Manager) sendNotification(msg string) {
	if !m.notificationsEnabled.Load() {
		return
	}
	n := Notification{Message: msg}
	m.subMu.Lock()
	subs := make([]func(Notification), len(m.subscribers))
	copy(subs, m.subscribers)
	m.subMu.Unlock()
	for _, fn := range subs {
		if fn != nil {
			fn(n)
		}
	}
}

func (m *Manager) BaseFolder() string {
	return m.baseFolder
}

func (m *Manager) Start(folder string) (*Session, error) {
	fullPath, relName, err := resolveProjectPath(m.baseFolder, folder)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(fullPath)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("folder %q does not exist", relName)
	}

	gitPath := filepath.Join(fullPath, ".git")
	if _, err := os.Stat(gitPath); err != nil {
		return nil, fmt.Errorf("folder %q is not a git repository", relName)
	}

	projCfg, err := config.LoadProjectConfig(fullPath)
	if err != nil {
		return nil, fmt.Errorf("loading project config: %w", err)
	}

	m.mu.Lock()
	for _, s := range m.sessions {
		if s.Folder == fullPath && s.Status == StatusRunning {
			m.mu.Unlock()
			return nil, fmt.Errorf("session already running in %q (ID: %s)", relName, s.ID)
		}
	}

	id, err := m.generateUniqueSessionID()
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("generating session ID: %w", err)
	}

	sess := &Session{
		ID:        id,
		Folder:    fullPath,
		RelName:   relName,
		Config:    projCfg,
		Status:    StatusRunning,
		StartedAt: time.Now(),
		OutputBuf: NewRingBuffer(500),
	}

	// Reserve slot before releasing lock
	m.sessions[id] = sess
	m.saveState()
	m.mu.Unlock()

	if err := m.startProcess(sess); err != nil {
		// Rollback on process start failure
		m.mu.Lock()
		delete(m.sessions, id)
		m.saveState()
		m.mu.Unlock()
		return nil, fmt.Errorf("starting process: %w", err)
	}

	msg := fmt.Sprintf(
		"<b>Session started</b>\nID: <code>%s</code>\nProject: <code>%s</code>",
		html.EscapeString(id),
		html.EscapeString(relName),
	)
	if projCfg != nil && projCfg.Prompt != "" {
		msg += fmt.Sprintf("\n\n<b>Project prompt</b>\n<code>%s</code>", html.EscapeString(projCfg.Prompt))
	}
	if projCfg != nil && projCfg.MaxDuration > 0 {
		msg += fmt.Sprintf("\nMax duration: <code>%s</code>", html.EscapeString(time.Duration(projCfg.MaxDuration).String()))
	}

	m.sendNotification(msg)

	return sess, nil
}

func resolveProjectPath(baseFolder, folder string) (string, string, error) {
	if strings.TrimSpace(folder) == "" {
		return "", "", fmt.Errorf("folder is required")
	}

	baseAbs, err := filepath.Abs(baseFolder)
	if err != nil {
		return "", "", fmt.Errorf("resolving base folder: %w", err)
	}

	targetAbs, err := filepath.Abs(filepath.Join(baseAbs, filepath.Clean(folder)))
	if err != nil {
		return "", "", fmt.Errorf("resolving folder %q: %w", folder, err)
	}

	relPath, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return "", "", fmt.Errorf("resolving folder %q: %w", folder, err)
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("folder %q must stay within rc.base_folder", folder)
	}

	return targetAbs, relPath, nil
}

func (m *Manager) startProcess(sess *Session) error {
	ctx, cancel := context.WithCancel(context.Background())
	sess.cancel = cancel

	urlScan := newURLScanner(sess.OutputBuf, func(url string) {
		m.mu.Lock()
		sess.ClaudeURL = url
		sess.URL = url
		m.saveState()
		m.mu.Unlock()

		m.sendNotification(fmt.Sprintf(
			"Session <code>%s</code> is ready.\n<a href=\"%s\">Open in Claude</a>",
			html.EscapeString(sess.ID),
			html.EscapeString(url),
		))
	})

	// Resolve effective notifications config (project overrides global)
	var projNotif *config.NotificationsConfig
	if sess.Config != nil {
		projNotif = sess.Config.Notifications
	}
	effectiveNotif := config.ResolveNotifications(m.notificationsCfg, projNotif)

	evtScan := newEventScanner(urlScan, effectiveNotif,
		func(patternName, match string) {
			m.sendNotification(fmt.Sprintf(
				"Session <code>%s</code> (%s)\n<b>%s</b>: <code>%s</code>",
				html.EscapeString(sess.ID),
				html.EscapeString(sess.RelName),
				html.EscapeString(patternName),
				html.EscapeString(match),
			))
		},
		func() {
			m.mu.Lock()
			sess.LastOutputAt = time.Now()
			m.mu.Unlock()
		},
		func() {
			m.sendNotification(fmt.Sprintf(
				"Session <code>%s</code> (%s) is idle for %s.",
				html.EscapeString(sess.ID),
				html.EscapeString(sess.RelName),
				html.EscapeString(time.Duration(effectiveNotif.IdleTimeout).String()),
			))
		},
	)
	sess.eventScanner = evtScan
	m.startProgressReporter(sess, effectiveNotif)

	cmd, err := m.runner.Start(ctx, sess.Folder, evtScan, evtScan)
	if err != nil {
		m.stopProgressReporter(sess)
		cancel()
		return err
	}
	sess.Cmd = cmd
	if cmd.Process != nil {
		sess.PID = cmd.Process.Pid
	}

	if sess.Config != nil && sess.Config.MaxDuration > 0 {
		time.AfterFunc(time.Duration(sess.Config.MaxDuration), func() {
			m.mu.Lock()
			if sess.Status == StatusRunning {
				m.mu.Unlock()
				m.sendNotification(fmt.Sprintf(
					"Session <code>%s</code> reached max duration (%s) and will be stopped.",
					html.EscapeString(sess.ID),
					html.EscapeString(time.Duration(sess.Config.MaxDuration).String()),
				))
				m.Kill(sess.ID)
			} else {
				m.mu.Unlock()
			}
		})
	}

	go m.monitor(sess)

	return nil
}

func (m *Manager) monitor(sess *Session) {
	err := sess.Cmd.Wait()

	if sess.eventScanner != nil {
		sess.eventScanner.Stop()
	}
	m.stopProgressReporter(sess)

	m.mu.Lock()
	if sess.Status == StatusStopped {
		// Killed intentionally
		m.saveState()
		m.mu.Unlock()
		return
	}

	sess.Status = StatusCrashed
	m.saveState()

	autoRestart := m.autoRestart
	maxRestarts := m.maxRestarts
	restartDelay := m.restartDelay

	if sess.Config != nil && sess.Config.AutoRestart != nil {
		autoRestart = sess.Config.AutoRestart.Enabled
		maxRestarts = sess.Config.AutoRestart.MaxAttempts
		restartDelay = time.Duration(sess.Config.AutoRestart.Delay)
	}

	if err != nil && autoRestart && sess.Restarts < maxRestarts {
		sess.Restarts++
		restarts := sess.Restarts
		m.mu.Unlock()

		m.sendNotification(fmt.Sprintf(
			"Session <code>%s</code> crashed, restarting (%d/%d).",
			html.EscapeString(sess.ID),
			restarts,
			maxRestarts,
		))

		time.Sleep(restartDelay)

		m.mu.Lock()
		if sess.Status != StatusCrashed {
			m.mu.Unlock()
			return
		}
		sess.Status = StatusRunning
		m.mu.Unlock()

		if err := m.startProcess(sess); err != nil {
			m.mu.Lock()
			sess.Status = StatusCrashed
			m.saveState()
			m.mu.Unlock()
			m.sendNotification(fmt.Sprintf(
				"Session <code>%s</code> restart failed: <code>%s</code>",
				html.EscapeString(sess.ID),
				html.EscapeString(err.Error()),
			))
		}
	} else {
		m.mu.Unlock()
		if sess.Restarts >= maxRestarts {
			m.sendNotification(m.renderSessionSummary(
				sess,
				"Session crashed",
				fmt.Sprintf("Restart limit reached (%d/%d).", sess.Restarts, maxRestarts),
			))
		} else {
			m.sendNotification(m.renderSessionSummary(sess, "Session ended", "Process exited without auto-restart."))
		}
	}
}

func (m *Manager) Kill(id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}
	if sess.Status != StatusRunning {
		m.mu.Unlock()
		return fmt.Errorf("session %q not running (status: %s)", id, sess.Status)
	}
	sess.Status = StatusStopped
	m.saveState()
	m.mu.Unlock()

	if sess.cancel != nil {
		sess.cancel()
	}
	return nil
}

func (m *Manager) Restart(id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session %q not found", id)
	}

	if sess.Status == StatusRunning {
		sess.Status = StatusStopped
		if sess.cancel != nil {
			sess.cancel()
		}
		m.mu.Unlock()
		// Wait a moment for process to exit
		time.Sleep(500 * time.Millisecond)
		m.mu.Lock()
	}

	sess.Status = StatusRunning
	sess.Restarts = 0
	sess.StartedAt = time.Now()
	m.saveState()
	m.mu.Unlock()

	if err := m.startProcess(sess); err != nil {
		m.mu.Lock()
		sess.Status = StatusCrashed
		m.saveState()
		m.mu.Unlock()
		return fmt.Errorf("restart failed: %w", err)
	}

	m.sendNotification(fmt.Sprintf("Session <code>%s</code> restarted.", html.EscapeString(id)))
	return nil
}

func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

func (m *Manager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	return list
}

func (m *Manager) StopAll() int {
	m.mu.Lock()
	var running []*Session
	for _, s := range m.sessions {
		if s.Status == StatusRunning {
			s.Status = StatusStopped
			running = append(running, s)
		}
	}
	m.saveState()
	m.mu.Unlock()

	for _, s := range running {
		if s.cancel != nil {
			s.cancel()
		}
		m.stopProgressReporter(s)
	}

	// Wait for processes to actually exit
	for _, s := range running {
		if s.Cmd != nil && s.Cmd.Process != nil {
			s.Cmd.Wait()
		}
		if s.eventScanner != nil {
			sessEsc := s.eventScanner
			sessEsc.Stop()
		}
	}
	return len(running)
}

func (m *Manager) saveState() {
	if m.statePath == "" {
		return
	}

	data := struct {
		Sessions []sessionState `json:"sessions"`
	}{
		Sessions: make([]sessionState, 0, len(m.sessions)),
	}

	for _, s := range m.sessions {
		data.Sessions = append(data.Sessions, sessionState{
			ID:        s.ID,
			Folder:    s.Folder,
			RelName:   s.RelName,
			PID:       s.PID,
			URL:       sessionURL(s),
			Status:    s.Status,
			StartedAt: s.StartedAt,
			Restarts:  s.Restarts,
		})
	}

	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling session state: %v\n", err)
		return
	}

	if err := os.WriteFile(m.statePath, b, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing session state: %v\n", err)
	}
}

func (m *Manager) Restore() error {
	if m.statePath == "" {
		return nil
	}

	b, err := os.ReadFile(m.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading state file: %w", err)
	}

	var data struct {
		Sessions []sessionState `json:"sessions"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return fmt.Errorf("unmarshaling state: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, sd := range data.Sessions {
		if sd.Status != StatusRunning {
			continue
		}

		// Verify process
		if sd.PID > 0 && m.runner.IsClaudeProcess(sd.PID) {
			// Found alive process, re-attach
			projCfg, _ := config.LoadProjectConfig(sd.Folder)
			ctx, cancel := context.WithCancel(context.Background())
			sess := &Session{
				ID:        sd.ID,
				Folder:    sd.Folder,
				RelName:   sd.RelName,
				ClaudeURL: sd.URL,
				Config:    projCfg,
				PID:       sd.PID,
				URL:       sd.URL,
				Status:    StatusRunning,
				StartedAt: sd.StartedAt,
				Restarts:  sd.Restarts,
				OutputBuf: NewRingBuffer(500),
				cancel:    cancel,
			}
			m.sessions[sd.ID] = sess

			// Start polling monitor for orphaned process
			go m.monitorOrphan(ctx, sess)

			// Re-deliver URL if present
			if sess.URL != "" {
				m.sendNotification(fmt.Sprintf(
					"<b>Session restored</b>\nSession <code>%s</code> (<code>%s</code>)\n<a href=\"%s\">Open in Claude</a>",
					html.EscapeString(sess.ID),
					html.EscapeString(sess.RelName),
					html.EscapeString(sess.URL),
				))
			} else {
				m.sendNotification(fmt.Sprintf(
					"<b>Session restored</b>\nSession <code>%s</code> (<code>%s</code>)\nNo Claude URL recorded.",
					html.EscapeString(sess.ID),
					html.EscapeString(sess.RelName),
				))
			}
		} else {
			// Process is dead
			projCfg, _ := config.LoadProjectConfig(sd.Folder)
			sess := &Session{
				ID:        sd.ID,
				Folder:    sd.Folder,
				RelName:   sd.RelName,
				Config:    projCfg,
				Status:    StatusCrashed,
				StartedAt: sd.StartedAt,
				Restarts:  sd.Restarts,
				OutputBuf: NewRingBuffer(500),
			}
			m.sessions[sd.ID] = sess
		}
	}

	m.saveState()
	return nil
}

func (m *Manager) monitorOrphan(ctx context.Context, sess *Session) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !m.runner.IsClaudeProcess(sess.PID) {
				m.mu.Lock()
				if sess.Status != StatusRunning {
					m.mu.Unlock()
					return
				}
				sess.Status = StatusCrashed
				m.saveState()
				m.mu.Unlock()

				m.sendNotification(fmt.Sprintf(
					"<b>Restored session exited</b>\nSession <code>%s</code> (<code>%s</code>) exited or crashed.",
					html.EscapeString(sess.ID),
					html.EscapeString(sess.RelName),
				))
				return
			}
		}
	}
}

func generateID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// generateUniqueSessionID generates a session ID that does not collide with existing sessions.
// Must be called with m.mu held.
func (m *Manager) generateUniqueSessionID() (string, error) {
	for i := 0; i < 100; i++ {
		id, err := generateID()
		if err != nil {
			return "", err
		}
		if _, exists := m.sessions[id]; !exists {
			return id, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique session ID after 100 attempts")
}

func (m *Manager) startProgressReporter(sess *Session, notifCfg *config.NotificationsConfig) {
	m.stopProgressReporter(sess)
	if notifCfg == nil || notifCfg.ProgressUpdateInterval <= 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	sess.stopProgress = cancel
	interval := time.Duration(notifCfg.ProgressUpdateInterval)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.notifyProgress(sess)
			}
		}
	}()
}

func (m *Manager) stopProgressReporter(sess *Session) {
	if sess.stopProgress != nil {
		sess.stopProgress()
		sess.stopProgress = nil
	}
}

func (m *Manager) notifyProgress(sess *Session) {
	m.mu.Lock()
	if sess.Status != StatusRunning {
		m.mu.Unlock()
		return
	}

	id := sess.ID
	project := sess.RelName
	uptime := time.Since(sess.StartedAt).Truncate(time.Second)
	lastOutputAt := sess.LastOutputAt
	claudeURL := sessionURL(sess)
	lines := sess.OutputBuf.Lines(1)
	m.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("<b>Progress update</b>\n")
	sb.WriteString(fmt.Sprintf("Session <code>%s</code>\n", html.EscapeString(id)))
	sb.WriteString(fmt.Sprintf("Project: <code>%s</code>\n", html.EscapeString(project)))
	sb.WriteString(fmt.Sprintf("Uptime: %s\n", html.EscapeString(uptime.String())))
	if lastOutputAt.IsZero() {
		sb.WriteString("Last output: waiting for first response\n")
	} else {
		sb.WriteString(fmt.Sprintf("Last output: %s ago\n", html.EscapeString(time.Since(lastOutputAt).Truncate(time.Second).String())))
	}
	if len(lines) > 0 {
		sb.WriteString(fmt.Sprintf("Latest log: <code>%s</code>\n", html.EscapeString(trimForTelegram(lines[0], 180))))
	}
	if claudeURL != "" {
		sb.WriteString(fmt.Sprintf("<a href=\"%s\">Open in Claude</a>", html.EscapeString(claudeURL)))
	}

	m.sendNotification(strings.TrimSpace(sb.String()))
}

func trimForTelegram(value string, maxLen int) string {
	if len(value) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return value[:maxLen]
	}
	return value[:maxLen-3] + "..."
}

func sessionURL(sess *Session) string {
	if sess.ClaudeURL != "" {
		return sess.ClaudeURL
	}
	return sess.URL
}

// ListFolders returns relative paths of all git repositories under baseFolder.
func (m *Manager) ListFolders() []string {
	info, err := os.Stat(m.baseFolder)
	if err != nil || !info.IsDir() {
		return nil
	}

	var folders []string
	_ = filepath.WalkDir(m.baseFolder, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || path == m.baseFolder {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return filepath.SkipDir
		}
		switch name {
		case "node_modules", "vendor", "dist", "build", "tmp":
			return filepath.SkipDir
		}
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			rel, err := filepath.Rel(m.baseFolder, path)
			if err == nil && rel != "." {
				folders = append(folders, rel)
			}
			return filepath.SkipDir
		}
		return nil
	})

	sort.Strings(folders)
	return folders
}

func (m *Manager) renderSessionSummary(sess *Session, title, summary string) string {
	m.mu.Lock()
	id := sess.ID
	project := sess.RelName
	uptime := time.Since(sess.StartedAt).Truncate(time.Second)
	restarts := sess.Restarts
	claudeURL := sessionURL(sess)
	lines := sess.OutputBuf.Lines(1)
	m.mu.Unlock()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>%s</b>\n", html.EscapeString(title)))
	sb.WriteString(fmt.Sprintf("Session <code>%s</code>\n", html.EscapeString(id)))
	sb.WriteString(fmt.Sprintf("Project: <code>%s</code>\n", html.EscapeString(project)))
	sb.WriteString(fmt.Sprintf("Uptime: %s\n", html.EscapeString(uptime.String())))
	sb.WriteString(fmt.Sprintf("Restarts: %d\n", restarts))
	if summary != "" {
		sb.WriteString(fmt.Sprintf("%s\n", html.EscapeString(summary)))
	}
	if len(lines) > 0 {
		sb.WriteString(fmt.Sprintf("Latest log: <code>%s</code>\n", html.EscapeString(trimForTelegram(lines[0], 180))))
	}
	if claudeURL != "" {
		sb.WriteString(fmt.Sprintf("<a href=\"%s\">Open in Claude</a>", html.EscapeString(claudeURL)))
	}

	return strings.TrimSpace(sb.String())
}
