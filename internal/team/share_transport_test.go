package team

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// TestShareTransportRunRequiresHost confirms Run rejects a nil host so a
// misconfigured launcher fails loudly instead of silently degrading.
func TestShareTransportRunRequiresHost(t *testing.T) {
	st := NewShareTransport(newTestBroker(t), nil)
	err := st.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("Run(nil host) returned nil error; want guard error")
	}
}

// TestShareTransportNameAndBinding pins the stable adapter identity and scope
// declaration. Changing either is a contract break — admitted-human identity
// is keyed off shareAdapterName across restarts.
func TestShareTransportNameAndBinding(t *testing.T) {
	st := NewShareTransport(newTestBroker(t), nil)
	if got, want := st.Name(), "human-share"; got != want {
		t.Errorf("Name(): got %q want %q", got, want)
	}
	binding := st.Binding()
	if binding.Scope != transport.ScopeOffice {
		t.Errorf("Binding().Scope: got %q want %q", binding.Scope, transport.ScopeOffice)
	}
	if binding.MemberSlug != "" || binding.ChannelSlug != "" {
		t.Errorf("Binding(): expected empty MemberSlug/ChannelSlug for office scope, got %+v", binding)
	}
}

// TestShareTransportCreateInviteFallback verifies CreateInvite returns a
// relative /join/<token> path when no urlBuilder is injected. This is the
// path the launcher takes today; production wiring of an absolute URL will
// arrive when the share controller adopts the adapter.
func TestShareTransportCreateInviteFallback(t *testing.T) {
	b := newTestBroker(t)
	st := NewShareTransport(b, nil)
	got, err := st.CreateInvite(context.Background(), "tailscale")
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if !strings.HasPrefix(got, "/join/") {
		t.Errorf("CreateInvite: got %q, want /join/<token>", got)
	}
	if got == "/join/" {
		t.Error("CreateInvite: empty token in returned URL")
	}
}

// TestShareTransportCreateInviteUsesBuilder confirms the injected URL builder
// is invoked with the same token the broker minted.
func TestShareTransportCreateInviteUsesBuilder(t *testing.T) {
	b := newTestBroker(t)
	var seenToken string
	builder := func(token string) string {
		seenToken = token
		return "https://office.example/join/" + token
	}
	st := NewShareTransport(b, builder)
	got, err := st.CreateInvite(context.Background(), "")
	if err != nil {
		t.Fatalf("CreateInvite: %v", err)
	}
	if !strings.HasPrefix(got, "https://office.example/join/") {
		t.Errorf("CreateInvite: got %q, want https://office.example/join/<token>", got)
	}
	if seenToken == "" {
		t.Error("urlBuilder was not invoked")
	}
	if !strings.HasSuffix(got, seenToken) {
		t.Errorf("returned URL %q does not include builder-seen token %q", got, seenToken)
	}
}

// TestShareTransportRevokeInviteFansOutToHost is the central correctness test:
// after CreateInvite + accept (simulating a human joining), RevokeInvite must
// (a) mark the invite revoked in the broker, (b) call host.RevokeParticipant
// for every active session under that invite, and (c) leave the affected
// sessions revoked in the broker (because Host.RevokeParticipant routes to
// revokeHumanSession). Guards against silently dropping the per-session
// teardown that the OfficeBoundTransport contract requires.
func TestShareTransportRevokeInviteFansOutToHost(t *testing.T) {
	b := newTestBroker(t)
	st := NewShareTransport(b, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := &recordingHost{broker: b}

	runErr := make(chan error, 1)
	go func() { runErr <- st.Run(ctx, host) }()

	// Wait for Run to store the host before calling RevokeInvite; without
	// this, RevokeInvite may see a nil host pointer and silently skip the
	// per-session fan-out, making the test a false positive.
	for st.Health().State != transport.HealthConnected {
		runtime.Gosched()
	}

	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("createHumanInvite: %v", err)
	}
	inviteID := lastHumanInviteID(b)

	_, session, err := b.acceptHumanInvite(token, "Mira", "laptop")
	if err != nil {
		t.Fatalf("acceptHumanInvite: %v", err)
	}

	if err := st.RevokeInvite(ctx, inviteID); err != nil {
		t.Fatalf("RevokeInvite: %v", err)
	}

	if got, want := len(host.revokeCalls()), 1; got != want {
		t.Fatalf("host.RevokeParticipant call count: got %d want %d", got, want)
	}
	call := host.revokeCalls()[0]
	if call.adapter != "human-share" {
		t.Errorf("RevokeParticipant adapter: got %q want %q", call.adapter, "human-share")
	}
	if call.key != session.ID {
		t.Errorf("RevokeParticipant key: got %q want %q", call.key, session.ID)
	}

	// Session must now be revoked at the broker level (the recordingHost
	// passes RevokeParticipant through to revokeHumanSession).
	if b.humanSessionIDActive(session.ID) {
		t.Errorf("session %q still active after RevokeInvite", session.ID)
	}

	// Invite itself is marked revoked.
	if !inviteRevoked(b, inviteID) {
		t.Errorf("invite %q not marked revoked", inviteID)
	}

	cancel()
	if err := <-runErr; err != nil {
		t.Fatalf("Run returned: %v", err)
	}
}

// TestShareTransportRevokeInviteUnknown asserts RevokeInvite surfaces the
// broker error for an unknown invite ID rather than silently succeeding.
func TestShareTransportRevokeInviteUnknown(t *testing.T) {
	b := newTestBroker(t)
	st := NewShareTransport(b, nil)
	err := st.RevokeInvite(context.Background(), "invite-does-not-exist")
	if err == nil {
		t.Fatal("RevokeInvite(unknown) returned nil; want error")
	}
}

// TestBrokerTransportHostRevokeParticipantHumanShare confirms the host stub
// added in this slice routes human-share keys to revokeHumanSession and
// rejects unknown adapter names.
func TestBrokerTransportHostRevokeParticipantHumanShare(t *testing.T) {
	b := newTestBroker(t)
	host := &brokerTransportHost{broker: b}

	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("createHumanInvite: %v", err)
	}
	_, session, err := b.acceptHumanInvite(token, "Devon", "")
	if err != nil {
		t.Fatalf("acceptHumanInvite: %v", err)
	}

	if err := host.RevokeParticipant(context.Background(), "human-share", session.ID); err != nil {
		t.Fatalf("RevokeParticipant: %v", err)
	}
	if b.humanSessionIDActive(session.ID) {
		t.Errorf("session %q still active after RevokeParticipant", session.ID)
	}

	// Unknown adapter must not silently no-op.
	if err := host.RevokeParticipant(context.Background(), "made-up", "key"); err == nil {
		t.Error("RevokeParticipant(unknown adapter) returned nil; want unsupported-adapter error")
	}
}

// TestShareTransportSendIsNoop pins the no-op Send semantics. Office-bound
// human-share has no external network to push to (admitted humans poll the
// broker directly), so Send accepts the message and returns nil. Locking this
// in stops a future refactor from making Send error and breaking outbound
// office broadcasts.
func TestShareTransportSendIsNoop(t *testing.T) {
	st := NewShareTransport(newTestBroker(t), nil)
	if err := st.Send(context.Background(), transport.Outbound{Text: "hello"}); err != nil {
		t.Errorf("Send returned %v; want nil for human-share", err)
	}
}

// TestShareTransportHealthBeforeRun pins the pre-Run health state.
func TestShareTransportHealthBeforeRun(t *testing.T) {
	st := NewShareTransport(newTestBroker(t), nil)
	if got := st.Health().State; got != transport.HealthDisconnected {
		t.Errorf("Health() before Run: got %q want %q", got, transport.HealthDisconnected)
	}
}

// TestShareTransportHealthAfterRun runs Run with a pre-cancelled context so
// Run returns synchronously after setting startedAt. After Run returns Health
// must report Connected (the launcher renders Health on every channel header,
// so a regression that always reports Disconnected would silently degrade the
// UI).
func TestShareTransportHealthAfterRun(t *testing.T) {
	st := NewShareTransport(newTestBroker(t), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := st.Run(ctx, &recordingHost{}); err != nil {
		t.Fatalf("Run(cancelled): %v", err)
	}
	if got := st.Health().State; got != transport.HealthConnected {
		t.Errorf("Health() after Run: got %q want %q", got, transport.HealthConnected)
	}
}

// recordingHost is a transport.Host that records every RevokeParticipant call
// and (when broker is non-nil) delegates to brokerTransportHost so the broker
// actually revokes the session. UpsertParticipant and ReceiveMessage are
// no-ops; the share adapter never calls them in slice B.
type recordingHost struct {
	broker  *Broker
	mu      sync.Mutex
	revoked []revokeCall
}

type revokeCall struct {
	adapter string
	key     string
}

func (h *recordingHost) ReceiveMessage(_ context.Context, _ transport.Message) error {
	return nil
}

func (h *recordingHost) UpsertParticipant(_ context.Context, _ transport.Participant, _ transport.Binding) error {
	return nil
}

func (h *recordingHost) DetachParticipant(_ context.Context, _ string, _ string) error {
	return nil
}

func (h *recordingHost) RevokeParticipant(ctx context.Context, adapterName, key string) error {
	h.mu.Lock()
	h.revoked = append(h.revoked, revokeCall{adapter: adapterName, key: key})
	h.mu.Unlock()
	if h.broker == nil {
		return nil
	}
	return (&brokerTransportHost{broker: h.broker}).RevokeParticipant(ctx, adapterName, key)
}

func (h *recordingHost) revokeCalls() []revokeCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]revokeCall, len(h.revoked))
	copy(out, h.revoked)
	return out
}

// lastHumanInviteID returns the most recently created invite ID. Test-only.
func lastHumanInviteID(b *Broker) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.humanInvites) == 0 {
		return ""
	}
	return b.humanInvites[len(b.humanInvites)-1].ID
}

// inviteRevoked reports whether the invite with the given ID has RevokedAt set.
func inviteRevoked(b *Broker, inviteID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, inv := range b.humanInvites {
		if inv.ID == inviteID {
			return inv.RevokedAt != ""
		}
	}
	return false
}
