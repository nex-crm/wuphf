package team

import (
	"fmt"
	"strings"
)

// findUnansweredMessages returns the subset of humanMsgs that have received no
// agent reply in allMessages. A human message is considered "answered" when at
// least one message in allMessages has ReplyTo set to that human message's ID.
func findUnansweredMessages(humanMsgs, allMessages []channelMessage) []channelMessage {
	// Build a set of human message IDs that have been replied to.
	replied := make(map[string]struct{})
	for _, msg := range allMessages {
		if msg.ReplyTo != "" {
			replied[msg.ReplyTo] = struct{}{}
		}
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
