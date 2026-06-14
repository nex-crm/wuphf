package team

// policy_compile.go — B3 policy compilation (core-loop step 7.3 + 11).
//
// When the skill compile funnel (skill_scanner.go) compiles a playbook
// article, it ALSO extracts the playbook's enforcement one-liners into
// officePolicy records. The source is deterministic: a "## Rules" or
// "## Policies" section in the playbook article, one bullet = one policy
// candidate (policies are always single-threaded — one atomic rule per
// record). Candidates that match an existing policy by normalized text are
// absorbed into it (scope widened, never narrowed) instead of minting a
// duplicate.
//
// Assignment: compiled policies get the SAME relevant-agent assignment the
// compiled skill gets (today: the office roster at creation; the human/CEO
// narrows afterwards via /policies/{id}/assign|unassign).

import (
	"log/slog"
	"strings"
)

// maxCompiledPolicyRuneLen rejects bullets too long to be an atomic
// one-liner. A constitution line should fit in a sentence or two.
const maxCompiledPolicyRuneLen = 240

// playbookPolicySectionHeadings are the H2 headings whose bullets compile
// into policies. Matched case-insensitively on the trimmed line.
var playbookPolicySectionHeadings = []string{"## rules", "## policies"}

// extractPlaybookPolicyRules parses the policy candidates out of a playbook
// article: every top-level "-" or "*" bullet inside a "## Rules" or
// "## Policies" section. One bullet = one candidate. Deterministic — no LLM.
func extractPlaybookPolicyRules(content string) []string {
	var rules []string
	inSection := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			inSection = false
			lower := strings.ToLower(trimmed)
			for _, h := range playbookPolicySectionHeadings {
				if lower == h {
					inSection = true
					break
				}
			}
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			inSection = false
			continue
		}
		if !inSection {
			continue
		}
		// Top-level bullets only: indented (nested) bullets are detail
		// under a rule, not separate atomic rules.
		if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
			continue
		}
		rule := strings.TrimSpace(line[2:])
		if rule == "" {
			continue
		}
		if len([]rune(rule)) > maxCompiledPolicyRuneLen {
			slog.Debug("policy_compile: skipping over-long rule bullet",
				"runes", len([]rune(rule)))
			continue
		}
		rules = append(rules, rule)
	}
	return rules
}

// recordPlaybookPolicies compiles a playbook article's "## Rules" /
// "## Policies" bullets into officePolicy records with per-agent
// assignment. articlePath gates the call to the playbooks subtree; agents
// is the compiled skill's OwnerAgents (the same relevant-agent assignment).
// Existing policies with the same normalized rule text are reactivated and
// widened, never duplicated. Failures are logged, never fatal — policy
// compilation is additive intelligence riding the skill compile pass.
func (b *Broker) recordPlaybookPolicies(articlePath, content string, agents []string) int {
	if !strings.HasPrefix(articlePath, playbooksDirPrefix) {
		return 0
	}
	rules := extractPlaybookPolicyRules(content)
	recorded := 0
	for _, rule := range rules {
		if _, err := b.RecordPolicyScoped("auto_detected", rule, agents); err != nil {
			slog.Warn("policy_compile: record policy failed",
				"article", articlePath, "err", err)
			continue
		}
		recorded++
	}
	if recorded > 0 {
		slog.Info("policy_compile: compiled playbook rules into policies",
			"article", articlePath, "rules", recorded, "agents", len(agents))
	}
	return recorded
}
