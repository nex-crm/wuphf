package team

// wiki_page_ops.go — Slice 2 of the wiki feature: page create / move /
// rename / delete with [[wikilink]] reference rewriting.
//
// Surface
// =======
//
//	POST   /wiki/page/create   {path, title?, content?}        → {path, commit_sha}
//	POST   /wiki/page/move     {from, to}                       → {to, commit_sha, references_rewritten, rewritten_paths}
//	POST   /wiki/page/rename   {path, newName}                  → same as move
//	DELETE /wiki/page?path=...                                  → {path, commit_sha}
//
// Every mutation is a single git commit authored as the requesting human
// (resolved server-side from the request actor, falling back to the synthetic
// `human <human@wuphf.local>` identity), exactly like /wiki/write-human. The
// move/rename path additionally rewrites every reference across the wiki in the
// SAME commit so a rename never strands a [[wikilink]].
//
// Design
// ======
//
//   - Repo-level methods (CreatePage, MovePage, DeletePage) hold r.mu and own
//     all filesystem + git mechanics, mirroring CommitArchive's atomicity
//     (write files, regen index, stage, diff-check, commit, resolve SHA). They
//     do NOT go through the single-file WikiWorker queue because a move is an
//     inherently multi-file commit; the queue's primitive is one file per
//     commit. Serialization is still total: r.mu is the same lock the worker's
//     commits ultimately take.
//   - Link rewriting is delegated to the pure engine in wiki_link_rewrite.go.
//     The resolver is built from the actual on-disk article set so rewrite-time
//     resolution is byte-identical to the web client's click-time resolution.
//   - Delete deliberately does NOT rewrite links: leaving [[broken]] links is
//     intentional (existing broken-link styling + the daily lint surface them).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// errWikiPageExists / errWikiPageMissing map to 409 / 404 at the HTTP boundary.
var (
	errWikiPageExists  = errors.New("wiki page: target already exists")
	errWikiPageMissing = errors.New("wiki page: source does not exist")
)

// PageMoveResult is the wire-shape payload for a successful move/rename.
//
// MovedPaths carries the post-move path of every .md page that physically
// moved: a single entry for a file move/rename, one per descendant page for a
// directory move. It is the SSE fan-out source for the destination side of the
// move — `To` alone is insufficient for a directory move, where `To` names a
// directory (no .md suffix) and the actual pages live beneath it.
type PageMoveResult struct {
	To                  string   `json:"to"`
	CommitSHA           string   `json:"commit_sha"`
	ReferencesRewritten int      `json:"references_rewritten"`
	RewrittenPaths      []string `json:"rewritten_paths"`
	MovedPaths          []string `json:"moved_paths"`
}

// CreatePage creates a brand-new article at relPath and commits it as the given
// human identity. title, when non-empty, seeds a single H1 heading at the top
// of the body when the supplied content does not already begin with one.
//
//   - relPath must be a valid team/<...>.md article path.
//   - Fails with errWikiPageExists when the file already exists (409).
//
// Returns the new short commit SHA.
func (r *Repo) CreatePage(ctx context.Context, relPath, title, content string, identity HumanIdentity) (string, error) {
	clean, abs, err := resolveTeamRelPath(r.root, relPath)
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".md") {
		return "", fmt.Errorf("%w: page path must end with .md; got %q", errWikiFSBadPath, relPath)
	}
	name, email, slug := effectiveHumanIdentity(identity)

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, statErr := os.Stat(abs); statErr == nil {
		return "", fmt.Errorf("%w: %s", errWikiPageExists, clean)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("wiki page: stat target: %w", statErr)
	}

	body := seedPageBody(title, content)
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return "", fmt.Errorf("wiki page: mkdir %s: %w", filepath.Dir(abs), err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o600); err != nil {
		return "", fmt.Errorf("wiki page: write %s: %w", clean, err)
	}

	msg := fmt.Sprintf("human: create %s", clean)
	sha, commitErr := r.commitPathsLocked(ctx, name, email, msg, []string{clean})
	if commitErr != nil {
		// Roll back the file so a failed commit does not leave an untracked
		// article that RecoverDirtyTree would later misattribute. Surface both
		// failures so neither the commit cause nor the rollback failure is lost.
		if rmErr := os.Remove(abs); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return "", errors.Join(commitErr, fmt.Errorf("wiki page: rollback create %s: %w", clean, rmErr))
		}
		return "", commitErr
	}
	_ = slug
	return sha, nil
}

// MovePage moves the article (or, when `from` names a directory, the whole
// subtree) from `from` to `to`, rewrites every [[wikilink]] across the wiki
// that resolved to a moved page, and commits all of it in one commit.
//
//   - Both paths must be under team/.
//   - `from` must exist; `to` (and, for a directory move, every descendant
//     target) must NOT exist — otherwise errWikiPageExists (409).
//   - Single-file move requires `from` to end in .md; directory move requires
//     `from` to be a directory and `to` to be a (non-existent) directory.
//
// Returns the destination path, the new commit SHA, the number of individual
// links rewritten, and the sorted set of article paths whose bodies changed
// (excluding the moved files themselves unless they contained a rewritten link).
func (r *Repo) MovePage(ctx context.Context, from, to string, identity HumanIdentity) (PageMoveResult, error) {
	fromClean, fromAbs, err := resolveTeamRelPath(r.root, from)
	if err != nil {
		return PageMoveResult{}, err
	}
	toClean, toAbs, err := resolveTeamRelPath(r.root, to)
	if err != nil {
		return PageMoveResult{}, err
	}
	if fromClean == toClean {
		return PageMoveResult{}, fmt.Errorf("%w: from and to are identical", errWikiFSBadPath)
	}
	name, email, _ := effectiveHumanIdentity(identity)

	r.mu.Lock()
	defer r.mu.Unlock()

	info, statErr := os.Stat(fromAbs)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return PageMoveResult{}, fmt.Errorf("%w: %s", errWikiPageMissing, fromClean)
		}
		return PageMoveResult{}, fmt.Errorf("wiki page: stat source: %w", statErr)
	}

	moves, err := r.computeMovesLocked(fromClean, fromAbs, toClean, toAbs, info.IsDir())
	if err != nil {
		return PageMoveResult{}, err
	}

	// Snapshot every article BEFORE moving so resolveFrom sees the pre-move
	// world. We read the bodies once and reuse them for the rewrite.
	preArticles, err := r.scanArticlesLocked()
	if err != nil {
		return PageMoveResult{}, err
	}

	// Build the pre-move and post-move existence sets for the resolvers.
	preExists := func(p string) bool { _, ok := preArticles[p]; return ok }
	postSet := make(map[string]struct{}, len(preArticles))
	for p := range preArticles {
		postSet[p] = struct{}{}
	}
	for _, m := range moves {
		delete(postSet, m.From)
		postSet[m.To] = struct{}{}
	}
	postExists := func(p string) bool { _, ok := postSet[p]; return ok }

	resolveFrom := newLinkResolver(preExists)
	resolveTo := newLinkResolver(postExists)

	// Move files on disk first. Any failure here aborts before git is touched.
	moved, err := r.applyMovesLocked(moves)
	if err != nil {
		r.undoMovesLocked(moved)
		return PageMoveResult{}, err
	}

	// Rewrite references. The scan map already reflects pre-move paths; after
	// moving, the moved bodies live at their new paths but their CONTENT is
	// unchanged, so we remap the snapshot keys to post-move paths before
	// rewriting so a self/sibling link inside a moved page is written back to
	// the correct (new) file.
	rewriteInput := remapSnapshotKeys(preArticles, moves)
	changed, count := rewriteWikilinksMulti(rewriteInput, moves, resolveFrom, resolveTo)

	// Write rewritten bodies to disk.
	rewrittenPaths := make([]string, 0, len(changed))
	for p, body := range changed {
		absP := filepath.Join(r.root, filepath.FromSlash(p))
		if writeErr := os.WriteFile(absP, []byte(body), 0o600); writeErr != nil {
			r.undoMovesLocked(moved)
			return PageMoveResult{}, fmt.Errorf("wiki page: write rewritten %s: %w", p, writeErr)
		}
		rewrittenPaths = append(rewrittenPaths, p)
	}
	sort.Strings(rewrittenPaths)

	// Stage exactly the paths we touched: every old path (now deleted) + every
	// new path + every rewritten body. `git add -A <pathspec>` records deletes
	// for the vacated sources and adds for the destinations.
	staged := make([]string, 0, len(moves)*2+len(rewrittenPaths))
	for _, m := range moves {
		staged = append(staged, m.From, m.To)
	}
	staged = append(staged, rewrittenPaths...)

	msg := movePageCommitMessage(fromClean, toClean, info.IsDir(), count)
	sha, commitErr := r.commitPathsLocked(ctx, name, email, msg, staged)
	if commitErr != nil {
		r.undoRewritesLocked(preArticles, rewrittenPaths, moves)
		r.undoMovesLocked(moved)
		return PageMoveResult{}, commitErr
	}

	// MovedPaths is every post-move .md page path, sorted for deterministic SSE
	// fan-out. For a file move/rename this is a single entry; for a directory
	// move it is one per descendant page (res.To names the directory, so the
	// destination SSE events must come from here, not from res.To).
	movedPaths := make([]string, 0, len(moves))
	for _, m := range moves {
		movedPaths = append(movedPaths, m.To)
	}
	sort.Strings(movedPaths)

	return PageMoveResult{
		To:                  toClean,
		CommitSHA:           sha,
		ReferencesRewritten: count,
		RewrittenPaths:      rewrittenPaths,
		MovedPaths:          movedPaths,
	}, nil
}

// DeletePage removes the article (or, when `path` names a directory, the whole
// subtree) and commits the deletion. It does NOT rewrite references: broken
// [[wikilink]]s are surfaced by the existing broken-link styling + lint, which
// is the intended behaviour for a delete.
//
// Fails with errWikiPageMissing (404) when the path does not exist.
func (r *Repo) DeletePage(ctx context.Context, path string, identity HumanIdentity) (string, error) {
	clean, abs, err := resolveTeamRelPath(r.root, path)
	if err != nil {
		return "", err
	}
	name, email, _ := effectiveHumanIdentity(identity)

	r.mu.Lock()
	defer r.mu.Unlock()

	info, statErr := os.Stat(abs)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return "", fmt.Errorf("%w: %s", errWikiPageMissing, clean)
		}
		return "", fmt.Errorf("wiki page: stat target: %w", statErr)
	}

	if info.IsDir() {
		if rmErr := os.RemoveAll(abs); rmErr != nil {
			return "", fmt.Errorf("wiki page: remove subtree %s: %w", clean, rmErr)
		}
	} else if rmErr := os.Remove(abs); rmErr != nil {
		return "", fmt.Errorf("wiki page: remove %s: %w", clean, rmErr)
	}

	msg := fmt.Sprintf("human: delete %s", clean)
	if info.IsDir() {
		msg = fmt.Sprintf("human: delete subtree %s", clean)
	}
	sha, commitErr := r.commitPathsLocked(ctx, name, email, msg, []string{clean})
	if commitErr != nil {
		return "", commitErr
	}
	return sha, nil
}

// computeMovesLocked builds the list of concrete .md page moves for a request.
// For a single-file move it returns one entry; for a directory move it returns
// one entry per descendant .md page (reparented under `to`). It validates that
// no destination already exists. Caller must hold r.mu.
func (r *Repo) computeMovesLocked(fromClean, fromAbs, toClean, toAbs string, fromIsDir bool) ([]pageMove, error) {
	if !fromIsDir {
		if !strings.HasSuffix(strings.ToLower(fromClean), ".md") {
			return nil, fmt.Errorf("%w: source page must end with .md; got %q", errWikiFSBadPath, fromClean)
		}
		if !strings.HasSuffix(strings.ToLower(toClean), ".md") {
			return nil, fmt.Errorf("%w: destination page must end with .md; got %q", errWikiFSBadPath, toClean)
		}
		if _, statErr := os.Stat(toAbs); statErr == nil {
			return nil, fmt.Errorf("%w: %s", errWikiPageExists, toClean)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return nil, fmt.Errorf("wiki page: stat destination: %w", statErr)
		}
		return []pageMove{{From: fromClean, To: toClean}}, nil
	}

	// Directory move. The destination directory must not already exist, and the
	// destination must itself be a directory path (no .md suffix).
	if strings.HasSuffix(strings.ToLower(toClean), ".md") {
		return nil, fmt.Errorf("%w: cannot move a directory onto a file path %q", errWikiFSBadPath, toClean)
	}
	if _, statErr := os.Stat(toAbs); statErr == nil {
		return nil, fmt.Errorf("%w: %s", errWikiPageExists, toClean)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return nil, fmt.Errorf("wiki page: stat destination dir: %w", statErr)
	}

	var moves []pageMove
	walkErr := filepath.WalkDir(fromAbs, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(fromAbs, p)
		if relErr != nil {
			return fmt.Errorf("wiki page: rel %s: %w", p, relErr)
		}
		relSlash := filepath.ToSlash(rel)
		moves = append(moves, pageMove{
			From: fromClean + "/" + relSlash,
			To:   toClean + "/" + relSlash,
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("wiki page: walk subtree %s: %w", fromClean, walkErr)
	}
	if len(moves) == 0 {
		return nil, fmt.Errorf("%w: %s has no pages to move", errWikiPageMissing, fromClean)
	}
	return moves, nil
}

// appliedMove records a completed on-disk move so it can be undone on failure.
type appliedMove struct{ fromAbs, toAbs string }

// applyMovesLocked renames every move's source to its destination on disk,
// creating parent directories as needed. It returns the moves it completed so a
// later failure can undo them in reverse. Caller must hold r.mu.
func (r *Repo) applyMovesLocked(moves []pageMove) ([]appliedMove, error) {
	done := make([]appliedMove, 0, len(moves))
	for _, m := range moves {
		fromAbs := filepath.Join(r.root, filepath.FromSlash(m.From))
		toAbs := filepath.Join(r.root, filepath.FromSlash(m.To))
		if err := os.MkdirAll(filepath.Dir(toAbs), 0o700); err != nil {
			return done, fmt.Errorf("wiki page: mkdir %s: %w", filepath.Dir(toAbs), err)
		}
		if err := os.Rename(fromAbs, toAbs); err != nil {
			return done, fmt.Errorf("wiki page: rename %s -> %s: %w", m.From, m.To, err)
		}
		done = append(done, appliedMove{fromAbs: fromAbs, toAbs: toAbs})
	}
	return done, nil
}

// undoMovesLocked reverses completed on-disk moves (best effort) after a
// failure so the working tree is left as close as possible to its prior state.
// Caller must hold r.mu.
func (r *Repo) undoMovesLocked(moved []appliedMove) {
	for i := len(moved) - 1; i >= 0; i-- {
		_ = os.MkdirAll(filepath.Dir(moved[i].fromAbs), 0o700)
		_ = os.Rename(moved[i].toAbs, moved[i].fromAbs)
	}
}

// undoRewritesLocked restores the original content of every rewritten article
// from the pre-move snapshot (best effort) after a failed commit. It runs while
// files still sit at their POST-move paths (before undoMovesLocked moves them
// back), so it writes each restored body to the post-move path.
//
// The snapshot is keyed by PRE-move paths. For an unmoved article the rewritten
// path equals its snapshot key, so a direct lookup works. For a MOVED article
// whose own body was rewritten (a self-link), the rewritten path is the
// post-move path, which has no snapshot entry — we must translate it back to the
// pre-move key via the reverse (To -> From) map to recover the original body.
// Without this, a moved-and-self-rewritten article's body is never restored.
// Caller must hold r.mu.
func (r *Repo) undoRewritesLocked(snapshot map[string]string, rewrittenPaths []string, moves []pageMove) {
	fromByTo := make(map[string]string, len(moves))
	for _, m := range moves {
		fromByTo[filepath.ToSlash(m.To)] = filepath.ToSlash(m.From)
	}
	for _, p := range rewrittenPaths {
		// Prefer the direct (unmoved) key; fall back to the pre-move key for a
		// moved article whose body was itself rewritten.
		body, ok := snapshot[p]
		if !ok {
			if pre, moved := fromByTo[p]; moved {
				body, ok = snapshot[pre]
			}
		}
		if ok {
			_ = os.WriteFile(filepath.Join(r.root, filepath.FromSlash(p)), []byte(body), 0o600)
		}
	}
}

// scanArticlesLocked reads every team/**.md article into a map keyed by
// repo-root-relative slash path. Dot-prefixed files and the .gitkeep stubs are
// skipped. Caller must hold r.mu.
func (r *Repo) scanArticlesLocked() (map[string]string, error) {
	teamDir := r.TeamDir()
	out := make(map[string]string)
	walkErr := filepath.WalkDir(teamDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("wiki page: walk %s: %w", p, err)
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(r.root, p)
		if relErr != nil {
			return fmt.Errorf("wiki page: rel %s: %w", p, relErr)
		}
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			if errors.Is(readErr, fs.ErrNotExist) || errors.Is(readErr, fs.ErrPermission) {
				return nil
			}
			return fmt.Errorf("wiki page: read %s: %w", p, readErr)
		}
		out[filepath.ToSlash(rel)] = string(content)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}

// commitPathsLocked stages the given repo-root-relative paths plus the
// regenerated index, and commits them under the supplied human identity. It
// short-circuits to current HEAD when nothing was actually staged (e.g. a
// content-identical rewrite), mirroring Repo.Commit / CommitHuman. Caller must
// hold r.mu.
func (r *Repo) commitPathsLocked(ctx context.Context, name, email, message string, paths []string) (string, error) {
	if err := r.regenerateIndexLocked(); err != nil {
		return "", fmt.Errorf("wiki page: index regen: %w", err)
	}

	addArgs := append([]string{"add", "-A", "--"}, dedupePaths(append(paths, "index/all.md"))...)
	if out, err := r.runGitLockedAs(ctx, name, email, addArgs...); err != nil {
		return "", fmt.Errorf("wiki page: git add: %w: %s", err, out)
	}

	cached, err := r.runGitLockedAs(ctx, name, email, "diff", "--cached", "--name-only")
	if err != nil {
		return "", fmt.Errorf("wiki page: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cached) == "" {
		head, headErr := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if headErr != nil {
			return "", fmt.Errorf("wiki page: resolve HEAD sha: %w", headErr)
		}
		return strings.TrimSpace(head), nil
	}

	if out, err := r.runGitLockedAs(ctx, name, email, "commit", "-q", "-m", message); err != nil {
		return "", fmt.Errorf("wiki page: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("wiki page: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), nil
}

// remapSnapshotKeys returns a copy of the pre-move article snapshot with each
// moved page's body re-keyed to its post-move path, so writes target the
// destination file. Bodies of unmoved articles keep their keys.
func remapSnapshotKeys(snapshot map[string]string, moves []pageMove) map[string]string {
	toByFrom := make(map[string]string, len(moves))
	for _, m := range moves {
		toByFrom[filepath.ToSlash(m.From)] = filepath.ToSlash(m.To)
	}
	out := make(map[string]string, len(snapshot))
	for p, body := range snapshot {
		if to, ok := toByFrom[p]; ok {
			out[to] = body
			continue
		}
		out[p] = body
	}
	return out
}

// dedupePaths removes duplicate slash paths preserving first-seen order.
func dedupePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = filepath.ToSlash(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// seedPageBody builds the initial body of a new page. When title is non-empty
// and the supplied content does not already start with an H1, the title is
// prepended as `# Title`. An empty title + empty content yields a single
// newline so git has something to commit (an empty file is still a valid
// commit, but a trailing newline keeps editors happy).
func seedPageBody(title, content string) string {
	title = strings.TrimSpace(title)
	body := content
	trimmedBody := strings.TrimLeft(body, " \t\r\n")
	hasH1 := strings.HasPrefix(trimmedBody, "# ")
	if title != "" && !hasH1 {
		if strings.TrimSpace(body) == "" {
			return "# " + title + "\n"
		}
		return "# " + title + "\n\n" + body
	}
	if strings.TrimSpace(body) == "" {
		return "\n"
	}
	return body
}

// movePageCommitMessage builds a descriptive, human-prefixed commit message.
func movePageCommitMessage(from, to string, isDir bool, refs int) string {
	verb := "move"
	if isDir {
		verb = "move subtree"
	}
	base := fmt.Sprintf("human: %s %s -> %s", verb, from, to)
	if refs > 0 {
		return fmt.Sprintf("%s (rewrote %d reference%s)", base, refs, plural(refs))
	}
	return base
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// effectiveHumanIdentity resolves the commit identity, falling back to the
// synthetic `human <human@wuphf.local>` when any field is empty. Mirrors
// CommitHuman's identity derivation so attribution is consistent across the
// human-authored write paths.
func effectiveHumanIdentity(identity HumanIdentity) (name, email, slug string) {
	name = strings.TrimSpace(identity.Name)
	email = strings.TrimSpace(identity.Email)
	slug = strings.TrimSpace(identity.Slug)
	if name == "" || email == "" || slug == "" {
		return FallbackHumanIdentity.Name, FallbackHumanIdentity.Email, FallbackHumanIdentity.Slug
	}
	return name, email, slug
}

// ── HTTP handlers ──────────────────────────────────────────────────────────

// handleWikiPageCreate handles POST /wiki/page/create.
func (b *Broker) handleWikiPageCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	var body struct {
		Path    string `json:"path"`
		Title   string `json:"title"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	identity := b.resolvePageIdentity(r)
	sha, err := worker.Repo().CreatePage(r.Context(), body.Path, body.Title, body.Content, identity)
	if err != nil {
		writePageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":       cleanPagePath(body.Path),
		"commit_sha": sha,
	})
}

// handleWikiPageMove handles POST /wiki/page/move.
func (b *Broker) handleWikiPageMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	identity := b.resolvePageIdentity(r)
	res, err := worker.Repo().MovePage(r.Context(), body.From, body.To, identity)
	if err != nil {
		writePageError(w, err)
		return
	}
	b.publishPageMoveEvents(res, identity.Slug)
	writeJSON(w, http.StatusOK, res)
}

// handleWikiPageRename handles POST /wiki/page/rename — a thin wrapper that
// computes the destination path (same directory + sanitized newName + .md) and
// then runs the move logic.
func (b *Broker) handleWikiPageRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	var body struct {
		Path    string `json:"path"`
		NewName string `json:"newName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	to, err := renameTarget(body.Path, body.NewName)
	if err != nil {
		writePageError(w, err)
		return
	}
	identity := b.resolvePageIdentity(r)
	res, moveErr := worker.Repo().MovePage(r.Context(), body.Path, to, identity)
	if moveErr != nil {
		writePageError(w, moveErr)
		return
	}
	b.publishPageMoveEvents(res, identity.Slug)
	writeJSON(w, http.StatusOK, res)
}

// handleWikiPageDelete handles DELETE /wiki/page?path=...
func (b *Broker) handleWikiPageDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path query parameter is required"})
		return
	}
	identity := b.resolvePageIdentity(r)
	sha, err := worker.Repo().DeletePage(r.Context(), path, identity)
	if err != nil {
		writePageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":       cleanPagePath(path),
		"commit_sha": sha,
	})
}

// resolvePageIdentity resolves the human identity to stamp on a page mutation,
// preferring the authenticated request actor and falling back to the registered
// local identity. Clients cannot forge attribution — the identity is derived
// server-side, exactly as handleWikiWriteHuman does.
func (b *Broker) resolvePageIdentity(r *http.Request) HumanIdentity {
	identity := brokerHumanIdentityRegistry().Local()
	if actor, ok := requestActorFromContext(r.Context()); ok && actor.Kind == requestActorKindHuman {
		identity = humanIdentityFromActor(actor)
	}
	return identity
}

// publishPageMoveEvents fans out a wiki:write SSE event for every moved page and
// every rewritten article so open clients refetch without polling. The
// destination side is driven by res.MovedPaths (one entry per physically moved
// .md page) rather than res.To, because a directory move's res.To names a
// directory with no .md suffix and the actual pages live beneath it.
func (b *Broker) publishPageMoveEvents(res PageMoveResult, slug string) {
	ts := time.Now().UTC().Format(time.RFC3339)
	emitted := make(map[string]struct{}, len(res.MovedPaths)+len(res.RewrittenPaths))
	emit := func(p string) {
		if p == "" {
			return
		}
		if _, dup := emitted[p]; dup {
			return
		}
		emitted[p] = struct{}{}
		b.PublishWikiEvent(wikiWriteEvent{
			Path:       p,
			CommitSHA:  res.CommitSHA,
			AuthorSlug: slug,
			Timestamp:  ts,
		})
	}
	for _, p := range res.MovedPaths {
		emit(p)
	}
	for _, p := range res.RewrittenPaths {
		emit(p)
	}
}

// renameTarget computes the destination path for a rename: the source's
// directory + a sanitized newName + .md. The source path must validate under
// team/; newName must be a single safe path segment (no separators, traversal,
// or control bytes).
func renameTarget(path, newName string) (string, error) {
	clean := cleanPagePath(path)
	if clean == "" {
		return "", fmt.Errorf("%w: path is required", errWikiFSBadPath)
	}
	base := sanitizeRenameSegment(newName)
	if base == "" {
		return "", fmt.Errorf("%w: newName is required", errWikiFSBadPath)
	}
	dir := ""
	if idx := strings.LastIndexByte(clean, '/'); idx >= 0 {
		dir = clean[:idx]
	}
	target := base
	if !strings.HasSuffix(strings.ToLower(target), ".md") {
		target += ".md"
	}
	if dir == "" {
		return target, nil
	}
	return dir + "/" + target, nil
}

// sanitizeRenameSegment strips a single-segment new name down to a safe token:
// it rejects path separators, traversal, and control bytes. A caller-supplied
// ".md" extension is preserved (renameTarget re-adds it if absent).
func sanitizeRenameSegment(name string) string {
	name = strings.TrimSpace(filepath.ToSlash(name))
	name = strings.Trim(name, "/")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return ""
	}
	if hasControlByte(name) {
		return ""
	}
	return name
}

// cleanPagePath normalizes a caller path to slash form without surrounding
// whitespace. It is presentation-only normalization; full validation happens in
// resolveTeamRelPath inside the repo methods.
func cleanPagePath(path string) string {
	return filepath.ToSlash(strings.TrimSpace(path))
}

// writePageError maps a page-op error to the right HTTP status + JSON body.
func writePageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errWikiPageExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case errors.Is(err, errWikiPageMissing):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, errWikiFSBadPath):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		// Never forward the raw err: it can leak git stderr / filesystem layout.
		// Log it server-side and return a fixed string, matching the pattern in
		// wiki_fs_handlers.go.
		log.Printf("wiki page: internal error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "wiki page operation failed"})
	}
}
