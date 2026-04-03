package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nex-crm/wuphf/internal/tui"
)

// WorkflowView is a bubbletea model that renders and drives a workflow.
// It owns the Runtime, renders the current step using A2UI components,
// shows action hints, handles key dispatch, and runs actions async.
//
//	┌─ Workflow Header ────────────────────────────┐
//	│  Title                          Step N        │
//	│  ─────────────────────────────────────────── │
//	│  ┌─ Step Content ──────────────────────────┐ │
//	│  │  (A2UI rendered component)              │ │
//	│  └─────────────────────────────────────────┘ │
//	│  [a] Approve  [r] Reject  [Esc] Cancel       │
//	│  ● Running  |  N actions  |  0 errors        │
//	└──────────────────────────────────────────────┘
type WorkflowView struct {
	runtime      *Runtime
	gen          tui.GenerativeModel
	spinnerFrame int
	width        int
	height       int
	stepNum      int
	err          error
	quitting     bool
	confirmAbort bool
}

// spinnerFrames are the dots-style spinner characters.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// spinnerTickMsg triggers the next spinner frame.
type spinnerTickMsg struct{}

// Styles for the workflow TUI (from design review spec).
var (
	wfBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#374151")).
			Padding(0, 1)

	wfTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(tui.NexPurple))

	wfActionHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color(tui.MutedColor))

	wfErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ef4444")).
			Bold(true)

	wfSuccessStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#22c55e")).
			Bold(true)

	wfDryRunStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#eab308")).
			Bold(true)

	wfStatusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color(tui.MutedColor))
)

// actionResultMsg is sent when an async action completes.
type actionResultMsg struct {
	result map[string]any
	err    error
}

// NewWorkflowView creates a view for an interactive workflow.
func NewWorkflowView(rt *Runtime, width, height int) WorkflowView {
	gen := tui.NewGenerativeModel()
	gen.SetWidth(clampWidth(width))

	return WorkflowView{
		runtime: rt,
		gen:     gen,
		width:   width,
		height:  height,
		stepNum: 0,
	}
}

func (v WorkflowView) spinnerView() string {
	frame := spinnerFrames[v.spinnerFrame%len(spinnerFrames)]
	return lipgloss.NewStyle().Foreground(lipgloss.Color(tui.NexPurple)).Render(frame)
}

func tickSpinner() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// Init starts the spinner and the workflow.
func (v WorkflowView) Init() tea.Cmd {
	return tea.Batch(tickSpinner(), v.startWorkflow())
}

func (v WorkflowView) startWorkflow() tea.Cmd {
	return func() tea.Msg {
		if err := v.runtime.Start(); err != nil {
			return actionResultMsg{err: err}
		}
		return actionResultMsg{}
	}
}

// Update handles key events and async results.
func (v WorkflowView) Update(msg tea.Msg) (WorkflowView, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return v.handleKey(msg)

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		v.gen.SetWidth(clampWidth(msg.Width))
		return v, nil

	case actionResultMsg:
		return v.handleActionResult(msg)

	case spinnerTickMsg:
		v.spinnerFrame++
		state := v.runtime.State()
		if state == StatePending || state == StateExecutingAction {
			return v, tickSpinner()
		}
		return v, nil
	}
	return v, nil
}

func (v WorkflowView) handleKey(msg tea.KeyMsg) (WorkflowView, tea.Cmd) {
	key := msg.String()

	// Abort confirmation.
	if v.confirmAbort {
		switch key {
		case "y", "Y":
			_ = v.runtime.Abort()
			v.quitting = true
			return v, nil
		default:
			v.confirmAbort = false
			return v, nil
		}
	}

	state := v.runtime.State()

	// Esc handling: smart confirmation.
	if key == "esc" {
		if state == StateDone || state == StateAborted {
			v.quitting = true
			return v, nil
		}
		if v.runtime.NeedsAbortConfirmation() {
			v.confirmAbort = true
			return v, nil
		}
		_ = v.runtime.Abort()
		v.quitting = true
		return v, nil
	}

	// Help overlay.
	if key == "?" {
		// TODO: show keybinding help overlay
		return v, nil
	}

	// Only handle keys when awaiting input.
	if state != StateAwaitingInput {
		return v, nil
	}

	// List navigation for select steps.
	step := v.runtime.CurrentStep()
	if step != nil && step.Type == StepSelect {
		switch key {
		case "up", "k":
			v.gen.MoveSelection(-1)
			return v, nil
		case "down", "j":
			v.gen.MoveSelection(1)
			return v, nil
		}
	}

	// Action dispatch.
	transition, exec, err := v.runtime.HandleAction(key)
	if err != nil {
		// Unknown key, ignore.
		return v, nil
	}

	v.stepNum++

	if exec != nil {
		// Async action execution.
		return v, v.executeAction(*exec)
	}

	// Direct transition (no side-effect).
	if transition != "" {
		if transition == TransitionDone {
			return v, nil
		}
		if err := v.runtime.Transition(transition); err != nil {
			v.err = err
		}
		v.syncGenModel()
	}
	return v, nil
}

func (v WorkflowView) executeAction(exec ExecuteSpec) tea.Cmd {
	rt := v.runtime
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		provider := rt.ActionProviderFor(exec.Provider)
		if provider == nil {
			return actionResultMsg{err: fmt.Errorf("no provider for %q", exec.Provider)}
		}
		result, err := provider.Execute(ctx, exec, rt.DataStore())
		return actionResultMsg{result: result, err: err}
	}
}

func (v WorkflowView) handleActionResult(msg actionResultMsg) (WorkflowView, tea.Cmd) {
	v.runtime.CompleteAction(msg.result, msg.err)
	v.syncGenModel()

	// If runtime auto-transitioned (submit/run steps), check for more auto-steps.
	step := v.runtime.CurrentStep()
	if step != nil && v.runtime.State() == StateActive {
		if step.Type == StepSubmit || step.Type == StepRun {
			if step.Execute != nil {
				return v, v.executeAction(*step.Execute)
			}
		}
	}
	return v, nil
}

// syncGenModel updates the GenerativeModel to match the current step.
func (v *WorkflowView) syncGenModel() {
	step := v.runtime.CurrentStep()
	if step == nil {
		return
	}
	v.gen.SetData(v.runtime.DataStore())
	if step.Display != nil {
		v.gen.SetSchema(tui.A2UIComponent{
			Type:    step.Display.Component,
			Props:   step.Display.Props,
			DataRef: step.Display.DataRef,
		})
	}
	// Set up interaction state.
	actions := make([]tui.ComponentAction, 0, len(step.Actions))
	for _, a := range step.Actions {
		actions = append(actions, tui.ComponentAction{
			Key:        a.Key,
			Label:      a.Label,
			Transition: a.Transition,
		})
	}
	// Add Esc hint.
	actions = append(actions, tui.ComponentAction{Key: "Esc", Label: "Cancel"})
	v.gen.SetInteractive(actions)

	// Set item count for select steps.
	if step.Type == StepSelect && step.DataRef != "" {
		if items, ok := v.runtime.DataStore()[strings.TrimPrefix(step.DataRef, "/")].([]any); ok {
			v.gen.SetItemCount(len(items))
		}
	}
}

// View renders the workflow TUI.
func (v WorkflowView) View() string {
	if v.quitting {
		return ""
	}

	w := clampWidth(v.width)
	state := v.runtime.State()

	var sections []string

	// Header.
	spec := v.runtime.Spec()
	title := wfTitleStyle.Render(spec.Title)
	if spec.DryRun {
		title = wfDryRunStyle.Render("⚡ DRY RUN") + "  " + title
	}
	step := v.runtime.CurrentStep()
	stepLabel := ""
	if step != nil {
		stepLabel = fmt.Sprintf("Step: %s", step.ID)
	}
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		title,
		strings.Repeat(" ", max(1, w-lipgloss.Width(title)-lipgloss.Width(stepLabel)-4)),
		wfStatusStyle.Render(stepLabel),
	)
	sections = append(sections, header)
	sections = append(sections, strings.Repeat("─", min(w-2, 60)))

	// Confirm abort dialog.
	if v.confirmAbort {
		sections = append(sections, "")
		sections = append(sections, wfErrorStyle.Render("Abort workflow? Progress will be saved."))
		sections = append(sections, "[y] Yes  [any key] No")
		return wfBorderStyle.Width(w - 2).Render(strings.Join(sections, "\n"))
	}

	// Step content.
	switch state {
	case StatePending:
		sections = append(sections, v.spinnerView()+" Starting workflow...")

	case StateActive, StateAwaitingInput:
		if step != nil {
			if step.Prompt != "" {
				sections = append(sections, "")
				sections = append(sections, step.Prompt)
			}
			// Render A2UI component.
			if step.Display != nil {
				sections = append(sections, "")
				sections = append(sections, v.gen.View())
			}
		}

	case StateExecutingAction:
		sections = append(sections, "")
		sections = append(sections, v.spinnerView()+" Executing action...")

	case StateError:
		sections = append(sections, "")
		if lastErr := v.runtime.LastError(); lastErr != nil {
			sections = append(sections, wfErrorStyle.Render("✗ "+lastErr.Error()))
		}
		sections = append(sections, "")
		sections = append(sections, "[r] Retry  [s] Skip  [Esc] Cancel")

	case StateDone:
		sections = append(sections, "")
		sections = append(sections, wfSuccessStyle.Render("✓ Workflow complete"))
		sections = append(sections, "")
		sections = append(sections, fmt.Sprintf("  %d steps completed", len(v.runtime.stepHistory)))
		sections = append(sections, "")
		sections = append(sections, "[Enter] Return  [s] Save as skill")

	case StateAborted:
		sections = append(sections, "")
		sections = append(sections, wfStatusStyle.Render("Workflow aborted."))
		sections = append(sections, "[Enter] Return")
	}

	// Action hints (when awaiting input).
	if state == StateAwaitingInput && v.gen.Interactive() {
		sections = append(sections, "")
		sections = append(sections, wfActionHintStyle.Render(v.gen.RenderActionHints()))
	}

	// Status bar.
	history := v.runtime.StepHistory()
	completed := len(history)
	errors := 0
	for _, e := range history {
		if e.Error != "" {
			errors++
		}
	}
	status := fmt.Sprintf("● %s  |  %d completed  |  %d errors", state, completed, errors)
	sections = append(sections, "")
	sections = append(sections, wfStatusStyle.Render(status))

	return wfBorderStyle.Width(w - 2).Render(strings.Join(sections, "\n"))
}

// Quitting returns true when the workflow view should be dismissed.
func (v WorkflowView) Quitting() bool {
	return v.quitting
}

// Runtime returns the underlying runtime for inspection.
func (v WorkflowView) Runtime() *Runtime {
	return v.runtime
}

func clampWidth(w int) int {
	if w <= 0 {
		return 80
	}
	if w > 100 {
		return 100
	}
	return w
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
