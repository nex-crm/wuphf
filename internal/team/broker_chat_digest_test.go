package team

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func digestMsg(from, channel, content, replyTo string, ts time.Time) chatDigestMessage {
	return chatDigestMessage{
		ID:        from + "-" + ts.Format("150405"),
		From:      from,
		Channel:   channel,
		Content:   content,
		ReplyTo:   replyTo,
		Timestamp: ts.UTC(),
	}
}

func TestBuildChatDigestJobs_KeepsMeaningfulDropsChatter(t *testing.T) {
	base := time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC)
	msgs := []chatDigestMessage{
		// general: a real thread — two participants, replies.
		digestMsg("jim", "general", "should we ship the wiki today?", "", base),
		digestMsg("pam", "general", "yes, tests are green", "jim-090000", base.Add(time.Minute)),
		digestMsg("jim", "general", "great, merging", "pam-090100", base.Add(2*time.Minute)),
		// random: single-message chatter — must be dropped.
		digestMsg("dwight", "random", "good morning", "", base.Add(time.Hour)),
		// noise: pure system posts — must be dropped.
		digestMsg("system", "noise", "task-1 approved", "", base),
		digestMsg("system", "noise", "task-2 approved", "", base.Add(time.Minute)),
	}

	jobs := buildChatDigestJobs(msgs, 24*time.Hour)
	if len(jobs) != 1 {
		t.Fatalf("expected exactly 1 meaningful digest, got %d: %+v", len(jobs), jobs)
	}
	job := jobs[0]
	if job.Kind != SourceKindChat {
		t.Errorf("kind = %q, want chat", job.Kind)
	}
	if job.Origin != "general:2026-06-25" {
		t.Errorf("origin = %q, want general:2026-06-25", job.Origin)
	}
	for _, want := range []string{"should we ship the wiki today?", "yes, tests are green", "jim", "pam"} {
		if !strings.Contains(job.Content, want) {
			t.Errorf("digest content missing %q\n%s", want, job.Content)
		}
	}
	if strings.Contains(job.Content, "good morning") || strings.Contains(job.Content, "approved") {
		t.Errorf("digest leaked chatter/system content:\n%s", job.Content)
	}
}

func TestBuildChatDigestJobs_StableIDAcrossRuns(t *testing.T) {
	base := time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC)
	msgs := []chatDigestMessage{
		digestMsg("jim", "general", "a", "", base),
		digestMsg("pam", "general", "b", "jim-090000", base.Add(time.Minute)),
	}
	first := buildChatDigestJobs(msgs, 24*time.Hour)
	second := buildChatDigestJobs(msgs, 24*time.Hour)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected 1 job each run, got %d/%d", len(first), len(second))
	}
	if first[0].ID != second[0].ID {
		t.Fatalf("digest id not stable: %q vs %q", first[0].ID, second[0].ID)
	}
}

// TestSweepChatDigests_CapturesAndDedupes drives the sweep function directly
// (not the ticker) through a real broker + wiki worker + capture dispatcher,
// and asserts a meaningful thread produces exactly one chat source even after
// re-running the sweep (write-once dedupe).
func TestSweepChatDigests_CapturesAndDedupes(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WUPHF_RUNTIME_HOME", dir)
	b := newTestBroker(t)

	base := time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC)
	b.mu.Lock()
	b.messages = append(b.messages,
		channelMessage{ID: "m1", From: "jim", Channel: "general", Content: "ship it?", Timestamp: base.Format(time.RFC3339)},
		channelMessage{ID: "m2", From: "pam", Channel: "general", Content: "green, go", ReplyTo: "m1", Timestamp: base.Add(time.Minute).Format(time.RFC3339)},
		// single-message chatter that must NOT produce a digest.
		channelMessage{ID: "m3", From: "dwight", Channel: "random", Content: "hi", Timestamp: base.Format(time.RFC3339)},
	)
	b.mu.Unlock()

	wikiRoot := filepath.Join(dir, "wiki-repo")
	repo := NewRepoAt(wikiRoot, filepath.Join(dir, "wiki-backup"))
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("repo.Init: %v", err)
	}
	worker := NewWikiWorker(repo, nil)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()
	b.startSourceCaptureDispatcher()
	t.Cleanup(func() {
		if disp := b.sourceCaptureDispatcher.Load(); disp != nil {
			disp.Stop(2 * time.Second)
		}
		worker.Stop()
		<-worker.Done()
		cancel()
	})

	b.sweepChatDigests(base.Add(time.Hour))
	chatDir := filepath.Join(wikiRoot, "sources", "chat")
	testTickUntil(t, 5*time.Second, func() bool { return countMarkdown(t, chatDir) == 1 })

	// Re-running the sweep is a write-once no-op: still exactly one source.
	b.sweepChatDigests(base.Add(2 * time.Hour))
	// Give the drain a moment; the count must remain 1.
	time.Sleep(300 * time.Millisecond)
	if got := countMarkdown(t, chatDir); got != 1 {
		t.Fatalf("re-sweep changed source count: got %d, want 1", got)
	}

	body := readOnlyMarkdown(t, chatDir)
	for _, want := range []string{"kind: chat", "ship it?", "green, go"} {
		if !strings.Contains(body, want) {
			t.Errorf("chat source missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "random") || strings.Contains(body, "dwight") {
		t.Errorf("chat source leaked chatter channel:\n%s", body)
	}
}
