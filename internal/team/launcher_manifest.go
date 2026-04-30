package team

// launcher_manifest.go owns the manifest + onboarding helpers
// (PLAN.md §C14). isOnboarded probes config.json for runtime
// presence; resetManifestToPack/Blueprint rewrites the local
// office.yaml when the user switches packs/blueprints;
// resolveRepoRoot finds the project root by walking up for go.mod
// or templates/; loadRunningSessionMode queries the running broker
// for its session mode (used by ResetSession). Split out of
// launcher.go because these helpers operate on filesystem +
// running broker state rather than Launcher fields.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/onboarding"
)

// isOnboarded reports whether the user has completed the onboarding wizard.
// Any error loading state is treated as not-onboarded so a corrupt or
// missing ~/.wuphf/onboarded.json still lets the web UI boot into the
// wizard rather than failing at preflight.
func isOnboarded() bool {
	s, err := onboarding.Load()
	if err != nil || s == nil {
		return false
	}
	return s.Onboarded()
}

// resetManifestToPack overwrites company.json with the members defined in the
// given legacy pack. Called when the user passes --pack explicitly so the flag
// remains authoritative over any previously saved company configuration.
func resetManifestToPack(pack *agent.PackDefinition) error {
	members := make([]company.MemberSpec, 0, len(pack.Agents))
	for _, cfg := range pack.Agents {
		members = append(members, company.MemberSpec{
			Slug:           cfg.Slug,
			Name:           cfg.Name,
			Role:           cfg.Name,
			Expertise:      append([]string(nil), cfg.Expertise...),
			Personality:    cfg.Personality,
			PermissionMode: cfg.PermissionMode,
			AllowedTools:   append([]string(nil), cfg.AllowedTools...),
			System:         cfg.Slug == pack.LeadSlug || cfg.Slug == "ceo",
		})
	}
	manifest := company.Manifest{
		Name:    pack.Name,
		Lead:    pack.LeadSlug,
		Members: members,
	}
	return company.SaveManifest(manifest)
}

func resetManifestToOperationBlueprint(repoRoot, blueprintID string) error {
	manifest := company.Manifest{
		BlueprintRefs: []company.BlueprintRef{{
			Kind:   "operation",
			ID:     blueprintID,
			Source: "launcher",
		}},
	}
	resolved, ok := company.MaterializeManifest(manifest, repoRoot)
	if !ok {
		return fmt.Errorf("materialize operation blueprint %q", blueprintID)
	}
	return company.SaveManifest(resolved)
}

func resolveRepoRoot(start string) string {
	start = strings.TrimSpace(start)
	if start == "" {
		start = "."
	}
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		if _, err := os.Stat(filepath.Join(current, "templates")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return start
		}
		current = parent
	}
}

func loadRunningSessionMode() (string, string) {
	token := strings.TrimSpace(os.Getenv("WUPHF_BROKER_TOKEN"))
	if token == "" {
		return SessionModeOffice, DefaultOneOnOneAgent
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brokerBaseURL()+"/session-mode", nil)
	if err != nil {
		return SessionModeOffice, DefaultOneOnOneAgent
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return SessionModeOffice, DefaultOneOnOneAgent
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return SessionModeOffice, DefaultOneOnOneAgent
	}

	var result struct {
		SessionMode   string `json:"session_mode"`
		OneOnOneAgent string `json:"one_on_one_agent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return SessionModeOffice, DefaultOneOnOneAgent
	}
	return NormalizeSessionMode(result.SessionMode), NormalizeOneOnOneAgent(result.OneOnOneAgent)
}
