package team

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// issueMentionRegex matches @slug tokens in comment bodies. Slugs
// follow the same shape used elsewhere in the broker — lowercase
// letters, digits, and hyphens — and must be followed by a non-word
// boundary so we don't accidentally pick up "@example.com" emails.
var issueMentionRegex = regexp.MustCompile(`(?i)(?:^|[\s,.;:!?(\[])@([a-z][a-z0-9-]{1,40})\b`)

// parseAtMentions extracts unique @slug mentions from a comment body
// in left-to-right order. Used to wake the right agents when a human
// comments on an Issue. Returns lowercased slugs.
func parseAtMentions(body string) []string {
	matches := issueMentionRegex.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		slug := strings.ToLower(strings.TrimSpace(m[1]))
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	return out
}

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

// postIssueCreatedCardLocked emits a system-authored chat message that
// renders as an issue card in the channel where the Issue was filed.
// The card is the audit-trail anchor for "any work getting done should
// have an issue in place" — the human (and other agents in the channel)
// see the new Issue as soon as it lands, with a one-click link into the
// Issue detail view. The agent that called team_task can still post its
// own chat reply; this card is independent.
//
// Only called when task_type=issue (other types are internal and do not
// surface to the user). Must be called while b.mu is held for write.
func (b *Broker) postIssueCreatedCardLocked(actor string, task *teamTask) {
	if task == nil {
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
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	payload := map[string]string{
		"task_id":         task.ID,
		"title":           title,
		"owner":           strings.TrimSpace(task.Owner),
		"channel":         taskChannel,
		"lifecycle_state": string(task.LifecycleState),
		"created_by":      actor,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   taskChannel,
		Kind:      "issue_created",
		Title:     title,
		Content:   fmt.Sprintf("Issue created: %s — %s", task.ID, title),
		Tagged:    dedupeReassignTags([]string{"ceo", strings.TrimSpace(task.Owner), actor}),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		// ReplyTo folds the card into the originating chat thread so
		// subsequent messages in that thread appear after the card
		// rather than the card visually floating "above" newer chat
		// activity in its own top-level slot. task.ThreadID is set at
		// create time from the agent's MCP call.
		ReplyTo:      strings.TrimSpace(task.ThreadID),
		SourceTaskID: task.ID,
		Payload:      raw,
	})
}

// postIssueCommentBroadcastLocked emits a channel message when a human
// (or any actor) leaves a PR-style comment on an Issue via
// POST /tasks/{id}/comment. Without this, the comment lands on the
// packet feedback log but no agent loop ever wakes up to read it.
//
// Wake rules (locked 2026-05-26):
//   - Untagged comments → CEO is woken to reply.
//   - Tagged comments → every @mentioned agent + CEO are woken.
//
// CEO is always in the tagged list so the founder's voice never gets a
// "no one is listening" comment. Tag dedupe + ordering follows the
// existing dedupeReassignTags helper.
//
// Content shape (load-bearing): the broker emits an instructional brief
// the woken agent reads — "reply via team_task action=comment, do NOT
// change lifecycle state" — INSTEAD of the raw comment body. Without
// this hint, agents historically interpreted the comment text as a
// chat directive and started executing work on un-approved Issues
// rather than answering the comment thread. The full body still lives
// on the packet feedback log (AppendPacketFeedbackLocked, called by
// the handler before this) and in the structured Payload below so the
// FE card can show the excerpt.
//
// Must be called while b.mu is held for write.
func (b *Broker) postIssueCommentBroadcastLocked(actor string, task *teamTask, body string) {
	if task == nil {
		return
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "human"
	}
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	owner := strings.TrimSpace(task.Owner)
	lifecycleState := string(task.LifecycleState)

	// Build the tagged list. Always include CEO + the Issue owner so
	// they see clarifications even when no one @-mentioned them. Add
	// any @slug parsed from the body so multi-agent threads work.
	tagged := []string{"ceo"}
	if owner != "" {
		tagged = append(tagged, owner)
	}
	tagged = append(tagged, parseAtMentions(body)...)
	tagged = dedupeReassignTags(tagged)

	excerpt := body
	if len(excerpt) > 500 {
		excerpt = strings.TrimSpace(excerpt[:500]) + "…"
	}

	payload := map[string]string{
		"task_id":         task.ID,
		"title":           title,
		"owner":           owner,
		"channel":         taskChannel,
		"lifecycle_state": lifecycleState,
		"author":          actor,
		"excerpt":         excerpt,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}

	// Instructional brief the woken agent reads. Tells them WHERE to
	// reply (the Issue's comment thread, not chat) and explicitly
	// forbids lifecycle changes from this signal alone. The actual
	// comment text is structured under Payload + on the packet, so
	// the agent can fetch it deliberately when ready to reply.
	stateHint := ""
	if lifecycleState != "" {
		stateHint = fmt.Sprintf(" (state: %s)", lifecycleState)
	}
	authorLabel := actor
	if authorLabel == "" {
		authorLabel = "Someone"
	}
	content := fmt.Sprintf(
		"@%s commented on Issue %s%s — %s.\n"+
			"Reply via team_task action=comment on this Issue. "+
			"Do NOT change its lifecycle state from this comment alone.",
		authorLabel,
		task.ID,
		stateHint,
		title,
	)

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   taskChannel,
		Kind:      "issue_comment",
		Title:     title,
		Content:   content,
		Tagged:    tagged,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		// Fold into the Issue's originating thread (see postIssueCreatedCardLocked).
		ReplyTo:      strings.TrimSpace(task.ThreadID),
		SourceTaskID: task.ID,
		Payload:      raw,
	})
}

// IssueLifecycleTransition is the small enum the FE renders against for
// the per-transition copy ("Approved & started", "Done", "Closed", etc.).
// Keep these stable — the FE switches on the exact strings.
type IssueLifecycleTransition string

const (
	IssueLifecycleTransitionStarted    IssueLifecycleTransition = "started"
	IssueLifecycleTransitionInReview   IssueLifecycleTransition = "in_review"
	IssueLifecycleTransitionApproved   IssueLifecycleTransition = "approved"
	IssueLifecycleTransitionRejected   IssueLifecycleTransition = "rejected"
	IssueLifecycleTransitionBlocked    IssueLifecycleTransition = "blocked"
	IssueLifecycleTransitionNeedsInput IssueLifecycleTransition = "needs_input"
	IssueLifecycleTransitionRevising   IssueLifecycleTransition = "revising"
	IssueLifecycleTransitionGeneric    IssueLifecycleTransition = "generic"
)

// classifyIssueLifecycleTransition reduces a from→to LifecycleState pair
// into the small UI-facing kind the FE switches on. Anything that doesn't
// match a known transition lands in "generic" so the FE always has
// something to render (the underlying from/to are also in the payload so
// the user still sees what changed).
func classifyIssueLifecycleTransition(from, to LifecycleState) IssueLifecycleTransition {
	switch {
	case from == LifecycleStateDrafting && to == LifecycleStateRunning:
		return IssueLifecycleTransitionStarted
	case to == LifecycleStateReview || to == LifecycleStateDecision:
		return IssueLifecycleTransitionInReview
	case to == LifecycleStateApproved:
		return IssueLifecycleTransitionApproved
	case to == LifecycleStateRejected:
		return IssueLifecycleTransitionRejected
	case to == LifecycleStateBlockedOnPRMerge:
		return IssueLifecycleTransitionBlocked
	case to == LifecycleStateChangesRequested:
		return IssueLifecycleTransitionRevising
	}
	return IssueLifecycleTransitionGeneric
}

// postIssueLifecycleCardLocked emits a system-authored chat card whenever
// an Issue's lifecycle state transitions in a way the human should see —
// most importantly Drafting → Running ("Approved & started — @owner on
// it") so the human knows the owner woke up. The card also doubles as
// the wake signal: tagging the owner in `tagged` causes the agent loop
// to pick this up as a notification on its next tick.
//
// Only emitted for task_type=issue. Caller holds b.mu for write.
// Skip when from == to to avoid empty no-op cards on idempotent writes.
func (b *Broker) postIssueLifecycleCardLocked(task *teamTask, from, to LifecycleState, actor string) {
	if task == nil {
		return
	}
	if !strings.EqualFold(task.TaskType, "issue") {
		return
	}
	if from == to {
		return
	}
	transition := classifyIssueLifecycleTransition(from, to)
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	owner := strings.TrimSpace(task.Owner)
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}

	payload := map[string]string{
		"task_id":    task.ID,
		"title":      title,
		"owner":      owner,
		"channel":    taskChannel,
		"from_state": string(from),
		"to_state":   string(to),
		"transition": string(transition),
		"actor":      actor,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}

	// Always wake the owner on important lifecycle changes — even when
	// the actor IS the owner (self-transitions on submit_for_review /
	// complete still want a chat trace). CEO is included so the
	// coordination view stays in sync.
	tagged := []string{"ceo"}
	if owner != "" {
		tagged = append(tagged, owner)
	}
	tagged = dedupeReassignTags(tagged)

	human := issueLifecycleHumanLine(transition, task.ID, title, owner)

	// Coalesce: if a recent (<10s) issue_lifecycle card for the SAME
	// task is still in the message log, drop it before appending the
	// new card. This collapses multi-step flows that a user perceives
	// as ONE action (e.g. Approve & Start fires Drafting→Approved
	// AND Approved→Running back-to-back). Without this, the channel
	// shows two lifecycle cards for one click. The 10s window is
	// conservative — typical multi-step flows complete in under 1s.
	b.coalesceRecentLifecycleCardLocked(task.ID, 10*time.Second)

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   taskChannel,
		Kind:      "issue_lifecycle",
		Title:     title,
		Content:   human,
		Tagged:    tagged,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		// Fold into the Issue's originating thread (see postIssueCreatedCardLocked).
		ReplyTo:      strings.TrimSpace(task.ThreadID),
		SourceTaskID: task.ID,
		Payload:      raw,
	})
}

// coalesceRecentLifecycleCardLocked removes any issue_lifecycle card
// for taskID emitted within the last `window`. Used to collapse a
// burst of lifecycle transitions (e.g. Drafting→Approved→Running on
// a single Approve & Start click) into the most recent card, so the
// channel does not stack redundant cards for what a user reads as one
// action.
//
// Walks newest-first and stops at the first message older than the
// window. Returns the number of cards removed.
//
// Caller must hold b.mu for write.
func (b *Broker) coalesceRecentLifecycleCardLocked(taskID string, window time.Duration) int {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || len(b.messages) == 0 {
		return 0
	}
	cutoff := time.Now().UTC().Add(-window)
	removed := 0
	for i := len(b.messages) - 1; i >= 0; i-- {
		msg := b.messages[i]
		ts, err := time.Parse(time.RFC3339, msg.Timestamp)
		if err == nil && ts.Before(cutoff) {
			break
		}
		if msg.Kind != "issue_lifecycle" {
			continue
		}
		if strings.TrimSpace(msg.SourceTaskID) != taskID {
			continue
		}
		b.messages = append(b.messages[:i], b.messages[i+1:]...)
		removed++
	}
	return removed
}

// issueLifecycleHumanLine renders the plain-text fallback that shows in
// channels that don't render the card (and as the notification preview).
// The FE prefers the structured payload via IssueLifecycleCard.
func issueLifecycleHumanLine(transition IssueLifecycleTransition, taskID, title, owner string) string {
	ownerTag := owner
	if ownerTag == "" {
		ownerTag = "no one"
	} else {
		ownerTag = "@" + ownerTag
	}
	switch transition {
	case IssueLifecycleTransitionStarted:
		return fmt.Sprintf("Approved — %s starting work on %s: %s", ownerTag, taskID, title)
	case IssueLifecycleTransitionInReview:
		return fmt.Sprintf("Ready for review — %s submitted %s: %s", ownerTag, taskID, title)
	case IssueLifecycleTransitionApproved:
		return fmt.Sprintf("Done — %s wrapped %s: %s", ownerTag, taskID, title)
	case IssueLifecycleTransitionRejected:
		return fmt.Sprintf("Closed — %s: %s", taskID, title)
	case IssueLifecycleTransitionBlocked:
		return fmt.Sprintf("Blocked — %s on %s: %s", ownerTag, taskID, title)
	case IssueLifecycleTransitionNeedsInput:
		return fmt.Sprintf("Needs your input — %s on %s: %s", ownerTag, taskID, title)
	case IssueLifecycleTransitionRevising:
		return fmt.Sprintf("Revising — %s reworking %s: %s", ownerTag, taskID, title)
	}
	return fmt.Sprintf("Issue %s updated: %s", taskID, title)
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
