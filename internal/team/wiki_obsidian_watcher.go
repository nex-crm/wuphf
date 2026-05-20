package team

// wiki_obsidian_watcher.go is the Phase-3 daemon that captures Obsidian-side
// edits to the on-disk wiki and routes them through Repo.Commit so the
// single-writer invariant (WIKI-SCHEMA §1.3) keeps holding when humans edit
// the working tree directly.
//
// Commit pipeline (in order):
//
//   - fsnotify watch on <wiki-root>/team/, recursive (re-arm on new dirs)
//   - debounce per-path; latest write wins
//   - drop events that follow a NotifyWrite within its TTL
//   - resolve author via an injected identity callback; skip when absent
//   - stamp `last_human_edit_ts` frontmatter sentinel (WIKI-OBSIDIAN §6.3)
//   - normalize loose `[[Acme]]` wikilinks on brief paths (§5)
//   - ingest `![[image.png]]` siblings into team/inbox/raw/ (§7.2)
//   - call Repo.Commit, which takes an advisory flock for the write (§6.2)

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	defaultObsidianDebounce     = 1500 * time.Millisecond
	defaultObsidianWriteFilter  = 5 * time.Second
	obsidianWatcherCommitTimout = 10 * time.Second
)

// ObsidianWatcherIdentity resolves the author slug for an externally
// originated wiki edit. When ok is false the watcher logs a warning and
// skips the commit — there is no synthetic fallback because that would
// silently misattribute human edits.
type ObsidianWatcherIdentity func() (slug string, ok bool)

// ObsidianLooseLinkResolver resolves an Obsidian-typed wikilink target
// (display string or bare slug) to the canonical (kind, slug) pair used in
// the kinded wikilink form. Returning ok=false leaves the loose link
// unmodified — per WIKI-OBSIDIAN-COMPATIBILITY §5 ambiguous matches must
// not be auto-rewritten.
type ObsidianLooseLinkResolver func(displayOrSlug string) (kind EntityKind, slug string, ok bool)

// ObsidianEmbedIngester moves a brief-local image referenced by `![[...]]`
// into team/inbox/raw/ and returns the rewritten body. The function lives
// behind an interface so tests can inject a fake without touching disk.
type ObsidianEmbedIngester func(repo *Repo, relPath, body string) (string, []string, error)

// ObsidianWatcher is the fsnotify-driven daemon that funnels external
// edits to <wiki-root>/team/ back through Repo.Commit.
type ObsidianWatcher struct {
	repo   *Repo
	worker *WikiWorker

	mu         sync.Mutex
	identityFn ObsidianWatcherIdentity
	normalizer ObsidianLooseLinkResolver
	embedFn    ObsidianEmbedIngester
	debounceMs time.Duration
	writeTTL   time.Duration
	timers     map[string]*time.Timer
	recent     map[string]time.Time
	logger     *log.Logger

	watcher *fsnotify.Watcher
	running atomic.Bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	pending sync.WaitGroup
}

// NewObsidianWatcher returns a watcher bound to the given repo + worker. The
// watcher does nothing until Start is called.
func NewObsidianWatcher(repo *Repo, worker *WikiWorker) *ObsidianWatcher {
	return &ObsidianWatcher{
		repo:       repo,
		worker:     worker,
		debounceMs: defaultObsidianDebounce,
		writeTTL:   defaultObsidianWriteFilter,
		timers:     make(map[string]*time.Timer),
		recent:     make(map[string]time.Time),
	}
}

// SetIdentity wires the callback used to resolve the author slug for an
// external edit. Tests inject a fake; production wires it through the
// broker's per-human identity. Safe to call before or after Start.
func (w *ObsidianWatcher) SetIdentity(fn ObsidianWatcherIdentity) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.identityFn = fn
}

// SetNormalizer wires the loose-link resolver used to rewrite Obsidian-typed
// `[[Acme Corp]]` to `[[companies/acme-corp|Acme Corp]]`. Nil disables
// normalization (the commit pipeline still runs). Production wires this to
// the signal index; tests inject a static map.
func (w *ObsidianWatcher) SetNormalizer(fn ObsidianLooseLinkResolver) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.normalizer = fn
}

// SetEmbedIngester wires the image-embed pass that runs after the normalizer
// and before Repo.Commit. Nil disables embed ingestion. Production wires
// this to IngestImageEmbeds; tests inject a fake to keep the disk cold.
func (w *ObsidianWatcher) SetEmbedIngester(fn ObsidianEmbedIngester) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.embedFn = fn
}

// SetLogger swaps the destination for warning lines. Defaults to the
// standard logger. Tests inject a buffer-backed logger to capture output.
func (w *ObsidianWatcher) SetLogger(l *log.Logger) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.logger = l
}

// SetDebounceForTest overrides the trailing-edge debounce window. Test-only;
// production keeps the default 1.5s.
func (w *ObsidianWatcher) SetDebounceForTest(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if d > 0 {
		w.debounceMs = d
	}
}

// SetWriteTTLForTest overrides how long a NotifyWrite suppresses subsequent
// fsnotify events for the same path. Test-only.
func (w *ObsidianWatcher) SetWriteTTLForTest(d time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if d > 0 {
		w.writeTTL = d
	}
}

// NotifyWrite records that the worker (or any other internal writer) just
// touched relPath. fsnotify events on that path within writeTTL are dropped
// so the watcher never re-commits its own outputs.
func (w *ObsidianWatcher) NotifyWrite(relPath string) {
	rel := normalizeRel(relPath)
	if rel == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.recent[rel] = time.Now()
}

// Start launches the fsnotify watcher and the event-dispatch goroutine. It
// returns once the recursive add of team/ has completed. Errors during the
// initial walk abort the start; transient errors during steady-state are
// logged and elided.
func (w *ObsidianWatcher) Start(ctx context.Context) error {
	if w.running.Swap(true) {
		return errors.New("obsidian watcher: already running")
	}
	teamDir := w.repo.TeamDir()
	if _, err := os.Stat(teamDir); err != nil {
		w.running.Store(false)
		return fmt.Errorf("obsidian watcher: team dir: %w", err)
	}
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		w.running.Store(false)
		return fmt.Errorf("obsidian watcher: new fsnotify: %w", err)
	}
	w.watcher = fs
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})

	if err := w.addRecursive(teamDir); err != nil {
		_ = fs.Close()
		w.running.Store(false)
		return fmt.Errorf("obsidian watcher: add recursive: %w", err)
	}

	go w.run(ctx)
	return nil
}

// Stop shuts down the fsnotify watcher and waits for any in-flight debounce
// timers and commit goroutines to drain.
func (w *ObsidianWatcher) Stop() error {
	if !w.running.Swap(false) {
		return nil
	}
	close(w.stopCh)
	if w.watcher != nil {
		_ = w.watcher.Close()
	}
	<-w.doneCh
	// Cancel any timers that have not yet fired. A timer.Stop() returning
	// true means the AfterFunc callback never ran, so we own the Add() that
	// was paired with it and must call Done() here. Returning false means
	// the callback has already started or completed; its own deferred
	// Done() in the AfterFunc wrapper will run.
	w.mu.Lock()
	for k, t := range w.timers {
		if t.Stop() {
			w.pending.Done()
		}
		delete(w.timers, k)
	}
	w.mu.Unlock()
	w.pending.Wait()
	return nil
}

// run is the single dispatcher goroutine. It receives fsnotify events, walks
// new directories, and schedules debounced commits.
func (w *ObsidianWatcher) run(ctx context.Context) {
	defer close(w.doneCh)
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case ev, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(ctx, ev)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logf("obsidian watcher: fsnotify error: %v", err)
		}
	}
}

// handleEvent classifies a single fsnotify event: new directories get an
// Add; ignored paths short-circuit; eligible writes schedule a debounce.
func (w *ObsidianWatcher) handleEvent(ctx context.Context, ev fsnotify.Event) {
	if ev.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			if err := w.addRecursive(ev.Name); err != nil {
				w.logf("obsidian watcher: add dir %s: %v", ev.Name, err)
			}
			return
		}
	}
	// Only write / create on .md files trigger commit logic. Renames and
	// removes are not in scope for the skeleton.
	if ev.Op&(fsnotify.Write|fsnotify.Create) == 0 {
		return
	}
	rel, ok := w.relPath(ev.Name)
	if !ok {
		return
	}
	if !isObsidianWatchableMarkdown(rel) {
		return
	}
	w.schedule(ctx, rel)
}

// schedule debounces commits per-path. The latest write wins: a new event
// during an existing window resets the timer.
//
// The pending WaitGroup is incremented HERE (under the lock), not inside the
// AfterFunc callback, so the Add is sequenced before any Wait in Stop(). The
// callback owns its own Done() via the defer in the AfterFunc wrapper. When
// an existing timer is replaced and Stop() returns true (callback never
// ran), schedule() pays the Done() for the cancelled timer before adding
// the new one.
func (w *ObsidianWatcher) schedule(ctx context.Context, rel string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running.Load() {
		return
	}
	if existing, ok := w.timers[rel]; ok {
		if existing.Stop() {
			w.pending.Done()
		}
	}
	d := w.debounceMs
	w.pending.Add(1)
	w.timers[rel] = time.AfterFunc(d, func() {
		defer w.pending.Done()
		w.fire(ctx, rel)
	})
}

// fire runs at the trailing edge of a debounce window. It rechecks the
// worker-write TTL, resolves the author, and synchronously calls
// Repo.Commit. Errors are logged. The pending WaitGroup is managed by the
// caller (schedule's AfterFunc wrapper).
func (w *ObsidianWatcher) fire(ctx context.Context, rel string) {
	w.mu.Lock()
	delete(w.timers, rel)
	if !w.running.Load() {
		w.mu.Unlock()
		return
	}
	if ts, ok := w.recent[rel]; ok {
		if time.Since(ts) < w.writeTTL {
			w.mu.Unlock()
			return
		}
		delete(w.recent, rel)
	}
	identityFn := w.identityFn
	normalizer := w.normalizer
	embedFn := w.embedFn
	w.mu.Unlock()

	if identityFn == nil {
		w.logf("obsidian watcher: no identity callback set; skipping commit for %s", rel)
		return
	}
	slug, ok := identityFn()
	if !ok || strings.TrimSpace(slug) == "" {
		w.logf("obsidian watcher: identity unavailable; skipping commit for %s", rel)
		return
	}

	full := filepath.Join(w.repo.Root(), filepath.FromSlash(rel))
	content, err := os.ReadFile(full)
	if err != nil {
		// File can vanish between fsnotify Write and the trailing-edge
		// read (e.g. Obsidian's atomic-write rename followed by a fast
		// delete). Drop the event silently — a subsequent write will
		// re-arm the debounce.
		if !errors.Is(err, os.ErrNotExist) {
			w.logf("obsidian watcher: read %s: %v", rel, err)
		}
		return
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		// Repo.Commit rejects empty bodies; treat zero-byte writes as
		// transient (Obsidian touches a fresh file before populating it).
		return
	}

	body := string(content)
	if stamped, ferr := applyHumanEditSentinel(body, time.Now().UTC()); ferr == nil {
		body = stamped
	} else {
		w.logf("obsidian watcher: sentinel stamp %s: %v", rel, ferr)
	}

	if normalizer != nil && isBriefPath(rel) {
		if rewritten, changed := NormalizeLooseWikilinks(body, normalizer); changed {
			body = rewritten
		}
	}

	if embedFn != nil && isBriefPath(rel) {
		if rewritten, ingested, ierr := embedFn(w.repo, rel, body); ierr != nil {
			w.logf("obsidian watcher: image embed %s: %v", rel, ierr)
		} else {
			body = rewritten
			for _, p := range ingested {
				w.NotifyWrite(p)
			}
		}
	}

	commitCtx, cancel := context.WithTimeout(ctx, obsidianWatcherCommitTimout)
	defer cancel()
	msg := fmt.Sprintf("wiki: external edit to %s", rel)
	if _, _, err := w.repo.Commit(commitCtx, slug, rel, body, "replace", msg); err != nil {
		w.logf("obsidian watcher: commit %s: %v", rel, err)
	}
}

// addRecursive walks dir and registers every subdirectory with the
// fsnotify watcher. Files inside the dir are not added individually —
// fsnotify reports parent-dir events for them on Linux.
func (w *ObsidianWatcher) addRecursive(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(w.repo.Root(), path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if isObsidianIgnoredDir(relSlash) {
			return filepath.SkipDir
		}
		return w.watcher.Add(path)
	})
}

// relPath converts an absolute event path into a slash-separated repo-rooted
// path. Returns ("", false) when the path escapes the repo root.
func (w *ObsidianWatcher) relPath(abs string) (string, bool) {
	rel, err := filepath.Rel(w.repo.Root(), abs)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "..") {
		return "", false
	}
	return rel, true
}

// logf routes a warning through the injected logger when set, otherwise the
// default `log` package destination. Kept as a helper so tests can capture.
func (w *ObsidianWatcher) logf(format string, args ...any) {
	w.mu.Lock()
	l := w.logger
	w.mu.Unlock()
	if l != nil {
		l.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

func normalizeRel(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(rel))
}

// isObsidianWatchableMarkdown returns true when rel is a candidate for the
// external-edit commit pipeline: under team/, ends in .md, and not in any of
// the ignored worker-owned or derived subtrees.
func isObsidianWatchableMarkdown(rel string) bool {
	if !strings.HasPrefix(rel, "team/") {
		return false
	}
	if !strings.HasSuffix(strings.ToLower(rel), ".md") {
		return false
	}
	base := filepath.Base(rel)
	if strings.HasPrefix(base, ".") {
		// `.gitkeep` and `team/.obsidian/app.json` are not .md so they
		// never reach this branch; any other dotfile is ignored.
		return false
	}
	if isObsidianIgnoredPath(rel) {
		return false
	}
	return true
}

// isObsidianIgnoredPath returns true when rel is inside a worker-owned or
// derived subtree (compiled skills, the graph log, the raw inbox, the
// Obsidian config dir).
func isObsidianIgnoredPath(rel string) bool {
	switch {
	case strings.HasPrefix(rel, "team/playbooks/.compiled/"):
		return true
	case rel == "team/entities/.graph.jsonl":
		return true
	case strings.HasPrefix(rel, "team/inbox/raw/"):
		return true
	case strings.HasPrefix(rel, "team/.obsidian/"):
		return rel != "team/.obsidian/app.json"
	}
	return false
}

// isObsidianIgnoredDir returns true when a directory walk should skip a
// subtree entirely. Mirrors isObsidianIgnoredPath at the directory level so
// the initial recursive watch never registers ignored trees.
func isObsidianIgnoredDir(rel string) bool {
	// Nested directories under team/.obsidian (themes/, plugins/,
	// snippets/, ...) are all user-owned config we never observe. Skip
	// them so the recursive walk doesn't register watches it'll never
	// use. team/.obsidian itself is intentionally NOT skipped — fsnotify
	// on Linux delivers file-level Writes via the parent directory's
	// watch, which is how we see app.json edits.
	if strings.HasPrefix(rel, "team/.obsidian/") {
		return true
	}
	switch rel {
	case "team/playbooks/.compiled":
		return true
	case "team/inbox/raw":
		return true
	case "team/.obsidian":
		return false
	}
	return false
}
