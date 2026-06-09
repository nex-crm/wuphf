package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRequireTeamMemberApprovalBypassesWhenUnsafe pins the one escape hatch:
// an operator who launched with --unsafe (WUPHF_UNSAFE=1) has opted out of
// every approval gate, so member creation proceeds without a card. This
// returns immediately and never touches the broker.
func TestRequireTeamMemberApprovalBypassesWhenUnsafe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_UNSAFE", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := requireTeamMemberApproval(ctx, "ceo", TeamMemberArgs{
		Slug: "growth", Name: "Growth", Role: "growth lead",
	}); err != nil {
		t.Fatalf("WUPHF_UNSAFE=1 must bypass the member-approval gate, got err=%v", err)
	}
}

// TestRequireTeamMemberApprovalProceedsOnApprove proves the happy path: the
// gate raises a blocking approval, polls, and returns nil once the human
// approves — letting handleTeamMember go on to create the member.
func TestRequireTeamMemberApprovalProceedsOnApprove(t *testing.T) {
	if testing.Short() {
		t.Skip("relies on the 1.5s poll tick; skip under -short")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_UNSAFE", "")

	var sawRequest bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/requests":
			sawRequest = true
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "req-member-approve"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/interview/answer"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "answered",
				"answered": map[string]any{
					"choice_id":   "approve",
					"answered_at": "2026-06-09T12:00:00Z",
				},
			})
		default:
			http.Error(w, "stub: unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	withBrokerURL(t, srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := requireTeamMemberApproval(ctx, "ceo", TeamMemberArgs{
		Slug: "growth", Name: "Growth", Role: "growth lead",
	}); err != nil {
		t.Fatalf("approved request must return nil, got err=%v", err)
	}
	if !sawRequest {
		t.Fatal("expected the gate to POST an approval request to /requests")
	}
}

// TestRequireTeamMemberApprovalBlocksOnReject proves the hard gate: when the
// human declines, the gate returns an error that names the declined slug, so
// the CEO routes to reusing an existing specialist instead of creating one.
func TestRequireTeamMemberApprovalBlocksOnReject(t *testing.T) {
	if testing.Short() {
		t.Skip("relies on the 1.5s poll tick; skip under -short")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_UNSAFE", "")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/requests":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "req-member-reject"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/interview/answer"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "answered",
				"answered": map[string]any{
					"choice_id":   "reject",
					"choice_text": "we already have growth covered",
					"answered_at": "2026-06-09T12:00:00Z",
				},
			})
		default:
			http.Error(w, "stub: unhandled "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()
	withBrokerURL(t, srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := requireTeamMemberApproval(ctx, "ceo", TeamMemberArgs{
		Slug: "growth", Name: "Growth", Role: "growth lead",
	})
	if err == nil {
		t.Fatal("rejected request must return an error so the CEO reuses an existing specialist")
	}
	if !strings.Contains(err.Error(), "growth") {
		t.Fatalf("error should name the declined slug so the agent knows what was blocked, got: %v", err)
	}
}
