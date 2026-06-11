package team

// notification_context.go owns the per-target notification-context and
// work-packet construction that used to live as ten methods on Launcher
// (PLAN.md §C3). The cluster is pure-string assembly over broker reads —
// no goroutines, no tmux, no broker writes — so it can be exercised with
// stub callbacks instead of a fully-wired Broker fixture.
//
// State sharing notes (PLAN.md §5.7): primeVisibleAgents and
// respawnPanesAfterReseed straddle this cluster and the future
// paneLifecycle (C5). Both stay on Launcher (and call the builder) so
// the dependency direction is paneLifecycle → notificationContext, never
// the reverse — see the trap discussion in PLAN.md.

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/nex-crm/wuphf/internal/channel"
)

// Context-packet sizing. These were historically tuned down to minimize
// per-turn token cost (specialists woke with 4 thread messages and
// 512-char task details, which starved them into re-asking answered
// questions and contradicting prior decisions). The SOTA uplift
// (docs/specs/sota-uplift.md, U0.2) inverts that: packets are sized for
// outcome quality, and token cost is no longer a design constraint.
const (
	// threadContextLimit is how many recent thread messages every agent
	// (lead and specialist alike) receives in a work packet.
	threadContextLimit = 20
	// threadMessageClipChars clips a single message inside the context block.
	threadMessageClipChars = 2000
	// taskDetailsClipChars clips the task spec text injected into packets.
	taskDetailsClipChars = 4096
	// taskListTitleClipChars / taskListDetailsClipChars clip per-task lines
	// in multi-task summary lists.
	taskListTitleClipChars   = 120
	taskListDetailsClipChars = 512
	// triggerContentClipChars clips the trigger message in execution packets.
	triggerContentClipChars = 4000
	// changesRequestedClipChars clips the latest request-changes feedback
	// rendered in execution packets. Generous on purpose: ICP-eval v2 J2
	// found agents reworking blind because the human's typed feedback was
	// invisible or truncated ("The feedback text is truncated in the
	// packet"), so this block must carry the full review verbatim for any
	// realistic comment length.
	changesRequestedClipChars = 4000
	// changesRequestedNotifyClipChars clips the same feedback inside the
	// short single-line wake notification header.
	changesRequestedNotifyClipChars = 600
	// leadTaskContextLimit is how many task summaries the lead's packet carries.
	leadTaskContextLimit = 10
)

// recipientHasTaskVisibility reports whether the given recipient slug
// is authorized to see in-flight messages from `task`. Used by the
// pre-review-message filter in NotificationContext. The owner of the
// task sees their own work; each reviewer slug sees the task they're
// reviewing; anyone else has to wait for the merge-broadcast system
// message in the channel + the canonical wiki entry.
func recipientHasTaskVisibility(recipient string, task *teamTask) bool {
	if task == nil {
		return false
	}
	if recipient == "" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(task.Owner), recipient) {
		return true
	}
	for _, r := range task.Reviewers {
		if strings.EqualFold(strings.TrimSpace(r), recipient) {
			return true
		}
	}
	return false
}

// notificationContextBuilder assembles the strings (notification context,
// work packets, response instructions, task content) that get typed into
// agent panes or sent through the headless dispatch queue. Constructed
// fresh per call from launcher state so it always sees current broker
// reads.
type notificationContextBuilder struct {
	targeter *officeTargeter

	// Broker-shaped reads. Callbacks rather than a *Broker pointer so tests
	// can stub without instantiating the full broker (and so headless
	// queue peeks below stay scoped to one accessor).
	channelMessages func(channel string) []channelMessage
	channelTasks    func(channel string) []teamTask
	allTasks        func() []teamTask
	channelStore    func() *channel.Store

	// scoreTaskCandidate is the routing.go score function pulled in via
	// callback; the builder doesn't need to know about routing internals.
	scoreTaskCandidate func(msg channelMessage, task teamTask) float64

	// activeHeadlessAgents returns slugs that have non-empty headless
	// queues or active turns at the moment of the call. The launcher
	// implements this with the headlessMu lock held; the builder treats
	// it as opaque. The except parameter is the slug being notified —
	// the lead must not list itself as "already active".
	activeHeadlessAgents func(except string) map[string]struct{}

	// searchLearnings / searchWiki feed the task-scoped knowledge block
	// (context_assembler.go, U2.2). Nil-safe: when unset the packet simply
	// carries no knowledge block.
	searchLearnings func(query string, limit int) []LearningSearchResult
	searchWiki      func(ctx context.Context, query string, topK int) []SearchHit

	// taskByID returns the task with the given ID (or nil). Used by the
	// pre-review filter to decide whether a message tagged with
	// SourceTaskID is visible to a given recipient. Nil callback means
	// no filtering — every message passes.
	taskByID func(taskID string) *teamTask
}

// NotificationContext returns the recent-messages context block for the
// given (channel, threadRoot) pair. Excludes the trigger message itself,
// system messages, and STATUS chatter. Thread-scoped
// when threadRoot is non-empty (anchors at root + most-recent thread
// activity); recent-channel fallback otherwise.
//
// recipientSlug is the agent the context is being built for. When set,
// the builder additionally suppresses messages whose source task is in
// a pre-merge lifecycle state and the recipient is NOT the task owner
// or one of its reviewers. This prevents Agent B from picking up Agent
// A's in-stream pre-review commentary as if it were canonical output —
// agents subscribed to merged-state results read the wiki, not the
// channel scrollback. Pass empty recipient for backwards-compatible
// "no filter" semantics.
func (b *notificationContextBuilder) NotificationContext(recipientSlug, channel, triggerMsgID, threadRootID string, limit int) string {
	if b == nil || b.channelMessages == nil {
		return ""
	}
	if limit <= 0 {
		return ""
	}
	if strings.TrimSpace(channel) == "" {
		channel = "general"
	}

	msgs := b.channelMessages(channel)
	if len(msgs) == 0 {
		return ""
	}

	recipient := normalizeActorSlug(recipientSlug)
	baseFilter := func(m channelMessage) bool {
		if m.From == "system" {
			return false
		}
		if strings.TrimSpace(triggerMsgID) != "" && strings.TrimSpace(m.ID) == strings.TrimSpace(triggerMsgID) {
			return false
		}
		if strings.HasPrefix(strings.TrimSpace(m.Content), "[STATUS]") {
			return false
		}
		// Pre-review-message filter: hide pre-merge chatter from
		// agents who aren't authoritatively involved in the source
		// task. System and merged-decision broadcasts pass through
		// because SourceTaskID is either empty or the task is no
		// longer pre-merge.
		if recipient != "" && b.taskByID != nil && strings.TrimSpace(m.SourceTaskID) != "" {
			if t := b.taskByID(m.SourceTaskID); t != nil && lifecycleStateIsPreMerge(t.LifecycleState) {
				if !recipientHasTaskVisibility(recipient, t) {
					return false
				}
			}
		}
		return true
	}

	formatContext := func(items []channelMessage) string {
		var sb strings.Builder
		for _, m := range items {
			sb.WriteString(fmt.Sprintf("@%s: %s\n", m.From, truncate(m.Content, threadMessageClipChars)))
		}
		return strings.TrimRight(sb.String(), "\n")
	}

	threadRoot := strings.TrimSpace(threadRootID)
	if threadRoot != "" {
		threadIDs := b.ThreadMessageIDs(channel, threadRoot)
		var rootMsg *channelMessage
		var rest []channelMessage
		for i := range msgs {
			m := &msgs[i]
			if !baseFilter(*m) {
				continue
			}
			if strings.TrimSpace(m.ID) == threadRoot {
				rootMsg = m
			} else if _, inThread := threadIDs[strings.TrimSpace(m.ID)]; inThread {
				rest = append(rest, *m)
			}
		}
		if rootMsg != nil || len(rest) > 0 {
			remaining := limit
			if rootMsg != nil {
				remaining--
			}
			if len(rest) > remaining {
				rest = rest[len(rest)-remaining:]
			}
			var thread []channelMessage
			if rootMsg != nil {
				thread = append(thread, *rootMsg)
			}
			thread = append(thread, rest...)
			return "[Recent thread]\n" + formatContext(thread)
		}
	}

	var filtered []channelMessage
	for _, m := range msgs {
		if baseFilter(m) {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return "[Recent channel]\n" + formatContext(filtered)
}

// UltimateThreadRoot walks the reply-to chain from startID up to the
// topmost ancestor and returns its ID. Caps at 8 hops to defend against
// cycles or pathological data.
func (b *notificationContextBuilder) UltimateThreadRoot(channelSlug, startID string) string {
	startID = strings.TrimSpace(startID)
	if b == nil || startID == "" || b.channelMessages == nil {
		return startID
	}
	msgs := b.channelMessages(channelSlug)
	byID := make(map[string]channelMessage, len(msgs))
	for _, m := range msgs {
		if id := strings.TrimSpace(m.ID); id != "" {
			byID[id] = m
		}
	}
	root := startID
	for depth := 0; depth < 8; depth++ {
		m, ok := byID[root]
		if !ok {
			break
		}
		parent := strings.TrimSpace(m.ReplyTo)
		if parent == "" {
			break
		}
		root = parent
	}
	return root
}

// ThreadMessageIDs returns the BFS set of message IDs rooted at rootID.
// The root itself is included. Empty when rootID is empty.
func (b *notificationContextBuilder) ThreadMessageIDs(channelSlug, rootID string) map[string]struct{} {
	rootID = strings.TrimSpace(rootID)
	result := make(map[string]struct{})
	if b == nil || rootID == "" || b.channelMessages == nil {
		return result
	}
	msgs := b.channelMessages(channelSlug)
	byParent := make(map[string][]string, len(msgs))
	for _, m := range msgs {
		parent := strings.TrimSpace(m.ReplyTo)
		if parent != "" {
			byParent[parent] = append(byParent[parent], strings.TrimSpace(m.ID))
		}
	}
	result[rootID] = struct{}{}
	queue := []string{rootID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range byParent[cur] {
			if _, seen := result[child]; !seen {
				result[child] = struct{}{}
				queue = append(queue, child)
			}
		}
	}
	return result
}

// TaskNotificationContext returns the "Active tasks:" block for the given
// agent. Lead agents see all-channel tasks (sort by UpdatedAt desc),
// non-leads see their own owned work first then a short fallback list.
// Lead agents also get a review-backlog hint when tasks are stuck waiting
// on review.
func (b *notificationContextBuilder) TaskNotificationContext(channelSlug, slug string, limit int) string {
	if b == nil || limit <= 0 {
		return ""
	}
	var tasks []teamTask
	if strings.TrimSpace(channelSlug) == "" {
		if b.allTasks == nil {
			return ""
		}
		tasks = b.allTasks()
	} else {
		if b.channelTasks == nil {
			return ""
		}
		tasks = b.channelTasks(channelSlug)
	}
	if len(tasks) == 0 {
		return ""
	}

	formatTask := func(task teamTask) string {
		owner := strings.TrimSpace(task.Owner)
		switch {
		case owner == "":
			owner = "unassigned"
		default:
			owner = "@" + owner
		}
		status := strings.TrimSpace(task.status)
		if status == "" {
			status = "open"
		}
		meta := owner + ", " + status
		if task.blocked {
			meta += ", blocked"
		}
		if len(task.DependsOn) > 0 {
			meta += ", depends: " + strings.Join(task.DependsOn, " ")
		}
		taskChannel := normalizeChannelSlug(task.Channel)
		if taskChannel == "" {
			taskChannel = "general"
		}
		line := fmt.Sprintf("- #%s on #%s %s (%s)", task.ID, taskChannel, truncate(task.Title, taskListTitleClipChars), meta)
		if details := strings.TrimSpace(task.Details); details != "" {
			line += ": " + truncate(details, taskListDetailsClipChars)
		}
		return line
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].UpdatedAt > tasks[j].UpdatedAt
	})

	lines := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	addTask := func(task teamTask) {
		if len(lines) >= limit {
			return
		}
		if strings.EqualFold(strings.TrimSpace(task.status), "done") {
			return
		}
		if _, ok := seen[task.ID]; ok {
			return
		}
		seen[task.ID] = struct{}{}
		lines = append(lines, formatTask(task))
	}

	lead := b.targeter.LeadSlug()
	for _, task := range tasks {
		if slug == lead || strings.TrimSpace(task.Owner) == slug {
			addTask(task)
		}
	}
	if len(lines) == 0 {
		for _, task := range tasks {
			addTask(task)
		}
	}
	if len(lines) == 0 {
		if slug == lead {
			return "Active tasks:\n- None currently active. If the overall build is not actually finished, create the next owned task(s) now instead of ending with narrative next steps."
		}
		return ""
	}

	result := "Active tasks:\n" + strings.Join(lines, "\n")
	if slug == lead {
		// Concurrency awareness: the lead runs one dispatch lane per task, so the
		// tasks above may be advancing in parallel right now (its own lanes and
		// specialists'). Push non-dependent tasks forward together rather than
		// serializing them, and reuse a live task before creating an overlapping one.
		// The phrasing avoids implying the printed list is complete — the list is
		// truncated to `limit`, so a reusable task may not be shown.
		result += "\nCoordination: your tasks run on separate lanes and may be progressing in parallel right now. Advance non-dependent tasks together instead of one at a time, and reuse or update an existing task for the same work before creating a new one."
		// If more eligible (non-done) tasks exist than were shown, the reusable
		// task the lead should update may have been truncated away. Tell it to
		// check the full list before creating a new one, so it doesn't duplicate.
		// Dedup by ID to mirror the `seen` filter the printed list applies, so a
		// repeated task can't trip a false-positive truncation cue.
		eligibleIDs := make(map[string]struct{}, len(tasks))
		for _, task := range tasks {
			if strings.EqualFold(strings.TrimSpace(task.status), "done") {
				continue
			}
			eligibleIDs[task.ID] = struct{}{}
		}
		if len(eligibleIDs) > len(lines) {
			result += "\nNote: more active tasks exist than shown here — check the full task list before creating a new one."
		}
		reviewCount := 0
		for _, task := range tasks {
			if strings.EqualFold(strings.TrimSpace(task.status), "done") {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(task.status), "review") || strings.EqualFold(strings.TrimSpace(task.reviewState), "ready_for_review") {
				reviewCount++
			}
		}
		if reviewCount > 0 {
			result += fmt.Sprintf("\nLead action: %d task(s) are waiting in review. Approve them or translate them into the next owned task(s) before you stop.", reviewCount)
		}
	}
	return result
}

// RelevantTaskForTarget returns a task this slug owns that's tied to the
// given message — first by ThreadID match, then by message-vs-task scoring
// above the routing threshold. Done tasks and other-owned tasks never
// match.
func (b *notificationContextBuilder) RelevantTaskForTarget(msg channelMessage, slug string) (teamTask, bool) {
	if b == nil || b.allTasks == nil || slug == "" {
		return teamTask{}, false
	}
	// Resolve to the ultimate thread root so deep replies still match a
	// task anchored on the top-level message. msg.ReplyTo only points at
	// the immediate parent, which loses second-level+ replies.
	threadRoot := strings.TrimSpace(msg.ID)
	if replyTo := strings.TrimSpace(msg.ReplyTo); replyTo != "" {
		if root := strings.TrimSpace(b.UltimateThreadRoot(strings.TrimSpace(msg.Channel), replyTo)); root != "" {
			threadRoot = root
		} else {
			threadRoot = replyTo
		}
	}
	rawMsgCh := strings.TrimSpace(msg.Channel)
	var (
		domainOwned    teamTask
		bestOwnedScore = 0.0
		channelMatch   teamTask
		channelMatches int
	)
	for _, task := range b.allTasks() {
		if strings.EqualFold(strings.TrimSpace(task.status), "done") {
			continue
		}
		if strings.TrimSpace(task.Owner) != slug {
			continue
		}
		// Channel-per-task: the message's channel directly names the task it
		// belongs to, so a channel match is the strongest binding — stronger
		// than a thread or content-similarity match, which can drift for a
		// short human chat. Guard on the RAW channels being non-empty (an
		// unset channel normalizes to "general", which would otherwise let a
		// channel-less task collide with #general) and skip archived tasks so
		// general office chat in #general — owned by the archived Backup &
		// Migration task — keeps routing normally. (Done is skipped above.)
		// Legacy shared channels fall through to the thread/score checks below.
		//
		// Accumulate channel matches across ALL owned tasks instead of
		// early-returning the first one: a single human can own several tasks
		// that share a legacy channel, and binding to whichever happened to be
		// scanned first attaches the wrong task. Only trust the channel binding
		// when it is unambiguous (exactly one owned task in that channel);
		// otherwise fall through to the thread/score logic below.
		if rawMsgCh != "" {
			rawTaskCh := strings.TrimSpace(task.Channel)
			st := strings.ToLower(strings.TrimSpace(task.status))
			if rawTaskCh != "" && st != "archived" &&
				normalizeChannelSlug(rawTaskCh) == normalizeChannelSlug(rawMsgCh) {
				channelMatches++
				channelMatch = task
			}
		}
		if task.ThreadID != "" && (task.ThreadID == msg.ID || task.ThreadID == threadRoot) {
			return task, true
		}
		if b.scoreTaskCandidate == nil {
			continue
		}
		score := b.scoreTaskCandidate(msg, task)
		if score >= officeRoutingMatchThreshold && score > bestOwnedScore {
			domainOwned = task
			bestOwnedScore = score
		}
	}
	if channelMatches == 1 {
		return channelMatch, true
	}
	if domainOwned.ID != "" {
		return domainOwned, true
	}
	return teamTask{}, false
}

// ResponseInstructionForTarget returns the per-agent guidance string
// appended to a notification. Branches: lead-from-human, lead-from-
// specialist, DM, tagged, owns-matching-task, default-domain-chime-in.
func (b *notificationContextBuilder) ResponseInstructionForTarget(msg channelMessage, slug string) string {
	lead := b.targeter.LeadSlug()
	if slug == lead {
		from := strings.TrimSpace(msg.From)
		isFromHuman := isHumanMessageSender(from) || from == "nex"
		if !isFromHuman {
			return fmt.Sprintf("You are @%s. A specialist just finished a lane. If the build is still underway, any task is open or in review, or the next lane is obvious, act now: approve/release review items, create the next owned team_task records, and only then stop. On a human build/ship/end-to-end request, if you approve or close an engineering/execution slice and the product is not yet runnable end to end, you MUST leave at least one engineering or execution lane active before you stop; do not let the company drift into GTM-only, rubric-only, or evaluation-only work while the build is still incomplete. Before you say a task is approved, closed, back in progress, reassigned, or blocked, you MUST make the matching team_task or team_plan call first; channel narration alone does not change durable state. If the task mutation fails, say that it failed and do not claim the state changed. Before creating any new task, inspect Active tasks in this packet: if a live task already covers that lane, reuse or update that task instead of creating an overlapping duplicate with a new title. Stay quiet only when the human already has what they need AND there is no remaining office work or obvious follow-up.", slug)
		}
		return fmt.Sprintf("You are @%s. Give the first top-level reply quickly, then pull in specialists only when needed. For build/ship/end-to-end requests, the first engineering task itself must be a single smallest runnable feature slice, not an MVP umbrella or a multi-output minimum bar. Do not put a separate repo audit, architecture, or cut-line research task in front of that first feature unless the human explicitly asked for analysis first or the repo truly has no identifiable implementation target. Do not spend the whole first turn on `pwd`, `ls`, `rg --files`, `find .`, or another repo-wide inventory; use the named docs/configs in the packet or at most one or two targeted reads, then create the first durable task/channel state in that same turn. %s", slug, capabilityGapCoachingBlock())
	}
	channelSlug := normalizeChannelSlug(msg.Channel)
	if IsDMSlug(channelSlug) && DMTargetAgent(channelSlug) == slug {
		return fmt.Sprintf("You are @%s. The human is messaging you directly in a DM. Respond helpfully from your domain expertise.", slug)
	}
	if isDM, agentTarget := b.targeter.IsChannelDM(channelSlug); isDM && agentTarget == slug {
		return fmt.Sprintf("You are @%s. The human is messaging you directly in a DM. Respond helpfully from your domain expertise.", slug)
	}
	if containsSlug(msg.Tagged, slug) {
		return fmt.Sprintf("You are @%s. You were directly tagged. Reply only from your domain with concrete progress, a blocker, or a handoff.", slug)
	}
	if task, ok := b.RelevantTaskForTarget(msg, slug); ok && strings.TrimSpace(task.Owner) == slug {
		if taskRequiresRealExternalExecution(&task) {
			return fmt.Sprintf("You are @%s. You already own matching work that requires a real connected-system action. Take the smallest allowed live external step now or report a blocker; repo docs, previews, local markdown, proof markers, and test artifacts do not satisfy it. Frame the result as a business deliverable, approval, handoff, or record, not as an eval or proof artifact. %s %s", slug, capabilityGapCoachingBlock(), taskHygieneCoachingBlock())
		}
		return fmt.Sprintf("You are @%s. You already own matching work. Reply only with concrete progress or a blocker; do not re-triage the thread.", slug)
	}
	return fmt.Sprintf("You are @%s. You were woken because the thread brushes your domain. If you have a sharp take, a push-back from your expertise, or a quick observation that moves the work, drop it — short. Skip the turn only if you truly have nothing to add; do not reply just to acknowledge.", slug)
}

// BuildMessageWorkPacket returns the work packet a notified agent receives
// for a channel message: header lines (thread / DM preamble / group
// preamble / tagged hint / active task), recent-message context, and (for
// the lead) a list of agents who have already acted in this thread or
// have pending headless turns ("do NOT re-route").
func (b *notificationContextBuilder) BuildMessageWorkPacket(msg channelMessage, slug string) string {
	packet, _ := b.BuildMessageWorkPacketWithContext(msg, slug)
	return packet
}

// BuildMessageWorkPacketWithContext is BuildMessageWorkPacket plus the
// context manifest: the ids of every knowledge/upstream/journal item the
// packet injected, recorded on the turn so the ledger (and the Activity
// rail) can show the human what context the agent was handed (B4).
func (b *notificationContextBuilder) BuildMessageWorkPacketWithContext(msg channelMessage, slug string) (string, []string) {
	channelSlug := normalizeChannelSlug(msg.Channel)
	if channelSlug == "" {
		channelSlug = "general"
	}
	lines := []string{
		"Work packet:",
		fmt.Sprintf("- Thread: #%s reply_to %s", channelSlug, msg.ID),
	}
	if isDM, _ := b.targeter.IsChannelDM(channelSlug); isDM {
		dmPreamble := []string{
			"Context: DIRECT MESSAGE",
			"This is a private 1:1 conversation with the human. Respond to every message.",
			"You do not need to coordinate with other agents.",
			"---",
		}
		lines = append(dmPreamble, lines...)
	} else if b.channelStore != nil {
		if cs := b.channelStore(); cs != nil {
			if storeChannel, ok := cs.GetBySlug(channelSlug); ok && storeChannel.Type == "G" {
				members := cs.Members(storeChannel.ID)
				names := make([]string, 0, len(members))
				for _, m := range members {
					if m.Slug != slug {
						names = append(names, "@"+m.Slug)
					}
				}
				groupPreamble := []string{
					"Context: GROUP MESSAGE",
					fmt.Sprintf("This is a group conversation with: %s.", strings.Join(names, ", ")),
					"Respond to messages directed at you or within your expertise.",
					"---",
				}
				lines = append(groupPreamble, lines...)
			}
		}
	}
	if containsSlug(msg.Tagged, slug) {
		lines = append(lines, "- Trigger: you were explicitly tagged")
	}
	var contextUsed []string
	if task, ok := b.RelevantTaskForTarget(msg, slug); ok {
		lines = append(lines, fmt.Sprintf("- Active task: #%s %s (%s)", task.ID, truncate(task.Title, taskListTitleClipChars), strings.TrimSpace(task.status)))
		if defLines := taskDefinitionPacketLines(task.Definition); len(defLines) > 0 {
			lines = append(lines, "- Task definition (the contract this work executes against):")
			lines = append(lines, defLines...)
		}
		if details := strings.TrimSpace(task.Details); details != "" {
			lines = append(lines, fmt.Sprintf("- Task details: %s", truncate(details, taskDetailsClipChars)))
		}
		if path := strings.TrimSpace(task.WorktreePath); path != "" {
			lines = append(lines, fmt.Sprintf("- Working directory: %q", path))
		}
		if knowledge, manifest := b.taskKnowledgeContext(task); knowledge != "" {
			lines = append(lines, knowledge)
			contextUsed = append(contextUsed, manifest...)
		}
		if upstream, manifest := b.upstreamOutcomesContext(task); upstream != "" {
			lines = append(lines, upstream)
			contextUsed = append(contextUsed, manifest...)
		}
		if journal := taskLedgerContext(task); journal != "" {
			lines = append(lines, journal)
			contextUsed = append(contextUsed, "journal:"+task.ID)
		}
	}
	threadRoot := b.UltimateThreadRoot(channelSlug, msg.ReplyTo)
	// Every agent gets the full thread window. Specialists used to be
	// capped at 4 messages "to stay token-efficient", which starved them
	// into improvising from a keyhole view of the thread (sota-uplift U0.2).
	if ctx := b.NotificationContext(slug, channelSlug, msg.ID, threadRoot, threadContextLimit); ctx != "" {
		lines = append(lines, ctx)
	}
	if slug == b.targeter.LeadSlug() {
		if taskCtx := b.TaskNotificationContext("", slug, leadTaskContextLimit); taskCtx != "" {
			lines = append(lines, taskCtx)
		}
		activeAgents := map[string]struct{}{}
		if b.channelMessages != nil {
			threadRoot := strings.TrimSpace(b.UltimateThreadRoot(channelSlug, msg.ReplyTo))
			if threadRoot == "" {
				threadRoot = strings.TrimSpace(msg.ID)
			}
			threadIDs := b.ThreadMessageIDs(channelSlug, threadRoot)
			for _, tm := range b.channelMessages(channelSlug) {
				if _, inThread := threadIDs[strings.TrimSpace(tm.ID)]; !inThread {
					continue
				}
				if !isHumanMessageSender(tm.From) && tm.From != "nex" && tm.From != slug {
					activeAgents[tm.From] = struct{}{}
				}
			}
		}
		if b.activeHeadlessAgents != nil {
			for s := range b.activeHeadlessAgents(slug) {
				activeAgents[s] = struct{}{}
			}
		}
		if len(activeAgents) > 0 {
			names := make([]string, 0, len(activeAgents))
			for name := range activeAgents {
				names = append(names, "@"+name)
			}
			sort.Strings(names)
			lines = append(lines, fmt.Sprintf("- Already active in this thread (do NOT re-route): %s", strings.Join(names, ", ")))
		}
	}
	return strings.Join(lines, "\n"), contextUsed
}

// BuildTaskExecutionPacket returns the work packet for a task assignment
// or update. Includes the task header, worktree path, named file targets
// pulled from title+details, local-worktree guardrails, external-execution
// guidance for live business tasks, and the recent thread context.
func (b *notificationContextBuilder) BuildTaskExecutionPacket(slug string, action officeActionLog, task teamTask, content string) string {
	packet, _ := b.BuildTaskExecutionPacketWithContext(slug, action, task, content)
	return packet
}

// BuildTaskExecutionPacketWithContext is BuildTaskExecutionPacket plus the
// context manifest (B4 transparency): ids of the injected knowledge,
// upstream-outcome, and journal blocks. Recorded on the headless turn and
// stamped onto the task ledger entry when the turn settles.
func (b *notificationContextBuilder) BuildTaskExecutionPacketWithContext(slug string, action officeActionLog, task teamTask, content string) (string, []string) {
	channelSlug := normalizeChannelSlug(task.Channel)
	if channelSlug == "" {
		channelSlug = "general"
	}
	lines := []string{
		fmt.Sprintf("[Task update from @%s]", action.Actor),
		"Work packet:",
		fmt.Sprintf("- Task: #%s %s", task.ID, truncate(task.Title, taskListTitleClipChars)),
		fmt.Sprintf("- Status: %s", strings.TrimSpace(task.status)),
		fmt.Sprintf("- Owner: @%s", slug),
	}
	// Latest request-changes feedback leads the packet (core-loop grader
	// fix family #1, ICP-eval v2 J2): the reviewer's verbatim text rides
	// on the task itself (teamTask.ChangesRequested) because the Decision
	// Packet feedback log is invisible to the reworking agent. Rendered
	// BEFORE the definition so a bounced task's next turn opens with what
	// the reviewer actually said, not a bare "changes requested" flag.
	if lines2 := changesRequestedPacketLines(task); len(lines2) > 0 {
		lines = append(lines, lines2...)
	}
	// R4 definition: the structured intake contract leads the packet — the
	// goal, deliverables (+format), success criteria, and access this work
	// is executed against (task_definition.go).
	if defLines := taskDefinitionPacketLines(task.Definition); len(defLines) > 0 {
		lines = append(lines, "- DEFINITION (the contract you execute against):")
		lines = append(lines, defLines...)
	}
	if artifact := strings.TrimSpace(task.Artifact); artifact != "" {
		lines = append(lines, fmt.Sprintf("- Delivered artifact: %s", artifact))
	} else if task.Definition != nil {
		lines = append(lines, "- Artifact gate: this task has a Definition, so it cannot reach done until you publish the deliverable to the wiki and pass artifact_path (wiki-relative path or visual-artifact id) on team_task action=complete.")
	}
	if details := strings.TrimSpace(task.Details); details != "" {
		lines = append(lines, fmt.Sprintf("- Details: %s", truncate(details, taskDetailsClipChars)))
	}
	if v := task.Verification; v != nil && v.Kind != taskVerificationKindNone {
		gate := "advisory"
		if v.Required {
			gate = "REQUIRED — complete/approve is blocked until this passes"
		}
		lines = append(lines, fmt.Sprintf("- Machine check (%s, %s): %s", v.Kind, gate, v.Spec))
	}
	if res := task.VerificationResult; res != nil && !res.Pass {
		lines = append(lines, fmt.Sprintf("- LAST VERIFICATION FAILED (%s at %s): %s", res.Kind, res.CheckedAt, truncate(strings.TrimSpace(res.Detail), 2000)))
		lines = append(lines, "  Fix the work so the definition-of-done check passes, then complete again. Do not try to bypass the check.")
	}
	var contextUsed []string
	if knowledge, manifest := b.taskKnowledgeContext(task); knowledge != "" {
		lines = append(lines, knowledge)
		contextUsed = append(contextUsed, manifest...)
	}
	if upstream, manifest := b.upstreamOutcomesContext(task); upstream != "" {
		lines = append(lines, upstream)
		contextUsed = append(contextUsed, manifest...)
	}
	if journal := taskLedgerContext(task); journal != "" {
		lines = append(lines, journal)
		contextUsed = append(contextUsed, "journal:"+task.ID)
	}
	if targets := extractTaskFileTargets(task.Title + " " + task.Details); len(targets) > 0 {
		lines = append(lines, fmt.Sprintf("- Named file targets: %s", strings.Join(targets, ", ")))
	}
	if task.ThreadID != "" {
		lines = append(lines, fmt.Sprintf("- Thread: #%s reply_to %s", channelSlug, task.ThreadID))
	} else {
		lines = append(lines, fmt.Sprintf("- Channel: #%s", channelSlug))
	}
	lines = append(lines, fmt.Sprintf("- Mutation channel: use #%s when claiming or completing #%s", channelSlug, task.ID))
	if path := strings.TrimSpace(task.WorktreePath); path != "" {
		lines = append(lines, fmt.Sprintf("- Working directory: %q", path))
	}
	if slug == b.targeter.LeadSlug() {
		// The lead (CEO) is the coordinator, not the sole worker. The default
		// execution framing below pushes solo direct-implementation, which is
		// right for a specialist in a worktree but wrong for the CEO on a broad
		// owned task. Give the lead an explicit decompose-and-delegate path so
		// it breaks large or cross-functional work into owned sub-tasks that
		// spin off and run concurrently (Phase 2 lanes), instead of doing it
		// all itself.
		lines = append(lines,
			"Lead execution rule: you are the coordinator, not the sole worker. For anything larger than a single owned step, DECOMPOSE this task instead of doing it all yourself:",
			fmt.Sprintf("- Break the work into concrete sub-tasks with team_task action=create, each carrying parent_issue_id=%s so they nest under this task.", task.ID),
			"- Give each sub-task an `owner`: REUSE the existing specialist whose expertise best fits (see AVAILABLE AGENTS). Only when no current teammate fits, propose a new specialist with team_member — creating a new agent ALWAYS requires explicit human approval (the tool blocks until the human decides), so prefer reusing the roster.",
			"- Sub-tasks spin off automatically: once created with an owner they wake that owner and run concurrently on their own lanes. You do not need to tag or chase each one separately.",
			"- Keep THIS task as the umbrella: track its sub-tasks, aggregate their results here as they land, and complete the parent only after the children are done. Do not mark the parent complete while children are still open.",
			"- Do the work directly yourself only when it is genuinely a single step in your own domain, or when decomposition would not help.",
		)
	}
	if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		lines = append(lines, "Execution rule: this is a local_worktree build task. Work inside the assigned working_directory and default to direct implementation. Do not spend this turn on another repo audit, architecture memo, or nested office launch unless the packet explicitly asks for that.")
		lines = append(lines, "First-turn rule: choose the smallest shippable implementation slice you can finish in this turn and edit files for that slice now. If the overall MVP is broad, narrow it yourself and ship the first runnable sub-piece instead of trying to map the whole system.")
		lines = append(lines, "Time rule: cut scope to something you can plausibly ship in under five minutes of focused work. If the chosen slice still needs broad exploration, cut it down again before you continue.")
		lines = append(lines, "Cut-line rule: if the task description lists multiple outputs or phases, pick exactly one contiguous slice for this turn, then post team_status naming that cut line before you read files. Example cut lines: `config -> idea queue`, `idea queue -> script drafts`, or `script drafts -> publish pack`.")
		if targets := extractTaskFileTargets(task.Title + " " + task.Details); len(targets) > 0 {
			lines = append(lines, "Startup rule: open the named file targets first and use them as your starting point. Do not broaden into repo search unless those exact files are insufficient for the chosen cut line.")
		}
		lines = append(lines, "Audit guardrail: do NOT start with `rg --files`, `find .`, repo-wide README sweeps, or broad file inventories. Read only the handful of files directly tied to this task, then begin editing once you have the first target file.")
		lines = append(lines, "Boundary rule: stay inside the assigned working_directory. Do NOT run `find ..`, `rg ..`, search sibling task worktrees, or inspect parent temp directories like `/var/folders`, `TMPDIR`, or `TemporaryItems`. Those paths are sandbox noise, not repo context.")
		lines = append(lines, "Dirty-tree rule: ignore unrelated modified or untracked files already present in the worktree unless they are directly required for your slice. They may be preexisting repo state, not part of your task.")
		lines = append(lines, "Deliverable rule: a local_worktree feature task is not satisfied by another plan, architecture memo, or audit summary unless the packet explicitly says research-only. Land code, scripts, docs for the runnable slice, or a concrete task-state blocker.")
	}
	if taskRequiresRealExternalExecution(&task) {
		lines = append(lines, "External execution rule: this task names a connected external system and expects a real action there. Use the live integration/workflow path for the smallest allowed step now instead of producing another repo doc, proof marker, or internal package first.")
		lines = append(lines, "Evidence rule: a local markdown file, preview note, repo artifact, or test output does NOT satisfy this task unless the packet explicitly says preview/mock/stub-only. Leave durable external evidence through the broker-integrated workflow/integration path before marking the task review-ready, done, or blocked.")
		lines = append(lines, "Business framing: describe the work as a client deliverable, approval, handoff, update, or record. Avoid proof/marker/test/eval language unless the task explicitly asks for testing or evidence capture.")
		lines = append(lines, "Pace rule: do the smallest safe external step first, then summarize it. Do not spend this turn building extra kickoff decks, review bundles, or substitute proof artifacts if the connected system action is already allowed.")
		lines = append(lines, capabilityGapCoachingBlock())
	}
	if taskLooksLikeLiveBusinessObjective(&task) {
		lines = append(lines, "Task hygiene rule: if this lane drifts into a proof packet, review bundle, blueprint-derived scaffold, rubric, or other internal artifact shell, rewrite it immediately into either the next real deliverable step or the exact capability-enablement task that will unlock that deliverable.")
	}
	threadRoot := b.UltimateThreadRoot(channelSlug, task.ThreadID)
	if ctx := b.NotificationContext(slug, channelSlug, "", threadRoot, threadContextLimit); ctx != "" {
		lines = append(lines, ctx)
	}
	lines = append(lines, fmt.Sprintf("If you deliver the substantive result for #%s in this turn, you MUST call team_task complete or review-ready for \"%s\" before any completion post and before you stop. A channel reply alone does not unblock dependent work, and a completion post without the task mutation is a failure.", task.ID, task.ID))
	lines = append(lines, "Runtime rule: never launch another WUPHF office, copied wuphf binary, browser instance, or local web server/--web-port process from inside this turn. The office is already running; use the existing repo, broker state, and assigned worktree instead.")
	lines = append(lines, fmt.Sprintf("%s Use team_task with my_slug \"%s\" to update status as you go.", truncate(content, triggerContentClipChars), slug))
	return strings.Join(lines, "\n"), contextUsed
}

// changesRequestedPacketLines renders the LATEST request-changes verdict
// stored on the task (teamTask.ChangesRequested) for the execution packet.
// Returns nil when no verdict is pending. When the verdict is the open
// HUMAN objection (teamTask.HumanObjection), the block also states the
// sovereignty contract: no agent — including the lead — can approve or
// complete the task until the human clears it.
func changesRequestedPacketLines(task teamTask) []string {
	obj := task.ChangesRequested
	if obj == nil {
		return nil
	}
	body := strings.TrimSpace(obj.Body)
	if body == "" {
		body = "(no written feedback was provided)"
	}
	lines := []string{
		fmt.Sprintf("- CHANGES REQUESTED by @%s: %s", obj.Actor, truncate(body, changesRequestedClipChars)),
		"  Address this feedback FIRST — point by point, in the actual artifact — then resubmit with team_task action=submit_for_review. Do not guess at what the reviewer meant when the text above answers it.",
	}
	if task.HumanObjection != nil {
		lines = append(lines, "  This objection is from the HUMAN and is sovereign: no agent (including the lead/CEO) can approve or complete this task while it stands. Only the human can clear it by approving or completing.")
	}
	return lines
}

// TaskNotificationContent returns the short single-line task notification
// header (verb, owner, status, optional pipeline / review / execMode /
// worktree / guidance / framing / capability / hygiene fragments).
func (b *notificationContextBuilder) TaskNotificationContent(action officeActionLog, task teamTask) string {
	channelSlug := normalizeChannelSlug(task.Channel)
	if channelSlug == "" {
		channelSlug = "general"
	}
	verb := "Task update"
	switch action.Kind {
	case "task_created":
		verb = "Task created"
	case "task_updated":
		verb = "Task updated"
	case "task_unblocked":
		verb = "Task unblocked — dependencies resolved, ready to start"
	case "watchdog_alert":
		verb = "Watchdog reminder"
	}
	owner := strings.TrimSpace(task.Owner)
	if owner == "" {
		owner = "unassigned"
	} else {
		owner = "@" + owner
	}
	status := strings.TrimSpace(task.status)
	if status == "" {
		status = "open"
	}
	details := strings.TrimSpace(task.Details)
	if details != "" {
		details = " — " + truncate(details, 120)
	}
	pipeline := ""
	if strings.TrimSpace(task.pipelineStage) != "" {
		pipeline = ", stage " + task.pipelineStage
	}
	review := ""
	if strings.TrimSpace(task.reviewState) != "" && task.reviewState != "not_required" {
		review = ", review " + task.reviewState
	}
	execMode := ""
	if strings.TrimSpace(task.ExecutionMode) != "" {
		execMode = ", execution " + task.ExecutionMode
	}
	worktree := ""
	if strings.TrimSpace(task.WorktreeBranch) != "" || strings.TrimSpace(task.WorktreePath) != "" {
		parts := make([]string, 0, 2)
		if strings.TrimSpace(task.WorktreeBranch) != "" {
			parts = append(parts, "branch "+task.WorktreeBranch)
		}
		if strings.TrimSpace(task.WorktreePath) != "" {
			parts = append(parts, "path "+task.WorktreePath)
		}
		worktree = ", worktree " + strings.Join(parts, " · ")
	}
	guidance := ""
	if path := strings.TrimSpace(task.WorktreePath); path != "" {
		guidance = fmt.Sprintf(" If you own this task, use working_directory=%q for local file and bash tools.", path)
	}
	framing := ""
	if taskRequiresRealExternalExecution(&task) {
		framing = " Live business framing: describe the work as a client deliverable, approval, handoff, update, or record. Do not present it as a proof marker, eval artifact, or test artifact unless the task explicitly asks for testing or evidence capture."
	}
	capability := ""
	if taskRequiresRealExternalExecution(&task) {
		capability = "\n" + capabilityGapCoachingBlock()
	}
	hygiene := ""
	if taskLooksLikeLiveBusinessObjective(&task) {
		hygiene = "\n" + taskHygieneCoachingBlock()
	}
	// The latest request-changes verdict rides the wake content itself (not
	// only the full execution packet) so the bounced owner's very first
	// line of context names who said no and what they said. ICP-eval v2 J2:
	// "the feedback isn't visible in the packet" cost the human a manual
	// re-explanation on every changes-request.
	changes := ""
	if obj := task.ChangesRequested; obj != nil {
		body := strings.TrimSpace(obj.Body)
		if body == "" {
			body = "(no written feedback was provided)"
		}
		changes = fmt.Sprintf(" CHANGES REQUESTED by @%s: %s", obj.Actor, truncate(body, changesRequestedNotifyClipChars))
		if task.HumanObjection != nil {
			changes += " This is the HUMAN's objection — only the human can approve or complete this task while it stands."
		}
	}
	return fmt.Sprintf("[%s #%s on #%s]: %s%s (owner %s, status %s%s%s%s%s).%s Context is included — do NOT call team_poll or team_tasks. Respond with the concrete next step immediately. Stay in your lane. Once you have posted the needed update, STOP and wait for the next pushed notification.%s%s%s%s", verb, task.ID, channelSlug, task.Title, details, owner, status, pipeline, review, execMode, worktree, changes, guidance, framing, capability, hygiene)
}

// ── package-level helpers used inside the builder ──────────────────────

// truncate clips s to max bytes followed by "..." when it overflows.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// extractTaskFileTargets returns up to four backtick-quoted candidates
// from text that look like file paths (contain "/" or "."). De-duplicated
// in order; empty when nothing matches.
func extractTaskFileTargets(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	seen := map[string]struct{}{}
	targets := make([]string, 0, 4)
	for {
		start := strings.Index(text, "`")
		if start < 0 {
			break
		}
		text = text[start+1:]
		end := strings.Index(text, "`")
		if end < 0 {
			break
		}
		candidate := strings.TrimSpace(text[:end])
		text = text[end+1:]
		if candidate == "" {
			continue
		}
		if !strings.Contains(candidate, "/") && !strings.Contains(candidate, ".") {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		targets = append(targets, candidate)
		if len(targets) == 4 {
			break
		}
	}
	return targets
}

// humanizeNotificationType converts a snake_case kind into a Title Case
// label for human display. Used by launcher_nex.go for nex notification
// rendering as well as the task-execution packet header.
func humanizeNotificationType(kind string) string {
	switch strings.TrimSpace(kind) {
	case "context_alert":
		return "Context alert"
	case "daily_digest":
		return "Daily digest"
	case "meeting_summary":
		return "Meeting summary"
	case "task_reminder":
		return "Task reminder"
	case "task_assigned":
		return "Task assigned"
	default:
		if kind == "" {
			return ""
		}
		parts := strings.Split(strings.ReplaceAll(kind, "_", " "), " ")
		for i, part := range parts {
			if part == "" {
				continue
			}
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
		return strings.Join(parts, " ")
	}
}
