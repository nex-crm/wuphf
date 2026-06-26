package team

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// officeSignal is an internal audit record used by watchdog monitoring and
// relay event tracking. It is NOT used for policy generation.
type officeSignal struct {
	ID            string
	Source        string
	Kind          string
	Title         string
	Content       string
	Confidence    string
	Urgency       string
	Channel       string
	Owner         string
	RequiresHuman bool
	Blocking      bool
}

// officePolicy is a named operating rule for the office. Policies are always
// single-threaded: one atomic rule per record (core-loop step 11).
// Source is either "human_directed" (explicitly set by the human via message
// or command) or "auto_detected" (compiled from a playbook's ## Rules section
// or otherwise inferred from a recurring working pattern).
type officePolicy struct {
	ID        string `json:"id"`
	Source    string `json:"source"` // "human_directed" | "auto_detected"
	Rule      string `json:"rule"`   // plain-English description of the rule
	Active    bool   `json:"active"`
	CreatedAt string `json:"created_at"`
	// Agents lists the agent slugs this policy is assigned to (core-loop
	// step 8/B3). Empty or nil means the policy applies to ALL agents —
	// today's behavior, preserved for every pre-existing record. Wire
	// shape: additive `agents` key, omitted when empty.
	Agents []string `json:"agents,omitempty"`
}

func newOfficePolicy(source, rule string) officePolicy {
	rule = strings.TrimSpace(rule)
	source = strings.TrimSpace(source)
	if source == "" {
		source = "human_directed"
	}
	return officePolicy{
		ID:        fmt.Sprintf("policy-%d", time.Now().UnixNano()),
		Source:    source,
		Rule:      rule,
		Active:    true,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// normalizePolicyAgents canonicalizes an agent-scope list: trims, drops
// empties, dedupes, and sorts so persisted scope (and the prompt blocks
// derived from it) are deterministic. Returns nil for an effectively-empty
// list, which is the "applies to all agents" representation.
func normalizePolicyAgents(agents []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		slug := strings.ToLower(strings.TrimSpace(a))
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// policyAppliesToAgent reports whether the policy is in force for the given
// agent slug. Nil/empty Agents means everyone (legacy + human-feedback
// default); a non-empty list scopes the policy to exactly those agents.
func policyAppliesToAgent(p officePolicy, slug string) bool {
	if len(p.Agents) == 0 {
		return true
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	for _, a := range p.Agents {
		if a == slug {
			return true
		}
	}
	return false
}

// normalizePolicyRuleText collapses whitespace and lowercases a rule for
// duplicate detection. "Simple normalized-text match" — the policy analogue
// of the skill dedup gate's tier-1 check, deliberately cheap.
func normalizePolicyRuleText(rule string) string {
	return strings.ToLower(strings.Join(strings.Fields(rule), " "))
}
