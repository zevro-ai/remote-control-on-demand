//go:build !release

package dashboard

import (
	"io/fs"
	"testing"
)

func TestDevFSDoesNotExposeRepositoryRoot(t *testing.T) {
	if _, err := fs.Stat(FS(), "go.mod"); err == nil {
		t.Fatal("expected go.mod to be inaccessible from dashboard FS")
	}
}
