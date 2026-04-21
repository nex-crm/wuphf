package team

// human_commit.go owns the repo-level write path for human-authored wiki
// edits. It mirrors Repo.Commit but adds optimistic-concurrency checks:
// the caller must pass the SHA of the last article version they saw, and
// the commit is rejected with ErrWikiSHAMismatch when HEAD has moved on.
//
// Design notes
// ============
//
//   - Identity is fixed: every human write is attributed to a synthetic
//     `human` slug, yielding commit author `Human <human@wuphf.local>`.
//     v1 deliberately ships a single human identity — the founder. Per-user
//     attribution is a v1.1 concern; see the PR description.
//   - Concurrency model is optimistic, not pessimistic: we never lock the
//     article across the editor open → save round-trip. Readers and
//     agents keep writing freely; conflicts surface at save time and the
//     client re-loads the latest. This mirrors Wikipedia's "edit
//     conflict" flow, and avoids the half-typed-draft lock state that
//     pessimistic locking would introduce.
//   - Serialization still flows through the single-writer WikiWorker
//     queue. This file owns the repo-level mechanics; the worker owns
//     the scheduling primitive. Two different layers, same invariant.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HumanAuthor is the synthetic commit author slug for every human edit.
// Yields `Human <human@wuphf.local>` via runGitLocked's identity derivation.
// Distinct from every agent slug and from the other synthetic identities
// (archivist, wuphf-bootstrap, wuphf-recovery, system) so audit views can
// colour human edits distinctly.
const HumanAuthor = "human"

// ErrWikiSHAMismatch is returned by CommitHuman when the caller's
// expected_sha does not match the current HEAD SHA for the article. The
// HTTP handler surfaces this as 409 Conflict + the current article body
// so the client can show the re-load prompt without a second round trip.
var ErrWikiSHAMismatch = errors.New("wiki: article changed since it was opened")

// CommitHuman writes content to relPath as the synthetic human author,
// enforcing an expected-SHA pre-check. Returns the new short SHA, bytes
// written, and an error; ErrWikiSHAMismatch means the caller should
// re-load and re-apply their edits. Mode is inferred from mustExist: a
// fresh article (expectedSHA == "") uses "create", an edit uses "replace".
//
// Mirrors Repo.Commit in all other respects: same validateArticlePath
// guard, same working-tree atomicity, same regenerateIndexLocked pass
// so the index lands in the same commit as the article edit.
func (r *Repo) CommitHuman(ctx context.Context, relPath, content, expectedSHA, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := validateArticlePath(relPath); err != nil {
		return "", 0, err
	}
	if strings.TrimSpace(content) == "" {
		return "", 0, fmt.Errorf("wiki: content is required")
	}

	fullPath := filepath.Join(r.root, relPath)
	exists := false
	if _, err := os.Stat(fullPath); err == nil {
		exists = true
	}

	// Optimistic concurrency pre-check. Runs BEFORE any filesystem mutation
	// so a rejection leaves the working tree clean — matches the pattern
	// used in broker_review.reviewApprove (validate, then enqueue).
	if exists {
		if expectedSHA == "" {
			// Caller tried to create over an existing article; reject so
			// they get the current SHA and can re-submit as an edit.
			curSHA, serr := r.currentArticleSHALocked(ctx, relPath)
			if serr != nil {
				return "", 0, fmt.Errorf("wiki: resolve current sha: %w", serr)
			}
			return curSHA, 0, fmt.Errorf("%w: article exists but no expected_sha supplied", ErrWikiSHAMismatch)
		}
		curSHA, serr := r.currentArticleSHALocked(ctx, relPath)
		if serr != nil {
			return "", 0, fmt.Errorf("wiki: resolve current sha: %w", serr)
		}
		if !shaEquivalent(curSHA, expectedSHA) {
			return curSHA, 0, fmt.Errorf("%w: current %s, expected %s", ErrWikiSHAMismatch, curSHA, expectedSHA)
		}
	} else {
		if expectedSHA != "" {
			return "", 0, fmt.Errorf("%w: article not found but expected_sha supplied", ErrWikiSHAMismatch)
		}
	}

	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("wiki: mkdir %s: %w", filepath.Dir(fullPath), err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		return "", 0, fmt.Errorf("wiki: write article: %w", err)
	}
	bytesWritten := len(content)

	if err := r.regenerateIndexLocked(); err != nil {
		return "", 0, fmt.Errorf("wiki: index regen: %w", err)
	}

	relForGit := filepath.ToSlash(relPath)
	if out, err := r.runGitLocked(ctx, HumanAuthor, "add", "--", relForGit, "index/all.md"); err != nil {
		return "", 0, fmt.Errorf("wiki: git add %s: %w: %s", relPath, err, out)
	}

	// Byte-identical re-write short-circuits to current HEAD; mirrors Repo.Commit.
	cachedDiff, err := r.runGitLocked(ctx, HumanAuthor, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("wiki: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("wiki: resolve HEAD sha: %w", herr)
		}
		return strings.TrimSpace(headSha), bytesWritten, nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("human: update %s", relPath)
	}
	if !strings.HasPrefix(commitMsg, "human:") {
		// Enforce a visible prefix so audit log and git log --oneline
		// make the provenance obvious even at a glance.
		commitMsg = "human: " + commitMsg
	}
	if out, err := r.runGitLocked(ctx, HumanAuthor, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("wiki: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, HumanAuthor, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("wiki: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), bytesWritten, nil
}

// currentArticleSHALocked returns the short SHA of the most recent commit
// touching relPath. Caller must hold r.mu. Returns empty string without
// error when the article has no commit history yet (bootstrap edge).
func (r *Repo) currentArticleSHALocked(ctx context.Context, relPath string) (string, error) {
	out, err := r.runGitLocked(
		ctx, "system",
		"log", "-n", "1", "--format=%h", "--", filepath.ToSlash(relPath),
	)
	if err != nil {
		return "", fmt.Errorf("git log: %w: %s", err, out)
	}
	return strings.TrimSpace(out), nil
}

// shaEquivalent compares two git SHAs leniently: they match when one is a
// prefix of the other. Git's `--short` varies with object count (7 chars
// when young, 8+ later) so we cannot require exact equality between an
// older fetched SHA and a freshly-resolved one.
func shaEquivalent(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if len(a) < len(b) {
		return strings.HasPrefix(b, a)
	}
	return strings.HasPrefix(a, b)
}
