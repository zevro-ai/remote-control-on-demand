//go:build !release

package dashboard

import (
	"io/fs"
	"os"
)

// FS returns an empty file system for local development
// to avoid errors when dist folder is missing.
func FS() fs.FS {
	// You could also use os.DirFS("app/dist") here if you want
	// to serve assets from disk during dev without embedding.
	return os.DirFS(".")
}
