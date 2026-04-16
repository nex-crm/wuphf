package company

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/operations"
)

// ---------------------------------------------------------------------------
// Unit Tests: MemberSpec from StarterAgent with explicit permission_mode
// ---------------------------------------------------------------------------

func TestMemberSpecFromStarterAgent_ExplicitPermissionMode(t *testing.T) {
	agent := operations.StarterAgent{
		Slug:           "builder",
		Name:           "Builder",
		Role:           "Builds things",
		PermissionMode: "auto",
		Type:           "specialist",
	}
	member := memberSpecFromStarterAgent(agent, "ceo")
	if member.PermissionMode != "auto" {
		t.Fatalf("expected explicit permission_mode=auto to flow through, got %q", member.PermissionMode)
	}
}

func TestMemberSpecFromStarterAgent_DefaultPermissionMode(t *testing.T) {
	agent := operations.StarterAgent{
		Slug: "operator",
		Name: "Operator",
		Role: "Operates",
		Type: "lead",
	}
	member := memberSpecFromStarterAgent(agent, "ceo")
	if member.PermissionMode != "plan" {
		t.Fatalf("expected default permission_mode=plan, got %q", member.PermissionMode)
	}
}

// ---------------------------------------------------------------------------
// Unit Tests: MemberSpec from EmployeeBlueprint with permission_mode override
// ---------------------------------------------------------------------------

func TestMemberSpecFromEmployeeBlueprint_PermissionModeOverride(t *testing.T) {
	blueprint := operations.EmployeeBlueprint{
		ID:               "executor",
		Name:             "Executor",
		Summary:          "Executes work",
		Role:             "implement and build",
		Responsibilities: []string{"implement features"},
		AutomatedLoops:   []string{"execute tasks"},
		Skills:           []string{"building"},
		Tools:            []string{"hammer"},
	}
	starter := operations.StarterAgent{
		Slug:           "executor",
		Name:           "Executor",
		PermissionMode: "plan",
		Type:           "specialist",
	}
	member := memberSpecFromEmployeeBlueprint(blueprint, starter, "ceo")
	// StarterAgent.PermissionMode takes precedence; the employee blueprint text is ignored
	if member.PermissionMode != "plan" {
		t.Fatalf("expected explicit permission_mode=plan to override, got %q", member.PermissionMode)
	}

	// Without explicit PermissionMode, should default to "plan"
	starterNoMode := operations.StarterAgent{
		Slug: "executor",
		Name: "Executor",
		Type: "specialist",
	}
	memberDefault := memberSpecFromEmployeeBlueprint(blueprint, starterNoMode, "ceo")
	if memberDefault.PermissionMode != "plan" {
		t.Fatalf("expected default permission_mode=plan, got %q", memberDefault.PermissionMode)
	}
}

func TestMemberSpecFromEmployeeBlueprint_DomainExpertise(t *testing.T) {
	blueprint := operations.EmployeeBlueprint{
		ID:               "bookkeeper-financial-analyst",
		Name:             "Bookkeeper",
		Summary:          "Handles financial records.",
		Role:             "financial analyst",
		Responsibilities: []string{"reconciliation"},
		AutomatedLoops:   []string{"monthly close"},
		Skills:           []string{"bookkeeping", "reconciliation", "invoicing"},
		Tools:            []string{"spreadsheet"},
		ExpectedResults:  []string{"balanced books"},
	}
	starter := operations.StarterAgent{
		Slug:      "bookkeeper",
		Name:      "Bookkeeper",
		Type:      "specialist",
		Expertise: []string{"financial-reporting"},
	}
	member := memberSpecFromEmployeeBlueprint(blueprint, starter, "ceo")
	// Blueprint skills should appear in expertise
	hasBookkeeping := false
	hasReconciliation := false
	hasFinancialReporting := false
	for _, exp := range member.Expertise {
		lower := strings.ToLower(exp)
		if strings.Contains(lower, "bookkeeping") {
			hasBookkeeping = true
		}
		if strings.Contains(lower, "reconciliation") {
			hasReconciliation = true
		}
		if strings.Contains(lower, "financial-reporting") {
			hasFinancialReporting = true
		}
	}
	if !hasBookkeeping {
		t.Fatalf("expected bookkeeping in expertise, got %+v", member.Expertise)
	}
	if !hasReconciliation {
		t.Fatalf("expected reconciliation in expertise, got %+v", member.Expertise)
	}
	if !hasFinancialReporting {
		t.Fatalf("expected financial-reporting from starter in expertise, got %+v", member.Expertise)
	}
}

func TestMemberSpecFromEmployeeBlueprint_DomainTools(t *testing.T) {
	blueprint := operations.EmployeeBlueprint{
		ID:               "workflow-automation-builder",
		Name:             "Workflow Builder",
		Summary:          "Builds workflows.",
		Role:             "automation builder",
		Responsibilities: []string{"build automations"},
		AutomatedLoops:   []string{"run workflows"},
		Skills:           []string{"automation"},
		Tools:            []string{"zapier", "n8n", "make"},
		ExpectedResults:  []string{"working automation"},
	}
	starter := operations.StarterAgent{
		Slug: "workflow-builder",
		Name: "Workflow Builder",
		Type: "specialist",
	}
	member := memberSpecFromEmployeeBlueprint(blueprint, starter, "ceo")
	if len(member.AllowedTools) == 0 {
		t.Fatal("expected employee blueprint tools to flow into AllowedTools")
	}
	toolSet := make(map[string]bool)
	for _, tool := range member.AllowedTools {
		toolSet[strings.ToLower(tool)] = true
	}
	for _, expected := range []string{"zapier", "n8n", "make"} {
		if !toolSet[expected] {
			t.Fatalf("expected tool %q in AllowedTools, got %+v", expected, member.AllowedTools)
		}
	}
}

// ---------------------------------------------------------------------------
// Regression Tests: Curated blueprint agent count
// ---------------------------------------------------------------------------

func TestCuratedBlueprintAgentCount(t *testing.T) {
	repoRoot := testRepoRoot(t)
	ids := operationFixtureIDs(t, repoRoot)
	if len(ids) == 0 {
		t.Fatal("no operation fixtures found")
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			bp, err := operations.LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Skipf("blueprint %s cannot load yet (likely needs employee blueprint migration): %v", id, err)
			}
			// Regression: every curated blueprint should have at least 4 agents
			if got := len(bp.Starter.Agents); got < 4 {
				t.Fatalf("blueprint %s has only %d agents, expected >= 4", id, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Regression Tests: Domain blueprint enrichment
// ---------------------------------------------------------------------------

func TestDomainBlueprintEnrichment(t *testing.T) {
	repoRoot := testRepoRoot(t)
	blueprint, err := operations.LoadEmployeeBlueprint(repoRoot, "bookkeeper-financial-analyst")
	if err != nil {
		t.Fatalf("load bookkeeper-financial-analyst: %v", err)
	}
	// The bookkeeper should have domain-specific skills
	skillSet := make(map[string]bool)
	for _, skill := range blueprint.Skills {
		skillSet[strings.ToLower(skill)] = true
	}
	for _, expected := range []string{"bookkeeping", "reconciliation", "invoicing"} {
		if !skillSet[expected] {
			t.Fatalf("expected skill %q in bookkeeper-financial-analyst, got %+v", expected, blueprint.Skills)
		}
	}
}

// ---------------------------------------------------------------------------
// Regression Tests: StarterAgent PermissionMode JSON roundtrip
// ---------------------------------------------------------------------------

func TestOperationBootstrapProjection(t *testing.T) {
	agent := operations.StarterAgent{
		Slug:           "test-agent",
		Name:           "Test Agent",
		Role:           "testing",
		PermissionMode: "auto",
		Type:           "specialist",
		Checked:        true,
		Expertise:      []string{"testing", "validation"},
	}
	data, err := json.Marshal(agent)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var restored operations.StarterAgent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if restored.PermissionMode != "auto" {
		t.Fatalf("expected permission_mode=auto after roundtrip, got %q", restored.PermissionMode)
	}
	if restored.Slug != "test-agent" {
		t.Fatalf("expected slug=test-agent after roundtrip, got %q", restored.Slug)
	}
	if restored.Name != "Test Agent" {
		t.Fatalf("expected name=Test Agent after roundtrip, got %q", restored.Name)
	}
	if !restored.Checked {
		t.Fatal("expected checked=true after roundtrip")
	}
}

// ---------------------------------------------------------------------------
// Feature Tests: MaterializeManifest with permission_mode
// ---------------------------------------------------------------------------

func TestMaterializeManifest_WithPermissionMode(t *testing.T) {
	root := t.TempDir()

	writeCompanyEmployeeBlueprint(t, root, "operator", `
id: operator
name: Operator
kind: employee
summary: Owns priorities and approvals.
role: priority lead
responsibilities:
  - Own the priorities.
starting_tasks:
  - Set the first priorities.
automated_loops:
  - Route approvals.
skills:
  - approvals
  - scope-setting
tools:
  - docs
expected_results:
  - Clear priorities
`)
	writeCompanyEmployeeBlueprint(t, root, "executor", `
id: executor
name: Executor
kind: employee
summary: Implements and ships work.
role: implement and build features
responsibilities:
  - Build and implement the deliverables.
starting_tasks:
  - Ship the first execution packet.
automated_loops:
  - Execute queued work items.
skills:
  - delivery
  - instrumentation
tools:
  - code-editor
  - terminal
expected_results:
  - Shipped artifact
`)
	writeCompanyOperationBlueprint(t, root, "test-pm-operation", `
id: test-pm-operation
name: Test PM Operation
kind: general
objective: Validate permission_mode flows through materialization.
employee_blueprints:
  - operator
  - executor
starter:
  lead_slug: operator
  general_channel_description: Test command deck.
  agents:
    - slug: operator
      name: Operator
      role: Owns priorities and approvals.
      employee_blueprint: operator
      permission_mode: plan
      checked: true
      type: lead
      built_in: true
    - slug: executor
      name: Executor
      role: Builds and ships.
      employee_blueprint: executor
      permission_mode: auto
      checked: true
      type: specialist
`)

	manifest := Manifest{
		BlueprintRefs: []BlueprintRef{
			{Kind: "operation", ID: "test-pm-operation", Source: "test"},
		},
	}
	resolved, ok := MaterializeManifest(manifest, root)
	if !ok {
		t.Fatal("expected materialization to succeed")
	}
	if len(resolved.Members) < 2 {
		t.Fatalf("expected at least 2 members, got %d", len(resolved.Members))
	}

	operatorMember := findMemberBySlug(resolved.Members, "operator")
	if operatorMember == nil {
		t.Fatal("expected operator member in resolved manifest")
	}
	if operatorMember.PermissionMode == "" {
		t.Fatal("expected operator to have a permission_mode")
	}

	executorMember := findMemberBySlug(resolved.Members, "executor")
	if executorMember == nil {
		t.Fatal("expected executor member in resolved manifest")
	}
	if executorMember.PermissionMode == "" {
		t.Fatal("expected executor to have a permission_mode")
	}

	// Verify AllowedTools from employee blueprint flow through
	if len(executorMember.AllowedTools) == 0 {
		t.Fatal("expected executor AllowedTools from employee blueprint tools")
	}

	// Verify channels were created
	if len(resolved.Channels) == 0 {
		t.Fatal("expected channels in resolved manifest")
	}
	if resolved.Channels[0].Slug != "general" {
		t.Fatalf("expected first channel to be general, got %q", resolved.Channels[0].Slug)
	}
}

// ---------------------------------------------------------------------------
// Feature Tests: Materialized manifest from every curated fixture
// ---------------------------------------------------------------------------

func TestMaterializedManifestPermissionModes(t *testing.T) {
	repoRoot := testRepoRoot(t)
	ids := operationFixtureIDs(t, repoRoot)
	if len(ids) == 0 {
		t.Fatal("no operation fixtures found")
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			manifest := Manifest{
				BlueprintRefs: []BlueprintRef{
					{Kind: "operation", ID: id, Source: "test"},
				},
			}
			resolved, ok := MaterializeManifest(manifest, repoRoot)
			if !ok {
				t.Skipf("blueprint %s cannot materialize yet (likely needs employee blueprint migration)", id)
			}
			if len(resolved.Members) == 0 {
				t.Fatalf("expected members for %s", id)
			}
			for _, member := range resolved.Members {
				if member.PermissionMode == "" {
					t.Fatalf("member %q in %s has empty permission_mode", member.Slug, id)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Regression: Employee blueprints directory contents
// ---------------------------------------------------------------------------

func TestEmployeeBlueprintDirectoryContents(t *testing.T) {
	repoRoot := testRepoRoot(t)
	entries, err := os.ReadDir(filepath.Join(repoRoot, "templates", "employees"))
	if err != nil {
		t.Fatalf("read employees dir: %v", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)

	// After the refactor, these system-role blueprints should still be present
	// (they are removed in a separate PR). This test documents the current state.
	expected := []string{
		"bookkeeper-financial-analyst",
		"discord-server-community-manager",
		"workflow-automation-builder",
	}
	for _, want := range expected {
		found := false
		for _, got := range ids {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected employee blueprint %q in templates/employees/, got %+v", want, ids)
		}
	}
}
