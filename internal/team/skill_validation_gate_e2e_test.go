package team

// skill_validation_gate_e2e_test.go exercises the defaultSkillValidationGate
// end-to-end with:
//
//  1. The real fixture files under /Users/belovetech/.wuphf/wiki (or skipped
//     when that path does not exist, so CI is not broken by a missing wiki).
//  2. A fake Anthropic HTTP server wired through rewriteTransport — no live
//     API key required; all judge calls are intercepted locally.
//  3. Synthesizer-level integration via a wikiRootOverrideGate wrapper that
//     threads a known wikiRoot into the gate when resolveWikiRoot() returns ""
//     (the no-wiki-worker case that applies to all newTestBroker tests).
//
// Bugs this suite is designed to catch:
//   - Fixture format mismatch: real files use \n\n---\n\n; splitFixtures must
//     still split correctly (covered by TestE2E_RealFixtures_SplitCorrectly).
//   - wikiRoot="" silent skip: when wikiRoot is empty the gate returns (false,nil)
//     and increments ValidationGateNoFixtures rather than blocking the proposal.
//   - Enhance-path baseline: the gate reads the existing skill body before making
//     the LLM call; the enhance path must pass the right baselineBody.
//   - Context cancellation: a cancelled context degrades all fixtures to
//     equivalent, which then triggers the "no improvement" rejection.
//   - Cache hit: two identical judge calls must only hit the HTTP server once.
//   - Judge response parsing: fenced JSON, extra whitespace, unknown verdict.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// realWikiFixturesDir returns the path to the live fixture directory.
// Returns "" when the directory does not exist (CI / fresh clone).
func realWikiFixturesDir() string {
	const wikiRoot = "/Users/belovetech/.wuphf/wiki"
	dir := filepath.Join(wikiRoot, "team", "skills", ".fixtures")
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return wikiRoot
}

// readCurrentSkillBody reads a skill file from the wiki and returns only the
// body — everything after the closing YAML frontmatter delimiter (---).
// Returns "" when the file is missing or has no frontmatter.
func readCurrentSkillBody(t *testing.T, wikiRoot, slug string) string {
	t.Helper()
	path := filepath.Join(wikiRoot, "team", "skills", slug+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Logf("readCurrentSkillBody: %v", err)
		return ""
	}
	content := string(raw)
	// Strip YAML frontmatter: find the second "---" delimiter.
	const delim = "---"
	first := strings.Index(content, delim)
	if first == -1 {
		return content
	}
	second := strings.Index(content[first+len(delim):], "\n"+delim)
	if second == -1 {
		return content
	}
	body := content[first+len(delim)+second+1+len(delim):]
	return strings.TrimSpace(body)
}

// fakeAnthropicServer builds an httptest.Server that returns a single
// repeating verdict for every /v1/messages call.
func fakeAnthropicServer(t *testing.T, verdict fixtureVerdict) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := map[string]any{
			"content": []map[string]any{
				{
					"type": "text",
					"text": fmt.Sprintf(`{"verdict":%q,"reasoning":"e2e stub"}`, verdict),
				},
			},
		}
		b, _ := json.Marshal(payload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
}

// fakeAnthropicServerSequence serves verdicts from a slice in order.
// Once exhausted, every subsequent call returns the last verdict.
func fakeAnthropicServerSequence(t *testing.T, verdicts []fixtureVerdict) (srv *httptest.Server, callCount *atomic.Int64) {
	t.Helper()
	var n atomic.Int64
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(n.Add(1) - 1)
		v := verdicts[len(verdicts)-1]
		if idx < len(verdicts) {
			v = verdicts[idx]
		}
		payload := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": fmt.Sprintf(`{"verdict":%q,"reasoning":"seq stub"}`, v)},
			},
		}
		b, _ := json.Marshal(payload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	return srv, &n
}

// gateWithFakeServer returns a defaultSkillValidationGate whose HTTP client
// routes all calls through the given fake server.
func gateWithFakeServer(t *testing.T, srv *httptest.Server) *defaultSkillValidationGate {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "test-e2e-key")
	g := newDefaultSkillValidationGate()
	g.httpClient = &http.Client{Transport: rewriteTransport{target: srv.URL}}
	return g
}

// ---------------------------------------------------------------------------
// Bug 1 — Fixture format: real files use \n\n---\n\n — must parse to 5 sections
// ---------------------------------------------------------------------------

func TestE2E_RealFixtures_SplitCorrectly(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	slugs := []string{
		"explore-data-layer",
		"explore-frontend-layer",
		"map-service-architecture",
		"orchestrate-codebase-exploration",
		"trace-call-graph",
		"review-codebase-exploration-findings",
	}
	for _, slug := range slugs {
		t.Run(slug, func(t *testing.T) {
			fixtures, err := loadFixturesForSlug(wikiRoot, slug)
			if err != nil {
				t.Fatalf("loadFixturesForSlug: %v", err)
			}
			if len(fixtures) != 5 {
				t.Fatalf("expected 5 fixtures, got %d", len(fixtures))
			}
			for i, f := range fixtures {
				if strings.TrimSpace(f) == "" {
					t.Fatalf("fixture %d is empty after trimming", i)
				}
				// Each fixture should be a non-trivial task description.
				if len(f) < 30 {
					t.Fatalf("fixture %d too short (%d chars): %q", i, len(f), f)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Bug 2 — Gate reads real fixtures + fake judge: acceptance path
// ---------------------------------------------------------------------------

func TestE2E_Gate_AcceptsWhenBetterOnRealFixtures(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	srv := fakeAnthropicServer(t, verdictBetter)
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	// A rich candidate body for explore-data-layer.
	candidate := "## Steps\n" +
		"1. Search for *.sql, sqlc.yaml, *.atlas.hcl.\n" +
		"2. Read the base DDL and identify tables, FKs, JSONB columns.\n" +
		"3. Read SQLC query files for CRUD vs append-only patterns.\n" +
		"4. Scan migration history for schema evolution clues.\n" +
		"5. Map Kafka event types to tables or projections they write to.\n" +
		"6. Write notebook entry: storage engine, schema overview, query patterns, event schema, surprises, open questions.\n" +
		"7. Broadcast team summary under 10 lines.\n" +
		"8. Complete or submit the task.\n"

	hasFixtures, err := gate.Validate(context.Background(), "explore-data-layer", candidate, "", wikiRoot)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true (real fixture file exists)")
	}
	if err != nil {
		t.Fatalf("expected acceptance, got rejection: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Bug 3 — Gate reads real fixtures + fake judge: regression rejection path
// ---------------------------------------------------------------------------

func TestE2E_Gate_RejectsOnRegressionWithRealFixtures(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	srv := fakeAnthropicServer(t, verdictWorse)
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	hasFixtures, err := gate.Validate(context.Background(), "trace-call-graph", "step 1. do the thing.", "", wikiRoot)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	if err == nil {
		t.Fatal("expected rejection for all-worse verdict, got nil")
	}
	if !containsStr(err.Error(), "regresses") {
		t.Fatalf("expected 'regresses' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Bug 4 — All-equivalent rejection with real fixtures
// ---------------------------------------------------------------------------

func TestE2E_Gate_RejectsAllEquivalentWithRealFixtures(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	srv := fakeAnthropicServer(t, verdictEquivalent)
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	hasFixtures, err := gate.Validate(context.Background(), "map-service-architecture", "do the thing", "", wikiRoot)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	if err == nil {
		t.Fatal("expected 'no improvement' rejection, got nil")
	}
	if !containsStr(err.Error(), "no improvement") {
		t.Fatalf("expected 'no improvement' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Bug 5 — Exactly one win wins the gate (even with many equivalents)
// ---------------------------------------------------------------------------

func TestE2E_Gate_OneWinAmongEquivalentsAccepts(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	// Return "better" only for fixture 1; the remaining 4 return "equivalent".
	verdicts := []fixtureVerdict{verdictBetter, verdictEquivalent, verdictEquivalent, verdictEquivalent, verdictEquivalent}
	srv, callCount := fakeAnthropicServerSequence(t, verdicts)
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	candidate := "## Steps\n" +
		"1. Scope check — confirm the repo path.\n" +
		"2. Spin up OFFICE-36 reviewer gate.\n" +
		"3. Create 5 parallel exploration Issues.\n" +
		"4. Tag specialists in kickoff broadcast.\n" +
		"5. Wait for notifications — do NOT poll.\n"

	hasFixtures, err := gate.Validate(context.Background(), "orchestrate-codebase-exploration", candidate, "", wikiRoot)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	if err != nil {
		t.Fatalf("expected acceptance with 1 win / 0 losses, got: %v", err)
	}
	// All 5 fixtures should have been judged.
	if callCount.Load() != 5 {
		t.Fatalf("expected 5 HTTP calls (one per fixture), got %d", callCount.Load())
	}
}

// ---------------------------------------------------------------------------
// Bug 6 — Cache: identical call pair must hit the server only once
// ---------------------------------------------------------------------------

func TestE2E_Gate_CacheDeduplicatesIdenticalCalls(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	srv, callCount := fakeAnthropicServerSequence(t,
		// Five fixtures, all "better".
		[]fixtureVerdict{verdictBetter, verdictBetter, verdictBetter, verdictBetter, verdictBetter},
	)
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	cand := "same-candidate-body"
	base := ""

	// First call: 5 LLM round-trips.
	_, _ = gate.Validate(context.Background(), "explore-data-layer", cand, base, wikiRoot)
	firstCount := callCount.Load()

	// Second call with identical inputs: all 5 results should be cache hits.
	_, _ = gate.Validate(context.Background(), "explore-data-layer", cand, base, wikiRoot)
	secondCount := callCount.Load()

	if secondCount != firstCount {
		t.Fatalf("second call made %d extra HTTP calls (want 0 — all cache hits)",
			secondCount-firstCount)
	}
}

// ---------------------------------------------------------------------------
// Bug 7 — wikiRoot="" graceful skip (no wiki worker in test broker)
// ---------------------------------------------------------------------------

func TestE2E_Gate_EmptyWikiRootSkipsGracefully(t *testing.T) {
	srv := fakeAnthropicServer(t, verdictBetter) // would be called if wiki existed
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	// Pass wikiRoot="" — simulates resolveWikiRoot() returning "" when
	// broker.wikiWorker is nil (the standard newTestBroker condition).
	hasFixtures, err := gate.Validate(context.Background(), "explore-data-layer", "anything", "", "")
	if err != nil {
		t.Fatalf("expected nil error for empty wikiRoot, got: %v", err)
	}
	if hasFixtures {
		t.Fatal("expected hasFixtures=false for empty wikiRoot")
	}
}

// ---------------------------------------------------------------------------
// Bug 8 — Context cancellation degrades all fixtures to equivalent → rejection
// ---------------------------------------------------------------------------

func TestE2E_Gate_CancelledContextRejectsWithNoImprovement(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}

	// Slow server: 200ms per call. Context is cancelled after 50ms.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read the body to avoid broken-pipe on the client side.
		_, _ = io.ReadAll(r.Body)
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"verdict\":\"better\",\"reasoning\":\"ok\"}"}]}`))
	}))
	defer srv.Close()

	gate := gateWithFakeServer(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	hasFixtures, err := gate.Validate(ctx, "explore-data-layer", "candidate body", "", wikiRoot)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true (fixture file exists)")
	}
	// All judge calls fail due to context deadline → all equivalent → no improvement.
	if err == nil {
		t.Fatal("expected 'no improvement' rejection when context cancelled, got nil")
	}
	if !containsStr(err.Error(), "no improvement") {
		t.Fatalf("expected 'no improvement' in error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Bug 9 — HTTP 500 from judge degrades, does not panic
// ---------------------------------------------------------------------------

func TestE2E_Gate_HTTP500DegradesCleanly(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"overloaded"}`))
	}))
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	hasFixtures, err := gate.Validate(context.Background(), "trace-call-graph", "candidate", "", wikiRoot)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	// All 5 calls fail → all equivalent → 0 wins, 0 losses → "no improvement".
	if err == nil {
		t.Fatal("expected rejection when all judges return HTTP 500, got nil")
	}
	if !containsStr(err.Error(), "no improvement") {
		t.Fatalf("expected 'no improvement', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Bug 10 — Malformed JSON response from judge degrades cleanly
// ---------------------------------------------------------------------------

func TestE2E_Gate_MalformedJSONDegrades(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		payload := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "Sure! I think the candidate is better. (not valid JSON)"},
			},
		}
		b, _ := json.Marshal(payload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	hasFixtures, err := gate.Validate(context.Background(), "explore-data-layer", "candidate", "", wikiRoot)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	// Unparseable verdict → equivalent for each fixture → no improvement.
	if err == nil {
		t.Fatal("expected rejection when all verdicts are unparseable, got nil")
	}
}

// ---------------------------------------------------------------------------
// Bug 11 — Synthesizer: real gate + no wiki worker → ValidationGateNoFixtures
// ---------------------------------------------------------------------------

func TestE2E_Synthesizer_RealGate_NoWikiWorker_AlwaysSkips(t *testing.T) {
	// When resolveWikiRoot() returns "" (no wiki worker, which is always the
	// case for newTestBroker), the real gate must skip and increment the
	// ValidationGateNoFixtures metric — it must NOT block the proposal.
	t.Setenv("ANTHROPIC_API_KEY", "test-e2e-key")

	b := newTestBroker(t)
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm:   SkillFrontmatter{Name: "data-layer-audit", Description: "Audit the data layer."},
			body: goodSynthBody(),
		}},
	}
	cands := []SkillCandidate{newCandidateForGateTests("data-layer-audit")}
	synth := newSynthWithCandidates(t, b, prov, cands)

	// Inject the real gate (not a stub). It will call loadFixturesForSlug with
	// wikiRoot="" because resolveWikiRoot() returns "" for a broker with no
	// wiki worker.
	synth.gate = newDefaultSkillValidationGate()

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 1 {
		t.Fatalf("Synthesized: got %d want 1 (gate must not block when wikiRoot empty; errors: %+v)",
			res.Synthesized, res.Errors)
	}
	if res.RejectedByValidation != 0 {
		t.Fatalf("RejectedByValidation: got %d want 0", res.RejectedByValidation)
	}
	// The metric IS incremented even when the skip reason is "no wikiRoot".
	// This is the expected (if slightly imprecise) telemetry behaviour.
	if atomic.LoadInt64(&b.skillCompileMetrics.ValidationGateNoFixtures) != 1 {
		t.Fatalf("ValidationGateNoFixtures: got %d want 1",
			atomic.LoadInt64(&b.skillCompileMetrics.ValidationGateNoFixtures))
	}
}

// ---------------------------------------------------------------------------
// Bug 12 — wikiRootOverrideGate: synthesizer integration with real fixtures
// ---------------------------------------------------------------------------

// wikiRootOverrideGate wraps the real gate but substitutes a known wikiRoot
// so tests can exercise the full fixture-loading + judging path without
// needing a live wiki worker wired into the broker.
type wikiRootOverrideGate struct {
	real     *defaultSkillValidationGate
	wikiRoot string
}

func (g *wikiRootOverrideGate) Validate(ctx context.Context, slug, candidate, baseline, _ string) (bool, error) {
	return g.real.Validate(ctx, slug, candidate, baseline, g.wikiRoot)
}

func TestE2E_Synthesizer_RealFixtures_GateAccepts(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}

	srv := fakeAnthropicServer(t, verdictBetter)
	defer srv.Close()

	b := newTestBroker(t)
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm:   SkillFrontmatter{Name: "explore-data-layer", Description: "Explore the data layer."},
			body: goodSynthBody(),
		}},
	}
	cands := []SkillCandidate{newCandidateForGateTests("explore-data-layer")}
	synth := newSynthWithCandidates(t, b, prov, cands)

	realGate := gateWithFakeServer(t, srv)
	synth.gate = &wikiRootOverrideGate{real: realGate, wikiRoot: wikiRoot}

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 1 {
		t.Fatalf("Synthesized: got %d want 1; errors: %+v", res.Synthesized, res.Errors)
	}
	if res.RejectedByValidation != 0 {
		t.Fatalf("RejectedByValidation: got %d want 0", res.RejectedByValidation)
	}
}

func TestE2E_Synthesizer_RealFixtures_GateRejects(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}

	srv := fakeAnthropicServer(t, verdictWorse)
	defer srv.Close()

	b := newTestBroker(t)
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm:   SkillFrontmatter{Name: "explore-data-layer", Description: "Explore the data layer."},
			body: goodSynthBody(),
		}},
	}
	cands := []SkillCandidate{newCandidateForGateTests("explore-data-layer")}
	synth := newSynthWithCandidates(t, b, prov, cands)

	realGate := gateWithFakeServer(t, srv)
	synth.gate = &wikiRootOverrideGate{real: realGate, wikiRoot: wikiRoot}

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 0 {
		t.Fatalf("Synthesized: got %d want 0 (gate must reject)", res.Synthesized)
	}
	if res.RejectedByValidation != 1 {
		t.Fatalf("RejectedByValidation: got %d want 1", res.RejectedByValidation)
	}
	if atomic.LoadInt64(&b.skillCompileMetrics.ValidationGateRejections) != 1 {
		t.Fatalf("ValidationGateRejections metric: got %d want 1",
			atomic.LoadInt64(&b.skillCompileMetrics.ValidationGateRejections))
	}
	// Rejection error should be captured in the errors slice.
	if len(res.Errors) == 0 {
		t.Fatal("expected at least one SynthError for rejected proposal, got none")
	}
	if !containsStr(res.Errors[0].Reason, "validation_gate") {
		t.Fatalf("error reason should contain 'validation_gate', got: %q", res.Errors[0].Reason)
	}
}

// ---------------------------------------------------------------------------
// Bug 13 — Enhance path: gate uses enhance slug and existing body as baseline
// ---------------------------------------------------------------------------

func TestE2E_Synthesizer_EnhancePath_BaselineBodyPassedToGate(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}

	// Capture what the gate receives.
	// Use a gate that records inputs but always accepts.
	capture := &capturingGate{wikiRoot: wikiRoot}

	b := newTestBroker(t)
	const existingContent = "## Steps\n1. Find schema files.\n2. Read the DDL.\n3. Write a notebook entry.\n"
	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		Name:      "explore-data-layer",
		Title:     "Explore Data Layer",
		Content:   existingContent,
		Status:    "active",
		CreatedBy: "scanner",
	})
	b.mu.Unlock()

	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm:      SkillFrontmatter{Name: "explore-data-layer", Description: "Enhanced."},
			body:    goodSynthBody(),
			enhance: "explore-data-layer",
		}},
	}
	cands := []SkillCandidate{newCandidateForGateTests("new-data-signal")}
	synth := newSynthWithCandidates(t, b, prov, cands)
	synth.gate = capture

	_, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}

	if capture.lastSlug != "explore-data-layer" {
		t.Fatalf("gate called with slug %q, want %q", capture.lastSlug, "explore-data-layer")
	}
	if capture.lastBaseline != existingContent {
		t.Fatalf("gate called with baseline %q, want %q", capture.lastBaseline, existingContent)
	}
}

// capturingGate records the last Validate call's slug and baseline for
// assertion. It always accepts — the test only cares what was passed in.
type capturingGate struct {
	wikiRoot     string
	lastSlug     string
	lastBaseline string
}

func (g *capturingGate) Validate(_ context.Context, slug, _, baseline, _ string) (bool, error) {
	g.lastSlug = slug
	g.lastBaseline = baseline
	return true, nil
}

// ---------------------------------------------------------------------------
// Bug 14 — No-API-key path: gate warns once and treats all as equivalent
// ---------------------------------------------------------------------------

func TestE2E_Gate_NoAPIKey_TreatsAllAsEquivalent(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	// Unset any API key so the gate cannot call the LLM.
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("WUPHF_ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("WUPHF_OPENAI_API_KEY", "")

	gate := newDefaultSkillValidationGate()

	// When no API key is configured, all fixtures return verdictEquivalent.
	// 0 wins, 0 losses → "no improvement" rejection.
	hasFixtures, err := gate.Validate(context.Background(), "explore-data-layer", "candidate body", "", wikiRoot)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true (fixture file exists)")
	}
	if err == nil {
		t.Fatal("expected rejection (all-equivalent → no improvement) when no API key, got nil")
	}
	if !containsStr(err.Error(), "no improvement") {
		t.Fatalf("expected 'no improvement', got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Bug 15 — Unknown skill slug: gate skips gracefully (no fixture file)
// ---------------------------------------------------------------------------

func TestE2E_Gate_UnknownSlug_SkipsGracefully(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	gate := newDefaultSkillValidationGate()

	hasFixtures, err := gate.Validate(context.Background(), "slug-with-no-fixture-file", "candidate", "", wikiRoot)
	if err != nil {
		t.Fatalf("expected nil error for unknown slug, got: %v", err)
	}
	if hasFixtures {
		t.Fatal("expected hasFixtures=false for slug with no fixture file")
	}
}

// ---------------------------------------------------------------------------
// Bug 16 — Request body is valid Anthropic payload shape
// ---------------------------------------------------------------------------

func TestE2E_Gate_AnthropicRequestShape(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}

	type anthropicMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type anthropicPayload struct {
		Model     string         `json:"model"`
		MaxTokens int            `json:"max_tokens"`
		System    string         `json:"system"`
		Messages  []anthropicMsg `json:"messages"`
	}

	var capturedPayload anthropicPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawBody, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(rawBody, &capturedPayload)

		payload := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": `{"verdict":"better","reasoning":"ok"}`},
			},
		}
		b, _ := json.Marshal(payload)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	// Run with one fixture (only one call will be captured).
	dir := t.TempDir()
	writeFixtureFile(t, dir, "test-skill", []string{"Deploy to production safely."})
	_, _ = gate.Validate(context.Background(), "test-skill", "candidate body", "baseline body", dir)

	// Verify required Anthropic fields.
	if capturedPayload.Model == "" {
		t.Fatal("model field missing from Anthropic request")
	}
	if capturedPayload.MaxTokens != skillValidationJudgeMaxTokens {
		t.Fatalf("max_tokens: got %d want %d", capturedPayload.MaxTokens, skillValidationJudgeMaxTokens)
	}
	if capturedPayload.System == "" {
		t.Fatal("system field missing from Anthropic request")
	}
	if len(capturedPayload.Messages) != 1 || capturedPayload.Messages[0].Role != "user" {
		t.Fatalf("unexpected messages shape: %+v", capturedPayload.Messages)
	}
	// User prompt must mention the task fixture and both skill bodies.
	userContent := capturedPayload.Messages[0].Content
	if !containsStr(userContent, "TASK:") {
		t.Fatalf("user prompt missing 'TASK:' section; got:\n%s", userContent)
	}
	if !containsStr(userContent, "BASELINE SKILL:") {
		t.Fatalf("user prompt missing 'BASELINE SKILL:' section")
	}
	if !containsStr(userContent, "CANDIDATE SKILL:") {
		t.Fatalf("user prompt missing 'CANDIDATE SKILL:' section")
	}
	// The fixture content should appear verbatim.
	if !containsStr(userContent, "Deploy to production safely.") {
		t.Fatalf("user prompt missing fixture content")
	}
	// Baseline body should appear verbatim.
	if !containsStr(userContent, "baseline body") {
		t.Fatalf("user prompt missing baseline body content")
	}
}

// ---------------------------------------------------------------------------
// Real current + candidate skill body pairs
//
// These tests close the gap identified in the review: all previous E2E tests
// used baselineBody="" (new-skill path). The tests below pair the actual
// current wiki skill body as baseline against a realistic candidate — the
// scenario that fires on every ENHANCE proposal in production.
// ---------------------------------------------------------------------------

// betterExploreDataLayerCandidate is an enhanced version of explore-data-layer
// that adds two specific improvements over the current body:
//  1. Step 1 now also checks for missing index coverage on frequently queried columns.
//  2. Step 7 notebook requires a "Query cost estimate" section (10× load projection).
//  3. Step 3 flags raw pgx usage outside SQLC as a maintenance risk, not just a signal.
const betterExploreDataLayerCandidate = `## Steps

1. **Locate the schema files.**
   - Search for ` + "`" + `*.sql` + "`" + `, ` + "`" + `schema.go` + "`" + `, ` + "`" + `*.atlas.hcl` + "`" + `, or migration directories (e.g. ` + "`" + `db/migrations/` + "`" + `, ` + "`" + `internal/db/` + "`" + `).
   - Also check for SQLC config (` + "`" + `sqlc.yaml` + "`" + `) to find generated query directories.
   - Note the storage engine (PostgreSQL, SQLite, in-memory) — this affects everything downstream.
   - **Check for missing indexes**: for every column used in a WHERE clause in the query layer, confirm an index exists. Flag any gap as a performance risk.

2. **Read the base schema.**
   - Open the primary DDL file (schema.sql or Atlas HCL).
   - Identify: table names, primary keys, foreign keys, JSONB/EAV columns, and any ` + "`" + `NOT NULL` + "`" + ` surprises.
   - Flag any unexpected design choices (e.g. no SQL DB — in-memory store, event-sourced projections only, flat-file receipts).

3. **Read the query layer.**
   - Open SQLC-generated files or hand-written query files.
   - Note which tables have full CRUD vs. append-only vs. read-only patterns.
   - **Flag raw ` + "`" + `database/sql` + "`" + ` or ` + "`" + `pgx` + "`" + ` usage outside SQLC as a maintenance risk** — it bypasses type safety and query auditing, not just custom logic.

4. **Read the migration history (if any).**
   - Scan migration files chronologically for schema evolution clues (dropped columns, renamed tables, added indexes).
   - Note the migration tool in use (Atlas, goose, flyway, raw SQL).

5. **Read the event/message schemas.**
   - Locate Protobuf or Avro definitions for Kafka/gRPC event types relevant to the data layer.
   - Map each event type to the table or projection it writes to.

6. **Check the projections / read models.**
   - Find any in-memory or derived state (e.g. approvals projector, thread payload assembler).
   - Note what raw events they consume and what query-ready shape they produce.

7. **Write the notebook entry.**
   - Path: ` + "`" + `agents/data-analyst/notebook/{issue-id}-data-layer.md` + "`" + `
   - Frontmatter: ` + "`" + `scratch: true` + "`" + ` (until promoted).
   - Required sections:
     - **Storage engine** (one line)
     - **Schema overview** (table list with one-line purpose each)
     - **Query patterns** (CRUD breakdown, notable hand-rolled queries)
     - **Event schema** (event types → projections map)
     - **Query cost estimate** (for the top 3 queries by frequency, estimate row count and latency at 10× current load)
     - **Surprises / design flags** (anything the team should know that they might not expect)
     - **Open questions** (gaps that need owner clarification)

8. **Post the team summary.**
   - Call ` + "`" + `team_broadcast` + "`" + ` with a tight summary: storage engine, schema shape, one or two key surprises.
   - Keep it under 10 lines — details live in the notebook.

9. **Complete or submit the task.**
   - If the issue needs reviewer sign-off: call ` + "`" + `team_task action=submit_for_review` + "`" + `.
   - If it is self-contained exploration: call ` + "`" + `team_task action=complete` + "`" + `.
   - Drop a one-line ` + "`" + `@ceo` + "`" + ` note in the channel if the exploration revealed something that should change the task graph.
`

// worseExploreDataLayerCandidate strips the current body down to vague steps
// that lose all the specific guidance agents actually need.
const worseExploreDataLayerCandidate = `## Steps

1. Find the database files.
2. Read the schema.
3. Check the queries.
4. Write a notebook entry with what you found.
5. Tell the team.
`

// betterTraceCallGraphCandidate adds two precise improvements to the current body:
//  1. Step 3 adds: flag any function that holds a mutex while making an external call.
//  2. Step 4 adds: flag context.Background() usage instead of the incoming ctx (span loss).
const betterTraceCallGraphCandidate = `**Input:** one entrypoint — a gRPC method name, HTTP route path, or Kafka topic+handler.

### Steps

1. **Locate the entrypoint definition**
   - For gRPC: find the ` + "`" + `.proto` + "`" + ` file, read the method signature and request/response types.
   - For HTTP: find the router registration (` + "`" + `mux.Handle` + "`" + `, ` + "`" + `r.POST` + "`" + `, etc.) and the handler function.
   - For Kafka: find the consumer group registration and the handler function.
   - Note the file path and line number.

2. **Read the handler implementation** (depth 0)
   - Read the full handler body.
   - List every function call made, with package + function name.
   - Note any domain model types created, read, or mutated.
   - Note any cross-cutting calls (auth middleware, logger, tracer, metrics).

3. **Trace one hop at a time** (depth 1 → max 3)
   - For each non-stdlib call at the current depth, read its implementation.
   - Stop descending into: standard library, generated code (protoc/sqlc), vendor/, and pure utility functions (string/time helpers).
   - At each hop record: package, function, domain models touched, storage calls (DB/cache/queue), external RPC calls.
   - **Flag any function that holds a mutex while making an external call** (DB, RPC, HTTP) — this is a deadlock risk if the external call blocks.
   - **Flag any use of ` + "`" + `context.Background()` + "`" + ` instead of the incoming ctx** — it breaks tracing span propagation and cancellation.

4. **Identify cross-cutting concerns**
   - Auth: where is identity asserted or passed? Which middleware or function enforces it?
   - Observability: where are spans started/ended, metrics incremented, structured logs emitted?
   - Error handling: what error types are returned upward? Where are errors wrapped vs. swallowed?

5. **Write a structured notebook entry** at ` + "`" + `agents/arch-analyst/notebook/{ISSUE-ID}-call-graph-{entrypoint}.md` + "`" + `

6. **Post a team summary** via ` + "`" + `team_broadcast` + "`" + `
`

func TestE2E_Gate_CurrentVsEnhancedCandidate_Accepted(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	currentBody := readCurrentSkillBody(t, wikiRoot, "explore-data-layer")
	if currentBody == "" {
		t.Skip("could not read current explore-data-layer skill body")
	}

	srv := fakeAnthropicServer(t, verdictBetter)
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	// baseline = current wiki body, candidate = enhanced version with index check + cost section.
	hasFixtures, err := gate.Validate(
		context.Background(),
		"explore-data-layer",
		betterExploreDataLayerCandidate,
		currentBody,
		wikiRoot,
	)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	if err != nil {
		t.Fatalf("expected enhanced candidate to be accepted, got: %v", err)
	}
}

func TestE2E_Gate_CurrentVsDegradedCandidate_Rejected(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	currentBody := readCurrentSkillBody(t, wikiRoot, "explore-data-layer")
	if currentBody == "" {
		t.Skip("could not read current explore-data-layer skill body")
	}

	srv := fakeAnthropicServer(t, verdictWorse)
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	// baseline = current wiki body, candidate = stripped vague version.
	hasFixtures, err := gate.Validate(
		context.Background(),
		"explore-data-layer",
		worseExploreDataLayerCandidate,
		currentBody,
		wikiRoot,
	)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	if err == nil {
		t.Fatal("expected degraded candidate to be rejected, got nil")
	}
	if !containsStr(err.Error(), "regresses") {
		t.Fatalf("expected 'regresses' in rejection reason, got: %v", err)
	}
}

func TestE2E_Gate_CurrentVsIdenticalCandidate_NoImprovement(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	currentBody := readCurrentSkillBody(t, wikiRoot, "trace-call-graph")
	if currentBody == "" {
		t.Skip("could not read current trace-call-graph skill body")
	}

	// When candidate == baseline the judge returns "equivalent" — the gate
	// must reject with "no improvement". This is the exact check that prevents
	// a no-op enhance from being accepted.
	srv := fakeAnthropicServer(t, verdictEquivalent)
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	hasFixtures, err := gate.Validate(
		context.Background(),
		"trace-call-graph",
		currentBody, // candidate is identical to baseline
		currentBody,
		wikiRoot,
	)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	if err == nil {
		t.Fatal("expected identical candidate to be rejected as 'no improvement', got nil")
	}
	if !containsStr(err.Error(), "no improvement") {
		t.Fatalf("expected 'no improvement', got: %v", err)
	}
}

func TestE2E_Gate_TraceCallGraph_EnhancedCandidateAccepted(t *testing.T) {
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	currentBody := readCurrentSkillBody(t, wikiRoot, "trace-call-graph")
	if currentBody == "" {
		t.Skip("could not read current trace-call-graph skill body")
	}

	// 4 fixtures: better, better, equivalent, equivalent, equivalent
	// → 2 wins, 0 losses → accepted.
	verdicts := []fixtureVerdict{
		verdictBetter, verdictBetter,
		verdictEquivalent, verdictEquivalent, verdictEquivalent,
	}
	srv, _ := fakeAnthropicServerSequence(t, verdicts)
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	hasFixtures, err := gate.Validate(
		context.Background(),
		"trace-call-graph",
		betterTraceCallGraphCandidate,
		currentBody,
		wikiRoot,
	)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	if err != nil {
		t.Fatalf("expected enhanced trace-call-graph candidate to be accepted, got: %v", err)
	}
}

func TestE2E_Gate_UserPromptContainsRealSkillBodies(t *testing.T) {
	// Verifies the judge actually receives the real current body and candidate
	// body verbatim — not truncated, not swapped.
	wikiRoot := realWikiFixturesDir()
	if wikiRoot == "" {
		t.Skip("real wiki fixture directory not present; skipping")
	}
	currentBody := readCurrentSkillBody(t, wikiRoot, "explore-data-layer")
	if currentBody == "" {
		t.Skip("could not read current explore-data-layer skill body")
	}

	type anthropicMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type payload struct {
		Messages []anthropicMsg `json:"messages"`
	}

	var captured payload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		resp := map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": `{"verdict":"better","reasoning":"ok"}`},
			},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(b)
	}))
	defer srv.Close()
	gate := gateWithFakeServer(t, srv)

	// Only one fixture so we get exactly one capture.
	dir := t.TempDir()
	writeFixtureFile(t, dir, "explore-data-layer", []string{
		"A new microservice has been handed off. Audit its schema before the first migration.",
	})

	_, _ = gate.Validate(context.Background(), "explore-data-layer",
		betterExploreDataLayerCandidate, currentBody, dir)

	if len(captured.Messages) == 0 {
		t.Fatal("no messages captured from judge request")
	}
	userContent := captured.Messages[0].Content

	// The current body must appear as the baseline.
	if !containsStr(userContent, "Locate the schema files") {
		t.Fatal("user prompt does not contain current skill body as baseline")
	}
	// The candidate must appear in the candidate section.
	if !containsStr(userContent, "Query cost estimate") {
		t.Fatal("user prompt does not contain candidate skill body (missing new section)")
	}
	// The baseline and candidate must not be swapped.
	baselineIdx := strings.Index(userContent, "BASELINE SKILL:")
	candidateIdx := strings.Index(userContent, "CANDIDATE SKILL:")
	if baselineIdx == -1 || candidateIdx == -1 {
		t.Fatal("user prompt missing BASELINE SKILL or CANDIDATE SKILL headers")
	}
	if candidateIdx < baselineIdx {
		t.Fatal("CANDIDATE SKILL appears before BASELINE SKILL — sections are swapped")
	}
}
