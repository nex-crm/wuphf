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
// Mirrors entityFactPathPattern's shape: kind is lowercase letters (a starter
// letter then alnum/dash), slug is alnum/dash starting with a non-dash
// character. Blocks traversal, hidden files, uppercase, and other shapes
// the resolver never produces.
var factLogPathPattern = regexp.MustCompile(`^wiki/facts/[a-z][a-z0-9-]*/[a-z0-9][a-z0-9-]*\.jsonl$`)

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

// AppendFactLog appends additionalContent to the fact-log file at relPath and
// commits the resulting bytes. The file is created if it does not exist.
// `additionalContent` must be the raw bytes to append — the caller is
// responsible for newline-terminating each JSONL record. A trailing newline
// is added if missing so the final file always ends with "\n".
//
// Uses the repo-wide write lock so concurrent appenders are serialised; the
// WikiWorker single-writer invariant (§11.5, Anti-pattern 5) routes every
// caller through this path.
//
// Implementation: O_APPEND on a per-open fd. Cheaper than the earlier
// read-modify-write for prolific entities whose JSONL files can grow past a
// few MB — each append is O(bytesWritten) instead of O(filesize). The
// repo-wide mutex still guarantees exclusivity for the non-atomic
// "fstat + write" sequence we need to keep the trailing-newline invariant.
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
	// Match CommitEntityFact's tighter mode (0o700 dirs / 0o600 files). The
	// wiki repo is per-user local state; the previous 0o755/0o644 mix was
	// unnecessarily permissive for an append-only log that may carry
	// sensitive extraction metadata.
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("fact append: mkdir: %w", err)
	}

	// Probe the trailing byte (if any) via a short read-only handle so we can
	// insert a separator newline only when needed. Cheap — reads at most one
	// byte, regardless of file size. The repo-wide mutex above guarantees no
	// other writer can change the tail between this probe and our append.
	// (ReadAt on an O_APPEND|O_WRONLY fd returns EBADF on macOS, so the
	// probe uses its own short-lived read-only fd before the append fd.)
	needsLeadingNewline := false
	if fi, statErr := os.Stat(fullPath); statErr == nil {
		if fi.Size() > 0 {
			rf, rerr := os.Open(fullPath)
			if rerr != nil {
				return "", 0, fmt.Errorf("fact append: probe open: %w", rerr)
			}
			last := make([]byte, 1)
			_, readErr := rf.ReadAt(last, fi.Size()-1)
			_ = rf.Close()
			if readErr != nil {
				return "", 0, fmt.Errorf("fact append: probe tail: %w", readErr)
			}
			if last[0] != '\n' {
				needsLeadingNewline = true
			}
		}
	} else if !os.IsNotExist(statErr) {
		return "", 0, fmt.Errorf("fact append: stat: %w", statErr)
	}

	// Build the payload: leading newline iff tail lacks one, new content, and
	// a trailing newline if the caller didn't include one. Written in a
	// single Write under O_APPEND so readers never observe a partial tail.
	buf := make([]byte, 0, len(additionalContent)+2)
	if needsLeadingNewline {
		buf = append(buf, '\n')
	}
	buf = append(buf, []byte(additionalContent)...)
	if len(buf) == 0 || buf[len(buf)-1] != '\n' {
		buf = append(buf, '\n')
	}

	f, err := os.OpenFile(fullPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return "", 0, fmt.Errorf("fact append: open: %w", err)
	}
	if _, werr := f.Write(buf); werr != nil {
		_ = f.Close()
		return "", 0, fmt.Errorf("fact append: write: %w", werr)
	}
	if cerr := f.Close(); cerr != nil {
		return "", 0, fmt.Errorf("fact append: close: %w", cerr)
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
