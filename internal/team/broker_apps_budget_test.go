package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestAppsAIBudget_PerApp proves the budget is keyed PER-APP: one app burning
// its ai() allowance must not throttle a different app. This is the property that
// makes a single misbehaving app (e.g. one re-summarizing on every tab refocus)
// contained rather than starving the whole workspace.
func TestAppsAIBudget_PerApp(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		return "ok", nil
	})

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	// Drain app A's per-minute budget.
	bodyA, _ := json.Marshal(map[string]any{"prompt": "go", "app_id": "app_aaaaaaaaaaaaaaaa"})
	limitedA := false
	for i := 0; i < appAIRateLimit+2; i++ {
		if code, _ := postAppsBridgeJSON(t, base+"/apps/ai", b.Token(), bodyA); code == http.StatusTooManyRequests {
			limitedA = true
		}
	}
	if !limitedA {
		t.Fatalf("app A should be rate-limited after %d ai() calls", appAIRateLimit)
	}

	// App B has its OWN bucket — app A draining its budget must not affect it.
	bodyB, _ := json.Marshal(map[string]any{"prompt": "go", "app_id": "app_bbbbbbbbbbbbbbbb"})
	if code, _ := postAppsBridgeJSON(t, base+"/apps/ai", b.Token(), bodyB); code != http.StatusOK {
		t.Fatalf("app B must not be throttled by app A's usage, got %d", code)
	}
}

// TestAppsIntegrationRead_Budget bounds integration READS per app: past the
// per-minute cap a read returns a clean {error:"rate_limited"} product state
// (HTTP 200) instead of hammering the upstream provider. The budget is charged
// BEFORE the provider is touched, so the cap holds regardless of connection state.
func TestAppsIntegrationRead_Budget(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	// Keep the read path provider-agnostic: the budget fires before connection
	// resolution, so Composio need not be configured for this test.
	t.Setenv("WUPHF_COMPOSIO_API_KEY", "")

	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()
	base := fmt.Sprintf("http://%s", b.Addr())

	read, _ := json.Marshal(map[string]any{
		"platform": "gmail",
		"action":   "GMAIL_FETCH_EMAILS", // a read action (reaches the read budget)
		"app_id":   "app_cccccccccccccccc",
	})
	rateLimited := false
	for i := 0; i < appIntegrationReadLimit+2; i++ {
		code, out := postAppsBridgeJSON(t, base+"/apps/integrations/call", b.Token(), read)
		if code != http.StatusOK {
			t.Fatalf("read call %d returned HTTP %d, want 200", i, code)
		}
		if e, _ := out["error"].(string); e == "rate_limited" {
			rateLimited = true
		}
	}
	if !rateLimited {
		t.Fatalf("expected a rate_limited read after %d integration reads", appIntegrationReadLimit)
	}
}

// TestRollingLimit_Windowing deterministically pins the shared budget primitive
// (used for both the per-minute and per-day caps): peek never records, capacity
// trips the limit, and the window resets once it elapses.
func TestRollingLimit_Windowing(t *testing.T) {
	buckets := map[string]ipRateLimitBucket{}
	now := time.Unix(1_000_000, 0)
	const limit = 3
	window := time.Minute

	for i := 0; i < limit; i++ {
		if _, limited := peekRollingLimit(buckets, "k", limit, window, now); limited {
			t.Fatalf("peek limited too early at i=%d", i)
		}
		recordRollingHit(buckets, "k", now)
	}
	// At capacity → limited, with a positive retry-after.
	retry, limited := peekRollingLimit(buckets, "k", limit, window, now)
	if !limited {
		t.Fatal("expected limited at capacity")
	}
	if retry <= 0 {
		t.Fatalf("expected a positive retry-after, got %s", retry)
	}
	// A separate key is independent.
	if _, limited := peekRollingLimit(buckets, "other", limit, window, now); limited {
		t.Fatal("a different key must have its own budget")
	}
	// Once the window elapses the old hits prune away AND the now-empty key is
	// swept from the map (no unbounded growth from idle keys).
	if _, limited := peekRollingLimit(buckets, "k", limit, window, now.Add(2*window)); limited {
		t.Fatal("expected the window to reset after it elapses")
	}
	if _, ok := buckets["k"]; ok {
		t.Fatal("an idle key must be swept from the bucket map, not resurrected")
	}
}

// TestAppBudgetKey keys on the host-supplied app id when present and well-formed
// (per-app budgets), falling back to the actor namespace for a blank OR malformed
// id so a forged key cannot spray arbitrary buckets.
func TestAppBudgetKey(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "/apps/ai", nil)
	const valid = "app_deadbeefdeadbeef"
	if got := appBudgetKey(valid, req); got != "app:"+valid {
		t.Errorf("valid app id key = %q, want app:%s", got, valid)
	}
	if got := appBudgetKey("  ", req); got != "actor:app" {
		t.Errorf("blank app id should fall back to the actor namespace, got %q", got)
	}
	// A malformed id (not app_<16 hex>) must NOT become a budget key.
	if got := appBudgetKey("../etc/passwd", req); got != "actor:app" {
		t.Errorf("malformed app id should fall back to the actor namespace, got %q", got)
	}
}
