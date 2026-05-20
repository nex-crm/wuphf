package team

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// entityPublisherStub captures SSE events for assertions.
type entityPublisherStub struct {
	mu     sync.Mutex
	briefs []EntityBriefSynthesizedEvent
	facts  []EntityFactRecordedEvent
}

func (p *entityPublisherStub) PublishEntityBriefSynthesized(evt EntityBriefSynthesizedEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.briefs = append(p.briefs, evt)
}
func (p *entityPublisherStub) PublishEntityFactRecorded(evt EntityFactRecordedEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.facts = append(p.facts, evt)
}
func (p *entityPublisherStub) briefCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.briefs)
}

// newSynthFixture wires the full stack: repo + worker + fact log + synth.
// The llmStub is injected via SynthesizerConfig.LLMCall.
func newSynthFixture(t *testing.T, llmStub func(ctx context.Context, sys, user string) (string, error)) (
	*EntitySynthesizer, *FactLog, *WikiWorker, *entityPublisherStub, func(),
) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	factLog := NewFactLog(worker)
	pub := &entityPublisherStub{}
	synth := NewEntitySynthesizer(worker, factLog, pub, SynthesizerConfig{
		Threshold: 2,
		Timeout:   5 * time.Second,
		LLMCall:   llmStub,
	})
	synth.Start(context.Background())
	return synth, factLog, worker, pub, func() {
		synth.Stop()
		cancel()
		<-worker.Done()
	}
}

func TestSynthesizer_HappyPathWritesBriefWithFrontmatter(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "# Nazz\n\nUpdated body with facts.\n", nil
	}
	synth, factLog, worker, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	_, _ = factLog.Append(ctx, EntityKindPeople, "nazz", "Ex-HubSpot.", "", "pm")
	_, _ = factLog.Append(ctx, EntityKindPeople, "nazz", "Loves big swings.", "", "eng")

	if _, err := synth.EnqueueSynthesis(EntityKindPeople, "nazz", "pm"); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	waitForBriefCount(t, pub, 1, 3*time.Second)

	// Verify file exists with frontmatter keys.
	briefBytes, err := readArticle(worker.Repo(), "team/people/nazz.md")
	if err != nil {
		t.Fatalf("read brief: %v", err)
	}
	body := string(briefBytes)
	if !strings.HasPrefix(body, "---\n") {
		t.Fatalf("brief missing frontmatter: %s", body)
	}
	for _, key := range []string{lastSHAKey, lastTSKey, factCntKey} {
		if !strings.Contains(body, key+":") {
			t.Errorf("frontmatter missing key %q: %s", key, body)
		}
	}
	if !strings.Contains(body, "Updated body with facts") {
		t.Errorf("brief missing LLM body: %s", body)
	}
}

func TestSynthesizer_FreshBriefCreatedWhenNoneExists(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "# Acme\n\nNew brief from scratch.\n", nil
	}
	synth, factLog, worker, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	_, _ = factLog.Append(ctx, EntityKindCompanies, "acme", "Founded 1999.", "", "pm")

	_, _ = synth.EnqueueSynthesis(EntityKindCompanies, "acme", "pm")
	waitForBriefCount(t, pub, 1, 3*time.Second)

	bytes, err := readArticle(worker.Repo(), "team/companies/acme.md")
	if err != nil {
		t.Fatalf("read brief: %v", err)
	}
	if !strings.Contains(string(bytes), "New brief from scratch") {
		t.Errorf("missing body")
	}
}

func TestSynthesizer_NoNewFactsIsIdempotentSkip(t *testing.T) {
	var calls atomic.Int32
	stub := func(ctx context.Context, sys, user string) (string, error) {
		calls.Add(1)
		return "# Updated\n\nok\n", nil
	}
	synth, factLog, _, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	_, _ = factLog.Append(ctx, EntityKindPeople, "pm", "One fact.", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindPeople, "pm", "pm")
	waitForBriefCount(t, pub, 1, 3*time.Second)

	// Second synth with no new facts should skip — the commit timestamp
	// covers all current facts.
	waitUntilNextSecond(t)
	_, _ = synth.EnqueueSynthesis(EntityKindPeople, "pm", "pm")
	// Give the worker a moment to drain.
	time.Sleep(500 * time.Millisecond)

	if pub.briefCount() != 1 {
		t.Fatalf("expected 1 brief commit; got %d", pub.briefCount())
	}
}

func TestSynthesizer_LLMErrorPropagates(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "", fmt.Errorf("llm boom")
	}
	synth, factLog, _, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	_, _ = factLog.Append(ctx, EntityKindPeople, "x", "one", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindPeople, "x", "pm")
	// Brief should NOT be published because synth failed.
	time.Sleep(500 * time.Millisecond)
	if pub.briefCount() != 0 {
		t.Fatalf("expected no brief on llm error; got %d", pub.briefCount())
	}
}

func TestSynthesizer_TimeoutTreatedAsError(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	synth, factLog, _, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	// Force a tight timeout.
	synth.cfg.Timeout = 50 * time.Millisecond

	ctx := context.Background()
	_, _ = factLog.Append(ctx, EntityKindPeople, "y", "one", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindPeople, "y", "pm")
	time.Sleep(600 * time.Millisecond)
	if pub.briefCount() != 0 {
		t.Fatalf("expected no brief on timeout")
	}
}

func TestSynthesizer_EmptyOutputRejected(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "   ", nil
	}
	synth, factLog, _, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()
	_, _ = factLog.Append(ctx, EntityKindPeople, "z", "one", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindPeople, "z", "pm")
	time.Sleep(500 * time.Millisecond)
	if pub.briefCount() != 0 {
		t.Fatalf("expected no brief on empty output")
	}
}

func TestSynthesizer_TooLargeOutputRejected(t *testing.T) {
	big := strings.Repeat("x", MaxBriefSize+1)
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return big, nil
	}
	synth, factLog, _, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()
	_, _ = factLog.Append(ctx, EntityKindPeople, "huge", "one", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindPeople, "huge", "pm")
	time.Sleep(500 * time.Millisecond)
	if pub.briefCount() != 0 {
		t.Fatalf("expected no brief on oversized output")
	}
}

func TestSynthesizer_ContradictionPhrasePassesThrough(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		// Echo a brief that includes a contradiction callout — verifies
		// our commit path doesn't strip the phrase.
		return "# Entity\n\n**Contradiction:** fact A says X, fact B says Y.\n", nil
	}
	synth, factLog, worker, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	_, _ = factLog.Append(ctx, EntityKindPeople, "dup", "Joined 2024.", "", "pm")
	_, _ = factLog.Append(ctx, EntityKindPeople, "dup", "Joined 2025.", "", "eng")
	_, _ = synth.EnqueueSynthesis(EntityKindPeople, "dup", "pm")
	waitForBriefCount(t, pub, 1, 3*time.Second)

	bytes, err := readArticle(worker.Repo(), "team/people/dup.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(bytes), "**Contradiction:**") {
		t.Errorf("contradiction callout lost: %s", string(bytes))
	}
}

func TestSynthesizer_DebounceCoalescesInflightFollowups(t *testing.T) {
	var running atomic.Int32
	hold := make(chan struct{})
	release := make(chan struct{})
	stub := func(ctx context.Context, sys, user string) (string, error) {
		c := running.Add(1)
		if c == 1 {
			// Signal we're running, then wait for release.
			close(hold)
			<-release
		}
		return "# Ok\n\nbody\n", nil
	}
	synth, factLog, _, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	_, _ = factLog.Append(ctx, EntityKindPeople, "coalesce", "f1", "", "pm")
	if _, err := synth.EnqueueSynthesis(EntityKindPeople, "coalesce", "pm"); err != nil {
		t.Fatalf("enqueue 1: %v", err)
	}
	<-hold // synth #1 is running

	// While synth #1 is blocked, append new facts so the follow-up has
	// something to do — and fire 5 enqueue calls. All five should coalesce
	// into exactly ONE follow-up.
	waitUntilNextSecond(t)
	_, _ = factLog.Append(ctx, EntityKindPeople, "coalesce", "f2", "", "pm")
	_, _ = factLog.Append(ctx, EntityKindPeople, "coalesce", "f3", "", "pm")
	for i := 0; i < 5; i++ {
		_, _ = synth.EnqueueSynthesis(EntityKindPeople, "coalesce", "pm")
	}

	// Release synth #1.
	close(release)

	// Wait for exactly 2 brief publications (one original + one follow-up).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pub.briefCount() >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Give any runaway extras a chance to happen then assert.
	time.Sleep(400 * time.Millisecond)
	if pub.briefCount() != 2 {
		t.Fatalf("expected exactly 2 brief syntheses (original + 1 coalesced follow-up); got %d", pub.briefCount())
	}
}

func TestSynthesizer_StopPreventsNewJobs(t *testing.T) {
	synth, factLog, _, _, teardown := newSynthFixture(t, func(context.Context, string, string) (string, error) {
		return "# Ok\n\nbody\n", nil
	})
	defer teardown()
	synth.Stop()

	_, _ = factLog.Append(context.Background(), EntityKindPeople, "stopped", "x", "", "pm")
	if _, err := synth.EnqueueSynthesis(EntityKindPeople, "stopped", "pm"); err != ErrSynthesizerStopped {
		t.Fatalf("expected ErrSynthesizerStopped; got %v", err)
	}
}

// waitForBriefCount polls the publisher stub until the brief count meets n
// or the deadline hits.
func waitForBriefCount(t *testing.T, pub *entityPublisherStub, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pub.briefCount() >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d brief events; got %d", n, pub.briefCount())
}

// seedBrief writes content at the brief's canonical path under the
// archivist identity. Used by sentinel/tags tests to set up a pre-existing
// frontmatter shape before invoking synthesis.
func seedBrief(t *testing.T, worker *WikiWorker, kind EntityKind, slug, body, msg string) {
	t.Helper()
	if _, _, err := worker.Enqueue(context.Background(), ArchivistAuthor, briefPath(kind, slug), body, "replace", msg); err != nil {
		t.Fatalf("seed brief: %v", err)
	}
}

func TestSentinel_NoHumanEditFullRewrite(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "# Acme\n\nFresh body from LLM.\n", nil
	}
	synth, factLog, worker, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	seedBrief(t, worker, EntityKindCompanies, "acme",
		"---\nkind: company\nlast_synthesized_ts: 2024-01-01T00:00:00Z\n---\n\n# Acme\n\nOld body.\n",
		"seed acme brief")

	_, _ = factLog.Append(ctx, EntityKindCompanies, "acme", "Founded 1999.", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindCompanies, "acme", "pm")
	waitForBriefCount(t, pub, 1, 3*time.Second)

	got, err := readArticle(worker.Repo(), "team/companies/acme.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(got)
	if !strings.Contains(body, "Fresh body from LLM") {
		t.Errorf("expected full rewrite with LLM body; got: %s", body)
	}
	if strings.Contains(body, "Old body") {
		t.Errorf("old body should be gone in full rewrite; got: %s", body)
	}
	if strings.Contains(body, whatWeLearnedHeading) {
		t.Errorf("rewrite mode must not emit learned section; got: %s", body)
	}
}

func TestSentinel_HumanEditedSwitchesToAppendMode(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "Synth-derived insight body.\n", nil
	}
	synth, factLog, worker, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	seedBrief(t, worker, EntityKindCompanies, "acme",
		"---\nkind: company\nlast_synthesized_ts: 2024-01-01T00:00:00Z\nlast_human_edit_ts: 2025-06-01T12:00:00Z\n---\n\n# Acme\n\nHuman-authored prose that must survive.\n",
		"seed acme brief")

	_, _ = factLog.Append(ctx, EntityKindCompanies, "acme", "Acme raised Series C.", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindCompanies, "acme", "pm")
	waitForBriefCount(t, pub, 1, 3*time.Second)

	got, err := readArticle(worker.Repo(), "team/companies/acme.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	body := string(got)
	if !strings.Contains(body, "Human-authored prose that must survive") {
		t.Errorf("user body was stomped; got: %s", body)
	}
	if !strings.Contains(body, whatWeLearnedHeading) {
		t.Errorf("expected %q section; got: %s", whatWeLearnedHeading, body)
	}
	if !strings.Contains(body, "Synth-derived insight body") {
		t.Errorf("expected LLM content under learned section; got: %s", body)
	}
	// last_synthesized_ts must advance away from the seeded value.
	if strings.Contains(body, "last_synthesized_ts: 2024-01-01T00:00:00Z") {
		t.Errorf("last_synthesized_ts must be updated post-synthesis; got: %s", body)
	}
}

func TestSentinel_StaleHumanEditTriggersFullRewrite(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "# Acme\n\nReplacement body.\n", nil
	}
	synth, factLog, worker, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	// Human edit predates the last synthesis — sentinel is stale, full rewrite is safe.
	seedBrief(t, worker, EntityKindCompanies, "acme",
		"---\nkind: company\nlast_synthesized_ts: 2025-06-01T12:00:00Z\nlast_human_edit_ts: 2024-01-01T00:00:00Z\n---\n\n# Acme\n\nStale user body.\n",
		"seed acme brief")

	_, _ = factLog.Append(ctx, EntityKindCompanies, "acme", "f1", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindCompanies, "acme", "pm")
	waitForBriefCount(t, pub, 1, 3*time.Second)

	got, _ := readArticle(worker.Repo(), "team/companies/acme.md")
	body := string(got)
	if !strings.Contains(body, "Replacement body") {
		t.Errorf("expected full rewrite when sentinel is stale; got: %s", body)
	}
	if strings.Contains(body, "Stale user body") {
		t.Errorf("stale body should be gone; got: %s", body)
	}
	if strings.Contains(body, whatWeLearnedHeading) {
		t.Errorf("stale sentinel must not trigger learned section; got: %s", body)
	}
}

func TestSentinel_EmptyBriefFullCreation(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "# Acme\n\nFresh creation.\n", nil
	}
	synth, factLog, worker, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	_, _ = factLog.Append(ctx, EntityKindCompanies, "acme", "Founded 1999.", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindCompanies, "acme", "pm")
	waitForBriefCount(t, pub, 1, 3*time.Second)

	got, _ := readArticle(worker.Repo(), "team/companies/acme.md")
	body := string(got)
	if !strings.Contains(body, "Fresh creation") {
		t.Errorf("expected fresh body on first synthesis; got: %s", body)
	}
	if strings.Contains(body, whatWeLearnedHeading) {
		t.Errorf("first synthesis must not produce learned section; got: %s", body)
	}
}

func TestTags_DerivedFromKindAndSignals(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "# Sarah\n\nbody\n", nil
	}
	synth, factLog, worker, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	seedBrief(t, worker, EntityKindPeople, "sarah",
		"---\nkind: person\nsignals:\n  job_title: VP Sales\n  domain: https://acme.com/x\n---\n\n# Sarah\n\nseeded body\n",
		"seed sarah brief")

	_, _ = factLog.Append(ctx, EntityKindPeople, "sarah", "She closed an $80k deal.", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindPeople, "sarah", "pm")
	waitForBriefCount(t, pub, 1, 3*time.Second)

	got, _ := readArticle(worker.Repo(), "team/people/sarah.md")
	body := string(got)
	for _, want := range []string{"person", "vp-sales", "acme.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected derived tag %q in tags line; body: %s", want, body)
		}
	}
	// Sanity: should be a `tags:` line.
	if !strings.Contains(body, "tags:") {
		t.Errorf("expected tags frontmatter; got: %s", body)
	}
}

func TestTags_PreservesUserAddedTags(t *testing.T) {
	stub := func(ctx context.Context, sys, user string) (string, error) {
		return "# Sarah\n\nbody\n", nil
	}
	synth, factLog, worker, pub, teardown := newSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()

	seedBrief(t, worker, EntityKindPeople, "sarah",
		"---\nkind: person\ntags: [confidential, hot-lead]\nsignals:\n  job_title: VP Sales\n  domain: acme.com\n---\n\n# Sarah\n\nbody\n",
		"seed sarah brief")

	_, _ = factLog.Append(ctx, EntityKindPeople, "sarah", "f1", "", "pm")
	_, _ = synth.EnqueueSynthesis(EntityKindPeople, "sarah", "pm")
	waitForBriefCount(t, pub, 1, 3*time.Second)

	got, _ := readArticle(worker.Repo(), "team/people/sarah.md")
	body := string(got)
	for _, want := range []string{"confidential", "hot-lead", "person", "vp-sales", "acme.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected tag %q to survive merge; body: %s", want, body)
		}
	}
}

func TestTags_CapAtEightDerived(t *testing.T) {
	derived := []string{"d1", "d2", "d3", "d4", "d5", "d6", "d7", "d8", "d9", "d10"}
	body := "---\nkind: person\n---\n\n# X\n"
	out := applyTagsFrontmatter(body, derived)

	// All 10 derived would exceed the cap; only 8 must be present.
	for i := 1; i <= 8; i++ {
		want := "d" + itoaTag(i)
		if !strings.Contains(out, want) {
			t.Errorf("expected derived tag %q within cap; got: %s", want, out)
		}
	}
	for _, dropped := range []string{"d9", "d10"} {
		if strings.Contains(out, dropped) {
			t.Errorf("derived tag %q must have been capped; got: %s", dropped, out)
		}
	}
}

func TestTags_IdempotentNormalization(t *testing.T) {
	body := "---\nkind: person\nsignals:\n  job_title: VP Sales\n  domain: acme.com\n---\n\n# X\n"
	derived := deriveTagsFromBrief(body)

	once := applyTagsFrontmatter(body, derived)
	twice := applyTagsFrontmatter(once, deriveTagsFromBrief(once))

	if once != twice {
		t.Errorf("non-idempotent: \nonce:  %s\ntwice: %s", once, twice)
	}
	// No duplicate occurrences inside the tags line.
	for _, line := range strings.Split(twice, "\n") {
		if !strings.HasPrefix(line, "tags:") {
			continue
		}
		seen := map[string]int{}
		inner := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(line, "tags:")), "["), "]")
		for _, p := range strings.Split(inner, ",") {
			seen[strings.TrimSpace(p)]++
		}
		for tag, n := range seen {
			if n > 1 {
				t.Errorf("duplicate tag %q (count=%d) in: %s", tag, n, line)
			}
		}
	}
}

// itoaTag — tiny local int-to-string to avoid pulling strconv into test
// imports just for the cap test.
func itoaTag(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
