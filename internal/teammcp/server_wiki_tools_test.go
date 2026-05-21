package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestHandleTeamWikiWriteRequiresHumanRequestByDefault(t *testing.T) {
	t.Setenv("WUPHF_ENABLE_AGENT_WIKI_WRITE", "")

	res, _, err := handleTeamWikiWrite(context.Background(), nil, TeamWikiWriteArgs{
		MySlug:      "pm",
		ArticlePath: "team/decisions/direct.md",
		Mode:        "create",
		Content:     "# Direct\n",
	})
	if err != nil {
		t.Fatalf("handler returned transport error: %v", err)
	}
	if !isToolError(res) || !strings.Contains(toolErrorText(res), "requires human_request") {
		t.Fatalf("expected missing-human-request tool error, got %#v text=%q", res, toolErrorText(res))
	}
}

func TestHandleTeamWikiWriteHumanRequestedPostsToBroker(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path":          "team/decisions/direct.md",
			"commit_sha":    "abc1234",
			"bytes_written": 9,
		})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_ENABLE_AGENT_WIKI_WRITE", "")
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleTeamWikiWrite(context.Background(), nil, TeamWikiWriteArgs{
		ArticlePath:  "team/decisions/direct.md",
		Mode:         "create",
		Content:      "# Direct\n",
		CommitMsg:    "wiki: add requested playbook",
		HumanRequest: "write up a playbook and put it directly in the wiki",
	})
	if err != nil {
		t.Fatalf("handler returned transport error: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("unexpected tool error: %s", toolErrorText(res))
	}
	if auth.lastPath != "/wiki/write" {
		t.Fatalf("posted path = %q, want /wiki/write", auth.lastPath)
	}
	if !strings.Contains(auth.lastBody, `"path":"team/decisions/direct.md"`) {
		t.Fatalf("broker body missing wiki path: %s", auth.lastBody)
	}
}

func TestHandleTeamWikiWriteAdminBypassPostsToBroker(t *testing.T) {
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path":          "team/decisions/direct.md",
			"commit_sha":    "abc1234",
			"bytes_written": 9,
		})
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_ENABLE_AGENT_WIKI_WRITE", "true")
	t.Setenv("WUPHF_AGENT_SLUG", "pm")

	res, _, err := handleTeamWikiWrite(context.Background(), nil, TeamWikiWriteArgs{
		ArticlePath: "team/decisions/direct.md",
		Mode:        "create",
		Content:     "# Direct\n",
		CommitMsg:   "admin: direct wiki write",
	})
	if err != nil {
		t.Fatalf("handler returned transport error: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("unexpected tool error: %s", toolErrorText(res))
	}
	if auth.lastPath != "/wiki/write" {
		t.Fatalf("posted path = %q, want /wiki/write", auth.lastPath)
	}
}
