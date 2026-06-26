package team

import (
	"strings"
	"testing"
)

func TestExtractPlaybookPolicyRules(t *testing.T) {
	article := `---
name: qualify-inbound-leads
description: Qualify inbound leads.
---
# Qualify inbound leads

## Steps

- Not a rule: this bullet lives outside the Rules section.

## Rules

- Always CC the CSM on renewal emails
- Never send pricing before the demo call
  - nested detail bullet must not become its own policy
-
- ` + strings.Repeat("x", maxCompiledPolicyRuneLen+1) + `

## Notes

- Also not a rule.

## Policies

* Escalate red accounts to the CEO same-day
`
	got := extractPlaybookPolicyRules(article)
	want := []string{
		"Always CC the CSM on renewal emails",
		"Never send pricing before the demo call",
		"Escalate red accounts to the CEO same-day",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d rules, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rule %d: want %q, got %q", i, want[i], got[i])
		}
	}
}

func TestRecordPlaybookPolicies_GatesAndDedupes(t *testing.T) {
	b := newTestBroker(t)
	content := "# P\n\n## Rules\n\n- Always log external sends\n"

	// Non-playbook paths never compile policies.
	if n := b.recordPlaybookPolicies("team/customers/acme.md", content, []string{"eng"}); n != 0 {
		t.Fatalf("non-playbook path must be gated, recorded %d", n)
	}

	if n := b.recordPlaybookPolicies("team/playbooks/sends.md", content, []string{"eng"}); n != 1 {
		t.Fatalf("expected 1 recorded policy, got %d", n)
	}
	policies := b.ListPolicies()
	if len(policies) != 1 || policies[0].Source != "auto_detected" ||
		len(policies[0].Agents) != 1 || policies[0].Agents[0] != "eng" {
		t.Fatalf("unexpected compiled policy: %+v", policies)
	}

	// Re-compiling the same playbook (or the same rule from another one)
	// widens scope instead of duplicating.
	if n := b.recordPlaybookPolicies("team/playbooks/other.md", content, []string{"ae"}); n != 1 {
		t.Fatalf("expected the dedupe path to still count as recorded, got %d", n)
	}
	policies = b.ListPolicies()
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy after dedupe, got %d", len(policies))
	}
	if len(policies[0].Agents) != 2 {
		t.Fatalf("expected widened scope [ae eng], got %v", policies[0].Agents)
	}
}
