package agent

import "testing"

func TestPacksRegistered(t *testing.T) {
	if len(Packs) != 4 {
		t.Fatalf("expected 4 packs, got %d", len(Packs))
	}
	founding := GetPack("founding-team")
	if founding == nil {
		t.Fatal("founding-team pack not found")
	}
	if founding.LeadSlug != "ceo" {
		t.Errorf("expected lead slug 'ceo', got '%s'", founding.LeadSlug)
	}
	if len(founding.Agents) != 8 {
		t.Errorf("expected 8 agents in founding team, got %d", len(founding.Agents))
	}
	foundAI := false
	for _, a := range founding.Agents {
		if a.Slug == "ai" && a.Name == "AI Engineer" {
			foundAI = true
			break
		}
	}
	if !foundAI {
		t.Error("expected founding team to include AI Engineer")
	}
}

func TestGetPackReturnsNilForUnknown(t *testing.T) {
	if GetPack("nonexistent") != nil {
		t.Error("expected nil for unknown pack")
	}
}

func TestAllPacksHaveLeadInAgents(t *testing.T) {
	for _, pack := range Packs {
		found := false
		for _, a := range pack.Agents {
			if a.Slug == pack.LeadSlug {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("pack %s: lead slug %s not found in agents", pack.Slug, pack.LeadSlug)
		}
	}
}

func TestCodingTeamPack(t *testing.T) {
	p := GetPack("coding-team")
	if p == nil {
		t.Fatal("coding-team pack not found")
	}
	if p.LeadSlug != "tech-lead" {
		t.Errorf("expected lead 'tech-lead', got '%s'", p.LeadSlug)
	}
	if len(p.Agents) != 4 {
		t.Errorf("expected 4 agents, got %d", len(p.Agents))
	}
}

func TestLeadGenAgencyPack(t *testing.T) {
	p := GetPack("lead-gen-agency")
	if p == nil {
		t.Fatal("lead-gen-agency pack not found")
	}
	if p.LeadSlug != "ae" {
		t.Errorf("expected lead 'ae', got '%s'", p.LeadSlug)
	}
	if len(p.Agents) != 4 {
		t.Errorf("expected 4 agents, got %d", len(p.Agents))
	}
}
