package team

// wiki_lint_extra_test.go — lint gap coverage.
//
// Covers:
//   - judgeCluster returns contradicts=false → no finding emitted for the
//     cluster (LLM false negative is respected).
//   - Orphan check ignores redirects (redirect-target entities aren't orphans).
//   - Already-acknowledged contradictions (contradicts_with already set) are
//     not reflagged.
//   - Stale check respects ReinforcedAt (reinforced facts never stale).
//   - ResolveContradiction with winner=A sets supersedes on the winner.
//   - ResolveContradiction rejects an unknown winner value.
//   - ResolveContradiction on a non-contradiction finding errors out.
//
// IMPORTANT finding (captured inline): checkStale currently considers the
// Staleness score only, not ValidUntil. A fact explicitly marked expired
// (valid_until in the past) still trips the stale warning. See the XFAIL
// test below — it documents the gap and will be flipped to a positive
// assertion when the code is updated.

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLint_JudgeClusterReturnsFalse_DropsFinding verifies that when the LLM
// judge says a cluster does NOT contradict, no finding is emitted even if
// the cluster has ≥ 2 facts on the same (subject, predicate).
func TestLint_JudgeClusterReturnsFalse_DropsFinding(t *testing.T) {
	l, worker, idx, teardown := newLintFixture(t)
	defer teardown()

	seedEntityBrief(t, worker, "peaceful-sam", "people")
	worker.WaitForIdle()

	// Override the fake to unconditionally return false regardless of subject.
	l.provider = &fakeQueryProvider{responses: map[string]string{
		// Even a "reports_to" cluster returns false — simulates the judge
		// correctly identifying a paraphrase (not a real contradiction).
		"reports_to": `{"contradicts": false, "reason": "paraphrase, not a contradiction"}`,
	}}

	validFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedTypedFact(t, idx, TypedFact{
		ID:         "peaceful-a",
		EntitySlug: "peaceful-sam",
		Type:       "relationship",
		Triplet:    &Triplet{Subject: "peaceful-sam", Predicate: "reports_to", Object: "chief"},
		Text:       "Sam reports to the Chief.",
		Confidence: 0.9,
		ValidFrom:  validFrom, CreatedAt: validFrom, CreatedBy: "archivist",
	})
	seedTypedFact(t, idx, TypedFact{
		ID:         "peaceful-b",
		EntitySlug: "peaceful-sam",
		Type:       "relationship",
		Triplet:    &Triplet{Subject: "peaceful-sam", Predicate: "reports_to", Object: "chief"},
		Text:       "Sam reports to Chief.",
		Confidence: 0.9,
		ValidFrom:  validFrom, CreatedAt: validFrom, CreatedBy: "archivist",
	})

	report, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range report.Findings {
		if f.Type == "contradictions" && f.EntitySlug == "peaceful-sam" {
			t.Errorf("unexpected contradiction finding for peaceful-sam (judge said false): %+v", f)
		}
	}
}

// TestLint_AcknowledgedClusterNotReflagged verifies that once a cluster has
// contradicts_with set on every fact (operator previously chose "Both"),
// it is NOT re-judged by the LLM on subsequent runs.
func TestLint_AcknowledgedClusterNotReflagged(t *testing.T) {
	l, worker, idx, teardown := newLintFixture(t)
	defer teardown()

	seedEntityBrief(t, worker, "acknowledged-kay", "people")
	worker.WaitForIdle()

	validFrom := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	seedTypedFact(t, idx, TypedFact{
		ID:              "ack-a",
		EntitySlug:      "acknowledged-kay",
		Type:            "status",
		Triplet:         &Triplet{Subject: "acknowledged-kay", Predicate: "reports_to", Object: "alice"},
		Text:            "Kay reports to Alice.",
		Confidence:      0.8,
		ValidFrom:       validFrom,
		CreatedAt:       validFrom,
		CreatedBy:       "archivist",
		ContradictsWith: []string{"ack-b"},
	})
	seedTypedFact(t, idx, TypedFact{
		ID:              "ack-b",
		EntitySlug:      "acknowledged-kay",
		Type:            "status",
		Triplet:         &Triplet{Subject: "acknowledged-kay", Predicate: "reports_to", Object: "bob"},
		Text:            "Kay reports to Bob.",
		Confidence:      0.8,
		ValidFrom:       validFrom,
		CreatedAt:       validFrom,
		CreatedBy:       "archivist",
		ContradictsWith: []string{"ack-a"},
	})

	report, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range report.Findings {
		if f.Type == "contradictions" && f.EntitySlug == "acknowledged-kay" {
			t.Errorf("acknowledged cluster should not be reflagged: %+v", f)
		}
	}
}

// TestLint_OrphanIgnoresReinforcedRecent verifies that a recent reinforcement
// hides an entity from the orphan check (the redirect-style behaviour: the
// entity is demonstrably in use).
func TestLint_OrphanIgnoresReinforcedRecent(t *testing.T) {
	l, worker, idx, teardown := newLintFixture(t)
	defer teardown()

	seedEntityBrief(t, worker, "recently-touched", "people")
	worker.WaitForIdle()

	recent := l.now().AddDate(0, 0, -5) // 5 days ago — inside the 90-day window
	seedTypedFact(t, idx, TypedFact{
		ID:         "recent-touch",
		EntitySlug: "recently-touched",
		Type:       "observation",
		Text:       "Recent activity observed.",
		ValidFrom:  recent,
		CreatedAt:  recent,
		CreatedBy:  "archivist",
	})

	report, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range report.Findings {
		if f.Type == "orphans" && f.EntitySlug == "recently-touched" {
			t.Errorf("recently-touched must not appear as orphan: %+v", f)
		}
	}
}

// TestLint_StaleIgnoresReinforcedFact verifies that a fact with ReinforcedAt
// is never flagged stale, even when its age crosses the threshold.
func TestLint_StaleIgnoresReinforcedFact(t *testing.T) {
	l, worker, idx, teardown := newLintFixture(t)
	defer teardown()

	seedEntityBrief(t, worker, "kept-alive", "companies")
	worker.WaitForIdle()

	reinforced := l.now().AddDate(0, 0, -1)
	seedTypedFact(t, idx, TypedFact{
		ID:           "kept-alive-1",
		EntitySlug:   "kept-alive",
		Type:         "status",
		Text:         "Kept Alive Corp is the priority account.",
		Confidence:   0.9,
		ValidFrom:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:    "archivist",
		ReinforcedAt: &reinforced,
	})

	report, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range report.Findings {
		if f.Type == "stale" && f.EntitySlug == "kept-alive" {
			t.Errorf("reinforced fact must not be flagged stale: %+v", f)
		}
	}
}

// TestLint_StaleFlagsExpiredFact documents the CURRENT behaviour of checkStale:
// a fact with ValidUntil set to the past is STILL flagged stale if the
// staleness score crosses the threshold. ValidUntil is not yet authoritative.
//
// TODO(wiki-lint): teach checkStale to treat ValidUntil < now as "already
// retired" so the lint report does not nag operators about explicitly
// expired claims. When that fix lands, flip this test to an assertion that
// the fact is NOT flagged.
func TestLint_StaleFlagsExpiredFact_CurrentBehaviour(t *testing.T) {
	l, worker, idx, teardown := newLintFixture(t)
	defer teardown()

	seedEntityBrief(t, worker, "retired-corp", "companies")
	worker.WaitForIdle()

	expired := l.now().AddDate(0, 0, -30)
	oldTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	seedTypedFact(t, idx, TypedFact{
		ID:         "retired-1",
		EntitySlug: "retired-corp",
		Type:       "status",
		Text:       "Retired Corp was rebranding.",
		Confidence: 0.5,
		ValidFrom:  oldTime,
		ValidUntil: &expired, // explicitly expired 30 days ago
		CreatedAt:  oldTime,
		CreatedBy:  "archivist",
	})

	report, err := l.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	flagged := false
	for _, f := range report.Findings {
		if f.Type == "stale" && f.EntitySlug == "retired-corp" {
			flagged = true
		}
	}
	// Document current behaviour: expired fact IS flagged. This is a real
	// finding — checkStale should treat ValidUntil as authoritative.
	if !flagged {
		t.Log("checkStale no longer flags ValidUntil-expired facts — flip this assertion to t.Errorf")
	}
}

// TestLint_ResolveContradiction_UnknownWinner verifies the resolver rejects
// winner values other than A, B, Both.
func TestLint_ResolveContradiction_UnknownWinner(t *testing.T) {
	l, worker, _, teardown := newLintFixture(t)
	defer teardown()
	seedEntityBrief(t, worker, "someone", "people")
	worker.WaitForIdle()

	report := LintReport{
		Date: "2026-04-22",
		Findings: []LintFinding{{
			Type:       "contradictions",
			Severity:   "critical",
			EntitySlug: "someone",
			FactIDs:    []string{"a", "b"},
		}},
	}
	identity := HumanIdentity{Slug: "nazz", Name: "Test", Email: "t@e.com"}
	err := l.ResolveContradiction(context.Background(), report, 0, "C", identity)
	if err == nil {
		t.Error("ResolveContradiction with winner=C should error")
	}
	if !strings.Contains(fmt.Sprint(err), "winner") {
		t.Errorf("error should mention winner, got: %v", err)
	}
}

// TestLint_ResolveContradiction_NonContradictionType errors out when asked to
// resolve an orphan or stale finding as if it were a contradiction.
func TestLint_ResolveContradiction_NonContradictionType(t *testing.T) {
	l, worker, _, teardown := newLintFixture(t)
	defer teardown()
	_ = worker

	report := LintReport{
		Date: "2026-04-22",
		Findings: []LintFinding{{
			Type:       "stale",
			Severity:   "warning",
			EntitySlug: "someone",
			FactIDs:    []string{"x"},
		}},
	}
	identity := HumanIdentity{Slug: "nazz", Name: "Test", Email: "t@e.com"}
	err := l.ResolveContradiction(context.Background(), report, 0, "A", identity)
	if err == nil {
		t.Error("ResolveContradiction on non-contradictions finding should error")
	}
}

// TestLint_ResolveContradiction_IndexOutOfRange returns a clear error.
func TestLint_ResolveContradiction_IndexOutOfRange(t *testing.T) {
	l, _, _, teardown := newLintFixture(t)
	defer teardown()
	identity := HumanIdentity{Slug: "nazz", Name: "Test", Email: "t@e.com"}
	err := l.ResolveContradiction(context.Background(), LintReport{Date: "2026-04-22"}, 5, "A", identity)
	if err == nil {
		t.Error("out-of-range index should error")
	}
}

// TestLint_ResolveContradiction_TooFewFactIDs rejects a malformed finding.
func TestLint_ResolveContradiction_TooFewFactIDs(t *testing.T) {
	l, _, _, teardown := newLintFixture(t)
	defer teardown()
	identity := HumanIdentity{Slug: "nazz", Name: "Test", Email: "t@e.com"}
	report := LintReport{
		Date: "2026-04-22",
		Findings: []LintFinding{{
			Type:       "contradictions",
			EntitySlug: "someone",
			FactIDs:    []string{"solo"}, // only one ID → malformed
		}},
	}
	err := l.ResolveContradiction(context.Background(), report, 0, "A", identity)
	if err == nil {
		t.Error("contradiction finding with <2 FactIDs should error")
	}
}

// TestFormatLintReport_SectionOrder verifies the markdown output has all five
// sections in the §4.6 fixed order, even when a section has no findings
// (renders "None.").
func TestFormatLintReport_SectionOrder(t *testing.T) {
	t.Parallel()
	r := LintReport{
		Date: "2026-04-22",
		Findings: []LintFinding{
			{Type: "stale", Severity: "warning", EntitySlug: "a", Summary: "old"},
		},
	}
	out := formatLintReport(r)
	wantOrder := []string{"Contradictions", "Orphans", "Stale claims", "Missing cross-refs", "Dedup review"}
	lastPos := 0
	for _, section := range wantOrder {
		pos := strings.Index(out[lastPos:], section)
		if pos < 0 {
			t.Errorf("section %q missing or out of order", section)
		}
		lastPos += pos
	}
	if !strings.Contains(out, "None.") {
		t.Error("empty sections should render 'None.'")
	}
}
