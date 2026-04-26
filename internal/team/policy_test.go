package team

import (
	"strings"
	"testing"
)

func TestNewOfficePolicyDefaults(t *testing.T) {
	p := newOfficePolicy("human_directed", "Always ask before deploying")
	if p.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if !p.Active {
		t.Fatal("expected active by default")
	}
	if p.Source != "human_directed" {
		t.Fatalf("expected human_directed source, got %q", p.Source)
	}
	if p.Rule != "Always ask before deploying" {
		t.Fatalf("unexpected rule: %q", p.Rule)
	}
	if p.CreatedAt == "" {
		t.Fatal("expected created_at timestamp")
	}
}

func TestNewOfficePolicyDefaultsSourceWhenEmpty(t *testing.T) {
	p := newOfficePolicy("", "Work autonomously")
	if p.Source != "human_directed" {
		t.Fatalf("expected default source human_directed, got %q", p.Source)
	}
}

func TestNewOfficePolicyTrimsRule(t *testing.T) {
	p := newOfficePolicy("auto_detected", "  User prefers short answers  ")
	if strings.HasPrefix(p.Rule, " ") || strings.HasSuffix(p.Rule, " ") {
		t.Fatalf("expected trimmed rule, got %q", p.Rule)
	}
}

func TestBrokerRecordAndListPolicies(t *testing.T) {
	b := newTestBroker(t)
	p1, err := b.RecordPolicy("human_directed", "Always ask before deploying to production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p1.ID == "" {
		t.Fatal("expected non-empty policy ID")
	}
	p2, err := b.RecordPolicy("auto_detected", "User prefers brief responses")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p2.ID == p1.ID {
		t.Fatal("expected distinct IDs")
	}

	list := b.ListPolicies()
	if len(list) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(list))
	}
}

func TestBrokerRecordPolicyDeduplicates(t *testing.T) {
	b := newTestBroker(t)
	const rule = "Work autonomously without approval"
	_, err := b.RecordPolicy("human_directed", rule)
	if err != nil {
		t.Fatalf("first record: %v", err)
	}
	_, err = b.RecordPolicy("human_directed", rule)
	if err != nil {
		t.Fatalf("second record: %v", err)
	}
	if got := len(b.ListPolicies()); got != 1 {
		t.Fatalf("expected 1 after dedup, got %d", got)
	}
}

func TestBrokerDeletePolicyDeactivates(t *testing.T) {
	b := newTestBroker(t)
	p, err := b.RecordPolicy("human_directed", "Ask before sending external emails")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(b.ListPolicies()) != 1 {
		t.Fatal("expected 1 active policy before delete")
	}
	// Deactivate via internal field (mirrors handlePolicies DELETE path).
	b.mu.Lock()
	for i, bp := range b.policies {
		if bp.ID == p.ID {
			b.policies[i].Active = false
		}
	}
	b.mu.Unlock()

	if len(b.ListPolicies()) != 0 {
		t.Fatal("expected 0 active policies after deactivation")
	}
}

func TestBrokerRecordPolicyEmptyRuleErrors(t *testing.T) {
	b := newTestBroker(t)
	_, err := b.RecordPolicy("human_directed", "  ")
	if err == nil {
		t.Fatal("expected error for empty rule")
	}
}
