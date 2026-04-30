package channelui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TrimRecoverySentence trims whitespace and a single trailing period
// off a recovery summary fragment so concatenated sentences ("Focus.
// Next: …") read cleanly.
func TrimRecoverySentence(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, ".")
	return text
}

// RenderAwayStrip renders the "While away · N new · /recover" pill at
// the top of the office feed when there are unread messages or a
// recovery summary. summary is folded into the label when set; the
// label is truncated to fit width.
func RenderAwayStrip(width, unreadCount int, summary string) string {
	label := fmt.Sprintf("While away · %d new · /recover", unreadCount)
	if strings.TrimSpace(summary) != "" {
		label = fmt.Sprintf("While away · %s · /recover", strings.TrimSpace(summary))
	}
	label = TruncateText(label, MaxInt(24, width-6))
	return "  " + lipgloss.NewStyle().
		Foreground(lipgloss.Color("#0F172A")).
		Background(lipgloss.Color("#BFDBFE")).
		Padding(0, 1).
		Bold(true).
		Render(label)
}

// RecoverySurgeryOption is one of the suggested "click to draft this
// recovery action" cards rendered under the recovery summary. Prompt
// is the suggested composer prefill when the card is clicked.
type RecoverySurgeryOption struct {
	Tag    string
	Title  string
	Body   string
	Accent string
	Extra  []string
	Prompt string
}

// BuildRecoverySurgeryOptions picks up to two open requests, two
// active tasks, and two recent threads and packages each into a
// RecoverySurgeryOption suitable for rendering. The caller decides
// how (and whether) to render them.
func BuildRecoverySurgeryOptions(tasks []Task, requests []Interview, messages []BrokerMessage) []RecoverySurgeryOption {
	options := make([]RecoverySurgeryOption, 0, 6)

	for _, req := range requests {
		if !IsOpenInterviewStatus(req.Status) {
			continue
		}
		options = append(options, RecoverySurgeryOption{
			Tag:    "decision brief",
			Title:  "Draft the decision context for " + req.ID,
			Body:   FallbackString(strings.TrimSpace(req.Context), req.TitleOrQuestion()),
			Accent: "#B45309",
			Extra:  []string{"Request " + req.ID, "Asked by @" + FallbackString(req.From, "unknown")},
			Prompt: BuildRecoveryPromptForRequest(req),
		})
		if len(options) >= 2 {
			break
		}
	}

	taskCount := 0
	for _, task := range RecoveryActiveTasks(tasks, 3) {
		options = append(options, RecoverySurgeryOption{
			Tag:    "task handoff",
			Title:  "Restore context for " + task.ID,
			Body:   FallbackString(strings.TrimSpace(task.Details), task.Title),
			Accent: "#2563EB",
			Extra:  []string{"Owner @" + FallbackString(task.Owner, "unowned"), "Status " + FallbackString(strings.TrimSpace(task.Status), "open")},
			Prompt: BuildRecoveryPromptForTask(task),
		})
		taskCount++
		if taskCount >= 2 {
			break
		}
	}

	threadCount := 0
	for _, msg := range RecoveryRecentThreads(messages, 3) {
		options = append(options, RecoverySurgeryOption{
			Tag:    "rewind",
			Title:  "Summarize everything since " + msg.ID,
			Body:   TruncateText(strings.TrimSpace(msg.Content), 160),
			Accent: "#475569",
			Extra:  []string{"Thread " + msg.ID, "Started by @" + FallbackString(msg.From, "unknown")},
			Prompt: BuildRecoveryPromptForMessage(msg),
		})
		threadCount++
		if threadCount >= 2 {
			break
		}
	}

	return options
}

// BuildRecoveryPromptForMessage builds the composer prefill for the
// "rewind" surgery option — asks the assistant to summarize everything
// since the message, focusing on decisions, blocked work, owner
// changes, risks, and next actions.
func BuildRecoveryPromptForMessage(msg BrokerMessage) string {
	return fmt.Sprintf("Summarize everything since %s from @%s, focusing on decisions, blocked work, owner changes, risks, and the next concrete actions. Include what a human needs to know before replying. Message context: %s", msg.ID, FallbackString(msg.From, "unknown"), TruncateText(strings.TrimSpace(msg.Content), 120))
}

// BuildRecoveryPromptForRequest builds the composer prefill for the
// "decision brief" surgery option — asks the assistant to draft the
// arguments, blockers, recommendation, risks, and next action.
func BuildRecoveryPromptForRequest(req Interview) string {
	return fmt.Sprintf("Draft a decision brief for request %s (%s). Summarize the arguments so far, what is blocked, the recommendation, open risks, and the smallest next action after the human answers.", req.ID, req.TitleOrQuestion())
}

// BuildRecoveryPromptForTask builds the composer prefill for the
// "task handoff" surgery option — asks the assistant to restore
// context with status, work done, blockers, thread context, review
// state, and next move.
func BuildRecoveryPromptForTask(task Task) string {
	return fmt.Sprintf("Restore context for task %s (%s). Draft a clean handoff note with current status, work already done, blockers, linked thread context, review state, and the next best move.", task.ID, task.Title)
}

// RenderRecoveryActionCard renders a left-bordered recovery card with
// a header row, optional body row, and any extra muted lines. accent
// drives the border color. Returns the rendered card string ready to
// be split by RenderedCardLines.
func RenderRecoveryActionCard(contentWidth int, header, body, accent string, extra []string) string {
	cardWidth := MaxInt(24, contentWidth-6)
	parts := []string{header}
	if strings.TrimSpace(body) != "" {
		parts = append(parts, MutedText(body))
	}
	for _, line := range extra {
		if strings.TrimSpace(line) != "" {
			parts = append(parts, MutedText(line))
		}
	}
	return lipgloss.NewStyle().
		Width(cardWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(accent)).
		Background(lipgloss.Color("#16181E")).
		Padding(0, 1).
		Render(strings.Join(parts, "\n"))
}

// PrefixedCardLines returns a copy of lines with prefix prepended to
// each Text. Used to indent recovery cards under their section
// header without mutating the input slice.
func PrefixedCardLines(lines []RenderedLine, prefix string) []RenderedLine {
	out := make([]RenderedLine, 0, len(lines))
	for _, line := range lines {
		line.Text = prefix + line.Text
		out = append(out, line)
	}
	return out
}

// RecoveryActiveTasks filters tasks to those still in flight (status
// is not done/completed/canceled and not empty), sorted newest-first
// by UpdatedAt and capped to limit. limit <= 0 keeps all matches.
func RecoveryActiveTasks(tasks []Task, limit int) []Task {
	filtered := make([]Task, 0, len(tasks))
	for _, task := range tasks {
		switch strings.ToLower(strings.TrimSpace(task.Status)) {
		case "", "done", "completed", "canceled", "cancelled":
			continue
		default:
			filtered = append(filtered, task)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		left, lok := ParseChannelTime(filtered[i].UpdatedAt)
		right, rok := ParseChannelTime(filtered[j].UpdatedAt)
		switch {
		case lok && rok:
			return left.After(right)
		case lok:
			return true
		case rok:
			return false
		default:
			return filtered[i].ID > filtered[j].ID
		}
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

// RecoveryRecentThreads returns up to limit thread roots from the
// most recent messages (newest-first). Roots are messages that
// either have replies or are themselves a reply (so a fresh
// standalone message doesn't qualify). Each root is included at most
// once.
func RecoveryRecentThreads(messages []BrokerMessage, limit int) []BrokerMessage {
	roots := []BrokerMessage{}
	seen := map[string]bool{}
	for i := len(messages) - 1; i >= 0 && len(roots) < limit; i-- {
		msg := messages[i]
		rootID := ThreadRootMessageID(messages, msg.ID)
		if rootID == "" || seen[rootID] {
			continue
		}
		if !HasThreadReplies(messages, rootID) && strings.TrimSpace(msg.ReplyTo) == "" {
			continue
		}
		root, ok := FindMessageByID(messages, rootID)
		if !ok {
			continue
		}
		roots = append(roots, root)
		seen[rootID] = true
	}
	return roots
}
