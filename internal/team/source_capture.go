package team

// source_capture.go is the S2 "source capture" layer: it snapshots office
// activity (completed tasks + decisions, chat digests, learnings/playbooks)
// into the gbrain knowledge backend as upsert-by-slug pages. Capture hooks fire
// on the broker's hot path — often while the broker mutex b.mu is held — so they
// MUST be non-blocking.
//
// THE DEADLOCK GUARD
// ==================
//
// Writing into gbrain (put_page over MCP) is a blocking network/subprocess call.
// Calling it while b.mu is held would freeze the whole broker. So every capture
// is routed through SourceCaptureDispatcher.Enqueue, a NON-BLOCKING buffered
// channel send. A single drain goroutine — which never touches b.mu — owns the
// blocking put_page write. Records are assembled at the hook site (where the
// domain data is in scope), not on the drain.
//
// De-dup is by slug: put_page is an upsert, and the slug is derived
// deterministically from the job (origin-keyed where possible via
// DeriveSourceID), so re-capturing the same office event overwrites the same
// gbrain page rather than duplicating it.
//
// gbrain-absent is graceful: when no gbrain client is registered (or gbrain is
// not installed) the drain logs-and-drops the job — it never blocks or spins on
// a dead backend.

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/gbrain"
	"gopkg.in/yaml.v3"
)

// SourceCaptureQueue is the buffered job-channel capacity. Generous on purpose:
// each job is a tiny pre-built payload and the drain is fast (one idempotent
// gbrain put_page per job). On saturation the dispatcher drops + logs rather
// than block — the load-bearing rule is that a hook firing under b.mu never
// blocks.
const SourceCaptureQueue = 128

// gbrain capture provenance. These label every office-activity page so gbrain
// can attribute and filter them away from human-curated wiki content.
const (
	// gbrainCaptureSource is the frontmatter `source:` value stamped on every
	// captured page.
	gbrainCaptureSource = "wuphf-office"
	// gbrainCaptureTag is the shared frontmatter tag on every captured page;
	// the per-kind tag (e.g. "task") is appended alongside it.
	gbrainCaptureTag = "office"
	// gbrainCaptureSourceKindPrefix prefixes the put_page source_kind so a
	// captured task lands as source_kind "wuphf_office_task", etc.
	gbrainCaptureSourceKindPrefix = "wuphf_office_"
	// gbrainCaptureIngestedVia is the put_page ingested_via provenance.
	gbrainCaptureIngestedVia = "wuphf-capture"
)

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

// sourceCaptureWriter is the narrow write seam the drain depends on: turn one
// capture job into a durable write. Production injects a gbrain-backed writer
// (put_page upsert); tests inject a fake to assert the write shape without a
// live gbrain subprocess. Implementations MUST be safe to call from the drain
// goroutine and MUST NOT block indefinitely on an absent backend.
type sourceCaptureWriter interface {
	WriteSource(ctx context.Context, job SourceCaptureJob) error
}

// SourceCaptureDispatcher owns the off-lock drain that turns capture jobs into
// gbrain pages. Construct one per broker, Start it once at boot, and Stop it on
// shutdown.
type SourceCaptureDispatcher struct {
	writer sourceCaptureWriter
	jobs   chan SourceCaptureJob

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// NewSourceCaptureDispatcher wires a dispatcher against the given writer.
func NewSourceCaptureDispatcher(writer sourceCaptureWriter) *SourceCaptureDispatcher {
	return &SourceCaptureDispatcher{
		writer: writer,
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

// commit hands the job to the injected writer. This runs ONLY on the drain
// goroutine, never under b.mu, so the writer's blocking put_page call cannot
// deadlock the broker. Errors are logged, never fatal — capture is best-effort.
func (d *SourceCaptureDispatcher) commit(ctx context.Context, job SourceCaptureJob) {
	if d.writer == nil {
		return
	}
	if err := d.writer.WriteSource(ctx, job); err != nil {
		log.Printf("source_capture: write %s source %q: %v", job.Kind, captureSlug(job), err)
	}
}

// ── gbrain-backed writer ─────────────────────────────────────────────────────

// gbrainPutPager is the one-method slice of *gbrain.Client the source writer
// needs. Reducing the dependency to put_page lets tests inject a fake without a
// live gbrain subprocess. The broker-owned gbrainMemoryClient (Query + PutPage)
// satisfies it.
type gbrainPutPager interface {
	PutPage(ctx context.Context, content string, opts gbrain.PutOptions) (gbrain.PutResult, error)
}

// gbrainSourceWriter writes office-activity capture jobs into gbrain as
// upsert-by-slug pages. The client is resolved lazily per write so the writer
// always sees the current broker-owned client and degrades to a log-and-drop
// no-op when gbrain is absent (resolve returns nil).
type gbrainSourceWriter struct {
	resolve func() gbrainPutPager
}

// newGBrainSourceWriter builds the production writer. It resolves the
// broker-owned gbrain client per write via sharedGBrainClient, so a client
// registered after the dispatcher starts is still picked up, and an absent one
// (nil) cleanly degrades to log-and-drop.
func newGBrainSourceWriter() *gbrainSourceWriter {
	return &gbrainSourceWriter{
		resolve: func() gbrainPutPager {
			if c := sharedGBrainClient(); c != nil {
				return c
			}
			return nil
		},
	}
}

// WriteSource renders the job as a markdown page (YAML frontmatter + body) and
// upserts it into gbrain keyed by a deterministic slug. A nil/absent client is
// logged and dropped (not an error): gbrain is optional, and a dead backend
// must never block or fail the drain.
func (w *gbrainSourceWriter) WriteSource(ctx context.Context, job SourceCaptureJob) error {
	if w == nil || w.resolve == nil {
		return nil
	}
	client := w.resolve()
	if client == nil {
		log.Printf("source_capture: gbrain unavailable, dropping %s source %q", job.Kind, captureSlug(job))
		return nil
	}
	content, err := renderGBrainSourcePage(job)
	if err != nil {
		return fmt.Errorf("render %s page: %w", job.Kind, err)
	}
	slug := captureSlug(job)
	if _, err := client.PutPage(ctx, content, gbrain.PutOptions{
		Slug:        slug,
		SourceKind:  gbrainCaptureSourceKindPrefix + string(job.Kind),
		IngestedVia: gbrainCaptureIngestedVia,
	}); err != nil {
		return fmt.Errorf("put_page %q: %w", slug, err)
	}
	return nil
}

// captureSlug returns the stable gbrain slug for a job: the job's pre-derived ID
// when set (feeders key it via DeriveSourceID), else a fresh derivation. Slugs
// are origin-keyed where possible so re-capturing the same office event upserts
// the same page (write-once spirit) rather than duplicating it.
func captureSlug(job SourceCaptureJob) string {
	if id := strings.TrimSpace(job.ID); id != "" {
		return id
	}
	return DeriveSourceID(job.Kind, job.Origin, job.Title, job.Content)
}

// gbrainSourceFrontmatter is the YAML frontmatter stamped on every captured
// page. It is intentionally distinct from wiki_source.go's on-disk frontmatter:
// the target here is a gbrain page, and the tag set lets gbrain filter
// office-captured pages from human-curated content.
type gbrainSourceFrontmatter struct {
	Title      string   `yaml:"title"`
	Kind       string   `yaml:"kind"`
	Origin     string   `yaml:"origin,omitempty"`
	CapturedAt string   `yaml:"captured_at"`
	Source     string   `yaml:"source"`
	Tags       []string `yaml:"tags"`
}

// renderGBrainSourcePage serializes a job into the full markdown gbrain ingests:
// a YAML frontmatter block followed by the job's body.
func renderGBrainSourcePage(job SourceCaptureJob) (string, error) {
	title := strings.TrimSpace(job.Title)
	if title == "" {
		title = strings.TrimSpace(string(job.Kind) + " " + strings.TrimSpace(job.Origin))
	}
	if title == "" {
		title = "untitled"
	}
	capturedAt := job.CapturedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now().UTC()
	}
	fm := gbrainSourceFrontmatter{
		Title:      title,
		Kind:       string(job.Kind),
		Origin:     strings.TrimSpace(job.Origin),
		CapturedAt: capturedAt.UTC().Format(time.RFC3339),
		Source:     gbrainCaptureSource,
		Tags:       []string{gbrainCaptureTag, string(job.Kind)},
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return "", fmt.Errorf("yaml encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("yaml close: %w", err)
	}
	buf.WriteString("---\n\n")
	buf.WriteString(strings.TrimRight(job.Content, "\n"))
	buf.WriteString("\n")
	return buf.String(), nil
}

// ── broker glue ────────────────────────────────────────────────────────────

// startSourceCaptureDispatcher constructs + starts the dispatcher against the
// gbrain-backed writer. Idempotent. The writer resolves the broker-owned gbrain
// client lazily per write, so this is independent of the markdown wiki worker:
// office activity flows into gbrain regardless of which memory backend is
// active, and gracefully log-and-drops when gbrain is absent. Called from
// Broker.Start.
func (b *Broker) startSourceCaptureDispatcher() {
	if b.sourceCaptureDispatcher.Load() != nil {
		return
	}
	disp := NewSourceCaptureDispatcher(newGBrainSourceWriter())
	if !b.sourceCaptureDispatcher.CompareAndSwap(nil, disp) {
		return // lost the race; another caller already wired one
	}
	disp.Start(b.brokerLifecycleContext())
}

// captureSource routes a pre-built source payload to the capture dispatcher
// with a non-blocking send. Safe to call while holding b.mu: it reads the
// dispatcher via an atomic load (no lock) and the dispatcher's Enqueue never
// blocks and never calls the put_page write path itself (the drain goroutine
// owns it). A nil dispatcher (not yet started) is a no-op.
func (b *Broker) captureSource(job SourceCaptureJob) {
	if disp := b.sourceCaptureDispatcher.Load(); disp != nil {
		disp.Enqueue(job)
	}
}

// captureCompletedTaskSourceLocked snapshots an approved task + its decision
// packet into gbrain (kind=task). Decisions are folded in here — there is no
// separate decision source. The caller holds b.mu; this only assembles the
// payload and hands it to the non-blocking dispatcher, so it never blocks on
// the put_page write.
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
