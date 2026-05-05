package team

// share_transport.go implements transport.OfficeBoundTransport on top of the
// existing human-share invite/session surface (broker_human_share.go). The
// adapter is a thin wrapper: invite + session storage and revocation already
// live on Broker; this file gives that surface an OfficeBoundTransport face so
// it can be registered alongside Telegram and OpenClaw via RegisterTransports.
//
// CreateInvite delegates to Broker.createHumanInvite and asks the injected
// JoinURLBuilder to format the shareable URL. RevokeInvite calls
// Broker.RevokeHumanInvite, then fans out host.RevokeParticipant for each
// session that was active under the invite — fulfilling the contract clause
// that the adapter (not the Host) drives the per-session teardown after invite
// revocation.
//
// v1 lifecycle invariant: only the revoke half of the OfficeBoundTransport
// admit/revoke pair is wired through the transport host. Admit happens via the
// in-process /humans/invites/accept HTTP handler, which calls
// Broker.acceptHumanInvite directly; the transport host's UpsertParticipant is
// never invoked for share-admitted humans. This is intentional — admitted
// humans poll the broker via /api/* routes rather than through the transport
// contract — but a future phase that wants Host.ReceiveMessage for share
// participants must close this gap by also calling Host.UpsertParticipant from
// the accept path.

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// shareAdapterName is the stable identifier the broker uses to namespace
// participant keys created by this adapter. Per Transport.Name() it must be a
// valid Go identifier (lowercase, no spaces or hyphens). Changing this between
// releases would lose admitted-human identity across restarts.
const shareAdapterName = "share"

// JoinURLBuilder maps an invite token to a shareable URL. Required —
// NewShareTransport panics on a nil builder so a misconfigured launcher fails
// loudly rather than producing relative-path URLs that look fine until a
// remote user tries to click them.
type JoinURLBuilder func(token string) string

// RelativeJoinURL is the degenerate builder used when no absolute host is
// known (e.g. the launcher does not yet know the share controller's bind
// address). It returns "/join/<token>" so callers can prepend their own host
// and the contract is satisfied with a non-empty result.
func RelativeJoinURL(token string) string { return "/join/" + token }

// ShareTransport adapts the broker's human-share surface to the
// transport.OfficeBoundTransport contract. It holds no state of its own beyond
// the broker reference, the URL builder, and the host pointer set by Run.
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
// urlBuilder must be non-nil; pass [RelativeJoinURL] when no absolute base is
// known so callers see an explicit relative-path choice rather than a silent
// nil-builder fallback.
func NewShareTransport(broker *Broker, urlBuilder JoinURLBuilder) *ShareTransport {
	if urlBuilder == nil {
		panic("team: NewShareTransport: urlBuilder is required (use RelativeJoinURL for the degenerate case)")
	}
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
// broker_human_share.go drive accept directly, so Run does not subscribe to
// anything external. A nil host is rejected so a misconfigured launcher fails
// loudly rather than silently degrading.
//
// See the file header for the v1 lifecycle invariant: only the revoke half of
// the OfficeBoundTransport admit/revoke pair flows through the transport host
// today.
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
// its existing channel-message machinery. This is a conscious contract choice,
// not an oversight — a future push transport (WebSocket, SSE) for share-
// admitted humans would replace this no-op with real delivery.
func (s *ShareTransport) Send(_ context.Context, _ transport.Outbound) error {
	return nil
}

// Health reports Connected once Run has started; before Run is called Health
// returns Disconnected. The share surface is in-process and has no upstream
// dependency to fail, so once Run is live the state is steady.
func (s *ShareTransport) Health() transport.Health {
	if s == nil {
		return transport.Health{State: transport.HealthDisconnected}
	}
	nano := s.startedAt.Load()
	if nano == 0 {
		return transport.Health{State: transport.HealthDisconnected}
	}
	return transport.Health{
		State:         transport.HealthConnected,
		LastSuccessAt: time.Unix(0, nano),
	}
}

// CreateInvite creates a new human-share invite via Broker.createHumanInvite
// and returns the join URL produced by the injected JoinURLBuilder. The
// network argument is part of the OfficeBoundTransport contract but
// ShareTransport ignores it: URL construction is controlled by the builder,
// which the share controller selects based on its own bind logic.
func (s *ShareTransport) CreateInvite(_ context.Context, _ string) (string, error) {
	if s == nil || s.broker == nil {
		return "", fmt.Errorf("share: CreateInvite: nil broker")
	}
	token, _, err := s.broker.createHumanInvite()
	if err != nil {
		return "", fmt.Errorf("share: CreateInvite: %w", err)
	}
	return s.urlBuilder(token), nil
}

// RevokeInvite revokes the invite and every session it admitted. Per the
// OfficeBoundTransport contract the adapter is responsible for calling
// host.RevokeParticipant for each affected admitted human before returning;
// host.RevokeParticipant in turn closes the session in the broker. The two-step
// dance (broker.RevokeHumanInvite + host.RevokeParticipant) keeps the broker's
// revocation idempotent: if Host.RevokeParticipant errors mid-fan-out the
// invite is already marked revoked, so a retry will only fan out the remaining
// sessions.
//
// Errors from individual host.RevokeParticipant calls are accumulated via
// errors.Join so a partial fan-out does not silently hide later failures
// behind the first one. The loop runs to completion on every call; failing
// fast on the first error would leave later sessions live.
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
	var errs []error
	for _, sid := range revokedSessions {
		if err := host.RevokeParticipant(ctx, shareAdapterName, sid); err != nil {
			errs = append(errs, fmt.Errorf("share: RevokeParticipant %s: %w", sid, err))
		}
	}
	return errors.Join(errs...)
}
