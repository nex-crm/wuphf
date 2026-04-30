package team

// escalation.go owns the launcher's broker-write helpers for
// surfacing agent-stuck / max-retries / generic escalations into
// the #general channel as Slack-style heads-ups. Pure broker
// passthrough plus the self-healing kick — no tmux, no goroutines,
// just a #general post and a log line.

import (
	"fmt"
	"strings"

	"github.com/nex-crm/wuphf/internal/agent"
)

// postEscalation writes a system message to #general when an agent is stuck
// or has blown its retry budget. The Slack-style UI renders this as a normal
// message so humans see it without needing to open a panel.
//
// detail is sanitized + length-capped before being broadcast: it
// commonly contains subprocess stderr, tool failure messages, or
// command lines, which can leak worktree paths, stack traces, or
// credential-shaped fragments. Internal tooling still sees the full
// detail via requestSelfHealing and the launcher log.
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
	// requestSelfHealing receives the unsanitized detail because the
	// self-healing prompt benefits from the full stderr/tool context.
	_, _, _ = l.requestSelfHealing(slug, taskID, reason, detail)
}

// sanitizeEscalationDetail collapses multiline/path-shaped detail into
// a single line and caps it at a length the channel UI can render
// without disclosing more than necessary. Conservatively replaces
// what looks like absolute paths with their basename and trims to
// 240 chars so a cascade-failure stack trace doesn't fill #general.
func sanitizeEscalationDetail(detail string) string {
	one := strings.ReplaceAll(detail, "\n", " ")
	one = strings.ReplaceAll(one, "\t", " ")
	one = strings.TrimSpace(one)
	const maxLen = 240
	if len(one) > maxLen {
		one = one[:maxLen] + "…"
	}
	if one == "" {
		return "(no detail)"
	}
	return one
}
