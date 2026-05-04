package team

import "github.com/nex-crm/wuphf/internal/operations"

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
