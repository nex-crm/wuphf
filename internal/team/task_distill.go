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
	"log"
	"strings"
	"time"
)

const taskDistillInsightDetailClip = 400

// learningKeyFromTitle produces a key that satisfies the learning store's
// ^[a-z0-9][a-z0-9_-]*$ pattern from an arbitrary task title. Titles with
// punctuation ("Fix #42: crash v2.0") previously produced invalid keys and
// the distillation silently no-opped (review HIGH finding).
func learningKeyFromTitle(title string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		switch {
		case isAlnum:
			b.WriteRune(r)
			lastDash = false
		case !lastDash && b.Len() > 0:
			b.WriteRune('-')
			lastDash = true
		}
	}
	key := strings.Trim(b.String(), "-")
	if key == "" {
		key = "task"
	}
	if len(key) > MaxLearningKeyLen {
		key = strings.Trim(key[:MaxLearningKeyLen], "-")
	}
	return key
}

// taskDistillInsight renders the verified-outcome learning text. Pure
// string assembly, shared by the learning record and the notebook
// post-task bookend.
func taskDistillInsight(task teamTask, res *TaskVerificationResult) string {
	insight := fmt.Sprintf("Verified outcome: %s.", strings.TrimSpace(task.Title))
	if details := tailClip(task.Details, taskDistillInsightDetailClip); details != "" {
		insight += " " + details
	}
	if res != nil {
		if proof := strings.TrimSpace(res.Detail); proof != "" {
			insight += fmt.Sprintf(" Proof (%s): %s", res.Kind, truncate(proof, 200))
		}
	}
	return insight
}

// queueTaskDistillation schedules distillation off the mutation hot path.
func (b *Broker) queueTaskDistillation(taskID string) {
	b.bgWG.Add(1)
	go func() {
		defer b.bgWG.Done()
		defer recoverPanicTo("distillCompletedTask", "task="+taskID)
		b.distillCompletedTask(taskID)
	}()
}

// WaitBackground blocks until all fire-and-forget broker goroutines that write
// to disk (distillation, wiki promotion) have finished. Used on shutdown / test
// teardown so their writes never race state removal.
func (b *Broker) WaitBackground() {
	if b == nil {
		return
	}
	b.bgWG.Wait()
}

// distillCompletedTask writes one learning record for a verified-done task.
// Idempotent per task: a prior learning with the same TaskID short-circuits,
// so approve-after-complete and watchdog replays do not duplicate.
func (b *Broker) distillCompletedTask(taskID string) {
	b.mu.Lock()
	// Single-flight per task: two distillation goroutines for the same task
	// (approve-after-complete double fire) could both pass the Search-based
	// dedup below and write twice (review MEDIUM finding).
	if b.distillInFlight == nil {
		b.distillInFlight = map[string]struct{}{}
	}
	if _, busy := b.distillInFlight[taskID]; busy {
		b.mu.Unlock()
		return
	}
	b.distillInFlight[taskID] = struct{}{}
	var task teamTask
	found := false
	if t := b.taskByIDLocked(taskID); t != nil {
		task = *t
		found = true
	}
	llog := b.teamLearningLog
	factLog := b.factLog
	graph := b.entityGraph
	worker := b.wikiWorker
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.distillInFlight, taskID)
		b.mu.Unlock()
	}()

	if !found || task.System {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(task.status), "done") {
		return
	}

	// B1 entity extraction (task_completion_hook.go): every non-system done
	// task records its deterministically extracted entities + associations
	// into the team knowledge graph via the existing fact-log path. Runs
	// before the verification gate below — entity facts do not require a
	// machine-verified outcome, learnings do.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	recordTaskCompletionEntityFacts(ctx, factLog, graph, task)

	// B2 (entity_article.go): with the facts + graph edges committed, the
	// touched entities' wiki articles are regenerated deterministically —
	// the wiki-worker queue serializes the commits, this goroutine is
	// already off the broker hot path, and no LLM is involved.
	regenerateTaskEntityArticles(ctx, worker, factLog, graph, task)

	res := task.VerificationResult
	if res == nil || !res.Pass {
		// No machine proof → no automatic memory. The wiki/notebook path
		// (agent-initiated, librarian-curated) still covers these.
		return
	}

	insight := taskDistillInsight(task, res)

	// B4 post-task bookend (task_notebook_bookends.go): verified done
	// appends the deliverable link + distilled learning + ledger highlights
	// to the owner's per-task notebook note. Idempotent — replays of this
	// goroutine append exactly once.
	appendTaskNotebookPostBookend(ctx, worker, task, insight)

	if llog == nil {
		return
	}

	existing, err := llog.Search(LearningSearchFilters{Limit: MaxLearningLimit})
	if err != nil {
		return
	}
	for _, rec := range existing {
		if rec.TaskID == task.ID {
			return
		}
	}

	createdBy := strings.TrimSpace(task.Owner)
	if createdBy == "" {
		createdBy = "system"
	}
	rec := LearningRecord{
		Type:       "operational",
		Key:        learningKeyFromTitle(task.Title),
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
	if _, err := llog.AppendVerified(ctx, rec); err != nil {
		// Surface, never swallow: a verified outcome that fails to land in
		// team memory is a broken compounding loop, not a cosmetic miss.
		log.Printf("task distill: failed to record verified learning for %s: %v", taskID, err)
		return
	}

	// B3 (playbook_draft.go): a verified-done task whose Definition shows a
	// repeatable shape (≥2 success criteria) — and whose learning record
	// just landed — drafts (or updates, by slug similarity) a playbook
	// article under team/playbooks/. Deterministic skeleton, no LLM; the
	// skill-compile cron later turns the playbook into skills + policies.
	draftPlaybookFromTask(ctx, worker, task)
}
