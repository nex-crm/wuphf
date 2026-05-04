package team

// launcher_transports.go is the single registration site for all transport
// adapters. Both Launch() (tmux-mode) and LaunchWeb() (web-mode) call
// RegisterTransports after the broker is up. Adding a transport here
// automatically makes it available in both surfaces — you cannot accidentally
// wire it to one and miss the other.
//
// Phase 1: RegisterTransports is the stub. No real adapters are registered yet.
// The function signature and call sites in Launch/LaunchWeb/launchHeadlessCodex
// are established so Phase 2a can add the Telegram wiring in a single diff.
//
// See docs/ADD-A-TRANSPORT.md for the full contributor guide.

import (
	"github.com/nex-crm/wuphf/internal/team/transport"
)

// RegisterTransports registers all configured transport adapters against host.
// Called once per launch after broker.Start() succeeds. Returns a cleanup
// function and an optional error. The cleanup function stops all registered
// adapters and must be called before broker.Stop() on any early-abort path.
// It is always non-nil and safe to call even when err is non-nil.
//
// A non-nil error means a required adapter (one whose config is present but
// invalid) failed to register; optional adapters that are not configured are
// silently skipped. Callers log the error and continue — a misconfigured
// Telegram token should not prevent the office from starting.
func RegisterTransports(_ transport.Host) (func(), error) {
	// Phase 2a TODO: check cfg for TELEGRAM_BOT_TOKEN; if set, construct
	// TelegramTransport, start its Run goroutine, and add its Stop to cleanup.
	//
	// Phase 3a TODO: check cfg for OpenClaw gateway URL + token; if set,
	// construct OpenclawBridge adapter and add to cleanup.
	//
	// Phase 4 TODO: register human-share adapter when share is enabled.
	return func() {}, nil
}
