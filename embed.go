// Package wuphf provides the embedded web UI bundle shipped with the
// binary. The bundle is built from web/dist/ at release time and embedded
// so single-binary installs (`curl | bash`) have a working UI without any
// external files on disk.
package wuphf

import (
	"embed"
	"io/fs"
)

//go:embed all:web/dist
var webBundle embed.FS

// WebFS returns the embedded web/dist filesystem with the "web/dist" prefix
// stripped, so callers can serve it as the web root. Returns ok=false if
// the embed is empty (dev build without `npm run build`), in which case
// callers should fall back to a filesystem lookup or the legacy UI.
func WebFS() (fs.FS, bool) {
	sub, err := fs.Sub(webBundle, "web/dist")
	if err != nil {
		return nil, false
	}
	// .gitkeep is the only file when no build has run; check for index.html
	// to decide whether the bundle is populated.
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, false
	}
	return sub, true
}
