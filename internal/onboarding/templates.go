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
