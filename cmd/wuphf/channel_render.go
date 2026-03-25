package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type renderedLine struct {
	Text     string
	ThreadID string
}

func buildOfficeMessageLines(messages []brokerMessage, expanded map[string]bool, contentWidth int, threadsDefaultExpand bool) []renderedLine {
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#CBD5E1")).Italic(true)

	var lines []renderedLine
	if len(messages) == 0 {
		lines = append(lines,
			renderedLine{Text: ""},
			renderedLine{Text: mutedStyle.Render("  Welcome to The WUPHF Office.")},
			renderedLine{Text: mutedStyle.Render("  Drop a company-building thought here or tag a teammate.")},
			renderedLine{Text: ""},
			renderedLine{Text: mutedStyle.Render("  Suggested: Let's build an AI notetaking company.")},
		)
		return lines
	}

	lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Today")})

	if threadsDefaultExpand {
		for _, msg := range messages {
			if msg.ReplyTo == "" && hasThreadReplies(messages, msg.ID) {
				if _, explicit := expanded[msg.ID]; !explicit {
					expanded[msg.ID] = true
				}
			}
		}
	}

	for _, tm := range flattenThreadMessages(messages, expanded) {
		msg := tm.Message
		ts := msg.Timestamp
		if len(ts) > 19 {
			ts = ts[11:19]
		}

		color := agentColorMap[msg.From]
		if color == "" {
			color = "#9CA3AF"
		}
		nameStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(color)).
			Bold(true)
		ruleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))

		appendWrappedLine := func(text string) {
			wrapped := appendWrapped(nil, contentWidth, text)
			for _, line := range wrapped {
				lines = append(lines, renderedLine{Text: line})
			}
		}

		if msg.Kind == "automation" || msg.From == "nex" {
			lines = append(lines, renderedLine{Text: ""})
			headerPrefix := "  " + strings.Repeat("  ", tm.Depth)
			if tm.Depth > 0 {
				headerPrefix += "↳ "
			}
			source := msg.Source
			if source == "" {
				source = "context graph"
			} else {
				source = strings.ReplaceAll(source, "_", " ")
			}
			meta := fmt.Sprintf("%s · automated · %s", source, msg.ID)
			if tm.Depth > 0 {
				meta += fmt.Sprintf(" · thread reply to %s", tm.ParentLabel)
			}
			appendWrappedLine(fmt.Sprintf("%s%s  %s  %s", headerPrefix, nameStyle.Render(displayName(msg.From)), mutedStyle.Render(ts), mutedStyle.Render(meta)))

			prefix := "  " + strings.Repeat("  ", tm.Depth)
			if tm.Depth > 0 {
				prefix += ruleStyle.Render("┆") + " "
			} else {
				prefix += ruleStyle.Render("│") + " "
			}
			if msg.Title != "" {
				appendWrappedLine(prefix + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Render(msg.Title))
			}
			textPart, a2uiRendered := renderA2UIBlocks(msg.Content, contentWidth-4)
			for _, paragraph := range strings.Split(textPart, "\n") {
				appendWrappedLine(prefix + paragraph)
			}
			if a2uiRendered != "" {
				for _, lineText := range strings.Split(a2uiRendered, "\n") {
					lines = append(lines, renderedLine{Text: prefix + lineText})
				}
			}
			continue
		}

		if strings.HasPrefix(msg.Content, "[STATUS]") {
			status := strings.TrimPrefix(msg.Content, "[STATUS] ")
			statusPrefix := "  " + strings.Repeat("  ", tm.Depth)
			if tm.Depth > 0 {
				statusPrefix += "↳ "
			}
			appendWrappedLine(fmt.Sprintf("%s%s  %s %s", statusPrefix, mutedStyle.Render(ts), nameStyle.Render("@"+msg.From), statusStyle.Render("is "+status)))
			continue
		}

		mood := inferMood(msg.Content)
		meta := roleLabel(msg.From) + " · " + msg.ID
		if mood != "" {
			meta += " · " + mood
		}
		if tm.Depth > 0 {
			meta += fmt.Sprintf(" · thread reply to %s", tm.ParentLabel)
		}
		metaStyle := mutedStyle
		if mood != "" {
			metaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		}
		lines = append(lines, renderedLine{Text: ""})
		headerPrefix := "  " + strings.Repeat("  ", tm.Depth)
		if tm.Depth > 0 {
			headerPrefix += "↳ "
		}
		appendWrappedLine(fmt.Sprintf("%s%s  %s  %s", headerPrefix, nameStyle.Render(displayName(msg.From)), mutedStyle.Render(ts), metaStyle.Render(meta)))

		prefix := "  " + strings.Repeat("  ", tm.Depth)
		if tm.Depth > 0 {
			prefix += ruleStyle.Render("┆") + " "
		} else {
			prefix += ruleStyle.Render("│") + " "
		}

		textPart, a2uiRendered := renderA2UIBlocks(msg.Content, contentWidth-4)
		for _, paragraph := range strings.Split(textPart, "\n") {
			paragraph = highlightMentions(paragraph, agentColorMap)
			appendWrappedLine(prefix + paragraph)
		}
		if a2uiRendered != "" {
			for _, lineText := range strings.Split(a2uiRendered, "\n") {
				lines = append(lines, renderedLine{Text: prefix + lineText})
			}
		}
		if tm.Collapsed && tm.HiddenReplies > 0 {
			var coloredNames []string
			for _, p := range tm.ThreadParticipants {
				pColor := agentColorMap[strings.TrimPrefix(strings.ToLower(p), "@")]
				if pColor == "" {
					for slug, name := range map[string]string{
						"ceo": "CEO", "pm": "Product Manager", "fe": "Frontend Engineer", "be": "Backend Engineer",
						"ai": "AI Engineer", "designer": "Designer", "cmo": "CMO", "cro": "CRO",
					} {
						if p == name {
							pColor = agentColorMap[slug]
							break
						}
					}
				}
				if pColor == "" {
					pColor = "#ABABAD"
				}
				coloredNames = append(coloredNames, lipgloss.NewStyle().Foreground(lipgloss.Color(pColor)).Bold(true).Render(p))
			}
			participantStr := ""
			if len(coloredNames) > 0 {
				participantStr = "  " + strings.Join(coloredNames, ", ")
			}
			label := fmt.Sprintf("  ↩ %d repl%s%s", tm.HiddenReplies, pluralSuffix(tm.HiddenReplies), participantStr)
			lines = append(lines, renderedLine{Text: label, ThreadID: msg.ID})
		}
	}

	return lines
}

func sliceRenderedLines(lines []renderedLine, msgH, scroll int) ([]renderedLine, int, int, int) {
	total := len(lines)
	scroll = clampScroll(total, msgH, scroll)
	end := total - scroll
	if end > total {
		end = total
	}
	if end < 1 && total > 0 {
		end = 1
	}
	start := end - msgH
	if start < 0 {
		start = 0
	}
	if total == 0 {
		return nil, scroll, 0, 0
	}
	return lines[start:end], scroll, start, end
}
