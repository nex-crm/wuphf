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
