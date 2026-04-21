package team

// promotion_commit.go owns the atomic promote-commit that bridges a
// notebook entry to the canonical team wiki.
//
// Flow (copy-not-move — the source notebook entry is preserved with a
// back-link frontmatter block):
//
//   1. Read notebook source at agents/{sourceSlug}/notebook/{name}.md.
//   2. Verify target team/{...}.md does not exist (409 if it does).
//   3. Write target file with the same body.
//   4. Update source frontmatter: promoted_to / promoted_at / promoted_by /
//      promoted_commit_sha. A placeholder SHA goes in during the first
//      commit; a second commit patches the real SHA once we know it.
//   5. Regenerate index/all.md so the new article appears in the catalog.
//   6. git add + git commit under the approver's slug.
//   7. Second tiny commit to patch the real SHA into the frontmatter.
//
// We use TWO commits (the spec allowed either approach). Rationale: the
// alternative — a sidecar JSON with the SHA — moves the SHA out of the
// notebook entry itself, which is where a later reader of the notebook
// actually looks. A second commit of <100 bytes is cheap, keeps the
// notebook self-describing, and leaves the git history human-readable.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrPromotionTargetExists is returned when the target wiki path already
// has content. The state machine maps this to `changes-requested` so the
// reviewer can work with the author on a different target.
var ErrPromotionTargetExists = errors.New("promotion: target wiki path already exists")

// ApplyPromotion executes the atomic promote commit. Returns the short
// commit SHA of the primary promotion commit.
//
// The approverSlug is the git author slug that the commit is recorded
// under — typically the reviewer's slug, or "human" when a human clicked
// Approve in the web UI.
func (r *Repo) ApplyPromotion(ctx context.Context, p *Promotion, approverSlug string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("promotion: nil promotion")
	}
	approverSlug = strings.TrimSpace(approverSlug)
	if approverSlug == "" {
		approverSlug = "human"
	}
	if err := validateNotebookPath(p.SourcePath); err != nil {
		return "", err
	}
	if err := validateArticlePath(p.TargetPath); err != nil {
		return "", err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	sourceFull := filepath.Join(r.root, p.SourcePath)
	targetFull := filepath.Join(r.root, p.TargetPath)

	// Pre-flight: target must not already exist.
	if _, err := os.Stat(targetFull); err == nil {
		return "", fmt.Errorf("%w: %s", ErrPromotionTargetExists, p.TargetPath)
	}

	sourceBytes, err := os.ReadFile(sourceFull)
	if err != nil {
		return "", fmt.Errorf("promotion: read source: %w", err)
	}
	// The target body is the source body MINUS any existing promotion
	// frontmatter. Otherwise the target ends up with a self-referencing
	// back-link that points to its own source — confusing for readers.
	targetBody := stripFrontmatter(string(sourceBytes))

	if err := os.MkdirAll(filepath.Dir(targetFull), 0o700); err != nil {
		return "", fmt.Errorf("promotion: mkdir target: %w", err)
	}
	if err := os.WriteFile(targetFull, []byte(targetBody), 0o600); err != nil {
		return "", fmt.Errorf("promotion: write target: %w", err)
	}

	// Stamp the source notebook with a placeholder SHA. The real SHA is
	// patched in by a second commit below once `git commit` names it.
	now := time.Now().UTC()
	placeholder := "0000000000"
	sourceWithFrontmatter := upsertPromotionFrontmatter(string(sourceBytes), frontmatterFields{
		PromotedTo:   p.TargetPath,
		PromotedAt:   now.Format(time.RFC3339),
		PromotedBy:   approverSlug,
		PromotedSHA:  placeholder,
		SourceAgent:  p.SourceSlug,
		TargetHeader: headerLineFrom(targetBody),
	})
	if err := os.WriteFile(sourceFull, []byte(sourceWithFrontmatter), 0o600); err != nil {
		return "", fmt.Errorf("promotion: write source frontmatter: %w", err)
	}

	// Regenerate the wiki catalog so the new article shows up immediately.
	if err := r.regenerateIndexLocked(); err != nil {
		return "", fmt.Errorf("promotion: regen index: %w", err)
	}

	targetRel := filepath.ToSlash(p.TargetPath)
	sourceRel := filepath.ToSlash(p.SourcePath)

	if out, err := r.runGitLocked(ctx, approverSlug, "add", "--",
		targetRel, sourceRel, "index/all.md",
	); err != nil {
		return "", fmt.Errorf("promotion: git add: %w: %s", err, out)
	}

	msg := fmt.Sprintf(
		"promote: %s -> %s\n\nApproved by %s: %s\n\nSource notebook entry retained with back-link frontmatter.",
		p.SourcePath, p.TargetPath, approverSlug, firstLine(p.Rationale),
	)
	if out, err := r.runGitLocked(ctx, approverSlug, "commit", "-q", "-m", msg); err != nil {
		return "", fmt.Errorf("promotion: git commit: %w: %s", err, out)
	}
	shaOut, err := r.runGitLocked(ctx, approverSlug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("promotion: resolve HEAD: %w", err)
	}
	sha := strings.TrimSpace(shaOut)

	// Second tiny commit: patch the placeholder SHA with the real one.
	// Keeps the notebook entry self-describing without an amend.
	finalSource := strings.Replace(sourceWithFrontmatter, "promoted_commit_sha: "+placeholder, "promoted_commit_sha: "+sha, 1)
	if finalSource != sourceWithFrontmatter {
		if err := os.WriteFile(sourceFull, []byte(finalSource), 0o600); err != nil {
			return sha, fmt.Errorf("promotion: patch sha in source: %w", err)
		}
		if out, err := r.runGitLocked(ctx, approverSlug, "add", "--", sourceRel); err != nil {
			return sha, fmt.Errorf("promotion: git add sha patch: %w: %s", err, out)
		}
		patchMsg := fmt.Sprintf("promote: record SHA %s on %s", sha, p.SourcePath)
		if out, err := r.runGitLocked(ctx, approverSlug, "commit", "-q", "-m", patchMsg); err != nil {
			return sha, fmt.Errorf("promotion: git commit sha patch: %w: %s", err, out)
		}
	}

	return sha, nil
}

// frontmatterFields captures the keys we write into a notebook entry's
// YAML frontmatter block after promotion.
type frontmatterFields struct {
	PromotedTo   string
	PromotedAt   string
	PromotedBy   string
	PromotedSHA  string
	SourceAgent  string
	TargetHeader string
}

// upsertPromotionFrontmatter prepends or updates the YAML frontmatter on a
// notebook entry. Existing frontmatter keys are replaced in place; missing
// ones are appended to the block. When the entry has no frontmatter, a
// fresh block is prepended.
func upsertPromotionFrontmatter(body string, f frontmatterFields) string {
	fmBlock := buildFrontmatterBlock(f)
	if !strings.HasPrefix(body, "---\n") {
		if body != "" && !strings.HasPrefix(body, "\n") {
			return fmBlock + "\n" + body
		}
		return fmBlock + body
	}
	// Existing frontmatter: replace just the promotion keys, preserve the
	// rest. We walk until the closing --- and rewrite matching lines.
	lines := strings.Split(body, "\n")
	if len(lines) < 2 {
		return fmBlock + "\n" + body
	}
	endIdx := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			endIdx = i
			break
		}
	}
	if endIdx < 0 {
		// Malformed frontmatter: prepend a fresh block and let the reader
		// sort it out. Safer than guessing where the old block should end.
		return fmBlock + "\n" + body
	}

	// Update or insert each promoted_* key.
	replacements := map[string]string{
		"promoted_to":         f.PromotedTo,
		"promoted_at":         f.PromotedAt,
		"promoted_by":         f.PromotedBy,
		"promoted_commit_sha": f.PromotedSHA,
	}
	existing := map[string]bool{}
	for i := 1; i < endIdx; i++ {
		for key, value := range replacements {
			prefix := key + ":"
			if strings.HasPrefix(strings.TrimSpace(lines[i]), prefix) {
				lines[i] = key + ": " + value
				existing[key] = true
			}
		}
	}
	// Append any keys that weren't already present.
	var inserts []string
	for _, key := range []string{"promoted_to", "promoted_at", "promoted_by", "promoted_commit_sha"} {
		if !existing[key] {
			inserts = append(inserts, key+": "+replacements[key])
		}
	}
	if len(inserts) == 0 {
		return strings.Join(lines, "\n")
	}
	// Splice inserts immediately before the closing ---.
	out := make([]string, 0, len(lines)+len(inserts))
	out = append(out, lines[:endIdx]...)
	out = append(out, inserts...)
	out = append(out, lines[endIdx:]...)
	return strings.Join(out, "\n")
}

// buildFrontmatterBlock produces a fresh YAML frontmatter block with the
// promotion back-link keys.
func buildFrontmatterBlock(f frontmatterFields) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("promoted_to: " + f.PromotedTo + "\n")
	b.WriteString("promoted_at: " + f.PromotedAt + "\n")
	b.WriteString("promoted_by: " + f.PromotedBy + "\n")
	b.WriteString("promoted_commit_sha: " + f.PromotedSHA + "\n")
	b.WriteString("---\n")
	return b.String()
}

// stripFrontmatter returns the body with a leading YAML frontmatter block
// removed. Used when building the target wiki article so the wiki copy
// doesn't inherit the source's back-link keys.
func stripFrontmatter(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return body
	}
	rest := body[len("---\n"):]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return body
	}
	return strings.TrimLeft(rest[idx+len("\n---\n"):], "\n")
}

// headerLineFrom returns the first markdown H1 line from a body, or "" when
// none is found. Used for nicer commit messages / UI; not load-bearing.
func headerLineFrom(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return ""
}

// firstLine returns the first non-empty line of s, or "(no rationale)".
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return "(no rationale)"
}
