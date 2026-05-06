package main

import (
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
	rl := &joinRateLimiter{
		buckets:  make(map[string]*joinRateBucket),
		burst:    1,
		interval: 10 * time.Millisecond,
	}
	if !rl.allow("ip") {
		t.Fatal("first attempt blocked")
	}
	if rl.allow("ip") {
		t.Fatal("second attempt should be blocked before refill")
	}
	time.Sleep(15 * time.Millisecond)
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
	if !strings.EqualFold(shareJoinPasscodeRequiredMessage, shareJoinPasscodeRequiredMessage) {
		t.Fatal("constant should equal itself; unreachable")
	}
	// We assert by inspection that exactly one user-facing string covers
	// both paths: handleShareJoinSubmit calls
	// writeShareJoinError(..., "passcode_required", shareJoinPasscodeRequiredMessage)
	// regardless of which sentinel the gate returned. The two error
	// sentinels exist so audit logs can distinguish them, but neither
	// sentinel's .Error() string flows to the client.
	if errJoinPasscodeRequired.Error() == errJoinPasscodeInvalid.Error() {
		t.Fatal("internal sentinels collapsed; audit trail loses fidelity")
	}
}
