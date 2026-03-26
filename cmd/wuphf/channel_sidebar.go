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
	Avatar string
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
	frame := int(now.Unix() % 2)
	avatar := agentCharacter(m.Slug, act.Label, frame)
	bubble := officeAside(m.Slug, act.Label, m.LastMessage)
	return officeCharacter{Avatar: avatar, Bubble: bubble}
}

func officeAside(slug, activity, lastMessage string) string {
	lists := map[string][]string{
		"ceo:talking": {
			"I am delegating. This is leadership.",
			"Everyone relax. I have a framework for this.",
		},
		"ceo:plotting": {
			"I can already feel a reorg trying to happen.",
			"This is either strategy or a very expensive detour.",
		},
		"pm:plotting": {
			"Scope is a social construct until launch day.",
			"I am once again asking for narrower requirements.",
		},
		"pm:lurking": {
			"I am listening for hidden complexity.",
			"This smells like a roadmap conversation.",
		},
		"fe:shipping": {
			"If this turns into a redesign, I am muting the channel.",
			"I can ship this. I can also regret it later.",
		},
		"fe:plotting": {
			"That button is carrying a lot of emotional weight.",
			"We are one vague sentence away from scope creep.",
		},
		"be:shipping": {
			"I will make it work. I did not say it will be pretty.",
			"The database is about to learn some new feelings.",
		},
		"be:plotting": {
			"Every shortcut becomes my personality later.",
			"I would love one fewer moving part here.",
		},
		"ai:plotting": {
			"We should maybe eval this before we marry it.",
			"Everyone wants magic until latency arrives.",
		},
		"ai:talking": {
			"I can make it smarter. Whether we should is different.",
			"That is one prompt away from becoming a whole system.",
		},
		"designer:plotting": {
			"I am begging this team to let whitespace live.",
			"We are not calling that polished yet.",
		},
		"designer:lurking": {
			"I have notes. They are visual and judgmental.",
			"That color is not surviving review.",
		},
		"cmo:talking": {
			"Messaging is a product decision. Sorry, but it is.",
			"Someone has to stop us from sounding like enterprise oatmeal.",
		},
		"cmo:plotting": {
			"I am trying to save us from bland positioning.",
			"This headline currently fears commitment.",
		},
		"cro:talking": {
			"At some point a buyer will ask what this costs.",
			"I am just here to remind everyone revenue is real.",
		},
		"cro:lurking": {
			"I can hear an objection forming from across the office.",
			"Someone should probably decide what we are selling first.",
		},
		"default:talking": {
			"I have a thought, unfortunately.",
			"This feels important enough to have opinions about.",
		},
		"default:plotting": {
			"I am forming a tasteful amount of concern.",
			"I can already tell this will need follow-up.",
		},
		"default:shipping": {
			"Well, here goes nothing professional.",
			"I touched it, so now it is my problem.",
		},
		"default:lurking": {
			"I am listening. Against my will.",
			"I do have thoughts. I am rationing them.",
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

	if lower := strings.ToLower(lastMessage); lower != "" {
		switch {
		case strings.Contains(lower, "blocked"):
			return "Cool. So this is on fire now."
		case strings.Contains(lower, "launch"):
			return "Everyone loves urgency until it becomes a calendar event."
		case strings.Contains(lower, "design"):
			return "This is becoming a taste question, which is dangerous."
		case strings.Contains(lower, "pricing"):
			return "Ah yes, the part where money becomes visible."
		}
	}

	h := fnv.New32a()
	_, _ = h.Write([]byte(key + "|" + lastMessage))
	return options[int(h.Sum32())%len(options)]
}

// renderSidebar renders the Slack-style sidebar with channels and team members.
func renderSidebar(channels []channelInfo, members []channelMember, activeChannel string, activeApp officeApp, cursor int, focused bool, quickJump quickJumpTarget, brokerConnected bool, width, height int) string {
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
	lines = append(lines, " "+workspaceStyle.Render("WUPHF"))
	lines = append(lines, " "+workspaceMetaStyle.Render("The WUPHF Office"))
	lines = append(lines, " "+workspaceMetaStyle.Italic(true).Render("Somehow still operational"))
	lines = append(lines, " "+workspaceMetaStyle.Render("Ctrl+G channels · Ctrl+O apps"))
	if brokerConnected {
		lines = append(lines, " "+workspaceMetaStyle.Render("Team connected"))
	} else {
		lines = append(lines, " "+workspaceMetaStyle.Render("Offline preview from manifest"))
	}
	lines = append(lines, "")
	channelHeaderText := "Channels"
	if quickJump == quickJumpChannels {
		channelHeaderText = "Channels · 1-9"
	}
	lines = append(lines, " "+sectionBandStyle.Width(innerW-1).Render(channelHeaderText))
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
			lines = append(lines, " "+activeRowStyle.Width(innerW-1).Render(label))
		case focused && cursor == sidebarIndex:
			lines = append(lines, " "+cursorRowStyle.Width(innerW-1).Render(label))
		default:
			lines = append(lines, " "+channelRowStyle.Render(label))
		}
		sidebarIndex++
	}

	lines = append(lines, "")
	appHeaderText := "Apps"
	if quickJump == quickJumpApps {
		appHeaderText = "Apps · 1-9"
	}
	lines = append(lines, " "+sectionBandStyle.Width(innerW-1).Render(appHeaderText))
	apps := []struct {
		App   officeApp
		Label string
	}{
		{officeAppMessages, "Messages"},
		{officeAppTasks, "Tasks"},
		{officeAppRequests, "Requests"},
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
			lines = append(lines, " "+activeRowStyle.Width(innerW-1).Render(label))
		case focused && cursor == sidebarIndex:
			lines = append(lines, " "+cursorRowStyle.Width(innerW-1).Render(label))
		default:
			lines = append(lines, " "+channelRowStyle.Render(label))
		}
		sidebarIndex++
		appIndex++
	}

	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(sidebarDivider))
	divider := dividerStyle.Render(strings.Repeat("\u2500", innerW))
	lines = append(lines, " "+divider)

	peopleHeader := "People"
	if len(members) == 0 {
		peopleHeader = "People · office roster"
	}
	lines = append(lines, " "+sectionBandStyle.Width(innerW-1).Render(peopleHeader))

	usedLines := len(lines)
	availableLines := height - usedLines - 1
	if availableLines < 0 {
		availableLines = 0
	}
	compact := availableLines < 14
	maxMembers := availableLines / 4 // 4 lines per member: name, face+role, bubble (may wrap)
	if compact {
		maxMembers = availableLines // 1 line per member in compact mode
	}
	if maxMembers < 1 {
		maxMembers = 1
	}

	if len(members) == 0 {
		members = defaultSidebarRoster()
	}

	visibleCount := len(members)
	overflow := 0
	if visibleCount > maxMembers {
		overflow = visibleCount - maxMembers
		visibleCount = maxMembers
	}

	now := time.Now()
	for i := 0; i < visibleCount; i++ {
		m := members[i]
		act := classifyActivity(m)
		character := renderOfficeCharacter(m, act, now)

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
		name = truncateLabel(name, innerW-8)
		nameStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(agentColor)).
			Bold(true)
		nameRendered := nameStyle.Render(name)
		role := m.Role
		if role == "" {
			role = roleLabel(m.Slug)
		}
		role = truncateLabel(role, innerW-8)
		roleRendered := memberMetaStyle.Render(role)
		accent := lipgloss.NewStyle().Foreground(lipgloss.Color(agentColor)).Render("▎")
		leftPart := accent + " " + dot + " " + nameRendered
		if compact {
			// Compact: single line per member (name + activity, no face)
			meta := memberMetaStyle.Render(act.Label)
			pad := innerW - ansi.StringWidth(leftPart) - ansi.StringWidth(meta)
			if pad < 1 {
				pad = 1
			}
			lines = append(lines, " "+leftPart+strings.Repeat(" ", pad)+meta)
		} else {
			// Full: line 1 = accent + dot + name + activity
			pad := innerW - ansi.StringWidth(leftPart) - ansi.StringWidth(act.Label)
			if pad < 1 {
				pad = 1
			}
			lines = append(lines, " "+leftPart+strings.Repeat(" ", pad)+memberMetaStyle.Render(act.Label))
			// Line 2: character face + role
			charFace := lipgloss.NewStyle().Foreground(lipgloss.Color(agentColor)).Render(character.Avatar)
			lines = append(lines, "   "+charFace+" "+roleRendered)
			// Line 3+: mood bubble — wrap across lines instead of truncating
			if character.Bubble != "" {
				bubbleStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("#7A7A7E")).
					Italic(true)
				bubbleWidth := innerW - 5 // 3 indent + 2 quote chars
				if bubbleWidth < 12 {
					bubbleWidth = 12
				}
				quoted := "\u201c" + character.Bubble + "\u201d"
				// Wrap long bubbles across two lines max
				if len([]rune(quoted)) <= bubbleWidth {
					lines = append(lines, "   "+bubbleStyle.Render(quoted))
				} else {
					// Split at word boundary near bubbleWidth
					runes := []rune(quoted)
					splitAt := bubbleWidth
					for splitAt > bubbleWidth/2 {
						if runes[splitAt] == ' ' {
							break
						}
						splitAt--
					}
					if splitAt <= bubbleWidth/2 {
						splitAt = bubbleWidth // no good split point, hard break
					}
					lines = append(lines, "   "+bubbleStyle.Render(string(runes[:splitAt])))
					remainder := strings.TrimSpace(string(runes[splitAt:]))
					if len([]rune(remainder)) > bubbleWidth {
						remainder = string([]rune(remainder)[:bubbleWidth-1]) + "…"
					}
					if remainder != "" {
						lines = append(lines, "   "+bubbleStyle.Render(remainder))
					}
				}
			}
		}
	}

	if overflow > 0 {
		more := memberMetaStyle.Render(fmt.Sprintf("\u22EF +%d more in office", overflow))
		lines = append(lines, " "+more)
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
	panel := lipgloss.NewStyle().
		Width(width).
		Background(bg)

	var rendered []string
	for _, l := range lines {
		rendered = append(rendered, panel.Render(l))
	}

	return strings.Join(rendered, "\n")
}
