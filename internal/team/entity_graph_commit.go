package team

// entity_graph_commit.go owns the repo-level write that rewrites the
// cross-entity adjacency log at team/entities/.graph.jsonl. Shares the
// same single-writer wiki goroutine as fact writes, but uses a dedicated
// path pattern + commit method because the standard Repo.Commit path only
// accepts .md extensions.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CommitEntityGraph writes the full graph log to team/entities/.graph.jsonl
// and commits under the supplied author slug. Always replace-mode — the
// EntityGraph builder in entity_graph.go merges existing bytes with the
// new edges before calling this.
func (r *Repo) CommitEntityGraph(ctx context.Context, slug, content, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("entity graph commit: author slug is required")
	}
	if content == "" {
		return "", 0, fmt.Errorf("entity graph commit: content is required")
	}

	clean := filepath.ToSlash(filepath.Clean(EntityGraphPath))
	fullPath := filepath.Join(r.root, clean)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("entity graph commit: mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
		return "", 0, fmt.Errorf("entity graph commit: write: %w", err)
	}

	if out, err := r.runGitLocked(ctx, slug, "add", "--", clean); err != nil {
		return "", 0, fmt.Errorf("entity graph commit: git add: %w: %s", err, out)
	}

	// Byte-identical rewrite → no commit. Report current HEAD.
	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("entity graph commit: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("entity graph commit: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(content), nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = "graph: update " + clean
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("entity graph commit: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("entity graph commit: resolve HEAD: %w", err)
	}
	return strings.TrimSpace(sha), len(content), nil
}
