package team

// playbook_commit.go owns the two non-.md commit paths used by the v1.3
// playbook surface:
//
//   - CommitPlaybookSkill     — writes team/playbooks/.compiled/{slug}/SKILL.md
//   - CommitPlaybookExecution — appends to team/playbooks/{slug}.executions.jsonl
//
// The standard Repo.Commit path enforces the .md extension under team/ AND
// regens the wiki catalog. Playbook compilations are .md too, but live
// under a hidden `.compiled` subdirectory that we deliberately keep OUT of
// the catalog — the compiled skill is a tool, not an article. Execution
// logs are .jsonl and also must not regen the catalog.
//
// Both paths use the same locking + identity plumbing as entity_commit.go.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	compiledSkillPathPattern = regexp.MustCompile(`^team/playbooks/\.compiled/[a-z0-9][a-z0-9-]*/SKILL\.md$`)
	executionLogPathPattern  = regexp.MustCompile(`^team/playbooks/[a-z0-9][a-z0-9-]*\.executions\.jsonl$`)
)

// CommitPlaybookSkill writes content to the canonical compiled-skill path
// and commits it. Does NOT regen index/all.md — the compiled subdirectory
// is hidden from the catalog on purpose.
func (r *Repo) CommitPlaybookSkill(ctx context.Context, slug, relPath, content, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("playbook commit: author slug is required")
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !compiledSkillPathPattern.MatchString(clean) {
		return "", 0, fmt.Errorf("playbook commit: path must match team/playbooks/.compiled/{slug}/SKILL.md; got %q", relPath)
	}
	if content == "" {
		return "", 0, fmt.Errorf("playbook commit: content is required")
	}

	fullPath := filepath.Join(r.root, clean)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("playbook commit: mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		return "", 0, fmt.Errorf("playbook commit: write: %w", err)
	}

	if out, err := r.runGitLocked(ctx, slug, "add", "--", clean); err != nil {
		return "", 0, fmt.Errorf("playbook commit: git add: %w: %s", err, out)
	}

	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("playbook commit: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("playbook commit: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(content), nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = "archivist: compile " + clean
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("playbook commit: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("playbook commit: resolve HEAD: %w", err)
	}
	return strings.TrimSpace(sha), len(content), nil
}

// CommitPlaybookExecution appends-in-full to the jsonl execution log.
// Same "replace-with-merged-bytes" pattern as entity facts.
func (r *Repo) CommitPlaybookExecution(ctx context.Context, slug, relPath, content, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("playbook execution: author slug is required")
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !executionLogPathPattern.MatchString(clean) {
		return "", 0, fmt.Errorf("playbook execution: path must match team/playbooks/{slug}.executions.jsonl; got %q", relPath)
	}
	if content == "" {
		return "", 0, fmt.Errorf("playbook execution: content is required")
	}

	fullPath := filepath.Join(r.root, clean)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("playbook execution: mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		return "", 0, fmt.Errorf("playbook execution: write: %w", err)
	}
	if out, err := r.runGitLocked(ctx, slug, "add", "--", clean); err != nil {
		return "", 0, fmt.Errorf("playbook execution: git add: %w: %s", err, out)
	}
	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("playbook execution: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("playbook execution: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(content), nil
	}
	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = "playbook execution: update " + clean
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("playbook execution: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("playbook execution: resolve HEAD: %w", err)
	}
	return strings.TrimSpace(sha), len(content), nil
}
