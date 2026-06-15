package team

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// Legacy title prefix (pre-2026-05-28). New self-heal titles use the
// "[@<agent>] <verb>: <parent title>" format — recognition is now
// pipeline-id based (see isSelfHealingTaskTitle). Kept here so tasks
// persisted before the rename still parse as self-heals.
const selfHealingTaskTitlePrefix = "Self-heal "

// selfHealingTitleAgentPrefix is the new title shape's leading token:
// "[@" + agent slug + "]". The legacy prefix above is kept for
// backward compatibility with persisted state.
const selfHealingTitleAgentPrefix = "[@"

// maxActiveSelfHealsPerAgent caps how many non-terminal self-heal tasks can
// exist for a single agent. Once an agent is at the cap, additional
// self-heal requests merge their incident detail into the most recently
// updated active self-heal instead of opening a new task. This prevents the
// (agent, taskID) dedupe key from leaking N self-heal entries when an agent
// fails on N distinct original task IDs.
//
// Override with WUPHF_SELF_HEAL_MAX_ACTIVE_PER_AGENT (>0) for installs with
// taller per-agent repair lanes.
const defaultMaxActiveSelfHealsPerAgent = 3

var maxActiveSelfHealsPerAgent = clampSelfHealCap(envIntDefault("WUPHF_SELF_HEAL_MAX_ACTIVE_PER_AGENT", defaultMaxActiveSelfHealsPerAgent))

// clampSelfHealCap rejects non-positive overrides. A cap of 0 or less would
// silently disable the per-agent cap and reintroduce the explosion this fix
// is meant to prevent.
func clampSelfHealCap(n int) int {
	if n <= 0 {
		return defaultMaxActiveSelfHealsPerAgent
	}
	return n
}

func (l *Launcher) requestSelfHealing(agentSlug, taskID string, reason agent.EscalationReason, detail string) (teamTask, bool, error) {
	if l == nil || l.broker == nil {
		return teamTask{}, false, nil
	}
	return l.broker.RequestSelfHealing(agentSlug, taskID, reason, detail)
}

func (b *Broker) RequestSelfHealing(agentSlug, taskID string, reason agent.EscalationReason, detail string) (teamTask, bool, error) {
	if b == nil {
		return teamTask{}, false, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.requestSelfHealingLocked(agentSlug, taskID, reason, detail)
}

func (b *Broker) requestSelfHealingLocked(agentSlug, taskID string, reason agent.EscalationReason, detail string) (teamTask, bool, error) {
	agentSlug = strings.TrimSpace(agentSlug)
	taskID = strings.TrimSpace(taskID)
	if b.isSelfHealingTaskIDLocked(taskID) {
		return teamTask{}, true, nil
	}

	owner := strings.TrimSpace(officeLeadSlugFrom(b.members))
	if owner == "" {
		owner = agentSlug
	}
	// Look up the parent Issue so the human-facing self-heal title carries
	// real context ("Agent stuck on: Send VC outreach") and the agent half
	// of the details carries the parent's FULL work contract — never just
	// the clipped escalation reason (ten-out-of-ten A3: a v3 lane worked
	// off a truncated source and shipped a second conflicting brief).
	parentTitle := ""
	parentDetails := ""
	if parent := b.findTaskByIDLocked(taskID); parent != nil {
		parentTitle = strings.TrimSpace(parent.Title)
		parentDetails = selfHealingParentContract(parent)
	}
	// Resolve the root Issue up front so the new lane lands as a sub-issue
	// of the stalled parent AND the covering-lane dedupe below can scan
	// against the right anchor. Walk all the way up (the FE nests one deep);
	// a cycle or unexpectedly deep chain is bounded by maxParentWalkHops.
	parentIssueID := strings.TrimSpace(taskID)
	const maxParentWalkHops = 5
	for hop := 0; hop < maxParentWalkHops; hop++ {
		src := b.findTaskByIDLocked(parentIssueID)
		if src == nil {
			break
		}
		next := strings.TrimSpace(src.ParentIssueID)
		if next == "" || next == parentIssueID {
			break
		}
		parentIssueID = next
	}
	title := selfHealingTaskTitle(agentSlug, taskID, parentTitle, reason)
	details := selfHealingTaskDetails(agentSlug, taskID, parentTitle, parentDetails, reason, detail)
	createdBy := selfHealingCreatedByForMode(b.sessionMode)
	channel := b.preferredTaskChannelLocked("general", createdBy, owner, title, details)
	if b.findChannelLocked(channel) == nil {
		return teamTask{}, false, fmt.Errorf("channel not found")
	}
	if !b.canAccessChannelLocked(createdBy, channel) {
		return teamTask{}, false, fmt.Errorf("channel access denied")
	}

	existing := b.findReusableTaskLocked(taskReuseMatch{
		Channel:    channel,
		Title:      title,
		Owner:      owner,
		PipelineID: "incident",
	})
	mergeOverflow := false
	if existing == nil {
		// Covering-lane dedupe (ten-out-of-ten A1, V3-N8): before opening a
		// new repair lane, check whether an OPEN lane already covers the same
		// work — either an active self-heal child of the same root Issue, or
		// any open task in the channel whose title is slug-similar to the
		// stalled work. The v3 run spawned OFFICE-8/-262/-295 around the same
		// primaries and the deliverables scattered across them.
		if lane := b.findOpenLaneCoveringWorkLocked(channel, parentIssueID, taskID, parentTitle); lane != nil {
			if isSelfHealingTask(lane) {
				existing = lane // merge the incident into the open repair lane
			} else {
				// A primary open task already covers this work. Do not open a
				// duplicate repair lane and do not pollute the primary's
				// contract with incident plumbing — report it as the reused
				// lane so the caller's audit trail points at real work.
				return *lane, true, nil
			}
		}
	}
	if existing == nil {
		if overflow := b.findOverflowSelfHealForAgentLocked(agentSlug); overflow != nil {
			existing = overflow
			mergeOverflow = true
		}
	}
	if existing != nil {
		beforeStatus := existing.status
		incidentBody := selfHealingIncidentUpdate(reason, detail)
		if mergeOverflow {
			incidentBody = selfHealingOverflowIncidentUpdate(taskID, reason, detail)
		}
		if existing.Details == "" {
			existing.Details = details
		} else if err := appendTaskDetailLocked(existing, incidentBody); err != nil {
			return teamTask{}, true, err
		}
		if existing.Owner == "" && owner != "" {
			existing.Owner = owner
		}
		if strings.TrimSpace(existing.Owner) != "" {
			if err := b.applyLifecycleStateLocked(existing, LifecycleStateRunning); err != nil {
				return teamTask{}, true, err
			}
		}
		if existing.TaskType == "" {
			existing.TaskType = "incident"
		}
		if existing.PipelineID == "" {
			existing.PipelineID = "incident"
		}
		if existing.ExecutionMode == "" {
			existing.ExecutionMode = "office"
		}
		b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		b.queueTaskBehindActiveOwnerLaneLocked(existing)
		if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
			return teamTask{}, true, err
		}
		b.scheduleTaskLifecycleLocked(existing)
		if err := b.syncTaskWorktreeLocked(existing); err != nil {
			return teamTask{}, true, err
		}
		b.appendActionLocked("task_updated", "office", channel, createdBy, truncateSummary(existing.Title+" [updated]", 140), existing.ID)
		if err := b.saveLocked(); err != nil {
			return teamTask{}, true, err
		}
		b.emitTaskTransitionAutoNotebook(existing, beforeStatus, createdBy)
		return *existing, true, nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	task := teamTask{
		ID:        b.allocateIssueIDLocked(),
		Channel:   channel,
		Title:     title,
		Details:   details,
		Owner:     owner,
		status:    "open",
		CreatedBy: createdBy,
		// Self-heal records render as Issues under the parent so the human
		// sees them on the Issues board and the per-issue Activity feed —
		// not buried as a separate "incident" type the UI doesn't surface.
		TaskType:      "issue",
		ParentIssueID: parentIssueID,
		PipelineID:    "incident",
		ExecutionMode: "office",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if task.Owner != "" {
		task.status = "in_progress"
	}
	b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
	b.queueTaskBehindActiveOwnerLaneLocked(&task)
	if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
		return teamTask{}, false, err
	}
	b.scheduleTaskLifecycleLocked(&task)
	if err := b.syncTaskWorktreeLocked(&task); err != nil {
		return teamTask{}, false, err
	}
	b.reindexTaskLifecycleFromLegacyLocked(&task)
	b.tasks = append(b.tasks, task)
	b.appendActionLocked("task_created", "office", channel, createdBy, truncateSummary(task.Title, 140), task.ID)
	if err := b.saveLocked(); err != nil {
		return teamTask{}, false, err
	}
	b.emitTaskTransitionAutoNotebook(&task, "", createdBy)
	return task, false, nil
}

// selfHealLaneSimilarityThreshold is the Jaro-Winkler score above which two
// normalized work titles are treated as covering the same work. Matches the
// ≥0.9 tier used by the entity resolver and skill dedupe.
const selfHealLaneSimilarityThreshold = 0.90

// normalizedWorkTitle reduces a task title to its comparable work phrase:
// strips the self-heal "[@slug] " provenance prefix and the reason-verb
// prefix ("Agent stuck on: …"), lowercases, and collapses whitespace.
func normalizedWorkTitle(title string) string {
	t := strings.TrimSpace(title)
	if strings.HasPrefix(t, selfHealingTitleAgentPrefix) {
		if close := strings.Index(t, "] "); close > len(selfHealingTitleAgentPrefix) {
			t = t[close+2:]
		}
	}
	if colon := strings.Index(t, ": "); colon > 0 {
		verb := t[:colon]
		switch verb {
		case "Agent stuck on", "Repeated errors blocked", "Missing capability for", "Help needed on":
			t = t[colon+2:]
		}
	}
	return strings.Join(strings.Fields(strings.ToLower(t)), " ")
}

// findOpenLaneCoveringWorkLocked returns an open (non-terminal) task that
// already covers the work the new self-heal would duplicate:
//
//   - an active self-heal lane parented under the same root Issue, or
//   - any open task in the channel whose normalized title is slug-similar
//     (Jaro-Winkler ≥ selfHealLaneSimilarityThreshold) to the stalled
//     parent's title.
//
// The stalled parent itself, its root Issue, and system tasks are excluded.
// Caller holds b.mu.
func (b *Broker) findOpenLaneCoveringWorkLocked(channel, rootIssueID, stalledTaskID, parentTitle string) *teamTask {
	if b == nil {
		return nil
	}
	rootIssueID = strings.TrimSpace(rootIssueID)
	stalledTaskID = strings.TrimSpace(stalledTaskID)
	normParent := normalizedWorkTitle(parentTitle)
	for i := range b.tasks {
		t := &b.tasks[i]
		if t.System || isTerminalTeamTaskStatus(t.status) {
			continue
		}
		switch strings.TrimSpace(t.ID) {
		case "", stalledTaskID, rootIssueID:
			continue
		}
		if isSelfHealingTask(t) && rootIssueID != "" && strings.TrimSpace(t.ParentIssueID) == rootIssueID {
			return t
		}
		if normParent == "" || normalizeChannelSlug(t.Channel) != channel {
			continue
		}
		if JaroWinkler(normalizedWorkTitle(t.Title), normParent) >= selfHealLaneSimilarityThreshold {
			return t
		}
	}
	return nil
}

// selfHealingParentContract renders the stalled parent's full work contract
// (Details plus the structured Definition goal/deliverables when present) for
// embedding in the self-heal lane. Agent-facing content must come from
// Details/Definition — a repair lane that only sees the clipped escalation
// reason re-derives the work from a truncated source (ten-out-of-ten A3).
func selfHealingParentContract(parent *teamTask) string {
	if parent == nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if def := parent.Definition; def != nil {
		if goal := strings.TrimSpace(def.Goal); goal != "" {
			parts = append(parts, "Goal: "+goal)
		}
		for _, d := range def.Deliverables {
			name := strings.TrimSpace(d.Name)
			if name == "" {
				continue
			}
			line := "Deliverable: " + name
			if format := strings.TrimSpace(d.Format); format != "" {
				line += " (" + format + ")"
			}
			parts = append(parts, line)
		}
	}
	if details := strings.TrimSpace(parent.Details); details != "" {
		parts = append(parts, details)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// attachSelfHealCompletionToParentLocked routes a finished self-heal lane's
// outcome back onto the stalled parent (ten-out-of-ten A1, V3-N8): the
// delivered artifact is recorded on the PARENT, the delivery is noted in the
// parent's details, and the parent advances through the legitimate path —
// into Review for the human's decision, never straight to done. Pre-execution
// parents (parked/intake/ready) only receive the artifact + note: a parked
// parent stays parked until the human starts it. Caller holds b.mu; the
// caller's saveLocked persists the parent mutation alongside the child's.
func (b *Broker) attachSelfHealCompletionToParentLocked(child *teamTask) {
	if b == nil || child == nil || !isSelfHealingTask(child) {
		return
	}
	parentID := strings.TrimSpace(child.ParentIssueID)
	if parentID == "" {
		return
	}
	parent := b.findTaskByIDLocked(parentID)
	if parent == nil || parent.System || isTerminalTeamTaskStatus(parent.status) {
		return
	}
	artifact := strings.TrimSpace(child.Artifact)
	if artifact == "" {
		return
	}
	if strings.TrimSpace(parent.Artifact) == "" {
		parent.Artifact = artifact
	}
	note := fmt.Sprintf("Self-heal %s delivered for this task — artifact: %s. The deliverable belongs to THIS task; review it here.", child.ID, artifact)
	if err := appendTaskDetailLocked(parent, note); err != nil {
		log.Printf("self-healing: attach completion note to parent %s: %v", parent.ID, err)
	}
	parentChannel := normalizeChannelSlug(parent.Channel)
	if parentChannel == "" {
		parentChannel = "general"
	}
	switch parent.LifecycleState {
	case LifecycleStateReview, LifecycleStateDecision:
		// already awaiting the human — the artifact + note are enough
	case LifecycleStateDrafting, LifecycleStateIntake, LifecycleStateReady:
		// never entered execution (parked or awaiting staffing) — record
		// the artifact + note only; do not force a Review transition
	default:
		if err := b.applyLifecycleStateLocked(parent, LifecycleStateReview); err != nil {
			log.Printf("self-healing: advance parent %s to review: %v", parent.ID, err)
		}
	}
	parent.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	b.appendActionLocked("task_updated", "office", parentChannel, "system",
		truncateSummary(parent.Title+" [self-heal delivered]", 140), parent.ID)
}

// requestCapabilitySelfHealingHook is a swap-able test hook used by the
// build-time gate #1 unit test to observe whether the call site fires
// (Lane A: blocked must NOT trigger self-heal). Production
// always leaves this nil and the real implementation runs.
var requestCapabilitySelfHealingHook func(blockedTask *teamTask, actor, detail string)

func (b *Broker) requestCapabilitySelfHealingLocked(blockedTask *teamTask, actor, detail string) {
	if hook := requestCapabilitySelfHealingHook; hook != nil {
		hook(blockedTask, actor, detail)
	}
	if blockedTask == nil || !isCapabilityGapBlocker(detail) || isSelfHealingTaskTitle(blockedTask.Title) {
		return
	}
	agentSlug := strings.TrimSpace(actor)
	if agentSlug == "" || agentSlug == "system" {
		agentSlug = strings.TrimSpace(blockedTask.Owner)
	}
	if agentSlug == "" {
		agentSlug = "agent"
	}
	if _, _, err := b.requestSelfHealingLocked(agentSlug, blockedTask.ID, agent.EscalationCapabilityGap, detail); err != nil {
		log.Printf("self-healing: create capability repair task for agent=%s task=%s: %v", agentSlug, blockedTask.ID, err)
	}
}

func isCapabilityGapBlocker(detail string) bool {
	text := strings.ToLower(strings.TrimSpace(detail))
	if text == "" {
		return false
	}
	if strings.Contains(text, "capability gap") || strings.Contains(text, "missing capability") {
		return true
	}
	capabilityTerms := []string{
		"specialist", "channel", "skill", "playbook", "tool", "provider", "integration",
		"workflow", "action", "api", "connection", "connector", "credential", "credentials",
		"permission", "access", "account", "runtime", "session",
	}
	positiveSignals := []string{
		"missing", "no ", "not connected", "not configured", "not available", "unavailable",
		"unsupported", "can't", "cannot", "unable", "need", "needs", "requires", "require",
	}
	for _, term := range capabilityTerms {
		if !strings.Contains(text, term) {
			continue
		}
		for _, signal := range positiveSignals {
			if strings.Contains(text, signal) {
				return true
			}
		}
		if strings.Contains(text, "tool path") || strings.Contains(text, "provider gap") || strings.Contains(text, "integration path") {
			return true
		}
	}
	return false
}

func (l *Launcher) selfHealingCreatedBy() string {
	if l == nil {
		return "system"
	}
	return selfHealingCreatedByForMode(l.sessionMode)
}

func selfHealingCreatedByForMode(mode string) string {
	if NormalizeSessionMode(mode) == SessionModeOneOnOne {
		return "you"
	}
	return "system"
}

func (l *Launcher) isSelfHealingTaskID(taskID string) bool {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || l == nil || l.broker == nil {
		return false
	}
	return l.broker.isSelfHealingTaskID(taskID)
}

func (b *Broker) isSelfHealingTaskID(taskID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.isSelfHealingTaskIDLocked(taskID)
}

func (b *Broker) isSelfHealingTaskIDLocked(taskID string) bool {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || b == nil {
		return false
	}
	for _, task := range b.tasks {
		if strings.TrimSpace(task.ID) != taskID {
			continue
		}
		return isSelfHealingTaskTitle(task.Title)
	}
	return false
}

// isSelfHealingTaskTitle returns true for both the legacy
// "Self-heal …" prefix and the current "[@<agent>] …" prefix. Production
// callers that already hold a teamTask should prefer the PipelineID
// check below.
func isSelfHealingTaskTitle(title string) bool {
	trimmed := strings.TrimSpace(title)
	if strings.HasPrefix(trimmed, selfHealingTaskTitlePrefix) {
		return true
	}
	if !strings.HasPrefix(trimmed, selfHealingTitleAgentPrefix) {
		return false
	}
	// "[@<slug>] " requires a closing bracket followed by space + the
	// reason verb. Anything else is a stray [@mention] in a normal title.
	close := strings.Index(trimmed, "] ")
	return close > len(selfHealingTitleAgentPrefix)
}

// isSelfHealingTask is the preferred recognition check when the caller
// has the full teamTask: a task created via requestSelfHealingLocked
// always carries PipelineID == "incident". This is more robust than a
// title-prefix match for surfaces that mutate titles (renames, merges).
func isSelfHealingTask(t *teamTask) bool {
	if t == nil {
		return false
	}
	if strings.TrimSpace(t.PipelineID) == "incident" {
		return true
	}
	return isSelfHealingTaskTitle(t.Title)
}

// humanReasonVerb maps the agent-side escalation tag to a short human
// phrase used in the self-heal title. Non-tech operators don't need to
// know what "capability_gap" or "max_retries" mean — they need to know
// "the agent got stuck" or "the agent needs a tool it doesn't have."
func humanReasonVerb(reason agent.EscalationReason) string {
	switch reason {
	case agent.EscalationStuck:
		return "Agent stuck on"
	case agent.EscalationMaxRetries:
		return "Repeated errors blocked"
	case agent.EscalationCapabilityGap:
		return "Missing capability for"
	default:
		return "Help needed on"
	}
}

// humanReasonSummary is one sentence in plain English describing what
// kind of problem this is. Surfaces in the "What happened" block of the
// self-heal details.
func humanReasonSummary(reason agent.EscalationReason, agentName string) string {
	switch reason {
	case agent.EscalationStuck:
		return fmt.Sprintf("%s kept trying to make progress on this work but couldn't move it forward.", agentName)
	case agent.EscalationMaxRetries:
		return fmt.Sprintf("%s hit the same error repeatedly while working on this and stopped to avoid wasting more attempts.", agentName)
	case agent.EscalationCapabilityGap:
		return fmt.Sprintf("%s realized it doesn't have a tool, skill, or piece of information it needs to finish this work.", agentName)
	default:
		return fmt.Sprintf("%s couldn't complete this work on its own and needs help to continue.", agentName)
	}
}

// agentDisplayNameFromSlug formats an agent slug for human-facing copy.
// Uppercases conventional 2–3-letter abbreviations (CEO, CTO, CFO, COO,
// CMO, VP, PM) and otherwise title-cases the slug. Empty slug renders
// as a neutral fallback.
func agentDisplayNameFromSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "An agent"
	}
	upper := strings.ToUpper(slug)
	switch upper {
	case "CEO", "CTO", "CFO", "COO", "CMO", "VP", "PM":
		return upper
	}
	// Title-case the slug, replacing hyphens/underscores with spaces.
	cleaned := strings.NewReplacer("-", " ", "_", " ").Replace(slug)
	return strings.Title(cleaned) //nolint:staticcheck // cases.Title is overkill for our ASCII slugs
}

// selfHealingTaskTitle composes a title a non-tech operator can read at
// a glance. Format: "[@<agent>] <reason verb>: <parent issue title>" —
// e.g. "[@ceo] Agent stuck on: Send VC outreach email". The `[@slug]`
// prefix carries provenance (preserved for overflow-merge lookups that
// scan titles per agent) and is stripped on the FE for display. Falls
// back gracefully when the parent title is missing.
func selfHealingTaskTitle(agentSlug, taskID, parentTitle string, reason agent.EscalationReason) string {
	verb := humanReasonVerb(reason)
	who := strings.TrimSpace(agentSlug)
	if who == "" {
		who = "agent"
	}
	prefix := fmt.Sprintf("[@%s] ", who)
	parentTitle = strings.TrimSpace(parentTitle)
	if parentTitle != "" {
		return prefix + fmt.Sprintf("%s: %s", verb, parentTitle)
	}
	if taskID = strings.TrimSpace(taskID); taskID != "" {
		return prefix + fmt.Sprintf("%s issue %s", verb, taskID)
	}
	return prefix + fmt.Sprintf("%s — agent couldn't continue", verb)
}

// selfHealingTaskDetails is split into two halves:
//   - HUMAN HALF (top): What happened + What needs to happen, in plain
//     English. This is what the operator reads when they open the
//     self-heal issue on the Issues board.
//   - AGENT HALF (bottom): structured context + repair loop the assigned
//     agent uses to recover. Same content as before so agent behavior
//     doesn't regress. Visually separated by a divider so the operator
//     can scroll past it.
func selfHealingTaskDetails(agentSlug, taskID, parentTitle, parentDetails string, reason agent.EscalationReason, detail string) string {
	agentName := agentDisplayNameFromSlug(agentSlug)
	who := strings.TrimSpace(agentSlug)
	if who == "" {
		who = "unknown"
	}
	originalTask := strings.TrimSpace(taskID)
	if originalTask == "" {
		originalTask = "unknown"
	}
	trigger := strings.TrimSpace(string(reason))
	if trigger == "" {
		trigger = "unknown"
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "(no further detail from the agent)"
	}
	parentLine := strings.TrimSpace(parentTitle)
	if parentLine == "" {
		parentLine = "Issue " + originalTask
	} else {
		parentLine = fmt.Sprintf("%s · %s", parentLine, originalTask)
	}

	whatNeeds := whatNeedsToHappen(reason)

	lines := []string{
		"## What happened",
		"",
		humanReasonSummary(reason, agentName),
		"",
		fmt.Sprintf("> %s reported: %s", agentName, detail),
		"",
		"## What needs to happen",
		"",
		whatNeeds,
		"",
		fmt.Sprintf("**Original work:** %s", parentLine),
		"",
		"---",
		"",
		"### Agent context (for the assigned agent)",
		"",
		fmt.Sprintf("- Agent: @%s", who),
		fmt.Sprintf("- Original task: %s", originalTask),
		fmt.Sprintf("- Trigger: %s", trigger),
		fmt.Sprintf("- Detail: %s", detail),
	}
	// Full work contract of the stalled task (A3): the escalation detail
	// above is a clipped one-liner; the repair lane must work from the
	// parent's verbatim Details/Definition, never re-derive the work from a
	// truncated echo.
	if contract := strings.TrimSpace(parentDetails); contract != "" {
		lines = append(lines,
			"",
			"**Original task contract (verbatim — work from THIS, not from the clipped detail above):**",
			"",
			contract,
		)
	}
	lines = append(lines,
		"",
		"**Repair loop:**",
		"",
		"1. Inspect the failed task and recent thread context. Use the pushed packet as authoritative; call team_poll or team_tasks only if context is missing.",
		"2. Classify the blocker: missing specialist/channel, missing or outdated skill/playbook, missing tool/provider/integration, stale runtime/session, unclear human decision, or implementation bug.",
		"3. Take the smallest reversible repair in office state. Prefer a bounded refresh/retry/requeue, reassignment, capability-check step, specialist/channel creation, playbook update, or exact human question before broad process changes.",
		"4. If runtime/tool state looks stale, refresh or reconnect once and verify with a cheap health check before treating it as a human blocker.",
		"5. Repair the missing capability first, then resume or requeue the original workflow with a concrete verification step. A self-heal that only reports the blocker is incomplete.",
		"6. Treat learning as a post-repair review: propose a skill or update a wiki/playbook only when the workaround is durable and reusable. Include the trigger, failure signature, recovery step, verification signal, and any tool/provider/channel constraints. If nothing reusable was learned, leave skills unchanged.",
		"7. Do not mark this self-healing task complete until the original task is unblocked, resumed/requeued with a clearer owner/cut line, or explicitly blocked behind a human decision.",
	)
	return strings.Join(lines, "\n")
}

// whatNeedsToHappen returns the operator-facing next-step guidance for
// each escalation reason. Plain English, no agent jargon — the operator
// should know whether they need to do something or whether the agent is
// expected to recover on its own.
func whatNeedsToHappen(reason agent.EscalationReason) string {
	switch reason {
	case agent.EscalationStuck:
		return "The CEO (or another suitable agent) will pick this up and try a different approach. You usually don't need to act — but if you have additional context or a workaround, drop it in the comments and the agent will use it on the next turn."
	case agent.EscalationMaxRetries:
		return "The CEO will look at the failing pattern and either fix the root cause or escalate to you with a specific question. If you know which step keeps breaking (e.g. \"the email send is rate-limited\"), comment it here — that often resolves it in one turn."
	case agent.EscalationCapabilityGap:
		return "The CEO will identify what's missing (a tool, a skill, an integration, or a piece of information) and either enable it, request it from you, or hire a specialist who has it. If you already know the answer — e.g. \"use the Gmail integration\" — comment it here and the agent will proceed."
	default:
		return "The CEO will review and decide how to proceed. If you have context that would unblock the agent, add it as a comment."
	}
}

// findOverflowSelfHealForAgentLocked returns the most recently updated
// active self-heal task for agentSlug when the agent's active count is at
// or above maxActiveSelfHealsPerAgent. Returns nil when the cap is
// disabled (<=0) or the agent is below the cap.
func (b *Broker) findOverflowSelfHealForAgentLocked(agentSlug string) *teamTask {
	if b == nil {
		return nil
	}
	limit := maxActiveSelfHealsPerAgent
	if limit <= 0 {
		return nil
	}
	agentSlug = strings.TrimSpace(agentSlug)
	if agentSlug == "" {
		return nil
	}
	// Two title formats are recognised so existing self-heals don't lose
	// their agent attribution after the human-friendly rename:
	//   legacy: "Self-heal @<slug> on <id>" — match "@<slug> "
	//   new:    "[@<slug>] <verb>: <parent>" — match "[@<slug>] "
	// Both anchor on a terminator (space or bracket) so agent "eng" does
	// not accidentally match "@engineering".
	legacyNeedle := "@" + agentSlug + " "
	newNeedle := "[@" + agentSlug + "] "
	var most *teamTask
	count := 0
	for i := range b.tasks {
		task := &b.tasks[i]
		if !isSelfHealingTask(task) {
			continue
		}
		if isTerminalTeamTaskStatus(task.status) {
			continue
		}
		title := task.Title
		if !strings.Contains(title, legacyNeedle) && !strings.Contains(title, newNeedle) {
			continue
		}
		count++
		if most == nil || task.UpdatedAt > most.UpdatedAt {
			most = task
		}
	}
	if count < limit {
		return nil
	}
	return most
}

// selfHealingOverflowIncidentUpdate is selfHealingIncidentUpdate with an
// extra "Original task" line so a merged overflow incident keeps a pointer
// back to the failing taskID it came from. Without this we lose the link
// between the merged incident and the task that triggered it.
func selfHealingOverflowIncidentUpdate(originalTaskID string, reason agent.EscalationReason, detail string) string {
	trigger := strings.TrimSpace(string(reason))
	if trigger == "" {
		trigger = "unknown"
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "no detail provided"
	}
	originalTaskID = strings.TrimSpace(originalTaskID)
	if originalTaskID == "" {
		originalTaskID = "unknown"
	}
	return strings.Join([]string{
		"Latest incident (merged from per-agent self-heal overflow):",
		fmt.Sprintf("- Original task: %s", originalTaskID),
		fmt.Sprintf("- Trigger: %s", trigger),
		fmt.Sprintf("- Detail: %s", detail),
	}, "\n")
}

func selfHealingIncidentUpdate(reason agent.EscalationReason, detail string) string {
	trigger := strings.TrimSpace(string(reason))
	if trigger == "" {
		trigger = "unknown"
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "no detail provided"
	}
	return strings.Join([]string{
		"Latest incident:",
		fmt.Sprintf("- Trigger: %s", trigger),
		fmt.Sprintf("- Detail: %s", detail),
	}, "\n")
}
