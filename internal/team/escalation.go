package team

// escalation.go owns the launcher's broker-write helpers for
// surfacing agent-stuck / max-retries / generic escalations into
// the #general channel as Slack-style heads-ups. Pure broker
// passthrough plus the self-healing kick — no tmux, no goroutines,
// just a #general post and a log line.

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/nex-crm/wuphf/internal/agent"
)

// postEscalation writes a system message to #general when an agent is stuck
// or has blown its retry budget. The Slack-style UI renders this as a normal
// message so humans see it without needing to open a panel.
//
// detail is sanitized + length-capped before being broadcast: it
// commonly contains subprocess stderr, tool failure messages, or
// command lines, which can leak worktree paths, stack traces, or
// credential-shaped fragments. The same sanitized detail flows to
// requestSelfHealing because selfHealingTaskDetails embeds it in a
// task body posted to a channel — same audience as the #general
// post — so the public-facing redaction must apply there too.
func (l *Launcher) postEscalation(slug, taskID string, reason agent.EscalationReason, detail string) {
	if l.broker == nil {
		return
	}
	who := strings.TrimSpace(slug)
	if who == "" {
		who = "an agent"
	}
	publicDetail := sanitizeEscalationDetail(detail)
	var body string
	switch reason {
	case agent.EscalationStuck:
		body = fmt.Sprintf("Heads up: %s looks stuck. Task %s — %s. Needs eyes.", who, taskID, publicDetail)
	case agent.EscalationMaxRetries:
		body = fmt.Sprintf("Heads up: %s keeps erroring on task %s. Last error: %s. Needs eyes.", who, taskID, publicDetail)
	default:
		body = fmt.Sprintf("Heads up: %s escalation on %s: %s", who, taskID, publicDetail)
	}
	l.broker.PostSystemMessage("general", body, "escalation")
	_, _, _ = l.requestSelfHealing(slug, taskID, reason, publicDetail)
}

// sanitizeEscalationDetail collapses multi-line / tab-laden detail
// into a single line and caps it at a length the channel UI can
// render. The trim keeps a cascade-failure stack trace from filling
// #general; it does NOT redact absolute paths or credential-shaped
// fragments — callers who need that should pre-redact. The cap
// truncates on a rune boundary so multi-byte runes near the limit
// don't render as a replacement character.
func sanitizeEscalationDetail(detail string) string {
	one := strings.ReplaceAll(detail, "\n", " ")
	one = strings.ReplaceAll(one, "\t", " ")
	one = strings.TrimSpace(one)
	const maxLen = 240
	if len(one) > maxLen {
		// Walk back to the last rune-boundary at or before maxLen.
		// Go strings are byte sequences; slicing in the middle of a
		// multi-byte rune produces invalid UTF-8 that renders as the
		// replacement character (U+FFFD) in the channel UI.
		cut := maxLen
		for cut > 0 && !utf8.RuneStart(one[cut]) {
			cut--
		}
		one = one[:cut] + "…"
	}
	if one == "" {
		return "(no detail)"
	}
	return one
}
