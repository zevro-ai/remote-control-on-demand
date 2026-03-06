package bot

import (
	"os"
	"path/filepath"
	"testing"
)

func mkGitRepo(t *testing.T, base string, parts ...string) {
	t.Helper()
	dir := filepath.Join(append([]string{base}, parts...)...)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func mkDir(t *testing.T, base string, parts ...string) {
	t.Helper()
	dir := filepath.Join(append([]string{base}, parts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mkFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListGitFolders_TopLevel(t *testing.T) {
	base := t.TempDir()
	mkGitRepo(t, base, "alpha")
	mkGitRepo(t, base, "beta")

	got := listGitFolders(base)
	want := []string{"alpha", "beta"}
	assertSliceEqual(t, got, want)
}

func TestListGitFolders_AggregatingFolder(t *testing.T) {
	base := t.TempDir()
	// "work" is an aggregating folder: no .git, contains only subdirs
	mkGitRepo(t, base, "work", "api")
	mkGitRepo(t, base, "work", "frontend")

	got := listGitFolders(base)
	want := []string{filepath.Join("work", "api"), filepath.Join("work", "frontend")}
	assertSliceEqual(t, got, want)
}

func TestListGitFolders_MixedStructure(t *testing.T) {
	base := t.TempDir()
	mkGitRepo(t, base, "standalone")
	mkGitRepo(t, base, "work", "api")
	mkGitRepo(t, base, "personal", "blog")
	mkDir(t, base, "personal", "notes")

	got := listGitFolders(base)
	want := []string{filepath.Join("personal", "blog"), "standalone", filepath.Join("work", "api")}
	assertSliceEqual(t, got, want)
}

func TestListGitFolders_SkipsPlainFoldersWithFiles(t *testing.T) {
	base := t.TempDir()
	mkDir(t, base, "docs", "guide")
	mkFile(t, filepath.Join(base, "docs", "README.md"))

	got := listGitFolders(base)
	if len(got) != 0 {
		t.Errorf("expected no folders, got %v", got)
	}
}

func TestListGitFolders_HiddenDirsSkipped(t *testing.T) {
	base := t.TempDir()
	mkGitRepo(t, base, ".hidden")
	mkGitRepo(t, base, "visible")

	got := listGitFolders(base)
	want := []string{"visible"}
	assertSliceEqual(t, got, want)
}

func TestListGitFolders_EmptyBase(t *testing.T) {
	base := t.TempDir()
	got := listGitFolders(base)
	if len(got) != 0 {
		t.Errorf("expected no folders, got %v", got)
	}
}

func TestListGitFolders_NonExistentBase(t *testing.T) {
	got := listGitFolders("/nonexistent/path/that/does/not/exist")
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestListGitFolders_RecursiveDiscovery(t *testing.T) {
	base := t.TempDir()
	mkGitRepo(t, base, "clients", "acme", "api")
	mkGitRepo(t, base, "clients", "acme", "frontend")
	mkGitRepo(t, base, "labs", "experiments", "agent")

	got := listGitFolders(base)
	want := []string{
		filepath.Join("clients", "acme", "api"),
		filepath.Join("clients", "acme", "frontend"),
		filepath.Join("labs", "experiments", "agent"),
	}
	assertSliceEqual(t, got, want)
}

func TestListGitFolders_SkipsNestedReposInsideRepo(t *testing.T) {
	base := t.TempDir()
	mkGitRepo(t, base, "platform")
	mkGitRepo(t, base, "platform", "nested")

	got := listGitFolders(base)
	want := []string{"platform"}
	assertSliceEqual(t, got, want)
}

func TestMatchFolderQuery(t *testing.T) {
	folders := []string{
		"client/api",
		"client/frontend",
		"ops/tools",
	}

	t.Run("exact match wins", func(t *testing.T) {
		got, matches := matchFolderQuery(folders, "client/api")
		if got != "client/api" {
			t.Fatalf("expected exact match, got %q", got)
		}
		if matches != nil {
			t.Fatalf("expected nil matches, got %v", matches)
		}
	})

	t.Run("unique partial match resolves", func(t *testing.T) {
		got, matches := matchFolderQuery(folders, "tools")
		if got != "ops/tools" {
			t.Fatalf("expected unique partial match, got %q", got)
		}
		if matches != nil {
			t.Fatalf("expected nil matches, got %v", matches)
		}
	})

	t.Run("ambiguous query returns suggestions", func(t *testing.T) {
		got, matches := matchFolderQuery(folders, "client")
		if got != "" {
			t.Fatalf("expected no resolved match, got %q", got)
		}
		assertSliceEqual(t, matches, []string{"client/api", "client/frontend"})
	})
}

func assertSliceEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
