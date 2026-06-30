package team

// broker_web_proxy_agent_test.go pins that the same-origin web proxy forwards
// the X-WUPHF-Agent identity header to the broker. The operator's "remove a
// failed app" action sends X-WUPHF-Agent: app-builder; the app-writer gate
// (appWriterAllowed) only honors that identity when it survives the proxy hop.
// Without forwarding, the Remove button 403s.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebUIProxyForwardsAgentHeader(t *testing.T) {
	var gotAgent, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgent = r.Header.Get(agentRateLimitHeader)
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	b := &Broker{token: "broker-secret"}
	h := b.webUIProxyHandler(upstream.URL, "/api")

	// With the header present, it must reach the broker verbatim.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/apps/app_x", nil)
	req.Header.Set(agentRateLimitHeader, "app-builder")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if gotAgent != "app-builder" {
		t.Fatalf("X-WUPHF-Agent not forwarded: got %q, want %q", gotAgent, "app-builder")
	}
	// The proxy still attaches the broker token for transport auth.
	if gotAuth != "Bearer broker-secret" {
		t.Fatalf("Authorization: got %q, want %q", gotAuth, "Bearer broker-secret")
	}

	// Without the header, the proxy must not invent one.
	gotAgent = "sentinel"
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/apps", nil)
	h.ServeHTTP(rec2, req2)
	if gotAgent != "" {
		t.Fatalf("X-WUPHF-Agent should be absent, got %q", gotAgent)
	}
}
