package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ── Approval decision types ──────────────────────────────────────────────

const (
	approvalDecisionApproved         = "approved"
	approvalDecisionApprovedWithNote = "approved_with_note"
	approvalDecisionRejected         = "rejected"
	approvalDecisionSteered          = "steered"
)

// approvalDecision captures the outcome of the approval workflow.
type approvalDecision struct {
	Type      string `json:"type"`
	Note      string `json:"note,omitempty"`
	Timestamp string `json:"timestamp"`
}

// ── Approval detection ───────────────────────────────────────────────────

// approvalKeywords are phrases that indicate an interview is an approval
// request rather than an information-gathering interview.
var approvalKeywords = []string{
	"approve", "permission", "proceed", "deploy", "confirm",
	"go ahead", "ship it", "sign off", "authorize", "authorise",
}

// isApprovalInterview returns true when the interview should be treated as
// an approval request rather than a regular information-gathering interview.
func isApprovalInterview(interview channelInterview) bool {
	kind := strings.TrimSpace(strings.ToLower(interview.Kind))
	if kind == "approval" || kind == "decision" {
		return true
	}
	q := strings.ToLower(interview.Question)
	for _, kw := range approvalKeywords {
		if strings.Contains(q, kw) {
			return true
		}
	}
	return false
}

// ── Approval workflow builder ────────────────────────────────────────────

// approvalWorkflowStep represents a single step in the approval workflow.
// This is a lightweight, TUI-only workflow model (no external runtime needed).
type approvalWorkflowStep struct {
	ID          string
	Type        string // "confirm" or "edit"
	Title       string
	Description string
	Actions     []approvalAction
}

// approvalAction is a user-facing action in a workflow step.
type approvalAction struct {
	Key   string // single key like "a", "r", "enter"
	Label string
	Next  string // step ID to jump to, or "" for submit
}

// approvalWorkflow is the full interactive approval workflow.
type approvalWorkflow struct {
	Steps     []approvalWorkflowStep
	Interview channelInterview
}

// buildApprovalWorkflow converts an approval-type interview into a rich
// workflow with approve/reject/steer options.
func buildApprovalWorkflow(interview channelInterview) approvalWorkflow {
	steps := []approvalWorkflowStep{
		{
			ID:          "context",
			Type:        "confirm",
			Title:       "Approval Required",
			Description: interview.Question,
			Actions: []approvalAction{
				{Key: "a", Label: "Approve", Next: "approve"},
				{Key: "r", Label: "Reject", Next: "reject"},
				{Key: "s", Label: "Steer", Next: "steer"},
				{Key: "q", Label: "Dismiss", Next: ""},
			},
		},
		{
			ID:          "approve",
			Type:        "confirm",
			Title:       "Approve",
			Description: "Approve. Add a note?",
			Actions: []approvalAction{
				{Key: "enter", Label: "Approve now", Next: ""},
				{Key: "n", Label: "Add note first", Next: "approve_note"},
			},
		},
		{
			ID:          "approve_note",
			Type:        "edit",
			Title:       "Approve with note",
			Description: "Add a note to your approval:",
		},
		{
			ID:          "reject",
			Type:        "edit",
			Title:       "Reject",
			Description: "Why are you rejecting? (required)",
		},
		{
			ID:          "steer",
			Type:        "edit",
			Title:       "Steer",
			Description: "What should be done differently?",
		},
	}

	return approvalWorkflow{
		Steps:     steps,
		Interview: interview,
	}
}

// resolveDecision converts the workflow result into a structured decision.
func resolveDecision(stepID string, text string) approvalDecision {
	now := time.Now().UTC().Format(time.RFC3339)
	switch stepID {
	case "approve":
		return approvalDecision{Type: approvalDecisionApproved, Timestamp: now}
	case "approve_note":
		return approvalDecision{Type: approvalDecisionApprovedWithNote, Note: text, Timestamp: now}
	case "reject":
		return approvalDecision{Type: approvalDecisionRejected, Note: text, Timestamp: now}
	case "steer":
		return approvalDecision{Type: approvalDecisionSteered, Note: text, Timestamp: now}
	default:
		return approvalDecision{Type: approvalDecisionApproved, Timestamp: now}
	}
}

// decisionChoiceID returns the choice_id string for the broker answer.
func decisionChoiceID(d approvalDecision) string {
	return d.Type
}

// decisionChoiceText returns a human-readable label for the decision.
func decisionChoiceText(d approvalDecision) string {
	switch d.Type {
	case approvalDecisionApproved:
		return "Approved"
	case approvalDecisionApprovedWithNote:
		return "Approved with note"
	case approvalDecisionRejected:
		return "Rejected"
	case approvalDecisionSteered:
		return "Approved with steering"
	default:
		return "Decided"
	}
}

// decisionCustomText returns the full answer text for the broker.
func decisionCustomText(d approvalDecision) string {
	label := decisionChoiceText(d)
	if d.Note != "" {
		return fmt.Sprintf("[%s] %s", label, d.Note)
	}
	return fmt.Sprintf("[%s]", label)
}

// ── Decision context card ────────────────────────────────────────────────

// renderApprovalContextCard renders a styled card for an approval-type
// interview before the user enters the workflow. Press Enter to start.
func renderApprovalContextCard(interview channelInterview, width int) string {
	if width < 40 {
		width = 40
	}

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))

	lines := []string{
		labelStyle.Render("Approval Required"),
		titleStyle.Render(fmt.Sprintf("From: @%s", interview.From)),
		"",
		textStyle.Width(width - 4).Render(interview.Question),
	}

	if ctx := strings.TrimSpace(interview.Context); ctx != "" {
		lines = append(lines, "")
		lines = append(lines, muted.Width(width-4).Render("Context: "+ctx))
	}

	lines = append(lines, "")
	lines = append(lines, muted.Render("[Enter] Review & decide"))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F59E0B")).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(lines, "\n")) + "\n"
}

// renderApprovalStepCard renders the current step of the approval workflow.
func renderApprovalStepCard(wf approvalWorkflow, stepIdx int, width int) string {
	if stepIdx < 0 || stepIdx >= len(wf.Steps) {
		return ""
	}
	if width < 40 {
		width = 40
	}

	step := wf.Steps[stepIdx]
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))
	accentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true)

	lines := []string{
		labelStyle.Render(step.Title),
		"",
		textStyle.Width(width - 4).Render(step.Description),
	}

	if step.Type == "confirm" && len(step.Actions) > 0 {
		lines = append(lines, "")
		for _, a := range step.Actions {
			lines = append(lines, accentStyle.Render(fmt.Sprintf("[%s]", a.Key))+" "+muted.Render(a.Label))
		}
	}

	if step.Type == "edit" {
		lines = append(lines, "")
		lines = append(lines, muted.Render("Type your response below and press Enter."))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F59E0B")).
		Padding(0, 1).
		Width(width).
		Render(strings.Join(lines, "\n")) + "\n"
}
