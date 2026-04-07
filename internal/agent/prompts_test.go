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
	if !strings.Contains(prompt, "delegate") || !strings.Contains(prompt, "MUST delegate") {
		t.Error("expected delegation instructions in prompt")
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
	if !strings.Contains(prompt, "you are needed") {
		t.Error("expected directive 'you are needed' language in prompt")
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

func TestBuildOfficeCompactionPrompt(t *testing.T) {
	prompt := BuildOfficeCompactionPrompt("PM decided to ship the launch page on Friday.")
	if !strings.Contains(prompt, "Office Insight") {
		t.Fatal("expected Office Insight instructions in compaction prompt")
	}
	if !strings.Contains(prompt, "mission summary") {
		t.Fatal("expected mission-summary guidance in compaction prompt")
	}
	if !strings.Contains(prompt, "key decisions, current blockers, and open follow-ups") {
		t.Fatal("expected compaction sections in prompt")
	}
	if !strings.Contains(prompt, "PM decided to ship the launch page on Friday.") {
		t.Fatal("expected archived thread content in prompt")
	}
}

func TestBuildCompactionPrompt(t *testing.T) {
	prompt := BuildCompactionPrompt()
	for _, want := range []string{"mission", "key decisions", "current blockers", "Office Insight"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected compaction prompt to mention %q", want)
		}
	}
}
