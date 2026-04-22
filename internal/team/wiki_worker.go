package team

// wiki_worker.go hosts the single-goroutine write queue for the team wiki.
//
// Data flow
// =========
//
//	MCP handler (any goroutine)
//	        │
//	        │ Enqueue(ctx, req{Slug,Path,Content,Mode,Msg,ReplyCh})
//	        ▼
//	┌──────────────────────────┐
//	│  wikiRequests chan (64)  │   buffered; fail-fast on full
//	└──────────┬───────────────┘
//	           │
//	           ▼
//	   worker goroutine (drain loop)
//	           │
//	           │ repo.Commit(slug, path, content, mode, msg)
//	           │ repo.IndexRegen(ctx)
//	           │ reply via req.ReplyCh
//	           │ publishWikiEventLocked(payload)   ─► SSE "wiki:write"
//	           │ async debounced BackupMirror      ─► ~/.wuphf/wiki.bak/
//	           ▼
//	       next request
//
// Channel-serialized by design; no sync.Mutex around the hot path — the repo
// goroutine is the only writer. Timeout is enforced per-request.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrQueueSaturated is returned by Enqueue when the buffered request channel
// is full. Callers (MCP handlers) should surface this to the agent as
// "wiki queue saturated, retry on next turn" — no hidden retries.
var ErrQueueSaturated = errors.New("wiki: queue saturated, retry on next turn")

// ErrWorkerStopped is returned when Enqueue is called after the worker has
// been stopped (context cancelled).
var ErrWorkerStopped = errors.New("wiki: worker is not running")

// wikiRequestBuffer is the channel buffer size. Kept as a package-level const
// so regression tests can assert against it without touching the struct.
const wikiRequestBuffer = 64

// wikiWriteTimeout bounds each commit+index+reply round-trip.
const wikiWriteTimeout = 10 * time.Second

// wikiBackupDebounce avoids redundant mirror copies under burst load.
const wikiBackupDebounce = 2 * time.Second

// wikiWriteEvent is the SSE payload broadcast on every successful commit.
// No article content is included — the UI re-fetches via the read API.
type wikiWriteEvent struct {
	Path       string `json:"path"`
	CommitSHA  string `json:"commit_sha"`
	AuthorSlug string `json:"author_slug"`
	Timestamp  string `json:"timestamp"`
}

// wikiWriteRequest carries a single write off the MCP handler goroutine onto
// the worker. The reply channel is single-use and buffered to 1 so the
// worker can always send without blocking even if the caller's context dies.
type wikiWriteRequest struct {
	Slug      string
	Path      string
	Content   string
	Mode      string
	CommitMsg string
	// IsNotebook routes the request to Repo.CommitNotebook instead of
	// Repo.Commit. Same serialization primitive; different target subtree
	// and no team-wiki index regen. See notebook_worker.go.
	IsNotebook bool
	// IsEntityFact routes the request to Repo.CommitEntityFact. Used for
	// the v1.2 append-only fact log at team/entities/{kind}-{slug}.facts.jsonl
	// — same serialization primitive, non-.md extension, no index regen.
	IsEntityFact bool
	// IsEntityGraph routes the request to Repo.CommitEntityGraph. Used for
	// the cross-entity adjacency log at team/entities/.graph.jsonl.
	// Same serialization primitive as IsEntityFact — non-.md extension,
	// no wiki-index regen, no backlinks recompute.
	IsEntityGraph bool
	// IsPlaybookCompile routes to Repo.CommitPlaybookSkill — writes the
	// compiled SKILL.md under team/playbooks/.compiled/{slug}/ without
	// regenerating the catalog. v1.3 compounding-intelligence compiler.
	IsPlaybookCompile bool
	// IsPlaybookExecution routes to Repo.CommitPlaybookExecution — appends
	// to team/playbooks/{slug}.executions.jsonl. Same append-only semantics
	// as entity facts.
	IsPlaybookExecution bool
	// PlaybookSlug carries the source slug so the post-write hook can
	// enqueue a follow-up recompile without re-parsing the path.
	PlaybookSlug string
	// IsHuman routes the request to Repo.CommitHuman — optimistic
	// concurrency via expected_sha, per-human git identity. Wikipedia-
	// style Edit source flow. v1.5: identity is resolved from the
	// HumanIdentityRegistry rather than being hard-coded.
	IsHuman bool
	// ExpectedSHA is consulted by the human write path. Empty means the
	// caller expects the article not to exist yet (new-article flow).
	ExpectedSHA string
	// HumanIdentity carries the per-human git identity used when
	// IsHuman is true. Zero value falls back to the synthetic
	// `human <human@wuphf.local>` author.
	HumanIdentity HumanIdentity
	ReplyCh       chan wikiWriteResult
}

// wikiWriteResult is the worker's reply for a single request.
type wikiWriteResult struct {
	SHA          string
	BytesWritten int
	Err          error
}

// wikiEventPublisher is the subset of Broker the worker needs. Having it as
// an interface keeps the worker testable without spinning up an HTTP server.
type wikiEventPublisher interface {
	PublishWikiEvent(evt wikiWriteEvent)
}

// wikiSectionsNotifier is the optional hook the worker pokes on every
// successful wiki write so the sections cache can debounce + recompute.
// Kept as its own interface so wiki_worker.go doesn't take a hard
// dependency on the sections cache (which lives in wiki_sections.go).
type wikiSectionsNotifier interface {
	EnqueueSectionsRefresh()
}

// noopPublisher is used when the worker runs without a broker attached
// (tests, or --memory-backend markdown without a broker yet).
type noopPublisher struct{}

func (noopPublisher) PublishWikiEvent(wikiWriteEvent)         {}
func (noopPublisher) PublishNotebookEvent(notebookWriteEvent) {}
func (noopPublisher) PublishPlaybookExecutionRecorded(PlaybookExecutionRecordedEvent) {
}

// WikiWorker owns the single goroutine that drains the write request queue.
type WikiWorker struct {
	repo      *Repo
	publisher wikiEventPublisher
	index     *WikiIndex // optional derived cache; nil means no-op
	requests  chan wikiWriteRequest

	running       atomic.Bool
	mu            sync.Mutex // guards lastBackupAt
	lastBackupAt  time.Time
	backupPending atomic.Bool

	// sideGoroutines tracks async helpers (e.g. auto-recompile, backup
	// mirror) spawned from the drain loop so Stop() and WaitForIdle() can
	// block until they finish — essential for tests on Linux where
	// t.TempDir() cleanup otherwise races in-flight repo writes.
	sideGoroutines sync.WaitGroup

	// drainDone closes when the drain goroutine has fully exited (including
	// its own sideGoroutines.Wait). Tests register `t.Cleanup(func() {
	// cancel(); <-worker.Done() })` so tempdir removal is deterministic.
	drainDone chan struct{}
}

// NewWikiWorker returns a worker ready to Start. The publisher is optional;
// when nil, events are dropped silently. The worker's index is nil — no
// derived-cache updates occur. Use NewWikiWorkerWithIndex when an index is
// available (production path).
func NewWikiWorker(repo *Repo, publisher wikiEventPublisher) *WikiWorker {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	return &WikiWorker{
		repo:      repo,
		publisher: publisher,
		requests:  make(chan wikiWriteRequest, wikiRequestBuffer),
		drainDone: make(chan struct{}),
	}
}

// NewWikiWorkerWithIndex is the production constructor. It behaves identically
// to NewWikiWorker but additionally wires up a WikiIndex so that after every
// successful commit the worker reconciles the affected path into the derived
// cache (SQLite+bleve in prod, in-memory for tests). The index update runs in
// a side goroutine tracked by sideGoroutines — WaitForIdle() covers it.
func NewWikiWorkerWithIndex(repo *Repo, publisher wikiEventPublisher, index *WikiIndex) *WikiWorker {
	w := NewWikiWorker(repo, publisher)
	w.index = index
	return w
}

// Start launches the drain goroutine. Returns immediately. The worker stops
// when ctx is cancelled.
func (w *WikiWorker) Start(ctx context.Context) {
	if w.running.Swap(true) {
		return // already running
	}
	go w.drain(ctx)
}

// Stop is a test helper that closes the request channel so the drain loop
// returns. Production code should cancel the context passed to Start instead.
//
// Ordering matters: mark as stopped → wait for any in-flight side
// goroutines (e.g. auto-recompile helpers that take the queue) → close
// the channel. Without the wait, a recompile goroutine can attempt to
// send on a closed channel and panic.
func (w *WikiWorker) Stop() {
	if !w.running.Swap(false) {
		return
	}
	w.sideGoroutines.Wait()
	close(w.requests)
}

// Enqueue submits a write request to the worker and blocks (up to
// wikiWriteTimeout) for the reply. Returns ErrQueueSaturated if the queue is
// full — callers should surface this as a tool error with no hidden retry.
func (w *WikiWorker) Enqueue(ctx context.Context, slug, path, content, mode, commitMsg string) (string, int, error) {
	if !w.running.Load() {
		return "", 0, ErrWorkerStopped
	}
	req := wikiWriteRequest{
		Slug:      slug,
		Path:      path,
		Content:   content,
		Mode:      mode,
		CommitMsg: commitMsg,
		ReplyCh:   make(chan wikiWriteResult, 1),
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
		return "", 0, fmt.Errorf("wiki: write timed out after %s", wikiWriteTimeout)
	}
}

// drain is the single worker goroutine. It runs exactly one request at a time.
func (w *WikiWorker) drain(ctx context.Context) {
	defer close(w.drainDone)
	defer w.running.Store(false)
	// Wait for detached helpers (auto-recompile, backup mirror) before the
	// drain goroutine returns so test harnesses that cancel the context see
	// a quiesced worker — otherwise background writes to the repo can race
	// t.TempDir() cleanup.
	defer w.sideGoroutines.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-w.requests:
			if !ok {
				return
			}
			w.process(ctx, req)
		}
	}
}

// process handles one request end-to-end: commit → index → reply → event →
// async backup. It never panics; all errors are surfaced via req.ReplyCh.
func (w *WikiWorker) process(ctx context.Context, req wikiWriteRequest) {
	// Commit under a write-scoped context so a slow git exec cannot hang
	// the whole worker forever.
	writeCtx, cancel := context.WithTimeout(ctx, wikiWriteTimeout)
	defer cancel()

	var (
		sha string
		n   int
		err error
	)
	if req.IsHuman {
		// Human edits use optimistic concurrency (expected_sha) and the
		// per-human identity resolved from the registry — req.Slug is
		// ignored on this branch to prevent a caller from forging
		// attribution. Zero-value HumanIdentity falls back to the
		// synthetic `human` author inside CommitHuman.
		sha, n, err = w.repo.CommitHuman(writeCtx, req.Path, req.Content, req.ExpectedSHA, req.CommitMsg, req.HumanIdentity)
	} else if req.IsEntityFact {
		sha, n, err = w.repo.CommitEntityFact(writeCtx, req.Slug, req.Path, req.Content, req.CommitMsg)
	} else if req.IsEntityGraph {
		sha, n, err = w.repo.CommitEntityGraph(writeCtx, req.Slug, req.Content, req.CommitMsg)
	} else if req.IsPlaybookCompile {
		sha, n, err = w.repo.CommitPlaybookSkill(writeCtx, req.Slug, req.Path, req.Content, req.CommitMsg)
	} else if req.IsPlaybookExecution {
		sha, n, err = w.repo.CommitPlaybookExecution(writeCtx, req.Slug, req.Path, req.Content, req.CommitMsg)
	} else if req.IsNotebook {
		// Notebook writes do NOT regen the team wiki index. Commit target is
		// agents/{slug}/notebook/... — scoped to the author.
		sha, n, err = w.repo.CommitNotebook(writeCtx, req.Slug, req.Path, req.Content, req.Mode, req.CommitMsg)
	} else {
		// Wiki Commit owns the full atomic unit: write article bytes, regen
		// the catalog, stage both, commit together. That keeps the working
		// tree clean and the index commit attributable to the same author as
		// the article edit. No post-commit IndexRegen here.
		sha, n, err = w.repo.Commit(writeCtx, req.Slug, req.Path, req.Content, req.Mode, req.CommitMsg)
	}
	if err != nil {
		// On ErrWikiSHAMismatch the human path returns the current HEAD
		// SHA alongside the error so callers can surface 409 bodies
		// without a second round trip. For all other errors `sha` is
		// empty and carrying it is a harmless no-op.
		req.ReplyCh <- wikiWriteResult{SHA: sha, Err: err}
		return
	}
	// Reply is sent at the end of process() so every sideGoroutines.Add(1)
	// from the event fan-out below has landed before the caller unblocks.
	// Tests that call WaitForIdle() right after Enqueue can then see a
	// consistent WaitGroup counter — without this ordering, the backup
	// mirror goroutine could still be spawning while t.TempDir() cleanup
	// races it.
	defer func() {
		req.ReplyCh <- wikiWriteResult{SHA: sha, BytesWritten: n}
	}()

	ts := time.Now().UTC().Format(time.RFC3339)
	switch {
	case req.IsEntityFact:
		// Entity fact writes have their own SSE event (entity:fact_recorded)
		// published by the broker handler, not by the worker. No-op here.
	case req.IsEntityGraph:
		// Graph log appends are internal bookkeeping triggered by fact
		// writes — no dedicated SSE event. Subscribers that care hear
		// entity:fact_recorded + refetch the graph read API.
	case req.IsPlaybookCompile:
		// Compile writes are internal — no SSE event. The trigger (the
		// source-article commit) already published wiki:write; emitting a
		// second event for the compiled skill would double-count in the
		// feed for callers that don't care about the hidden directory.
	case req.IsPlaybookExecution:
		if pbPub, ok := w.publisher.(playbookEventPublisher); ok {
			pbPub.PublishPlaybookExecutionRecorded(PlaybookExecutionRecordedEvent{
				Slug:       req.PlaybookSlug,
				Path:       req.Path,
				CommitSHA:  sha,
				RecordedBy: req.Slug,
				Timestamp:  ts,
			})
		}
	case req.IsNotebook:
		if nbPub, ok := w.publisher.(notebookEventPublisher); ok {
			nbPub.PublishNotebookEvent(notebookWriteEvent{
				Slug:      req.Slug,
				Path:      req.Path,
				CommitSHA: sha,
				Timestamp: ts,
			})
		}
	default:
		w.publisher.PublishWikiEvent(wikiWriteEvent{
			Path:       req.Path,
			CommitSHA:  sha,
			AuthorSlug: req.Slug,
			Timestamp:  ts,
		})
		// Poke the sections cache so it debounces + recomputes. Only
		// fired on team wiki writes — notebook + entity-fact writes
		// never change the sidebar IA.
		if notifier, ok := w.publisher.(wikiSectionsNotifier); ok {
			notifier.EnqueueSectionsRefresh()
		}
		// Auto-recompile trigger: a standard wiki write to team/playbooks/{slug}.md
		// should kick off a compile. We do it in a side goroutine so the
		// current request's drain slot is released before the recompile job
		// tries to enter the queue — the queue is single-reader, so doing
		// it inline would deadlock on a full buffer.
		if slug, ok := PlaybookSlugFromPath(req.Path); ok {
			w.sideGoroutines.Add(1)
			go func(slug, authorSlug string) {
				defer w.sideGoroutines.Done()
				if !w.running.Load() {
					return
				}
				bgCtx, cancel := context.WithTimeout(context.Background(), wikiWriteTimeout*2)
				defer cancel()
				if _, _, err := w.EnqueuePlaybookCompile(bgCtx, slug, authorSlug); err != nil {
					log.Printf("playbook: auto-recompile %s failed: %v", slug, err)
				}
			}(slug, req.Slug)
		}
	}

	w.maybeScheduleBackup(ctx)
	w.maybeReconcileIndex(writeCtx, req.Path)
}

// maybeReconcileIndex updates the derived WikiIndex for the committed path.
// It is a no-op when no index is wired (w.index == nil), so existing tests
// that construct workers without an index are unaffected.
//
// The update runs in a tracked side goroutine so it never blocks the commit
// reply to the caller. On error the failure is logged but does NOT propagate
// — markdown is the source of truth; the index is a rebuildable cache (§7.4).
func (w *WikiWorker) maybeReconcileIndex(ctx context.Context, relPath string) {
	if w.index == nil {
		return
	}
	w.sideGoroutines.Add(1)
	go func() {
		defer w.sideGoroutines.Done()
		bgCtx, cancel := context.WithTimeout(context.Background(), wikiWriteTimeout)
		defer cancel()
		if err := w.index.ReconcilePath(bgCtx, relPath); err != nil {
			log.Printf("wiki_index: reconcile %s failed: %v", relPath, err)
		}
	}()
}

// maybeScheduleBackup kicks off a debounced backup mirror. The copy runs in
// its own goroutine and does NOT block the worker. If another backup is
// already pending within wikiBackupDebounce, this call is a no-op.
func (w *WikiWorker) maybeScheduleBackup(ctx context.Context) {
	w.mu.Lock()
	since := time.Since(w.lastBackupAt)
	w.mu.Unlock()
	if since < wikiBackupDebounce {
		return
	}
	if !w.backupPending.CompareAndSwap(false, true) {
		return
	}
	// Track on sideGoroutines so Stop() waits for the backup copy to finish.
	// Without this, the copy can outlive the test's TempDir and race the
	// filesystem cleanup, producing flaky "directory not empty" errors.
	w.sideGoroutines.Add(1)
	go func() {
		defer w.sideGoroutines.Done()
		defer w.backupPending.Store(false)
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = bgCtx // reserved for future cancellation hooks
		if err := w.repo.BackupMirror(bgCtx); err != nil {
			log.Printf("wiki: backup mirror failed: %v", err)
			return
		}
		w.mu.Lock()
		w.lastBackupAt = time.Now()
		w.mu.Unlock()
	}()
}

// QueueLength returns the current number of pending requests. Useful for
// diagnostics and tests.
func (w *WikiWorker) QueueLength() int {
	return len(w.requests)
}

// WaitForIdle blocks until every detached side goroutine spawned by the
// worker (auto-recompile, backup mirror) has finished. Tests register this
// via t.Cleanup so t.TempDir() RemoveAll does not race in-flight background
// writes into wiki.bak/ — the symptom is "directory not empty" or
// "no such file or directory" cleanup errors on Linux CI.
//
// Safe to call after ctx cancellation: the side-goroutine WaitGroup is
// independent of drain lifecycle.
func (w *WikiWorker) WaitForIdle() {
	w.sideGoroutines.Wait()
}

// Done returns a channel that closes when the drain goroutine has fully
// exited — including its wait on side goroutines. Tests that started the
// worker with a cancellable context should `<-worker.Done()` after the
// cancel so drain's in-flight repo writes settle before t.TempDir()
// removal.
func (w *WikiWorker) Done() <-chan struct{} {
	return w.drainDone
}

// EnqueueHuman submits a human-authored wiki write to the shared single-
// writer queue with the legacy fallback identity. Retained for backward
// compatibility with tests and call sites that predate v1.5's
// per-human identity registry; prefer EnqueueHumanAs for new code.
func (w *WikiWorker) EnqueueHuman(ctx context.Context, path, content, commitMsg, expectedSHA string) (string, int, error) {
	return w.EnqueueHumanAs(ctx, HumanIdentity{}, path, content, commitMsg, expectedSHA)
}

// EnqueueHumanAs submits a human-authored wiki write to the shared
// single-writer queue, stamping the commit with the supplied identity.
// A zero-value identity falls back to the synthetic `human` author so
// single-user installs keep working.
//
// The HTTP handler is already gated by the broker bearer token, so the
// worker trusts the identity it is given — this is belt-and-braces
// between two authenticated layers, not an anti-spoofing boundary.
//
// Returns ErrWikiSHAMismatch wrapped with the current HEAD SHA (in the
// SHA return slot) when expected_sha does not match; callers pass that
// back to the client so the 409 prompt can reload the latest content.
func (w *WikiWorker) EnqueueHumanAs(ctx context.Context, id HumanIdentity, path, content, commitMsg, expectedSHA string) (string, int, error) {
	if !w.running.Load() {
		return "", 0, ErrWorkerStopped
	}
	slug := strings.TrimSpace(id.Slug)
	if slug == "" {
		slug = HumanAuthor
	}
	req := wikiWriteRequest{
		Slug:          slug,
		Path:          path,
		Content:       content,
		Mode:          "replace",
		CommitMsg:     commitMsg,
		IsHuman:       true,
		ExpectedSHA:   expectedSHA,
		HumanIdentity: id,
		ReplyCh:       make(chan wikiWriteResult, 1),
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
		return "", 0, fmt.Errorf("wiki: human write timed out after %s", wikiWriteTimeout)
	}
}

// EnqueueEntityFact submits a fact-log append to the shared wiki queue.
// The path must be team/entities/{kind}-{slug}.facts.jsonl and is routed
// to Repo.CommitEntityFact (which does NOT regen the wiki index).
func (w *WikiWorker) EnqueueEntityFact(ctx context.Context, slug, path, content, commitMsg string) (string, int, error) {
	if !w.running.Load() {
		return "", 0, ErrWorkerStopped
	}
	req := wikiWriteRequest{
		Slug:         slug,
		Path:         path,
		Content:      content,
		Mode:         "replace",
		CommitMsg:    commitMsg,
		IsEntityFact: true,
		ReplyCh:      make(chan wikiWriteResult, 1),
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
		return "", 0, fmt.Errorf("wiki: entity-fact write timed out after %s", wikiWriteTimeout)
	}
}

// EnqueueEntityGraph submits a full rewrite of the cross-entity adjacency
// log at team/entities/.graph.jsonl. Same single-writer queue as the fact
// log; routed to Repo.CommitEntityGraph (which does NOT regen the wiki
// index or backlinks). Caller (EntityGraph) owns append-merge semantics —
// the worker just replaces the bytes.
func (w *WikiWorker) EnqueueEntityGraph(ctx context.Context, slug, content, commitMsg string) (string, int, error) {
	if !w.running.Load() {
		return "", 0, ErrWorkerStopped
	}
	req := wikiWriteRequest{
		Slug:          slug,
		Path:          EntityGraphPath,
		Content:       content,
		Mode:          "replace",
		CommitMsg:     commitMsg,
		IsEntityGraph: true,
		ReplyCh:       make(chan wikiWriteResult, 1),
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
		return "", 0, fmt.Errorf("wiki: entity-graph write timed out after %s", wikiWriteTimeout)
	}
}

// Repo returns the underlying wiki repo — used by read-side broker handlers
// which do not need the serialized write queue.
func (w *WikiWorker) Repo() *Repo {
	return w.repo
}

// EnqueuePlaybookCompile runs CompilePlaybook against the current on-disk
// source and submits the output to the queue as a compiled-skill write.
// The commit is attributed to the archivist identity regardless of who
// authored the source edit — the compilation is a machine artifact.
func (w *WikiWorker) EnqueuePlaybookCompile(ctx context.Context, slug, _ string) (string, int, error) {
	if !w.running.Load() {
		return "", 0, ErrWorkerStopped
	}
	sourcePath := playbookSourceRel(slug)
	relSkill, err := CompilePlaybook(w.repo, sourcePath)
	if err != nil {
		return "", 0, err
	}
	fullSkill := filepath.Join(w.repo.Root(), filepath.FromSlash(relSkill))
	// Read what CompilePlaybook just wrote; the queue submission must carry
	// the full file bytes because CommitPlaybookSkill rewrites the file from
	// content (mirrors entity-fact append).
	skillBytes, rerr := os.ReadFile(fullSkill)
	if rerr != nil {
		return "", 0, fmt.Errorf("playbook: read compiled skill back: %w", rerr)
	}
	req := wikiWriteRequest{
		Slug:              ArchivistAuthor,
		Path:              relSkill,
		Content:           string(skillBytes),
		Mode:              "replace",
		CommitMsg:         fmt.Sprintf("archivist: compile playbook %s", slug),
		IsPlaybookCompile: true,
		PlaybookSlug:      slug,
		ReplyCh:           make(chan wikiWriteResult, 1),
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
		return "", 0, fmt.Errorf("playbook: compile write timed out after %s", wikiWriteTimeout)
	}
}

// EnqueuePlaybookExecution submits an execution-log append to the shared
// queue. Used by ExecutionLog.Append; mirrors EnqueueEntityFact shape.
func (w *WikiWorker) EnqueuePlaybookExecution(ctx context.Context, slug, path, content, commitMsg string) (string, int, error) {
	if !w.running.Load() {
		return "", 0, ErrWorkerStopped
	}
	// Extract the playbook slug from the jsonl path so the SSE event can
	// carry it without a second parse on the subscriber side.
	pbSlug := executionPlaybookSlug(path)
	req := wikiWriteRequest{
		Slug:                slug,
		Path:                path,
		Content:             content,
		Mode:                "replace",
		CommitMsg:           commitMsg,
		IsPlaybookExecution: true,
		PlaybookSlug:        pbSlug,
		ReplyCh:             make(chan wikiWriteResult, 1),
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
		return "", 0, fmt.Errorf("playbook: execution write timed out after %s", wikiWriteTimeout)
	}
}

// playbookSourceRel resolves the source-article path for a slug.
func playbookSourceRel(slug string) string {
	return "team/playbooks/" + slug + ".md"
}

// executionPlaybookSlug extracts the slug from a jsonl log path. Returns
// "" when the shape is wrong — the caller uses that only for the SSE event
// payload, so a blank slug is not load-bearing.
func executionPlaybookSlug(path string) string {
	if !executionLogPathPattern.MatchString(path) {
		return ""
	}
	base := filepath.Base(path)
	const suffix = ".executions.jsonl"
	if strings.HasSuffix(base, suffix) {
		return strings.TrimSuffix(base, suffix)
	}
	return ""
}

// handleWikiWrite is the broker HTTP endpoint the MCP subprocess posts to
// when an agent calls team_wiki_write. Shape:
//
//	POST /wiki/write
//	{ "slug":..., "path":..., "content":..., "mode":..., "commit_message":... }
//
// Response: 200 { "path":..., "commit_sha":..., "bytes_written":... }
//
//	429 { "error":"wiki queue saturated, retry on next turn" }
//	500 { "error":"..." }
//	503 { "error":"..." } when worker is not running
func (b *Broker) handleWikiWrite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Slug          string `json:"slug"`
		Path          string `json:"path"`
		Content       string `json:"content"`
		Mode          string `json:"mode"`
		CommitMessage string `json:"commit_message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	sha, n, err := worker.Enqueue(r.Context(), body.Slug, body.Path, body.Content, body.Mode, body.CommitMessage)
	if err != nil {
		if errors.Is(err, ErrQueueSaturated) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path":          body.Path,
		"commit_sha":    sha,
		"bytes_written": n,
	})
}

// handleWikiRead returns raw article bytes.
//
//	GET /wiki/read?path=team/people/nazz.md
func (b *Broker) handleWikiRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if err := validateArticlePath(relPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	bytes, err := readArticle(worker.Repo(), relPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(bytes)
}

// handleWikiSearch returns literal-substring matches across team/.
//
//	GET /wiki/search?pattern=launch
func (b *Broker) handleWikiSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	pattern := strings.TrimSpace(r.URL.Query().Get("pattern"))
	if pattern == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "pattern is required"})
		return
	}
	hits, err := searchArticles(worker.Repo(), pattern)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
}

// handleWikiList returns the contents of index/all.md.
//
//	GET /wiki/list
func (b *Broker) handleWikiList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	bytes, err := readIndexAll(worker.Repo())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(bytes)
}

// handleWikiCatalog returns the full catalog as structured JSON for the UI.
//
//	GET /wiki/catalog
//
// Response shape matches web/src/api/wiki.ts { articles: WikiCatalogEntry[] }.
// Distinct from /wiki/list (which returns raw markdown from index/all.md) —
// agents read the markdown index, the UI reads this JSON.
func (b *Broker) handleWikiCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	entries, err := worker.Repo().BuildCatalog(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []CatalogEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"articles": entries})
}

// handleWikiArticle returns the rich article metadata for the UI: content +
// title + revisions + contributors + backlinks + word count.
//
//	GET /wiki/article?path=team/people/nazz.md
//
// Response shape matches web/src/api/wiki.ts WikiArticle.
func (b *Broker) handleWikiArticle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if err := validateArticlePath(relPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	meta, err := worker.Repo().BuildArticle(r.Context(), relPath)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// handleWikiAudit returns the cross-article commit log for audit / compliance.
// Unlike /wiki/history/<path> which scopes to one article, this feed covers
// the whole wiki and includes bootstrap + recovery + system commits so the
// lineage is complete.
//
//	GET /wiki/audit
//	GET /wiki/audit?limit=50
//	GET /wiki/audit?since=2026-04-01T00:00:00Z
//
// Response:
//
//	{
//	  "entries": [
//	    {
//	      "sha": "...", "author_slug": "...", "timestamp": "...",
//	      "message": "...", "paths": ["team/..."]
//	    },
//	    ...
//	  ],
//	  "total": N
//	}
func (b *Broker) handleWikiAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	if worker == nil {
		http.Error(w, `{"error":"wiki backend is not active"}`, http.StatusServiceUnavailable)
		return
	}
	// Parse limit (optional, 0 = all). Default cap keeps a runaway caller
	// from dragging in 100k commits; explicit `limit=0` opts out of the cap.
	const defaultLimit = 500
	limit := defaultLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a non-negative integer"})
			return
		}
		limit = v
	}
	var since time.Time
	if raw := strings.TrimSpace(r.URL.Query().Get("since")); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "since must be RFC3339 (e.g. 2026-04-01T00:00:00Z)"})
			return
		}
		since = t
	}
	entries, err := worker.Repo().AuditLog(r.Context(), since, limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Re-shape to snake_case for the JSON API — same convention as
	// /wiki/catalog and /wiki/article. `paths` never serialised as null:
	// absent paths (rare, but possible for a signed-only commit) get an
	// empty array so consumers don't have to null-guard.
	type wireEntry struct {
		SHA        string   `json:"sha"`
		AuthorSlug string   `json:"author_slug"`
		Timestamp  string   `json:"timestamp"`
		Message    string   `json:"message"`
		Paths      []string `json:"paths"`
	}
	wire := make([]wireEntry, 0, len(entries))
	for _, e := range entries {
		paths := e.Paths
		if paths == nil {
			paths = []string{}
		}
		wire = append(wire, wireEntry{
			SHA:        e.SHA,
			AuthorSlug: e.Author,
			Timestamp:  e.Timestamp.UTC().Format(time.RFC3339),
			Message:    e.Message,
			Paths:      paths,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": wire,
		"total":   len(wire),
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
