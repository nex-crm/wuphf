package team

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/operations"
)

type operationCompanyProfile struct {
	BlueprintID string
	Name        string
	Description string
	Goals       string
	Size        string
	Priority    string
}

type operationBootstrapPackage struct {
	BlueprintID        string                      `json:"blueprint_id"`
	BlueprintLabel     string                      `json:"blueprint_label,omitempty"`
	PackID             string                      `json:"pack_id,omitempty"`    // legacy alias
	PackLabel          string                      `json:"pack_label,omitempty"` // legacy alias
	SourcePath         string                      `json:"source_path,omitempty"`
	ConnectionProvider string                      `json:"connection_provider,omitempty"`
	Blueprint          operations.Blueprint        `json:"blueprint,omitempty"`
	BootstrapConfig    operationBootstrapConfig    `json:"bootstrap_config"`
	Starter            operationStarterTemplate    `json:"starter"`
	Automation         []operationAutomationModule `json:"automation"`
	Integrations       []operationIntegrationStub  `json:"integrations"`
	Connections        []operationConnectionCard   `json:"connections"`
	SmokeTests         []operationSmokeTest        `json:"smoke_tests"`
	WorkflowDrafts     []operationWorkflowDraft    `json:"workflow_drafts"`
	ValueCapturePlan   []operationMonetizationStep `json:"value_capture_plan"`
	MonetizationLadder []operationMonetizationStep `json:"monetization_ladder"` // legacy alias
	WorkstreamSeed     []operationQueueItem        `json:"workstream_seed"`
	QueueSeed          []operationQueueItem        `json:"queue_seed"` // legacy alias
	Offers             []operationOffer            `json:"offers"`
}

type operationBootstrapConfig struct {
	ChannelName       string                       `json:"channel_name"`
	ChannelSlug       string                       `json:"channel_slug"`
	Niche             string                       `json:"niche,omitempty"`
	Audience          string                       `json:"audience,omitempty"`
	Positioning       string                       `json:"positioning,omitempty"`
	ContentPillars    []string                     `json:"content_pillars,omitempty"`
	ContentSeries     []string                     `json:"content_series,omitempty"`
	MonetizationHooks []string                     `json:"monetization_hooks,omitempty"`
	PublishingCadence string                       `json:"publishing_cadence,omitempty"`
	LeadMagnet        operationLeadMagnet          `json:"lead_magnet,omitempty"`
	MonetizationAsset []operationMonetizationAsset `json:"monetization_assets,omitempty"`
	KPITracking       []operationKPI               `json:"kpi_tracking,omitempty"`
}

type operationLeadMagnet struct {
	Name string `json:"name,omitempty"`
	CTA  string `json:"cta,omitempty"`
	Path string `json:"path,omitempty"`
}

type operationMonetizationAsset struct {
	Stage string `json:"stage,omitempty"`
	Name  string `json:"name,omitempty"`
	Slot  string `json:"slot,omitempty"`
	CTA   string `json:"cta,omitempty"`
}

type operationKPI struct {
	Name string `json:"name,omitempty"`
	// Target is intentionally stringly typed because these are business targets,
	// not strongly typed metrics in the UI today.
	Target string `json:"target,omitempty"`
	Why    string `json:"why,omitempty"`
}

type operationAutomationModule struct {
	ID     string `json:"id"`
	Kicker string `json:"kicker,omitempty"`
	Title  string `json:"title"`
	Copy   string `json:"copy,omitempty"`
	Status string `json:"status,omitempty"`
	Footer string `json:"footer,omitempty"`
}

type operationIntegrationStub struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type operationConnectionCard struct {
	Name        string `json:"name"`
	Integration string `json:"integration,omitempty"`
	Owner       string `json:"owner,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Mode        string `json:"mode,omitempty"`
	State       string `json:"state,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
	SmokeTest   string `json:"smokeTest,omitempty"`
	Blocker     string `json:"blocker,omitempty"`
}

type operationSmokeTest struct {
	Name         string         `json:"name"`
	WorkflowKey  string         `json:"workflowKey"`
	Mode         string         `json:"mode"`
	Integrations []string       `json:"integrations,omitempty"`
	Proof        string         `json:"proof,omitempty"`
	Inputs       map[string]any `json:"inputs,omitempty"`
}

type operationWorkflowDraft struct {
	SkillName         string         `json:"skillName"`
	Title             string         `json:"title"`
	Trigger           string         `json:"trigger,omitempty"`
	Description       string         `json:"description,omitempty"`
	OwnedIntegrations []string       `json:"ownedIntegrations,omitempty"`
	Schedule          string         `json:"schedule,omitempty"`
	Checklist         []string       `json:"checklist,omitempty"`
	Definition        map[string]any `json:"definition,omitempty"`
}

type operationMonetizationStep struct {
	Kicker string `json:"kicker,omitempty"`
	Title  string `json:"title"`
	Copy   string `json:"copy,omitempty"`
	Footer string `json:"footer,omitempty"`
}

type operationQueueItem struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Format       string `json:"format"`
	StageIndex   int    `json:"stageIndex"`
	Score        int    `json:"score"`
	UnitCost     int    `json:"unitCost"`
	Eta          string `json:"eta,omitempty"`
	Monetization string `json:"monetization,omitempty"`
	State        string `json:"state,omitempty"`
}

type operationOffer struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	CTA         string `json:"cta,omitempty"`
	Destination string `json:"destination,omitempty"`
}

type operationStarterTemplate struct {
	ID             string                    `json:"id"`
	Kicker         string                    `json:"kicker,omitempty"`
	Name           string                    `json:"name"`
	Badge          string                    `json:"badge,omitempty"`
	Blurb          string                    `json:"blurb,omitempty"`
	Points         []operationStarterPoint   `json:"points,omitempty"`
	Defaults       operationStarterDefaults  `json:"defaults"`
	Agents         []operationStarterAgent   `json:"agents"`
	Channels       []operationStarterChannel `json:"channels"`
	Tasks          []operationStarterTask    `json:"tasks"`
	KickoffTagged  []string                  `json:"kickoffTagged,omitempty"`
	KickoffMessage string                    `json:"kickoffMessage,omitempty"`
	GeneralDesc    string                    `json:"generalDesc,omitempty"`
}

type operationStarterPoint struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type operationStarterDefaults struct {
	Company     string `json:"company,omitempty"`
	Description string `json:"description,omitempty"`
	Goals       string `json:"goals,omitempty"`
	Priority    string `json:"priority,omitempty"`
	Size        string `json:"size,omitempty"`
}

type operationStarterAgent struct {
	Slug           string   `json:"slug"`
	Emoji          string   `json:"emoji,omitempty"`
	Name           string   `json:"name"`
	Role           string   `json:"role,omitempty"`
	Checked        bool     `json:"checked"`
	Type           string   `json:"type,omitempty"`
	PermissionMode string   `json:"permissionMode,omitempty"`
	BuiltIn        bool     `json:"builtIn,omitempty"`
	Expertise      []string `json:"expertise,omitempty"`
	Personality    string   `json:"personality,omitempty"`
}

type operationStarterChannel struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members,omitempty"`
}

type operationStarterTask struct {
	Channel string `json:"channel"`
	Owner   string `json:"owner"`
	Title   string `json:"title"`
	Details string `json:"details,omitempty"`
}

type operationChannelPackDoc struct {
	Metadata struct {
		ID      string `yaml:"id"`
		Purpose string `yaml:"purpose"`
		Status  string `yaml:"status"`
	} `yaml:"metadata"`
	Workspace struct {
		WorkspaceID string `yaml:"workspace_id"`
		PipelineID  string `yaml:"pipeline_id"`
		PublishMode string `yaml:"publish_mode"`
	} `yaml:"workspace"`
	Channel struct {
		BrandName string `yaml:"brand_name"`
		Thesis    string `yaml:"thesis"`
		Tagline   string `yaml:"tagline"`
		ShortBio  string `yaml:"short_bio"`
		Playlists []struct {
			ID    string `yaml:"id"`
			Title string `yaml:"title"`
		} `yaml:"playlists"`
		RenderDefaults struct {
			Format string `yaml:"format"`
		} `yaml:"render_defaults"`
	} `yaml:"channel"`
	Audience struct {
		PrimaryICP   []string `yaml:"primary_icp"`
		TeamSize     string   `yaml:"team_size"`
		JobsToBeDone []string `yaml:"jobs_to_be_done"`
		PainWords    []string `yaml:"pain_words"`
	} `yaml:"audience"`
	LaunchDefaults struct {
		Cadence struct {
			PublishDays []string `yaml:"publish_days"`
			ReviewDay   string   `yaml:"review_day"`
			CutdownDay  string   `yaml:"cutdown_day"`
		} `yaml:"cadence"`
		FirstFourPublishOrder []string `yaml:"first_four_publish_order"`
	} `yaml:"launch_defaults"`
	OfferDefaults struct {
		PrimaryLeadMagnet struct {
			OfferID     string `yaml:"offer_id"`
			Name        string `yaml:"name"`
			Promise     string `yaml:"promise"`
			LandingPage struct {
				Slug string `yaml:"slug"`
			} `yaml:"landing_page"`
		} `yaml:"primary_lead_magnet"`
		SupportingAssets []struct {
			OfferID        string `yaml:"offer_id"`
			CanonicalAsset string `yaml:"canonical_asset"`
		} `yaml:"supporting_assets"`
		RevenueLadder []string `yaml:"revenue_ladder"`
	} `yaml:"offer_defaults"`
	ApprovalBoundaries struct {
		RequireHumanApprovalFor []string `yaml:"require_human_approval_for"`
	} `yaml:"approval_boundaries"`
}

type operationBacklogDoc struct {
	Episodes []operationBacklogEpisode `yaml:"episodes"`
}

type operationBacklogEpisode struct {
	ID                string `yaml:"id"`
	Priority          int    `yaml:"priority"`
	WorkingTitle      string `yaml:"working_title"`
	Pillar            string `yaml:"pillar"`
	PrimaryCTA        string `yaml:"primary_cta"`
	FallbackCTA       string `yaml:"fallback_cta"`
	AffiliateCategory string `yaml:"affiliate_category"`
	SponsorCategory   string `yaml:"sponsor_category"`
	Scores            struct {
		Pain        int `yaml:"pain"`
		BuyerIntent int `yaml:"buyer_intent"`
		Originality int `yaml:"originality"`
		ProductFit  int `yaml:"product_fit"`
	} `yaml:"scores"`
}

type operationMonetizationDoc struct {
	Offers struct {
		LeadMagnets []struct {
			ID      string `yaml:"id"`
			Name    string `yaml:"name"`
			Promise string `yaml:"promise"`
		} `yaml:"lead_magnets"`
		DigitalProducts []struct {
			ID   string `yaml:"id"`
			Name string `yaml:"name"`
		} `yaml:"digital_products"`
		Services []struct {
			ID      string `yaml:"id"`
			Name    string `yaml:"name"`
			Outcome string `yaml:"outcome"`
		} `yaml:"services"`
	} `yaml:"offers"`
}

type operationPackFile struct {
	Path string
	Doc  operationChannelPackDoc
}

type operationBlueprintFile struct {
	Path      string
	Blueprint operations.Blueprint
}

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

func (b *Broker) handleOperationBootstrapPackage(w http.ResponseWriter, r *http.Request) {
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
		if strings.TrimSpace(query) != "" {
			return operationPackFile{}, fmt.Errorf("no matching operation pack")
		}
		for _, pack := range packs {
			if strings.Contains(strings.ToLower(pack.Doc.Metadata.ID), "default") {
				return pack, nil
			}
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
		return operationBootstrapPackage{}, false, nil
	}
	blueprint, err := operations.LoadBlueprint(repoRoot, operationFirstNonEmpty(selected.Doc.Workspace.PipelineID, selected.Doc.Metadata.ID))
	if err != nil {
		return operationBootstrapPackage{}, false, nil
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

func loadOperationRuntimeConnections(ctx context.Context) ([]action.Connection, string) {
	registry := action.NewRegistryFromEnv()
	provider, err := registry.ProviderFor(action.CapabilityConnections)
	if err != nil {
		return nil, ""
	}
	result, err := provider.ListConnections(ctx, action.ListConnectionsOptions{Limit: 200})
	if err != nil {
		return nil, provider.Name()
	}
	return result.Connections, provider.Name()
}

func buildOperationBootstrapPackage(selected operationPackFile, blueprint operations.Blueprint, backlog operationBacklogDoc, monetization operationMonetizationDoc, runtimeConnections []action.Connection, providerName string, profile operationCompanyProfile) operationBootstrapPackage {
	pack := selected.Doc
	drafts := buildOperationWorkflowDrafts(blueprint)
	smokeTests := buildOperationSmokeTests(blueprint)
	connections := buildOperationConnectionCards(blueprint, runtimeConnections, providerName)
	valueCapturePlan := buildOperationValueCapturePlan(blueprint, pack)
	workstreamSeed := buildOperationWorkstreamSeed(blueprint, pack, backlog)
	sourcePath := filepath.ToSlash(selected.Path)
	if strings.TrimSpace(sourcePath) == "" {
		sourcePath = "synthesized"
	}
	blueprintID := operationFirstNonEmpty(blueprint.ID, profile.BlueprintID, pack.Metadata.ID, operationSlug(operationFirstNonEmpty(profile.Name, blueprint.Name, "synthesized-operation")))
	blueprintLabel := operationFirstNonEmpty(profile.Name, blueprint.Name, pack.Channel.BrandName, blueprint.Description, "Synthesized operation")
	return operationBootstrapPackage{
		BlueprintID:        blueprintID,
		BlueprintLabel:     blueprintLabel,
		PackID:             blueprintID,
		PackLabel:          blueprintLabel,
		SourcePath:         sourcePath,
		ConnectionProvider: providerName,
		Blueprint:          blueprint,
		BootstrapConfig:    buildOperationBootstrapConfig(blueprint, pack, profile),
		Starter:            buildOperationStarterTemplate(blueprint, pack, backlog, profile),
		Automation:         buildOperationAutomation(blueprint, providerName),
		Integrations:       buildOperationIntegrationStubs(connections),
		Connections:        connections,
		SmokeTests:         smokeTests,
		WorkflowDrafts:     drafts,
		ValueCapturePlan:   valueCapturePlan,
		MonetizationLadder: valueCapturePlan,
		WorkstreamSeed:     workstreamSeed,
		QueueSeed:          workstreamSeed,
		Offers:             buildOperationOffers(blueprint, pack, monetization, profile),
	}
}

func buildOperationSynthesizedBootstrapPackage(profile operationCompanyProfile, runtimeConnections []action.Connection, providerName string) operationBootstrapPackage {
	blueprint := operations.SynthesizeBlueprint(operations.SynthesisInput{
		Directive: operationFirstNonEmpty(profile.Goals, profile.Description, profile.Name, "stand up a new operation"),
		Profile: operations.CompanyProfile{
			Name:        strings.TrimSpace(profile.Name),
			Industry:    strings.TrimSpace(profile.Priority),
			Description: strings.TrimSpace(profile.Description),
			Audience:    strings.TrimSpace(profile.Size),
			Offer:       strings.TrimSpace(profile.Goals),
			Notes:       []string{strings.TrimSpace(profile.BlueprintID)},
		},
		Integrations: operationRuntimeIntegrationsFromConnections(runtimeConnections),
		Capabilities: operationRuntimeCapabilitiesFromConnections(runtimeConnections, providerName),
	})
	return buildOperationBootstrapPackage(operationPackFile{}, blueprint, operationBacklogDoc{}, operationMonetizationDoc{}, runtimeConnections, providerName, profile)
}

func buildOperationStarterTemplate(blueprint operations.Blueprint, pack operationChannelPackDoc, backlog operationBacklogDoc, profile operationCompanyProfile) operationStarterTemplate {
	brandName := operationFirstResolvedNonEmpty(profile.Name, blueprint.BootstrapConfig.ChannelName, blueprint.Name, pack.Channel.BrandName, "Autonomous operation")
	niche := operationFirstResolvedNonEmpty(blueprint.BootstrapConfig.Niche, blueprint.Description, profile.Description, pack.Channel.Thesis, pack.Channel.Tagline, "Automated operation")
	goals := operationFirstNonEmpty(
		strings.TrimSpace(profile.Goals),
		strings.TrimSpace(blueprint.Objective),
		"Stand up the first repeatable workflow, validate operator demand, and turn it into a durable operating asset.",
	)
	priority := operationFirstNonEmpty(
		strings.TrimSpace(profile.Priority),
		firstBacklogTitle(backlog),
		firstOperationStarterTaskTitle(blueprint.Starter.Tasks),
		"Stand up the first workflow lane and prove the office can run it with the right approvals.",
	)
	size := operationFirstNonEmpty(strings.TrimSpace(profile.Size), strings.TrimSpace(pack.Audience.TeamSize), "2-5")
	id := operationSlug(operationFirstNonEmpty(profile.BlueprintID, blueprint.ID, pack.Metadata.ID, brandName))
	if id == "" {
		id = "autonomous-operation"
	}
	commandSlug := operationSlug(brandName + " command")
	if commandSlug == "" {
		commandSlug = "command"
	}
	replacements := operationBootstrapTemplateReplacements(brandName, commandSlug, niche)
	starter := blueprint.Starter
	leadSlug := operationFirstNonEmpty(starter.LeadSlug, "ceo")
	return operationStarterTemplate{
		ID:     id,
		Kicker: "Starter plan",
		Name:   brandName,
		Badge:  "Operation template",
		Blurb:  operationFirstNonEmpty(blueprint.Description, profile.Description, pack.Channel.ShortBio, pack.Channel.Tagline, niche),
		Points: []operationStarterPoint{
			{Label: "Audience", Value: operationFirstNonEmpty(blueprint.BootstrapConfig.Audience, strings.TrimSpace(profile.Size), strings.Join(pack.Audience.PrimaryICP, ", "), "Operators and stakeholders")},
			{Label: "Cadence", Value: operationFirstNonEmpty(strings.TrimSpace(blueprint.BootstrapConfig.PublishingCadence), operationPublishingCadence(pack), "Weekly operating review")},
			{Label: "Value Capture", Value: operationFirstNonEmpty(strings.Join(blueprint.BootstrapConfig.MonetizationHooks, ", "), strings.Join(pack.OfferDefaults.RevenueLadder, ", "), "Approval-gated value capture")},
		},
		Defaults: operationStarterDefaults{
			Company:     brandName,
			Description: operationFirstNonEmpty(blueprint.Description, profile.Description, pack.Channel.ShortBio, pack.Channel.Tagline, niche),
			Goals:       goals,
			Priority:    priority,
			Size:        size,
		},
		Agents:         operationStarterAgentsFromBlueprint(starter.Agents, replacements),
		Channels:       operationStarterChannelsFromBlueprint(starter.Channels, replacements),
		Tasks:          operationStarterTasksFromBlueprint(starter.Tasks, replacements),
		KickoffTagged:  []string{leadSlug},
		KickoffMessage: operationRenderTemplateString(starter.KickoffPrompt, replacements),
		GeneralDesc:    operationRenderTemplateString(starter.GeneralChannelDescription, replacements),
	}
}

func firstOperationStarterTaskTitle(tasks []operations.StarterTask) string {
	for _, task := range tasks {
		if strings.TrimSpace(task.Title) != "" {
			return strings.TrimSpace(task.Title)
		}
	}
	return ""
}

func operationBootstrapTemplateReplacements(brandName, commandSlug, niche string) map[string]string {
	brandName = strings.TrimSpace(brandName)
	if brandName == "" {
		brandName = "Autonomous operation"
	}
	commandSlug = operationSlug(commandSlug)
	if commandSlug == "" {
		commandSlug = "command"
	}
	return map[string]string{
		"brand_name":   brandName,
		"brand_slug":   operationSlug(brandName),
		"command_slug": commandSlug,
		"niche":        niche,
	}
}

func operationFirstResolvedNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, "{{") && strings.Contains(value, "}}") {
			continue
		}
		return value
	}
	return ""
}

func operationStarterAgentsFromBlueprint(agents []operations.StarterAgent, replacements map[string]string) []operationStarterAgent {
	out := make([]operationStarterAgent, 0, len(agents))
	for _, agent := range agents {
		expertise := make([]string, 0, len(agent.Expertise))
		for _, item := range agent.Expertise {
			expertise = append(expertise, operationRenderTemplateString(item, replacements))
		}
		out = append(out, operationStarterAgent{
			Slug:           operationRenderTemplateString(agent.Slug, replacements),
			Emoji:          operationRenderTemplateString(agent.Emoji, replacements),
			Name:           operationRenderTemplateString(agent.Name, replacements),
			Role:           operationRenderTemplateString(agent.Role, replacements),
			Checked:        agent.Checked,
			Type:           operationRenderTemplateString(agent.Type, replacements),
			PermissionMode: agent.PermissionMode,
			BuiltIn:        agent.BuiltIn,
			Expertise:      expertise,
			Personality:    operationRenderTemplateString(agent.Personality, replacements),
		})
	}
	return out
}

func operationStarterChannelsFromBlueprint(channels []operations.StarterChannel, replacements map[string]string) []operationStarterChannel {
	out := make([]operationStarterChannel, 0, len(channels))
	for _, channel := range channels {
		members := make([]string, 0, len(channel.Members))
		for _, member := range channel.Members {
			members = append(members, operationRenderTemplateString(member, replacements))
		}
		out = append(out, operationStarterChannel{
			Slug:        operationRenderTemplateString(channel.Slug, replacements),
			Name:        operationRenderTemplateString(channel.Name, replacements),
			Description: operationRenderTemplateString(channel.Description, replacements),
			Members:     members,
		})
	}
	return out
}

func operationStarterTasksFromBlueprint(tasks []operations.StarterTask, replacements map[string]string) []operationStarterTask {
	out := make([]operationStarterTask, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, operationStarterTask{
			Channel: operationRenderTemplateString(task.Channel, replacements),
			Owner:   operationRenderTemplateString(task.Owner, replacements),
			Title:   operationRenderTemplateString(task.Title, replacements),
			Details: operationRenderTemplateString(task.Details, replacements),
		})
	}
	return out
}

func firstBacklogTitle(backlog operationBacklogDoc) string {
	for _, episode := range backlog.Episodes {
		title := strings.TrimSpace(episode.WorkingTitle)
		if title != "" {
			return title
		}
	}
	return ""
}

func buildOperationBootstrapConfig(blueprint operations.Blueprint, pack operationChannelPackDoc, profile operationCompanyProfile) operationBootstrapConfig {
	brandName := operationFirstResolvedNonEmpty(profile.Name, blueprint.BootstrapConfig.ChannelName, blueprint.Name, pack.Channel.BrandName, "Autonomous operation")
	replacements := operationBootstrapTemplateReplacements(
		brandName,
		operationSlug(brandName+" command"),
		operationFirstResolvedNonEmpty(blueprint.BootstrapConfig.Niche, blueprint.Description, profile.Description, pack.Channel.Thesis, pack.Channel.Tagline, "Automated operation"),
	)
	if cfg := buildOperationBootstrapConfigFromBlueprint(blueprint.BootstrapConfig, replacements); cfg != nil {
		return *cfg
	}
	brandName = operationFirstNonEmpty(profile.Name, blueprint.Name, pack.Channel.BrandName, "Autonomous operation")
	channelSlug := operationSlug(brandName)
	if channelSlug == "" {
		channelSlug = "autonomous-operation"
	}
	leadMagnetName := operationFirstNonEmpty(profile.Name, blueprint.Name, "Operation starter")
	leadMagnetPath := operationSlug(leadMagnetName)
	if leadMagnetPath == "" {
		leadMagnetPath = "starter"
	}
	return operationBootstrapConfig{
		ChannelName:       brandName,
		ChannelSlug:       channelSlug,
		Niche:             operationFirstNonEmpty(blueprint.Description, profile.Description, pack.Channel.Thesis, pack.Channel.Tagline, "Automated operation"),
		Audience:          operationFirstNonEmpty(strings.TrimSpace(profile.Size), strings.Join(pack.Audience.PrimaryICP, ", "), "Operators and stakeholders"),
		Positioning:       operationFirstNonEmpty(profile.Description, blueprint.Objective, blueprint.Description, pack.Channel.ShortBio, pack.Channel.Tagline, "Blueprint-driven operating system"),
		MonetizationHooks: []string{"Approval-gated value capture"},
		PublishingCadence: operationFirstNonEmpty(operationPublishingCadence(pack), "Weekly operating review"),
		LeadMagnet: operationLeadMagnet{
			Name: leadMagnetName,
			CTA:  "Open the starter package",
			Path: leadMagnetPath,
		},
		KPITracking: []operationKPI{
			{
				Name:   "Workflow completions",
				Target: "3+ completed loops per week",
				Why:    "Confirms the operation is running repeatably, not just generating plans.",
			},
			{
				Name:   "Approval turnaround",
				Target: "<24h on blocked steps",
				Why:    "Keeps human checkpoints from stalling the system.",
			},
			{
				Name:   "Outcome conversion",
				Target: "1 measurable business outcome per cycle",
				Why:    "Proves the workflows are tied to value, not just activity.",
			},
		},
	}
}

func buildOperationBootstrapConfigFromBlueprint(cfg operations.BootstrapConfig, replacements map[string]string) *operationBootstrapConfig {
	if operationBootstrapConfigIsEmpty(cfg) {
		return nil
	}
	contentPillars := make([]string, 0, len(cfg.ContentPillars))
	for _, item := range cfg.ContentPillars {
		contentPillars = append(contentPillars, operationRenderTemplateString(item, replacements))
	}
	contentSeries := make([]string, 0, len(cfg.ContentSeries))
	for _, item := range cfg.ContentSeries {
		contentSeries = append(contentSeries, operationRenderTemplateString(item, replacements))
	}
	monetizationHooks := make([]string, 0, len(cfg.MonetizationHooks))
	for _, item := range cfg.MonetizationHooks {
		monetizationHooks = append(monetizationHooks, operationRenderTemplateString(item, replacements))
	}
	assets := make([]operationMonetizationAsset, 0, len(cfg.MonetizationAsset))
	for _, asset := range cfg.MonetizationAsset {
		assets = append(assets, operationMonetizationAsset{
			Stage: operationRenderTemplateString(asset.Stage, replacements),
			Name:  operationRenderTemplateString(asset.Name, replacements),
			Slot:  operationRenderTemplateString(asset.Slot, replacements),
			CTA:   operationRenderTemplateString(asset.CTA, replacements),
		})
	}
	kpis := make([]operationKPI, 0, len(cfg.KPITracking))
	for _, kpi := range cfg.KPITracking {
		kpis = append(kpis, operationKPI{
			Name:   operationRenderTemplateString(kpi.Name, replacements),
			Target: operationRenderTemplateString(kpi.Target, replacements),
			Why:    operationRenderTemplateString(kpi.Why, replacements),
		})
	}
	return &operationBootstrapConfig{
		ChannelName:       operationRenderTemplateString(cfg.ChannelName, replacements),
		ChannelSlug:       operationRenderTemplateString(cfg.ChannelSlug, replacements),
		Niche:             operationRenderTemplateString(cfg.Niche, replacements),
		Audience:          operationRenderTemplateString(cfg.Audience, replacements),
		Positioning:       operationRenderTemplateString(cfg.Positioning, replacements),
		ContentPillars:    contentPillars,
		ContentSeries:     contentSeries,
		MonetizationHooks: monetizationHooks,
		PublishingCadence: operationRenderTemplateString(cfg.PublishingCadence, replacements),
		LeadMagnet: operationLeadMagnet{
			Name: operationRenderTemplateString(cfg.LeadMagnet.Name, replacements),
			CTA:  operationRenderTemplateString(cfg.LeadMagnet.CTA, replacements),
			Path: operationRenderTemplateString(cfg.LeadMagnet.Path, replacements),
		},
		MonetizationAsset: assets,
		KPITracking:       kpis,
	}
}

func operationBootstrapConfigIsEmpty(cfg operations.BootstrapConfig) bool {
	return strings.TrimSpace(cfg.ChannelName) == "" &&
		strings.TrimSpace(cfg.ChannelSlug) == "" &&
		strings.TrimSpace(cfg.Niche) == "" &&
		len(cfg.ContentPillars) == 0 &&
		len(cfg.ContentSeries) == 0 &&
		len(cfg.MonetizationHooks) == 0 &&
		strings.TrimSpace(cfg.PublishingCadence) == "" &&
		strings.TrimSpace(cfg.LeadMagnet.Name) == "" &&
		len(cfg.MonetizationAsset) == 0 &&
		len(cfg.KPITracking) == 0
}

func buildOperationBootstrapConfigFromPack(pack operationChannelPackDoc) operationBootstrapConfig {
	contentSeries := make([]string, 0, len(pack.Channel.Playlists))
	for i, playlist := range pack.Channel.Playlists {
		if i >= 4 {
			break
		}
		contentSeries = append(contentSeries, strings.TrimSpace(playlist.Title))
	}
	return operationBootstrapConfig{
		ChannelName:       pack.Channel.BrandName,
		ChannelSlug:       operationSlug(pack.Channel.BrandName),
		Niche:             pack.Channel.Thesis,
		Audience:          strings.Join(pack.Audience.PrimaryICP, ", "),
		Positioning:       operationFirstNonEmpty(pack.Channel.ShortBio, pack.Channel.Tagline, pack.Channel.Thesis),
		ContentPillars:    append([]string(nil), pack.Audience.JobsToBeDone...),
		ContentSeries:     contentSeries,
		MonetizationHooks: append([]string(nil), pack.OfferDefaults.RevenueLadder...),
		PublishingCadence: operationPublishingCadence(pack),
		LeadMagnet: operationLeadMagnet{
			Name: pack.OfferDefaults.PrimaryLeadMagnet.Name,
			CTA:  "Get the " + strings.TrimSpace(pack.OfferDefaults.PrimaryLeadMagnet.Name),
			Path: pack.OfferDefaults.PrimaryLeadMagnet.LandingPage.Slug,
		},
		MonetizationAsset: buildOperationMonetizationAssets(pack),
		KPITracking:       buildOperationKPIs(pack),
	}
}

func buildOperationAutomation(blueprint operations.Blueprint, providerName string) []operationAutomationModule {
	connectionMode := "Stub first"
	connectionStatus := "stub"
	connectionFooter := "Waiting on connected external systems and human approvals."
	if providerName != "" {
		connectionMode = "Live-capable"
		connectionStatus = "build_now"
		connectionFooter = fmt.Sprintf("Connected systems are inspected via %s; keep mutating actions behind approval.", titleCaser.String(providerName))
	}
	replacements := map[string]string{
		"connection_mode":     connectionMode,
		"connection_status":   connectionStatus,
		"connection_footer":   connectionFooter,
		"approval_boundaries": operationAutomationApprovalSummary(blueprint.ApprovalRules),
	}
	out := make([]operationAutomationModule, 0, len(blueprint.Automation))
	for _, module := range blueprint.Automation {
		out = append(out, operationAutomationModule{
			ID:     operationRenderTemplateString(module.ID, replacements),
			Kicker: operationRenderTemplateString(module.Kicker, replacements),
			Title:  operationRenderTemplateString(module.Title, replacements),
			Copy:   operationRenderTemplateString(module.Copy, replacements),
			Status: operationRenderTemplateString(module.Status, replacements),
			Footer: operationRenderTemplateString(module.Footer, replacements),
		})
	}
	return out
}

func buildOperationIntegrationStubs(cards []operationConnectionCard) []operationIntegrationStub {
	out := make([]operationIntegrationStub, 0, len(cards))
	for _, card := range cards {
		status := "stub"
		switch card.State {
		case "connected", "smoke_tested":
			status = "connected"
		case "ready_for_auth":
			status = "ready_for_auth"
		}
		out = append(out, operationIntegrationStub{
			Name:   card.Name,
			Status: status,
			Detail: operationFirstNonEmpty(card.Purpose, card.Blocker),
		})
	}
	return out
}

type operationIntegrationBlueprint struct {
	Name        string
	Integration string
	Owner       string
	Priority    string
	Purpose     string
	SmokeTest   string
	Blocker     string
}

func buildOperationConnectionCards(blueprint operations.Blueprint, runtimeConnections []action.Connection, providerName string) []operationConnectionCard {
	blueprints := make([]operationIntegrationBlueprint, 0, len(blueprint.Connections))
	for _, item := range blueprint.Connections {
		blueprints = append(blueprints, operationIntegrationBlueprint{
			Name:        item.Name,
			Integration: item.Integration,
			Owner:       item.Owner,
			Priority:    item.Priority,
			Purpose:     item.Purpose,
			SmokeTest:   item.SmokeTest,
			Blocker:     item.Blocker,
		})
	}
	sort.SliceStable(blueprints, func(i, j int) bool {
		return blueprints[i].Priority < blueprints[j].Priority
	})

	connectionMap := make(map[string]action.Connection, len(runtimeConnections))
	for _, connection := range runtimeConnections {
		key := normalizeOperationIntegrationKey(connection.Platform)
		if key == "" {
			continue
		}
		if existing, ok := connectionMap[key]; ok && !operationConnectionIsBetter(connection, existing) {
			continue
		}
		connectionMap[key] = connection
	}

	out := make([]operationConnectionCard, 0, len(blueprints))
	for _, blueprint := range blueprints {
		card := operationConnectionCard{
			Name:        blueprint.Name,
			Integration: blueprint.Integration,
			Owner:       blueprint.Owner,
			Priority:    blueprint.Priority,
			Mode:        "approval_required",
			State:       "stubbed",
			Purpose:     blueprint.Purpose,
			SmokeTest:   blueprint.SmokeTest,
			Blocker:     blueprint.Blocker,
		}
		if providerName != "" {
			card.State = "ready_for_auth"
		}
		if live, ok := connectionMap[normalizeOperationIntegrationKey(blueprint.Integration)]; ok {
			card.State = "connected"
			card.Mode = "live_capable"
			card.Blocker = fmt.Sprintf("Connection %q is available via %s. Live mutations still require human approval.", live.Key, providerName)
		}
		out = append(out, card)
	}
	return out
}

func operationConnectionIsBetter(next, current action.Connection) bool {
	return operationConnectionStateRank(next.State) > operationConnectionStateRank(current.State)
}

func operationConnectionStateRank(state string) int {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "operational", "active", "connected":
		return 3
	case "ready", "authorized":
		return 2
	default:
		return 1
	}
}

func buildOperationWorkflowDrafts(blueprint operations.Blueprint) []operationWorkflowDraft {
	out := make([]operationWorkflowDraft, 0, len(blueprint.Workflows))
	for _, workflow := range blueprint.Workflows {
		out = append(out, operationWorkflowDraft{
			SkillName:         strings.TrimSpace(workflow.ID),
			Title:             strings.TrimSpace(workflow.Name),
			Trigger:           strings.TrimSpace(workflow.Trigger),
			Description:       strings.TrimSpace(workflow.Description),
			OwnedIntegrations: append([]string(nil), workflow.Integrations...),
			Schedule:          strings.TrimSpace(workflow.Schedule),
			Checklist:         append([]string(nil), workflow.Checklist...),
			Definition:        cloneOperationMap(workflow.Definition),
		})
	}
	return out
}

func buildOperationSmokeTests(blueprint operations.Blueprint) []operationSmokeTest {
	out := make([]operationSmokeTest, 0, len(blueprint.Workflows))
	for _, workflow := range blueprint.Workflows {
		if strings.TrimSpace(workflow.SmokeTest.Name) == "" {
			continue
		}
		out = append(out, operationSmokeTest{
			Name:         strings.TrimSpace(workflow.SmokeTest.Name),
			WorkflowKey:  operationWorkflowKeyFromTemplate(workflow),
			Mode:         operationFirstNonEmpty(strings.TrimSpace(workflow.SmokeTest.Mode), strings.TrimSpace(workflow.Mode)),
			Integrations: append([]string(nil), workflow.Integrations...),
			Proof:        strings.TrimSpace(workflow.SmokeTest.Proof),
			Inputs:       cloneOperationMap(workflow.SmokeTest.Inputs),
		})
	}
	return out
}

func operationWorkflowKey(draft operationWorkflowDraft) string {
	if value, ok := draft.Definition["key"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(draft.SkillName)
}

func operationWorkflowKeyFromTemplate(workflow operations.WorkflowTemplate) string {
	if value, ok := workflow.Definition["key"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(workflow.ID)
}

func buildOperationValueCapturePlan(blueprint operations.Blueprint, pack operationChannelPackDoc) []operationMonetizationStep {
	if len(blueprint.MonetizationLadder) > 0 {
		replacements := operationBootstrapTemplateReplacements(
			operationFirstNonEmpty(blueprint.Name, pack.Channel.BrandName, "operation"),
			operationSlug(operationFirstNonEmpty(blueprint.Name, pack.Channel.BrandName, "operation")+" command"),
			operationFirstNonEmpty(blueprint.Description, pack.Channel.Thesis, pack.Channel.Tagline, "Automated operation"),
		)
		out := make([]operationMonetizationStep, 0, len(blueprint.MonetizationLadder))
		for _, step := range blueprint.MonetizationLadder {
			out = append(out, operationMonetizationStep{
				Kicker: operationRenderTemplateString(step.Kicker, replacements),
				Title:  operationRenderTemplateString(step.Title, replacements),
				Copy:   operationRenderTemplateString(step.Copy, replacements),
				Footer: operationRenderTemplateString(step.Footer, replacements),
			})
		}
		return out
	}
	ladder := pack.OfferDefaults.RevenueLadder
	if len(ladder) == 0 {
		return nil
	}
	out := make([]operationMonetizationStep, 0, len(ladder))
	for i, item := range ladder {
		out = append(out, operationMonetizationStep{
			Kicker: fmt.Sprintf("Step %d", i+1),
			Title:  strings.ReplaceAll(item, "_", " "),
			Copy:   fmt.Sprintf("Turn %s into a reusable commercial lane in the operating system.", strings.ReplaceAll(item, "_", " ")),
			Footer: "Loaded from the legacy pack revenue ladder.",
		})
	}
	return out
}

func buildOperationWorkstreamSeed(blueprint operations.Blueprint, pack operationChannelPackDoc, backlog operationBacklogDoc) []operationQueueItem {
	if len(blueprint.QueueSeed) > 0 {
		replacements := operationBootstrapTemplateReplacements(
			operationFirstNonEmpty(blueprint.Name, pack.Channel.BrandName, "operation"),
			operationSlug(operationFirstNonEmpty(blueprint.Name, pack.Channel.BrandName, "operation")+" command"),
			operationFirstNonEmpty(blueprint.Description, pack.Channel.Thesis, pack.Channel.Tagline, "Automated operation"),
		)
		out := make([]operationQueueItem, 0, len(blueprint.QueueSeed))
		for _, item := range blueprint.QueueSeed {
			out = append(out, operationQueueItem{
				ID:           operationRenderTemplateString(item.ID, replacements),
				Title:        operationRenderTemplateString(item.Title, replacements),
				Format:       operationRenderTemplateString(item.Format, replacements),
				StageIndex:   item.StageIndex,
				Score:        item.Score,
				UnitCost:     item.UnitCost,
				Eta:          operationRenderTemplateString(item.Eta, replacements),
				Monetization: operationRenderTemplateString(item.Monetization, replacements),
				State:        operationRenderTemplateString(item.State, replacements),
			})
		}
		return out
	}
	format := "Work item"
	if strings.Contains(strings.ToLower(pack.Channel.RenderDefaults.Format), "short") {
		format = "Short"
	} else if strings.TrimSpace(pack.Channel.RenderDefaults.Format) != "" {
		format = strings.TrimSpace(pack.Channel.RenderDefaults.Format)
	}
	items := make([]operationQueueItem, 0, 5)
	for i, episode := range backlog.Episodes {
		if i >= 5 {
			break
		}
		items = append(items, operationQueueItem{
			ID:           operationFirstNonEmpty(strings.TrimSpace(episode.ID), fmt.Sprintf("run-%d", i+1)),
			Title:        operationFirstNonEmpty(strings.TrimSpace(episode.WorkingTitle), fmt.Sprintf("Launch slot %d", i+1)),
			Format:       format,
			StageIndex:   i % 5,
			Score:        operationEpisodeScore(episode),
			UnitCost:     operationQueueUnitCost(format),
			Eta:          fmt.Sprintf("Launch slot %d", i+1),
			Monetization: operationQueueMonetization(episode),
			State:        "active",
		})
	}
	return items
}

func operationEpisodeScore(episode operationBacklogEpisode) int {
	total := episode.Scores.Pain + episode.Scores.BuyerIntent + episode.Scores.Originality + episode.Scores.ProductFit
	if total <= 0 {
		return 75
	}
	return total * 5
}

func operationQueueUnitCost(format string) int {
	if strings.EqualFold(format, "Short") {
		return 4
	}
	return 18
}

func operationQueueMonetization(episode operationBacklogEpisode) string {
	if strings.TrimSpace(episode.PrimaryCTA) == "" {
		return "owned audience"
	}
	parts := []string{strings.ReplaceAll(episode.PrimaryCTA, "_", " ")}
	if strings.TrimSpace(episode.AffiliateCategory) != "" {
		parts = append(parts, strings.ReplaceAll(episode.AffiliateCategory, "_", " "))
	}
	return strings.Join(parts, " + ")
}

func buildOperationOffers(blueprint operations.Blueprint, pack operationChannelPackDoc, monetization operationMonetizationDoc, profile operationCompanyProfile) []operationOffer {
	leadMagnetName := operationFirstNonEmpty(blueprint.BootstrapConfig.LeadMagnet.Name, profile.Name, blueprint.Name, pack.OfferDefaults.PrimaryLeadMagnet.Name, "Operation blueprint")
	leadMagnetID := operationFirstNonEmpty(operationSlug(leadMagnetName), pack.OfferDefaults.PrimaryLeadMagnet.OfferID, operationSlug(operationFirstNonEmpty(profile.Name, blueprint.Name, "operation")))
	destination := operationFirstNonEmpty(blueprint.BootstrapConfig.LeadMagnet.Path, pack.OfferDefaults.PrimaryLeadMagnet.LandingPage.Slug, "bootstrap")
	out := []operationOffer{
		{
			ID:          leadMagnetID,
			Name:        leadMagnetName,
			Type:        "lead_magnet",
			CTA:         operationFirstNonEmpty(blueprint.BootstrapConfig.LeadMagnet.CTA, pack.OfferDefaults.PrimaryLeadMagnet.Promise, "Open the starter package."),
			Destination: destination,
		},
	}
	for _, asset := range blueprint.BootstrapConfig.MonetizationAsset {
		name := strings.TrimSpace(asset.Name)
		if name == "" {
			continue
		}
		out = append(out, operationOffer{
			ID:          operationFirstNonEmpty(operationSlug(name), operationSlug(asset.Stage), operationSlug(asset.Slot)),
			Name:        name,
			Type:        "asset",
			CTA:         operationFirstNonEmpty(asset.CTA, "Open "+name),
			Destination: operationFirstNonEmpty(asset.Slot, asset.Stage),
		})
	}
	for _, product := range monetization.Offers.DigitalProducts {
		out = append(out, operationOffer{
			ID:   product.ID,
			Name: product.Name,
			Type: "digital_product",
			CTA:  "See " + product.Name,
		})
	}
	for _, service := range monetization.Offers.Services {
		out = append(out, operationOffer{
			ID:   service.ID,
			Name: service.Name,
			Type: "service",
			CTA:  "Request " + service.Name,
		})
	}
	return out
}

func operationRuntimeIntegrationsFromConnections(runtimeConnections []action.Connection) []operations.RuntimeIntegration {
	integrations := make([]operations.RuntimeIntegration, 0, len(runtimeConnections))
	seen := make(map[string]struct{}, len(runtimeConnections))
	for _, conn := range runtimeConnections {
		integration := strings.TrimSpace(conn.Platform)
		if integration == "" {
			continue
		}
		key := strings.ToLower(integration)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		integrations = append(integrations, operations.RuntimeIntegration{
			Name:        operationFirstNonEmpty(strings.TrimSpace(conn.Name), titleCaser.String(integration)),
			Provider:    integration,
			Status:      strings.TrimSpace(conn.State),
			Purpose:     fmt.Sprintf("Connected %s account available for workflow planning.", integration),
			Description: fmt.Sprintf("Connected account %q with key %q.", strings.TrimSpace(conn.Name), strings.TrimSpace(conn.Key)),
			Connected:   isOperationConnectionConnected(conn),
		})
	}
	sort.SliceStable(integrations, func(i, j int) bool { return integrations[i].Provider < integrations[j].Provider })
	return integrations
}

func operationRuntimeCapabilitiesFromConnections(runtimeConnections []action.Connection, providerName string) []operations.RuntimeCapability {
	capabilities := []operations.RuntimeCapability{
		{Key: "bootstrap", Name: "Bootstrap synthesis", Category: "planner", Lifecycle: "active", Detail: "Turn a blank directive into an operation blueprint."},
		{Key: "approval", Name: "Human approval gate", Category: "policy", Lifecycle: "active", Detail: "Block high-risk actions until a human approves them."},
	}
	if providerName != "" {
		capabilities = append(capabilities, operations.RuntimeCapability{
			Key:       operationSlug(providerName + "-connections"),
			Name:      titleCaser.String(strings.TrimSpace(providerName)) + " connections",
			Category:  "integration",
			Lifecycle: "active",
			Detail:    "Discover connected accounts and map them into workflows.",
		})
	}
	for _, conn := range runtimeConnections {
		integration := strings.TrimSpace(conn.Platform)
		if integration == "" {
			continue
		}
		capabilities = append(capabilities, operations.RuntimeCapability{
			Key:       operationSlug(integration),
			Name:      titleCaser.String(integration),
			Category:  "integration",
			Lifecycle: strings.TrimSpace(conn.State),
			Detail:    fmt.Sprintf("Use the connected %s account when the workflow needs it.", integration),
		})
	}
	return capabilities
}

func isOperationConnectionConnected(conn action.Connection) bool {
	switch strings.ToLower(strings.TrimSpace(conn.State)) {
	case "connected", "active", "operational", "ready", "authorized":
		return true
	default:
		return false
	}
}

func buildOperationMonetizationAssets(pack operationChannelPackDoc) []operationMonetizationAsset {
	out := []operationMonetizationAsset{
		{
			Stage: "Day 0",
			Name:  pack.OfferDefaults.PrimaryLeadMagnet.Name,
			Slot:  "pinned_comment",
			CTA:   "Get the " + strings.TrimSpace(pack.OfferDefaults.PrimaryLeadMagnet.Name),
		},
	}
	for _, asset := range pack.OfferDefaults.SupportingAssets {
		out = append(out, operationMonetizationAsset{
			Stage: "Support",
			Name:  asset.CanonicalAsset,
			Slot:  "description_links",
			CTA:   "Open " + asset.CanonicalAsset,
		})
	}
	return out
}

func buildOperationKPIs(pack operationChannelPackDoc) []operationKPI {
	brand := operationFirstNonEmpty(pack.Channel.BrandName, "channel")
	return []operationKPI{
		{
			Name:   "Primary CTA conversions",
			Target: "25+ monthly",
			Why:    fmt.Sprintf("Proves %s is capturing owned demand instead of only views.", brand),
		},
		{
			Name:   "Workflow click-through",
			Target: "3%+",
			Why:    "Shows the monetization lane matches the workflow problem.",
		},
		{
			Name:   "First paid offer conversion",
			Target: "Within 30 days",
			Why:    "Confirms the content engine reaches buyers, not only viewers.",
		},
		{
			Name:   "Repeatable winners",
			Target: "2 episodes above baseline CTR + retention",
			Why:    "Marks the point where sponsor and scale experiments become sensible.",
		},
	}
}

func operationPublishingCadence(pack operationChannelPackDoc) string {
	days := pack.LaunchDefaults.Cadence.PublishDays
	if len(days) == 0 {
		return "Publish cadence pending"
	}
	return fmt.Sprintf("%d release days/week (%s)", len(days), strings.Join(days, ", "))
}

func normalizeOperationIntegrationKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return replacer.Replace(value)
}

func operationSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "&", " and ")
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func operationFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func operationRenderTemplateString(value string, replacements map[string]string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(replacements) == 0 {
		return value
	}
	args := make([]string, 0, len(replacements)*2)
	for key, replacement := range replacements {
		args = append(args, "{{"+key+"}}", replacement)
	}
	return strings.NewReplacer(args...).Replace(value)
}

func cloneOperationMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

func operationAutomationApprovalSummary(rules []operations.ApprovalRule) string {
	if len(rules) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rules))
	for _, rule := range rules {
		if desc := strings.TrimSpace(rule.Description); desc != "" {
			parts = append(parts, desc)
			continue
		}
		if trigger := strings.TrimSpace(rule.Trigger); trigger != "" {
			parts = append(parts, trigger)
		}
	}
	return strings.Join(parts, "; ")
}
