package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/tui"
)

// renderThreadPanel renders the thread side panel with parent message,
// reply count divider, replies, and its own input field.
func renderThreadPanel(allMessages []channelui.BrokerMessage, parentID string,
	width, height int, threadInput []rune, threadInputPos int,
	threadScroll int, popup string, focused bool, historyAvailable bool) string {

	if width < 8 || height < 4 {
		return ""
	}

	bg := lipgloss.Color(channelui.SlackThreadBg)
	innerW := width - 2 // 1 char padding each side

	// ── Header: "Thread" + "✕" ────────────────────────────────────────
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true)
	closeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(channelui.SlackMuted))

	titleText := headerStyle.Render("Thread")
	closeText := closeStyle.Render("✕ Esc")
	titleWidth := lipgloss.Width(titleText)
	closeWidth := lipgloss.Width(closeText)
	headerPad := innerW - titleWidth - closeWidth
	if headerPad < 1 {
		headerPad = 1
	}
	headerLine := titleText + strings.Repeat(" ", headerPad) + closeText

	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackDivider))
	headerDivider := dividerStyle.Render(strings.Repeat("─", innerW))

	// ── Find parent message ───────────────────────────────────────────
	parent, parentFound := channelui.FindMessageByID(allMessages, parentID)

	// ── Collect full thread replies (including nested replies) ───────
	replies := channelui.FlattenThreadReplies(allMessages, parentID)

	// ── Build content lines ───────────────────────────────────────────
	var contentLines []string

	// Parent message
	if parentFound {
		contentLines = append(contentLines, channelui.RenderThreadMessage(parent, innerW, true)...)
		contentLines = append(contentLines, "")

		// Reply count divider
		replyCount := len(replies)
		if replyCount > 0 {
			replyWord := "reply"
			if replyCount != 1 {
				replyWord = "replies"
			}
			divLabel := fmt.Sprintf(" %d %s ", replyCount, replyWord)
			lineLen := innerW - len(divLabel) - 2
			if lineLen < 4 {
				lineLen = 4
			}
			leftSeg := strings.Repeat("─", lineLen/2)
			rightSeg := strings.Repeat("─", lineLen-lineLen/2)
			contentLines = append(contentLines,
				dividerStyle.Render(leftSeg+divLabel+rightSeg))
			contentLines = append(contentLines, "")
		}

		contentLines = append(contentLines, channelui.RenderThreadReplies(replies, innerW)...)
	} else {
		contentLines = append(contentLines,
			lipgloss.NewStyle().
				Foreground(lipgloss.Color(channelui.SlackMuted)).
				Render("  Thread message not found."))
	}

	// ── Thread input field ────────────────────────────────────────────
	threadInputRendered := renderThreadInput(threadInput, threadInputPos, innerW-2, focused, historyAvailable)
	inputH := lipgloss.Height(threadInputRendered)
	usedH := 3 // header line + header divider + blank
	contentH := height - usedH - inputH
	if contentH < 1 {
		contentH = 1
	}

	// Apply scroll to content
	total := len(contentLines)
	scroll := channelui.ClampScroll(total, contentH, threadScroll)
	end := total - scroll
	if end > total {
		end = total
	}
	if end < 1 && total > 0 {
		end = 1
	}
	start := end - contentH
	if start < 0 {
		start = 0
	}

	var visible []string
	if total > 0 {
		visible = contentLines[start:end]
	}
	for len(visible) < contentH {
		visible = append(visible, "")
	}
	if popup != "" {
		visible = channelui.OverlayBottomLines(visible, strings.Split(popup, "\n"))
	}

	// ── Compose panel ─────────────────────────────────────────────────
	var panelLines []string
	panelLines = append(panelLines, " "+headerLine)
	panelLines = append(panelLines, " "+headerDivider)
	for _, line := range visible {
		panelLines = append(panelLines, " "+line)
	}
	panelLines = append(panelLines, threadInputRendered)

	// Pad/trim to exact height
	for len(panelLines) < height {
		panelLines = append(panelLines, "")
	}
	if len(panelLines) > height {
		panelLines = panelLines[:height]
	}

	// Apply background to each line
	panelStyle := lipgloss.NewStyle().
		Width(width).
		Background(bg)

	var rendered []string
	for _, l := range panelLines {
		rendered = append(rendered, panelStyle.Render(l))
	}

	return strings.Join(rendered, "\n")
}

// renderThreadInput renders the input area at the bottom of the thread panel.
func renderThreadInput(input []rune, inputPos int, width int, focused bool, historyAvailable bool) string {
	if width < 6 {
		width = 6
	}

	var inputStr string
	if len(input) == 0 {
		cursorStyle := lipgloss.NewStyle().Reverse(true)
		placeholder := lipgloss.NewStyle().
			Foreground(lipgloss.Color(channelui.SlackMuted)).
			Render(" Reply in thread...")
		inputStr = cursorStyle.Render(" ") + placeholder
	} else {
		before := string(input[:inputPos])
		cursorStyle := lipgloss.NewStyle().Reverse(true)
		var cursor, after string
		if inputPos < len(input) {
			cursor = cursorStyle.Render(string(input[inputPos]))
			after = string(input[inputPos+1:])
		} else {
			cursor = cursorStyle.Render(" ")
			after = ""
		}
		inputStr = before + cursor + after
	}

	inputStr = ansi.Wrap(inputStr, width-2, "")

	borderColor := channelui.SlackInputBorder
	if focused {
		borderColor = channelui.SlackInputFocus
	}
	borderStyle := lipgloss.NewStyle().
		Width(width).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(borderColor)).
		Background(lipgloss.Color("#17161C")).
		Padding(0, 1)

	label := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackActive)).Bold(true).Render("Reply")
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackMuted)).Render(
		tui.ComposerHint(tui.ComposerHintState{
			Context:          tui.ContextThreadCompose,
			HistoryAvailable: historyAvailable,
		}),
	)
	return " " + label + "  " + hint + "\n " + borderStyle.Render(inputStr)
}
