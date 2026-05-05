package team

// openclaw_transport.go implements transport.MemberBoundTransport on
// OpenclawBridge. This is the Phase 3b adapter contract — Name/Binding/Run/
// Send/Health satisfy the base Transport interface; AttachSlug, DetachSlug,
// and CreateSession satisfy the member-bound extension. The contract-level
// AttachSlug/DetachSlug are defined in openclaw.go alongside their
// synchronous error-returning siblings (AttachSlugAndSubscribe /
// DetachSlugAndUnsubscribe) used by HTTP handlers.
//
// Phase 4 will move the launcher's StartOpenclawBridgeFromConfig wiring onto
// the Host contract via Run(); for now Run() is a thin shim around the
// existing supervised loop so RegisterTransports can treat the bridge like
// any other Transport implementation.

import (
	"context"
	"fmt"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// adapterName is the stable identifier the broker uses to namespace
// participant keys created by this adapter. Changing this between releases
// would lose participant identity across restarts (per Transport.Name docs).
const openclawAdapterName = "openclaw"

// Compile-time assertion that OpenclawBridge satisfies MemberBoundTransport.
// If the contract changes, the build breaks here, not at the registration site.
var _ transport.MemberBoundTransport = (*OpenclawBridge)(nil)

// Name returns the stable adapter identifier.
func (b *OpenclawBridge) Name() string { return openclawAdapterName }

// Binding declares the adapter scope. OpenClaw is member-bound: each bridged
// session becomes an office member, but the bridge itself does not anchor to
// a single member slug (it manages many). Like Telegram's multi-channel
// pattern, we return a zero MemberSlug — honest declaration that no static
// member is bound at the adapter level.
func (b *OpenclawBridge) Binding() transport.Binding {
	return transport.Binding{Scope: transport.ScopeMember}
}

// Run starts the supervised bridge and blocks until ctx is cancelled. The
// host is attached before Start so handleClientEvent routes inbound assistant
// messages through host.ReceiveMessage instead of writing to the broker
// directly. Callers that drive the bridge via Start (probes, integration
// tests) bypass the host and fall back to the legacy broker entrypoint.
func (b *OpenclawBridge) Run(ctx context.Context, host transport.Host) error {
	if b == nil {
		return fmt.Errorf("openclaw: nil bridge")
	}
	b.attachHost(host)
	if err := b.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	b.Stop()
	return nil
}

// Send delivers one outbound message from the office to the bridged agent.
// Routes via OnOfficeMessage, which handles retries with a single reused
// idempotency key. The Outbound.Binding.MemberSlug identifies the target
// agent; ChannelSlug carries the reply-routing hint that handleClientEvent
// uses when the assistant reply arrives via the async event stream.
func (b *OpenclawBridge) Send(ctx context.Context, msg transport.Outbound) error {
	if b == nil {
		return fmt.Errorf("openclaw: nil bridge")
	}
	slug := msg.Binding.MemberSlug
	if slug == "" {
		return fmt.Errorf("openclaw: Send requires Binding.MemberSlug")
	}
	return b.OnOfficeMessage(ctx, slug, msg.Binding.ChannelSlug, msg.Text)
}

// Health returns a point-in-time connectivity snapshot. Reads only the cached
// health fields under healthMu so it is O(1) and safe to call on every
// channel-header render (per Transport.Health contract).
func (b *OpenclawBridge) Health() transport.Health {
	if b == nil {
		return transport.Health{State: transport.HealthDisconnected}
	}
	b.healthMu.RLock()
	lastSuccess := b.lastSuccessAt
	lastErr := b.lastError
	b.healthMu.RUnlock()

	state := transport.HealthDisconnected
	if b.breaker != nil && b.breaker.Open() {
		state = transport.HealthDisconnected
	} else if !lastSuccess.IsZero() && lastErr == nil {
		state = transport.HealthConnected
	} else if !lastSuccess.IsZero() && lastErr != nil {
		state = transport.HealthDegraded
	}
	return transport.Health{
		State:         state,
		LastSuccessAt: lastSuccess,
		LastError:     lastErr,
	}
}
