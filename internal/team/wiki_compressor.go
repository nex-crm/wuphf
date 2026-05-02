package team

// wiki_compressor.go is the broker-level LLM compression worker for PR 4 of
// the wiki content lifecycle.
//
// Design summary (see docs/specs/wiki-compress-icp-examples.md):
//   - Compression is NOT an agent turn. It runs inside the broker as a
//     dedicated goroutine consuming a buffered CompressJob channel — the
//     same shape as EntitySynthesizer.
//   - The worker shells out to the user's configured CLI (claude-code,
//     codex, openclaw, ...) via the same defaultLLMCall used by the
//     synthesizer, so we never carry an LLM SDK in the broker binary.
//   - Output is committed via the WikiWorker queue under the synthetic
//     `archivist` git identity, preserving the single-writer invariant.
//   - The worker coalesces duplicate compress requests per-article: a
//     second click while a job is in-flight returns (queued=false) and
//     IsInflight reports true. We do NOT auto-queue a follow-up — repeated
//     compress on the same article is almost always a misclick, and the
//     LLM run that just landed is already the freshest result.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CompressPromptSystem is the locked system prompt for compression.
// Wording is part of the spec — do not edit without updating the ICP doc.
const CompressPromptSystem = `You are editing a team wiki article to reduce its length while preserving all facts. Remove redundancy, tighten prose, and eliminate filler. Do not remove named facts, dates, contacts, or decisions. Target 40–60% of the original word count. Output ONLY the compressed markdown, no explanation.`

// DefaultCompressTimeout bounds a single LLM shell-out for compression.
const DefaultCompressTimeout = 30 * time.Second

// MaxCompressQueue is the buffered channel size for pending compress jobs.
const MaxCompressQueue = 32

// ErrCompressQueueSaturated is returned by EnqueueCompress when the
// buffered channel is full.
var ErrCompressQueueSaturated = errors.New("wiki compress: queue saturated")

// ErrCompressorStopped is returned when EnqueueCompress is called after
// the worker has been stopped.
var ErrCompressorStopped = errors.New("wiki compress: not running")

// CompressorConfig is the tunable knobs. Defaults match the constants above.
type CompressorConfig struct {
	Timeout time.Duration

	// LLMCall is the pluggable shell-out used by tests. Production code
	// leaves this nil and the worker falls back to defaultLLMCall.
	LLMCall func(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// CompressJob is one pending compression request for a specific article.
type CompressJob struct {
	RelPath    string
	RequestBy  string
	EnqueuedAt time.Time
}

// WikiCompressor is the broker-level compression worker.
type WikiCompressor struct {
	worker *WikiWorker
	cfg    CompressorConfig

	mu       sync.Mutex
	jobs     chan CompressJob
	inflight map[string]bool // key=relPath
	queued   map[string]bool // key=relPath
	running  bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewWikiCompressor wires a compressor against the given wiki worker.
// Config may be the zero value; defaults are filled in here.
func NewWikiCompressor(worker *WikiWorker, cfg CompressorConfig) *WikiCompressor {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultCompressTimeout
	}
	return &WikiCompressor{
		worker:   worker,
		cfg:      cfg,
		jobs:     make(chan CompressJob, MaxCompressQueue),
		inflight: make(map[string]bool),
		queued:   make(map[string]bool),
	}
}

// Start launches the compress loop. Returns immediately. Stop via Stop().
func (c *WikiCompressor) Start(ctx context.Context) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.stopCh = make(chan struct{})
	c.mu.Unlock()

	c.wg.Add(1)
	go c.drain(ctx)
}

// Stop signals the worker to exit. Pending jobs in the buffered channel are
// discarded — caller is responsible for only calling this at shutdown.
func (c *WikiCompressor) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	close(c.stopCh)
	c.mu.Unlock()
	c.wg.Wait()
}

// IsInflight reports whether a compress job is currently running or queued
// for the given article path.
func (c *WikiCompressor) IsInflight(relPath string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inflight[relPath] || c.queued[relPath]
}

// EnqueueCompress adds a compression job if none is already in-flight or
// queued for the same article. Returns (queued, inFlight bool, err).
//   - queued=true, inFlight=false: fresh job scheduled.
//   - queued=false, inFlight=true: job already running/queued — debounced.
//   - queued=false, inFlight=false, err!=nil: queue saturated or compressor stopped.
//
// queued and inFlight are captured atomically under the lock so callers do
// not need a separate IsInflight call that could race with job completion.
func (c *WikiCompressor) EnqueueCompress(relPath, requestBy string) (queued, inFlight bool, err error) {
	if validateErr := validateArticlePath(relPath); validateErr != nil {
		return false, false, validateErr
	}
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return false, false, ErrCompressorStopped
	}
	if c.inflight[relPath] || c.queued[relPath] {
		c.mu.Unlock()
		return false, true, nil
	}
	c.queued[relPath] = true
	c.mu.Unlock()

	job := CompressJob{
		RelPath:    relPath,
		RequestBy:  strings.TrimSpace(requestBy),
		EnqueuedAt: time.Now().UTC(),
	}
	select {
	case c.jobs <- job:
		return true, false, nil
	default:
		// Queue saturated — undo the reservation so future calls can retry.
		c.mu.Lock()
		delete(c.queued, relPath)
		c.mu.Unlock()
		return false, false, ErrCompressQueueSaturated
	}
}

// drain is the single compression worker goroutine. Runs exactly one job at
// a time so the WikiWorker queue never has two archivist writes racing.
func (c *WikiCompressor) drain(ctx context.Context) {
	defer c.wg.Done()
	for {
		// Priority check: stop signals must be handled before draining jobs
		// so that a full c.jobs buffer cannot delay shutdown indefinitely.
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		default:
		}
		select {
		case <-ctx.Done():
			return
		case <-c.stopCh:
			return
		case job := <-c.jobs:
			c.runJob(ctx, job)
		}
	}
}

// runJob marks the article as inflight, calls compress(), and clears state.
func (c *WikiCompressor) runJob(ctx context.Context, job CompressJob) {
	c.mu.Lock()
	c.inflight[job.RelPath] = true
	delete(c.queued, job.RelPath)
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.inflight, job.RelPath)
		c.mu.Unlock()
	}()

	if err := c.compress(ctx, job); err != nil {
		log.Printf("wiki compress: %s failed: %v", job.RelPath, err)
	}
}

// compress runs the full pipeline for one job: read article, strip
// frontmatter, call LLM, validate, re-apply frontmatter, commit.
func (c *WikiCompressor) compress(ctx context.Context, job CompressJob) error {
	repo := c.worker.Repo()
	rawBytes, err := readArticle(repo, job.RelPath)
	if err != nil {
		return fmt.Errorf("read article: %w", err)
	}
	raw := string(rawBytes)
	frontmatter := extractFrontmatterBlock(raw)
	body := strings.TrimSpace(stripFrontmatter(raw))
	if body == "" {
		return fmt.Errorf("article body is empty after stripping frontmatter")
	}

	// Build prompt. Body is derived from artifact content, so escape it.
	userPrompt := fmt.Sprintf(
		"# Article to compress\n\n%s\n\n# Your task\nCompress this article now.",
		EscapeForPromptBody(body),
	)

	callCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	llm := c.cfg.LLMCall
	if llm == nil {
		llm = defaultLLMCall
	}
	output, llmErr := llm(callCtx, CompressPromptSystem, userPrompt)
	if llmErr != nil {
		return fmt.Errorf("llm: %w", llmErr)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Errorf("llm output is empty")
	}
	if len(output) > MaxBriefSize {
		return fmt.Errorf("llm output exceeds %d bytes (got %d)", MaxBriefSize, len(output))
	}
	// Weak prompt-echo check: drop the result rather than commit garbage.
	if strings.Contains(output, "# Article to compress") && strings.Contains(output, "# Your task") {
		return fmt.Errorf("llm output appears to contain the prompt verbatim")
	}

	// Strip any frontmatter the LLM may have echoed back, then re-apply
	// the original frontmatter block verbatim. Compression must never
	// silently mutate frontmatter keys (synthesis stamps, ghost flags,
	// promoted_* keys, etc.).
	compressed := strings.TrimSpace(stripFrontmatter(output))
	if compressed == "" {
		return fmt.Errorf("model returned empty body after stripping frontmatter — refusing to overwrite article")
	}
	if countWords([]byte(compressed)) >= countWords([]byte(body)) {
		return fmt.Errorf("compression did not reduce article length — refusing to overwrite article")
	}

	// Re-read the article to detect concurrent edits that occurred during the
	// LLM call. If the content changed, abort to avoid clobbering the newer
	// version (read-LLM-write TOCTOU guard).
	currentBytes, rerr := readArticle(repo, job.RelPath)
	if rerr != nil {
		return fmt.Errorf("re-read after llm: %w", rerr)
	}
	if !bytes.Equal(currentBytes, rawBytes) {
		return fmt.Errorf("article was modified during compression — skipping to avoid overwrite")
	}

	var newBody string
	if frontmatter != "" {
		newBody = frontmatter + compressed + "\n"
	} else {
		newBody = compressed + "\n"
	}

	commitMsg := fmt.Sprintf("archivist: compress %s", job.RelPath)
	if _, _, werr := c.worker.Enqueue(ctx, ArchivistAuthor, job.RelPath, newBody, "replace", commitMsg); werr != nil {
		return fmt.Errorf("commit: %w", werr)
	}
	return nil
}

// extractFrontmatterBlock returns the leading YAML frontmatter block of body
// terminated by `---\n` (matching the shape stripFrontmatter consumes), or
// "" when no frontmatter is present. The returned string ends with `---\n\n`
// so it can be cleanly concatenated with a compressed body.
func extractFrontmatterBlock(body string) string {
	if !strings.HasPrefix(body, "---\n") {
		return ""
	}
	rest := body[len("---\n"):]
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		return ""
	}
	end := len("---\n") + idx + len("\n---\n")
	return body[:end] + "\n"
}

// WikiCompressor returns the active compressor or nil.
func (b *Broker) WikiCompressor() *WikiCompressor {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wikiCompressor
}

// SetWikiCompressor wires a compressor from tests. Must be called after the
// wiki worker is attached (the compressor depends on the worker's queue).
func (b *Broker) SetWikiCompressor(c *WikiCompressor) {
	b.mu.Lock()
	b.wikiCompressor = c
	b.mu.Unlock()
}

// ensureWikiCompressor initializes the compressor when the wiki worker is
// online. Idempotent.
func (b *Broker) ensureWikiCompressor() {
	b.mu.Lock()
	if b.wikiCompressor != nil {
		b.mu.Unlock()
		return
	}
	worker := b.wikiWorker
	b.mu.Unlock()
	if worker == nil {
		return
	}
	c := NewWikiCompressor(worker, CompressorConfig{})
	c.Start(context.Background())
	b.mu.Lock()
	b.wikiCompressor = c
	b.mu.Unlock()
}

// handleWikiCompress is POST /wiki/compress?path=<relPath>.
//
// Response body: {queued: bool, in_flight: bool, path: string}.
//
//   - 405 on non-POST (browsers fire GET on accidental nav).
//   - 400 on missing/invalid path.
//   - 503 if the wiki backend or compressor is not active.
//   - 429 if the compress queue is saturated.
func (b *Broker) handleWikiCompress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	worker := b.WikiWorker()
	compressor := b.WikiCompressor()
	if worker == nil || compressor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wiki backend is not active"})
		return
	}
	relPath := strings.TrimSpace(r.URL.Query().Get("path"))
	if err := validateArticlePath(relPath); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	requestBy := strings.TrimSpace(r.Header.Get("X-Wuphf-Agent"))
	queued, inFlight, err := compressor.EnqueueCompress(relPath, requestBy)
	if err != nil {
		if errors.Is(err, ErrCompressQueueSaturated) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, ErrCompressorStopped) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	resp := map[string]any{
		"queued":    queued,
		"in_flight": inFlight,
		"path":      relPath,
	}
	writeJSON(w, http.StatusOK, resp)
}
