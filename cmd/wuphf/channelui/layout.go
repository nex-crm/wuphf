package channelui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// LayoutDimensions describes the computed panel widths and the content
// height for the current terminal. Returned by ComputeLayout and read by
// the channel view's render loop, the mouse hit-test, and viewport
// virtualization.
type LayoutDimensions struct {
	SidebarW    int
	MainW       int
	ThreadW     int
	ContentH    int
	ShowSidebar bool
	ShowThread  bool
}

// ComputeLayout calculates panel widths based on terminal size and UI
// state.
//
// Breakpoints:
//
//	Wide  (126+)  : sidebar 31, thread 40 (when open)
//	Medium(88-125): sidebar 26, thread overlays main
//	Narrow(<88)   : no sidebar, thread overlays main
func ComputeLayout(width, height int, threadOpen, sidebarCollapsed bool) LayoutDimensions {
	const (
		statusBarH  = 1
		borderW     = 1 // vertical border between panels
		wideBreak   = 126
		mediumBreak = 88
		wideSidebar = 31
		medSidebar  = 26
		wideThread  = 40
	)

	ld := LayoutDimensions{
		ContentH: height - statusBarH,
	}
	if ld.ContentH < 1 {
		ld.ContentH = 1
	}

	switch {
	case width >= wideBreak:
		// Wide: sidebar + main + optional thread, all visible
		ld.ShowSidebar = !sidebarCollapsed
		ld.ShowThread = threadOpen

		usedW := 0
		if ld.ShowSidebar {
			ld.SidebarW = wideSidebar
			usedW += ld.SidebarW + borderW
		}
		if ld.ShowThread {
			ld.ThreadW = wideThread
			usedW += ld.ThreadW + borderW
		}
		ld.MainW = width - usedW
		if ld.MainW < 1 {
			ld.MainW = 1
		}

	case width >= mediumBreak:
		// Medium: sidebar + main; thread overlays main area
		ld.ShowSidebar = !sidebarCollapsed
		ld.ShowThread = threadOpen

		usedW := 0
		if ld.ShowSidebar {
			ld.SidebarW = medSidebar
			usedW += ld.SidebarW + borderW
		}
		remaining := width - usedW
		if ld.ShowThread {
			// Thread overlays right portion of main
			ld.ThreadW = wideThread
			if ld.ThreadW > remaining {
				ld.ThreadW = remaining
			}
			ld.MainW = remaining - ld.ThreadW - borderW
			if ld.MainW < 1 {
				ld.MainW = 1
			}
		} else {
			ld.MainW = remaining
		}

	default:
		// Narrow: no sidebar; thread overlays main
		ld.ShowSidebar = false
		ld.ShowThread = threadOpen

		if ld.ShowThread {
			ld.ThreadW = width
			ld.MainW = 0
		} else {
			ld.MainW = width
		}
	}

	return ld
}

// RenderVerticalBorder draws a single vertical line of the given height
// in the supplied color. Used between panels in the channel layout.
func RenderVerticalBorder(height int, color string) string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(color))
	return style.Render(strings.Repeat("│\n", height-1) + "│")
}
