package team

// HTTP-routing coverage for the operation bootstrap endpoints. The
// existing operation_matrix_test.go exercises the handler by calling
// it directly (good enough for the body's logic) but never goes
// through the broker mux. After the Track B refactor changed
// handleOperationBootstrapPackage from a *Broker method to a free
// function, the mux registration in broker.go is the seam that could
// silently regress without any direct-call test noticing.
//
// This test boots a real broker on an ephemeral port and hits each
// route via http.Client. If a future change drops the route, swaps
// the handler reference, or breaks requireAuth wiring, this fails.

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
)

func TestOperationBootstrapPackageRoutesReachHandler(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "broker-state.json")
	b := NewBrokerAt(statePath)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}
	defer b.Stop()

	// Both routes are registered to the same handler in broker.go's mux.
	// Cover both so a reorganization of the registration block doesn't
	// drop one without us noticing.
	for _, path := range []string{"/operations/bootstrap-package", "/studio/bootstrap-package"} {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "http://"+b.Addr()+path, nil)
			if err != nil {
				t.Fatalf("NewRequest %s: %v", path, err)
			}
			req.Header.Set("Authorization", "Bearer "+b.Token())
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s: status %d", path, resp.StatusCode)
			}
			// Don't assert on package shape — that's covered by
			// operation_matrix_test.go and the bootstrap package's
			// content is non-deterministic when no blueprint is
			// configured. Just confirm the handler returned something
			// JSON-shaped with the expected outer key.
			var body struct {
				Package json.RawMessage `json:"package"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			if len(body.Package) == 0 {
				t.Fatalf("GET %s: empty package field in response", path)
			}
		})
	}
}

func TestOperationBootstrapPackageRouteRejectsUnauth(t *testing.T) {
	// requireAuth wraps the handler at registration time. If a future
	// change accidentally registers the bare handler without the
	// requireAuth wrapper, every operation route silently becomes
	// unauthenticated. This catches that immediately.
	statePath := filepath.Join(t.TempDir(), "broker-state.json")
	b := NewBrokerAt(statePath)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}
	defer b.Stop()

	for _, path := range []string{"/operations/bootstrap-package", "/studio/bootstrap-package"} {
		t.Run(path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, "http://"+b.Addr()+path, nil)
			if err != nil {
				t.Fatalf("NewRequest %s: %v", path, err)
			}
			// No Authorization header — must be rejected.
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("GET %s without token: expected 401, got %d", path, resp.StatusCode)
			}
		})
	}
}
