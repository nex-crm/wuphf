package team

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/openclaw"
	"github.com/nex-crm/wuphf/internal/team/transport"
)

// fakeHost captures every ReceiveMessage call so tests can assert the bridge
// routes inbound assistant events through transport.Host with the expected
// Participant + Binding fields. Each ReceiveMessage call also signals on
// `received` so callers can wait deterministically without polling.
type fakeHost struct {
	mu       sync.Mutex
	messages []transport.Message
	err      error
	received chan struct{}
}

func newFakeHost() *fakeHost {
	return &fakeHost{received: make(chan struct{}, 8)}
}

func (h *fakeHost) ReceiveMessage(_ context.Context, msg transport.Message) error {
	h.mu.Lock()
	h.messages = append(h.messages, msg)
	err := h.err
	h.mu.Unlock()
	select {
	case h.received <- struct{}{}:
	default:
	}
	return err
}

func (h *fakeHost) UpsertParticipant(context.Context, transport.Participant, transport.Binding) error {
	return nil
}

func (h *fakeHost) DetachParticipant(context.Context, string, string) error { return nil }

func (h *fakeHost) RevokeParticipant(context.Context, string, string) error { return nil }

func (h *fakeHost) snapshot() []transport.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]transport.Message, len(h.messages))
	copy(out, h.messages)
	return out
}

// waitForFakeOCSubscribe blocks until the fake openclaw client has recorded at
// least one Subscribe call or the deadline fires. The fakeOCClient already
// exposes a subscribeHook; install one that closes a channel so callers can
// rendezvous deterministically instead of polling fake.subscribed in a loop.
func waitForFakeOCSubscribe(t *testing.T, fake *fakeOCClient, timeout time.Duration) {
	t.Helper()
	subscribed := make(chan struct{}, 1)
	fake.mu.Lock()
	prior := fake.subscribeHook
	fake.subscribeHook = func() {
		if prior != nil {
			prior()
		}
		select {
		case subscribed <- struct{}{}:
		default:
		}
	}
	already := len(fake.subscribed) > 0
	fake.mu.Unlock()
	if already {
		return
	}
	select {
	case <-subscribed:
	case <-time.After(timeout):
		t.Fatalf("fake openclaw never received Subscribe within %s", timeout)
	}
}

// TestPostBridgeMessageRoutesToHost confirms that when a transport.Host is
// attached via Run, an assistant reply lands in host.ReceiveMessage with a
// fully populated transport.Message rather than going through the broker
// fallback. Guards against silent regressions in the host branch of
// postBridgeMessage.
func TestPostBridgeMessageRoutesToHost(t *testing.T) {
	fake := newFakeOC()
	broker := newTestBroker(t)
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k-host", Slug: "openclaw-host"}}
	bridge := NewOpenclawBridge(broker, fake, bindings)
	host := newFakeHost()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- bridge.Run(ctx, host) }()

	// Block until the supervised loop subscribes the seeded binding so the
	// assistant event is not racing the initial subscribe.
	waitForFakeOCSubscribe(t, fake, time.Second)

	beforeBroker := len(broker.AllMessages())

	seq := int64(2)
	fake.events <- openclaw.ClientEvent{
		Kind: openclaw.EventKindMessage,
		SessionMessage: &openclaw.SessionMessageEvent{
			SessionKey:  "k-host",
			MessageSeq:  &seq,
			Role:        "assistant",
			MessageText: "host-routed reply",
		},
	}

	// Wait for ReceiveMessage; bounded so a regression that silently routes
	// through the broker fails fast rather than hitting the test timeout.
	select {
	case <-host.received:
	case <-time.After(time.Second):
		t.Fatalf("host.ReceiveMessage was not called within 1s; broker had %d msgs", len(broker.AllMessages())-beforeBroker)
	}

	got := host.snapshot()
	if len(got) != 1 {
		t.Fatalf("host.ReceiveMessage call count: got %d want 1; broker had %d msgs", len(got), len(broker.AllMessages())-beforeBroker)
	}
	msg := got[0]
	if msg.Text != "host-routed reply" {
		t.Errorf("Text: got %q want %q", msg.Text, "host-routed reply")
	}
	if msg.Participant.AdapterName != openclawAdapterName {
		t.Errorf("Participant.AdapterName: got %q want %q", msg.Participant.AdapterName, openclawAdapterName)
	}
	if msg.Participant.Key != "k-host" {
		t.Errorf("Participant.Key: got %q want %q", msg.Participant.Key, "k-host")
	}
	if msg.Participant.DisplayName != "openclaw-host" {
		t.Errorf("Participant.DisplayName: got %q want %q", msg.Participant.DisplayName, "openclaw-host")
	}
	if msg.Binding.Scope != transport.ScopeMember {
		t.Errorf("Binding.Scope: got %q want %q", msg.Binding.Scope, transport.ScopeMember)
	}
	if msg.Binding.MemberSlug != "openclaw-host" {
		t.Errorf("Binding.MemberSlug: got %q want %q", msg.Binding.MemberSlug, "openclaw-host")
	}
	if msg.Binding.ChannelSlug != "general" {
		t.Errorf("Binding.ChannelSlug: got %q want %q", msg.Binding.ChannelSlug, "general")
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

// TestOpenclawBridgeRunRequiresHost confirms Run rejects a nil host so a
// misconfigured launcher fails loudly instead of silently degrading to the
// legacy broker entrypoint.
func TestOpenclawBridgeRunRequiresHost(t *testing.T) {
	bridge := NewOpenclawBridge(newTestBroker(t), newFakeOC(), nil)
	err := bridge.Run(context.Background(), nil)
	if err == nil {
		t.Fatal("Run(nil host) returned nil error; want guard error")
	}
}

// TestRegisterTransportsShutdownOrdering wires the openclaw bridge through
// RegisterTransports + Run(ctx, host) and confirms the returned cleanup
// drains both the router and the Run goroutines without deadlocking.
func TestRegisterTransportsShutdownOrdering(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg := config.Config{
		OpenclawBridges: []config.OpenclawBridgeBinding{
			{SessionKey: "shutdown-k", Slug: "openclaw-shutdown", DisplayName: "Shutdown"},
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	fake := newFakeOC()
	openclawBootstrapDialer = func(context.Context) (openclawClient, error) { return fake, nil }
	t.Cleanup(func() { openclawBootstrapDialer = nil })

	broker := newTestBroker(t)
	stop, err := RegisterTransports(broker)
	if err != nil {
		t.Fatalf("RegisterTransports: %v", err)
	}

	// Block until the bridge has subscribed so we know Run actually started.
	waitForFakeOCSubscribe(t, fake, time.Second)

	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RegisterTransports cleanup did not return within 2s; router or Run goroutine deadlocked")
	}
}
