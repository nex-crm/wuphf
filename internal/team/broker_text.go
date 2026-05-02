package team

import (
	"regexp"
	"strings"

	"github.com/nex-crm/wuphf/internal/company"
)

// Text helpers: @-mention extraction from free-form message bodies,
// slug-list de-duplication / normalization, content truncation, and
// the slug→display-name humanizer. Pure functions, no broker state.
//
// The mention regex is the only stateful thing in here (a compiled
// pattern at package init); callers don't construct it themselves.

// mentionPattern matches @slug tokens in free-form message text. Slugs are
// alphanumeric plus hyphen, 2–30 chars (to dodge false positives from
// conversational @ use). A preceding word character (e.g. email "a@b.com")
// is excluded via the lookahead-free alternative: we anchor to start of
// string or a non-alphanumeric rune.
var mentionPattern = regexp.MustCompile(`(?:^|[^a-zA-Z0-9_])@([a-z0-9][a-z0-9-]{1,29})\b`)

// extractMentionedSlugs pulls @-mention slugs out of body content. Duplicates
// are removed. The caller is responsible for validating whether each slug is
// a real office member.
func extractMentionedSlugs(content string) []string {
	matches := mentionPattern.FindAllStringSubmatch(strings.ToLower(content), -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		slug := m[1]
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	return out
}

func uniqueSlugs(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = normalizeChannelSlug(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeStringList(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func humanizeSlug(slug string) string {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(slug), "-", " "), " ")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func defaultTeamChannelDescription(slug, name string) string {
	manifest, err := company.LoadManifest()
	if err == nil {
		for _, ch := range manifest.Channels {
			if normalizeChannelSlug(ch.Slug) == normalizeChannelSlug(slug) && strings.TrimSpace(ch.Description) != "" {
				return strings.TrimSpace(ch.Description)
			}
		}
	}
	if normalizeChannelSlug(slug) == "general" {
		return "The default company-wide room for top-level coordination, announcements, and cross-functional discussion."
	}
	label := strings.TrimSpace(name)
	if label == "" {
		label = humanizeSlug(slug)
	}
	return label + " focused work. Use this channel for discussion, decisions, and execution specific to that stream."
}

func truncateSummary(s string, max int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}
