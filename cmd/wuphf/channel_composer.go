package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// renderComposer renders the Slack-style input area with typing indicator,
// label, rounded border, cursor, @mention popup, and interview options.
func renderComposer(width int, input []rune, inputPos int, channelName string,
	replyToID string, typingAgents []string, liveActivities map[string]string,
	pending *channelInterview, selectedOption int, hint string, focused bool, tickFrame int) string {

	if width < 10 {
		width = 10
	}

	var parts []string

	// ── Typing indicator ──────────────────────────────────────────────

	// ── Composer label ────────────────────────────────────────────────
	isDM := strings.HasPrefix(channelName, "DM→")
	label := fmt.Sprintf("Message #%s", channelName)
	if strings.HasPrefix(channelName, "1:1 ") {
		label = channelName
	} else if isDM {
		label = channelName
	}
	if pending != nil {
		label = fmt.Sprintf("Answer @%s's question", pending.From)
	} else if replyToID != "" {
		label = fmt.Sprintf("Reply to thread %s", replyToID)
	}
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(slackActive)).
		Bold(true)
	if isDM && pending == nil && replyToID == "" {
		labelStyle = labelStyle.Foreground(lipgloss.Color("#8B5CF6"))
	}
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	if strings.TrimSpace(hint) == "" {
		hint = "/ commands · @ mention · Ctrl+J newline · Enter send · Esc pause all"
		if pending != nil {
			hint = "↑/↓ pick option · Enter submit · type to answer freeform · Esc pause all"
		} else if strings.HasPrefix(channelName, "1:1 ") {
			hint = "/ commands · @ mention · Ctrl+J newline · Enter send direct · Esc pause all"
		} else if isDM {
			hint = "/ commands · Ctrl+J newline · Enter send direct · Ctrl+D close DM · Esc pause all"
		}
	}
	parts = append(parts, "  "+labelStyle.Render(label)+"  "+hintStyle.Render(hint))

	// ── Input field with rounded border ───────────────────────────────
	innerW := width - 6 // border (2) + padding (2) + outer margin (2)
	if innerW < 10 {
		innerW = 10
	}

	var inputStr string
	if len(input) == 0 {
		cursorStyle := lipgloss.NewStyle().Reverse(true)
		placeholder := "Type a message... (/ commands, @ mention)"
		if strings.HasPrefix(channelName, "1:1 ") {
			placeholder = "Talk directly to your agent here... (Ctrl+J for a new line)"
		} else if isDM {
			agentName := strings.TrimPrefix(channelName, "DM→")
			placeholder = fmt.Sprintf("Message %s directly... (office stays active)", agentName)
		}
		if pending != nil {
			placeholder = "Type your answer here, or Enter to accept the highlighted option"
		} else if replyToID != "" {
			placeholder = fmt.Sprintf("Reply in thread %s... (Ctrl+J newline, /cancel to go back)", replyToID)
		}
		inputStr = cursorStyle.Render(" ") + lipgloss.NewStyle().
			Foreground(lipgloss.Color(slackMuted)).Render(" "+placeholder)
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

	// Wrap input text to fit within border
	inputStr = ansi.Wrap(inputStr, innerW, "")

	borderStyle := composerBorderStyle(width-4, focused)
	inputBox := borderStyle.Render(inputStr)
	parts = append(parts, inputBox)

	return strings.Join(parts, "\n")
}
