package teammcp

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nex-crm/wuphf/internal/agent"
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
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	// Seed a skill the agent can invoke. After PR 7 task #3 the broker
	// enforces per-agent visibility on /skills/{name}/invoke, so the spec
	// must list "eng" in OwnerAgents to keep this happy-path test green.
	b.SeedDefaultSkills([]agent.PackSkillSpec{{
		Name:        "investigate",
		Title:       "Investigate a Bug",
		Description: "Systematic debugging with root cause analysis.",
		Trigger:     "When a bug or error is reported",
		Tags:        []string{"engineering", "debugging"},
		Content:     "Step 1: Reproduce. Step 2: Isolate. Step 3: Root cause. Step 4: Fix.",
		OwnerAgents: []string{"eng"},
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
	b := newTestBroker(t)
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
	b := newTestBroker(t)
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
	b := newTestBroker(t)
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
	b := newTestBroker(t)
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

// teamSkillListPayload extracts the JSON payload returned by handleTeamSkillList.
// The tool returns a pretty-printed JSON object via textResult; tests decode it
// into a typed struct so they can assert per-field rather than substring-match.
type teamSkillListPayload struct {
	OK     bool   `json:"ok"`
	Scope  string `json:"scope"`
	Count  int    `json:"count"`
	Skills []struct {
		Name        string   `json:"name"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Trigger     string   `json:"trigger"`
		OwnerAgents []string `json:"owner_agents"`
		Tags        []string `json:"tags"`
		Content     string   `json:"content,omitempty"`
	} `json:"skills"`
}

func decodeTeamSkillListResult(t *testing.T, res *mcp.CallToolResult) teamSkillListPayload {
	t.Helper()
	text := toolErrorText(res)
	if text == "" {
		t.Fatalf("expected text content in result, got %+v", res)
	}
	var payload teamSkillListPayload
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("decode payload %q: %v", text, err)
	}
	return payload
}

// TestTeamSkillRun_NotOwner_ReturnsStructuredDelegateTo verifies the F1 fix:
// when the broker returns the structured 403 from handleInvokeSkill (PR 7
// task #3 / writeSkillForbidden), the MCP layer must surface it as a
// machine-readable textResult with `ok:false, error, delegate_to, hint`
// rather than collapsing it into an unstructured toolError string.
//
// Without this, the agent sees the failure but cannot route the request —
// it has to guess which agent owns the skill. With it, the agent gets the
// owner list and can hand off via team_broadcast.
func TestTeamSkillRun_NotOwner_ReturnsStructuredDelegateTo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	// Skill is csm-scoped; eng tries to invoke it.
	b.SeedDefaultSkills([]agent.PackSkillSpec{{
		Name:        "csm-only-skill",
		Title:       "CSM Only",
		Description: "Csm-scoped playbook",
		Content:     "Step 1.",
		OwnerAgents: []string{"csm"},
	}})

	res, _, err := handleTeamSkillRun(context.Background(), nil, TeamSkillRunArgs{
		SkillName: "csm-only-skill",
		MySlug:    "eng",
		Channel:   "general",
	})
	if err != nil {
		t.Fatalf("skill run: %v", err)
	}
	if res == nil {
		t.Fatalf("expected non-nil result")
	}
	// Structured 403 must come back as a normal textResult — NOT a toolError.
	// The agent reads the body to decide what to do next; a tool error would
	// just look like a transport failure.
	if res.IsError {
		t.Fatalf("expected structured 403 to be surfaced as textResult, got toolError: %s", toolErrorText(res))
	}

	text := toolErrorText(res)
	if text == "" {
		t.Fatalf("expected text content with structured forbidden body")
	}
	var body struct {
		OK         bool     `json:"ok"`
		Error      string   `json:"error"`
		DelegateTo []string `json:"delegate_to"`
		Hint       string   `json:"hint"`
	}
	if err := json.Unmarshal([]byte(text), &body); err != nil {
		t.Fatalf("expected JSON body, got %q: %v", text, err)
	}
	if body.OK {
		t.Errorf("expected ok=false, got true")
	}
	if body.Error != "not_owner" {
		t.Errorf("expected error=not_owner, got %q", body.Error)
	}
	if len(body.DelegateTo) != 1 || body.DelegateTo[0] != "csm" {
		t.Errorf("expected delegate_to=[csm], got %v", body.DelegateTo)
	}
	if strings.TrimSpace(body.Hint) == "" {
		t.Errorf("expected non-empty hint, got %q", body.Hint)
	}
}

// TestTeamSkillList_Scope_Own verifies that scope=own (default) returns the
// caller's visible skills with full Content, sorted by name.
func TestTeamSkillList_Scope_Own(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	b.SeedDefaultSkills([]agent.PackSkillSpec{
		{Name: "csm-onboard", Title: "CSM Onboarding", Description: "Walk a new customer through onboarding.", Trigger: "When a new customer signs up", Tags: []string{"csm"}, Content: "Step 1. Greet customer.", OwnerAgents: []string{"csm"}},
		{Name: "eng-deploy", Title: "Eng Deploy", Description: "Deploy a service to prod.", Trigger: "Before a release", Tags: []string{"eng"}, Content: "Step 1. Run tests.", OwnerAgents: []string{"eng"}},
		{Name: "shared-routine", Title: "Shared Routine", Description: "Joint csm+eng routine.", Trigger: "Daily standup", Tags: []string{"ops"}, Content: "Step 1. Sync.", OwnerAgents: []string{"csm", "eng"}},
		{Name: "lead-only", Title: "Lead Only", Description: "CEO-coordinated playbook.", Trigger: "When CEO triages.", Tags: []string{"ops"}, Content: "Step 1. Triage.", OwnerAgents: nil},
	})

	res, _, err := handleTeamSkillList(context.Background(), nil, TeamSkillListArgs{
		MySlug: "csm",
	})
	if err != nil {
		t.Fatalf("skill list (own): %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected successful tool result, got %+v", res)
	}

	payload := decodeTeamSkillListResult(t, res)
	if payload.Scope != "own" {
		t.Errorf("expected scope=own, got %q", payload.Scope)
	}
	if payload.Count != len(payload.Skills) {
		t.Errorf("count mismatch: count=%d, len(skills)=%d", payload.Count, len(payload.Skills))
	}
	wantNames := []string{"csm-onboard", "shared-routine"}
	if len(payload.Skills) != len(wantNames) {
		gotNames := make([]string, len(payload.Skills))
		for i, sk := range payload.Skills {
			gotNames[i] = sk.Name
		}
		t.Fatalf("scope=own for csm: got %v, want %v", gotNames, wantNames)
	}
	for i, want := range wantNames {
		if payload.Skills[i].Name != want {
			t.Errorf("skills[%d].Name = %q, want %q (sorted lex)", i, payload.Skills[i].Name, want)
		}
	}
	for _, sk := range payload.Skills {
		if sk.Content == "" {
			t.Errorf("scope=own must include Content (full payload), got empty for %q", sk.Name)
		}
	}
}

// TestTeamSkillList_Scope_All_StripsContent verifies cross-role discovery
// returns every active skill but with Content stripped (privacy + token
// discipline).
func TestTeamSkillList_Scope_All_StripsContent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	b.SeedDefaultSkills([]agent.PackSkillSpec{
		{Name: "csm-onboard", Title: "CSM Onboarding", Description: "d1", Trigger: "t1", Tags: []string{"csm"}, Content: "csm body", OwnerAgents: []string{"csm"}},
		{Name: "eng-deploy", Title: "Eng Deploy", Description: "d2", Trigger: "t2", Tags: []string{"eng"}, Content: "eng body", OwnerAgents: []string{"eng"}},
		{Name: "lead-only", Title: "Lead Only", Description: "d3", Trigger: "t3", Tags: []string{"ops"}, Content: "lead body", OwnerAgents: nil},
	})

	res, _, err := handleTeamSkillList(context.Background(), nil, TeamSkillListArgs{
		Scope:  "all",
		MySlug: "csm",
	})
	if err != nil {
		t.Fatalf("skill list (all): %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected successful tool result, got text=%q", toolErrorText(res))
	}

	payload := decodeTeamSkillListResult(t, res)
	if payload.Scope != "all" {
		t.Errorf("expected scope=all, got %q", payload.Scope)
	}
	if len(payload.Skills) != 3 {
		gotNames := make([]string, len(payload.Skills))
		for i, sk := range payload.Skills {
			gotNames[i] = sk.Name
		}
		t.Fatalf("scope=all should return all 3 active skills regardless of caller; got %v", gotNames)
	}
	for _, sk := range payload.Skills {
		if sk.Content != "" {
			t.Errorf("scope=all must strip Content for privacy + token discipline; got Content=%q on %q", sk.Content, sk.Name)
		}
		// Metadata must still flow through so agents can decide whether to delegate.
		if sk.Description == "" {
			t.Errorf("scope=all must keep Description for cross-role discovery; got empty on %q", sk.Name)
		}
	}
}

// TestTeamSkillList_TagFilter exercises the tag query in both scopes.
func TestTeamSkillList_TagFilter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	t.Setenv("WUPHF_TEAM_BROKER_URL", "http://"+b.Addr())
	t.Setenv("WUPHF_BROKER_TOKEN", b.Token())

	b.SeedDefaultSkills([]agent.PackSkillSpec{
		{Name: "csm-onboard", Title: "T1", Description: "d", Trigger: "t", Tags: []string{"csm", "ops"}, Content: "c", OwnerAgents: []string{"csm"}},
		{Name: "csm-followup", Title: "T2", Description: "d", Trigger: "t", Tags: []string{"csm"}, Content: "c", OwnerAgents: []string{"csm"}},
		{Name: "eng-deploy", Title: "T3", Description: "d", Trigger: "t", Tags: []string{"eng", "ops"}, Content: "c", OwnerAgents: []string{"eng"}},
	})

	t.Run("scope=own + tag filter", func(t *testing.T) {
		res, _, err := handleTeamSkillList(context.Background(), nil, TeamSkillListArgs{
			Tag:    "ops",
			MySlug: "csm",
		})
		if err != nil {
			t.Fatalf("skill list: %v", err)
		}
		if res == nil || res.IsError {
			t.Fatalf("expected success, got text=%q", toolErrorText(res))
		}
		payload := decodeTeamSkillListResult(t, res)
		// csm sees csm-onboard (tag=ops) and csm-followup (no ops tag).
		// Tag=ops filters to just csm-onboard.
		if len(payload.Skills) != 1 || payload.Skills[0].Name != "csm-onboard" {
			gotNames := make([]string, len(payload.Skills))
			for i, sk := range payload.Skills {
				gotNames[i] = sk.Name
			}
			t.Errorf("scope=own + tag=ops for csm: got %v, want [csm-onboard]", gotNames)
		}
	})

	t.Run("scope=all + tag filter", func(t *testing.T) {
		res, _, err := handleTeamSkillList(context.Background(), nil, TeamSkillListArgs{
			Scope:  "all",
			Tag:    "ops",
			MySlug: "csm",
		})
		if err != nil {
			t.Fatalf("skill list: %v", err)
		}
		if res == nil || res.IsError {
			t.Fatalf("expected success, got text=%q", toolErrorText(res))
		}
		payload := decodeTeamSkillListResult(t, res)
		// scope=all with tag=ops returns csm-onboard AND eng-deploy across
		// owners — that's the cross-role discovery promise.
		if len(payload.Skills) != 2 {
			gotNames := make([]string, len(payload.Skills))
			for i, sk := range payload.Skills {
				gotNames[i] = sk.Name
			}
			t.Fatalf("scope=all + tag=ops: got %v, want 2 entries (csm-onboard, eng-deploy)", gotNames)
		}
		// Sorted lex: csm-onboard before eng-deploy.
		if payload.Skills[0].Name != "csm-onboard" || payload.Skills[1].Name != "eng-deploy" {
			t.Errorf("scope=all + tag=ops order: got [%s, %s], want [csm-onboard, eng-deploy]",
				payload.Skills[0].Name, payload.Skills[1].Name)
		}
	})

	t.Run("tag with no matches returns empty list", func(t *testing.T) {
		res, _, err := handleTeamSkillList(context.Background(), nil, TeamSkillListArgs{
			Scope:  "all",
			Tag:    "nonexistent-tag",
			MySlug: "csm",
		})
		if err != nil {
			t.Fatalf("skill list: %v", err)
		}
		payload := decodeTeamSkillListResult(t, res)
		if len(payload.Skills) != 0 {
			t.Errorf("expected empty result for unknown tag, got %d entries", len(payload.Skills))
		}
	})
}

// TestTeamSkillList_RegisteredForOfficeAndDM verifies the tool is exposed in
// the office and DM-mode tool sets but NOT in the 1:1 mode (matching the
// existing team_skill_run scoping).
func TestTeamSkillList_RegisteredForOfficeAndDM(t *testing.T) {
	for _, tc := range []struct {
		name    string
		channel string
		want    bool
	}{
		{name: "office", channel: "general", want: true},
		{name: "dm", channel: "dm-csm", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			names := listRegisteredTools(t, tc.channel, false)
			if !slices.Contains(names, "team_skill_list") {
				t.Errorf("expected team_skill_list registered in %s mode; got %v", tc.name, names)
			}
		})
	}
}

// TestHandleGetSkills_ForAgentFilter is the F6 fold-in: GET /skills?for_agent
// must filter to skills the named agent can see, plugging the cross-scope
// content leak the existing endpoint had.
func TestHandleGetSkills_ForAgentFilter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	b.SeedDefaultSkills([]agent.PackSkillSpec{
		{Name: "csm-only", Title: "CSM", Content: "csm body", OwnerAgents: []string{"csm"}},
		{Name: "eng-only", Title: "ENG", Content: "eng body", OwnerAgents: []string{"eng"}},
		{Name: "lead-only", Title: "LEAD", Content: "lead body", OwnerAgents: nil},
	})

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/skills?for_agent=csm", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get skills: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Skills []struct {
			Name string `json:"name"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Skills) != 1 || result.Skills[0].Name != "csm-only" {
		gotNames := make([]string, len(result.Skills))
		for i, sk := range result.Skills {
			gotNames[i] = sk.Name
		}
		t.Errorf("for_agent=csm: got %v, want [csm-only]", gotNames)
	}
}

// TestHandleGetSkills_NoForAgentReturnsAll keeps the back-compat humans/UI
// path: when the query param is absent the endpoint behaves as before
// (full unfiltered list, archived excluded).
func TestHandleGetSkills_NoForAgentReturnsAll(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	b := newTestBroker(t)
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	defer b.Stop()

	b.SeedDefaultSkills([]agent.PackSkillSpec{
		{Name: "csm-only", Title: "CSM", Content: "csm body", OwnerAgents: []string{"csm"}},
		{Name: "eng-only", Title: "ENG", Content: "eng body", OwnerAgents: []string{"eng"}},
		{Name: "lead-only", Title: "LEAD", Content: "lead body", OwnerAgents: nil},
	})

	req, _ := http.NewRequest(http.MethodGet, "http://"+b.Addr()+"/skills", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get skills: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Skills []struct {
			Name string `json:"name"`
		} `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Skills) != 3 {
		gotNames := make([]string, len(result.Skills))
		for i, sk := range result.Skills {
			gotNames[i] = sk.Name
		}
		t.Errorf("absent for_agent: got %v, want all 3 skills", gotNames)
	}
}
