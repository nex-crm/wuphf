package team

// slack_task_reporting.go makes a task's Slack thread a live status feed for
// the humans and foreign agents watching it. Three signals land here:
//
//	subtask created + assigned → the real @-ping + full task context is posted
//	                             into the SUBTASK's OWN thread, and only a plain
//	                             (no-ping) MENTION line lands in the parent. The
//	                             ping is what makes a foreign agent pick the work
//	                             up; putting it in the subtask thread is what
//	                             makes the agent work THERE. The earlier version
//	                             pinged the assignee in the PARENT thread, so a
//	                             foreign agent (e.g. Hermes) replied in the parent
//	                             and never started its subtask. The assignee must
//	                             be ADDRESSED where the work lives (the subtask),
//	                             not where it is tracked (the parent).
//	lifecycle state change     → a concise threaded update in the task's own
//	                             thread (planning → running → review → done),
//	                             de-duped so identical states never spam.
//	wiki article written       → a link to the new article in the related
//	                             task's thread.
//
// Design: the subtask + lifecycle signals are reconciled by POLLING the broker
// task list on a ticker. The broker's lifecycle transition layer has no
// broadcast event channel, and the action log (SubscribeActions) carries no
// owner / parent / state fields, so a poll is the only way to observe these
// reliably. This mirrors runTaskCardSync, which already reconciles lifecycle
// state changes onto pinned cards the same way. The wiki signal DOES have a
// clean event stream (SubscribeWikiEvents), so that half is event-driven.
//
// Every post is best-effort: a Slack failure is logged and retried on the next
// tick (reporter state is only advanced after a successful post), never fatal.

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// slackTaskReportingInterval is the poll cadence for the subtask + lifecycle
// reconciler. Matched to the card-sync loop: lifecycle transitions are
// human-scale, so 15s keeps the thread live without burning rate budget.
const slackTaskReportingInterval = 15 * time.Second

// runTaskReporting drives the task-thread reporter until ctx is cancelled.
// Registered by EnsureSlackTransportRunning alongside Run and the other
// transport goroutines; mirrors runAgentThinkingStatus' shape (one blocking
// loop, clean shutdown on ctx.Done()).
func (t *SlackTransport) runTaskReporting(ctx context.Context) error {
	if t.Broker == nil || t.api == nil {
		<-ctx.Done()
		return ctx.Err()
	}

	wiki, unsub := t.Broker.SubscribeWikiEvents(64)
	defer unsub()

	r := &slackTaskReporter{
		t:            t,
		lastState:    map[string]string{},
		seenSubtasks: map[string]bool{},
	}
	// Prime the reporter against the current world WITHOUT posting, so a restart
	// does not replay every existing subtask + state as if it just happened. The
	// reporter only emits on transitions observed after this point.
	r.prime()

	ticker := time.NewTicker(slackTaskReportingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			r.reconcile(ctx)
		case evt, ok := <-wiki:
			if !ok {
				return nil
			}
			r.reportWiki(ctx, evt)
		}
	}
}

// slackTaskReporter holds the per-loop memory that turns the polled task list
// into edge-triggered posts. Not safe for concurrent use; owned by the single
// runTaskReporting goroutine.
type slackTaskReporter struct {
	t *SlackTransport
	// lastState maps task id → the lifecycle state we last reported, so an
	// unchanged state never re-posts.
	lastState map[string]string
	// seenSubtasks tracks subtask ids whose assignment line has already been
	// posted, so a subtask is announced exactly once.
	seenSubtasks map[string]bool
}

// prime records the current state of every task without posting, so the
// reporter starts edge-triggered from "now" rather than replaying history on
// boot / reconnect.
func (r *slackTaskReporter) prime() {
	for _, task := range r.t.Broker.AllTasks() {
		r.lastState[task.ID] = slackTaskCardState(&task)
		if strings.TrimSpace(task.ParentIssueID) != "" {
			r.seenSubtasks[task.ID] = true
		}
	}
}

// reconcile is one poll pass: it announces newly-assigned subtasks and posts
// lifecycle-state changes into the relevant task threads.
func (r *slackTaskReporter) reconcile(ctx context.Context) {
	for _, task := range r.t.Broker.AllTasks() {
		task := task
		// New subtask with a parent → delegate (ping + context) in the SUBTASK
		// thread, mention (no ping) in the PARENT thread.
		if parent := strings.TrimSpace(task.ParentIssueID); parent != "" && !r.seenSubtasks[task.ID] {
			if r.reportSubtaskAssigned(ctx, &task, parent) {
				r.seenSubtasks[task.ID] = true
			}
		}
		// Lifecycle state change → update in the task's OWN thread.
		state := slackTaskCardState(&task)
		if r.lastState[task.ID] != state {
			if r.reportLifecycle(ctx, &task, state) {
				r.lastState[task.ID] = state
			}
		}
	}
}

// reportSubtaskAssigned delivers a newly-assigned subtask two ways:
//
//  1. DELEGATION in the SUBTASK's OWN thread — a real <@U…> ping for the owner
//     plus the task's title and details, so a registered foreign agent picks the
//     work up and works it THERE. This is the only place the assignee is pinged.
//  2. MENTION in the PARENT thread — a plain, no-ping line naming the subtask and
//     its owner, so humans watching the parent see the structure without pulling
//     the assignee into the wrong thread.
//
// Returns true when the subtask has been handled (delivered, or unbridged so
// there is nothing to deliver), so the caller marks it seen. Returns false only
// on a transient failure worth retrying, or when there is no owner to address
// yet (left unseen so a later poll delivers once an owner is set).
func (r *slackTaskReporter) reportSubtaskAssigned(ctx context.Context, sub *teamTask, parentID string) bool {
	owner := strings.TrimSpace(sub.Owner)
	if owner == "" {
		// No assignee to address yet. Leave UNSEEN so a later poll delivers once
		// an owner is set.
		return false
	}

	// 1) Delegate in the subtask's own thread: ping + full context.
	subThreadTS := r.t.ensureTaskThreadRoot(ctx, sub.ID)
	subChannelID := r.t.channelIDForSlug(normalizeChannelSlug(sub.Channel))
	if subThreadTS == "" || subChannelID == "" {
		// Subtask not bridged to a Slack surface — nothing to deliver. Mark seen
		// so we don't re-probe it every tick.
		return true
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	_, _, err := r.t.api.PostMessageContext(cctx, subChannelID,
		slack.MsgOptionText(r.t.subtaskDelegationText(sub, owner), false),
		slack.MsgOptionTS(subThreadTS),
	)
	cancel()
	if err != nil {
		log.Printf("[slack] subtask delegation failed for %s: %v", sub.ID, err)
		return false
	}

	// 2) Mention (no ping) in the parent thread so the parent stays a live map of
	//    its subtasks. Best-effort: a failure here does not undo the delegation.
	if parentTS := r.t.ensureTaskThreadRoot(ctx, parentID); parentTS != "" {
		if parentTask := r.t.Broker.TaskByID(parentID); parentTask != nil {
			if parentChannelID := r.t.channelIDForSlug(normalizeChannelSlug(parentTask.Channel)); parentChannelID != "" {
				line := fmt.Sprintf("Subtask %s assigned to %s (%s); handed off in its own thread.",
					r.t.taskIDLink(sub.ID), r.t.ownerLabel(owner), slackEscape(slackTaskCardState(sub)))
				pctx, pcancel := context.WithTimeout(ctx, 30*time.Second)
				if _, _, perr := r.t.api.PostMessageContext(pctx, parentChannelID,
					slack.MsgOptionText(line, false),
					slack.MsgOptionTS(parentTS),
				); perr != nil {
					log.Printf("[slack] subtask parent mention failed for %s: %v", sub.ID, perr)
				}
				pcancel()
			}
		}
	}
	return true
}

// subtaskDelegationText is the message posted INTO a subtask's own thread to hand
// it to its owner: a real ping (so a foreign agent picks it up), the linked id +
// title, the task details as context, and an instruction to work it in-thread.
func (t *SlackTransport) subtaskDelegationText(sub *teamTask, owner string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s you own %s: *%s*.\n",
		t.ownerMention(owner), t.taskIDLink(sub.ID),
		slackEscape(truncate(strings.TrimSpace(sub.Title), 200)))
	if details := strings.TrimSpace(sub.Details); details != "" {
		fmt.Fprintf(&b, "%s\n", slackEscape(truncate(details, 1500)))
	}
	b.WriteString("Work this task in THIS thread: post your progress and the deliverable here, and reply here if you need anything to proceed.")
	return b.String()
}

// reportLifecycle posts a concise state-change line into the task's own Slack
// thread. Returns true when the line was posted (so the caller records the new
// state and de-dupes), false on a transient failure to retry next tick.
func (r *slackTaskReporter) reportLifecycle(ctx context.Context, task *teamTask, state string) bool {
	// Only report tasks that have (or can have) a Slack thread. ensureTaskThreadRoot
	// returns "" for unbridged tasks; we skip those but still record the state so
	// we don't probe them every tick.
	threadTS := r.t.ensureTaskThreadRoot(ctx, task.ID)
	if threadTS == "" {
		return true
	}
	channelID := r.t.channelIDForSlug(normalizeChannelSlug(task.Channel))
	if channelID == "" {
		return true
	}
	emoji := slackCardStateEmoji[state]
	if emoji == "" {
		emoji = "•"
	}
	text := fmt.Sprintf("%s %s is now *%s*.", emoji, r.t.taskIDLink(task.ID), slackEscape(state))

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, _, err := r.t.api.PostMessageContext(cctx, channelID,
		slack.MsgOptionText(text, false),
		slack.MsgOptionTS(threadTS),
	); err != nil {
		log.Printf("[slack] lifecycle report failed for %s: %v", task.ID, err)
		return false
	}
	return true
}

// reportWiki posts a link to a freshly-written wiki article into the thread of
// the task its author is actively working on. The wiki write event carries no
// task association, so we attribute the article to the author's active task
// thread (best-effort): the common case is an agent publishing a deliverable
// while running its task. When the author has no active bridged task thread,
// the article is simply not announced in Slack (it still lands in the wiki).
func (r *slackTaskReporter) reportWiki(ctx context.Context, evt wikiWriteEvent) {
	path := strings.TrimSpace(evt.Path)
	if path == "" {
		return
	}
	webURL := strings.TrimRight(strings.TrimSpace(r.t.Broker.WebURL()), "/")
	if webURL == "" {
		return
	}
	refs := r.t.Broker.ActiveSlackTaskThreadsForOwner(evt.AuthorSlug)
	if len(refs) == 0 {
		return
	}
	title := wikiTitleFromPath(path)
	// Path is office-controlled (a repo-relative wiki path), but escape it for
	// the link target defensively; the visible title is escaped too.
	link := fmt.Sprintf("<%s/wiki/%s|%s>", webURL, slackEscape(path), slackEscape(title))
	text := fmt.Sprintf("📄 New wiki article: %s", link)

	for _, ref := range refs {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		_, _, err := r.t.api.PostMessageContext(cctx, ref.ChannelID,
			slack.MsgOptionText(text, false),
			slack.MsgOptionTS(ref.ThreadTS),
		)
		cancel()
		if err != nil {
			log.Printf("[slack] wiki report failed for %s: %v", path, err)
		}
	}
}

// taskIDLink renders a single task id as a Slack link to the web app, or the
// bare (escaped) id when no WebURL is configured. Used by the report lines,
// which build their own text rather than passing through renderTaskLinks.
func (t *SlackTransport) taskIDLink(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	webURL := strings.TrimRight(strings.TrimSpace(t.Broker.WebURL()), "/")
	if webURL == "" {
		return slackEscape(id)
	}
	return fmt.Sprintf("<%s/tasks/%s|%s>", webURL, id, slackEscape(id))
}

// ownerMention renders a task owner for a report line: a registered foreign
// agent becomes a real <@U…> ping (the ONLY mint source is the auth registry,
// never message text), so the assignee is actually notified; any other owner
// renders as a plain escaped name prefixed with "@" for readability.
func (t *SlackTransport) ownerMention(owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return "unassigned"
	}
	if userID := t.Broker.SlackAgentUserIDBySlug(owner); userID != "" {
		return "<@" + slackEscape(userID) + ">"
	}
	name := owner
	if names := t.Broker.MemberDisplayNames(); names != nil {
		if n := strings.TrimSpace(names[normalizeActorSlug(owner)]); n != "" {
			name = n
		}
	}
	return "@" + slackEscape(name)
}

// ownerLabel renders a task owner for a FYI/status reference that must NOT ping —
// always the plain display name, never a <@U…>. Used in the parent thread, where
// naming the assignee should not pull a foreign agent into the wrong thread. The
// real ping is reserved for the subtask delegation (ownerMention).
func (t *SlackTransport) ownerLabel(owner string) string {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return "unassigned"
	}
	name := owner
	if names := t.Broker.MemberDisplayNames(); names != nil {
		if n := strings.TrimSpace(names[normalizeActorSlug(owner)]); n != "" {
			name = n
		}
	}
	return slackEscape(name)
}

// wikiTitleFromPath derives a human title from a wiki path: the base file name
// without its extension, with separators turned into spaces. The article body's
// own "# Heading" is not available on the event, so the path is the best title
// source here.
func wikiTitleFromPath(path string) string {
	base := path
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		base = base[:i]
	}
	base = strings.ReplaceAll(base, "-", " ")
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.TrimSpace(base)
	if base == "" {
		return path
	}
	return base
}
