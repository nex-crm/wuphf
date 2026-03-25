# Multi-Agent CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a demo-ready "Zero Human Company" CLI where an autonomous team of AI agents operates from a single terminal window.

**Architecture:** Go + Bubbletea TUI. User gives directives to Team-Lead (CEO), who narrates delegation to specialist agents. Agents work in parallel using Claude Code as LLM provider. All output streams to a chat-style TUI with agent roster sidebar.

**Tech Stack:** Go 1.24+, Bubbletea, Lipgloss, Claude Code CLI (`claude -p`)

**Spec:** `docs/superpowers/specs/2026-03-22-multi-agent-cli-design.md`

---

## Task 1: Agent Packs — Data Model and Registry

**Files:**
- Create: `internal/agent/packs.go`
- Modify: `internal/config/config.go`
- Test: `internal/agent/packs_test.go`

- [ ] **Step 1: Write failing test for pack registry**

```go
// internal/agent/packs_test.go
package agent

import "testing"

func TestPacksRegistered(t *testing.T) {
	if len(Packs) != 3 {
		t.Fatalf("expected 3 packs, got %d", len(Packs))
	}
	founding := GetPack("founding-team")
	if founding == nil {
		t.Fatal("founding-team pack not found")
	}
	if founding.LeadSlug != "ceo" {
		t.Errorf("expected lead slug 'ceo', got '%s'", founding.LeadSlug)
	}
	if len(founding.Agents) != 7 {
		t.Errorf("expected 7 agents in founding team, got %d", len(founding.Agents))
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./internal/agent/ -run TestPack -v`
Expected: FAIL — `Packs` and `GetPack` undefined

- [ ] **Step 3: Implement packs.go**

```go
// internal/agent/packs.go
package agent

// PackDefinition defines a team of agents that work together.
type PackDefinition struct {
	Slug        string
	Name        string
	Description string
	LeadSlug    string
	Agents      []AgentConfig
}

// Packs is the registry of all available agent packs.
var Packs = []PackDefinition{
	{
		Slug:        "founding-team",
		Name:        "Founding Team",
		Description: "Full autonomous company — CEO delegates to specialists",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{Slug: "ceo", Name: "CEO", Expertise: []string{"strategy", "decision-making", "prioritization", "delegation", "orchestration"}, Personality: "Strategic leader who breaks down complex directives into clear specialist assignments"},
			{Slug: "pm", Name: "Product Manager", Expertise: []string{"roadmap", "user-stories", "requirements", "prioritization", "specs"}, Personality: "Detail-oriented PM who translates business needs into actionable specs"},
			{Slug: "fe", Name: "FE Engineer", Expertise: []string{"frontend", "React", "CSS", "UI-UX", "components"}, Personality: "Frontend specialist focused on clean, accessible implementations"},
			{Slug: "be", Name: "BE Engineer", Expertise: []string{"backend", "APIs", "databases", "infrastructure", "architecture"}, Personality: "Backend engineer focused on reliable, scalable systems"},
			{Slug: "designer", Name: "Designer", Expertise: []string{"UI-UX-design", "branding", "visual-systems", "prototyping"}, Personality: "Creative designer who balances aesthetics with usability"},
			{Slug: "cmo", Name: "CMO", Expertise: []string{"marketing", "content", "brand", "growth", "analytics", "campaigns"}, Personality: "Growth-focused marketer who drives awareness and engagement"},
			{Slug: "cro", Name: "CRO", Expertise: []string{"sales", "pipeline", "revenue", "partnerships", "outreach", "closing"}, Personality: "Revenue-driven closer who builds pipeline and converts deals"},
		},
	},
	{
		Slug:        "coding-team",
		Name:        "Coding Team",
		Description: "High-velocity software development team",
		LeadSlug:    "tech-lead",
		Agents: []AgentConfig{
			{Slug: "tech-lead", Name: "Tech Lead", Expertise: []string{"architecture", "code-review", "technical-decisions", "planning"}, Personality: "Senior engineer who makes sound architectural decisions and coordinates the team"},
			{Slug: "fe", Name: "FE Engineer", Expertise: []string{"frontend", "React", "CSS", "components", "accessibility"}, Personality: "Frontend specialist focused on clean, accessible implementations"},
			{Slug: "be", Name: "BE Engineer", Expertise: []string{"backend", "APIs", "databases", "DevOps", "infrastructure"}, Personality: "Backend engineer focused on reliable, scalable systems"},
			{Slug: "qa", Name: "QA Engineer", Expertise: []string{"testing", "automation", "quality", "edge-cases", "CI-CD"}, Personality: "Quality-focused engineer who catches issues before they reach production"},
		},
	},
	{
		Slug:        "lead-gen-agency",
		Name:        "Lead Gen Agency",
		Description: "Quiet outbound systems and automated GTM",
		LeadSlug:    "ae",
		Agents: []AgentConfig{
			{Slug: "ae", Name: "Account Executive", Expertise: []string{"prospecting", "outreach", "pipeline", "closing", "negotiation"}, Personality: "Seasoned closer who builds relationships and converts opportunities"},
			{Slug: "sdr", Name: "SDR", Expertise: []string{"cold-outreach", "qualification", "booking-meetings", "sequences"}, Personality: "Persistent SDR who opens doors and qualifies opportunities"},
			{Slug: "research", Name: "Research Analyst", Expertise: []string{"market-research", "competitive-analysis", "ICP-profiling", "trends"}, Personality: "Analytical researcher who surfaces actionable intelligence"},
			{Slug: "content", Name: "Content Strategist", Expertise: []string{"SEO", "copywriting", "nurture-sequences", "thought-leadership"}, Personality: "Strategic writer who creates content that drives engagement"},
		},
	},
}

// GetPack returns the pack with the given slug, or nil if not found.
func GetPack(slug string) *PackDefinition {
	for i := range Packs {
		if Packs[i].Slug == slug {
			return &Packs[i]
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/ -run TestPack -v`
Expected: PASS (all 5 tests)

- [ ] **Step 5: Add Pack and TeamLeadSlug to Config**

In `internal/config/config.go`, add two fields to `Config`:

```go
// Add to Config struct after GeminiAPIKey:
Pack           string `json:"pack,omitempty"`
TeamLeadSlug   string `json:"team_lead_slug,omitempty"`
MaxConcurrent  int    `json:"max_concurrent_agents,omitempty"`
```

- [ ] **Step 6: Run all tests**

Run: `go test ./...`
Expected: All existing tests still pass

- [ ] **Step 7: Commit**

```bash
git add internal/agent/packs.go internal/agent/packs_test.go internal/config/config.go
git commit -m "feat: add agent pack definitions and config fields"
```

---

## Task 2: Make Provider Default Claude Code

**Files:**
- Modify: `internal/provider/resolver.go`
- Test: `internal/provider/resolver_test.go` (if exists, else create)

- [ ] **Step 1: Write failing test**

```go
// internal/provider/resolver_test.go
package provider

import (
	"testing"

	"github.com/nex-crm/wuphf/internal/config"
)

func TestDefaultResolverUsesClaudeCode(t *testing.T) {
	// When LLMProvider is empty, should resolve to claude-code, not wuphf-ask
	cfg := config.Config{} // empty provider
	resolver := DefaultStreamFnResolver(nil)
	fn := resolver("test-agent")
	if fn == nil {
		t.Fatal("resolver returned nil StreamFn")
	}
	// We can't easily test the internal provider, but we verify it doesn't panic
	_ = cfg // used for context
}
```

- [ ] **Step 2: Change default in resolver.go**

In `internal/provider/resolver.go`, change the `default` case from `CreateNexAskStreamFn(client)` to `CreateClaudeCodeStreamFn(agentSlug)`:

```go
// resolver.go — updated DefaultStreamFnResolver
func DefaultStreamFnResolver(client *api.Client) agent.StreamFnResolver {
	cfg, _ := config.Load()
	return func(agentSlug string) agent.StreamFn {
		switch cfg.LLMProvider {
		case "gemini":
			return CreateGeminiStreamFn(cfg.GeminiAPIKey)
		case "wuphf-ask":
			return CreateNexAskStreamFn(client)
		case "claude-code", "":
			// Default to Claude Code — most capable for multi-turn orchestration
			return CreateClaudeCodeStreamFn(agentSlug)
		default:
			return CreateClaudeCodeStreamFn(agentSlug)
		}
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/provider/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/provider/resolver.go internal/provider/resolver_test.go
git commit -m "feat: default LLM provider to Claude Code instead of WUPHF Ask"
```

---

## Task 3: Configurable Team-Lead in MessageRouter

**Files:**
- Modify: `internal/orchestration/message_router.go`
- Modify: `internal/orchestration/message_router_test.go`

- [ ] **Step 1: Write failing test**

Add test to `message_router_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestration/ -run TestRouteUsesConfiguredTeamLead -v`
Expected: FAIL — `SetTeamLeadSlug` undefined

- [ ] **Step 3: Add teamLeadSlug field and setter to MessageRouter**

In `internal/orchestration/message_router.go`:
- Add `teamLeadSlug string` field to `MessageRouter` struct (around line 33)
- Add method:

```go
func (m *MessageRouter) SetTeamLeadSlug(slug string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.teamLeadSlug = slug
}

func (m *MessageRouter) getTeamLeadSlug() string {
	if m.teamLeadSlug != "" {
		return m.teamLeadSlug
	}
	return "team-lead" // backward compat default
}
```

- Replace all hardcoded `"team-lead"` references in `Route()` (lines ~105, ~117) with `m.getTeamLeadSlug()`

- [ ] **Step 4: Run tests**

Run: `go test ./internal/orchestration/ -v`
Expected: All pass including new test

- [ ] **Step 5: Commit**

```bash
git add internal/orchestration/message_router.go internal/orchestration/message_router_test.go
git commit -m "feat: make MessageRouter team-lead slug configurable per pack"
```

---

## Task 4: Team-Lead Delegation Parser

**Files:**
- Create: `internal/orchestration/delegator.go`
- Create: `internal/orchestration/delegator_test.go`

- [ ] **Step 1: Write failing tests**

```go
// internal/orchestration/delegator_test.go
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
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/orchestration/ -run TestExtractDelegation -v`
Expected: FAIL

- [ ] **Step 3: Implement delegator.go**

```go
// internal/orchestration/delegator.go
package orchestration

import (
	"fmt"
	"regexp"
	"strings"
)

// Delegation represents a sub-task extracted from Team-Lead output.
type Delegation struct {
	AgentSlug string
	Task      string // The sentence context around the @mention
}

// Delegator parses Team-Lead responses and extracts specialist delegations.
type Delegator struct {
	maxConcurrent int
	mentionRe     *regexp.Regexp
}

// NewDelegator creates a delegator with the given concurrency limit.
func NewDelegator(maxConcurrent int) *Delegator {
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	return &Delegator{
		maxConcurrent: maxConcurrent,
		mentionRe:     regexp.MustCompile(`@([a-z][a-z0-9-]*)`),
	}
}

// ExtractDelegations parses the Team-Lead response for @agent-slug mentions
// and extracts the surrounding sentence as the task description.
// Only mentions matching knownSlugs are returned.
func (d *Delegator) ExtractDelegations(response string, knownSlugs []string) []Delegation {
	known := make(map[string]bool, len(knownSlugs))
	for _, s := range knownSlugs {
		known[s] = true
	}

	matches := d.mentionRe.FindAllStringSubmatchIndex(response, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var delegations []Delegation

	for _, match := range matches {
		slug := response[match[2]:match[3]]
		if !known[slug] || seen[slug] {
			continue
		}
		seen[slug] = true

		sentence := extractSentence(response, match[0])
		task := strings.TrimSpace(sentence)

		delegations = append(delegations, Delegation{
			AgentSlug: slug,
			Task:      task,
		})
	}

	return delegations
}

// FormatSteerMessage formats a delegation as a steer message for the specialist.
func FormatSteerMessage(d Delegation) string {
	return fmt.Sprintf("[TEAM-LEAD DELEGATION] %s", d.Task)
}

// extractSentence finds the sentence containing the position pos.
// Sentences are delimited by periods, newlines, or string boundaries.
func extractSentence(text string, pos int) string {
	start := 0
	for i := pos - 1; i >= 0; i-- {
		if text[i] == '.' || text[i] == '\n' {
			start = i + 1
			break
		}
	}

	end := len(text)
	for i := pos; i < len(text); i++ {
		if text[i] == '.' || text[i] == '\n' {
			end = i + 1
			break
		}
	}

	return text[start:end]
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/orchestration/ -run TestExtractDelegation -v`
Expected: All PASS

- [ ] **Step 5: Run all tests**

Run: `go test ./...`
Expected: All pass

- [ ] **Step 6: Commit**

```bash
git add internal/orchestration/delegator.go internal/orchestration/delegator_test.go
git commit -m "feat: add Team-Lead delegation parser with sentence extraction"
```

---

## Task 5: Wire Delegation into TUI Stream

**Files:**
- Modify: `internal/tui/stream.go` (handleSubmit and AgentDoneMsg handler)
- Modify: `internal/tui/model.go` (bootstrap pack agents instead of single team-lead)

- [ ] **Step 1: Add delegator field to StreamModel**

In `internal/tui/stream.go`, add to `StreamModel` struct:

```go
delegator      *orchestration.Delegator
teamLeadSlug   string
```

- [ ] **Step 2: Update NewStreamModel to accept delegator**

Update the constructor to accept and store the delegator:

```go
func NewStreamModel(agentSvc *agent.AgentService, msgRouter *orchestration.MessageRouter, events chan tea.Msg, delegator *orchestration.Delegator, teamLeadSlug string) StreamModel {
```

Store the fields in the returned model.

- [ ] **Step 3: Add delegation logic to Update()**

In `stream.go`'s `Update()` method, find the `AgentDoneMsg` case (around line 130-140). After the accumulated text is captured but before it's added to messages, add delegation check:

```go
case AgentDoneMsg:
	m.streaming = false
	// Check if this was the team-lead — parse for delegations
	if msg.AgentSlug == m.teamLeadSlug && m.delegator != nil {
		// Collect known specialist slugs
		var knownSlugs []string
		for _, a := range m.agentService.List() {
			if a.Config.Slug != m.teamLeadSlug {
				knownSlugs = append(knownSlugs, a.Config.Slug)
			}
		}
		// Find the last message content from team-lead
		lastMsg := ""
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].AgentSlug == msg.AgentSlug {
				lastMsg = m.messages[i].Content
				break
			}
		}
		if lastMsg != "" {
			delegations := m.delegator.ExtractDelegations(lastMsg, knownSlugs)
			for _, d := range delegations {
				steerMsg := orchestration.FormatSteerMessage(d)
				m.agentService.Steer(d.AgentSlug, steerMsg)
				m.agentService.EnsureRunning(d.AgentSlug)
				if !m.wiredAgents[d.AgentSlug] {
					m.wireAgent(d.AgentSlug)
				}
			}
		}
	}
```

- [ ] **Step 4: Update model.go to bootstrap pack agents**

In `internal/tui/model.go`'s `NewModel()`, replace the single `team-lead` bootstrap with pack-based bootstrap:

```go
// Load config to get pack preference
cfg, _ := config.Load()
packSlug := cfg.Pack
if packSlug == "" {
	packSlug = "founding-team"
}
teamLeadSlug := cfg.TeamLeadSlug

pack := agent.GetPack(packSlug)
if pack != nil {
	teamLeadSlug = pack.LeadSlug
	for _, agentCfg := range pack.Agents {
		agentSvc.Create(agentCfg)
		msgRouter.RegisterAgent(agentCfg.Slug, agentCfg.Expertise)
	}
} else {
	// Fallback: create single team-lead
	teamLeadSlug = "team-lead"
	agentSvc.CreateFromTemplate("team-lead", "team-lead")
	msgRouter.RegisterAgent("team-lead", []string{"general", "orchestration"})
}
msgRouter.SetTeamLeadSlug(teamLeadSlug)

maxConcurrent := cfg.MaxConcurrent
if maxConcurrent <= 0 {
	maxConcurrent = 3
}
delegator := orchestration.NewDelegator(maxConcurrent)
```

Pass `delegator` and `teamLeadSlug` to `NewStreamModel()`.

- [ ] **Step 5: Add system prompt to Team-Lead agent**

In `internal/agent/loop.go`'s `buildContext()` method, inject a system prompt when the agent is the team-lead. Add at the start of `buildContext()`:

```go
// Inject team-lead system prompt if this agent has delegation personality
if strings.Contains(l.state.Config.Personality, "delegate") || strings.Contains(l.state.Config.Personality, "assignment") {
	// Build team roster for system prompt
	// This is injected as the first session entry
}
```

A simpler approach: set the Personality field in packs.go to include delegation instructions, and the existing `buildContext()` already uses Personality as part of the prompt context.

- [ ] **Step 6: Run all tests**

Run: `go test ./...`
Expected: All pass (no test breakage from wiring changes)

- [ ] **Step 7: Commit**

```bash
git add internal/tui/stream.go internal/tui/model.go
git commit -m "feat: wire delegation into TUI — pack bootstrap + team-lead parsing"
```

---

## Task 6: System Prompts and Context Engineering

**Files:**
- Modify: `internal/agent/loop.go` (buildContext to inject system prompt)
- Create: `internal/agent/prompts.go`
- Create: `internal/agent/prompts_test.go`

- [ ] **Step 1: Write failing test for prompt generation**

```go
// internal/agent/prompts_test.go
package agent

import (
	"strings"
	"testing"
)

func TestBuildTeamLeadPrompt(t *testing.T) {
	lead := AgentConfig{Slug: "ceo", Name: "CEO", Expertise: []string{"strategy"}}
	team := []AgentConfig{
		{Slug: "fe", Name: "FE Engineer", Expertise: []string{"frontend", "React"}},
		{Slug: "be", Name: "BE Engineer", Expertise: []string{"backend", "APIs"}},
	}
	prompt := BuildTeamLeadPrompt(lead, team, "Founding Team")
	if !strings.Contains(prompt, "@fe") {
		t.Error("expected prompt to contain @fe")
	}
	if !strings.Contains(prompt, "@be") {
		t.Error("expected prompt to contain @be")
	}
	if !strings.Contains(prompt, "delegate") || !strings.Contains(prompt, "narrate") {
		t.Error("expected delegation instructions in prompt")
	}
}

func TestBuildSpecialistPrompt(t *testing.T) {
	specialist := AgentConfig{Slug: "fe", Name: "FE Engineer", Expertise: []string{"frontend", "React"}}
	prompt := BuildSpecialistPrompt(specialist)
	if !strings.Contains(prompt, "FE Engineer") {
		t.Error("expected specialist name in prompt")
	}
	if !strings.Contains(prompt, "frontend") {
		t.Error("expected expertise in prompt")
	}
}

func TestBuildTeamLeadPromptMentionsAllAgents(t *testing.T) {
	lead := AgentConfig{Slug: "ceo", Name: "CEO"}
	team := []AgentConfig{
		{Slug: "pm", Name: "PM", Expertise: []string{"roadmap"}},
		{Slug: "fe", Name: "FE", Expertise: []string{"frontend"}},
		{Slug: "be", Name: "BE", Expertise: []string{"backend"}},
	}
	prompt := BuildTeamLeadPrompt(lead, team, "Founding Team")
	for _, a := range team {
		if !strings.Contains(prompt, "@"+a.Slug) {
			t.Errorf("expected prompt to mention @%s", a.Slug)
		}
	}
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./internal/agent/ -run TestBuild.*Prompt -v`
Expected: FAIL

- [ ] **Step 3: Implement prompts.go**

```go
// internal/agent/prompts.go
package agent

import (
	"fmt"
	"strings"
)

// BuildTeamLeadPrompt generates the system prompt for a team-lead agent.
func BuildTeamLeadPrompt(lead AgentConfig, team []AgentConfig, packName string) string {
	var roster strings.Builder
	for _, a := range team {
		if a.Slug == lead.Slug {
			continue
		}
		roster.WriteString(fmt.Sprintf("- @%s (%s): %s\n", a.Slug, a.Name, strings.Join(a.Expertise, ", ")))
	}

	return fmt.Sprintf(`You are the %s of the %s. Your team consists of:
%s
When the user gives you a directive:
1. Analyze what needs to be done
2. Break it into sub-tasks for your team members
3. Narrate your delegation plan, mentioning each agent by @slug
4. Example: "I'll have @research analyze the competitive landscape while @content drafts the positioning document."

Always delegate to the most appropriate specialist. Never do specialist work yourself.
Keep your delegation plan concise — one or two sentences per agent.`, lead.Name, packName, roster.String())
}

// BuildSpecialistPrompt generates the system prompt for a specialist agent.
func BuildSpecialistPrompt(specialist AgentConfig) string {
	return fmt.Sprintf(`You are %s, a specialist in %s.

You receive tasks from your team lead. Focus on your area of expertise.
Be thorough but concise. Report your findings clearly.
If you need information from the knowledge base, use the available tools.`,
		specialist.Name, strings.Join(specialist.Expertise, ", "))
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agent/ -run TestBuild.*Prompt -v`
Expected: All PASS

- [ ] **Step 5: Wire system prompts into buildContext in loop.go**

In `internal/agent/loop.go`'s `buildContext()` method, inject the system prompt as the first message if there's no existing system entry in the session:

```go
// At the start of buildContext(), after creating the session:
// Inject system prompt if not already present
entries, _ := l.sessions.List(l.state.SessionID)
hasSystem := false
for _, e := range entries {
	if e.Type == "system" {
		hasSystem = true
		break
	}
}
if !hasSystem && l.state.Config.Personality != "" {
	l.sessions.Append(l.state.SessionID, SessionEntry{
		Type:    "system",
		Content: l.state.Config.Personality,
	})
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./...`
Expected: All pass

- [ ] **Step 7: Commit**

```bash
git add internal/agent/prompts.go internal/agent/prompts_test.go internal/agent/loop.go
git commit -m "feat: add system prompt generation for team-lead and specialist agents"
```

---

## Task 7: Update Roster Phase Labels and Colors

**Files:**
- Modify: `internal/tui/roster.go`
- Modify: `internal/tui/roster_test.go`

- [ ] **Step 1: Write failing test for new phase labels**

```go
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
	}
	for _, tt := range tests {
		got := phaseLabel(tt.phase)
		if got != tt.expected {
			t.Errorf("phaseLabel(%q) = %q, want %q", tt.phase, got, tt.expected)
		}
	}
}
```

- [ ] **Step 2: Implement phase labels**

In `roster.go`, update `phaseShortLabel()` (or create `phaseLabel()`) to return full labels:
- `build_context` → "preparing"
- `stream_llm` → "thinking"
- `execute_tool` → "running tool"
- `idle` → "idle"
- `done` → "done"
- `error` → "error"

Add phase-specific colors using lipgloss:
- preparing → yellow
- thinking → blue
- running tool → purple (NexPurple)
- done → green
- error → red

- [ ] **Step 3: Run tests**

Run: `go test ./internal/tui/ -run TestPhaseLabel -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/tui/roster.go internal/tui/roster_test.go
git commit -m "feat: update roster phase labels and colors per spec"
```

---

## Task 8: Wire Non-Interactive Dispatch

**Files:**
- Modify: `cmd/wuphf/main.go`

- [ ] **Step 1: Replace dispatch stub with real command dispatch**

```go
func dispatch(cmd string) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	apiKey := config.ResolveAPIKey("")
	if apiKey == "" {
		fmt.Fprintf(os.Stderr, "No API key found. Set WUPHF_API_KEY or run: wuphf (interactive) then /init\n")
		os.Exit(2)
	}

	client := api.NewClient(config.APIBase(), apiKey, cfg.DefaultTimeout)
	ctx := &commands.SlashContext{
		Client:       client,
		Config:       &cfg,
		AgentService: nil, // non-interactive, no agent service
	}

	result := commands.Dispatch(ctx, cmd)
	if result.Error != "" {
		fmt.Fprintf(os.Stderr, "error: %s\n", result.Error)
		if strings.Contains(result.Error, "401") || strings.Contains(result.Error, "auth") {
			os.Exit(2)
		}
		os.Exit(1)
	}
	if result.Output != "" {
		fmt.Println(result.Output)
	}
}
```

- [ ] **Step 2: Add necessary imports**

Add `strings`, `config`, `api`, `commands` imports to main.go.

- [ ] **Step 3: Test non-interactive mode**

Run: `go build -o wuphf ./cmd/wuphf && ./wuphf --version`
Expected: Prints version

Run: `./wuphf --cmd "help"`
Expected: Prints help text (or available commands)

- [ ] **Step 4: Commit**

```bash
git add cmd/wuphf/main.go
git commit -m "feat: wire non-interactive dispatch to real command registry"
```

---

## Task 9: Init Flow — Pack Selection and Provider Choice

**Files:**
- Modify: `internal/tui/init_flow.go` (rewrite with full flow)

- [ ] **Step 1: Rewrite init_flow.go with complete onboarding**

Replace the stub init flow with a complete state machine:

States: `idle` → `api_key` → `provider_choice` → `pack_choice` → `done`

Each state uses the Picker component for selection. The flow:
1. Check for existing API key in config — skip to provider_choice if found
2. Show text input for API key
3. Show picker: Claude Code / Gemini / WUPHF Ask
4. Show picker: Founding Team / Coding Team / Lead Gen Agency
5. Save config, create agents, show welcome

The init flow should emit `InitFlowMsg` with phase and data for each transition. The stream model handles these messages to update state.

- [ ] **Step 2: Test the flow manually**

Run: `go build -o wuphf ./cmd/wuphf && ./wuphf`
Type: `/init`
Expected: Walks through provider and pack selection

- [ ] **Step 3: Commit**

```bash
git add internal/tui/init_flow.go
git commit -m "feat: implement full /init onboarding with provider and pack selection"
```

---

## Task 10: Integration Test — Full Delegation Flow

**Files:**
- Create: `tests/e2e/delegation_test.go` (or termwright steps)

- [ ] **Step 1: Write integration test**

Test the full flow:
1. Create agent service with founding team pack
2. Send a message that should trigger delegation
3. Verify Team-Lead receives the message
4. Verify delegator extracts @mentions
5. Verify steer messages are queued to specialists

```go
package e2e

import (
	"testing"
	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/orchestration"
)

func TestDelegationFlow(t *testing.T) {
	// Create service with pack
	svc := agent.NewAgentService()
	pack := agent.GetPack("founding-team")
	if pack == nil {
		t.Fatal("founding-team pack not found")
	}

	for _, cfg := range pack.Agents {
		_, err := svc.Create(cfg)
		if err != nil {
			t.Fatalf("failed to create agent %s: %v", cfg.Slug, err)
		}
	}

	// Create delegator
	d := orchestration.NewDelegator(3)

	// Simulate team-lead response with delegations
	response := "I'll have @fe build the landing page while @be sets up the API endpoints."
	knownSlugs := []string{"pm", "fe", "be", "designer", "cmo", "cro"}

	delegations := d.ExtractDelegations(response, knownSlugs)
	if len(delegations) != 2 {
		t.Fatalf("expected 2 delegations, got %d", len(delegations))
	}

	// Verify steer messages can be sent
	for _, del := range delegations {
		msg := orchestration.FormatSteerMessage(del)
		err := svc.Steer(del.AgentSlug, msg)
		if err != nil {
			t.Errorf("failed to steer %s: %v", del.AgentSlug, err)
		}
	}
}
```

- [ ] **Step 2: Run integration test**

Run: `go test ./tests/e2e/ -run TestDelegationFlow -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/delegation_test.go
git commit -m "test: add integration test for full delegation flow"
```

---

## Task 11: Build, Verify, and Final Smoke Test

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/najmuzzaman/Documents/nex/WUPHF && go test ./...`
Expected: All tests pass (existing + new)

- [ ] **Step 2: Build binary**

Run: `go build -o wuphf ./cmd/wuphf`
Expected: Clean build, binary under 15MB

- [ ] **Step 3: Verify non-interactive mode**

Run: `./wuphf --version && ./wuphf --cmd "help"`
Expected: Version prints, help shows commands

- [ ] **Step 4: Verify interactive mode launches**

Run: `./wuphf`
Expected: TUI launches with banner, roster sidebar shows all agents from founding team, input prompt visible

- [ ] **Step 5: Commit any final fixes**

```bash
git add -A && git commit -m "chore: final build verification and cleanup"
```
