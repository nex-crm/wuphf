package orchestration

import (
	"testing"
)

func TestSimilarity(t *testing.T) {
	if similarity("research", "research") != 1.0 {
		t.Error("identical strings should return 1.0")
	}
	if similarity("research", "xyz") >= 0.3 {
		t.Error("unrelated strings should score < 0.3")
	}
	if similarity("prospecting", "prospect") < 0.5 {
		t.Error("similar strings should score > 0.5")
	}
}

func TestTaskRouter_RegisterUnregister(t *testing.T) {
	r := NewTaskRouter()
	r.RegisterAgent("alice", []SkillDeclaration{{Name: "research", Proficiency: 0.9}})
	if _, ok := r.agents["alice"]; !ok {
		t.Fatal("agent should be registered")
	}
	r.UnregisterAgent("alice")
	if _, ok := r.agents["alice"]; ok {
		t.Fatal("agent should be removed")
	}
}

func TestTaskRouter_ScoreMatch(t *testing.T) {
	r := NewTaskRouter()
	r.RegisterAgent("alice", []SkillDeclaration{
		{Name: "market-research", Proficiency: 0.9},
		{Name: "competitive-analysis", Proficiency: 0.8},
	})

	task := TaskDefinition{
		ID:             "t1",
		RequiredSkills: []string{"market-research"},
	}
	score := r.ScoreMatch("alice", task)
	if score < 0.3 {
		t.Errorf("expected score >= 0.3, got %f", score)
	}

	// Unknown agent returns 0.
	if r.ScoreMatch("unknown", task) != 0 {
		t.Error("unknown agent should return 0")
	}

	// Empty required skills returns 0.
	emptyTask := TaskDefinition{ID: "t2"}
	if r.ScoreMatch("alice", emptyTask) != 0 {
		t.Error("empty required skills should return 0")
	}
}

func TestTaskRouter_FindBestAgent(t *testing.T) {
	r := NewTaskRouter()
	r.RegisterAgent("alice", []SkillDeclaration{{Name: "prospecting", Proficiency: 0.9}})
	r.RegisterAgent("bob", []SkillDeclaration{{Name: "outreach", Proficiency: 0.7}})

	task := TaskDefinition{
		ID:             "t1",
		RequiredSkills: []string{"prospecting"},
	}
	best := r.FindBestAgent(task)
	if best == nil {
		t.Fatal("expected a best agent")
	}
	if best.AgentSlug != "alice" {
		t.Errorf("expected alice, got %s", best.AgentSlug)
	}

	// No match returns nil.
	noMatchTask := TaskDefinition{
		ID:             "t2",
		RequiredSkills: []string{"zzz-nonexistent"},
	}
	if r.FindBestAgent(noMatchTask) != nil {
		t.Error("expected nil for unmatched task")
	}
}

func TestTaskRouter_FindCapableAgents_Sorted(t *testing.T) {
	r := NewTaskRouter()
	r.RegisterAgent("alice", []SkillDeclaration{{Name: "seo", Proficiency: 0.5}})
	r.RegisterAgent("bob", []SkillDeclaration{{Name: "seo", Proficiency: 1.0}})

	task := TaskDefinition{
		ID:             "t1",
		RequiredSkills: []string{"seo"},
	}
	results := r.FindCapableAgents(task)
	if len(results) != 2 {
		t.Fatalf("expected 2 capable agents, got %d", len(results))
	}
	// Should be sorted descending — bob has higher proficiency.
	if results[0].Score < results[1].Score {
		t.Error("results should be sorted descending by score")
	}
}
