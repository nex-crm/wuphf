package team

import (
	"path/filepath"
	"strings"
	"testing"
)

func newTestBrokerWithSkills(t *testing.T, skills []teamSkill) *Broker {
	t.Helper()
	b := NewBrokerAt(filepath.Join(t.TempDir(), "broker-state.json"))
	b.mu.Lock()
	b.skills = append(b.skills, skills...)
	b.mu.Unlock()
	return b
}

func TestFindSimilarSkillsLocked_SlugTier(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run for an inbound prospect", Status: "active"},
	})

	b.mu.Lock()
	defer b.mu.Unlock()

	// "enrich-prospect-inbox" is very close to "enrich-prospect" by slug.
	results := b.findSimilarSkillsLocked("enrich-prospect-inbox",
		"Pull inbox messages, extract signals, record facts")

	if len(results) == 0 {
		t.Fatal("expected tier 1 slug match, got none")
	}
	if results[0].Tier != 1 {
		t.Errorf("expected tier 1, got %d", results[0].Tier)
	}
	if results[0].Skill.Name != "enrich-prospect" {
		t.Errorf("expected match on enrich-prospect, got %q", results[0].Skill.Name)
	}
}

func TestFindSimilarSkillsLocked_DescTier(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run for an inbound prospect: fetch message, extract entities, record atomic facts", Status: "active"},
	})

	b.mu.Lock()
	defer b.mu.Unlock()

	// Different slug, but very similar description.
	results := b.findSimilarSkillsLocked("prospect-message-enrichment",
		"Full enrichment run for an inbound prospect message: extract entities and record atomic facts")

	if len(results) == 0 {
		t.Fatal("expected tier 2 description match, got none")
	}
	if results[0].Tier != 2 {
		t.Errorf("expected tier 2, got %d", results[0].Tier)
	}
}

func TestFindSimilarSkillsLocked_NoFalsePositives(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run for an inbound prospect", Status: "active"},
	})

	b.mu.Lock()
	defer b.mu.Unlock()

	// Completely different skill — should NOT match.
	results := b.findSimilarSkillsLocked("bad-data-self-heal",
		"Diagnose and resolve tasks blocked by unresolvable prospect data")

	if len(results) != 0 {
		t.Errorf("expected no match for unrelated skill, got %d results (tier %d)",
			len(results), results[0].Tier)
	}
}

func TestFindSimilarSkillsLocked_SkipsArchived(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run", Status: "archived"},
	})

	b.mu.Lock()
	defer b.mu.Unlock()

	results := b.findSimilarSkillsLocked("enrich-prospect-inbox",
		"Full enrichment run for inbox")

	if len(results) != 0 {
		t.Errorf("expected no match against archived skill, got %d", len(results))
	}
}

func TestFindSimilarSkillsLocked_SkipsExactSlug(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run", Status: "active"},
	})

	b.mu.Lock()
	defer b.mu.Unlock()

	// Exact same slug — handled by findSkillByNameLocked, not the semantic dedup.
	results := b.findSimilarSkillsLocked("enrich-prospect",
		"Full enrichment run for an inbound prospect")

	if len(results) != 0 {
		t.Errorf("expected no match for exact slug (handled elsewhere), got %d", len(results))
	}
}

func TestFindSimilarSkillsLocked_Disabled(t *testing.T) {
	t.Setenv("WUPHF_SKILL_DEDUP_ENABLED", "0")

	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run", Status: "active"},
	})

	b.mu.Lock()
	defer b.mu.Unlock()

	results := b.findSimilarSkillsLocked("enrich-prospect-inbox",
		"Full enrichment run for inbox")

	if len(results) != 0 {
		t.Errorf("expected no match when dedup disabled, got %d", len(results))
	}
}

func TestBuildExistingSkillsSummary(t *testing.T) {
	skills := []teamSkill{
		{Name: "enrich-prospect", Description: "Full enrichment run", Status: "active"},
		{Name: "inbox-triage", Description: "Pre-filter incoming messages", Status: "active"},
		{Name: "old-skill", Description: "Archived skill", Status: "archived"},
	}

	summary := buildExistingSkillsSummary(skills, 2048)

	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if !strings.Contains(summary, "enrich-prospect") {
		t.Error("expected summary to contain enrich-prospect")
	}
	if !strings.Contains(summary, "inbox-triage") {
		t.Error("expected summary to contain inbox-triage")
	}
	if strings.Contains(summary, "old-skill") {
		t.Error("expected summary to exclude archived skill")
	}
}

func TestBuildExistingSkillsSummary_ContainsEnhanceInstruction(t *testing.T) {
	skills := []teamSkill{
		{Name: "create-pitch-deck", Description: "Create a pitch deck", Status: "active"},
	}
	summary := buildExistingSkillsSummary(skills, 2048)
	if !strings.Contains(summary, "enhance") {
		t.Error("expected summary to contain enhance instruction")
	}
}

func TestEnhanceSkillLocked(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "create-pitch-deck", Description: "Create a pitch deck", Content: "Generic pitch deck steps", Status: "active"},
	})

	b.mu.Lock()
	enhanced, err := b.enhanceSkillLocked("create-pitch-deck",
		"Generic pitch deck steps\n\n## SaaS variant\n\nInclude ARR metrics and churn analysis.",
		"Create a pitch deck with SaaS-specific details",
		"create-pitch-deck-for-saas")
	b.mu.Unlock()

	if err != nil {
		t.Fatalf("enhance failed: %v", err)
	}
	if enhanced == nil {
		t.Fatal("expected enhanced skill, got nil")
	}
	if !strings.Contains(enhanced.Content, "SaaS variant") {
		t.Error("expected enhanced content to contain SaaS variant details")
	}
	if !strings.Contains(enhanced.Description, "SaaS") {
		t.Error("expected description to be updated with more specific version")
	}
}

func TestEnhanceSkillLocked_SkillNotFound(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{})

	b.mu.Lock()
	_, err := b.enhanceSkillLocked("nonexistent", "content", "desc", "")
	b.mu.Unlock()

	if err == nil {
		t.Error("expected error for nonexistent skill")
	}
}

func TestEnhanceSkillLocked_EmptyContent(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "my-skill", Description: "A skill", Content: "Original", Status: "active"},
	})

	b.mu.Lock()
	_, err := b.enhanceSkillLocked("my-skill", "", "new desc", "")
	b.mu.Unlock()

	if err == nil {
		t.Error("expected error for empty content")
	}
}

func TestEnhanceSkillLocked_KeepsShorterDescription(t *testing.T) {
	b := newTestBrokerWithSkills(t, []teamSkill{
		{Name: "my-skill", Description: "A longer and more detailed description of the skill", Content: "Original", Status: "active"},
	})

	b.mu.Lock()
	enhanced, err := b.enhanceSkillLocked("my-skill", "New content", "Short", "")
	b.mu.Unlock()

	if err != nil {
		t.Fatalf("enhance failed: %v", err)
	}
	if enhanced.Description != "A longer and more detailed description of the skill" {
		t.Errorf("expected original (longer) description to be kept, got %q", enhanced.Description)
	}
}

func TestParseSkillJSONFull_EnhanceField(t *testing.T) {
	raw := `{"is_skill": true, "enhance": "create-pitch-deck", "name": "create-pitch-deck", "description": "Create a pitch deck with SaaS details", "body": "merged body"}`
	result := parseSkillJSONFull(raw)

	if result.Err != nil {
		t.Fatalf("parse error: %v", result.Err)
	}
	if !result.IsSkill {
		t.Error("expected is_skill=true")
	}
	if result.Enhance != "create-pitch-deck" {
		t.Errorf("expected enhance=create-pitch-deck, got %q", result.Enhance)
	}
	if result.Body != "merged body" {
		t.Errorf("expected body='merged body', got %q", result.Body)
	}
}

func TestParseSkillJSONFull_NoEnhance(t *testing.T) {
	raw := `{"is_skill": true, "name": "new-skill", "description": "A new skill", "body": "skill body"}`
	result := parseSkillJSONFull(raw)

	if result.Err != nil {
		t.Fatalf("parse error: %v", result.Err)
	}
	if result.Enhance != "" {
		t.Errorf("expected empty enhance, got %q", result.Enhance)
	}
}

func TestBuildExistingSkillsSummary_Truncation(t *testing.T) {
	skills := make([]teamSkill, 100)
	for i := range skills {
		skills[i] = teamSkill{
			Name:        "skill-" + string(rune('a'+i%26)) + "-" + string(rune('a'+i/26)),
			Description: "A fairly long description that takes up space in the summary output buffer",
			Status:      "active",
		}
	}

	summary := buildExistingSkillsSummary(skills, 512)
	if len(summary) > 600 { // some slack for the header + truncation marker
		t.Errorf("expected summary capped near 512 bytes, got %d", len(summary))
	}
	if !strings.Contains(summary, "truncated") {
		t.Error("expected truncation marker")
	}
}
