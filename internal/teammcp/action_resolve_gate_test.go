package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/action"
)

// TestActionResolveWireDecode pins the CONSUMER side of the resolve wire shape:
// the broker emits these keys (pinned in internal/team/
// broker_integrations_wire_test.go), and the gate must decode every field it
// relies on. If the broker renames a tag, that golden test fails; if the gate's
// struct tags drift, this test fails — together they catch silent field drops
// across the two hand-mirrored copies.
func TestActionResolveWireDecode(t *testing.T) {
	// This literal is the broker's integrationResolveResponse JSON for an
	// `approve` decision (keys must match broker_integrations_wire_test.go).
	const brokerJSON = `{
		"decision":"approve","state":"connected","provider":"composio",
		"platform":"gmail","action_id":"GMAIL_SEND_EMAIL","name":"Gmail",
		"logo_url":"logo","read_only":false,
		"account":{"name":"Founder Gmail","key":"ca_1"},
		"raw_envelope":{"method":"POST","url":"https://x/y","headers":{"h":"1"},"data":{"to":"a@b.com","token":"***"}},
		"detail":"","request_id":"request-1"
	}`
	var resp actionResolveResponse
	if err := json.Unmarshal([]byte(brokerJSON), &resp); err != nil {
		t.Fatalf("decode broker resolve JSON: %v", err)
	}
	if resp.Decision != "approve" || resp.State != "connected" || resp.Platform != "gmail" ||
		resp.ActionID != "GMAIL_SEND_EMAIL" || resp.Name != "Gmail" || resp.LogoURL != "logo" {
		t.Fatalf("scalar fields dropped: %+v", resp)
	}
	if resp.Account == nil || resp.Account.Name != "Founder Gmail" || resp.Account.Key != "ca_1" {
		t.Fatalf("account dropped: %+v", resp.Account)
	}
	if resp.RawEnvelope == nil || resp.RawEnvelope.Method != "POST" || resp.RawEnvelope.URL != "https://x/y" {
		t.Fatalf("raw envelope dropped: %+v", resp.RawEnvelope)
	}
	if resp.RawEnvelope.Data["token"] != "***" {
		t.Fatalf("envelope body dropped/altered: %+v", resp.RawEnvelope.Data)
	}

	// The gate re-emits the structured payload as integration_action; round-trip
	// it through actionCardPayload to pin that the producer key matches.
	card := buildActionCardPayload(TeamActionExecuteArgs{Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL"}, resp)
	raw, _ := json.Marshal(card)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("re-marshal card: %v", err)
	}
	for _, k := range []string{"platform", "action_id", "verb", "name", "logo_url", "account", "raw_envelope"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("actionCardPayload missing key %q: %v", k, m)
		}
	}
}

func TestActionResolveBlockMessage(t *testing.T) {
	cases := []struct {
		decision string
		want     string
	}{
		{"connect", "not connected"},
		{"wait", "still settling"},
		{"fail_safe", "temporarily unreachable"},
		{"fallback", "not available via Composio"},
		{"weird", "cannot proceed"},
	}
	for _, c := range cases {
		msg := actionResolveBlockMessage(actionResolveResponse{Decision: c.decision, State: "missing", Detail: "why"}, "Gmail")
		if !strings.Contains(msg, c.want) {
			t.Errorf("decision %q: message %q missing %q", c.decision, msg, c.want)
		}
		if !strings.Contains(msg, "Gmail") {
			t.Errorf("decision %q: message %q missing platform label", c.decision, msg)
		}
		if !strings.Contains(msg, "why") {
			t.Errorf("decision %q: message %q dropped the detail", c.decision, msg)
		}
	}
}

// recordingActionProvider counts ExecuteAction calls so a test can assert the
// gate blocked an action before it reached the provider.
type recordingActionProvider struct {
	stubActionProvider
	calls *int
}

func (p *recordingActionProvider) ExecuteAction(_ context.Context, req action.ExecuteRequest) (action.ExecuteResult, error) {
	*p.calls++
	return action.ExecuteResult{
		DryRun:  req.DryRun,
		Request: action.ExecuteEnvelope{Method: "POST", URL: "https://example.test/send"},
	}, nil
}

func resolveGateResultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func startResolveGateBroker(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(b.Stop)
	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())
	// Force Composio unconfigured so the resolver classifies a mutating action
	// as needing a connection (decision=connect) without any network.
	t.Setenv("WUPHF_COMPOSIO_API_KEY", "")
	t.Setenv("COMPOSIO_API_KEY", "")
	t.Setenv("WUPHF_COMPOSIO_USER_ID", "")
	t.Setenv("COMPOSIO_USER_ID", "")
}

// A mutating action against an unconnected integration is blocked by the gate
// and never reaches the provider — the core slice-2 guarantee.
func TestHandleTeamActionExecuteGatesUnconnectedMutating(t *testing.T) {
	startResolveGateBroker(t)

	calls := 0
	prev := externalActionProvider
	externalActionProvider = &recordingActionProvider{calls: &calls}
	defer func() { externalActionProvider = prev }()

	res, _, err := handleTeamActionExecute(context.Background(), nil, TeamActionExecuteArgs{
		Platform: "gmail",
		ActionID: "GMAIL_SEND_EMAIL",
		MySlug:   "ceo",
		Channel:  "general",
		Data:     map[string]any{"to": "lead@acme.com"},
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected an error result blocking the action, got %+v", res)
	}
	text := resolveGateResultText(t, res)
	if !strings.Contains(text, "not connected") {
		t.Fatalf("expected a connect block message, got: %s", text)
	}
	if calls != 0 {
		t.Fatalf("provider.ExecuteAction was called %d times; the gate must block an unconnected mutating action before it fires", calls)
	}
}

// A read-only action bypasses the connection gate and executes as before — the
// gate must not add a provider round-trip to the hot lookup path.
func TestHandleTeamActionExecuteReadOnlyBypassesGate(t *testing.T) {
	startResolveGateBroker(t)

	calls := 0
	prev := externalActionProvider
	externalActionProvider = &recordingActionProvider{calls: &calls}
	defer func() { externalActionProvider = prev }()

	res, _, err := handleTeamActionExecute(context.Background(), nil, TeamActionExecuteArgs{
		Platform: "gmail",
		ActionID: "GMAIL_FETCH_EMAILS",
		MySlug:   "ceo",
		Channel:  "general",
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("read-only action should execute, got error result: %+v", res)
	}
	if calls != 1 {
		t.Fatalf("read-only action should execute via the provider exactly once, got %d", calls)
	}
}

// A resolver response with an unrecognized decision string must fail CLOSED:
// the action is blocked and the provider is never called. This guards the
// fail-open hole where an empty/garbled/novel decision fell through the switch.
func TestHandleTeamActionExecuteFailsClosedOnUnknownDecision(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/integrations/resolve" {
			_, _ = w.Write([]byte(`{"decision":"bogus","platform":"gmail"}`))
			return
		}
		// Other broker calls (e.g. channel resolution) get a benign response;
		// the gate must block before reaching the approval/execute path.
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	t.Setenv("WUPHF_TEAM_BROKER_URL", srv.URL)
	t.Setenv("WUPHF_BROKER_TOKEN", "test-token")

	calls := 0
	prev := externalActionProvider
	externalActionProvider = &recordingActionProvider{calls: &calls}
	defer func() { externalActionProvider = prev }()

	res, _, err := handleTeamActionExecute(context.Background(), nil, TeamActionExecuteArgs{
		Platform: "gmail",
		ActionID: "GMAIL_SEND_EMAIL",
		MySlug:   "ceo",
		Channel:  "general",
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected a fail-closed error result, got %+v", res)
	}
	if text := resolveGateResultText(t, res); !strings.Contains(text, "unrecognized decision") {
		t.Fatalf("expected unrecognized-decision message, got: %s", text)
	}
	if calls != 0 {
		t.Fatalf("provider executed on an unknown decision; the gate must fail closed (calls=%d)", calls)
	}
}

// A `proceed` decision (a standing human grant covers this exact action) must
// skip the approval modal AND still execute: the provider is called and no
// approval request blocks the run. This is the scoped-grant fast path.
func TestHandleTeamActionExecuteGrantProceedSkipsModalAndExecutes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")
	approvalPosted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/integrations/resolve":
			// Granted: resolver returns proceed with the verified connection.
			_, _ = w.Write([]byte(`{"decision":"proceed","platform":"gmail","account":{"key":"ca_123"}}`))
		case "/requests":
			// If the gate ever creates an approval request for a granted action,
			// the modal was NOT skipped — record it so the test can fail.
			approvalPosted = true
			_, _ = w.Write([]byte(`{"id":"request-should-not-exist"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	t.Setenv("WUPHF_TEAM_BROKER_URL", srv.URL)
	t.Setenv("WUPHF_BROKER_TOKEN", "test-token")

	calls := 0
	prev := externalActionProvider
	externalActionProvider = &recordingActionProvider{calls: &calls}
	defer func() { externalActionProvider = prev }()

	res, _, err := handleTeamActionExecute(context.Background(), nil, TeamActionExecuteArgs{
		Platform: "gmail",
		ActionID: "GMAIL_SEND_EMAIL",
		MySlug:   "ceo",
		Channel:  "general",
		Data:     map[string]any{"to": "lead@acme.com"},
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("granted action should execute, got error result: %+v", res)
	}
	if calls != 1 {
		t.Fatalf("granted action should execute via the provider exactly once, got %d", calls)
	}
	if approvalPosted {
		t.Fatalf("granted action created an approval request; the modal must be skipped on a proceed decision")
	}
}
