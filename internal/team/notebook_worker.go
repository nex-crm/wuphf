package team

// notebook_worker.go hosts the per-agent notebook helpers that ride on top of
// the existing wiki write queue. Notebooks are the per-agent draft workspace:
// same git/markdown substrate as the wiki, opposite editorial posture.
//
// Data flow (shared with wiki)
// ============================
//
//	MCP handler (any goroutine)
//	        │
//	        │ NotebookWrite(ctx, slug, path, content, mode, msg)
//	        ▼
//	┌──────────────────────────────────────────────┐
//	│  wikiRequests chan (same shared queue)       │
//	└──────────┬───────────────────────────────────┘
//	           │
//	           ▼
//	   worker goroutine (drain loop — wiki_worker.go)
//	           │
//	           │ req.IsNotebook ? repo.CommitNotebook() : repo.Commit()
//	           │ publish wiki:write OR notebook:write
//	           │ async debounced BackupMirror
//	           ▼
//	       next request
//
// Rationale for the shared queue: any commit against ~/.wuphf/wiki/ must
// serialize through a single git writer. Wiki vs notebook is a path-prefix
// distinction, not a separate repo.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ErrNotebookPathNotAuthorOwned is returned when an agent tries to write to
// another agent's notebook directory. Notebooks are author-only on the write
// side; reads and searches are cross-agent by design (see DESIGN-NOTEBOOK.md).
var ErrNotebookPathNotAuthorOwned = errors.New("notebook_path_not_author_owned: write path must live under agents/{my_slug}/notebook/")

// notebookWriteEvent mirrors wikiWriteEvent but is routed to a separate SSE
// event name so the UI can subscribe by intent (notebook surface vs wiki).
type notebookWriteEvent struct {
	Slug      string `json:"slug"`
	Path      string `json:"path"`
	CommitSHA string `json:"commit_sha"`
	Timestamp string `json:"timestamp"`
}

// notebookEventPublisher is the extension the worker needs in order to emit
// notebook-scoped SSE events. Broker satisfies both wiki and notebook variants.
type notebookEventPublisher interface {
	PublishNotebookEvent(evt notebookWriteEvent)
}

// NotebookEntry summarises one entry in an agent's notebook catalog. Ordered
// by filename reverse-chron (dated-prefix filenames sort naturally).
type NotebookEntry struct {
	Path      string    `json:"path"`
	Title     string    `json:"title"`
	Modified  time.Time `json:"modified"`
	SizeBytes int64     `json:"size_bytes"`
}

// NotebookWrite submits a notebook write to the shared wiki queue. The slug
// MUST match the agent slug embedded in the path — enforced here before the
// request is handed off to the worker.
func (w *WikiWorker) NotebookWrite(ctx context.Context, slug, path, content, mode, commitMsg string) (string, int, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", 0, fmt.Errorf("notebook: write requires an author slug")
	}
	if err := validateNotebookWritePath(slug, path); err != nil {
		return "", 0, err
	}
	if !w.running.Load() {
		return "", 0, ErrWorkerStopped
	}
	req := wikiWriteRequest{
		Slug:       slug,
		Path:       path,
		Content:    content,
		Mode:       mode,
		CommitMsg:  commitMsg,
		IsNotebook: true,
		ReplyCh:    make(chan wikiWriteResult, 1),
	}
	select {
	case w.requests <- req:
	default:
		return "", 0, ErrQueueSaturated
	}
	waitCtx, cancel := context.WithTimeout(ctx, wikiWriteTimeout)
	defer cancel()
	select {
	case result := <-req.ReplyCh:
		return result.SHA, result.BytesWritten, result.Err
	case <-waitCtx.Done():
		return "", 0, fmt.Errorf("notebook: write timed out after %s", wikiWriteTimeout)
	}
}

// NotebookList returns the agent's notebook entries newest-first. Empty slice
// (never nil) when the agent has no entries — callers can marshal as JSON
// without null-guarding.
func (w *WikiWorker) NotebookList(slug string) ([]NotebookEntry, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, fmt.Errorf("notebook: slug is required")
	}
	if err := validateNotebookSlug(slug); err != nil {
		return nil, err
	}
	dir := filepath.Join(w.repo.Root(), "agents", slug, "notebook")
	w.repo.mu.Lock()
	defer w.repo.mu.Unlock()
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return []NotebookEntry{}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("notebook: read dir: %w", err)
	}
	out := make([]NotebookEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || e.Name() == ".gitkeep" {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		fullPath := filepath.Join(dir, e.Name())
		fi, statErr := os.Stat(fullPath)
		if statErr != nil {
			continue
		}
		rel := filepath.ToSlash(filepath.Join("agents", slug, "notebook", e.Name()))
		out = append(out, NotebookEntry{
			Path:      rel,
			Title:     extractArticleTitle(fullPath),
			Modified:  fi.ModTime().UTC(),
			SizeBytes: fi.Size(),
		})
	}
	// Reverse-chron by filename first (dated-prefix filenames like
	// 2026-04-20-retro.md sort naturally), then fall back to mtime for
	// un-dated filenames. Sorting by filename keeps the ordering stable even
	// if mtimes collide on fast test runs.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path > out[j].Path
		}
		return out[i].Modified.After(out[j].Modified)
	})
	return out, nil
}

// AgentsWithNotebooks walks the wiki repo and returns the slugs of every
// agent that has at least a `agents/{slug}/notebook/` directory. Used by the
// bookshelf catalog so the UI does not need to pre-enumerate the roster.
// Order is lexicographic for stable rendering.
func (w *WikiWorker) AgentsWithNotebooks() ([]string, error) {
	root := filepath.Join(w.repo.Root(), "agents")
	w.repo.mu.Lock()
	defer w.repo.mu.Unlock()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("notebook: read agents dir: %w", err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		slug := e.Name()
		if validateNotebookSlug(slug) != nil {
			continue
		}
		nbDir := filepath.Join(root, slug, "notebook")
		if info, statErr := os.Stat(nbDir); statErr == nil && info.IsDir() {
			out = append(out, slug)
		}
	}
	sort.Strings(out)
	return out, nil
}

// NotebookRead returns raw entry bytes for any agent's notebook. Cross-agent
// reads are intentional: notebooks are private-by-convention, not by access
// control.
func (w *WikiWorker) NotebookRead(path string) ([]byte, error) {
	if err := validateNotebookPath(path); err != nil {
		return nil, err
	}
	w.repo.mu.Lock()
	defer w.repo.mu.Unlock()
	return os.ReadFile(filepath.Join(w.repo.Root(), path))
}

// NotebookSearch runs a literal substring search scoped to a single agent's
// notebook subtree. Pattern is escaped by the caller path; this function
// only does substring matching — no regex — so there is no injection surface.
func (w *WikiWorker) NotebookSearch(slug, pattern string) ([]WikiSearchHit, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, fmt.Errorf("notebook: slug is required")
	}
	if err := validateNotebookSlug(slug); err != nil {
		return nil, err
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil, fmt.Errorf("notebook: search pattern is required")
	}
	dir := filepath.Join(w.repo.Root(), "agents", slug, "notebook")
	w.repo.mu.Lock()
	defer w.repo.mu.Unlock()
	if _, err := os.Stat(dir); err != nil {
		return []WikiSearchHit{}, nil
	}
	const maxHits = 100
	hits := make([]WikiSearchHit, 0, 16)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
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
		rel, _ := filepath.Rel(w.repo.Root(), path)
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
		return nil, fmt.Errorf("notebook: search walk: %w", err)
	}
	return hits, nil
}

// commitNotebookLocked writes a notebook entry and commits it under the
// author's slug. Does NOT regen the team wiki index — notebooks are per-agent
// and do not feed team/ catalog. Caller must hold r.mu.
func (r *Repo) commitNotebookLocked(ctx context.Context, slug, relPath, content, mode, message string) (string, int, error) {
	if err := validateNotebookWritePath(slug, relPath); err != nil {
		return "", 0, err
	}
	fullPath := filepath.Join(r.root, relPath)

	switch mode {
	case "create":
		if _, err := os.Stat(fullPath); err == nil {
			return "", 0, fmt.Errorf("notebook: entry already exists at %q; use replace or append_section", relPath)
		}
	case "replace":
		if _, err := os.Stat(fullPath); err != nil {
			return "", 0, fmt.Errorf("notebook: entry does not exist at %q; use create", relPath)
		}
	case "append_section":
		// handled below
	default:
		return "", 0, fmt.Errorf("notebook: unknown write mode %q; expected create|replace|append_section", mode)
	}

	if strings.TrimSpace(content) == "" {
		return "", 0, fmt.Errorf("notebook: content is required")
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		return "", 0, fmt.Errorf("notebook: mkdir %s: %w", filepath.Dir(fullPath), err)
	}

	var bytesWritten int
	switch mode {
	case "create", "replace":
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			return "", 0, fmt.Errorf("notebook: write entry: %w", err)
		}
		bytesWritten = len(content)
	case "append_section":
		existing, err := os.ReadFile(fullPath)
		if err != nil && !os.IsNotExist(err) {
			return "", 0, fmt.Errorf("notebook: read for append: %w", err)
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
			return "", 0, fmt.Errorf("notebook: write entry: %w", err)
		}
		bytesWritten = len(content)
	}

	relForGit := filepath.ToSlash(relPath)
	if out, err := r.runGitLocked(ctx, slug, "add", "--", relForGit); err != nil {
		return "", 0, fmt.Errorf("notebook: git add %s: %w: %s", relPath, err, out)
	}

	// Byte-identical re-write: nothing to commit. Return HEAD short sha so
	// downstream code (SSE event, response body) stays well-formed.
	cachedDiff, err := r.runGitLocked(ctx, slug, "diff", "--cached", "--name-only")
	if err != nil {
		return "", 0, fmt.Errorf("notebook: git diff --cached: %w", err)
	}
	if strings.TrimSpace(cachedDiff) == "" {
		headSha, err := r.runGitLocked(ctx, "system", "rev-parse", "--short", "HEAD")
		if err != nil {
			return "", 0, fmt.Errorf("notebook: resolve HEAD sha: %w", err)
		}
		return strings.TrimSpace(headSha), bytesWritten, nil
	}

	commitMsg := strings.TrimSpace(message)
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("notebook: update %s", relPath)
	}
	if out, err := r.runGitLocked(ctx, slug, "commit", "-q", "-m", commitMsg); err != nil {
		return "", 0, fmt.Errorf("notebook: git commit: %w: %s", err, out)
	}
	sha, err := r.runGitLocked(ctx, slug, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", 0, fmt.Errorf("notebook: resolve HEAD sha: %w", err)
	}
	return strings.TrimSpace(sha), bytesWritten, nil
}

// CommitNotebook writes + commits a notebook entry. Exposed for the worker.
func (r *Repo) CommitNotebook(ctx context.Context, slug, relPath, content, mode, message string) (string, int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.commitNotebookLocked(ctx, slug, relPath, content, mode, message)
}

// validateNotebookWritePath enforces that the path lives under
// agents/{slug}/notebook/ where slug is the caller's own agent slug.
func validateNotebookWritePath(slug, relPath string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return fmt.Errorf("notebook: my_slug is required")
	}
	if err := validateNotebookSlug(slug); err != nil {
		return err
	}
	if err := validateNotebookPath(relPath); err != nil {
		return err
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	prefix := "agents/" + slug + "/notebook/"
	if !strings.HasPrefix(clean, prefix) {
		return fmt.Errorf("%w: got path %q, expected %s...", ErrNotebookPathNotAuthorOwned, relPath, prefix)
	}
	rest := strings.TrimPrefix(clean, prefix)
	if rest == "" || strings.Contains(rest, "/") {
		// Forbid nested subdirs under notebook/ for v1.1. Keeps the listing
		// flat and the catalog/promotion flow simple.
		return fmt.Errorf("notebook: entries must live directly under %s (no subdirectories); got %q", prefix, relPath)
	}
	return nil
}

// validateNotebookPath allows any agents/{slug}/notebook/{file}.md path,
// regardless of which slug owns it. Used by read/list/search which are
// cross-agent.
func validateNotebookPath(relPath string) error {
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return fmt.Errorf("notebook: path is required")
	}
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("notebook: path must be relative; got %q", relPath)
	}
	clean := filepath.ToSlash(filepath.Clean(relPath))
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || clean == ".." {
		return fmt.Errorf("notebook: path must not contain ..; got %q", relPath)
	}
	if !strings.HasPrefix(clean, "agents/") {
		return fmt.Errorf("notebook: path must be within agents/; got %q", relPath)
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 4 || parts[2] != "notebook" {
		return fmt.Errorf("notebook: path must match agents/{slug}/notebook/...; got %q", relPath)
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".md") {
		return fmt.Errorf("notebook: path must end with .md; got %q", relPath)
	}
	return nil
}

// validateNotebookSlug guards against slug values that would break filesystem
// paths or be mistaken for directory traversal. Keep the allowed charset
// tight; agent slugs are kebab-case alphanumerics in WUPHF.
func validateNotebookSlug(slug string) error {
	if slug == "" {
		return fmt.Errorf("notebook: slug is required")
	}
	if slug == "." || slug == ".." {
		return fmt.Errorf("notebook: invalid slug %q", slug)
	}
	for _, r := range slug {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return fmt.Errorf("notebook: slug %q contains invalid characters (allowed: a-z, A-Z, 0-9, -, _)", slug)
		}
	}
	return nil
}
