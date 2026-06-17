package workflow

import (
	"path/filepath"
	"testing"
)

func TestDraftSpecShipchecks(t *testing.T) {
	// Note the duplicate slack_send — the draft must dedup it.
	spec := DraftSpec("spotted-ceo-abc", "Referral outreach", "ceo",
		[]string{"crm_lookup", "owner_resolve", "slack_draft", "slack_send", "referral_track", "slack_send"})
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
	spec := DraftSpec("x", "g", "op", []string{"a", "b"})
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
