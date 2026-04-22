package team

// pam.go is the broker-level dispatcher for Pam the Archivist.
//
// Pam is the archivist — same git identity (ArchivistAuthor), same
// single-writer commit path through WikiWorker. What's new: Pam now runs in
// her own sub-process per task, mirroring how roster agents are spawned.
// Two sub-process modes are supported:
//
//   1. Headless (default):  shells out to the user's configured CLI via
//      provider.RunConfiguredOneShot. Each invocation is a fresh process —
//      context is inherently clean per task, so no /clear is needed.
//
//   2. Tmux pane (opt-in):  routes the prompt to a persistent tmux pane that
//      belongs to Pam. The launcher's existing sendNotificationToPane helper
//      sends "/clear" + Enter before every notification, satisfying the
//      clean-context requirement for long-lived panes.
//
// Callers pick a mode by supplying a PamRunner to NewPamDispatcher. The
// broker wires a HeadlessPamRunner by default; a pane-backed runner is
// layered in when the launcher is present and paneBackedAgents is true.
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

// MaxPamOutputSize caps the output we're willing to commit from a Pam run.
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
// whether that sub-process is headless (provider.RunConfiguredOneShot) or
// attached to a long-lived tmux pane (launcher.sendNotificationToPane).
type PamRunner interface {
	Run(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// HeadlessPamRunner is the default: one fresh CLI process per Pam turn.
// Context is clean by construction, so no /clear is needed.
type HeadlessPamRunner struct{}

// Run shells out via provider.RunConfiguredOneShot. The cwd argument is
// intentionally empty — Pam operates on the wiki via the broker API, not on
// the caller's working directory.
func (HeadlessPamRunner) Run(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	// RunConfiguredOneShot doesn't take a context; synthesize's timeout wraps
	// this call via context.WithTimeout and the OS tears down the child on
	// deadline.
	_ = ctx
	return provider.RunConfiguredOneShot(systemPrompt, userPrompt, "")
}

// PamDispatcherConfig tunes the dispatcher. Zero values -> defaults.
type PamDispatcherConfig struct {
	Timeout time.Duration
	Runner  PamRunner
}

// PamDispatcher serializes Pam's work. Like the entity + playbook
// synthesizers, only one job runs at a time — otherwise two archivist
// commits could race on the WikiWorker queue.
type PamDispatcher struct {
	worker    *WikiWorker
	publisher pamEventPublisher
	cfg       PamDispatcherConfig

	mu       sync.Mutex
	jobs     chan PamJob
	inflight map[string]bool // key = action|path
	queued   map[string]bool
	running  bool
	nextID   uint64
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewPamDispatcher wires a dispatcher against the given worker. The publisher
// may be nil in tests that don't care about SSE fan-out.
func NewPamDispatcher(worker *WikiWorker, publisher pamEventPublisher, cfg PamDispatcherConfig) *PamDispatcher {
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
		inflight:  make(map[string]bool),
		queued:    make(map[string]bool),
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

// SetRunner swaps the sub-process runner at runtime. Useful when the launcher
// finishes spawning panes and wants to promote Pam to a pane-backed runner.
func (d *PamDispatcher) SetRunner(r PamRunner) {
	if r == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cfg.Runner = r
}

func pamJobKey(action PamActionID, path string) string {
	return string(action) + "|" + path
}

// Enqueue submits a Pam job. Coalesces per (action, path): repeated clicks
// while a job is running fold into at most one follow-up.
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
	if d.queued[key] {
		d.mu.Unlock()
		return 0, nil
	}
	if d.inflight[key] {
		d.queued[key] = true
		d.mu.Unlock()
		return 0, nil
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
	d.queued[key] = true
	d.mu.Unlock()

	select {
	case d.jobs <- job:
		return id, nil
	default:
		d.mu.Lock()
		delete(d.queued, key)
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
	d.inflight[key] = true
	delete(d.queued, key)
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		delete(d.inflight, key)
		needsFollowup := d.queued[key]
		delete(d.queued, key)
		running := d.running
		d.mu.Unlock()
		if needsFollowup && running {
			d.wg.Add(1)
			go func() {
				defer d.wg.Done()
				select {
				case <-time.After(10 * time.Millisecond):
				case <-d.stopCh:
					return
				}
				_, _ = d.Enqueue(job.Action, job.ArticlePath, ArchivistAuthor)
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
	bytes, err := readArticle(repo, job.ArticlePath)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrPamArticleMissing, job.ArticlePath)
	}
	existing := string(bytes)

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
