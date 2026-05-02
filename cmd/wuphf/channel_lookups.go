package main

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/tui"
)

// Lookup helpers + request-action dispatch. Pure ID-to-entity finds
// (findTaskByID, findRequestByID) plus the request-focus / answer-flow
// transitions (focusRequest, answerRequest) and the picker-opening
// helpers (openTaskActionPicker, openRequestActionPicker) that bridge
// the slash-command dispatcher to the action picker UI.

func (m channelModel) findTaskByID(id string) (channelui.Task, bool) {
	for _, task := range m.tasks {
		if task.ID == id {
			return task, true
		}
	}
	return channelui.Task{}, false
}

func (m channelModel) findRequestByID(id string) (channelui.Interview, bool) {
	for _, req := range m.requests {
		if req.ID == id {
			return req, true
		}
	}
	return channelui.Interview{}, false
}

func (m channelModel) focusRequest(req channelui.Interview, notice string) (channelModel, tea.Cmd) {
	if req.Blocking || req.Required {
		m.activeApp = channelui.OfficeAppMessages
	} else {
		m.activeApp = channelui.OfficeAppRequests
	}
	m.syncSidebarCursorToActive()
	m.pending = &req
	m.selectedOption = m.recommendedOptionIndex()
	m.notice = notice
	if req.ReplyTo != "" {
		m.threadPanelOpen = true
		m.threadPanelID = req.ReplyTo
	}
	return m, tea.Batch(pollRequests(m.activeChannel))
}

func (m channelModel) answerRequest(req channelui.Interview) (channelModel, tea.Cmd) {
	if req.Blocking || req.Required {
		m.activeApp = channelui.OfficeAppMessages
	} else {
		m.activeApp = channelui.OfficeAppRequests
	}
	m.syncSidebarCursorToActive()
	m.pending = &req
	m.selectedOption = m.recommendedOptionIndex()
	m.notice = "Answering request " + req.ID + ". Type your answer and press Enter."
	if req.ReplyTo != "" {
		m.threadPanelOpen = true
		m.threadPanelID = req.ReplyTo
	}
	return m, nil
}

func (m *channelModel) openTaskActionPicker(task channelui.Task) tea.Cmd {
	actions := m.buildTaskActionPickerOptions(task)
	if len(actions) == 0 {
		return nil
	}
	m.picker = tui.NewPicker("Task: "+channelui.TruncateText(task.Title, 40), actions)
	m.picker.SetActive(true)
	m.pickerMode = channelPickerTaskAction
	m.notice = "Choose a task action."
	return nil
}

func (m *channelModel) openRequestActionPicker(req channelui.Interview) tea.Cmd {
	actions := m.buildRequestActionPickerOptions(req)
	if len(actions) == 0 {
		return nil
	}
	m.picker = tui.NewPicker("Request: "+channelui.TruncateText(req.TitleOrQuestion(), 40), actions)
	m.picker.SetActive(true)
	m.pickerMode = channelPickerRequestAction
	m.notice = "Choose a request action."
	return nil
}
