package onboarding

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/nex-crm/wuphf/internal/operations"
)

// TaskTemplate describes a first-task suggestion shown during onboarding.
// Templates are scoped to a specific agent role via OwnerSlug.
type TaskTemplate struct {
	// ID is a stable, URL-safe identifier for the template.
	ID string `json:"id"`

	// Title is the short, human-readable task name.
	Title string `json:"title"`

	// Description is a single-sentence clarification shown below the title.
	Description string `json:"description"`

	// OwnerSlug is the agent slug that should receive this task.
	OwnerSlug string `json:"owner_slug"`
}

const blankSlateStarterTemplateID = "__blank_slate__"

// DefaultTemplates returns the generic fallback starter tasks used when no
// blueprint-specific task list can be resolved.
func DefaultTemplates() []TaskTemplate {
	return []TaskTemplate{
		{ID: "landing", Title: "Draft the landing page", Description: "Hero, value props, one clear CTA. Not the WUPHF.com approach.", OwnerSlug: "executor"},
		{ID: "repo", Title: "Set up repo structure", Description: "Folders, README, CI scaffold. Dwight would document everything.", OwnerSlug: "executor"},
		{ID: "spec", Title: "Write the product spec", Description: "What we're building, why, and what done looks like. Michael would skip this step.", OwnerSlug: "planner"},
		{ID: "readme", Title: "Write the README", Description: "Installation, usage, one example. Short enough that someone actually reads it.", OwnerSlug: "planner"},
		{ID: "audit", Title: "Audit the competition", Description: "What they do, what they miss, where we win. No memos. Just findings.", OwnerSlug: "ceo"},
	}
}

// RevOpsTemplates preserves the existing legacy pack-specific starter set.
func RevOpsTemplates() []TaskTemplate {
	return []TaskTemplate{
		{ID: "pipeline_audit", Title: "Run a pipeline audit", Description: "CRM hygiene sweep — stale deals, missing fields, bad data. Find the leaks before forecast.", OwnerSlug: "analyst"},
		{ID: "meeting_prep", Title: "Prep me for my next call", Description: "One-page brief on the account, deal stage, stakeholders, and the ask. No fluff.", OwnerSlug: "ae"},
		{ID: "revive_closed_lost", Title: "Revive closed-lost leads", Description: "Surface deals lost 3–18 months ago with trigger events. Draft re-engagement outreach.", OwnerSlug: "sdr"},
		{ID: "score_inbound", Title: "Score new inbound", Description: "Rate unworked leads on fit and intent. Route Tier 1 to the AE within 24 hours.", OwnerSlug: "analyst"},
		{ID: "stalled_deals", Title: "Find stalled deals", Description: "Open pipeline with no activity in 10+ days. Diagnose the cause and recommend a next step.", OwnerSlug: "ops-lead"},
	}
}

func BlankSlateTemplates() []TaskTemplate {
	return []TaskTemplate{
		{ID: "objective", Title: "Choose the first real business win", Description: "Turn the directive into one concrete outcome for a real customer, audience, or internal operation this week.", OwnerSlug: "founder"},
		{ID: "offer", Title: "Draft the first sellable offer", Description: "Name the customer, the promise, the scope, and the next decision needed to move the business forward.", OwnerSlug: "operator"},
		{ID: "delivery", Title: "Build the first delivery loop", Description: "Create the minimum workflow, handoffs, approvals, and artifacts needed to deliver the offer end to end.", OwnerSlug: "builder"},
		{ID: "instrumentation", Title: "Create the operating record", Description: "Set up the place where client state, approvals, and delivery evidence will live so the office can keep operating.", OwnerSlug: "founder"},
		{ID: "go-live", Title: "Create missing capabilities and take the first live step", Description: "If agents, channels, skills, or tooling are missing, create them, then execute the smallest safe real action in the business workflow.", OwnerSlug: "founder"},
	}
}

// TemplatesForPack is a legacy alias retained for older callers that still
// talk about packs.
func TemplatesForPack(packSlug string) []TaskTemplate {
	return TemplatesForSelection("", packSlug)
}

func TemplatesForSelection(repoRoot, selection string) []TaskTemplate {
	repoRoot = resolveTemplatesRepoRoot(repoRoot)
	selection = strings.TrimSpace(selection)
	switch selection {
	case blankSlateStarterTemplateID, "from-scratch", "blank-slate":
		return BlankSlateTemplates()
	}
	if repoRoot != "" && selection != "" {
		if blueprint, err := operations.LoadBlueprint(repoRoot, selection); err == nil {
			if templates := templatesFromBlueprint(blueprint); len(templates) > 0 {
				return templates
			}
		}
	}
	switch selection {
	case "revops":
		return RevOpsTemplates()
	default:
		return DefaultTemplates()
	}
}

func templatesFromBlueprint(blueprint operations.Blueprint) []TaskTemplate {
	out := make([]TaskTemplate, 0, len(blueprint.Starter.Tasks))
	for _, task := range blueprint.Starter.Tasks {
		title := strings.TrimSpace(task.Title)
		description := strings.TrimSpace(task.Details)
		owner := strings.TrimSpace(task.Owner)
		if title == "" || description == "" || owner == "" {
			continue
		}
		out = append(out, TaskTemplate{
			ID:          onboardingTemplateID(title),
			Title:       title,
			Description: description,
			OwnerSlug:   owner,
		})
		if len(out) == 5 {
			break
		}
	}
	return out
}

func onboardingTemplateID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, " ", "-")
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == '.':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// ResolveTemplatesRepoRoot walks up from repoRoot (or cwd if empty) until
// it finds a templates/operations directory, returning the containing
// path. Used by the broker to load curated blueprints when the user
// finishes onboarding.
func ResolveTemplatesRepoRoot(repoRoot string) string {
	return resolveTemplatesRepoRoot(repoRoot)
}

func resolveTemplatesRepoRoot(repoRoot string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return ""
		}
		repoRoot = cwd
	}
	for current := repoRoot; ; current = filepath.Dir(current) {
		if _, err := os.Stat(filepath.Join(current, "templates", "operations")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return ""
}
