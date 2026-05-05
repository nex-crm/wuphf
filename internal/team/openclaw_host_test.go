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
// Participant + Binding fields.
type fakeHost struct {
	mu       sync.Mutex
	received []transport.Message
	err      error
}

func (h *fakeHost) ReceiveMessage(_ context.Context, msg transport.Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.received = append(h.received, msg)
	return h.err
}

func (h *fakeHost) UpsertParticipant(context.Context, transport.Participant, transport.Binding) error {
	return nil
}

func (h *fakeHost) DetachParticipant(context.Context, string, string) error { return nil }

func (h *fakeHost) RevokeParticipant(context.Context, string, string) error { return nil }

func (h *fakeHost) snapshot() []transport.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]transport.Message, len(h.received))
	copy(out, h.received)
	return out
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
	host := &fakeHost{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runErr := make(chan error, 1)
	go func() { runErr <- bridge.Run(ctx, host) }()

	// Wait until the supervised loop subscribes the seeded binding so the
	// assistant event is not racing the initial subscribe.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		ready := len(fake.subscribed) > 0
		fake.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

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

	// Poll until ReceiveMessage is observed; bounded so a regression that
	// silently routes through the broker fails fast.
	hostDeadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(hostDeadline) {
		if len(host.snapshot()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
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

	// Wait until the bridge has subscribed so we know Run actually started.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		ready := len(fake.subscribed) > 0
		fake.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

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
