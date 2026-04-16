package operations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadBlueprintRequiresStarterEmployeeBindings(t *testing.T) {
	root := t.TempDir()
	writeEmployeeBlueprint(t, root, "planner", `
id: planner
name: Planner
kind: employee
summary: Breaks directives into workstreams.
role: planning
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
`)
	writeOperationBlueprint(t, root, "test-operation", `
id: test-operation
name: Test Operation
kind: general
objective: Validate loader bindings.
employee_blueprints:
  - planner
starter:
  lead_slug: planner
  agents:
    - slug: planner
      name: Planner
      role: Turns directives into workstreams.
      checked: true
      type: specialist
      expertise:
        - scoping
`)

	_, err := LoadBlueprint(root, "test-operation")
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "requires employee_blueprint or permission_mode") {
		t.Fatalf("expected employee blueprint or permission_mode binding error, got %v", err)
	}
}

func TestLoadBlueprintBackfillsStarterEmployeeBindingsIntoRefs(t *testing.T) {
	root := t.TempDir()
	writeEmployeeBlueprint(t, root, "planner", `
id: planner
name: Planner
kind: employee
summary: Breaks directives into workstreams.
role: planning
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
`)
	writeOperationBlueprint(t, root, "test-operation", `
id: test-operation
name: Test Operation
kind: general
objective: Validate loader bindings.
starter:
  lead_slug: planner
  agents:
    - slug: planner
      name: Planner
      role: Turns directives into workstreams.
      employee_blueprint: planner
      checked: true
      type: specialist
      expertise:
        - scoping
`)

	blueprint, err := LoadBlueprint(root, "test-operation")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(blueprint.EmployeeBlueprints) != 1 || blueprint.EmployeeBlueprints[0] != "planner" {
		t.Fatalf("expected starter binding to populate employee blueprint refs, got %+v", blueprint.EmployeeBlueprints)
	}
}

func writeEmployeeBlueprint(t *testing.T, root, id, body string) {
	t.Helper()
	path := filepath.Join(root, "templates", "employees", id)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir employee blueprint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "blueprint.yaml"), []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("write employee blueprint: %v", err)
	}
}

func writeOperationBlueprint(t *testing.T, root, id, body string) {
	t.Helper()
	path := filepath.Join(root, "templates", "operations", id)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir operation blueprint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "blueprint.yaml"), []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("write operation blueprint: %v", err)
	}
}
