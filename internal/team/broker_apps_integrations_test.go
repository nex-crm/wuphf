package team

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/action"
)

// postAppsBridgeJSON posts a body to a Bridge-v2 apps endpoint with the broker
// token and returns the decoded JSON response + status. Unlike postAppsJSON it
// does NOT fail on non-200 — these endpoints return business outcomes (400 for
// bad input, 200 for connected/needs_approval) the test must assert on.
func postAppsBridgeJSON(t *testing.T, url, token string, body []byte) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

// TestAppsIntegrationCall_ReadNotConnected: a READ action against an
// unconfigured/unconnected integration returns { connected:false } with HTTP
// 200 (so the app renders a connect-state) and raises NO approval card.
func TestAppsIntegrationCall_ReadNotConnected(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	// Force composio "not configured" so the read path resolves not-connected
	// deterministically without any network.
	t.Setenv("WUPHF_NO_NEX", "1")

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	body, _ := json.Marshal(map[string]any{
		"platform": "gmail",
		"action":   "GMAIL_FETCH_EMAILS",
		"params":   map[string]any{"max_results": 5},
	})
	code, out := postAppsBridgeJSON(t, base+"/apps/integrations/call", b.Token(), body)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", code, out)
	}
	if connected, _ := out["connected"].(bool); connected {
		t.Fatalf("expected connected:false for unconfigured composio, got %v", out)
	}
	if readOnly, _ := out["read_only"].(bool); !readOnly {
		t.Fatalf("GMAIL_FETCH_EMAILS should classify read_only, got %v", out)
	}
	// A read must NEVER raise an approval card.
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.requests {
		if normalizeRequestKind(b.requests[i].Kind) == "approval" {
			t.Fatalf("a read action raised an approval card: %+v", b.requests[i])
		}
	}
}

// TestAppsIntegrationCall_MutatingRaisesApproval: a MUTATING action is NEVER
// executed by the app path. The broker returns { status:"needs_approval",
// request_id } and mints a real approval card carrying the structured
// integration_action payload — the SAME card the agent gate raises.
func TestAppsIntegrationCall_MutatingRaisesApproval(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	t.Setenv("WUPHF_NO_NEX", "1")

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	body, _ := json.Marshal(map[string]any{
		"platform": "gmail",
		"action":   "GMAIL_SEND_EMAIL",
		"params": map[string]any{
			"recipient_email": "alex@nex.ai",
			"subject":         "Hi",
			"body":            "hello",
		},
	})
	code, out := postAppsBridgeJSON(t, base+"/apps/integrations/call", b.Token(), body)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", code, out)
	}
	if status, _ := out["status"].(string); status != "needs_approval" {
		t.Fatalf("mutating action status = %v, want needs_approval; body=%v", out["status"], out)
	}
	if readOnly, _ := out["read_only"].(bool); readOnly {
		t.Fatalf("GMAIL_SEND_EMAIL must not classify read_only: %v", out)
	}
	reqID, _ := out["request_id"].(string)
	if reqID == "" {
		t.Fatalf("needs_approval must return a request_id: %v", out)
	}

	// The card must exist as a pending approval anchored to gmail and carry the
	// structured action payload (not just a context string).
	b.mu.Lock()
	defer b.mu.Unlock()
	var found *humanInterview
	for i := range b.requests {
		if b.requests[i].ID == reqID {
			found = &b.requests[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("no request %s minted; requests=%+v", reqID, b.requests)
	}
	if normalizeRequestKind(found.Kind) != "approval" {
		t.Fatalf("card kind = %q, want approval", found.Kind)
	}
	if found.Platform != "gmail" {
		t.Fatalf("card platform = %q, want gmail", found.Platform)
	}
	if found.Action == nil || found.Action.ActionID != "GMAIL_SEND_EMAIL" {
		t.Fatalf("card missing structured integration_action: %+v", found.Action)
	}
	// Non-blocking by design: a human chose to run this from their tool; it must
	// not freeze the channel.
	if found.Blocking {
		t.Fatalf("app-raised approval should be non-blocking")
	}
}

// TestAppsIntegrationCall_MutatingDedupes: an app that loops a mutating call for
// the same (platform, action) must fold onto ONE card, not stack duplicates.
func TestAppsIntegrationCall_MutatingDedupes(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	t.Setenv("WUPHF_NO_NEX", "1")

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	body, _ := json.Marshal(map[string]any{
		"platform": "gmail",
		"action":   "GMAIL_SEND_EMAIL",
		"params":   map[string]any{"subject": "x"},
	})
	_, first := postAppsBridgeJSON(t, base+"/apps/integrations/call", b.Token(), body)
	_, second := postAppsBridgeJSON(t, base+"/apps/integrations/call", b.Token(), body)
	if first["request_id"] != second["request_id"] {
		t.Fatalf("dedupe failed: %v vs %v", first["request_id"], second["request_id"])
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	count := 0
	for i := range b.requests {
		if normalizeRequestKind(b.requests[i].Kind) == "approval" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("want exactly 1 approval card after dedupe, got %d", count)
	}
}

// TestAppsIntegrationCall_RejectsBadInput: missing platform/action -> 400;
// oversize params -> 400. The host validates too, but the broker is authority.
func TestAppsIntegrationCall_RejectsBadInput(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	missing, _ := json.Marshal(map[string]any{"platform": "gmail"})
	if code, _ := postAppsBridgeJSON(t, base+"/apps/integrations/call", b.Token(), missing); code != http.StatusBadRequest {
		t.Fatalf("missing action: status = %d, want 400", code)
	}

	big := strings.Repeat("x", appIntegrationMaxParamBytes+100)
	oversize, _ := json.Marshal(map[string]any{
		"platform": "gmail",
		"action":   "GMAIL_FETCH_EMAILS",
		"params":   map[string]any{"blob": big},
	})
	if code, _ := postAppsBridgeJSON(t, base+"/apps/integrations/call", b.Token(), oversize); code != http.StatusBadRequest {
		t.Fatalf("oversize params: status = %d, want 400", code)
	}
}

// TestAppsIntegrationCatalog_Empty: an unconfigured composio yields an empty
// connected list (HTTP 200) rather than an error.
func TestAppsIntegrationCatalog_Empty(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	t.Setenv("WUPHF_NO_NEX", "1")
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	code, out := getAppsJSON(t, base+"/apps/integrations/catalog", b.Token())
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	conn, _ := out["connected"].([]any)
	if len(conn) != 0 {
		t.Fatalf("expected empty connected list for unconfigured composio, got %v", conn)
	}
}

// ─────────────────────────── ai() ──────────────────────────────────────────

// withFakeAppsLLM swaps the apps LLM completer for the duration of the test.
func withFakeAppsLLM(t *testing.T, fn appsLLMCompleter) {
	t.Helper()
	appsLLMCompleterMu.Lock()
	prev := appsLLMCompleterFn
	appsLLMCompleterFn = fn
	appsLLMCompleterMu.Unlock()
	t.Cleanup(func() {
		appsLLMCompleterMu.Lock()
		appsLLMCompleterFn = prev
		appsLLMCompleterMu.Unlock()
	})
}

// TestAppsAI_TextCompletion: a plain ai() call returns { text } from the
// configured provider seam.
func TestAppsAI_TextCompletion(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	withFakeAppsLLM(t, func(_ context.Context, _ string, prompt string) (string, error) {
		if !strings.Contains(prompt, "Summarize these") {
			t.Errorf("prompt missing app text: %q", prompt)
		}
		if !strings.Contains(prompt, "Input data:") || !strings.Contains(prompt, "Acme") {
			t.Errorf("input data not folded into prompt: %q", prompt)
		}
		return "  All quiet.  ", nil
	})

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	body, _ := json.Marshal(map[string]any{
		"prompt": "Summarize these emails.",
		"input":  []map[string]any{{"from": "Acme", "subject": "Renewal"}},
	})
	code, out := postAppsBridgeJSON(t, base+"/apps/ai", b.Token(), body)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", code, out)
	}
	if text, _ := out["text"].(string); text != "All quiet." {
		t.Fatalf("text = %q, want trimmed 'All quiet.'", out["text"])
	}
}

// TestAppsAI_JSONExtraction: with json:true a model answer wrapped in prose /
// fences still yields a parsed object.
func TestAppsAI_JSONExtraction(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	withFakeAppsLLM(t, func(_ context.Context, system string, _ string) (string, error) {
		if !strings.Contains(system, "SINGLE valid JSON") {
			t.Errorf("json mode should pin the system prompt to JSON: %q", system)
		}
		return "Sure! ```json\n{\"score\": 7, \"tier\": \"hot\"}\n``` done", nil
	})

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	body, _ := json.Marshal(map[string]any{
		"prompt": "Score this lead.",
		"json":   true,
	})
	code, out := postAppsBridgeJSON(t, base+"/apps/ai", b.Token(), body)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", code, out)
	}
	obj, _ := out["object"].(map[string]any)
	if obj == nil {
		t.Fatalf("expected parsed object, got %v", out)
	}
	if score, _ := obj["score"].(float64); score != 7 {
		t.Fatalf("object.score = %v, want 7", obj["score"])
	}
}

// TestAppsAI_Unavailable: a provider error surfaces as a typed ai_unavailable
// (HTTP 200) so the app renders a fallback rather than crashing.
func TestAppsAI_Unavailable(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	withFakeAppsLLM(t, func(_ context.Context, _ string, _ string) (string, error) {
		return "", fmt.Errorf("no provider configured")
	})

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	body, _ := json.Marshal(map[string]any{"prompt": "anything"})
	code, out := postAppsBridgeJSON(t, base+"/apps/ai", b.Token(), body)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", code, out)
	}
	if errStr, _ := out["error"].(string); errStr != "ai_unavailable" {
		t.Fatalf("error = %v, want ai_unavailable", out["error"])
	}
}

// TestAppsAI_Bounds: an empty prompt -> 400; an over-cap prompt -> 400; an
// over-cap input -> 400. The completer must never be called for these.
func TestAppsAI_Bounds(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	called := false
	withFakeAppsLLM(t, func(_ context.Context, _ string, _ string) (string, error) {
		called = true
		return "should not run", nil
	})

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	empty, _ := json.Marshal(map[string]any{"prompt": "   "})
	if code, _ := postAppsBridgeJSON(t, base+"/apps/ai", b.Token(), empty); code != http.StatusBadRequest {
		t.Fatalf("empty prompt: status = %d, want 400", code)
	}

	bigPrompt, _ := json.Marshal(map[string]any{"prompt": strings.Repeat("p", appAIMaxPromptBytes+1)})
	if code, _ := postAppsBridgeJSON(t, base+"/apps/ai", b.Token(), bigPrompt); code != http.StatusBadRequest {
		t.Fatalf("oversize prompt: status = %d, want 400", code)
	}

	bigInput, _ := json.Marshal(map[string]any{
		"prompt": "ok",
		"input":  strings.Repeat("i", appAIMaxInputBytes+1),
	})
	if code, _ := postAppsBridgeJSON(t, base+"/apps/ai", b.Token(), bigInput); code != http.StatusBadRequest {
		t.Fatalf("oversize input: status = %d, want 400", code)
	}

	if called {
		t.Fatalf("the LLM completer must not run for out-of-bounds requests")
	}
}

// TestAppsAI_RateLimited: an app that loops ai() past the per-actor window is
// throttled with HTTP 429 (security review H2) — the completer must stop being
// called once the bucket fills.
func TestAppsAI_RateLimited(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	calls := 0
	withFakeAppsLLM(t, func(_ context.Context, _ string, _ string) (string, error) {
		calls++
		return "ok", nil
	})

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	body, _ := json.Marshal(map[string]any{"prompt": "go"})
	limited := false
	for i := 0; i < appAIRateLimit+3; i++ {
		code, _ := postAppsBridgeJSON(t, base+"/apps/ai", b.Token(), body)
		if code == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatalf("expected a 429 after %d ai() calls", appAIRateLimit)
	}
	if calls > appAIRateLimit {
		t.Fatalf("completer ran %d times, want <= %d (throttle must stop work)", calls, appAIRateLimit)
	}
}

// TestActionIsReadOnly_MutatingVerbsM1 locks the M1 verb-table additions: these
// unambiguous mutating verbs must veto read-only even when composed with a read
// verb, so an App cannot reach them without an approval card.
func TestActionIsReadOnly_MutatingVerbsM1(t *testing.T) {
	mutating := []string{
		"GMAIL_REPLY_EMAIL",
		"GMAIL_FORWARD_EMAIL",
		"HUBSPOT_UPSERT_CONTACT",
		"LINEAR_ASSIGN_ISSUE",
		"GITHUB_FORK_REPO",
		"NOTION_DUPLICATE_PAGE",
		// composite: a read verb is present but the mutating verb vetoes.
		"GMAIL_FETCH_AND_REPLY",
	}
	for _, id := range mutating {
		if action.ActionIsReadOnly(id) {
			t.Errorf("ActionIsReadOnly(%q) = true, want false (M1 mutating verb)", id)
		}
	}
	// Sanity: genuine reads still classify read-only after the additions.
	for _, id := range []string{"GMAIL_FETCH_EMAILS", "SLACK_LIST_CHANNELS", "HUBSPOT_GET_CONTACT"} {
		if !action.ActionIsReadOnly(id) {
			t.Errorf("ActionIsReadOnly(%q) = false, want true (still a read)", id)
		}
	}
}

// ─────────────────────────── pure helpers ──────────────────────────────────

func TestBuildAppsAIPrompt_Bounds(t *testing.T) {
	if _, _, ok := buildAppsAIPrompt(strings.Repeat("x", appAIMaxPromptBytes+1), nil, false); ok {
		t.Errorf("over-cap prompt should be rejected")
	}
	if _, _, ok := buildAppsAIPrompt("ok", strings.Repeat("x", appAIMaxInputBytes+1), false); ok {
		t.Errorf("over-cap input should be rejected")
	}
	sys, user, ok := buildAppsAIPrompt("hi", map[string]any{"a": 1}, true)
	if !ok {
		t.Fatalf("valid prompt rejected")
	}
	if !strings.Contains(sys, "SINGLE valid JSON") {
		t.Errorf("json mode missing from system prompt: %q", sys)
	}
	if !strings.Contains(user, "Input data:") || !strings.Contains(user, `"a":1`) {
		t.Errorf("input not folded: %q", user)
	}
}

func TestExtractFirstJSON(t *testing.T) {
	cases := []struct {
		in       string
		wantOK   bool
		contains string
	}{
		{`{"a":1}`, true, `"a":1`},
		{"prefix {\"a\":1} suffix", true, `"a":1`},
		{"```json\n[1,2,3]\n```", true, `[1,2,3]`},
		{`text {"s":"has } brace"} end`, true, `has } brace`},
		{"no json here", false, ""},
		{"{unbalanced", false, ""},
	}
	for _, c := range cases {
		got, ok := extractFirstJSON(c.in)
		if ok != c.wantOK {
			t.Errorf("extractFirstJSON(%q) ok = %v, want %v", c.in, ok, c.wantOK)
			continue
		}
		if ok && !strings.Contains(string(got), c.contains) {
			t.Errorf("extractFirstJSON(%q) = %q, want contains %q", c.in, got, c.contains)
		}
	}
}

func TestAppActionApprovalDedupeKey(t *testing.T) {
	a := appActionApprovalDedupeKey("Gmail", "GMAIL_SEND_EMAIL")
	b := appActionApprovalDedupeKey("gmail", "gmail_send_email")
	if a != b {
		t.Errorf("dedupe key should be case-insensitive: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "app-action:") {
		t.Errorf("unexpected dedupe key shape: %q", a)
	}
}
