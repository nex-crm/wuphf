package team

// playbook_draft.go — B3 playbook detection + deterministic draft articles
// (docs/specs/core-loop.md, Core Loop step 7.2).
//
// The same queued distillation path that writes entity facts
// (task_distill.go) detects REPEATABLE work and drafts a playbook article
// under the existing playbooks path (team/playbooks/<slug>.md). The
// heuristic is deterministic — no LLM:
//
//   - the task reached done with a PASSING machine verification AND its
//     learning record was distilled (enforced by the call site in
//     distillCompletedTask), AND
//   - the task has a Definition with at least two success criteria
//     (shouldDraftPlaybook), AND
//   - update-first: when an existing playbook's slug is similar
//     (Jaro-Winkler, same tier-1 gate as skill dedup), the task is appended
//     to that playbook as an additional worked example instead of creating
//     a new file.
//
// The draft skeleton is assembled deterministically from the task's
// Definition: goal → Purpose, success criteria → Checklist, ledger/turn
// journal highlights → Steps skeleton, artifact → worked example. Claims
// carry footnote citations tying them back to the source task + artifact —
// the same citation discipline as B2's entity articles. Frontmatter is
// stamped `draft: true` so humans (and the wiki UI) can tell a machine
// draft from a curated playbook; the skill compiler still scans it, which
// is the bridge into step 7.3 (skills + policies).

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	// playbooksDirPrefix is the wiki-relative directory playbook articles
	// live in — the path the skill compiler already scans.
	playbooksDirPrefix = "team/playbooks/"

	// playbookDraftSlugThreshold is the Jaro-Winkler slug-similarity gate
	// for update-first: at or above it, the new task folds into the
	// existing playbook. Same default as the skill dedup tier-1 gate.
	playbookDraftSlugThreshold = 0.85

	// playbookStepSaidClip bounds how much of a ledger entry's message tail
	// lands in the Steps skeleton.
	playbookStepSaidClip = 200

	// maxPlaybookDraftSteps bounds the Steps skeleton.
	maxPlaybookDraftSteps = 6
)

// playbookDraftMarker identifies a machine-drafted playbook body.
const playbookDraftMarker = "<!-- wuphf:playbook-draft"

// playbookDraftHeaderComment is the managed-content notice at the top of
// every drafted playbook body.
const playbookDraftHeaderComment = playbookDraftMarker + ` — drafted deterministically from verified task completions (core-loop B3). New similar completions append worked examples. The skill compiler scans this article; bullets under a "## Rules" or "## Policies" section compile into office policies. -->`

// playbookFootnoteDefPattern matches existing footnote definitions so an
// appended worked example can pick the next stable number.
var playbookFootnoteDefPattern = regexp.MustCompile(`(?m)^\[\^(\d+)\]:`)

// playbookDraftMu serializes the read-modify-write envelope around draft
// commits so two tasks completing concurrently cannot lose one another's
// worked examples. The WikiWorker queue serializes the commits themselves.
var playbookDraftMu sync.Mutex

// playbookArticlePath returns the stable wiki-relative path for a playbook.
func playbookArticlePath(slug string) string {
	return playbooksDirPrefix + slug + ".md"
}

// playbookSlugForTask derives the playbook slug from the task title using
// the same normalizer the learning store uses, so the slug satisfies the
// wiki's ^[a-z0-9][a-z0-9_-]*$ conventions for arbitrary titles.
func playbookSlugForTask(task teamTask) string {
	return learningKeyFromTitle(task.Title)
}

// shouldDraftPlaybook is the deterministic repeatability heuristic: the
// task carries a Definition with at least two non-empty success criteria.
// The call site additionally requires a verified-done outcome whose
// learning record was just distilled.
func shouldDraftPlaybook(task teamTask) bool {
	def := task.Definition
	if def == nil {
		return false
	}
	criteria := 0
	for _, c := range def.SuccessCriteria {
		if strings.TrimSpace(c) != "" {
			criteria++
		}
	}
	return criteria >= 2
}

// findSimilarPlaybookSlug scans the on-disk playbooks directory for an
// article whose slug matches (exactly or by Jaro-Winkler similarity).
// Returns the best-matching existing slug, or "" when none clears the
// threshold. Deterministic: ties resolve to the lexicographically first
// slug because the directory listing is sorted.
func findSimilarPlaybookSlug(repoRoot, slug string) string {
	entries, err := os.ReadDir(filepath.Join(repoRoot, filepath.FromSlash(strings.TrimSuffix(playbooksDirPrefix, "/"))))
	if err != nil {
		return ""
	}
	best := ""
	bestScore := 0.0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, ".") {
			continue
		}
		existing := strings.TrimSuffix(name, ".md")
		if existing == slug {
			return existing
		}
		if score := JaroWinkler(slug, existing); score >= playbookDraftSlugThreshold && score > bestScore {
			best = existing
			bestScore = score
		}
	}
	return best
}

// playbookWorkedExampleLine renders the cited worked-example bullet for a
// task. footnote is the citation number the bullet references.
func playbookWorkedExampleLine(task teamTask, footnote int) string {
	line := fmt.Sprintf("- Task %s — %q", task.ID, strings.TrimSpace(task.Title))
	if artifact := strings.TrimSpace(task.Artifact); artifact != "" {
		line += fmt.Sprintf(" — artifact: [%s](%s)", artifact, artifact)
	}
	return fmt.Sprintf("%s[^%d]", line, footnote)
}

// playbookFootnoteLine renders the footnote definition citing the task and
// its artifact — the same citation shape as B2's entity articles.
func playbookFootnoteLine(task teamTask, footnote int) string {
	parts := []string{"Task " + task.ID}
	if artifact := strings.TrimSpace(task.Artifact); artifact != "" {
		parts = append(parts, fmt.Sprintf("artifact: [%s](%s)", artifact, artifact))
	}
	by := strings.TrimSpace(task.Owner)
	if by == "" {
		by = "system"
	}
	return fmt.Sprintf("[^%d]: %s; completed by %s.", footnote, strings.Join(parts, " — "), by)
}

// playbookDraftSteps assembles the Steps skeleton: turn-journal highlights
// (ledger entries with a message tail) first, falling back to the
// Definition's deliverables when the journal is empty.
func playbookDraftSteps(task teamTask) []string {
	var steps []string
	for _, entry := range task.Ledger {
		said := strings.TrimSpace(strings.Join(strings.Fields(entry.Said), " "))
		if said == "" {
			continue
		}
		steps = append(steps, truncate(said, playbookStepSaidClip))
		if len(steps) >= maxPlaybookDraftSteps {
			return steps
		}
	}
	if len(steps) > 0 {
		return steps
	}
	if def := task.Definition; def != nil {
		for _, d := range def.Deliverables {
			name := strings.TrimSpace(d.Name)
			if name == "" {
				continue
			}
			step := "Produce the " + name
			if format := strings.TrimSpace(d.Format); format != "" {
				step += " (" + format + ")"
			}
			steps = append(steps, step+".")
			if len(steps) >= maxPlaybookDraftSteps {
				break
			}
		}
	}
	return steps
}

// buildPlaybookDraftArticle assembles a brand-new draft playbook from one
// verified task. Pure deterministic template assembly.
func buildPlaybookDraftArticle(task teamTask, slug string) string {
	def := task.Definition
	title := humanizeSlugForBrief(slug)

	var b strings.Builder
	b.WriteString("---\ndraft: true\n---\n")
	b.WriteString(playbookDraftHeaderComment)
	b.WriteString("\n\n# ")
	b.WriteString(title)
	b.WriteString("\n\n")

	b.WriteString("## Purpose\n\n")
	goal := strings.TrimSpace(task.Title)
	if def != nil && strings.TrimSpace(def.Goal) != "" {
		goal = strings.TrimSpace(def.Goal)
	}
	b.WriteString(goal)
	b.WriteString("[^1]\n\n")

	if def != nil && len(def.SuccessCriteria) > 0 {
		b.WriteString("## Checklist\n\n")
		for _, c := range def.SuccessCriteria {
			c = strings.TrimSpace(c)
			if c == "" {
				continue
			}
			b.WriteString("- [ ] ")
			b.WriteString(c)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if steps := playbookDraftSteps(task); len(steps) > 0 {
		b.WriteString("## Steps\n\n")
		for i, s := range steps {
			fmt.Fprintf(&b, "%d. %s\n", i+1, s)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Worked examples\n\n")
	b.WriteString(playbookWorkedExampleLine(task, 1))
	b.WriteString("\n\n")

	b.WriteString("## References\n\n")
	b.WriteString(playbookFootnoteLine(task, 1))
	b.WriteString("\n")
	return b.String()
}

// appendPlaybookWorkedExample folds a new verified task into an existing
// playbook article as an additional worked example (update-first). The
// bullet lands under "## Worked examples" (created at the end when the
// article lacks one — e.g. a human-authored playbook) and the citation
// under "## References". Idempotent per task: when the body already cites
// the task ID, the article is returned unchanged.
func appendPlaybookWorkedExample(existing string, task teamTask) string {
	if strings.Contains(existing, task.ID) {
		return existing
	}
	footnote := 1
	for _, m := range playbookFootnoteDefPattern.FindAllStringSubmatch(existing, -1) {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil && n >= footnote {
			footnote = n + 1
		}
	}
	example := playbookWorkedExampleLine(task, footnote)
	citation := playbookFootnoteLine(task, footnote)

	body := strings.TrimRight(existing, "\n")
	body = insertLineUnderSection(body, "## Worked examples", example)
	body = insertLineUnderSection(body, "## References", citation)
	return body + "\n"
}

// insertLineUnderSection appends line at the END of the named H2 section
// (just before the next "## " heading), creating the section at the end of
// the document when it does not exist.
func insertLineUnderSection(body, heading, line string) string {
	lines := strings.Split(body, "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == heading {
			start = i
			break
		}
	}
	if start < 0 {
		return body + "\n\n" + heading + "\n\n" + line
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	// Trim trailing blank lines inside the section so the new entry sits
	// directly under the last one.
	insert := end
	for insert > start+1 && strings.TrimSpace(lines[insert-1]) == "" {
		insert--
	}
	out := make([]string, 0, len(lines)+2)
	out = append(out, lines[:insert]...)
	out = append(out, line)
	if end < len(lines) {
		out = append(out, "")
	}
	out = append(out, lines[insert:]...)
	return strings.Join(out, "\n")
}

// draftPlaybookFromTask is the B3 trigger body: detect repeatable shape,
// pick create-vs-update by slug similarity, and commit the draft through
// the WikiWorker queue under the archivist identity. Runs from the queued
// distillation goroutine — never under b.mu. Failures are logged, never
// fatal: a missing draft is recoverable on the next similar completion.
func draftPlaybookFromTask(ctx context.Context, worker *WikiWorker, task teamTask) {
	if worker == nil || task.System || !shouldDraftPlaybook(task) {
		return
	}
	repo := worker.Repo()
	if repo == nil {
		return
	}
	slug := playbookSlugForTask(task)
	if slug == "" || !slugPattern.MatchString(slug) {
		return
	}

	playbookDraftMu.Lock()
	defer playbookDraftMu.Unlock()

	target := slug
	mode := "create"
	var body string
	if existingSlug := findSimilarPlaybookSlug(repo.Root(), slug); existingSlug != "" {
		// Update-first: append the task as another worked example.
		target = existingSlug
		mode = "update"
		raw, err := readArticle(repo, playbookArticlePath(existingSlug))
		if err != nil {
			log.Printf("playbook draft: read %s: %v", playbookArticlePath(existingSlug), err)
			return
		}
		updated := appendPlaybookWorkedExample(string(raw), task)
		if updated == string(raw) {
			return // already cited — replayed distillation
		}
		body = updated
	} else {
		body = buildPlaybookDraftArticle(task, slug)
	}

	relPath := playbookArticlePath(target)
	msg := fmt.Sprintf("archivist: %s playbook draft %s from %s", mode, target, task.ID)
	if _, _, err := worker.Enqueue(ctx, ArchivistAuthor, relPath, body, "replace", msg); err != nil {
		log.Printf("playbook draft: commit %s: %v", relPath, err)
	}
}
