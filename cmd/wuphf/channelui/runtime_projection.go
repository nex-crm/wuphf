package channelui

import (
	"strings"

	"github.com/nex-crm/wuphf/internal/team"
)

// RuntimeTasksFromChannel projects channel UI Task slice into the
// team.RuntimeTask shape used to build a runtime snapshot. Trims
// whitespace on every string field and infers Blocked from a
// case-insensitive "blocked" status match.
func RuntimeTasksFromChannel(tasks []Task) []team.RuntimeTask {
	out := make([]team.RuntimeTask, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, team.RuntimeTask{
			ID:             task.ID,
			Title:          strings.TrimSpace(task.Title),
			Owner:          strings.TrimSpace(task.Owner),
			Status:         strings.TrimSpace(task.Status),
			PipelineStage:  strings.TrimSpace(task.PipelineStage),
			ReviewState:    strings.TrimSpace(task.ReviewState),
			ExecutionMode:  strings.TrimSpace(task.ExecutionMode),
			WorktreePath:   strings.TrimSpace(task.WorktreePath),
			WorktreeBranch: strings.TrimSpace(task.WorktreeBranch),
			Blocked:        strings.EqualFold(strings.TrimSpace(task.Status), "blocked"),
		})
	}
	return out
}

// RuntimeRequestsFromChannel projects channel UI Interview slice
// into team.RuntimeRequest. Trims string fields; preserves Blocking,
// Required, and Secret booleans verbatim.
func RuntimeRequestsFromChannel(requests []Interview) []team.RuntimeRequest {
	out := make([]team.RuntimeRequest, 0, len(requests))
	for _, req := range requests {
		out = append(out, team.RuntimeRequest{
			ID:       req.ID,
			Kind:     strings.TrimSpace(req.Kind),
			Title:    strings.TrimSpace(req.Title),
			Question: strings.TrimSpace(req.Question),
			From:     strings.TrimSpace(req.From),
			Blocking: req.Blocking,
			Required: req.Required,
			Status:   strings.TrimSpace(req.Status),
			Channel:  strings.TrimSpace(req.Channel),
			Secret:   req.Secret,
		})
	}
	return out
}

// RuntimeMessagesFromChannel projects the most recent up-to-limit
// BrokerMessages into team.RuntimeMessage. Walks newest-first so the
// returned slice preserves the original slice's tail order. limit
// defaults to 6 when non-positive.
func RuntimeMessagesFromChannel(messages []BrokerMessage, limit int) []team.RuntimeMessage {
	if limit <= 0 {
		limit = 6
	}
	out := make([]team.RuntimeMessage, 0, MinInt(len(messages), limit))
	for i := len(messages) - 1; i >= 0 && len(out) < limit; i-- {
		msg := messages[i]
		out = append(out, team.RuntimeMessage{
			ID:        msg.ID,
			From:      strings.TrimSpace(msg.From),
			Title:     strings.TrimSpace(msg.Title),
			Content:   strings.TrimSpace(msg.Content),
			ReplyTo:   strings.TrimSpace(msg.ReplyTo),
			Timestamp: strings.TrimSpace(msg.Timestamp),
		})
	}
	return out
}

// CountRunningRuntimeTasks counts tasks that are not in a terminal
// or empty state ("", "done", "completed", "canceled", "cancelled"
// — case-insensitive — are excluded). Used to drive the "N running"
// readiness pill in the workspace state.
func CountRunningRuntimeTasks(tasks []team.RuntimeTask) int {
	count := 0
	for _, task := range tasks {
		switch strings.ToLower(strings.TrimSpace(task.Status)) {
		case "", "done", "completed", "canceled", "cancelled":
			continue
		default:
			count++
		}
	}
	return count
}

// CountIsolatedRuntimeTasks counts tasks executing in an isolated
// worktree — execution mode "local_worktree" or any non-empty
// WorktreePath / WorktreeBranch. Used to surface "N isolated
// worktrees" in the recovery card so the human knows the team has
// detached state somewhere on disk.
func CountIsolatedRuntimeTasks(tasks []team.RuntimeTask) int {
	count := 0
	for _, task := range tasks {
		if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") ||
			strings.TrimSpace(task.WorktreePath) != "" ||
			strings.TrimSpace(task.WorktreeBranch) != "" {
			count++
		}
	}
	return count
}
