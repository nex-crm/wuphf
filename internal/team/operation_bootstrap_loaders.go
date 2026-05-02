package team

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/operations"
)

func loadOperationChannelPackFiles(rootDir string) ([]operationPackFile, error) {
	matches := make([]string, 0, 8)
	if err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(d.Name())
		if strings.HasSuffix(name, "operation-pack.yaml") || strings.HasSuffix(name, "channel-pack.yaml") {
			matches = append(matches, path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no operation pack files found under %s", rootDir)
	}
	sort.Strings(matches)
	packs := make([]operationPackFile, 0, len(matches))
	for _, path := range matches {
		doc, err := loadOperationChannelPackDoc(path)
		if err != nil {
			return nil, err
		}
		packs = append(packs, operationPackFile{Path: path, Doc: doc})
	}
	return packs, nil
}

func loadOperationBlueprintFiles(repoRoot string) ([]operationBlueprintFile, error) {
	rootDir := filepath.Join(repoRoot, "templates", "operations")
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)
	out := make([]operationBlueprintFile, 0, len(ids))
	for _, id := range ids {
		blueprint, err := operations.LoadBlueprint(repoRoot, id)
		if err != nil {
			return nil, err
		}
		out = append(out, operationBlueprintFile{
			Path:      filepath.Join(rootDir, id, "blueprint.yaml"),
			Blueprint: blueprint,
		})
	}
	return out, nil
}

func loadOperationChannelPackFilesOptional(rootDir string) ([]operationPackFile, error) {
	packs, err := loadOperationChannelPackFiles(rootDir)
	if err == nil {
		return packs, nil
	}
	if os.IsNotExist(err) || strings.Contains(strings.ToLower(err.Error()), "no operation pack files found") {
		return nil, nil
	}
	return nil, err
}

func loadOperationChannelPackDoc(path string) (operationChannelPackDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return operationChannelPackDoc{}, err
	}
	var doc operationChannelPackDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return operationChannelPackDoc{}, err
	}
	return doc, nil
}

func loadOperationBacklogDoc(path string) (operationBacklogDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return operationBacklogDoc{}, err
	}
	var doc operationBacklogDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return operationBacklogDoc{}, err
	}
	sort.SliceStable(doc.Episodes, func(i, j int) bool {
		return doc.Episodes[i].Priority < doc.Episodes[j].Priority
	})
	return doc, nil
}

func loadOperationBacklogDocOptional(path string) (operationBacklogDoc, error) {
	doc, err := loadOperationBacklogDoc(path)
	if err == nil || !os.IsNotExist(err) {
		return doc, err
	}
	return operationBacklogDoc{}, nil
}

func loadOperationBacklogDocOptionalCandidates(paths ...string) (operationBacklogDoc, error) {
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		doc, err := loadOperationBacklogDocOptional(path)
		if err != nil {
			return operationBacklogDoc{}, err
		}
		if len(doc.Episodes) > 0 {
			return doc, nil
		}
	}
	return operationBacklogDoc{}, nil
}

func loadOperationMonetizationDoc(path string) (operationMonetizationDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return operationMonetizationDoc{}, err
	}
	var doc operationMonetizationDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return operationMonetizationDoc{}, err
	}
	return doc, nil
}

func loadOperationMonetizationDocOptional(path string) (operationMonetizationDoc, error) {
	doc, err := loadOperationMonetizationDoc(path)
	if err == nil || !os.IsNotExist(err) {
		return doc, err
	}
	return operationMonetizationDoc{}, nil
}

func loadOperationMonetizationDocOptionalCandidates(paths ...string) (operationMonetizationDoc, error) {
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		doc, err := loadOperationMonetizationDocOptional(path)
		if err != nil {
			return operationMonetizationDoc{}, err
		}
		if len(doc.Offers.LeadMagnets) > 0 || len(doc.Offers.DigitalProducts) > 0 || len(doc.Offers.Services) > 0 {
			return doc, nil
		}
	}
	return operationMonetizationDoc{}, nil
}

func selectOperationPackFile(packs []operationPackFile, profile operationCompanyProfile) (operationPackFile, error) {
	if len(packs) == 0 {
		return operationPackFile{}, fmt.Errorf("no operation packs available")
	}
	if wanted := strings.TrimSpace(strings.ToLower(profile.BlueprintID)); wanted != "" {
		for _, pack := range packs {
			base := strings.ToLower(strings.TrimSuffix(filepath.Base(pack.Path), filepath.Ext(pack.Path)))
			if strings.ToLower(pack.Doc.Metadata.ID) == wanted || base == wanted {
				return pack, nil
			}
		}
	}
	query := strings.ToLower(strings.Join([]string{
		profile.Name,
		profile.Description,
		profile.Goals,
		profile.Size,
		profile.Priority,
	}, " "))
	best := packs[0]
	bestScore := operationPackScore(best.Doc, query)
	for _, pack := range packs[1:] {
		if score := operationPackScore(pack.Doc, query); score > bestScore {
			best = pack
			bestScore = score
		}
	}
	if bestScore <= 0 {
		for _, pack := range packs {
			if strings.Contains(strings.ToLower(pack.Doc.Metadata.ID), "default") {
				return pack, nil
			}
		}
		if strings.TrimSpace(query) != "" {
			return operationPackFile{}, fmt.Errorf("no matching operation pack")
		}
	}
	return best, nil
}

func selectOperationBlueprintFile(repoRoot string, profile operationCompanyProfile) (operationBlueprintFile, bool, error) {
	if wanted := operationSlug(profile.BlueprintID); wanted != "" {
		if blueprint, err := operations.LoadBlueprint(repoRoot, wanted); err == nil {
			return operationBlueprintFile{
				Path:      filepath.Join(repoRoot, "templates", "operations", wanted, "blueprint.yaml"),
				Blueprint: blueprint,
			}, true, nil
		}
	}
	files, err := loadOperationBlueprintFiles(repoRoot)
	if err != nil {
		return operationBlueprintFile{}, false, err
	}
	if len(files) == 0 {
		return operationBlueprintFile{}, false, nil
	}
	query := normalizeOperationBlueprintSelector(strings.Join([]string{
		profile.BlueprintID,
		profile.Name,
		profile.Description,
		profile.Goals,
		profile.Size,
		profile.Priority,
	}, " "))
	best := operationBlueprintFile{}
	bestScore := 0
	for _, file := range files {
		if score := operationBlueprintScore(file.Blueprint, query); score > bestScore {
			best = file
			bestScore = score
		}
	}
	if bestScore > 0 {
		return best, true, nil
	}
	if query == "" {
		if ref := currentOperationBlueprintRef(); ref != "" {
			for _, file := range files {
				if file.Blueprint.ID == ref {
					return file, true, nil
				}
			}
		}
	}
	return operationBlueprintFile{}, false, nil
}

func currentOperationBlueprintRef() string {
	manifest, err := company.LoadManifest()
	if err != nil {
		return ""
	}
	refs := manifest.BlueprintRefsByKind("operation")
	if len(refs) == 0 {
		return ""
	}
	return strings.TrimSpace(refs[0].ID)
}

func operationBlueprintScore(blueprint operations.Blueprint, query string) int {
	if query == "" {
		return 0
	}
	score := 0
	candidates := []struct {
		Value  string
		Weight int
	}{
		{blueprint.ID, 10},
		{blueprint.Name, 8},
		{blueprint.Kind, 4},
		{blueprint.Description, 3},
		{blueprint.Objective, 2},
	}
	for _, candidate := range candidates {
		value := normalizeOperationBlueprintSelector(candidate.Value)
		if value == "" {
			continue
		}
		if strings.Contains(query, value) {
			score += candidate.Weight
			continue
		}
		for _, token := range strings.Fields(value) {
			if len(token) < 4 {
				continue
			}
			if strings.Contains(query, token) {
				score++
			}
		}
	}
	return score
}

func normalizeOperationBlueprintSelector(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("_", " ", "-", " ", "/", " ", ".", " ")
	return strings.Join(strings.Fields(replacer.Replace(value)), " ")
}

// Legacy doc translators remain as a compatibility fallback for older repos.
// The active bootstrap path should resolve a blueprint directly from templates.
func buildOperationBootstrapPackageFromLegacySeedDocs(repoRoot string, runtimeConnections []action.Connection, providerName string, profile operationCompanyProfile) (operationBootstrapPackage, bool, error) {
	packs, err := loadOperationChannelPackFilesOptional(filepath.Join(repoRoot, "docs"))
	if err != nil {
		return operationBootstrapPackage{}, false, err
	}
	if len(packs) == 0 {
		return operationBootstrapPackage{}, false, nil
	}
	selected, err := selectOperationPackFile(packs, profile)
	if err != nil {
		// Legacy seed docs exist but none match this profile (selector
		// returns "no matching operation pack" / "no operation packs").
		// That's not a hard error: the caller falls back to the
		// synthesized bootstrap. Signal "not found" via ok=false.
		return operationBootstrapPackage{}, false, nil //nolint:nilerr // intentional: legacy-pack miss falls back to synthesis
	}
	blueprint, err := operations.LoadBlueprint(repoRoot, operationFirstNonEmpty(selected.Doc.Workspace.PipelineID, selected.Doc.Metadata.ID))
	if err != nil {
		// The legacy pack referenced a blueprint that no longer ships in
		// the templates dir. Same posture as above: fall back to the
		// synthesized bootstrap rather than fail the boot.
		return operationBootstrapPackage{}, false, nil //nolint:nilerr // intentional: missing blueprint falls back to synthesis
	}
	sourceDir := filepath.Dir(selected.Path)
	backlog, err := loadOperationBacklogDocOptionalCandidates(
		filepath.Join(sourceDir, "operation-backlog.yaml"),
		filepath.Join(sourceDir, "content-backlog.yaml"),
	)
	if err != nil {
		return operationBootstrapPackage{}, false, err
	}
	monetization, err := loadOperationMonetizationDocOptionalCandidates(
		filepath.Join(sourceDir, "operation-offers.yaml"),
		filepath.Join(sourceDir, "operation-monetization.yaml"),
		filepath.Join(sourceDir, "monetization-registry.yaml"),
	)
	if err != nil {
		return operationBootstrapPackage{}, false, err
	}
	return buildOperationBootstrapPackage(selected, blueprint, backlog, monetization, runtimeConnections, providerName, profile), true, nil
}

func operationPackScore(doc operationChannelPackDoc, query string) int {
	if strings.TrimSpace(query) == "" {
		return 0
	}
	score := 0
	candidates := []struct {
		Value  string
		Weight int
	}{
		{doc.Metadata.ID, 6},
		{doc.Metadata.Purpose, 2},
		{doc.Workspace.WorkspaceID, 5},
		{doc.Channel.BrandName, 10},
		{doc.Channel.Thesis, 6},
		{doc.Channel.Tagline, 3},
		{doc.Channel.ShortBio, 2},
	}
	for _, candidate := range candidates {
		value := strings.ToLower(strings.TrimSpace(candidate.Value))
		if value == "" {
			continue
		}
		if strings.Contains(query, value) {
			score += candidate.Weight
			continue
		}
		for _, token := range strings.Fields(value) {
			if len(token) < 4 {
				continue
			}
			if strings.Contains(query, token) {
				score++
			}
		}
	}
	return score
}
