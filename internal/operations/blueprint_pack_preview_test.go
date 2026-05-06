package operations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadBlueprintPackPreviewMetadata asserts every shipped operation
// blueprint declares the metadata the onboarding pack library needs:
// outcome, category, at least one first task with a real prompt and
// expected output, and at least one declared skill and requirement.
// Acceptance criterion from Phase 4 PR 1: "all built-in packs have
// metadata. Each pack has at least one first-task suggestion."
func TestLoadBlueprintPackPreviewMetadata(t *testing.T) {
	repoRoot := findRepoRoot(t)
	ids := operationFixtureIDs(t, repoRoot)
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			bp, err := LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Fatalf("load blueprint %q: %v", id, err)
			}
			if strings.TrimSpace(bp.Outcome) == "" {
				t.Fatalf("blueprint %q is missing outcome — pack library cards need it", id)
			}
			switch bp.Category {
			case "services", "media", "product":
			default:
				t.Fatalf("blueprint %q has invalid category %q (want services|media|product)", id, bp.Category)
			}
			if len(bp.FirstTasks) == 0 {
				t.Fatalf("blueprint %q has no first_tasks — pack library expects at least one", id)
			}
			for _, ft := range bp.FirstTasks {
				if strings.TrimSpace(ft.Title) == "" {
					t.Fatalf("blueprint %q first task missing title", id)
				}
				if strings.TrimSpace(ft.Prompt) == "" {
					t.Fatalf("blueprint %q first task %q missing prompt", id, ft.Title)
				}
				if strings.TrimSpace(ft.ExpectedOutput) == "" {
					t.Fatalf("blueprint %q first task %q missing expected_output", id, ft.Title)
				}
				if strings.TrimSpace(ft.ID) == "" {
					t.Fatalf("blueprint %q first task %q missing id slug", id, ft.Title)
				}
			}
			if len(bp.Skills) == 0 {
				t.Fatalf("blueprint %q declares no skills — pack library expects at least one", id)
			}
			if len(bp.Requirements) == 0 {
				t.Fatalf("blueprint %q declares no requirements — pack library expects at least one", id)
			}
			for _, req := range bp.Requirements {
				switch req.Kind {
				case "runtime", "api-key", "local-tool":
				default:
					t.Fatalf("blueprint %q requirement %q has invalid kind %q", id, req.Name, req.Kind)
				}
			}
		})
	}
}

// TestLoadBlueprintAcceptsMissingPackPreview confirms a blueprint
// without any pack-preview fields still parses and loads successfully —
// the additive metadata must not break older blueprints that have not
// been migrated yet.
func TestLoadBlueprintAcceptsMissingPackPreview(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "templates", "operations", "legacy-pack", "blueprint.yaml")
	if err := os.MkdirAll(filepath.Dir(yamlPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Reuse a real employee blueprint reference so validation passes.
	repoRoot := findRepoRoot(t)
	const employeeID = "workflow-automation-builder"
	employeePath := filepath.Join(repoRoot, "templates", "employees", employeeID)
	if _, err := os.Stat(employeePath); err != nil {
		t.Skipf("repo employee fixtures not reachable: %v", err)
	}
	if err := mirrorEmployeeFixture(dir, repoRoot, employeeID); err != nil {
		t.Fatalf("mirror employee fixture: %v", err)
	}
	body := `id: legacy-pack
name: Legacy Pack
kind: legacy
description: A pack without any of the new pack-preview metadata.
objective: Prove backwards compatibility.
default_reviewer: ceo
employee_blueprints:
  - workflow-automation-builder
starter:
  lead_slug: ceo
  agents:
    - slug: ceo
      name: CEO
      role: lead
      employee_blueprint: workflow-automation-builder
      permission_mode: plan
      checked: true
      type: lead
      built_in: true
  channels:
    - slug: command
      name: command
      description: command channel
      members: [ceo]
    - slug: build
      name: build
      description: build channel
      members: [ceo]
    - slug: review
      name: review
      description: review channel
      members: [ceo]
    - slug: notes
      name: notes
      description: notes channel
      members: [ceo]
  tasks:
    - channel: command
      owner: ceo
      title: "Kick off"
      details: "Kick off the legacy pack."
`
	if err := os.WriteFile(yamlPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	bp, err := LoadBlueprint(dir, "legacy-pack")
	if err != nil {
		t.Fatalf("expected legacy pack to load without pack-preview metadata, got %v", err)
	}
	if bp.Outcome != "" {
		t.Fatalf("expected outcome empty, got %q", bp.Outcome)
	}
	if len(bp.FirstTasks) != 0 {
		t.Fatalf("expected zero first_tasks, got %d", len(bp.FirstTasks))
	}
	if len(bp.Skills) != 0 {
		t.Fatalf("expected zero skills, got %d", len(bp.Skills))
	}
	if len(bp.Requirements) != 0 {
		t.Fatalf("expected zero requirements, got %d", len(bp.Requirements))
	}
}

// mirrorEmployeeFixture copies a real employee blueprint directory from
// the repo into the tempdir-mounted repo root used by
// TestLoadBlueprintAcceptsMissingPackPreview so validateBlueprint can
// resolve the employee_blueprint reference without assuming any specific
// shipped employee schema.
func mirrorEmployeeFixture(dst, src, id string) error {
	srcDir := filepath.Join(src, "templates", "employees", id)
	dstDir := filepath.Join(dst, "templates", "employees", id)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(srcDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dstDir, entry.Name()), raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}
