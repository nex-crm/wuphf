package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGeneratePasscodeShape(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 8; i++ {
		p, err := generatePasscode()
		if err != nil {
			t.Fatalf("generatePasscode: %v", err)
		}
		if len(p) != passcodeLength {
			t.Fatalf("len(passcode)=%d want %d", len(p), passcodeLength)
		}
		for _, c := range p {
			if c < '0' || c > '9' {
				t.Fatalf("non-digit %q in %q", c, p)
			}
		}
		seen[p] = true
	}
	if len(seen) < 6 {
		// Two collisions in 8 draws over a 1M space is astronomically
		// unlikely; if we see it, the RNG is broken.
		t.Fatalf("expected near-unique passcodes; got %d distinct in 8 draws: %v", len(seen), seen)
	}
}

func TestConstantTimeCompareMatchesAndDiffers(t *testing.T) {
	if !constantTimeCompare("123456", "123456") {
		t.Fatal("equal strings reported unequal")
	}
	if constantTimeCompare("123456", "123450") {
		t.Fatal("different strings reported equal")
	}
	// Length-mismatch must NOT panic and must return false.
	if constantTimeCompare("123456", "12345") {
		t.Fatal("length-mismatch reported equal")
	}
}

func TestJoinRateLimiterBlocksAfterBurst(t *testing.T) {
	rl := newJoinRateLimiter()
	for i := 0; i < joinRateBurst; i++ {
		if !rl.allow("1.2.3.4") {
			t.Fatalf("burst attempt %d unexpectedly blocked", i+1)
		}
	}
	if rl.allow("1.2.3.4") {
		t.Fatal("attempt past burst was not blocked")
	}
	// A different IP starts with a fresh bucket — don't punish unrelated joiners.
	if !rl.allow("5.6.7.8") {
		t.Fatal("second IP blocked by first IP's bucket")
	}
}

func TestJoinRateLimiterRefillsOverTime(t *testing.T) {
	// Drive an injected clock so the refill semantics are exercised
	// deterministically — CONTRIBUTING.md hard-bans new time.Sleep in
	// tests, and a sleep-loop here would flake under load anyway.
	clock := time.Unix(0, 0)
	rl := &joinRateLimiter{
		buckets:  make(map[string]*joinRateBucket),
		burst:    1,
		interval: 10 * time.Millisecond,
		now:      func() time.Time { return clock },
	}
	if !rl.allow("ip") {
		t.Fatal("first attempt blocked")
	}
	if rl.allow("ip") {
		t.Fatal("second attempt should be blocked before refill")
	}
	clock = clock.Add(15 * time.Millisecond)
	if !rl.allow("ip") {
		t.Fatal("post-refill attempt blocked")
	}
}

func TestExtractJoinSourceIPPrefersCFConnectingIPOnLoopback(t *testing.T) {
	r := httptest.NewRequest("POST", "/join/abc", nil)
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("Cf-Connecting-Ip", "203.0.113.99")
	if got := extractJoinSourceIP(r); got != "203.0.113.99" {
		t.Fatalf("extractJoinSourceIP = %q, want CF-Connecting-Ip on loopback", got)
	}
}

func TestExtractJoinSourceIPIgnoresCFOnNonLoopback(t *testing.T) {
	// A LAN attacker at 10.0.0.5 cannot spoof a CF-Connecting-Ip header to
	// dodge their own bucket. Tunnel mode is loopback-only, so non-loopback
	// requests are network-share traffic where the header is untrusted.
	r := httptest.NewRequest("POST", "/join/abc", nil)
	r.RemoteAddr = "10.0.0.5:54321"
	r.Header.Set("Cf-Connecting-Ip", "203.0.113.99")
	if got := extractJoinSourceIP(r); got != "10.0.0.5" {
		t.Fatalf("extractJoinSourceIP = %q, want RemoteAddr (no header trust)", got)
	}
}

// TestPasscodeRequiredAndInvalidProduceSameUserMessage guards the
// indistinguishability invariant that protects against token enumeration.
// If a future refactor lets one path leak a different message, an attacker
// can sweep the trycloudflare URL space and learn which tokens are live by
// the response copy alone.
func TestPasscodeRequiredAndInvalidProduceSameUserMessage(t *testing.T) {
	// Run an actual /join POST through the share handler with a stub gate
	// that returns each sentinel in turn, then assert the two responses
	// are byte-identical (status, error code, and message). A future
	// refactor that accidentally lets one sentinel surface a different
	// user-facing message would let an attacker fingerprint live tokens
	// by sweeping the trycloudflare URL space — this test is the only
	// thing pinning that invariant mechanically.
	type wireResponse struct {
		Status int
		Body   string
	}
	captureGateResponse := func(t *testing.T, gateErr error) wireResponse {
		t.Helper()
		srv := httptest.NewServer(newShareHandler(shareHandlerConfig{
			BrokerURL: "http://unused.invalid",
			JoinGate: func(_, _ string) error {
				return gateErr
			},
		}))
		t.Cleanup(srv.Close)
		req, err := http.NewRequest(
			http.MethodPost,
			srv.URL+"/join/some-token",
			strings.NewReader(`{"display_name":"Maya"}`),
		)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do request: %v", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return wireResponse{Status: resp.StatusCode, Body: string(body)}
	}

	required := captureGateResponse(t, errJoinPasscodeRequired)
	invalid := captureGateResponse(t, errJoinPasscodeInvalid)

	if required.Status != http.StatusUnauthorized {
		t.Errorf("required: status=%d want 401", required.Status)
	}
	if invalid.Status != http.StatusUnauthorized {
		t.Errorf("invalid: status=%d want 401", invalid.Status)
	}
	if required.Body != invalid.Body {
		t.Fatalf("indistinguishability invariant broken:\nrequired: %s\ninvalid:  %s",
			required.Body, invalid.Body)
	}

	// Sanity: the body actually contains the canonical message + code.
	var decoded struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(strings.NewReader(required.Body)).Decode(&decoded); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if decoded.Error != "passcode_required" {
		t.Errorf("error code = %q, want passcode_required", decoded.Error)
	}
	if decoded.Message != shareJoinPasscodeRequiredMessage {
		t.Errorf("message = %q, want shareJoinPasscodeRequiredMessage", decoded.Message)
	}

	// The internal sentinels must still differ so the audit log can
	// distinguish "unknown token" from "wrong passcode" without leaking
	// to the wire.
	if errJoinPasscodeRequired.Error() == errJoinPasscodeInvalid.Error() {
		t.Fatal("internal sentinels collapsed; audit trail loses fidelity")
	}
}
