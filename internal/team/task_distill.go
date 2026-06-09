package team

// task_distill.go — U4.1 post-task auto-distillation
// (docs/specs/sota-uplift.md).
//
// When a task reaches done with a PASSING machine verification, the broker
// distills it into a learning record automatically — verified execution is
// the curation gate (the Voyager admission rule), so no human review stands
// between a proven outcome and the team's memory. Unverified completions
// are deliberately NOT distilled: auto-capturing unproven claims is how the
// old auto-writer turned shelves into noise.
//
// Runs as a goroutine queued AFTER the completing mutation commits, so the
// learning-log write (file I/O through the wiki worker) never runs under
// b.mu — the lock hazard that killed the previous auto-capture path.

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const taskDistillInsightDetailClip = 400

// queueTaskDistillation schedules distillation off the mutation hot path.
func (b *Broker) queueTaskDistillation(taskID string) {
	go func() {
		defer recoverPanicTo("distillCompletedTask", "task="+taskID)
		b.distillCompletedTask(taskID)
	}()
}

// distillCompletedTask writes one learning record for a verified-done task.
// Idempotent per task: a prior learning with the same TaskID short-circuits,
// so approve-after-complete and watchdog replays do not duplicate.
func (b *Broker) distillCompletedTask(taskID string) {
	b.mu.Lock()
	var task teamTask
	found := false
	if t := b.taskByIDLocked(taskID); t != nil {
		task = *t
		found = true
	}
	log := b.teamLearningLog
	b.mu.Unlock()

	if !found || log == nil || task.System {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(task.status), "done") {
		return
	}
	res := task.VerificationResult
	if res == nil || !res.Pass {
		// No machine proof → no automatic memory. The wiki/notebook path
		// (agent-initiated, librarian-curated) still covers these.
		return
	}

	existing, err := log.Search(LearningSearchFilters{Limit: MaxLearningLimit})
	if err != nil {
		return
	}
	for _, rec := range existing {
		if rec.TaskID == task.ID {
			return
		}
	}

	insight := fmt.Sprintf("Verified outcome: %s.", strings.TrimSpace(task.Title))
	if details := tailClip(task.Details, taskDistillInsightDetailClip); details != "" {
		insight += " " + details
	}
	if proof := strings.TrimSpace(res.Detail); proof != "" {
		insight += fmt.Sprintf(" Proof (%s): %s", res.Kind, truncate(proof, 200))
	}

	createdBy := strings.TrimSpace(task.Owner)
	if createdBy == "" {
		createdBy = "system"
	}
	rec := LearningRecord{
		Type:       "operational",
		Key:        normalizeChannelSlug(strings.TrimSpace(task.Title)),
		Insight:    insight,
		Confidence: 7,
		Source:     "execution",
		Trusted:    true, // machine-verified — the one capture path allowed to self-trust
		Scope:      "team",
		TaskID:     task.ID,
		Files:      extractTaskFileTargets(task.Title + " " + task.Details),
		CreatedBy:  createdBy,
		CreatedAt:  time.Now().UTC(),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = log.AppendVerified(ctx, rec)
}
