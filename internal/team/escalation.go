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
func (l *Launcher) postEscalation(slug, taskID string, reason agent.EscalationReason, detail string) {
	if l.broker == nil {
		return
	}
	who := strings.TrimSpace(slug)
	if who == "" {
		who = "an agent"
	}
	var body string
	switch reason {
	case agent.EscalationStuck:
		body = fmt.Sprintf("Heads up: %s looks stuck. Task %s — %s. Needs eyes.", who, taskID, detail)
	case agent.EscalationMaxRetries:
		body = fmt.Sprintf("Heads up: %s keeps erroring on task %s. Last error: %s. Needs eyes.", who, taskID, detail)
	default:
		body = fmt.Sprintf("Heads up: %s escalation on %s: %s", who, taskID, detail)
	}
	l.broker.PostSystemMessage("general", body, "escalation")
	_, _, _ = l.requestSelfHealing(slug, taskID, reason, detail)
}
