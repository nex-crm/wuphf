package team

// broker_chat_digest.go is S2 feeder 3: a deterministic, LLM-free daily sweep
// that turns meaningful chat threads into immutable source records (kind=chat).
//
// Each tick briefly locks b.mu to snapshot the in-memory chat log, RELEASES the
// lock, groups messages into (channel, time-window) buckets, keeps only the
// "meaningful" ones (a real conversation, not system noise or single-message
// chatter), renders one markdown digest per bucket, and routes each through the
// non-blocking capture dispatcher. Re-snapshotting an unchanged window is a
// write-once no-op (origin = "channel:window-label").

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	// defaultChatDigestInterval is how often the sweep runs. Overridable with
	// WUPHF_CHAT_DIGEST_INTERVAL ("0"/"disabled" turns the loop off).
	defaultChatDigestInterval = 24 * time.Hour
	// defaultChatDigestWindow is the bucket size threads are grouped into.
	// Overridable with WUPHF_CHAT_DIGEST_WINDOW.
	defaultChatDigestWindow = 24 * time.Hour
	// chatDigestMaxBodyChars caps each rendered message line so a runaway paste
	// cannot bloat a source record.
	chatDigestMaxBodyChars = 500
)

// chatDigestMessage is the off-lock snapshot of one chat message. Only the
// fields the digest needs are copied, so b.mu is released before any rendering
// or capture work.
type chatDigestMessage struct {
	ID        string
	From      string
	Channel   string
	Kind      string
	Content   string
	ReplyTo   string
	Timestamp time.Time
	Redacted  bool
}

func chatDigestIntervalFromEnv() time.Duration {
	return chatDigestDurationFromEnv("WUPHF_CHAT_DIGEST_INTERVAL", defaultChatDigestInterval)
}

func chatDigestWindowFromEnv() time.Duration {
	d := chatDigestDurationFromEnv("WUPHF_CHAT_DIGEST_WINDOW", defaultChatDigestWindow)
	if d <= 0 {
		return defaultChatDigestWindow
	}
	return d
}

// chatDigestDurationFromEnv parses a duration env var. Empty → fallback;
// "0"/"disabled" → 0 (caller decides what 0 means); unparseable → fallback.
func chatDigestDurationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	if raw == "0" || raw == "disabled" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < 0 {
		log.Printf("chat_digest: invalid %s %q, using default %s", key, raw, fallback)
		return fallback
	}
	return d
}

// startChatDigestLoop launches the digest ticker. A non-positive interval
// (WUPHF_CHAT_DIGEST_INTERVAL=0/disabled) leaves the loop unstarted.
func (b *Broker) startChatDigestLoop(ctx context.Context) {
	interval := chatDigestIntervalFromEnv()
	if interval <= 0 {
		return
	}
	go b.runChatDigest(ctx, interval)
}

func (b *Broker) runChatDigest(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.sweepChatDigests(time.Now().UTC())
		}
	}
}

// sweepChatDigests snapshots the chat log under b.mu, RELEASES the lock, then
// renders + captures one digest per meaningful (channel, window). No worker
// call happens under the lock — captureSource is a non-blocking buffered send.
func (b *Broker) sweepChatDigests(now time.Time) {
	_ = now // reserved: a future cadence could window relative to "now".
	window := chatDigestWindowFromEnv()

	b.mu.Lock()
	snapshot := make([]chatDigestMessage, 0, len(b.messages))
	for _, m := range b.messages {
		ts, err := time.Parse(time.RFC3339, m.Timestamp)
		if err != nil {
			continue // an undated message can't be windowed
		}
		snapshot = append(snapshot, chatDigestMessage{
			ID:        m.ID,
			From:      m.From,
			Channel:   normalizeChannelSlug(m.Channel),
			Kind:      m.Kind,
			Content:   m.Content,
			ReplyTo:   m.ReplyTo,
			Timestamp: ts.UTC(),
			Redacted:  m.Redacted,
		})
	}
	b.mu.Unlock()

	for _, job := range buildChatDigestJobs(snapshot, window) {
		b.captureSource(job)
	}
}

// chatDigestGroupKey identifies one (channel, time-window) bucket.
type chatDigestGroupKey struct {
	channel     string
	windowStart time.Time
}

// buildChatDigestJobs groups messages into (channel, window) buckets, keeps the
// meaningful threads, and renders one source-capture job per kept bucket. Pure
// function (no broker state) so it is unit-testable in isolation.
func buildChatDigestJobs(messages []chatDigestMessage, window time.Duration) []SourceCaptureJob {
	if window <= 0 {
		window = defaultChatDigestWindow
	}
	groups := map[chatDigestGroupKey][]chatDigestMessage{}
	for _, m := range messages {
		if strings.TrimSpace(m.Channel) == "" {
			m.Channel = "general"
		}
		key := chatDigestGroupKey{
			channel:     m.Channel,
			windowStart: m.Timestamp.Truncate(window).UTC(),
		}
		groups[key] = append(groups[key], m)
	}

	keys := make([]chatDigestGroupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	// Deterministic output: oldest window first, then channel alphabetical.
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].windowStart.Equal(keys[j].windowStart) {
			return keys[i].channel < keys[j].channel
		}
		return keys[i].windowStart.Before(keys[j].windowStart)
	})

	jobs := make([]SourceCaptureJob, 0, len(keys))
	for _, k := range keys {
		msgs := groups[k]
		sort.SliceStable(msgs, func(i, j int) bool {
			return msgs[i].Timestamp.Before(msgs[j].Timestamp)
		})
		if !chatDigestMeaningful(msgs) {
			continue
		}
		label := chatDigestWindowLabel(k.windowStart, window)
		title := fmt.Sprintf("#%s — %s", k.channel, label)
		origin := fmt.Sprintf("%s:%s", k.channel, label)
		content := renderChatDigest(k.channel, label, msgs)
		jobs = append(jobs, SourceCaptureJob{
			Kind:       SourceKindChat,
			ID:         DeriveSourceID(SourceKindChat, origin, title, content),
			Title:      title,
			Origin:     origin,
			Content:    content,
			CapturedAt: k.windowStart,
		})
	}
	return jobs
}

// chatDigestIsSystem reports whether a message is a pure system/auto post that
// should not, on its own, make a window "meaningful".
func chatDigestIsSystem(m chatDigestMessage) bool {
	from := strings.ToLower(strings.TrimSpace(m.From))
	return from == "" || from == "system"
}

// chatDigestMeaningful keeps a window only when, after dropping system/auto and
// empty posts, it holds at least two human/agent messages AND either two-plus
// of them are replies or they come from two-plus distinct participants.
func chatDigestMeaningful(msgs []chatDigestMessage) bool {
	participants := map[string]struct{}{}
	replies := 0
	human := 0
	for _, m := range msgs {
		if chatDigestIsSystem(m) || strings.TrimSpace(m.Content) == "" {
			continue
		}
		human++
		participants[strings.ToLower(strings.TrimSpace(m.From))] = struct{}{}
		if strings.TrimSpace(m.ReplyTo) != "" {
			replies++
		}
	}
	if human < 2 {
		return false // single-message chatter or a pure system window
	}
	return replies >= 2 || len(participants) >= 2
}

// chatDigestWindowLabel renders a stable, filesystem-friendly label for a
// bucket. Day-or-wider windows use the date; sub-day windows append the time so
// distinct buckets on the same date keep distinct origins.
func chatDigestWindowLabel(start time.Time, window time.Duration) string {
	if window >= 24*time.Hour {
		return start.UTC().Format("2006-01-02")
	}
	return start.UTC().Format("2006-01-02T15:04")
}

func renderChatDigest(channelSlug, label string, msgs []chatDigestMessage) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# #%s — %s\n\n", channelSlug, label))

	participants := map[string]struct{}{}
	shown := 0
	for _, m := range msgs {
		if chatDigestIsSystem(m) || strings.TrimSpace(m.Content) == "" {
			continue
		}
		participants[strings.TrimSpace(m.From)] = struct{}{}
		shown++
	}
	sb.WriteString(fmt.Sprintf("_%d messages from %d participants._\n\n", shown, len(participants)))

	for _, m := range msgs {
		if chatDigestIsSystem(m) || strings.TrimSpace(m.Content) == "" {
			continue
		}
		ts := m.Timestamp.UTC().Format("15:04")
		from := strings.TrimSpace(m.From)
		if from == "" {
			from = "unknown"
		}
		// Collapse newlines so each message stays on one markdown list line.
		body := strings.Join(strings.Fields(strings.TrimSpace(m.Content)), " ")
		body = chatDigestTruncate(body, chatDigestMaxBodyChars)
		if m.Redacted {
			body += " _(secrets redacted)_"
		}
		sb.WriteString(fmt.Sprintf("- `%s` **%s**: %s\n", ts, from, body))
	}
	return sb.String()
}

func chatDigestTruncate(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
