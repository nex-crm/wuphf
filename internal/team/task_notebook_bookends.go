package team

// task_notebook_bookends.go — the B4 deterministic notebook bookends
// (docs/specs/core-loop.md, Core Loop step 5: "private KG generates
// notebooks").
//
// Every defined task gets one per-(agent, task) notebook note at
// agents/{slug}/notebook/{task-id}.md — the existing flat notebook
// convention (notebook_worker.go forbids subdirectories), written through
// the same wiki worker queue every other notebook write rides:
//
//   - PRE bookend: queued from the FIRST headless-turn enqueue for the
//     (agent, task) pair. A deterministic research skeleton: the task
//     Definition, the retrieved knowledge block the packet carried, and an
//     empty Research section the agent fills mid-task with the existing
//     notebook tools. CREATE-only: an existing note (the agent got there
//     first) is never overwritten, so the bookend can never race or stomp
//     agent writes.
//   - POST bookend: appended by the completion hook when the task reaches
//     verified done (task_distill.go). Deliverable link, the distilled
//     learning, and the last ledger entries. Idempotent via an HTML-comment
//     marker, so distillation replays (approve-after-complete, watchdog)
//     append exactly once.
//
// Both sections are generated views of structured data — Definition,
// retrieval manifest, verification result, ledger — not LLM output. This is
// where "the private KG generates notebooks" starts.

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// taskNotebookWriteTimeout bounds each bookend write. Same class as
// autoNotebookWriteTimeout; the writes run from queued goroutines, never
// under b.mu.
const taskNotebookWriteTimeout = 30 * time.Second

// taskNotebookPostMarker makes the post-task append idempotent: a note that
// already carries the marker is never appended to again.
const taskNotebookPostMarker = "<!-- wuphf:post-task -->"

// taskNotebookLedgerHighlightLimit caps the ledger lines the post-task
// section carries.
const taskNotebookLedgerHighlightLimit = 3

// taskNotebookEntryPath is the per-(agent, task) note location, following
// the existing flat agents/{slug}/notebook/ convention.
func taskNotebookEntryPath(slug, taskID string) string {
	return fmt.Sprintf("agents/%s/notebook/%s.md", slug, taskID)
}

// queueTaskNotebookPreBookend fires the pre-task research note for the
// (slug, task) pair exactly once per launcher lifetime. Called from the
// headless enqueue funnel — the in-memory dedupe is the only work done on
// the caller's path; broker reads and the notebook write happen in the
// queued goroutine, and the write itself rides the wiki worker queue.
func (l *Launcher) queueTaskNotebookPreBookend(slug, taskID string) {
	if l == nil || l.broker == nil {
		return
	}
	slug = strings.TrimSpace(slug)
	taskID = strings.TrimSpace(taskID)
	if slug == "" || taskID == "" || !IsSafeTaskID(taskID) || validateNotebookSlug(slug) != nil {
		return
	}
	key := slug + "|" + taskID
	l.notebookBookendMu.Lock()
	if l.notebookBookendSeen == nil {
		l.notebookBookendSeen = map[string]struct{}{}
	}
	if _, done := l.notebookBookendSeen[key]; done {
		l.notebookBookendMu.Unlock()
		return
	}
	l.notebookBookendSeen[key] = struct{}{}
	l.notebookBookendMu.Unlock()

	go func() {
		defer recoverPanicTo("taskNotebookPreBookend", key)
		l.writeTaskNotebookPreBookend(slug, taskID)
	}()
}

// writeTaskNotebookPreBookend builds and commits the pre-task note. Only
// tasks with a Definition (the R4 intake contract) get one — chat turns and
// legacy undefined tasks produce no notebook ceremony.
func (l *Launcher) writeTaskNotebookPreBookend(slug, taskID string) {
	task := l.broker.TaskByID(taskID)
	if task == nil || task.System || task.Definition == nil {
		return
	}
	worker := l.broker.WikiWorker()
	if worker == nil {
		return
	}
	knowledge, manifest := l.notifyCtx().taskKnowledgeContext(*task)
	body := renderTaskNotebookPreSection(*task, slug, knowledge, manifest)
	ctx, cancel := context.WithTimeout(context.Background(), taskNotebookWriteTimeout)
	defer cancel()
	// CREATE-only: if the agent (or a concurrent enqueue path) already
	// created the note, leave it alone — the bookend must never overwrite
	// agent-authored content.
	if _, _, err := worker.NotebookWrite(ctx, slug, taskNotebookEntryPath(slug, taskID), body, "create",
		fmt.Sprintf("notebook: pre-task research note for %s", taskID)); err != nil {
		if !strings.Contains(err.Error(), "already exists") {
			log.Printf("notebook bookend: pre-task note for %s/%s: %v", slug, taskID, err)
		}
	}
}

// renderTaskNotebookPreSection is the deterministic pre-task skeleton: the
// Definition, the retrieved context block, and an empty Research section.
func renderTaskNotebookPreSection(task teamTask, slug, knowledge string, manifest []string) string {
	var b strings.Builder
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	fmt.Fprintf(&b, "# %s — pre-task research\n\n", title)
	fmt.Fprintf(&b, "- task: %s\n", task.ID)
	fmt.Fprintf(&b, "- agent: @%s\n", slug)
	if owner := strings.TrimSpace(task.Owner); owner != "" {
		fmt.Fprintf(&b, "- owner: @%s\n", owner)
	}
	b.WriteString("\n## Definition\n\n")
	for _, line := range taskDefinitionPacketLines(task.Definition) {
		b.WriteString(strings.TrimRight(line, " "))
		b.WriteString("\n")
	}
	b.WriteString("\n## Retrieved context\n\n")
	if strings.TrimSpace(knowledge) == "" {
		b.WriteString("- none matched this task\n")
	} else {
		b.WriteString(knowledge)
		b.WriteString("\n")
		if len(manifest) > 0 {
			fmt.Fprintf(&b, "\ncontext ids: %s\n", strings.Join(manifest, ", "))
		}
	}
	b.WriteString("\n## Research\n")
	return b.String()
}

// appendTaskNotebookPostBookend appends the post-task section to the
// owner's note when the task reaches verified done. Called from the queued
// distillation goroutine (task_distill.go) — never under b.mu. Idempotent
// via taskNotebookPostMarker; append_section creates the note when the pre
// bookend never ran (legacy tasks that gained a Definition late).
func appendTaskNotebookPostBookend(ctx context.Context, worker *WikiWorker, task teamTask, learningInsight string) {
	if worker == nil || task.System || task.Definition == nil {
		return
	}
	slug := strings.TrimSpace(task.Owner)
	if slug == "" || validateNotebookSlug(slug) != nil || !IsSafeTaskID(task.ID) {
		return
	}
	path := taskNotebookEntryPath(slug, task.ID)
	if raw, err := worker.NotebookRead(path); err == nil && strings.Contains(string(raw), taskNotebookPostMarker) {
		return
	}
	section := renderTaskNotebookPostSection(task, learningInsight)
	writeCtx, cancel := context.WithTimeout(ctx, taskNotebookWriteTimeout)
	defer cancel()
	if _, _, err := worker.NotebookWrite(writeCtx, slug, path, section, "append_section",
		fmt.Sprintf("notebook: post-task section for %s", task.ID)); err != nil {
		log.Printf("notebook bookend: post-task section for %s/%s: %v", slug, task.ID, err)
	}
}

// renderTaskNotebookPostSection is the deterministic post-task section:
// deliverable link, verification proof, the distilled learning, and the
// last ledger entries.
func renderTaskNotebookPostSection(task teamTask, learningInsight string) string {
	var b strings.Builder
	b.WriteString("## Post-task\n")
	b.WriteString(taskNotebookPostMarker)
	b.WriteString("\n\n")
	if artifact := strings.TrimSpace(task.Artifact); artifact != "" {
		fmt.Fprintf(&b, "- delivered artifact: [%s](%s)\n", artifact, artifact)
	} else {
		b.WriteString("- delivered artifact: none recorded\n")
	}
	if res := task.VerificationResult; res != nil && res.Pass {
		detail := strings.TrimSpace(res.Detail)
		if detail == "" {
			detail = "passed"
		}
		fmt.Fprintf(&b, "- verified (%s): %s\n", res.Kind, truncate(detail, 400))
	}
	if insight := strings.TrimSpace(learningInsight); insight != "" {
		b.WriteString("\n### Learnings distilled\n\n")
		fmt.Fprintf(&b, "- %s\n", insight)
	}
	entries := task.Ledger
	if len(entries) > taskNotebookLedgerHighlightLimit {
		entries = entries[len(entries)-taskNotebookLedgerHighlightLimit:]
	}
	if len(entries) > 0 {
		b.WriteString("\n### Ledger highlights\n\n")
		for _, e := range entries {
			line := fmt.Sprintf("- [%s @%s] %s", e.At, e.Agent, e.Outcome)
			if e.Said != "" {
				line += " — " + truncate(e.Said, 200)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}
