package team

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// TestHostUpsertParticipantMarksMemberOnline asserts that a member-bound
// UpsertParticipant call (the openclaw shape) flips presence on for the slug
// declared in the binding. The reverse-lookup map is populated as a side effect
// so a follow-up DetachParticipant carrying only (adapter, key) can resolve
// back to the slug.
func TestHostUpsertParticipantMarksMemberOnline(t *testing.T) {
	b := newTestBroker(t)
	host := &brokerTransportHost{broker: b}

	before := time.Now()
	err := host.UpsertParticipant(context.Background(),
		transport.Participant{AdapterName: openclawAdapterName, Key: "session-abc", DisplayName: "eng"},
		transport.Binding{Scope: transport.ScopeMember, MemberSlug: "eng", ChannelSlug: "general"},
	)
	if err != nil {
		t.Fatalf("UpsertParticipant: %v", err)
	}

	b.mu.Lock()
	rec, ok := b.presenceForSlugLocked("eng")
	keyLookup := b.presenceKeyToSlug[presenceLookupKey(openclawAdapterName, "session-abc")]
	b.mu.Unlock()
	if !ok {
		t.Fatalf("presence record missing for slug %q", "eng")
	}
	if !rec.Online {
		t.Errorf("Online: got false, want true")
	}
	if rec.LastSeenAt.Before(before) {
		t.Errorf("LastSeenAt %v is before test start %v", rec.LastSeenAt, before)
	}
	if rec.AdapterName != openclawAdapterName || rec.SessionKey != "session-abc" {
		t.Errorf("adapter/key: got (%q, %q) want (%q, %q)", rec.AdapterName, rec.SessionKey, openclawAdapterName, "session-abc")
	}
	if keyLookup != "eng" {
		t.Errorf("presenceKeyToSlug: got %q, want %q", keyLookup, "eng")
	}
}

// TestHostUpsertParticipantSkipsNonMemberScope asserts that office-bound (share)
// and channel-bound (telegram) participants do not produce a member-presence
// record. Telegram attribution lives in PostInboundSurfaceMessage by display
// name; admitted humans have their own LastSeenAt on humanSession. Touching
// memberPresence for either would lie about who is an office member.
//
// Each case constructs its own broker so a leak in case N cannot inflate
// memberPresence for case N+1 — failures bisect to the responsible case
// instead of cascading. The "member with empty slug" case uses
// openclawAdapterName (not shareAdapterName) because the intent is a
// member-scope binding with an empty slug; share is office-scope by contract.
func TestHostUpsertParticipantSkipsNonMemberScope(t *testing.T) {
	cases := []struct {
		name    string
		adapter string
		binding transport.Binding
	}{
		{"office (share)", shareAdapterName, transport.Binding{Scope: transport.ScopeOffice, MemberSlug: "team-member"}},
		{"channel (telegram)", "telegram", transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: "general"}},
		{"member with empty slug", openclawAdapterName, transport.Binding{Scope: transport.ScopeMember, MemberSlug: ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newTestBroker(t)
			host := &brokerTransportHost{broker: b}
			err := host.UpsertParticipant(context.Background(),
				transport.Participant{AdapterName: tc.adapter, Key: "session-x", DisplayName: "x"},
				tc.binding,
			)
			if err != nil {
				t.Fatalf("UpsertParticipant: %v", err)
			}
			b.mu.Lock()
			n := len(b.memberPresence)
			b.mu.Unlock()
			if n != 0 {
				t.Errorf("memberPresence size: got %d, want 0 (binding %+v should not produce a record)", n, tc.binding)
			}
		})
	}
}

// TestHostDetachParticipantClearsOnlinePreservesLastSeen asserts that detach
// flips Online off but leaves LastSeenAt intact so the API can render a
// "last seen 5m ago" indicator. The reverse-lookup entry is removed so a
// second Detach with the same (adapter, key) is a clean no-op.
func TestHostDetachParticipantClearsOnlinePreservesLastSeen(t *testing.T) {
	b := newTestBroker(t)
	host := &brokerTransportHost{broker: b}

	if err := host.UpsertParticipant(context.Background(),
		transport.Participant{AdapterName: openclawAdapterName, Key: "session-abc"},
		transport.Binding{Scope: transport.ScopeMember, MemberSlug: "eng"},
	); err != nil {
		t.Fatalf("UpsertParticipant: %v", err)
	}
	b.mu.Lock()
	upsertSeen := b.memberPresence["eng"].LastSeenAt
	b.mu.Unlock()

	if err := host.DetachParticipant(context.Background(), openclawAdapterName, "session-abc"); err != nil {
		t.Fatalf("DetachParticipant: %v", err)
	}

	b.mu.Lock()
	rec := b.memberPresence["eng"]
	_, lookupRemains := b.presenceKeyToSlug[presenceLookupKey(openclawAdapterName, "session-abc")]
	b.mu.Unlock()

	if rec.Online {
		t.Errorf("Online after detach: got true, want false")
	}
	if !rec.LastSeenAt.Equal(upsertSeen) {
		t.Errorf("LastSeenAt mutated by detach: was %v, now %v (must preserve)", upsertSeen, rec.LastSeenAt)
	}
	if lookupRemains {
		t.Errorf("presenceKeyToSlug entry not cleared after detach")
	}
}

// TestHostDetachParticipantUnknownKeyIsNoop asserts that a Detach without a
// prior Upsert (e.g. an adapter sending shutdown teardown for a session that
// never produced a message) returns nil cleanly. Surfacing this as an error
// would cascade into adapter shutdown noise that no operator can act on.
func TestHostDetachParticipantUnknownKeyIsNoop(t *testing.T) {
	b := newTestBroker(t)
	host := &brokerTransportHost{broker: b}
	if err := host.DetachParticipant(context.Background(), openclawAdapterName, "never-upserted"); err != nil {
		t.Fatalf("DetachParticipant for unknown key: %v (want nil)", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.memberPresence) != 0 {
		t.Errorf("memberPresence got rows from a no-op detach: %d", len(b.memberPresence))
	}
}

// TestHostUpsertParticipantUnknownAdapterErrorsAtMemberScope asserts that an
// adapter without a matching DetachParticipant allowlist entry is rejected at
// member scope. Without this symmetry an Upsert could set online=true with no
// valid detach path, leaving a permanent stale "online" indicator. The
// allowlists in Upsert and Detach must move together — this test fails first
// when a future adapter is added to one without the other.
func TestHostUpsertParticipantUnknownAdapterErrorsAtMemberScope(t *testing.T) {
	b := newTestBroker(t)
	host := &brokerTransportHost{broker: b}
	err := host.UpsertParticipant(context.Background(),
		transport.Participant{AdapterName: "future-bound", Key: "k"},
		transport.Binding{Scope: transport.ScopeMember, MemberSlug: "eng"},
	)
	if err == nil {
		t.Fatal("UpsertParticipant with unknown adapter at member scope: got nil, want error")
	}
	if !strings.Contains(err.Error(), "unsupported adapter") {
		t.Errorf("error message missing context: %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.memberPresence) != 0 {
		t.Errorf("memberPresence got rows from a rejected upsert: %d", len(b.memberPresence))
	}
}

// TestHostDetachParticipantUnknownAdapterErrors asserts that a misnamed
// adapter (typo, version skew) surfaces loudly instead of silently dropping
// the call. A silent no-op here would mask a regression where an adapter is
// renamed without updating the host's switch.
func TestHostDetachParticipantUnknownAdapterErrors(t *testing.T) {
	b := newTestBroker(t)
	host := &brokerTransportHost{broker: b}
	err := host.DetachParticipant(context.Background(), "unknown-adapter", "k")
	if err == nil {
		t.Fatal("DetachParticipant with unknown adapter: got nil, want error")
	}
	if !strings.Contains(err.Error(), "unsupported adapter") {
		t.Errorf("error message missing context: %v", err)
	}
}

// TestHostUpsertReplacingSessionKeyClearsOldReverseEntry asserts that when a
// slug's session key changes (reconnect with a new gateway session) the old
// reverse-map entry is cleared so a stale Detach against the old key cannot
// flip the slug offline once the new session is live. Without this guard, an
// in-flight detach from the prior session would race the new upsert.
func TestHostUpsertReplacingSessionKeyClearsOldReverseEntry(t *testing.T) {
	b := newTestBroker(t)
	host := &brokerTransportHost{broker: b}

	if err := host.UpsertParticipant(context.Background(),
		transport.Participant{AdapterName: openclawAdapterName, Key: "old-session"},
		transport.Binding{Scope: transport.ScopeMember, MemberSlug: "eng"},
	); err != nil {
		t.Fatalf("first UpsertParticipant: %v", err)
	}
	if err := host.UpsertParticipant(context.Background(),
		transport.Participant{AdapterName: openclawAdapterName, Key: "new-session"},
		transport.Binding{Scope: transport.ScopeMember, MemberSlug: "eng"},
	); err != nil {
		t.Fatalf("second UpsertParticipant: %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if _, stillThere := b.presenceKeyToSlug[presenceLookupKey(openclawAdapterName, "old-session")]; stillThere {
		t.Errorf("stale reverse-map entry for old-session not cleared after key swap")
	}
	if got := b.presenceKeyToSlug[presenceLookupKey(openclawAdapterName, "new-session")]; got != "eng" {
		t.Errorf("new-session reverse entry: got %q, want %q", got, "eng")
	}
	rec := b.memberPresence["eng"]
	if rec.SessionKey != "new-session" || !rec.Online {
		t.Errorf("presence record after key swap: %+v", rec)
	}
}

// TestOfficeMembersListIncludesPresence is the API-surface test: after an
// adapter UpsertParticipant flips presence on, /office-members must surface
// online=true and a non-empty last_seen_at for that slug.
func TestOfficeMembersListIncludesPresence(t *testing.T) {
	b := newTestBroker(t)

	// CEO is seeded by ensureDefaultOfficeMembersLocked, so we can mark it
	// online without going through the office-member create flow.
	host := &brokerTransportHost{broker: b}
	if err := host.UpsertParticipant(context.Background(),
		transport.Participant{AdapterName: openclawAdapterName, Key: "session-ceo"},
		transport.Binding{Scope: transport.ScopeMember, MemberSlug: "ceo"},
	); err != nil {
		t.Fatalf("UpsertParticipant: %v", err)
	}

	rec := httptest.NewRecorder()
	b.serveOfficeMemberList(rec)
	if rec.Code != 200 {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}

	var resp struct {
		Members []officeMemberListEntry `json:"members"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var ceo *officeMemberListEntry
	for i := range resp.Members {
		if resp.Members[i].Slug == "ceo" {
			ceo = &resp.Members[i]
			break
		}
	}
	if ceo == nil {
		t.Fatal("ceo not in /office-members response")
	}
	if !ceo.Online {
		t.Errorf("ceo.online: got false, want true after UpsertParticipant")
	}
	if ceo.LastSeenAt == "" {
		t.Errorf("ceo.last_seen_at: got empty, want RFC3339 timestamp")
	}
	if _, err := time.Parse(time.RFC3339, ceo.LastSeenAt); err != nil {
		t.Errorf("last_seen_at not RFC3339: %v (got %q)", err, ceo.LastSeenAt)
	}
}

// TestOfficeMembersListSerializesOnlineFalseExplicitly asserts that members
// without a presence record (and detached members) serialize with an explicit
// `online: false` rather than omitting the field. Clients must be able to
// distinguish "offline" from "field missing because the build is older" — the
// difference matters for any UI that wants to render an offline indicator
// rather than silently no-op when presence data is absent.
func TestOfficeMembersListSerializesOnlineFalseExplicitly(t *testing.T) {
	b := newTestBroker(t)

	rec := httptest.NewRecorder()
	b.serveOfficeMemberList(rec)
	if rec.Code != 200 {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}

	// Decode into a generic shape so we can assert on the JSON key's presence,
	// not just the Go zero value (which a typed decode would hide).
	var raw struct {
		Members []map[string]any `json:"members"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw.Members) == 0 {
		t.Fatal("no members in response")
	}
	for _, m := range raw.Members {
		online, ok := m["online"]
		if !ok {
			t.Errorf("member %q: missing `online` key — omitempty regression on offline members", m["slug"])
			continue
		}
		if online != false {
			t.Errorf("member %q: online=%v, want false (no Upsert was issued)", m["slug"], online)
		}
	}
}

// TestHostUpsertCanonicalizesNonCanonicalSlug asserts that a binding arriving
// with a mixed-case or trim-required MemberSlug keys the presence record
// under the same canonical slug the /office-members read path uses. Without
// this canonicalization, an "Eng" Upsert would create an orphan presence row
// the API never reads, and the member would render as offline despite an
// active session.
func TestHostUpsertCanonicalizesNonCanonicalSlug(t *testing.T) {
	b := newTestBroker(t)
	host := &brokerTransportHost{broker: b}

	if err := host.UpsertParticipant(context.Background(),
		transport.Participant{AdapterName: openclawAdapterName, Key: "session-1"},
		transport.Binding{Scope: transport.ScopeMember, MemberSlug: "  Eng  "},
	); err != nil {
		t.Fatalf("UpsertParticipant: %v", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.memberPresence["eng"]; !ok {
		gotKeys := make([]string, 0, len(b.memberPresence))
		for k := range b.memberPresence {
			gotKeys = append(gotKeys, k)
		}
		t.Errorf("memberPresence missing canonical key %q (got keys: %v)", "eng", gotKeys)
	}
	if _, ok := b.memberPresence["  Eng  "]; ok {
		t.Errorf("memberPresence contains uncanonicalized key %q — canonicalization regressed", "  Eng  ")
	}
}

// TestResetClearsPresenceMaps asserts that /workspace/reset clears the
// presence maps so the rebuilt default roster does not surface stale `online`
// or `last_seen_at` from the prior session. Without this guard, a reset
// followed by a fresh launch shows lingering "online" indicators until
// another adapter detach/upsert arrives or the process restarts.
func TestResetClearsPresenceMaps(t *testing.T) {
	b := newTestBroker(t)
	host := &brokerTransportHost{broker: b}

	if err := host.UpsertParticipant(context.Background(),
		transport.Participant{AdapterName: openclawAdapterName, Key: "session-x"},
		transport.Binding{Scope: transport.ScopeMember, MemberSlug: "ceo"},
	); err != nil {
		t.Fatalf("UpsertParticipant: %v", err)
	}
	b.mu.Lock()
	if _, ok := b.memberPresence["ceo"]; !ok {
		b.mu.Unlock()
		t.Fatal("precondition: presence record not stored")
	}
	b.mu.Unlock()

	b.Reset()

	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.memberPresence) != 0 {
		t.Errorf("memberPresence after Reset: got %d rows, want 0", len(b.memberPresence))
	}
	if len(b.presenceKeyToSlug) != 0 {
		t.Errorf("presenceKeyToSlug after Reset: got %d rows, want 0", len(b.presenceKeyToSlug))
	}
}
