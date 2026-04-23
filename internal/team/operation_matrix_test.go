package team

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/operations"
)

func TestOperationBlueprintMatrixBuildsBootstrapPackage(t *testing.T) {
	repoRoot := teamTestRepoRoot(t)
	for _, id := range teamOperationFixtureIDs(t, repoRoot) {
		t.Run(id, func(t *testing.T) {
			blueprint, err := operations.LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Fatalf("load blueprint: %v", err)
			}

			pkg := buildOperationBootstrapPackage(
				operationPackFile{},
				blueprint,
				operationBacklogDoc{},
				operationMonetizationDoc{},
				nil,
				"",
				operationCompanyProfile{
					BlueprintID: id,
					Name:        blueprint.Name,
					Description: blueprint.Description,
					Goals:       blueprint.Objective,
				},
			)

			if pkg.Blueprint.ID != id {
				t.Fatalf("unexpected package blueprint id: got %q want %q", pkg.Blueprint.ID, id)
			}
			if pkg.BlueprintID == "" || pkg.BlueprintLabel == "" || pkg.SourcePath == "" {
				t.Fatalf("expected package metadata, got %+v", pkg)
			}
			if pkg.BootstrapConfig.ChannelName == "" || pkg.BootstrapConfig.ChannelSlug == "" {
				t.Fatalf("expected bootstrap config identifiers, got %+v", pkg.BootstrapConfig)
			}
			if len(pkg.Starter.Agents) != len(blueprint.Starter.Agents) {
				t.Fatalf("expected starter agents to mirror blueprint, got %d want %d", len(pkg.Starter.Agents), len(blueprint.Starter.Agents))
			}
			if len(pkg.Starter.Channels) != len(blueprint.Starter.Channels) {
				t.Fatalf("expected starter channels to mirror blueprint, got %d want %d", len(pkg.Starter.Channels), len(blueprint.Starter.Channels))
			}
			if len(pkg.Starter.Tasks) != len(blueprint.Starter.Tasks) {
				t.Fatalf("expected starter tasks to mirror blueprint, got %d want %d", len(pkg.Starter.Tasks), len(blueprint.Starter.Tasks))
			}
			if len(pkg.WorkflowDrafts) != len(blueprint.Workflows) {
				t.Fatalf("expected workflow drafts to mirror blueprint, got %d want %d", len(pkg.WorkflowDrafts), len(blueprint.Workflows))
			}
			if len(pkg.SmokeTests) != teamExpectedSmokeTestCount(blueprint) {
				t.Fatalf("expected smoke tests to mirror blueprint, got %d want %d", len(pkg.SmokeTests), teamExpectedSmokeTestCount(blueprint))
			}
			if len(pkg.Connections) != len(blueprint.Connections) {
				t.Fatalf("expected connection cards to mirror blueprint, got %d want %d", len(pkg.Connections), len(blueprint.Connections))
			}
			if len(pkg.Automation) != len(blueprint.Automation) {
				t.Fatalf("expected automation modules to mirror blueprint, got %d want %d", len(pkg.Automation), len(blueprint.Automation))
			}
			if len(pkg.Offers) == 0 || len(pkg.WorkstreamSeed) == 0 || len(pkg.ValueCapturePlan) == 0 {
				t.Fatalf("expected commercial/bootstrap artifacts, got offers=%d workstream=%d value_capture=%d", len(pkg.Offers), len(pkg.WorkstreamSeed), len(pkg.ValueCapturePlan))
			}
			if len(pkg.QueueSeed) != len(pkg.WorkstreamSeed) || len(pkg.MonetizationLadder) != len(pkg.ValueCapturePlan) {
				t.Fatalf("expected legacy aliases to mirror generic fields, got queue=%d workstream=%d ladder=%d value_capture=%d", len(pkg.QueueSeed), len(pkg.WorkstreamSeed), len(pkg.MonetizationLadder), len(pkg.ValueCapturePlan))
			}
			if strings.Contains(pkg.Starter.GeneralDesc, "{{") || strings.Contains(pkg.BootstrapConfig.ChannelName, "{{") || strings.Contains(pkg.BootstrapConfig.ChannelSlug, "{{") {
				t.Fatalf("expected rendered bootstrap config for %s, got starter=%q cfg=%+v", id, pkg.Starter.GeneralDesc, pkg.BootstrapConfig)
			}

			agentSlugs := make(map[string]struct{}, len(pkg.Starter.Agents))
			for _, agent := range pkg.Starter.Agents {
				if strings.TrimSpace(agent.Slug) == "" || strings.TrimSpace(agent.Name) == "" || strings.TrimSpace(agent.Role) == "" {
					t.Fatalf("expected starter agent to be fully populated, got %+v", agent)
				}
				agentSlugs[agent.Slug] = struct{}{}
			}
			if got, want := len(pkg.Starter.KickoffTagged), 1; got != want {
				t.Fatalf("expected a single kickoff tag, got %d", got)
			}
			if _, ok := agentSlugs[pkg.Starter.KickoffTagged[0]]; !ok {
				t.Fatalf("expected kickoff tag %q to map to a starter agent, got %+v", pkg.Starter.KickoffTagged[0], pkg.Starter.Agents)
			}
			for _, channel := range pkg.Starter.Channels {
				if strings.TrimSpace(channel.Slug) == "" || strings.TrimSpace(channel.Name) == "" {
					t.Fatalf("expected starter channel to be populated, got %+v", channel)
				}
				for _, value := range []string{channel.Slug, channel.Name, channel.Description} {
					if strings.Contains(value, "{{") || strings.Contains(value, "}}") {
						t.Fatalf("expected rendered starter channel strings for %s, got %+v", id, channel)
					}
				}
				for _, member := range channel.Members {
					if _, ok := agentSlugs[member]; !ok {
						t.Fatalf("expected channel member %q to reference a starter agent, got %+v", member, channel)
					}
				}
			}
			for _, task := range pkg.Starter.Tasks {
				if strings.TrimSpace(task.Title) == "" || strings.TrimSpace(task.Channel) == "" || strings.TrimSpace(task.Owner) == "" {
					t.Fatalf("expected starter task to be populated, got %+v", task)
				}
				if _, ok := agentSlugs[task.Owner]; !ok {
					t.Fatalf("expected task owner %q to reference a starter agent, got %+v", task.Owner, task)
				}
			}
		})
	}
}

func TestOperationBlueprintMatrixSeedsBrokerOffice(t *testing.T) {
	repoRoot := teamTestRepoRoot(t)
	oldPathFn := brokerStatePath
	defer func() { brokerStatePath = oldPathFn }()

	for _, id := range teamOperationFixtureIDs(t, repoRoot) {
		t.Run(id, func(t *testing.T) {
			manifestPath := filepath.Join(t.TempDir(), "company.json")
			t.Setenv("WUPHF_COMPANY_FILE", manifestPath)

			raw, err := json.MarshalIndent(company.Manifest{
				Name:        "Blueprint Office",
				Description: "Runtime matrix coverage",
				BlueprintRefs: []company.BlueprintRef{{
					Kind:   "operation",
					ID:     id,
					Source: "test",
				}},
			}, "", "  ")
			if err != nil {
				t.Fatalf("marshal manifest: %v", err)
			}
			if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}

			stateDir := t.TempDir()
			brokerStatePath = func() string { return filepath.Join(stateDir, "broker-state.json") }

			blueprint, err := operations.LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Fatalf("load blueprint: %v", err)
			}

			b := NewBroker()
			members := b.OfficeMembers()
			if len(members) != len(blueprint.Starter.Agents) {
				t.Fatalf("expected broker office roster to match starter agents, got %d want %d", len(members), len(blueprint.Starter.Agents))
			}

			memberBySlug := make(map[string]officeMember, len(members))
			for _, member := range members {
				memberBySlug[member.Slug] = member
			}
			for _, starter := range blueprint.Starter.Agents {
				member, ok := memberBySlug[starter.Slug]
				if !ok {
					t.Fatalf("expected starter agent %q in office roster, got %+v", starter.Slug, members)
				}
				if strings.TrimSpace(member.Name) == "" || strings.TrimSpace(member.Role) == "" {
					t.Fatalf("expected populated office member for %q, got %+v", starter.Slug, member)
				}
				if strings.TrimSpace(starter.EmployeeBlueprint) != "" && len(member.Expertise) == 0 {
					t.Fatalf("expected employee-blueprint-backed expertise for %q, got %+v", starter.Slug, member)
				}
			}

			b.mu.Lock()
			general := b.findChannelLocked("general")
			b.mu.Unlock()
			if general == nil {
				t.Fatal("expected general channel to exist")
			}
			for _, starter := range blueprint.Starter.Agents {
				if !teamContainsString(general.Members, starter.Slug) {
					t.Fatalf("expected general channel to include %q, got %+v", starter.Slug, general.Members)
				}
			}
		})
	}
}

func TestOperationBlueprintMatrixServesBootstrapPackageEndpoint(t *testing.T) {
	repoRoot := teamTestRepoRoot(t)
	oldPathFn := brokerStatePath
	defer func() { brokerStatePath = oldPathFn }()

	for _, id := range teamOperationFixtureIDs(t, repoRoot) {
		t.Run(id, func(t *testing.T) {
			manifestPath := filepath.Join(t.TempDir(), "company.json")
			t.Setenv("WUPHF_COMPANY_FILE", manifestPath)

			raw, err := json.MarshalIndent(company.Manifest{
				Name:        "Blueprint Office",
				Description: "Runtime matrix coverage",
				BlueprintRefs: []company.BlueprintRef{{
					Kind:   "operation",
					ID:     id,
					Source: "test",
				}},
			}, "", "  ")
			if err != nil {
				t.Fatalf("marshal manifest: %v", err)
			}
			if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o600); err != nil {
				t.Fatalf("write manifest: %v", err)
			}

			stateDir := t.TempDir()
			brokerStatePath = func() string { return filepath.Join(stateDir, "broker-state.json") }

			blueprint, err := operations.LoadBlueprint(repoRoot, id)
			if err != nil {
				t.Fatalf("load blueprint: %v", err)
			}

			b := NewBroker()
			req := httptest.NewRequest(http.MethodGet, "/operations/bootstrap-package", nil)
			rec := httptest.NewRecorder()
			b.handleOperationBootstrapPackage(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
			}

			var resp struct {
				Package operationBootstrapPackage `json:"package"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			pkg := resp.Package
			if pkg.Blueprint.ID != id {
				t.Fatalf("unexpected endpoint blueprint id: got %q want %q", pkg.Blueprint.ID, id)
			}
			if !strings.Contains(filepath.ToSlash(pkg.SourcePath), filepath.ToSlash(filepath.Join("templates", "operations", id, "blueprint.yaml"))) {
				t.Fatalf("expected template blueprint source path, got %q", pkg.SourcePath)
			}
			if len(pkg.Starter.Agents) != len(blueprint.Starter.Agents) {
				t.Fatalf("expected starter agents to mirror blueprint, got %d want %d", len(pkg.Starter.Agents), len(blueprint.Starter.Agents))
			}
			if len(pkg.Starter.Channels) != len(blueprint.Starter.Channels) {
				t.Fatalf("expected starter channels to mirror blueprint, got %d want %d", len(pkg.Starter.Channels), len(blueprint.Starter.Channels))
			}
			if len(pkg.Starter.Tasks) != len(blueprint.Starter.Tasks) {
				t.Fatalf("expected starter tasks to mirror blueprint, got %d want %d", len(pkg.Starter.Tasks), len(blueprint.Starter.Tasks))
			}
			if len(pkg.WorkflowDrafts) != len(blueprint.Workflows) {
				t.Fatalf("expected workflow drafts to mirror blueprint, got %d want %d", len(pkg.WorkflowDrafts), len(blueprint.Workflows))
			}
			if len(pkg.SmokeTests) != teamExpectedSmokeTestCount(blueprint) {
				t.Fatalf("expected smoke tests to mirror blueprint, got %d want %d", len(pkg.SmokeTests), teamExpectedSmokeTestCount(blueprint))
			}
			if len(pkg.Connections) != len(blueprint.Connections) {
				t.Fatalf("expected connection cards to mirror blueprint, got %d want %d", len(pkg.Connections), len(blueprint.Connections))
			}
			if len(pkg.Automation) != len(blueprint.Automation) {
				t.Fatalf("expected automation modules to mirror blueprint, got %d want %d", len(pkg.Automation), len(blueprint.Automation))
			}
			if pkg.BootstrapConfig.ChannelName == "" || pkg.BootstrapConfig.ChannelSlug == "" {
				t.Fatalf("expected bootstrap config identifiers, got %+v", pkg.BootstrapConfig)
			}
			if len(pkg.Offers) == 0 || len(pkg.WorkstreamSeed) == 0 || len(pkg.ValueCapturePlan) == 0 {
				t.Fatalf("expected commercial artifacts in endpoint package, got offers=%d workstream=%d value_capture=%d", len(pkg.Offers), len(pkg.WorkstreamSeed), len(pkg.ValueCapturePlan))
			}
			if len(pkg.QueueSeed) != len(pkg.WorkstreamSeed) || len(pkg.MonetizationLadder) != len(pkg.ValueCapturePlan) {
				t.Fatalf("expected endpoint legacy aliases to mirror generic fields, got queue=%d workstream=%d ladder=%d value_capture=%d", len(pkg.QueueSeed), len(pkg.WorkstreamSeed), len(pkg.MonetizationLadder), len(pkg.ValueCapturePlan))
			}
		})
	}
}

func TestOperationBlueprintMatrixNewLauncherAcceptsAllBlueprints(t *testing.T) {
	repoRoot := teamTestRepoRoot(t)
	for _, id := range teamOperationFixtureIDs(t, repoRoot) {
		t.Run(id, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			// Pair HOME with WUPHF_RUNTIME_HOME so NewLauncher writes its
			// blueprint-materialized company.json into this test's tmpdir,
			// not the process-level leaked runtime home from
			// worktree_guard_test's init — that leak pollutes downstream
			// tests that read company.json via NewBroker.
			t.Setenv("WUPHF_RUNTIME_HOME", home)
			t.Setenv("WUPHF_BROKER_TOKEN", "")
			if err := config.Save(config.Config{LLMProvider: "codex"}); err != nil {
				t.Fatalf("save config: %v", err)
			}

			l, err := NewLauncher(id)
			if err != nil {
				t.Fatalf("NewLauncher(%q): %v", id, err)
			}
			if got, want := l.packSlug, id; got != want {
				t.Fatalf("unexpected launcher blueprint id: got %q want %q", got, want)
			}
			if l.pack != nil {
				t.Fatalf("expected no static pack for operation blueprint launch, got %+v", l.pack)
			}
			if l.provider != "codex" {
				t.Fatalf("expected codex provider, got %q", l.provider)
			}
		})
	}
}

func TestSynthesizedOperationBootstrapPackageUsesGenericFallbackCopy(t *testing.T) {
	pkg := buildOperationSynthesizedBootstrapPackage(operationCompanyProfile{
		Name:        "Field Service Ops",
		Description: "Run dispatch, quoting, approvals, and customer follow-up for a field team.",
		Goals:       "Stand up a repeatable service-delivery operation with approval-gated customer actions.",
		Size:        "5-10",
	}, nil, "")

	if pkg.Blueprint.ID == "" {
		t.Fatalf("expected synthesized blueprint metadata, got %+v", pkg.Blueprint)
	}
	if pkg.BootstrapConfig.ChannelName == "" || pkg.BootstrapConfig.Positioning == "" {
		t.Fatalf("expected synthesized bootstrap config identifiers, got %+v", pkg.BootstrapConfig)
	}
	if len(pkg.Offers) == 0 || len(pkg.WorkstreamSeed) == 0 || len(pkg.ValueCapturePlan) == 0 {
		t.Fatalf("expected synthesized operating artifacts, got offers=%d workstream=%d value_capture=%d", len(pkg.Offers), len(pkg.WorkstreamSeed), len(pkg.ValueCapturePlan))
	}

	raw, err := json.Marshal(pkg)
	if err != nil {
		t.Fatalf("marshal synthesized package: %v", err)
	}
	lower := strings.ToLower(string(raw))
	for _, needle := range []string{"channel pack", "channel business", "viewers"} {
		if strings.Contains(lower, needle) {
			t.Fatalf("expected synthesized package to avoid legacy media copy %q, got %s", needle, raw)
		}
	}
}

func teamOperationFixtureIDs(t *testing.T, repoRoot string) []string {
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

func teamExpectedSmokeTestCount(blueprint operations.Blueprint) int {
	count := 0
	for _, workflow := range blueprint.Workflows {
		if strings.TrimSpace(workflow.SmokeTest.Name) != "" {
			count++
		}
	}
	return count
}

func teamContainsString(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func teamTestRepoRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
