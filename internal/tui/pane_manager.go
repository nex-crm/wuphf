package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// PaneInterface abstracts a terminal pane so PaneManager can be built
// and tested independently of the concrete TerminalPane implementation.
type PaneInterface interface {
	Slug() string
	Name() string
	View() string
	SendKey(data []byte)
	IsAlive() bool
	IsFocused() bool
	SetFocused(bool)
	Resize(w, h int)
	Close()
}

// PaneManager manages multiple panes with a leader-focused layout and focus routing.
type PaneManager struct {
	panes         []PaneInterface
	paneMap       map[string]PaneInterface
	focusedIdx    int
	layout        string // "leader"
	width         int
	height        int
	broadcastMode bool
}

// NewPaneManager creates a PaneManager with leader layout.
func NewPaneManager() *PaneManager {
	return &PaneManager{
		paneMap: make(map[string]PaneInterface),
		layout:  "leader",
	}
}

// AddPane appends a pane and indexes it by slug.
func (pm *PaneManager) AddPane(p PaneInterface) {
	if _, exists := pm.paneMap[p.Slug()]; exists {
		return
	}
	pm.panes = append(pm.panes, p)
	pm.paneMap[p.Slug()] = p

	// If this is the first pane, focus it.
	if len(pm.panes) == 1 {
		p.SetFocused(true)
		pm.focusedIdx = 0
	}

	if pm.width > 0 && pm.height > 0 {
		pm.resizePane(p)
	}
}

// RemovePane removes a pane by slug.
func (pm *PaneManager) RemovePane(slug string) {
	p, exists := pm.paneMap[slug]
	if !exists {
		return
	}

	delete(pm.paneMap, slug)

	for i, pane := range pm.panes {
		if pane.Slug() == slug {
			pm.panes = append(pm.panes[:i], pm.panes[i+1:]...)
			break
		}
	}

	p.Close()

	// Fix focus index.
	if len(pm.panes) == 0 {
		pm.focusedIdx = 0
		return
	}
	if pm.focusedIdx >= len(pm.panes) {
		pm.focusedIdx = len(pm.panes) - 1
	}
	pm.panes[pm.focusedIdx].SetFocused(true)
}

// FocusPane sets focus to the pane with the given slug.
func (pm *PaneManager) FocusPane(slug string) {
	for i, p := range pm.panes {
		if p.Slug() == slug {
			pm.setFocusIdx(i)
			return
		}
	}
}

// FocusNext moves focus to the next pane.
func (pm *PaneManager) FocusNext() {
	if len(pm.panes) == 0 {
		return
	}
	pm.setFocusIdx((pm.focusedIdx + 1) % len(pm.panes))
}

// FocusPrev moves focus to the previous pane.
func (pm *PaneManager) FocusPrev() {
	if len(pm.panes) == 0 {
		return
	}
	idx := pm.focusedIdx - 1
	if idx < 0 {
		idx = len(pm.panes) - 1
	}
	pm.setFocusIdx(idx)
}

// Focused returns the currently focused pane, or nil.
func (pm *PaneManager) Focused() PaneInterface {
	if len(pm.panes) == 0 {
		return nil
	}
	return pm.panes[pm.focusedIdx]
}

// SetBroadcastMode enables or disables broadcast mode (keystrokes sent to all panes).
func (pm *PaneManager) SetBroadcastMode(on bool) {
	pm.broadcastMode = on
}

// IsBroadcastMode returns whether broadcast mode is active.
func (pm *PaneManager) IsBroadcastMode() bool {
	return pm.broadcastMode
}

// PaneCount returns the number of managed panes.
func (pm *PaneManager) PaneCount() int {
	return len(pm.panes)
}

// Panes returns all managed panes in order.
func (pm *PaneManager) Panes() []PaneInterface {
	return pm.panes
}

// HandleKey routes key input. Ctrl+N/P cycle focus, Ctrl+1..7 jump to index.
// All other keys are forwarded to the focused pane (or all panes in broadcast mode).
func (pm *PaneManager) HandleKey(key string) {
	switch key {
	case "ctrl+n":
		pm.FocusNext()
		return
	case "ctrl+p":
		pm.FocusPrev()
		return
	case "ctrl+1", "ctrl+2", "ctrl+3", "ctrl+4", "ctrl+5", "ctrl+6", "ctrl+7":
		idx := int(key[len(key)-1]-'0') - 1
		if idx < len(pm.panes) {
			pm.setFocusIdx(idx)
		}
		return
	}

	// Forward keystroke.
	data := []byte(key)
	if pm.broadcastMode {
		for _, p := range pm.panes {
			p.SendKey(data)
		}
	} else if f := pm.Focused(); f != nil {
		f.SendKey(data)
	}
}

// ResizeAll propagates a resize to all panes and stores the dimensions.
func (pm *PaneManager) ResizeAll(w, h int) {
	pm.width = w
	pm.height = h
	for _, p := range pm.panes {
		pm.resizePane(p)
	}
}

// maxVisibleSpecialists is the maximum number of specialist panes shown in the bottom row.
const maxVisibleSpecialists = 3

// View renders the leader-focused layout.
//
// Layout:
//
//	+--------- 70% w ----------+---- 30% w ----+
//	|                          |                |
//	|       Leader pane        |   Specialist   |
//	|       (60% h)            |   overflow /   |
//	|                          |   info         |
//	+--------- bottom 40% h --+----------------+
//	| Specialist 1 | Specialist 2 | Specialist 3|
//	+--------------+--------------+-------------+
func (pm *PaneManager) View(width, height int) string {
	pm.width = width
	pm.height = height

	if len(pm.panes) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, "No panes")
	}

	// Single pane: full screen.
	if len(pm.panes) == 1 {
		return pm.renderPane(pm.panes[0], width-2, height-2)
	}

	// Leader dimensions.
	leaderH := height * 60 / 100
	bottomH := height - leaderH

	leader := pm.panes[0]
	specialists := pm.panes[1:]

	// Render leader pane full width.
	leaderView := pm.renderPane(leader, width-2, leaderH-2)

	// Render specialist row (bottom).
	visible := specialists
	if len(visible) > maxVisibleSpecialists {
		visible = visible[:maxVisibleSpecialists]
	}

	specWidth := 0
	if len(visible) > 0 {
		specWidth = width / len(visible)
	}

	specViews := make([]string, len(visible))
	for i, sp := range visible {
		w := specWidth - 2
		if i == len(visible)-1 {
			// Last specialist takes remaining width.
			w = width - specWidth*(len(visible)-1) - 2
		}
		specViews[i] = pm.renderPane(sp, w, bottomH-2)
	}

	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, specViews...)

	// Overflow indicator.
	if len(specialists) > maxVisibleSpecialists {
		overflow := len(specialists) - maxVisibleSpecialists
		indicator := lipgloss.NewStyle().
			Foreground(lipgloss.Color(MutedColor)).
			Render(fmt.Sprintf(" +%d more panes", overflow))
		bottomRow = lipgloss.JoinVertical(lipgloss.Left, bottomRow, indicator)
	}

	return lipgloss.JoinVertical(lipgloss.Left, leaderView, bottomRow)
}

// CloseAll gracefully closes all panes (SIGTERM, then SIGKILL after timeout).
func (pm *PaneManager) CloseAll() {
	for _, p := range pm.panes {
		p.Close()
	}
}

// RemoveDeadPanes removes panes whose processes have exited.
// Returns the slugs of removed panes.
func (pm *PaneManager) RemoveDeadPanes() []string {
	var dead []string
	for _, p := range pm.panes {
		if !p.IsAlive() {
			dead = append(dead, p.Slug())
		}
	}
	for _, slug := range dead {
		// Use internal removal without Close (already dead).
		delete(pm.paneMap, slug)
		for i, pane := range pm.panes {
			if pane.Slug() == slug {
				pm.panes = append(pm.panes[:i], pm.panes[i+1:]...)
				break
			}
		}
	}
	// Fix focus.
	if len(pm.panes) == 0 {
		pm.focusedIdx = 0
	} else if pm.focusedIdx >= len(pm.panes) {
		pm.focusedIdx = len(pm.panes) - 1
		pm.panes[pm.focusedIdx].SetFocused(true)
	}
	return dead
}

// --- internal helpers ---

func (pm *PaneManager) setFocusIdx(idx int) {
	if len(pm.panes) == 0 {
		return
	}
	// Unfocus current.
	if pm.focusedIdx < len(pm.panes) {
		pm.panes[pm.focusedIdx].SetFocused(false)
	}
	pm.focusedIdx = idx
	pm.panes[pm.focusedIdx].SetFocused(true)
}

func (pm *PaneManager) resizePane(p PaneInterface) {
	// For now give each pane the full dimensions; View() handles cropping.
	p.Resize(pm.width, pm.height)
}

func (pm *PaneManager) renderPane(p PaneInterface, w, h int) string {
	borderColor := lipgloss.Color("#6B7280") // gray
	if p.IsFocused() {
		borderColor = lipgloss.Color("#06B6D4") // cyan
	}
	if !p.IsAlive() {
		borderColor = lipgloss.Color(Error) // red
	}

	// Title bar.
	focusIndicator := " "
	if p.IsFocused() {
		focusIndicator = "*"
	}
	title := fmt.Sprintf("[%s] %s %s", p.Slug(), p.Name(), focusIndicator)
	if pm.broadcastMode {
		title += " [BROADCAST]"
	}

	// Content: truncate view to fit inside border.
	content := p.View()
	contentLines := strings.Split(content, "\n")
	maxLines := h - 1 // reserve 1 line for title
	if maxLines < 0 {
		maxLines = 0
	}
	if len(contentLines) > maxLines {
		contentLines = contentLines[:maxLines]
	}
	body := strings.Join(contentLines, "\n")

	inner := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Foreground(borderColor).Render(title),
		body,
	)

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(w).
		Height(h).
		Render(inner)
}
