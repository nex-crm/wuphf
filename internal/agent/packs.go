package agent

// PackSkillSpec defines a skill to pre-seed when a pack is first launched.
type PackSkillSpec struct {
	Name        string
	Title       string
	Description string
	Tags        []string
	Trigger     string
	Content     string
}

// PackDefinition defines a team of agents that work together.
type PackDefinition struct {
	Slug          string
	Name          string
	Description   string
	LeadSlug      string
	Agents        []AgentConfig
}

// legacyPacks retains the old hard-coded pack registry strictly as a
// compatibility fallback for callers that have not yet moved to operation
// blueprints.
var legacyPacks = []PackDefinition{
	{
		Slug:        "starter",
		Name:        "Starter Team",
		Description: "CEO, engineer, and GTM — the three roles that actually ship and sell",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{Slug: "ceo", Name: "CEO", Expertise: []string{"strategy", "decision-making", "prioritization", "delegation", "orchestration"}, Personality: "Strategic leader who breaks down directives into clear specialist assignments", PermissionMode: "plan"},
			{Slug: "eng", Name: "Founding Engineer", Expertise: []string{"full-stack", "backend", "frontend", "APIs", "databases", "architecture", "DevOps"}, Personality: "Scrappy full-stack engineer who ships fast and keeps the system simple until it needs to be complex", PermissionMode: "auto", AllowedTools: []string{"Edit", "Write", "Bash(go*,git*,npm*,make*)"}},
			{Slug: "gtm", Name: "GTM Lead", Expertise: []string{"go-to-market", "sales", "outreach", "positioning", "content", "pipeline", "ICP", "growth"}, Personality: "Revenue-focused generalist who handles the full GTM motion from messaging to closed deals", PermissionMode: "plan"},
		},
	},
	{
		Slug:        "founding-team",
		Name:        "Founding Team",
		Description: "Full autonomous company — CEO delegates to specialists",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{Slug: "ceo", Name: "CEO", Expertise: []string{"strategy", "decision-making", "prioritization", "delegation", "orchestration"}, Personality: "Strategic leader who breaks down complex directives into clear specialist assignments", PermissionMode: "plan"},
			{Slug: "pm", Name: "Product Manager", Expertise: []string{"roadmap", "user-stories", "requirements", "prioritization", "specs"}, Personality: "Detail-oriented PM who translates business needs into actionable specs", PermissionMode: "plan"},
			{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend", "React", "CSS", "UI-UX", "components"}, Personality: "Frontend specialist focused on clean, accessible implementations", PermissionMode: "auto", AllowedTools: []string{"Edit", "Write", "Bash(npm*)"}},
			{Slug: "be", Name: "Backend Engineer", Expertise: []string{"backend", "APIs", "databases", "infrastructure", "architecture"}, Personality: "Backend engineer focused on reliable, scalable systems", PermissionMode: "auto", AllowedTools: []string{"Edit", "Write", "Bash(go*,git*)"}},
			{Slug: "ai", Name: "AI Engineer", Expertise: []string{"LLMs", "AI-product-design", "retrieval", "evaluations", "agents", "model-integration"}, Personality: "AI engineer focused on making model-powered features reliable, useful, and actually shippable", PermissionMode: "auto", AllowedTools: []string{"Edit", "Write", "Bash(curl*,python*,pip*)"}},
			{Slug: "designer", Name: "Designer", Expertise: []string{"UI-UX-design", "branding", "visual-systems", "prototyping"}, Personality: "Creative designer who balances aesthetics with usability", PermissionMode: "plan"},
			{Slug: "cmo", Name: "CMO", Expertise: []string{"marketing", "content", "brand", "growth", "analytics", "campaigns"}, Personality: "Growth-focused marketer who drives awareness and engagement", PermissionMode: "plan"},
			{Slug: "cro", Name: "CRO", Expertise: []string{"sales", "pipeline", "revenue", "partnerships", "outreach", "closing"}, Personality: "Revenue-driven closer who builds pipeline and converts deals", PermissionMode: "plan"},
		},
	},
	{
		Slug:        "coding-team",
		Name:        "Coding Team",
		Description: "High-velocity software development team",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{Slug: "ceo", Name: "CEO", Expertise: []string{"architecture", "code-review", "technical-decisions", "planning"}, Personality: "Senior technical leader who makes sound architectural decisions and coordinates the team"},
			{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend", "React", "CSS", "components", "accessibility"}, Personality: "Frontend specialist focused on clean, accessible implementations"},
			{Slug: "be", Name: "Backend Engineer", Expertise: []string{"backend", "APIs", "databases", "DevOps", "infrastructure"}, Personality: "Backend engineer focused on reliable, scalable systems"},
			{Slug: "qa", Name: "QA Engineer", Expertise: []string{"testing", "automation", "quality", "edge-cases", "CI-CD"}, Personality: "Quality-focused engineer who catches issues before they reach production"},
		},
	},
	{
		Slug:        "lead-gen-agency",
		Name:        "Lead Gen Agency",
		Description: "Quiet outbound systems and automated GTM",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{Slug: "ceo", Name: "CEO", Expertise: []string{"prospecting", "outreach", "pipeline", "closing", "negotiation", "revenue-leadership"}, Personality: "Seasoned closer who builds relationships, converts opportunities, and sets the outbound strategy"},
			{Slug: "sdr", Name: "SDR", Expertise: []string{"cold-outreach", "qualification", "booking-meetings", "sequences"}, Personality: "Persistent SDR who opens doors and qualifies opportunities"},
			{Slug: "research", Name: "Research Analyst", Expertise: []string{"market-research", "competitive-analysis", "ICP-profiling", "trends"}, Personality: "Analytical researcher who surfaces actionable intelligence"},
			{Slug: "content", Name: "Content Strategist", Expertise: []string{"SEO", "copywriting", "nurture-sequences", "thought-leadership"}, Personality: "Strategic writer who creates content that drives engagement"},
		},
	},
	{
		Slug:        "revops",
		Name:        "RevOps Team",
		Description: "Revenue operations team — CRM hygiene, pipeline health, and GTM execution",
		LeadSlug:    "ceo",
		Agents: []AgentConfig{
			{
				Slug:           "ceo",
				Name:           "Chief Revenue Officer",
				Expertise:      []string{"revenue-leadership", "GTM-strategy", "prioritization", "delegation", "orchestration", "forecasting"},
				Personality:    "Revenue-obsessed leader who breaks down GTM directives into clear specialist assignments. Routes CRM hygiene to the analyst, deal work to the AE, outbound to the SDR, and keeps ops-lead focused on pipeline mechanics.",
				PermissionMode: "plan",
			},
			{
				Slug:           "ops-lead",
				Name:           "Revenue Operations Lead",
				Expertise:      []string{"revenue-operations", "GTM-strategy", "pipeline-management", "forecasting", "CRM", "data-quality", "process-design"},
				Personality:    "Data-driven RevOps lead who spots pipeline leaks, enforces CRM discipline, and keeps the GTM machine humming. Reports to the CRO on pipeline health and process improvements.",
				PermissionMode: "plan",
			},
			{
				Slug:           "ae",
				Name:           "Account Executive",
				Expertise:      []string{"pipeline", "deal-management", "closing", "negotiation", "stakeholder-mapping", "discovery", "objection-handling"},
				Personality:    "Seasoned AE focused on moving deals forward. Keeps detailed notes, flags stalled opportunities early, and knows when to escalate vs. push through.",
				PermissionMode: "plan",
			},
			{
				Slug:           "sdr",
				Name:           "SDR",
				Expertise:      []string{"outbound", "cold-outreach", "prospecting", "sequences", "qualification", "re-engagement", "ICP-targeting"},
				Personality:    "High-output SDR who writes sharp, relevant outreach. Understands that personalization beats volume and always ties messaging to business context.",
				PermissionMode: "plan",
			},
			{
				Slug:           "analyst",
				Name:           "Revenue Analyst",
				Expertise:      []string{"CRM-hygiene", "data-quality", "lead-scoring", "reporting", "funnel-analysis", "attribution", "forecasting"},
				Personality:    "Methodical analyst who treats the CRM as a source of truth, not a passive archive. Flags data gaps, builds scoring models, and turns pipeline data into decisions.",
				PermissionMode: "plan",
			},
		},

	},
}

// ListLegacyPacks returns a copy of the compatibility pack registry.
func ListLegacyPacks() []PackDefinition {
	out := make([]PackDefinition, 0, len(legacyPacks))
	for _, pack := range legacyPacks {
		cloned := pack
		cloned.Agents = append([]AgentConfig(nil), pack.Agents...)
		out = append(out, cloned)
	}
	return out
}

// LookupLegacyPack returns the compatibility pack with the given slug, or nil
// if not found.
func LookupLegacyPack(slug string) *PackDefinition {
	for i := range legacyPacks {
		if legacyPacks[i].Slug == slug {
			pack := legacyPacks[i]
			pack.Agents = append([]AgentConfig(nil), legacyPacks[i].Agents...)
			return &pack
		}
	}
	return nil
}

// GetPack is a deprecated compatibility alias for LookupLegacyPack.
func GetPack(slug string) *PackDefinition { return LookupLegacyPack(slug) }

// PackSlugs returns the list of all registered pack slugs, in registration order.
func PackSlugs() []string {
	slugs := make([]string, 0, len(legacyPacks))
	for i := range legacyPacks {
		slugs = append(slugs, legacyPacks[i].Slug)
	}
	return slugs
}
