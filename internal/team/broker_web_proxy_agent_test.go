package team

// broker_web_proxy_agent_test.go pins how the same-origin web proxy treats the
// X-WUPHF-Agent identity header. The operator's "remove a failed app" action
// sends X-WUPHF-Agent: app-builder; the app-writer gate (appWriterAllowed) only
// honors that identity, so it must reach the broker for a genuine same-origin
// request — and must NOT be spoofable by a cross-site page or relayed for an
// arbitrary agent slug. The proxy already attaches the broker token, so a
// forwarded identity is privileged.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebUIProxyAgentHeaderForwarding(t *testing.T) {
	var gotAgent, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgent = r.Header.Get(agentRateLimitHeader)
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	b := &Broker{token: "broker-secret"}
	h := b.webUIProxyHandler(upstream.URL, "/api")

	call := func(agent, fetchSite string) string {
		gotAgent = "sentinel"
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodDelete, "/api/apps/app_x", nil)
		if agent != "" {
			req.Header.Set(agentRateLimitHeader, agent)
		}
		if fetchSite != "" {
			req.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status: got %d, want 200", rec.Code)
		}
		return gotAgent
	}

	// Genuine same-origin App Builder identity → forwarded; broker token attached.
	if got := call("app-builder", "same-origin"); got != "app-builder" {
		t.Fatalf("same-origin app-builder: got %q, want %q", got, "app-builder")
	}
	if gotAuth != "Bearer broker-secret" {
		t.Fatalf("Authorization: got %q, want %q", gotAuth, "Bearer broker-secret")
	}

	// Cross-site / same-site page (potential CSRF) → identity stripped.
	for _, site := range []string{"cross-site", "same-site", ""} {
		if got := call("app-builder", site); got != "" {
			t.Fatalf("Sec-Fetch-Site=%q must strip identity, got %q", site, got)
		}
	}

	// The proxy never relays an arbitrary agent slug, even same-origin.
	if got := call("ceo", "same-origin"); got != "" {
		t.Fatalf("non-app-builder slug must be dropped, got %q", got)
	}

	// No identity header at all → none invented.
	if got := call("", "same-origin"); got != "" {
		t.Fatalf("absent identity must stay absent, got %q", got)
	}
}
