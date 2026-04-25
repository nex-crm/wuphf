package company

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/operations"
	"github.com/nex-crm/wuphf/internal/provider"
)

func testRepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func TestLoadManifestFallsBackToDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if manifest.Name == "" || len(manifest.Members) == 0 || len(manifest.Channels) == 0 {
		t.Fatalf("expected default manifest, got %+v", manifest)
	}
}

func TestSaveAndLoadManifestRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "company.json")
	t.Setenv("WUPHF_COMPANY_FILE", path)

	manifest := Manifest{
		Name: "Test Office",
		Lead: "ceo",
		Members: []MemberSpec{
			{Slug: "ceo", Name: "CEO", Role: "CEO", System: true},
			{Slug: "ops", Name: "Ops", Role: "Operations"},
		},
		Channels: []ChannelSpec{
			{Slug: "general", Name: "general", Members: []string{"ceo", "ops"}},
			{Slug: "deals", Name: "deals", Members: []string{"ceo", "ops"}},
		},
		BlueprintRefs: []BlueprintRef{
			{Kind: "operation", ID: " multi-agent-workflow-consulting ", Source: " template "},
			{Kind: "employee", ID: " workflow automation builder ", Source: "template"},
			{Kind: "operation", ID: "multi-agent-workflow-consulting", Source: "template"},
		},
	}
	if err := SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected manifest file: %v", err)
	}

	loaded, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.Name != "Test Office" {
		t.Fatalf("unexpected manifest name: %q", loaded.Name)
	}
	if len(loaded.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(loaded.Channels))
	}
	if got := loaded.ActiveBlueprintRefs(); len(got) != 2 {
		t.Fatalf("expected 2 normalized blueprint refs, got %+v", got)
	}
	if got, want := loaded.BlueprintRefs[0].Kind, "operation"; got != want {
		t.Fatalf("unexpected ref kind: got %q want %q", got, want)
	}
	if got, want := loaded.BlueprintRefs[0].ID, "multi-agent-workflow-consulting"; got != want {
		t.Fatalf("unexpected ref id: got %q want %q", got, want)
	}
	if got, want := loaded.BlueprintRefs[1].Kind, "employee"; got != want {
		t.Fatalf("unexpected second ref kind: got %q want %q", got, want)
	}
	if got, want := loaded.BlueprintRefs[1].ID, "workflow-automation-builder"; got != want {
		t.Fatalf("unexpected second ref id: got %q want %q", got, want)
	}
	for _, ch := range loaded.Channels {
		if ch.Description == "" {
			t.Fatalf("expected channel description for %s", ch.Slug)
		}
		if !containsSlug(ch.Members, "ceo") {
			t.Fatalf("expected CEO to be present in channel %s", ch.Slug)
		}
	}
}

func TestLoadManifestBackfillsBlueprintRefsFromConfigPack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.Save(config.Config{Pack: "coding-team"}); err != nil {
		t.Fatalf("config.Save: %v", err)
	}

	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if got := manifest.ActiveBlueprintRefs(); len(got) != 1 {
		t.Fatalf("expected 1 active blueprint ref, got %+v", got)
	}
	if got, want := manifest.BlueprintRefs[0].Kind, "operation"; got != want {
		t.Fatalf("unexpected ref kind: got %q want %q", got, want)
	}
	if got, want := manifest.BlueprintRefs[0].ID, "coding-team"; got != want {
		t.Fatalf("unexpected ref id: got %q want %q", got, want)
	}
	if got, want := manifest.BlueprintRefs[0].Source, "config"; got != want {
		t.Fatalf("unexpected ref source: got %q want %q", got, want)
	}
}

func TestManifestSurfaceSpecRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "company.json")
	t.Setenv("WUPHF_COMPANY_FILE", path)

	manifest := Manifest{
		Name: "Surface Test",
		Lead: "ceo",
		Members: []MemberSpec{
			{Slug: "ceo", Name: "CEO", Role: "CEO", System: true},
		},
		Channels: []ChannelSpec{
			{
				Slug:    "general",
				Name:    "general",
				Members: []string{"ceo"},
			},
			{
				Slug:    "tg-ops",
				Name:    "Telegram Ops",
				Members: []string{"ceo"},
				Surface: &ChannelSurfaceSpec{
					Provider:    "telegram",
					RemoteID:    "-100123",
					RemoteTitle: "Ops Group",
					Mode:        "supergroup",
					BotTokenEnv: "OPS_BOT_TOKEN",
				},
			},
		},
	}
	if err := SaveManifest(manifest); err != nil {
		t.Fatalf("SaveManifest: %v", err)
	}

	loaded, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}

	var tgChannel *ChannelSpec
	for i, ch := range loaded.Channels {
		if ch.Slug == "tg-ops" {
			tgChannel = &loaded.Channels[i]
			break
		}
	}
	if tgChannel == nil {
		t.Fatal("expected tg-ops channel after reload")
	}
	if tgChannel.Surface == nil {
		t.Fatal("expected surface spec to persist")
	}
	if tgChannel.Surface.Provider != "telegram" {
		t.Fatalf("expected provider=telegram, got %q", tgChannel.Surface.Provider)
	}
	if tgChannel.Surface.RemoteID != "-100123" {
		t.Fatalf("expected remote_id=-100123, got %q", tgChannel.Surface.RemoteID)
	}
	if tgChannel.Surface.RemoteTitle != "Ops Group" {
		t.Fatalf("expected remote_title, got %q", tgChannel.Surface.RemoteTitle)
	}
	if tgChannel.Surface.Mode != "supergroup" {
		t.Fatalf("expected mode=supergroup, got %q", tgChannel.Surface.Mode)
	}
	if tgChannel.Surface.BotTokenEnv != "OPS_BOT_TOKEN" {
		t.Fatalf("expected bot_token_env=OPS_BOT_TOKEN, got %q", tgChannel.Surface.BotTokenEnv)
	}

	// Channel without surface should have nil
	for _, ch := range loaded.Channels {
		if ch.Slug == "general" && ch.Surface != nil {
			t.Fatal("general channel should not have a surface")
		}
	}
}

func TestDefaultManifestHasNoSurface(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	manifest := DefaultManifest()
	if strings.Contains(strings.ToLower(manifest.Description), "founding team") {
		t.Fatalf("default manifest should not reference founding team in description: %q", manifest.Description)
	}
	if got := manifest.ActiveBlueprintRefs(); len(got) != 0 {
		t.Fatalf("expected no active blueprint refs by default when no pack is configured, got %+v", got)
	}
	for _, ch := range manifest.Channels {
		if ch.Surface != nil {
			t.Fatalf("default channel %s should not have a surface", ch.Slug)
		}
	}
}

func TestMaterializeManifestBuildsRuntimeOfficeFromBlueprintRefs(t *testing.T) {
	manifest := Manifest{
		BlueprintRefs: []BlueprintRef{
			{Kind: "operation", ID: "youtube-factory", Source: "test"},
		},
		Members: []MemberSpec{{
			Slug:     "ceo",
			Name:     "Stale CEO",
			Provider: provider.ProviderBinding{Kind: provider.KindClaudeCode},
		}},
	}
	resolved, ok := MaterializeManifest(manifest, testRepoRoot(t))
	if !ok {
		t.Fatal("expected blueprint-backed manifest materialization")
	}
	if resolved.Lead == "" {
		t.Fatalf("expected lead from blueprint, got %+v", resolved)
	}
	if len(resolved.Members) == 0 {
		t.Fatalf("expected members from blueprint, got %+v", resolved)
	}
	if len(resolved.Channels) == 0 || resolved.Channels[0].Slug != "general" {
		t.Fatalf("expected general channel from blueprint, got %+v", resolved.Channels)
	}
	for _, member := range resolved.Members {
		if member.Provider.Kind != "" {
			t.Fatalf("blueprint materialization must not set member provider bindings, got %+v", member)
		}
	}
}

func TestLoadRuntimeManifestMaterializesEveryOperationFixture(t *testing.T) {
	repoRoot := testRepoRoot(t)
	operationIDs := operationFixtureIDs(t, repoRoot)
	if len(operationIDs) == 0 {
		t.Fatal("expected at least one operation blueprint fixture")
	}
	for _, id := range operationIDs {
		t.Run(id, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			path := filepath.Join(t.TempDir(), "company.json")
			t.Setenv("WUPHF_COMPANY_FILE", path)

			raw, err := json.MarshalIndent(Manifest{
				Name:        "Blueprint Office",
				Description: "Refs only startup manifest",
				BlueprintRefs: []BlueprintRef{{
					Kind:   "operation",
					ID:     id,
					Source: "test",
				}},
			}, "", "  ")
			if err != nil {
				t.Fatalf("marshal manifest: %v", err)
			}
			if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}

			runtimeManifest, err := LoadRuntimeManifest(repoRoot)
			if err != nil {
				t.Fatalf("LoadRuntimeManifest: %v", err)
			}
			ref, ok := runtimeManifest.PrimaryBlueprintRef()
			if !ok || ref.ID != id {
				t.Fatalf("expected primary blueprint ref %q, got %+v", id, runtimeManifest.BlueprintRefs)
			}
			if runtimeManifest.Name == "" {
				t.Fatalf("expected runtime manifest name for %s", id)
			}
			if runtimeManifest.Lead == "" {
				t.Fatalf("expected runtime manifest lead for %s", id)
			}
			if len(runtimeManifest.Members) == 0 {
				t.Fatalf("expected members for %s, got %+v", id, runtimeManifest)
			}
			if len(runtimeManifest.Channels) == 0 {
				t.Fatalf("expected channels for %s, got %+v", id, runtimeManifest)
			}
			if runtimeManifest.Channels[0].Slug != "general" {
				t.Fatalf("expected general channel first for %s, got %+v", id, runtimeManifest.Channels)
			}
			for _, ch := range runtimeManifest.Channels {
				for _, value := range []string{ch.Slug, ch.Name, ch.Description} {
					if strings.Contains(value, "{{") || strings.Contains(value, "}}") {
						t.Fatalf("expected rendered runtime channel strings for %s, got channel %+v", id, ch)
					}
				}
			}

			blueprint, err := operations.LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Fatalf("load blueprint %s: %v", id, err)
			}
			if len(blueprint.EmployeeBlueprints) == 0 {
				t.Fatalf("expected employee blueprint refs for %s", id)
			}
			for _, employeeID := range blueprint.EmployeeBlueprints {
				if _, err := operations.LoadEmployeeBlueprint(repoRoot, employeeID); err != nil {
					t.Fatalf("expected employee blueprint %q to load for %s: %v", employeeID, id, err)
				}
			}
		})
	}
}

func TestLoadManifestSupportsRefsOnlyManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "company.json")
	t.Setenv("WUPHF_COMPANY_FILE", path)
	raw := `{
  "name": "Blueprint Office",
  "description": "Refs only manifest",
  "blueprint_refs": [
    {"kind":"operation","id":"youtube-factory","source":"test"}
  ]
}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	manifest, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if got := manifest.ActiveBlueprintRefs(); len(got) != 1 || got[0].ID != "youtube-factory" {
		t.Fatalf("expected refs-only manifest to retain blueprint refs, got %+v", got)
	}
	if len(manifest.Members) == 0 {
		t.Fatalf("expected blueprint-backed members from refs-only manifest, got %+v", manifest)
	}
}

func TestMaterializeManifestUsesEmployeeBlueprintBindings(t *testing.T) {
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
	writeCompanyEmployeeBlueprint(t, root, "planner", `
id: planner
name: Planner
kind: employee
summary: Breaks directives into workstreams.
role: planning lead
responsibilities:
  - Decompose the directive into workstreams.
starting_tasks:
  - Draft the first operating plan.
automated_loops:
  - Convert goals into sequenced tasks.
skills:
  - decomposition
  - sequencing
tools:
  - docs
expected_results:
  - Clear plan
`)
	writeCompanyOperationBlueprint(t, root, "test-operation", `
id: test-operation
name: Test Operation
kind: general
employee_blueprints:
  - operator
  - planner
starter:
  lead_slug: planner
  general_channel_description: Test command deck.
  agents:
    - slug: operator
      name: Operator
      role: Owns priorities and approvals.
      employee_blueprint: operator
      checked: true
      type: lead
      built_in: true
    - slug: planner
      name: Planner
      role: Turns directives into workstreams.
      employee_blueprint: planner
      checked: true
      type: specialist
      personality: Fast and precise.
      expertise:
        - scoping
`)

	resolved, ok := MaterializeManifest(Manifest{
		BlueprintRefs: []BlueprintRef{
			{Kind: "operation", ID: "test-operation", Source: "test"},
		},
	}, root)
	if !ok {
		t.Fatal("expected blueprint-backed manifest materialization")
	}
	if resolved.Lead != "planner" {
		t.Fatalf("expected lead from blueprint, got %+v", resolved.Lead)
	}
	planner := findMemberBySlug(resolved.Members, "planner")
	if planner == nil {
		t.Fatalf("expected planner member in resolved manifest: %+v", resolved.Members)
	}
	if planner.Role != "Turns directives into workstreams." {
		t.Fatalf("expected starter role overlay, got %+v", planner)
	}
	if planner.Personality != "Fast and precise." {
		t.Fatalf("expected starter personality overlay, got %+v", planner)
	}
	if !containsSlug(planner.Expertise, "decomposition") || !containsSlug(planner.Expertise, "scoping") {
		t.Fatalf("expected merged expertise from employee blueprint and starter, got %+v", planner.Expertise)
	}
	if !containsSlug(planner.AllowedTools, "docs") {
		t.Fatalf("expected employee blueprint tools to flow through, got %+v", planner.AllowedTools)
	}
}

func writeCompanyEmployeeBlueprint(t *testing.T, root, id, body string) {
	t.Helper()
	path := filepath.Join(root, "templates", "employees", id)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir employee blueprint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "blueprint.yaml"), []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("write employee blueprint: %v", err)
	}
}

func writeCompanyOperationBlueprint(t *testing.T, root, id, body string) {
	t.Helper()
	path := filepath.Join(root, "templates", "operations", id)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir operation blueprint: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "blueprint.yaml"), []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("write operation blueprint: %v", err)
	}
}

func findMemberBySlug(members []MemberSpec, slug string) *MemberSpec {
	slug = strings.TrimSpace(slug)
	for i := range members {
		if members[i].Slug == slug {
			return &members[i]
		}
	}
	return nil
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
