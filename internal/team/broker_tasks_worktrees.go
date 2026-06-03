package team

import (
	"fmt"
	"strings"
)

func taskNeedsLocalWorktree(task *teamTask) bool {
	if task == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return false
	}
	if strings.TrimSpace(task.Owner) == "" {
		return false
	}
	switch strings.TrimSpace(task.status) {
	case "", "open":
		return false
	case "done":
		return strings.TrimSpace(task.WorktreePath) != "" || strings.TrimSpace(task.WorktreeBranch) != ""
	default:
		return true
	}
}

func taskBlockReasonLooksLikeWorkspaceWriteIssue(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	markers := []string{
		"read-only",
		"read only",
		"writable workspace",
		"write access",
		"filesystem sandbox",
		"workspace sandbox",
		"operation not permitted",
		"permission denied",
	}
	for _, marker := range markers {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

func rejectFalseLocalWorktreeBlock(task *teamTask, reason string) error {
	if task == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return nil
	}
	if !taskBlockReasonLooksLikeWorkspaceWriteIssue(reason) {
		return nil
	}
	worktreePath := strings.TrimSpace(task.WorktreePath)
	if worktreePath == "" {
		return nil
	}
	if err := verifyTaskWorktreeWritable(worktreePath); err == nil {
		return fmt.Errorf("assigned local worktree is writable at %s; do not request writable-workspace approval; continue implementation in that worktree", worktreePath)
	}
	return nil
}

// taskRequiresExclusiveOwnerTurn reports whether a task must be the OWNER's only
// active task of its kind — i.e. admission control queues a second such task
// behind the first instead of letting both run at once.
//
// Only live_external still requires this. External side-effects (sends, posts,
// remote mutations) have no per-task workspace to isolate them, so running two
// at once for one owner risks double-sends and races on shared remote state;
// serializing them per owner (with a clean "queued behind" dependency) is the
// safe default.
//
// local_worktree used to be serialized here too, but parallel instances now run
// distinct-worktree tasks for one owner concurrently. Safety no longer needs
// admission control: the headless scheduler keys dispatch lanes by worktree
// path (see headlessLane / laneForTurn), so two worktree tasks for one owner
// run in parallel only when their worktrees differ, and collapse to one
// serialized lane whenever they SHARE a tree (e.g. a dependent reusing its
// parent's worktree). The filesystem boundary is enforced at dispatch, not by
// withholding the second task.
func taskRequiresExclusiveOwnerTurn(task *teamTask) bool {
	if task == nil {
		return false
	}
	if strings.TrimSpace(task.Owner) == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "live_external")
}

func taskStatusConsumesExclusiveOwnerTurn(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "in_progress", "review":
		return true
	default:
		return false
	}
}

func (b *Broker) syncTaskWorktreeLocked(task *teamTask) error {
	if task == nil {
		return nil
	}
	// Automatically assign local_worktree mode when a coding agent claims a task.
	if task.ExecutionMode == "" && codingAgentSlugs[strings.TrimSpace(task.Owner)] {
		switch strings.TrimSpace(task.status) {
		case "", "open", "done":
			// not yet in-progress; leave mode unset
		default:
			task.ExecutionMode = "local_worktree"
		}
	}
	if taskNeedsLocalWorktree(task) {
		if strings.TrimSpace(task.WorktreePath) != "" || strings.TrimSpace(task.WorktreeBranch) != "" {
			if taskWorktreeSourceLooksUsable(task.WorktreePath) {
				return nil
			}
			if err := cleanupTaskWorktree(task.WorktreePath, task.WorktreeBranch); err != nil {
				return err
			}
			task.WorktreePath = ""
			task.WorktreeBranch = ""
		}
		if path, branch := b.reusableDependencyWorktreeLocked(task); path != "" && branch != "" {
			task.WorktreePath = path
			task.WorktreeBranch = branch
			return nil
		}
		path, branch, err := prepareTaskWorktree(task.ID)
		if err != nil {
			return err
		}
		task.WorktreePath = path
		task.WorktreeBranch = branch
		return nil
	}

	if strings.TrimSpace(task.WorktreePath) == "" && strings.TrimSpace(task.WorktreeBranch) == "" {
		return nil
	}
	if err := cleanupTaskWorktree(task.WorktreePath, task.WorktreeBranch); err != nil {
		return err
	}
	task.WorktreePath = ""
	task.WorktreeBranch = ""
	return nil
}

func (b *Broker) reusableDependencyWorktreeLocked(task *teamTask) (string, string) {
	if b == nil || task == nil || len(task.DependsOn) == 0 {
		return "", ""
	}
	owner := strings.TrimSpace(task.Owner)
	var fallbackPath string
	var fallbackBranch string
	for _, depID := range task.DependsOn {
		depID = strings.TrimSpace(depID)
		if depID == "" {
			continue
		}
		for i := range b.tasks {
			dep := &b.tasks[i]
			if strings.TrimSpace(dep.ID) != depID {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(dep.ExecutionMode), "local_worktree") {
				continue
			}
			path := strings.TrimSpace(dep.WorktreePath)
			branch := strings.TrimSpace(dep.WorktreeBranch)
			if path == "" || branch == "" {
				continue
			}
			status := strings.ToLower(strings.TrimSpace(dep.status))
			review := strings.ToLower(strings.TrimSpace(dep.reviewState))
			if status != "review" && status != "done" && review != "ready_for_review" && review != "approved" {
				continue
			}
			if owner != "" && strings.TrimSpace(dep.Owner) == owner {
				return path, branch
			}
			if fallbackPath == "" && fallbackBranch == "" {
				fallbackPath = path
				fallbackBranch = branch
			}
		}
	}
	return fallbackPath, fallbackBranch
}

func (b *Broker) activeExclusiveOwnerTaskLocked(owner, excludeTaskID string) *teamTask {
	owner = strings.TrimSpace(owner)
	excludeTaskID = strings.TrimSpace(excludeTaskID)
	if b == nil || owner == "" {
		return nil
	}
	for i := range b.tasks {
		task := &b.tasks[i]
		if excludeTaskID != "" && strings.TrimSpace(task.ID) == excludeTaskID {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(task.Owner), owner) {
			continue
		}
		if !taskRequiresExclusiveOwnerTurn(task) {
			continue
		}
		if !taskStatusConsumesExclusiveOwnerTurn(task.status) {
			continue
		}
		return task
	}
	return nil
}

func (b *Broker) queueTaskBehindActiveOwnerLaneLocked(task *teamTask) {
	if b == nil || task == nil {
		return
	}
	if !taskRequiresExclusiveOwnerTurn(task) {
		return
	}
	if !taskStatusConsumesExclusiveOwnerTurn(task.status) {
		return
	}
	active := b.activeExclusiveOwnerTaskLocked(task.Owner, task.ID)
	if active == nil {
		return
	}
	if !stringSliceContainsFold(task.DependsOn, active.ID) {
		task.DependsOn = append(task.DependsOn, active.ID)
	}
	b.markTaskQueuedBehindActiveOwnerLocked(task)
	queueNote := fmt.Sprintf("Queued behind %s so @%s only carries one active %s lane at a time.", active.ID, strings.TrimSpace(task.Owner), strings.TrimSpace(task.ExecutionMode))
	switch existing := strings.TrimSpace(task.Details); {
	case existing == "":
		task.Details = queueNote
	case !strings.Contains(existing, queueNote):
		task.Details = existing + "\n\n" + queueNote
	}
}

// preferredTaskChannelLocked resolves the channel slug for a task.
// If the caller supplied an explicit non-empty channel it is returned
// as-is (after normalisation).  An empty / whitespace-only request
// falls back to "general".
//
// The old behaviour of scanning recent execution channels and routing
// business-objective tasks there has been removed.  Each new
// business-objective task now gets its own dedicated channel (minted
// by createPerTaskChannelLocked in the individual create paths); this
// function is now purely a slug normaliser.
func (b *Broker) preferredTaskChannelLocked(requestedChannel, _, _, _, _ string) string {
	channel := normalizeChannelSlug(requestedChannel)
	if channel == "" {
		return "general"
	}
	return channel
}

// shouldMintPerTaskChannel reports whether a newly created task
// warrants a dedicated task-<id> channel.
//
// The product vision is "every task spins up its own channel", so the
// default is to mint for any real, top-level task. We only withhold a
// channel for the three internal-plumbing cases that genuinely belong in
// #general (or on a parent):
//  1. The resolved channel is "general" — an explicit non-general channel
//     request is already a real channel, so leave it alone.
//  2. It is not a system task (System==true is the Backup & Migration
//     entry, which always owns "general").
//  3. It is not an incident self-heal (PipelineID=="incident") — those are
//     internal tooling, not user work.
//  4. It is not a sub-issue (ParentIssueID!="") — sub-issues share their
//     parent task's channel.
//
// Note: this used to additionally require taskLooksLikeLiveBusinessObjective
// (a keyword heuristic). That under-delivered the vision — a real task whose
// title lacked execution keywords ("Draft Q3 outbound sequence") stayed in
// #general — so the heuristic was dropped (2026-06-03). The function is still
// used elsewhere (notifications / pipeline), just not as a channel gate.
func shouldMintPerTaskChannel(channel string, task *teamTask) bool {
	if normalizeChannelSlug(channel) != "general" {
		return false
	}
	if task == nil {
		return false
	}
	if task.System {
		return false
	}
	if strings.TrimSpace(task.PipelineID) == "incident" {
		return false
	}
	if strings.TrimSpace(task.ParentIssueID) != "" {
		return false
	}
	return true
}

// createPerTaskChannelLocked mints a dedicated channel for a task.
// Slug: "task-<taskID>".  Name: task title (or slug if title is empty).
// Members: owner (if a registered member) + actor (the creator).
// TaskID is set on the returned channel so the UI can correlate the
// two.  Caller MUST hold b.mu.  Returns nil if channel creation fails
// (caller should keep the task in "general" in that case).
func (b *Broker) createPerTaskChannelLocked(taskID, title, owner, actor string) *teamChannel {
	slug := "task-" + taskID
	name := strings.TrimSpace(title)
	if name == "" {
		name = slug
	}
	// Build the member list from known-registered actors only —
	// createChannelLocked validates every entry against findMemberLocked
	// and returns an error for unknown slugs.
	members := make([]string, 0, 2)
	if o := normalizeActorSlug(owner); o != "" && o != "ceo" && b.findMemberLocked(o) != nil {
		members = append(members, o)
	}
	// Actor may be "human", "you", "system", "ceo", or a specialist
	// slug.  Trusted senders are not in the members list so skip them;
	// createChannelLocked will return an error for unknown slugs.
	actorNorm := normalizeActorSlug(actor)
	isAlreadyMember := false
	for _, m := range members {
		if m == actorNorm {
			isAlreadyMember = true
			break
		}
	}
	if !isAlreadyMember && actorNorm != "" && actorNorm != "ceo" &&
		!isHumanMessageSender(actorNorm) && actorNorm != "system" &&
		actorNorm != "nex" && b.findMemberLocked(actorNorm) != nil {
		members = append(members, actorNorm)
	}
	ch, cerr := b.createChannelLocked(channelCreateInput{
		Slug:      slug,
		Name:      name,
		Members:   members,
		CreatedBy: actorNorm,
	})
	if cerr != nil {
		return nil
	}
	// Link channel back to its owning task so the UI can correlate.
	ch.TaskID = taskID
	return ch
}
