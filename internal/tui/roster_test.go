package tui

import (
	"strings"
	"testing"
	"time"
)

func TestRosterUpdateAgents(t *testing.T) {
	r := NewRoster()

	agents := []AgentEntry{
		{Slug: "writer", Name: "Writer", Phase: "idle"},
		{Slug: "coder", Name: "Coder", Phase: "stream_llm"},
	}
	r.UpdateAgents(agents)

	if len(r.agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(r.agents))
	}
}

func TestRosterSpinnerActiveWhenAgentBusy(t *testing.T) {
	r := NewRoster()

	agents := []AgentEntry{
		{Slug: "coder", Name: "Coder", Phase: "build_context"},
	}
	r.UpdateAgents(agents)

	if !r.spinner.active {
		t.Fatal("expected spinner to be active when agent is in active phase")
	}
}

func TestRosterSpinnerInactiveWhenAllIdle(t *testing.T) {
	r := NewRoster()

	agents := []AgentEntry{
		{Slug: "coder", Name: "Coder", Phase: "idle"},
	}
	r.UpdateAgents(agents)

	if r.spinner.active {
		t.Fatal("expected spinner to be inactive when all agents are idle")
	}
}

func TestRosterSpinnerFrameAdvancesOnTick(t *testing.T) {
	r := NewRoster()
	agents := []AgentEntry{
		{Slug: "coder", Name: "Coder", Phase: "execute_tool"},
	}
	r.UpdateAgents(agents)

	initial := r.spinner.frame
	msg := SpinnerTickMsg{Time: time.Now()}
	r2, _ := r.Update(msg)

	if r2.spinner.frame == initial {
		t.Fatal("expected roster spinner frame to advance on tick")
	}
}

func TestRosterViewContainsHeader(t *testing.T) {
	r := NewRoster()
	view := r.View()
	if !strings.Contains(view, "TEAM") {
		t.Fatal("expected roster view to contain 'TEAM' header")
	}
}

func TestRosterViewContainsAgentName(t *testing.T) {
	r := NewRoster()
	r.UpdateAgents([]AgentEntry{
		{Slug: "hal", Name: "HAL 9000", Phase: "idle"},
	})
	view := r.View()
	if !strings.Contains(view, "HAL 9000") {
		t.Fatalf("expected roster view to contain agent name, got:\n%s", view)
	}
}

func TestRosterPhaseLabels(t *testing.T) {
	cases := []struct {
		phase string
		label string
	}{
		{"idle", "idle"},
		{"build_context", "ctx"},
		{"stream_llm", "llm"},
		{"execute_tool", "tool"},
		{"done", "done"},
		{"error", "err"},
		{"dead", "dead"},
		{"talking", "talk"},
		{"thinking", "think"},
		{"coding", "code"},
		{"listening", "listen"},
	}
	for _, tc := range cases {
		got := phaseShortLabel(tc.phase)
		if got != tc.label {
			t.Errorf("phaseShortLabel(%q) = %q, want %q", tc.phase, got, tc.label)
		}
	}
}

func TestPhaseLabels(t *testing.T) {
	tests := []struct {
		phase    string
		expected string
	}{
		{"build_context", "preparing"},
		{"stream_llm", "thinking"},
		{"execute_tool", "running tool"},
		{"idle", "idle"},
		{"done", "done"},
		{"error", "error"},
		{"dead", "exited"},
		{"talking", "talking"},
		{"thinking", "thinking"},
		{"coding", "coding"},
		{"listening", "listening"},
	}
	for _, tt := range tests {
		got := phaseLabel(tt.phase)
		if got != tt.expected {
			t.Errorf("phaseLabel(%q) = %q, want %q", tt.phase, got, tt.expected)
		}
	}
}

func TestRosterGossipActivityIcons(t *testing.T) {
	r := NewRoster()
	cases := []struct {
		phase string
		icon  string
	}{
		{"talking", "●"},
		{"thinking", "◐"},
		{"coding", "⚡"},
		{"listening", "◆"},
		{"dead", "✕"},
		{"idle", "○"},
	}
	for _, tc := range cases {
		got := r.agentIcon(tc.phase)
		if got != tc.icon {
			t.Errorf("agentIcon(%q) = %q, want %q", tc.phase, got, tc.icon)
		}
	}
}

func TestRosterUpdateFromGossip(t *testing.T) {
	r := NewRoster()
	r.UpdateAgents([]AgentEntry{
		{Slug: "ceo", Name: "CEO", Phase: "idle"},
		{Slug: "fe", Name: "FE", Phase: "idle"},
	})

	r.UpdateFromGossip("ceo", "text")
	if r.agents[0].Phase != "talking" {
		t.Errorf("expected ceo phase 'talking', got %q", r.agents[0].Phase)
	}

	r.UpdateFromGossip("fe", "tool_use")
	if r.agents[1].Phase != "coding" {
		t.Errorf("expected fe phase 'coding', got %q", r.agents[1].Phase)
	}

	r.SetAgentPhase("ceo", "dead")
	if r.agents[0].Phase != "dead" {
		t.Errorf("expected ceo phase 'dead', got %q", r.agents[0].Phase)
	}
}

func TestRosterSpinnerActiveForGossipPhases(t *testing.T) {
	r := NewRoster()
	r.UpdateAgents([]AgentEntry{
		{Slug: "ceo", Name: "CEO", Phase: "talking"},
	})
	if !r.spinner.active {
		t.Error("expected spinner active for 'talking' phase")
	}

	r.UpdateAgents([]AgentEntry{
		{Slug: "fe", Name: "FE", Phase: "coding"},
	})
	if !r.spinner.active {
		t.Error("expected spinner active for 'coding' phase")
	}
}
