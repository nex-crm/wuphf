package team

// pam.go is the broker-level dispatcher for Pam the Archivist.
//
// Pam is the archivist — same git identity (ArchivistAuthor), same
// single-writer commit path through WikiWorker. What's new: Pam now runs in
// her own sub-process per task, mirroring how roster agents are spawned.
//
// The current sub-process mode is headless: each Pam turn shells out via
// provider.RunConfiguredOneShot, a fresh process per task. Context is
// inherently clean per invocation, so no /clear is needed.
//
// Callers supply a PamRunner to NewPamDispatcher. The broker wires a
// HeadlessPamRunner by default.
//
// Pam is NOT a roster member (not in any PackDefinition.Agents[]). She sits
// on top of the wiki UI and is triggered explicitly by the user.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// PamSlug is the identity used for Pam's sub-process dispatch. Kept distinct
// from ArchivistAuthor so the git commit author stays "archivist" while the
// runtime routing can reference "pam" (e.g. tmux window names, log files).
const PamSlug = "pam"

// DefaultPamTimeout bounds a single Pam sub-process. Web enrichment can
// legitimately take longer than an entity-brief synthesis (the LLM fans out
// to WebSearch + WebFetch), so this is wider than the synthesizer default.
const DefaultPamTimeout = 90 * time.Second

// MaxPamQueue is the buffered channel size for pending Pam jobs.
const MaxPamQueue = 16

// MaxPamOutputSize caps a single run at 128 KiB to bound blast radius if the
// LLM produces runaway output and to keep git objects small.
const MaxPamOutputSize = 128 * 1024

// ErrPamQueueSaturated is returned by Enqueue when the buffered channel is
// full. Callers surface as 429.
var ErrPamQueueSaturated = errors.New("pam: queue saturated")

// ErrPamStopped is returned when Enqueue is called after Stop.
var ErrPamStopped = errors.New("pam: not running")

// ErrPamArticleMissing is returned when the target article does not exist.
var ErrPamArticleMissing = errors.New("pam: target article does not exist")

// PamJob is one pending action for a specific article.
type PamJob struct {
	Action      PamActionID
	ArticlePath string
	RequestBy   string
	EnqueuedAt  time.Time
	ID          uint64
}

// PamActionStartedEvent is broadcast when Pam begins a job.
type PamActionStartedEvent struct {
	JobID       uint64 `json:"job_id"`
	Action      string `json:"action"`
	ArticlePath string `json:"article_path"`
	RequestBy   string `json:"request_by"`
	StartedAt   string `json:"started_at"`
}

// PamActionDoneEvent is broadcast when Pam commits the result of a job.
type PamActionDoneEvent struct {
	JobID       uint64 `json:"job_id"`
	Action      string `json:"action"`
	ArticlePath string `json:"article_path"`
	CommitSHA   string `json:"commit_sha"`
	FinishedAt  string `json:"finished_at"`
}

// PamActionFailedEvent is broadcast when Pam could not finish a job. The
// Error field is the short reason for the UI; details go to the server log.
type PamActionFailedEvent struct {
	JobID       uint64 `json:"job_id"`
	Action      string `json:"action"`
	ArticlePath string `json:"article_path"`
	Error       string `json:"error"`
	FailedAt    string `json:"failed_at"`
}

// pamEventPublisher is the subset of Broker the dispatcher needs.
type pamEventPublisher interface {
	PublishPamActionStarted(evt PamActionStartedEvent)
	PublishPamActionDone(evt PamActionDoneEvent)
	PublishPamActionFailed(evt PamActionFailedEvent)
}

// PamRunner runs a single Pam turn as a sub-process. Implementations decide
// how to execute that sub-process (e.g. headless one-shot via the configured
// provider CLI).
type PamRunner interface {
	Run(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// HeadlessPamRunner is the default: one fresh CLI process per Pam turn.
// Context is clean by construction, so no /clear is needed.
type HeadlessPamRunner struct{}

// Run shells out via provider.RunConfiguredOneShot. The cwd argument is
// intentionally empty — Pam operates on the wiki via the broker API, not on
// the caller's working directory.
//
// Cancellation caveat: provider.RunConfiguredOneShot does not accept a
// context, so we cannot tear down the spawned child when ctx is cancelled.
// We run the provider call in a goroutine and select on ctx.Done() so the
// dispatcher can unblock on deadline, but the child process may outlive the
// cancel until the provider call returns. A future provider-package change
// should plumb context through so cancel actually kills the child.
func (HeadlessPamRunner) Run(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	type result struct {
		out string
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		out, err := provider.RunConfiguredOneShot(systemPrompt, userPrompt, "")
		resCh <- result{out: out, err: err}
	}()
	select {
	case <-ctx.Done():
		log.Printf("pam: cancel signalled; child process may outlive ctx until provider call completes")
		return "", ctx.Err()
	case r := <-resCh:
		return r.out, r.err
	}
}

// PamDispatcherConfig tunes the dispatcher. Zero values -> defaults.
type PamDispatcherConfig struct {
	Timeout time.Duration
	Runner  PamRunner
}

// pamWiki is the slice of WikiWorker that Pam actually depends on:
// commit a replacement article body via Enqueue, and read the existing
// article body via Repo (which reads through to the on-disk markdown).
//
// Stating it as an interface here is the dependency-inversion seam for
// the planned `internal/pam` extraction (Track B): once pam.go lives
// in its own package, this interface stays with pam (the consumer) and
// `*team.WikiWorker` continues to satisfy it via duck typing — no
// import-cycle, no shared "core" package needed.
type pamWiki interface {
	Enqueue(ctx context.Context, slug, path, content, mode, commitMsg string) (string, int, error)
	Repo() *Repo
}

// PamDispatcher serializes Pam's work. Like the entity + playbook
// synthesizers, only one job runs at a time — otherwise two archivist
// commits could race on the WikiWorker queue.
type PamDispatcher struct {
	worker    pamWiki
	publisher pamEventPublisher
	cfg       PamDispatcherConfig

	mu sync.Mutex
	// jobs is the buffered channel the drain goroutine pulls from.
	jobs chan PamJob
	// inflight maps a coalesce key to the id of the currently-running job.
	// A zero value means "not present"; zero ids are never stored.
	inflight map[string]uint64
	// queued maps a coalesce key to the id of the pending (enqueued or
	// coalesced follow-up) job for that key. Same zero semantics as inflight.
	queued  map[string]uint64
	running bool
	nextID  uint64
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewPamDispatcher wires a dispatcher against the given worker. The publisher
// may be nil in tests that don't care about SSE fan-out. Worker is typed as
// the narrow `pamWiki` interface — *WikiWorker satisfies it today, and any
// future test seam or alternative backend only needs Enqueue + Repo.
func NewPamDispatcher(worker pamWiki, publisher pamEventPublisher, cfg PamDispatcherConfig) *PamDispatcher {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultPamTimeout
	}
	if cfg.Runner == nil {
		cfg.Runner = HeadlessPamRunner{}
	}
	return &PamDispatcher{
		worker:    worker,
		publisher: publisher,
		cfg:       cfg,
		jobs:      make(chan PamJob, MaxPamQueue),
		inflight:  make(map[string]uint64),
		queued:    make(map[string]uint64),
	}
}

// Start launches the drain goroutine. Idempotent.
func (d *PamDispatcher) Start(ctx context.Context) {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return
	}
	d.running = true
	d.stopCh = make(chan struct{})
	d.mu.Unlock()

	d.wg.Add(1)
	go d.drain(ctx)
}

// Stop signals the worker to exit. Pending jobs are dropped.
func (d *PamDispatcher) Stop() {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	d.running = false
	close(d.stopCh)
	d.mu.Unlock()
	d.wg.Wait()
}

func pamJobKey(action PamActionID, path string) string {
	return string(action) + "|" + path
}

// Enqueue submits a Pam job. Coalesces per (action, path): repeated clicks
// while a job is running fold into at most one follow-up. On a coalesce hit
// the existing job's id is returned — zero is reserved for errors.
func (d *PamDispatcher) Enqueue(action PamActionID, articlePath, requestBy string) (uint64, error) {
	articlePath = strings.TrimSpace(articlePath)
	if articlePath == "" {
		return 0, fmt.Errorf("pam: empty article path")
	}
	if _, err := LookupPamAction(action); err != nil {
		return 0, err
	}
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return 0, ErrPamStopped
	}
	key := pamJobKey(action, articlePath)
	// If there's already a follow-up queued for this key, fold into it.
	if existingID := d.queued[key]; existingID != 0 {
		d.mu.Unlock()
		return existingID, nil
	}
	// If a job is currently running for this key, mark a follow-up queued
	// against the running job's id so the caller gets a non-zero handle.
	if inflightID := d.inflight[key]; inflightID != 0 {
		d.queued[key] = inflightID
		d.mu.Unlock()
		return inflightID, nil
	}
	d.nextID++
	id := d.nextID
	job := PamJob{
		Action:      action,
		ArticlePath: articlePath,
		RequestBy:   strings.TrimSpace(requestBy),
		EnqueuedAt:  time.Now().UTC(),
		ID:          id,
	}
	// Hold the mutex across the non-blocking send so a concurrent Enqueue
	// can't observe queued[key]==id before the job is actually in the
	// channel (TOCTOU: two callers would both "succeed" but only one job
	// would land).
	select {
	case d.jobs <- job:
		d.queued[key] = id
		d.mu.Unlock()
		return id, nil
	default:
		d.mu.Unlock()
		return 0, ErrPamQueueSaturated
	}
}

func (d *PamDispatcher) drain(ctx context.Context) {
	defer d.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case job := <-d.jobs:
			d.runJob(ctx, job)
		}
	}
}

func (d *PamDispatcher) runJob(ctx context.Context, job PamJob) {
	key := pamJobKey(job.Action, job.ArticlePath)

	d.mu.Lock()
	d.inflight[key] = job.ID
	delete(d.queued, key)
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.inflight, key)
		needsFollowup := d.queued[key] != 0
		delete(d.queued, key)
		running := d.running
		d.mu.Unlock()
		if needsFollowup && running {
			// Preserve the original requestor on the follow-up so the audit
			// trail still reflects the human (or agent) who triggered Pam.
			requestor := job.RequestBy
			d.wg.Add(1)
			go func() {
				defer d.wg.Done()
				// Small yield so the drain goroutine is back in its select
				// before the follow-up job arrives, avoiding a busy
				// re-enqueue race.
				select {
				case <-time.After(10 * time.Millisecond):
				case <-d.stopCh:
					return
				}
				if _, err := d.Enqueue(job.Action, job.ArticlePath, requestor); err != nil {
					if errors.Is(err, ErrPamQueueSaturated) {
						log.Printf("pam: follow-up re-enqueue dropped, queue saturated: action=%s path=%s", job.Action, job.ArticlePath)
					} else if !errors.Is(err, ErrPamStopped) {
						log.Printf("pam: follow-up re-enqueue failed: %v", err)
					}
				}
			}()
		}
	}()

	if err := d.execute(ctx, job); err != nil {
		log.Printf("pam: %s on %s failed: %v", job.Action, job.ArticlePath, err)
		if d.publisher != nil {
			d.publisher.PublishPamActionFailed(PamActionFailedEvent{
				JobID:       job.ID,
				Action:      string(job.Action),
				ArticlePath: job.ArticlePath,
				Error:       err.Error(),
				FailedAt:    time.Now().UTC().Format(time.RFC3339),
			})
		}
	}
}

// execute is the per-job pipeline: read article → LLM → commit → publish.
func (d *PamDispatcher) execute(ctx context.Context, job PamJob) error {
	action, err := LookupPamAction(job.Action)
	if err != nil {
		return err
	}

	repo := d.worker.Repo()
	articleBytes, err := readArticle(repo, job.ArticlePath)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrPamArticleMissing, job.ArticlePath)
	}
	existing := string(articleBytes)

	if d.publisher != nil {
		d.publisher.PublishPamActionStarted(PamActionStartedEvent{
			JobID:       job.ID,
			Action:      string(job.Action),
			ArticlePath: job.ArticlePath,
			RequestBy:   job.RequestBy,
			StartedAt:   time.Now().UTC().Format(time.RFC3339),
		})
	}

	callCtx, cancel := context.WithTimeout(ctx, d.cfg.Timeout)
	defer cancel()

	output, runErr := d.cfg.Runner.Run(callCtx, action.SystemPrompt, action.renderUserPrompt(existing))
	if runErr != nil {
		return fmt.Errorf("runner: %w", runErr)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return fmt.Errorf("runner output is empty")
	}
	if len(output) > MaxPamOutputSize {
		return fmt.Errorf("runner output exceeds %d bytes (got %d)", MaxPamOutputSize, len(output))
	}

	commitMsg := action.renderCommitMsg(job.ArticlePath)
	sha, _, werr := d.worker.Enqueue(ctx, ArchivistAuthor, job.ArticlePath, output, "replace", commitMsg)
	if werr != nil {
		return fmt.Errorf("commit: %w", werr)
	}

	if d.publisher != nil {
		d.publisher.PublishPamActionDone(PamActionDoneEvent{
			JobID:       job.ID,
			Action:      string(job.Action),
			ArticlePath: job.ArticlePath,
			CommitSHA:   sha,
			FinishedAt:  time.Now().UTC().Format(time.RFC3339),
		})
	}
	return nil
}
