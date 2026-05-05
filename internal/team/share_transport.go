package team

// share_transport.go implements transport.OfficeBoundTransport on top of the
// existing human-share invite/session surface (broker_human_share.go). The
// adapter is a thin wrapper: invite + session storage and revocation already
// live on Broker; this file gives that surface an OfficeBoundTransport face so
// it can be registered alongside Telegram and OpenClaw via RegisterTransports.
//
// CreateInvite delegates to Broker.createHumanInvite and formats a join URL
// using an injected builder. The launcher does not currently know the share
// controller's bind address (the share controller lives in cmd/wuphf), so the
// builder is optional — when nil, CreateInvite returns the relative path
// "/join/<token>" so the contract is satisfied and callers can prepend their
// own host. RevokeInvite delegates to Broker.RevokeHumanInvite, then fans out
// host.RevokeParticipant for each session that was actually revoked, fulfilling
// the OfficeBoundTransport contract that the adapter (not the Host) drives the
// per-session teardown.

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// shareAdapterName is the stable identifier the broker uses to namespace
// participant keys created by this adapter. Changing this between releases
// would lose admitted-human identity across restarts.
const shareAdapterName = "human-share"

// JoinURLBuilder maps an invite token to a shareable URL. Optional — when nil,
// ShareTransport.CreateInvite returns the relative path "/join/<token>".
type JoinURLBuilder func(token string) string

// ShareTransport adapts the broker's human-share surface to the
// transport.OfficeBoundTransport contract. It holds no state of its own beyond
// the broker reference, the optional URL builder, and the host pointer set by
// Run.
type ShareTransport struct {
	broker     *Broker
	urlBuilder JoinURLBuilder
	host       atomic.Pointer[transport.Host]
	startedAt  atomic.Int64 // unix nanos; zero before Run, set on Run entry
}

// Compile-time assertion that ShareTransport satisfies OfficeBoundTransport.
// If the contract changes, the build breaks here, not at the registration site.
var _ transport.OfficeBoundTransport = (*ShareTransport)(nil)

// NewShareTransport constructs a ShareTransport bound to the given broker.
// urlBuilder is optional and used by CreateInvite to produce absolute join
// URLs; pass nil to fall back to a relative "/join/<token>" path.
func NewShareTransport(broker *Broker, urlBuilder JoinURLBuilder) *ShareTransport {
	return &ShareTransport{broker: broker, urlBuilder: urlBuilder}
}

// Name returns the stable adapter identifier.
func (s *ShareTransport) Name() string { return shareAdapterName }

// Binding declares the office scope. The adapter admits humans into the office
// itself rather than a specific channel or member, so MemberSlug and
// ChannelSlug are intentionally empty.
func (s *ShareTransport) Binding() transport.Binding {
	return transport.Binding{Scope: transport.ScopeOffice}
}

// Run stores the host atomically and blocks until ctx is cancelled. The
// human-share surface is in-process — the existing handlers in
// broker_human_share.go drive accept/revoke directly, so Run does not subscribe
// to anything external. A nil host is rejected so a misconfigured launcher
// fails loudly rather than silently degrading.
func (s *ShareTransport) Run(ctx context.Context, host transport.Host) error {
	if s == nil {
		return fmt.Errorf("share: nil transport")
	}
	if host == nil {
		return fmt.Errorf("share: Run requires a non-nil host")
	}
	s.host.Store(&host)
	s.startedAt.Store(time.Now().UnixNano())
	<-ctx.Done()
	return nil
}

// Send is a no-op for the human-share adapter. Office-wide messages reach
// admitted humans through the existing broker channels (the shared web UI
// polls /api/* routes on the same broker), so there is no external network for
// the adapter to push to. Returning nil keeps the OfficeBoundTransport contract
// honest: the adapter accepts the message and trusts the broker to deliver via
// its existing channel-message machinery.
func (s *ShareTransport) Send(_ context.Context, _ transport.Outbound) error {
	return nil
}

// Health reports Connected once Run has started; before Run is called Health
// returns Disconnected. The share surface is in-process and has no upstream
// dependency to fail, so once Run is live the state is steady.
func (s *ShareTransport) Health() transport.Health {
	if s == nil || s.startedAt.Load() == 0 {
		return transport.Health{State: transport.HealthDisconnected}
	}
	return transport.Health{
		State:         transport.HealthConnected,
		LastSuccessAt: time.Unix(0, s.startedAt.Load()),
	}
}

// CreateInvite creates a new human-share invite via Broker.createHumanInvite
// and returns the join URL. The network argument is accepted to satisfy the
// OfficeBoundTransport contract but is informational in v1 — the share
// controller decides bind address out-of-band when it boots.
func (s *ShareTransport) CreateInvite(_ context.Context, _ string) (string, error) {
	if s == nil || s.broker == nil {
		return "", fmt.Errorf("share: CreateInvite: nil broker")
	}
	token, _, err := s.broker.createHumanInvite()
	if err != nil {
		return "", fmt.Errorf("share: CreateInvite: %w", err)
	}
	if s.urlBuilder != nil {
		return s.urlBuilder(token), nil
	}
	return "/join/" + token, nil
}

// RevokeInvite revokes the invite and every session it admitted. Per the
// OfficeBoundTransport contract the adapter is responsible for calling
// host.RevokeParticipant for each affected admitted human before returning;
// host.RevokeParticipant in turn closes the session in the broker. The two-step
// dance (broker.RevokeHumanInvite + host.RevokeParticipant) keeps the broker's
// revocation idempotent: if Host.RevokeParticipant errors mid-fan-out the
// invite is already marked revoked, so a retry will only fan out the remaining
// sessions.
func (s *ShareTransport) RevokeInvite(ctx context.Context, inviteID string) error {
	if s == nil || s.broker == nil {
		return fmt.Errorf("share: RevokeInvite: nil broker")
	}
	revokedSessions, err := s.broker.RevokeHumanInvite(inviteID)
	if err != nil {
		return fmt.Errorf("share: RevokeInvite %q: %w", inviteID, err)
	}
	hp := s.host.Load()
	if hp == nil {
		// Adapter not running under a host (e.g. Run never called) — the
		// broker-level revoke still happened above; nothing more to do.
		return nil
	}
	host := *hp
	var firstErr error
	for _, sid := range revokedSessions {
		if err := host.RevokeParticipant(ctx, shareAdapterName, sid); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("share: RevokeParticipant %s: %w", sid, err)
		}
	}
	return firstErr
}
