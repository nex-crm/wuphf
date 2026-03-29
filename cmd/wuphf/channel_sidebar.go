package main

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func truncateLabel(label string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(label)
	if len(r) <= max {
		return label
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

// Sidebar theme colors.
const (
	sidebarBG      = "#1A1D21"
	sidebarMuted   = "#ABABAD"
	sidebarDivider = "#35373B"
	sidebarActive  = "#1264A3"

	dotTalking  = "#2BAC76"
	dotThinking = "#E8912D"
	dotCoding   = "#8B5CF6"
	dotIdle     = "#ABABAD"
)

// sidebarAgentColors maps slugs to their display colors.
var sidebarAgentColors = map[string]string{
	"ceo": "#EAB308", "pm": "#22C55E", "fe": "#3B82F6",
	"be": "#8B5CF6", "ai": "#14B8A6", "designer": "#EC4899",
	"cmo": "#F97316", "cro": "#06B6D4", "you": "#FFFFFF",
}

// memberActivity describes what an agent is doing based on recency and content.
type memberActivity struct {
	Label string
	Color string
	Dot   string
}

type officeCharacter struct {
	Avatar []string
	Bubble string
}

// classifyActivity determines activity from last message time and content.
func classifyActivity(m channelMember) memberActivity {
	now := time.Now()
	// Default to stale/idle until we can prove the last activity is recent.
	elapsed := 24 * time.Hour

	if m.LastTime != "" {
		for _, layout := range []string{
			time.RFC3339,
			"2006-01-02T15:04:05.000Z",
			"2006-01-02T15:04:05Z",
		} {
			if t, err := time.Parse(layout, m.LastTime); err == nil {
				elapsed = now.Sub(t)
				break
			}
		}
	}

	// Check for tool-use keywords indicating "coding".
	if elapsed < 30*time.Second && m.LastMessage != "" {
		lower := strings.ToLower(m.LastMessage)
		for _, kw := range []string{"bash", "edit", "read", "write", "grep", "glob"} {
			if strings.Contains(lower, kw) {
				return memberActivity{Label: "shipping", Color: dotCoding, Dot: "\u26A1"}
			}
		}
	}

	if m.LastTime == "" {
		return memberActivity{Label: "lurking", Color: dotIdle, Dot: "\u25CB"}
	}

	switch {
	case elapsed < 10*time.Second:
		return memberActivity{Label: "talking", Color: dotTalking, Dot: "\U0001F7E2"}
	case elapsed < 30*time.Second:
		return memberActivity{Label: "plotting", Color: dotThinking, Dot: "\U0001F7E1"}
	default:
		if m.LiveActivity != "" {
			dots := strings.Repeat("\u00b7", int(time.Now().Unix()%3)+1) // animated ···
			return memberActivity{Label: "thinking" + dots, Color: dotCoding, Dot: "\u26A1"}
		}
		return memberActivity{Label: "lurking", Color: dotIdle, Dot: "\u25CB"}
	}
}

func defaultSidebarRoster() []channelMember {
	return []channelMember{
		{Slug: "ceo", Name: "CEO", Role: "strategy"},
		{Slug: "pm", Name: "Product Manager", Role: "product"},
		{Slug: "fe", Name: "Frontend Engineer", Role: "frontend"},
		{Slug: "be", Name: "Backend Engineer", Role: "backend"},
		{Slug: "ai", Name: "AI Engineer", Role: "AI Engineer"},
		{Slug: "designer", Name: "Designer", Role: "design"},
		{Slug: "cmo", Name: "CMO", Role: "marketing"},
		{Slug: "cro", Name: "CRO", Role: "revenue"},
	}
}

func renderOfficeCharacter(m channelMember, act memberActivity, now time.Time) officeCharacter {
	seed := m.Name
	if seed == "" {
		seed = m.Slug
	}
	talkFrame := 0
	if act.Label == "talking" {
		talkFrame = int(now.UnixNano()/250_000_000) % 2
	}
	avatar := renderWuphfAvatar(seed, m.Slug, talkFrame)
	bubble := officeAside(m.Slug, act.Label, m.LastMessage, now)
	return officeCharacter{Avatar: avatar, Bubble: bubble}
}

func officeAside(slug, activity, lastMessage string, now time.Time) string {
	lists := map[string][]string{
		"ceo:talking": {
			"Delegating.",
			"Have a plan.",
		},
		"ceo:plotting": {
			"Smells strategic.",
			"Possible reorg.",
		},
		"pm:plotting": {
			"Scope creep.",
			"Needs triage.",
		},
		"pm:lurking": {
			"Hidden work.",
			"Roadmap vibes.",
		},
		"fe:shipping": {
			"Shipping it.",
			"Please no redesign.",
		},
		"fe:plotting": {
			"That button though.",
			"UI is loaded.",
		},
		"be:shipping": {
			"It will work.",
			"DB has feelings.",
		},
		"be:plotting": {
			"Too many moving parts.",
			"One less service?",
		},
		"ai:plotting": {
			"Eval first.",
			"Latency says hi.",
		},
		"ai:talking": {
			"Could be smarter.",
			"This becomes a system.",
		},
		"designer:plotting": {
			"Needs whitespace.",
			"Not polished.",
		},
		"designer:lurking": {
			"I have notes.",
			"That color dies.",
		},
		"cmo:talking": {
			"Message matters.",
			"No oatmeal copy.",
		},
		"cmo:plotting": {
			"Bland alert.",
			"We need a hook.",
		},
		"cro:talking": {
			"Price question.",
			"Revenue is real.",
		},
		"cro:lurking": {
			"Objection incoming.",
			"What are we selling?",
		},
		"default:talking": {
			"Have a thought.",
			"Need opinions.",
		},
		"default:plotting": {
			"Mild concern.",
			"Needs follow-up.",
		},
		"default:shipping": {
			"Doing it.",
			"My problem now.",
		},
		"default:lurking": {
			"Still here.",
			"Thinking quietly.",
		},
	}

	key := slug + ":" + activity
	options := lists[key]
	if len(options) == 0 {
		options = lists["default:"+activity]
	}
	if len(options) == 0 {
		return ""
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(key + "|" + lastMessage))
	offset := int(h.Sum32() % 9)
	phase := (int(now.Unix()) + offset) % 18
	if activity != "talking" {
		showFor := 5
		if phase >= showFor {
			return ""
		}
	}
	if activity == "talking" && lastMessage == "" {
		return ""
	}

	if lower := strings.ToLower(lastMessage); lower != "" {
		switch {
		case strings.Contains(lower, "blocked"):
			return "Blocked."
		case strings.Contains(lower, "launch"):
			return "Launch mode."
		case strings.Contains(lower, "design"):
			return "Taste fight."
		case strings.Contains(lower, "pricing"):
			return "Money time."
		}
	}
	return options[int(h.Sum32())%len(options)]
}

func activeSidebarTask(tasks []channelTask, slug string) (channelTask, bool) {
	bestScore := -1
	var best channelTask
	for _, task := range tasks {
		if strings.TrimSpace(task.Owner) != slug {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if status == "done" || status == "released" {
			continue
		}
		score := 1
		switch status {
		case "in_progress":
			score = 4
		case "review":
			score = 3
		case "blocked":
			score = 2
		case "claimed", "pending", "open":
			score = 1
		}
		if score > bestScore {
			bestScore = score
			best = task
		}
	}
	return best, bestScore >= 0
}

func applyTaskActivity(act memberActivity, task channelTask) memberActivity {
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "in_progress":
		return memberActivity{Label: "working", Color: dotCoding, Dot: "\u26A1"}
	case "review":
		return memberActivity{Label: "reviewing", Color: dotThinking, Dot: "\u25C6"}
	case "blocked":
		return memberActivity{Label: "blocked", Color: "#DC2626", Dot: "\u25CF"}
	case "claimed", "pending", "open":
		if act.Label == "talking" || act.Label == "plotting" {
			return act
		}
		return memberActivity{Label: "queued", Color: dotThinking, Dot: "\u25D4"}
	default:
		return act
	}
}

func taskBubbleText(task channelTask) string {
	title := strings.TrimSpace(task.Title)
	if title == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "in_progress":
		return "On " + title + "."
	case "review":
		return "Reviewing " + title + "."
	case "blocked":
		return "Blocked on " + title + "."
	case "claimed", "pending", "open":
		return "Queued: " + title + "."
	default:
		return ""
	}
}

func renderThoughtBubble(text string, width int) []string {
	if text == "" || width < 6 {
		return nil
	}
	wrapWidth := width - 4
	if wrapWidth < 6 {
		wrapWidth = 6
	}
	wrapped := strings.Split(ansi.Wrap(text, wrapWidth, ""), "\n")
	if len(wrapped) == 0 {
		return nil
	}
	bubbleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#2E2827")).
		Background(lipgloss.Color("#F2EDE6")).
		Bold(true)
	tailStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#F2EDE6"))
	lines := make([]string, 0, len(wrapped))
	for i, line := range wrapped {
		rendered := bubbleStyle.Render("▗ " + strings.TrimSpace(line) + " ▖")
		if i == len(wrapped)-1 {
			rendered += tailStyle.Render(" ▘")
		}
		lines = append(lines, rendered)
	}
	return lines
}

func padSidebarContent(text string, width int) string {
	if width <= 0 {
		return ""
	}
	visibleWidth := ansi.StringWidth(text)
	if visibleWidth < width {
		text += strings.Repeat(" ", width-visibleWidth)
	}
	return text
}

func sidebarPlainRow(text string, width int) string {
	return " " + padSidebarContent(text, maxInt(1, width-1))
}

func sidebarStyledRow(style lipgloss.Style, text string, width int) string {
	return style.Width(maxInt(1, width)).Render(text)
}

// renderSidebar renders the Slack-style sidebar with channels and team members.
func renderSidebar(channels []channelInfo, members []channelMember, tasks []channelTask, activeChannel string, activeApp officeApp, cursor int, rosterOffset int, focused bool, quickJump quickJumpTarget, brokerConnected bool, width, height int) string {
	if width < 2 {
		return ""
	}

	bg := lipgloss.Color(sidebarBG)
	innerW := width - 2 // 1 char padding each side

	sectionBandStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#D4D4D8")).
		Background(lipgloss.Color("#20242A")).
		Bold(true).
		Padding(0, 1)
	workspaceStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true)
	workspaceMetaStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sidebarMuted))
	activeRowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color(sidebarActive)).
		Bold(true).
		Padding(0, 1)
	cursorRowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB")).
		Background(lipgloss.Color("#253041")).
		Padding(0, 1)
	channelRowStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sidebarMuted)).
		Padding(0, 1)
	memberMetaStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(sidebarMuted))

	var lines []string
	lines = append(lines, "")
	lines = append(lines, sidebarPlainRow(workspaceStyle.Render("WUPHF"), width))
	lines = append(lines, sidebarPlainRow(workspaceMetaStyle.Render("The WUPHF Office"), width))
	lines = append(lines, sidebarPlainRow(workspaceMetaStyle.Italic(true).Render("Somehow still operational"), width))
	lines = append(lines, sidebarPlainRow(workspaceMetaStyle.Render("Ctrl+G channels · Ctrl+O apps"), width))
	if brokerConnected {
		lines = append(lines, sidebarPlainRow(workspaceMetaStyle.Render("Team connected"), width))
	} else {
		lines = append(lines, sidebarPlainRow(workspaceMetaStyle.Render("Offline preview from manifest"), width))
	}
	lines = append(lines, "")
	channelHeaderText := "Channels"
	if quickJump == quickJumpChannels {
		channelHeaderText = "Channels · 1-9"
	}
	lines = append(lines, sidebarStyledRow(sectionBandStyle, channelHeaderText, width))
	if len(channels) == 0 {
		channels = []channelInfo{{Slug: "general", Name: "general"}}
	}
	sidebarIndex := 0
	for _, ch := range channels {
		label := "# " + ch.Slug
		shortcut := sidebarShortcutLabel(sidebarIndex)
		if shortcut != "" {
			label = shortcut + "  " + label
		}
		switch {
		case ch.Slug == activeChannel:
			lines = append(lines, sidebarStyledRow(activeRowStyle, label, width))
		case focused && cursor == sidebarIndex:
			lines = append(lines, sidebarStyledRow(cursorRowStyle, label, width))
		default:
			lines = append(lines, sidebarStyledRow(channelRowStyle, label, width))
		}
		sidebarIndex++
	}

	lines = append(lines, "")
	appHeaderText := "Apps"
	if quickJump == quickJumpApps {
		appHeaderText = "Apps · 1-9"
	}
	lines = append(lines, sidebarStyledRow(sectionBandStyle, appHeaderText, width))
	apps := []struct {
		App   officeApp
		Label string
	}{
		{officeAppMessages, "Messages"},
		{officeAppTasks, "Tasks"},
		{officeAppRequests, "Requests"},
		{officeAppSkills, "Skills"},
		{officeAppInsights, "Insights"},
		{officeAppCalendar, "Calendar"},
	}
	appIndex := 0
	for _, app := range apps {
		label := appIcon(app.App) + " " + app.Label
		shortcut := sidebarShortcutLabel(appIndex)
		if shortcut != "" {
			label = shortcut + "  " + label
		}
		switch {
		case activeApp == app.App:
			lines = append(lines, sidebarStyledRow(activeRowStyle, label, width))
		case focused && cursor == sidebarIndex:
			lines = append(lines, sidebarStyledRow(cursorRowStyle, label, width))
		default:
			lines = append(lines, sidebarStyledRow(channelRowStyle, label, width))
		}
		sidebarIndex++
		appIndex++
	}

	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(sidebarDivider))
	divider := dividerStyle.Render(strings.Repeat("\u2500", innerW))
	lines = append(lines, sidebarPlainRow(divider, width))

	usedLines := len(lines)
	availableLines := height - usedLines - 1
	if availableLines < 0 {
		availableLines = 0
	}
	compact := availableLines < 14
	maxMembers := availableLines / 4
	if compact {
		maxMembers = availableLines // 1 line per member in compact mode
	}
	if maxMembers < 1 {
		maxMembers = 1
	}

	fallbackRoster := len(members) == 0
	if fallbackRoster {
		members = defaultSidebarRoster()
	}

	totalMembers := len(members)
	start := rosterOffset
	if start < 0 {
		start = 0
	}
	if totalMembers <= maxMembers {
		start = 0
	}
	maxStart := totalMembers - maxMembers
	if maxStart < 0 {
		maxStart = 0
	}
	if start > maxStart {
		start = maxStart
	}
	end := start + maxMembers
	if end > totalMembers {
		end = totalMembers
	}
	peopleHeader := "Agents"
	if fallbackRoster {
		peopleHeader = "Agents · office roster"
	} else if totalMembers > 0 && end > start {
		peopleHeader = fmt.Sprintf("Agents · %d-%d/%d", start+1, end, totalMembers)
	}
	lines = append(lines, sidebarStyledRow(sectionBandStyle, peopleHeader, width))

	now := time.Now()
	for i := start; i < end; i++ {
		m := members[i]
		act := classifyActivity(m)
		if task, ok := activeSidebarTask(tasks, m.Slug); ok {
			act = applyTaskActivity(act, task)
		}
		character := renderOfficeCharacter(m, act, now)
		if task, ok := activeSidebarTask(tasks, m.Slug); ok && character.Bubble == "" {
			character.Bubble = taskBubbleText(task)
		}

		dotStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(act.Color))
		dot := dotStyle.Render(act.Dot)

		agentColor := sidebarAgentColors[m.Slug]
		if agentColor == "" {
			agentColor = "#64748B"
		}
		name := m.Name
		if name == "" {
			name = displayName(m.Slug)
		}
		nameMax := innerW - 8 - ansi.StringWidth(act.Label)
		if nameMax < 8 {
			nameMax = 8
		}
		name = truncateLabel(name, nameMax)
		nameStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(agentColor)).
			Bold(true)
		nameRendered := nameStyle.Render(name)
		accent := lipgloss.NewStyle().Foreground(lipgloss.Color(agentColor)).Render("▎")
		leftPart := accent + " " + dot + " " + nameRendered
		if compact {
			// Compact: single line per member with a simple glyph.
			meta := memberMetaStyle.Render(act.Label)
			mini := lipgloss.NewStyle().Foreground(lipgloss.Color(agentColor)).Render(agentAvatar(m.Slug))
			line := leftPart + " " + mini
			pad := innerW - ansi.StringWidth(line) - ansi.StringWidth(meta)
			if pad < 1 {
				pad = 1
			}
			lines = append(lines, sidebarPlainRow(line+strings.Repeat(" ", pad)+meta, width))
		} else {
			// Full mode: first face line shares the row with name/activity.
			const avatarW = 4
			avatarTop := ""
			avatarBottom := ""
			if len(character.Avatar) > 0 {
				avatarTop = character.Avatar[0]
			}
			if len(character.Avatar) > 1 {
				avatarBottom = character.Avatar[1]
			}
			if ansi.StringWidth(avatarTop) < avatarW {
				avatarTop += strings.Repeat(" ", avatarW-ansi.StringWidth(avatarTop))
			}
			if ansi.StringWidth(avatarBottom) < avatarW {
				avatarBottom += strings.Repeat(" ", avatarW-ansi.StringWidth(avatarBottom))
			}

			linePrefix := avatarTop + " " + leftPart
			pad := innerW - ansi.StringWidth(linePrefix) - ansi.StringWidth(act.Label)
			if pad < 1 {
				pad = 1
			}
			lines = append(lines, sidebarPlainRow(linePrefix+strings.Repeat(" ", pad)+memberMetaStyle.Render(act.Label), width))
			if avatarBottom != "" {
				lines = append(lines, sidebarPlainRow(avatarBottom, width))
			}
			if character.Bubble != "" {
				for _, bubbleLine := range renderThoughtBubble(character.Bubble, innerW-2) {
					lines = append(lines, sidebarPlainRow(bubbleLine, width))
				}
			}
			lines = append(lines, "")
		}
	}

	if totalMembers > maxMembers {
		hint := memberMetaStyle.Render("PgUp/PgDn scroll agents")
		lines = append(lines, sidebarPlainRow(hint, width))
	}

	// Pad remaining height with empty lines.
	for len(lines) < height {
		lines = append(lines, "")
	}

	// Truncate if somehow over height.
	if len(lines) > height {
		lines = lines[:height]
	}

	// Apply sidebar background to each line, padded to full width.
	panel := lipgloss.NewStyle().Background(bg)

	var rendered []string
	for _, l := range lines {
		visibleWidth := ansi.StringWidth(l)
		if visibleWidth < width {
			l += strings.Repeat(" ", width-visibleWidth)
		}
		rendered = append(rendered, panel.Render(l))
	}

	return strings.Join(rendered, "\n")
}
