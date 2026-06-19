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
// is inferred from the name. When a token names a real integration action (its
// platform prefix is in knownPlatforms, e.g. "gmail_fetch_emails" with gmail
// connected), the action is BOUND back to its platform + action_id so the
// drafted contract is executable instead of an empty skeleton — a read becomes
// a deterministic integration read (added to AllowedReads, the human-blessed
// allow-list), a write/send becomes a gated external action. Pass nil for
// knownPlatforms to skip binding (pure structural scaffold). The result
// validates and passes Shipcheck.
func DraftSpec(id, goal, operator string, shape []string, knownPlatforms map[string]bool) Spec {
	actions := make([]Action, 0, len(shape))
	actionIDs := make([]string, 0, len(shape))
	var allowedReads []ActionRef
	seen := map[string]bool{}
	seenRead := map[string]bool{}
	for _, tool := range shape {
		t := strings.TrimSpace(tool)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		a := Action{ID: t, Description: "Detected step: " + t}
		if platform, actionID, ok := bindIntegrationAction(t, knownPlatforms); ok {
			a.Platform = platform
			a.ActionID = actionID
			if isReadAction(t) {
				// Deterministic integration read; must be allow-listed or
				// Validate rejects it (a human approves reads at freeze).
				a.Kind = ActionDeterministic
				if key := platform + "\x00" + actionID; !seenRead[key] {
					seenRead[key] = true
					allowedReads = append(allowedReads, ActionRef{Platform: platform, ActionID: actionID})
				}
			} else {
				a.Kind = ActionExternal
			}
		} else {
			a.Kind = inferKind(t)
		}
		actions = append(actions, a)
		actionIDs = append(actionIDs, t)
	}
	if goal == "" {
		goal = "Repeated workflow detected from office activity"
	}
	if operator == "" {
		operator = "operator"
	}
	return Spec{
		Version:      "1",
		ID:           id,
		Goal:         goal,
		Operator:     operator,
		States:       []State{{ID: "start", Label: "Start"}, {ID: "done", Label: "Done"}},
		Initial:      "start",
		Terminal:     []string{"done"},
		Events:       []Event{{ID: "run", Label: "Run the workflow"}},
		Actions:      actions,
		AllowedReads: allowedReads,
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

// bindIntegrationAction reverses a detected action token back to its
// (platform, action_id). Detection tokens are the lowercased Composio action id
// (manifestToolToken: GMAIL_FETCH_EMAILS -> "gmail_fetch_emails"), and Composio
// action ids are platform-prefixed, so the first underscore segment is the
// platform and the upper-cased token is the action id. Binds only when that
// platform is in known (a real connected platform), so non-integration tokens
// ("summarize_threads") are left unbound.
func bindIntegrationAction(token string, known map[string]bool) (platform, actionID string, ok bool) {
	seg := strings.SplitN(strings.TrimSpace(token), "_", 2)
	if len(seg) < 2 {
		return "", "", false
	}
	p := strings.ToLower(seg[0])
	if known == nil || !known[p] {
		return "", "", false
	}
	return p, strings.ToUpper(strings.TrimSpace(token)), true
}

// readVerbs name integration actions that only READ (no external side effect),
// so a bound action carrying one is a deterministic integration read rather than
// a gated write/send.
var readVerbs = map[string]bool{
	"fetch": true, "get": true, "list": true, "search": true, "read": true,
	"find": true, "lookup": true, "retrieve": true, "pull": true, "load": true,
	"query": true, "count": true, "check": true,
}

// isReadAction reports whether a platform-prefixed action token is a read. It
// scans the verb segments after the platform prefix; a token with no read verb
// (e.g. slack_chat_post_message, gmail_send_email, gmail_create_email_draft) is
// treated as a write/send.
func isReadAction(token string) bool {
	parts := strings.Split(strings.ToLower(token), "_")
	for _, v := range parts[1:] { // skip the platform prefix
		if readVerbs[v] {
			return true
		}
	}
	return false
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
