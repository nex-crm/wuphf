package channelui

import "strings"

// InterviewPhase identifies which step of the request-answer flow the
// composer is in: choose an option, draft a free-text answer, or
// review the about-to-be-submitted answer. Empty string means there
// is no active interview.
type InterviewPhase string

const (
	InterviewPhaseChoose InterviewPhase = "choose"
	InterviewPhaseDraft  InterviewPhase = "draft"
	InterviewPhaseReview InterviewPhase = "review"
)

// InterviewOptionRequiresText reports whether the option mandates a
// free-text answer alongside the choice. True when option.RequiresText
// is set, or when the option ID looks like a "note" / "steer" prompt
// (which historically required text but were not flagged explicitly).
// Returns false for nil.
func InterviewOptionRequiresText(option *InterviewOption) bool {
	if option == nil {
		return false
	}
	if option.RequiresText {
		return true
	}
	id := strings.TrimSpace(strings.ToLower(option.ID))
	return strings.Contains(id, "note") || strings.Contains(id, "steer")
}

// InterviewOptionTextHint returns the hint string the composer shows
// for an option's free-text input — the option's TextHint when set,
// otherwise a generic prompt when the option requires text, otherwise
// "".
func InterviewOptionTextHint(option *InterviewOption) string {
	if option == nil {
		return ""
	}
	if strings.TrimSpace(option.TextHint) != "" {
		return option.TextHint
	}
	if InterviewOptionRequiresText(option) {
		return "Type your note, rationale, or steering before submitting this choice."
	}
	return ""
}

// SelectedInterviewOption returns a pointer to options[index], or nil
// when index is out of range. The returned pointer is to a copy of
// the option so callers cannot mutate the underlying slice via it.
func SelectedInterviewOption(options []InterviewOption, index int) *InterviewOption {
	if index < 0 || index >= len(options) {
		return nil
	}
	option := options[index]
	return &option
}
