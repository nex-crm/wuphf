package team

// broker_auth.go centralizes bearer-token authentication for broker HTTP
// routes. Every protected route is wrapped via withAuth (or its older
// requireAuth alias). The single point of failure makes auth bugs caught by
// one test surface — TestEveryProtectedRouteRequiresAuth lives in
// broker_workspaces_test.go.
//
// Auth contract:
//
//   - Bearer token is validated against b.token. The token is the same
//     value the broker writes to brokerTokenFilePath on startup; tests
//     read it directly via b.Token().
//   - Either Authorization: Bearer <token> or ?token=<token> are accepted.
//     The query-param form exists for EventSource which cannot set
//     headers; it is intentionally NOT advertised to non-EventSource
//     callers.
//   - Missing/invalid bearer returns 401 with a JSON {"error":"unauthorized"}
//     body to match the existing handler shape — not a bare 401 with no
//     body, which would break existing clients that surface server
//     messages on auth failures.
//
// Excluded from withAuth (intentional, documented):
//
//   - /health, /version: liveness/version endpoints used by external
//     tooling (CI, npx wrapper, npm postinstall) before the bearer token
//     is available. Already exempt today; preserved.
//   - /web-token: returns the bearer token to localhost callers. Loopback
//     RemoteAddr + loopback Host header guards stand in for auth (see
//     handleWebToken). DNS-rebinding-safe by construction.
//   - /events: SSE event stream. Handler validates auth inline (it must
//     keep streaming even if the connection is held open longer than a
//     standard request). This is functionally inside the auth boundary —
//     handleEvents calls requestHasBrokerAuth at the top.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// generateToken returns a cryptographically random hex token.
func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing means the broker cannot issue a secure token.
		panic("crypto/rand.Read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// withAuth wraps a handler to require Bearer-token authentication.
//
// This is the canonical middleware for protected broker routes. The older
// requireAuth method is kept as a thin alias so that the existing 100+ route
// registrations don't all churn in this PR; new routes should call b.withAuth
// directly.
//
// Both names share an implementation, so the auth-route assertion in
// broker_workspaces_test.go covers both call sites.
func (b *Broker) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if actor, ok := b.requestActorFromRequest(r); ok {
			if actor.Kind == requestActorKindHuman && !humanRouteAllowed(r) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"error":"host_only"}`)
				return
			}
			next(w, requestWithActor(r, actor))
			return
		}
		// Honor the documented JSON contract: http.Error sets text/plain
		// and appends a newline, which breaks clients that parse the body.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
	}
}

// requireAuth wraps a handler to enforce Bearer token authentication.
// Accepts token via Authorization header or ?token= query parameter for
// EventSource, which can't set headers.
func (b *Broker) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return b.withAuth(next)
}

func humanRouteAllowed(r *http.Request) bool {
	path := "/" + strings.TrimLeft(r.URL.Path, "/")
	method := r.Method

	if method == http.MethodGet {
		switch {
		case path == "/messages",
			path == "/channels" && !strings.EqualFold(r.URL.Query().Get("type"), "dm"),
			path == "/office-members",
			path == "/channel-members",
			path == "/members",
			path == "/tasks",
			path == "/agent-logs",
			path == "/requests",
			path == "/interview",
			path == "/usage",
			path == "/policies",
			path == "/signals",
			path == "/decisions",
			path == "/watchdogs",
			path == "/actions",
			path == "/scheduler",
			path == "/commands",
			path == "/company",
			path == "/status/local-providers",
			path == "/humans",
			path == "/wiki/read",
			path == "/wiki/search",
			path == "/wiki/lookup",
			path == "/wiki/list",
			path == "/wiki/article",
			path == "/wiki/catalog",
			path == "/wiki/audit",
			path == "/wiki/sections",
			path == "/review/list",
			path == "/entity/facts",
			path == "/entity/briefs",
			path == "/entity/graph",
			path == "/entity/graph/all",
			path == "/playbook/list",
			path == "/playbook/executions",
			path == "/playbook/synthesis-status",
			path == "/learning/search",
			path == "/skills",
			path == "/skills/compile/stats":
			return true
		case strings.HasPrefix(path, "/review/"):
			return true
		}
		return false
	}

	if method == http.MethodPost {
		switch path {
		case "/messages",
			"/reactions",
			"/actions",
			"/requests/answer",
			"/interview/answer",
			"/wiki/write-human":
			return true
		}
	}

	return false
}

// handleWebToken returns the broker token to localhost clients without requiring auth.
// This lets the web UI fetch the token to authenticate subsequent API calls.
//
// DNS rebinding: even though the listener binds 127.0.0.1, an attacker's
// DNS record with a short TTL can point rebind.example.com at 127.0.0.1
// after the browser's origin check passes. Go's default mux routes purely
// on path, so without an explicit Host check the response would flow back
// to the attacker's origin. Validate both RemoteAddr AND Host here.
func (b *Broker) handleWebToken(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemote(r) || !hostHeaderIsLoopback(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": b.token})
}
