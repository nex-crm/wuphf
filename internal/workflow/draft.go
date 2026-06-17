package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// draft.go bridges discovery -> contract. A detection candidate is a tool-shape,
// not a state machine; DraftSpec turns it into a VALID linear scaffold that
// passes shipcheck, which the operator then reviews and enriches into a real
// lifecycle (the referral spec is what a reviewed contract looks like). The
// draft is deliberately minimal: discovery proposes, the human authors.

// DraftSpec builds a shippable linear scaffold from a detected tool shape:
//
//	start --run--> done, running every tool as an action on the transition.
//
// Tool names are deduped (first-use order) and each becomes an Action whose kind
// is inferred from the name. The result validates and passes Shipcheck.
func DraftSpec(id, goal, operator string, shape []string) Spec {
	actions := make([]Action, 0, len(shape))
	actionIDs := make([]string, 0, len(shape))
	seen := map[string]bool{}
	for _, tool := range shape {
		t := strings.TrimSpace(tool)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		actions = append(actions, Action{
			ID:          t,
			Kind:        inferKind(t),
			Description: "Detected step: " + t,
		})
		actionIDs = append(actionIDs, t)
	}
	if goal == "" {
		goal = "Repeated workflow detected from office activity"
	}
	if operator == "" {
		operator = "operator"
	}
	return Spec{
		Version:  "1",
		ID:       id,
		Goal:     goal,
		Operator: operator,
		States:   []State{{ID: "start", Label: "Start"}, {ID: "done", Label: "Done"}},
		Initial:  "start",
		Terminal: []string{"done"},
		Events:   []Event{{ID: "run", Label: "Run the workflow"}},
		Actions:  actions,
		Transitions: []Transition{
			{From: "start", To: "done", On: "run", Actions: actionIDs},
		},
		Scenarios: []Scenario{
			{
				Name:          "happy_path",
				Events:        []ScenarioEvent{{Event: "run", DedupKey: "sample-1"}},
				ExpectStates:  []string{"start", "done"},
				ExpectActions: actionIDs,
			},
		},
		ImprovementSignals: []string{"run_count", "exception_rate"},
	}
}

// inferKind guesses an action's kind from its tool name. Sends/posts are
// external (gated); drafting/composing is an LLM call; everything else is
// treated as deterministic. The operator corrects this during review.
func inferKind(tool string) ActionKind {
	l := strings.ToLower(tool)
	// Check LLM verbs before send verbs so "slack_draft" reads as drafting (llm),
	// not as a Slack action. External triggers are send-VERBS, not platform names
	// ("slack" alone is not a send).
	switch {
	case containsAny(l, "draft", "compose", "write", "generate", "summar"):
		return ActionLLM
	case containsAny(l, "send", "post", "deliver", "dm", "notify", "push", "sms"):
		return ActionExternal
	default:
		return ActionDeterministic
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// SaveSpec writes a spec to path as indented JSON (creating parent dirs).
func SaveSpec(path string, s *Spec) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
