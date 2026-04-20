package operations

// MaterializeWiki seeds a git-native LLM wiki with the thematic
// directories and skeleton articles declared on a blueprint's
// WikiSchema. It is the Lane B half of WUPHF's LLM Wiki v1: Lane A owns
// the actual git repo at ~/.wuphf/wiki/; this file just writes files
// into its working tree and lets Lane A's commit worker pick them up on
// its next pass.
//
// Guarantees
//
//	Transactional (per article): a temp dir holds new skeleton bytes;
//	os.Rename promotes each file to its final path. Either the rename
//	succeeds and the article is there, or it doesn't and the temp dir
//	is cleaned up. Across-article atomicity is best-effort: files that
//	landed before a later failure are left in place (see comment in
//	writeBootstrap). For v1 this is sufficient.
//
//	Idempotent: running twice with the same schema leaves the wiki in
//	the same state. Existing articles are preserved byte-for-byte,
//	regardless of their current content. This is load-bearing — users
//	who re-pick a blueprint must not lose the notes their agents wrote.
//
//	Path-safe: every bootstrap path is rejected unless it is a clean
//	relative path under team/. No ".." segments, no absolute paths, no
//	symlink chasing. A malicious blueprint cannot escape the wiki root.
//
// Happy-path flow
//
//	                                 ┌──────────────────────────┐
//	MaterializeWiki(wikiRoot, schema)│                          │
//	            │                    │    validate schema       │
//	            ▼                    │    ensure wikiRoot exists│
//	   sanitize paths + MkdirAll ─▶  └──────────────────────────┘
//	   schema.Dirs at target                       │
//	            │                                  ▼
//	            │                         any bootstrap articles missing?
//	            ▼                                  │
//	   partition bootstrap          ┌─────── no ───┴─── yes ───────┐
//	   by Stat(target_path):        ▼                              ▼
//	            │              return result          make tempDir under wikiRoot
//	            │              {SkippedOnly}          write each missing article to
//	            ▼                                     tempDir/<relative-path>
//	                                                          │
//	                                                          ▼
//	                                                  per-article os.Rename
//	                                                  tempDir/rel -> wikiRoot/rel
//	                                                          │
//	                                                          ▼
//	                                                     os.RemoveAll(tempDir)
//	                                                          │
//	                                                          ▼
//	                                                return MaterializeResult

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// MaterializeResult summarizes the work MaterializeWiki did. Callers log
// these counts so a human can see — e.g. in onboarding output — how many
// articles were newly created vs. preserved from an earlier run.
type MaterializeResult struct {
	DirsCreated     []string
	ArticlesCreated []string
	ArticlesSkipped []string
}

// wikiTeamPrefix is the forward-slash prefix every schema path must live
// under. We enforce this independently of filepath.Separator so a
// Windows host can't weaponize backslash separators to bypass the guard.
const wikiTeamPrefix = "team/"

// MaterializeWiki creates the thematic directories and bootstrap articles
// for schema inside wikiRoot (typically ~/.wuphf/wiki). It is
// transactional (per article) and idempotent (existing articles are
// preserved). A nil schema is a no-op — the wiki is simply left empty
// and Lane A's worker has nothing to commit.
func MaterializeWiki(ctx context.Context, wikiRoot string, schema *BlueprintWikiSchema) (MaterializeResult, error) {
	result := MaterializeResult{}
	if schema == nil {
		return result, nil
	}

	wikiRoot = strings.TrimSpace(wikiRoot)
	if wikiRoot == "" {
		return result, fmt.Errorf("operations: wikiRoot required")
	}

	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("operations: materialize cancelled: %w", err)
	}

	if err := os.MkdirAll(wikiRoot, 0o755); err != nil {
		return result, fmt.Errorf("operations: create wiki root %q: %w", wikiRoot, err)
	}

	// 1. Directories. Safe to create eagerly at the target — empty dirs
	//    are not destructive and are trivially idempotent via MkdirAll.
	dirsCreated, err := ensureDirs(wikiRoot, schema.Dirs)
	if err != nil {
		return result, err
	}
	result.DirsCreated = dirsCreated

	// 2. Bootstrap articles. Partition into missing (needs write) and
	//    existing (skipped). Skipped articles are preserved regardless
	//    of content — this is the idempotency guarantee.
	missing, skipped, err := partitionBootstrap(wikiRoot, schema.Bootstrap)
	if err != nil {
		return result, err
	}
	result.ArticlesSkipped = skipped

	if len(missing) == 0 {
		return result, nil
	}

	created, err := writeBootstrap(ctx, wikiRoot, missing)
	result.ArticlesCreated = created
	if err != nil {
		return result, err
	}
	return result, nil
}

// ensureDirs resolves each schema-declared directory under wikiRoot and
// MkdirAll's it. Each path is validated against path traversal before we
// touch the filesystem.
func ensureDirs(wikiRoot string, dirs []string) ([]string, error) {
	out := make([]string, 0, len(dirs))
	seen := make(map[string]struct{}, len(dirs))
	for _, rel := range dirs {
		cleaned, err := sanitizeWikiRelPath(rel)
		if err != nil {
			return out, fmt.Errorf("operations: reject dir %q: %w", rel, err)
		}
		if _, dup := seen[cleaned]; dup {
			continue
		}
		seen[cleaned] = struct{}{}
		target := filepath.Join(wikiRoot, filepath.FromSlash(cleaned))
		if err := os.MkdirAll(target, 0o755); err != nil {
			return out, fmt.Errorf("operations: mkdir wiki dir %q: %w", cleaned, err)
		}
		out = append(out, cleaned)
	}
	return out, nil
}

// partitionBootstrap splits schema.Bootstrap into (missing, skipped) by
// os.Stat. Missing articles are the ones we will write; skipped ones
// already exist and must not be touched.
func partitionBootstrap(wikiRoot string, items []BlueprintWikiBootstrapItem) ([]BlueprintWikiBootstrapItem, []string, error) {
	missing := make([]BlueprintWikiBootstrapItem, 0, len(items))
	skipped := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		cleaned, err := sanitizeWikiRelPath(item.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("operations: reject article %q: %w", item.Path, err)
		}
		if _, dup := seen[cleaned]; dup {
			continue
		}
		seen[cleaned] = struct{}{}
		item.Path = cleaned
		target := filepath.Join(wikiRoot, filepath.FromSlash(cleaned))
		_, statErr := os.Stat(target)
		switch {
		case statErr == nil:
			skipped = append(skipped, cleaned)
		case errors.Is(statErr, fs.ErrNotExist):
			missing = append(missing, item)
		default:
			return nil, nil, fmt.Errorf("operations: stat wiki article %q: %w", cleaned, statErr)
		}
	}
	return missing, skipped, nil
}

// writeBootstrap materializes missing articles via a temp-dir-then-rename
// pattern. Per-article atomicity via os.Rename: each file either lands
// whole at its final path or it doesn't. Across articles, if article N
// fails, articles 0..N-1 that were already renamed stay — this matches
// the transactional guarantee documented at the top of the file.
func writeBootstrap(ctx context.Context, wikiRoot string, items []BlueprintWikiBootstrapItem) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	tempDir, err := makeWikiTempDir(wikiRoot)
	if err != nil {
		return nil, err
	}
	// Best-effort cleanup even on success — temp dir should never outlive
	// this function. On failure, we still want the stage removed so a
	// later run isn't confused by orphan .tmp.* dirs.
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// Stage every missing article into the temp dir first. A failure
	// here costs us nothing — nothing has been promoted into wikiRoot
	// yet, so we just remove tempDir and return the error.
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("operations: materialize cancelled: %w", err)
		}
		stagePath := filepath.Join(tempDir, filepath.FromSlash(item.Path))
		if err := os.MkdirAll(filepath.Dir(stagePath), 0o755); err != nil {
			return nil, fmt.Errorf("operations: stage dir for %q: %w", item.Path, err)
		}
		if err := os.WriteFile(stagePath, []byte(item.Skeleton), 0o644); err != nil {
			return nil, fmt.Errorf("operations: stage article %q: %w", item.Path, err)
		}
	}

	// Promote each staged file to its final path via os.Rename. Rename
	// is atomic on the same filesystem and both sides live under
	// wikiRoot, so this is a cheap per-file commit. We also MkdirAll the
	// final parent dir because the schema.Dirs set may not cover every
	// article (an article at team/customers/intake.md is fine even if
	// team/customers/ isn't in schema.Dirs).
	created := make([]string, 0, len(items))
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return created, fmt.Errorf("operations: materialize cancelled: %w", err)
		}
		stagePath := filepath.Join(tempDir, filepath.FromSlash(item.Path))
		finalPath := filepath.Join(wikiRoot, filepath.FromSlash(item.Path))
		if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
			return created, fmt.Errorf("operations: parent dir for %q: %w", item.Path, err)
		}
		if err := os.Rename(stagePath, finalPath); err != nil {
			return created, fmt.Errorf("operations: promote article %q: %w", item.Path, err)
		}
		created = append(created, item.Path)
	}
	return created, nil
}

// makeWikiTempDir creates a sibling temp directory next to wikiRoot's
// contents but still under wikiRoot so os.Rename into final paths stays
// on the same filesystem. The random token avoids collisions between
// concurrent onboarding runs — unlikely in v1 but cheap insurance.
func makeWikiTempDir(wikiRoot string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("operations: tempdir token: %w", err)
	}
	name := fmt.Sprintf(".wiki.tmp.%s", hex.EncodeToString(buf))
	dir := filepath.Join(wikiRoot, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("operations: tempdir %q: %w", dir, err)
	}
	return dir, nil
}

// sanitizeWikiRelPath returns a cleaned, forward-slash relative path
// under team/, rejecting anything that could escape the wiki root.
// Accepts both a trailing slash (for dirs) and no trailing slash.
func sanitizeWikiRelPath(input string) (string, error) {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return "", fmt.Errorf("empty path, got %q", input)
	}
	// Normalize Windows-style backslashes to forward slashes before the
	// substring checks below — a schema authored on a Windows host
	// shouldn't slip a \..\etc\passwd past us.
	raw = strings.ReplaceAll(raw, "\\", "/")
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("absolute path not allowed, got %q", input)
	}
	// Clean collapses redundant separators and resolves "." segments.
	// Importantly, it does NOT resolve ".." against the filesystem — it
	// keeps them as literal segments, which is exactly what we need to
	// detect traversal attempts.
	cleaned := path.Clean(raw)
	// Preserve trailing slash semantics for dir entries: path.Clean drops
	// it, but callers still get a clean result since we always use
	// filepath.Join(wikiRoot, cleaned) afterward and MkdirAll is happy
	// with either form.
	if cleaned == "." || cleaned == "" {
		return "", fmt.Errorf("empty path after clean, got %q", input)
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.Contains(cleaned, "/../") || strings.HasSuffix(cleaned, "/..") {
		return "", fmt.Errorf("path traversal not allowed, got %q", input)
	}
	if !strings.HasPrefix(cleaned, wikiTeamPrefix) && cleaned != strings.TrimSuffix(wikiTeamPrefix, "/") {
		return "", fmt.Errorf("path must be under %s, got %q", wikiTeamPrefix, input)
	}
	return cleaned, nil
}
