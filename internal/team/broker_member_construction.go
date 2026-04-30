package team

import (
	"strings"

	"github.com/nex-crm/wuphf/internal/company"
)

// Office member construction + inference. Owns:
//   - memberFromSpec: the canonical officeMember constructor (used by
//     defaults loader + HTTP create handler so the field-copy logic
//     lives once)
//   - applyOfficeMemberDefaults: fills Name / Role / Expertise /
//     Personality / PermissionMode when the input shape is partial
//   - inferOfficeExpertise / inferOfficePersonality: text-based defaults
//     keyed off slug + role substrings (frontend, backend, ai/ml,
//     marketing, sales, product, design)
//
// All three feed the same pipeline: convert a thin spec or partial
// member into a fully-populated officeMember ready to persist.

// memberFromSpec is the canonical officeMember constructor. Defensive copies
// the slice fields (Expertise, AllowedTools) and threads creation meta +
// Provider through. Used by defaultOfficeMembers and by HTTP create paths so
// field-copy logic lives in one place.
func memberFromSpec(spec company.MemberSpec, createdBy, createdAt string, builtIn bool) officeMember {
	return officeMember{
		Slug:           spec.Slug,
		Name:           spec.Name,
		Role:           spec.Role,
		Expertise:      append([]string(nil), spec.Expertise...),
		Personality:    spec.Personality,
		PermissionMode: spec.PermissionMode,
		AllowedTools:   append([]string(nil), spec.AllowedTools...),
		CreatedBy:      createdBy,
		CreatedAt:      createdAt,
		BuiltIn:        builtIn,
		Provider:       spec.Provider,
	}
}

func applyOfficeMemberDefaults(member *officeMember) {
	if member == nil {
		return
	}
	if member.Name == "" {
		member.Name = humanizeSlug(member.Slug)
	}
	if member.Role == "" {
		member.Role = member.Name
	}
	if len(member.Expertise) == 0 {
		member.Expertise = inferOfficeExpertise(member.Slug, member.Role)
	}
	if member.Personality == "" {
		member.Personality = inferOfficePersonality(member.Slug, member.Role)
	}
	if member.PermissionMode == "" {
		member.PermissionMode = "plan"
	}
}

func inferOfficeExpertise(slug, role string) []string {
	text := strings.ToLower(strings.TrimSpace(slug + " " + role))
	switch {
	case strings.Contains(text, "front"), strings.Contains(text, "ui"), strings.Contains(text, "design eng"):
		return []string{"frontend", "UI", "interaction design", "components", "accessibility"}
	case strings.Contains(text, "back"), strings.Contains(text, "api"), strings.Contains(text, "infra"):
		return []string{"backend", "APIs", "systems", "infrastructure", "databases"}
	case strings.Contains(text, "ai"), strings.Contains(text, "ml"), strings.Contains(text, "llm"):
		return []string{"AI", "LLMs", "agents", "retrieval", "evaluations"}
	case strings.Contains(text, "market"), strings.Contains(text, "brand"), strings.Contains(text, "growth"):
		return []string{"marketing", "growth", "positioning", "campaigns", "brand"}
	case strings.Contains(text, "revenue"), strings.Contains(text, "sales"), strings.Contains(text, "cro"):
		return []string{"sales", "revenue", "pipeline", "partnerships", "closing"}
	case strings.Contains(text, "product"), strings.Contains(text, "pm"):
		return []string{"product", "roadmap", "requirements", "prioritization", "scope"}
	case strings.Contains(text, "design"):
		return []string{"design", "UX", "visual systems", "prototyping", "brand"}
	default:
		return []string{strings.ToLower(strings.TrimSpace(role))}
	}
}

func inferOfficePersonality(slug, role string) string {
	text := strings.ToLower(strings.TrimSpace(slug + " " + role))
	switch {
	case strings.Contains(text, "front"):
		return "Frontend specialist focused on polished user-facing work and sharp interaction details."
	case strings.Contains(text, "back"):
		return "Systems-minded engineer who keeps complexity under control and worries about reliability."
	case strings.Contains(text, "ai"), strings.Contains(text, "ml"), strings.Contains(text, "llm"):
		return "AI engineer who likes ambitious ideas but immediately asks how they will actually work."
	case strings.Contains(text, "market"), strings.Contains(text, "brand"), strings.Contains(text, "growth"):
		return "Growth and positioning operator who translates product work into market momentum."
	case strings.Contains(text, "revenue"), strings.Contains(text, "sales"):
		return "Commercial operator who thinks in demand, objections, and revenue consequences."
	case strings.Contains(text, "product"), strings.Contains(text, "pm"):
		return "Product thinker who turns ambiguity into scope, sequencing, and crisp tradeoffs."
	case strings.Contains(text, "design"):
		return "Taste-driven designer who cares about clarity, craft, and how the product actually feels."
	default:
		return "A sharp teammate with a clear specialty, strong point of view, and enough personality to feel human."
	}
}
