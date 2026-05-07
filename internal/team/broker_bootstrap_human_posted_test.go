package team

// Tests for bootstrapHumanHasPostedLocked — the function that re-seeds
// b.humanHasPosted from the persisted message log when a broker restarts.
// Four cases:
//
//  1. Empty log → restart → humanHasPosted == false.
//  2. Human message in log → restart → humanHasPosted == true.
//  3. Empty/whitespace From → restart → humanHasPosted stays false
//     (pins the adversarial-fix: isHumanMessageSender("") returns true,
//     but the empty-From guard in appendMessageLocked and bootstrap must
//     prevent a false positive).
//  4. Agent-only message in log → restart → humanHasPosted == false.
//
// Each test uses the standard newTestBroker/saveLocked/reloadedBroker
// pattern (same as TestBrokerSurfaceMetadataPersists and friends) so
// there is no need to touch production code.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// metaHumanHasPosted GETs /office-members on the given broker via the
// real handler and decodes meta.humanHasPosted from the JSON response.
// It is a strict helper: any unexpected status or decode failure is
// fatal, not just an error.
func metaHumanHasPosted(t *testing.T, b *Broker) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	b.handleOfficeMembers(rec, httptest.NewRequest(http.MethodGet, "/office-members", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("handleOfficeMembers: expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Meta struct {
			HumanHasPosted bool `json:"humanHasPosted"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode /office-members response: %v (body=%s)", err, rec.Body.String())
	}
	return resp.Meta.HumanHasPosted
}

// seedAndReload saves the broker state to disk (using the standard
// saveLocked path under b.mu) and returns a fresh broker that fully
// replicates a production restart: NewBrokerAt on the same path →
// loadState → bootstrapHumanHasPostedLocked. This is the "restart" in
// all four test cases.
//
// Why not use reloadedBroker? reloadedBroker calls NewBrokerAt (which
// runs bootstrapHumanHasPostedLocked against an empty message slice
// because skipBrokerStateLoadOnConstruct is true in tests) and then
// calls loadState() a second time to fill b.messages — but at that
// point bootstrapHumanHasPostedLocked has already run and won't run
// again. The explicit call below mirrors exactly what NewBrokerAt does
// in production (lines 330-337 of broker.go), producing a broker whose
// humanHasPosted reflects the reloaded message log.
func seedAndReload(t *testing.T, b *Broker) *Broker {
	t.Helper()
	b.mu.Lock()
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked: %v", err)
	}
	b.mu.Unlock()

	fresh := NewBrokerAt(b.statePath)
	if err := fresh.loadState(); err != nil {
		t.Fatalf("loadState: %v", err)
	}
	// Re-run bootstrap after the explicit loadState so humanHasPosted
	// reflects the reloaded message slice, not the empty slice present
	// when NewBrokerAt ran it the first time (test-mode skips auto-load).
	fresh.mu.Lock()
	fresh.bootstrapHumanHasPostedLocked()
	fresh.mu.Unlock()
	return fresh
}

// TestBootstrapHumanHasPosted_EmptyLog verifies that an empty message log
// leaves humanHasPosted false after a broker restart.
func TestBootstrapHumanHasPosted_EmptyLog(t *testing.T) {
	b := newTestBroker(t)
	// No messages seeded — log is empty.
	reloaded := seedAndReload(t, b)

	if reloaded.HumanHasPosted() {
		t.Fatal("empty log: expected humanHasPosted=false after restart, got true")
	}
	if metaHumanHasPosted(t, reloaded) {
		t.Fatal("empty log: expected meta.humanHasPosted=false on /office-members, got true")
	}
}

// TestBootstrapHumanHasPosted_HumanMessage verifies that a persisted
// human-authored message flips humanHasPosted true after a broker restart.
func TestBootstrapHumanHasPosted_HumanMessage(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.counter = 1
	b.messages = []channelMessage{
		{
			ID:        "msg-1",
			From:      "human:najm",
			Channel:   "general",
			Content:   "hello from a human",
			Timestamp: "2026-05-06T10:00:00Z",
		},
	}
	b.mu.Unlock()

	reloaded := seedAndReload(t, b)

	if !reloaded.HumanHasPosted() {
		t.Fatal("human message persisted: expected humanHasPosted=true after restart, got false")
	}
	if !metaHumanHasPosted(t, reloaded) {
		t.Fatal("human message persisted: expected meta.humanHasPosted=true on /office-members, got false")
	}
}

// TestBootstrapHumanHasPosted_EmptyFromLatch pins the adversarial fix:
// a message with an empty (or whitespace-only) From field must NOT set
// humanHasPosted, even though isHumanMessageSender("") historically
// returns true. The guard in both appendMessageLocked and
// bootstrapHumanHasPostedLocked must hold across restarts.
func TestBootstrapHumanHasPosted_EmptyFromLatch(t *testing.T) {
	cases := []struct {
		name string
		from string
	}{
		{"empty-string", ""},
		{"whitespace-only", "   "},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			b := newTestBroker(t)
			b.mu.Lock()
			b.counter = 1
			b.messages = []channelMessage{
				{
					ID:        "msg-1",
					From:      tc.from,
					Channel:   "general",
					Content:   "mysterious origin",
					Timestamp: "2026-05-06T10:00:00Z",
				},
			}
			b.mu.Unlock()

			reloaded := seedAndReload(t, b)

			if reloaded.HumanHasPosted() {
				t.Fatalf("from=%q: expected humanHasPosted=false (empty-From latch), got true", tc.from)
			}
			if metaHumanHasPosted(t, reloaded) {
				t.Fatalf("from=%q: expected meta.humanHasPosted=false (empty-From latch), got true", tc.from)
			}
		})
	}
}

// TestBootstrapHumanHasPosted_AgentOnlyMessages verifies that a log
// containing only agent-authored messages leaves humanHasPosted false
// after a broker restart.
func TestBootstrapHumanHasPosted_AgentOnlyMessages(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.counter = 2
	b.messages = []channelMessage{
		{
			ID:        "msg-1",
			From:      "ceo",
			Channel:   "general",
			Content:   "agent message one",
			Timestamp: "2026-05-06T10:00:00Z",
		},
		{
			ID:        "msg-2",
			From:      "eng",
			Channel:   "general",
			Content:   "agent message two",
			Timestamp: "2026-05-06T10:01:00Z",
		},
	}
	b.mu.Unlock()

	reloaded := seedAndReload(t, b)

	if reloaded.HumanHasPosted() {
		t.Fatal("agent-only log: expected humanHasPosted=false after restart, got true")
	}
	if metaHumanHasPosted(t, reloaded) {
		t.Fatal("agent-only log: expected meta.humanHasPosted=false on /office-members, got true")
	}
}
