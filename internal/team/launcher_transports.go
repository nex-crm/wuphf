package team

// launcher_transports.go is the single registration site for all transport
// adapters. Both Launch() (tmux-mode) and LaunchWeb() (web-mode) call
// RegisterTransports after the broker is up. Adding a transport here
// automatically makes it available in both surfaces — you cannot accidentally
// wire it to one and miss the other.
//
// Each adapter is driven via Run(ctx, host) on a per-transport goroutine using
// a shared brokerTransportHost so inbound messages flow through the Host
// contract instead of writing to the broker directly. Adapters whose
// configuration is absent are skipped silently; their absence is not an error.
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

	// OpenClaw: opt-in when members exist or a gateway URL is configured.
	bridge, ocErr := BuildOpenclawBridgeFromConfig(b)
	if ocErr != nil {
		log.Printf("[transport] openclaw: bootstrap error — %v", ocErr)
	} else if bridge != nil {
		b.AttachOpenclawBridge(bridge)
		// Router and bridge get independent contexts so the cleanup can drain
		// the router (in-flight broker writes) before cancelling the bridge.
		// Sharing one context would let bridge.Run trigger b.Stop() while the
		// router still has messages mid-flight — exactly the race the
		// StartOpenclawRouter doc warns against.
		routerCtx, routerCancel := context.WithCancel(context.Background())
		bridgeCtx, bridgeCancel := context.WithCancel(context.Background())
		routerDone := StartOpenclawRouter(routerCtx, b, bridge)
		runDone := make(chan struct{})
		host := &brokerTransportHost{broker: b}
		go func() {
			defer close(runDone)
			if err := bridge.Run(bridgeCtx, host); err != nil && bridgeCtx.Err() == nil {
				log.Printf("[transport] openclaw: exited with error: %v", err)
			}
		}()
		stops = append(stops, func() {
			routerCancel()
			<-routerDone // drain router before tearing down the bridge
			bridgeCancel()
			<-runDone // bridge.Run returns after Stop; ensures clean broker shutdown
			b.AttachOpenclawBridge(nil)
		})
		log.Printf("[transport] openclaw: started (%d session(s))", len(bridge.SnapshotBindings()))
	}

	// Human-share: always registered. The adapter wraps the in-process
	// invite/session surface in broker_human_share.go so RegisterTransports
	// owns the OfficeBoundTransport lifecycle alongside Telegram and OpenClaw.
	// RelativeJoinURL is the constructor builder; the in-process share
	// controller (cmd/wuphf/share.go) installs an absolute-URL builder via
	// ShareTransport.SetURLBuilder once it knows its bind address. The
	// constructor builder remains the safe default for any caller that reads
	// CreateInvite before the controller has started. Registering the handle
	// on the broker via SetShareTransport lets the controller obtain the
	// adapter without a separate plumbing channel.
	share := NewShareTransport(b, RelativeJoinURL)
	b.SetShareTransport(share)
	shareCtx, shareCancel := context.WithCancel(context.Background())
	shareDone := make(chan struct{})
	shareHost := &brokerTransportHost{broker: b}
	go func() {
		defer close(shareDone)
		if err := share.Run(shareCtx, shareHost); err != nil && shareCtx.Err() == nil {
			log.Printf("[transport] share: exited with error: %v", err)
		}
	}()
	stops = append(stops, func() {
		shareCancel()
		<-shareDone
		b.SetShareTransport(nil)
	})
	log.Printf("[transport] share: registered (human-share)")

	return cleanup, nil
}
