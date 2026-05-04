package team

import (
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (b *Broker) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exempt liveness and version checks from all rate limiting.
		if isLivenessPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		authenticated := b.requestHasBrokerAuth(r)

		// Authenticated callers bypass the IP-scoped bucket (web UI and trusted
		// tools must not share a bucket with anonymous callers), but authenticated
		// *agent* traffic is still subject to a separate per-agent bucket below.
		if !authenticated {
			retryAfter, limited := b.consumeRateLimit(clientIPFromRequest(r))
			if limited {
				writeRateLimitedResponse(w, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// Authenticated — check the per-agent bucket so a prompt-injected agent
		// cannot loop forever on team_broadcast / team_action_execute. Operator
		// traffic (web UI) does not set X-WUPHF-Agent and is exempt.
		agentSlug := strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
		if agentSlug == "" || isAgentBucketExemptPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		retryAfter, limited := b.consumeAgentRateLimit(agentSlug)
		if limited {
			log.Printf("broker: agent %q tripped per-agent rate limit (%d req / %s) on %s — possible runaway loop", agentSlug, b.agentRateLimitRequests, b.agentRateLimitWindow, r.URL.Path)
			writeRateLimitedResponse(w, retryAfter)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLivenessPath reports whether the request path is a pure liveness or
// version probe that must never be rate-limited (operators need these even
// when the broker is saturated). /web-token is NOT on this list — it
// dispenses the broker bearer and an unthrottled enumeration path would be
// surprising, even though the handler itself is loopback+Host gated.
func isLivenessPath(path string) bool {
	return path == "/health" || path == "/version"
}

// isAgentBucketExemptPath reports whether the path is an open SSE stream or
// otherwise doesn't represent a tool-call-shaped loopable request. These
// connections stay open for a long time rather than spinning on request
// count, so counting them against the agent bucket would be incorrect.
func isAgentBucketExemptPath(path string) bool {
	if path == "/events" {
		return true
	}
	if strings.HasPrefix(path, "/agent-stream/") {
		return true
	}
	if strings.HasPrefix(path, "/terminal/agents/") {
		return true
	}
	return false
}

func writeRateLimitedResponse(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = io.WriteString(w, `{"error":"rate_limited"}`)
}

// rateLimitNow returns the current time for rate-limit calculations.
// Tests may override b.nowFn to advance a synthetic clock without sleeping.
func (b *Broker) rateLimitNow() time.Time {
	if b.nowFn != nil {
		return b.nowFn()
	}
	return time.Now()
}

func (b *Broker) consumeRateLimit(clientIP string) (time.Duration, bool) {
	limit := b.rateLimitRequests
	if limit <= 0 {
		limit = defaultRateLimitRequestsPerWindow
	}
	window := b.rateLimitWindow
	if window <= 0 {
		window = defaultRateLimitWindow
	}

	now := b.rateLimitNow()
	key := rateLimitKey(clientIP)
	cutoff := now.Add(-window)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.rateLimitBuckets == nil {
		b.rateLimitBuckets = make(map[string]ipRateLimitBucket)
	}
	if b.lastRateLimitPrune.IsZero() || now.Sub(b.lastRateLimitPrune) >= window {
		for ip, bucket := range b.rateLimitBuckets {
			bucket.timestamps = pruneRateLimitEntries(bucket.timestamps, cutoff)
			if len(bucket.timestamps) == 0 {
				delete(b.rateLimitBuckets, ip)
				continue
			}
			b.rateLimitBuckets[ip] = bucket
		}
		b.lastRateLimitPrune = now
	}

	bucket := b.rateLimitBuckets[key]
	bucket.timestamps = pruneRateLimitEntries(bucket.timestamps, cutoff)
	if len(bucket.timestamps) >= limit {
		retryAfter := bucket.timestamps[0].Add(window).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		b.rateLimitBuckets[key] = bucket
		return retryAfter, true
	}

	bucket.timestamps = append(bucket.timestamps, now)
	b.rateLimitBuckets[key] = bucket
	return 0, false
}

// consumeAgentRateLimit counts an authenticated request against the per-agent
// bucket keyed by the X-WUPHF-Agent header. It mirrors consumeRateLimit but
// lives in its own bucket so agent traffic cannot starve operator traffic and
// vice versa.
func (b *Broker) consumeAgentRateLimit(agentSlug string) (time.Duration, bool) {
	agentSlug = strings.TrimSpace(agentSlug)
	if agentSlug == "" {
		return 0, false
	}

	limit := b.agentRateLimitRequests
	if limit <= 0 {
		limit = defaultAgentRateLimitRequestsPerWindow
	}
	window := b.agentRateLimitWindow
	if window <= 0 {
		window = defaultAgentRateLimitWindow
	}

	now := b.rateLimitNow()
	cutoff := now.Add(-window)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.agentRateLimitBuckets == nil {
		b.agentRateLimitBuckets = make(map[string]ipRateLimitBucket)
	}
	if b.lastAgentRateLimitPrune.IsZero() || now.Sub(b.lastAgentRateLimitPrune) >= window {
		for slug, bucket := range b.agentRateLimitBuckets {
			bucket.timestamps = pruneRateLimitEntries(bucket.timestamps, cutoff)
			if len(bucket.timestamps) == 0 {
				delete(b.agentRateLimitBuckets, slug)
				continue
			}
			b.agentRateLimitBuckets[slug] = bucket
		}
		b.lastAgentRateLimitPrune = now
	}

	bucket := b.agentRateLimitBuckets[agentSlug]
	bucket.timestamps = pruneRateLimitEntries(bucket.timestamps, cutoff)
	if len(bucket.timestamps) >= limit {
		retryAfter := bucket.timestamps[0].Add(window).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		b.agentRateLimitBuckets[agentSlug] = bucket
		return retryAfter, true
	}

	bucket.timestamps = append(bucket.timestamps, now)
	b.agentRateLimitBuckets[agentSlug] = bucket
	return 0, false
}

func pruneRateLimitEntries(entries []time.Time, cutoff time.Time) []time.Time {
	keepIdx := 0
	for keepIdx < len(entries) && !entries[keepIdx].After(cutoff) {
		keepIdx++
	}
	if keepIdx == 0 {
		return entries
	}
	if keepIdx >= len(entries) {
		return nil
	}
	return entries[keepIdx:]
}

func rateLimitKey(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil && strings.TrimSpace(host) != "" {
		return host
	}
	return remoteAddr
}

func clientIPFromRequest(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	if trustForwardedClientIP(r.RemoteAddr) {
		if forwarded := firstForwardedIP(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			return forwarded
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			return rateLimitKey(realIP)
		}
	}
	return rateLimitKey(r.RemoteAddr)
}

func firstForwardedIP(value string) string {
	for _, part := range strings.Split(value, ",") {
		candidate := rateLimitKey(part)
		if candidate == "" || candidate == "unknown" {
			continue
		}
		if ip := net.ParseIP(candidate); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func trustForwardedClientIP(remoteAddr string) bool {
	host := rateLimitKey(remoteAddr)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func setProxyClientIPHeaders(header http.Header, remoteAddr string) {
	if header == nil {
		return
	}
	if clientIP := rateLimitKey(remoteAddr); clientIP != "unknown" {
		header.Set("X-Forwarded-For", clientIP)
		header.Set("X-Real-IP", clientIP)
	}
}

func (b *Broker) requestAuthToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		return token
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}

func (b *Broker) requestHasBrokerAuth(r *http.Request) bool {
	return b.requestAuthToken(r) == b.token
}

// corsMiddleware adds CORS headers only for the web UI origin.
// If no web UI origins are configured, no CORS headers are set.
//
// Requests with empty or "null" Origin are same-origin or non-browser callers
// (curl, Go tests, CLI tools). They do not need a CORS header to succeed. We
// intentionally do NOT set Access-Control-Allow-Origin: * for them — that
// would let a file:// page or sandboxed iframe make authenticated cross-origin
// reads once it has the Bearer token.
func (b *Broker) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "null" && len(b.webUIOrigins) > 0 {
			for _, allowed := range b.webUIOrigins {
				if origin == allowed {
					w.Header().Set("Access-Control-Allow-Origin", allowed)
					w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
					break
				}
			}
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackRemote reports whether r.RemoteAddr is loopback (127.0.0.0/8, ::1).
// Returns false if RemoteAddr is empty or unparseable — fail closed.
func isLoopbackRemote(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}

// hostHeaderIsLoopback reports whether the HTTP Host header is a loopback
// hostname (localhost, 127.0.0.1, ::1). DNS-rebinding attacks rely on r.Host
// being an attacker-controlled name like rebind.example.com that only
// resolves to 127.0.0.1 at request time; Go's default mux routes by path and
// ignores Host, so routes that sit on 127.0.0.1 will happily serve responses
// to the attacker's origin. Validating Host on sensitive handlers closes this.
//
// The port component is intentionally not validated — the broker and web UI
// run on different ports and dev setups may proxy through 80/443. The
// loopback hostname is the security boundary. Assumes no trusted reverse
// proxy sits in front of the listener; operators adding one must re-evaluate
// (r.Host would then reflect the proxy's upstream, not the browser origin).
func hostHeaderIsLoopback(r *http.Request) bool {
	if r == nil {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// webUIRebindGuard wraps a handler with a DNS-rebinding / cross-origin gate.
// It rejects any request whose RemoteAddr is not loopback or whose Host header
// is not a recognized localhost form. Applied on the web UI mux because that
// mux auto-attaches the broker's Bearer token on forwarded requests; without
// this gate, a malicious website can use DNS rebinding to ride the token.
func webUIRebindGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRemote(r) || !hostHeaderIsLoopback(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
