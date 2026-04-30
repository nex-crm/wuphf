package channelui

import (
	"hash/fnv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/nex-crm/wuphf/internal/avatar"
)

// TruncateLabel shortens label to at most max runes. Returns "" for
// max <= 0 and "…" for max == 1; otherwise truncates and appends an
// ellipsis. Multi-byte aware (operates on runes).
func TruncateLabel(label string, max int) string {
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

// Sidebar theme colors. Distinct from the slack-themed feed palette
// in styles.go so the sidebar can carry its own muted Slack-sidebar
// look (darker bg, dimmer dividers).
const (
	SidebarBG      = "#1A1D21"
	SidebarMuted   = "#ABABAD"
	SidebarDivider = "#35373B"
	SidebarActive  = "#1264A3"

	DotTalking  = "#2BAC76"
	DotThinking = "#E8912D"
	DotCoding   = "#8B5CF6"
	DotIdle     = "#ABABAD"
)

// SidebarAgentColors maps agent slugs to their sidebar/avatar colors.
// Mirrors AgentColorMap (in styles.go) but for the sidebar palette.
var SidebarAgentColors = map[string]string{
	"ceo": "#EAB308", "pm": "#22C55E", "fe": "#3B82F6",
	"be": "#8B5CF6", "ai": "#14B8A6", "designer": "#EC4899",
	"cmo": "#F97316", "cro": "#06B6D4", "you": "#38BDF8", "human": "#38BDF8",
}

// MemberActivity describes what a member is doing right now — the
// rendered label ("talking", "shipping", "lurking", etc.), the pill
// color, and the dot glyph (filled / empty / lightning) that goes
// next to the avatar.
type MemberActivity struct {
	Label string
	Color string
	Dot   string
}

// OfficeCharacter is the sidebar's per-member visual: an ASCII-art
// avatar (multi-line) plus an optional thought bubble.
type OfficeCharacter struct {
	Avatar []string
	Bubble string
}

// ClassifyActivity determines a member's activity from elapsed time
// since LastTime, the kind of work hinted at by LastMessage, and any
// LiveActivity captured from their tmux pane. Disabled members are
// "away" with an empty dot. Members who posted within 10 seconds are
// "talking"; within 30 seconds are "shipping" (when LastMessage looks
// like a tool call) or "plotting" otherwise; members with active
// LiveActivity are "talking" too. Idle members are "lurking".
func ClassifyActivity(m Member) MemberActivity {
	if m.Disabled {
		return MemberActivity{Label: "away", Color: DotIdle, Dot: "○"}
	}

	now := time.Now()
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

	if elapsed < 10*time.Second {
		return MemberActivity{Label: "talking", Color: DotTalking, Dot: "●"}
	}
	if elapsed < 30*time.Second {
		lower := strings.ToLower(m.LastMessage)
		for _, kw := range []string{"bash", "edit", "read", "write", "grep", "glob"} {
			if strings.Contains(lower, kw) {
				return MemberActivity{Label: "shipping", Color: DotCoding, Dot: "●"}
			}
		}
		return MemberActivity{Label: "plotting", Color: DotThinking, Dot: "●"}
	}
	if m.LiveActivity != "" {
		return MemberActivity{Label: "talking", Color: DotTalking, Dot: "●"}
	}

	return MemberActivity{Label: "lurking", Color: DotIdle, Dot: "●"}
}

// DefaultSidebarRoster returns the canonical eight-agent office
// roster used as a fallback when no broker-side roster is available.
func DefaultSidebarRoster() []Member {
	return []Member{
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

// RenderOfficeCharacter assembles the sidebar visual for a member —
// an ASCII avatar (with a "talking" frame swap every 250ms) plus a
// thought bubble derived from the member's slug, activity, and
// LastMessage.
func RenderOfficeCharacter(m Member, act MemberActivity, now time.Time) OfficeCharacter {
	talkFrame := 0
	if act.Label == "talking" {
		talkFrame = int(now.UnixNano()/250_000_000) % 2
	}
	portrait := avatar.RenderAvatar(m.Slug, talkFrame)
	bubble := OfficeAside(m.Slug, act.Label, m.LastMessage, now)
	return OfficeCharacter{Avatar: portrait, Bubble: bubble}
}

// OfficeAside picks a per-slug catchphrase for the thought bubble,
// falling back to a generic phrase when the slug:activity key is not
// in the table. Phrases rotate with a per-second phase offset so the
// sidebar feels alive without spamming a new phrase every tick. Some
// LastMessage keywords ("blocked", "launch", "design", "pricing")
// short-circuit to a topical line.
func OfficeAside(slug, activity, lastMessage string, now time.Time) string {
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

// ActiveSidebarTask picks the highest-priority in-flight task owned
// by slug. Status priority: in_progress > review > blocked > pending
// (claimed/pending/open). Returns the chosen task and whether one was
// found. Done/released tasks are skipped.
func ActiveSidebarTask(tasks []Task, slug string) (Task, bool) {
	bestScore := -1
	var best Task
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

// ApplyTaskActivity overrides the activity-derived MemberActivity
// with a task-status-specific one when the task signals work
// (working / reviewing / blocked / queued). Pending tasks fall
// through to the original act when it is already an active label.
func ApplyTaskActivity(act MemberActivity, task Task) MemberActivity {
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "in_progress":
		return MemberActivity{Label: "working", Color: DotCoding, Dot: "⚡"}
	case "review":
		return MemberActivity{Label: "reviewing", Color: DotThinking, Dot: "◆"}
	case "blocked":
		return MemberActivity{Label: "blocked", Color: "#DC2626", Dot: "●"}
	case "claimed", "pending", "open":
		if act.Label == "talking" || act.Label == "plotting" {
			return act
		}
		return MemberActivity{Label: "queued", Color: DotThinking, Dot: "◔"}
	default:
		return act
	}
}

// TaskBubbleText returns a sidebar-bubble line describing what the
// member is working on, derived from the task's status and title.
// Returns "" for empty title or unrecognized status.
func TaskBubbleText(task Task) string {
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

// RenderThoughtBubble renders a multi-line speech bubble using
// ▗ … ▖ … ▘ glyphs at the given width. Returns nil for empty input
// or width below 6 (the minimum at which the wrapping math is
// non-degenerate).
func RenderThoughtBubble(text string, width int) []string {
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

// PadSidebarContent right-pads text with spaces so its visible width
// matches width. Returns "" for non-positive width. When text is
// already at or beyond width it's returned unchanged (no truncation).
func PadSidebarContent(text string, width int) string {
	if width <= 0 {
		return ""
	}
	visibleWidth := ansi.StringWidth(text)
	if visibleWidth < width {
		text += strings.Repeat(" ", width-visibleWidth)
	}
	return text
}

// SidebarPlainRow returns a sidebar row prefixed with a single
// padding space and right-padded to fill width.
func SidebarPlainRow(text string, width int) string {
	return " " + PadSidebarContent(text, MaxInt(1, width-1))
}

// SidebarStyledRow renders text with the supplied lipgloss style at
// the given width. Used for section headers and dividers that need
// to fill the sidebar column.
func SidebarStyledRow(style lipgloss.Style, text string, width int) string {
	return style.Width(MaxInt(1, width)).Render(text)
}
