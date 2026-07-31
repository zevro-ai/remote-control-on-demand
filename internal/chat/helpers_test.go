package chat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectPathRejectsSymlinkEscape(t *testing.T) {
	baseDir := t.TempDir()
	outsideDir := t.TempDir()

	linkPath := filepath.Join(baseDir, "escape")
	if err := os.Symlink(outsideDir, linkPath); err != nil {
		t.Skipf("Symlink() unsupported in this environment: %v", err)
	}

	if _, _, err := ResolveProjectPath(baseDir, "escape"); err == nil {
		t.Fatal("expected symlink escape to be rejected")
	}
}

func TestResolveProjectPathRejectsLexicalEscapeEvenIfSymlinkResolvesInsideBase(t *testing.T) {
	baseDir := t.TempDir()
	insideDir := filepath.Join(baseDir, "demo")
	if err := os.MkdirAll(insideDir, 0755); err != nil {
		t.Fatalf("MkdirAll(demo): %v", err)
	}

	aliasDir := t.TempDir()
	linkPath := filepath.Join(aliasDir, "demo-link")
	if err := os.Symlink(insideDir, linkPath); err != nil {
		t.Skipf("Symlink() unsupported in this environment: %v", err)
	}

	folder := filepath.Join("..", filepath.Base(aliasDir), "demo-link")
	if _, _, err := ResolveProjectPath(baseDir, folder); err == nil {
		t.Fatal("expected lexical escape through sibling symlink path to be rejected")
	}
}

func TestResolveProjectPathAllowsMissingChildWithinResolvedBase(t *testing.T) {
	baseDir := t.TempDir()
	realBase := filepath.Join(baseDir, "real")
	if err := os.MkdirAll(realBase, 0755); err != nil {
		t.Fatalf("MkdirAll(real): %v", err)
	}

	baseLink := filepath.Join(baseDir, "base-link")
	if err := os.Symlink(realBase, baseLink); err != nil {
		t.Skipf("Symlink() unsupported in this environment: %v", err)
	}

	fullPath, relPath, err := ResolveProjectPath(baseLink, "nested/project")
	if err != nil {
		t.Fatalf("ResolveProjectPath(): %v", err)
	}

	wantPath, err := evalSymlinksAllowMissing(filepath.Join(realBase, "nested", "project"))
	if err != nil {
		t.Fatalf("evalSymlinksAllowMissing(): %v", err)
	}
	if fullPath != wantPath {
		t.Fatalf("fullPath = %q, want %q", fullPath, wantPath)
	}
	if relPath != filepath.Join("nested", "project") {
		t.Fatalf("relPath = %q, want %q", relPath, filepath.Join("nested", "project"))
	}
}

func TestResolveWorkspacePathIncludesExternalGitRepositories(t *testing.T) {
	baseDir := t.TempDir()
	externalDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(externalDir, ".git", "objects"), 0755); err != nil {
		t.Fatalf("MkdirAll(.git): %v", err)
	}
	workspaceDir := filepath.Join(externalDir, "nested")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("MkdirAll(nested): %v", err)
	}

	workspace, err := ResolveWorkspacePath(baseDir, workspaceDir)
	if err != nil {
		t.Fatalf("ResolveWorkspacePath(): %v", err)
	}
	if workspace.Folder != workspaceDir || workspace.RelName != externalDir || workspace.RelCWD != "nested" {
		t.Fatalf("workspace = %#v", workspace)
	}
}

func TestResolveWorkspacePathAllowsNonRepositoryWorkspace(t *testing.T) {
	baseDir := t.TempDir()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	workspaceDir := filepath.Join(homeDir, "notes")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatalf("MkdirAll(notes): %v", err)
	}

	workspace, err := ResolveWorkspacePath(baseDir, workspaceDir)
	if err != nil {
		t.Fatalf("ResolveWorkspacePath(): %v", err)
	}
	if workspace.Folder != workspaceDir || workspace.RelName != filepath.Join("~", "notes") || workspace.RelCWD != "" {
		t.Fatalf("workspace = %#v", workspace)
	}
}

func TestResolveWorkspacePathDescribesMissingWorkspaceForHistory(t *testing.T) {
	baseDir := t.TempDir()
	missing := filepath.Join(baseDir, "deleted-project")

	workspace, err := ResolveWorkspacePath(baseDir, missing)
	if err != nil {
		t.Fatalf("ResolveWorkspacePath(): %v", err)
	}
	if workspace.Exists {
		t.Fatal("workspace.Exists = true, want false")
	}
	if workspace.Folder != missing || workspace.RelName != "deleted-project" {
		t.Fatalf("workspace = %#v", workspace)
	}
}
