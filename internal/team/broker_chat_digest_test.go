package team

import (
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
// (not the ticker) through a real broker + capture dispatcher with a fake gbrain
// client, and asserts a meaningful thread produces exactly one chat page (one
// distinct slug) even after re-running the sweep — gbrain put_page is an upsert,
// so a re-sweep overwrites the same slug rather than duplicating it.
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

	fake := &fakeGBrainCaptureClient{}
	setSharedGBrainClient(fake)
	t.Cleanup(func() { setSharedGBrainClient(nil) })
	b.startSourceCaptureDispatcher()
	t.Cleanup(func() {
		if disp := b.sourceCaptureDispatcher.Load(); disp != nil {
			disp.Stop(2 * time.Second)
		}
	})

	wantSlug := DeriveSourceID(SourceKindChat, "general:2026-06-25", "", "")

	b.sweepChatDigests(base.Add(time.Hour))
	testTickUntil(t, 5*time.Second, func() bool {
		_, ok := fake.lastFor(wantSlug)
		return ok
	})

	// Re-running the sweep upserts the same slug: still exactly one distinct page.
	b.sweepChatDigests(base.Add(2 * time.Hour))
	// Wait for the second sweep to drain (a second put for the same slug)
	// instead of sleeping a fixed interval.
	testTickUntil(t, 5*time.Second, func() bool { return fake.count() >= 2 })
	if slugs := fake.distinctSlugs(); len(slugs) != 1 {
		t.Fatalf("re-sweep changed distinct page count: got %d (%v), want 1", len(slugs), slugs)
	}

	put, ok := fake.lastFor(wantSlug)
	if !ok {
		t.Fatalf("no chat page for slug %q; slugs=%v", wantSlug, fake.distinctSlugs())
	}
	if put.opts.SourceKind != gbrainCaptureSourceKindPrefix+string(SourceKindChat) {
		t.Errorf("source_kind = %q, want %q", put.opts.SourceKind, gbrainCaptureSourceKindPrefix+string(SourceKindChat))
	}
	for _, want := range []string{"kind: chat", "ship it?", "green, go"} {
		if !strings.Contains(put.content, want) {
			t.Errorf("chat page missing %q\n%s", want, put.content)
		}
	}
	if strings.Contains(put.content, "random") || strings.Contains(put.content, "dwight") {
		t.Errorf("chat page leaked chatter channel:\n%s", put.content)
	}
}
