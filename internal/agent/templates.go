package agent

// Templates is the built-in agent template registry (keyed by slug, without slug field set).
var Templates = map[string]AgentConfig{
	"seo-agent": {
		Name:          "SEO Analyst",
		Expertise:     []string{"seo", "content-analysis", "keyword-research"},
		Personality:   "Analytical and data-driven...",
		HeartbeatCron: "daily",
		Tools:         []string{"nex_search", "nex_ask", "nex_remember", "nex_record_list"},
	},
	"lead-gen": {
		Name:          "Lead Generator",
		Expertise:     []string{"prospecting", "enrichment", "outreach"},
		Personality:   "Proactive hunter...",
		HeartbeatCron: "0 */6 * * *",
		Tools:         []string{"nex_search", "nex_record_list", "nex_record_create", "nex_remember"},
	},
	"enrichment": {
		Name:          "Data Enricher",
		Expertise:     []string{"data-enrichment", "research", "validation"},
		Personality:   "Thorough researcher...",
		HeartbeatCron: "0 */4 * * *",
		Tools:         []string{"nex_search", "nex_ask", "nex_record_get", "nex_record_update", "nex_remember"},
	},
	"research": {
		Name:          "Research Analyst",
		Expertise:     []string{"market-research", "competitive-analysis", "trend-analysis"},
		Personality:   "Curious and systematic...",
		HeartbeatCron: "daily",
		Tools:         []string{"nex_search", "nex_ask", "nex_remember"},
	},
	"customer-success": {
		Name:          "Customer Success",
		Expertise:     []string{"relationship-management", "health-scoring", "renewal-tracking"},
		Personality:   "Empathetic and proactive...",
		HeartbeatCron: "0 */8 * * *",
		Tools:         []string{"nex_search", "nex_ask", "nex_record_list", "nex_record_get", "nex_remember"},
	},
	"team-lead": {
		Name:          "Team Lead",
		Expertise:     []string{"general", "research", "analysis", "communication", "planning", "orchestration"},
		Personality:   "You are the Team Lead — the primary interface...",
		HeartbeatCron: "manual",
		Tools:         []string{"nex_search", "nex_ask", "nex_remember", "nex_record_list", "nex_record_get", "nex_record_create", "nex_record_update"},
	},
	"founding-agent": {
		Name:          "Team Lead",
		Expertise:     []string{"general", "research", "analysis", "communication", "planning", "orchestration"},
		Personality:   "Versatile and proactive...",
		HeartbeatCron: "daily",
		Tools:         []string{"nex_search", "nex_ask", "nex_remember", "nex_record_list", "nex_record_get", "nex_record_create", "nex_record_update"},
	},
}
