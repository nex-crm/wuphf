package team

// launcher_transports.go is the single registration site for all transport
// adapters. Both Launch() (tmux-mode) and LaunchWeb() (web-mode) call
// RegisterTransports after the broker is up. Adding a transport here
// automatically makes it available in both surfaces — you cannot accidentally
// wire it to one and miss the other.
//
// Phase 2a: wires the existing TelegramTransport when a bot token is present.
// The transport is started in a supervised goroutine; the returned cleanup
// function cancels it and must be called before broker.Stop() on any abort path.
//
// Phase 2b will refactor TelegramTransport onto the transport.Transport contract.
// Phase 3a/3b will do the same for OpenClaw.
// Phase 4 will do the same for human-share.
//
// See docs/ADD-A-TRANSPORT.md for the full contributor guide.

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/nex-crm/wuphf/internal/config"
)

// RegisterTransports registers all configured transport adapters against the
// broker. Called once per launch after broker.Start() succeeds. Returns a
// cleanup function and an optional error. The cleanup function cancels all
// running adapters and must be called before broker.Stop() on any early-abort
// path so adapters can flush in-flight sends. It is always non-nil and safe to
// call even when err is non-nil.
//
// A non-nil error means a required adapter (one whose config is present but
// invalid) failed to start; optional adapters that are not configured are
// silently skipped. Callers log the error and continue — a misconfigured
// Telegram token should not prevent the office from starting.
func RegisterTransports(b *Broker) (func(), error) {
	var stops []func()
	cleanup := func() {
		for _, stop := range stops {
			stop()
		}
	}

	// Telegram: start if a bot token is configured and the broker has at least
	// one telegram surface channel. If the token is set but there are no
	// surface channels yet (user hasn't connected via /connect), skip silently —
	// the transport will start on the next launch after a channel is connected.
	token := resolveTelegramToken()
	if token != "" {
		t := NewTelegramTransport(b, token)
		if len(t.ChatMap) == 0 && t.DMChannel == "" {
			log.Printf("[transport] telegram: token present but no channels connected yet — skipping")
		} else {
			ctx, cancel := context.WithCancel(context.Background())
			stops = append(stops, cancel)
			go func() {
				if err := t.Start(ctx); err != nil && ctx.Err() == nil {
					log.Printf("[transport] telegram: exited with error: %v", err)
				}
			}()
			log.Printf("[transport] telegram: started (%d group(s), dm=%v)", len(t.ChatMap), t.DMChannel != "")
		}
	}

	// Phase 3a TODO: start OpenClaw bridge when gateway URL + token are configured.
	// Phase 4 TODO: start human-share adapter when share is enabled.

	return cleanup, nil
}

// resolveTelegramToken returns the bot token from the environment or persisted
// config. Prefers WUPHF_TELEGRAM_BOT_TOKEN so CI/dev overrides work without
// touching config.json.
func resolveTelegramToken() string {
	if v := os.Getenv("WUPHF_TELEGRAM_BOT_TOKEN"); v != "" {
		return v
	}
	return config.ResolveTelegramBotToken()
}

// startupTransportError wraps an adapter startup failure with the adapter name
// so the caller's warning log identifies which transport failed.
type startupTransportError struct {
	adapter string
	err     error
}

func (e *startupTransportError) Error() string {
	return fmt.Sprintf("transport %q failed to start: %v", e.adapter, e.err)
}

func (e *startupTransportError) Unwrap() error { return e.err }
