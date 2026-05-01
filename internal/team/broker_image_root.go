package team

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

// imagegenArtistRoot returns the on-disk directory that holds Artist's
// generated images. Mirrors imagegen.outputDir() but lives in the team
// package so we don't expose an internal helper from internal/imagegen.
// Override with WUPHF_IMAGEGEN_DIR; default ~/.wuphf/office/artist.
func imagegenArtistRoot() string {
	if root := strings.TrimSpace(os.Getenv("WUPHF_IMAGEGEN_DIR")); root != "" {
		return root
	}
	if home := config.RuntimeHomeDir(); home != "" {
		return filepath.Join(home, ".wuphf", "office", "artist")
	}
	return filepath.Join(".wuphf", "office", "artist")
}
