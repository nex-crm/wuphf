package team

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestBuildPlaybookSynthUserPromptV2_RendersClusters asserts that a canned
// cluster set shows up verbatim in the v2 prompt body under the
// "# Reinforced patterns across entities" section, and that rule 4a about
// the output "## Patterns across entities" section is present.
func TestBuildPlaybookSynthUserPromptV2_RendersClusters(t *testing.T) {
	source := `---
author: pm
---

# Churn prevention

1. Page the CSM.
`
	execs := []Execution{
		{
			Slug:       "churn-prevention",
			Outcome:    PlaybookOutcomeSuccess,
			Summary:    "Saved the account.",
			RecordedBy: "cmo",
			CreatedAt:  time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC),
		},
	}
	clusters := []FactCluster{
		{Predicate: "champions", Object: "q2-pilot", Entities: []string{"alice", "bob", "carol"}, Count: 3},
		{Predicate: "works_at", Object: "acme-corp", Entities: []string{"dave", "eve"}, Count: 2},
	}

	got, err := buildPlaybookSynthUserPromptV2(source, execs, clusters)
	if err != nil {
		t.Fatalf("render v2: %v", err)
	}

	mustContain(t, got, "# Existing playbook")
	mustContain(t, got, "# Recent executions (newest first, max 20)")
	mustContain(t, got, "# Reinforced patterns across entities")
	mustContain(t, got, "3 entities share `champions → q2-pilot`")
	mustContain(t, got, "2 entities share `works_at → acme-corp`")
	// Entity slugs ARE listed in the prompt input so the model has grounding,
	// but rule 4 tells the model to cite counts only in the output.
	mustContain(t, got, "(alice, bob, carol)")
	mustContain(t, got, "## Patterns across entities")
	// Contradiction-handling rule from v1 is preserved.
	mustContain(t, got, "**Contradiction:**")
	mustContain(t, got, WhatWeveLearnedHeading)
}

// TestBuildPlaybookSynthUserPromptV2_EmptyClusters verifies the template's
// explicit "no clusters detected" branch so an empty cluster slice still
// produces a valid prompt and the model is instructed to OMIT the patterns
// section.
func TestBuildPlaybookSynthUserPromptV2_EmptyClusters(t *testing.T) {
	source := `---
author: pm
---

# Churn prevention
`
	got, err := buildPlaybookSynthUserPromptV2(source, nil, nil)
	if err != nil {
		t.Fatalf("render v2 empty: %v", err)
	}
	mustContain(t, got, "_No cross-entity reinforced patterns were detected")
	mustContain(t, got, "OMIT this section entirely")
	mustContain(t, got, "_No executions provided._")
}

// TestBuildPlaybookSynthUserPromptV2_IsIdempotent asserts identical input
// produces byte-identical output — i.e. the render is a pure function of
// its inputs. This pins cache-key stability for upstream prompt caching.
//
// NOT a semantic-quality eval. Whether the prompt produces GOOD synthesis
// output is a separate concern belonging to a golden-harness run planned
// for Slice 3; this test only guards against accidental non-determinism
// sneaking into the template (e.g. a map iteration in a helper).
func TestBuildPlaybookSynthUserPromptV2_IsIdempotent(t *testing.T) {
	source := "---\nauthor: pm\n---\n\n# Churn prevention\n"
	execs := []Execution{
		{Slug: "churn", Outcome: PlaybookOutcomeSuccess, Summary: "ok", RecordedBy: "cmo", CreatedAt: time.Date(2026, 4, 20, 10, 0, 0, 0, time.UTC)},
	}
	clusters := []FactCluster{
		{Predicate: "champions", Object: "q2-pilot", Entities: []string{"alice", "bob", "carol"}, Count: 3},
	}

	a, err := buildPlaybookSynthUserPromptV2(source, execs, clusters)
	if err != nil {
		t.Fatalf("render 1: %v", err)
	}
	b, err := buildPlaybookSynthUserPromptV2(source, execs, clusters)
	if err != nil {
		t.Fatalf("render 2: %v", err)
	}
	if a != b {
		t.Fatalf("prompt render is non-deterministic\n first:\n%s\n second:\n%s", a, b)
	}
}

// TestPlaybookSynthesisWithClusters is the end-to-end check that wiring a
// ClusterSource on PlaybookSynthesizerConfig routes the synthesis path
// through the v2 prompt. The stub LLM asserts the user prompt contains the
// cross-entity pattern section and returns a well-formed draft; the
// committed playbook must include the Patterns section.
func TestPlaybookSynthesisWithClusters(t *testing.T) {
	// Seed a FactStore with a cross-entity reinforced cluster.
	store := newClusterTestStore(t, []TypedFact{
		reinforcedFact("alice", "champions", "q2-pilot", true),
		reinforcedFact("bob", "champions", "q2-pilot", true),
		reinforcedFact("carol", "champions", "q2-pilot", true),
	})

	var capturedPrompt string
	stub := func(ctx context.Context, sys, user string) (string, error) {
		capturedPrompt = user
		return `## What we've learned

- CSM pages land faster than email.

## Patterns across entities

- 3 entities share champions → q2-pilot. Lean on the shared champions when escalating retention plays.
`, nil
	}

	synth, execLog, worker, pub, teardown := newPlaybookSynthFixtureWithCluster(t, stub, store, 2)
	defer teardown()
	ctx := context.Background()

	writePlaybookSource(t, worker, "retention", seededPlaybookBody)
	if _, err := execLog.Append(ctx, "retention", PlaybookOutcomeSuccess, "Saved.", "", "cmo"); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if _, err := execLog.Append(ctx, "retention", PlaybookOutcomePartial, "Blocked on legal.", "", "cmo"); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	if _, err := synth.SynthesizeNow(ctx, "retention", "human"); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	waitForSynthCount(t, pub, 1, 3*time.Second)

	// Prompt assertions: the v2 body went to the LLM.
	mustContain(t, capturedPrompt, "# Reinforced patterns across entities")
	mustContain(t, capturedPrompt, "3 entities share `champions → q2-pilot`")
	mustContain(t, capturedPrompt, "## Patterns across entities")

	// Commit assertions: the author body survived verbatim, the learnings
	// section landed, and the patterns section from the draft came through.
	bytes, err := readArticle(worker.Repo(), playbookSourceRel("retention"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(bytes)
	mustContain(t, got, "1. Pull the account's ARR.")
	mustContain(t, got, WhatWeveLearnedHeading)
	mustContain(t, got, "CSM pages land faster than email.")
	mustContain(t, got, "## Patterns across entities")
	mustContain(t, got, "3 entities share champions")
}

// TestPlaybookSynthesisFallsBackWhenClusterSourceNil verifies the additive
// rollout guarantee: leaving ClusterSource unset runs the v1 prompt, byte-
// for-byte identical to the pre-Thread-C code path.
func TestPlaybookSynthesisFallsBackWhenClusterSourceNil(t *testing.T) {
	var capturedPrompt string
	stub := func(ctx context.Context, sys, user string) (string, error) {
		capturedPrompt = user
		return "## What we've learned\n\n- CSM pages land faster than email.\n", nil
	}
	synth, execLog, worker, pub, teardown := newPlaybookSynthFixture(t, stub)
	defer teardown()
	ctx := context.Background()
	writePlaybookSource(t, worker, "no-clusters", seededPlaybookBody)
	if _, err := execLog.Append(ctx, "no-clusters", PlaybookOutcomeSuccess, "Saved.", "", "cmo"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := execLog.Append(ctx, "no-clusters", PlaybookOutcomeSuccess, "Again.", "", "cmo"); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if _, err := synth.SynthesizeNow(ctx, "no-clusters", "human"); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	waitForSynthCount(t, pub, 1, 3*time.Second)

	if strings.Contains(capturedPrompt, "Reinforced patterns across entities") {
		t.Errorf("v1 fallback should NOT include the v2 pattern section; prompt was:\n%s", capturedPrompt)
	}
	if !strings.Contains(capturedPrompt, "# Existing playbook") {
		t.Errorf("v1 prompt body missing; prompt was:\n%s", capturedPrompt)
	}
}

// TestPlaybookSynthesisEmptyClusterSet routes through v2 even when the
// scan yields zero clusters. The prompt must carry the "no patterns"
// marker and the resulting playbook should NOT contain a
// "## Patterns across entities" section (the model honored the OMIT rule).
func TestPlaybookSynthesisEmptyClusterSet(t *testing.T) {
	// FactStore with no reinforced cross-entity pairs.
	emptyStore := newClusterTestStore(t, nil)

	var capturedPrompt string
	stub := func(ctx context.Context, sys, user string) (string, error) {
		capturedPrompt = user
		// Model honors the OMIT rule from the template.
		return "## What we've learned\n\n- CSM pages land faster than email.\n", nil
	}

	synth, execLog, worker, pub, teardown := newPlaybookSynthFixtureWithCluster(t, stub, emptyStore, 2)
	defer teardown()
	ctx := context.Background()
	writePlaybookSource(t, worker, "quiet", seededPlaybookBody)
	if _, err := execLog.Append(ctx, "quiet", PlaybookOutcomeSuccess, "Saved.", "", "cmo"); err != nil {
		t.Fatalf("append: %v", err)
	}
	if _, err := execLog.Append(ctx, "quiet", PlaybookOutcomeSuccess, "Again.", "", "cmo"); err != nil {
		t.Fatalf("append 2: %v", err)
	}
	if _, err := synth.SynthesizeNow(ctx, "quiet", "human"); err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	waitForSynthCount(t, pub, 1, 3*time.Second)

	mustContain(t, capturedPrompt, "_No cross-entity reinforced patterns were detected")

	bytes, err := readArticle(worker.Repo(), playbookSourceRel("quiet"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	got := string(bytes)
	if strings.Contains(got, "## Patterns across entities") {
		t.Errorf("empty-cluster run should not land a patterns section; got:\n%s", got)
	}
	mustContain(t, got, WhatWeveLearnedHeading)
}

// newPlaybookSynthFixtureWithCluster mirrors newPlaybookSynthFixture but
// injects a ClusterSource + minEntities override for Thread C tests.
func newPlaybookSynthFixtureWithCluster(
	t *testing.T,
	llmStub func(ctx context.Context, sys, user string) (string, error),
	clusterSource FactStore,
	minEntities int,
) (*PlaybookSynthesizer, *ExecutionLog, *WikiWorker, *playbookPublisherStub, func()) {
	t.Helper()
	root := fmt.Sprintf("%s/wiki", t.TempDir())
	backup := fmt.Sprintf("%s/wiki.bak", t.TempDir())
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	execLog := NewExecutionLog(worker)
	pub := &playbookPublisherStub{}
	synth := NewPlaybookSynthesizer(worker, execLog, pub, PlaybookSynthesizerConfig{
		Threshold:          2,
		Timeout:            5 * time.Second,
		LLMCall:            llmStub,
		ClusterSource:      clusterSource,
		ClusterMinEntities: minEntities,
	})
	synth.Start(context.Background())

	teardown := func() {
		synth.Stop()
		cancel()
		<-worker.Done()
	}
	return synth, execLog, worker, pub, teardown
}

func mustContain(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("expected output to contain %q; got:\n%s", want, got)
	}
}
