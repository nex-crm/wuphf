//go:build windows

package team

import (
	"context"
	"errors"
)

// defaultComposioInstaller is a no-op on Windows: the official installer is a
// `curl | bash` pipeline with no Windows equivalent, so auto-install reports
// not-supported and the sign-in flow falls back to surfacing the manual install
// command (cli_missing). The returned error is advisory — composioSigninAutoInstall
// treats the CLI's presence on PATH, not this exit, as the source of truth.
func defaultComposioInstaller(_ context.Context) error {
	return errors.New("automatic Composio CLI install is not supported on Windows; run the install command manually")
}
