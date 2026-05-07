package main

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"math/big"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// joinGateFn returns nil when a (token, suppliedPasscode) pair is allowed to
// proceed to the broker's invite-accept endpoint, or an error whose message
// is safe to render to the unauthenticated joiner. Returning a non-nil
// error MUST NOT distinguish "unknown token" from "wrong passcode" — both
// surface as the same client-visible string so a stranger holding the
// tunnel URL cannot enumerate which tokens were issued.
//
// nil joinGateFn = no passcode gate (network-share path); supplied passcode
// is ignored entirely. The tunnel path always installs a non-nil gate.
type joinGateFn func(token, passcode string) error

// joinAttemptOutcome lets the rate limiter charge a different rate against
// "took a real shot at the gate" vs. "submitted nothing / malformed".
// Currently both count the same — the distinction is here so a future
// refinement (e.g. don't charge the bucket for empty-passcode pre-flights)
// is a one-line change instead of a refactor.
type joinAttemptOutcome int

const (
	joinAttemptAccepted joinAttemptOutcome = iota
	joinAttemptRejected
)

// passcodeLength is the digit count the host UI surfaces and the joiner
// types in. 6 digits = ~20 bits of entropy. Combined with a one-use 24h
// invite token (already crypto-random), the joint search space is well
// out of reach for an online attacker even before the rate limit clamps.
const passcodeLength = 6

// generatePasscode mints a uniformly-distributed numeric passcode of length
// passcodeLength using crypto/rand. Rolling each digit independently keeps
// the implementation obvious and avoids modulo-bias on a 32-bit truncation
// of a wider random integer.
func generatePasscode() (string, error) {
	digits := make([]byte, passcodeLength)
	for i := range digits {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		digits[i] = '0' + byte(n.Int64())
	}
	return string(digits), nil
}

// constantTimeCompare returns true iff a and b are equal as byte slices,
// without leaking the first differing index through timing. Used by
// joinGateFn implementations to compare the supplied passcode against the
// stored one — equal-length compare so an attacker cannot use response
// time to learn the passcode prefix.
func constantTimeCompare(a, b string) bool {
	if len(a) != len(b) {
		// Still run the compare on a same-length pair so the early-return
		// path takes the same wall-clock as the unequal-length path.
		_ = subtle.ConstantTimeCompare([]byte(a), []byte(a))
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// joinRateLimiter is a per-source-IP token bucket that brakes brute-force
// attempts at the unauthenticated /join/<token> POST. Bucket size is
// joinRateBurst; tokens refill at one per joinRateInterval. With 6-digit
// passcodes this caps an attacker's effective guess rate to a few per
// minute per IP — well below the rate they'd need to win the lottery in a
// 24h invite window. A goroutine-free design (lazy GC during allow())
// keeps the surface small and easy to reason about.
type joinRateLimiter struct {
	mu          sync.Mutex
	buckets     map[string]*joinRateBucket
	burst       int
	interval    time.Duration
	lastSweepAt time.Time
	// now is the clock the limiter reads. Defaults to time.Now; tests
	// substitute a controllable closure so refill semantics can be
	// exercised without sleeping (CONTRIBUTING.md hard-bans new time.Sleep
	// in tests, and a sleep-loop here flakes anyway).
	now func() time.Time
}

type joinRateBucket struct {
	tokens   float64
	updated  time.Time
	lastUsed time.Time
}

const (
	joinRateBurst    = 5                // 5 attempts immediately available
	joinRateInterval = 12 * time.Second // refill 1 token / 12s = ~5/min steady state
	joinRateGCAfter  = 10 * time.Minute // drop buckets that haven't been touched recently
)

func newJoinRateLimiter() *joinRateLimiter {
	return &joinRateLimiter{
		buckets:  make(map[string]*joinRateBucket),
		burst:    joinRateBurst,
		interval: joinRateInterval,
		now:      time.Now,
	}
}

// allow returns true if the source IP may attempt a join right now. It
// always charges a token from the bucket on success; on rejection it
// leaves the (already-empty) bucket alone so a flood of attackers cannot
// keep the bucket from refilling. The function is safe for concurrent use.
func (l *joinRateLimiter) allow(ip string) bool {
	if l == nil {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	clock := l.now
	if clock == nil {
		clock = time.Now
	}
	now := clock()
	l.gcLocked(now)
	bucket, ok := l.buckets[ip]
	if !ok {
		bucket = &joinRateBucket{tokens: float64(l.burst), updated: now, lastUsed: now}
		l.buckets[ip] = bucket
	} else {
		elapsed := now.Sub(bucket.updated)
		if elapsed > 0 {
			bucket.tokens += elapsed.Seconds() / l.interval.Seconds()
			if bucket.tokens > float64(l.burst) {
				bucket.tokens = float64(l.burst)
			}
			bucket.updated = now
		}
	}
	bucket.lastUsed = now
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}

// gcLocked drops cold buckets so a long-lived process whose attackers
// rotate IPs faster than they re-attack does not retain state forever.
// Sweeps at most every joinRateGCAfter/2 to keep the hot-path cost ~O(1).
func (l *joinRateLimiter) gcLocked(now time.Time) {
	if now.Sub(l.lastSweepAt) < joinRateGCAfter/2 {
		return
	}
	for ip, b := range l.buckets {
		if now.Sub(b.lastUsed) > joinRateGCAfter {
			delete(l.buckets, ip)
		}
	}
	l.lastSweepAt = now
}

// extractJoinSourceIP picks the right "remote IP" for the rate limiter
// when the share HTTP server runs behind cloudflared. cloudflared injects
// Cf-Connecting-Ip with the real visitor address; r.RemoteAddr by then is
// the loopback connection from cloudflared itself, which would put every
// attacker into one bucket and let them DoS legitimate joiners.
//
// The header is trusted ONLY when the immediate r.RemoteAddr is loopback,
// which is the case when running the tunnel-mode share server (bound to
// 127.0.0.1) but NOT when the network-share server is bound to a
// Tailscale address. Outside loopback we fall back to r.RemoteAddr so a
// LAN attacker cannot spoof the header.
func extractJoinSourceIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if isLoopbackHost(host) {
		if cf := strings.TrimSpace(r.Header.Get("Cf-Connecting-Ip")); cf != "" {
			return cf
		}
	}
	return host
}

func isLoopbackHost(host string) bool {
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// errJoinPasscodeRequired and errJoinPasscodeInvalid are the canonical gate
// failures. The two sentinels are kept distinct so callers (audit log,
// future telemetry) can distinguish the cases internally, but they collapse
// to the same user-visible message via shareJoinPasscodeRequiredMessage so
// an attacker sweeping for "is this token live without the passcode" sees
// identical responses to "valid token + missing passcode" and "valid token
// + wrong passcode" — see the comment on joinGateFn.
var (
	errJoinPasscodeRequired = errors.New("passcode required")
	errJoinPasscodeInvalid  = errors.New("passcode invalid")
)

// shareJoinPasscodeRequiredMessage is the only string the joiner sees for
// either gate failure. Lives next to the errors so a future copy tweak
// keeps the indistinguishability invariant in one place.
const shareJoinPasscodeRequiredMessage = "This invite needs a passcode. Ask the host to read it to you."
