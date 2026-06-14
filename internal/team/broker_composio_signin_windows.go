//go:build windows

package team

import (
	"context"
	"errors"
)

// composioInstallCommand on Windows is the cross-platform npm install: the
// official `curl | bash` script has no Windows equivalent, and npm is Composio's
// recommended install path off Unix. Surfaced to the user as the manual
// fallback (auto-install is not attempted on Windows).
const composioInstallCommand = "npm install -g @composio/cli"

// defaultComposioInstaller is a no-op on Windows: the official installer is a
// `curl | bash` pipeline with no Windows equivalent, so auto-install reports
// not-supported and the sign-in flow falls back to surfacing the manual install
// command (cli_missing). The returned error is advisory — composioSigninAutoInstall
// treats the CLI's presence on PATH, not this exit, as the source of truth.
func defaultComposioInstaller(_ context.Context) error {
	return errors.New("automatic Composio CLI install is not supported on Windows; run the install command manually")
}
