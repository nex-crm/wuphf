package agent

// PackDefinition defines a team of agents that work together.
type PackDefinition struct {
	Slug        string
	Name        string
	Description string
	LeadSlug    string
	Agents      []AgentConfig
}

// Packs is the registry of all available agent packs.
var Packs = []PackDefinition{
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
		LeadSlug:    "tech-lead",
		Agents: []AgentConfig{
			{Slug: "tech-lead", Name: "Tech Lead", Expertise: []string{"architecture", "code-review", "technical-decisions", "planning"}, Personality: "Senior engineer who makes sound architectural decisions and coordinates the team"},
			{Slug: "fe", Name: "Frontend Engineer", Expertise: []string{"frontend", "React", "CSS", "components", "accessibility"}, Personality: "Frontend specialist focused on clean, accessible implementations"},
			{Slug: "be", Name: "Backend Engineer", Expertise: []string{"backend", "APIs", "databases", "DevOps", "infrastructure"}, Personality: "Backend engineer focused on reliable, scalable systems"},
			{Slug: "qa", Name: "QA Engineer", Expertise: []string{"testing", "automation", "quality", "edge-cases", "CI-CD"}, Personality: "Quality-focused engineer who catches issues before they reach production"},
		},
	},
	{
		Slug:        "lead-gen-agency",
		Name:        "Lead Gen Agency",
		Description: "Quiet outbound systems and automated GTM",
		LeadSlug:    "ae",
		Agents: []AgentConfig{
			{Slug: "ae", Name: "Account Executive", Expertise: []string{"prospecting", "outreach", "pipeline", "closing", "negotiation"}, Personality: "Seasoned closer who builds relationships and converts opportunities"},
			{Slug: "sdr", Name: "SDR", Expertise: []string{"cold-outreach", "qualification", "booking-meetings", "sequences"}, Personality: "Persistent SDR who opens doors and qualifies opportunities"},
			{Slug: "research", Name: "Research Analyst", Expertise: []string{"market-research", "competitive-analysis", "ICP-profiling", "trends"}, Personality: "Analytical researcher who surfaces actionable intelligence"},
			{Slug: "content", Name: "Content Strategist", Expertise: []string{"SEO", "copywriting", "nurture-sequences", "thought-leadership"}, Personality: "Strategic writer who creates content that drives engagement"},
		},
	},
}

// GetPack returns the pack with the given slug, or nil if not found.
func GetPack(slug string) *PackDefinition {
	for i := range Packs {
		if Packs[i].Slug == slug {
			return &Packs[i]
		}
	}
	return nil
}
