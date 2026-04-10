//go:build !release

package dashboard

import (
	"io/fs"
	"os"
)

// FS returns dashboard assets for non-release builds.
func FS() fs.FS {
	// Limit static serving to dashboard build artifacts only.
	// If app/dist is absent, the server will return 404 for SPA routes.
	return os.DirFS("app/dist")
}
