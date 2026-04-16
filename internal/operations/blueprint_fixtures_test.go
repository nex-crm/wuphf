package operations

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestLoadOperationBlueprintFixtures(t *testing.T) {
	repoRoot := findRepoRoot(t)
	ids := operationFixtureIDs(t, repoRoot)
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			bp, err := LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Fatalf("load blueprint: %v", err)
			}
			assertBlueprintFixtureShape(t, repoRoot, bp, id)
		})
	}
}

func operationFixtureIDs(t *testing.T, repoRoot string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repoRoot, "templates", "operations"))
	if err != nil {
		t.Fatalf("read operation templates: %v", err)
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	sort.Strings(ids)
	return ids
}

func assertBlueprintFixtureShape(t *testing.T, repoRoot string, bp Blueprint, id string) {
	t.Helper()

	if bp.ID != id {
		t.Fatalf("unexpected blueprint id: got %q want %q", bp.ID, id)
	}
	if strings.TrimSpace(bp.Name) == "" {
		t.Fatalf("expected blueprint name for %s", id)
	}
	if strings.TrimSpace(bp.Kind) == "" {
		t.Fatalf("expected blueprint kind for %s", id)
	}
	if strings.TrimSpace(bp.Objective) == "" {
		t.Fatalf("expected objective to be populated for %s", id)
	}
	for _, ref := range bp.EmployeeBlueprints {
		if _, err := LoadEmployeeBlueprint(repoRoot, ref); err != nil {
			t.Fatalf("expected employee blueprint %q to load for %s: %v", ref, id, err)
		}
	}
	for _, agent := range bp.Starter.Agents {
		if strings.TrimSpace(agent.PermissionMode) != "" {
			continue
		}
		if strings.TrimSpace(agent.EmployeeBlueprint) == "" {
			t.Fatalf("expected starter agent %q to bind an employee blueprint or set permission_mode", agent.Slug)
		}
		if !containsString(bp.EmployeeBlueprints, agent.EmployeeBlueprint) {
			t.Fatalf("expected starter agent %q binding %q to appear in employee blueprint refs %+v", agent.Slug, agent.EmployeeBlueprint, bp.EmployeeBlueprints)
		}
	}
	if strings.TrimSpace(bp.Starter.LeadSlug) == "" {
		t.Fatalf("expected lead slug for %s", id)
	}
	if len(bp.Starter.Agents) < 4 {
		t.Fatalf("expected starter agents, got %+v", bp.Starter.Agents)
	}
	if len(bp.Starter.Channels) < 4 {
		t.Fatalf("expected starter channels, got %+v", bp.Starter.Channels)
	}
	if len(bp.Starter.Tasks) < 4 {
		t.Fatalf("expected starter tasks, got %+v", bp.Starter.Tasks)
	}
	if strings.TrimSpace(bp.BootstrapConfig.ChannelName) == "" || strings.TrimSpace(bp.BootstrapConfig.ChannelSlug) == "" {
		t.Fatalf("expected bootstrap config identifiers, got %+v", bp.BootstrapConfig)
	}
	if len(bp.BootstrapConfig.ContentPillars) < 3 {
		t.Fatalf("expected bootstrap content pillars, got %+v", bp.BootstrapConfig)
	}
	if len(bp.BootstrapConfig.MonetizationHooks) < 3 {
		t.Fatalf("expected bootstrap monetization hooks, got %+v", bp.BootstrapConfig)
	}
	if len(bp.MonetizationLadder) < 4 {
		t.Fatalf("expected monetization ladder, got %+v", bp.MonetizationLadder)
	}
	if len(bp.QueueSeed) < 4 {
		t.Fatalf("expected queue seed, got %+v", bp.QueueSeed)
	}
	if len(bp.Stages) < 5 {
		t.Fatalf("expected at least 5 stages, got %+v", bp.Stages)
	}
	if len(bp.Artifacts) < 4 {
		t.Fatalf("expected artifacts, got %+v", bp.Artifacts)
	}
	if len(bp.Capabilities) < 4 {
		t.Fatalf("expected capabilities, got %+v", bp.Capabilities)
	}
	if len(bp.ApprovalRules) < 4 {
		t.Fatalf("expected approval rules, got %+v", bp.ApprovalRules)
	}
	if len(bp.Connections) < 4 {
		t.Fatalf("expected connections, got %+v", bp.Connections)
	}
	if len(bp.Workflows) < 4 {
		t.Fatalf("expected workflows, got %+v", bp.Workflows)
	}
}

func containsString(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = parent
	}
}
