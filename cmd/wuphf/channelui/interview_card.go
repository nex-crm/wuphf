package channelui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// RenderInterviewCard renders the rounded amber interview card for the
// pending request panel: header pills (kind label, optional phase,
// optional blocking / private accents), title, question body, optional
// context, optional timing summary, and the option list with the
// selected option arrowed. Falls through to a "Something else" custom
// row that the user can type into directly. width is clamped to a
// minimum of 40 columns.
func RenderInterviewCard(interview Interview, selected int, phaseTitle string, width int) string {
	cardWidth := width
	if cardWidth < 40 {
		cardWidth = 40
	}
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))

	cardLabel := "Request"
	switch strings.TrimSpace(interview.Kind) {
	case "interview":
		cardLabel = "Human Interview"
	case "approval":
		cardLabel = "Approval Request"
	case "confirm":
		cardLabel = "Confirmation Request"
	case "secret":
		cardLabel = "Private Request"
	case "freeform":
		cardLabel = "Open Question"
	}
	title := fmt.Sprintf("@%s needs your decision", interview.From)
	if strings.TrimSpace(interview.Title) != "" {
		title = interview.Title + " · @" + interview.From
	}
	headerBits := []string{labelStyle.Render(cardLabel)}
	if strings.TrimSpace(phaseTitle) != "" {
		headerBits = append(headerBits, SubtlePill(phaseTitle, "#DBEAFE", "#1D4ED8"))
	}
	if interview.Blocking {
		headerBits = append(headerBits, AccentPill("blocking", "#B45309"))
	}
	if interview.Secret {
		headerBits = append(headerBits, AccentPill("private", "#6D28D9"))
	}
	lines := []string{
		strings.Join(headerBits, "  "),
		titleStyle.Render(title),
		"",
		textStyle.Width(cardWidth - 4).Render(interview.Question),
	}
	if strings.TrimSpace(interview.Context) != "" {
		lines = append(lines, "")
		lines = append(lines, muted.Width(cardWidth-4).Render(interview.Context))
	}
	if timing := RenderTimingSummary(interview.DueAt, interview.FollowUpAt, interview.ReminderAt, interview.RecheckAt); timing != "" {
		lines = append(lines, "", muted.Render(timing))
	}
	lines = append(lines, "", muted.Render("Options"))
	for i, option := range interview.Options {
		prefix := "  "
		if i == selected {
			prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true).Render("→ ")
		}
		label := option.Label
		if option.ID == interview.RecommendedID {
			label += " (Recommended)"
		}
		lines = append(lines, prefix+titleStyle.Render(label))
		if strings.TrimSpace(option.Description) != "" {
			lines = append(lines, "    "+muted.Width(cardWidth-8).Render(option.Description))
		}
	}
	if hint := InterviewOptionTextHint(SelectedInterviewOption(interview.Options, selected)); hint != "" {
		lines = append(lines, "", muted.Width(cardWidth-4).Render(hint))
	}
	customPrefix := "  "
	if selected >= len(interview.Options) {
		customPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true).Render("→ ")
	}
	customLine := lipgloss.NewStyle().
		Foreground(lipgloss.Color(SlackMuted)).
		Render("Something else")
	lines = append(lines, customPrefix+customLine)
	lines = append(lines, "    "+muted.Width(cardWidth-8).Render("Type your own answer directly in the composer below."))
	lines = append(lines, "", muted.Render("Press Enter to accept the selected option, or type your own answer below."))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F59E0B")).
		Padding(0, 1).
		Width(cardWidth).
		Render(strings.Join(lines, "\n")) + "\n"
}
