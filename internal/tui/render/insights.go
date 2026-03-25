package render

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Insight represents a single insight entry with priority, category, and details.
type Insight struct {
	Priority string // "critical", "high", "medium", "low"
	Category string
	Title    string
	Body     string
	Target   string
	Time     string
}

var (
	critBadge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e23428"))
	highBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("#df750c"))
	medBadge  = lipgloss.NewStyle().Foreground(lipgloss.Color("#4d97ff"))
	lowBadge  = lipgloss.NewStyle().Foreground(lipgloss.Color("#838485"))
	titleSty  = lipgloss.NewStyle().Bold(true)
	dimSty    = lipgloss.NewStyle().Foreground(lipgloss.Color("#838485"))
)

// RenderInsights renders a list of insights with priority badges, categories,
// truncated bodies, target hints, and timestamps.
func RenderInsights(insights []Insight) string {
	if len(insights) == 0 {
		return MutedStyle.Render("(no insights)")
	}

	var b strings.Builder
	for i, ins := range insights {
		if i > 0 {
			b.WriteString(dimSty.Render("───") + "\n")
		}

		// Priority badge.
		badge := insightBadge(ins.Priority)

		// Category.
		cat := ""
		if ins.Category != "" {
			cat = dimSty.Render("["+ins.Category+"]") + " "
		}

		// Title line: badge [category] title   timestamp
		line := badge + " " + cat + titleSty.Render(ins.Title)
		if ins.Time != "" {
			line += "  " + dimSty.Render(ins.Time)
		}
		b.WriteString(line + "\n")

		// Body, truncated to 120 chars.
		if ins.Body != "" {
			body := truncateBody(ins.Body, 120)
			b.WriteString(dimSty.Render(body) + "\n")
		}

		// Target hint.
		if ins.Target != "" {
			b.WriteString(dimSty.Render("("+ins.Target+")") + "\n")
		}
	}

	return b.String()
}

func insightBadge(p string) string {
	switch strings.ToLower(p) {
	case "critical":
		return critBadge.Render("[CRIT]")
	case "high":
		return highBadge.Render("[HIGH]")
	case "medium":
		return medBadge.Render("[MED]")
	case "low":
		return lowBadge.Render("[LOW]")
	default:
		return lowBadge.Render("[" + strings.ToUpper(p) + "]")
	}
}

func truncateBody(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
