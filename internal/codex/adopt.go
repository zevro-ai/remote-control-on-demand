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

		workspace, err := chat.ResolveWorkspacePath(baseFolder, thread.CWD)
		if err != nil || !workspace.Exists {
			continue
		}

		adoptable = append(adoptable, provider.AdoptableSession{
			ThreadID:  thread.ID,
			RelName:   workspace.RelName,
			RelCWD:    workspace.RelCWD,
			Folder:    workspace.Folder,
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
		workspace, err := chat.ResolveWorkspacePath(m.core.BaseFolder(), thread.CWD)
		if err != nil {
			continue
		}
		history = append(history, provider.HistorySession{
			ThreadID:     thread.ID,
			RelName:      workspace.RelName,
			RelCWD:       workspace.RelCWD,
			Folder:       workspace.Folder,
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
	byID := make(map[string]storedThread)
	if dbPaths, err := locateStateDBs(codexHome); err == nil {
		for _, dbPath := range dbPaths {
			// Read archived rows as well so the rollout fallback cannot
			// accidentally reintroduce an archived thread into adoption.
			threads, err := listStoredThreads(dbPath, true)
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
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	// Recent Codex builds write rollout JSONL files even when the optional
	// state database has not indexed them yet. Merge those files as a fallback
	// so history remains complete across CLI versions and concurrent sessions.
	if rollouts, err := listRolloutThreads(codexHome); err == nil {
		for _, thread := range rollouts {
			if _, ok := byID[thread.ID]; !ok {
				byID[thread.ID] = thread
			}
		}
	}
	threads := make([]storedThread, 0, len(byID))
	for _, thread := range byID {
		if !includeArchived && thread.Archived {
			continue
		}
		threads = append(threads, thread)
	}
	sort.Slice(threads, func(i, j int) bool { return threads[i].UpdatedAt.After(threads[j].UpdatedAt) })
	return threads, nil
}

func listRolloutThreads(codexHome string) ([]storedThread, error) {
	root := filepath.Join(codexHome, "sessions")
	var result []storedThread
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".jsonl" {
			return nil
		}
		thread, err := readRolloutThread(path)
		if err == nil && thread.ID != "" {
			result = append(result, thread)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scanning Codex rollouts: %w", err)
	}
	return result, nil
}

func readRolloutThread(path string) (storedThread, error) {
	info, err := os.Stat(path)
	if err != nil {
		return storedThread{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return storedThread{}, err
	}
	defer file.Close()

	thread := storedThread{RolloutPath: path, UpdatedAt: info.ModTime()}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var event map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		payload, _ := event["payload"].(map[string]interface{})
		if payload == nil {
			payload = event
		}
		if thread.ID == "" {
			thread.ID = firstStringValue(payload, "id", "session_id", "thread_id")
		}
		if thread.CWD == "" {
			thread.CWD = firstStringValue(payload, "cwd", "working_directory")
		}
		if thread.Model == "" {
			thread.Model = firstStringValue(payload, "model", "model_name")
		}
		if timestamp := firstStringValue(event, "timestamp"); timestamp != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil && parsed.After(thread.UpdatedAt) {
				thread.UpdatedAt = parsed
			}
		}
		if stringValue(payload["type"]) == "session_meta" {
			if timestamp := firstStringValue(payload, "timestamp"); timestamp != "" {
				if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err == nil && parsed.After(thread.UpdatedAt) {
					thread.UpdatedAt = parsed
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return storedThread{}, err
	}
	if thread.ID == "" || thread.CWD == "" {
		return storedThread{}, errors.New("rollout metadata is incomplete")
	}

	messages, err := readRolloutMessages(path)
	if err == nil {
		thread.MessageCount = len(messages)
		for _, message := range messages {
			if thread.Preview == "" && strings.TrimSpace(message.Content) != "" {
				thread.Preview = strings.TrimSpace(message.Content)
			}
			if thread.Title == "" && message.Role == "user" && strings.TrimSpace(message.Content) != "" {
				thread.Title = strings.TrimSpace(message.Content)
			}
		}
	}
	return thread, nil
}

func firstStringValue(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(values[key]); value != "" {
			return value
		}
	}
	return ""
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
