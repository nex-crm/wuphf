package main

import (
	"strings"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

func (m channelModel) currentInterviewPhase() channelui.InterviewPhase {
	if m.pending == nil {
		return ""
	}
	if m.confirm != nil && m.confirm.Action == channelui.ChannelConfirmActionSubmitRequest {
		return channelui.InterviewPhaseReview
	}
	if strings.TrimSpace(string(m.input)) != "" {
		return channelui.InterviewPhaseDraft
	}
	if channelui.InterviewOptionRequiresText(m.selectedInterviewOption()) {
		return channelui.InterviewPhaseDraft
	}
	return channelui.InterviewPhaseChoose
}

func (m channelModel) interviewPhaseTitle() string {
	switch m.currentInterviewPhase() {
	case channelui.InterviewPhaseReview:
		return "Step 3 of 3 · review"
	case channelui.InterviewPhaseDraft:
		return "Step 2 of 3 · draft"
	default:
		return "Step 1 of 3 · choose"
	}
}

func (m channelModel) interviewStatusLine() string {
	switch m.currentInterviewPhase() {
	case channelui.InterviewPhaseReview:
		return " Request review │ Enter submit │ Esc revise"
	case channelui.InterviewPhaseDraft:
		return " Request draft │ type answer │ Enter review"
	default:
		return " Request choose │ ↑/↓ select │ Enter continue"
	}
}
