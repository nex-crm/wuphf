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

func normalizeInterviewKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func normalizeInterviewOptionID(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}

func humanizeInterviewOptionID(id string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(id), func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

func interviewOptionDisplayLabel(option *channelInterviewOption) string {
	if option == nil {
		return ""
	}
	if label := strings.TrimSpace(option.Label); label != "" {
		return label
	}
	return humanizeInterviewOptionID(option.ID)
}

func (req channelInterview) optionByID(id string) *channelInterviewOption {
	id = normalizeInterviewOptionID(id)
	if id == "" {
		return nil
	}
	for i := range req.Options {
		if normalizeInterviewOptionID(req.Options[i].ID) == id {
			return &req.Options[i]
		}
	}
	return nil
}

func (req channelInterview) recommendedOptionLabel() string {
	if option := req.optionByID(req.RecommendedID); option != nil {
		return interviewOptionDisplayLabel(option)
	}
	return humanizeInterviewOptionID(req.RecommendedID)
}

func interviewReviewTitle(interview channelInterview) string {
	switch normalizeInterviewKind(interview.Kind) {
	case "approval":
		return "Review Approval Decision"
	case "confirm":
		return "Review Confirmation"
	case "choice":
		return "Review Direction"
	case "secret":
		return "Review Private Reply"
	case "interview":
		return "Review Human Answer"
	default:
		return "Review Request Response"
	}
}

func interviewOptionOutcome(interview channelInterview, option *channelInterviewOption) string {
	switch normalizeInterviewOptionID(optionID(option)) {
	case "approve":
		return "The team will proceed immediately."
	case "approve_with_note":
		return "The team will proceed, but your note becomes explicit guardrails."
	case "needs_more_info", "need_more_context":
		return "The office will gather the missing context before coming back."
	case "reject":
		return "The proposed work will stop here."
	case "reject_with_steer":
		return "The team will not proceed as proposed and will redirect around your steering."
	case "confirm_proceed":
		return "The office will proceed as planned."
	case "adjust":
		return "The office will revise the plan before proceeding."
	case "reassign":
		return "The office will reroute ownership or scope before proceeding."
	case "hold":
		return "The office will keep this paused for later review."
	case "answer_directly":
		return "Your answer will go directly back to the team."
	case "give_direction":
		return "The team will proceed using your direction."
	case "delegate":
		return "The office will route this to the owner you name."
	case "proceed":
		return "The team will proceed using its best judgment."
	}

	switch normalizeInterviewKind(interview.Kind) {
	case "approval":
		return "Your decision will either unblock or redirect the proposed work."
	case "confirm":
		return "Your answer will confirm, hold, or adjust the office plan."
	case "choice":
		return "Your answer will set the office direction."
	default:
		return "Your answer will be sent back to the team."
	}
}

func interviewCustomTextLabel(option *channelInterviewOption) string {
	switch normalizeInterviewOptionID(optionID(option)) {
	case "approve_with_note":
		return "Guardrails"
	case "reject_with_steer":
		return "Steering"
	case "adjust":
		return "Required changes"
	case "reassign", "delegate":
		return "Routing"
	case "needs_more_info", "need_more_context":
		return "Missing context"
	case "give_direction":
		return "Direction"
	}
	if interviewOptionRequiresText(option) {
		return "Note"
	}
	return "Answer"
}

func interviewOptionDraftBadge(option *channelInterviewOption) string {
	switch normalizeInterviewOptionID(optionID(option)) {
	case "approve_with_note", "give_direction":
		return "add guidance"
	case "reject_with_steer", "delegate", "reassign":
		return "add steering"
	case "adjust":
		return "add changes"
	case "needs_more_info", "need_more_context":
		return "add context gap"
	default:
		if interviewOptionRequiresText(option) {
			return "needs note"
		}
		return ""
	}
}

func answerRequestNotice(req channelInterview) string {
	switch normalizeInterviewKind(req.Kind) {
	case "approval":
		return "Answering approval " + req.ID + ". Pick a decision or type steering, then press Enter."
	case "confirm":
		return "Answering confirmation " + req.ID + ". Confirm, adjust, or hold before sending."
	case "choice":
		return "Answering decision " + req.ID + ". Choose a direction or add guidance, then press Enter."
	case "interview":
		return "Answering request " + req.ID + ". Type your answer and press Enter."
	default:
		return "Answering request " + req.ID + ". Pick an option or type your answer, then press Enter."
	}
}

func optionID(option *channelInterviewOption) string {
	if option == nil {
		return ""
	}
	return option.ID
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
		return " Request choose │ ↑/↓ select │ Enter review"
	}
}
