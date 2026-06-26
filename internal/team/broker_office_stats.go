package team

import (
	"net/http"
	"strings"
	"time"
)

// broker_office_stats.go — GET /office/stats, the single derived-stats
// source for every count the web surfaces show (header runtime strip,
// board lane headers, dashboard tiles, inbox badge, wiki home count).
//
// Motivation (ten-out-of-ten C1): each surface used to derive its own
// counts from its own list query with its own bucketing predicate —
// "header blocked=1 vs board Blocked 0", "wiki 0 articles vs 19",
// "6 active while every agent reads waiting". This endpoint computes
// every number from the same indexes the list endpoints read, in one
// b.mu lock pass for broker state, so the numbers cannot drift between
// surfaces that consume it.
//
// Bucketing contract: taskBoardStage mirrors the frontend projection
// (web/src/lib/types/lifecycle.ts stageForState ∘ api/tasks.ts
// taskToLifecycleState) and isBoardSpecTask mirrors
// TasksList.isTaskSpecTask. TestOfficeStats_MatchesListEndpoints pins
// stats == list-endpoint-derived truth.

// Board stage names. Wire-stable: the frontend consumes these as the
// lane keys of OfficeStats.Tasks.
const (
	boardStageBacklog    = "backlog"
	boardStageInProgress = "in_progress"
	boardStageBlocked    = "blocked"
	boardStageNeedsHuman = "needs_human"
	boardStageDone       = "done"
	boardStageArchive    = "archive"
)

// OfficeStatsTasks buckets the board's spec-level tasks by lane.
// Active mirrors the board's "In progress" lane; Review is the
// subset of Active currently sitting in review / changes_requested
// (surfaced separately for review-centric badges).
type OfficeStatsTasks struct {
	Backlog    int `json:"backlog"`
	Active     int `json:"active"`
	Blocked    int `json:"blocked"`
	Review     int `json:"review"`
	NeedsHuman int `json:"needs_human"`
	Done       int `json:"done"`
	Archive    int `json:"archive"`
}

// OfficeStatsRequests splits pending human requests into blocking
// (blocking or required — the human must act for work to continue)
// and notices (informational, non-blocking).
type OfficeStatsRequests struct {
	Blocking int `json:"blocking"`
	Notices  int `json:"notices"`
}

// OfficeStats is the wire shape of GET /office/stats.
type OfficeStats struct {
	Tasks    OfficeStatsTasks    `json:"tasks"`
	Requests OfficeStatsRequests `json:"requests"`
	// InboxAttention is the unified-inbox badge count: requests +
	// reviews + tasks in a human-attention lifecycle state. Computed
	// from the same fan-out /inbox/items serves.
	InboxAttention int `json:"inbox_attention"`
	// WikiArticles counts curated wiki articles with the same filter
	// rules /wiki/catalog applies. Zero when the wiki worker is off.
	WikiArticles int `json:"wiki_articles"`
	// AgentsActive counts roster agents whose live activity snapshot
	// reports a working status (same derivation /office-members uses).
	AgentsActive int    `json:"agents_active"`
	GeneratedAt  string `json:"generated_at"`
}

// isBoardSpecTask mirrors the frontend board filter
// (TasksList.isTaskSpecTask): top-level Issues only — sub-tasks live on
// the parent's detail surface, and only issue-typed / issue-pipeline /
// drafted-spec tasks are board cards.
func isBoardSpecTask(t *teamTask) bool {
	if t == nil {
		return false
	}
	if strings.TrimSpace(t.ParentIssueID) != "" {
		return false
	}
	taskType := strings.ToLower(strings.TrimSpace(t.TaskType))
	pipelineID := strings.ToLower(strings.TrimSpace(t.PipelineID))
	return taskType == "issue" || pipelineID == "issue"
}

// taskBoardStage projects a task onto its board lane. This is the Go
// mirror of the frontend's stageForState(taskToLifecycleState(task))
// composition — keep the three in sync (the office-stats contract test
// fails if the wire mapping drifts from what the board derives).
func taskBoardStage(t *teamTask) string {
	if strings.ToLower(strings.TrimSpace(t.PipelineStage())) == "draft" {
		return boardStageBacklog // drafting
	}
	switch t.LifecycleState {
	case LifecycleStateDrafting, LifecycleStateIntake, LifecycleStateReady:
		return boardStageBacklog
	case LifecycleStateRunning, LifecycleStateReview, LifecycleStateChangesRequested:
		return boardStageInProgress
	case LifecycleStateBlocked, LifecycleStateQueuedBehindOwner:
		return boardStageBlocked
	case LifecycleStateDecision:
		return boardStageNeedsHuman
	case LifecycleStateApproved:
		return boardStageDone
	case LifecycleStateArchived, LifecycleStateRejected:
		return boardStageArchive
	}
	// Legacy status fallback — mirrors taskToLifecycleState's switch for
	// tasks that predate lifecycle_state (or carry an unknown value).
	switch strings.ToLower(strings.TrimSpace(t.Status())) {
	case "in_progress":
		return boardStageInProgress
	case "done":
		return boardStageDone
	case "blocked":
		return boardStageBlocked
	case "review":
		return boardStageInProgress
	case "rejected", "archived":
		return boardStageArchive
	default:
		// "open" and anything unknown lands in backlog, same as the
		// frontend's default branch.
		return boardStageBacklog
	}
}

// taskInReview reports whether the task currently sits with reviewers.
// Counted inside the Active lane but surfaced separately.
func taskInReview(t *teamTask) bool {
	switch t.LifecycleState {
	case LifecycleStateReview, LifecycleStateChangesRequested:
		return true
	}
	return t.LifecycleState == "" && strings.ToLower(strings.TrimSpace(t.Status())) == "review"
}

// memberLiveStatusLocked derives the live status string for one roster
// slug exactly as serveOfficeMemberList does for the /office-members
// payload: activity snapshot first, then the 60s @mention grace window,
// then "idle". Caller holds b.mu.
func (b *Broker) memberLiveStatusLocked(slug string, now time.Time) string {
	if snapshot, ok := b.activity[slug]; ok && snapshot.Status != "" {
		return snapshot.Status
	}
	if b.lastTaggedAt != nil {
		if taggedAt, ok := b.lastTaggedAt[slug]; ok && now.Sub(taggedAt) < 60*time.Second {
			return "active"
		}
	}
	return "idle"
}

// inboxItemNeedsAttention mirrors the frontend badge predicate
// (InboxButton.isAttentionItem): requests always demand attention;
// tasks only in the human-attention lifecycle states.
func inboxItemNeedsAttention(item InboxItem) bool {
	switch item.Kind {
	case InboxItemKindRequest:
		return true
	case InboxItemKindTask:
		if item.TaskRow == nil {
			return false
		}
		switch item.TaskRow.LifecycleState {
		case LifecycleStateDecision, LifecycleStateReview,
			LifecycleStateChangesRequested, LifecycleStateBlocked:
			return true
		}
	}
	return false
}

// computeOfficeTaskAndRequestStats fills the task buckets, request
// split, and active-agent count in a single b.mu pass over the same
// slices /tasks, /requests, and /office-members serve.
func (b *Broker) computeOfficeTaskAndRequestStats(viewerSlug string) (OfficeStatsTasks, OfficeStatsRequests, int) {
	var tasks OfficeStatsTasks
	var requests OfficeStatsRequests
	agentsActive := 0

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	accessCache := make(map[string]bool)
	channelAllowed := func(slug string) bool {
		if v, ok := accessCache[slug]; ok {
			return v
		}
		v := b.canAccessChannelLocked(viewerSlug, slug)
		accessCache[slug] = v
		return v
	}

	for i := range b.tasks {
		task := &b.tasks[i]
		if !channelAllowed(normalizeChannelSlug(task.Channel)) {
			continue
		}
		if !b.viewerCanSeeTaskLocked(viewerSlug, task) {
			continue
		}
		if !isBoardSpecTask(task) {
			continue
		}
		switch taskBoardStage(task) {
		case boardStageBacklog:
			tasks.Backlog++
		case boardStageInProgress:
			tasks.Active++
		case boardStageBlocked:
			tasks.Blocked++
		case boardStageNeedsHuman:
			tasks.NeedsHuman++
		case boardStageDone:
			tasks.Done++
		case boardStageArchive:
			tasks.Archive++
		}
		if taskInReview(task) {
			tasks.Review++
		}
	}

	for _, req := range b.requests {
		if !requestIsActive(req) {
			continue
		}
		reqChannel := normalizeChannelSlug(req.Channel)
		if reqChannel == "" {
			reqChannel = "general"
		}
		if !channelAllowed(reqChannel) {
			continue
		}
		if req.Blocking || req.Required {
			requests.Blocking++
		} else {
			requests.Notices++
		}
	}

	for _, member := range b.members {
		switch member.Slug {
		case "", "human", "you", "system":
			continue
		}
		status := strings.ToLower(b.memberLiveStatusLocked(member.Slug, now))
		if status != "" && status != "idle" && status != "offline" {
			agentsActive++
		}
	}

	return tasks, requests, agentsActive
}

// handleOfficeStats serves GET /office/stats.
func (b *Broker) handleOfficeStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	actor, ok := requestActorFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	viewerSlug := strings.TrimSpace(r.URL.Query().Get("viewer_slug"))
	if viewerSlug == "" {
		viewerSlug = "human"
	}

	var stats OfficeStats
	stats.Tasks, stats.Requests, stats.AgentsActive = b.computeOfficeTaskAndRequestStats(viewerSlug)

	// Inbox attention rides the same fan-out /inbox/items serves (the
	// helpers take their own short b.mu passes). Errors degrade to zero
	// rather than failing the whole stats payload — the badge consumer
	// treats a missing count as "no badge", which matches today's
	// behaviour when /inbox/items errors.
	if items, err := b.inboxItemsForActor(actor, InboxFilterAll); err == nil {
		for _, item := range items {
			if inboxItemNeedsAttention(item) {
				stats.InboxAttention++
			}
		}
	}

	// Wiki count uses the same filter rules as /wiki/catalog (see
	// Repo.CountArticles). Filesystem walk — deliberately outside b.mu.
	if worker := b.WikiWorker(); worker != nil {
		if n, err := worker.Repo().CountArticles(); err == nil {
			stats.WikiArticles = n
		}
	}

	stats.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	writeJSON(w, http.StatusOK, stats)
}
