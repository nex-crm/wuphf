package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// interviewWorkflowStepKind identifies the type of input a workflow step collects.
type interviewWorkflowStepKind string

const (
	stepKindSelect  interviewWorkflowStepKind = "select"
	stepKindEdit    interviewWorkflowStepKind = "edit"
	stepKindConfirm interviewWorkflowStepKind = "confirm"
)

// interviewWorkflowStep is a single step in the interview workflow mini-flow.
type interviewWorkflowStep struct {
	ID          string
	Kind        interviewWorkflowStepKind
	Title       string
	Description string
	Options     []interviewWorkflowOption // populated for select steps
}

// interviewWorkflowOption is a selectable choice within a select step.
type interviewWorkflowOption struct {
	ID          string
	Label       string
	Description string
	Recommended bool
}

// interviewWorkflowSpec is the full workflow derived from a pending interview.
type interviewWorkflowSpec struct {
	InterviewID string
	From        string
	Title       string
	Context     string
	Blocking    bool
	Required    bool
	Steps       []interviewWorkflowStep
}

// interviewWorkflowState tracks the runtime state of a running interview workflow.
type interviewWorkflowState struct {
	Spec         interviewWorkflowSpec
	CurrentStep  int
	SelectedIdx  int // cursor within current select step
	EditBuffer   []rune
	EditPos      int
	Data         map[string]string // step ID -> collected value
	DataOptionID string            // the chosen option ID (for select steps)
	Completed    bool
	Cancelled    bool
}

// interviewToWorkflow converts a pending channelInterview into an interviewWorkflowSpec.
func interviewToWorkflow(interview channelInterview) interviewWorkflowSpec {
	spec := interviewWorkflowSpec{
		InterviewID: interview.ID,
		From:        interview.From,
		Title:       interview.Title,
		Context:     interview.Context,
		Blocking:    interview.Blocking,
		Required:    interview.Required,
		Steps:       make([]interviewWorkflowStep, 0, 3),
	}

	if len(interview.Options) > 0 {
		// Build select step with options
		opts := make([]interviewWorkflowOption, 0, len(interview.Options))
		// If there's a recommended option, put it first
		for _, o := range interview.Options {
			opts = append(opts, interviewWorkflowOption{
				ID:          o.ID,
				Label:       o.Label,
				Description: o.Description,
				Recommended: o.ID == interview.RecommendedID,
			})
		}
		spec.Steps = append(spec.Steps, interviewWorkflowStep{
			ID:          "answer",
			Kind:        stepKindSelect,
			Title:       interview.Question,
			Description: interview.Context,
			Options:     opts,
		})
	} else {
		// Free-text edit step
		spec.Steps = append(spec.Steps, interviewWorkflowStep{
			ID:          "answer",
			Kind:        stepKindEdit,
			Title:       interview.Question,
			Description: interview.Context,
		})
	}

	// Confirmation step
	spec.Steps = append(spec.Steps, interviewWorkflowStep{
		ID:    "confirm",
		Kind:  stepKindConfirm,
		Title: "Review and submit",
	})

	return spec
}

// newInterviewWorkflowState creates a ready-to-run workflow state from a spec.
func newInterviewWorkflowState(spec interviewWorkflowSpec) interviewWorkflowState {
	state := interviewWorkflowState{
		Spec: spec,
		Data: make(map[string]string),
	}
	// Pre-select the recommended option if this is a select step
	if len(spec.Steps) > 0 && spec.Steps[0].Kind == stepKindSelect {
		for i, opt := range spec.Steps[0].Options {
			if opt.Recommended {
				state.SelectedIdx = i
				break
			}
		}
	}
	return state
}

// currentStep returns the current step spec, or nil if completed.
func (s *interviewWorkflowState) currentStep() *interviewWorkflowStep {
	if s.CurrentStep >= len(s.Spec.Steps) {
		return nil
	}
	return &s.Spec.Steps[s.CurrentStep]
}

// stepCount returns the total number of steps.
func (s *interviewWorkflowState) stepCount() int {
	return len(s.Spec.Steps)
}

// advance moves to the next step, collecting the current step's data.
// Returns true if the workflow completed.
func (s *interviewWorkflowState) advance() bool {
	step := s.currentStep()
	if step == nil {
		return true
	}

	switch step.Kind {
	case stepKindSelect:
		if s.SelectedIdx >= 0 && s.SelectedIdx < len(step.Options) {
			opt := step.Options[s.SelectedIdx]
			s.Data[step.ID] = opt.Label
			s.DataOptionID = opt.ID
		}
	case stepKindEdit:
		s.Data[step.ID] = string(s.EditBuffer)
	case stepKindConfirm:
		// Nothing to collect
	}

	s.CurrentStep++
	if s.CurrentStep >= len(s.Spec.Steps) {
		s.Completed = true
		return true
	}

	// Reset input state for the new step
	s.SelectedIdx = 0
	s.EditBuffer = nil
	s.EditPos = 0
	return false
}

// goBack moves to the previous step. Returns false if already at step 0.
func (s *interviewWorkflowState) goBack() bool {
	if s.CurrentStep <= 0 {
		return false
	}
	s.CurrentStep--
	// Restore edit state from collected data if re-visiting an edit step
	step := s.currentStep()
	if step != nil && step.Kind == stepKindEdit {
		if prev, ok := s.Data[step.ID]; ok {
			s.EditBuffer = []rune(prev)
			s.EditPos = len(s.EditBuffer)
		}
	}
	return true
}

// answerChoiceID returns the selected option ID (for select workflows).
func (s *interviewWorkflowState) answerChoiceID() string {
	return s.DataOptionID
}

// answerChoiceText returns the selected option label (for select workflows).
func (s *interviewWorkflowState) answerChoiceText() string {
	return s.Data["answer"]
}

// answerCustomText returns the free-text answer (for edit workflows).
func (s *interviewWorkflowState) answerCustomText() string {
	if s.DataOptionID != "" {
		return "" // Was a select, not custom text
	}
	return s.Data["answer"]
}

// renderInterviewWorkflow renders the interview workflow card with progress,
// current step, and navigation hints.
func renderInterviewWorkflow(state interviewWorkflowState, width int) string {
	if width < 40 {
		width = 40
	}
	cardWidth := width

	// Styles
	accentColor := "#F59E0B"
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(accentColor)).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true)

	step := state.currentStep()
	if step == nil {
		return ""
	}

	var lines []string

	// ── Header with blocking/required badges ────────────────────────
	headerBits := []string{labelStyle.Render("Human Interview")}
	if state.Spec.Blocking {
		headerBits = append(headerBits, accentPill("blocking", "#B45309"))
	}
	if state.Spec.Required {
		headerBits = append(headerBits, accentPill("required", "#B91C1C"))
	}
	lines = append(lines, strings.Join(headerBits, "  "))

	// ── From / Title ────────────────────────────────────────────────
	fromLine := fmt.Sprintf("@%s needs your decision", state.Spec.From)
	if state.Spec.Title != "" {
		fromLine = state.Spec.Title + " · @" + state.Spec.From
	}
	lines = append(lines, titleStyle.Render(fromLine))

	// ── Progress indicator ──────────────────────────────────────────
	progress := fmt.Sprintf("Step %d of %d", state.CurrentStep+1, state.stepCount())
	progressBar := renderStepProgress(state.CurrentStep, state.stepCount(), cardWidth-4)
	lines = append(lines, "", muted.Render(progress))
	lines = append(lines, progressBar)

	// ── Context (if present and on the first step) ──────────────────
	if state.Spec.Context != "" && state.CurrentStep == 0 {
		lines = append(lines, "")
		lines = append(lines, muted.Width(cardWidth-4).Render(state.Spec.Context))
	}

	// ── Step content ────────────────────────────────────────────────
	lines = append(lines, "")
	lines = append(lines, titleStyle.Render(step.Title))

	switch step.Kind {
	case stepKindSelect:
		lines = append(lines, "")
		for i, opt := range step.Options {
			prefix := "  "
			style := textStyle
			if i == state.SelectedIdx {
				prefix = selectedStyle.Render("→ ")
				style = titleStyle
			}
			label := opt.Label
			if opt.Recommended {
				label += muted.Render(" (recommended)")
			}
			lines = append(lines, prefix+style.Render(label))
			if opt.Description != "" {
				lines = append(lines, "    "+muted.Width(cardWidth-8).Render(opt.Description))
			}
		}

	case stepKindEdit:
		lines = append(lines, "")
		cursorStyle := lipgloss.NewStyle().Reverse(true)
		var inputStr string
		if len(state.EditBuffer) == 0 {
			inputStr = cursorStyle.Render(" ") + muted.Render(" Type your answer...")
		} else {
			before := string(state.EditBuffer[:state.EditPos])
			var cursor, after string
			if state.EditPos < len(state.EditBuffer) {
				cursor = cursorStyle.Render(string(state.EditBuffer[state.EditPos]))
				after = string(state.EditBuffer[state.EditPos+1:])
			} else {
				cursor = cursorStyle.Render(" ")
			}
			inputStr = before + cursor + after
		}
		innerW := cardWidth - 8
		if innerW < 20 {
			innerW = 20
		}
		inputBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#334155")).
			Padding(0, 1).
			Width(innerW).
			Render(inputStr)
		lines = append(lines, "  "+inputBox)

	case stepKindConfirm:
		lines = append(lines, "")
		answer := state.Data["answer"]
		if answer == "" {
			answer = "(no answer)"
		}
		lines = append(lines, "  "+muted.Render("Your answer:")+"  "+textStyle.Render(answer))
	}

	// ── Navigation hints ────────────────────────────────────────────
	lines = append(lines, "")
	var hints []string
	if state.CurrentStep > 0 {
		hints = append(hints, "Backspace: back")
	}
	if step.Kind == stepKindSelect {
		hints = append(hints, "↑/↓: choose")
	}
	hints = append(hints, "Enter: continue")
	hints = append(hints, "Esc: cancel")
	lines = append(lines, muted.Render(strings.Join(hints, " · ")))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(accentColor)).
		Padding(0, 1).
		Width(cardWidth).
		Render(strings.Join(lines, "\n")) + "\n"
}

// renderStepProgress renders a visual step progress bar like ●──●──○──○
func renderStepProgress(current, total, width int) string {
	if total <= 0 {
		return ""
	}
	doneStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	activeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true)
	todoStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#475569"))
	lineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#334155"))

	var parts []string
	for i := 0; i < total; i++ {
		if i > 0 {
			parts = append(parts, lineStyle.Render("──"))
		}
		switch {
		case i < current:
			parts = append(parts, doneStyle.Render("●"))
		case i == current:
			parts = append(parts, activeStyle.Render("◉"))
		default:
			parts = append(parts, todoStyle.Render("○"))
		}
	}
	return strings.Join(parts, "")
}
