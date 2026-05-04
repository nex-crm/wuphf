package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/buildinfo"
	"github.com/nex-crm/wuphf/internal/config"
)

// TestPruneRateLimitEntries_RemovesAllAtCutoff pins the boundary case
// previously uncovered: when every entry is at or before the cutoff,
// the prune must return nil so the caller's `len(...) == 0` cleanup
// branch fires and the bucket gets garbage-collected.
func TestPruneRateLimitEntries_RemovesAllAtCutoff(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cutoff := now
	entries := []time.Time{
		now.Add(-3 * time.Second),
		now.Add(-2 * time.Second),
		now.Add(-1 * time.Second),
	}
	got := pruneRateLimitEntries(entries, cutoff)
	if got != nil {
		t.Fatalf("expected nil when all entries at-or-before cutoff, got %v", got)
	}

	// Mixed: only some after cutoff — keep the trailing slice.
	mixed := []time.Time{
		now.Add(-3 * time.Second),
		now.Add(1 * time.Second),
		now.Add(2 * time.Second),
	}
	gotMixed := pruneRateLimitEntries(mixed, cutoff)
	if len(gotMixed) != 2 {
		t.Fatalf("expected 2 entries after cutoff, got %d", len(gotMixed))
	}
	if !gotMixed[0].Equal(now.Add(1 * time.Second)) {
		t.Fatalf("unexpected first entry: %v", gotMixed[0])
	}

	// All after cutoff — return slice unchanged.
	allAfter := []time.Time{now.Add(time.Second), now.Add(2 * time.Second)}
	gotAfter := pruneRateLimitEntries(allAfter, cutoff)
	if len(gotAfter) != 2 {
		t.Fatalf("expected unchanged length, got %d", len(gotAfter))
	}
}

// TestExternalWorkflowRetryAfter_ParsesRFC3339FromErrorString pins the
// regex+RFC3339 contract: provider errors that include "retry after
// <RFC3339>" let the broker schedule a deferred retry; without a match
// the function must return false so callers fall back to default
// backoff.
func TestExternalWorkflowRetryAfter_ParsesRFC3339FromErrorString(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	t.Run("nil error", func(t *testing.T) {
		got, ok := externalWorkflowRetryAfter(nil, now)
		if ok || !got.IsZero() {
			t.Fatalf("nil error: want zero/false, got (%v, %v)", got, ok)
		}
	})

	t.Run("no match", func(t *testing.T) {
		_, ok := externalWorkflowRetryAfter(errors.New("plain old failure"), now)
		if ok {
			t.Fatal("expected false for unmatchable error")
		}
	})

	t.Run("future retryAt", func(t *testing.T) {
		future := now.Add(15 * time.Minute).Format(time.RFC3339Nano)
		got, ok := externalWorkflowRetryAfter(fmt.Errorf("rate limited; retry after %s", future), now)
		if !ok {
			t.Fatal("expected ok=true for matched RFC3339")
		}
		want, _ := time.Parse(time.RFC3339Nano, future)
		if !got.Equal(want) {
			t.Fatalf("retryAt: want %v, got %v", want, got)
		}
	})

	t.Run("past retryAt clamps to now", func(t *testing.T) {
		past := now.Add(-time.Hour).Format(time.RFC3339Nano)
		got, ok := externalWorkflowRetryAfter(fmt.Errorf("Retry After %s", past), now)
		if !ok {
			t.Fatal("expected ok=true even for past time")
		}
		if !got.Equal(now) {
			t.Fatalf("past retryAt should clamp to now, got %v", got)
		}
	})
}

// TestRequestHasBrokerAuth_AcceptsBearerAndQueryToken pins both auth-detection
// paths: a Bearer header AND a ?token= query param both must be honored, and
// any other shape (including empty, wrong prefix, wrong token) must be
// rejected.
func TestRequestHasBrokerAuth_AcceptsBearerAndQueryToken(t *testing.T) {
	b := newTestBroker(t)
	tok := b.Token()

	mk := func(headerVal, queryVal string) *http.Request {
		target := "/messages"
		if queryVal != "" {
			target += "?token=" + queryVal
		}
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if headerVal != "" {
			req.Header.Set("Authorization", headerVal)
		}
		return req
	}

	cases := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{"bearer header valid", mk("Bearer "+tok, ""), true},
		{"query token valid", mk("", tok), true},
		{"both valid", mk("Bearer "+tok, tok), true},
		{"empty", mk("", ""), false},
		{"wrong prefix", mk("Token "+tok, ""), false},
		{"bearer wrong token", mk("Bearer not-the-token", ""), false},
		{"query wrong token", mk("", "not-the-token"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.requestHasBrokerAuth(tc.req); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBrokerAuthRejectsUnauthenticated(t *testing.T) {
	b := newTestBroker(t)
	b.runtimeProvider = "codex"
	t.Setenv("WUPHF_MEMORY_BACKEND", config.MemoryBackendGBrain)
	t.Setenv("WUPHF_OPENAI_API_KEY", "sk-test-openai")
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())

	// Health should work without auth
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("expected 200 on /health, got %d", resp.StatusCode)
	}
	var health struct {
		SessionMode         string `json:"session_mode"`
		OneOnOneAgent       string `json:"one_on_one_agent"`
		Provider            string `json:"provider"`
		MemoryBackend       string `json:"memory_backend"`
		MemoryBackendActive string `json:"memory_backend_active"`
		NexConnected        bool   `json:"nex_connected"`
		Build               struct {
			Version        string `json:"version"`
			BuildTimestamp string `json:"build_timestamp"`
		} `json:"build"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		resp.Body.Close()
		t.Fatalf("decode health: %v", err)
	}
	resp.Body.Close()
	if health.SessionMode != SessionModeOffice {
		t.Fatalf("expected health to report office mode, got %q", health.SessionMode)
	}
	if health.OneOnOneAgent != DefaultOneOnOneAgent {
		t.Fatalf("expected health to report default 1o1 agent %q, got %q", DefaultOneOnOneAgent, health.OneOnOneAgent)
	}
	if health.Provider != "codex" {
		t.Fatalf("expected health to report provider codex, got %q", health.Provider)
	}
	if health.MemoryBackend != config.MemoryBackendGBrain {
		t.Fatalf("expected health to report selected memory backend gbrain, got %q", health.MemoryBackend)
	}
	if health.MemoryBackendActive != config.MemoryBackendNone {
		t.Fatalf("expected inactive gbrain backend without CLI installed, got %q", health.MemoryBackendActive)
	}
	if health.NexConnected {
		t.Fatal("expected nex_connected=false when gbrain is selected")
	}
	wantBuild := buildinfo.Current()
	if health.Build.Version != wantBuild.Version {
		t.Fatalf("expected health build version %q, got %q", wantBuild.Version, health.Build.Version)
	}
	if health.Build.BuildTimestamp != wantBuild.BuildTimestamp {
		t.Fatalf("expected health build timestamp %q, got %q", wantBuild.BuildTimestamp, health.Build.BuildTimestamp)
	}

	resp, err = http.Get(base + "/version")
	if err != nil {
		t.Fatalf("version request failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("expected 200 on /version, got %d", resp.StatusCode)
	}
	var version struct {
		Version        string `json:"version"`
		BuildTimestamp string `json:"build_timestamp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&version); err != nil {
		resp.Body.Close()
		t.Fatalf("decode version: %v", err)
	}
	resp.Body.Close()
	if version.Version != wantBuild.Version {
		t.Fatalf("expected /version version %q, got %q", wantBuild.Version, version.Version)
	}
	if version.BuildTimestamp != wantBuild.BuildTimestamp {
		t.Fatalf("expected /version build timestamp %q, got %q", wantBuild.BuildTimestamp, version.BuildTimestamp)
	}

	// Messages without auth should be rejected
	resp, err = http.Get(base + "/messages")
	if err != nil {
		t.Fatalf("messages request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /messages without auth, got %d", resp.StatusCode)
	}

	// Messages with correct token should succeed
	req, _ := http.NewRequest("GET", base+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 on /messages with auth, got %d: %s", resp.StatusCode, body)
	}

	// Messages with wrong token should be rejected
	req, _ = http.NewRequest("GET", base+"/messages", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bad token request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /messages with wrong token, got %d", resp.StatusCode)
	}
}

func TestBrokerRateLimitsRequestsPerIP(t *testing.T) {
	b := newTestBroker(t)
	b.rateLimitRequests = 100
	b.rateLimitWindow = time.Second

	// Synthetic clock: starts at a fixed point; advance() jumps it past the window
	// so the test never sleeps for real.
	fakeClock := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	b.nowFn = func() time.Time { return fakeClock }
	advance := func(d time.Duration) { fakeClock = fakeClock.Add(d) }

	mux := http.NewServeMux()
	mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := b.corsMiddleware(b.rateLimitMiddleware(mux))
	doRequest := func(forwardedFor string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	for i := 0; i < 100; i++ {
		resp := doRequest("")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected request %d to succeed, got %d", i+1, resp.StatusCode)
		}
	}

	resp := doRequest("")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 101st request to be rate limited, got %d", resp.StatusCode)
	}
	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter == "" {
		t.Fatal("expected Retry-After header on rate-limited response")
	}
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil || seconds < 1 || seconds > 2 {
		t.Fatalf("expected sane Retry-After seconds, got %q", retryAfter)
	}

	// Advance the synthetic clock past the window — no real sleep needed.
	advance(b.rateLimitWindow + time.Millisecond)

	resp = doRequest("")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected request after rolling window expiry to succeed, got %d", resp.StatusCode)
	}
}

func TestBrokerAuthenticatedRequestsBypassRateLimit(t *testing.T) {
	b := newTestBroker(t)
	b.rateLimitRequests = 1
	b.rateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func(setAuthHeader bool, useQueryToken bool) *http.Response {
		target := "/messages"
		if useQueryToken {
			target += "?token=" + b.Token()
		}
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		if setAuthHeader {
			req.Header.Set("Authorization", "Bearer "+b.Token())
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	resp := doRequest(true, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected authenticated header request to bypass limiter, got %d", resp.StatusCode)
	}

	resp = doRequest(true, false)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected repeated authenticated header request to bypass limiter, got %d", resp.StatusCode)
	}

	resp = doRequest(false, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected authenticated query-token request to bypass limiter, got %d", resp.StatusCode)
	}

	resp = doRequest(false, true)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected repeated authenticated query-token request to bypass limiter, got %d", resp.StatusCode)
	}
}

func TestBrokerRateLimitsUsingForwardedClientIP(t *testing.T) {
	b := newTestBroker(t)
	b.rateLimitRequests = 1
	b.rateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func(remoteAddr, forwardedFor string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		req.RemoteAddr = remoteAddr
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	resp := doRequest("127.0.0.1:1111", "203.0.113.10")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected first forwarded request to succeed, got %d", resp.StatusCode)
	}

	resp = doRequest("127.0.0.1:2222", "203.0.113.10")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected repeated forwarded client IP to be limited, got %d", resp.StatusCode)
	}

	resp = doRequest("127.0.0.1:3333", "203.0.113.11")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected distinct forwarded client IP to get its own bucket, got %d", resp.StatusCode)
	}
}

func TestBrokerIgnoresForwardedClientIPFromNonLoopbackPeer(t *testing.T) {
	b := newTestBroker(t)
	b.rateLimitRequests = 1
	b.rateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func(remoteAddr, forwardedFor string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		req.RemoteAddr = remoteAddr
		if forwardedFor != "" {
			req.Header.Set("X-Forwarded-For", forwardedFor)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	resp := doRequest("198.51.100.8:1111", "203.0.113.10")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected first request to succeed, got %d", resp.StatusCode)
	}

	resp = doRequest("198.51.100.8:2222", "203.0.113.11")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected non-loopback peer to be bucketed by remote addr, got %d", resp.StatusCode)
	}
}

// TestBrokerAgentRateLimitTripsOnRunawayLoop verifies that a prompt-injected
// agent that loops on tool calls eventually gets 429'd even though it holds a
// valid Bearer token. The IP bucket alone exempts token-holders, so this is
// the containment for runaway agent cost.
func TestBrokerAgentRateLimitTripsOnRunawayLoop(t *testing.T) {
	b := newTestBroker(t)
	b.agentRateLimitRequests = 5
	b.agentRateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func(slug string) *http.Response {
		req := httptest.NewRequest(http.MethodPost, "/actions/execute", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("Authorization", "Bearer "+b.Token())
		if slug != "" {
			req.Header.Set("X-WUPHF-Agent", slug)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	for i := 0; i < 5; i++ {
		resp := doRequest("ceo")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected request %d within agent budget to succeed, got %d", i+1, resp.StatusCode)
		}
	}

	resp := doRequest("ceo")
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 6th request to trip per-agent bucket, got %d", resp.StatusCode)
	}
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter == "" {
		t.Fatal("expected Retry-After header on rate-limited response")
	}

	// A different agent slug gets its own bucket.
	resp = doRequest("engineer")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected distinct agent slug to get its own bucket, got %d", resp.StatusCode)
	}
}

// TestBrokerOperatorTrafficBypassesAgentRateLimit verifies the web UI, which
// authenticates with the broker token but does not identify itself as any
// particular agent, is not blocked by the per-agent bucket. If this breaks the
// operator loses access to their office whenever one agent loops.
func TestBrokerOperatorTrafficBypassesAgentRateLimit(t *testing.T) {
	b := newTestBroker(t)
	b.agentRateLimitRequests = 1
	b.agentRateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func() *http.Response {
		req := httptest.NewRequest(http.MethodGet, "/messages", nil)
		req.RemoteAddr = "127.0.0.1:5555"
		req.Header.Set("Authorization", "Bearer "+b.Token())
		// Deliberately no X-WUPHF-Agent — this is operator traffic.
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}
	for i := 0; i < 10; i++ {
		resp := doRequest()
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected operator request %d to bypass agent limiter, got %d", i+1, resp.StatusCode)
		}
	}
}

// TestBrokerAgentRateLimitExemptsSSEPaths verifies long-lived SSE streams are
// not counted against the per-agent bucket. They do not represent loopable
// tool calls — a single subscribe holds the connection open for minutes.
func TestBrokerAgentRateLimitExemptsSSEPaths(t *testing.T) {
	b := newTestBroker(t)
	b.agentRateLimitRequests = 2
	b.agentRateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	doRequest := func(path string) *http.Response {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "127.0.0.1:6666"
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("X-WUPHF-Agent", "ceo")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result()
	}

	for i := 0; i < 10; i++ {
		resp := doRequest("/events")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected /events subscribe %d to bypass agent limiter, got %d", i+1, resp.StatusCode)
		}
		resp = doRequest("/agent-stream/ceo")
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected /agent-stream subscribe %d to bypass agent limiter, got %d", i+1, resp.StatusCode)
		}
	}
}

func TestBrokerAgentRateLimitCountsTerminalWebsocketPath(t *testing.T) {
	b := newTestBroker(t)
	b.agentRateLimitRequests = 2
	b.agentRateLimitWindow = time.Second
	handler := b.rateLimitMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/terminal/agents/ceo", nil)
		req.RemoteAddr = "127.0.0.1:6666"
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("X-WUPHF-Agent", "ceo")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		resp := rec.Result()
		resp.Body.Close()

		if i < 2 && resp.StatusCode != http.StatusOK {
			t.Fatalf("terminal request %d status = %d, want 200", i+1, resp.StatusCode)
		}
		if i == 2 && resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("terminal request %d status = %d, want 429", i+1, resp.StatusCode)
		}
	}
}

// TestIsLoopbackRemote verifies the RemoteAddr-side half of the DNS-rebinding
// guard. Empty and unparseable addresses must fail closed — otherwise a
// test-only path, or a listener that exposes synthetic RemoteAddr, would
// silently open the gate.
func TestIsLoopbackRemote(t *testing.T) {
	cases := []struct {
		name       string
		remoteAddr string
		want       bool
	}{
		{"ipv4 loopback with port", "127.0.0.1:1234", true},
		{"ipv6 loopback with port", "[::1]:1234", true},
		{"localhost hostname", "localhost:4444", true},
		{"ipv4 loopback high octet", "127.255.255.255:1", true},
		{"ipv4 non-loopback", "10.0.0.5:1234", false},
		{"ipv4 external", "203.0.113.9:80", false},
		{"empty remote addr", "", false},
		{"malformed", "not-an-address", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remoteAddr
			if got := isLoopbackRemote(req); got != tc.want {
				t.Fatalf("isLoopbackRemote(%q) = %v, want %v", tc.remoteAddr, got, tc.want)
			}
		})
	}
	if got := isLoopbackRemote(nil); got {
		t.Fatal("isLoopbackRemote(nil) = true, want false (must fail closed)")
	}
}

// TestHostHeaderIsLoopback verifies the Host-side half of the DNS-rebinding
// guard. An attacker-controlled name that resolves to 127.0.0.1 at request
// time must be rejected based on the Host header alone.
func TestHostHeaderIsLoopback(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"localhost:7890", true},
		{"127.0.0.1:7890", true},
		{"[::1]:7890", true},
		{"localhost", true},
		{"127.0.0.1", true},
		{"::1", true},
		{"evil.example.com", false},
		{"evil.example.com:7890", false},
		{"rebind.attacker.test:7900", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tc.host
			if got := hostHeaderIsLoopback(req); got != tc.want {
				t.Fatalf("hostHeaderIsLoopback(Host=%q) = %v, want %v", tc.host, got, tc.want)
			}
		})
	}
	if got := hostHeaderIsLoopback(nil); got {
		t.Fatal("hostHeaderIsLoopback(nil) = true, want false (must fail closed)")
	}
}

// TestWebUIRebindGuard is the integrated test for the guard composed of
// isLoopbackRemote AND hostHeaderIsLoopback. Both must pass for the request
// to reach the next handler. Either failing → 403.
func TestWebUIRebindGuard(t *testing.T) {
	guarded := webUIRebindGuard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("reached"))
	}))

	cases := []struct {
		name       string
		remoteAddr string
		host       string
		wantStatus int
	}{
		{"loopback remote + loopback host", "127.0.0.1:5000", "localhost:7900", http.StatusOK},
		{"loopback remote + evil host (rebind attempt)", "127.0.0.1:5000", "evil.example.com:7900", http.StatusForbidden},
		{"non-loopback remote + loopback host", "203.0.113.9:80", "127.0.0.1:7900", http.StatusForbidden},
		{"non-loopback remote + evil host", "203.0.113.9:80", "evil.example.com:7900", http.StatusForbidden},
		{"empty remote + loopback host", "", "127.0.0.1:7900", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api-token", nil)
			req.RemoteAddr = tc.remoteAddr
			req.Host = tc.host
			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d; body=%q", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

// TestCORSMiddlewareDropsNullOriginWildcard verifies the fix for the CSO
// finding: Access-Control-Allow-Origin: * was previously returned for empty
// or "null" Origin headers. A file:// page could open on the operator's
// laptop and make authenticated cross-origin reads once it had the token.
// The new contract: no CORS header unless the Origin exactly matches the
// configured web UI origin.
func TestCORSMiddlewareDropsNullOriginWildcard(t *testing.T) {
	b := newTestBroker(t)
	b.webUIOrigins = []string{"http://localhost:7900", "http://127.0.0.1:7900"}
	handler := b.corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name     string
		origin   string
		wantACAO string // empty means header must not be set
	}{
		{"null origin", "null", ""},
		{"empty origin", "", ""},
		{"allowed localhost origin", "http://localhost:7900", "http://localhost:7900"},
		{"allowed loopback origin", "http://127.0.0.1:7900", "http://127.0.0.1:7900"},
		{"disallowed origin", "http://evil.example.com", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			got := rec.Header().Get("Access-Control-Allow-Origin")
			if got != tc.wantACAO {
				t.Fatalf("Access-Control-Allow-Origin=%q, want %q", got, tc.wantACAO)
			}
		})
	}
}

func TestSetProxyClientIPHeaders(t *testing.T) {
	headers := make(http.Header)
	setProxyClientIPHeaders(headers, "203.0.113.44:5678")
	if got := headers.Get("X-Forwarded-For"); got != "203.0.113.44" {
		t.Fatalf("expected X-Forwarded-For to preserve remote IP, got %q", got)
	}
	if got := headers.Get("X-Real-IP"); got != "203.0.113.44" {
		t.Fatalf("expected X-Real-IP to preserve remote IP, got %q", got)
	}
}
