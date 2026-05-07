package team

import (
	"context"
	"strings"
	"sync"
	"time"
)

const (
	agentStreamHistoryLimit     = 2000
	agentStreamTaskHistoryLimit = 2000
	agentStreamTaskBufferLimit  = 64
)

type agentStreamBuffer struct {
	mu        sync.Mutex
	lines     []agentStreamLine
	taskLines map[string][]string
	taskOrder []string
	subs      map[int]agentStreamSubscriber
	nextID    int
}

type agentStreamLine struct {
	TaskID string
	Text   string
}

type agentStreamSubscriber struct {
	TaskID string
	Ch     chan string
}

func (s *agentStreamBuffer) Push(line string) {
	s.PushTask("", line)
}

func (s *agentStreamBuffer) PushTask(taskID, line string) {
	taskID = strings.TrimSpace(taskID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, agentStreamLine{
		TaskID: taskID,
		Text:   line,
	})
	if len(s.lines) > agentStreamHistoryLimit {
		s.lines = s.lines[len(s.lines)-agentStreamHistoryLimit:]
	}
	if taskID != "" {
		s.pushTaskHistoryLocked(taskID, line)
	}
	for _, sub := range s.subs {
		if sub.TaskID != "" && sub.TaskID != taskID {
			continue
		}
		select {
		case sub.Ch <- line:
		default:
		}
	}
}

func (s *agentStreamBuffer) pushTaskHistoryLocked(taskID, line string) {
	if s.taskLines == nil {
		s.taskLines = make(map[string][]string)
	}
	if _, ok := s.taskLines[taskID]; !ok {
		s.taskOrder = append(s.taskOrder, taskID)
		for len(s.taskOrder) > agentStreamTaskBufferLimit {
			evict := s.taskOrder[0]
			s.taskOrder = s.taskOrder[1:]
			delete(s.taskLines, evict)
		}
	}
	lines := append(s.taskLines[taskID], line)
	if len(lines) > agentStreamTaskHistoryLimit {
		lines = lines[len(lines)-agentStreamTaskHistoryLimit:]
	}
	s.taskLines[taskID] = lines
}

func (s *agentStreamBuffer) subscribe() (<-chan string, func()) {
	return s.subscribeTask("")
}

func (s *agentStreamBuffer) subscribeTask(taskID string) (<-chan string, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.subscribeTaskLocked(taskID)
}

func (s *agentStreamBuffer) subscribeTaskWithRecent(taskID string) ([]string, <-chan string, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, unsubscribe := s.subscribeTaskLocked(taskID)
	return s.recentTaskLocked(taskID), ch, unsubscribe
}

func (s *agentStreamBuffer) subscribeTaskLocked(taskID string) (<-chan string, func()) {
	id := s.nextID
	s.nextID++
	taskID = strings.TrimSpace(taskID)
	ch := make(chan string, 128)
	s.subs[id] = agentStreamSubscriber{TaskID: taskID, Ch: ch}
	return ch, func() {
		// Close the channel after removing it from the map so a
		// consumer blocked on `<-ch` is unparked instead of leaking.
		// Matches the pattern used by Broker.SubscribeMessages and
		// friends. Push() never sends after delete(), so closing here
		// is race-free.
		s.mu.Lock()
		if existing, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(existing.Ch)
		}
		s.mu.Unlock()
	}
}

func (s *agentStreamBuffer) recent() []string {
	return s.recentTask("")
}

func (s *agentStreamBuffer) recentTask(taskID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recentTaskLocked(taskID)
}

func (s *agentStreamBuffer) recentTaskLocked(taskID string) []string {
	taskID = strings.TrimSpace(taskID)
	if taskID != "" {
		lines := s.taskLines[taskID]
		out := make([]string, len(lines))
		copy(out, lines)
		return out
	}
	out := make([]string, 0, len(s.lines))
	for _, line := range s.lines {
		out = append(out, line.Text)
	}
	return out
}

// AgentStream returns (or lazily creates) the stream buffer for a given agent slug.
// It is safe to call concurrently.
func (b *Broker) AgentStream(slug string) *agentStreamBuffer {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.agentStreams == nil {
		b.agentStreams = make(map[string]*agentStreamBuffer)
	}
	s, ok := b.agentStreams[slug]
	if !ok {
		s = &agentStreamBuffer{
			taskLines: make(map[string][]string),
			subs:      make(map[int]agentStreamSubscriber),
		}
		b.agentStreams[slug] = s
	}
	return s
}

func (l *Launcher) agentActiveTaskID(slug string) string {
	if l == nil {
		return ""
	}
	task := l.agentActiveTask(slug)
	if task == nil {
		return ""
	}
	return strings.TrimSpace(task.ID)
}

func (b *Broker) SubscribeMessages(buffer int) (<-chan channelMessage, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan channelMessage, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.messageSubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.messageSubscribers[id]; ok {
			delete(b.messageSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

func (b *Broker) SubscribeActions(buffer int) (<-chan officeActionLog, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan officeActionLog, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.actionSubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.actionSubscribers[id]; ok {
			delete(b.actionSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

func (b *Broker) SubscribeActivity(buffer int) (<-chan agentActivitySnapshot, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan agentActivitySnapshot, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.activitySubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.activitySubscribers[id]; ok {
			delete(b.activitySubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

type officeChangeEvent struct {
	Kind string `json:"kind"` // "member_created", "member_removed", "channel_created", "channel_removed", "channel_updated", "office_reseeded"
	Slug string `json:"slug"`
}

func (b *Broker) SubscribeOfficeChanges(buffer int) (<-chan officeChangeEvent, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan officeChangeEvent, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.officeSubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.officeSubscribers[id]; ok {
			delete(b.officeSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

func (b *Broker) publishOfficeChangeLocked(evt officeChangeEvent) {
	for _, ch := range b.officeSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// SubscribeWikiEvents returns a channel of wiki commit notifications plus an
// unsubscribe func. The web UI's SSE loop uses this to push "wiki:write"
// events to the browser.
func (b *Broker) SubscribeWikiEvents(buffer int) (<-chan wikiWriteEvent, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan wikiWriteEvent, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.wikiSubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.wikiSubscribers[id]; ok {
			delete(b.wikiSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// PublishWikiEvent fans out a commit notification to all SSE subscribers.
// Implements the wikiEventPublisher interface consumed by WikiWorker.
func (b *Broker) PublishWikiEvent(evt wikiWriteEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.wikiSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// WikiWorker returns the broker's attached wiki worker, or nil when the
// active memory backend is not markdown.
func (b *Broker) WikiWorker() *WikiWorker {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wikiWorker
}

// WikiIndex returns the broker's derived wiki index, or nil when the active
// memory backend is not markdown. HTTP handlers use this to run search queries
// against the structured fact store without going through the write worker.
func (b *Broker) WikiIndex() *WikiIndex {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wikiIndex
}

func (b *Broker) UpdateAgentActivity(update agentActivitySnapshot) {
	slug := normalizeChannelSlug(update.Slug)
	if slug == "" {
		return
	}
	if update.LastTime == "" {
		update.LastTime = time.Now().UTC().Format(time.RFC3339)
	}
	update.Slug = slug

	b.mu.Lock()
	current := b.activity[slug]
	current.Slug = slug
	if update.Status != "" {
		current.Status = update.Status
	}
	if update.Activity != "" {
		current.Activity = update.Activity
	}
	if update.Detail != "" {
		current.Detail = update.Detail
	}
	if update.LastTime != "" {
		current.LastTime = update.LastTime
	}
	if update.TotalMs > 0 {
		current.TotalMs = update.TotalMs
	}
	// Latency metrics: only overwrite when the caller actually supplied a
	// value. The previous `>= 0` guard treated unset zero-value fields the
	// same as a real "0 ms" measurement and would wipe a previously
	// recorded latency on every status-only update. A literal 0 ms first
	// event isn't meaningful in practice (we measure user→first-stream-
	// chunk latency) so `> 0` is a safe sentinel.
	if update.FirstEventMs > 0 {
		current.FirstEventMs = update.FirstEventMs
	}
	if update.FirstTextMs > 0 {
		current.FirstTextMs = update.FirstTextMs
	}
	if update.FirstToolMs > 0 {
		current.FirstToolMs = update.FirstToolMs
	}
	// Kind is set per-event by the classifier in headless_progress.go, or
	// explicitly by the reaper / watchdog hooks for "stuck". An empty Kind
	// here means the caller did not classify (e.g. legacy code paths) — we
	// leave the previous Kind in place rather than wiping it, so a stuck
	// agent does not silently flip back to routine on a status-only update
	// that omits Kind.
	if update.Kind != "" {
		current.Kind = update.Kind
	}
	b.activity[slug] = current
	b.publishActivityLocked(current)
	b.mu.Unlock()
}

// duplicateBroadcastWindow is how recent an earlier broadcast from the same
// agent in the same channel+thread must be to count as a duplicate. Set tight
// so legitimate quick follow-ups still land, but tight enough to catch the
// "same turn, paraphrased again" pattern that agents produce.
const duplicateBroadcastWindow = 30 * time.Second

// duplicateBroadcastSimilarity is the lower bound at which two messages are
// considered near-duplicates. 1.0 means "byte-identical"; we pick 0.85 to
// catch paraphrased restatements while letting actual new content through.
const duplicateBroadcastSimilarity = 0.85

// isDuplicateAgentBroadcastLocked returns true when the agent has already
// posted a nearly-identical message to the same (channel, thread) pair within
// duplicateBroadcastWindow. Must be called with b.mu held.
func (b *Broker) isDuplicateAgentBroadcastLocked(sender, channel, replyTo, content string) bool {
	newNorm := normalizeBroadcastContent(content)
	if newNorm == "" {
		return false
	}
	cutoff := time.Now().UTC().Add(-duplicateBroadcastWindow)
	// Walk backwards — most recent messages are at the end.
	for i := len(b.messages) - 1; i >= 0; i-- {
		prev := b.messages[i]
		ts, err := time.Parse(time.RFC3339, prev.Timestamp)
		if err == nil && ts.Before(cutoff) {
			break
		}
		if prev.From != sender {
			continue
		}
		if normalizeChannelSlug(prev.Channel) != channel {
			continue
		}
		if strings.TrimSpace(prev.ReplyTo) != strings.TrimSpace(replyTo) {
			continue
		}
		prevNorm := normalizeBroadcastContent(prev.Content)
		if prevNorm == "" {
			continue
		}
		if jaccardWordSimilarity(newNorm, prevNorm) >= duplicateBroadcastSimilarity {
			return true
		}
	}
	return false
}

// normalizeBroadcastContent lowercases and collapses whitespace so trivial
// formatting drift does not defeat the dedup check.
func normalizeBroadcastContent(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

// jaccardWordSimilarity returns the Jaccard similarity of the two strings'
// whitespace-split word sets. 1.0 = identical word sets; 0.0 = disjoint.
// Cheap and good enough to catch "ship it" / "ship it 🚀" style paraphrases.
func jaccardWordSimilarity(a, b string) float64 {
	wa := uniqueWordSet(a)
	wb := uniqueWordSet(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	inter := 0
	for w := range wa {
		if _, ok := wb[w]; ok {
			inter++
		}
	}
	union := len(wa) + len(wb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func uniqueWordSet(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, w := range strings.Fields(s) {
		// Strip leading/trailing ASCII punctuation so "court," and "court"
		// collapse to the same token for Jaccard. Keeps intra-word characters
		// like apostrophes inside "reviewer's".
		w = strings.TrimFunc(w, func(r rune) bool {
			switch r {
			case '.', ',', ';', ':', '!', '?', '—', '–', '-', '"', '\'', '(', ')', '[', ']', '`':
				return true
			}
			return false
		})
		if w == "" {
			continue
		}
		out[w] = struct{}{}
	}
	return out
}

// activityWatchdogEnabled controls whether NewBroker starts the background
// activity-watchdog goroutine. Tests that create many short-lived brokers set
// this to false via TestMain so goroutines don't accumulate and cause
// goleak/timeout failures. Production always runs with the default (true).
var activityWatchdogEnabled = true

// staleActivityThreshold is how long an agent can stay in a non-idle/non-error
// activity state before the watchdog forcibly resets it to idle. Set long
// enough to cover normal long turns (tool chains, big edits) but short enough
// that a crashed spawn does not leave the agent looking "active" for hours —
// which blocks the CEO's "Already active in this thread" re-route guard and
// prevents the specialist from being dispatched again.
const staleActivityThreshold = 5 * time.Minute

// stuckThresholdSeconds is how long an active agent can go without any new
// activity event before the reaper marks it Kind="stuck" (without changing
// Status — the next real event clears the stuck flag). Far below
// staleActivityThreshold so the human gets a stuck signal in the office well
// before the safety reset to idle fires. See ICP tutorial 2 (Marcus): 90s is
// calibrated for "long terraform plan stalled on remote-state lock" rhythm.
//
// TODO: tune from dogfood — start at 90s, measure false-positive rate.
const stuckThresholdSeconds = 90

// reapStaleActivityLocked walks the activity map and emits two kinds of
// follow-up snapshots when an agent's LastTime has aged past a threshold:
//
//  1. Stuck-while-active (>= stuckThresholdSeconds, < staleActivityThreshold):
//     the agent is still nominally "active"/"thinking"/"tool_use" but has gone
//     quiet long enough that the human in the office should be alerted. We
//     stamp Kind="stuck" without changing Status — the next real event from
//     the agent flips Kind back to whatever the classifier returns. Emitted
//     once per stuck transition (re-emitting every reaper tick would spam the
//     SSE stream and re-trigger frontend assertive announcements).
//
//  2. Stale-active reset (>= staleActivityThreshold): the long-standing
//     safety net. Treat the agent as crashed and force it back to idle so
//     the CEO's "Already active in this thread" re-route guard releases.
//
// Must be called with b.mu held. Returns the snapshots that need to be
// published to subscribers after the caller releases the lock.
func (b *Broker) reapStaleActivityLocked(now time.Time) []agentActivitySnapshot {
	if len(b.activity) == 0 {
		return nil
	}
	stuckThreshold := time.Duration(stuckThresholdSeconds) * time.Second
	var reset []agentActivitySnapshot
	for slug, snap := range b.activity {
		status := strings.ToLower(strings.TrimSpace(snap.Status))
		if status == "" || status == "idle" || status == "error" {
			continue
		}
		lastTime, err := time.Parse(time.RFC3339, snap.LastTime)
		if err != nil {
			// Unparseable LastTime means we cannot age the entry safely; leave it.
			continue
		}
		age := now.Sub(lastTime)
		switch {
		case age >= staleActivityThreshold:
			snap.Status = "idle"
			snap.Activity = "idle"
			snap.Detail = "stale activity reaped (no progress for " + staleActivityThreshold.String() + ")"
			snap.LastTime = now.UTC().Format(time.RFC3339)
			// Reaping back to idle clears any prior stuck flag — the
			// agent is no longer claiming to be working.
			snap.Kind = "routine"
			b.activity[slug] = snap
			reset = append(reset, snap)
		case age >= stuckThreshold && snap.Kind != "stuck":
			// First time crossing the stuck line: emit a stuck snapshot
			// without otherwise mutating Status/Activity/Detail. The
			// frontend uses Kind=="stuck" to escalate the pill chrome
			// and fire the assertive aria-live announcement.
			snap.Kind = "stuck"
			b.activity[slug] = snap
			reset = append(reset, snap)
		}
	}
	return reset
}

// runActivityWatchdog scans the in-memory activity map every minute and
// resets agents that have been stuck in a non-terminal state past
// staleActivityThreshold. Stops when ctx is done so NewBroker can tear it
// down alongside the rest of the broker's lifecycle.
func (b *Broker) runActivityWatchdog(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			b.mu.Lock()
			reset := b.reapStaleActivityLocked(now)
			for _, snap := range reset {
				b.publishActivityLocked(snap)
			}
			b.mu.Unlock()
		}
	}
}
