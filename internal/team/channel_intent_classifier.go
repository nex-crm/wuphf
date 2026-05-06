package team

// channel_intent_classifier.go is PR 5 of the notebook-wiki-promise design
// (~/.gstack/projects/nex-crm-wuphf/najmuzzaman-main-design-20260505-131620-notebook-wiki-promise.md).
//
// It classifies channel messages that are asking for prior team context
// ("who has context on X", "does anyone know what we decided about Y",
// "what do we have on Z") and, on a hit, searches every agent's notebook
// shelf for the topic. Each cross-agent hit feeds PR 3's NotebookDemandIndex
// with a DemandSignalChannelContextAsk event (weight 2.0).
//
// Lock discipline (mirrors PR 1 auto_notebook_writer.go and PR 2
// human_wiki_intent.go):
//   - Handle() is called from broker_messages.go AFTER b.mu.Unlock(). It is a
//     non-blocking enqueue — never re-enters b.mu.
//   - The classifier (regex match + topic extraction) is pure CPU and runs
//     INSIDE the dispatcher goroutine, not at the hook site. The hot path is
//     a single channel send.
//   - The dispatcher goroutine calls only:
//       * client.NotebookSearchAll(query)   // file I/O, no b.mu
//       * client.RecordDemandSignal(evt)    // idx.mu only
//       * (optional) client.PostSystemMessage — gated OFF by default in PR 5.
//     It NEVER acquires b.mu.
//
// Shutdown race (same as PR 1/PR 2): close stopCh, never the queue, so a
// concurrent Handle() past the running.Load() check cannot panic on
// send-to-closed-chan.

import (
	"context"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// channelIntentKind tags which family of phrase matched. Currently only one
// kind exists (context_ask); the type is kept open so future intents (e.g.
// "summarise X for me") can be added without churning the wire shape.
type channelIntentKind string

const (
	// ChannelIntentContextAsk fires on question-form context-seeking phrases:
	// "who has context on …", "does anyone know …", "what do we have on …".
	ChannelIntentContextAsk channelIntentKind = "context_ask"
)

// channelIntentQueueSize mirrors PR 1/PR 2 — same enqueue cadence (one per
// PostMessage), same drop-on-saturation safety net.
const channelIntentQueueSize = 256

// channelIntentSearchTimeout bounds a single NotebookSearchAll call so a
// degenerate corpus or stuck git op cannot wedge the dispatcher goroutine.
const channelIntentSearchTimeout = 10 * time.Second

// channelIntentTopicMinTokens drops topic phrases shorter than this many
// whitespace-delimited tokens. Without it, agent technical chatter like
// "who has access" or "what we ship" produces 1- or 2-word topics that
// match thousands of unrelated notebook lines.
const channelIntentTopicMinTokens = 2

// channelIntentTopicMaxLen caps the topic length used as a search query so
// a runaway sentence does not turn into a multi-kilobyte regex search.
const channelIntentTopicMaxLen = 120

// envChannelIntentReply names the env var that toggles the optional system
// reply post-summary. Default OFF for PR 5 — the spec leaves the wiring in
// place but does not enable it. Set to "true" to opt in.
const envChannelIntentReply = "WUPHF_CHANNEL_INTENT_REPLY"

// channelIntentMatch is the classifier output. Topic is the noun phrase
// after the question opener; it is used verbatim as the substring search
// pattern across all notebook shelves.
type channelIntentMatch struct {
	Kind  channelIntentKind
	Topic string
}

// channelIntentPattern pairs a kind with a question-form regex that captures
// the topic in submatch 1. Order is significant: the first match wins.
type channelIntentPattern struct {
	Kind channelIntentKind
	Re   *regexp.Regexp
}

// channelIntentPatterns is the locked classifier set. Each pattern anchors
// at the start of the (trimmed, scrubbed) first line and requires an
// interrogative form. Statements with the same verbs ("I have context …",
// "we know …") do NOT match because the regex demands the question opener.
//
// Conservative by design: false positives flooding the demand index would
// distort scoring across the team. PR 5b (deferred) will add an LLM-based
// disambiguator if regex miss rate proves too high.
var channelIntentPatterns = []channelIntentPattern{
	// "who has (context|notes|docs|info|...) (on|about|for) X"
	// "who wrote/owns/tracks (context|notes|...) (on|about) X"
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*who\s+(?:has|wrote|owns|tracks)\s+(?:any\s+|the\s+|some\s+)?(?:context|info(?:rmation)?|notes?|docs?|details?|history|background|knowledge)\s+(?:on|about|for|regarding|re)\s+(.+?)\??\s*$`)},
	// "who knows about X" / "who remembers X" — bare verb + "about"; the
	// topic noun is implicit ("knowledge"). Only fires when the verb
	// directly governs "about" so "who knows the answer" does not match.
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*who\s+(?:knows|remembers|recalls)\s+(?:about|of|anything\s+about)\s+(.+?)\??\s*$`)},
	// "does anyone (know|remember|recall) (about|what) X"
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*does\s+anyone\s+(?:know|remember|recall)\s+(?:about|of)\s+(.+?)\??\s*$`)},
	// "does anyone know what we (decided|landed|did|did with) ... <topic>"
	// — the "what we decided about" form pulls the topic from after the
	// trailing preposition. Capture the tail and clean up "about|on|re"
	// at the front of the match in cleanChannelIntentTopic.
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*does\s+anyone\s+(?:know|remember|recall)\s+what\s+we\s+(?:decided|landed|agreed|got|did|chose|picked)\s+(?:on|about|for|with|regarding|re)\s+(.+?)\??\s*$`)},
	// "does anyone have (any|the|some) (context|notes|docs|info) (on|about) X"
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*does\s+anyone\s+have\s+(?:any\s+|the\s+|some\s+)?(?:context|info(?:rmation)?|notes?|docs?|details?|history|background)\s+(?:on|about|for|regarding|re)\s+(.+?)\??\s*$`)},
	// "anyone know about X" (informal — drops "does")
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*anyone\s+(?:know|remember|recall)\s+(?:about|of)\s+(.+?)\??\s*$`)},
	// "anyone have (context|notes|docs) on X"
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*anyone\s+have\s+(?:any\s+|the\s+|some\s+)?(?:context|info(?:rmation)?|notes?|docs?|details?|history|background)\s+(?:on|about|for|regarding|re)\s+(.+?)\??\s*$`)},
	// "what (do|did|have) we (decide|land|agree) (on|about) X"
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*what\s+(?:do|did|have)\s+we\s+(?:decide(?:d)?|land(?:ed)?|agree(?:d)?|chose|pick(?:ed)?)\s+(?:on|about|for|with|regarding|re)\s+(.+?)\??\s*$`)},
	// "what do we have on X" / "what do we know about X" / "what did we get on X"
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*what\s+(?:do\s+we|have\s+we|did\s+we)\s+(?:have|know|got)\s+(?:on|about|for|regarding|re)\s+(.+?)\??\s*$`)},
	// "where can I find (info|context|docs) (on|about) X"
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*where\s+can\s+(?:i|we)\s+find\s+(?:any\s+|the\s+|some\s+)?(?:context|info(?:rmation)?|notes?|docs?|details?|history|background)\s+(?:on|about|for|regarding|re)\s+(.+?)\??\s*$`)},
	// "do we have (notes|docs|context) on X"
	{Kind: ChannelIntentContextAsk, Re: regexp.MustCompile(`(?i)^\s*(?:do|did)\s+we\s+have\s+(?:any\s+|the\s+|some\s+)?(?:context|info(?:rmation)?|notes?|docs?|details?|history|background|knowledge)\s+(?:on|about|for|regarding|re)\s+(.+?)\??\s*$`)},
}

// classifyChannelIntent inspects body and returns (match, true) when a
// question-form context-ask matches. Otherwise (zero, false).
//
// Preprocessing:
//   - Strip fenced code blocks (``` … ```) and inline backticks; intent
//     phrases inside code do not count (mirrors PR 2's stripCodeFences).
//   - Take the FIRST line only. A user pasting a long log followed by a
//     question on line 30 should not flood the demand index.
//   - Drop URLs from the first line so "who has context on https://…" does
//     not treat the URL as the topic.
//
// Pure function: no locks, no I/O. Safe to call from any goroutine.
func classifyChannelIntent(body string) (channelIntentMatch, bool) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return channelIntentMatch{}, false
	}

	scrubbed := stripCodeFences(trimmed)
	scrubbed = stripInlineCode(scrubbed)
	scrubbed = strings.TrimSpace(scrubbed)
	if scrubbed == "" {
		return channelIntentMatch{}, false
	}

	firstLine := scrubbed
	if idx := strings.IndexByte(scrubbed, '\n'); idx >= 0 {
		firstLine = scrubbed[:idx]
	}
	firstLine = stripURLs(strings.TrimSpace(firstLine))
	firstLine = strings.TrimSpace(firstLine)
	if firstLine == "" {
		return channelIntentMatch{}, false
	}

	// Question-form heuristic: line must end with "?" OR start with one of
	// the recognised interrogative openers ("who", "does", "anyone",
	// "what", "where", "do we", "did we"). This shape filter is what makes
	// statements like "I have context on X" reliably miss.
	if !looksLikeContextAskQuestion(firstLine) {
		return channelIntentMatch{}, false
	}

	for _, pat := range channelIntentPatterns {
		m := pat.Re.FindStringSubmatch(firstLine)
		if m == nil {
			continue
		}
		topic := ""
		if len(m) >= 2 {
			topic = strings.TrimSpace(m[1])
		}
		topic = cleanChannelIntentTopic(topic)
		if !channelIntentTopicValid(topic) {
			return channelIntentMatch{}, false
		}
		return channelIntentMatch{
			Kind:  pat.Kind,
			Topic: topic,
		}, true
	}
	return channelIntentMatch{}, false
}

// channelIntentQuestionOpenerRe is a fast pre-filter: only lines whose first
// word is one of the recognised interrogative openers reach the regex set.
// This is the question-form gate that statements like "I have context on X"
// must fail.
var channelIntentQuestionOpenerRe = regexp.MustCompile(`(?i)^\s*(?:who|does|do|did|anyone|what|where)\b`)

// looksLikeContextAskQuestion returns true when line is plausibly an
// interrogative context-ask. The trailing "?" is NOT required — many users
// drop it in chat — but the leading interrogative opener IS required.
func looksLikeContextAskQuestion(line string) bool {
	return channelIntentQuestionOpenerRe.MatchString(line)
}

// channelIntentURLRe strips http(s) URLs from the topic line. Email
// addresses and bare hostnames are left in place — those are valid topic
// fragments ("who has context on stripe.com integration").
var channelIntentURLRe = regexp.MustCompile(`https?://\S+`)

func stripURLs(s string) string {
	return channelIntentURLRe.ReplaceAllString(s, " ")
}

// cleanChannelIntentTopic trims trailing punctuation and collapses
// whitespace so the resulting search query is stable.
func cleanChannelIntentTopic(topic string) string {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return ""
	}
	// Drop a trailing question mark / period / exclamation that survived
	// the regex.
	topic = strings.TrimRight(topic, "?.!,;:")
	topic = strings.TrimSpace(topic)
	// Collapse runs of whitespace.
	topic = strings.Join(strings.Fields(topic), " ")
	if len(topic) > channelIntentTopicMaxLen {
		// Cut at a UTF-8 boundary.
		n := channelIntentTopicMaxLen
		for n > 0 && (topic[n]&0xC0) == 0x80 {
			n--
		}
		topic = strings.TrimSpace(topic[:n])
	}
	return topic
}

// channelIntentTopicValid enforces the minimum-tokens guard. Single-word
// topics ("X") would match too broadly and feed the demand index with
// spurious cross-agent signals.
func channelIntentTopicValid(topic string) bool {
	if topic == "" {
		return false
	}
	tokens := strings.Fields(topic)
	return len(tokens) >= channelIntentTopicMinTokens
}

// channelIntentBrokerClient is the slice of *Broker the dispatcher needs.
// Kept as an interface so unit tests can substitute a fake without spinning
// the real wiki worker / demand index / system message poster.
type channelIntentBrokerClient interface {
	// NotebookSearchAll runs a substring search across every notebook shelf
	// (slug=all). Returns one merged hit slice plus a parallel slice of
	// owner slugs aligned with hits — i.e. ownerSlugs[i] is the owner of
	// hits[i]. Errors are logged by the caller; an error short-circuits
	// the dispatch for one message without crashing the goroutine.
	NotebookSearchAll(ctx context.Context, query string) (hits []WikiSearchHit, ownerSlugs []string, err error)
	// RecordDemandSignal funnels a PromotionDemandEvent into the demand
	// index. No-op when the index is nil.
	RecordDemandSignal(evt PromotionDemandEvent) error
	// PostSystemMessage is the optional reply path. Only invoked when
	// envChannelIntentReply is "true".
	PostSystemMessage(channel, content, kind string)
}

// ChannelIntentDispatcher ingests channel messages, classifies them, and
// fires the demand-signal recording for cross-agent notebook hits.
// Lifecycle mirrors AutoNotebookWriter: New → Start(ctx) → Stop(timeout).
// Safe for concurrent Handle() callers.
type ChannelIntentDispatcher struct {
	client channelIntentBrokerClient
	queue  chan channelMessage
	// stopCh signals shutdown without closing w.queue. See PR 1's
	// auto_notebook_writer for the rationale (close-vs-stopCh race).
	stopCh chan struct{}

	// progressMu + progressCond drive WaitForCondition for tests. Same
	// pattern as AutoNotebookWriter / HumanWikiIntentWriter.
	progressMu   sync.Mutex
	progressCond *sync.Cond

	running atomic.Bool
	done    chan struct{}

	// replyEnabled mirrors envChannelIntentReply at start time so the
	// dispatcher's behaviour is locked once it begins. Defaults to false.
	replyEnabled bool

	// counters
	enqueued     atomic.Int64
	classified   atomic.Int64 // matched the regex
	skipped      atomic.Int64 // no intent match
	searched     atomic.Int64 // ran NotebookSearchAll at least once
	hitsFound    atomic.Int64 // number of (path, owner) pairs recorded
	demandFired  atomic.Int64 // RecordDemandSignal succeeded
	searchFailed atomic.Int64
	recordFailed atomic.Int64
	queueSat     atomic.Int64
	repliesSent  atomic.Int64
}

// ChannelIntentCounters is a thread-safe snapshot of the dispatcher's
// observability counters.
type ChannelIntentCounters struct {
	Enqueued     int64
	Classified   int64
	Skipped      int64
	Searched     int64
	HitsFound    int64
	DemandFired  int64
	SearchFailed int64
	RecordFailed int64
	QueueSat     int64
	RepliesSent  int64
}

// NewChannelIntentDispatcher constructs an idle dispatcher. nil client is
// safe but disables the search + record path (counters still advance up to
// classification).
func NewChannelIntentDispatcher(client channelIntentBrokerClient) *ChannelIntentDispatcher {
	d := &ChannelIntentDispatcher{
		client: client,
		queue:  make(chan channelMessage, channelIntentQueueSize),
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	d.progressCond = sync.NewCond(&d.progressMu)
	d.replyEnabled = strings.EqualFold(strings.TrimSpace(os.Getenv(envChannelIntentReply)), "true")
	return d
}

// Start launches the drain goroutine. Idempotent.
func (d *ChannelIntentDispatcher) Start(ctx context.Context) {
	if d == nil {
		return
	}
	if d.running.Swap(true) {
		return
	}
	go d.run(ctx)
}

// Stop signals the drain goroutine and waits up to timeout. Idempotent.
// Mirrors AutoNotebookWriter.Stop — closes stopCh, never the queue, and
// wakes any test goroutine parked on progressCond.
func (d *ChannelIntentDispatcher) Stop(timeout time.Duration) {
	if d == nil || !d.running.Swap(false) {
		return
	}
	close(d.stopCh)
	d.progressMu.Lock()
	d.progressCond.Broadcast()
	d.progressMu.Unlock()
	if timeout <= 0 {
		<-d.done
		return
	}
	select {
	case <-d.done:
	case <-time.After(timeout):
	}
}

// Handle is the broker-side ingress. Non-blocking enqueue. Drops with a
// counter increment on saturation. The classifier is intentionally NOT
// called here — it runs inside the dispatcher goroutine so the hot path
// stays a single channel send.
//
// The hook fires for ALL senders (human or agent). The classifier's
// question-form filter is what restricts demand recording to genuine
// context-asks; sender role is not a useful pre-filter because agents can
// also ask each other for context.
func (d *ChannelIntentDispatcher) Handle(msg channelMessage) {
	if d == nil || !d.running.Load() {
		return
	}
	if strings.TrimSpace(msg.Content) == "" {
		return
	}
	select {
	case <-d.stopCh:
		return
	default:
	}
	select {
	case <-d.stopCh:
	case d.queue <- msg:
		d.enqueued.Add(1)
	default:
		d.queueSat.Add(1)
		log.Printf("channel_intent_dispatcher: queue saturated, dropping msg id=%s from=%s", msg.ID, msg.From)
		d.signalProgress()
	}
}

// signalProgress wakes any goroutine parked in WaitForCondition. Cheap.
func (d *ChannelIntentDispatcher) signalProgress() {
	d.progressMu.Lock()
	d.progressCond.Broadcast()
	d.progressMu.Unlock()
}

// WaitForCondition blocks until predicate returns true, ctx cancels, or the
// dispatcher stops. Test-only.
func (d *ChannelIntentDispatcher) WaitForCondition(ctx context.Context, predicate func() bool) error {
	if d == nil {
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
			d.progressMu.Lock()
			d.progressCond.Broadcast()
			d.progressMu.Unlock()
		case <-cancelWatcher:
		}
	}()
	d.progressMu.Lock()
	defer d.progressMu.Unlock()
	for !predicate() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !d.running.Load() {
			if predicate() {
				return nil
			}
			return ErrWorkerStopped
		}
		d.progressCond.Wait()
	}
	return nil
}

// Counters returns a thread-safe snapshot.
func (d *ChannelIntentDispatcher) Counters() ChannelIntentCounters {
	if d == nil {
		return ChannelIntentCounters{}
	}
	return ChannelIntentCounters{
		Enqueued:     d.enqueued.Load(),
		Classified:   d.classified.Load(),
		Skipped:      d.skipped.Load(),
		Searched:     d.searched.Load(),
		HitsFound:    d.hitsFound.Load(),
		DemandFired:  d.demandFired.Load(),
		SearchFailed: d.searchFailed.Load(),
		RecordFailed: d.recordFailed.Load(),
		QueueSat:     d.queueSat.Load(),
		RepliesSent:  d.repliesSent.Load(),
	}
}

func (d *ChannelIntentDispatcher) run(ctx context.Context) {
	defer close(d.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case msg := <-d.queue:
			d.process(ctx, msg)
		}
	}
}

// process classifies one message and, on a match, performs the search +
// demand-recording. Errors are logged + counter-incremented but never
// surface to the caller — the dispatcher must stay alive across single
// failures.
func (d *ChannelIntentDispatcher) process(ctx context.Context, msg channelMessage) {
	defer d.signalProgress()

	match, ok := classifyChannelIntent(msg.Content)
	if !ok {
		d.skipped.Add(1)
		return
	}
	d.classified.Add(1)

	if d.client == nil {
		// No client wired (test harness or partial init). We classified
		// the intent but cannot search; bail before counting a search.
		return
	}

	searchCtx, cancel := context.WithTimeout(ctx, channelIntentSearchTimeout)
	defer cancel()

	d.searched.Add(1)
	hits, owners, err := d.client.NotebookSearchAll(searchCtx, match.Topic)
	if err != nil {
		d.searchFailed.Add(1)
		log.Printf("channel_intent_dispatcher: NotebookSearchAll failed topic=%q: %v", match.Topic, err)
		return
	}
	if len(hits) == 0 {
		// Zero hits: classified an intent but no notebook entry answered.
		// Do not record any demand event; the entry doesn't exist yet.
		return
	}

	// Dedupe (entryPath, ownerSlug) pairs so a single search returning
	// multiple line-level hits on the same file fires one demand event.
	type pairKey struct {
		path  string
		owner string
	}
	seen := map[pairKey]struct{}{}
	now := time.Now().UTC()
	channel := strings.TrimSpace(msg.Channel)
	if channel == "" {
		channel = "general"
	}
	// SearcherSlug records the message author so the demand index can dedupe
	// "same searcher same entry same day". For human senders this groups
	// repeated questions from one person under one signal per day. For
	// agent senders it does the same per agent.
	searcher := strings.TrimSpace(msg.From)
	if searcher == "" {
		searcher = "channel"
	}

	for i, h := range hits {
		owner := ""
		if i < len(owners) {
			owner = strings.TrimSpace(owners[i])
		}
		path := strings.TrimSpace(h.Path)
		if path == "" || owner == "" {
			continue
		}
		// Self-search guard: if the searcher is the same as the entry
		// owner, this is the agent finding its own notebook. Not a
		// cross-agent signal.
		if owner == searcher {
			continue
		}
		key := pairKey{path: path, owner: owner}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		evt := PromotionDemandEvent{
			EntryPath:    path,
			OwnerSlug:    owner,
			SearcherSlug: searcher,
			Signal:       DemandSignalChannelContextAsk,
			RecordedAt:   now,
		}
		if err := d.client.RecordDemandSignal(evt); err != nil {
			d.recordFailed.Add(1)
			log.Printf("channel_intent_dispatcher: RecordDemandSignal failed path=%s: %v", path, err)
			continue
		}
		d.hitsFound.Add(1)
		d.demandFired.Add(1)
	}

	// Optional system reply path. OFF by default in PR 5; the env flag
	// keeps the wiring discoverable for the demand-signals operator runbook
	// without exposing the surface to users until the copy is reviewed.
	if d.replyEnabled && d.client != nil && len(seen) > 0 && isHumanMessageSender(msg.From) {
		summary := renderChannelIntentReply(match.Topic, hits, owners)
		if summary != "" {
			d.client.PostSystemMessage(channel, summary, "channel_intent_context")
			d.repliesSent.Add(1)
		}
	}
}

// renderChannelIntentReply produces a short system-message summary for the
// optional reply path. Format is intentionally terse: one line per hit,
// owner-slug + path. The entry is not quoted in full — the demand index
// is the durable record; the reply is just a pointer.
func renderChannelIntentReply(topic string, hits []WikiSearchHit, owners []string) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Found context on ")
	b.WriteString(strings.TrimSpace(topic))
	b.WriteString(":\n")
	limit := len(hits)
	if limit > 5 {
		limit = 5
	}
	for i := 0; i < limit; i++ {
		owner := ""
		if i < len(owners) {
			owner = strings.TrimSpace(owners[i])
		}
		if owner == "" {
			owner = "agent"
		}
		b.WriteString("- @")
		b.WriteString(owner)
		b.WriteString(": ")
		b.WriteString(hits[i].Path)
		b.WriteString("\n")
	}
	return b.String()
}

// dispatchChannelIntentAsync is the broker-side helper invoked from
// handlePostMessage / PostMessage AFTER b.mu.Unlock(). nil-safe: a missing
// dispatcher is a silent no-op so PR 5 can revert by clearing
// b.channelIntentDispatcher without breaking message posting.
//
// Lock invariant: caller MUST NOT hold b.mu. The dispatcher's Handle is
// non-blocking and its goroutine never re-enters b.mu.
func (b *Broker) dispatchChannelIntentAsync(msg channelMessage) {
	if b == nil {
		return
	}
	d := b.channelIntentDispatcher
	if d == nil {
		return
	}
	d.Handle(msg)
}

// NotebookSearchAll runs a substring search across every notebook shelf
// (slug=all) and returns the merged hits with a parallel slice of owner
// slugs. This is the broker-side adapter for ChannelIntentDispatcher and
// any future caller that needs the same cross-shelf rollup outside the
// HTTP path.
//
// Lock invariant: this method does NOT acquire b.mu. It calls
// notebookSearchSlugs (which itself calls b.OfficeMembers and so does
// briefly take b.mu), then iterates worker.NotebookSearch — both are
// safe to invoke from a goroutine.
func (b *Broker) NotebookSearchAll(_ context.Context, query string) ([]WikiSearchHit, []string, error) {
	if b == nil {
		return nil, nil, nil
	}
	worker := b.WikiWorker()
	if worker == nil {
		return nil, nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil, nil
	}
	slugs, err := b.notebookSearchSlugs(worker)
	if err != nil {
		return nil, nil, err
	}
	var hits []WikiSearchHit
	var owners []string
	for _, slug := range slugs {
		slugHits, err := worker.NotebookSearch(slug, query)
		if err != nil {
			// Skip per-slug failures rather than abort the whole sweep.
			// A single corrupt shelf must not silence cross-agent demand.
			log.Printf("channel_intent: NotebookSearch slug=%s failed: %v", slug, err)
			continue
		}
		for _, h := range slugHits {
			hits = append(hits, h)
			owners = append(owners, slug)
		}
	}
	return hits, owners, nil
}

// RecordDemandSignal funnels an event into the demand index. nil-safe:
// when the index is not wired the call is a silent no-op so the dispatcher
// can run on a partially-initialised broker without crashing.
func (b *Broker) RecordDemandSignal(evt PromotionDemandEvent) error {
	if b == nil || b.demandIndex == nil {
		return nil
	}
	return b.demandIndex.Record(evt)
}
