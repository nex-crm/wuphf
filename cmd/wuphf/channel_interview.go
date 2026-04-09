package main

import "strings"

type channelInterviewPhase string

const (
	interviewPhaseChoose channelInterviewPhase = "choose"
	interviewPhaseDraft  channelInterviewPhase = "draft"
	interviewPhaseReview channelInterviewPhase = "review"
)

func interviewOptionRequiresText(option *channelInterviewOption) bool {
	if option == nil {
		return false
	}
	if option.RequiresText {
		return true
	}
	id := strings.TrimSpace(strings.ToLower(option.ID))
	return strings.Contains(id, "note") || strings.Contains(id, "steer")
}

func interviewOptionTextHint(option *channelInterviewOption) string {
	if option == nil {
		return ""
	}
	if strings.TrimSpace(option.TextHint) != "" {
		return option.TextHint
	}
	if interviewOptionRequiresText(option) {
		return "Type your note, rationale, or steering before submitting this choice."
	}
	return ""
}

func selectedInterviewOption(options []channelInterviewOption, index int) *channelInterviewOption {
	if index < 0 || index >= len(options) {
		return nil
	}
	option := options[index]
	return &option
}

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
