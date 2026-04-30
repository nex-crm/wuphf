package channelui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// mentionPattern matches an "@slug" mention — slug is one or more
// alphanumeric, underscore, or hyphen characters. The capture group
// holds just the slug (without the leading @), but HighlightMentions
// uses the full match.
var mentionPattern = regexp.MustCompile(`@([A-Za-z0-9_-]+)`)

// HighlightMentions wraps every "@slug" mention in text with a
// bold-foreground style colored after agentColors[slug]. Slugs are
// looked up case-insensitively against agentColors so callers don't
// need to normalize the map themselves. Mentions whose slug isn't in
// the map are returned unchanged.
func HighlightMentions(text string, agentColors map[string]string) string {
	if len(agentColors) == 0 {
		return text
	}
	lowerColors := make(map[string]string, len(agentColors))
	for k, v := range agentColors {
		lowerColors[strings.ToLower(k)] = v
	}
	return mentionPattern.ReplaceAllStringFunc(text, func(match string) string {
		slug := strings.TrimPrefix(strings.ToLower(match), "@")
		color := lowerColors[slug]
		if color == "" {
			return match
		}
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(color)).
			Bold(true).
			Render(match)
	})
}
