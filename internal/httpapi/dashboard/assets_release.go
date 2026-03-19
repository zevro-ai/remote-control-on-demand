//go:build release

package dashboard

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil
	}
	return sub
}
