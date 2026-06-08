package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nex-crm/wuphf/internal/action"
)

// The connect decision kind is a blocking, two-option human decision: Connect
// (drives OAuth) or Skip (abandons the action). This is the user's "block on a
// typed Connect decision" call, so it must register as a human decision.
func TestConnectDecisionKindDefaults(t *testing.T) {
	options, recommended := requestOptionDefaults("connect")
	if recommended != "connect" {
		t.Fatalf("recommended option = %q, want connect", recommended)
	}
	if len(options) != 2 || options[0].ID != "connect" || options[1].ID != "skip" {
		t.Fatalf("connect options = %+v, want [connect, skip]", options)
	}
	for _, o := range options {
		if o.RequiresText {
			t.Errorf("connect option %q should not require free text", o.ID)
		}
	}
	if !requestNeedsHumanDecision(humanInterview{Kind: "connect"}) {
		t.Fatalf("connect kind must register as a human decision")
	}
}

func activeConnectCards(b *Broker, platform string) []humanInterview {
	key := connectRequestDedupeKey(platform)
	var out []humanInterview
	for _, req := range b.requests {
		if normalizeRequestKind(req.Kind) == "connect" &&
			req.DedupeKey == key && requestIsActive(req) {
			out = append(out, req)
		}
	}
	return out
}

// A mutating action against a missing connection routes to connect AND raises a
// single blocking Connect card. A second resolve of the same platform dedupes
// onto the same card (workspace-wide) rather than stacking duplicates.
func TestResolveRaisesAndDedupesConnectCard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Force Composio unconfigured so the resolver classifies a mutating action as
	// connect deterministically, with no network.
	t.Setenv("WUPHF_COMPOSIO_API_KEY", "")
	t.Setenv("COMPOSIO_API_KEY", "")
	t.Setenv("WUPHF_COMPOSIO_USER_ID", "")
	t.Setenv("COMPOSIO_USER_ID", "")

	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	srv := newIntegrationsTestServer(t, b)
	defer srv.Close()

	resolve := func() integrationResolveResponse {
		body, _ := json.Marshal(integrationResolveRequest{
			Platform: "gmail",
			ActionID: "GMAIL_SEND_EMAIL",
			Agent:    "ceo",
			Channel:  "general",
			Data:     map[string]any{"to": "lead@acme.com"},
		})
		return decodeResolve(t, integrationRequest(t, srv, b, http.MethodPost, "/integrations/resolve", body))
	}

	first := resolve()
	if first.Decision != "connect" {
		t.Fatalf("decision = %q, want connect (%+v)", first.Decision, first)
	}
	if first.RequestID == "" {
		t.Fatalf("connect decision did not raise a card (empty request_id)")
	}
	cards := activeConnectCards(b, "gmail")
	if len(cards) != 1 {
		t.Fatalf("expected exactly one active connect card, got %d", len(cards))
	}
	card := cards[0]
	if card.ID != first.RequestID {
		t.Fatalf("card id %q != response request_id %q", card.ID, first.RequestID)
	}
	if !card.Blocking || !card.Required {
		t.Fatalf("connect card must block + require a decision, got blocking=%v required=%v", card.Blocking, card.Required)
	}
	if card.Platform != "gmail" {
		t.Fatalf("connect card Platform = %q, want gmail (the web Connect card drives OAuth from it)", card.Platform)
	}

	second := resolve()
	if second.RequestID != first.RequestID {
		t.Fatalf("second resolve minted a new card %q (want dedupe onto %q)", second.RequestID, first.RequestID)
	}
	if got := len(activeConnectCards(b, "gmail")); got != 1 {
		t.Fatalf("dedupe failed: %d active connect cards after two resolves", got)
	}
}

// When the OAuth flow reports the connection live, the fan-out flips the
// registry to connected and auto-answers the open Connect card so the parked
// action resumes with zero re-asking. It must be idempotent across repeat polls.
func TestFanOutConnectedResolvesParkedCard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))

	reqID := b.ensureConnectRequest("gmail", "general", "ceo", "Gmail", "")
	if reqID == "" {
		t.Fatalf("ensureConnectRequest returned no id")
	}
	if got := len(activeConnectCards(b, "gmail")); got != 1 {
		t.Fatalf("expected one active connect card before fan-out, got %d", got)
	}

	b.fanOutConnected("gmail", "ca_123", "Founder Gmail", "you")

	// The card is terminally answered with connect.
	var card *humanInterview
	for i := range b.requests {
		if b.requests[i].ID == reqID {
			card = &b.requests[i]
			break
		}
	}
	if card == nil {
		t.Fatalf("connect card %s vanished", reqID)
	}
	if requestIsActive(*card) {
		t.Fatalf("connect card still active after fan-out (status=%q)", card.Status)
	}
	if card.Answered == nil || card.Answered.ChoiceID != "connect" {
		t.Fatalf("connect card not auto-answered with connect: %+v", card.Answered)
	}

	// The registry now holds the live connection.
	entry, ok := b.lookupConnectionRegistry("gmail")
	if !ok || entry.State != string(action.StateConnected) || entry.ConnectionKey != "ca_123" {
		t.Fatalf("registry not updated by fan-out: ok=%v entry=%+v", ok, entry)
	}

	// Idempotent: a second fan-out (the next 2s poll) finds no active cards.
	b.fanOutConnected("gmail", "ca_123", "Founder Gmail", "you")
	if got := len(activeConnectCards(b, "gmail")); got != 0 {
		t.Fatalf("fan-out not idempotent: %d active connect cards remain", got)
	}
}

// End-to-end over HTTP: a parked Connect card is auto-resolved the moment
// /integrations/connect-status observes the OAuth completion — the wiring that
// makes "connect once, resume automatically" real.
func TestConnectStatusFanOutEndToEnd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("WUPHF_RUNTIME_HOME", tmp)
	t.Setenv("WUPHF_COMPOSIO_API_KEY", "cmp_test")
	t.Setenv("WUPHF_COMPOSIO_USER_ID", "ceo@example.com")

	composioMux := http.NewServeMux()
	composioMux.HandleFunc("/connected_accounts/ca_123", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "ca_123", "status": "ACTIVE", "toolkit": map[string]any{"slug": "gmail"},
		})
	})
	composioServer := httptest.NewServer(composioMux)
	defer composioServer.Close()
	t.Setenv("WUPHF_COMPOSIO_BASE_URL", composioServer.URL)

	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	srv := newIntegrationsTestServer(t, b)
	defer srv.Close()

	// Park a Connect card directly (the resolve path that raises it is covered
	// separately; here we exercise the connect-status -> fan-out wiring).
	reqID := b.ensureConnectRequest("gmail", "general", "ceo", "Gmail", "")
	if got := len(activeConnectCards(b, "gmail")); got != 1 {
		t.Fatalf("expected a parked connect card, got %d", got)
	}

	resp := integrationRequest(t, srv, b, http.MethodGet, "/integrations/connect-status?provider=composio&connect_id=ca_123", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("connect-status code = %d", resp.StatusCode)
	}

	if got := len(activeConnectCards(b, "gmail")); got != 0 {
		t.Fatalf("connect-status did not resolve the parked card: %d still active", got)
	}
	var resolved *humanInterview
	for i := range b.requests {
		if b.requests[i].ID == reqID {
			resolved = &b.requests[i]
			break
		}
	}
	if resolved == nil || resolved.Answered == nil || resolved.Answered.ChoiceID != "connect" {
		t.Fatalf("parked card not auto-answered via connect-status: %+v", resolved)
	}
	if entry, ok := b.lookupConnectionRegistry("gmail"); !ok || entry.State != string(action.StateConnected) {
		t.Fatalf("registry not connected after connect-status fan-out: ok=%v entry=%+v", ok, entry)
	}
}
