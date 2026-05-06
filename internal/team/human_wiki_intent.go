package team

// human_wiki_intent.go is PR 2 of the notebook-wiki-promise design
// (~/.gstack/projects/nex-crm-wuphf/najmuzzaman-main-design-20260505-131620-notebook-wiki-promise.md).
//
// It populates the team wiki immediately when a HUMAN posts a channel message
// containing a remember-intent phrase ("remember this", "save to wiki", etc.).
// Bypasses the notebook → promote → review loop entirely; the human's word IS
// the canonical source.
//
// Lock discipline (mirrors PR 1's auto_notebook_writer):
//   - Handle() is called from broker_messages.go AFTER b.mu.Unlock(). It is a
//     non-blocking enqueue — never re-enters b.mu, never calls broker methods.
//   - The classifier is pure CPU (regex match + string slice); it runs INSIDE
//     the writer goroutine, not at the hook site, so the hot path stays
//     sub-microsecond.
//   - The writer goroutine calls only wiki.EnqueueHuman; no broker mutex
//     involvement at all.
//
// CodeRabbit-flagged shutdown race (same as PR 1): we close stopCh, never the
// queue, so concurrent Handle() callers past the running.Load() check cannot
// panic on send-to-closed-chan.

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

// humanWikiIntentKind tags which phrase pattern fired. The string values
// participate in entry rendering (intent meta line) so they must stay stable.
type humanWikiIntentKind string

const (
	HumanWikiIntentRemember  humanWikiIntentKind = "remember"
	HumanWikiIntentSaveMem   humanWikiIntentKind = "save_memory"
	HumanWikiIntentWriteKB   humanWikiIntentKind = "write_kb"
	HumanWikiIntentWikiThis  humanWikiIntentKind = "wiki_this"
	HumanWikiIntentCanonical humanWikiIntentKind = "canonical"
)

// humanWikiIntentQueueSize is the buffered-channel capacity. Mirrors the auto
// notebook writer's choice (256) — same workload pattern (one enqueue per
// PostMessage), same drop-on-saturation safety net.
const humanWikiIntentQueueSize = 256

// humanWikiIntentWriteTimeout bounds a single EnqueueHuman call.
const humanWikiIntentWriteTimeout = 10 * time.Second

// humanWikiIntentTopicLimit caps the topic length used in filenames + H1.
// Wide enough for a real sentence fragment but bounded so a long paste does
// not produce an unwieldy filename.
const humanWikiIntentTopicLimit = 80

// humanWikiIntentMatch is the classifier output.
type humanWikiIntentMatch struct {
	Kind    humanWikiIntentKind
	Topic   string // extracted topic, used in filename + H1; "" → "note"
	Content string // full trimmed payload after stripping the intent phrase
}

// humanWikiIntentPattern pairs a kind with the regex that triggers it. Order
// matters: the first match wins. More specific patterns (write to KB, save to
// memory) come before the bare "remember this" / "save this" patterns so a
// message like "save to wiki: foo" does not get classified as Remember.
type humanWikiIntentPattern struct {
	Kind humanWikiIntentKind
	Re   *regexp.Regexp
}

// humanWikiIntentPatterns is the locked classifier set. Each pattern anchors
// at the start of the (trimmed) line and captures the payload after the
// optional separator (":", "—", "-", "—", or just whitespace).
//
// Conservative by design: only imperative / present-first-person forms fire.
// "remember when we shipped X" (historical) does NOT match because the trailing
// "when" path is intentionally absent. "I remembered this yesterday" (past
// tense) does NOT match because the regex requires the bare verb, not its
// past form.
var humanWikiIntentPatterns = []humanWikiIntentPattern{
	// More specific phrases first — order matters for ambiguous prefixes.
	{Kind: HumanWikiIntentSaveMem, Re: regexp.MustCompile(`(?i)^\s*save\s+(?:this\s+)?to\s+memory\b[\s:\-—–]*(.*)$`)},
	{Kind: HumanWikiIntentWriteKB, Re: regexp.MustCompile(`(?i)^\s*save\s+(?:this\s+)?to\s+(?:kb|knowledge[\s\-_]?base|wiki)\b[\s:\-—–]*(.*)$`)},
	{Kind: HumanWikiIntentWriteKB, Re: regexp.MustCompile(`(?i)^\s*write\s+(?:this\s+)?to\s+(?:kb|knowledge[\s\-_]?base|wiki)\b[\s:\-—–]*(.*)$`)},
	{Kind: HumanWikiIntentWikiThis, Re: regexp.MustCompile(`(?i)^\s*add\s+(?:this\s+)?to\s+(?:the\s+)?wiki\b[\s:\-—–]*(.*)$`)},
	{Kind: HumanWikiIntentWikiThis, Re: regexp.MustCompile(`(?i)^\s*wiki\s+this\b[\s:\-—–]*(.*)$`)},
	{Kind: HumanWikiIntentCanonical, Re: regexp.MustCompile(`(?i)^\s*this\s+is\s+canonical\b[\s:\-—–]*(.*)$`)},
	// Generic fallbacks — anchored verbs only, so historical / past-tense
	// phrasings do not match. Go's RE2 has no negative lookahead, so the
	// "remember when ..." historical reading is filtered by the call-site
	// guard humanWikiIsHistoricalRemember.
	{Kind: HumanWikiIntentRemember, Re: regexp.MustCompile(`(?i)^\s*remember\s+this\b[\s:\-—–]*(.*)$`)},
	{Kind: HumanWikiIntentRemember, Re: regexp.MustCompile(`(?i)^\s*save\s+this\b[\s:\-—–]*(.*)$`)},
	{Kind: HumanWikiIntentRemember, Re: regexp.MustCompile(`(?i)^\s*write\s+this\s+down\b[\s:\-—–]*(.*)$`)},
}

// historicalRememberRe matches phrasings where "remember" is followed by a
// historical / interrogative continuation rather than an instruction. RE2
// has no lookahead so we run this as a separate fast check.
var historicalRememberRe = regexp.MustCompile(`(?i)^\s*remember\s+(when|how|that\s+time|the\s+time|why|where|who|what|if)\b`)

// pastTenseRememberRe drops "remembered ..." / "I remembered ..." which are
// reflective, not instructive. The bare-verb regexes already require
// "remember" without the past-tense suffix; this guard is here for symmetry
// + to keep the no-match table comprehensive.
var pastTenseRememberRe = regexp.MustCompile(`(?i)^\s*(?:i\s+)?remembered\b`)

// codeFenceLine matches lines that are part of a fenced code block.
var codeFenceLineRe = regexp.MustCompile("(?m)^```")

// humanWikiIntentInsideCodeFence returns true when the body opens a fenced
// block whose closing fence is missing or after the rest of the message — in
// either case treating the intent phrase as code, not instruction.
func humanWikiIntentInsideCodeFence(body string) bool {
	matches := codeFenceLineRe.FindAllStringIndex(body, -1)
	return len(matches)%2 != 0 || (len(matches) >= 2 && strings.Contains(body, "```"))
}

// classifyHumanWikiIntent inspects body for an intent phrase. Returns
// (match, true) if any pattern fires, (zero, false) otherwise. Pure function:
// no locks, no I/O. Safe to call from any goroutine.
func classifyHumanWikiIntent(body string) (humanWikiIntentMatch, bool) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return humanWikiIntentMatch{}, false
	}

	// Drop fenced code-block content so an intent phrase quoted inside ``` ... ```
	// does not trigger a write. Also drops `inline-code` spans because a
	// developer demonstrating the syntax should not accidentally trip the
	// classifier.
	scrubbed := stripCodeFences(trimmed)
	scrubbed = stripInlineCode(scrubbed)
	scrubbed = strings.TrimSpace(scrubbed)
	if scrubbed == "" {
		return humanWikiIntentMatch{}, false
	}

	// Match against the FIRST line of the scrubbed body — the intent phrase
	// must be the leading instruction. This prevents "lots of context...
	// remember when X happened" from accidentally matching.
	firstLine := scrubbed
	if idx := strings.IndexByte(scrubbed, '\n'); idx >= 0 {
		firstLine = scrubbed[:idx]
	}
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" {
		return humanWikiIntentMatch{}, false
	}

	// Historical / interrogative "remember when ..." style phrasings reach into
	// the past, not into the wiki. Filter before the pattern loop so they
	// never match the bare-remember fallback.
	if historicalRememberRe.MatchString(firstLine) || pastTenseRememberRe.MatchString(firstLine) {
		return humanWikiIntentMatch{}, false
	}

	for _, pat := range humanWikiIntentPatterns {
		m := pat.Re.FindStringSubmatch(firstLine)
		if m == nil {
			continue
		}
		// Capture group 1 is the payload after the intent phrase on the first
		// line. Append any subsequent lines so multi-line "remember this:\n
		// bullet 1\n bullet 2" payloads survive.
		payloadFirst := ""
		if len(m) >= 2 {
			payloadFirst = strings.TrimSpace(m[1])
		}
		var payload string
		if idx := strings.IndexByte(scrubbed, '\n'); idx >= 0 {
			payload = strings.TrimSpace(payloadFirst + "\n" + scrubbed[idx+1:])
		} else {
			payload = payloadFirst
		}
		topic := humanWikiTopicFromPayload(payload)
		return humanWikiIntentMatch{
			Kind:    pat.Kind,
			Topic:   topic,
			Content: payload,
		}, true
	}
	return humanWikiIntentMatch{}, false
}

// stripCodeFences removes ``` ... ``` blocks from s. Pairs are matched
// greedily; an unbalanced opening fence drops everything from the fence
// onward (treat it as code, ignore for classification).
func stripCodeFences(s string) string {
	var out strings.Builder
	for {
		i := strings.Index(s, "```")
		if i < 0 {
			out.WriteString(s)
			return out.String()
		}
		out.WriteString(s[:i])
		rest := s[i+3:]
		j := strings.Index(rest, "```")
		if j < 0 {
			// unbalanced; drop the rest
			return out.String()
		}
		s = rest[j+3:]
	}
}

// stripInlineCode removes `…` runs from s.
func stripInlineCode(s string) string {
	var out strings.Builder
	for {
		i := strings.IndexByte(s, '`')
		if i < 0 {
			out.WriteString(s)
			return out.String()
		}
		out.WriteString(s[:i])
		j := strings.IndexByte(s[i+1:], '`')
		if j < 0 {
			// unbalanced; keep the rest as-is so we do not silently swallow
			// real prose that happens to contain a stray backtick.
			out.WriteString(s[i:])
			return out.String()
		}
		s = s[i+1+j+1:]
	}
}

// humanWikiTopicFromPayload derives a short topic string from the payload.
// Falls back to "" when the payload is empty; the path generator turns "" into
// "note" so file paths are always valid.
func humanWikiTopicFromPayload(payload string) string {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return ""
	}
	// First sentence-ish chunk: split on newline / period / semicolon and take
	// the leading fragment.
	cut := len(payload)
	for _, sep := range []string{"\n", ". ", "; "} {
		if i := strings.Index(payload, sep); i >= 0 && i < cut {
			cut = i
		}
	}
	topic := strings.TrimSpace(payload[:cut])
	if topic == "" {
		return ""
	}
	// Clip to humanWikiIntentTopicLimit bytes at a UTF-8 boundary.
	if len(topic) > humanWikiIntentTopicLimit {
		n := humanWikiIntentTopicLimit
		for n > 0 && (topic[n]&0xC0) == 0x80 {
			n--
		}
		topic = topic[:n]
	}
	return topic
}

// humanWikiSlugify lowercases and replaces non-alphanumeric runs with "-".
// Drops leading / trailing dashes. Result is empty when the topic is empty.
func humanWikiSlugify(topic string) string {
	topic = strings.ToLower(strings.TrimSpace(topic))
	if topic == "" {
		return ""
	}
	var b strings.Builder
	prevDash := true
	for _, r := range topic {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := b.String()
	out = strings.Trim(out, "-")
	if len(out) > humanWikiIntentTopicLimit {
		out = out[:humanWikiIntentTopicLimit]
		out = strings.TrimRight(out, "-")
	}
	return out
}

// humanWikiEntryPath builds team/{YYYY-MM-DD}-{topic-slug}-{shortHash}.md.
// shortHash = sha256(content)[:8] — same content posted twice on the same day
// produces the same path, so EnqueueHuman with mode=replace is a true no-op.
// Empty topic falls back to "note" so the path is always well-formed.
func humanWikiEntryPath(match humanWikiIntentMatch, ts time.Time) string {
	stamp := ts.UTC().Format("2006-01-02")
	slug := humanWikiSlugify(match.Topic)
	if slug == "" {
		slug = "note"
	}
	short := humanWikiSha256Hex(match.Content)[:8]
	return fmt.Sprintf("team/%s-%s-%s.md", stamp, slug, short)
}

func humanWikiSha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// renderHumanWikiEntry produces the canonical wiki entry body. H1 is the
// extracted topic (with a fallback). Meta block records the timestamp,
// source=human, intent kind, and channel. Content is included verbatim under
// a "## Note" heading so downstream readers (catalog, search) treat it as a
// real article.
func renderHumanWikiEntry(match humanWikiIntentMatch, channel string, ts time.Time) string {
	topic := strings.TrimSpace(match.Topic)
	if topic == "" {
		topic = "Note"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", topic)
	fmt.Fprintf(&b, "- timestamp: %s\n", ts.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "- source: human\n")
	fmt.Fprintf(&b, "- intent: %s\n", match.Kind)
	if strings.TrimSpace(channel) != "" {
		fmt.Fprintf(&b, "- channel: #%s\n", strings.TrimSpace(channel))
	}
	b.WriteString("\n## Note\n\n")
	content := strings.TrimSpace(match.Content)
	if content == "" {
		content = topic
	}
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// humanWikiIntentWriterClient is the slice of WikiWorker the writer needs.
// Kept as an interface so tests can substitute a fake.
type humanWikiIntentWriterClient interface {
	EnqueueHuman(ctx context.Context, path, content, commitMsg, expectedSHA string) (string, int, error)
}

// HumanWikiIntentWriter ingests human channel messages, classifies for
// remember-intent, and writes matching messages directly into the team wiki.
// Lifecycle mirrors AutoNotebookWriter: New → Start(ctx) → Stop(timeout).
// Safe for concurrent Handle() callers.
type HumanWikiIntentWriter struct {
	wiki  humanWikiIntentWriterClient
	queue chan channelMessage
	// stopCh signals shutdown without closing w.queue (see PR 1's writer for
	// the rationale: closing the queue would race with Handle callers past
	// the running.Load() fast path).
	stopCh chan struct{}

	// progressMu + progressCond drive WaitForCondition. Tests block on a
	// counter or filesystem state and are released by signalProgress() at
	// the end of each processed event.
	progressMu   sync.Mutex
	progressCond *sync.Cond

	running atomic.Bool
	done    chan struct{}

	// counters
	enqueued    atomic.Int64
	written     atomic.Int64
	skipped     atomic.Int64 // no intent match
	writeFailed atomic.Int64
	queueSat    atomic.Int64
}

// HumanWikiIntentCounters is a snapshot of the writer's observability
// counters, returned by Counters() for tests + future metrics surface.
type HumanWikiIntentCounters struct {
	Enqueued    int64
	Written     int64
	Skipped     int64
	WriteFailed int64
	QueueSat    int64
}

// NewHumanWikiIntentWriter constructs an idle writer. Call Start to begin.
// nil wiki is safe but disables actual writes (counters still advance).
func NewHumanWikiIntentWriter(wiki humanWikiIntentWriterClient) *HumanWikiIntentWriter {
	w := &HumanWikiIntentWriter{
		wiki:   wiki,
		queue:  make(chan channelMessage, humanWikiIntentQueueSize),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	w.progressCond = sync.NewCond(&w.progressMu)
	return w
}

// Start launches the drain goroutine. Idempotent.
func (w *HumanWikiIntentWriter) Start(ctx context.Context) {
	if w == nil {
		return
	}
	if w.running.Swap(true) {
		return
	}
	go w.run(ctx)
}

// Stop signals the drain goroutine to exit and waits up to timeout. Idempotent.
// Mirrors AutoNotebookWriter.Stop — closes stopCh, never the queue, and wakes
// any test goroutine parked on progressCond.
func (w *HumanWikiIntentWriter) Stop(timeout time.Duration) {
	if w == nil || !w.running.Swap(false) {
		return
	}
	close(w.stopCh)
	w.progressMu.Lock()
	w.progressCond.Broadcast()
	w.progressMu.Unlock()
	if timeout <= 0 {
		<-w.done
		return
	}
	select {
	case <-w.done:
	case <-time.After(timeout):
	}
}

// Handle is the broker-side ingress. Non-blocking enqueue. Drops with a
// counter increment on saturation.
//
// The classifier is intentionally NOT called here — it runs inside the writer
// goroutine. Keeping the hook site to a single channel send keeps the
// PostMessage hot path predictable.
func (w *HumanWikiIntentWriter) Handle(msg channelMessage) {
	if w == nil || !w.running.Load() {
		return
	}
	if !isHumanMessageSender(msg.From) {
		// Defence in depth — broker filters at the hook site too. Non-human
		// senders never reach the wiki path.
		return
	}
	if strings.TrimSpace(msg.Content) == "" {
		return
	}
	select {
	case <-w.stopCh:
		return
	default:
	}
	select {
	case <-w.stopCh:
	case w.queue <- msg:
		w.enqueued.Add(1)
	default:
		w.queueSat.Add(1)
		log.Printf("human_wiki_intent_writer: queue saturated, dropping msg id=%s from=%s", msg.ID, msg.From)
		w.signalProgress()
	}
}

func (w *HumanWikiIntentWriter) signalProgress() {
	w.progressMu.Lock()
	w.progressCond.Broadcast()
	w.progressMu.Unlock()
}

// WaitForCondition blocks until predicate returns true, ctx cancels, or the
// writer stops. Test-only.
func (w *HumanWikiIntentWriter) WaitForCondition(ctx context.Context, predicate func() bool) error {
	if w == nil {
		return nil
	}
	if predicate() {
		return nil
	}
	cancelWatcher := make(chan struct{})
	defer close(cancelWatcher)
	go func() {
		select {
		case <-ctx.Done():
			w.progressMu.Lock()
			w.progressCond.Broadcast()
			w.progressMu.Unlock()
		case <-cancelWatcher:
		}
	}()
	w.progressMu.Lock()
	defer w.progressMu.Unlock()
	for !predicate() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !w.running.Load() {
			if predicate() {
				return nil
			}
			return ErrWorkerStopped
		}
		w.progressCond.Wait()
	}
	return nil
}

// Counters returns a thread-safe snapshot.
func (w *HumanWikiIntentWriter) Counters() HumanWikiIntentCounters {
	if w == nil {
		return HumanWikiIntentCounters{}
	}
	return HumanWikiIntentCounters{
		Enqueued:    w.enqueued.Load(),
		Written:     w.written.Load(),
		Skipped:     w.skipped.Load(),
		WriteFailed: w.writeFailed.Load(),
		QueueSat:    w.queueSat.Load(),
	}
}

func (w *HumanWikiIntentWriter) run(ctx context.Context) {
	defer close(w.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case msg := <-w.queue:
			w.process(ctx, msg)
		}
	}
}

func (w *HumanWikiIntentWriter) process(ctx context.Context, msg channelMessage) {
	defer w.signalProgress()

	match, ok := classifyHumanWikiIntent(msg.Content)
	if !ok {
		w.skipped.Add(1)
		return
	}

	ts := parseHumanWikiTimestamp(msg.Timestamp)
	path := humanWikiEntryPath(match, ts)
	body := renderHumanWikiEntry(match, msg.Channel, ts)
	commitMsg := fmt.Sprintf("human: remember (%s)", match.Kind)

	if w.wiki == nil {
		w.writeFailed.Add(1)
		return
	}
	writeCtx, cancel := context.WithTimeout(ctx, humanWikiIntentWriteTimeout)
	defer cancel()
	// expectedSHA is intentionally empty: this is a new file path on every
	// distinct content. If the path already exists (same content, same day),
	// EnqueueHuman with mode=replace is a no-op commit at the git layer.
	_, _, err := w.wiki.EnqueueHuman(writeCtx, path, body, commitMsg, "")
	if err != nil {
		w.writeFailed.Add(1)
		log.Printf("human_wiki_intent_writer: write failed path=%s: %v", path, err)
		return
	}
	w.written.Add(1)
}

// parseHumanWikiTimestamp extracts a UTC timestamp from a channelMessage.
// Falls back to time.Now() when the message lacks a parseable RFC3339 stamp.
func parseHumanWikiTimestamp(ts string) time.Time {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return time.Now().UTC()
	}
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return t.UTC()
	}
	return time.Now().UTC()
}
