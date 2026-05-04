package team

// launcher_transports.go is the single registration site for all transport
// adapters. Both Launch() (tmux-mode) and LaunchWeb() (web-mode) call
// RegisterTransports after the broker is up. Adding a transport here
// automatically makes it available in both surfaces — you cannot accidentally
// wire it to one and miss the other.
//
// Phase 1: RegisterTransports is the stub. No real adapters are registered yet.
// The function signature and call sites in Launch/LaunchWeb are established so
// Phase 2a can add the Telegram wiring in a single diff (one adapter, one line).
//
// See docs/ADD-A-TRANSPORT.md for the full contributor guide.

import (
	"github.com/nex-crm/wuphf/internal/team/transport"
)

// RegisterTransports registers all configured transport adapters against host.
// Called once per launch after broker.Start() succeeds. Returns a non-nil error
// only when a required adapter (one whose config is present but invalid) fails
// to register; optional adapters that are not configured are silently skipped.
//
// Callers (Launch, LaunchWeb) log the error and continue — a misconfigured
// Telegram token should not prevent the office from starting.
func RegisterTransports(_ transport.Host) error {
	// Phase 2a TODO: check cfg for TELEGRAM_BOT_TOKEN; if set, construct
	// TelegramTransport and call host.Register (once Host gains that method).
	//
	// Phase 3a TODO: check cfg for OpenClaw gateway URL + token; if set,
	// construct OpenclawBridge adapter and register.
	//
	// Phase 4 TODO: register human-share adapter when share is enabled.
	return nil
}
