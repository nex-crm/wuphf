package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// A grant matches EXACTLY one (agent, platform, action_id) — never a different
// action, agent, or platform, and never a wildcard. This is the invariant that
// keeps a scoped grant from widening the approval bypass beyond what the human
// authorized.
func TestActionGrantExactMatchOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now().UTC()

	b.addActionGrant(actionGrant{AgentSlug: "ceo", Platform: "gmail", ActionScope: "GMAIL_SEND_EMAIL", GrantedBy: "human"})

	if !b.hasActiveActionGrant("ceo", "gmail", "GMAIL_SEND_EMAIL", now) {
		t.Fatalf("exact grant did not match")
	}
	// Case-insensitive on all three keys.
	if !b.hasActiveActionGrant("CEO", "Gmail", "gmail_send_email", now) {
		t.Fatalf("grant match should be case-insensitive")
	}
	for _, c := range []struct{ agent, platform, action, label string }{
		{"ceo", "gmail", "GMAIL_DELETE_EMAIL", "different action"},
		{"sales", "gmail", "GMAIL_SEND_EMAIL", "different agent"},
		{"ceo", "slack", "GMAIL_SEND_EMAIL", "different platform"},
	} {
		if b.hasActiveActionGrant(c.agent, c.platform, c.action, now) {
			t.Errorf("%s must NOT match the grant", c.label)
		}
	}
}

// Expired and revoked grants never authorize, and an unparseable expiry fails
// closed (treated as expired) rather than silently authorizing.
func TestActionGrantExpiryAndRevoke(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now().UTC()

	expired := b.addActionGrant(actionGrant{
		AgentSlug: "ceo", Platform: "gmail", ActionScope: "GMAIL_SEND_EMAIL",
		ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339),
	})
	if b.hasActiveActionGrant("ceo", "gmail", "GMAIL_SEND_EMAIL", now) {
		t.Fatalf("expired grant must not authorize")
	}
	if actionGrantActive(actionGrant{ExpiresAt: "not-a-time"}, now) {
		t.Fatalf("unparseable expiry must fail closed (treated as expired)")
	}
	_ = expired

	live := b.addActionGrant(actionGrant{AgentSlug: "ceo", Platform: "slack", ActionScope: "SLACK_SEND_MESSAGE"})
	if !b.hasActiveActionGrant("ceo", "slack", "SLACK_SEND_MESSAGE", now) {
		t.Fatalf("live grant should authorize before revoke")
	}
	if _, ok := b.revokeActionGrant(live.ID, "you"); !ok {
		t.Fatalf("revoke should find the grant")
	}
	if b.hasActiveActionGrant("ceo", "slack", "SLACK_SEND_MESSAGE", now) {
		t.Fatalf("revoked grant must not authorize")
	}
}

// addActionGrant is idempotent on (agent, platform, scope): the same triple does
// not stack duplicate grants.
func TestActionGrantIdempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	g1 := b.addActionGrant(actionGrant{AgentSlug: "ceo", Platform: "gmail", ActionScope: "GMAIL_SEND_EMAIL"})
	g2 := b.addActionGrant(actionGrant{AgentSlug: "ceo", Platform: "gmail", ActionScope: "GMAIL_SEND_EMAIL"})
	if g1.ID != g2.ID {
		t.Fatalf("duplicate grant minted: %s vs %s", g1.ID, g2.ID)
	}
	if len(b.actionGrants) != 1 {
		t.Fatalf("expected 1 stored grant, got %d", len(b.actionGrants))
	}
}

// Every grant is capped to maxGrantTTL even when minted with no expiry, so a
// forgotten or maliciously-created standing grant self-expires (defense in
// depth against the broker-token trust boundary).
func TestActionGrantCappedToMaxTTL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := NewBrokerAt(filepath.Join(t.TempDir(), "state.json"))
	now := time.Now().UTC()

	g := b.addActionGrant(actionGrant{AgentSlug: "ceo", Platform: "gmail", ActionScope: "GMAIL_SEND_EMAIL"})
	if g.ExpiresAt == "" {
		t.Fatalf("grant minted with no expiry; max TTL cap not applied")
	}
	exp, ok := parseGrantTime(g.ExpiresAt)
	if !ok {
		t.Fatalf("capped expiry is unparseable: %q", g.ExpiresAt)
	}
	if exp.After(now.Add(maxGrantTTL + time.Minute)) {
		t.Fatalf("expiry %v exceeds the max TTL cap", exp)
	}
	if !b.hasActiveActionGrant("ceo", "gmail", "GMAIL_SEND_EMAIL", now) {
		t.Fatalf("a freshly capped grant should authorize now")
	}
	if b.hasActiveActionGrant("ceo", "gmail", "GMAIL_SEND_EMAIL", now.Add(maxGrantTTL+time.Hour)) {
		t.Fatalf("grant must not authorize past the cap")
	}

	// A request for a longer-than-policy expiry is clamped down.
	far := now.Add(365 * 24 * time.Hour).Format(time.RFC3339)
	g2 := b.addActionGrant(actionGrant{AgentSlug: "ceo", Platform: "slack", ActionScope: "SLACK_SEND_MESSAGE", ExpiresAt: far})
	exp2, _ := parseGrantTime(g2.ExpiresAt)
	if exp2.After(now.Add(maxGrantTTL + time.Minute)) {
		t.Fatalf("over-long expiry not clamped: %v", exp2)
	}
}

// A connected mutating action with a standing grant resolves to `proceed` (skip
// the modal) instead of `approve`. Without the grant it resolves to `approve`.
func TestResolveProceedsWithGrant(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("WUPHF_RUNTIME_HOME", tmp)
	t.Setenv("WUPHF_COMPOSIO_API_KEY", "cmp_test")
	t.Setenv("WUPHF_COMPOSIO_USER_ID", "ceo@example.com")

	composioMux := http.NewServeMux()
	composioMux.HandleFunc("/connected_accounts", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{
				"id": "ca_123", "status": "ACTIVE",
				"toolkit":    map[string]any{"slug": "gmail", "name": "Gmail"},
				"connection": map[string]any{"name": "Founder Gmail"},
			}},
		})
	})
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

	body, _ := json.Marshal(integrationResolveRequest{
		Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL", Agent: "ceo",
		Data: map[string]any{"to": "lead@acme.com"},
	})

	// No grant yet: connected mutating action -> approve (modal).
	got := decodeResolve(t, integrationRequest(t, srv, b, http.MethodPost, "/integrations/resolve", body))
	if got.Decision != "approve" {
		t.Fatalf("without a grant, decision = %q, want approve", got.Decision)
	}

	// Mint a grant for exactly this (agent, platform, action), then re-resolve.
	b.addActionGrant(actionGrant{AgentSlug: "ceo", Platform: "gmail", ActionScope: "GMAIL_SEND_EMAIL", GrantedBy: "human"})
	got = decodeResolve(t, integrationRequest(t, srv, b, http.MethodPost, "/integrations/resolve", body))
	if got.Decision != "proceed" {
		t.Fatalf("with a grant, decision = %q, want proceed (skip the modal)", got.Decision)
	}
}

type grantListResponse struct {
	Grants []actionGrant `json:"grants"`
}

// The grants endpoint mints concrete-scope grants, rejects wildcards, lists
// active grants, and revokes — the contract the web grant button + revoke UI
// (slice 5b) depend on. It also persists across a reload.
func TestActionGrantEndpointLifecycle(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	statePath := filepath.Join(t.TempDir(), "state.json")
	b := NewBrokerAt(statePath)
	srv := newIntegrationsTestServer(t, b)
	defer srv.Close()

	// Wildcard scope is rejected — a grant can never be platform-wide.
	wild, _ := json.Marshal(map[string]any{"agent_slug": "ceo", "platform": "gmail", "action_scope": "*"})
	resp := integrationRequest(t, srv, b, http.MethodPost, "/integrations/grants", wild)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("wildcard scope should be 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	create, _ := json.Marshal(map[string]any{
		"agent_slug": "ceo", "platform": "gmail", "action_scope": "GMAIL_SEND_EMAIL", "channel": "general",
	})
	resp = integrationRequest(t, srv, b, http.MethodPost, "/integrations/grants", create)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create grant status = %d", resp.StatusCode)
	}
	var created struct {
		Grant actionGrant `json:"grant"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	if created.Grant.ID == "" {
		t.Fatalf("created grant has no id")
	}

	// List shows the active grant.
	resp = integrationRequest(t, srv, b, http.MethodGet, "/integrations/grants", nil)
	var list grantListResponse
	_ = json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Grants) != 1 || list.Grants[0].ID != created.Grant.ID {
		t.Fatalf("list did not return the created grant: %+v", list.Grants)
	}

	// It authorizes in the store.
	if !b.hasActiveActionGrant("ceo", "gmail", "GMAIL_SEND_EMAIL", time.Now().UTC()) {
		t.Fatalf("created grant does not authorize")
	}

	// Revoke, then it no longer authorizes and drops out of the active list.
	revoke, _ := json.Marshal(map[string]any{"action": "revoke", "id": created.Grant.ID})
	resp = integrationRequest(t, srv, b, http.MethodPost, "/integrations/grants", revoke)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if b.hasActiveActionGrant("ceo", "gmail", "GMAIL_SEND_EMAIL", time.Now().UTC()) {
		t.Fatalf("revoked grant still authorizes")
	}

	// Persists across reload (revoked state included).
	b2 := NewBrokerAt(statePath)
	if err := b2.loadState(); err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if b2.hasActiveActionGrant("ceo", "gmail", "GMAIL_SEND_EMAIL", time.Now().UTC()) {
		t.Fatalf("revoked grant authorized after reload")
	}
}
