package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/internal/team"
)

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

func confirmationForSessionSwitch(mode, agent string) *channelConfirm {
	mode = strings.TrimSpace(mode)
	agent = strings.TrimSpace(agent)
	var title, detail string
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
