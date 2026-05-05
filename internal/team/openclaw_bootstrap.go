package team

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/openclaw"
	"github.com/nex-crm/wuphf/internal/provider"
)

// openclawBootstrapDialer is an override hook for tests. When non-nil it is
// used instead of dialing the real gateway. Never set in production paths.
var openclawBootstrapDialer openclawDialer

// BuildOpenclawBridgeFromConfig reads persisted OpenClaw bridge bindings from
// config and, if any are configured, constructs (but does not Start) an
// OpenclawBridge. Returns (nil, nil) when no bindings are configured so callers
// can treat the integration as strictly opt-in.
//
// Build is the host-driven entrypoint: callers (the launcher) own the bridge
// lifecycle via [OpenclawBridge.Run], which attaches a transport.Host before
// the supervised loop starts. The legacy [StartOpenclawBridgeFromConfig] wraps
// Build + Start for callers that drive the bridge directly (probes, tests).
func BuildOpenclawBridgeFromConfig(broker *Broker) (*OpenclawBridge, error) {
	if broker == nil {
		return nil, fmt.Errorf("openclaw bootstrap: broker is required")
	}

	// Migration: any legacy config.OpenclawBridges entries are converted into
	// per-agent ProviderBindings on matching members. After this runs, the
	// source of truth for bridged sessions is the office member roster.
	legacy, _, err := config.MigrateOpenclawBridgesFromConfig()
	if err != nil {
		return nil, fmt.Errorf("openclaw migration: %w", err)
	}
	for _, bind := range legacy {
		name := bind.DisplayName
		if name == "" {
			name = bind.Slug
		}
		if err := broker.EnsureBridgedMember(bind.Slug, name, "openclaw"); err != nil {
			return nil, fmt.Errorf("migrate bridged member %q: %w", bind.Slug, err)
		}
		if err := broker.SetMemberProvider(bind.Slug, provider.ProviderBinding{
			Kind:     provider.KindOpenclaw,
			Openclaw: &provider.OpenclawProviderBinding{SessionKey: bind.SessionKey},
		}); err != nil {
			return nil, fmt.Errorf("attach provider to %q: %w", bind.Slug, err)
		}
	}

	// Collect the current set of openclaw-bound members to seed the bridge.
	type bridgedSlug struct{ Slug, SessionKey string }
	var bridged []bridgedSlug
	for _, m := range broker.OfficeMembers() {
		if m.Provider.Kind != provider.KindOpenclaw || m.Provider.Openclaw == nil {
			continue
		}
		bridged = append(bridged, bridgedSlug{Slug: m.Slug, SessionKey: m.Provider.Openclaw.SessionKey})
	}

	// Decide whether to build the bridge. We build it when EITHER there are
	// already openclaw members (the classic case) OR the gateway is reachable
	// via configured URL + token (so /office-members POST can live-hire a new
	// openclaw agent without requiring a pre-existing one). Without this, the
	// first openclaw hire on a fresh install would fail with "bridge not
	// active," which is exactly the chicken-and-egg we want to avoid.
	cfg, _ := config.Load()
	gatewayConfigured := strings.TrimSpace(cfg.OpenclawGatewayURL) != "" || strings.TrimSpace(os.Getenv("WUPHF_OPENCLAW_GATEWAY_URL")) != "" || strings.TrimSpace(os.Getenv("NEX_OPENCLAW_GATEWAY_URL")) != ""
	if len(bridged) == 0 && !gatewayConfigured {
		return nil, nil
	}

	dialer := openclawBootstrapDialer
	if dialer == nil {
		dialer = defaultOpenclawDialer
	}

	// bindings slice is kept for bridge bookkeeping; AttachSlug seeds the
	// runtime maps so live add/remove during a session works correctly.
	bindings := make([]config.OpenclawBridgeBinding, 0, len(bridged))
	for _, b := range bridged {
		bindings = append(bindings, config.OpenclawBridgeBinding{Slug: b.Slug, SessionKey: b.SessionKey})
	}
	return NewOpenclawBridgeWithDialer(broker, nil, dialer, bindings), nil
}

// StartOpenclawBridgeFromConfig builds the bridge and Starts it directly,
// bypassing transport.Host attachment. Inbound messages route via
// broker.PostInboundSurfaceMessage rather than host.ReceiveMessage. Use
// [BuildOpenclawBridgeFromConfig] + [OpenclawBridge.Run] to opt into the
// host-driven path.
func StartOpenclawBridgeFromConfig(ctx context.Context, broker *Broker) (*OpenclawBridge, error) {
	bridge, err := BuildOpenclawBridgeFromConfig(broker)
	if err != nil || bridge == nil {
		return bridge, err
	}
	if err := bridge.Start(ctx); err != nil {
		return nil, fmt.Errorf("openclaw bridge start: %w", err)
	}
	return bridge, nil
}

// StartOpenclawRouter starts the mention+DM routing goroutine. Exported so
// out-of-package callers (e.g. bridge probes) can opt into the same routing
// behavior production WUPHF runs via launcher.go. The goroutine exits when
// ctx is cancelled. The returned channel is closed when the goroutine has
// fully exited — callers should block on it before stopping the bridge to
// avoid races between in-flight broker writes and broker shutdown.
func StartOpenclawRouter(ctx context.Context, broker *Broker, bridge *OpenclawBridge) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		routeOpenclawMentionsLoop(ctx, broker, bridge)
	}()
	return done
}

// routeOpenclawMentionsLoop subscribes to broker messages and forwards
// human-authored posts to the OpenClaw bridge via OnOfficeMessage in two cases:
//
//  1. Channel posts that @mention a bridged slug (one forward per mention).
//  2. 1:1 DM posts whose partner slug is bridged (no @mention required).
//
// Agent-to-agent mentions are intentionally skipped to prevent broadcast loops
// (mirroring the thread auto-tag decision in broker.go). The loop exits when
// ctx is cancelled and the subscriber channel drains.
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
			if !isHumanMessageSender(msg.From) {
				continue
			}

			// Collect the set of bridged slugs to forward this message to.
			// Mentions and DM partner can both apply; dedupe so a DM that
			// also happens to @mention the same agent isn't double-fired.
			targets := make(map[string]struct{})
			for _, slug := range msg.Tagged {
				if bridge.HasSlug(slug) {
					targets[slug] = struct{}{}
				}
			}
			if partner := broker.DMPartner(msg.Channel); partner != "" && bridge.HasSlug(partner) {
				targets[partner] = struct{}{}
			}

			for slug := range targets {
				// Best-effort: OnOfficeMessage retries internally and posts
				// its own system message on permanent failure, so we do
				// not propagate the error here.
				go func(slug, channel, content string) {
					_ = bridge.OnOfficeMessage(ctx, slug, channel, content)
				}(slug, msg.Channel, msg.Content)
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
