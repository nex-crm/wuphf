package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
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
		switch r.URL.Path {
		case "/channels":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"channels": []map[string]any{{"slug": "general", "members": []string{"pm"}}},
			})
		case "/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []map[string]any{{
					"id":        "msg-42",
					"from":      "human:team-member",
					"channel":   "general",
					"content":   "Please write this playbook directly to the wiki.",
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				}},
			})
		case "/wiki/write":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"path":          "team/decisions/direct.md",
				"commit_sha":    "abc1234",
				"bytes_written": 9,
			})
		default:
			http.NotFound(w, r)
		}
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
		HumanRequest: "msg-42",
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

func TestHandleTeamWikiWriteRejectsUnverifiedHumanRequest(t *testing.T) {
	srv, _ := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/channels":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"channels": []map[string]any{{"slug": "general", "members": []string{"pm"}}},
			})
		case "/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []map[string]any{{
					"id":        "msg-42",
					"from":      "pm",
					"channel":   "general",
					"content":   "Please write this directly to the wiki.",
					"timestamp": time.Now().UTC().Format(time.RFC3339),
				}},
			})
		default:
			http.NotFound(w, r)
		}
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
		HumanRequest: "msg-42",
	})
	if err != nil {
		t.Fatalf("handler returned transport error: %v", err)
	}
	if !isToolError(res) || !strings.Contains(toolErrorText(res), "not a human-authored message") {
		t.Fatalf("expected unverified-human-request tool error, got %#v text=%q", res, toolErrorText(res))
	}
}

func TestHandleTeamWikiWriteRejectsExpiredHumanRequest(t *testing.T) {
	srv, _ := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/channels":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"channels": []map[string]any{{"slug": "general", "members": []string{"pm"}}},
			})
		case "/messages":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"messages": []map[string]any{{
					"id":        "msg-42",
					"from":      "human:team-member",
					"channel":   "general",
					"content":   "Please write this directly to the wiki.",
					"timestamp": time.Now().Add(-2 * humanWikiDelegationMaxAge).UTC().Format(time.RFC3339),
				}},
			})
		default:
			http.NotFound(w, r)
		}
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
		HumanRequest: "msg-42",
	})
	if err != nil {
		t.Fatalf("handler returned transport error: %v", err)
	}
	if !isToolError(res) || !strings.Contains(toolErrorText(res), "is expired") {
		t.Fatalf("expected expired-human-request tool error, got %#v text=%q", res, toolErrorText(res))
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
