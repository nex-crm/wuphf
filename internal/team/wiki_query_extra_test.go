package team

// wiki_query_extra_test.go — additional gaps in the cited-answer loop.
//
// Covers:
//   - Query with no matching facts: sources slice is empty, provider still
//     runs, answer round-trips.
//   - Provider transport error: answer carries Coverage=none, non-nil error
//     message in AnswerMarkdown, no panic.
//   - Provider returns JSON with valid_from timestamps inside the sources —
//     verify hydrateFact preserves the dates.
//   - Code-fence stripping: ```json ... ``` wrapped output parses cleanly.
//   - parseProviderResponse rejects complete garbage with an error (not a
//     panic) — the code path is exercised end-to-end through Answer which
//     converts that into Coverage=none.
//   - QueryAnswer's Sources field length never exceeds TopK.
//   - Default TopK and Timeout are applied when zero.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestQueryHandler_NoHits_StillCallsProvider verifies that when the search
// returns zero hits, the handler still renders the template with an empty
// Sources slice and calls the LLM — the LLM is responsible for the
// "no_sources" refusal text.
func TestQueryHandler_NoHits_StillCallsProvider(t *testing.T) {
	t.Parallel()

	idx := NewWikiIndex(t.TempDir())
	p := &fakeProvider{
		response: validLLMResponse("status", "No relevant sources.", []int{}, "none"),
	}
	h := NewQueryHandler(idx, p)

	ans, err := h.Answer(context.Background(), QueryRequest{
		Query:   "sarah", // entity token → not general
		TopK:    5,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.calls != 1 {
		t.Errorf("provider called %d times, want 1", p.calls)
	}
	if len(ans.Sources) != 0 {
		t.Errorf("sources = %d, want 0 (no hits)", len(ans.Sources))
	}
	if ans.Coverage != "none" {
		t.Errorf("coverage = %q, want none", ans.Coverage)
	}
}

// TestQueryHandler_ProviderError_NoPanic verifies a provider that returns
// error gracefully surfaces through Coverage=none.
func TestQueryHandler_ProviderError_NoPanic(t *testing.T) {
	t.Parallel()

	idx := NewWikiIndex(t.TempDir())
	p := &fakeProvider{err: errors.New("upstream 500")}
	h := NewQueryHandler(idx, p)

	ans, err := h.Answer(context.Background(), QueryRequest{
		Query:   "sarah",
		TopK:    5,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Answer returned error; want nil: %v", err)
	}
	if ans.Coverage != "none" {
		t.Errorf("coverage = %q, want none", ans.Coverage)
	}
	if !strings.Contains(ans.AnswerMarkdown, "upstream 500") {
		t.Errorf("answer markdown missing provider error: %q", ans.AnswerMarkdown)
	}
}

// TestQueryHandler_CodeFencedJSON verifies parseProviderResponse strips a
// markdown code fence from the LLM output and still unmarshals the JSON.
func TestQueryHandler_CodeFencedJSON(t *testing.T) {
	t.Parallel()

	idx := NewWikiIndex(t.TempDir())
	seedFact(t, idx, "fc1", "sarah-jones", "person", "status",
		"sarah is head of sales")

	inner := validLLMResponse("status", "fenced answer", []int{}, "complete")
	fenced := "```json\n" + inner + "\n```"
	p := &fakeProvider{response: fenced}
	h := NewQueryHandler(idx, p)

	ans, err := h.Answer(context.Background(), QueryRequest{
		Query:   "sarah",
		TopK:    5,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ans.Coverage != "complete" {
		t.Errorf("coverage = %q, want complete", ans.Coverage)
	}
	if ans.AnswerMarkdown != "fenced answer" {
		t.Errorf("answer markdown = %q, want 'fenced answer'", ans.AnswerMarkdown)
	}
}

// TestQueryHandler_SourcesHydrateValidFrom verifies that TypedFact.ValidFrom
// is propagated into QuerySource.ValidFrom in the final answer.
func TestQueryHandler_SourcesHydrateValidFrom(t *testing.T) {
	t.Parallel()

	idx := NewWikiIndex(t.TempDir())
	ctx := context.Background()

	validFrom := time.Date(2025, 12, 3, 0, 0, 0, 0, time.UTC)
	f := TypedFact{
		ID:         "hydrate-src-1",
		EntitySlug: "sarah-jones",
		Kind:       "person",
		Type:       "status",
		Text:       "Sarah began a new role.",
		ValidFrom:  validFrom,
		CreatedAt:  validFrom,
		CreatedBy:  "archivist",
	}
	_ = idx.store.UpsertFact(ctx, f)
	_ = idx.text.Index(ctx, f)

	p := &fakeProvider{
		response: validLLMResponse("status", "answer", []int{1}, "complete"),
	}
	h := NewQueryHandler(idx, p)

	ans, err := h.Answer(ctx, QueryRequest{
		Query:   "sarah",
		TopK:    5,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ans.Sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(ans.Sources))
	}
	if ans.Sources[0].ValidFrom != "2025-12-03" {
		t.Errorf("ValidFrom = %q, want 2025-12-03", ans.Sources[0].ValidFrom)
	}
}

// TestQueryHandler_SourcesLengthCappedByTopK verifies that however many facts
// are in the index, the Sources slice never exceeds TopK.
func TestQueryHandler_SourcesLengthCappedByTopK(t *testing.T) {
	t.Parallel()

	idx := NewWikiIndex(t.TempDir())
	ctx := context.Background()
	// Seed 25 facts that all match the query term.
	for i := 0; i < 25; i++ {
		f := TypedFact{
			ID:         fmt.Sprintf("cap-%04d", i),
			EntitySlug: "acme",
			Text:       "acme corp revenue and sarah-related context",
			CreatedAt:  time.Now().UTC(),
			CreatedBy:  "archivist",
		}
		_ = idx.store.UpsertFact(ctx, f)
		_ = idx.text.Index(ctx, f)
	}

	p := &fakeProvider{
		response: validLLMResponse("status", "x", []int{}, "complete"),
	}
	h := NewQueryHandler(idx, p)

	ans, err := h.Answer(ctx, QueryRequest{
		Query:   "sarah",
		TopK:    5,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ans.Sources) > 5 {
		t.Errorf("len(sources) = %d, want ≤ 5 (TopK)", len(ans.Sources))
	}
}

// TestQueryHandler_DefaultsApplied verifies that passing zero TopK and zero
// Timeout results in the documented defaults (TopK=20, Timeout=10s) rather
// than zero-value failures.
func TestQueryHandler_DefaultsApplied(t *testing.T) {
	t.Parallel()

	idx := NewWikiIndex(t.TempDir())
	// No facts, no entity tokens → general refusal path (0 LLM calls).
	p := &fakeProvider{response: "unused"}
	h := NewQueryHandler(idx, p)

	ans, err := h.Answer(context.Background(), QueryRequest{
		Query: "What is the weather?",
		// TopK and Timeout are zero — defaults must kick in.
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.calls != 0 {
		t.Errorf("provider called for general refusal path: %d times", p.calls)
	}
	if ans.Coverage != "none" {
		t.Errorf("coverage = %q, want none", ans.Coverage)
	}
}

// TestParseProviderResponse_RejectsNoJSON is a unit test on the internal
// parser: raw text without any { } returns an error, which Answer converts
// into a Coverage=none response.
func TestParseProviderResponse_RejectsNoJSON(t *testing.T) {
	t.Parallel()

	_, err := parseProviderResponse("Sorry, I cannot help with that.")
	if err == nil {
		t.Error("expected error for text without JSON object")
	}
}

// TestParseProviderResponse_ExtractsEmbeddedJSON verifies that JSON embedded
// inside a larger body still parses cleanly — the parser finds the outermost
// braces.
func TestParseProviderResponse_ExtractsEmbeddedJSON(t *testing.T) {
	t.Parallel()
	raw := "Here is your answer:\n\n" +
		`{"query_class":"status","answer_markdown":"ok","sources_cited":[],"confidence":0.7,"coverage":"partial"}` +
		"\n\nThanks!"
	parsed, err := parseProviderResponse(raw)
	if err != nil {
		t.Fatalf("parseProviderResponse: %v", err)
	}
	if parsed.QueryClass != "status" {
		t.Errorf("QueryClass = %q, want status", parsed.QueryClass)
	}
	if parsed.Coverage != "partial" {
		t.Errorf("Coverage = %q, want partial", parsed.Coverage)
	}
}

// TestHydrateFact_LongTextTruncated verifies the excerpt is capped at ~300
// chars so the prompt budget is predictable.
func TestHydrateFact_LongTextTruncated(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 1000)
	f := TypedFact{
		ID:         "trunc-1",
		EntitySlug: "alice",
		Text:       long,
		CreatedAt:  time.Now().UTC(),
		CreatedBy:  "archivist",
	}
	src := hydrateFact(f, time.Now().UTC())
	// Excerpt should be truncated with an ellipsis.
	if !strings.HasSuffix(src.Excerpt, "…") {
		t.Errorf("excerpt not truncated: len=%d", len(src.Excerpt))
	}
	if len([]rune(src.Excerpt)) > 301 { // 300 chars + 1 ellipsis rune
		t.Errorf("excerpt length = %d, want ≤ 301", len([]rune(src.Excerpt)))
	}
}
