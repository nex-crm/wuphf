package team

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/nex-crm/wuphf/internal/config"
)

func buildOperationBootstrapPackageFromRepo(ctx context.Context, profile operationCompanyProfile) (operationBootstrapPackage, error) {
	repoRoot, err := gitRepoRoot()
	if err != nil {
		return operationBootstrapPackage{}, err
	}
	connections, providerName := loadOperationRuntimeConnections(ctx)
	if selected, ok, err := selectOperationBlueprintFile(repoRoot, profile); err != nil {
		return operationBootstrapPackage{}, err
	} else if ok {
		return buildOperationBootstrapPackage(operationPackFile{Path: selected.Path}, selected.Blueprint, operationBacklogDoc{}, operationMonetizationDoc{}, connections, providerName, profile), nil
	}
	if pkg, ok, err := buildOperationBootstrapPackageFromLegacySeedDocs(repoRoot, connections, providerName, profile); err != nil {
		return operationBootstrapPackage{}, err
	} else if ok {
		return pkg, nil
	}
	return buildOperationSynthesizedBootstrapPackage(profile, connections, providerName), nil
}

// handleOperationBootstrapPackage serves the studio + operations
// /bootstrap-package endpoints. It is intentionally a free function, not
// a *Broker method: the body never touches broker state, only config +
// the local repo. This is the dependency-inversion seam for the planned
// internal/operation extraction (Track B) — once the operation cluster
// moves into its own package, this function moves with it and broker.go
// continues to wire the route through the package boundary.
func handleOperationBootstrapPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, _ := config.Load()
	blueprintID := strings.TrimSpace(r.URL.Query().Get("blueprint_id"))
	if blueprintID == "" {
		blueprintID = strings.TrimSpace(r.URL.Query().Get("pack_id"))
	}
	if blueprintID == "" {
		// Manifest's explicit BlueprintRef beats fuzzy-matching company config
		// fields, which may contain onboarding noise that scores against no real
		// blueprint and sends us to the synthesis fallback with a garbage slug.
		blueprintID = currentOperationBlueprintRef()
	}
	profile := operationCompanyProfile{
		BlueprintID: blueprintID,
		Name:        strings.TrimSpace(cfg.CompanyName),
		Description: strings.TrimSpace(cfg.CompanyDescription),
		Goals:       strings.TrimSpace(cfg.CompanyGoals),
		Size:        strings.TrimSpace(cfg.CompanySize),
		Priority:    strings.TrimSpace(cfg.CompanyPriority),
	}
	pkg, err := buildOperationBootstrapPackageFromRepo(r.Context(), profile)
	if err != nil {
		http.Error(w, "failed to build operation bootstrap package: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"package": pkg})
}
