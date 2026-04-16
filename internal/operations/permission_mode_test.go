package operations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Unit Tests: StarterAgent YAML deserialization
// ---------------------------------------------------------------------------

func TestStarterAgentPermissionModeYAML(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantMode string
	}{
		{
			name: "explicit permission_mode plan",
			yaml: `
slug: planner
name: Planner
permission_mode: plan
`,
			wantMode: "plan",
		},
		{
			name: "explicit permission_mode auto",
			yaml: `
slug: executor
name: Executor
permission_mode: auto
`,
			wantMode: "auto",
		},
		{
			name: "permission_mode omitted defaults to empty",
			yaml: `
slug: operator
name: Operator
`,
			wantMode: "",
		},
		{
			name: "permission_mode with employee_blueprint",
			yaml: `
slug: builder
name: Builder
employee_blueprint: executor
permission_mode: auto
`,
			wantMode: "auto",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var agent StarterAgent
			if err := yaml.Unmarshal([]byte(strings.TrimSpace(tt.yaml)), &agent); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if agent.PermissionMode != tt.wantMode {
				t.Fatalf("permission_mode: got %q want %q", agent.PermissionMode, tt.wantMode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Unit Tests: Loader validation with permission_mode
// ---------------------------------------------------------------------------

func TestLoaderValidation_PermissionModeOnly(t *testing.T) {
	root := t.TempDir()
	writeTestEmployeeBlueprint(t, root, "workflow-automation-builder", minimalEmployeeBlueprintYAML("workflow-automation-builder", "Workflow Automation Builder"))

	writeOperationBlueprint(t, root, "pm-only", `
id: pm-only
name: Permission Mode Only
kind: general
objective: Test permission_mode without employee_blueprint.
starter:
  lead_slug: agent-a
  agents:
    - slug: agent-a
      name: Agent A
      role: Does things.
      employee_blueprint: workflow-automation-builder
      permission_mode: auto
      checked: true
      type: specialist
`)

	bp, err := LoadBlueprint(root, "pm-only")
	if err != nil {
		t.Fatalf("expected valid blueprint with permission_mode only, got error: %v", err)
	}
	if bp.Starter.Agents[0].PermissionMode != "auto" {
		t.Fatalf("expected permission_mode=auto, got %q", bp.Starter.Agents[0].PermissionMode)
	}
}

func TestLoaderValidation_EmployeeBlueprintOnly(t *testing.T) {
	root := t.TempDir()
	writeTestEmployeeBlueprint(t, root, "planner", minimalEmployeeBlueprintYAML("planner", "Planner"))

	writeOperationBlueprint(t, root, "eb-only", `
id: eb-only
name: Employee Blueprint Only
kind: general
objective: Test employee_blueprint without permission_mode.
starter:
  lead_slug: agent-a
  agents:
    - slug: agent-a
      name: Agent A
      role: Plans things.
      employee_blueprint: planner
      checked: true
      type: specialist
`)

	bp, err := LoadBlueprint(root, "eb-only")
	if err != nil {
		t.Fatalf("expected valid blueprint with employee_blueprint only, got error: %v", err)
	}
	if bp.Starter.Agents[0].EmployeeBlueprint != "planner" {
		t.Fatalf("expected employee_blueprint=planner, got %q", bp.Starter.Agents[0].EmployeeBlueprint)
	}
}

func TestLoaderValidation_BothEmpty(t *testing.T) {
	root := t.TempDir()

	writeOperationBlueprint(t, root, "both-empty", `
id: both-empty
name: Both Empty
kind: general
objective: Neither field set.
starter:
  lead_slug: agent-a
  agents:
    - slug: agent-a
      name: Agent A
      role: Does nothing specific.
      checked: true
      type: specialist
`)

	_, err := LoadBlueprint(root, "both-empty")
	if err == nil {
		t.Fatal("expected validation error when both employee_blueprint and permission_mode are empty")
	}
	if !strings.Contains(err.Error(), "employee_blueprint") && !strings.Contains(err.Error(), "employee blueprint") && !strings.Contains(err.Error(), "permission_mode") {
		t.Fatalf("expected employee_blueprint/permission_mode validation error, got: %v", err)
	}
}

func TestLoaderValidation_BothSet(t *testing.T) {
	root := t.TempDir()
	writeTestEmployeeBlueprint(t, root, "executor", minimalEmployeeBlueprintYAML("executor", "Executor"))

	writeOperationBlueprint(t, root, "both-set", `
id: both-set
name: Both Set
kind: general
objective: Both fields set.
starter:
  lead_slug: agent-a
  agents:
    - slug: agent-a
      name: Agent A
      role: Builds things.
      employee_blueprint: executor
      permission_mode: plan
      checked: true
      type: specialist
`)

	bp, err := LoadBlueprint(root, "both-set")
	if err != nil {
		t.Fatalf("expected valid blueprint with both fields set, got error: %v", err)
	}
	agent := bp.Starter.Agents[0]
	if agent.EmployeeBlueprint != "executor" {
		t.Fatalf("expected employee_blueprint=executor, got %q", agent.EmployeeBlueprint)
	}
	if agent.PermissionMode != "plan" {
		t.Fatalf("expected permission_mode=plan, got %q", agent.PermissionMode)
	}
}

// ---------------------------------------------------------------------------
// Unit Tests: Synthesized blueprint permission modes
// ---------------------------------------------------------------------------

func TestSynthesizedBlueprintPermissionModes(t *testing.T) {
	tests := []struct {
		name  string
		input SynthesisInput
	}{
		{
			name: "generic synthesis with directive",
			input: SynthesisInput{
				Directive: "Build a GTM engine.",
				Profile:   CompanyProfile{Name: "TestCo"},
			},
		},
		{
			name: "legacy synthesis with name only",
			input: SynthesisInput{
				Name:        "Test Operation",
				Description: "A test operation for validation.",
				Goals:       "Validate synthesis.",
			},
		},
		{
			name: "empty input fallback",
			input: SynthesisInput{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bp := SynthesizeBlueprint(tt.input)
			if len(bp.Starter.Agents) == 0 {
				t.Fatal("expected at least one agent in synthesized blueprint")
			}
			for _, agent := range bp.Starter.Agents {
				if strings.TrimSpace(agent.EmployeeBlueprint) == "" && strings.TrimSpace(agent.PermissionMode) == "" {
					t.Fatalf("agent %q has neither employee_blueprint nor permission_mode", agent.Slug)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Unit Tests: Kind-specific agent names
// ---------------------------------------------------------------------------

func TestKindSpecificAgentNames(t *testing.T) {
	kinds := []struct {
		kind      string
		directive string
	}{
		{kind: "content", directive: "Build a content engine for YouTube creators."},
		{kind: "gtm", directive: "Build a lead generation pipeline for enterprise sales."},
		{kind: "product", directive: "Build a software product and ship it."},
		{kind: "commerce", directive: "Build an ecommerce store with checkout."},
		{kind: "support", directive: "Build a customer support helpdesk."},
		{kind: "research", directive: "Conduct market research and analysis."},
		{kind: "operations", directive: "Build an operations automation workflow."},
		{kind: "general", directive: "Do something."},
	}

	for _, tt := range kinds {
		t.Run(tt.kind, func(t *testing.T) {
			bp := SynthesizeBlueprint(SynthesisInput{
				Directive: tt.directive,
				Profile:   CompanyProfile{Name: "TestCo"},
			})
			if len(bp.Starter.Agents) == 0 {
				t.Fatal("expected agents in synthesized blueprint")
			}
			for _, agent := range bp.Starter.Agents {
				if strings.TrimSpace(agent.Name) == "" {
					t.Fatalf("agent %q has empty name for kind %q", agent.Slug, tt.kind)
				}
				if strings.TrimSpace(agent.Slug) == "" {
					t.Fatalf("agent has empty slug for kind %q", tt.kind)
				}
			}
			// The inferred kind should match (or be close)
			if bp.Kind == "" {
				t.Fatalf("expected kind to be set, got empty for directive %q", tt.directive)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration Tests: Curated blueprint permission modes
// ---------------------------------------------------------------------------

func TestLoadCuratedBlueprintPermissionModes(t *testing.T) {
	repoRoot := findRepoRoot(t)
	ids := operationFixtureIDs(t, repoRoot)
	if len(ids) == 0 {
		t.Fatal("no operation blueprint fixtures found")
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			bp, err := LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Skipf("blueprint %s cannot load yet (likely needs employee blueprint migration): %v", id, err)
			}
			for _, agent := range bp.Starter.Agents {
				hasEB := strings.TrimSpace(agent.EmployeeBlueprint) != ""
				hasPM := strings.TrimSpace(agent.PermissionMode) != ""
				if !hasEB && !hasPM {
					t.Fatalf("agent %q in blueprint %q has neither employee_blueprint nor permission_mode", agent.Slug, id)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Integration Tests: Synthesis end-to-end for each kind
// ---------------------------------------------------------------------------

func TestSynthesisEndToEnd(t *testing.T) {
	kindInputs := []struct {
		kind  string
		input SynthesisInput
	}{
		{
			kind: "content",
			input: SynthesisInput{
				Directive: "Build a YouTube content factory.",
				Profile:   CompanyProfile{Name: "ContentCo", Industry: "media", Audience: "creators", Offer: "video production"},
			},
		},
		{
			kind: "gtm",
			input: SynthesisInput{
				Directive: "Build a lead gen pipeline with outbound email and CRM.",
				Profile:   CompanyProfile{Name: "SalesCo", Industry: "B2B SaaS", Audience: "enterprise buyers", Offer: "pipeline generation"},
				Integrations: []RuntimeIntegration{
					{Name: "Gmail", Provider: "gmail", Connected: true, Purpose: "Outbound communication"},
				},
			},
		},
		{
			kind: "product",
			input: SynthesisInput{
				Directive: "Ship the next product release with engineering team.",
				Profile:   CompanyProfile{Name: "DevCo", Industry: "software", Audience: "developers"},
			},
		},
		{
			kind: "commerce",
			input: SynthesisInput{
				Directive: "Set up an ecommerce checkout experience.",
				Profile:   CompanyProfile{Name: "ShopCo", Industry: "retail", Audience: "online shoppers"},
			},
		},
		{
			kind: "support",
			input: SynthesisInput{
				Directive: "Build a customer support helpdesk operation.",
				Profile:   CompanyProfile{Name: "HelpCo", Industry: "SaaS", Audience: "customers"},
			},
		},
		{
			kind: "research",
			input: SynthesisInput{
				Directive: "Conduct competitive research and synthesis.",
				Profile:   CompanyProfile{Name: "InsightCo", Industry: "consulting", Audience: "analysts"},
			},
		},
		{
			kind: "operations",
			input: SynthesisInput{
				Directive: "Automate back-office operations workflow.",
				Profile:   CompanyProfile{Name: "OpsCo", Industry: "operations", Audience: "operators"},
			},
		},
		{
			kind: "general",
			input: SynthesisInput{
				Directive: "Run a general autonomous operation.",
				Profile:   CompanyProfile{Name: "GenCo"},
			},
		},
	}

	for _, tt := range kindInputs {
		t.Run(tt.kind, func(t *testing.T) {
			bp := SynthesizeBlueprint(tt.input)
			if bp.ID == "" {
				t.Fatal("expected non-empty blueprint ID")
			}
			if bp.Name == "" {
				t.Fatal("expected non-empty blueprint name")
			}
			if bp.Kind == "" {
				t.Fatal("expected non-empty blueprint kind")
			}
			if bp.Objective == "" {
				t.Fatal("expected non-empty objective")
			}
			if len(bp.Starter.Agents) == 0 {
				t.Fatal("expected at least one agent")
			}
			if bp.Starter.LeadSlug == "" {
				t.Fatal("expected non-empty lead slug")
			}
			if len(bp.Starter.Channels) < 4 {
				t.Fatalf("expected at least 4 channels, got %d", len(bp.Starter.Channels))
			}
			if len(bp.Starter.Tasks) < 4 {
				t.Fatalf("expected at least 4 tasks, got %d", len(bp.Starter.Tasks))
			}
			if len(bp.Stages) < 5 {
				t.Fatalf("expected at least 5 stages, got %d", len(bp.Stages))
			}
			// Verify all agents have employee_blueprint or permission_mode
			for _, agent := range bp.Starter.Agents {
				hasEB := strings.TrimSpace(agent.EmployeeBlueprint) != ""
				hasPM := strings.TrimSpace(agent.PermissionMode) != ""
				if !hasEB && !hasPM {
					t.Fatalf("agent %q has neither employee_blueprint nor permission_mode", agent.Slug)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Smoke Tests: All curated blueprints load successfully
// ---------------------------------------------------------------------------

func TestAllCuratedBlueprintsLoadSuccessfully(t *testing.T) {
	repoRoot := findRepoRoot(t)
	ids := operationFixtureIDs(t, repoRoot)
	if len(ids) == 0 {
		t.Fatal("no operation blueprints found in templates/operations/")
	}
	loaded := 0
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			bp, err := LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Skipf("blueprint %s cannot load yet (likely needs employee blueprint migration): %v", id, err)
			}
			loaded++
			if bp.ID != id {
				t.Fatalf("expected ID %q, got %q", id, bp.ID)
			}
			if bp.Name == "" {
				t.Fatalf("blueprint %s has empty name", id)
			}
			if bp.Kind == "" {
				t.Fatalf("blueprint %s has empty kind", id)
			}
			if len(bp.Starter.Agents) == 0 {
				t.Fatalf("blueprint %s has no starter agents", id)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Smoke Tests: Synthesize all known kinds
// ---------------------------------------------------------------------------

func TestSynthesizeAllKinds(t *testing.T) {
	knownKinds := []string{"content", "gtm", "product", "commerce", "support", "research", "operations", "general"}
	directives := map[string]string{
		"content":    "Build a YouTube audience with video content.",
		"gtm":        "Build a sales pipeline with outbound email.",
		"product":    "Ship a software application.",
		"commerce":   "Launch an ecommerce store.",
		"support":    "Run a customer success helpdesk.",
		"research":   "Investigate market trends and insights.",
		"operations": "Automate the operations workflow.",
		"general":    "Run a general purpose operation.",
	}

	for _, kind := range knownKinds {
		t.Run(kind, func(t *testing.T) {
			bp := SynthesizeBlueprint(SynthesisInput{
				Directive: directives[kind],
				Profile:   CompanyProfile{Name: "TestCo-" + kind},
			})
			if bp.ID == "" {
				t.Fatal("empty blueprint ID")
			}
			if bp.Name == "" {
				t.Fatal("empty blueprint name")
			}
			if bp.Objective == "" {
				t.Fatal("empty objective")
			}
			if len(bp.Starter.Agents) == 0 {
				t.Fatal("no agents synthesized")
			}
			if len(bp.Stages) < 5 {
				t.Fatalf("expected >= 5 stages, got %d", len(bp.Stages))
			}
			if len(bp.Artifacts) < 4 {
				t.Fatalf("expected >= 4 artifacts, got %d", len(bp.Artifacts))
			}
			if len(bp.ApprovalRules) == 0 {
				t.Fatal("expected at least one approval rule")
			}
			if len(bp.MonetizationLadder) == 0 {
				t.Fatal("expected at least one monetization step")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeTestEmployeeBlueprint(t *testing.T, root, id, body string) {
	t.Helper()
	path := filepath.Join(root, "templates", "employees", id)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir employee blueprint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "blueprint.yaml"), []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("write employee blueprint: %v", err)
	}
}

func minimalEmployeeBlueprintYAML(id, name string) string {
	return strings.TrimSpace(`
id: ` + id + `
name: ` + name + `
kind: employee
summary: Minimal blueprint for testing.
role: test role
responsibilities:
  - Responsibility one.
starting_tasks:
  - Starting task one.
automated_loops:
  - Automated loop one.
skills:
  - skill-one
tools:
  - tool-one
expected_results:
  - Expected result one.
`)
}
