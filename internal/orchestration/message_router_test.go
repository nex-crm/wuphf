package orchestration

import (
	"testing"
	"time"
)

func TestMessageRouter_ExtractSkills(t *testing.T) {
	mr := NewMessageRouter()

	tests := []struct {
		msg     string
		wantAny []string
	}{
		{"Can you research our competitors?", []string{"market-research", "competitive-analysis"}},
		{"Find me new leads and outreach targets", []string{"prospecting", "outreach"}},
		{"Fix the bug in the code", []string{"general", "planning"}},
		{"Help with SEO and keyword ranking", []string{"seo", "content-analysis"}},
		{"Build a landing page and backend API with a positioning brief", []string{"frontend", "backend", "positioning"}},
		{"Hello", nil},
	}

	for _, tt := range tests {
		skills := mr.ExtractSkills(tt.msg)
		if len(tt.wantAny) == 0 {
			if len(skills) != 0 {
				t.Errorf("msg %q: expected no skills, got %v", tt.msg, skills)
			}
			continue
		}
		found := false
		for _, want := range tt.wantAny {
			for _, got := range skills {
				if got == want {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("msg %q: expected one of %v in skills %v", tt.msg, tt.wantAny, skills)
		}
	}
}

func TestNewDirectiveGoesToTeamLead(t *testing.T) {
	mr := NewMessageRouter()
	mr.SetTeamLeadSlug("ceo")
	agents := []AgentInfo{
		{Slug: "ceo", Expertise: []string{"strategy"}},
		{Slug: "researcher", Expertise: []string{"market-research", "competitive-analysis"}},
		{Slug: "coder", Expertise: []string{"general", "planning"}},
	}

	// Even when a specialist matches, new directives should go to team-lead.
	result := mr.Route("Can you research our market?", agents)
	if result.Primary != "ceo" {
		t.Errorf("expected primary='ceo' (team-lead), got '%s'", result.Primary)
	}
	if result.IsFollowUp {
		t.Error("should not be a follow-up")
	}
	if !result.TeamLeadAware {
		t.Error("team-lead should be aware")
	}
}

func TestRouteSuggestsCollaboratorsForProductLaunchWork(t *testing.T) {
	mr := NewMessageRouter()
	mr.SetTeamLeadSlug("ceo")
	agents := []AgentInfo{
		{Slug: "ceo", Expertise: []string{"strategy", "delegation"}},
		{Slug: "fe", Expertise: []string{"frontend", "React", "CSS", "UI-UX", "components"}},
		{Slug: "be", Expertise: []string{"backend", "APIs", "databases"}},
		{Slug: "cmo", Expertise: []string{"positioning", "messaging", "go-to-market"}},
	}

	result := mr.Route("Build a landing page, backend API, and positioning brief for Nex.", agents)
	if result.Primary != "ceo" {
		t.Fatalf("expected primary='ceo', got %q", result.Primary)
	}
	want := map[string]bool{"fe": false, "be": false, "cmo": false}
	for _, slug := range result.Collaborators {
		if _, ok := want[slug]; ok {
			want[slug] = true
		}
	}
	for slug, found := range want {
		if !found {
			t.Fatalf("expected collaborator %q in %v", slug, result.Collaborators)
		}
	}
}

func TestFollowUpGoesToLastActive(t *testing.T) {
	mr := NewMessageRouter()
	mr.SetTeamLeadSlug("ceo")
	mr.mu.Lock()
	mr.recentThreads["fe"] = &threadContext{
		agentSlug:    "fe",
		lastActivity: time.Now(),
	}
	mr.mu.Unlock()

	agents := []AgentInfo{
		{Slug: "ceo", Expertise: []string{"strategy"}},
		{Slug: "fe", Expertise: []string{"frontend"}},
	}
	result := mr.Route("Also add a dark mode toggle", agents)
	if !result.IsFollowUp {
		t.Error("should be detected as follow-up")
	}
	if result.Primary != "fe" {
		t.Errorf("expected primary='fe' (last active), got '%s'", result.Primary)
	}
}

func TestExplicitMentionRoutes(t *testing.T) {
	mr := NewMessageRouter()
	mr.SetTeamLeadSlug("ceo")
	agents := []AgentInfo{
		{Slug: "ceo", Expertise: []string{"strategy"}},
		{Slug: "fe", Expertise: []string{"frontend"}},
		{Slug: "be", Expertise: []string{"backend"}},
	}

	result := mr.Route("@fe build the login page", agents)
	if result.Primary != "fe" {
		t.Errorf("expected primary='fe' (explicit @mention), got '%s'", result.Primary)
	}
	if result.IsFollowUp {
		t.Error("should not be a follow-up")
	}
}

func TestMessageRouter_RoutesToTeamLeadWhenNoSkills(t *testing.T) {
	mr := NewMessageRouter()
	result := mr.Route("Hello there", []AgentInfo{
		{Slug: "researcher", Expertise: []string{"market-research"}},
	})
	if result.Primary != "team-lead" {
		t.Errorf("expected team-lead, got %s", result.Primary)
	}
}

func TestMessageRouter_DetectsFollowUp(t *testing.T) {
	mr := NewMessageRouter()
	mr.mu.Lock()
	mr.recentThreads["researcher"] = &threadContext{
		agentSlug:    "researcher",
		lastActivity: time.Now(),
	}
	mr.mu.Unlock()

	agents := []AgentInfo{
		{Slug: "researcher", Expertise: []string{"market-research"}},
	}
	result := mr.Route("Also what about their pricing?", agents)
	if !result.IsFollowUp {
		t.Error("should be detected as follow-up")
	}
	if result.Primary != "researcher" {
		t.Errorf("expected researcher, got %s", result.Primary)
	}
}

func TestMessageRouter_FollowUpExpires(t *testing.T) {
	mr := NewMessageRouter()
	mr.followUpWindow = 10 * time.Millisecond
	mr.mu.Lock()
	mr.recentThreads["researcher"] = &threadContext{
		agentSlug:    "researcher",
		lastActivity: time.Now().Add(-100 * time.Millisecond),
	}
	mr.mu.Unlock()

	result := mr.Route("Also what about their pricing?", []AgentInfo{
		{Slug: "team-lead", Expertise: []string{}},
	})
	if result.IsFollowUp {
		t.Error("follow-up window should have expired")
	}
}

func TestRouteUsesConfiguredTeamLead(t *testing.T) {
	router := NewMessageRouter()
	router.SetTeamLeadSlug("ceo")
	router.RegisterAgent("ceo", []string{"strategy", "delegation"})
	router.RegisterAgent("pm", []string{"roadmap", "requirements"})

	agents := []AgentInfo{
		{Slug: "ceo", Expertise: []string{"strategy"}},
		{Slug: "pm", Expertise: []string{"roadmap"}},
	}

	result := router.Route("do something random", agents)
	if result.Primary != "ceo" {
		t.Errorf("expected primary='ceo', got '%s'", result.Primary)
	}
}

func TestMessageRouter_RecordActivity(t *testing.T) {
	mr := NewMessageRouter()
	mr.RecordAgentActivity("agent-x")
	mr.mu.Lock()
	tc, ok := mr.recentThreads["agent-x"]
	mr.mu.Unlock()
	if !ok {
		t.Fatal("activity should be recorded")
	}
	if time.Since(tc.lastActivity) > time.Second {
		t.Error("last activity should be recent")
	}
}
