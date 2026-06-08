package teammcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/action"
)

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
