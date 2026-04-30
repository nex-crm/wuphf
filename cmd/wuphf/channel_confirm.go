package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/team"
)

func (m channelModel) confirmationForReset() *channelui.ChannelConfirm {
	title := "Reset Office Session"
	detail := "This clears the live office transcript and refreshes all team panes in place."
	if m.isOneOnOne() {
		title = "Reset Direct Session"
		detail = fmt.Sprintf("This clears the direct transcript with %s and reloads the direct pane in place.", m.oneOnOneAgentName())
	}
	return &channelui.ChannelConfirm{
		Title:        title,
		Detail:       detail,
		ConfirmLabel: "Enter reset now",
		CancelLabel:  "Esc keep working",
		Action:       channelui.ChannelConfirmActionResetTeam,
		SessionMode:  m.sessionMode,
		Agent:        m.oneOnOneAgent,
	}
}

func confirmationForSessionSwitch(mode, agent string) *channelui.ChannelConfirm {
	mode = strings.TrimSpace(mode)
	agent = strings.TrimSpace(agent)
	var title, detail string
	if team.NormalizeSessionMode(mode) == team.SessionModeOneOnOne {
		name := channelui.DisplayName(agent)
		if agent == "" {
			name = channelui.DisplayName(team.DefaultOneOnOneAgent)
		}
		title = "Enter Direct Session"
		detail = fmt.Sprintf("This leaves the shared office view and zooms into a direct session with %s.", name)
	} else {
		title = "Return To Main Office"
		detail = "This exits direct mode and restores the shared office session."
	}
	return &channelui.ChannelConfirm{
		Title:        title,
		Detail:       detail,
		ConfirmLabel: "Enter switch now",
		CancelLabel:  "Esc stay here",
		Action:       channelui.ChannelConfirmActionSwitchMode,
		SessionMode:  mode,
		Agent:        agent,
	}
}

func (m channelModel) executeConfirmation(confirm channelui.ChannelConfirm) (tea.Model, tea.Cmd) {
	switch confirm.Action {
	case channelui.ChannelConfirmActionResetTeam:
		m.confirm = nil
		m.notice = ""
		m.posting = true
		return m, resetTeamSession(m.isOneOnOne())
	case channelui.ChannelConfirmActionResetDM:
		m.confirm = nil
		m.posting = true
		return m, resetDMSession(confirm.Agent, confirm.Channel)
	case channelui.ChannelConfirmActionSwitchMode:
		m.confirm = nil
		m.posting = true
		return m, switchSessionMode(confirm.SessionMode, confirm.Agent)
	case channelui.ChannelConfirmActionSubmitRequest:
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
