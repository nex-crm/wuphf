package main

import (
	"fmt"

	"github.com/nex-crm/wuphf/internal/tui"
)

type channelInteractionContext string

const (
	contextConfirm      channelInteractionContext = "confirm"
	contextPicker       channelInteractionContext = "picker"
	contextAutocomplete channelInteractionContext = "autocomplete"
	contextMention      channelInteractionContext = "mention"
	contextMemberDraft  channelInteractionContext = "member_draft"
	contextDoctor       channelInteractionContext = "doctor"
	contextInterview    channelInteractionContext = "interview"
	contextThread       channelInteractionContext = "thread"
	contextSidebar      channelInteractionContext = "sidebar"
	contextMain         channelInteractionContext = "main"
)

func (m channelModel) activeInteractionContext() channelInteractionContext {
	switch {
	case m.confirm != nil:
		return contextConfirm
	case m.picker.IsActive() || m.initFlow.IsActive():
		return contextPicker
	case m.autocomplete.IsVisible():
		return contextAutocomplete
	case m.mention.IsVisible():
		return contextMention
	case m.memberDraft != nil:
		return contextMemberDraft
	case m.doctor != nil:
		return contextDoctor
	case m.pending != nil && !m.posting:
		return contextInterview
	case m.focus == focusThread && m.threadPanelOpen:
		return contextThread
	case m.focus == focusSidebar && !m.sidebarCollapsed:
		return contextSidebar
	default:
		return contextMain
	}
}

func (m channelModel) composerHint(channelName, replyToID string, pending *channelInterview) string {
	switch m.activeInteractionContext() {
	case contextConfirm:
		if m.confirm != nil && m.confirm.Action == confirmActionSubmitRequest {
			return tui.ComposerHint(tui.ComposerHintState{
				Context:          tui.ContextInterviewReview,
				HistoryAvailable: len(m.inputHistory.entries) > 0,
			})
		}
		return tui.ContinuePrompt{
			Title:       "Review change",
			Description: "Confirm the disruptive action before WUPHF changes runtime state",
		}.InlineHint()
	case contextMemberDraft:
		return "Enter save teammate · Ctrl+J newline · Esc cancel editor"
	case contextInterview:
		return tui.ComposerHint(tui.ComposerHintState{
			Context:          tui.ContextInterview,
			HistoryAvailable: len(m.inputHistory.entries) > 0,
		})
	case contextThread:
		return tui.ComposerHint(tui.ComposerHintState{
			Context:          tui.ContextThreadCompose,
			HistoryAvailable: len(m.threadInputHistory.entries) > 0,
		})
	case contextMain:
		if replyToID != "" {
			return fmt.Sprintf("%s · /cancel leave thread %s", tui.ComposerHint(tui.ComposerHintState{
				Context:          tui.ContextReplyCompose,
				HistoryAvailable: len(m.inputHistory.entries) > 0,
			}), replyToID)
		}
		if stringsHasOneOnOnePrefix(channelName) {
			return tui.ComposerHint(tui.ComposerHintState{
				Context:          tui.ContextDirectCompose,
				HistoryAvailable: len(m.inputHistory.entries) > 0,
			})
		}
		return tui.ComposerHint(tui.ComposerHintState{
			Context:          tui.ContextCompose,
			HistoryAvailable: len(m.inputHistory.entries) > 0,
		})
	case contextAutocomplete:
		return tui.ComposerHint(tui.ComposerHintState{
			Context:          tui.ContextAutocomplete,
			HistoryAvailable: len(m.inputHistory.entries) > 0,
		})
	case contextMention:
		return tui.ComposerHint(tui.ComposerHintState{
			Context:          tui.ContextMention,
			HistoryAvailable: len(m.inputHistory.entries) > 0,
		})
	case contextPicker:
		return tui.ComposerHint(tui.ComposerHintState{
			Context:          tui.ContextPicker,
			HistoryAvailable: len(m.inputHistory.entries) > 0,
		})
	case contextDoctor:
		return tui.ComposerHint(tui.ComposerHintState{
			Context:          tui.ContextDoctor,
			HistoryAvailable: len(m.inputHistory.entries) > 0,
		})
	default:
		if stringsHasOneOnOnePrefix(channelName) {
			return tui.ComposerHint(tui.ComposerHintState{
				Context:          tui.ContextDirectCompose,
				HistoryAvailable: len(m.inputHistory.entries) > 0,
			})
		}
		return tui.ComposerHint(tui.ComposerHintState{
			Context:          tui.ContextCompose,
			HistoryAvailable: len(m.inputHistory.entries) > 0,
		})
	}
}

func stringsHasOneOnOnePrefix(channelName string) bool {
	return len(channelName) >= 4 && channelName[:4] == "1:1 "
}
