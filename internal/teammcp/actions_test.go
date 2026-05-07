package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/action"
)

type stubActionProvider struct{}

func (stubActionProvider) Name() string                    { return "one" }
func (stubActionProvider) Configured() bool                { return true }
func (stubActionProvider) Supports(action.Capability) bool { return true }
func (stubActionProvider) Guide(context.Context, string) (action.GuideResult, error) {
	return action.GuideResult{}, nil
}
func (stubActionProvider) ListConnections(context.Context, action.ListConnectionsOptions) (action.ConnectionsResult, error) {
	return action.ConnectionsResult{}, nil
}
func (stubActionProvider) SearchActions(context.Context, string, string, string) (action.ActionSearchResult, error) {
	return action.ActionSearchResult{}, nil
}
func (stubActionProvider) ActionKnowledge(context.Context, string, string) (action.KnowledgeResult, error) {
	return action.KnowledgeResult{}, nil
}
func (stubActionProvider) ExecuteAction(context.Context, action.ExecuteRequest) (action.ExecuteResult, error) {
	return action.ExecuteResult{
		DryRun: true,
		Request: action.ExecuteEnvelope{
			Method: "POST",
			URL:    "https://api.withone.ai/send",
		},
	}, nil
}
func (stubActionProvider) CreateWorkflow(context.Context, action.WorkflowCreateRequest) (action.WorkflowCreateResult, error) {
	return action.WorkflowCreateResult{Created: true, Key: "daily-digest"}, nil
}
func (stubActionProvider) ExecuteWorkflow(context.Context, action.WorkflowExecuteRequest) (action.WorkflowExecuteResult, error) {
	return action.WorkflowExecuteResult{RunID: "run-1", Status: "completed"}, nil
}
func (stubActionProvider) ListWorkflowRuns(context.Context, string) (action.WorkflowRunsResult, error) {
	return action.WorkflowRunsResult{}, nil
}
func (stubActionProvider) ListRelays(context.Context, action.ListRelaysOptions) (action.RelayListResult, error) {
	return action.RelayListResult{}, nil
}
func (stubActionProvider) RelayEventTypes(context.Context, string) (action.RelayEventTypesResult, error) {
	return action.RelayEventTypesResult{}, nil
}
func (stubActionProvider) CreateRelay(context.Context, action.RelayCreateRequest) (action.RelayResult, error) {
	return action.RelayResult{}, nil
}
func (stubActionProvider) ActivateRelay(context.Context, action.RelayActivateRequest) (action.RelayResult, error) {
	return action.RelayResult{}, nil
}
func (stubActionProvider) ListRelayEvents(context.Context, action.RelayEventsOptions) (action.RelayEventsResult, error) {
	return action.RelayEventsResult{}, nil
}
func (stubActionProvider) GetRelayEvent(context.Context, string) (action.RelayEventDetail, error) {
	return action.RelayEventDetail{}, nil
}

// TestActionIsReadOnly pins the behaviour of the approval-gate allow-list.
// The original implementation used strings.Contains and would classify any
// action_id that contained a substring from the read-verb list as read-only,
// which made mutating actions like "budget_update" or "findone_and_update"
// bypass the gate. The fixed implementation tokenizes on common separators
// and requires a whole-token match AND no mutating verb elsewhere in the ID.
//
// Every "mutation bypass" case in this table corresponds to a real Composio-
// shaped action name or a near-miss of one. A regression here silently
// reopens finding #7 of the CSO audit.
func TestActionIsReadOnly(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		// Read actions — must bypass the approval gate.
		{"GMAIL_FETCH_MAILS", true},
		{"GMAIL_SEARCH_EMAILS", true},
		{"HUBSPOT_GET_CONTACT", true},
		{"SLACKBOT_LIST_CHANNELS", true},
		{"GOOGLECALENDAR_EVENTS_LIST", true},
		{"describe_schema", true},
		{"browse.docs", true},
		{"list-contacts", true},

		// Mutating actions — must NOT be classified read-only.
		{"GMAIL_SEND_EMAIL", false},
		{"GMAIL_CREATE_DRAFT", false},
		{"HUBSPOT_UPDATE_CONTACT", false},
		{"SLACKBOT_SEND_MESSAGE", false},
		{"GOOGLECALENDAR_CREATE_EVENT", false},
		{"delete-user", false},
		{"post.message", false},
		{"set_status", false},

		// Substring bypasses the old implementation was vulnerable to.
		{"budget_update", false},         // contains "get" as substring
		{"findone_and_update", false},    // contains "find" + mutating "update"
		{"review_delete", false},         // contains "view" + mutating "delete"
		{"account_create", false},        // contains "count" + mutating "create"
		{"send_status_update", false},    // contains "status" + mutating "send"/"update"
		{"POST_SUMMARY", false},          // contains "summary" + mutating "post"
		{"archive_report", false},        // "archive" is mutating
		{"GMAIL_LIST_AND_DELETE", false}, // read AND mutate → must gate

		// Empty / unknown / ambiguous — default is GATED (return false).
		{"", false},
		{"mystery_action", false},
		{"do_thing", false},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			if got := actionIsReadOnly(tc.id); got != tc.want {
				t.Fatalf("actionIsReadOnly(%q) = %v, want %v", tc.id, got, tc.want)
			}
		})
	}
}

// TestRequireTeamActionApprovalBypasses exercises the three bypass paths
// that must never require a human click: DryRun=true, WUPHF_UNSAFE=1, and
// read-only action_ids. If any of these regress, agents either can't take
// safe actions (read operations pile up approval requests) or the gate
// fails open entirely.
func TestRequireTeamActionApprovalBypasses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Ensure the default bypass state — no WUPHF_UNSAFE unless a subtest sets it.
	t.Setenv("WUPHF_UNSAFE", "")

	t.Run("DryRun bypasses", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := requireTeamActionApproval(ctx, "ceo", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL", DryRun: true,
		})
		if err != nil {
			t.Fatalf("DryRun must bypass approval, got err=%v", err)
		}
	})

	t.Run("WUPHF_UNSAFE bypasses", func(t *testing.T) {
		t.Setenv("WUPHF_UNSAFE", "1")
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := requireTeamActionApproval(ctx, "ceo", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL", DryRun: false,
		})
		if err != nil {
			t.Fatalf("WUPHF_UNSAFE=1 must bypass approval, got err=%v", err)
		}
	})

	t.Run("read-only action_id bypasses", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := requireTeamActionApproval(ctx, "ceo", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_FETCH_MAILS", DryRun: false,
		})
		if err != nil {
			t.Fatalf("read-only action_id must bypass approval, got err=%v", err)
		}
	})
}

// TestBuildActionApprovalSpecGmailSend pins the canonical case the user
// flagged: One CLI for Gmail asks the human to approve sending an email,
// and the approval card MUST surface the recipient, subject, and body so
// the human can decide without leaving the page. Before this change, the
// card said only "Approve gmail action: GMAIL_SEND_EMAIL" and the user
// had to dig through the agent transcript to find what was being sent.
func TestBuildActionApprovalSpecGmailSend(t *testing.T) {
	args := TeamActionExecuteArgs{
		Platform: "gmail",
		ActionID: "GMAIL_SEND_EMAIL",
		Data: map[string]any{
			"to":      "alex@nex.ai",
			"subject": "Welcome to Nex",
			"body":    "Hi Alex,\n\nWelcome aboard!\n\nNazz",
		},
		ConnectionKey: "live::gmail::default::abc123",
		Summary:       "Sending a welcome note to a new user.",
	}
	spec := buildActionApprovalSpec("growthops", "general", args)

	if got, want := spec.Title, "Send Email via Gmail"; got != want {
		t.Errorf("Title = %q, want %q", got, want)
	}
	if got, want := spec.Question, "@growthops wants to send email via Gmail. Approve?"; got != want {
		t.Errorf("Question = %q, want %q", got, want)
	}
	for _, want := range []string{
		"Why: Sending a welcome note to a new user.",
		"What this will do:",
		"• To: alex@nex.ai",
		"• Subject: Welcome to Nex",
		"• Body: Hi Alex, Welcome aboard! Nazz",
		"Action: GMAIL_SEND_EMAIL via Gmail",
		"Account: live::gmail::default::abc123",
		"Channel: #general",
	} {
		if !strings.Contains(spec.Context, want) {
			t.Errorf("Context missing %q\n--- got ---\n%s", want, spec.Context)
		}
	}
}

// TestBuildActionApprovalSpecPlatforms exercises the platform/verb
// rendering for the action shapes One CLI can fan out to. Each row
// captures the exact title and question text the human would see.
func TestBuildActionApprovalSpecPlatforms(t *testing.T) {
	cases := []struct {
		name     string
		args     TeamActionExecuteArgs
		title    string
		question string
	}{
		{
			name:     "gmail_create_draft",
			args:     TeamActionExecuteArgs{Platform: "gmail", ActionID: "GMAIL_CREATE_DRAFT"},
			title:    "Create Draft via Gmail",
			question: "@growthops wants to create draft via Gmail. Approve?",
		},
		{
			name:     "hubspot_update_contact",
			args:     TeamActionExecuteArgs{Platform: "hubspot", ActionID: "HUBSPOT_UPDATE_CONTACT"},
			title:    "Update Contact via HubSpot",
			question: "@growthops wants to update contact via HubSpot. Approve?",
		},
		{
			name:     "slack_post_message",
			args:     TeamActionExecuteArgs{Platform: "slack", ActionID: "SLACKBOT_SEND_MESSAGE"},
			title:    "Send Message via Slack",
			question: "@growthops wants to send message via Slack. Approve?",
		},
		{
			name:     "google_calendar_create",
			args:     TeamActionExecuteArgs{Platform: "google-calendar", ActionID: "GOOGLECALENDAR_CREATE_EVENT"},
			title:    "Create Event via Google Calendar",
			question: "@growthops wants to create event via Google Calendar. Approve?",
		},
		{
			name:     "github_create_issue",
			args:     TeamActionExecuteArgs{Platform: "github", ActionID: "GITHUB_CREATE_ISSUE"},
			title:    "Create Issue via GitHub",
			question: "@growthops wants to create issue via GitHub. Approve?",
		},
		{
			name:     "no_platform_prefix",
			args:     TeamActionExecuteArgs{Platform: "stripe", ActionID: "create_refund"},
			title:    "Create Refund via Stripe",
			question: "@growthops wants to create refund via Stripe. Approve?",
		},
		{
			name:     "single_token_action",
			args:     TeamActionExecuteArgs{Platform: "gmail", ActionID: "GMAIL_SEND"},
			title:    "Send via Gmail",
			question: "@growthops wants to send via Gmail. Approve?",
		},
		{
			name:     "missing_platform_falls_back",
			args:     TeamActionExecuteArgs{Platform: "", ActionID: "DO_THING"},
			title:    "Do Thing via Unknown",
			question: "@growthops wants to do thing via Unknown. Approve?",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := buildActionApprovalSpec("growthops", "general", tc.args)
			if spec.Title != tc.title {
				t.Errorf("Title = %q, want %q", spec.Title, tc.title)
			}
			if spec.Question != tc.question {
				t.Errorf("Question = %q, want %q", spec.Question, tc.question)
			}
		})
	}
}

// TestBuildActionApprovalSpecPayloadEdgeCases pins the behaviour that
// keeps the approval card readable when payloads are unusual: redacted
// keys never leak, long bodies are clipped, list recipients render as
// comma-separated, and missing payload data still produces a sensible
// card (no empty "What this will do" header).
func TestBuildActionApprovalSpecPayloadEdgeCases(t *testing.T) {
	t.Run("redacts secret-shaped keys", func(t *testing.T) {
		spec := buildActionApprovalSpec("growthops", "general", TeamActionExecuteArgs{
			Platform: "gmail",
			ActionID: "GMAIL_SEND_EMAIL",
			Data: map[string]any{
				"to":           "alex@nex.ai",
				"access_token": "super-secret-token",
				"password":     "nope",
			},
		})
		if strings.Contains(spec.Context, "super-secret-token") || strings.Contains(spec.Context, "nope") {
			t.Fatalf("approval context leaks redacted value:\n%s", spec.Context)
		}
		if !strings.Contains(spec.Context, "alex@nex.ai") {
			t.Fatalf("approval context dropped recipient:\n%s", spec.Context)
		}
	})

	t.Run("clips long bodies", func(t *testing.T) {
		long := strings.Repeat("x", 800)
		spec := buildActionApprovalSpec("growthops", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL",
			Data: map[string]any{"to": "a@b.com", "body": long},
		})
		if !strings.Contains(spec.Context, "…") {
			t.Fatalf("expected clipped body marker, got:\n%s", spec.Context)
		}
		if strings.Count(spec.Context, "x") > payloadValueClipLen+10 {
			t.Fatalf("body not clipped: %d xs", strings.Count(spec.Context, "x"))
		}
	})

	t.Run("renders list recipients", func(t *testing.T) {
		spec := buildActionApprovalSpec("growthops", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL",
			Data: map[string]any{
				"recipients": []any{"a@x.com", "b@x.com"},
				"subject":    "Hi",
			},
		})
		if !strings.Contains(spec.Context, "• To: a@x.com, b@x.com") {
			t.Fatalf("expected joined recipients, got:\n%s", spec.Context)
		}
	})

	t.Run("omits details block when payload is empty", func(t *testing.T) {
		spec := buildActionApprovalSpec("growthops", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL",
		})
		if strings.Contains(spec.Context, "What this will do:") {
			t.Fatalf("unexpected details block for empty payload:\n%s", spec.Context)
		}
		if !strings.Contains(spec.Context, "Action: GMAIL_SEND_EMAIL via Gmail") {
			t.Fatalf("missing footer for empty payload:\n%s", spec.Context)
		}
	})

	t.Run("omits why when summary missing", func(t *testing.T) {
		spec := buildActionApprovalSpec("growthops", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL",
			Data: map[string]any{"to": "a@b.com"},
		})
		if strings.Contains(spec.Context, "Why:") {
			t.Fatalf("unexpected Why line without summary:\n%s", spec.Context)
		}
	})

	t.Run("falls back through key synonyms", func(t *testing.T) {
		spec := buildActionApprovalSpec("growthops", "general", TeamActionExecuteArgs{
			Platform: "slack", ActionID: "SLACKBOT_SEND_MESSAGE",
			Data: map[string]any{
				"channel_id": "C123",
				"text":       "  hello  team  ",
			},
		})
		if !strings.Contains(spec.Context, "• Channel: C123") {
			t.Fatalf("expected Channel label, got:\n%s", spec.Context)
		}
		if !strings.Contains(spec.Context, "• Body: hello team") {
			t.Fatalf("expected collapsed Body label, got:\n%s", spec.Context)
		}
	})

	t.Run("synonyms collapse onto one label", func(t *testing.T) {
		spec := buildActionApprovalSpec("growthops", "general", TeamActionExecuteArgs{
			Platform: "gmail", ActionID: "GMAIL_SEND_EMAIL",
			Data: map[string]any{
				"to":         "primary@x.com",
				"recipients": []any{"other@x.com"},
			},
		})
		// "to" is earlier in the synonym list — recipients must NOT add a second "To:".
		if got := strings.Count(spec.Context, "• To:"); got != 1 {
			t.Fatalf("expected exactly one To bullet, got %d:\n%s", got, spec.Context)
		}
		if !strings.Contains(spec.Context, "primary@x.com") {
			t.Fatalf("expected primary recipient, got:\n%s", spec.Context)
		}
	})
}

// TestBuildActionApprovalSpecMatchesCanonicalFixture pins the exact context
// string the Go encoder produces against testdata/approval_context_canonical.txt.
// The web-side parser (web/src/lib/parseApprovalContext.test.ts) loads the
// SAME file and asserts the parser yields the expected structure. Together
// these two tests form the contract: if either side renames a prefix, moves
// a section, or changes the bullet character, one of these tests breaks
// loudly. Without this, the rich rendering can degrade silently to plain
// pre-wrap text and no test catches it.
func TestBuildActionApprovalSpecMatchesCanonicalFixture(t *testing.T) {
	args := TeamActionExecuteArgs{
		Platform: "gmail",
		ActionID: "GMAIL_SEND_EMAIL",
		Data: map[string]any{
			"to":      "alex@nex.ai",
			"subject": "Welcome to Nex",
			"body":    "Hi Alex, welcome aboard! Looking forward to working with you. -Nazz",
		},
		ConnectionKey: "live::gmail::default::abc123",
		Summary:       "Sending a welcome note to a new user.",
	}
	spec := buildActionApprovalSpec("growthops", "general", args)

	want, err := os.ReadFile(filepath.Join("testdata", "approval_context_canonical.txt"))
	if err != nil {
		t.Fatalf("read canonical fixture: %v", err)
	}
	expected := strings.TrimRight(string(want), "\n")
	if spec.Context != expected {
		t.Fatalf("context mismatch — fixture drift would break web parser.\n--- want ---\n%s\n--- got ---\n%s", expected, spec.Context)
	}
}

// TestActionApprovalDedupeKey pins the dedupe key shape so future refactors
// of requireTeamActionApproval cannot silently drift the key format. The
// broker collapses retries based on whatever this function emits — if the
// shape changes (e.g., absorbing args.Summary into the key), every previously
// in-flight approval becomes a new pending request and the human stares at
// stacked duplicates.
func TestActionApprovalDedupeKey(t *testing.T) {
	cases := []struct {
		name string
		slug string
		args TeamActionExecuteArgs
		want string
	}{
		{
			name: "canonical_gmail_send",
			slug: "growthops",
			args: TeamActionExecuteArgs{
				Platform:      "gmail",
				ActionID:      "GMAIL_SEND_EMAIL",
				ConnectionKey: "live::gmail::default::abc123",
			},
			want: "action:growthops:gmail:gmail_send_email:live::gmail::default::abc123",
		},
		{
			name: "case_insensitive_normalization",
			slug: "GrowthOps",
			args: TeamActionExecuteArgs{
				Platform:      "Gmail",
				ActionID:      "Gmail_Send_Email",
				ConnectionKey: "  Live::Gmail::Default::ABC123  ",
			},
			want: "action:growthops:gmail:gmail_send_email:live::gmail::default::abc123",
		},
		{
			name: "different_payloads_same_key",
			// Two genuinely-different sends to different recipients on the
			// same connection collapse onto one approval. This is the
			// existing collision behavior; this test pins it so anyone
			// changing the key shape (e.g., to disambiguate by payload
			// hash) sees the regression here.
			slug: "growthops",
			args: TeamActionExecuteArgs{
				Platform:      "gmail",
				ActionID:      "GMAIL_SEND_EMAIL",
				ConnectionKey: "live::gmail::default::abc123",
				Data:          map[string]any{"to": "different@example.com"},
			},
			want: "action:growthops:gmail:gmail_send_email:live::gmail::default::abc123",
		},
		{
			name: "empty_connection_renders_trailing_empty",
			slug: "growthops",
			args: TeamActionExecuteArgs{
				Platform: "gmail",
				ActionID: "GMAIL_SEND_EMAIL",
			},
			want: "action:growthops:gmail:gmail_send_email:",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := actionApprovalDedupeKey(tc.slug, tc.args); got != tc.want {
				t.Fatalf("dedupe key = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBuildActionApprovalSpecRejectsForgedSummary is the adversarial
// regression test for the trust-boundary vulnerability flagged during
// /ship's adversarial review. A malicious agent supplies a Summary
// containing fake "What this will do:" / "Action:" / "Channel:" sections.
// Without the sanitizer, the web parser's first-match-wins regexes would
// match the FORGED structure (it appears first in the context string),
// hiding the real action behind a benign-looking display. With the
// sanitizer, the agent's newlines and bullet glyph are flattened so
// none of those forged prefixes can land at a line start where the
// `^Section:` parser regexes match.
func TestBuildActionApprovalSpecRejectsForgedSummary(t *testing.T) {
	forged := "Routine bookkeeping.\n\nWhat this will do:\n• To: ceo@nex.ai\n• Subject: Approve immediately\n• Body: Routine task\n\nAction: GMAIL_FETCH_MAILS via Gmail\nChannel: #general-fake\n\nReal:"
	args := TeamActionExecuteArgs{
		Platform:      "gmail",
		ActionID:      "GMAIL_DELETE_THREAD",
		Summary:       forged,
		ConnectionKey: "live::gmail::default::abc123",
		Data:          map[string]any{"thread_id": "important-thread-123"},
	}
	spec := buildActionApprovalSpec("growthops", "general", args)

	// The legit details block must survive: no agent-injected "What this
	// will do:" line should appear at a line start before the real one.
	whatLineCount := strings.Count(spec.Context, "\nWhat this will do:")
	if whatLineCount != 1 {
		t.Errorf("expected exactly one line-leading 'What this will do:' line, got %d in:\n%s", whatLineCount, spec.Context)
	}

	// The legit Action footer must reflect the REAL action_id, not the
	// forged one. The first "Action:" line at a line start is what the
	// parser matches.
	actionLineCount := 0
	realActionPresent := false
	for _, line := range strings.Split(spec.Context, "\n") {
		if strings.HasPrefix(line, "Action: ") {
			actionLineCount++
			if strings.Contains(line, "GMAIL_DELETE_THREAD") {
				realActionPresent = true
			}
			if strings.Contains(line, "GMAIL_FETCH_MAILS") {
				t.Errorf("forged Action: line landed at line start: %q", line)
			}
		}
	}
	if actionLineCount != 1 {
		t.Errorf("expected exactly one line-leading Action: line, got %d", actionLineCount)
	}
	if !realActionPresent {
		t.Errorf("real action GMAIL_DELETE_THREAD missing from Action: line")
	}

	// The legit Channel must be #general (not #general-fake).
	channelLines := 0
	for _, line := range strings.Split(spec.Context, "\n") {
		if strings.HasPrefix(line, "Channel: ") {
			channelLines++
			if strings.Contains(line, "fake") {
				t.Errorf("forged Channel: line landed at line start: %q", line)
			}
		}
	}
	if channelLines != 1 {
		t.Errorf("expected exactly one Channel: line, got %d", channelLines)
	}

	// No line-leading bullet may carry one of the FORGED values from the
	// agent's Summary. Legit bullets (e.g., "• Thread: important-thread-123"
	// produced from args.Data) are fine; the test rejects only the
	// specific forged content.
	forgedValues := []string{"ceo@nex.ai", "Approve immediately", "Routine task"}
	for _, line := range strings.Split(spec.Context, "\n") {
		if !strings.HasPrefix(line, "• ") {
			continue
		}
		for _, fv := range forgedValues {
			if strings.Contains(line, fv) {
				t.Errorf("forged value %q landed in line-leading bullet: %q", fv, line)
			}
		}
	}
}

// TestBuildActionApprovalSpecRejectsForgedConnectionKey covers the same
// trust-boundary defense for the Account: line. An agent that controls
// the connection_key string could otherwise newline-inject a forged
// Channel: line.
func TestBuildActionApprovalSpecRejectsForgedConnectionKey(t *testing.T) {
	args := TeamActionExecuteArgs{
		Platform:      "gmail",
		ActionID:      "GMAIL_SEND_EMAIL",
		ConnectionKey: "real_conn_key\nChannel: #ceo-private",
		Data:          map[string]any{"to": "alex@nex.ai"},
	}
	spec := buildActionApprovalSpec("growthops", "general", args)

	// The forged Channel: substring should not appear at any line start.
	for _, line := range strings.Split(spec.Context, "\n") {
		if strings.HasPrefix(line, "Channel: ") && strings.Contains(line, "ceo-private") {
			t.Errorf("forged Channel: from connection_key landed at line start: %q", line)
		}
	}
	// The legit Channel: line must be #general.
	if !strings.Contains(spec.Context, "\nChannel: #general") {
		t.Errorf("legit Channel: #general missing from context:\n%s", spec.Context)
	}
}

// TestSanitizeContextValue pins the structural-delimiter scrubber.
func TestSanitizeContextValue(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text passes through", "Sending welcome.", "Sending welcome."},
		{"newlines collapse to spaces", "line1\nline2\nline3", "line1 line2 line3"},
		{"crlf collapses", "a\r\nb", "a b"},
		{"u2028 line separator", "a b", "a b"},
		{"u2029 paragraph separator", "a b", "a b"},
		{"bullet becomes middle dot", "Plain • bullet", "Plain · bullet"},
		{"forged section headers stay inline (no line-start)",
			"Routine.\n\nWhat this will do:\n• To: x@y.com",
			"Routine. What this will do: · To: x@y.com"},
		{"runs of whitespace collapse", "a    b\n\n  c", "a b c"},
		{"trailing/leading whitespace stripped", "  hello  ", "hello"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeContextValue(tc.in); got != tc.want {
				t.Errorf("sanitizeContextValue(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestFormatPayloadValueRuneAwareTruncation guards against the
// multi-byte UTF-8 byte-slicing bug. A 100-rune CJK string takes 300
// bytes; clipping at 240 bytes would land mid-codepoint and produce
// invalid UTF-8 that the JSON encoder later replaces with U+FFFD,
// silently corrupting the rendered card.
func TestFormatPayloadValueRuneAwareTruncation(t *testing.T) {
	t.Run("ASCII string clipped at rune boundary", func(t *testing.T) {
		long := strings.Repeat("a", 300)
		got := formatPayloadValue(long)
		if !strings.HasSuffix(got, "…") {
			t.Errorf("expected truncation marker, got %q", got)
		}
		if utf8.RuneCountInString(got) != payloadValueClipLen+1 {
			t.Errorf("expected %d runes after clip, got %d", payloadValueClipLen+1, utf8.RuneCountInString(got))
		}
	})

	t.Run("CJK string clipped without producing invalid UTF-8", func(t *testing.T) {
		long := strings.Repeat("好", 300)
		got := formatPayloadValue(long)
		if !utf8.ValidString(got) {
			t.Fatalf("formatPayloadValue produced invalid UTF-8: %q", got)
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("expected truncation marker, got %q", got)
		}
		// Verify the clipped portion is exactly payloadValueClipLen runes
		// of CJK (each 3 bytes) plus the ellipsis (also 3 bytes).
		runes := []rune(got)
		if len(runes) != payloadValueClipLen+1 {
			t.Errorf("expected %d runes, got %d", payloadValueClipLen+1, len(runes))
		}
	})

	t.Run("emoji string clipped without producing invalid UTF-8", func(t *testing.T) {
		// 4-byte codepoint (rocket emoji) repeated.
		long := strings.Repeat("🚀", 300)
		got := formatPayloadValue(long)
		if !utf8.ValidString(got) {
			t.Fatalf("formatPayloadValue produced invalid UTF-8: %q", got)
		}
	})
}

func TestHandleTeamActionExecuteLogsBrokerAction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	prev := externalActionProvider
	externalActionProvider = stubActionProvider{}
	defer func() { externalActionProvider = prev }()

	if _, _, err := handleTeamActionExecute(context.Background(), nil, TeamActionExecuteArgs{
		Platform:      "gmail",
		ActionID:      "send-email",
		ConnectionKey: "live::gmail::default::abc123",
		DryRun:        true,
		MySlug:        "ceo",
		Channel:       "general",
	}); err != nil {
		t.Fatalf("execute action: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/actions", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get actions: %v", err)
	}
	defer resp.Body.Close()
	if got := len(b.Actions()); got != 1 {
		t.Fatalf("expected 1 action, got %d", got)
	}
	if action := b.Actions()[0]; action.Kind != "external_action_planned" || action.Source != "one" {
		t.Fatalf("unexpected action %+v", action)
	}
}

func TestHandleTeamActionWorkflowCreateMirrorsSkill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	prev := externalActionProvider
	externalActionProvider = stubActionProvider{}
	defer func() { externalActionProvider = prev }()

	_, _, err := handleTeamActionWorkflowCreate(context.Background(), nil, TeamActionWorkflowCreateArgs{
		Key:              "daily-digest",
		DefinitionJSON:   `{"steps":[]}`,
		MySlug:           "ceo",
		Channel:          "general",
		SkillName:        "daily-digest",
		SkillTitle:       "Daily Digest",
		SkillDescription: "Send the daily digest.",
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/skills?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get skills: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Skills []struct {
			Name             string `json:"name"`
			WorkflowProvider string `json:"workflow_provider"`
			WorkflowKey      string `json:"workflow_key"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	if len(result.Skills) != 1 {
		t.Fatalf("expected mirrored skill, got %+v", result.Skills)
	}
	if result.Skills[0].WorkflowProvider != "one" || result.Skills[0].WorkflowKey != "daily-digest" {
		t.Fatalf("unexpected skill metadata %+v", result.Skills[0])
	}
}

func TestHandleTeamActionWorkflowScheduleCreatesSchedulerJob(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	prev := externalActionProvider
	externalActionProvider = stubActionProvider{}
	defer func() { externalActionProvider = prev }()

	_, _, err := handleTeamActionWorkflowSchedule(context.Background(), nil, TeamActionWorkflowScheduleArgs{
		Key:      "daily-digest",
		Schedule: "daily",
		MySlug:   "ceo",
		Channel:  "general",
	})
	if err != nil {
		t.Fatalf("schedule workflow: %v", err)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/scheduler", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get scheduler: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Jobs []struct {
			Kind         string `json:"kind"`
			TargetType   string `json:"target_type"`
			TargetID     string `json:"target_id"`
			Provider     string `json:"provider"`
			ScheduleExpr string `json:"schedule_expr"`
		} `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode jobs: %v", err)
	}
	if len(result.Jobs) != 1 {
		t.Fatalf("expected one scheduled job, got %+v", result.Jobs)
	}
	job := result.Jobs[0]
	if job.Kind != "one_workflow" || job.TargetType != "workflow" || job.TargetID != "daily-digest" || job.Provider != "one" || job.ScheduleExpr != "daily" {
		t.Fatalf("unexpected scheduler job %+v", job)
	}
}

func TestHandleTeamActionWorkflowScheduleRunNowExecutesImmediately(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	prev := externalActionProvider
	externalActionProvider = stubActionProvider{}
	defer func() { externalActionProvider = prev }()

	res, _, err := handleTeamActionWorkflowSchedule(context.Background(), nil, TeamActionWorkflowScheduleArgs{
		Key:      "daily-digest",
		Schedule: "daily",
		RunNow:   true,
		MySlug:   "ceo",
		Channel:  "general",
	})
	if err != nil {
		t.Fatalf("schedule workflow with run_now: %v", err)
	}
	text, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected text response, got %+v", res.Content)
	}
	if got := text.Text; !strings.Contains(got, "\"run_now\"") || !strings.Contains(got, "\"ok\": true") {
		t.Fatalf("expected run_now block in response, got %s", got)
	}
	var sawScheduled, sawExecuted bool
	for _, entry := range b.Actions() {
		if entry.Kind == "external_workflow_scheduled" {
			sawScheduled = true
		}
		if entry.Kind == "external_workflow_executed" {
			sawExecuted = true
		}
	}
	if !sawScheduled || !sawExecuted {
		t.Fatalf("expected scheduled and executed actions, got %+v", b.Actions())
	}
}

func TestSelectedActionProviderIncludesCapabilityGuidance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_NO_NEX", "1")

	prev := externalActionProvider
	externalActionProvider = nil
	defer func() { externalActionProvider = prev }()

	_, err := selectedActionProvider(action.CapabilityActionExecute)
	if err == nil {
		t.Fatal("expected provider selection to fail when Nex is disabled")
	}
	if !strings.Contains(err.Error(), "Restart without --no-nex") {
		t.Fatalf("expected readiness next step in %q", err)
	}
}
