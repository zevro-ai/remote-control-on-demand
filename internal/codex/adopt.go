package codex

import (
	"database/sql"
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
	ID        string
	CWD       string
	Title     string
	Model     string
	UpdatedAt time.Time
}

func listAdoptableSessions(baseFolder string, existing []*chat.Session) ([]provider.AdoptableSession, error) {
	codexHome, err := resolveCodexHome()
	if err != nil {
		return nil, err
	}

	dbPath, err := locateStateDB(codexHome)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []provider.AdoptableSession{}, nil
		}
		return nil, err
	}

	threads, err := listStoredThreads(dbPath)
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

func stateDBVersion(path string) int {
	base := filepath.Base(path)
	trimmed := strings.TrimSuffix(strings.TrimPrefix(base, "state_"), ".sqlite")
	version, err := strconv.Atoi(trimmed)
	if err != nil {
		return -1
	}
	return version
}

func listStoredThreads(dbPath string) ([]storedThread, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening Codex state DB: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT id, cwd, title, COALESCE(model, ''), updated_at
		FROM threads
		WHERE archived = 0
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying Codex threads: %w", err)
	}
	defer rows.Close()

	var threads []storedThread
	for rows.Next() {
		var (
			id        string
			cwd       string
			title     string
			model     string
			updatedAt int64
		)
		if err := rows.Scan(&id, &cwd, &title, &model, &updatedAt); err != nil {
			return nil, fmt.Errorf("reading Codex thread row: %w", err)
		}
		threads = append(threads, storedThread{
			ID:        id,
			CWD:       cwd,
			Title:     title,
			Model:     model,
			UpdatedAt: time.Unix(updatedAt, 0).UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating Codex threads: %w", err)
	}

	return threads, nil
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
