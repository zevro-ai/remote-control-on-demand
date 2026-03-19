package botutil

import (
	"fmt"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func ListGitFolders(baseFolder string) []string {
	info, err := os.Stat(baseFolder)
	if err != nil || !info.IsDir() {
		return nil
	}

	var folders []string
	err = filepath.WalkDir(baseFolder, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if path == baseFolder {
			return nil
		}

		if d.IsDir() {
			if ShouldSkipScanDir(d.Name()) {
				return filepath.SkipDir
			}
			if HasGitMetadata(path) {
				rel, err := filepath.Rel(baseFolder, path)
				if err == nil && rel != "." {
					folders = append(folders, rel)
				}
				return filepath.SkipDir
			}
		}

		return nil
	})
	if err != nil {
		return nil
	}

	sort.Strings(folders)
	return folders
}

func ShouldSkipScanDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}

	switch name {
	case "node_modules", "vendor", "dist", "build", "tmp":
		return true
	default:
		return false
	}
}

func HasGitMetadata(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

func MatchFolderQuery(folders []string, query string) (string, []string) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", nil
	}

	normalizedQuery := NormalizeFolderQuery(query)
	for _, folder := range folders {
		if NormalizeFolderQuery(folder) == normalizedQuery {
			return folder, nil
		}
	}

	var matches []string
	for _, folder := range folders {
		if strings.Contains(strings.ToLower(filepath.ToSlash(folder)), normalizedQuery) {
			matches = append(matches, folder)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", matches
}

func NormalizeFolderQuery(value string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(value)))
}

func FormatCodeList(items []string, limit int) string {
	if len(items) == 0 {
		return ""
	}

	maxItems := min(len(items), limit)
	lines := make([]string, 0, maxItems+1)
	for _, item := range items[:maxItems] {
		lines = append(lines, "• <code>"+html.EscapeString(item)+"</code>")
	}
	if len(items) > maxItems {
		lines = append(lines, fmt.Sprintf("• and %d more", len(items)-maxItems))
	}
	return strings.Join(lines, "\n")
}
