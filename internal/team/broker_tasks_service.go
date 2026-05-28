package team

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var errTaskChannelAccessDenied = errors.New("task channel access denied")
var errTaskAckInvalid = errors.New("id and slug required")
var errTaskAckOwnerOnly = errors.New("only the task owner can ack")
var errTaskNotFound = errors.New("task not found")
var errTaskPersistFailed = errors.New("failed to persist")

// ListActiveIssueSummariesForPrompt returns slim summaries of every open
// Issue (not yet approved / rejected / done / cancelled) across all
// channels. Used by promptBuilder to render the ACTIVE ISSUES catalog
// block into agent system prompts.
//
// Internal accessor: no channel-access check (the prompt builder is
// office-wide and pre-auth). Slim by design — title + state + channel +
// owner is enough for the agent to decide "reuse via comment" vs
// "create new"; full body content would bloat every system prompt on
// every spawn.
func (b *Broker) ListActiveIssueSummariesForPrompt() []IssueSummary {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]IssueSummary, 0, len(b.tasks))
	for _, t := range b.tasks {
		st := strings.ToLower(strings.TrimSpace(t.Status()))
		switch st {
		case "done", "approved", "rejected", "cancelled", "canceled":
			continue
		}
		ls := strings.ToLower(strings.TrimSpace(string(t.LifecycleState)))
		switch ls {
		case "approved", "rejected":
			continue
		}
		state := ls
		if state == "" {
			state = st
		}
		if state == "" {
			state = "open"
		}
		out = append(out, IssueSummary{
			ID:      t.ID,
			Title:   strings.TrimSpace(t.Title),
			State:   state,
			Channel: normalizeChannelSlug(t.Channel),
			Owner:   strings.TrimSpace(t.Owner),
		})
	}
	return out
}

func (b *Broker) ListTasks(req TaskListRequest) (TaskListResponse, error) {
	statusFilter := strings.TrimSpace(req.StatusFilter)
	mySlug := strings.TrimSpace(req.MySlug)
	viewerSlug := strings.TrimSpace(req.ViewerSlug)
	channel := normalizeChannelSlug(req.Channel)
	allChannels := req.AllChannels
	includeDone := req.IncludeDone
	parentIssueID := strings.TrimSpace(req.ParentIssueID)

	b.mu.Lock()
	defer b.mu.Unlock()

	if !allChannels && !b.canAccessChannelLocked(viewerSlug, channel) {
		return TaskListResponse{}, errTaskChannelAccessDenied
	}

	result := make([]teamTask, 0, len(b.tasks))
	// allChannels=true must NOT bypass channel authorization. Without this
	// per-task check, an authenticated viewer could enumerate every task in
	// every channel, including private ones they aren't a member of,
	// just by passing all_channels=true. Apply the same access predicate
	// to each candidate channel before letting the task into the response.
	allChannelsCache := make(map[string]bool)
	channelAllowed := func(slug string) bool {
		if !allChannels {
			return true
		}
		if v, ok := allChannelsCache[slug]; ok {
			return v
		}
		v := b.canAccessChannelLocked(viewerSlug, slug)
		allChannelsCache[slug] = v
		return v
	}
	for _, task := range b.tasks {
		taskChannel := normalizeChannelSlug(task.Channel)
		if !allChannels && taskChannel != channel {
			continue
		}
		if !channelAllowed(taskChannel) {
			continue
		}
		if task.status == "done" && !includeDone && statusFilter == "" {
			continue
		}
		if statusFilter != "" && task.status != statusFilter {
			continue
		}
		if mySlug != "" && task.Owner != "" && task.Owner != mySlug {
			continue
		}
		// Sub-issue filter: only return tasks whose parent matches when
		// caller asked for a specific parent. When empty, no filter
		// applies — top-level Issues and sub-issues mingle as before.
		if parentIssueID != "" && task.ParentIssueID != parentIssueID {
			continue
		}
		// Specialist visibility filter (Slice 7): when the viewer is a
		// specialist agent (not human, not CEO/lead), they only see
		// Issues they own OR Issues where they own a sub-issue. CEO +
		// human see everything as before. Web UI uses viewer_slug=human
		// so the human-facing surface is unchanged.
		if !b.viewerCanSeeTaskLocked(viewerSlug, &task) {
			continue
		}
		result = append(result, task)
	}

	return TaskListResponse{Channel: channel, Tasks: result}, nil
}

// viewerCanSeeTaskLocked enforces the specialist-visibility rule. CEO,
// human, and internal recovery actors see everything. A specialist sees
// only Issues they own OR Issues where they own a sub-issue (so the
// parent context is visible when they own a child).
// Caller holds b.mu.
func (b *Broker) viewerCanSeeTaskLocked(viewerSlug string, task *teamTask) bool {
	if task == nil {
		return false
	}
	v := strings.ToLower(strings.TrimSpace(viewerSlug))
	// Human + internal recovery + empty viewer all see everything.
	// Empty viewer is the legacy path used by tests + CLI tools that
	// never pass viewer_slug.
	switch v {
	case "", "human", "you", "system", "broker", "nex":
		return true
	}
	leadSlug := strings.ToLower(strings.TrimSpace(officeLeadSlugFrom(b.members)))
	// No lead established yet (pre-onboarding / test fixtures) → no
	// visibility scoping; everyone sees everything.
	if leadSlug == "" {
		return true
	}
	if v == leadSlug {
		return true
	}
	// Only registered specialists get the visibility filter. Test
	// slugs / external callers / CLI tools pass arbitrary viewer
	// slugs we can't validate as a real specialist, so allow them
	// through. The same pattern as the mutation gate in
	// checkTaskActionAuthLocked.
	if b.findMemberLocked(v) == nil {
		return true
	}
	// Specialist owns the task directly.
	if strings.EqualFold(strings.TrimSpace(task.Owner), v) {
		return true
	}
	// Specialist owns a sub-issue whose parent is this task. Allow them
	// to read the parent so the breadcrumb works.
	for i := range b.tasks {
		child := &b.tasks[i]
		if !strings.EqualFold(strings.TrimSpace(child.ParentIssueID), task.ID) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(child.Owner), v) {
			return true
		}
	}
	return false
}

func (b *Broker) AckTask(req TaskAckRequest) (TaskResponse, error) {
	taskID := strings.TrimSpace(req.ID)
	slug := strings.TrimSpace(req.Slug)
	channel := normalizeChannelSlug(req.Channel)
	if channel == "" {
		channel = "general"
	}
	if taskID == "" || slug == "" {
		return TaskResponse{}, errTaskAckInvalid
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == taskID && normalizeChannelSlug(b.tasks[i].Channel) == channel {
			if b.tasks[i].Owner != slug {
				return TaskResponse{}, errTaskAckOwnerOnly
			}
			now := time.Now().UTC().Format(time.RFC3339)
			b.tasks[i].AckedAt = now
			b.tasks[i].UpdatedAt = now
			if err := b.saveLocked(); err != nil {
				return TaskResponse{}, fmt.Errorf("%w: %w", errTaskPersistFailed, err)
			}
			return TaskResponse{Task: b.tasks[i]}, nil
		}
	}
	return TaskResponse{}, errTaskNotFound
}
