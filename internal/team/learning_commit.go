package team

// learning_commit.go owns the wiki-backed team learnings write path.
//
// Source of truth:
//   - team/learnings/index.jsonl
//
// Human-facing generated article:
//   - team/learnings/index.md
//
// The JSONL file is append-only from the caller's perspective. The generated
// markdown page makes learnings visible in the wiki catalog without asking the
// UI to render raw JSONL.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	TeamLearningsJSONLPath = "team/learnings/index.jsonl"
	TeamLearningsPagePath  = "team/learnings/index.md"
)

var learningLogPathPattern = regexp.MustCompile(`^team/learnings/index\.jsonl$`)

// CommitTeamLearnings writes the merged JSONL log and generated markdown page
// in one commit. The normal Repo.Commit path rejects .jsonl, so learnings use
// this narrow path while still regenerating the wiki catalog for index.md.
func (r *Repo) CommitTeamLearnings(ctx context.Context, slug, relPath, jsonlContent, markdownContent, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("team learnings: author slug is required")
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !learningLogPathPattern.MatchString(clean) {
		return "", 0, fmt.Errorf("team learnings: path must match %s; got %q", TeamLearningsJSONLPath, relPath)
	}
	if strings.TrimSpace(jsonlContent) == "" {
		return "", 0, fmt.Errorf("team learnings: jsonl content is required")
	}
	if strings.TrimSpace(markdownContent) == "" {
		return "", 0, fmt.Errorf("team learnings: markdown content is required")
	}

	jsonlPath := filepath.Join(r.root, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(jsonlPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("team learnings: mkdir jsonl: %w", err)
	}
	if err := writeFileAtomic(jsonlPath, []byte(jsonlContent), 0o600); err != nil {
		return "", 0, fmt.Errorf("team learnings: write jsonl: %w", err)
	}

	mdPath := filepath.Join(r.root, filepath.FromSlash(TeamLearningsPagePath))
	if err := writeFileAtomic(mdPath, []byte(markdownContent), 0o600); err != nil {
		return "", 0, fmt.Errorf("team learnings: write page: %w", err)
	}
	if err := r.regenerateIndexLocked(); err != nil {
		return "", 0, fmt.Errorf("team learnings: index regen: %w", err)
	}

	if out, err := r.runGitLocked(ctx, slug, "add", "--", clean, TeamLearningsPagePath, "index/all.md"); err != nil {
		return "", 0, fmt.Errorf("team learnings: git add: %w: %s", err, out)
	}
	cachedDiff, err := r.runGitLocked(
		ctx,
		slug,
		"diff", "--cached", "--name-only", "--",
		clean, TeamLearningsPagePath, "index/all.md",
	)
	if err != nil {
		return "", 0, fmt.Errorf("team learnings: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("team learnings: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(jsonlContent), nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = "learning: update team learnings"
	}
	if out, err := r.runGitLocked(
		ctx,
		slug,
		"commit", "-q", "-m", commitMsg, "--",
		clean, TeamLearningsPagePath, "index/all.md",
	); err != nil {
		return "", 0, fmt.Errorf("team learnings: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("team learnings: resolve HEAD: %w", err)
	}
	return strings.TrimSpace(sha), len(jsonlContent), nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
