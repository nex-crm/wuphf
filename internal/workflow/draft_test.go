package workflow

import (
	"path/filepath"
	"testing"
)

func TestDraftSpecShipchecks(t *testing.T) {
	// Note the duplicate slack_send — the draft must dedup it.
	spec := DraftSpec("spotted-ceo-abc", "Referral outreach", "ceo",
		[]string{"crm_lookup", "owner_resolve", "slack_draft", "slack_send", "referral_track", "slack_send"}, nil)
	rep := Shipcheck(&spec)
	if !rep.Passed {
		t.Fatalf("a draft scaffold must pass shipcheck:\n%s", rep.String())
	}
	if n := countAction(spec, "slack_send"); n != 1 {
		t.Fatalf("duplicate tool should dedup to 1 action, got %d", n)
	}
	// Kind inference.
	if k := kindOf(spec, "slack_send"); k != ActionExternal {
		t.Fatalf("slack_send should be external, got %q", k)
	}
	if k := kindOf(spec, "slack_draft"); k != ActionLLM {
		t.Fatalf("slack_draft should be llm, got %q", k)
	}
	if k := kindOf(spec, "crm_lookup"); k != ActionDeterministic {
		t.Fatalf("crm_lookup should be deterministic, got %q", k)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	spec := DraftSpec("x", "g", "op", []string{"a", "b"}, nil)
	path := filepath.Join(t.TempDir(), "x.workflow-spec.json")
	if err := SaveSpec(path, &spec); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := LoadSpec(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ID != "x" || len(got.Actions) != 2 {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

// TestDraftSpecBindsIntegrationActions is the regression for the "hollow draft"
// gap: a detected integration shape must come back EXECUTABLE — each real
// action bound to its platform/action_id, reads allow-listed and deterministic,
// writes external — while non-integration tokens stay unbound.
func TestDraftSpecBindsIntegrationActions(t *testing.T) {
	known := map[string]bool{"gmail": true, "slack": true}
	spec := DraftSpec("spotted-x", "Email to Slack", "outbound",
		[]string{"gmail_fetch_emails", "summarize_threads", "slack_chat_post_message"}, known)

	if rep := Shipcheck(&spec); !rep.Passed {
		t.Fatalf("bound draft must pass shipcheck:\n%s", rep.String())
	}

	fetch := actionByID(spec, "gmail_fetch_emails")
	if fetch == nil || fetch.Platform != "gmail" || fetch.ActionID != "GMAIL_FETCH_EMAILS" {
		t.Fatalf("gmail_fetch_emails must bind to gmail/GMAIL_FETCH_EMAILS, got %+v", fetch)
	}
	if !fetch.IsIntegrationRead() {
		t.Fatalf("gmail_fetch_emails must be a deterministic integration read, got kind %q", fetch.Kind)
	}
	if !readAllowed(spec, "gmail", "GMAIL_FETCH_EMAILS") {
		t.Fatalf("a bound read must be on AllowedReads, got %+v", spec.AllowedReads)
	}

	post := actionByID(spec, "slack_chat_post_message")
	if post == nil || post.Platform != "slack" || post.ActionID != "SLACK_CHAT_POST_MESSAGE" || post.Kind != ActionExternal {
		t.Fatalf("slack_chat_post_message must bind to slack as external, got %+v", post)
	}

	// Non-integration token stays unbound (no platform), kind from inference.
	sum := actionByID(spec, "summarize_threads")
	if sum == nil || sum.Platform != "" || sum.Kind != ActionLLM {
		t.Fatalf("summarize_threads must stay unbound + llm, got %+v", sum)
	}
}

func actionByID(s Spec, id string) *Action {
	for i := range s.Actions {
		if s.Actions[i].ID == id {
			return &s.Actions[i]
		}
	}
	return nil
}

func readAllowed(s Spec, platform, actionID string) bool {
	for _, r := range s.AllowedReads {
		if r.Platform == platform && r.ActionID == actionID {
			return true
		}
	}
	return false
}

func countAction(s Spec, id string) int {
	n := 0
	for _, a := range s.Actions {
		if a.ID == id {
			n++
		}
	}
	return n
}

func kindOf(s Spec, id string) ActionKind {
	for _, a := range s.Actions {
		if a.ID == id {
			return a.Kind
		}
	}
	return ""
}
