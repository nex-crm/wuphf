package team

import (
	"fmt"
	"strings"
)

// recentHumanMessageLimit is the number of recent human messages to consider
// when building resume packets.
const recentHumanMessageLimit = 10

// isHumanOrSystemSender reports whether a message sender is a human or system
// source (not an agent). Only agent replies count as "answers".
func isHumanOrSystemSender(from string) bool {
	f := strings.ToLower(strings.TrimSpace(from))
	return f == "you" || f == "human" || f == "nex" || f == "system" || f == ""
}

// findUnansweredMessages returns the subset of humanMsgs that have received no
// agent reply in allMessages. A human message is considered "answered" only when
// at least one AGENT message (not human/nex/system) in allMessages has ReplyTo
// set to that human message's ID.
func findUnansweredMessages(humanMsgs, allMessages []channelMessage) []channelMessage {
	// Build a set of human message IDs that have been replied to by agents.
	// Skip replies from human/nex/system senders — only agent replies count.
	replied := make(map[string]struct{})
	for _, msg := range allMessages {
		if msg.ReplyTo == "" {
			continue
		}
		if isHumanOrSystemSender(msg.From) {
			continue
		}
		replied[msg.ReplyTo] = struct{}{}
	}

	var out []channelMessage
	for _, hm := range humanMsgs {
		if _, ok := replied[hm.ID]; !ok {
			out = append(out, hm)
		}
	}
	return out
}

// buildResumePacket constructs a context string that an agent can use to resume
// in-flight work. It combines the agent's assigned tasks and any unanswered human
// messages directed at them. Returns an empty string when there is nothing to resume.
func buildResumePacket(slug string, tasks []teamTask, msgs []channelMessage) string {
	if len(tasks) == 0 && len(msgs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("You are @%s. Here is your in-flight work to resume:\n\n", slug))

	if len(tasks) > 0 {
		sb.WriteString("## Your assigned tasks\n\n")
		for _, task := range tasks {
			sb.WriteString(fmt.Sprintf("- [%s] %s (status: %s)\n", task.ID, task.Title, task.Status))
			if task.Details != "" {
				sb.WriteString(fmt.Sprintf("  %s\n", task.Details))
			}
		}
		sb.WriteString("\n")
	}

	if len(msgs) > 0 {
		sb.WriteString("## Unanswered messages awaiting your response\n\n")
		for _, msg := range msgs {
			sb.WriteString(fmt.Sprintf("- @%s: %s\n", msg.From, msg.Content))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Please pick up where you left off.\n")
	return sb.String()
}

// buildResumePackets scans the broker for in-flight tasks and unanswered
// human messages, then builds a resume packet per agent. Routing:
//   - tasks: routed to their owner slug
//   - tagged messages: each tagged agent receives the message
//   - untagged messages: the pack lead receives the message
//
// Returns a map of agent slug → resume packet (empty strings are omitted).
func (l *Launcher) buildResumePackets() map[string]string {
	if l.broker == nil {
		return nil
	}

	// Determine pack lead slug.
	lead := l.officeLeadSlug()

	// Collect in-flight tasks per owner.
	tasksByAgent := make(map[string][]teamTask)
	for _, task := range l.broker.InFlightTasks() {
		tasksByAgent[task.Owner] = append(tasksByAgent[task.Owner], task)
	}

	// Collect unanswered human messages.
	humanMsgs := l.broker.RecentHumanMessages(recentHumanMessageLimit)
	allMsgs := l.broker.AllMessages()
	unanswered := findUnansweredMessages(humanMsgs, allMsgs)

	// Route unanswered messages: explicit tags → tagged agents; untagged → lead.
	msgsByAgent := make(map[string][]channelMessage)
	for _, msg := range unanswered {
		if len(msg.Tagged) > 0 {
			for _, tag := range msg.Tagged {
				slug := strings.TrimPrefix(tag, "@")
				// Skip human/you tags — those are not agents.
				if slug == "you" || slug == "human" {
					continue
				}
				msgsByAgent[slug] = append(msgsByAgent[slug], msg)
			}
		} else {
			if lead != "" {
				msgsByAgent[lead] = append(msgsByAgent[lead], msg)
			}
		}
	}

	// Build packets — include an agent only if they have tasks or messages.
	allSlugs := make(map[string]struct{})
	for slug := range tasksByAgent {
		allSlugs[slug] = struct{}{}
	}
	for slug := range msgsByAgent {
		allSlugs[slug] = struct{}{}
	}

	packets := make(map[string]string)
	for slug := range allSlugs {
		packet := buildResumePacket(slug, tasksByAgent[slug], msgsByAgent[slug])
		if packet != "" {
			packets[slug] = packet
		}
	}
	return packets
}

// resumeInFlightWork builds resume packets for all agents with pending work and
// delivers them via the appropriate runtime:
//   - Headless (Codex / web mode): enqueueHeadlessCodexTurn
//   - tmux: sendNotificationToPane
func (l *Launcher) resumeInFlightWork() {
	packets := l.buildResumePackets()
	if len(packets) == 0 {
		return
	}

	if l.usesCodexRuntime() || l.webMode {
		for slug, packet := range packets {
			l.enqueueHeadlessCodexTurn(slug, packet)
		}
		return
	}

	// tmux path — need pane targets.
	paneTargets := l.agentPaneTargets()
	for slug, packet := range packets {
		target, ok := paneTargets[slug]
		if !ok {
			continue
		}
		l.sendNotificationToPane(target.PaneTarget, packet)
	}
}
