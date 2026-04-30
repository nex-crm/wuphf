package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func TestReviewToolsRegisteredOnlyInMarkdownBackend(t *testing.T) {
	reviewTools := []string{"team_reviews", "team_review"}
	cases := []struct {
		name     string
		backend  string
		mustHave bool
	}{
		{"markdown registers review tools", "markdown", true},
		{"nex excludes review tools", "nex", false},
		{"gbrain excludes review tools", "gbrain", false},
		{"none excludes review tools", "none", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WUPHF_MEMORY_BACKEND", tc.backend)
			names := listRegisteredTools(t, "general", false)
			for _, name := range reviewTools {
				if tc.mustHave && !slices.Contains(names, name) {
					t.Errorf("backend=%s expected %q registered; got %v", tc.backend, name, names)
				}
				if !tc.mustHave && slices.Contains(names, name) {
					t.Errorf("backend=%s expected %q NOT registered; got %v", tc.backend, name, names)
				}
			}
		})
	}
}

func TestHandleTeamReviewsDefaultsToCallerQueue(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"reviews": []map[string]any{
				{"id": "rvw-1", "reviewer_slug": "ceo", "state": "pending"},
			},
		})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	res, _, err := handleTeamReviews(context.Background(), nil, TeamReviewsArgs{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("unexpected tool error: %s", toolErrorText(res))
	}
	if auth.lastPath != "/review/list" {
		t.Fatalf("wrong path: %s", auth.lastPath)
	}
	if !strings.Contains(auth.lastRaw, "scope=ceo") {
		t.Fatalf("expected caller scope, got %q", auth.lastRaw)
	}
	if !strings.Contains(toolErrorText(res), "rvw-1") {
		t.Fatalf("expected review id in response, got %s", toolErrorText(res))
	}
}

func TestHandleTeamReviewApprove(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/review/rvw-1/approve" {
			t.Fatalf("wrong path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":          "rvw-1",
			"state":       "approved",
			"target_path": "team/processes/passport.md",
			"commit_sha":  "abc1234",
		})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")

	res, _, err := handleTeamReview(context.Background(), nil, TeamReviewArgs{
		Action:    "approve",
		ReviewID:  "rvw-1",
		Rationale: "ready for the wiki",
	})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("unexpected tool error: %s", toolErrorText(res))
	}
	if !strings.Contains(auth.lastBody, "\"actor_slug\":\"ceo\"") {
		t.Fatalf("expected actor slug in body: %s", auth.lastBody)
	}
	if !strings.Contains(auth.lastBody, "\"rationale\":\"ready for the wiki\"") {
		t.Fatalf("expected rationale in body: %s", auth.lastBody)
	}
	if !strings.Contains(toolErrorText(res), "\"state\":\"approved\"") ||
		!strings.Contains(toolErrorText(res), "team/processes/passport.md") {
		t.Fatalf("unexpected result text: %s", toolErrorText(res))
	}
}

func TestHandleTeamReviewValidation(t *testing.T) {
	t.Setenv("WUPHF_AGENT_SLUG", "ceo")
	cases := []struct {
		name string
		args TeamReviewArgs
	}{
		{"missing id", TeamReviewArgs{Action: "approve"}},
		{"unknown action", TeamReviewArgs{Action: "banana", ReviewID: "rvw-1"}},
		{"request changes needs rationale", TeamReviewArgs{Action: "request_changes", ReviewID: "rvw-1"}},
		{"comment needs body", TeamReviewArgs{Action: "comment", ReviewID: "rvw-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, _, _ := handleTeamReview(context.Background(), nil, tc.args)
			if !isToolError(res) {
				t.Fatalf("expected tool error for %s", tc.name)
			}
		})
	}
}
