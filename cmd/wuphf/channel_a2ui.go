package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nex-crm/wuphf/internal/tui"
)

// WorkflowSpec defines an embeddable workflow that agents post in messages.
type WorkflowSpec struct {
	ID    string         `json:"id"`
	Title string         `json:"title"`
	Steps []WorkflowStep `json:"steps"`
}

// WorkflowStep is a single step in an embedded workflow.
type WorkflowStep struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

// embeddedBlock represents a detected fenced block in message content.
type embeddedBlock struct {
	Kind    string // "a2ui" or "workflow"
	JSON    string // raw JSON payload
	StartOK int    // byte offset of fence start in original content
	EndOK   int    // byte offset of fence end in original content
}

// messageA2UIState tracks which rendered messages have interactive A2UI
// content and enables focused navigation.
type messageA2UIState struct {
	// indices into the rendered message list that contain A2UI content
	interactiveIndices []int
	// current focus position within interactiveIndices (-1 = none)
	focusPos int
}

func newMessageA2UIState() messageA2UIState {
	return messageA2UIState{focusPos: -1}
}

// setInteractiveMessages replaces the set of message indices that have A2UI.
func (s *messageA2UIState) setInteractiveMessages(indices []int) {
	s.interactiveIndices = indices
	if len(indices) == 0 {
		s.focusPos = -1
	} else if s.focusPos >= len(indices) {
		s.focusPos = len(indices) - 1
	}
}

// focusNext moves focus to the next interactive message.
func (s *messageA2UIState) focusNext() {
	if len(s.interactiveIndices) == 0 {
		return
	}
	s.focusPos++
	if s.focusPos >= len(s.interactiveIndices) {
		s.focusPos = 0
	}
}

// focusPrev moves focus to the previous interactive message.
func (s *messageA2UIState) focusPrev() {
	if len(s.interactiveIndices) == 0 {
		return
	}
	s.focusPos--
	if s.focusPos < 0 {
		s.focusPos = len(s.interactiveIndices) - 1
	}
}

// focusedIndex returns the message index that currently has focus, or -1.
func (s *messageA2UIState) focusedIndex() int {
	if s.focusPos < 0 || s.focusPos >= len(s.interactiveIndices) {
		return -1
	}
	return s.interactiveIndices[s.focusPos]
}

// ---------------------------------------------------------------------------
// Block detection
// ---------------------------------------------------------------------------

var (
	fenceA2UIRe    = regexp.MustCompile("(?s)```a2ui\\s*\n(.*?)```")
	fenceWorkflowRe = regexp.MustCompile("(?s)```workflow\\s*\n(.*?)```")
)

// detectA2UIBlocks finds all ```a2ui fenced blocks in content and returns
// the parsed blocks plus the content with fences removed.
func detectA2UIBlocks(content string) ([]embeddedBlock, string) {
	return detectFencedBlocks(content, fenceA2UIRe, "a2ui")
}

// detectWorkflowBlocks finds all ```workflow fenced blocks in content and
// returns the parsed blocks plus the content with fences removed.
func detectWorkflowBlocks(content string) ([]embeddedBlock, string) {
	return detectFencedBlocks(content, fenceWorkflowRe, "workflow")
}

func detectFencedBlocks(content string, re *regexp.Regexp, kind string) ([]embeddedBlock, string) {
	matches := re.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil, content
	}

	var blocks []embeddedBlock
	var textParts []string
	lastEnd := 0

	for _, match := range matches {
		if match[0] > lastEnd {
			textParts = append(textParts, content[lastEnd:match[0]])
		}
		jsonStr := content[match[2]:match[3]]
		blocks = append(blocks, embeddedBlock{
			Kind:    kind,
			JSON:    jsonStr,
			StartOK: match[0],
			EndOK:   match[1],
		})
		lastEnd = match[1]
	}
	if lastEnd < len(content) {
		textParts = append(textParts, content[lastEnd:])
	}

	remaining := strings.TrimSpace(strings.Join(textParts, "\n"))
	return blocks, remaining
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// parseWorkflowSpec parses raw JSON into a WorkflowSpec.
func parseWorkflowSpec(raw string) (*WorkflowSpec, error) {
	var spec WorkflowSpec
	if err := json.Unmarshal([]byte(raw), &spec); err != nil {
		return nil, err
	}
	if spec.ID == "" {
		return nil, fmt.Errorf("workflow spec missing id")
	}
	if spec.Title == "" {
		return nil, fmt.Errorf("workflow spec missing title")
	}
	return &spec, nil
}

// parseA2UIComponent parses raw JSON into an A2UIComponent.
func parseA2UIComponent(raw string) (*tui.A2UIComponent, error) {
	var comp tui.A2UIComponent
	if err := json.Unmarshal([]byte(raw), &comp); err != nil {
		return nil, err
	}
	if !isA2UIType(comp.Type) {
		return nil, fmt.Errorf("unknown A2UI type %q", comp.Type)
	}
	return &comp, nil
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// renderEmbeddedWorkflowCard renders a card showing a workflow spec with
// title, step count, and an action hint.
func renderEmbeddedWorkflowCard(spec *WorkflowSpec, width int, focused bool) string {
	if width < 20 {
		width = 20
	}

	stepCount := len(spec.Steps)
	stepLabel := fmt.Sprintf("%d step", stepCount)
	if stepCount != 1 {
		stepLabel += "s"
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(tui.NexPurple))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tui.MutedColor))
	actionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tui.NexGreen)).Bold(true)

	title := titleStyle.Render("⚡ " + spec.Title)
	meta := mutedStyle.Render("workflow · " + stepLabel)
	action := actionStyle.Render("[Enter] Run")

	body := title + "\n" + meta + "\n" + action

	borderColor := "#374151"
	if focused {
		borderColor = tui.NexPurple
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Padding(0, 1).
		Width(max(4, width-2))

	typeLabel := mutedStyle.Render("workflow")
	return typeLabel + "\n" + cardStyle.Render(body)
}

// renderA2UIComponentLabeled renders an A2UI component with a dimmed type
// label above it.
func renderA2UIComponentLabeled(comp *tui.A2UIComponent, width int) string {
	gm := tui.NewGenerativeModel()
	gm.SetWidth(width)
	gm.SetSchema(*comp)
	if err := gm.Validate(); err != nil {
		return ""
	}

	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(tui.MutedColor))
	typeLabel := mutedStyle.Render(comp.Type)
	rendered := gm.View()
	if rendered == "" {
		return ""
	}
	return typeLabel + "\n" + rendered
}

// ---------------------------------------------------------------------------
// Combined message processing
// ---------------------------------------------------------------------------

// processedMessageContent is the result of extracting and rendering all
// embedded blocks from a message.
type processedMessageContent struct {
	TextPart       string   // remaining prose text
	RenderedBlocks []string // rendered A2UI + workflow blocks
	HasInteractive bool     // true if any workflow blocks were found
	WorkflowSpecs  []*WorkflowSpec
}

// processMessageA2UI extracts all fenced blocks (a2ui and workflow) from
// message content, renders them, and returns the pieces.
func processMessageA2UI(content string, width int) processedMessageContent {
	result := processedMessageContent{}

	// First pass: extract workflow blocks
	workflowBlocks, remaining := detectWorkflowBlocks(content)
	for _, block := range workflowBlocks {
		spec, err := parseWorkflowSpec(block.JSON)
		if err != nil {
			// Invalid workflow JSON -- leave as text
			remaining += "\n```workflow\n" + block.JSON + "\n```"
			continue
		}
		result.WorkflowSpecs = append(result.WorkflowSpecs, spec)
		result.RenderedBlocks = append(result.RenderedBlocks, renderEmbeddedWorkflowCard(spec, width, false))
		result.HasInteractive = true
	}

	// Second pass: extract a2ui blocks from whatever remains
	a2uiBlocks, remaining := detectA2UIBlocks(remaining)
	for _, block := range a2uiBlocks {
		comp, err := parseA2UIComponent(block.JSON)
		if err != nil {
			remaining += "\n```a2ui\n" + block.JSON + "\n```"
			continue
		}
		rendered := renderA2UIComponentLabeled(comp, width)
		if rendered != "" {
			result.RenderedBlocks = append(result.RenderedBlocks, rendered)
		}
	}

	// Also try bare inline JSON (existing behavior compatibility)
	if len(a2uiBlocks) == 0 && len(workflowBlocks) == 0 {
		if idx := strings.Index(remaining, `{"type":"`); idx >= 0 {
			jsonStr, endIdx := extractJSONObject(remaining, idx)
			if jsonStr != "" {
				comp, err := parseA2UIComponent(jsonStr)
				if err == nil {
					rendered := renderA2UIComponentLabeled(comp, width)
					if rendered != "" {
						result.RenderedBlocks = append(result.RenderedBlocks, rendered)
						parts := []string{strings.TrimSpace(remaining[:idx]), strings.TrimSpace(remaining[endIdx:])}
						remaining = strings.TrimSpace(strings.Join(parts, "\n"))
					}
				}
			}
		}
	}

	result.TextPart = strings.TrimSpace(remaining)
	return result
}
