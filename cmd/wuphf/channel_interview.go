package main

import "strings"

func (m channelModel) currentInterviewPhase() channelInterviewPhase {
	if m.pending == nil {
		return ""
	}
	if m.confirm != nil && m.confirm.Action == confirmActionSubmitRequest {
		return interviewPhaseReview
	}
	if strings.TrimSpace(string(m.input)) != "" {
		return interviewPhaseDraft
	}
	if interviewOptionRequiresText(m.selectedInterviewOption()) {
		return interviewPhaseDraft
	}
	return interviewPhaseChoose
}

func (m channelModel) interviewPhaseTitle() string {
	switch m.currentInterviewPhase() {
	case interviewPhaseReview:
		return "Step 3 of 3 · review"
	case interviewPhaseDraft:
		return "Step 2 of 3 · draft"
	default:
		return "Step 1 of 3 · choose"
	}
}

func (m channelModel) interviewStatusLine() string {
	switch m.currentInterviewPhase() {
	case interviewPhaseReview:
		return " Request review │ Enter submit │ Esc revise"
	case interviewPhaseDraft:
		return " Request draft │ type answer │ Enter review"
	default:
		return " Request choose │ ↑/↓ select │ Enter continue"
	}
}
