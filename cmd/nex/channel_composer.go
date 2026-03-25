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
	replyToID string, typingAgents []string, pending *channelInterview,
	selectedOption int, focused bool) string {

	if width < 10 {
		width = 10
	}

	var parts []string

	// ── Typing indicator ──────────────────────────────────────────────
	if len(typingAgents) > 0 {
		var typing string
		switch len(typingAgents) {
		case 1:
			typing = typingAgents[0] + " is typing..."
		case 2:
			typing = typingAgents[0] + ", " + typingAgents[1] + " are typing..."
		default:
			typing = fmt.Sprintf("%s, %s +%d are typing...",
				typingAgents[0], typingAgents[1], len(typingAgents)-2)
		}
		typingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(slackMuted)).
			Italic(true)
		parts = append(parts, "  "+typingStyle.Render(typing))
	}

	// ── Composer label ────────────────────────────────────────────────
	label := fmt.Sprintf("Message #%s", channelName)
	if pending != nil {
		label = fmt.Sprintf("Answer @%s's question", pending.From)
	} else if replyToID != "" {
		label = fmt.Sprintf("Reply to thread %s", replyToID)
	}
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(slackActive)).
		Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	parts = append(parts, "  "+labelStyle.Render(label)+"  "+hintStyle.Render("Use / for commands · @ to mention"))

	// ── Input field with rounded border ───────────────────────────────
	innerW := width - 6 // border (2) + padding (2) + outer margin (2)
	if innerW < 10 {
		innerW = 10
	}

	var inputStr string
	if len(input) == 0 {
		cursorStyle := lipgloss.NewStyle().Reverse(true)
		placeholder := "Type a message... (/ commands, @ mention)"
		if pending != nil {
			placeholder = "Type a custom answer, or Enter to accept"
		} else if replyToID != "" {
			placeholder = fmt.Sprintf("Reply in thread %s... (/cancel to go back)", replyToID)
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

type composerPopupOption struct {
	Label string
	Meta  string
}

func renderComposerPopup(options []composerPopupOption, selectedIdx int, width int, accent string) string {
	if len(options) == 0 {
		return ""
	}

	maxShow := len(options)

	popupStyle := lipgloss.NewStyle().
		Background(lipgloss.Color("#111218")).
		Foreground(lipgloss.Color(slackText)).
		Width(width).
		Padding(0, 1).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(slackBorder))

	selectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(accent)).
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(slackText))

	var lines []string
	for i := 0; i < maxShow; i++ {
		option := options[i]
		entry := fmt.Sprintf("  %-18s %s", option.Label, option.Meta)
		if i == selectedIdx {
			lines = append(lines, selectedStyle.Render(entry))
		} else {
			lines = append(lines, normalStyle.Render(entry))
		}
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted)).Render("  Enter submit • Tab complete • Esc close"))
	return popupStyle.Render(strings.Join(lines, "\n"))
}

// typingAgentsFromMembers returns a list of agent display names that are
// currently typing (activity within the last 5 seconds).
func typingAgentsFromMembers(members []channelMember) []string {
	var typing []string
	for _, m := range members {
		if m.Slug == "you" {
			continue
		}
		act := classifyActivity(m)
		if act.Label == "talking" {
			typing = append(typing, sidebarName(m.Slug))
		}
	}
	return typing
}
