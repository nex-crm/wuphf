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
	"io"
	"net/http"
)

// withAuth wraps a handler to require Bearer-token authentication.
//
// This is the canonical middleware for protected broker routes. The older
// requireAuth method (above on Broker) is kept as a thin alias so that the
// existing 100+ route registrations don't all churn in this PR; new routes
// should call b.withAuth directly.
//
// Both names share an implementation, so the auth-route assertion in
// broker_workspaces_test.go covers both call sites.
func (b *Broker) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if b.requestHasBrokerAuth(r) {
			next(w, r)
			return
		}
		// Honor the documented JSON contract: http.Error sets text/plain
		// and appends a newline, which breaks clients that parse the body.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
	}
}
