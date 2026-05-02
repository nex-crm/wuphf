package main

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/tui"
)

// Main composer keymap. The tea.KeyMsg case body extracted from
// Update. Three layers, processed in order:
//
//  1. Global keys (always active): ctrl+c (double-tap to quit),
//     ctrl+b (sidebar collapse), ctrl+g/ctrl+o (quick-jump nav),
//     ctrl+d (DM → #general). The ctrl+c double-tap is the only one
//     that mutates m.lastCtrlCAt; every other branch resets it to
//     zero so a stray ctrl+c never accidentally quits.
//
//  2. Quick-jump consumption: 1-9 jumps to channel/app while
//     m.quickJumpTarget is set; esc cancels.
//
//  3. Esc: close overlays in priority order — confirm > picker >
//     autocomplete/mention > memberDraft > doctor > interview >
//     thread. If nothing's open, fire postHumanInterrupt to pause
//     the team.
//
//  4. Tab focus cycle (only when no overlay/picker is active).
//
//  5. Active-overlay routing: confirm / picker / initFlow /
//     autocomplete / mention each get the key first. Returns
//     immediately if any consume it.
//
//  6. Calendar app keys: d/w/f/a when the calendar is focused and
//     the composer is empty.
//
//  7. Focus-area dispatch: focusThread → updateThread,
//     focusSidebar → updateSidebar, focusMain → main composer
//     keymap (motion / enter / backspace / ctrl-* / arrows /
//     scroll / literal text insert).
//
// The thread keymap lives in channel_composer_input.go; the
// sidebar keymap in channel_sidebar_state.go.

func (m channelModel) handleKeyMsg(msg tea.KeyMsg) (channelModel, tea.Cmd) {
	// ── Global keys (always active) ───────────────────────────────
	key := msg.String()
	if msg.Type == tea.KeyCtrlJ {
		key = "ctrl+j"
	}
	switch key {
	case "ctrl+c":
		now := time.Now()
		if !m.lastCtrlCAt.IsZero() && now.Sub(m.lastCtrlCAt) <= 2*time.Second {
			killTeamSession()
			return m, tea.Quit
		}
		m.lastCtrlCAt = now
		m.setTransientNotice("Press Ctrl+C again to quit WUPHF. Toby will file the exit paperwork.")
		return m, nil
	case "ctrl+b":
		if m.isOneOnOne() {
			return m, nil
		}
		m.sidebarCollapsed = !m.sidebarCollapsed
		return m, nil
	case "ctrl+g":
		if m.isOneOnOne() {
			m.setTransientNotice("1:1 mode: no sidebar, no distractions, no Toby. Ideal.")
			return m, nil
		}
		if m.quickJumpTarget == quickJumpChannels {
			m.quickJumpTarget = quickJumpNone
		} else {
			m.quickJumpTarget = quickJumpChannels
			m.setTransientNotice("Quick nav: 1-9 switches channels. Faster than Dwight in a fire drill.")
		}
		return m, nil
	case "ctrl+o":
		if m.isOneOnOne() {
			m.setTransientNotice("1:1 mode: just the direct conversation. Like a conference room with no Toby.")
			return m, nil
		}
		if m.quickJumpTarget == quickJumpApps {
			m.quickJumpTarget = quickJumpNone
		} else {
			m.quickJumpTarget = quickJumpApps
			m.setTransientNotice("Quick nav: 1-9 switches apps. Even faster than Stanley doing the crossword.")
		}
		return m, nil
	case "ctrl+d":
		// Return to #general from a DM channel.
		if chInfo := m.findChannelInfo(m.activeChannel); chInfo != nil && chInfo.IsDM() {
			m.activeChannel = "general"
			m.lastID = ""
			m.messages = nil
			m.setTransientNotice("Back to #general — the heart of the office.")
			return m, pollBroker("", m.activeChannel)
		}
		return m, nil
	}

	if m.quickJumpTarget != quickJumpNone {
		target := m.quickJumpTarget
		items := m.quickJumpItems()
		switch msg.String() {
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			idx := int(msg.String()[0] - '1')
			m.quickJumpTarget = quickJumpNone
			if idx >= 0 && idx < len(items) {
				m.setSidebarCursorForItem(items[idx])
				return m, m.selectSidebarItem(items[idx])
			}
			if target == quickJumpChannels {
				m.setTransientNotice("No channel on that number. Even Michael checks the directory first.")
			} else {
				m.setTransientNotice("No app on that number. Try a different one — WUPHF believes in you.")
			}
			return m, nil
		case "esc":
			m.quickJumpTarget = quickJumpNone
		default:
			m.quickJumpTarget = quickJumpNone
		}
	}

	// ── Esc: close overlays/thread, then cycle ────────────────────
	if msg.String() == "esc" {
		switch m.activeInteractionContext() {
		case contextConfirm:
			if m.confirm != nil && m.confirm.Action == channelui.ChannelConfirmActionSubmitRequest {
				m.confirm = nil
				m.notice = "Review closed. Keep editing before you send."
				return m, nil
			}
			m.confirm = nil
			m.notice = "Canceled."
			return m, nil
		case contextPicker:
			m.picker.SetActive(false)
			if m.pickerMode == channelPickerIntegrations {
				m.notice = "Integration canceled."
			} else {
				m.initFlow = tui.NewInitFlow()
				m.notice = "Setup canceled. Come back when you're ready. That's what she said."
			}
			m.pickerMode = channelPickerNone
			return m, nil
		case contextAutocomplete, contextMention:
			var cmd tea.Cmd
			m.autocomplete, cmd = m.autocomplete.Update(msg)
			_ = cmd
			m.mention, _ = m.mention.Update(msg)
			return m, nil
		case contextMemberDraft:
			m.memberDraft = nil
			m.input = nil
			m.inputPos = 0
			m.notice = "Agent setup canceled."
			return m, nil
		case contextDoctor:
			m.doctor = nil
			m.notice = "Health check closed. The doctor says you're fine — or at least healthy enough to ship."
			return m, nil
		case contextInterview:
			req := *m.pending
			m.pending = nil
			m.input = nil
			m.inputPos = 0
			m.updateInputOverlays()
			m.posting = true
			m.notice = "Request canceled."
			return m, cancelRequest(req)
		case contextThread:
			m.threadPanelOpen = false
			m.threadPanelID = ""
			m.threadInput = nil
			m.threadInputPos = 0
			m.threadScroll = 0
			if m.focus == focusThread {
				m.focus = focusMain
			}
			return m, nil
		}
		// Nothing to close — fire human interrupt to pause the whole team
		if m.pending == nil {
			m.posting = true
			m.notice = "Pausing team..."
			return m, postHumanInterrupt(m.activeChannel)
		}
		return m, nil
	}

	// ── Tab: cycle focus 0→1→2→0 (only visible panels) ───────────
	if msg.String() == "tab" && !m.autocomplete.IsVisible() && !m.mention.IsVisible() && !m.picker.IsActive() {
		m.focus = m.nextFocus()
		m.quickJumpTarget = quickJumpNone
		m.updateOverlaysForCurrentInput()
		return m, nil
	}

	// ── Global overlays/pickers before panel-specific handling ────
	if m.confirm != nil {
		switch msg.String() {
		case "enter":
			next, cmd := m.executeConfirmation(*m.confirm)
			return next.(channelModel), cmd
		case "ctrl+c", "esc":
			m.confirm = nil
			m.notice = "Canceled."
			return m, nil
		default:
			return m, nil
		}
	}
	if m.picker.IsActive() {
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		return m, cmd
	}
	if m.initFlow.IsActive() && m.initFlow.Phase() == tui.InitAPIKey {
		var cmd tea.Cmd
		m.initFlow, cmd = m.initFlow.Update(msg)
		return m, cmd
	}
	if m.autocomplete.IsVisible() {
		switch msg.String() {
		case "tab":
			if name := m.autocomplete.Accept(); name != "" {
				m.setActiveInput("/" + name + " ")
			}
			return m, nil
		case "enter":
			if name := m.autocomplete.Accept(); name != "" {
				next, cmd := m.runActiveCommand("/" + name)
				return next.(channelModel), cmd
			}
		case "up", "down", "shift+tab":
			var cmd tea.Cmd
			m.autocomplete, cmd = m.autocomplete.Update(msg)
			_ = cmd
			return m, nil
		default:
			var cmd tea.Cmd
			m.autocomplete, cmd = m.autocomplete.Update(msg)
			_ = cmd
		}
	}
	if m.mention.IsVisible() {
		switch msg.String() {
		case "tab", "enter":
			if mention := m.mention.Accept(); mention != "" {
				m.insertAcceptedMention(mention)
			}
			return m, nil
		case "up", "down", "shift+tab":
			var cmd tea.Cmd
			m.mention, cmd = m.mention.Update(msg)
			_ = cmd
			return m, nil
		default:
			var cmd tea.Cmd
			m.mention, cmd = m.mention.Update(msg)
			_ = cmd
		}
	}

	if m.focus == focusMain && m.activeApp == channelui.OfficeAppCalendar && len(m.input) == 0 && !m.posting {
		switch msg.String() {
		case "d":
			m.calendarRange = channelui.CalendarRangeDay
			m.notice = "Calendar now shows today."
			return m, nil
		case "w":
			m.calendarRange = channelui.CalendarRangeWeek
			m.notice = "Calendar now shows this week."
			return m, nil
		case "f":
			options := m.buildCalendarAgentPickerOptions()
			if len(options) == 0 {
				m.notice = "No teammate filters available."
				return m, nil
			}
			m.picker = tui.NewPicker("Filter Calendar", options)
			m.picker.SetActive(true)
			m.pickerMode = channelPickerCalendarAgent
			return m, nil
		case "a":
			m.calendarFilter = ""
			m.notice = "Showing all teammate calendars."
			return m, nil
		}
	}

	// ── Route by focus area ───────────────────────────────────────
	if m.focus == focusThread && m.threadPanelOpen {
		next, cmd := m.updateThread(msg)
		return next.(channelModel), cmd
	}
	if m.focus == focusSidebar && !m.sidebarCollapsed {
		next, cmd := m.updateSidebar(msg)
		return next.(channelModel), cmd
	}

	// ── focusMain: existing behavior ──────────────────────────────
	if motionKey, ok := composerMotionKey(msg); ok {
		m.lastCtrlCAt = time.Time{}
		if nextPos, handled := channelui.MoveComposerCursor(m.input, m.inputPos, motionKey); handled {
			m.inputPos = nextPos
			m.updateInputOverlays()
		}
		return m, nil
	}
	switch msg.String() {
	case "enter":
		m.lastCtrlCAt = time.Time{}
		if m.memberDraft != nil {
			next, cmd := m.submitMemberDraft()
			return next.(channelModel), cmd
		}
		if len(m.input) > 0 {
			text := string(m.input)
			trimmed := strings.TrimSpace(text)
			m.inputHistory.Record(m.input, m.inputPos)
			if trimmed == "/quit" || trimmed == "/exit" || trimmed == "/q" {
				killTeamSession()
				return m, tea.Quit
			}
			if strings.HasPrefix(trimmed, "/") {
				next, cmd := m.runActiveCommand(trimmed)
				return next.(channelModel), cmd
			}
			if m.pending != nil {
				m.confirm = channelui.ConfirmationForInterviewAnswer(*m.pending, m.selectedInterviewOption(), text)
				m.notice = "Review your answer before sending."
				return m, nil
			}

			m.input = nil
			m.inputPos = 0
			m.updateInputOverlays()
			m.notice = ""
			m.posting = true
			return m, postToChannel(text, m.replyToID, m.activeChannel)
		}
		if m.pending != nil {
			if opt := m.selectedInterviewOption(); opt != nil {
				if channelui.InterviewOptionRequiresText(opt) {
					m.notice = channelui.InterviewOptionTextHint(opt)
					return m, nil
				}
				m.confirm = channelui.ConfirmationForInterviewAnswer(*m.pending, opt, "")
				m.notice = "Review your answer before sending."
				return m, nil
			}
			m.notice = "Choose an option or type your own answer before sending."
			return m, nil
		}
	case "backspace":
		m.lastCtrlCAt = time.Time{}
		if m.inputPos > 0 {
			m.inputHistory.ResetRecall()
			m.input = append(m.input[:m.inputPos-1], m.input[m.inputPos:]...)
			m.inputPos--
			m.updateInputOverlays()
		}
	case "ctrl+u":
		m.lastCtrlCAt = time.Time{}
		m.inputHistory.ResetRecall()
		m.input = nil
		m.inputPos = 0
		m.updateInputOverlays()
	case "ctrl+p":
		m.lastCtrlCAt = time.Time{}
		if snapshot, ok := m.inputHistory.Previous(m.input, m.inputPos); ok {
			m.restoreMainSnapshot(snapshot)
		}
	case "ctrl+n":
		m.lastCtrlCAt = time.Time{}
		if snapshot, ok := m.inputHistory.Next(); ok {
			m.restoreMainSnapshot(snapshot)
		}
	case "ctrl+a":
		m.lastCtrlCAt = time.Time{}
		m.inputPos = 0
		m.updateInputOverlays()
	case "ctrl+e":
		m.lastCtrlCAt = time.Time{}
		m.inputPos = len(m.input)
		m.updateInputOverlays()
	case "ctrl+j":
		m.lastCtrlCAt = time.Time{}
		m.inputHistory.ResetRecall()
		ch := []rune{'\n'}
		tail := make([]rune, len(m.input[m.inputPos:]))
		copy(tail, m.input[m.inputPos:])
		m.input = append(m.input[:m.inputPos], append(ch, tail...)...)
		m.inputPos++
		m.updateInputOverlays()
	case "left":
		m.lastCtrlCAt = time.Time{}
		if m.inputPos > 0 {
			m.inputPos--
			m.updateInputOverlays()
		}
	case "right":
		m.lastCtrlCAt = time.Time{}
		if m.inputPos < len(m.input) {
			m.inputPos++
			m.updateInputOverlays()
		}
	case "up":
		m.lastCtrlCAt = time.Time{}
		if m.pending != nil && m.selectedOption > 0 {
			m.selectedOption--
		} else {
			m.scroll++
		}
	case "down":
		m.lastCtrlCAt = time.Time{}
		if m.pending != nil && m.selectedOption < m.interviewOptionCount()-1 {
			m.selectedOption++
		} else {
			m.scroll--
			if m.scroll < 0 {
				m.scroll = 0
			}
		}
	case "home":
		m.lastCtrlCAt = time.Time{}
		m.scroll = 1 << 30
	case "end":
		m.lastCtrlCAt = time.Time{}
		m.scroll = 0
		m.clearUnreadState()
	case "pgup":
		m.lastCtrlCAt = time.Time{}
		m.scroll += channelui.MaxInt(10, m.height/2)
	case "pgdown":
		m.lastCtrlCAt = time.Time{}
		m.scroll -= channelui.MaxInt(10, m.height/2)
		if m.scroll < 0 {
			m.scroll = 0
		}
		if m.scroll == 0 {
			m.clearUnreadState()
		}
	default:
		m.lastCtrlCAt = time.Time{}
		if ch := composerInsertRunes(msg); len(ch) > 0 {
			m.inputHistory.ResetRecall()
			m.input, m.inputPos = channelui.InsertComposerRunes(m.input, m.inputPos, ch)
			m.updateInputOverlays()
		} else if len(msg.String()) == 1 || msg.Type == tea.KeyRunes {
			ch := msg.Runes
			if len(ch) == 0 {
				ch = []rune(msg.String())
			}
			if len(ch) > 0 {
				m.inputHistory.ResetRecall()
				tail := make([]rune, len(m.input[m.inputPos:]))
				copy(tail, m.input[m.inputPos:])
				m.input = append(m.input[:m.inputPos], append(ch, tail...)...)
				m.inputPos += len(ch)
				m.updateInputOverlays()
			}
		}
		if m.maybeActivateChannelPickerFromInput() {
			return m, nil
		}
	}
	return m, nil
}
