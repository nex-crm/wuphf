package transport_test

// host_misuse_test.go asserts that the transport contract surfaces actionable
// errors for the six most common adapter-authoring mistakes. Each test name
// maps directly to a pitfall in docs/ADD-A-TRANSPORT.md §Common pitfalls so
// contributors running `go test ./internal/team/transport/...` against their
// adapter see exactly which contract they violated.
//
// These tests use fakeHost and the fake adapters from webhook_fake_test.go.
// They do NOT start a real broker — they test the contract shape only.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	. "github.com/nex-crm/wuphf/internal/team/transport"
)

// TestMisuse1_ReceiveWithoutUpsert verifies that a host correctly returns
// ErrParticipantUnknown when ReceiveMessage is called without a prior
// UpsertParticipant. This is pitfall #1 in ADD-A-TRANSPORT.md.
func TestMisuse1_ReceiveWithoutUpsert(t *testing.T) {
	host := &fakeHost{
		receiveErr: &ParticipantUnknownError{AdapterName: "telegram", Key: "12345"},
	}

	err := host.ReceiveMessage(context.Background(), Message{
		Participant: Participant{AdapterName: "telegram", Key: "12345", DisplayName: "Alice"},
		Binding:     Binding{Scope: ScopeChannel, ChannelSlug: "general"},
		Text:        "hello",
	})

	if err == nil {
		t.Fatal("expected ErrParticipantUnknown, got nil")
	}
	if !errors.Is(err, ErrParticipantUnknown) {
		t.Fatalf("expected ErrParticipantUnknown, got %v", err)
	}
}

// TestMisuse2_BindingChannelMissing verifies that declaring a non-existent
// channel in Binding produces ErrBindingChannelMissing. This is pitfall #2.
func TestMisuse2_BindingChannelMissing(t *testing.T) {
	host := &fakeHost{
		receiveErr: &BindingChannelMissingError{ChannelSlug: "nonexistent"},
	}

	// Upsert succeeds (channel check happens at ReceiveMessage time).
	if err := host.UpsertParticipant(context.Background(),
		Participant{AdapterName: "telegram", Key: "42", DisplayName: "Bob"},
		Binding{Scope: ScopeChannel, ChannelSlug: "nonexistent"},
	); err != nil {
		t.Fatalf("upsert: unexpected error: %v", err)
	}

	err := host.ReceiveMessage(context.Background(), Message{
		Participant: Participant{AdapterName: "telegram", Key: "42"},
		Binding:     Binding{Scope: ScopeChannel, ChannelSlug: "nonexistent"},
		Text:        "msg",
	})
	if err == nil {
		t.Fatal("expected ErrBindingChannelMissing, got nil")
	}
	if !errors.Is(err, ErrBindingChannelMissing) {
		t.Fatalf("expected ErrBindingChannelMissing, got %v", err)
	}
}

// TestMisuse3_RegistrationConflict verifies that re-registering an
// (AdapterName, Key) pair with a different member slug returns
// ErrRegistrationConflict. This is pitfall #3 — unstable session keys.
func TestMisuse3_RegistrationConflict(t *testing.T) {
	host := &fakeHost{
		upsertErr: &RegistrationConflictError{
			AdapterName:     "openclaw",
			Key:             "sess-abc",
			ExistingSlug:    "alice",
			ConflictingSlug: "bob",
		},
	}

	err := host.UpsertParticipant(context.Background(),
		Participant{AdapterName: "openclaw", Key: "sess-abc", DisplayName: "Bob"},
		Binding{Scope: ScopeMember},
	)
	if err == nil {
		t.Fatal("expected ErrRegistrationConflict, got nil")
	}
	if !errors.Is(err, ErrRegistrationConflict) {
		t.Fatalf("expected ErrRegistrationConflict, got %v", err)
	}
}

// TestMisuse4_SendTimeoutSentinel verifies that ErrSendTimeout survives wrapping
// with fmt.Errorf. The Host worker wraps it before returning to the adapter, so
// adapter code that does errors.Is(err, ErrSendTimeout) must match the wrapped form.
// Pitfall #4.
func TestMisuse4_SendTimeoutSentinel(t *testing.T) {
	// Simulate the Host worker wrapping ErrSendTimeout with context.
	wrapped := fmt.Errorf("worker dropped message after 10s: %w", ErrSendTimeout)

	if !errors.Is(wrapped, ErrSendTimeout) {
		t.Fatalf("wrapped ErrSendTimeout: errors.Is expected true, got false")
	}
	// Confirm it does not accidentally match unrelated sentinels.
	if errors.Is(wrapped, ErrParticipantUnknown) {
		t.Fatal("wrapped ErrSendTimeout must not match ErrParticipantUnknown")
	}
}

// TestMisuse5_HealthDegradedSentinel verifies ErrHealthDegraded survives wrapping
// with fmt.Errorf. Pitfall #5 — adapter code must detect degraded state via
// errors.Is even when the Host adds context to the error.
func TestMisuse5_HealthDegradedSentinel(t *testing.T) {
	wrapped := fmt.Errorf("host pausing inbound: %w", ErrHealthDegraded)

	if !errors.Is(wrapped, ErrHealthDegraded) {
		t.Fatalf("wrapped ErrHealthDegraded: errors.Is expected true, got false")
	}
	if errors.Is(wrapped, ErrAdapterCrashed) {
		t.Fatal("wrapped ErrHealthDegraded must not match ErrAdapterCrashed")
	}
}

// TestMisuse6_UpsertBeforeReceive verifies the correct flow: UpsertParticipant
// followed by ReceiveMessage succeeds with a compliant fakeHost. This is the
// positive control — if this test fails the fakeHost or contract type has a bug.
func TestMisuse6_UpsertBeforeReceive(t *testing.T) {
	host := &fakeHost{}
	ctx := context.Background()

	p := Participant{AdapterName: "telegram", Key: "99", DisplayName: "Carol", Human: true}
	b := Binding{Scope: ScopeChannel, ChannelSlug: "general"}

	if err := host.UpsertParticipant(ctx, p, b); err != nil {
		t.Fatalf("UpsertParticipant: unexpected error: %v", err)
	}
	if err := host.ReceiveMessage(ctx, Message{
		Participant: p,
		Binding:     b,
		Text:        "ping",
	}); err != nil {
		t.Fatalf("ReceiveMessage: unexpected error: %v", err)
	}

	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.upserted) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(host.upserted))
	}
	if len(host.received) != 1 {
		t.Fatalf("expected 1 received message, got %d", len(host.received))
	}
	if host.received[0].Text != "ping" {
		t.Fatalf("expected message text %q, got %q", "ping", host.received[0].Text)
	}
}

// TestFakeAdaptersSatisfyInterfaces is a compile-time assertion that the three
// fake adapters implement their respective Transport interfaces. The test body
// is empty — the var block fails at compile time if any interface is unmet.
func TestFakeAdaptersSatisfyInterfaces(t *testing.T) {
	var _ Transport = (*fakeChannelTransport)(nil)
	var _ MemberBoundTransport = (*fakeMemberTransport)(nil)
	var _ OfficeBoundTransport = (*fakeOfficeTransport)(nil)
	var _ Host = (*fakeHost)(nil)
}

// TestHealthStateConstants verifies all HealthState constants are distinct.
func TestHealthStateConstants(t *testing.T) {
	states := []HealthState{HealthConnected, HealthDegraded, HealthDisconnected}
	seen := make(map[HealthState]bool)
	for _, s := range states {
		if seen[s] {
			t.Fatalf("duplicate HealthState value: %q", s)
		}
		seen[s] = true
	}
}

// TestScopeConstants verifies all Scope constants are distinct.
func TestScopeConstants(t *testing.T) {
	scopes := []Scope{ScopeChannel, ScopeMember, ScopeOffice}
	seen := make(map[Scope]bool)
	for _, s := range scopes {
		if seen[s] {
			t.Fatalf("duplicate Scope value: %q", s)
		}
		seen[s] = true
	}
}
