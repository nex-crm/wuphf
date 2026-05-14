package team

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeAutoNotebookWriterClient implements autoNotebookWriterClient so the
// AutoEscalateDemandCandidates path can validate entry existence via NotebookRead.
type fakeAutoNotebookWriterClient struct {
	existing map[string]string
	readErr  error
}

func (f *fakeAutoNotebookWriterClient) NotebookWrite(_ context.Context, slug, path, content, mode, _ string) (string, int, error) {
	_ = slug
	_ = path
	_ = content
	_ = mode
	return "deadbeef", 0, nil
}

// fakeNotebookReader exposes NotebookRead so AutoEscalateDemandCandidates can
// verify the entry path exists before submitting it to ReviewLog.
type fakeNotebookReader struct {
	existing map[string]string
	readErr  error
}

func (f *fakeNotebookReader) NotebookRead(path string) ([]byte, error) {
	if f == nil {
		return nil, os.ErrNotExist
	}
	if f.readErr != nil {
		return nil, f.readErr
	}
	body, ok := f.existing[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(body), nil
}

func newDemandIndex(t *testing.T) *NotebookDemandIndex {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	idx, err := NewNotebookDemandIndex(logPath)
	if err != nil {
		t.Fatalf("NewNotebookDemandIndex: %v", err)
	}
	return idx
}

func demandTestNow() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)
}

func TestSignalWeight_Mapping(t *testing.T) {
	cases := []struct {
		signal PromotionDemandSignal
		want   float64
	}{
		{DemandSignalCrossAgentSearch, 1.0},
		{DemandSignalChannelContextAsk, 2.0},
		{DemandSignalCEOReviewFlag, 1.5},
		{DemandSignalRejectionCooldown, -2.0},
	}
	for _, c := range cases {
		if got := signalWeight(c.signal); got != c.want {
			t.Fatalf("signalWeight(%d) = %v, want %v", c.signal, got, c.want)
		}
	}
}

func TestDemandScoring_CrossAgentSearch(t *testing.T) {
	idx := newDemandIndex(t)
	now := demandTestNow()
	idx.SetClockForTest(func() time.Time { return now })
	path := "agents/pm/notebook/2026-05-06-retro.md"

	// Two distinct agents searching the same entry → score = 2.0.
	if err := idx.Record(PromotionDemandEvent{
		EntryPath:    path,
		OwnerSlug:    "pm",
		SearcherSlug: "eng",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   now,
	}); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if err := idx.Record(PromotionDemandEvent{
		EntryPath:    path,
		OwnerSlug:    "pm",
		SearcherSlug: "design",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   now.Add(time.Minute),
	}); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if got := idx.Score(path); got != 2.0 {
		t.Fatalf("score after 2 distinct searchers = %v, want 2.0", got)
	}

	// Same agent re-searching same entry within 24h → still 2.0 (deduped).
	if err := idx.Record(PromotionDemandEvent{
		EntryPath:    path,
		OwnerSlug:    "pm",
		SearcherSlug: "eng",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   now.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("record dup: %v", err)
	}
	if got := idx.Score(path); got != 2.0 {
		t.Fatalf("score after same-day dup = %v, want 2.0", got)
	}

	// Third distinct agent → 3.0.
	if err := idx.Record(PromotionDemandEvent{
		EntryPath:    path,
		OwnerSlug:    "pm",
		SearcherSlug: "ops",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   now.Add(3 * time.Hour),
	}); err != nil {
		t.Fatalf("record 3: %v", err)
	}
	if got := idx.Score(path); got != 3.0 {
		t.Fatalf("score after 3 distinct searchers = %v, want 3.0", got)
	}
}

func TestDemandScoring_DifferentDays(t *testing.T) {
	idx := newDemandIndex(t)
	now := demandTestNow()
	idx.SetClockForTest(func() time.Time { return now })
	path := "agents/pm/notebook/entry.md"

	// Same agent, different days → both events count.
	if err := idx.Record(PromotionDemandEvent{
		EntryPath:    path,
		OwnerSlug:    "pm",
		SearcherSlug: "eng",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   now,
	}); err != nil {
		t.Fatalf("day1: %v", err)
	}
	if err := idx.Record(PromotionDemandEvent{
		EntryPath:    path,
		OwnerSlug:    "pm",
		SearcherSlug: "eng",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   now.Add(25 * time.Hour),
	}); err != nil {
		t.Fatalf("day2: %v", err)
	}
	if got := idx.Score(path); got != 2.0 {
		t.Fatalf("score across 2 days = %v, want 2.0", got)
	}
}

func TestDemandScoring_RejectionCooldown(t *testing.T) {
	idx := newDemandIndex(t)
	now := demandTestNow()
	idx.SetClockForTest(func() time.Time { return now })
	path := "agents/pm/notebook/entry.md"

	// Build score above threshold (3 distinct searchers).
	for _, slug := range []string{"eng", "design", "ops"} {
		if err := idx.Record(PromotionDemandEvent{
			EntryPath:    path,
			OwnerSlug:    "pm",
			SearcherSlug: slug,
			Signal:       DemandSignalCrossAgentSearch,
			RecordedAt:   now,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	if got := idx.Score(path); got != 3.0 {
		t.Fatalf("pre-rejection score = %v, want 3.0", got)
	}
	// Add a rejection cooldown — score drops below threshold.
	if err := idx.Record(PromotionDemandEvent{
		EntryPath:    path,
		OwnerSlug:    "pm",
		SearcherSlug: "ceo",
		Signal:       DemandSignalRejectionCooldown,
		RecordedAt:   now,
	}); err != nil {
		t.Fatalf("rejection: %v", err)
	}
	if got := idx.Score(path); got != 1.0 {
		t.Fatalf("post-rejection score = %v, want 1.0", got)
	}
}

func TestDemandScoring_WindowExpiry(t *testing.T) {
	idx := newDemandIndex(t)
	now := demandTestNow()
	path := "agents/pm/notebook/old.md"

	// Event 8 days ago → outside default 7d window.
	if err := idx.Record(PromotionDemandEvent{
		EntryPath:    path,
		OwnerSlug:    "pm",
		SearcherSlug: "eng",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   now.Add(-8 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("old: %v", err)
	}
	// Event 1 day ago → in window.
	if err := idx.Record(PromotionDemandEvent{
		EntryPath:    path,
		OwnerSlug:    "pm",
		SearcherSlug: "design",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   now.Add(-24 * time.Hour),
	}); err != nil {
		t.Fatalf("recent: %v", err)
	}
	idx.SetClockForTest(func() time.Time { return now })
	if got := idx.Score(path); got != 1.0 {
		t.Fatalf("score with window expiry = %v, want 1.0 (only recent event counts)", got)
	}
}

func TestAutoEscalate_ThresholdBreach(t *testing.T) {
	idx := newDemandIndex(t)
	now := demandTestNow()
	idx.SetClockForTest(func() time.Time { return now })

	path := "agents/pm/notebook/onboarding.md"
	// 3 distinct searchers → score 3.0 hits threshold.
	for _, slug := range []string{"eng", "design", "ops"} {
		if err := idx.Record(PromotionDemandEvent{
			EntryPath:    path,
			OwnerSlug:    "pm",
			SearcherSlug: slug,
			Signal:       DemandSignalCrossAgentSearch,
			RecordedAt:   now,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	// Set up a real ReviewLog backed by t.TempDir().
	rl, err := NewReviewLog(filepath.Join(t.TempDir(), "reviews.jsonl"), nil, func() time.Time { return now })
	if err != nil {
		t.Fatalf("review log: %v", err)
	}

	reader := &fakeNotebookReader{existing: map[string]string{
		path: "# onboarding\n\ngotchas",
	}}
	if err := idx.AutoEscalateDemandCandidates(context.Background(), rl, reader); err != nil {
		t.Fatalf("escalate: %v", err)
	}
	reviews := rl.List("all")
	if len(reviews) != 1 {
		t.Fatalf("expected 1 escalated review, got %d", len(reviews))
	}
	if reviews[0].SourcePath != path {
		t.Fatalf("source path = %q, want %q", reviews[0].SourcePath, path)
	}
	if reviews[0].SourceSlug != "pm" {
		t.Fatalf("source slug = %q, want pm", reviews[0].SourceSlug)
	}
}

func TestAutoEscalate_Idempotent(t *testing.T) {
	idx := newDemandIndex(t)
	now := demandTestNow()
	idx.SetClockForTest(func() time.Time { return now })

	path := "agents/pm/notebook/onboarding.md"
	for _, slug := range []string{"eng", "design", "ops"} {
		if err := idx.Record(PromotionDemandEvent{
			EntryPath:    path,
			OwnerSlug:    "pm",
			SearcherSlug: slug,
			Signal:       DemandSignalCrossAgentSearch,
			RecordedAt:   now,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	rl, err := NewReviewLog(filepath.Join(t.TempDir(), "reviews.jsonl"), nil, func() time.Time { return now })
	if err != nil {
		t.Fatalf("review log: %v", err)
	}
	reader := &fakeNotebookReader{existing: map[string]string{
		path: "# onboarding\n\ngotchas",
	}}
	// First escalation creates a review.
	if err := idx.AutoEscalateDemandCandidates(context.Background(), rl, reader); err != nil {
		t.Fatalf("escalate1: %v", err)
	}
	// Second escalation should NOT create a duplicate.
	if err := idx.AutoEscalateDemandCandidates(context.Background(), rl, reader); err != nil {
		t.Fatalf("escalate2: %v", err)
	}
	reviews := rl.List("all")
	if len(reviews) != 1 {
		t.Fatalf("idempotent escalation produced %d reviews, want 1", len(reviews))
	}
}

func TestAutoEscalate_MissingEntry(t *testing.T) {
	idx := newDemandIndex(t)
	now := demandTestNow()
	idx.SetClockForTest(func() time.Time { return now })

	path := "agents/pm/notebook/nonexistent.md"
	for _, slug := range []string{"eng", "design", "ops"} {
		if err := idx.Record(PromotionDemandEvent{
			EntryPath:    path,
			OwnerSlug:    "pm",
			SearcherSlug: slug,
			Signal:       DemandSignalCrossAgentSearch,
			RecordedAt:   now,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	rl, err := NewReviewLog(filepath.Join(t.TempDir(), "reviews.jsonl"), nil, func() time.Time { return now })
	if err != nil {
		t.Fatalf("review log: %v", err)
	}
	// Reader returns ErrNotExist for everything → escalation must skip.
	reader := &fakeNotebookReader{existing: map[string]string{}}
	if err := idx.AutoEscalateDemandCandidates(context.Background(), rl, reader); err != nil {
		t.Fatalf("escalate: %v", err)
	}
	if got := len(rl.List("all")); got != 0 {
		t.Fatalf("missing entry produced %d reviews, want 0", got)
	}
}

func TestNotebookDemandIndex_ReloadFromJSONL(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	now := demandTestNow()
	clock := func() time.Time { return now }
	path := "agents/pm/notebook/entry.md"

	// Use the test constructor so the initial replay() uses the fake clock
	// instead of `time.Now`. Without this, events recorded at `now` would
	// be filtered out during reload once real wall-clock time advances
	// past `now + 7d`.
	idx1, err := NewNotebookDemandIndexForTest(logPath, clock)
	if err != nil {
		t.Fatalf("init1: %v", err)
	}
	for _, slug := range []string{"eng", "design"} {
		if err := idx1.Record(PromotionDemandEvent{
			EntryPath:    path,
			OwnerSlug:    "pm",
			SearcherSlug: slug,
			Signal:       DemandSignalCrossAgentSearch,
			RecordedAt:   now,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}
	if got := idx1.Score(path); got != 2.0 {
		t.Fatalf("idx1 score = %v, want 2.0", got)
	}

	// Reopen the index against the same JSONL — events must replay.
	idx2, err := NewNotebookDemandIndexForTest(logPath, clock)
	if err != nil {
		t.Fatalf("init2: %v", err)
	}
	if got := idx2.Score(path); got != 2.0 {
		t.Fatalf("idx2 score after reload = %v, want 2.0", got)
	}
}

func TestNotebookDemandIndex_ReloadDropsExpired(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	now := demandTestNow()
	path := "agents/pm/notebook/entry.md"

	idx1, err := NewNotebookDemandIndex(logPath)
	if err != nil {
		t.Fatalf("init1: %v", err)
	}
	idx1.SetClockForTest(func() time.Time { return now })
	// Old event (10 days ago) gets persisted.
	if err := idx1.Record(PromotionDemandEvent{
		EntryPath:    path,
		OwnerSlug:    "pm",
		SearcherSlug: "eng",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   now.Add(-10 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("record: %v", err)
	}
	// Reopen with current clock — event should be outside the 7d window.
	idx2, err := NewNotebookDemandIndex(logPath)
	if err != nil {
		t.Fatalf("init2: %v", err)
	}
	idx2.SetClockForTest(func() time.Time { return now })
	if got := idx2.Score(path); got != 0 {
		t.Fatalf("expired event score = %v, want 0", got)
	}
}

func TestTopCandidates_SortedDescending(t *testing.T) {
	idx := newDemandIndex(t)
	now := demandTestNow()
	idx.SetClockForTest(func() time.Time { return now })

	// path A: 3 hits
	for _, slug := range []string{"eng", "design", "ops"} {
		_ = idx.Record(PromotionDemandEvent{
			EntryPath:    "agents/pm/notebook/a.md",
			OwnerSlug:    "pm",
			SearcherSlug: slug,
			Signal:       DemandSignalCrossAgentSearch,
			RecordedAt:   now,
		})
	}
	// path B: 1 hit
	_ = idx.Record(PromotionDemandEvent{
		EntryPath:    "agents/pm/notebook/b.md",
		OwnerSlug:    "pm",
		SearcherSlug: "eng",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   now,
	})
	// path C: 2 hits
	for _, slug := range []string{"eng", "design"} {
		_ = idx.Record(PromotionDemandEvent{
			EntryPath:    "agents/pm/notebook/c.md",
			OwnerSlug:    "pm",
			SearcherSlug: slug,
			Signal:       DemandSignalCrossAgentSearch,
			RecordedAt:   now,
		})
	}
	got := idx.TopCandidates(10)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].EntryPath != "agents/pm/notebook/a.md" || got[0].Score != 3.0 {
		t.Fatalf("top = %+v, want a.md / 3.0", got[0])
	}
	if got[1].EntryPath != "agents/pm/notebook/c.md" || got[1].Score != 2.0 {
		t.Fatalf("mid = %+v, want c.md / 2.0", got[1])
	}
	if got[2].EntryPath != "agents/pm/notebook/b.md" || got[2].Score != 1.0 {
		t.Fatalf("low = %+v, want b.md / 1.0", got[2])
	}
}

func TestRecord_RejectsEmptyPath(t *testing.T) {
	idx := newDemandIndex(t)
	err := idx.Record(PromotionDemandEvent{
		EntryPath:    "",
		OwnerSlug:    "pm",
		SearcherSlug: "eng",
		Signal:       DemandSignalCrossAgentSearch,
		RecordedAt:   time.Now(),
	})
	if err == nil {
		t.Fatalf("expected error on empty entry_path")
	}
	if !errors.Is(err, ErrPromotionDemandInvalid) {
		t.Fatalf("err = %v, want ErrPromotionDemandInvalid", err)
	}
}
