package team

// task_completion_hook.go — the B1 deterministic completion hook
// (docs/specs/core-loop.md, Core Loop steps 6–7.1).
//
// When a task reaches done (complete/approve landing in terminal done,
// the same commit point that queues task_distill.go), the broker:
//
//  1. Enforces the artifact requirement: a task WITH a Definition (R4)
//     cannot reach done until a delivered artifact is on record
//     (TaskPostRequest.ArtifactPath → teamTask.Artifact). The gate lives
//     in broker_tasks_mutation_service.go; the validator lives here.
//  2. Posts a deterministic done-post to the task channel and raises a
//     non-blocking Inbox notice (humanInterview kind="notice") —
//     "<task> delivered: <summary> — artifact: <link>". No LLM call.
//  3. Extracts entities deterministically (explicit @mentions, capitalized
//     multi-word names in goal/deliverables) and records them through the
//     EXISTING entity fact log + cross-entity graph (entity_facts.go,
//     entity_graph.go) from the queued distillation goroutine — off the
//     broker hot path, never under b.mu. After the facts land, B2
//     (entity_article.go) deterministically regenerates the touched
//     entities' wiki articles from the same goroutine.

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// maxTaskCompletionEntities bounds how many entities one task completion can
// write facts for, so a pathological title/goal cannot fan out unbounded
// wiki-worker commits.
const maxTaskCompletionEntities = 8

// taskDeliveredMessageKind is the chat-message kind for the deterministic
// done-post. The FE can render it as a plain system line until it grows a
// dedicated card.
const taskDeliveredMessageKind = "task_delivered"

// validateTaskArtifactPath sanity-checks an artifact reference: a
// wiki-relative path or a visual-artifact id. Rejects absolute paths and
// traversal so the stored value is always safe to render as a wiki link.
func validateTaskArtifactPath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return fmt.Errorf("artifact_path is empty")
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") {
		return fmt.Errorf("artifact_path must be wiki-relative (or a visual-artifact id), not absolute: %q", p)
	}
	if strings.Contains(p, "..") {
		return fmt.Errorf("artifact_path must not contain path traversal: %q", p)
	}
	return nil
}

// taskDeliveredSummaryLine assembles the deterministic delivery summary:
// the first success criterion when the task has a Definition with criteria,
// else the Definition goal, else the task title. Pure string assembly.
func taskDeliveredSummaryLine(task *teamTask) string {
	if task == nil {
		return ""
	}
	if def := task.Definition; def != nil {
		if len(def.SuccessCriteria) > 0 && strings.TrimSpace(def.SuccessCriteria[0]) != "" {
			return strings.TrimSpace(def.SuccessCriteria[0])
		}
		if goal := strings.TrimSpace(def.Goal); goal != "" {
			return goal
		}
	}
	return strings.TrimSpace(task.Title)
}

// taskDeliveredContentLine renders the canonical done-post body:
// "<task> delivered: <summary line> — artifact: <link>". The artifact
// segment is omitted for legacy (no-Definition) tasks that landed done
// without one.
func taskDeliveredContentLine(task *teamTask) string {
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	line := fmt.Sprintf("%s delivered: %s", title, taskDeliveredSummaryLine(task))
	if artifact := strings.TrimSpace(task.Artifact); artifact != "" {
		line += " — artifact: " + artifact
	}
	return line
}

// postTaskDeliveredLocked is the deterministic done-post (B1 step 2): one
// chat message in the task channel plus one non-blocking Inbox notice.
// System tasks are skipped (internal bookkeeping, not deliveries).
//
// Caller holds b.mu for write. No file I/O, no LLM, no wiki-worker calls —
// string assembly and in-memory appends only (the auto-notebook-writer
// deadlock rule).
func (b *Broker) postTaskDeliveredLocked(task *teamTask) {
	if b == nil || task == nil || task.System {
		return
	}
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	owner := strings.TrimSpace(task.Owner)
	now := time.Now().UTC().Format(time.RFC3339)
	content := taskDeliveredContentLine(task)

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:           fmt.Sprintf("msg-%d", b.counter),
		From:         "system",
		Channel:      taskChannel,
		Kind:         taskDeliveredMessageKind,
		Title:        title,
		Content:      content,
		Tagged:       dedupeReassignTags([]string{"ceo", owner}),
		Timestamp:    now,
		ReplyTo:      strings.TrimSpace(task.ThreadID),
		SourceTaskID: task.ID,
	})

	// Inbox notice: the smallest existing non-blocking inbox primitive is a
	// humanInterview row; kind="notice" keeps it non-blocking/non-required
	// (requestNeedsHumanDecision falls through to Required=false) and gives
	// it a single Acknowledge option (requestOptionDefaults). No reminder
	// scheduling — a delivery notice must never nag.
	noticeFrom := owner
	if noticeFrom == "" {
		noticeFrom = "system"
	}
	b.counter++
	notice := humanInterview{
		ID:        fmt.Sprintf("request-%d", b.counter),
		Kind:      "notice",
		Status:    "pending",
		From:      noticeFrom,
		Channel:   taskChannel,
		Title:     fmt.Sprintf("%s delivered", task.ID),
		Question:  content,
		Blocking:  false,
		Required:  false,
		ReplyTo:   strings.TrimSpace(task.ThreadID),
		IssueID:   task.ID,
		CreatedAt: now,
		UpdatedAt: now,
	}
	notice.Options, notice.RecommendedID = normalizeRequestOptions(notice.Kind, "", nil)
	b.requests = append(b.requests, notice)
	b.pendingInterview = firstBlockingRequest(b.requests)
	b.appendActionLocked("request_created", "office", taskChannel, noticeFrom, truncateSummary(notice.Title+" "+notice.Question, 140), notice.ID)
}

// ── Deterministic entity extraction (B1 step 3) ──────────────────────────────

// taskEntityMentionExclusions are actor slugs that are office plumbing, not
// knowledge-graph entities. @-mentions of these never produce facts.
var taskEntityMentionExclusions = map[string]struct{}{
	"human":  {},
	"you":    {},
	"system": {},
	"broker": {},
	"nex":    {},
}

// capitalizedNamePattern matches multi-word capitalized names ("Acme Corp",
// "Jane Smith Consulting") — two or more capitalized words in sequence.
var capitalizedNamePattern = regexp.MustCompile(`\b[A-Z][A-Za-z0-9&'.-]*(?: [A-Z][A-Za-z0-9&'.-]*)+\b`)

// taskCompletionEntity is one deterministically extracted entity reference.
type taskCompletionEntity struct {
	Kind EntityKind
	Slug string
	Name string
}

// taskCompletionEntities extracts entity references from a completed task —
// deterministic only, no LLM:
//
//   - explicit @mentions in title/details/goal/deliverables → people
//     (agents and humans referenced by slug), minus plumbing slugs;
//   - capitalized multi-word names in the Definition goal + deliverable
//     names (per B1: goal/deliverables only) → companies, slugified via
//     the existing slugify normalizer and validated against slugPattern.
//
// Bounded to maxTaskCompletionEntities, first-seen order.
func taskCompletionEntities(task teamTask) []taskCompletionEntity {
	goal := ""
	deliverableText := ""
	if def := task.Definition; def != nil {
		goal = def.Goal
		names := make([]string, 0, len(def.Deliverables))
		for _, d := range def.Deliverables {
			names = append(names, d.Name)
		}
		deliverableText = strings.Join(names, "\n")
	}

	seen := map[string]struct{}{}
	out := make([]taskCompletionEntity, 0, 8)
	add := func(e taskCompletionEntity) {
		if len(out) >= maxTaskCompletionEntities {
			return
		}
		if e.Slug == "" || !slugPattern.MatchString(e.Slug) {
			return
		}
		key := string(e.Kind) + "/" + e.Slug
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, e)
	}

	mentionSource := strings.Join([]string{task.Title, task.Details, goal, deliverableText}, "\n")
	for _, slug := range parseAtMentions(mentionSource) {
		if _, excluded := taskEntityMentionExclusions[slug]; excluded {
			continue
		}
		add(taskCompletionEntity{Kind: EntityKindPeople, Slug: slug, Name: "@" + slug})
	}
	nameSource := goal + "\n" + deliverableText
	for _, name := range capitalizedNamePattern.FindAllString(nameSource, -1) {
		add(taskCompletionEntity{Kind: EntityKindCompanies, Slug: slugify(name), Name: strings.TrimSpace(name)})
	}
	return out
}

// taskCompletionFactText renders the deterministic fact recorded on each
// extracted entity. The text carries the task→entity association (task id),
// the task→artifact association (produced artifact), and kinded wikilinks
// for every co-occurring entity so the existing graph extractor
// (EntityGraph.RecordFactRefs → ExtractRefs) records the entity↔entity
// co-occurrence edges. Stable content (no timestamps) so the fact log's
// deterministic-ID dedup absorbs replays.
func taskCompletionFactText(task teamTask, entities []taskCompletionEntity, self taskCompletionEntity) string {
	goal := strings.TrimSpace(task.Title)
	if def := task.Definition; def != nil && strings.TrimSpace(def.Goal) != "" {
		goal = strings.TrimSpace(def.Goal)
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Completed task %s (%q) involved this entity. Goal: %s.", task.ID, strings.TrimSpace(task.Title), goal)
	if artifact := strings.TrimSpace(task.Artifact); artifact != "" {
		fmt.Fprintf(&sb, " Produced artifact: %s.", artifact)
	}
	links := make([]string, 0, len(entities))
	for _, e := range entities {
		if e.Kind == self.Kind && e.Slug == self.Slug {
			continue
		}
		links = append(links, fmt.Sprintf("[[%s/%s]]", e.Kind, e.Slug))
	}
	if len(links) > 0 {
		fmt.Fprintf(&sb, " Co-occurring entities: %s.", strings.Join(links, ", "))
	}
	return sb.String()
}

// taskCompletionFactSourcePath maps the task artifact onto the fact log's
// source_path contract (must start with agents/ or team/). Visual-artifact
// ids and other references fall back to empty.
func taskCompletionFactSourcePath(task teamTask) string {
	artifact := strings.TrimSpace(task.Artifact)
	if strings.HasPrefix(artifact, "team/") || strings.HasPrefix(artifact, "agents/") {
		return artifact
	}
	return ""
}

// recordTaskCompletionEntityFacts writes one fact per extracted entity
// through the existing fact-log path and rides the existing graph hook so
// co-occurrence edges land in team/entities/.graph.jsonl. Runs from the
// queued distillation goroutine — never under b.mu. Failures are logged,
// never fatal: the knowledge graph is additive intelligence.
func recordTaskCompletionEntityFacts(ctx context.Context, factLog *FactLog, graph *EntityGraph, task teamTask) {
	if factLog == nil || task.System {
		return
	}
	entities := taskCompletionEntities(task)
	if len(entities) == 0 {
		return
	}
	recordedBy := strings.TrimSpace(task.Owner)
	if recordedBy == "" {
		recordedBy = "system"
	}
	sourcePath := taskCompletionFactSourcePath(task)
	for _, e := range entities {
		fact, err := factLog.Append(ctx, e.Kind, e.Slug, taskCompletionFactText(task, entities, e), sourcePath, recordedBy)
		if err != nil {
			log.Printf("completion hook: record entity fact %s/%s for %s: %v", e.Kind, e.Slug, task.ID, err)
			continue
		}
		if graph != nil {
			if _, gerr := graph.RecordFactRefs(ctx, fact); gerr != nil {
				log.Printf("completion hook: record graph refs %s/%s for %s: %v", e.Kind, e.Slug, task.ID, gerr)
			}
		}
	}
}
