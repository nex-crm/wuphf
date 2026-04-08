package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/team"
)

type channelConfirmAction string

const (
	confirmActionResetTeam     channelConfirmAction = "reset_team"
	confirmActionResetDM       channelConfirmAction = "reset_dm"
	confirmActionSwitchMode    channelConfirmAction = "switch_mode"
	confirmActionRecoverFocus  channelConfirmAction = "recover_focus"
	confirmActionSubmitRequest channelConfirmAction = "submit_request"
)

type channelConfirm struct {
	Title        string
	Detail       string
	ConfirmLabel string
	CancelLabel  string
	Action       channelConfirmAction
	SessionMode  string
	Agent        string
	Channel      string
	Request      *channelInterview
	ChoiceID     string
	ChoiceText   string
	CustomText   string
}

func (m channelModel) confirmationForReset() *channelConfirm {
	title := "Reset Office Session"
	detail := "This clears the live office transcript and refreshes all team panes in place."
	if m.isOneOnOne() {
		title = "Reset Direct Session"
		detail = fmt.Sprintf("This clears the direct transcript with %s and reloads the direct pane in place.", m.oneOnOneAgentName())
	}
	return &channelConfirm{
		Title:        title,
		Detail:       detail,
		ConfirmLabel: "Enter reset now",
		CancelLabel:  "Esc keep working",
		Action:       confirmActionResetTeam,
		SessionMode:  m.sessionMode,
		Agent:        m.oneOnOneAgent,
	}
}

func confirmationForResetDM(agent, channel string) *channelConfirm {
	return &channelConfirm{
		Title:        "Clear Direct Messages",
		Detail:       fmt.Sprintf("This deletes the saved direct transcript with %s for this session.", displayName(agent)),
		ConfirmLabel: "Enter clear DMs",
		CancelLabel:  "Esc keep transcript",
		Action:       confirmActionResetDM,
		Agent:        agent,
		Channel:      channel,
	}
}

func confirmationForSessionSwitch(mode, agent string) *channelConfirm {
	mode = strings.TrimSpace(mode)
	agent = strings.TrimSpace(agent)
	title := "Switch Session Mode"
	detail := "This changes how the office routes the live session."
	if team.NormalizeSessionMode(mode) == team.SessionModeOneOnOne {
		name := displayName(agent)
		if agent == "" {
			name = displayName(team.DefaultOneOnOneAgent)
		}
		title = "Enter Direct Session"
		detail = fmt.Sprintf("This leaves the shared office view and zooms into a direct session with %s.", name)
	} else {
		title = "Return To Main Office"
		detail = "This exits direct mode and restores the shared office session."
	}
	return &channelConfirm{
		Title:        title,
		Detail:       detail,
		ConfirmLabel: "Enter switch now",
		CancelLabel:  "Esc stay here",
		Action:       confirmActionSwitchMode,
		SessionMode:  mode,
		Agent:        agent,
	}
}

func confirmationForInterviewAnswer(interview channelInterview, option *channelInterviewOption, customText string) *channelConfirm {
	title := interviewReviewTitle(interview)
	detailLines := []string{
		fmt.Sprintf("Question: %s", strings.TrimSpace(interview.Question)),
	}
	if label := interviewOptionDisplayLabel(option); label != "" {
		detailLines = append(detailLines, fmt.Sprintf("Decision: %s", label))
	}
	if outcome := interviewOptionOutcome(interview, option); outcome != "" {
		detailLines = append(detailLines, fmt.Sprintf("Outcome: %s", outcome))
	}
	customText = strings.TrimSpace(customText)
	if customText != "" {
		detailLines = append(detailLines, fmt.Sprintf("%s: %s", interviewCustomTextLabel(option), customText))
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
	return &channelConfirm{
		Title:        title,
		Detail:       strings.Join(detailLines, "\n\n"),
		ConfirmLabel: "Enter send answer",
		CancelLabel:  "Esc keep editing",
		Action:       confirmActionSubmitRequest,
		Request:      &interview,
		ChoiceID:     choiceID,
		ChoiceText:   choiceText,
		CustomText:   customText,
	}
}

func renderConfirmCard(confirm channelConfirm, width int) string {
	cardWidth := maxInt(48, width)
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).Render(confirm.Title)
	body := lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")).Width(cardWidth - 4).Render(confirm.Detail)
	footer := mutedText(confirm.ConfirmLabel + "  ·  " + confirm.CancelLabel)
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

func (m channelModel) executeConfirmation(confirm channelConfirm) (tea.Model, tea.Cmd) {
	switch confirm.Action {
	case confirmActionResetTeam:
		m.confirm = nil
		m.notice = ""
		m.posting = true
		return m, resetTeamSession(m.isOneOnOne())
	case confirmActionResetDM:
		m.confirm = nil
		m.posting = true
		return m, resetDMSession(confirm.Agent, confirm.Channel)
	case confirmActionSwitchMode:
		m.confirm = nil
		m.posting = true
		return m, switchSessionMode(confirm.SessionMode, confirm.Agent)
	case confirmActionSubmitRequest:
		m.confirm = nil
		m.notice = ""
		m.posting = true
		if confirm.Request == nil {
			m.posting = false
			m.notice = "No request selected."
			return m, nil
		}
		return m, postInterviewAnswer(*confirm.Request, confirm.ChoiceID, confirm.ChoiceText, confirm.CustomText)
	default:
		m.confirm = nil
		return m, nil
	}
}
