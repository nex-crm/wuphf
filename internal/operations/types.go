package operations

type Blueprint struct {
	ID                 string                  `json:"id" yaml:"id"`
	Name               string                  `json:"name" yaml:"name"`
	Kind               string                  `json:"kind,omitempty" yaml:"kind,omitempty"`
	Description        string                  `json:"description,omitempty" yaml:"description,omitempty"`
	Objective          string                  `json:"objective,omitempty" yaml:"objective,omitempty"`
	EmployeeBlueprints []string                `json:"employee_blueprints,omitempty" yaml:"employee_blueprints,omitempty"`
	Starter            StarterPlan             `json:"starter" yaml:"starter"`
	BootstrapConfig    BootstrapConfig         `json:"bootstrap_config,omitempty" yaml:"bootstrap_config,omitempty"`
	MonetizationLadder []MonetizationStep      `json:"monetization_ladder,omitempty" yaml:"monetization_ladder,omitempty"`
	QueueSeed          []QueueItem             `json:"queue_seed,omitempty" yaml:"queue_seed,omitempty"`
	Automation         []AutomationModule      `json:"automation,omitempty" yaml:"automation,omitempty"`
	Stages             []StageDefinition       `json:"stages,omitempty" yaml:"stages,omitempty"`
	Artifacts          []ArtifactType          `json:"artifacts,omitempty" yaml:"artifacts,omitempty"`
	Capabilities       []CapabilityRequirement `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	ApprovalRules      []ApprovalRule          `json:"approval_rules,omitempty" yaml:"approval_rules,omitempty"`
	Connections        []ConnectionBlueprint   `json:"connections,omitempty" yaml:"connections,omitempty"`
	Workflows          []WorkflowTemplate      `json:"workflows,omitempty" yaml:"workflows,omitempty"`
}

type StarterPlan struct {
	LeadSlug                  string           `json:"lead_slug,omitempty" yaml:"lead_slug,omitempty"`
	GeneralChannelDescription string           `json:"general_channel_description,omitempty" yaml:"general_channel_description,omitempty"`
	KickoffPrompt             string           `json:"kickoff_prompt,omitempty" yaml:"kickoff_prompt,omitempty"`
	Agents                    []StarterAgent   `json:"agents,omitempty" yaml:"agents,omitempty"`
	Channels                  []StarterChannel `json:"channels,omitempty" yaml:"channels,omitempty"`
	Tasks                     []StarterTask    `json:"tasks,omitempty" yaml:"tasks,omitempty"`
}

type BootstrapConfig struct {
	ChannelName       string              `json:"channel_name,omitempty" yaml:"channel_name,omitempty"`
	ChannelSlug       string              `json:"channel_slug,omitempty" yaml:"channel_slug,omitempty"`
	Niche             string              `json:"niche,omitempty" yaml:"niche,omitempty"`
	Audience          string              `json:"audience,omitempty" yaml:"audience,omitempty"`
	Positioning       string              `json:"positioning,omitempty" yaml:"positioning,omitempty"`
	ContentPillars    []string            `json:"content_pillars,omitempty" yaml:"content_pillars,omitempty"`
	ContentSeries     []string            `json:"content_series,omitempty" yaml:"content_series,omitempty"`
	MonetizationHooks []string            `json:"monetization_hooks,omitempty" yaml:"monetization_hooks,omitempty"`
	PublishingCadence string              `json:"publishing_cadence,omitempty" yaml:"publishing_cadence,omitempty"`
	LeadMagnet        LeadMagnet          `json:"lead_magnet,omitempty" yaml:"lead_magnet,omitempty"`
	MonetizationAsset []MonetizationAsset `json:"monetization_assets,omitempty" yaml:"monetization_assets,omitempty"`
	KPITracking       []KPI               `json:"kpi_tracking,omitempty" yaml:"kpi_tracking,omitempty"`
}

type LeadMagnet struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	CTA  string `json:"cta,omitempty" yaml:"cta,omitempty"`
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
}

type MonetizationAsset struct {
	Stage string `json:"stage,omitempty" yaml:"stage,omitempty"`
	Name  string `json:"name,omitempty" yaml:"name,omitempty"`
	Slot  string `json:"slot,omitempty" yaml:"slot,omitempty"`
	CTA   string `json:"cta,omitempty" yaml:"cta,omitempty"`
}

type KPI struct {
	Name   string `json:"name,omitempty" yaml:"name,omitempty"`
	Target string `json:"target,omitempty" yaml:"target,omitempty"`
	Why    string `json:"why,omitempty" yaml:"why,omitempty"`
}

type MonetizationStep struct {
	Kicker string `json:"kicker,omitempty" yaml:"kicker,omitempty"`
	Title  string `json:"title" yaml:"title"`
	Copy   string `json:"copy,omitempty" yaml:"copy,omitempty"`
	Footer string `json:"footer,omitempty" yaml:"footer,omitempty"`
}

type QueueItem struct {
	ID           string `json:"id" yaml:"id"`
	Title        string `json:"title" yaml:"title"`
	Format       string `json:"format" yaml:"format"`
	StageIndex   int    `json:"stageIndex" yaml:"stage_index"`
	Score        int    `json:"score" yaml:"score"`
	UnitCost     int    `json:"unitCost" yaml:"unit_cost"`
	Eta          string `json:"eta,omitempty" yaml:"eta,omitempty"`
	Monetization string `json:"monetization,omitempty" yaml:"monetization,omitempty"`
	State        string `json:"state,omitempty" yaml:"state,omitempty"`
}

type StarterAgent struct {
	Slug              string   `json:"slug" yaml:"slug"`
	Emoji             string   `json:"emoji,omitempty" yaml:"emoji,omitempty"`
	Name              string   `json:"name" yaml:"name"`
	Role              string   `json:"role,omitempty" yaml:"role,omitempty"`
	EmployeeBlueprint string   `json:"employee_blueprint,omitempty" yaml:"employee_blueprint,omitempty"`
	Checked           bool     `json:"checked,omitempty" yaml:"checked,omitempty"`
	Type              string   `json:"type,omitempty" yaml:"type,omitempty"`
	PermissionMode    string   `json:"permission_mode,omitempty" yaml:"permission_mode,omitempty"`
	BuiltIn           bool     `json:"built_in,omitempty" yaml:"built_in,omitempty"`
	Expertise         []string `json:"expertise,omitempty" yaml:"expertise,omitempty"`
	Personality       string   `json:"personality,omitempty" yaml:"personality,omitempty"`
}

type StarterChannel struct {
	Slug        string   `json:"slug" yaml:"slug"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Members     []string `json:"members,omitempty" yaml:"members,omitempty"`
}

type StarterTask struct {
	Channel string `json:"channel" yaml:"channel"`
	Owner   string `json:"owner" yaml:"owner"`
	Title   string `json:"title" yaml:"title"`
	Details string `json:"details,omitempty" yaml:"details,omitempty"`
}

type EmployeeBlueprint struct {
	ID               string   `json:"id" yaml:"id"`
	Name             string   `json:"name" yaml:"name"`
	Kind             string   `json:"kind,omitempty" yaml:"kind,omitempty"`
	Description      string   `json:"description,omitempty" yaml:"description,omitempty"`
	Summary          string   `json:"summary,omitempty" yaml:"summary,omitempty"`
	Role             string   `json:"role,omitempty" yaml:"role,omitempty"`
	Responsibilities []string `json:"responsibilities,omitempty" yaml:"responsibilities,omitempty"`
	StartingTasks    []string `json:"starting_tasks,omitempty" yaml:"starting_tasks,omitempty"`
	AutomatedLoops   []string `json:"automated_loops,omitempty" yaml:"automated_loops,omitempty"`
	Skills           []string `json:"skills,omitempty" yaml:"skills,omitempty"`
	Tools            []string `json:"tools,omitempty" yaml:"tools,omitempty"`
	ExpectedResults  []string `json:"expected_results,omitempty" yaml:"expected_results,omitempty"`
	UsedBy           []string `json:"used_by,omitempty" yaml:"used_by,omitempty"`
}

type StageDefinition struct {
	ID           string `json:"id" yaml:"id"`
	Name         string `json:"name" yaml:"name"`
	Engine       string `json:"engine,omitempty" yaml:"engine,omitempty"`
	Description  string `json:"description,omitempty" yaml:"description,omitempty"`
	ExitCriteria string `json:"exit_criteria,omitempty" yaml:"exit_criteria,omitempty"`
}

type ArtifactType struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type AutomationModule struct {
	ID     string `json:"id" yaml:"id"`
	Kicker string `json:"kicker,omitempty" yaml:"kicker,omitempty"`
	Title  string `json:"title" yaml:"title"`
	Copy   string `json:"copy,omitempty" yaml:"copy,omitempty"`
	Status string `json:"status,omitempty" yaml:"status,omitempty"`
	Footer string `json:"footer,omitempty" yaml:"footer,omitempty"`
}

type CapabilityRequirement struct {
	ID           string   `json:"id" yaml:"id"`
	Name         string   `json:"name" yaml:"name"`
	Kind         string   `json:"kind,omitempty" yaml:"kind,omitempty"`
	Integrations []string `json:"integrations,omitempty" yaml:"integrations,omitempty"`
	Description  string   `json:"description,omitempty" yaml:"description,omitempty"`
}

type ApprovalRule struct {
	ID          string `json:"id" yaml:"id"`
	Trigger     string `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

type ConnectionBlueprint struct {
	Name        string `json:"name" yaml:"name"`
	Integration string `json:"integration" yaml:"integration"`
	Owner       string `json:"owner,omitempty" yaml:"owner,omitempty"`
	Priority    string `json:"priority,omitempty" yaml:"priority,omitempty"`
	Purpose     string `json:"purpose,omitempty" yaml:"purpose,omitempty"`
	SmokeTest   string `json:"smoke_test,omitempty" yaml:"smoke_test,omitempty"`
	Blocker     string `json:"blocker,omitempty" yaml:"blocker,omitempty"`
}

type WorkflowTemplate struct {
	ID           string            `json:"id" yaml:"id"`
	Name         string            `json:"name" yaml:"name"`
	Trigger      string            `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	Mode         string            `json:"mode,omitempty" yaml:"mode,omitempty"`
	Schedule     string            `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	Integrations []string          `json:"integrations,omitempty" yaml:"integrations,omitempty"`
	Checklist    []string          `json:"checklist,omitempty" yaml:"checklist,omitempty"`
	Description  string            `json:"description,omitempty" yaml:"description,omitempty"`
	Definition   map[string]any    `json:"definition,omitempty" yaml:"definition,omitempty"`
	SmokeTest    WorkflowSmokeTest `json:"smoke_test,omitempty" yaml:"smoke_test,omitempty"`
}

type WorkflowSmokeTest struct {
	Name   string         `json:"name,omitempty" yaml:"name,omitempty"`
	Mode   string         `json:"mode,omitempty" yaml:"mode,omitempty"`
	Proof  string         `json:"proof,omitempty" yaml:"proof,omitempty"`
	Inputs map[string]any `json:"inputs,omitempty" yaml:"inputs,omitempty"`
}

type CompanyProfile struct {
	Name        string
	Industry    string
	Description string
	Audience    string
	Website     string
	Geography   string
	Offer       string
	Notes       []string
}

type RuntimeIntegration struct {
	Name        string
	Provider    string
	Status      string
	Purpose     string
	Description string
	Connected   bool
}

type RuntimeCapability struct {
	Key       string
	Name      string
	Category  string
	Lifecycle string
	Detail    string
}
