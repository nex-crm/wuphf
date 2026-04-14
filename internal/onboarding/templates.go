package onboarding

// TaskTemplate describes a first-task suggestion shown during onboarding.
// Templates are scoped to a specific agent role via OwnerSlug.
type TaskTemplate struct {
	// ID is a stable, URL-safe identifier for the template.
	ID string `json:"id"`

	// Title is the short, human-readable task name.
	Title string `json:"title"`

	// Description is a single-sentence clarification shown below the title.
	Description string `json:"description"`

	// OwnerSlug is the agent slug that should receive this task (e.g. "eng",
	// "pm", "ceo"). Matches the default starter-pack slugs.
	OwnerSlug string `json:"owner_slug"`
}

// DefaultTemplates returns the five starter task templates scoped to the
// default CEO + Eng + PM pack. Copy leans on The Office for flavor —
// because Michael Scott would absolutely skip writing a spec.
func DefaultTemplates() []TaskTemplate {
	return []TaskTemplate{
		{
			ID:          "landing",
			Title:       "Draft the landing page",
			Description: "Hero, value props, one clear CTA. Not the WUPHF.com approach.",
			OwnerSlug:   "eng",
		},
		{
			ID:          "repo",
			Title:       "Set up repo structure",
			Description: "Folders, README, CI scaffold. Dwight would document everything.",
			OwnerSlug:   "eng",
		},
		{
			ID:          "spec",
			Title:       "Write the product spec",
			Description: "What we're building, why, and what done looks like. Michael would skip this step.",
			OwnerSlug:   "pm",
		},
		{
			ID:          "readme",
			Title:       "Write the README",
			Description: "Installation, usage, one example. Short enough that someone actually reads it.",
			OwnerSlug:   "pm",
		},
		{
			ID:          "audit",
			Title:       "Audit the competition",
			Description: "What they do, what they miss, where we win. No memos. Just findings.",
			OwnerSlug:   "ceo",
		},
	}
}

// RevOpsTemplates returns the five starter task templates scoped to the
// RevOps pack (CRO, ops-lead, AE, SDR, analyst). These map to the skills
// already pre-seeded by the pack so the first-task suggestions read as
// real work the team can execute on day one.
func RevOpsTemplates() []TaskTemplate {
	return []TaskTemplate{
		{
			ID:          "pipeline_audit",
			Title:       "Run a pipeline audit",
			Description: "CRM hygiene sweep — stale deals, missing fields, bad data. Find the leaks before forecast.",
			OwnerSlug:   "analyst",
		},
		{
			ID:          "meeting_prep",
			Title:       "Prep me for my next call",
			Description: "One-page brief on the account, deal stage, stakeholders, and the ask. No fluff.",
			OwnerSlug:   "ae",
		},
		{
			ID:          "revive_closed_lost",
			Title:       "Revive closed-lost leads",
			Description: "Surface deals lost 3–18 months ago with trigger events. Draft re-engagement outreach.",
			OwnerSlug:   "sdr",
		},
		{
			ID:          "score_inbound",
			Title:       "Score new inbound",
			Description: "Rate unworked leads on fit and intent. Route Tier 1 to the AE within 24 hours.",
			OwnerSlug:   "analyst",
		},
		{
			ID:          "stalled_deals",
			Title:       "Find stalled deals",
			Description: "Open pipeline with no activity in 10+ days. Diagnose the cause and recommend a next step.",
			OwnerSlug:   "ops-lead",
		},
	}
}

// TemplatesForPack returns the starter task templates for the given pack
// slug. Unknown or empty slugs fall through to DefaultTemplates so the
// founding-team behavior is preserved verbatim.
func TemplatesForPack(packSlug string) []TaskTemplate {
	switch packSlug {
	case "revops":
		return RevOpsTemplates()
	default:
		return DefaultTemplates()
	}
}
