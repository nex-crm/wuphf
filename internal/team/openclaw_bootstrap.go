package team

import (
	"context"
	"fmt"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/openclaw"
)

// openclawBootstrapDialer is an override hook for tests. When non-nil it is
// used instead of dialing the real gateway. Never set in production paths.
var openclawBootstrapDialer openclawDialer

// StartOpenclawBridgeFromConfig reads persisted OpenClaw bridge bindings from
// config and, if any are configured, dials the gateway and starts a supervised
// OpenclawBridge. Returns (nil, nil) when no bindings are configured so callers
// can treat the integration as strictly opt-in.
//
// The returned bridge's Stop should be called at shutdown to drain the event
// loop and close the gateway connection cleanly.
func StartOpenclawBridgeFromConfig(ctx context.Context, broker *Broker) (*OpenclawBridge, error) {
	if broker == nil {
		return nil, fmt.Errorf("openclaw bootstrap: broker is required")
	}
	cfg, _ := config.Load()
	if len(cfg.OpenclawBridges) == 0 {
		return nil, nil
	}

	dialer := openclawBootstrapDialer
	if dialer == nil {
		dialer = defaultOpenclawDialer
	}

	bridge := NewOpenclawBridgeWithDialer(broker, nil, dialer, cfg.OpenclawBridges)
	// Register each bridged session as an office member before starting the
	// supervise loop. Without this, bridged agents post messages into #general
	// but don't appear in the sidebar or the @mention autocomplete, so users
	// can't actually discover or talk to them.
	for _, b := range cfg.OpenclawBridges {
		name := b.DisplayName
		if name == "" {
			name = b.Slug
		}
		if err := broker.EnsureBridgedMember(b.Slug, name, "openclaw"); err != nil {
			return nil, fmt.Errorf("register bridged member %q: %w", b.Slug, err)
		}
	}
	if err := bridge.Start(ctx); err != nil {
		return nil, fmt.Errorf("openclaw bridge start: %w", err)
	}
	return bridge, nil
}

// routeOpenclawMentionsLoop subscribes to broker messages and forwards every
// human-authored @mention of a bridged slug to the OpenClaw bridge via
// OnOfficeMessage. Agent-to-agent mentions are intentionally skipped to
// prevent broadcast loops (mirroring the thread auto-tag decision in broker.go).
// The loop exits when ctx is cancelled and the subscriber channel drains.
func routeOpenclawMentionsLoop(ctx context.Context, broker *Broker, bridge *OpenclawBridge) {
	if broker == nil || bridge == nil {
		return
	}
	msgs, unsubscribe := broker.SubscribeMessages(128)
	defer unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-msgs:
			if !ok {
				return
			}
			if msg.From == "system" {
				continue
			}
			// Only route human-authored messages; agent cross-talk flows
			// through the bridge via explicit sends from handler code, not
			// by re-dispatching every agent message to the gateway.
			if msg.From != "you" && msg.From != "human" {
				continue
			}
			if len(msg.Tagged) == 0 {
				continue
			}
			for _, slug := range msg.Tagged {
				if !bridge.HasSlug(slug) {
					continue
				}
				// Best-effort: OnOfficeMessage retries internally and posts
				// its own system message on permanent failure, so we do
				// not propagate the error here.
				go func(slug string) {
					_ = bridge.OnOfficeMessage(ctx, slug, msg.Content)
				}(slug)
			}
		}
	}
}

// defaultOpenclawDialer is the production dialer. It resolves URL, token, and
// device identity at dial-time so rotated credentials take effect on reconnect
// without a WUPHF restart. OpenClaw rejects token-only clients with zero scopes,
// so loading the Ed25519 identity is non-optional.
func defaultOpenclawDialer(ctx context.Context) (openclawClient, error) {
	url := config.ResolveOpenclawGatewayURL()
	token := config.ResolveOpenclawToken()
	identity, err := openclaw.LoadOrCreateDeviceIdentity(config.ResolveOpenclawIdentityPath())
	if err != nil {
		return nil, fmt.Errorf("openclaw identity: %w", err)
	}
	c, err := openclaw.Dial(ctx, openclaw.Config{URL: url, Token: token, Identity: identity})
	if err != nil {
		return nil, err
	}
	return c, nil
}
