package team

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeNotebookClient records each NotebookWrite call. It is intentionally
// permissive — validation is the WikiWorker's job; here we test the writer's
// own logic (dedupe, redaction, roster, queue saturation, formatting).
type fakeNotebookClient struct {
	mu    sync.Mutex
	calls []fakeNotebookCall
	err   error
}

type fakeNotebookCall struct {
	Slug    string
	Path    string
	Content string
	Mode    string
}

func (f *fakeNotebookClient) NotebookWrite(_ context.Context, slug, path, content, mode, _ string) (string, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", 0, f.err
	}
	f.calls = append(f.calls, fakeNotebookCall{Slug: slug, Path: path, Content: content, Mode: mode})
	return "deadbeef", len(content), nil
}

func (f *fakeNotebookClient) snapshot() []fakeNotebookCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeNotebookCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// allowAllRoster mimics a broker where every slug is a registered agent.
type allowAllRoster struct{}

func (allowAllRoster) IsAgentMemberSlug(string) bool { return true }

// allowSetRoster gates membership on a small set of slugs.
type allowSetRoster struct {
	allowed map[string]struct{}
}

func (r allowSetRoster) IsAgentMemberSlug(slug string) bool {
	_, ok := r.allowed[strings.TrimSpace(slug)]
	return ok
}

func startWriter(t *testing.T, client autoNotebookWriterClient, roster autoNotebookRoster) *AutoNotebookWriter {
	t.Helper()
	w := NewAutoNotebookWriter(client, roster)
	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)
	t.Cleanup(func() {
		cancel()
		w.Stop(2 * time.Second)
	})
	return w
}

func waitForCalls(t *testing.T, client *fakeNotebookClient, n int) []fakeNotebookCall {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(client.snapshot()) >= n {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	calls := client.snapshot()
	if len(calls) < n {
		t.Fatalf("expected at least %d calls, got %d", n, len(calls))
	}
	return calls
}

func waitForCounter(t *testing.T, get func() int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if get() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("counter never reached %d (got %d)", want, get())
}

func TestAutoNotebookWriter_HandleEnqueuesAndWrites(t *testing.T) {
	client := &fakeNotebookClient{}
	w := startWriter(t, client, allowAllRoster{})

	w.Handle(autoNotebookEvent{
		Kind:      AutoNotebookEventMessagePosted,
		Slug:      "ceo",
		Actor:     "ceo",
		Channel:   "general",
		Content:   "hello team",
		Timestamp: time.Date(2026, 5, 5, 13, 14, 15, 0, time.UTC),
	})

	calls := waitForCalls(t, client, 1)
	if calls[0].Slug != "ceo" {
		t.Fatalf("slug: got %q want ceo", calls[0].Slug)
	}
	if calls[0].Mode != "create" {
		t.Fatalf("mode: got %q want create", calls[0].Mode)
	}
	if !strings.HasPrefix(calls[0].Path, "agents/ceo/notebook/2026-05-05-131415-message-posted-") {
		t.Fatalf("unexpected path: %q", calls[0].Path)
	}
	if !strings.HasSuffix(calls[0].Path, ".md") {
		t.Fatalf("path missing .md suffix: %q", calls[0].Path)
	}
	if !strings.Contains(calls[0].Content, "# message_posted in #general") {
		t.Fatalf("body missing header:\n%s", calls[0].Content)
	}
	if !strings.Contains(calls[0].Content, "> hello team") {
		t.Fatalf("body missing blockquote:\n%s", calls[0].Content)
	}
}

func TestAutoNotebookWriter_RosterFilterDropsNonAgents(t *testing.T) {
	client := &fakeNotebookClient{}
	roster := allowSetRoster{allowed: map[string]struct{}{"ceo": {}}}
	w := startWriter(t, client, roster)

	w.Handle(autoNotebookEvent{Kind: AutoNotebookEventMessagePosted, Slug: "human:nazz", Content: "hi"})
	w.Handle(autoNotebookEvent{Kind: AutoNotebookEventMessagePosted, Slug: "ceo", Content: "yo"})

	calls := waitForCalls(t, client, 1)
	if len(calls) != 1 {
		t.Fatalf("only the agent message should land; got %d", len(calls))
	}
	if w.Counters().NonRoster != 1 {
		t.Fatalf("nonRoster counter: got %d want 1", w.Counters().NonRoster)
	}
}

func TestAutoNotebookWriter_DedupeWithinDay(t *testing.T) {
	client := &fakeNotebookClient{}
	w := startWriter(t, client, allowAllRoster{})

	evt := autoNotebookEvent{
		Kind:      AutoNotebookEventMessagePosted,
		Slug:      "ceo",
		Channel:   "general",
		Content:   "shipping pr 1 today",
		Timestamp: time.Date(2026, 5, 5, 13, 14, 15, 0, time.UTC),
	}
	w.Handle(evt)
	evt2 := evt
	evt2.Timestamp = evt.Timestamp.Add(2 * time.Second)
	w.Handle(evt2)

	waitForCalls(t, client, 1)
	waitForCounter(t, func() int64 { return w.Counters().Deduped }, 1)
	if got := len(client.snapshot()); got != 1 {
		t.Fatalf("expected exactly 1 write, got %d", got)
	}
}

func TestAutoNotebookWriter_DedupePerDayBucket(t *testing.T) {
	client := &fakeNotebookClient{}
	w := startWriter(t, client, allowAllRoster{})

	common := autoNotebookEvent{
		Kind:    AutoNotebookEventMessagePosted,
		Slug:    "ceo",
		Channel: "general",
		Content: "weekly retro: shipped pr 1",
	}
	day1 := common
	day1.Timestamp = time.Date(2026, 5, 5, 23, 59, 59, 0, time.UTC)
	day2 := common
	day2.Timestamp = time.Date(2026, 5, 6, 0, 0, 1, 0, time.UTC)

	w.Handle(day1)
	w.Handle(day2)

	calls := waitForCalls(t, client, 2)
	if len(calls) != 2 {
		t.Fatalf("expected 2 writes (different day buckets), got %d", len(calls))
	}
}

func TestAutoNotebookWriter_RedactsSecretContent(t *testing.T) {
	client := &fakeNotebookClient{}
	w := startWriter(t, client, allowAllRoster{})

	// Build the fake-looking key at runtime so GitHub's secret scanner does
	// not flag the source line. The pattern still matches the writer's
	// regex set at the byte level, which is the property under test.
	fakeKey := "sk" + "_" + "live" + "_" + strings.Repeat("A", 24)
	w.Handle(autoNotebookEvent{
		Kind:    AutoNotebookEventMessagePosted,
		Slug:    "ceo",
		Channel: "general",
		Content: "found a stripe key " + fakeKey + " in the logs",
	})

	waitForCounter(t, func() int64 { return w.Counters().Redacted }, 1)
	if got := len(client.snapshot()); got != 0 {
		t.Fatalf("expected zero writes after redaction, got %d", got)
	}
}

func TestAutoNotebookWriter_TaskTransitionUsesOwnerShelf(t *testing.T) {
	client := &fakeNotebookClient{}
	w := startWriter(t, client, allowAllRoster{})

	w.Handle(autoNotebookEvent{
		Kind:         AutoNotebookEventTaskTransitioned,
		Slug:         "eng",
		Actor:        "ceo",
		Channel:      "engineering",
		TaskID:       "task-42",
		TaskTitle:    "Ship PR 1",
		BeforeStatus: "review",
		AfterStatus:  "done",
		Content:      "Ship PR 1",
		Timestamp:    time.Date(2026, 5, 5, 13, 14, 15, 0, time.UTC),
	})

	calls := waitForCalls(t, client, 1)
	if calls[0].Slug != "eng" {
		t.Fatalf("expected owner shelf 'eng', got %q", calls[0].Slug)
	}
	if !strings.Contains(calls[0].Content, "review → done") {
		t.Fatalf("missing transition delta in body:\n%s", calls[0].Content)
	}
	if !strings.Contains(calls[0].Content, "task: task-42 \"Ship PR 1\"") {
		t.Fatalf("missing task ref in body:\n%s", calls[0].Content)
	}
	if !strings.Contains(calls[0].Path, "task-transitioned") {
		t.Fatalf("path missing kind segment: %q", calls[0].Path)
	}
}

func TestAutoNotebookWriter_NoopTransitionDropped(t *testing.T) {
	client := &fakeNotebookClient{}
	w := startWriter(t, client, allowAllRoster{})

	w.Handle(autoNotebookEvent{
		Kind:         AutoNotebookEventTaskTransitioned,
		Slug:         "eng",
		BeforeStatus: "in_progress",
		AfterStatus:  "in_progress",
	})

	// Counter is updated synchronously in Handle, so no wait needed.
	if w.Counters().NoopTransition != 1 {
		t.Fatalf("noopTransition: want 1, got %d", w.Counters().NoopTransition)
	}
	if got := len(client.snapshot()); got != 0 {
		t.Fatalf("no-op transitions must not write; got %d", got)
	}
}

func TestAutoNotebookWriter_QueueSaturationDropsNotBlocks(t *testing.T) {
	// Block the writer goroutine by giving it a slow client. Once the queue
	// fills, additional Handle() calls must drop without blocking.
	release := make(chan struct{})
	client := &slowClient{release: release}
	w := startWriter(t, client, allowAllRoster{})

	// First N events fill the buffered channel + the in-process slot.
	for i := 0; i < autoNotebookQueueSize+2; i++ {
		w.Handle(autoNotebookEvent{
			Kind:    AutoNotebookEventMessagePosted,
			Slug:    "ceo",
			Channel: "general",
			Content: "msg-" + strings.Repeat("x", i+1), // unique content per call
		})
	}

	if w.Counters().QueueSaturated == 0 {
		t.Fatalf("expected at least one queue-saturated drop")
	}
	close(release)
}

func TestAutoNotebookWriter_HandleAfterStopIsNoop(t *testing.T) {
	client := &fakeNotebookClient{}
	w := NewAutoNotebookWriter(client, allowAllRoster{})
	ctx := context.Background()
	w.Start(ctx)
	w.Stop(time.Second)

	w.Handle(autoNotebookEvent{Kind: AutoNotebookEventMessagePosted, Slug: "ceo", Content: "after stop"})

	if got := len(client.snapshot()); got != 0 {
		t.Fatalf("Handle after Stop must drop; got %d writes", got)
	}
}

func TestAutoNotebookWriter_WriteFailureCountedNotPropagated(t *testing.T) {
	client := &fakeNotebookClient{err: errors.New("simulated git contention")}
	w := startWriter(t, client, allowAllRoster{})

	w.Handle(autoNotebookEvent{
		Kind:    AutoNotebookEventMessagePosted,
		Slug:    "ceo",
		Channel: "general",
		Content: "transient error",
	})

	waitForCounter(t, func() int64 { return w.Counters().WriteFailed }, 1)
	if w.Counters().Written != 0 {
		t.Fatalf("written counter should stay 0; got %d", w.Counters().Written)
	}
}

func TestAutoNotebookSecretPatterns(t *testing.T) {
	t.Parallel()
	// Build fixtures at runtime so GitHub's secret scanner does not flag the
	// source. Each value still satisfies the corresponding regex.
	awsKey := "AKIA" + strings.Repeat("A", 16)
	ghpKey := "ghp_" + strings.Repeat("a", 36)
	openaiKey := "sk-" + strings.Repeat("A", 32)
	slackKey := "xoxb-" + strings.Repeat("1", 10) + "-abcde"
	cases := []struct {
		name string
		in   string
		hit  bool
	}{
		{"aws access key", awsKey, true},
		{"github pat", ghpKey, true},
		{"openai key", openaiKey, true},
		{"slack token", slackKey, true},
		{"private key block", "-----BEGIN RSA PRIVATE KEY-----", true},
		{"plain word password", "password is fine to mention", false},
		{"normal sentence", "shipping notebook auto-writer pr1", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := autoNotebookContainsSecret(tc.in); got != tc.hit {
				t.Fatalf("autoNotebookContainsSecret(%q) = %v, want %v", tc.in, got, tc.hit)
			}
		})
	}
}

func TestAutoNotebookTruncate(t *testing.T) {
	t.Parallel()
	// Ascii within limit
	if got := autoNotebookTruncate("hello", 10); got != "hello" {
		t.Fatalf("short string changed: %q", got)
	}
	// Ascii over limit
	if got := autoNotebookTruncate("abcdefghij", 5); got != "abcde…" {
		t.Fatalf("ascii truncate: got %q", got)
	}
	// UTF-8 boundary
	got := autoNotebookTruncate("héllo", 3)
	// "h" (1 byte) + "é" (2 bytes) = 3 bytes; cut at 3 lands cleanly.
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected truncation marker; got %q", got)
	}
	for i, r := range got {
		if r == 0xFFFD {
			t.Fatalf("invalid utf-8 introduced at byte %d in %q", i, got)
		}
	}
}

func TestRenderAutoNotebookSection_MarkdownEscape(t *testing.T) {
	body := renderAutoNotebookSection(autoNotebookEvent{
		Kind:      AutoNotebookEventMessagePosted,
		Slug:      "ceo",
		Actor:     "ceo",
		Channel:   "general",
		Content:   "## not a section\n# definitely not\n- list",
		Timestamp: time.Date(2026, 5, 5, 13, 0, 0, 0, time.UTC),
	})
	// Each content line should be blockquoted, never promoted to a header.
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		if strings.HasPrefix(line, "##") {
			t.Fatalf("content line was promoted to H2 header:\n%s", body)
		}
	}
	if !strings.Contains(body, "> ## not a section") {
		t.Fatalf("expected blockquoted content line; body:\n%s", body)
	}
}

// slowClient simulates a NotebookWrite that blocks until release is closed.
type slowClient struct {
	release chan struct{}
	calls   atomic.Int32
}

func (s *slowClient) NotebookWrite(ctx context.Context, slug, path, content, mode, msg string) (string, int, error) {
	s.calls.Add(1)
	select {
	case <-s.release:
	case <-ctx.Done():
		return "", 0, ctx.Err()
	}
	return "deadbeef", len(content), nil
}
