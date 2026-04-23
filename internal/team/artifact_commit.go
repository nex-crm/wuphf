package team

// artifact_commit.go owns the repo-level write that commits a raw source
// artifact (chat transcript, meeting note, email body, manual note) to
// wiki/artifacts/{kind}/{sha}.md.
//
// Contract (docs/specs/WIKI-SCHEMA.md §3 Layer 1):
//   - Artifacts are immutable once committed. The LLM reads them but NEVER
//     modifies them.
//   - Path shape: wiki/artifacts/{source}/{sha}.md where {source} is one of
//     chat | meeting | email | manual | linkedin.
//   - One commit per artifact; author slug is the recording identity (the
//     agent that produced the message, or `archivist` for system-filed
//     artifacts). The commit triggers the async extractor hook in
//     wiki_worker.go.
//
// Separate from Repo.Commit because the standard .md path enforces a
// team/{kind}/{slug}.md shape. Artifacts live under wiki/artifacts/ and get
// their own thin method that does NOT regen the catalog (no index/all.md
// churn for every ingested artifact).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// artifactPathPattern validates wiki/artifacts/{source}/{sha}.md paths.
// The source subdir is free-form lowercase-alnum to allow new ingest kinds
// without a code change; the filename is .md.
var artifactPathPattern = regexp.MustCompile(`^wiki/artifacts/[a-z][a-z0-9_-]*/[a-zA-Z0-9][a-zA-Z0-9_.-]*\.md$`)

// IsArtifactPath reports whether relPath matches the canonical artifact
// layout. Exported so the extractor + tests can guard against non-artifact
// paths slipping into the extraction hook.
func IsArtifactPath(relPath string) bool {
	return artifactPathPattern.MatchString(filepath.ToSlash(relPath))
}

// CommitArtifact writes the raw artifact body to wiki/artifacts/{kind}/{sha}.md
// and commits it under the supplied author slug. Follows the same shape as
// CommitEntityFact / CommitLintReport (no index/all.md regen).
func (r *Repo) CommitArtifact(ctx context.Context, slug, relPath, content, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("artifact commit: author slug is required")
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !artifactPathPattern.MatchString(clean) {
		return "", 0, fmt.Errorf("artifact commit: path must match wiki/artifacts/{source}/{sha}.md; got %q", relPath)
	}
	if strings.TrimSpace(content) == "" {
		return "", 0, fmt.Errorf("artifact commit: content is required")
	}

	fullPath := filepath.Join(r.root, clean)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		return "", 0, fmt.Errorf("artifact commit: mkdir: %w", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		return "", 0, fmt.Errorf("artifact commit: write: %w", err)
	}

	if out, err := r.runGitLocked(ctx, slug, "add", "--", clean); err != nil {
		return "", 0, fmt.Errorf("artifact commit: git add: %w: %s", err, out)
	}

	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("artifact commit: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, herr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if herr != nil {
			return "", 0, fmt.Errorf("artifact commit: resolve HEAD: %w", herr)
		}
		return strings.TrimSpace(headSha), len(content), nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("artifact: ingest %s", clean)
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("artifact commit: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("artifact commit: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), len(content), nil
}

// ArtifactKind parses the {source} segment of an artifact path. Returns
// ("", false) when the path does not match the canonical layout.
func ArtifactKind(relPath string) (string, bool) {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !artifactPathPattern.MatchString(clean) {
		return "", false
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 4 {
		return "", false
	}
	return parts[2], true
}

// ArtifactSHAFromPath parses the {sha} segment (sans .md extension). Returns
// ("", false) when the path does not match.
func ArtifactSHAFromPath(relPath string) (string, bool) {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if !artifactPathPattern.MatchString(clean) {
		return "", false
	}
	base := filepath.Base(clean)
	return strings.TrimSuffix(base, ".md"), true
}
