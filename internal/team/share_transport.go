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
// Admit lifecycle: Run installs Broker.SetHumanAdmitHook so every successful
// invite acceptance via the in-process /humans/invites/accept HTTP handler
// fans out to Host.UpsertParticipant for the new admitted human. The hook is
// cleared on Run exit so a stale closure cannot keep firing after shutdown.
// With both halves of admit/revoke flowing through the transport host,
// Host.ReceiveMessage for share participants is no longer blocked on a
// missing UpsertParticipant call — admitted humans are first-class
// transport.Host participants.

import (
	"context"
	"errors"
	"fmt"
	"log"
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
// the broker reference, the URL builders, and the host pointer set by Run.
//
// Two URL builders exist: the immutable constructor builder (urlBuilder) and
// an optional override (urlBuilderOverride) that the in-process share
// controller installs once it knows its bind address. CreateInvite reads the
// override first; absent an override it falls back to the constructor builder.
// This split keeps the constructor builder (typically RelativeJoinURL) safe as
// a default while letting the controller upgrade to absolute URLs without a
// re-construction dance.
type ShareTransport struct {
	broker             *Broker
	urlBuilder         JoinURLBuilder
	urlBuilderOverride atomic.Pointer[JoinURLBuilder]
	host               atomic.Pointer[transport.Host]
	startedAt          atomic.Int64 // unix nanos; zero before Run, set on Run entry
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

// SetURLBuilder installs an override URL builder. Passing nil clears the
// override so CreateInvite falls back to the constructor builder. Atomic so
// the broker hot path that calls CreateInvite does not contend with the
// controller installing the override on start.
func (s *ShareTransport) SetURLBuilder(b JoinURLBuilder) {
	if s == nil {
		return
	}
	if b == nil {
		s.urlBuilderOverride.Store(nil)
		return
	}
	s.urlBuilderOverride.Store(&b)
}

// Name returns the stable adapter identifier.
func (s *ShareTransport) Name() string { return shareAdapterName }

// Binding declares the office scope. The adapter admits humans into the office
// itself rather than a specific channel or member, so MemberSlug and
// ChannelSlug are intentionally empty.
func (s *ShareTransport) Binding() transport.Binding {
	return transport.Binding{Scope: transport.ScopeOffice}
}

// Run stores the host atomically, installs the broker's human-admit hook so
// the in-process accept handler fans out to Host.UpsertParticipant, and then
// blocks until ctx is cancelled. The human-share surface is in-process — the
// existing handlers in broker_human_share.go drive accept directly — so Run
// does not subscribe to anything external. A nil host is rejected so a
// misconfigured launcher fails loudly rather than silently degrading.
//
// The admit hook is cleared on Run exit so a stale closure (capturing the now-
// stopped host) cannot keep firing if a second adapter installs itself later.
func (s *ShareTransport) Run(ctx context.Context, host transport.Host) error {
	if s == nil {
		return fmt.Errorf("share: nil transport")
	}
	if host == nil {
		return fmt.Errorf("share: Run requires a non-nil host")
	}
	s.host.Store(&host)
	s.startedAt.Store(time.Now().UnixNano())
	if s.broker != nil {
		s.broker.SetHumanAdmitHook(s.onHumanAdmit)
		defer s.broker.SetHumanAdmitHook(nil)
	}
	<-ctx.Done()
	return nil
}

// onHumanAdmit forwards a successful invite acceptance to Host.UpsertParticipant
// so the admitted human becomes a first-class transport.Host participant. The
// host pointer is read via the same atomic the rest of the adapter uses; if
// Run has not yet stored the pointer (or the adapter is mid-shutdown) the call
// is a silent no-op — the broker-level humanSession already exists either way.
//
// Errors from the host are logged rather than returned: the broker has already
// persisted the session and replied 200 to the HTTP caller by the time this
// fires, so an upsert failure cannot be surfaced to the human anymore. Logging
// keeps it visible without inventing a synthetic admit-failure path.
func (s *ShareTransport) onHumanAdmit(ctx context.Context, sessionID, slug, displayName string) {
	if s == nil {
		return
	}
	hp := s.host.Load()
	if hp == nil {
		return
	}
	host := *hp
	participant := transport.Participant{
		AdapterName: shareAdapterName,
		Key:         sessionID,
		DisplayName: displayName,
		Human:       true,
	}
	binding := transport.Binding{Scope: transport.ScopeOffice, MemberSlug: slug}
	if err := host.UpsertParticipant(ctx, participant, binding); err != nil && (ctx == nil || ctx.Err() == nil) {
		log.Printf("[transport] share: UpsertParticipant for %s: %v", sessionID, err)
	}
}

// Send is a no-op for the human-share adapter — and that is the correct
// architecture, not a TODO. Admitted humans already receive office-wide
// messages in real-time through the broker's existing SSE fan-out: the
// broker's handleEvents (broker_sse.go) accepts the human session cookie,
// publishMessage (broker_publish.go) fans every channelMessage to all
// subscribers without filtering humans out, the share HTTP server proxies
// /api/* including text/event-stream through to the broker
// (cmd/wuphf/share.go), and the React app subscribes via useBrokerEvents
// (web/src/hooks/useBrokerEvents.ts). Doing real delivery here would
// duplicate that path.
//
// Returning nil keeps the OfficeBoundTransport contract honest: the adapter
// accepts the outbound message and trusts the broker SSE channel to deliver.
// If a future deployment ever needs out-of-band push (e.g. native mobile
// admitted humans without the React EventSource), that work belongs in the
// broker's SSE layer or a sibling adapter — not here.
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
// and returns the join URL produced by the active JoinURLBuilder. The override
// builder (set via SetURLBuilder) wins over the constructor builder so the
// in-process share controller can upgrade from RelativeJoinURL to an absolute
// URL once its bind address is known. The network argument is part of the
// OfficeBoundTransport contract but ShareTransport ignores it: URL
// construction is controlled by the builder, which the share controller
// selects based on its own bind logic.
func (s *ShareTransport) CreateInvite(_ context.Context, _ string) (string, error) {
	if s == nil || s.broker == nil {
		return "", fmt.Errorf("share: CreateInvite: nil broker")
	}
	token, _, err := s.broker.createHumanInvite()
	if err != nil {
		return "", fmt.Errorf("share: CreateInvite: %w", err)
	}
	builder := s.urlBuilder
	if override := s.urlBuilderOverride.Load(); override != nil {
		builder = *override
	}
	return builder(token), nil
}

// ShareInviteDetails carries the join URL and broker-issued metadata for a
// freshly created invite. Callers that need more than the URL (e.g. the
// in-process share controller surfacing the expiry timestamp) should prefer
// CreateInviteDetailed over CreateInvite. ExpiresAt is the same RFC3339 string
// the broker stores so callers cannot drift in formatting.
type ShareInviteDetails struct {
	URL       string
	InviteID  string
	ExpiresAt string
}

// CreateInviteDetailed creates an invite and returns the join URL plus
// broker-issued metadata (invite ID and RFC3339 expiry). Identical to
// CreateInvite for URL construction; see CreateInvite for the override
// precedence rule.
//
// CONCURRENCY NOTE: when more than one in-process controller can mint
// invites against the same ShareTransport instance (e.g. the network-share
// controller and the public-tunnel controller running side-by-side),
// callers MUST use CreateInviteDetailedWithBuilder instead. This method
// reads the override builder via SetURLBuilder, which is a separate atomic
// operation from invite creation and can be raced — a tunnel that calls
// SetURLBuilder(tunnelJoinURL) immediately followed by CreateInviteDetailed
// can have its builder overwritten by a parallel share path's
// SetURLBuilder(shareJoinURL), producing an invite URL with the wrong
// origin.
func (s *ShareTransport) CreateInviteDetailed(_ context.Context) (ShareInviteDetails, error) {
	if s == nil || s.broker == nil {
		return ShareInviteDetails{}, fmt.Errorf("share: CreateInviteDetailed: nil broker")
	}
	token, invite, err := s.broker.createHumanInvite()
	if err != nil {
		return ShareInviteDetails{}, fmt.Errorf("share: CreateInviteDetailed: %w", err)
	}
	builder := s.urlBuilder
	if override := s.urlBuilderOverride.Load(); override != nil {
		builder = *override
	}
	return ShareInviteDetails{
		URL:       builder(token),
		InviteID:  invite.ID,
		ExpiresAt: invite.ExpiresAt,
	}, nil
}

// CreateInviteDetailedWithBuilder is the race-free variant: the URL builder
// is bound atomically to this single invite-creation, never touching the
// shared urlBuilderOverride field. Concurrent callers each see their own
// builder applied to their own token. Use this from any code path that may
// run alongside another controller that also mints invites against the
// same ShareTransport instance.
//
// A nil builder is rejected so a misuse fails loudly rather than silently
// substituting an empty string for the URL.
func (s *ShareTransport) CreateInviteDetailedWithBuilder(_ context.Context, builder JoinURLBuilder) (ShareInviteDetails, error) {
	if s == nil || s.broker == nil {
		return ShareInviteDetails{}, fmt.Errorf("share: CreateInviteDetailedWithBuilder: nil broker")
	}
	if builder == nil {
		return ShareInviteDetails{}, fmt.Errorf("share: CreateInviteDetailedWithBuilder: nil builder")
	}
	token, invite, err := s.broker.createHumanInvite()
	if err != nil {
		return ShareInviteDetails{}, fmt.Errorf("share: CreateInviteDetailedWithBuilder: %w", err)
	}
	return ShareInviteDetails{
		URL:       builder(token),
		InviteID:  invite.ID,
		ExpiresAt: invite.ExpiresAt,
	}, nil
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
