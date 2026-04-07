package tui

import "strings"

// InteractionContext identifies which surface currently owns the operator's
// attention. The channel model can derive this from its active overlays.
type InteractionContext string

const (
	ContextCompose         InteractionContext = "compose"
	ContextDirectCompose   InteractionContext = "direct_compose"
	ContextReplyCompose    InteractionContext = "reply_compose"
	ContextThreadCompose   InteractionContext = "thread_compose"
	ContextInterview       InteractionContext = "interview"
	ContextInterviewReview InteractionContext = "interview_review"
	ContextAutocomplete    InteractionContext = "autocomplete"
	ContextMention         InteractionContext = "mention"
	ContextPicker          InteractionContext = "picker"
	ContextDoctor          InteractionContext = "doctor"
	ContextContinue        InteractionContext = "continue"
)

type ComposerHintState struct {
	Context          InteractionContext
	HistoryAvailable bool
}

// ComposerHint returns truthful, mode-aware operator guidance for the active
// composer context instead of one global static footer string.
func ComposerHint(state ComposerHintState) string {
	parts := make([]string, 0, 7)
	switch state.Context {
	case ContextAutocomplete:
		parts = append(parts, "↑/↓ choose", "Tab complete", "Enter run", "Esc close")
	case ContextMention:
		parts = append(parts, "↑/↓ choose", "Tab insert", "Enter insert", "Esc close")
	case ContextPicker:
		parts = append(parts, "↑/↓ choose", "Enter confirm", "Esc close")
	case ContextDoctor:
		parts = append(parts, "Esc close")
	case ContextInterview:
		parts = append(parts, "↑/↓ pick option", "Enter continue", "type note or answer")
	case ContextInterviewReview:
		parts = append(parts, "Enter submit", "Esc revise")
	case ContextReplyCompose:
		parts = append(parts, "/ commands", "@ mention", "Ctrl+J newline", "Enter send reply")
	case ContextThreadCompose:
		parts = append(parts, "/ commands", "Ctrl+J newline", "Enter send reply")
	case ContextDirectCompose:
		parts = append(parts, "/ commands", "@ mention", "Ctrl+J newline", "Enter send direct")
	case ContextContinue:
		parts = append(parts, "Enter continue", "Esc cancel")
	default:
		parts = append(parts, "/ commands", "@ mention", "Ctrl+J newline", "Enter send")
	}
	if state.HistoryAvailable && allowsHistory(state.Context) {
		parts = append(parts, "Ctrl+P recall", "Ctrl+N restore")
	}
	if dismissesWithEsc(state.Context) {
		parts = append(parts, "Esc close")
	} else {
		parts = append(parts, "Esc pause all")
	}
	return strings.Join(parts, " · ")
}

func allowsHistory(ctx InteractionContext) bool {
	switch ctx {
	case ContextCompose, ContextDirectCompose, ContextReplyCompose, ContextThreadCompose, ContextInterview, ContextInterviewReview:
		return true
	default:
		return false
	}
}

func dismissesWithEsc(ctx InteractionContext) bool {
	switch ctx {
	case ContextAutocomplete, ContextMention, ContextPicker, ContextDoctor, ContextContinue:
		return true
	default:
		return false
	}
}

type ContinuePrompt struct {
	Title       string
	Description string
	ContinueKey string
	CancelKey   string
}

// InlineHint returns a compact byline that can be reused by confirmation and
// pending-state cards.
func (p ContinuePrompt) InlineHint() string {
	continueKey := strings.TrimSpace(p.ContinueKey)
	if continueKey == "" {
		continueKey = "Enter"
	}
	cancelKey := strings.TrimSpace(p.CancelKey)
	if cancelKey == "" {
		cancelKey = "Esc"
	}
	parts := []string{}
	if strings.TrimSpace(p.Title) != "" {
		parts = append(parts, p.Title)
	}
	if strings.TrimSpace(p.Description) != "" {
		parts = append(parts, p.Description)
	}
	parts = append(parts, continueKey+" continue", cancelKey+" cancel")
	return strings.Join(parts, " · ")
}
