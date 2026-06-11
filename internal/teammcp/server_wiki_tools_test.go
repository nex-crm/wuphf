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
	if !isToolError(res) || !strings.Contains(toolErrorText(res), "human_request") {
		t.Fatalf("expected missing-human-request tool error, got %#v text=%q", res, toolErrorText(res))
	}
	// Human-boundary copy guard (ten-out-of-ten E1b): the gate's wording must
	// be safe for an agent to relay — no "broker message ID" jargon leading
	// the message (ICP-eval v3 [18:07]).
	if !strings.Contains(toolErrorText(res), "direct go-ahead") {
		t.Fatalf("gate error lost its human wording: %q", toolErrorText(res))
	}
	if strings.Contains(toolErrorText(res), "broker message ID") {
		t.Fatalf("gate error leads with broker jargon again: %q", toolErrorText(res))
	}
}

func TestHandleTeamWikiWriteLibrarianWritesDirectlyWithoutHumanRequest(t *testing.T) {
	// Phase 4: the Librarian owns the wiki, so it writes canonical articles
	// directly — no human_request gate (unlike every other agent, see
	// TestHandleTeamWikiWriteRequiresHumanRequestByDefault).
	srv, auth := stubBroker(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wiki/write":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"path":          "team/runbooks/onboarding.md",
				"commit_sha":    "def5678",
				"bytes_written": 12,
			})
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()
	withBrokerURL(t, srv.URL)
	t.Setenv("WUPHF_ENABLE_AGENT_WIKI_WRITE", "") // admin bypass OFF
	t.Setenv("WUPHF_AGENT_SLUG", "librarian")

	res, _, err := handleTeamWikiWrite(context.Background(), nil, TeamWikiWriteArgs{
		ArticlePath: "team/runbooks/onboarding.md",
		Mode:        "create",
		Content:     "# Onboarding\n",
		CommitMsg:   "wiki: organize onboarding runbook",
		// No HumanRequest — the Librarian does not need one.
	})
	if err != nil {
		t.Fatalf("handler returned transport error: %v", err)
	}
	if isToolError(res) {
		t.Fatalf("librarian direct write should bypass the human-request gate, got tool error: %s", toolErrorText(res))
	}
	if auth.lastPath != "/wiki/write" {
		t.Fatalf("posted path = %q, want /wiki/write", auth.lastPath)
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

func TestHasDirectWikiWriteIntentUsesWholeWordsAndLocalNegation(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{
			name: "human direct wiki request",
			text: "Please write this playbook directly to the wiki.",
			want: true,
		},
		{
			name: "wiki target before verb",
			text: "Wiki this launch checklist and update it for the team.",
			want: true,
		},
		{
			name: "knowledge base phrase",
			text: "Save the onboarding decision to the knowledge base.",
			want: true,
		},
		{
			name: "does not match verb inside another word",
			text: "This additional context mentions the wiki.",
			want: false,
		},
		{
			name: "nearby negation blocks intent",
			text: "Do not write this to the wiki.",
			want: false,
		},
		{
			name: "distant negation does not block later direct request",
			text: "Do not write the old draft to the wiki. Instead, save this final playbook to the wiki.",
			want: true,
		},
		{
			name: "following negation does not block direct request",
			text: "Write this playbook to the wiki, not the old draft.",
			want: true,
		},
		{
			name: "target and verb must be close",
			text: "Write the full operating memo after reviewing the implementation details, edge cases, rollout risks, owner list, and wiki index.",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasDirectWikiWriteIntent(tt.text); got != tt.want {
				t.Fatalf("hasDirectWikiWriteIntent(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
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
