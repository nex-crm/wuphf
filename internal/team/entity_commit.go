package team

// entity_commit.go owns the repo-level write that appends one fact to the
// append-only fact log at team/entities/{kind}-{slug}.facts.jsonl. The
// standard Repo.Commit path rejects non-.md extensions, so entity writes
// ride their own thin method — same locking, same identity plumbing.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var entityFactPathPattern = regexp.MustCompile(`^team/entities/(people|companies|customers)-[a-z0-9][a-z0-9-]*\.facts\.jsonl$`)

// CommitEntityFact writes the given content to relPath inside the wiki
// repo and commits it under the supplied slug. Always uses "replace"
// semantics — the caller owns the merge (the fact log appends in memory
// and submits the full file bytes).
func (r *Repo) CommitEntityFact(ctx context.Context, slug, relPath, content, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("entity commit: author slug is required")
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !entityFactPathPattern.MatchString(clean) {
		return "", 0, fmt.Errorf("entity commit: path must match team/entities/{kind}-{slug}.facts.jsonl; got %q", relPath)
	}
	if content == "" {
		return "", 0, fmt.Errorf("entity commit: content is required")
	}

	fullPath := filepath.Join(r.root, clean)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("entity commit: mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		return "", 0, fmt.Errorf("entity commit: write: %w", err)
	}

	if out, err := r.runGitLocked(ctx, slug, "add", "--", clean); err != nil {
		return "", 0, fmt.Errorf("entity commit: git add: %w: %s", err, out)
	}

	// Byte-identical re-write is a no-op. Report current HEAD.
	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("entity commit: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("entity commit: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(content), nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = "fact: update " + clean
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("entity commit: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("entity commit: resolve HEAD: %w", err)
	}
	return strings.TrimSpace(sha), len(content), nil
}

// lintReportPathPattern validates wiki/.lint/report-YYYY-MM-DD.md paths.
var lintReportPathPattern = regexp.MustCompile(`^wiki/\.lint/report-\d{4}-\d{2}-\d{2}\.md$`)

// CommitLintReport writes the daily lint report markdown to wiki/.lint/ and
// commits it under the supplied slug (typically ArchivistAuthor).
func (r *Repo) CommitLintReport(ctx context.Context, slug, relPath, content, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("lint commit: author slug is required")
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !lintReportPathPattern.MatchString(clean) {
		return "", 0, fmt.Errorf("lint commit: path must match wiki/.lint/report-YYYY-MM-DD.md; got %q", relPath)
	}
	if content == "" {
		return "", 0, fmt.Errorf("lint commit: content is required")
	}

	fullPath := filepath.Join(r.root, clean)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", 0, fmt.Errorf("lint commit: mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return "", 0, fmt.Errorf("lint commit: write: %w", err)
	}

	if out, err := r.runGitLocked(ctx, slug, "add", "--", clean); err != nil {
		return "", 0, fmt.Errorf("lint commit: git add: %w: %s", err, out)
	}

	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("lint commit: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("lint commit: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(content), nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("archivist: lint report %s", relPath)
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("lint commit: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("lint commit: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), len(content), nil
}

// factLogPathPattern validates wiki/facts/{kind}/{slug}.jsonl paths (new schema §3).
var factLogPathPattern = regexp.MustCompile(`^wiki/facts/[^/]+/[^/]+\.jsonl$`)

// CommitFactLog writes the given content to relPath inside wiki/facts/ and
// commits it under the supplied slug. Used by ResolveContradiction to mutate
// fact records that live in the new-schema fact log location.
func (r *Repo) CommitFactLog(ctx context.Context, slug, relPath, content, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("fact commit: author slug is required")
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !factLogPathPattern.MatchString(clean) && !entityFactPathPattern.MatchString(clean) {
		return "", 0, fmt.Errorf("fact commit: path must be wiki/facts/**/*.jsonl or team/entities/*.facts.jsonl; got %q", relPath)
	}
	if content == "" {
		return "", 0, fmt.Errorf("fact commit: content is required")
	}

	fullPath := filepath.Join(r.root, clean)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", 0, fmt.Errorf("fact commit: mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return "", 0, fmt.Errorf("fact commit: write: %w", err)
	}

	if out, err := r.runGitLocked(ctx, slug, "add", "--", clean); err != nil {
		return "", 0, fmt.Errorf("fact commit: git add: %w: %s", err, out)
	}

	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("fact commit: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("fact commit: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(content), nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("lint: mutate fact log %s", relPath)
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("fact commit: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("fact commit: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), len(content), nil
}

// AppendFactLog appends newlineContent to the fact-log file at relPath and
// commits the resulting bytes. The file is created if it does not exist.
// `additionalContent` must be the raw bytes to append — the caller is
// responsible for newline-terminating each JSONL record. A trailing newline
// is added if missing so the final file always ends with "\n".
//
// Uses the repo-wide write lock so the read-modify-write sequence is safe
// against concurrent appenders; the WikiWorker single-writer invariant
// (§11.5, Anti-pattern 5) routes every caller through this path.
//
// The accepted relPath shape matches Repo.CommitFactLog: wiki/facts/**/*.jsonl
// or team/entities/*.facts.jsonl.
func (r *Repo) AppendFactLog(ctx context.Context, slug, relPath, additionalContent, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("fact append: author slug is required")
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !factLogPathPattern.MatchString(clean) && !entityFactPathPattern.MatchString(clean) {
		return "", 0, fmt.Errorf("fact append: path must be wiki/facts/**/*.jsonl or team/entities/*.facts.jsonl; got %q", relPath)
	}
	if additionalContent == "" {
		return "", 0, fmt.Errorf("fact append: content is required")
	}

	fullPath := filepath.Join(r.root, clean)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", 0, fmt.Errorf("fact append: mkdir: %w", err)
	}

	var existing []byte
	if b, err := os.ReadFile(fullPath); err == nil {
		existing = b
	} else if !os.IsNotExist(err) {
		return "", 0, fmt.Errorf("fact append: read existing: %w", err)
	}

	var buf []byte
	buf = append(buf, existing...)
	// Guarantee a newline between existing and new content.
	if len(buf) > 0 && buf[len(buf)-1] != '\n' {
		buf = append(buf, '\n')
	}
	buf = append(buf, []byte(additionalContent)...)
	// Guarantee trailing newline so reconcile reads every line.
	if len(buf) > 0 && buf[len(buf)-1] != '\n' {
		buf = append(buf, '\n')
	}

	if err := os.WriteFile(fullPath, buf, 0o644); err != nil {
		return "", 0, fmt.Errorf("fact append: write: %w", err)
	}

	if out, err := r.runGitLocked(ctx, slug, "add", "--", clean); err != nil {
		return "", 0, fmt.Errorf("fact append: git add: %w: %s", err, out)
	}

	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("fact append: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("fact append: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(buf), nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("archivist: append fact log %s", relPath)
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("fact append: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("fact append: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), len(buf), nil
}
