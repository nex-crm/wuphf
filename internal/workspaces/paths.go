// paths.go centralises home-directory resolution for cross-workspace
// artifacts (token files, the ~/.wuphf compatibility symlink, ~/.wuphf-spaces
// itself). Per the Phase-0 audit, anything that lives under ~/.wuphf-spaces/
// or alongside it must resolve to the user's REAL home — not under any
// workspace's WUPHF_RUNTIME_HOME — or sibling brokers cannot find each
// other and tokens written by workspace A cannot be read by workspace B.
package workspaces

import (
	"errors"
	"os"

	"github.com/nex-crm/wuphf/internal/config"
)

// realHomeDir returns the user's real home directory. It prefers
// os.UserHomeDir() and falls back to config.RuntimeHomeDir() only when the
// real lookup fails (e.g. tests that explicitly clear HOME). This matches
// the spacesDir/symlinkPaths pattern: cross-workspace artifacts must NOT
// live under WUPHF_RUNTIME_HOME, since that env var is per-workspace.
//
// user-global; intentionally NOT under WUPHF_RUNTIME_HOME — token files and
// the ~/.wuphf compatibility symlink are shared cross-workspace and must
// live at the user's real HOME.
func realHomeDir() (string, error) {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home, nil
	}
	if home := config.RuntimeHomeDir(); home != "" {
		return home, nil
	}
	return "", errors.New("workspaces: cannot resolve home directory")
}
