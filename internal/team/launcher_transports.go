package team

// launcher_transports.go is the single registration site for all transport
// adapters. Both Launch() (tmux-mode) and LaunchWeb() (web-mode) call
// RegisterTransports after the broker is up. Adding a transport here
// automatically makes it available in both surfaces — you cannot accidentally
// wire it to one and miss the other.
//
// Phase 2b: TelegramTransport satisfies transport.Transport. RegisterTransports
// constructs a brokerTransportHost and passes it to Run so inbound messages flow
// through the Host contract instead of writing to the broker directly.
//
// Phase 4: OpenClaw bridge is built here via BuildOpenclawBridgeFromConfig and
// driven via OpenclawBridge.Run with the same brokerTransportHost so inbound
// assistant messages flow through host.ReceiveMessage. The build returns
// (nil, nil) when no openclaw members and no gateway URL are configured, so
// the integration remains strictly opt-in. Future phases will wire human-share
// via OfficeBoundTransport.
//
// See docs/ADD-A-TRANSPORT.md for the full contributor guide.

import (
	"context"
	"log"

	"github.com/nex-crm/wuphf/internal/config"
)

// RegisterTransports registers all configured transport adapters against the
// broker. Called once per launch after broker.Start() succeeds. Returns a
// cleanup function that cancels all running adapters; always non-nil and safe to
// call even on the error path. The error return is reserved for future required
// adapters; all current adapters are optional and log failures rather than
// returning them.
func RegisterTransports(b *Broker) (func(), error) {
	var stops []func()
	cleanup := func() {
		for _, stop := range stops {
			stop()
		}
	}

	// Telegram: start if a bot token is configured and the broker has at least
	// one telegram surface channel. If the token is set but there are no
	// surface channels yet (user hasn't run /connect), skip silently — the
	// transport will start on the next launch after a channel is connected.
	token := config.ResolveTelegramBotToken()
	if token != "" {
		t := NewTelegramTransport(b, token)
		if len(t.ChatMap) == 0 && t.DMChannel == "" {
			log.Printf("[transport] telegram: token present but no channels connected yet — skipping")
		} else {
			host := &brokerTransportHost{broker: b}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			stops = append(stops, func() {
				cancel()
				<-done // wait for Run to return before broker.Stop()
			})
			go func() {
				defer close(done)
				if err := t.Run(ctx, host); err != nil && ctx.Err() == nil {
					log.Printf("[transport] telegram: exited with error: %v", err)
				}
			}()
			log.Printf("[transport] telegram: started (%d group(s), dm=%v)", len(t.ChatMap), t.DMChannel != "")
		}
	}

	// OpenClaw: build when openclaw members exist or a gateway URL is configured.
	// BuildOpenclawBridgeFromConfig returns (nil, nil) when neither condition
	// holds — the bridge is strictly opt-in and its absence is not an error.
	// Drive via Run(ctx, host) so inbound assistant messages flow through the
	// transport.Host contract rather than writing to the broker directly.
	bridge, ocErr := BuildOpenclawBridgeFromConfig(b)
	if ocErr != nil {
		log.Printf("[transport] openclaw: bootstrap error — %v", ocErr)
	} else if bridge != nil {
		b.AttachOpenclawBridge(bridge)
		ocCtx, ocCancel := context.WithCancel(context.Background())
		routerDone := StartOpenclawRouter(ocCtx, b, bridge)
		runDone := make(chan struct{})
		host := &brokerTransportHost{broker: b}
		go func() {
			defer close(runDone)
			if err := bridge.Run(ocCtx, host); err != nil && ocCtx.Err() == nil {
				log.Printf("[transport] openclaw: exited with error: %v", err)
			}
		}()
		stops = append(stops, func() {
			ocCancel()
			<-routerDone // wait for router goroutine to exit before tearing down
			<-runDone    // bridge.Run returns after Stop; ensures clean broker shutdown
		})
		log.Printf("[transport] openclaw: started (%d session(s))", len(bridge.SnapshotBindings()))
	}

	// Future: wire human-share adapter via OfficeBoundTransport.

	return cleanup, nil
}
