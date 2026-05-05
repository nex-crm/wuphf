package team

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// shareTestRunDeadline bounds the polling rendezvous and Run-return waits in
// share-transport tests. Each test should complete in milliseconds; a 2s cap
// keeps a real regression visible as a fast Fatal rather than a CI timeout.
const shareTestRunDeadline = 2 * time.Second

// waitForShareConnected polls st.Health() until it reports HealthConnected,
// failing the test with a deterministic message if the deadline elapses or if
// Run exited early. Used to rendezvous with the goroutine running st.Run
// before exercising RevokeInvite — the host pointer is published in the same
// atomic store that flips Health to Connected, so this is the right gate.
func waitForShareConnected(t *testing.T, st *ShareTransport, runErr <-chan error) {
	t.Helper()
	deadline := time.Now().Add(shareTestRunDeadline)
	for st.Health().State != transport.HealthConnected {
		select {
		case err := <-runErr:
			t.Fatalf("Run returned before HealthConnected: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for HealthConnected after %s", shareTestRunDeadline)
		}
		runtime.Gosched()
	}
}

// awaitRunReturn drains runErr after a test cancels its context, with a
// bounded timeout so a hung Run surfaces as a Fatal rather than the Go test
// framework's default 10-minute timeout.
func awaitRunReturn(t *testing.T, runErr <-chan error) {
	t.Helper()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run returned: %v", err)
		}
	case <-time.After(shareTestRunDeadline):
		t.Fatalf("Run did not return within %s after cancel", shareTestRunDeadline)
	}
}

// TestShareTransportRunRequiresHost confirms Run rejects a nil host so a
// misconfigured launcher fails loudly instead of silently degrading.
func TestShareTransportRunRequiresHost(t *testing.T) {
	st := NewShareTransport(newTestBroker(t), RelativeJoinURL)
	err := st.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("Run(nil host) returned nil error; want guard error")
	}
}

// TestNewShareTransportRequiresBuilder confirms a nil JoinURLBuilder panics on
// construction. The launcher must pass an explicit builder (RelativeJoinURL
// for the degenerate case) so future contract consumers cannot get a silent
// relative-path URL where they expected an absolute one.
func TestNewShareTransportRequiresBuilder(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("NewShareTransport(_, nil) did not panic")
		}
	}()
	_ = NewShareTransport(newTestBroker(t), nil)
}

// TestShareTransportNameAndBinding pins the stable adapter identity and scope
// declaration. Changing either is a contract break — admitted-human identity
// is keyed off shareAdapterName across restarts. The name must also be a
// valid Go identifier per Transport.Name() (no hyphens, no spaces).
func TestShareTransportNameAndBinding(t *testing.T) {
	st := NewShareTransport(newTestBroker(t), RelativeJoinURL)
	if got, want := st.Name(), "share"; got != want {
		t.Errorf("Name(): got %q want %q", got, want)
	}
	if strings.ContainsAny(st.Name(), "- ") {
		t.Errorf("Name() %q must be a valid Go identifier (no hyphens, no spaces)", st.Name())
	}
	binding := st.Binding()
	if binding.Scope != transport.ScopeOffice {
		t.Errorf("Binding().Scope: got %q want %q", binding.Scope, transport.ScopeOffice)
	}
	if binding.MemberSlug != "" || binding.ChannelSlug != "" {
		t.Errorf("Binding(): expected empty MemberSlug/ChannelSlug for office scope, got %+v", binding)
	}
}

// TestShareTransportCreateInviteRelativeBuilder verifies the degenerate
// RelativeJoinURL builder produces /join/<token>. This is the path the
// launcher takes today; an absolute-URL builder will arrive when the share
// controller adopts the adapter.
func TestShareTransportCreateInviteRelativeBuilder(t *testing.T) {
	b := newTestBroker(t)
	st := NewShareTransport(b, RelativeJoinURL)
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
	st := NewShareTransport(b, RelativeJoinURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := &recordingHost{broker: b}

	runErr := make(chan error, 1)
	go func() { runErr <- st.Run(ctx, host) }()

	// Wait for Run to store the host before calling RevokeInvite; without
	// this, RevokeInvite may see a nil host pointer and silently skip the
	// per-session fan-out, making the test a false positive.
	waitForShareConnected(t, st, runErr)

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
	if call.adapter != shareAdapterName {
		t.Errorf("RevokeParticipant adapter: got %q want %q", call.adapter, shareAdapterName)
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
	awaitRunReturn(t, runErr)
}

// TestShareTransportRevokeInviteUnknown asserts RevokeInvite surfaces the
// broker error for an unknown invite ID rather than silently succeeding.
func TestShareTransportRevokeInviteUnknown(t *testing.T) {
	b := newTestBroker(t)
	st := NewShareTransport(b, RelativeJoinURL)
	err := st.RevokeInvite(context.Background(), "invite-does-not-exist")
	if err == nil {
		t.Fatal("RevokeInvite(unknown) returned nil; want error")
	}
}

// TestShareTransportRevokeInviteWithoutRun pins the silent-success branch in
// RevokeInvite: when Run has never been called, host.Load() returns nil and
// the broker-level revoke must still happen. Guards against a refactor that
// reorders the broker call after the nil-host check (which would skip the
// invite revoke entirely when Run is not yet up).
func TestShareTransportRevokeInviteWithoutRun(t *testing.T) {
	b := newTestBroker(t)
	st := NewShareTransport(b, RelativeJoinURL)

	if _, _, err := b.createHumanInvite(); err != nil {
		t.Fatalf("createHumanInvite: %v", err)
	}
	inviteID := lastHumanInviteID(b)

	if err := st.RevokeInvite(context.Background(), inviteID); err != nil {
		t.Fatalf("RevokeInvite without Run: %v", err)
	}
	if !inviteRevoked(b, inviteID) {
		t.Errorf("invite %q not marked revoked despite RevokeInvite returning nil", inviteID)
	}
}

// TestShareTransportRevokeInviteIdempotent calls RevokeInvite twice on the
// same invite. The second call must succeed and not re-fan-out to the host —
// the affected-sessions list returned by Broker.RevokeHumanInvite is empty on
// a second call (the only session was already revoked), so host.
// RevokeParticipant must not be invoked again. Guards the retry contract that
// RevokeInvite's docstring describes.
func TestShareTransportRevokeInviteIdempotent(t *testing.T) {
	b := newTestBroker(t)
	st := NewShareTransport(b, RelativeJoinURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := &recordingHost{broker: b}

	runErr := make(chan error, 1)
	go func() { runErr <- st.Run(ctx, host) }()

	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("createHumanInvite: %v", err)
	}
	inviteID := lastHumanInviteID(b)
	if _, _, err := b.acceptHumanInvite(token, "Mira", ""); err != nil {
		t.Fatalf("acceptHumanInvite: %v", err)
	}

	if err := st.RevokeInvite(ctx, inviteID); err != nil {
		t.Fatalf("RevokeInvite #1: %v", err)
	}
	firstCallCount := len(host.revokeCalls())

	if err := st.RevokeInvite(ctx, inviteID); err != nil {
		t.Fatalf("RevokeInvite #2: %v", err)
	}
	if got := len(host.revokeCalls()); got != firstCallCount {
		t.Errorf("RevokeInvite #2 fanned out again: call count went %d -> %d", firstCallCount, got)
	}

	cancel()
	awaitRunReturn(t, runErr)
}

// TestShareTransportRevokeInviteAccumulatesErrors verifies that when more than
// one session is admitted under a single invite (impossible via the current
// acceptHumanInvite single-accept rule, but exercised here by appending a
// second session directly to broker state) a host error on the first session
// does not stop the second from being revoked, and both errors are returned
// via errors.Join. Without this loop guard a partial fan-out would leave
// later sessions live under a revoked invite.
func TestShareTransportRevokeInviteAccumulatesErrors(t *testing.T) {
	b := newTestBroker(t)
	st := NewShareTransport(b, RelativeJoinURL)

	token, _, err := b.createHumanInvite()
	if err != nil {
		t.Fatalf("createHumanInvite: %v", err)
	}
	inviteID := lastHumanInviteID(b)
	if _, _, err := b.acceptHumanInvite(token, "Mira", ""); err != nil {
		t.Fatalf("acceptHumanInvite: %v", err)
	}
	// Inject a second session under the same invite so we can exercise a
	// multi-session fan-out. Bypasses acceptHumanInvite's single-accept rule
	// because the production code today never produces this state — we test
	// the loop semantics, not the broker policy.
	b.mu.Lock()
	b.humanSessions = append(b.humanSessions, humanSession{
		ID:        "session-injected",
		InviteID:  inviteID,
		HumanSlug: "extra",
	})
	if b.humanSessionRevoke == nil {
		b.humanSessionRevoke = make(map[string]chan struct{})
	}
	b.humanSessionRevoke["session-injected"] = make(chan struct{})
	b.mu.Unlock()

	failFirst := &erroringHost{
		broker:   b,
		failOnce: errors.New("synthetic revoke failure"),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- st.Run(ctx, failFirst) }()

	err = st.RevokeInvite(ctx, inviteID)
	if err == nil {
		t.Fatal("RevokeInvite returned nil; want joined error")
	}
	if !strings.Contains(err.Error(), "synthetic revoke failure") {
		t.Errorf("RevokeInvite error %v should include first-call failure", err)
	}
	if got := len(failFirst.revokeCalls()); got != 2 {
		t.Errorf("RevokeParticipant call count: got %d want 2 (loop must continue past first error)", got)
	}

	cancel()
	awaitRunReturn(t, runErr)
}

// TestBrokerTransportHostRevokeParticipantShare confirms the host stub added
// in this slice routes share-adapter keys to revokeHumanSession and rejects
// unknown adapter names.
func TestBrokerTransportHostRevokeParticipantShare(t *testing.T) {
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

	if err := host.RevokeParticipant(context.Background(), shareAdapterName, session.ID); err != nil {
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
	st := NewShareTransport(newTestBroker(t), RelativeJoinURL)
	if err := st.Send(context.Background(), transport.Outbound{Text: "hello"}); err != nil {
		t.Errorf("Send returned %v; want nil for human-share", err)
	}
}

// TestShareTransportHealthBeforeRun pins the pre-Run health state.
func TestShareTransportHealthBeforeRun(t *testing.T) {
	st := NewShareTransport(newTestBroker(t), RelativeJoinURL)
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
	st := NewShareTransport(newTestBroker(t), RelativeJoinURL)
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

// erroringHost returns failOnce as the error from the first RevokeParticipant
// call, then delegates subsequent calls to the broker. Used to verify the
// fan-out loop continues past errors and accumulates them via errors.Join.
type erroringHost struct {
	broker   *Broker
	failOnce error
	mu       sync.Mutex
	revoked  []revokeCall
}

func (h *erroringHost) ReceiveMessage(_ context.Context, _ transport.Message) error { return nil }
func (h *erroringHost) UpsertParticipant(_ context.Context, _ transport.Participant, _ transport.Binding) error {
	return nil
}
func (h *erroringHost) DetachParticipant(_ context.Context, _, _ string) error { return nil }

func (h *erroringHost) RevokeParticipant(ctx context.Context, adapterName, key string) error {
	h.mu.Lock()
	h.revoked = append(h.revoked, revokeCall{adapter: adapterName, key: key})
	first := h.failOnce
	h.failOnce = nil
	h.mu.Unlock()
	if first != nil {
		return first
	}
	if h.broker == nil {
		return nil
	}
	return (&brokerTransportHost{broker: h.broker}).RevokeParticipant(ctx, adapterName, key)
}

func (h *erroringHost) revokeCalls() []revokeCall {
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
