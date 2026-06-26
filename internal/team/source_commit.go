package team

// source_commit.go owns the repo-level write that commits one immutable
// source record to sources/{kind}/{id}.md — the raw material the Karpathy
// "compile-from-sources" wiki is built FROM (see wiki_source.go).
//
// Contract:
//   - Sources are write-once / immutable. Once a path exists, CommitSource
//     never overwrites it; it returns the current HEAD SHA as a no-op success
//     (first-writer-wins). DeriveSourceID is origin-keyed, so re-capturing the
//     same office activity resolves to the same path and is idempotent.
//   - Path shape: sources/{kind}/{id}.md, validated with IsSourcePath. The
//     standard Repo.Commit path rejects non-team/ writes, so sources ride this
//     dedicated method — same locking + git add/commit shape as
//     CommitEntityFact / CommitArtifact, no catalog/index regen (sources are
//     NOT curated wiki articles).
//
// DECISION: first-writer-wins no-op on an existing path (not an error). The id
// is content-addressed for ingests (kind-title-hash8) and origin-keyed for
// auto-capture, so an existing path means "same source already captured."
// Erroring would turn benign re-captures into spurious failures on the worker
// path. Genuine id collisions between distinct sources are made improbable by
// the 8-char content-hash suffix DeriveSourceID appends to title-keyed ids; if
// one ever occurred the on-disk record simply wins, which is the safe,
// non-destructive outcome for an immutable layer.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CommitSource writes content to relPath (which must live under sources/) and
// commits it under the librarian identity. Write-once: if the file already
// exists on disk, it is left untouched and the current HEAD SHA is returned as
// a no-op success. Returns (commitSHA, bytesWritten, err).
func (r *Repo) CommitSource(ctx context.Context, relPath, content, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if strings.Contains(relPath, "..") {
		return "", 0, fmt.Errorf("source commit: path must not contain %q traversal; got %q", "..", relPath)
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !IsSourcePath(clean) {
		return "", 0, fmt.Errorf("source commit: path must match sources/{kind}/{id}.md; got %q", relPath)
	}
	if strings.TrimSpace(content) == "" {
		return "", 0, fmt.Errorf("source commit: content is required")
	}

	fullPath := filepath.Join(r.root, clean)

	// Write-once probe: an existing source is immutable. Return current HEAD
	// without touching the file so re-capture of the same activity is a clean
	// no-op (no overwrite, no churn on the wiki history).
	if _, statErr := os.Stat(fullPath); statErr == nil {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("source commit: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(content), nil
	} else if !os.IsNotExist(statErr) {
		return "", 0, fmt.Errorf("source commit: probe existing: %w", statErr)
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("source commit: mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		return "", 0, fmt.Errorf("source commit: write: %w", err)
	}

	author := LibrarianSlug
	if out, err := r.runGitLocked(ctx, author, "add", "--", clean); err != nil {
		return "", 0, fmt.Errorf("source commit: git add: %w: %s", err, out)
	}

	// A byte-identical re-write that the probe above missed (e.g. the file
	// existed but was uncommitted in the working tree) still resolves to a
	// no-op commit here. Report current HEAD so the caller cannot tell.
	cachedDiff, err := r.runGitLocked(ctx, author, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("source commit: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("source commit: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(content), nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("source: capture %s", clean)
	}
	if out, err := r.runGitLocked(ctx, author, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("source commit: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, author, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("source commit: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), len(content), nil
}
