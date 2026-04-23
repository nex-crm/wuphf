package team

import (
	"fmt"
	"strings"
)

// recentHumanMessageLimit is the number of recent human messages to consider
// when building resume packets. The spec requires the last 50 messages.
const recentHumanMessageLimit = 50

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
// in-flight work. It combines the agent's assigned tasks (with worktree paths)
// and any unanswered human messages (with channel/reply_to routing instructions).
// Returns an empty string when there is nothing to resume.
func buildResumePacket(slug string, tasks []teamTask, msgs []channelMessage) string {
	if len(tasks) == 0 && len(msgs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Session resumed — picking up where you left off]\n\n")

	if len(tasks) > 0 {
		sb.WriteString("Active tasks:\n")
		for _, task := range tasks {
			sb.WriteString(fmt.Sprintf("- [%s] %s (status: %s)\n", task.ID, task.Title, task.Status))
			if task.Details != "" {
				sb.WriteString(fmt.Sprintf("  %s\n", task.Details))
			}
			if path := strings.TrimSpace(task.WorktreePath); path != "" {
				sb.WriteString(fmt.Sprintf("  Working directory: %s\n", path))
			}
		}
		sb.WriteString("\n")
	}

	if len(msgs) > 0 {
		sb.WriteString("Unanswered messages:\n")
		for _, msg := range msgs {
			channel := msg.Channel
			if channel == "" {
				channel = "general"
			}
			sb.WriteString(fmt.Sprintf("- @%s (channel: %q, reply_to_id: %q): %s\n", msg.From, channel, msg.ID, msg.Content))
		}
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("Reply using team_broadcast with my_slug %q and the channel and reply_to_id shown above.\n", slug))
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
// Only agents in the current pack receive packets. Agents not in the pack
// (e.g. removed members with leftover tasks) are silently skipped.
//
// Returns a map of agent slug → resume packet (empty strings are omitted).
func (l *Launcher) buildResumePackets() map[string]string {
	if l.broker == nil {
		return nil
	}

	// Build the set of valid office agent slugs from the live broker roster, not
	// the original launch pack. Dynamically created specialists must resume after
	// restart just like built-in members.
	officeSlugs := make(map[string]struct{})
	for _, member := range l.officeMembersSnapshot() {
		officeSlugs[member.Slug] = struct{}{}
	}
	inPack := func(slug string) bool {
		if len(officeSlugs) == 0 {
			return true // no roster loaded — allow all (nil-roster safety)
		}
		_, ok := officeSlugs[slug]
		return ok
	}

	// Determine office lead slug.
	lead := l.officeLeadSlug()

	// Collect in-flight tasks per owner — skip owners not in the pack.
	tasksByAgent := make(map[string][]teamTask)
	for _, task := range l.broker.InFlightTasks() {
		if !inPack(task.Owner) {
			continue
		}
		tasksByAgent[task.Owner] = append(tasksByAgent[task.Owner], task)
	}

	// Collect unanswered human messages.
	humanMsgs := l.broker.RecentHumanMessages(recentHumanMessageLimit)
	allMsgs := l.broker.AllMessages()
	unanswered := findUnansweredMessages(humanMsgs, allMsgs)

	// Route unanswered messages: explicit tags → tagged agents; untagged → lead.
	// Skip agents not in the current pack.
	msgsByAgent := make(map[string][]channelMessage)
	for _, msg := range unanswered {
		if len(msg.Tagged) > 0 {
			for _, tag := range msg.Tagged {
				slug := strings.TrimPrefix(tag, "@")
				// Skip human/you tags — those are not agents.
				if isHumanOrSystemSender(slug) {
					continue
				}
				if !inPack(slug) {
					continue
				}
				msgsByAgent[slug] = append(msgsByAgent[slug], msg)
			}
		} else {
			if lead != "" && inPack(lead) {
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
//   - Headless or non-pane provider: enqueueHeadlessCodexTurn
//   - Pane-eligible provider with a live pane target: sendNotificationToPane
//
// Routing is decided per agent so ProviderBinding overrides can mix pane-backed
// and headless runtimes in the same team. webMode alone is not sufficient: TUI
// mode now defaults to headless dispatch, and keying on webMode would send
// resume packets through agentPaneTargets() to pane indices that were never
// spawned, silently dropping resumption.
//
// In headless mode the lead is enqueued FIRST to avoid the queue-hold guard:
// enqueueHeadlessCodexTurn suppresses lead notifications when any specialist
// queue is non-empty. Enqueuing the lead before specialists ensures the lead's
// resume packet is not silently dropped at startup.
func (l *Launcher) resumeInFlightWork() {
	packets := l.buildResumePackets()
	if len(packets) == 0 {
		return
	}

	paneTargets := l.agentPaneTargets()
	routePacket := func(slug, packet string) {
		if l.memberUsesHeadlessOneShotRuntime(slug) || !l.paneBackedAgents {
			l.enqueueHeadlessCodexTurn(slug, packet)
			return
		}
		target, ok := paneTargets[slug]
		if !ok {
			l.enqueueHeadlessCodexTurn(slug, packet)
			return
		}
		launcherSendNotificationToPane(l, target.PaneTarget, packet)
	}

	lead := l.officeLeadSlug()
	if packet, ok := packets[lead]; ok {
		routePacket(lead, packet)
	}
	for slug, packet := range packets {
		if slug == lead {
			continue
		}
		routePacket(slug, packet)
	}
}
