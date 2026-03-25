package orchestration

import "testing"

func TestExtractDelegations(t *testing.T) {
	d := NewDelegator(3)
	text := "I'll have @research analyze the competitive landscape while @content drafts the positioning document."

	delegations := d.ExtractDelegations(text, []string{"research", "content", "sdr"})

	if len(delegations) != 2 {
		t.Fatalf("expected 2 delegations, got %d", len(delegations))
	}
	if delegations[0].AgentSlug != "research" {
		t.Errorf("expected first delegation to 'research', got '%s'", delegations[0].AgentSlug)
	}
	if delegations[1].AgentSlug != "content" {
		t.Errorf("expected second delegation to 'content', got '%s'", delegations[1].AgentSlug)
	}
}

func TestExtractDelegationsIgnoresUnknownSlugs(t *testing.T) {
	d := NewDelegator(3)
	text := "Let me ask @nonexistent to handle this and @research to investigate."
	delegations := d.ExtractDelegations(text, []string{"research"})
	if len(delegations) != 1 {
		t.Fatalf("expected 1 delegation, got %d", len(delegations))
	}
	if delegations[0].AgentSlug != "research" {
		t.Errorf("expected 'research', got '%s'", delegations[0].AgentSlug)
	}
}

func TestExtractDelegationsNone(t *testing.T) {
	d := NewDelegator(3)
	text := "I'll handle this myself. The strategy looks solid."
	delegations := d.ExtractDelegations(text, []string{"research", "content"})
	if len(delegations) != 0 {
		t.Fatalf("expected 0 delegations, got %d", len(delegations))
	}
}

func TestExtractDelegationsSentenceContext(t *testing.T) {
	d := NewDelegator(3)
	text := "First, @fe should build the login page. Then @be needs to create the API endpoints. Finally @qa will write the test suite."
	delegations := d.ExtractDelegations(text, []string{"fe", "be", "qa"})
	if len(delegations) != 3 {
		t.Fatalf("expected 3 delegations, got %d", len(delegations))
	}
	// Each delegation should contain relevant sentence context
	for _, d := range delegations {
		if d.Task == "" {
			t.Errorf("delegation to %s has empty task", d.AgentSlug)
		}
	}
}

func TestConcurrencyLimit(t *testing.T) {
	d := NewDelegator(2) // max 2 concurrent
	text := "@fe builds UI. @be builds API. @qa writes tests."
	delegations := d.ExtractDelegations(text, []string{"fe", "be", "qa"})
	// All 3 should be extracted (limit is enforced at execution time, not extraction)
	if len(delegations) != 3 {
		t.Fatalf("expected 3 delegations, got %d", len(delegations))
	}
}

func TestApplyLimitEnforced(t *testing.T) {
	d := NewDelegator(1)
	delegations := []Delegation{
		{AgentSlug: "fe", Task: "build UI"},
		{AgentSlug: "be", Task: "build API"},
		{AgentSlug: "qa", Task: "write tests"},
	}
	immediate, queued := d.ApplyLimit(delegations)
	if len(immediate) != 1 {
		t.Fatalf("expected 1 immediate, got %d", len(immediate))
	}
	if immediate[0].AgentSlug != "fe" {
		t.Errorf("expected immediate[0]='fe', got '%s'", immediate[0].AgentSlug)
	}
	if len(queued) != 2 {
		t.Fatalf("expected 2 queued, got %d", len(queued))
	}
	if queued[0].AgentSlug != "be" {
		t.Errorf("expected queued[0]='be', got '%s'", queued[0].AgentSlug)
	}
	if queued[1].AgentSlug != "qa" {
		t.Errorf("expected queued[1]='qa', got '%s'", queued[1].AgentSlug)
	}
}

func TestApplyLimitUnderLimit(t *testing.T) {
	d := NewDelegator(5)
	delegations := []Delegation{
		{AgentSlug: "fe", Task: "build UI"},
		{AgentSlug: "be", Task: "build API"},
	}
	immediate, queued := d.ApplyLimit(delegations)
	if len(immediate) != 2 {
		t.Fatalf("expected 2 immediate, got %d", len(immediate))
	}
	if len(queued) != 0 {
		t.Fatalf("expected 0 queued, got %d", len(queued))
	}
}
