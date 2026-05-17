package team

import (
	"fmt"
	"strings"
	"time"
)

// postTaskReassignNotificationsLocked posts the channel announcement plus DMs
// to the new owner and previous owner whenever a task ownership change happens.
// The CEO is tagged in the channel message rather than DM'd (CEO is the human
// user; human↔ceo self-DM is not a valid DM target).
//
// Must be called while b.mu is held for write.
func (b *Broker) postTaskReassignNotificationsLocked(actor string, task *teamTask, prevOwner string) {
	if task == nil {
		return
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	newOwner := strings.TrimSpace(task.Owner)
	prevOwner = strings.TrimSpace(prevOwner)
	if newOwner == prevOwner {
		return
	}
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	now := time.Now().UTC().Format(time.RFC3339)

	newLabel := "(unassigned)"
	if newOwner != "" {
		newLabel = "@" + newOwner
	}
	prevLabel := "(unassigned)"
	if prevOwner != "" {
		prevLabel = "@" + prevOwner
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      actor,
		Channel:   taskChannel,
		Kind:      "task_reassigned",
		Title:     title,
		Content:   fmt.Sprintf("Task %q reassigned: %s → %s. (by @%s, cc @ceo)", title, prevLabel, newLabel, actor),
		Tagged:    dedupeReassignTags([]string{"ceo", newOwner, prevOwner}),
		Timestamp: now,
	})

	if isDMTargetSlug(newOwner) {
		b.postTaskDMLocked(actor, newOwner, "task_reassigned", title,
			fmt.Sprintf("Task %q is yours now. Details live in #%s.", title, taskChannel))
	}
	if isDMTargetSlug(prevOwner) && prevOwner != newOwner {
		b.postTaskDMLocked(actor, prevOwner, "task_reassigned", title,
			fmt.Sprintf("Task %q is off your plate — it moved to %s.", title, newLabel))
	}
}

// postTaskCancelNotificationsLocked posts a channel announcement plus a DM
// to the (previous) owner whenever a task is closed as "won't do".
// Must be called while b.mu is held for write.
func (b *Broker) postTaskCancelNotificationsLocked(actor string, task *teamTask, prevOwner string) {
	if task == nil {
		return
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	prevOwner = strings.TrimSpace(prevOwner)
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	now := time.Now().UTC().Format(time.RFC3339)

	ownerLabel := "(no owner)"
	if prevOwner != "" {
		ownerLabel = "@" + prevOwner
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      actor,
		Channel:   taskChannel,
		Kind:      "task_canceled",
		Title:     title,
		Content:   fmt.Sprintf("Task %q closed as won't do. Owner was %s. (by @%s, cc @ceo)", title, ownerLabel, actor),
		Tagged:    dedupeReassignTags([]string{"ceo", prevOwner}),
		Timestamp: now,
	})

	if isDMTargetSlug(prevOwner) {
		b.postTaskDMLocked(actor, prevOwner, "task_canceled", title,
			fmt.Sprintf("Heads up — task %q was closed as won't do. Take it off your list.", title))
	}
}

// postTaskDMLocked appends a direct-message notification to the DM channel
// between "human" and targetSlug, creating the channel if necessary.
// Must be called while b.mu is held for write.
func (b *Broker) postTaskDMLocked(from, targetSlug, kind, title, content string) {
	targetSlug = strings.TrimSpace(targetSlug)
	if targetSlug == "" || b.channelStore == nil {
		return
	}
	ch, err := b.channelStore.GetOrCreateDirect("human", targetSlug)
	if err != nil {
		return
	}
	if b.findChannelLocked(ch.Slug) == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		b.channels = append(b.channels, teamChannel{
			Slug:        ch.Slug,
			Name:        ch.Slug,
			Type:        "dm",
			Description: "Direct messages with " + targetSlug,
			Members:     []string{"human", targetSlug},
			CreatedBy:   "wuphf",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      strings.TrimSpace(from),
		Channel:   ch.Slug,
		Kind:      strings.TrimSpace(kind),
		Title:     title,
		Content:   content,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// isDMTargetSlug reports whether slug is a valid recipient for a human-to-agent DM.
// The human user ("human"/"you") and the CEO seat ("ceo", which is the human)
// are excluded because they would create self-DMs.
func isDMTargetSlug(slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return false
	}
	switch slug {
	case "human", "you", "ceo":
		return false
	}
	return true
}

// postTaskRequestChangesNotificationsLocked posts the channel announcement
// plus a DM to the owner whenever a reviewer bounces a task back with
// "request_changes". This is the PR-review rebound: the reviewer's feedback
// (passed via the mutation's Details) reaches the owner so they can revise
// and resubmit. Must be called while b.mu is held for write.
func (b *Broker) postTaskRequestChangesNotificationsLocked(actor string, task *teamTask, feedback string) {
	if task == nil {
		return
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	owner := strings.TrimSpace(task.Owner)
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	feedback = strings.TrimSpace(feedback)
	excerpt := feedback
	if len(excerpt) > 320 {
		excerpt = excerpt[:317] + "..."
	}
	ownerLabel := "(unassigned)"
	if owner != "" {
		ownerLabel = "@" + owner
	}
	body := fmt.Sprintf("🔁 Changes requested on %s %q by @%s — bounced back to %s. Revise per feedback, then call team_task action=submit_for_review.",
		task.ID, title, actor, ownerLabel)
	if excerpt != "" {
		body += "\n\nReviewer feedback:\n" + excerpt
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      actor,
		Channel:   taskChannel,
		Kind:      "task_changes_requested",
		Title:     title,
		Content:   body,
		Tagged:    dedupeReassignTags([]string{owner, "ceo"}),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if isDMTargetSlug(owner) {
		dmBody := fmt.Sprintf("Reviewer @%s requested changes on %s %q. Read the feedback in #%s, revise, and call team_task action=submit_for_review when ready.",
			actor, task.ID, title, taskChannel)
		if excerpt != "" {
			dmBody += "\n\nFeedback:\n" + excerpt
		}
		b.postTaskDMLocked(actor, owner, "task_changes_requested", title, dmBody)
	}
}

// postTaskRejectedNotificationsLocked posts a channel announcement and
// a DM to the owner when a reviewer rejects work outright (terminal,
// not "fix and resubmit"). Unlike request_changes, downstream tasks
// stay blocked permanently. Must be called while b.mu is held for write.
func (b *Broker) postTaskRejectedNotificationsLocked(actor string, task *teamTask, feedback string) {
	if task == nil {
		return
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	owner := strings.TrimSpace(task.Owner)
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	feedback = strings.TrimSpace(feedback)
	excerpt := feedback
	if len(excerpt) > 320 {
		excerpt = excerpt[:317] + "..."
	}
	ownerLabel := "(unassigned)"
	if owner != "" {
		ownerLabel = "@" + owner
	}
	body := fmt.Sprintf("🚫 %s %q rejected by @%s — terminal. Dependent tasks stay blocked. Owner: %s.",
		task.ID, title, actor, ownerLabel)
	if excerpt != "" {
		body += "\n\nRejection reason:\n" + excerpt
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      actor,
		Channel:   taskChannel,
		Kind:      "task_rejected",
		Title:     title,
		Content:   body,
		Tagged:    dedupeReassignTags([]string{owner, "ceo"}),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	if isDMTargetSlug(owner) {
		dmBody := fmt.Sprintf("Reviewer @%s rejected %s %q. This is terminal — the work won't land. Read the reason in #%s.",
			actor, task.ID, title, taskChannel)
		if excerpt != "" {
			dmBody += "\n\nReason:\n" + excerpt
		}
		b.postTaskDMLocked(actor, owner, "task_rejected", title, dmBody)
	}
}

func dedupeReassignTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}
