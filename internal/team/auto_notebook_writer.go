package team

// auto_notebook_writer.go is PR 1 of the notebook-wiki-promise design
// (~/.gstack/projects/nex-crm-wuphf/najmuzzaman-main-design-20260505-131620-notebook-wiki-promise.md).
//
// It populates per-agent notebook shelves deterministically: every roster-agent
// PostMessage and every task transition emits one notebook entry under
// agents/{slug}/notebook/{YYYY-MM-DD-HHMMSS}-{kind}-{shortHash}.md.
//
// Hot-path constraints (locked by eng review 2026-05-05):
//   - PostMessage stays sub-microsecond. Handle() is a non-blocking enqueue.
//   - Drop on queue saturation; never block the broker. Counter + warn log.
//   - In-memory LRU dedupe per (slug, day) keyed by sha256(content). Ring of 50.
//   - Pre-write secretlint regex scrub. Match → drop with redacted_event counter.
//   - Roster-membership filter at ingress. Non-agents bypass.
//   - One file per event (file-as-entry — aligns with NotebookSignalScanner).
//   - On NotebookWrite error: structured warn + counter + drop. No retry.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// AutoNotebookEventKind identifies which broker hook produced an event. The
// string values are written into entry filenames and section headers, so they
// participate in the public format and must stay stable.
type AutoNotebookEventKind string

const (
	AutoNotebookEventMessagePosted    AutoNotebookEventKind = "message_posted"
	AutoNotebookEventTaskTransitioned AutoNotebookEventKind = "task_transitioned"
)

// autoNotebookQueueSize is the buffered-channel capacity (decision 6A). 256
// gives ~minutes of headroom under realistic burst loads while keeping the
// drop-on-saturation tail short enough to surface in logs quickly.
const autoNotebookQueueSize = 256

// autoNotebookDedupeRing bounds the per-bucket sha256 history. 50 covers a
// reasonable rolling window of "same content posted twice" without unbounded
// memory growth on a long-running session (decision 4A).
const autoNotebookDedupeRing = 50

// autoNotebookContentLimit truncates the blockquote rendered into each entry
// (decision S1A). The notebook is meant to be a navigable shelf, not a verbatim
// transcript — full content lives in the broker's message log.
const autoNotebookContentLimit = 500

// autoNotebookWriteTimeout bounds a single NotebookWrite call so a stuck git
// process never wedges the writer goroutine.
const autoNotebookWriteTimeout = 10 * time.Second

// autoNotebookEvent is the in-memory event submitted to the writer's queue.
// Constructed inline at hook sites; never persisted.
type autoNotebookEvent struct {
	Kind         AutoNotebookEventKind
	Slug         string // owning agent — the notebook shelf the entry lands on
	Actor        string // who acted; usually equals Slug
	Channel      string
	TaskID       string
	TaskTitle    string
	BeforeStatus string
	AfterStatus  string
	Content      string
	Timestamp    time.Time
}

// autoNotebookWriterClient is the slice of WikiWorker the writer needs. Kept as
// an interface so tests can substitute a fake without spinning the real worker.
type autoNotebookWriterClient interface {
	NotebookWrite(ctx context.Context, slug, path, content, mode, commitMsg string) (string, int, error)
}

// autoNotebookRoster filters events to roster-member senders only (OV6A).
// Broker satisfies this via IsAgentMemberSlug.
type autoNotebookRoster interface {
	IsAgentMemberSlug(slug string) bool
}

// AutoNotebookWriter ingests broker events and writes notebook entries.
// Lifecycle mirrors WikiWorker: NewAutoNotebookWriter → Start(ctx) → Stop(timeout).
// Safe for concurrent Handle() callers.
type AutoNotebookWriter struct {
	wiki   autoNotebookWriterClient
	roster autoNotebookRoster
	queue  chan autoNotebookEvent

	mu      sync.Mutex
	buckets map[string]*autoNotebookDedupeBucket

	running atomic.Bool
	done    chan struct{}

	enqueued       atomic.Int64
	deduped        atomic.Int64
	redacted       atomic.Int64
	nonRoster      atomic.Int64
	written        atomic.Int64
	writeFailed    atomic.Int64
	queueSaturated atomic.Int64
	noopTransition atomic.Int64
}

// autoNotebookDedupeBucket is a small ring buffer of recent content hashes,
// keyed by (slug, YYYY-MM-DD). Day boundaries are natural section breaks in
// the bookshelf so per-day buckets are the right granularity.
type autoNotebookDedupeBucket struct {
	hashes []string
}

// NewAutoNotebookWriter constructs an idle writer. Call Start to begin
// processing. Either argument may be nil for tests; nil wiki disables writes,
// nil roster disables the membership filter.
func NewAutoNotebookWriter(wiki autoNotebookWriterClient, roster autoNotebookRoster) *AutoNotebookWriter {
	return &AutoNotebookWriter{
		wiki:    wiki,
		roster:  roster,
		queue:   make(chan autoNotebookEvent, autoNotebookQueueSize),
		buckets: make(map[string]*autoNotebookDedupeBucket),
		done:    make(chan struct{}),
	}
}

// Start launches the drain goroutine. Idempotent: a second call is a no-op.
func (w *AutoNotebookWriter) Start(ctx context.Context) {
	if w == nil {
		return
	}
	if w.running.Swap(true) {
		return
	}
	go w.run(ctx)
}

// Stop closes the queue and waits up to timeout for the drain goroutine to
// finish. Idempotent. Returns even if the deadline elapses with events still
// in flight — caller may inspect counters to detect drops.
func (w *AutoNotebookWriter) Stop(timeout time.Duration) {
	if w == nil || !w.running.Swap(false) {
		return
	}
	close(w.queue)
	if timeout <= 0 {
		<-w.done
		return
	}
	select {
	case <-w.done:
	case <-time.After(timeout):
	}
}

// Handle is the broker-side ingress. Roster-filters and validates, then does a
// non-blocking enqueue. Drops with a counter increment when the queue is full
// (decision S3A). Always cheap to call from a hot path.
func (w *AutoNotebookWriter) Handle(evt autoNotebookEvent) {
	if w == nil || !w.running.Load() {
		return
	}
	evt.Slug = strings.TrimSpace(evt.Slug)
	if evt.Slug == "" {
		return
	}
	if w.roster != nil && !w.roster.IsAgentMemberSlug(evt.Slug) {
		w.nonRoster.Add(1)
		return
	}
	if evt.Kind == AutoNotebookEventTaskTransitioned && strings.EqualFold(evt.BeforeStatus, evt.AfterStatus) {
		w.noopTransition.Add(1)
		return
	}
	if strings.TrimSpace(evt.Content) == "" && evt.Kind == AutoNotebookEventMessagePosted {
		// A truly empty agent message has nothing worth shelving. Task
		// transitions still record even with empty content because the
		// status delta itself is the signal.
		return
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	} else {
		evt.Timestamp = evt.Timestamp.UTC()
	}
	select {
	case w.queue <- evt:
		w.enqueued.Add(1)
	default:
		w.queueSaturated.Add(1)
		log.Printf("auto_notebook_writer: queue saturated, dropping event slug=%s kind=%s", evt.Slug, evt.Kind)
	}
}

func (w *AutoNotebookWriter) run(ctx context.Context) {
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-w.queue:
			if !ok {
				return
			}
			w.process(ctx, evt)
		}
	}
}

func (w *AutoNotebookWriter) process(ctx context.Context, evt autoNotebookEvent) {
	body := renderAutoNotebookSection(evt)
	if autoNotebookContainsSecret(body) {
		w.redacted.Add(1)
		log.Printf("auto_notebook_writer: secret pattern matched, dropping event slug=%s kind=%s", evt.Slug, evt.Kind)
		return
	}
	// Dedupe key is the raw content + transition delta, not the rendered body
	// — two events with identical message text but different timestamps render
	// differently, and we want them to collapse into a single shelf entry per
	// decision 4A. Including the kind+status pair distinguishes a status churn
	// from a chat repeat.
	dedupeBasis := string(evt.Kind) + "|" + evt.BeforeStatus + "→" + evt.AfterStatus + "|" + strings.TrimSpace(evt.Content)
	if w.isDuplicate(evt.Slug, evt.Timestamp, dedupeBasis) {
		w.deduped.Add(1)
		return
	}
	relPath := autoNotebookEntryPath(evt, body)
	commitMsg := fmt.Sprintf("notebook: auto-write %s for @%s", evt.Kind, evt.Slug)

	if w.wiki == nil {
		w.writeFailed.Add(1)
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, autoNotebookWriteTimeout)
	defer cancel()
	_, _, err := w.wiki.NotebookWrite(writeCtx, evt.Slug, relPath, body, "create", commitMsg)
	if err != nil {
		w.writeFailed.Add(1)
		log.Printf("auto_notebook_writer: write failed slug=%s path=%s: %v", evt.Slug, relPath, err)
		return
	}
	w.written.Add(1)
}

// isDuplicate consults the per-(slug, day) ring buffer. A hit returns true and
// does NOT add the hash again. A miss appends and returns false.
func (w *AutoNotebookWriter) isDuplicate(slug string, ts time.Time, content string) bool {
	key := autoNotebookDedupeKey(slug, ts)
	hash := autoNotebookSha256Hex(content)
	w.mu.Lock()
	defer w.mu.Unlock()
	bucket, ok := w.buckets[key]
	if !ok {
		bucket = &autoNotebookDedupeBucket{}
		w.buckets[key] = bucket
	}
	for _, h := range bucket.hashes {
		if h == hash {
			return true
		}
	}
	bucket.hashes = append(bucket.hashes, hash)
	if len(bucket.hashes) > autoNotebookDedupeRing {
		bucket.hashes = bucket.hashes[len(bucket.hashes)-autoNotebookDedupeRing:]
	}
	// Cheap GC: drop yesterday's bucket once a new day rolls over so we do not
	// retain stale day buckets across long-running sessions.
	w.gcOldBucketsLocked(ts)
	return false
}

func (w *AutoNotebookWriter) gcOldBucketsLocked(ts time.Time) {
	today := ts.UTC().Format("2006-01-02")
	for k := range w.buckets {
		if !strings.HasSuffix(k, today) {
			// Keep buckets whose key day matches today. Drop everything else.
			// Same-day keys end in today; mismatching keys are old.
			parts := strings.SplitN(k, "|", 2)
			if len(parts) != 2 || parts[1] != today {
				delete(w.buckets, k)
			}
		}
	}
}

// AutoNotebookCounters is a snapshot of the writer's observability counters.
// Returned by Counters() for tests and (eventually) the TODO #18 metrics surface.
type AutoNotebookCounters struct {
	Enqueued       int64
	Written        int64
	Deduped        int64
	Redacted       int64
	NonRoster      int64
	WriteFailed    int64
	QueueSaturated int64
	NoopTransition int64
}

// Counters returns a thread-safe snapshot of the writer's atomic counters.
func (w *AutoNotebookWriter) Counters() AutoNotebookCounters {
	if w == nil {
		return AutoNotebookCounters{}
	}
	return AutoNotebookCounters{
		Enqueued:       w.enqueued.Load(),
		Written:        w.written.Load(),
		Deduped:        w.deduped.Load(),
		Redacted:       w.redacted.Load(),
		NonRoster:      w.nonRoster.Load(),
		WriteFailed:    w.writeFailed.Load(),
		QueueSaturated: w.queueSaturated.Load(),
		NoopTransition: w.noopTransition.Load(),
	}
}

// renderAutoNotebookSection produces the markdown body for one entry. Format
// is locked by decision S1A: H1 with kind, bullet list of metadata, blockquote
// of the first autoNotebookContentLimit chars of content. Any markdown-special
// characters in content are forced into the blockquote so they cannot inject
// new H2 sections or break the catalog reader.
func renderAutoNotebookSection(evt autoNotebookEvent) string {
	ts := evt.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	} else {
		ts = ts.UTC()
	}
	var b strings.Builder
	switch evt.Kind {
	case AutoNotebookEventTaskTransitioned:
		fmt.Fprintf(&b, "# task_transitioned — %s → %s\n\n",
			autoNotebookFallback(evt.BeforeStatus, "(unset)"),
			autoNotebookFallback(evt.AfterStatus, "(unset)"))
	case AutoNotebookEventMessagePosted:
		fmt.Fprintf(&b, "# message_posted in #%s\n\n", autoNotebookFallback(evt.Channel, "general"))
	default:
		fmt.Fprintf(&b, "# %s\n\n", string(evt.Kind))
	}
	fmt.Fprintf(&b, "- timestamp: %s\n", ts.Format(time.RFC3339))
	fmt.Fprintf(&b, "- kind: %s\n", evt.Kind)
	fmt.Fprintf(&b, "- actor: @%s\n", autoNotebookFallback(evt.Actor, evt.Slug))
	if evt.Channel != "" {
		fmt.Fprintf(&b, "- channel: #%s\n", evt.Channel)
	}
	if evt.TaskID != "" {
		title := strings.TrimSpace(evt.TaskTitle)
		if title != "" {
			fmt.Fprintf(&b, "- task: %s %q\n", evt.TaskID, title)
		} else {
			fmt.Fprintf(&b, "- task: %s\n", evt.TaskID)
		}
	}
	if evt.Kind == AutoNotebookEventTaskTransitioned {
		fmt.Fprintf(&b, "- status: %s → %s\n",
			autoNotebookFallback(evt.BeforeStatus, "(unset)"),
			autoNotebookFallback(evt.AfterStatus, "(unset)"))
	}
	body := autoNotebookTruncate(strings.TrimSpace(evt.Content), autoNotebookContentLimit)
	if body != "" {
		b.WriteString("\n")
		for _, line := range strings.Split(body, "\n") {
			b.WriteString("> ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

func autoNotebookFallback(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// autoNotebookTruncate trims s to at most n bytes while preserving valid UTF-8
// at the cut. Appends a "…" marker when truncation occurs.
func autoNotebookTruncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// autoNotebookEntryPath builds agents/{slug}/notebook/{YYYY-MM-DD-HHMMSS}-{kind}-{shortHash}.md.
// The shortHash mixes timestamp nanoseconds into the digest so two events with
// identical content but different times still get distinct filenames; dedupe
// elsewhere (isDuplicate) protects against true duplicates.
func autoNotebookEntryPath(evt autoNotebookEvent, body string) string {
	ts := evt.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	} else {
		ts = ts.UTC()
	}
	stamp := ts.Format("2006-01-02-150405")
	mix := fmt.Sprintf("%d|%s|%s", ts.UnixNano(), evt.Kind, body)
	short := autoNotebookSha256Hex(mix)[:8]
	kind := strings.ReplaceAll(string(evt.Kind), "_", "-")
	if kind == "" {
		kind = "event"
	}
	return fmt.Sprintf("agents/%s/notebook/%s-%s-%s.md", evt.Slug, stamp, kind, short)
}

func autoNotebookDedupeKey(slug string, ts time.Time) string {
	return slug + "|" + ts.UTC().Format("2006-01-02")
}

func autoNotebookSha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// autoNotebookSecretPatterns is the OV5A pre-write scrub set. Patterns are
// high-confidence (typed prefixes, fixed lengths). Generic substring rules like
// "password=" are intentionally omitted to avoid false-positive drops on agent
// chatter that happens to mention the word "password".
var autoNotebookSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                                                 // AWS access key
	regexp.MustCompile(`ASIA[0-9A-Z]{16}`),                                                 // AWS STS access key
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`),                                       // GitHub personal/OAuth/server token
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),                                     // GitHub fine-grained PAT
	regexp.MustCompile(`sk_live_[A-Za-z0-9]{24,}`),                                         // Stripe secret key (live)
	regexp.MustCompile(`rk_live_[A-Za-z0-9]{24,}`),                                         // Stripe restricted key (live)
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`),                                       // Anthropic API key
	regexp.MustCompile(`sk-[A-Za-z0-9]{32,}`),                                              // OpenAI-style secret key
	regexp.MustCompile(`SG\.[A-Za-z0-9_\-]{22}\.[A-Za-z0-9_\-]{43}`),                       // SendGrid API key
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),                                     // Slack token
	regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`),                                           // Google API key
	regexp.MustCompile(`-----BEGIN ((RSA|EC|OPENSSH|DSA|PGP) )?PRIVATE KEY( BLOCK)?-----`), // Private key block
}

// autoNotebookContainsSecret returns true when any locked secret pattern
// matches anywhere in s. Errs on the side of dropping ambiguous content;
// the broker still has the raw message in its log, so nothing is lost.
func autoNotebookContainsSecret(s string) bool {
	if s == "" {
		return false
	}
	for _, re := range autoNotebookSecretPatterns {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}
