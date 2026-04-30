package channelui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderUsageStrip renders the "Spend by teammate" strip below the
// office feed: one pill per agent showing avatar, formatted token
// count, and dollar cost. Agents are ordered by appearance in members
// first (preserving channel order), then by the canonical office
// roster, then any remaining usage.Agents entries sorted alphabetically
// (so dynamic / unrostered slugs render in stable order).
// Returns "" when there are no agents tracked or width is below the
// 40-column readability floor.
func RenderUsageStrip(usage UsageState, members []Member, width int) string {
	if len(usage.Agents) == 0 || width < 40 {
		return ""
	}

	var ordered []string
	seen := make(map[string]bool)
	for _, member := range members {
		slug := strings.TrimSpace(member.Slug)
		if slug == "" {
			continue
		}
		if _, ok := usage.Agents[slug]; ok && !seen[slug] {
			ordered = append(ordered, slug)
			seen[slug] = true
		}
	}
	for _, slug := range canonicalRosterSlugs {
		if _, ok := usage.Agents[slug]; ok && !seen[slug] {
			ordered = append(ordered, slug)
			seen[slug] = true
		}
	}
	var rest []string
	for slug := range usage.Agents {
		if strings.TrimSpace(slug) == "" || seen[slug] {
			continue
		}
		rest = append(rest, slug)
	}
	sort.Strings(rest)
	ordered = append(ordered, rest...)

	pillStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#CBD5E1")).
		Background(lipgloss.Color("#111827")).
		Padding(0, 1)

	var pills []string
	for _, slug := range ordered {
		totals := usage.Agents[slug]
		if totals.TotalTokens == 0 && totals.CostUsd == 0 {
			continue
		}
		label := fmt.Sprintf("%s %s · %s", AgentAvatar(slug), FormatTokenCount(totals.TotalTokens), FormatUSD(totals.CostUsd))
		pills = append(pills, pillStyle.Render(label))
	}
	if len(pills) == 0 {
		return ""
	}
	prefix := lipgloss.NewStyle().Foreground(lipgloss.Color(SlackMuted)).Render("  Spend by teammate")
	return prefix + "  " + strings.Join(pills, " ")
}

// SidebarShortcutLabel returns the digit shortcut for sidebar item
// index ("1".."9") for index 0..8, or "" when out of range. Keys
// 1-9 jump to the corresponding sidebar item via the quick-jump
// overlay.
func SidebarShortcutLabel(index int) string {
	if index < 0 || index > 8 {
		return ""
	}
	return fmt.Sprintf("%d", index+1)
}
