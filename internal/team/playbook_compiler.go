package team

// playbook_compiler.go is the v1.3 compounding-intelligence compiler.
//
// Flow:
//
//	team/playbooks/{slug}.md            (human/agent-authored wiki article)
//	          │
//	          │ CompilePlaybook(repo, wikiPath)
//	          ▼
//	team/playbooks/.compiled/{slug}/SKILL.md   (invokable Claude Code skill)
//
// Any commit to team/playbooks/*.md enqueues a recompile via the same
// WikiWorker single-writer queue the rest of the wiki rides on. The compiled
// SKILL.md is committed under the `archivist` synthetic identity so the
// audit trail matches the entity-brief pattern (see entity_synthesizer.go).
//
// The compiled skill is NOT an independent document — it is a prompt that
// instructs any invoking agent to (a) read the source playbook, (b) execute
// the "What to do" steps, and (c) record the outcome via the
// playbook_execution_record MCP tool. That closes the loop:
//
//	read playbook → execute → record outcome → recompile → next agent starts smarter.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// PlaybookCompiledDirRel is the directory (under the wiki root) where the
// compiled SKILL.md files live. Exposed so tests + handlers can resolve
// skill paths without re-deriving the layout.
const PlaybookCompiledDirRel = "team/playbooks/.compiled"

// ArchivistAuthor is defined in entity_synthesizer.go — compilation reuses
// the same synthetic identity. Any write to team/playbooks/.compiled/...
// should be authored by the archivist, never by a roster agent.

// ErrNotAPlaybook is returned when CompilePlaybook is called on a path that
// is not a team/playbooks/*.md article.
var ErrNotAPlaybook = errors.New("playbook: path must be team/playbooks/{slug}.md (and not under .compiled/)")

// playbookPathPattern matches the source articles we compile. The .compiled
// subdirectory is explicitly excluded — we never compile our own output.
var playbookPathPattern = regexp.MustCompile(`^team/playbooks/([a-z0-9][a-z0-9-]*)\.md$`)

// PlaybookSlugFromPath returns the slug of a team/playbooks/{slug}.md path,
// or ("", false) when the path is not a source playbook.
func PlaybookSlugFromPath(relPath string) (string, bool) {
	clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(relPath)))
	m := playbookPathPattern.FindStringSubmatch(clean)
	if len(m) != 2 {
		return "", false
	}
	return m[1], true
}

// IsPlaybookPath returns true when relPath is a source playbook article.
func IsPlaybookPath(relPath string) bool {
	_, ok := PlaybookSlugFromPath(relPath)
	return ok
}

// CompiledSkillRelPath returns the wiki-relative path to the compiled skill
// for the given slug. Does not guarantee the file exists.
func CompiledSkillRelPath(slug string) string {
	return filepath.ToSlash(filepath.Join(PlaybookCompiledDirRel, slug, "SKILL.md"))
}

// ExecutionLogRelPath returns the wiki-relative path to the append-only
// execution JSONL log for a given slug.
func ExecutionLogRelPath(slug string) string {
	return filepath.ToSlash(filepath.Join("team", "playbooks", slug+".executions.jsonl"))
}

// CompilePlaybook reads a team/playbooks/{slug}.md article and writes the
// corresponding SKILL.md under team/playbooks/.compiled/{slug}/SKILL.md.
//
// The write goes directly to disk (caller is responsible for committing it
// via the wiki worker — see WikiWorker.EnqueuePlaybookCompile). Returns the
// wiki-relative path AND the rendered skill bytes. Callers that need to
// commit the output must use the returned bytes — reading the file back
// from disk is racy under filesystem pressure in CI: an empty buffer has
// been observed between WriteFile and a subsequent ReadFile of the same
// path, which then fails downstream as "content is required". Eliminating
// the round-trip is strictly cheaper than hardening it.
//
// Idempotency: invoking CompilePlaybook with unchanged source input produces
// byte-identical output. The downstream git layer collapses byte-identical
// writes into a no-op, so the audit log stays clean.
func CompilePlaybook(repo *Repo, wikiPath string) (string, []byte, error) {
	if repo == nil {
		return "", nil, fmt.Errorf("playbook: repo is required")
	}
	slug, ok := PlaybookSlugFromPath(wikiPath)
	if !ok {
		return "", nil, fmt.Errorf("%w: got %q", ErrNotAPlaybook, wikiPath)
	}

	sourceFull := filepath.Join(repo.Root(), filepath.FromSlash(wikiPath))
	sourceBytes, err := os.ReadFile(sourceFull)
	if err != nil {
		return "", nil, fmt.Errorf("playbook: read source %s: %w", wikiPath, err)
	}
	source := string(sourceBytes)

	skill := renderCompiledSkill(slug, wikiPath, source)
	skillBytes := []byte(skill)

	relSkill := CompiledSkillRelPath(slug)
	skillFull := filepath.Join(repo.Root(), filepath.FromSlash(relSkill))
	if err := os.MkdirAll(filepath.Dir(skillFull), 0o700); err != nil {
		return "", nil, fmt.Errorf("playbook: mkdir compiled dir: %w", err)
	}
	if err := os.WriteFile(skillFull, skillBytes, 0o600); err != nil {
		return "", nil, fmt.Errorf("playbook: write compiled skill: %w", err)
	}
	return relSkill, skillBytes, nil
}

// CompilePlaybookAndCommit runs CompilePlaybook and, when the output is new
// (not byte-identical to HEAD), commits it under the archivist identity via
// the supplied wiki worker. Returns the compiled path + short SHA.
//
// Prefer WikiWorker.EnqueuePlaybookCompile from handler code — that route
// hits the single-writer queue. This helper is here so the worker's drain
// goroutine can reuse the same compile-and-commit logic.
func CompilePlaybookAndCommit(ctx context.Context, repo *Repo, wikiPath string) (string, string, error) {
	relSkill, skillBytes, err := CompilePlaybook(repo, wikiPath)
	if err != nil {
		return "", "", err
	}
	slug, _ := PlaybookSlugFromPath(wikiPath)
	msg := fmt.Sprintf("archivist: compile playbook %s", slug)
	sha, _, cerr := repo.CommitPlaybookSkill(ctx, ArchivistAuthor, relSkill, string(skillBytes), msg)
	if cerr != nil {
		return relSkill, "", cerr
	}
	return relSkill, sha, nil
}

// renderCompiledSkill produces the SKILL.md content for a given playbook.
// The frontmatter + body shape is intentionally minimal and deterministic:
// same input → byte-identical output, so re-compiling a stable playbook is
// a no-op commit.
//
// Frontmatter keys:
//   - name            slug, kebab-case
//   - description     one-line extracted from the source (first paragraph)
//     — agents see this when deciding whether to invoke
//   - allowed-tools   the three MCP tools an execution run needs:
//     team_wiki_read (read the playbook), and the v1.3
//     playbook_* tools (invoke + record outcome).
//   - source_path     back-link to the authored wiki article
//   - compiled_by     always "archivist" — see ArchivistAuthor
func renderCompiledSkill(slug, sourcePath, source string) string {
	description := extractPlaybookDescription(source)
	title := extractPlaybookTitle(source, slug)

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: " + slug + "\n")
	b.WriteString("description: " + description + "\n")
	b.WriteString("allowed-tools: team_wiki_read, playbook_list, playbook_execution_record\n")
	b.WriteString("source_path: " + sourcePath + "\n")
	b.WriteString("compiled_by: " + ArchivistAuthor + "\n")
	b.WriteString("---\n\n")

	b.WriteString("# Playbook: " + title + "\n\n")
	b.WriteString("You are executing the **" + title + "** playbook.\n\n")
	b.WriteString("## How to run\n\n")
	b.WriteString("1. Call `team_wiki_read` with `article_path=\"" + sourcePath + "\"` to load the canonical playbook body.\n")
	b.WriteString("2. Parse the \"What to do\" section (or the body if no section header exists) and execute each step using the MCP tools you already have access to. Do NOT invent steps the playbook does not contain.\n")
	b.WriteString("3. When the execution finishes (success, partial success, or aborted), call `playbook_execution_record` with:\n")
	b.WriteString("   - `slug`: `" + slug + "`\n")
	b.WriteString("   - `outcome`: `success` | `partial` | `aborted`\n")
	b.WriteString("   - `summary`: one paragraph describing what actually happened and what you changed.\n")
	b.WriteString("   - `notes` (optional): anything the next runner should know that is not already captured by the playbook text.\n\n")
	b.WriteString("## Guarantees\n\n")
	b.WriteString("- The execution log at `" + ExecutionLogRelPath(slug) + "` is append-only — wrong outcomes are corrected by adding a new entry, never by editing or deleting an existing one.\n")
	b.WriteString("- This skill recompiles automatically whenever the source playbook changes. Do not edit `SKILL.md` directly; edit `" + sourcePath + "` instead.\n\n")
	b.WriteString("## Source\n\n")
	b.WriteString("The canonical playbook lives at `" + sourcePath + "`. This file is a deterministic compilation — see `internal/team/playbook_compiler.go`.\n")
	return b.String()
}

// extractPlaybookTitle returns the first H1 from the source or the slug.
func extractPlaybookTitle(source, fallback string) string {
	body := stripFrontmatter(source)
	if h := headerLineFrom(body); h != "" {
		return h
	}
	return fallback
}

// extractPlaybookDescription returns a sensible one-line description for the
// compiled frontmatter. Preference order:
//  1. First non-empty, non-heading line of the body.
//  2. "Playbook: {title}" fallback.
//
// Output is clipped to 240 chars so a runaway paragraph does not bloat the
// frontmatter.
func extractPlaybookDescription(source string) string {
	const maxLen = 240
	body := stripFrontmatter(source)
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "---") {
			continue
		}
		// Avoid yaml-unsafe characters: collapse to a single line and strip
		// leading markdown decoration.
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.ReplaceAll(line, "\r", " ")
		if len(line) > maxLen {
			line = line[:maxLen-1] + "…"
		}
		// Strip quotes to avoid breaking the yaml scalar.
		line = strings.ReplaceAll(line, "\"", "'")
		return line
	}
	return "Compiled playbook skill."
}
