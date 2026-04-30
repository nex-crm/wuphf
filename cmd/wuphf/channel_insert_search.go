package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

func (m channelModel) buildInsertPickerOptions() []tui.PickerOption {
	options := []tui.PickerOption{}

	for _, ch := range m.channels {
		if strings.TrimSpace(ch.Slug) == "" {
			continue
		}
		options = append(options, tui.PickerOption{
			Label:       "#" + ch.Slug,
			Value:       "#" + ch.Slug,
			Description: "Insert channel reference",
		})
	}

	for _, member := range channelui.MergeOfficeMembers(m.officeMembers, m.members, m.currentChannelInfo()) {
		if member.Slug == "you" || strings.TrimSpace(member.Slug) == "" {
			continue
		}
		options = append(options, tui.PickerOption{
			Label:       "@" + member.Slug,
			Value:       "@" + member.Slug + " ",
			Description: "Insert teammate mention",
		})
	}

	for _, task := range m.tasks {
		options = append(options, tui.PickerOption{
			Label:       "Task " + task.ID + " · " + channelui.TruncateText(task.Title, 48),
			Value:       fmt.Sprintf("[task %s] %s", task.ID, task.Title),
			Description: "Insert task reference",
		})
	}

	for _, req := range m.requests {
		options = append(options, tui.PickerOption{
			Label:       "Request " + req.ID + " · " + channelui.TruncateText(req.TitleOrQuestion(), 48),
			Value:       fmt.Sprintf("[request %s] %s", req.ID, req.TitleOrQuestion()),
			Description: "Insert request reference",
		})
	}

	for _, msg := range m.recentRootMessages(16) {
		options = append(options, tui.PickerOption{
			Label:       "Message " + msg.ID + " · @" + msg.From,
			Value:       fmt.Sprintf("[msg %s] @%s: %s", msg.ID, msg.From, channelui.TruncateText(msg.Content, 96)),
			Description: channelui.TruncateText(msg.Content, 56),
		})
	}

	return options
}

func (m channelModel) buildSearchPickerOptions() []tui.PickerOption {
	options := []tui.PickerOption{}
	seen := map[string]struct{}{}

	for _, opt := range m.buildWorkspaceSwitcherOptions() {
		if strings.TrimSpace(opt.Value) == "" {
			continue
		}
		if _, ok := seen[opt.Value]; ok {
			continue
		}
		seen[opt.Value] = struct{}{}
		options = append(options, opt)
	}

	for _, msg := range m.recentRootMessages(20) {
		valuePrefix := "message:"
		if channelui.HasThreadReplies(m.messages, msg.ID) || strings.TrimSpace(msg.ReplyTo) != "" {
			valuePrefix = "thread:"
		}
		value := valuePrefix + channelui.ThreadRootMessageID(m.messages, msg.ID)
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		options = append(options, tui.PickerOption{
			Label:       "Message " + msg.ID + " · @" + msg.From,
			Value:       value,
			Description: channelui.TruncateText(msg.Content, 64),
		})
	}

	return options
}

func (m channelModel) buildRecoveryPromptPickerOptions() []tui.PickerOption {
	options := []tui.PickerOption{}
	for _, msg := range m.recentRootMessages(16) {
		options = append(options, tui.PickerOption{
			Label:       "Since " + msg.ID + " · @" + msg.From,
			Value:       channelui.BuildRecoveryPromptForMessage(msg),
			Description: channelui.TruncateText(msg.Content, 64),
		})
	}
	for _, req := range m.requests {
		options = append(options, tui.PickerOption{
			Label:       "Pending request " + req.ID,
			Value:       channelui.BuildRecoveryPromptForRequest(req),
			Description: channelui.TruncateText(req.TitleOrQuestion(), 64),
		})
	}
	for _, task := range m.tasks {
		if strings.TrimSpace(task.Status) == "done" {
			continue
		}
		options = append(options, tui.PickerOption{
			Label:       "Task " + task.ID + " · " + channelui.TruncateText(task.Title, 48),
			Value:       channelui.BuildRecoveryPromptForTask(task),
			Description: channelui.TruncateText(task.Status, 32),
		})
	}
	return options
}

func (m *channelModel) insertIntoActiveComposer(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	insert := []rune(text)
	if m.focus == focusThread && m.threadPanelOpen {
		m.threadInput, m.threadInputPos = channelui.InsertComposerRunes(m.threadInput, m.threadInputPos, insert)
		m.threadInputHistory.ResetRecall()
		return
	}
	m.focus = focusMain
	m.input, m.inputPos = channelui.InsertComposerRunes(m.input, m.inputPos, insert)
	m.inputHistory.ResetRecall()
}

func (m *channelModel) applySearchSelection(value, label string) tea.Cmd {
	switch {
	case value == "mode:office" || strings.HasPrefix(value, "app:"):
		return m.applyWorkspaceSwitcherSelection(value)
	case strings.HasPrefix(value, "channel:"):
		channel := normalizeWorkspaceChannel(strings.TrimPrefix(value, "channel:"))
		if channel == "" {
			return nil
		}
		m.activeChannel = channel
		m.activeApp = channelui.OfficeAppMessages
		m.lastID = ""
		m.messages = nil
		m.members = nil
		m.requests = nil
		m.tasks = nil
		m.replyToID = ""
		m.threadPanelOpen = false
		m.threadPanelID = ""
		m.scroll = 0
		m.clearUnreadState()
		m.syncSidebarCursorToActive()
		m.notice = "Jumped to #" + channel
		return tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel))
	case strings.HasPrefix(value, "dm:"):
		agent := strings.TrimSpace(strings.TrimPrefix(value, "dm:"))
		if agent == "" {
			agent = team.DefaultOneOnOneAgent
		}
		m.confirm = confirmationForSessionSwitch(team.SessionModeOneOnOne, agent)
		m.notice = "Confirm the direct session switch."
		return nil
	case strings.HasPrefix(value, "task:"):
		taskID := strings.TrimSpace(strings.TrimPrefix(value, "task:"))
		task, ok := m.findTaskByID(taskID)
		if !ok {
			m.notice = "Task not found: " + taskID
			return nil
		}
		m.activeApp = channelui.OfficeAppTasks
		m.syncSidebarCursorToActive()
		if strings.TrimSpace(task.ThreadID) != "" {
			m.threadPanelOpen = true
			m.threadPanelID = task.ThreadID
			m.replyToID = task.ThreadID
		}
		m.notice = "Focused task " + task.ID
		return pollTasks(m.activeChannel)
	case strings.HasPrefix(value, "request:"):
		reqID := strings.TrimSpace(strings.TrimPrefix(value, "request:"))
		req, ok := m.findRequestByID(reqID)
		if !ok {
			m.notice = "Request not found: " + reqID
			return nil
		}
		next, cmd := m.focusRequest(req, "Focused request "+req.ID)
		if updated, ok := next.(channelModel); ok {
			*m = updated
		}
		return cmd
	case strings.HasPrefix(value, "thread:"):
		rootID := strings.TrimSpace(strings.TrimPrefix(value, "thread:"))
		if rootID == "" {
			return nil
		}
		m.activeApp = channelui.OfficeAppMessages
		m.threadPanelOpen = true
		m.threadPanelID = rootID
		m.replyToID = rootID
		m.focus = focusThread
		m.notice = "Opened thread " + rootID
		return pollBroker("", m.activeChannel)
	case strings.HasPrefix(value, "message:"):
		msgID := strings.TrimSpace(strings.TrimPrefix(value, "message:"))
		msg, ok := channelui.FindMessageByID(m.messages, msgID)
		if !ok {
			m.notice = "Message not found: " + msgID
			return nil
		}
		m.activeApp = channelui.OfficeAppMessages
		m.replyToID = msg.ID
		m.focus = focusMain
		m.notice = "Replying from message " + msg.ID
		return nil
	default:
		m.notice = "Unknown search target: " + label
		return nil
	}
}

func (m channelModel) recentRootMessages(limit int) []channelui.BrokerMessage {
	if limit <= 0 {
		limit = 16
	}
	out := make([]channelui.BrokerMessage, 0, limit)
	for i := len(m.messages) - 1; i >= 0 && len(out) < limit; i-- {
		msg := m.messages[i]
		if strings.TrimSpace(msg.ReplyTo) != "" {
			continue
		}
		out = append(out, msg)
	}
	return out
}
