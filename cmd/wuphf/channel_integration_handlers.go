package main

import (
	"fmt"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

// Update() handlers for the four telegram/openclaw integration
// done-msg types. Pair with channel_integration.go which holds the
// outbound flow helpers (startTelegramConnect, discoverTelegramGroups,
// fetchOpenclawSessions, etc.). The handlers here are the response
// side: each picks up the result of a discover/connect call and
// either advances the picker into the next step or surfaces the
// success/error in m.notice.

func (m channelModel) handleTelegramDiscoverMsg(msg telegramDiscoverMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		m.notice = "Telegram error: " + msg.err.Error()
		return m, nil
	}
	m.telegramToken = msg.token

	// Merge discovered groups with existing manifest channels
	allGroups := msg.groups
	manifest, _ := company.LoadManifest()
	for _, ch := range manifest.Channels {
		if ch.Surface == nil || ch.Surface.Provider != "telegram" || ch.Surface.RemoteID == "" || ch.Surface.RemoteID == "0" {
			continue
		}
		// Check if already discovered
		found := false
		for _, g := range allGroups {
			if fmt.Sprintf("%d", g.ChatID) == ch.Surface.RemoteID {
				found = true
				break
			}
		}
		if !found {
			chatID, _ := strconv.ParseInt(ch.Surface.RemoteID, 10, 64)
			if chatID != 0 {
				title := ch.Surface.RemoteTitle
				if title == "" {
					title = ch.Name
				}
				allGroups = append(allGroups, team.TelegramGroup{
					ChatID: chatID,
					Title:  title,
					Type:   "group",
				})
			}
		}
	}
	m.telegramGroups = allGroups

	// Build picker: DM + discovered groups + manual group entry
	options := []tui.PickerOption{
		{Label: "Direct message with Telegram bot", Value: "dm", Description: "Anyone can DM the bot to reach the office"},
	}
	for _, g := range allGroups {
		options = append(options, tui.PickerOption{
			Label:       g.Title,
			Value:       fmt.Sprintf("%d", g.ChatID),
			Description: fmt.Sprintf("Shared %s channel", g.Type),
		})
	}
	if len(allGroups) == 0 {
		options = append(options, tui.PickerOption{
			Label:       "Waiting for groups...",
			Value:       "retry",
			Description: "Add the bot to a Telegram group and send a message, then try again",
		})
	}
	m.picker = tui.NewPicker(fmt.Sprintf("Bot \"%s\" verified. Choose how to connect:", msg.botName), options)
	m.picker.SetActive(true)
	m.pickerMode = channelPickerTelegramGroup
	return m, nil
}

func (m channelModel) handleOpenclawSessionsMsg(msg openclawSessionsMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		options := []tui.PickerOption{
			{Label: "Retry with different gateway URL", Value: "retry-url", Description: "Go back and change the URL/token"},
		}
		m.picker = tui.NewPicker(fmt.Sprintf("OpenClaw dial failed: %s", msg.err.Error()), options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerOpenclawSession
		m.notice = "OpenClaw connect failed: " + msg.err.Error()
		return m, nil
	}
	m.openclawSessions = msg.sessions
	if len(msg.sessions) == 0 {
		m.notice = "OpenClaw gateway returned no sessions. Start one in OpenClaw and retry /connect openclaw."
		return m, nil
	}
	options := make([]tui.PickerOption, 0, len(msg.sessions))
	for _, s := range msg.sessions {
		label := s.Label
		if label == "" {
			label = s.SessionKey
		}
		desc := s.Preview
		options = append(options, tui.PickerOption{
			Label:       label,
			Value:       s.SessionKey,
			Description: desc,
		})
	}
	m.picker = tui.NewPicker("Pick an OpenClaw session to bridge:", options)
	m.picker.SetActive(true)
	m.pickerMode = channelPickerOpenclawSession
	m.notice = fmt.Sprintf("Found %d OpenClaw session(s). Pick one to bridge.", len(msg.sessions))
	return m, nil
}

func (m channelModel) handleOpenclawConnectDoneMsg(msg openclawConnectDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		m.notice = "OpenClaw connect failed: " + msg.err.Error()
		return m, nil
	}
	m.notice = fmt.Sprintf("@%s is now in the office", msg.slug)
	return m, nil
}

func (m channelModel) handleTelegramConnectDoneMsg(msg telegramConnectDoneMsg) (channelModel, tea.Cmd) {
	m.posting = false
	if msg.err != nil {
		m.notice = "Telegram connect failed: " + msg.err.Error()
		return m, nil
	}
	m.notice = fmt.Sprintf("Connected \"%s\" as #%s. Restart WUPHF to activate the Telegram bridge.", msg.groupTitle, msg.channelSlug)
	m.activeChannel = msg.channelSlug
	m.activeApp = channelui.OfficeAppMessages
	m.messages = nil
	m.members = nil
	m.tasks = nil
	m.requests = nil
	m.lastID = ""
	m.replyToID = ""
	m.threadPanelOpen = false
	m.threadPanelID = ""
	m.scroll = 0
	m.clearUnreadState()
	m.syncSidebarCursorToActive()
	manifest, _ := company.LoadManifest()
	m.channels = channelui.ChannelInfosFromManifest(manifest)
	return m, tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollChannels())
}
