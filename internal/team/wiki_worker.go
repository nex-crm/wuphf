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
	ReplyCh   chan wikiWriteResult
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

// noopPublisher is used when the worker runs without a broker attached
// (tests, or --memory-backend markdown without a broker yet).
type noopPublisher struct{}

func (noopPublisher) PublishWikiEvent(wikiWriteEvent) {}

// WikiWorker owns the single goroutine that drains the write request queue.
type WikiWorker struct {
	repo      *Repo
	publisher wikiEventPublisher
	requests  chan wikiWriteRequest

	running       atomic.Bool
	mu            sync.Mutex // guards lastBackupAt
	lastBackupAt  time.Time
	backupPending atomic.Bool
}

// NewWikiWorker returns a worker ready to Start. The publisher is optional;
// when nil, events are dropped silently.
func NewWikiWorker(repo *Repo, publisher wikiEventPublisher) *WikiWorker {
	if publisher == nil {
		publisher = noopPublisher{}
	}
	return &WikiWorker{
		repo:      repo,
		publisher: publisher,
		requests:  make(chan wikiWriteRequest, wikiRequestBuffer),
	}
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
func (w *WikiWorker) Stop() {
	if !w.running.Swap(false) {
		return
	}
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
	defer w.running.Store(false)
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

	sha, n, err := w.repo.Commit(writeCtx, req.Slug, req.Path, req.Content, req.Mode, req.CommitMsg)
	if err != nil {
		req.ReplyCh <- wikiWriteResult{Err: err}
		return
	}
	// Regenerate the catalog. A regen failure is non-fatal for the caller —
	// the article commit already landed. Log it loudly so the next run can
	// repair things if needed. TODO(telemetry): emit regen duration and
	// warn when p95 > 300ms once the observability hook exists.
	if err := w.repo.IndexRegen(writeCtx); err != nil {
		log.Printf("wiki: index regen failed after commit %s: %v", sha, err)
	}
	req.ReplyCh <- wikiWriteResult{SHA: sha, BytesWritten: n}

	evt := wikiWriteEvent{
		Path:       req.Path,
		CommitSHA:  sha,
		AuthorSlug: req.Slug,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
	}
	w.publisher.PublishWikiEvent(evt)

	w.maybeScheduleBackup(ctx)
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
	go func() {
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

// Repo returns the underlying wiki repo — used by read-side broker handlers
// which do not need the serialized write queue.
func (w *WikiWorker) Repo() *Repo {
	return w.repo
}

// handleWikiWrite is the broker HTTP endpoint the MCP subprocess posts to
// when an agent calls team_wiki_write. Shape:
//
//	POST /wiki/write
//	{ "slug":..., "path":..., "content":..., "mode":..., "commit_message":... }
//
// Response: 200 { "path":..., "commit_sha":..., "bytes_written":... }
//           429 { "error":"wiki queue saturated, retry on next turn" }
//           500 { "error":"..." }
//           503 { "error":"..." } when worker is not running
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

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
