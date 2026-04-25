package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/team"
)

func TestTeamSkillCreateRegisteredForSpecialists(t *testing.T) {
	for _, tc := range []struct {
		name     string
		channel  string
		oneOnOne bool
	}{
		{name: "office", channel: "general"},
		{name: "dm", channel: "dm-workflow-architect"},
		{name: "one-on-one", channel: "dm-workflow-architect", oneOnOne: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names := listRegisteredTools(t, tc.channel, tc.oneOnOne)
			if !slices.Contains(names, "team_skill_create") {
				t.Fatalf("expected team_skill_create for specialist in %s mode; tools=%v", tc.name, names)
			}
		})
	}
}

// TestHandleTeamSkillRunBumpsUsageAndLogsInvocation verifies that when an
// agent calls team_skill_run through the MCP, the broker bumps the skill's
// UsageCount and a skill_invocation message lands in the channel attributed
// to the calling agent (not "you").
func TestHandleTeamSkillRunBumpsUsageAndLogsInvocation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	// Seed a skill the agent can invoke.
	b.SeedDefaultSkills([]agent.PackSkillSpec{{
		Name:        "investigate",
		Title:       "Investigate a Bug",
		Description: "Systematic debugging with root cause analysis.",
		Trigger:     "When a bug or error is reported",
		Tags:        []string{"engineering", "debugging"},
		Content:     "Step 1: Reproduce. Step 2: Isolate. Step 3: Root cause. Step 4: Fix.",
	}})

	// Agent calls team_skill_run.
	res, _, err := handleTeamSkillRun(context.Background(), nil, TeamSkillRunArgs{
		SkillName: "investigate",
		MySlug:    "eng",
		Channel:   "general",
	})
	if err != nil {
		t.Fatalf("skill run: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected successful tool result, got %+v", res)
	}

	// Fetch skills via broker HTTP and confirm UsageCount bumped to 1.
	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/skills?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get skills: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Skills []struct {
			Name       string `json:"name"`
			UsageCount int    `json:"usage_count"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	if len(result.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %+v", result.Skills)
	}
	if got := result.Skills[0].UsageCount; got != 1 {
		t.Fatalf("expected usage_count=1 after one invocation, got %d", got)
	}

	// Confirm a skill_invocation message was appended, attributed to the
	// calling agent slug (not "you"), in the requested channel.
	var sawInvocation bool
	for _, msg := range b.Messages() {
		if msg.Kind != "skill_invocation" {
			continue
		}
		if msg.From != "eng" {
			t.Fatalf("expected invocation attributed to eng, got From=%q", msg.From)
		}
		if msg.Channel != "general" {
			t.Fatalf("expected invocation in channel=general, got %q", msg.Channel)
		}
		sawInvocation = true
	}
	if !sawInvocation {
		t.Fatalf("expected a skill_invocation message in broker; messages=%+v", b.Messages())
	}

	// Second invocation should bump UsageCount to 2, proving the tool is
	// re-entrant and not a no-op on repeat calls.
	if _, _, err := handleTeamSkillRun(context.Background(), nil, TeamSkillRunArgs{
		SkillName: "investigate",
		MySlug:    "eng",
		Channel:   "general",
	}); err != nil {
		t.Fatalf("second skill run: %v", err)
	}

	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get skills (2nd): %v", err)
	}
	defer resp2.Body.Close()
	var result2 struct {
		Skills []struct {
			Name       string `json:"name"`
			UsageCount int    `json:"usage_count"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&result2); err != nil {
		t.Fatalf("decode skills (2nd): %v", err)
	}
	if got := result2.Skills[0].UsageCount; got != 2 {
		t.Fatalf("expected usage_count=2 after two invocations, got %d", got)
	}
}

// TestHandleTeamSkillRunMissingSkillReturnsToolError verifies that calling
// team_skill_run with a skill that doesn't exist returns a tool-level error
// (so the agent sees the failure) rather than panicking.
func TestHandleTeamSkillRunMissingSkillReturnsToolError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	res, _, err := handleTeamSkillRun(context.Background(), nil, TeamSkillRunArgs{
		SkillName: "nonexistent-skill",
		MySlug:    "eng",
		Channel:   "general",
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError=true for missing skill, got %+v", res)
	}
}

func TestHandleTeamSkillCreateProposesSkill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	res, _, err := handleTeamSkillCreate(context.Background(), nil, TeamSkillCreateArgs{
		Name:        "handoff-checklist",
		Title:       "Handoff Checklist",
		Description: "A deterministic checklist for cross-agent handoffs.",
		Content:     "1. Summarize state.\n2. Name the owner.\n3. Name the next action.",
		Trigger:     "Before delegating multi-step work",
		Tags:        []string{"coordination", "ops"},
		Action:      "propose",
		MySlug:      "ceo",
		Channel:     "general",
	})
	if err != nil {
		t.Fatalf("skill create: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected successful tool result, got %+v", res)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/skills?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get skills: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Skills []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	if len(result.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %+v", result.Skills)
	}
	if got := result.Skills[0].Name; got != "handoff-checklist" {
		t.Fatalf("expected handoff-checklist skill, got %q", got)
	}
	if got := result.Skills[0].Status; got != "proposed" {
		t.Fatalf("expected proposed skill, got %q", got)
	}
	if got := len(b.Requests("general", false)); got != 1 {
		t.Fatalf("expected 1 approval request, got %d", got)
	}
}

func TestHandleTeamSkillCreateCanActivateImmediately(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	res, _, err := handleTeamSkillCreate(context.Background(), nil, TeamSkillCreateArgs{
		Name:    "already-approved",
		Title:   "Already Approved",
		Content: "1. Run the already-approved workflow.",
		Action:  "create",
		MySlug:  "ceo",
		Channel: "general",
	})
	if err != nil {
		t.Fatalf("skill create: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected successful tool result, got %+v", res)
	}

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/skills?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get skills: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Skills []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode skills: %v", err)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "already-approved" {
		t.Fatalf("expected already-approved skill, got %+v", result.Skills)
	}
	if got := result.Skills[0].Status; got != "active" {
		t.Fatalf("expected active skill, got %q", got)
	}
	if got := len(b.Requests("general", false)); got != 0 {
		t.Fatalf("expected no approval request for action=create, got %d", got)
	}
}

func TestHandleTeamSkillCreateAllowsNonCEOProposal(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := team.NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	res, _, err := handleTeamSkillCreate(context.Background(), nil, TeamSkillCreateArgs{
		Name:    "planner-retro-loop",
		Title:   "Planner Retro Loop",
		Content: "1. Collect notes.\n2. Extract action items.",
		Action:  "propose",
		MySlug:  "planner",
		Channel: "general",
	})
	if err != nil {
		t.Fatalf("skill propose: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected successful tool result, got %+v", res)
	}
	if got := len(b.Requests("general", false)); got != 1 {
		t.Fatalf("expected 1 approval request, got %d", got)
	}
	requests := b.Requests("general", false)
	if requests[0].From != "planner" || requests[0].ReplyTo != "planner-retro-loop" {
		t.Fatalf("unexpected approval request: %+v", requests[0])
	}
}

func TestHandleTeamSkillCreateRejectsNonCEOActiveCreate(t *testing.T) {
	res, _, err := handleTeamSkillCreate(context.Background(), nil, TeamSkillCreateArgs{
		Name:    "handoff-checklist",
		Content: "1. Do it",
		Action:  "create",
		MySlug:  "pm",
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError=true for non-CEO active create, got %+v", res)
	}
}

func TestHandleTeamSkillCreateRequiresAction(t *testing.T) {
	res, _, err := handleTeamSkillCreate(context.Background(), nil, TeamSkillCreateArgs{
		Name:    "handoff-checklist",
		Content: "1. Do it",
		MySlug:  "pm",
	})
	if err != nil {
		t.Fatalf("unexpected go error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected IsError=true when action is omitted, got %+v", res)
	}
}
