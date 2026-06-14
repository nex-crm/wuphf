package team

import (
	"context"
	"log"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team/transport"
)

// broker_slack_transport.go owns the in-process lifecycle of the Slack transport
// so connecting a channel is a true hot-start: the Socket Mode loop and the
// outbound/card/entity goroutines spin up live, with no broker re-exec. Both the
// boot path (RegisterTransports) and the runtime path (handleSlackConnect) funnel
// through EnsureSlackTransportRunning, which is idempotent under slackTransportMu.

// EnsureSlackTransportRunning starts the Slack transport in-process when its
// prerequisites are met — both tokens configured (xoxb- Web API + xapp- Socket
// Mode) AND at least one connected "slack" surface channel — and is a no-op when
// they are not, or when a transport is already running. Idempotent and safe to
// call from boot and at runtime.
//
// Callers MUST NOT hold b.mu: NewSlackTransport reads the broker's surface
// channels via SurfaceChannels, which locks b.mu itself. handleSlackConnect calls
// this only after createSlackChannel has released the lock.
//
// When a transport is already running, the live ChannelMap is refreshed from the
// broker's current surface channels so a channel connected after start begins
// bridging without a restart (Socket Mode already receives every workspace event;
// inbound routing only filters by ChannelMap).
func (b *Broker) EnsureSlackTransportRunning() {
	b.slackTransportMu.Lock()
	defer b.slackTransportMu.Unlock()

	if b.slackTransport != nil {
		b.slackTransport.refreshChannelMap()
		return
	}

	botToken := config.ResolveSlackBotToken()
	appToken := config.ResolveSlackAppToken()
	if botToken == "" && appToken == "" {
		return // not configured — silent skip, mirrors the boot behavior
	}
	if botToken == "" || appToken == "" {
		log.Printf("[transport] slack: only one of SLACK_BOT_TOKEN / SLACK_APP_TOKEN set — both are required, skipping")
		return
	}

	st := NewSlackTransport(b, botToken, appToken)
	if len(st.ChannelMap) == 0 {
		log.Printf("[transport] slack: tokens present but no channels connected yet — skipping")
		return
	}

	host := &brokerTransportHost{broker: b}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	dispatchDone := make(chan struct{})
	cardsDone := make(chan struct{})
	entitiesDone := make(chan struct{})
	thinkingDone := make(chan struct{})
	reportingDone := make(chan struct{})

	go func() {
		defer close(done)
		if err := st.Run(ctx, host); err != nil && ctx.Err() == nil {
			log.Printf("[transport] slack: exited with error: %v", err)
		}
	}()
	// Host-driven outbound dispatcher: polls the broker queue and wires
	// FormatOutbound + Send. Owned here (not inside Run) per the Transport.Send
	// contract intent — same shape as the Telegram registration.
	go func() {
		defer close(dispatchDone)
		if err := runOutboundDispatcher(ctx, b, st.Name(), st.FormatOutbound, st.Send); err != nil && ctx.Err() == nil {
			log.Printf("[transport] slack: outbound dispatcher exited: %v", err)
		}
	}()
	go func() {
		defer close(cardsDone)
		if err := st.runTaskCardSync(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[transport] slack: task card sync exited: %v", err)
		}
	}()
	go func() {
		defer close(entitiesDone)
		if err := st.runEntityFactSync(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[transport] slack: entity fact sync exited: %v", err)
		}
	}()
	go func() {
		defer close(thinkingDone)
		if err := st.runAgentThinkingStatus(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[transport] slack: thinking-status loop exited: %v", err)
		}
	}()
	// Task-thread reporter: pings assignees on subtask assignment, mirrors
	// lifecycle transitions, and links new wiki articles into the right thread.
	go func() {
		defer close(reportingDone)
		if err := st.runTaskReporting(ctx); err != nil && ctx.Err() == nil {
			log.Printf("[transport] slack: task-reporting loop exited: %v", err)
		}
	}()

	b.slackTransport = st
	b.slackTransportStop = func() {
		cancel()
		<-done          // Run returns before broker.Stop()
		<-dispatchDone  // outbound dispatcher
		<-cardsDone     // task-card sync loop
		<-entitiesDone  // entity fact sync loop
		<-thinkingDone  // native thinking-status loop
		<-reportingDone // task-thread reporting loop
	}
	log.Printf("[transport] slack: started (%d channel(s))", len(st.ChannelMap))
}

// stopSlackTransport cancels the running Slack transport and waits for its
// goroutines to drain. Idempotent: a no-op when nothing is running, so it is safe
// to register as a RegisterTransports cleanup callback even when the transport
// never started. The whole stop runs under slackTransportMu so a concurrent
// EnsureSlackTransportRunning cannot observe a half-torn-down transport and
// double-start; none of the drained goroutines acquire slackTransportMu, so
// holding it across the wait cannot deadlock.
func (b *Broker) stopSlackTransport() {
	b.slackTransportMu.Lock()
	defer b.slackTransportMu.Unlock()
	if b.slackTransportStop != nil {
		b.slackTransportStop()
		b.slackTransportStop = nil
		b.slackTransport = nil
	}
}

// slackTransportRunning reports whether the in-process Slack transport has been
// started (its goroutines are spawned). It does NOT mean the Socket Mode
// connection is up — use slackTransportConnected for that.
func (b *Broker) slackTransportRunning() bool {
	b.slackTransportMu.Lock()
	defer b.slackTransportMu.Unlock()
	return b.slackTransport != nil
}

// slackTransportConnected reports whether the live Slack transport's Socket Mode
// connection is healthy. This is the signal the onboarding wizard polls after
// /slack/connect hot-starts the transport, so the "you're live" confirmation
// reflects a real connection rather than just a spawned goroutine. The transport
// ref is captured under slackTransportMu, then Health() is read on the transport's
// own mutex (no nested lock).
func (b *Broker) slackTransportConnected() bool {
	b.slackTransportMu.Lock()
	st := b.slackTransport
	b.slackTransportMu.Unlock()
	if st == nil {
		return false
	}
	return st.Health().State == transport.HealthConnected
}
