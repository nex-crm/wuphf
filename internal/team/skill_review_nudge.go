package team

// skill_review_nudge.go owns the broker-side task creation that fires
// when the per-agent SkillCounter crosses its threshold. The task lands
// in the agent's home channel and asks them to review recent activity
// and propose a skill if the work pattern is reusable.

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// skillReviewNudgeTaskType is the TaskType field stamped on the nudge
// task. The web UI / agent dispatcher can branch on this value if it
// later wants to render the nudge specially.
const skillReviewNudgeTaskType = "skill_review_nudge"

// fireSkillReviewNudgeLocked creates a Task in agentSlug's lane asking
// them to review their recent tool-call activity and propose a reusable
// skill via team_skill_create(action=propose).
//
// Caller must hold b.mu. The task is written through the same
// task-creation pipeline used everywhere else in the broker, so the
// existing dispatcher picks it up on the agent's next turn. We do NOT
// call b.saveLocked here — the caller (the tool-event handler) is
// responsible for persisting after appending its own audit entry, so
// one save covers both writes.
func (b *Broker) fireSkillReviewNudgeLocked(agentSlug string) (string, error) {
	agentSlug = strings.TrimSpace(agentSlug)
	if agentSlug == "" {
		return "", fmt.Errorf("fireSkillReviewNudgeLocked: agentSlug required")
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Build the task body from the counter's ring buffer. If the counter
	// is somehow nil or empty (race during boot, deliberate test stub),
	// we still fire the task with a generic body — better to over-nudge
	// than to silently drop a counter event.
	var recent []recentToolCall
	if b.skillCounter != nil {
		recent = b.skillCounter.RecentToolCalls(agentSlug, 10)
	}
	details := buildSkillReviewNudgeBody(len(recent), recent)

	channel := b.preferredTaskChannelLocked("general", agentSlug, agentSlug, skillReviewNudgeTitle, details)

	b.counter++
	task := teamTask{
		ID:            fmt.Sprintf("task-skill-nudge-%d", b.counter),
		Channel:       channel,
		Title:         skillReviewNudgeTitle,
		Details:       details,
		Owner:         agentSlug,
		Status:        "in_progress",
		CreatedBy:     "system",
		TaskType:      skillReviewNudgeTaskType,
		PipelineID:    "skill_review",
		ExecutionMode: "office",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
	b.queueTaskBehindActiveOwnerLaneLocked(&task)
	if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
		return "", fmt.Errorf("rejectTheaterTask: %w", err)
	}
	b.scheduleTaskLifecycleLocked(&task)
	if err := b.syncTaskWorktreeLocked(&task); err != nil {
		return "", fmt.Errorf("syncTaskWorktree: %w", err)
	}
	b.tasks = append(b.tasks, task)
	b.appendActionLocked(
		"task_created",
		"office",
		channel,
		"system",
		truncateSummary(task.Title+" ["+agentSlug+"]", 140),
		task.ID,
	)

	slog.Info("skill_counter_nudge_fired",
		"agent", agentSlug,
		"task_id", task.ID,
		"channel", channel,
		"recent_calls", len(recent),
	)

	return task.ID, nil
}

const skillReviewNudgeTitle = "Skill review: codify what you've been doing"

// buildSkillReviewNudgeBody renders the task body. n is the count of
// listed calls (kept separate so the prelude reads cleanly when the
// counter ring buffer is short).
func buildSkillReviewNudgeBody(n int, recent []recentToolCall) string {
	var sb strings.Builder
	if n == 0 {
		sb.WriteString("You've made enough tool calls since your last skill_create / skill_patch to trigger a skill-review check.\n\n")
		sb.WriteString("Survey the patterns in your recent work. ")
	} else {
		fmt.Fprintf(&sb, "You've made %d recent tool calls since your last skill_create / skill_patch.\n\nRecent activity:\n", n)
		for _, r := range recent {
			summary := strings.TrimSpace(r.Summary)
			if summary == "" {
				summary = "(no summary)"
			}
			ts := r.At.UTC().Format(time.RFC3339)
			fmt.Fprintf(&sb, "- %s (%s) at %s\n", r.ToolName, truncateSummary(summary, 120), ts)
		}
		sb.WriteString("\nSurvey these patterns. ")
	}
	sb.WriteString("If you see a CLASS of work that's worth codifying as a reusable skill, ")
	sb.WriteString("call `team_skill_create(action=propose)` with a class-first name ")
	sb.WriteString("(e.g., `handle-deploy-failures`, NOT `fix-the-build-i-just-did`). ")
	sb.WriteString("The proposal goes through the existing human-approval gate.\n\n")
	sb.WriteString("If nothing stands out as reusable, just say \"Nothing to save\" and close this task.")
	return sb.String()
}
