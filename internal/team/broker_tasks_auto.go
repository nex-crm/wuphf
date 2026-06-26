package team

import (
	"fmt"
	"strings"
	"time"
)

// "Auto" owner triage.
//
// A task can be assigned to the "auto" sentinel instead of a concrete agent
// slug — the floor assignment when the user does not want to pick an owner
// themselves. "auto" is never a registered member; it means "the CEO should
// pick the best specialist." Resolution reuses the normal chat→notify→CEO loop:
// we post a human-authored, @ceo-tagged message in the task's channel asking
// the CEO to assign + start, and the CEO reassigns via its existing team_task
// tool. No bespoke dispatch wiring.

// isAutoOwner reports whether an owner/assignee value is the "auto" triage
// sentinel rather than a concrete agent slug.
func isAutoOwner(owner string) bool {
	return strings.EqualFold(strings.TrimSpace(owner), "auto")
}

// requestAutoAssignmentLocked asks the CEO to assign a real owner to an
// "auto"-assigned task. It posts a human-authored, @ceo-tagged message in the
// task's channel; the existing message-notification path wakes the CEO, which
// then reassigns the task to a specialist (setting a real owner → the task
// dispatches and runs). Caller must hold b.mu. `actor` is the task creator; a
// non-human / empty / auto actor falls back to "human" so the message routes
// (notifyAgentsLoop drops From=system posts).
func (b *Broker) requestAutoAssignmentLocked(task *teamTask, actor string) {
	if b == nil || task == nil {
		return
	}
	channel := normalizeChannelSlug(task.Channel)
	if channel == "" {
		channel = "general"
	}
	actor = strings.TrimSpace(actor)
	if actor == "" || isAutoOwner(actor) || !isHumanMessageSender(actor) {
		actor = "human"
	}
	title := strings.TrimSpace(task.Title)
	content := fmt.Sprintf(
		"@ceo Task %s (%s) needs an owner. Pick the best specialist for it and start them on it.",
		task.ID, title,
	)
	b.appendMessageLocked(channelMessage{
		From:      actor,
		Channel:   channel,
		Content:   content,
		Tagged:    []string{"ceo"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}
