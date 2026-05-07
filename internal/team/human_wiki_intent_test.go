package team

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- classifier tests --------------------------------------------------

func TestClassifyHumanWikiIntent_Matches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		body      string
		wantKind  humanWikiIntentKind
		wantTopic string // expected topic substring (lowercased, may be empty for fallback)
	}{
		{
			name:      "remember this colon",
			body:      "remember this: the retro deadline is every Friday",
			wantKind:  HumanWikiIntentRemember,
			wantTopic: "the retro deadline is every friday",
		},
		{
			name:      "save to wiki imperative",
			body:      "save to wiki: our ICP is founders running 3+ AI agents",
			wantKind:  HumanWikiIntentWriteKB,
			wantTopic: "our icp is founders running 3+ ai agents",
		},
		{
			name:      "save to KB",
			body:      "save to KB: bun not npm",
			wantKind:  HumanWikiIntentWriteKB,
			wantTopic: "bun not npm",
		},
		{
			name:      "save this",
			body:      "save this — we use bun, not npm",
			wantKind:  HumanWikiIntentRemember,
			wantTopic: "we use bun, not npm",
		},
		{
			name:      "write this down",
			body:      "write this down: standups at 10am",
			wantKind:  HumanWikiIntentRemember,
			wantTopic: "standups at 10am",
		},
		{
			name:      "write to KB",
			body:      "write to KB: prefer Vitest over Bun's native runner",
			wantKind:  HumanWikiIntentWriteKB,
			wantTopic: "prefer vitest over bun's native runner",
		},
		{
			name:      "write to knowledge base",
			body:      "write to knowledge base: we use bun, not npm",
			wantKind:  HumanWikiIntentWriteKB,
			wantTopic: "we use bun, not npm",
		},
		{
			name:      "add to wiki",
			body:      "add to wiki: the launch date is locked",
			wantKind:  HumanWikiIntentWikiThis,
			wantTopic: "the launch date is locked",
		},
		{
			name:      "wiki this",
			body:      "wiki this: prod deploys go through GH actions",
			wantKind:  HumanWikiIntentWikiThis,
			wantTopic: "prod deploys go through gh actions",
		},
		{
			name:      "this is canonical",
			body:      "this is canonical: bun is the JS runtime",
			wantKind:  HumanWikiIntentCanonical,
			wantTopic: "bun is the js runtime",
		},
		{
			name:      "save to memory",
			body:      "save to memory: we use bun not npm",
			wantKind:  HumanWikiIntentSaveMem,
			wantTopic: "we use bun not npm",
		},
		{
			name:      "save to memory (mixed case)",
			body:      "Save To Memory: prefer pnpm",
			wantKind:  HumanWikiIntentSaveMem,
			wantTopic: "prefer pnpm",
		},
		{
			name:      "remember this no separator",
			body:      "remember this our deploys go through GH actions",
			wantKind:  HumanWikiIntentRemember,
			wantTopic: "our deploys go through gh actions",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := classifyHumanWikiIntent(tc.body)
			if !ok {
				t.Fatalf("expected match for %q, got none", tc.body)
			}
			if got.Kind != tc.wantKind {
				t.Errorf("kind: got %q want %q", got.Kind, tc.wantKind)
			}
			topicLower := strings.ToLower(got.Topic)
			if tc.wantTopic != "" && !strings.Contains(topicLower, tc.wantTopic) {
				t.Errorf("topic %q does not contain expected fragment %q", got.Topic, tc.wantTopic)
			}
			if strings.TrimSpace(got.Content) == "" {
				t.Errorf("content must not be empty for %q", tc.body)
			}
		})
	}
}

func TestClassifyHumanWikiIntent_NoMatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{"empty body", ""},
		{"whitespace", "   \n\t  "},
		{"historical remember", "remember when we shipped the auth PR last quarter"},
		{"agent technical output", "running go build... done."},
		{"remember inside code fence", "```\nremember this: foo\n```\nlook above"},
		{"remember inside backticks", "the function is `remember this` from utils"},
		{"plain mention of wiki", "should we wiki this later? maybe."},
		{"rhetorical question", "do you remember when bun came out?"},
		{"past tense", "i remembered this yesterday"},
		{"random sentence", "the dashboard is loading slowly today"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := classifyHumanWikiIntent(tc.body); ok {
				t.Errorf("expected no match for %q, got match", tc.body)
			}
		})
	}
}

func TestClassifyHumanWikiIntent_BareIntentMatches(t *testing.T) {
	t.Parallel()
	// "remember this" with no payload still matches; topic is empty and the
	// path generator turns it into "note" (asserted in
	// TestHumanWikiEntryPath_EmptyTopicFallback).
	got, ok := classifyHumanWikiIntent("remember this")
	if !ok {
		t.Fatalf("expected match for bare intent")
	}
	if got.Kind != HumanWikiIntentRemember {
		t.Errorf("kind: got %q want %q", got.Kind, HumanWikiIntentRemember)
	}
}

// ---- render tests ------------------------------------------------------

func TestRenderHumanWikiEntry_Golden(t *testing.T) {
	t.Parallel()
	match := humanWikiIntentMatch{
		Kind:    HumanWikiIntentRemember,
		Topic:   "Retro deadline cadence",
		Content: "the retro deadline is every Friday",
	}
	ts := time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC)
	body := renderHumanWikiEntry(match, "general", ts)

	wantLines := []string{
		"# Retro deadline cadence",
		"- timestamp: 2026-05-06T14:30:00Z",
		"- source: human",
		"- intent: remember",
		"- channel: #general",
		"the retro deadline is every Friday",
	}
	for _, want := range wantLines {
		if !strings.Contains(body, want) {
			t.Errorf("rendered body missing %q\n--- body ---\n%s", want, body)
		}
	}
}

func TestRenderHumanWikiEntry_EmptyChannelOmitted(t *testing.T) {
	t.Parallel()
	match := humanWikiIntentMatch{
		Kind:    HumanWikiIntentRemember,
		Topic:   "x",
		Content: "y",
	}
	body := renderHumanWikiEntry(match, "", time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC))
	if strings.Contains(body, "channel:") {
		t.Errorf("channel line should be omitted when channel is empty\n%s", body)
	}
}

// ---- path tests --------------------------------------------------------

func TestHumanWikiEntryPath_Format(t *testing.T) {
	t.Parallel()
	match := humanWikiIntentMatch{
		Kind:    HumanWikiIntentRemember,
		Topic:   "Retro Deadline!!! Friday",
		Content: "the retro deadline is every Friday",
	}
	ts := time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC)
	path := humanWikiEntryPath(match, ts)

	if !strings.HasPrefix(path, "team/2026-05-06-") {
		t.Errorf("path prefix wrong: %q", path)
	}
	if !strings.HasSuffix(path, ".md") {
		t.Errorf("path suffix wrong: %q", path)
	}
	if !strings.Contains(path, "retro-deadline-friday") {
		t.Errorf("topic slug missing: %q", path)
	}
	// Path must NOT have raw whitespace, exclamation, or upper-case from topic.
	for _, ch := range []string{" ", "!", "Retro"} {
		if strings.Contains(path, ch) {
			t.Errorf("path contains forbidden fragment %q: %s", ch, path)
		}
	}
}

func TestHumanWikiEntryPath_EmptyTopicFallback(t *testing.T) {
	t.Parallel()
	match := humanWikiIntentMatch{
		Kind:    HumanWikiIntentRemember,
		Topic:   "",
		Content: "anything",
	}
	ts := time.Date(2026, 5, 6, 14, 30, 0, 0, time.UTC)
	path := humanWikiEntryPath(match, ts)
	if !strings.Contains(path, "-note-") {
		t.Errorf("expected fallback topic 'note' in path, got: %q", path)
	}
}

func TestHumanWikiEntryPath_HashStableForSameContent(t *testing.T) {
	t.Parallel()
	// shortHash is keyed on content, not timestamp, so the same content posted
	// twice in the same day produces the same path. This means the wiki-side
	// commit becomes a no-op replace if the human re-sends the same line.
	match := humanWikiIntentMatch{
		Kind:    HumanWikiIntentRemember,
		Topic:   "x",
		Content: "we use bun not npm",
	}
	ts1 := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 5, 6, 23, 59, 59, 0, time.UTC)
	if humanWikiEntryPath(match, ts1) != humanWikiEntryPath(match, ts2) {
		t.Errorf("paths must match for same content on same day")
	}
}

func TestHumanWikiEntryPath_DifferentContentDifferentHash(t *testing.T) {
	t.Parallel()
	a := humanWikiIntentMatch{Kind: HumanWikiIntentRemember, Topic: "x", Content: "alpha"}
	b := humanWikiIntentMatch{Kind: HumanWikiIntentRemember, Topic: "x", Content: "beta"}
	ts := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	if humanWikiEntryPath(a, ts) == humanWikiEntryPath(b, ts) {
		t.Errorf("different content must yield different paths")
	}
}

// ---- writer lifecycle tests ---------------------------------------------

// fakeHumanWikiClient records each EnqueueHuman call.
type fakeHumanWikiClient struct {
	mu    sync.Mutex
	calls []fakeHumanWikiCall
	err   error
	block chan struct{}
}

type fakeHumanWikiCall struct {
	Path        string
	Content     string
	CommitMsg   string
	ExpectedSHA string
}

func (f *fakeHumanWikiClient) EnqueueHuman(ctx context.Context, path, content, commitMsg, expectedSHA string) (string, int, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return "", 0, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return "", 0, f.err
	}
	f.calls = append(f.calls, fakeHumanWikiCall{
		Path: path, Content: content, CommitMsg: commitMsg, ExpectedSHA: expectedSHA,
	})
	return "deadbeef", len(content), nil
}

func (f *fakeHumanWikiClient) snapshot() []fakeHumanWikiCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeHumanWikiCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func startHumanWikiWriter(t *testing.T, client humanWikiIntentWriterClient) *HumanWikiIntentWriter {
	t.Helper()
	w := NewHumanWikiIntentWriter(client)
	ctx, cancel := context.WithCancel(context.Background())
	w.Start(ctx)
	t.Cleanup(func() {
		cancel()
		w.Stop(2 * time.Second)
	})
	return w
}

func TestHumanWikiIntentWriter_HandleEnqueuesAndWrites(t *testing.T) {
	client := &fakeHumanWikiClient{}
	w := startHumanWikiWriter(t, client)

	w.Handle(channelMessage{
		ID:        "msg-1",
		From:      "human",
		Channel:   "general",
		Content:   "remember this: bun is the JS runtime",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.WaitForCondition(ctx, func() bool {
		return len(client.snapshot()) >= 1
	}); err != nil {
		t.Fatalf("waiting for write: %v (counters=%+v)", err, w.Counters())
	}
	calls := client.snapshot()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if !strings.HasPrefix(calls[0].Path, "team/") {
		t.Errorf("path must start with team/: %q", calls[0].Path)
	}
	if !strings.HasSuffix(calls[0].Path, ".md") {
		t.Errorf("path must end with .md: %q", calls[0].Path)
	}
	if !strings.Contains(calls[0].Content, "bun is the JS runtime") {
		t.Errorf("content missing payload:\n%s", calls[0].Content)
	}
	if calls[0].ExpectedSHA != "" {
		t.Errorf("expectedSHA must be empty for new files, got %q", calls[0].ExpectedSHA)
	}
}

func TestHumanWikiIntentWriter_NoIntentSkips(t *testing.T) {
	client := &fakeHumanWikiClient{}
	w := startHumanWikiWriter(t, client)

	w.Handle(channelMessage{
		From:    "human",
		Channel: "general",
		Content: "the dashboard is loading slowly",
	})

	// Counter is updated synchronously inside the writer goroutine; wait on it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.WaitForCondition(ctx, func() bool {
		return w.Counters().Skipped >= 1
	}); err != nil {
		t.Fatalf("expected skip counter to fire: %v", err)
	}
	if got := len(client.snapshot()); got != 0 {
		t.Fatalf("non-intent message must not write; got %d", got)
	}
}

func TestHumanWikiIntentWriter_QueueSaturationCounted(t *testing.T) {
	release := make(chan struct{})
	client := &fakeHumanWikiClient{block: release}
	w := startHumanWikiWriter(t, client)

	for i := 0; i < humanWikiIntentQueueSize+4; i++ {
		w.Handle(channelMessage{
			From:    "human",
			Channel: "general",
			Content: "remember this: payload-" + strings.Repeat("x", i+1),
		})
	}
	if w.Counters().QueueSat == 0 {
		t.Fatalf("expected QueueSat > 0 under saturation")
	}
	close(release)
}

func TestHumanWikiIntentWriter_HandleAfterStopIsNoop(t *testing.T) {
	client := &fakeHumanWikiClient{}
	w := NewHumanWikiIntentWriter(client)
	w.Start(context.Background())
	w.Stop(time.Second)

	w.Handle(channelMessage{
		From:    "human",
		Content: "remember this: post-stop should drop",
	})
	if got := len(client.snapshot()); got != 0 {
		t.Fatalf("Handle after Stop must drop; got %d writes", got)
	}
}

func TestHumanWikiIntentWriter_WriteFailureCounted(t *testing.T) {
	client := &fakeHumanWikiClient{err: errors.New("simulated git lock")}
	w := startHumanWikiWriter(t, client)

	w.Handle(channelMessage{
		From:    "human",
		Channel: "general",
		Content: "remember this: write failure path",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := w.WaitForCondition(ctx, func() bool {
		return w.Counters().WriteFailed >= 1
	}); err != nil {
		t.Fatalf("expected WriteFailed counter: %v", err)
	}
	if w.Counters().Written != 0 {
		t.Fatalf("Written must remain 0 on failure, got %d", w.Counters().Written)
	}
}

// Compile-time check: atomic-counter snapshot is plain values, not pointers.
var _ HumanWikiIntentCounters = HumanWikiIntentCounters{
	Enqueued:    0,
	Written:     0,
	Skipped:     0,
	WriteFailed: 0,
	QueueSat:    0,
}
