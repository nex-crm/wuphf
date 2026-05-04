package transport_test

// webhook_fake_test.go provides minimal fake adapters for each Transport scope
// used as canonical examples in host_misuse_test.go. Reading this file alongside
// docs/ADD-A-TRANSPORT.md is the recommended starting point for contributors
// implementing a new integration.
//
// Each fake covers the minimum contract surface:
//   - fakeChannelTransport — channel-bound (ScopeChannel), mirrors Telegram.
//   - fakeMemberTransport  — member-bound (ScopeMember), mirrors OpenClaw.
//   - fakeOfficeTransport  — office-bound (ScopeOffice), mirrors human-share.
//
// All fakes are intentionally minimal and do NOT simulate reconnect, backoff,
// or deduplication — those are adapter-specific concerns, not contract concerns.

import (
	"context"
	"sync"
	"time"

	. "github.com/nex-crm/wuphf/internal/team/transport"
)

// ---- channel-bound fake ------------------------------------------------

type fakeChannelTransport struct {
	name        string
	channelSlug string

	mu          sync.Mutex
	sent        []Outbound
	healthState HealthState
}

func newFakeChannelTransport(name, channelSlug string) *fakeChannelTransport {
	return &fakeChannelTransport{
		name:        name,
		channelSlug: channelSlug,
		healthState: HealthConnected,
	}
}

func (f *fakeChannelTransport) Name() string { return f.name }

func (f *fakeChannelTransport) Binding() Binding {
	return Binding{Scope: ScopeChannel, ChannelSlug: f.channelSlug}
}

// Run blocks until ctx is cancelled. In a real adapter this is where the
// long-poll or WebSocket loop lives. The fake accepts an optional inject
// channel so tests can drive inbound messages.
func (f *fakeChannelTransport) Run(ctx context.Context, host Host) error {
	// Fake: nothing to poll. Block until shutdown.
	<-ctx.Done()
	return nil
}

func (f *fakeChannelTransport) Send(_ context.Context, msg Outbound) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeChannelTransport) Health() Health {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Health{
		State:         f.healthState,
		LastSuccessAt: time.Now(),
	}
}

func (f *fakeChannelTransport) sentMessages() []Outbound {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Outbound, len(f.sent))
	copy(out, f.sent)
	return out
}

// ---- member-bound fake -------------------------------------------------

type fakeMemberTransport struct {
	name string

	mu          sync.Mutex
	sessions    map[string]string // sessionKey -> slug
	slugs       map[string]string // slug -> sessionKey
	sent        []Outbound
	healthState HealthState
}

func newFakeMemberTransport(name string) *fakeMemberTransport {
	return &fakeMemberTransport{
		name:        name,
		sessions:    make(map[string]string),
		slugs:       make(map[string]string),
		healthState: HealthConnected,
	}
}

func (f *fakeMemberTransport) Name() string { return f.name }

func (f *fakeMemberTransport) Binding() Binding {
	// Member-bound adapters have no channel anchor; slug is resolved per-session.
	return Binding{Scope: ScopeMember}
}

func (f *fakeMemberTransport) Run(ctx context.Context, host Host) error {
	<-ctx.Done()
	return nil
}

func (f *fakeMemberTransport) Send(_ context.Context, msg Outbound) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeMemberTransport) Health() Health {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Health{State: f.healthState, LastSuccessAt: time.Now()}
}

// CreateSession satisfies MemberBoundTransport. The fake returns a deterministic
// key so tests can assert routing without real upstream calls.
func (f *fakeMemberTransport) CreateSession(_ context.Context, agentID, label string) (string, error) {
	key := "fake-session-" + agentID + "-" + label
	return key, nil
}

func (f *fakeMemberTransport) AttachSlug(slug, sessionKey string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Remove stale reverse entry before overwriting, so a slug reassignment
	// does not leave the old sessionKey mapped to this slug indefinitely.
	if oldKey, ok := f.slugs[slug]; ok {
		delete(f.sessions, oldKey)
	}
	f.slugs[slug] = sessionKey
	f.sessions[sessionKey] = slug
}

func (f *fakeMemberTransport) DetachSlug(slug string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := f.slugs[slug]
	delete(f.slugs, slug)
	delete(f.sessions, key)
}

// ---- office-bound fake -------------------------------------------------

type fakeOfficeTransport struct {
	name string

	mu          sync.Mutex
	invites     map[string]string // inviteID -> URL
	sent        []Outbound
	healthState HealthState
}

func newFakeOfficeTransport(name string) *fakeOfficeTransport {
	return &fakeOfficeTransport{
		name:        name,
		invites:     make(map[string]string),
		healthState: HealthConnected,
	}
}

func (f *fakeOfficeTransport) Name() string { return f.name }

func (f *fakeOfficeTransport) Binding() Binding {
	return Binding{Scope: ScopeOffice}
}

func (f *fakeOfficeTransport) Run(ctx context.Context, host Host) error {
	<-ctx.Done()
	return nil
}

func (f *fakeOfficeTransport) Send(_ context.Context, msg Outbound) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeOfficeTransport) Health() Health {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Health{State: f.healthState, LastSuccessAt: time.Now()}
}

func (f *fakeOfficeTransport) CreateInvite(_ context.Context, network string) (string, error) {
	id := "invite-" + network + "-1"
	url := "https://office.example.com/join/" + id
	f.mu.Lock()
	f.invites[id] = url
	f.mu.Unlock()
	return url, nil
}

func (f *fakeOfficeTransport) RevokeInvite(_ context.Context, inviteID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.invites, inviteID)
	return nil
}

// ---- fakeHost ----------------------------------------------------------
// fakeHost records calls so misuse tests can assert which Host methods were
// called and in what order.

type fakeHost struct {
	mu         sync.Mutex
	upserted   []upsertCall
	received   []Message
	detached   []detachCall
	revoked    []detachCall
	receiveErr error // if set, returned from ReceiveMessage
	upsertErr  error // if set, returned from UpsertParticipant
}

type upsertCall struct {
	Participant Participant
	Binding     Binding
}

type detachCall struct {
	AdapterName string
	Key         string
}

func (h *fakeHost) ReceiveMessage(_ context.Context, msg Message) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.received = append(h.received, msg)
	if h.receiveErr != nil {
		return h.receiveErr
	}
	return nil
}

func (h *fakeHost) UpsertParticipant(_ context.Context, p Participant, b Binding) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.upserted = append(h.upserted, upsertCall{p, b})
	if h.upsertErr != nil {
		return h.upsertErr
	}
	return nil
}

func (h *fakeHost) DetachParticipant(_ context.Context, adapterName, key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.detached = append(h.detached, detachCall{adapterName, key})
	return nil
}

func (h *fakeHost) RevokeParticipant(_ context.Context, adapterName, key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.revoked = append(h.revoked, detachCall{adapterName, key})
	return nil
}
