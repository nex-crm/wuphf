package team

// source_capture.go is the S2 "source capture" layer: it snapshots office
// activity into the immutable source records the Karpathy-style wiki compiles
// FROM (see wiki_source.go). Capture hooks fire on the broker's hot path —
// often while the broker mutex b.mu is held — so they MUST be non-blocking.
//
// THE DEADLOCK GUARD
// ==================
//
// WikiWorker.EnqueueSource performs a blocking git commit. Calling it while
// b.mu is held would freeze the whole broker. So every capture is routed
// through SourceCaptureDispatcher.Enqueue, a NON-BLOCKING buffered channel
// send. A single drain goroutine — which never touches b.mu — owns the
// blocking commit. Records are assembled at the hook site (where the domain
// data is in scope), not on the drain.
//
// De-dup is free: EnqueueSource is write-once by id at the repo layer, so
// re-capturing identical activity is a clean no-op.

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// SourceCaptureQueue is the buffered job-channel capacity. Generous on purpose:
// each job is a tiny pre-built payload and the drain is fast (one idempotent
// git commit per job). On saturation the dispatcher drops + logs rather than
// block — the load-bearing rule is that a hook firing under b.mu never blocks.
const SourceCaptureQueue = 128

// SourceCaptureJob is a pre-built source payload. Assembly happens at the hook
// (CapturedAt, Content, etc. are all resolved there); the drain only validates
// and commits. A zero ID is derived by the drain via DeriveSourceID.
type SourceCaptureJob struct {
	Kind       SourceKind
	ID         string
	Title      string
	Origin     string
	Content    string
	CapturedAt time.Time
}

// sourceCaptureWorker is the narrow slice of WikiWorker the dispatcher needs:
// the single write-once source commit path. *WikiWorker satisfies it.
type sourceCaptureWorker interface {
	EnqueueSource(ctx context.Context, rec SourceRecord) (string, int, error)
}

// SourceCaptureDispatcher owns the off-lock drain that turns capture jobs into
// immutable source records. Construct one per broker, Start it once the wiki
// worker is live, and Stop it on shutdown.
type SourceCaptureDispatcher struct {
	worker sourceCaptureWorker
	jobs   chan SourceCaptureJob

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewSourceCaptureDispatcher wires a dispatcher against the given worker.
func NewSourceCaptureDispatcher(worker sourceCaptureWorker) *SourceCaptureDispatcher {
	return &SourceCaptureDispatcher{
		worker: worker,
		jobs:   make(chan SourceCaptureJob, SourceCaptureQueue),
	}
}

// Start launches the single drain goroutine. Idempotent.
func (d *SourceCaptureDispatcher) Start(ctx context.Context) {
	if d == nil {
		return
	}
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

// Stop signals the drain goroutine to exit and waits up to timeout for it.
// Pending jobs are dropped — captures are idempotent and re-fire on the next
// occurrence of the same activity. A non-positive timeout waits indefinitely.
func (d *SourceCaptureDispatcher) Stop(timeout time.Duration) {
	if d == nil {
		return
	}
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}
	d.running = false
	close(d.stopCh)
	d.mu.Unlock()

	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	if timeout <= 0 {
		<-done
		return
	}
	select {
	case <-done:
	case <-time.After(timeout):
		log.Printf("source_capture: drain did not stop within %s", timeout)
	}
}

// Enqueue submits a pre-built job with a NON-BLOCKING send. Returns true when
// the job was accepted, false on saturation (drop + log) or when the dispatcher
// is not running. It MUST never block — callers fire it while holding b.mu.
func (d *SourceCaptureDispatcher) Enqueue(job SourceCaptureJob) (ok bool) {
	if d == nil {
		return false
	}
	d.mu.Lock()
	running := d.running
	d.mu.Unlock()
	if !running {
		return false
	}
	select {
	case d.jobs <- job:
		return true
	default:
		log.Printf("source_capture: queue saturated, dropping %s source %q", job.Kind, job.ID)
		return false
	}
}

func (d *SourceCaptureDispatcher) drain(ctx context.Context) {
	defer d.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case job := <-d.jobs:
			d.commit(ctx, job)
		}
	}
}

// commit builds the immutable record and writes it via the worker's write-once
// source path. Errors are logged, never fatal — capture is best-effort.
func (d *SourceCaptureDispatcher) commit(ctx context.Context, job SourceCaptureJob) {
	id := strings.TrimSpace(job.ID)
	if id == "" {
		id = DeriveSourceID(job.Kind, job.Origin, job.Title, job.Content)
	}
	capturedAt := job.CapturedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}
	rec, err := NewSourceRecord(id, job.Kind, job.Title, job.Origin, job.Content, capturedAt)
	if err != nil {
		log.Printf("source_capture: build %s source %q: %v", job.Kind, id, err)
		return
	}
	if _, _, err := d.worker.EnqueueSource(ctx, rec); err != nil {
		log.Printf("source_capture: commit %s source %q: %v", job.Kind, rec.ID, err)
	}
}

// ── broker glue ────────────────────────────────────────────────────────────

// startSourceCaptureDispatcher constructs + starts the dispatcher against the
// live wiki worker. Idempotent. A nil worker (non-markdown backend) is a no-op
// — captureSource stays a nil-safe no-op until a worker exists. Called from
// Broker.Start right after ensureWikiWorker.
func (b *Broker) startSourceCaptureDispatcher() {
	if b.sourceCaptureDispatcher.Load() != nil {
		return
	}
	b.mu.Lock()
	worker := b.wikiWorker
	b.mu.Unlock()
	if worker == nil {
		return
	}
	disp := NewSourceCaptureDispatcher(worker)
	if !b.sourceCaptureDispatcher.CompareAndSwap(nil, disp) {
		return // lost the race; another caller already wired one
	}
	disp.Start(b.brokerLifecycleContext())
}

// captureSource routes a pre-built source payload to the capture dispatcher
// with a non-blocking send. Safe to call while holding b.mu: it reads the
// dispatcher via an atomic load (no lock) and the dispatcher's Enqueue never
// blocks and never calls the git-commit path itself. A nil dispatcher (no
// markdown backend yet) is a no-op.
func (b *Broker) captureSource(job SourceCaptureJob) {
	if disp := b.sourceCaptureDispatcher.Load(); disp != nil {
		disp.Enqueue(job)
	}
}

// captureCompletedTaskSourceLocked snapshots an approved task + its decision
// packet into the immutable source layer (kind=task). Decisions are folded in
// here — there is no separate decision source. The caller holds b.mu; this
// only assembles the payload and hands it to the non-blocking dispatcher, so
// it never blocks on the git commit.
func (b *Broker) captureCompletedTaskSourceLocked(taskID string, packet DecisionPacket) {
	title := strings.TrimSpace(packet.Spec.Problem)
	artifactPath := ""
	if task := b.findTaskByIDLocked(taskID); task != nil {
		if title == "" {
			title = strings.TrimSpace(task.Title)
		}
		artifactPath = strings.TrimSpace(task.Artifact)
	}
	if title == "" {
		title = "Task " + taskID
	}
	content := renderCompletedTaskSource(packet, artifactPath)
	capturedAt := packet.UpdatedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}
	b.captureSource(SourceCaptureJob{
		Kind:       SourceKindTask,
		ID:         DeriveSourceID(SourceKindTask, taskID, title, content),
		Title:      title,
		Origin:     taskID,
		Content:    content,
		CapturedAt: capturedAt,
	})
}

// renderCompletedTaskSource builds the immutable markdown body for a completed
// task. It reuses renderWikiPromotion for the spec/session-report/grades body
// (so the source and the promotion article stay in lockstep) and folds in the
// review Feedback log + the delivered-artifact reference, which the promotion
// article omits.
func renderCompletedTaskSource(packet DecisionPacket, artifactPath string) string {
	var sb strings.Builder
	sb.WriteString(renderWikiPromotion(packet))
	if fb := packet.Spec.Feedback; len(fb) > 0 {
		sb.WriteString("\n## Feedback\n\n")
		for _, f := range fb {
			author := strings.TrimSpace(f.Author)
			if author == "" {
				author = "unknown"
			}
			sb.WriteString("- **" + author + "**")
			if !f.AppendedAt.IsZero() {
				sb.WriteString(" (" + f.AppendedAt.UTC().Format(time.RFC3339) + ")")
			}
			if body := strings.TrimSpace(f.Body); body != "" {
				sb.WriteString(": " + body)
			}
			sb.WriteString("\n")
		}
	}
	if artifactPath != "" {
		sb.WriteString("\n## Deliverable\n\n- `" + artifactPath + "`\n")
	}
	return sb.String()
}
