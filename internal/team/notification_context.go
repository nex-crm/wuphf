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
	"fmt"
	"sort"
	"strings"

	"github.com/nex-crm/wuphf/internal/channel"
)

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
}

// NotificationContext returns the recent-messages context block for the
// given (channel, threadRoot) pair. Excludes the trigger message itself,
// system messages, demo_seed posts, and STATUS chatter. Thread-scoped
// when threadRoot is non-empty (anchors at root + most-recent thread
// activity); recent-channel fallback otherwise.
func (b *notificationContextBuilder) NotificationContext(channel, triggerMsgID, threadRootID string, limit int) string {
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

	baseFilter := func(m channelMessage) bool {
		if m.From == "system" {
			return false
		}
		if m.Kind == "demo_seed" {
			return false
		}
		if strings.TrimSpace(triggerMsgID) != "" && strings.TrimSpace(m.ID) == strings.TrimSpace(triggerMsgID) {
			return false
		}
		if strings.HasPrefix(strings.TrimSpace(m.Content), "[STATUS]") {
			return false
		}
		return true
	}

	formatContext := func(items []channelMessage) string {
		var sb strings.Builder
		for _, m := range items {
			sb.WriteString(fmt.Sprintf("@%s: %s\n", m.From, truncate(m.Content, 600)))
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
		status := strings.TrimSpace(task.Status)
		if status == "" {
			status = "open"
		}
		meta := owner + ", " + status
		if task.Blocked {
			meta += ", blocked"
		}
		if len(task.DependsOn) > 0 {
			meta += ", depends: " + strings.Join(task.DependsOn, " ")
		}
		taskChannel := normalizeChannelSlug(task.Channel)
		if taskChannel == "" {
			taskChannel = "general"
		}
		line := fmt.Sprintf("- #%s on #%s %s (%s)", task.ID, taskChannel, truncate(task.Title, 72), meta)
		if details := strings.TrimSpace(task.Details); details != "" {
			line += ": " + truncate(details, 96)
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
		if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
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
		reviewCount := 0
		for _, task := range tasks {
			if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(task.Status), "review") || strings.EqualFold(strings.TrimSpace(task.ReviewState), "ready_for_review") {
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
	var domainOwned teamTask
	bestOwnedScore := 0.0
	for _, task := range b.allTasks() {
		if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			continue
		}
		if strings.TrimSpace(task.Owner) != slug {
			continue
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
	if domainOwned.ID != "" {
		return domainOwned, true
	}
	return teamTask{}, false
}

// ResponseInstructionForTarget returns the per-agent guidance string
// appended to a notification. Branches: lead-from-human, lead-from-
// specialist, DM, tagged, owns-matching-task, default-stay-quiet.
func (b *notificationContextBuilder) ResponseInstructionForTarget(msg channelMessage, slug string) string {
	lead := b.targeter.LeadSlug()
	if slug == lead {
		from := strings.TrimSpace(msg.From)
		isFromHuman := from == "" || from == "you" || from == "human" || from == "nex"
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
	return fmt.Sprintf("You are @%s. Stay quiet unless you are directly tagged, you own the active work, or you can unblock it. Prefer not to reply.", slug)
}

// BuildMessageWorkPacket returns the work packet a notified agent receives
// for a channel message: header lines (thread / DM preamble / group
// preamble / tagged hint / active task), recent-message context, and (for
// the lead) a list of agents who have already acted in this thread or
// have pending headless turns ("do NOT re-route").
func (b *notificationContextBuilder) BuildMessageWorkPacket(msg channelMessage, slug string) string {
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
	if task, ok := b.RelevantTaskForTarget(msg, slug); ok {
		lines = append(lines, fmt.Sprintf("- Active task: #%s %s (%s)", task.ID, truncate(task.Title, 96), strings.TrimSpace(task.Status)))
		if details := strings.TrimSpace(task.Details); details != "" {
			lines = append(lines, fmt.Sprintf("- Task details: %s", truncate(details, 512)))
		}
		if path := strings.TrimSpace(task.WorktreePath); path != "" {
			lines = append(lines, fmt.Sprintf("- Working directory: %q", path))
		}
	}
	threadRoot := b.UltimateThreadRoot(channelSlug, msg.ReplyTo)
	if ctx := b.NotificationContext(channelSlug, msg.ID, threadRoot, 4); ctx != "" {
		lines = append(lines, ctx)
	}
	if slug == b.targeter.LeadSlug() {
		if taskCtx := b.TaskNotificationContext("", slug, 3); taskCtx != "" {
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
				if tm.From != "" && tm.From != "you" && tm.From != "human" && tm.From != "nex" && tm.From != slug {
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
	return strings.Join(lines, "\n")
}

// BuildTaskExecutionPacket returns the work packet for a task assignment
// or update. Includes the task header, worktree path, named file targets
// pulled from title+details, local-worktree guardrails, external-execution
// guidance for live business tasks, and the recent thread context.
func (b *notificationContextBuilder) BuildTaskExecutionPacket(slug string, action officeActionLog, task teamTask, content string) string {
	channelSlug := normalizeChannelSlug(task.Channel)
	if channelSlug == "" {
		channelSlug = "general"
	}
	lines := []string{
		fmt.Sprintf("[Task update from @%s]", action.Actor),
		"Work packet:",
		fmt.Sprintf("- Task: #%s %s", task.ID, truncate(task.Title, 120)),
		fmt.Sprintf("- Status: %s", strings.TrimSpace(task.Status)),
		fmt.Sprintf("- Owner: @%s", slug),
	}
	if details := strings.TrimSpace(task.Details); details != "" {
		lines = append(lines, fmt.Sprintf("- Details: %s", truncate(details, 512)))
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
	if ctx := b.NotificationContext(channelSlug, "", threadRoot, 3); ctx != "" {
		lines = append(lines, ctx)
	}
	lines = append(lines, fmt.Sprintf("If you deliver the substantive result for #%s in this turn, you MUST call team_task complete or review-ready for \"%s\" before any completion post and before you stop. A channel reply alone does not unblock dependent work, and a completion post without the task mutation is a failure.", task.ID, task.ID))
	lines = append(lines, "Runtime rule: never launch another WUPHF office, copied wuphf binary, browser instance, or local web server/--web-port process from inside this turn. The office is already running; use the existing repo, broker state, and assigned worktree instead.")
	lines = append(lines, fmt.Sprintf("%s Use team_task with my_slug \"%s\" to update status as you go.", truncate(content, 1000), slug))
	return strings.Join(lines, "\n")
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
	status := strings.TrimSpace(task.Status)
	if status == "" {
		status = "open"
	}
	details := strings.TrimSpace(task.Details)
	if details != "" {
		details = " — " + truncate(details, 120)
	}
	pipeline := ""
	if strings.TrimSpace(task.PipelineStage) != "" {
		pipeline = ", stage " + task.PipelineStage
	}
	review := ""
	if strings.TrimSpace(task.ReviewState) != "" && task.ReviewState != "not_required" {
		review = ", review " + task.ReviewState
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
	return fmt.Sprintf("[%s #%s on #%s]: %s%s (owner %s, status %s%s%s%s%s). Context is included — do NOT call team_poll or team_tasks. Respond with the concrete next step immediately. Stay in your lane. Once you have posted the needed update, STOP and wait for the next pushed notification.%s%s%s%s", verb, task.ID, channelSlug, task.Title, details, owner, status, pipeline, review, execMode, worktree, guidance, framing, capability, hygiene)
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
