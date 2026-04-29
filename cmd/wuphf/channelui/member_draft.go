package channelui

import "strings"

// NormalizeDraftSlug canonicalizes a member draft slug — lower-cases,
// trims whitespace, and replaces spaces and underscores with hyphens.
// Mirrors NormalizeSidebarSlug but is intentionally kept separate so
// future drafts can diverge (e.g. enforce a different character set)
// without affecting sidebar-side equality.
func NormalizeDraftSlug(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	raw = strings.ReplaceAll(raw, " ", "-")
	raw = strings.ReplaceAll(raw, "_", "-")
	return raw
}

// ParseExpertiseInput splits a comma-separated expertise string into
// a deduped, trimmed list of expertise tags. Empty entries are
// dropped; case-sensitive duplicates are dropped on first occurrence.
func ParseExpertiseInput(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

// LiveActivityFromMembers returns a slug -> activity map for members
// currently doing real work in their Claude Code instance. The "you"
// slug and members with empty LiveActivity are filtered out.
func LiveActivityFromMembers(members []Member) map[string]string {
	result := make(map[string]string)
	for _, m := range members {
		if m.Slug == "you" || m.LiveActivity == "" {
			continue
		}
		result[m.Slug] = m.LiveActivity
	}
	return result
}
