package agent

import (
	"strings"
	"testing"
)

func TestBuildTeamLeadPrompt(t *testing.T) {
	lead := AgentConfig{Slug: "ceo", Name: "CEO", Expertise: []string{"strategy"}}
	team := []AgentConfig{
		{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend", "React"}},
		{Slug: "be", Name: "Backend Engineer", Expertise: []string{"backend", "APIs"}},
	}
	prompt := BuildTeamLeadPrompt(lead, team, "Founding Team")
	if !strings.Contains(prompt, "@fe") {
		t.Error("expected prompt to contain @fe")
	}
	if !strings.Contains(prompt, "@be") {
		t.Error("expected prompt to contain @be")
	}
	if !strings.Contains(prompt, "do NOT need to forward") {
		t.Error("expected broadcast instructions in prompt")
	}
	if !strings.Contains(prompt, "Never invent external teammates") {
		t.Error("expected prompt to forbid invented teammates")
	}
	if !strings.Contains(prompt, "Never claim specialist work is already complete") {
		t.Error("expected prompt to forbid fake completion")
	}
	if !strings.Contains(prompt, "Do not use headings, bullets, markdown, JSON, YAML, metadata") {
		t.Error("expected prompt to forbid verbose metadata-heavy output")
	}
}

func TestBuildSpecialistPrompt(t *testing.T) {
	specialist := AgentConfig{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend", "React"}}
	prompt := BuildSpecialistPrompt(specialist)
	if !strings.Contains(prompt, "Frontend Engineer") {
		t.Error("expected specialist name in prompt")
	}
	if !strings.Contains(prompt, "frontend") {
		t.Error("expected expertise in prompt")
	}
}

func TestBuildTeamLeadPromptIncludesConcludeHandoff(t *testing.T) {
	prompt := BuildTeamLeadPrompt(
		AgentConfig{Slug: "ceo", Name: "CEO"},
		[]AgentConfig{{Slug: "fe", Name: "FE"}},
		"TestPack",
	)
	if !strings.Contains(prompt, "team_conclude") {
		t.Fatal("CEO prompt should mention team_conclude")
	}
	if !strings.Contains(prompt, "team_handoff") {
		t.Fatal("CEO prompt should mention team_handoff")
	}
}

func TestBuildSpecialistPromptIncludesConcludeHandoff(t *testing.T) {
	prompt := BuildSpecialistPrompt(AgentConfig{Slug: "fe", Name: "FE", Expertise: []string{"frontend"}})
	if !strings.Contains(prompt, "team_conclude") {
		t.Fatal("specialist prompt should mention team_conclude")
	}
	if !strings.Contains(prompt, "team_handoff") {
		t.Fatal("specialist prompt should mention team_handoff")
	}
}

func TestBuildTeamLeadPromptMentionsAllAgents(t *testing.T) {
	lead := AgentConfig{Slug: "ceo", Name: "CEO"}
	team := []AgentConfig{
		{Slug: "pm", Name: "PM", Expertise: []string{"roadmap"}},
		{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend"}},
		{Slug: "be", Name: "Backend Engineer", Expertise: []string{"backend"}},
	}
	prompt := BuildTeamLeadPrompt(lead, team, "Founding Team")
	for _, a := range team {
		if !strings.Contains(prompt, "@"+a.Slug) {
			t.Errorf("expected prompt to mention @%s", a.Slug)
		}
	}
}
