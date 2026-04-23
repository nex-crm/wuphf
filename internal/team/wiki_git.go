package team

// wiki_git.go owns all git operations for the team wiki at ~/.wuphf/wiki/.
//
// State machine
// =============
//
//	      ┌─────────────────┐
//	      │   NotInit       │  no ~/.wuphf/wiki/ or no .git/ under it
//	      └──────┬──────────┘
//	             │ Init()
//	             ▼
//	      ┌─────────────────┐
//	      │   Clean         │  working tree clean, fsck passes
//	      └──┬────────────┬─┘
//	         │ Commit()   │ (startup) RecoverDirtyTree
//	         ▼            ▼
//	      ┌─────────────────┐
//	      │   Dirty         │  uncommitted changes in tree
//	      └──────┬──────────┘
//	             │ auto-commit as wuphf-recovery
//	             ▼
//	          Clean
//
// Durability
//
//	Clean ──BackupMirror──► ~/.wuphf/wiki.bak/  (async, debounced, no mutex)
//
// Corruption handling
//
//	 Clean ──fsck fails──► RestoreFromBackup() ──► Clean  (if backup exists)
//	                                          ──► error   (double-fault — caller should fall back)
//
// All exported methods serialize on the embedded sync.Mutex as belt-and-suspenders.
// The worker goroutine in wiki_worker.go is the real serializer for hot-path
// commits, but Init / Fsck / RecoverDirtyTree can run before the worker starts
// so they cannot rely on the worker's single-goroutine guarantee.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// ErrGitUnavailable is returned by Init when the `git` binary cannot be
// located on $PATH. Callers should surface a banner to the user and fall
// back to --memory-backend none.
var ErrGitUnavailable = errors.New("wiki: git binary not found on PATH")

// ErrRepoCorrupt is returned by Fsck when the underlying git repo has
// detectable corruption (bad objects, missing refs, etc.).
var ErrRepoCorrupt = errors.New("wiki: repo integrity check failed")

// ErrBackupMissing is returned by RestoreFromBackup when no backup mirror
// exists to restore from.
var ErrBackupMissing = errors.New("wiki: backup mirror does not exist")

// CommitRef is a lightweight git log entry for a single article.
type CommitRef struct {
	SHA       string
	Author    string
	Timestamp time.Time
	Message   string
}

// Repo represents the wiki git repository living at ~/.wuphf/wiki/.
type Repo struct {
	root       string
	backupRoot string
	mu         sync.Mutex
}

// WikiRootDir returns the canonical on-disk path for the team wiki.
// It honours config.RuntimeHomeDir so dev runs stay isolated from prod.
func WikiRootDir() string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return filepath.Join(".wuphf", "wiki")
	}
	return filepath.Join(home, ".wuphf", "wiki")
}

// WikiBackupDir returns the path to the lightweight backup mirror.
func WikiBackupDir() string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return filepath.Join(".wuphf", "wiki.bak")
	}
	return filepath.Join(home, ".wuphf", "wiki.bak")
}

// NewRepo returns a Repo rooted at the resolved wiki path.
func NewRepo() *Repo {
	return &Repo{root: WikiRootDir(), backupRoot: WikiBackupDir()}
}

// NewRepoAt returns a Repo rooted at an explicit path (used by tests).
func NewRepoAt(root, backup string) *Repo {
	return &Repo{root: root, backupRoot: backup}
}

// Root returns the wiki root path.
func (r *Repo) Root() string { return r.root }

// HeadSHA returns the short HEAD commit hash. Returns empty string if the
// repo has no commits yet.
func (r *Repo) HeadSHA(ctx context.Context) (string, error) {
	out, err := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("wiki: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// BackupRoot returns the wiki backup mirror path.
func (r *Repo) BackupRoot() string { return r.backupRoot }

// TeamDir returns the team/ subtree path.
func (r *Repo) TeamDir() string { return filepath.Join(r.root, "team") }

// IndexDir returns the index/ subtree path.
func (r *Repo) IndexDir() string { return filepath.Join(r.root, "index") }

// IndexAllPath returns the path to the auto-regenerated catalog.
func (r *Repo) IndexAllPath() string { return filepath.Join(r.IndexDir(), "all.md") }

// Init ensures the wiki repo exists at r.root with a valid .git directory.
// If git is missing, returns ErrGitUnavailable. Idempotent.
func (r *Repo) Init(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, err := exec.LookPath("git"); err != nil {
		return ErrGitUnavailable
	}

	gitDir := filepath.Join(r.root, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		return r.ensureLayoutLocked()
	}

	// If the wiki dir exists but .git does not, preserve whatever is there
	// by moving it aside before re-initialising. Never discard user data.
	if info, err := os.Stat(r.root); err == nil && info.IsDir() {
		if err := r.preserveOrphanDirLocked(); err != nil {
			return fmt.Errorf("wiki: preserve orphan dir: %w", err)
		}
	}

	if err := os.MkdirAll(r.root, 0o700); err != nil {
		return fmt.Errorf("wiki: create root: %w", err)
	}
	if out, err := r.runGitLocked(ctx, "system", "init", "-q", "-b", "main"); err != nil {
		return fmt.Errorf("wiki: git init: %w: %s", err, out)
	}
	if err := r.ensureLayoutLocked(); err != nil {
		return err
	}
	// Stage + commit the initial layout so HEAD exists and log/fsck pass.
	if err := r.stageAllLocked(ctx); err != nil {
		return err
	}
	if out, err := r.runGitLocked(ctx, "system", "commit", "-q", "--allow-empty", "-m", "wuphf: init wiki"); err != nil {
		return fmt.Errorf("wiki: initial commit: %w: %s", err, out)
	}
	return nil
}

// preserveOrphanDirLocked renames the existing wiki/ dir to wiki.orphan-<ts>
// so an init can proceed without destroying the user's files.
// Caller must hold r.mu.
func (r *Repo) preserveOrphanDirLocked() error {
	ts := time.Now().UTC().Format("20060102T150405")
	parent := filepath.Dir(r.root)
	target := filepath.Join(parent, fmt.Sprintf("wiki.orphan-%s", ts))
	return os.Rename(r.root, target)
}

// ensureLayoutLocked creates the thematic directories and the .gitignore so
// index/ regenerates cleanly without tripping git.
// Caller must hold r.mu.
func (r *Repo) ensureLayoutLocked() error {
	dirs := []string{
		filepath.Join(r.root, "team", "people"),
		filepath.Join(r.root, "team", "companies"),
		filepath.Join(r.root, "team", "projects"),
		filepath.Join(r.root, "team", "playbooks"),
		filepath.Join(r.root, "team", "decisions"),
		filepath.Join(r.root, "team", "inbox", "raw"),
		filepath.Join(r.root, "index"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return fmt.Errorf("wiki: mkdir %s: %w", d, err)
		}
		keep := filepath.Join(d, ".gitkeep")
		if _, err := os.Stat(keep); os.IsNotExist(err) {
			if err := os.WriteFile(keep, []byte(""), 0o600); err != nil {
				return fmt.Errorf("wiki: write .gitkeep: %w", err)
			}
		}
	}
	return nil
}

// Commit writes content for slug @ path, stages, and commits with a per-commit
// git identity that never touches the user's global git config. Returns the
// short commit SHA and the number of bytes written.
//
// mode must be one of: "create", "replace", "append_section". "create" fails
// if the file exists; "replace" overwrites wholesale; "append_section" appends
// two newlines + content to an existing file (or creates it fresh).
func (r *Repo) Commit(ctx context.Context, slug, relPath, content, mode, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("wiki: commit requires an author slug")
	}
	if err := validateArticlePath(relPath); err != nil {
		return "", 0, err
	}
	fullPath := filepath.Join(r.root, relPath)

	switch mode {
	case "create":
		if _, err := os.Stat(fullPath); err == nil {
			return "", 0, fmt.Errorf("wiki: article already exists at %q; use replace or append_section", relPath)
		}
	case "replace":
		// overwrite fine
	case "append_section":
		// handled below
	default:
		return "", 0, fmt.Errorf("wiki: unknown write mode %q; expected create|replace|append_section", mode)
	}

	if strings.TrimSpace(content) == "" {
		return "", 0, fmt.Errorf("wiki: content is required")
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("wiki: mkdir %s: %w", filepath.Dir(fullPath), err)
	}

	var bytesWritten int
	switch mode {
	case "create", "replace":
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			return "", 0, fmt.Errorf("wiki: write article: %w", err)
		}
		bytesWritten = len(content)
	case "append_section":
		existing, err := os.ReadFile(fullPath)
		if err != nil && !os.IsNotExist(err) {
			return "", 0, fmt.Errorf("wiki: read for append: %w", err)
		}
		var buf []byte
		if len(existing) > 0 {
			buf = append(buf, existing...)
			if !strings.HasSuffix(string(existing), "\n") {
				buf = append(buf, '\n')
			}
			buf = append(buf, '\n')
		}
		buf = append(buf, []byte(content)...)
		if err := os.WriteFile(fullPath, buf, 0o600); err != nil {
			return "", 0, fmt.Errorf("wiki: write article: %w", err)
		}
		bytesWritten = len(content)
	}

	// Regenerate the catalog BEFORE committing so index/all.md lands in the
	// same commit as the article. Without this, the index is repeatedly
	// modified-but-uncommitted and every `git status` sees it as dirty —
	// eventually RecoverDirtyTree folds it into a misattributed
	// `wuphf-recovery` commit. Inline regen keeps the working tree clean.
	if err := r.regenerateIndexLocked(); err != nil {
		return "", 0, fmt.Errorf("wiki: index regen: %w", err)
	}

	relForGit := filepath.ToSlash(relPath)
	if out, err := r.runGitLocked(ctx, slug, "add", "--", relForGit, "index/all.md"); err != nil {
		return "", 0, fmt.Errorf("wiki: git add %s: %w: %s", relPath, err, out)
	}

	// If the content is byte-identical to what's already committed (e.g. an
	// agent retrying with the exact same body), `git add` stages nothing new
	// and `git commit` would fail with "nothing to commit." Detect that and
	// return a no-op success — the caller's contract is "make the content
	// current," which it already is. We return the current HEAD so downstream
	// code (SSE event, response body) stays well-formed.
	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("wiki: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, err := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if err != nil {
			return "", 0, fmt.Errorf("wiki: resolve HEAD sha: %w", err)
		}
		return strings.TrimSpace(headSha), bytesWritten, nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("wiki: update %s", relPath)
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("wiki: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("wiki: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), bytesWritten, nil
}

// CommitBootstrap stages every untracked / modified path under team/ and
// commits the whole pile as author `wuphf-bootstrap`. It is idempotent: if
// nothing is dirty, it returns ("", nil) without creating an empty commit.
//
// This is the handshake between the blueprint materializer (which only
// writes files to disk) and git. Without it, the freshly-seeded skeletons
// are untracked — on a later crash they get folded into a `wuphf-recovery`
// commit, which is misleading in an audit view. With it, the first commit
// for every skeleton article is attributable to the bootstrap step, not to
// some later recovery pass or to an agent that happened to edit the file.
//
// The author slug `wuphf-bootstrap` is deliberate: it is visually distinct
// from both the per-agent slugs (operator/planner/…) and the two reserved
// system slugs (`system`, `wuphf-recovery`). Audit views can filter or
// colour it differently from real human / agent edits.
func (r *Repo) CommitBootstrap(ctx context.Context, message string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// If nothing is dirty there's nothing to commit. This is the common
	// case when the user re-picks the same blueprint — MaterializeWiki
	// preserved existing articles, so the worktree is already clean.
	status, err := r.runGitLocked(ctx, "system", "status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("wiki: git status: %w", err)
	}
	if strings.TrimSpace(status) == "" {
		return "", nil
	}

	if out, err := r.runGitLocked(ctx, "wuphf-bootstrap", "add", "-A"); err != nil {
		return "", fmt.Errorf("wiki: git add -A (bootstrap): %w: %s", err, out)
	}
	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = "wuphf: materialize blueprint skeletons"
	}
	if out, err := r.runGitLocked(ctx, "wuphf-bootstrap", "commit", "-q", "-m", commitMsg); err != nil {
		return "", fmt.Errorf("wiki: git commit (bootstrap): %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("wiki: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), nil
}

// Log returns the commit history for a single article, most-recent first.
func (r *Repo) Log(ctx context.Context, relPath string) ([]CommitRef, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := validateArticlePath(relPath); err != nil {
		return nil, err
	}
	out, err := r.runGitLocked(
		ctx, "system",
		"log",
		"--format=%h%x1f%an%x1f%aI%x1f%s",
		"--",
		filepath.ToSlash(relPath),
	)
	if err != nil {
		return nil, fmt.Errorf("wiki: git log: %w", err)
	}
	var refs []CommitRef
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\x1f")
		if len(parts) != 4 {
			continue
		}
		ts, _ := time.Parse(time.RFC3339, parts[2])
		refs = append(refs, CommitRef{
			SHA:       parts[0],
			Author:    parts[1],
			Timestamp: ts,
			Message:   parts[3],
		})
	}
	return refs, nil
}

// AuditEntry is a single cross-article commit surfaced by AuditLog. Unlike
// CommitRef (which powers per-article history), this carries the list of
// files touched by the commit so reviewers can reconstruct the full diff
// surface without running a second `git show`.
type AuditEntry struct {
	SHA       string
	Author    string
	Timestamp time.Time
	Message   string
	// Paths are the git pathspecs modified in this commit, forward-slashed.
	// Merge commits and commits that only touched index/ will appear with
	// whatever git log --name-only reports; callers can filter as needed.
	Paths []string
}

// AuditLog returns every commit in the wiki repo, most-recent first. This
// is the cross-article audit trail — per-article history lives in Log().
// Two intentional design choices:
//
//  1. We always include the full author slug, timestamp, and file list so
//     downstream audit tooling (CSV export, compliance review, SOC2
//     artefact generation, etc.) can work without re-shelling to git.
//  2. Bootstrap (`wuphf-bootstrap`), recovery (`wuphf-recovery`), and
//     system (`system`) authors are surfaced alongside agent slugs. Audit
//     tools can filter them out by author, but the default feed is the
//     complete lineage — hiding bootstrap would create a false impression
//     that articles "appeared" at first-agent-write time.
//
// limit <= 0 returns everything. since.IsZero() returns everything regardless
// of age; otherwise only commits strictly newer than `since` are returned.
func (r *Repo) AuditLog(ctx context.Context, since time.Time, limit int) ([]AuditEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	args := []string{
		"log",
		"--format=%h%x1f%an%x1f%aI%x1f%s",
		"--name-only",
		"--no-merges",
	}
	if !since.IsZero() {
		args = append(args, "--since="+since.UTC().Format(time.RFC3339))
	}
	if limit > 0 {
		args = append(args, fmt.Sprintf("--max-count=%d", limit))
	}
	out, err := r.runGitLocked(ctx, "system", args...)
	if err != nil {
		return nil, fmt.Errorf("wiki: audit log: %w", err)
	}
	return parseAuditLog(out), nil
}

// parseAuditLog splits the output of `git log --format=... --name-only`
// into AuditEntry records. Exposed for test-side coverage.
func parseAuditLog(raw string) []AuditEntry {
	var out []AuditEntry
	var cur *AuditEntry
	for _, rawLine := range strings.Split(raw, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if line == "" {
			// Blank line terminates the paths block for the current commit.
			// Do NOT reset cur here — another header may follow immediately.
			continue
		}
		// Header lines contain our unit separator 0x1f; path lines do not.
		if strings.Contains(line, "\x1f") {
			parts := strings.Split(line, "\x1f")
			if len(parts) != 4 {
				continue
			}
			ts, _ := time.Parse(time.RFC3339, parts[2])
			entry := AuditEntry{
				SHA:       parts[0],
				Author:    parts[1],
				Timestamp: ts,
				Message:   parts[3],
			}
			out = append(out, entry)
			cur = &out[len(out)-1]
			continue
		}
		if cur != nil {
			cur.Paths = append(cur.Paths, filepath.ToSlash(line))
		}
	}
	return out
}

// Fsck runs git fsck and returns ErrRepoCorrupt if the repo is unreadable.
func (r *Repo) Fsck(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := os.Stat(filepath.Join(r.root, ".git")); err != nil {
		return fmt.Errorf("%w: missing .git at %s", ErrRepoCorrupt, r.root)
	}
	if out, err := r.runGitLocked(ctx, "system", "fsck", "--no-progress", "--no-dangling"); err != nil {
		return fmt.Errorf("%w: %s", ErrRepoCorrupt, strings.TrimSpace(out))
	}
	return nil
}

// IndexRegen walks team/ and rewrites index/all.md with one entry per article.
// Entries are sorted by directory then by modification time (newest first
// within a directory).
func (r *Repo) IndexRegen(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.regenerateIndexLocked()
}

// regenerateIndexLocked is the mutex-free body of IndexRegen. Called by
// Commit which already holds r.mu so we can't re-enter. Kept in sync with
// IndexRegen — the only difference is the lock wrap.
func (r *Repo) regenerateIndexLocked() error {
	teamDir := filepath.Join(r.root, "team")
	if _, err := os.Stat(teamDir); err != nil {
		return fmt.Errorf("wiki: team dir missing: %w", err)
	}

	type entry struct {
		relPath string
		dir     string
		title   string
		mtime   time.Time
	}
	var entries []entry

	err := filepath.Walk(teamDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Base(path) == ".gitkeep" {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(r.root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		entries = append(entries, entry{
			relPath: rel,
			dir:     filepath.ToSlash(filepath.Dir(rel)),
			title:   extractArticleTitle(path),
			mtime:   info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("wiki: walk team dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].dir != entries[j].dir {
			return entries[i].dir < entries[j].dir
		}
		return entries[i].mtime.After(entries[j].mtime)
	})

	var buf strings.Builder
	buf.WriteString("# Team wiki index\n\n")
	buf.WriteString("_Auto-generated. Do not edit by hand — agents regenerate this on every commit._\n\n")
	if len(entries) == 0 {
		buf.WriteString("_No articles yet._\n")
	} else {
		currentDir := ""
		for _, e := range entries {
			if e.dir != currentDir {
				if currentDir != "" {
					buf.WriteString("\n")
				}
				buf.WriteString(fmt.Sprintf("## %s\n\n", e.dir))
				currentDir = e.dir
			}
			buf.WriteString(fmt.Sprintf(
				"- [%s](../%s) _(updated %s)_\n",
				e.title,
				e.relPath,
				e.mtime.UTC().Format(time.RFC3339),
			))
		}
	}

	indexPath := filepath.Join(r.root, "index", "all.md")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o700); err != nil {
		return fmt.Errorf("wiki: mkdir index: %w", err)
	}
	if err := os.WriteFile(indexPath, []byte(buf.String()), 0o600); err != nil {
		return fmt.Errorf("wiki: write index: %w", err)
	}
	return nil
}

// BackupMirror copies the wiki repo to ~/.wuphf/wiki.bak/ skipping git object
// packs for speed. The worker calls this asynchronously and debounced.
func (r *Repo) BackupMirror(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := copyTree(r.root, r.backupRoot); err != nil {
		return fmt.Errorf("wiki: backup mirror: %w", err)
	}
	return nil
}

// RestoreFromBackup swaps the corrupt repo for the backup mirror.
// Returns ErrBackupMissing if the mirror does not exist.
func (r *Repo) RestoreFromBackup(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := os.Stat(r.backupRoot); err != nil {
		return ErrBackupMissing
	}
	ts := time.Now().UTC().Format("20060102T150405")
	corruptTarget := r.root + ".corrupt-" + ts
	if _, err := os.Stat(r.root); err == nil {
		if err := os.Rename(r.root, corruptTarget); err != nil {
			return fmt.Errorf("wiki: set aside corrupt repo: %w", err)
		}
	}
	if err := copyTree(r.backupRoot, r.root); err != nil {
		return fmt.Errorf("wiki: restore from backup: %w", err)
	}
	return nil
}

// RecoverDirtyTree detects uncommitted changes on startup and auto-commits
// them as `wuphf-recovery` so no user data is discarded.
func (r *Repo) RecoverDirtyTree(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := os.Stat(filepath.Join(r.root, ".git")); err != nil {
		return nil // nothing to recover; Init will handle it
	}
	out, err := r.runGitLocked(ctx, "system", "status", "--porcelain")
	if err != nil {
		return fmt.Errorf("wiki: git status: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return nil
	}
	if _, err := r.runGitLocked(ctx, "system", "add", "-A"); err != nil {
		return fmt.Errorf("wiki: git add for recovery: %w", err)
	}
	msg := "wuphf: recover from crashed write"
	if _, err := r.runGitLocked(ctx, "wuphf-recovery", "commit", "-q", "--allow-empty", "-m", msg); err != nil {
		return fmt.Errorf("wiki: recovery commit: %w", err)
	}
	return nil
}

// stageAllLocked stages the full working tree. Caller must hold r.mu.
func (r *Repo) stageAllLocked(ctx context.Context) error {
	if out, err := r.runGitLocked(ctx, "system", "add", "-A"); err != nil {
		return fmt.Errorf("wiki: git add -A: %w: %s", err, out)
	}
	return nil
}

// runGitLocked runs `git` with per-commit identity flags in the repo root.
// Caller must hold r.mu. The slug is used as both the author name and the
// local-part of the author email so git log / git blame stay useful.
func (r *Repo) runGitLocked(ctx context.Context, slug string, args ...string) (string, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = "wuphf"
	}
	return r.runGitLockedAs(ctx, slug, slug+"@wuphf.local", args...)
}

// runGitLockedAs runs `git` with an explicit author name + email. Used
// for human wiki edits where we want the user's real git identity on
// the commit (e.g. `Sarah Chen <sarah@acme.com>`) instead of the
// synthetic slug@wuphf.local pattern used for agents.
//
// Caller must hold r.mu.
func (r *Repo) runGitLockedAs(ctx context.Context, name, email string, args ...string) (string, error) {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	if name == "" {
		name = "wuphf"
	}
	if email == "" {
		email = "wuphf@wuphf.local"
	}
	identity := []string{
		"-c", "user.name=" + name,
		"-c", "user.email=" + email,
		"-c", "advice.defaultBranchName=false",
		"-c", "init.defaultBranch=main",
		"-c", "commit.gpgsign=false",
	}
	all := append(identity, args...)
	cmd := exec.CommandContext(ctx, "git", all...)
	cmd.Dir = r.root
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// WikiSearchHit is a literal substring match returned by the search API.
type WikiSearchHit struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// readArticle returns the raw article bytes for a validated path.
func readArticle(repo *Repo, relPath string) ([]byte, error) {
	if err := validateArticlePath(relPath); err != nil {
		return nil, err
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	return os.ReadFile(filepath.Join(repo.root, relPath))
}

// readIndexAll returns the contents of index/all.md.
func readIndexAll(repo *Repo) ([]byte, error) {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	indexPath := filepath.Join(repo.root, "index", "all.md")
	bytes, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []byte("# Team wiki index\n\n_No articles yet._\n"), nil
		}
		return nil, err
	}
	return bytes, nil
}

// searchArticles walks team/ and returns every line that contains the literal
// pattern. This is intentionally not a regex — agents never get to inject
// patterns that could DoS the search. Limit 100 hits per query.
func searchArticles(repo *Repo, pattern string) ([]WikiSearchHit, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("wiki: search pattern is required")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()

	teamDir := filepath.Join(repo.root, "team")
	if _, err := os.Stat(teamDir); err != nil {
		return nil, nil
	}
	const maxHits = 100
	hits := make([]WikiSearchHit, 0, 16)
	err := filepath.Walk(teamDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		if len(hits) >= maxHits {
			return filepath.SkipDir
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer func() { _ = f.Close() }()
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		lineNo := 0
		rel, _ := filepath.Rel(repo.root, path)
		rel = filepath.ToSlash(rel)
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if strings.Contains(line, pattern) {
				hits = append(hits, WikiSearchHit{
					Path:    rel,
					Line:    lineNo,
					Snippet: strings.TrimSpace(line),
				})
				if len(hits) >= maxHits {
					return filepath.SkipDir
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("wiki: search walk: %w", err)
	}
	return hits, nil
}

// validateArticlePath rejects paths that escape the team/ subtree.
func validateArticlePath(relPath string) error {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return fmt.Errorf("wiki: article_path is required")
	}
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("wiki: article path must be relative; got %q", relPath)
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if clean != filepath.ToSlash(relPath) && !strings.HasPrefix(clean, "team/") {
		return fmt.Errorf("wiki: article path must be within team/; got %q", relPath)
	}
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || clean == ".." {
		return fmt.Errorf("wiki: article path must not contain ..; got %q", relPath)
	}
	if !strings.HasPrefix(clean, "team/") {
		return fmt.Errorf("wiki: article path must be within team/; got %q", relPath)
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".md") {
		return fmt.Errorf("wiki: article path must end with .md; got %q", relPath)
	}
	return nil
}

// extractArticleTitle returns the first level-1 heading in the file, or the
// base name without extension when no heading exists.
func extractArticleTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return filepath.Base(path)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	base := filepath.Base(path)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return base
}

// copyTree copies src onto dst, creating dst if needed. Skips
// .git/objects/pack/ for speed in line with the backup spec.
func copyTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			// Transient race: git creates internal dirs (info/, refs/) lazily
			// while we walk. A single missing entry should not abort backup —
			// the next successful commit triggers another mirror pass.
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		// Skip heavyweight git object packs.
		if strings.HasPrefix(filepath.ToSlash(rel), ".git/objects/pack/") {
			return nil
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	// Git stores loose objects with mode 0o444 (read-only). On the second
	// backup pass, os.OpenFile(dst, O_CREATE|O_WRONLY|O_TRUNC) fails with
	// "permission denied" because the file exists and is not writable.
	// Remove it first so we always get a fresh write. os.Remove with
	// IsNotExist-check because the common case is a brand-new dst.
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wiki: remove stale %s: %w", dst, err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
