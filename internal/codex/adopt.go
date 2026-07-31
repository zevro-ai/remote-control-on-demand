package codex

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/zevro-ai/remote-control-on-demand/internal/chat"
	"github.com/zevro-ai/remote-control-on-demand/internal/provider"
)

type storedThread struct {
	ID           string
	RolloutPath  string
	CWD          string
	Title        string
	Model        string
	Preview      string
	MessageCount int
	Archived     bool
	UpdatedAt    time.Time
}

func listAdoptableSessions(baseFolder string, existing []*chat.Session) ([]provider.AdoptableSession, error) {
	codexHome, err := resolveCodexHome()
	if err != nil {
		return nil, err
	}

	threads, err := listStoredThreadsFromHome(codexHome, false)
	if err != nil {
		return nil, err
	}

	existingThreads := make(map[string]struct{}, len(existing))
	for _, sess := range existing {
		if sess == nil || strings.TrimSpace(sess.ThreadID) == "" {
			continue
		}
		existingThreads[sess.ThreadID] = struct{}{}
	}

	adoptable := make([]provider.AdoptableSession, 0, len(threads))
	for _, thread := range threads {
		if _, ok := existingThreads[thread.ID]; ok {
			continue
		}

		_, relName, relCWD, err := resolveRepoForThread(baseFolder, thread.CWD)
		if err != nil {
			continue
		}

		adoptable = append(adoptable, provider.AdoptableSession{
			ThreadID:  thread.ID,
			RelName:   relName,
			RelCWD:    relCWD,
			Title:     strings.TrimSpace(thread.Title),
			Model:     strings.TrimSpace(thread.Model),
			UpdatedAt: thread.UpdatedAt,
		})
	}

	sort.Slice(adoptable, func(i, j int) bool {
		return adoptable[i].UpdatedAt.After(adoptable[j].UpdatedAt)
	})

	return adoptable, nil
}

func (m *Manager) ListHistory() ([]provider.HistorySession, error) {
	codexHome, err := resolveCodexHome()
	if err != nil {
		return nil, err
	}
	threads, err := listStoredThreadsFromHome(codexHome, true)
	if err != nil {
		return nil, err
	}

	history := make([]provider.HistorySession, 0, len(threads))
	for _, thread := range threads {
		_, relName, relCWD, err := resolveRepoForThread(m.core.BaseFolder(), thread.CWD)
		if err != nil {
			continue
		}
		history = append(history, provider.HistorySession{
			ThreadID:     thread.ID,
			RelName:      relName,
			RelCWD:       relCWD,
			Title:        strings.TrimSpace(thread.Title),
			Model:        strings.TrimSpace(thread.Model),
			Preview:      strings.TrimSpace(thread.Preview),
			UpdatedAt:    thread.UpdatedAt,
			Archived:     thread.Archived,
			MessageCount: thread.MessageCount,
		})
	}
	return history, nil
}

func (m *Manager) GetHistory(threadID string) ([]chat.Message, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, fmt.Errorf("thread ID is required")
	}
	codexHome, err := resolveCodexHome()
	if err != nil {
		return nil, err
	}
	threads, err := listStoredThreadsFromHome(codexHome, true)
	if err != nil {
		return nil, err
	}
	for _, thread := range threads {
		if thread.ID == threadID {
			return readRolloutMessages(thread.RolloutPath)
		}
	}
	return nil, fmt.Errorf("Codex history %q not found", threadID)
}

func resolveCodexHome() (string, error) {
	if value := strings.TrimSpace(os.Getenv("CODEX_HOME")); value != "" {
		return value, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".codex"), nil
}

func locateStateDB(codexHome string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(codexHome, "state_*.sqlite"))
	if err != nil {
		return "", fmt.Errorf("locating Codex state DB: %w", err)
	}
	if len(matches) == 0 {
		return "", os.ErrNotExist
	}

	type candidate struct {
		path    string
		version int
		modTime time.Time
	}

	candidates := make([]candidate, 0, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{
			path:    match,
			version: stateDBVersion(match),
			modTime: info.ModTime(),
		})
	}
	if len(candidates) == 0 {
		return "", os.ErrNotExist
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].version != candidates[j].version {
			return candidates[i].version > candidates[j].version
		}
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	return candidates[0].path, nil
}

func locateStateDBs(codexHome string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(codexHome, "state_*.sqlite"))
	if err != nil {
		return nil, fmt.Errorf("locating Codex state DBs: %w", err)
	}
	if len(matches) == 0 {
		return nil, os.ErrNotExist
	}
	sort.Slice(matches, func(i, j int) bool {
		return stateDBVersion(matches[i]) > stateDBVersion(matches[j])
	})
	return matches, nil
}

func stateDBVersion(path string) int {
	base := filepath.Base(path)
	trimmed := strings.TrimSuffix(strings.TrimPrefix(base, "state_"), ".sqlite")
	version, err := strconv.Atoi(trimmed)
	if err != nil {
		return -1
	}
	return version
}

func listStoredThreadsFromHome(codexHome string, includeArchived bool) ([]storedThread, error) {
	dbPaths, err := locateStateDBs(codexHome)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []storedThread{}, nil
		}
		return nil, err
	}
	byID := make(map[string]storedThread)
	for _, dbPath := range dbPaths {
		threads, err := listStoredThreads(dbPath, includeArchived)
		if err != nil {
			continue
		}
		for _, thread := range threads {
			current, ok := byID[thread.ID]
			if !ok || thread.UpdatedAt.After(current.UpdatedAt) {
				byID[thread.ID] = thread
			}
		}
	}
	threads := make([]storedThread, 0, len(byID))
	for _, thread := range byID {
		threads = append(threads, thread)
	}
	sort.Slice(threads, func(i, j int) bool { return threads[i].UpdatedAt.After(threads[j].UpdatedAt) })
	return threads, nil
}

func listStoredThreads(dbPath string, includeArchived bool) ([]storedThread, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening Codex state DB: %w", err)
	}
	defer db.Close()

	conn, err := db.Conn(context.Background())
	if err != nil {
		return nil, fmt.Errorf("opening Codex state DB connection: %w", err)
	}
	defer conn.Close()

	if err := configureSQLiteReadOnly(context.Background(), conn); err != nil {
		return nil, err
	}

	query := `
		SELECT id, rollout_path, cwd, title, COALESCE(model, ''),
		       COALESCE(first_user_message, ''), updated_at, archived,
		       COALESCE(has_user_event, 0)
		FROM threads`
	if !includeArchived {
		query += " WHERE archived = 0"
	}
	query += " ORDER BY updated_at DESC"
	rows, err := conn.QueryContext(context.Background(), query)
	if err != nil {
		return nil, fmt.Errorf("querying Codex threads: %w", err)
	}
	defer rows.Close()

	var threads []storedThread
	for rows.Next() {
		var (
			id, rolloutPath, cwd, title, model, preview string
			updatedAt, archived, messageCount           int64
		)
		if err := rows.Scan(&id, &rolloutPath, &cwd, &title, &model, &preview, &updatedAt, &archived, &messageCount); err != nil {
			return nil, fmt.Errorf("reading Codex thread row: %w", err)
		}
		threads = append(threads, storedThread{
			ID:           id,
			RolloutPath:  rolloutPath,
			CWD:          cwd,
			Title:        title,
			Model:        model,
			Preview:      preview,
			MessageCount: int(messageCount),
			Archived:     archived != 0,
			UpdatedAt:    time.Unix(updatedAt, 0).UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating Codex threads: %w", err)
	}

	return threads, nil
}

func readRolloutMessages(path string) ([]chat.Message, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return []chat.Message{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening Codex rollout: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	messages := make([]chat.Message, 0, 32)
	for scanner.Scan() {
		var event map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		for _, message := range messagesFromRolloutEvent(event) {
			if strings.TrimSpace(message.Content) == "" {
				continue
			}
			messages = append(messages, message)
			if len(messages) >= chat.DefaultMaxMessages {
				return messages, nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading Codex rollout: %w", err)
	}
	return messages, nil
}

func messagesFromRolloutEvent(event map[string]interface{}) []chat.Message {
	payload, _ := event["payload"].(map[string]interface{})
	if payload == nil {
		payload = event
	}
	messageType := stringValue(payload["type"])
	role := stringValue(payload["role"])
	if role == "" {
		switch messageType {
		case "user_message":
			role = "user"
		case "agent_message", "assistant_message":
			role = "assistant"
		}
	}
	if role != "user" && role != "assistant" {
		return nil
	}
	content := extractRolloutText(payload["content"])
	if content == "" {
		content = stringValue(payload["text"])
	}
	if content == "" {
		content = stringValue(payload["message"])
	}
	if content == "" {
		return nil
	}
	timestamp, _ := time.Parse(time.RFC3339Nano, stringValue(event["timestamp"]))
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	return []chat.Message{{Role: role, Kind: "text", Content: content, Timestamp: timestamp}}
}

func extractRolloutText(value interface{}) string {
	switch value := value.(type) {
	case string:
		return strings.TrimSpace(value)
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			if text := extractRolloutText(item); text != "" {
				parts = append(parts, text)
			}
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	case map[string]interface{}:
		for _, key := range []string{"text", "value", "content"} {
			if text := extractRolloutText(value[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

func stringValue(value interface{}) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func configureSQLiteReadOnly(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return fmt.Errorf("configuring Codex state DB busy_timeout: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "PRAGMA query_only = 1"); err != nil {
		return fmt.Errorf("configuring Codex state DB query_only: %w", err)
	}
	return nil
}

func resolveRepoForThread(baseFolder, cwd string) (string, string, string, error) {
	if strings.TrimSpace(baseFolder) == "" {
		return "", "", "", fmt.Errorf("base folder is required")
	}
	if strings.TrimSpace(cwd) == "" {
		return "", "", "", fmt.Errorf("thread cwd is required")
	}

	baseAbs, err := filepath.Abs(baseFolder)
	if err != nil {
		return "", "", "", fmt.Errorf("resolving base folder: %w", err)
	}
	baseResolved, err := filepath.EvalSymlinks(baseAbs)
	if err != nil {
		return "", "", "", fmt.Errorf("resolving base folder: %w", err)
	}

	cwdResolved, err := evalSymlinksAllowMissing(filepath.Clean(cwd))
	if err != nil {
		return "", "", "", fmt.Errorf("resolving thread cwd %q: %w", cwd, err)
	}

	relToBase, err := filepath.Rel(baseResolved, cwdResolved)
	if err != nil {
		return "", "", "", fmt.Errorf("resolving thread cwd %q: %w", cwd, err)
	}
	if relToBase == ".." || strings.HasPrefix(relToBase, ".."+string(os.PathSeparator)) {
		return "", "", "", fmt.Errorf("thread cwd %q must stay within rc.base_folder", cwd)
	}

	repoPath, err := findRepoRoot(baseResolved, cwdResolved)
	if err != nil {
		return "", "", "", err
	}

	relName, err := filepath.Rel(baseResolved, repoPath)
	if err != nil {
		return "", "", "", fmt.Errorf("resolving repo path %q: %w", repoPath, err)
	}
	relCWD, err := filepath.Rel(repoPath, cwdResolved)
	if err != nil {
		return "", "", "", fmt.Errorf("resolving thread cwd %q: %w", cwd, err)
	}
	if relCWD == "." {
		relCWD = ""
	}

	return repoPath, relName, relCWD, nil
}

func findRepoRoot(baseResolved, cwdResolved string) (string, error) {
	current := cwdResolved
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		if current == baseResolved {
			return "", fmt.Errorf("thread cwd %q is not inside a git repository under rc.base_folder", cwdResolved)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("thread cwd %q is not inside a git repository under rc.base_folder", cwdResolved)
		}
		current = parent
	}
}

func evalSymlinksAllowMissing(path string) (string, error) {
	current := filepath.Clean(path)
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}
