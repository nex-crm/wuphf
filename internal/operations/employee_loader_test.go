package operations

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func TestLoadEmployeeBlueprintReadsTrackedTemplate(t *testing.T) {
	blueprint, err := LoadEmployeeBlueprint(testRepoRoot(t), "workflow-automation-builder")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if blueprint.ID != "workflow-automation-builder" {
		t.Fatalf("unexpected id: %+v", blueprint.ID)
	}
	if blueprint.Kind != "employee" {
		t.Fatalf("unexpected kind: %+v", blueprint.Kind)
	}
	if len(blueprint.Responsibilities) == 0 || len(blueprint.AutomatedLoops) == 0 || len(blueprint.Tools) == 0 {
		t.Fatalf("expected populated employee blueprint shape: %+v", blueprint)
	}
}

func TestLoadEmployeeBlueprintDefaultsIDAndKindFromPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "templates", "employees", "planner")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	raw := []byte(strings.TrimSpace(`
name: Planner
summary: Breaks directives into workstreams and milestones.
role: Planning lead
responsibilities:
  - Decompose the directive into workstreams.
starting_tasks:
  - Draft the first operating plan.
automated_loops:
  - Convert goals into sequenced tasks.
skills:
  - decomposition
tools:
  - docs
expected_results:
  - Clear plan
`))
	if err := os.WriteFile(filepath.Join(path, "blueprint.yaml"), raw, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	blueprint, err := LoadEmployeeBlueprint(root, "planner")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if blueprint.ID != "planner" {
		t.Fatalf("expected path-derived id, got %+v", blueprint.ID)
	}
	if blueprint.Kind != "employee" {
		t.Fatalf("expected default kind, got %+v", blueprint.Kind)
	}
}

func TestLoadEmployeeBlueprintRejectsIncompleteTemplate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "templates", "employees", "broken")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	raw := []byte(strings.TrimSpace(`
id: broken
name: Broken
summary: Missing the required operational shape.
responsibilities:
  - Only one line
starting_tasks:
  - Only one task
automated_loops:
  - Only one loop
skills:
  - one skill
tools:
  - one tool
`))
	if err := os.WriteFile(filepath.Join(path, "blueprint.yaml"), raw, 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	_, err := LoadEmployeeBlueprint(root, "broken")
	if err == nil {
		t.Fatal("expected validation failure")
	}
	if !strings.Contains(err.Error(), "expected results") {
		t.Fatalf("expected missing expected results error, got %v", err)
	}
}

func TestListEmployeeBlueprintsReturnsTrackedTemplates(t *testing.T) {
	blueprints, err := ListEmployeeBlueprints(testRepoRoot(t))
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if got, want := len(blueprints), 3; got != want {
		t.Fatalf("unexpected employee blueprint count: got %d want %d", got, want)
	}
	got := make(map[string]EmployeeBlueprint, len(blueprints))
	for _, blueprint := range blueprints {
		if err := validateEmployeeBlueprint(blueprint); err != nil {
			t.Fatalf("invalid blueprint %q: %v", blueprint.ID, err)
		}
		got[blueprint.ID] = blueprint
	}
	for _, id := range []string{
		"workflow-automation-builder",
		"bookkeeper-financial-analyst",
		"discord-server-community-manager",
	} {
		if _, ok := got[id]; !ok {
			t.Fatalf("missing tracked employee blueprint %q", id)
		}
	}
}
