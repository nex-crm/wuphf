package channelui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ChannelConfirmAction identifies the action a confirmation card is
// asking the user to authorize. Used by the channelModel-bound
// executeConfirmation dispatcher to pick the correct command.
type ChannelConfirmAction string

const (
	ChannelConfirmActionResetTeam     ChannelConfirmAction = "reset_team"
	ChannelConfirmActionResetDM       ChannelConfirmAction = "reset_dm"
	ChannelConfirmActionSwitchMode    ChannelConfirmAction = "switch_mode"
	ChannelConfirmActionRecoverFocus  ChannelConfirmAction = "recover_focus"
	ChannelConfirmActionSubmitRequest ChannelConfirmAction = "submit_request"
)

// ChannelConfirm is a pending confirmation card — title, detail body,
// confirm/cancel labels, and action-specific payload (the session
// mode / agent / channel / interview answer parts the dispatcher
// uses to actually act). Only one ChannelConfirm is in flight at a
// time on the channelModel.
type ChannelConfirm struct {
	Title        string
	Detail       string
	ConfirmLabel string
	CancelLabel  string
	Action       ChannelConfirmAction
	SessionMode  string
	Agent        string
	Channel      string
	Request      *Interview
	ChoiceID     string
	ChoiceText   string
	CustomText   string
}

// ConfirmationForResetDM builds the "Clear Direct Messages" confirm
// card for a DM reset against agent in the given channel.
func ConfirmationForResetDM(agent, channel string) *ChannelConfirm {
	return &ChannelConfirm{
		Title:        "Clear Direct Messages",
		Detail:       fmt.Sprintf("This deletes the saved direct transcript with %s for this session.", DisplayName(agent)),
		ConfirmLabel: "Enter clear DMs",
		CancelLabel:  "Esc keep transcript",
		Action:       ChannelConfirmActionResetDM,
		Agent:        agent,
		Channel:      channel,
	}
}

// ConfirmationForInterviewAnswer builds the "Review Human Answer"
// confirm card for an interview submission. Detail lines summarize
// the question, the chosen option (when any), and any custom text.
// When the user is submitting only custom text without a choice, the
// detail prompts them to type something first.
func ConfirmationForInterviewAnswer(interview Interview, option *InterviewOption, customText string) *ChannelConfirm {
	title := "Review Human Answer"
	detailLines := []string{
		fmt.Sprintf("Question: %s", strings.TrimSpace(interview.Question)),
	}
	if option != nil && strings.TrimSpace(option.Label) != "" {
		detailLines = append(detailLines, fmt.Sprintf("Choice: %s", strings.TrimSpace(option.Label)))
	}
	customText = strings.TrimSpace(customText)
	if customText != "" {
		detailLines = append(detailLines, fmt.Sprintf("Note: %s", customText))
	}
	if len(detailLines) == 1 && option == nil {
		detailLines = append(detailLines, "Type an answer before submitting.")
	}
	choiceID := ""
	choiceText := ""
	if option != nil {
		choiceID = strings.TrimSpace(option.ID)
		choiceText = strings.TrimSpace(option.Label)
	}
	return &ChannelConfirm{
		Title:        title,
		Detail:       strings.Join(detailLines, "\n\n"),
		ConfirmLabel: "Enter send answer",
		CancelLabel:  "Esc keep editing",
		Action:       ChannelConfirmActionSubmitRequest,
		Request:      &interview,
		ChoiceID:     choiceID,
		ChoiceText:   choiceText,
		CustomText:   customText,
	}
}

// RenderConfirmCard renders a ChannelConfirm as a rounded-border card
// at min(48, width) columns: title, blank, body wrapped to the card
// width, blank, then a muted footer with the confirm and cancel
// labels.
func RenderConfirmCard(confirm ChannelConfirm, width int) string {
	cardWidth := MinInt(48, width)
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).Render(confirm.Title)
	body := lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")).Width(cardWidth - 4).Render(confirm.Detail)
	footer := MutedText(confirm.ConfirmLabel + "  ·  " + confirm.CancelLabel)
	lines := []string{
		title,
		"",
		body,
		"",
		footer,
	}
	return lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7C2D12")).
		Background(lipgloss.Color("#14151B")).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
}
