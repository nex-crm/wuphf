package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// layoutDimensions holds the computed panel sizes for the current terminal.
type layoutDimensions struct {
	SidebarW    int
	MainW       int
	ThreadW     int
	ContentH    int
	ShowSidebar bool
	ShowThread  bool
}

// computeLayout calculates panel widths based on terminal size and UI state.
//
// Breakpoints:
//
//	Wide  (120+) : sidebar 22, thread 35 (when open)
//	Medium(80-119): sidebar 20, thread overlays main
//	Narrow(<80)  : no sidebar, thread overlays main
func computeLayout(width, height int, threadOpen, sidebarCollapsed bool) layoutDimensions {
	const (
		statusBarH  = 1
		borderW     = 1 // vertical border between panels
		wideBreak   = 126
		mediumBreak = 88
		wideSidebar = 28
		medSidebar  = 24
		wideThread  = 40
	)

	ld := layoutDimensions{
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

// renderVerticalBorder draws a vertical line of the given height.
func renderVerticalBorder(height int, color string) string {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(color))
	return style.Render(strings.Repeat("│\n", height-1) + "│")
}
