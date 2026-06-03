package team

// skill_validation_gate_test.go covers the held-out validation gate for Stage
// B skill synthesis (issue #1004). Tests are split into three layers:
//
//  1. Pure-function unit tests: splitFixtures, loadFixturesForSlug,
//     validationCacheKey, parseJudgeResponse.
//  2. defaultSkillValidationGate integration tests with a fake HTTP server
//     (stubbed Anthropic endpoint via rewriteTransport).
//  3. Synthesizer-level integration tests with stubValidationGate injected
//     directly into SkillSynthesizer.gate.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// Stub gate
// ---------------------------------------------------------------------------

// stubValidationGate is an in-process implementation of skillValidationGate
// for synthesizer-level tests. It drains a queue FIFO; once empty it falls
// back to the rejectAll / noFixtures flags.
type stubValidationGate struct {
	calls      atomic.Int64
	mu         sync.Mutex
	rejectAll  bool
	noFixtures bool
	lastSlug   string
	queue      []stubValidationResult
}

type stubValidationResult struct {
	hasFixtures bool
	err         error
}

func (s *stubValidationGate) Validate(_ context.Context, slug, _, _, _ string) (bool, error) {
	s.calls.Add(1)
	s.mu.Lock()
	s.lastSlug = slug
	if len(s.queue) > 0 {
		r := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		return r.hasFixtures, r.err
	}
	s.mu.Unlock()
	if s.noFixtures {
		return false, nil
	}
	if s.rejectAll {
		return true, errors.New("stub: rejected")
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// Pure-function tests: splitFixtures
// ---------------------------------------------------------------------------

func TestSplitFixtures_EmptyString(t *testing.T) {
	if got := splitFixtures(""); len(got) != 0 {
		t.Fatalf("expected 0 sections, got %d", len(got))
	}
}

func TestSplitFixtures_WhitespaceOnly(t *testing.T) {
	if got := splitFixtures("   \n\t\n   "); len(got) != 0 {
		t.Fatalf("expected 0 sections for whitespace-only, got %d", len(got))
	}
}

func TestSplitFixtures_SingleSection(t *testing.T) {
	raw := "Deploy the service to production."
	got := splitFixtures(raw)
	if len(got) != 1 {
		t.Fatalf("expected 1 section, got %d", len(got))
	}
	if got[0] != "Deploy the service to production." {
		t.Fatalf("unexpected content: %q", got[0])
	}
}

func TestSplitFixtures_MultipleSections(t *testing.T) {
	raw := "Task A.\n---\nTask B.\n---\nTask C."
	got := splitFixtures(raw)
	if len(got) != 3 {
		t.Fatalf("expected 3 sections, got %d: %v", len(got), got)
	}
}

func TestSplitFixtures_DiscardEmptySections(t *testing.T) {
	raw := "Task A.\n---\n\n---\nTask B."
	got := splitFixtures(raw)
	if len(got) != 2 {
		t.Fatalf("expected 2 non-empty sections, got %d: %v", len(got), got)
	}
}

func TestSplitFixtures_TrimsWhitespace(t *testing.T) {
	raw := "  Task A.  \n---\n  Task B.  "
	got := splitFixtures(raw)
	for i, s := range got {
		if len(s) > 0 && (s[0] == ' ' || s[len(s)-1] == ' ') {
			t.Fatalf("section %d not trimmed: %q", i, s)
		}
	}
}

// ---------------------------------------------------------------------------
// Pure-function tests: loadFixturesForSlug
// ---------------------------------------------------------------------------

func TestLoadFixturesForSlug_FileNotExist(t *testing.T) {
	dir := t.TempDir()
	got, err := loadFixturesForSlug(dir, "nonexistent-slug")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil fixtures, got %v", got)
	}
}

func TestLoadFixturesForSlug_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	fixtureDir := filepath.Join(dir, "team", "skills", ".fixtures")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "my-skill.md"), []byte("   \n\t  "), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadFixturesForSlug(dir, "my-skill")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 fixtures for whitespace-only file, got %d", len(got))
	}
}

func TestLoadFixturesForSlug_TwoFixtures(t *testing.T) {
	dir := t.TempDir()
	fixtureDir := filepath.Join(dir, "team", "skills", ".fixtures")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "Deploy a microservice to production.\n---\nRollback after a bad deploy."
	if err := os.WriteFile(filepath.Join(fixtureDir, "deploy-service.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadFixturesForSlug(dir, "deploy-service")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 fixtures, got %d", len(got))
	}
}

func TestLoadFixturesForSlug_EmptyWikiRoot(t *testing.T) {
	got, err := loadFixturesForSlug("", "some-slug")
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil), got (%v, %v)", got, err)
	}
}

func TestLoadFixturesForSlug_EmptySlug(t *testing.T) {
	got, err := loadFixturesForSlug(t.TempDir(), "")
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for empty slug, got (%v, %v)", got, err)
	}
}

func TestLoadFixturesForSlug_SlugWithSeparator(t *testing.T) {
	// Slugs containing a path separator should be silently rejected.
	got, err := loadFixturesForSlug(t.TempDir(), "foo/bar")
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for separator-slug, got (%v, %v)", got, err)
	}
}

// ---------------------------------------------------------------------------
// Pure-function tests: validationCacheKey
// ---------------------------------------------------------------------------

func TestValidationCacheKey_Deterministic(t *testing.T) {
	k1 := validationCacheKey("fixture", "candidate", "baseline")
	k2 := validationCacheKey("fixture", "candidate", "baseline")
	if k1 != k2 {
		t.Fatalf("cache key is not deterministic: %q != %q", k1, k2)
	}
}

func TestValidationCacheKey_DifferentFixture(t *testing.T) {
	k1 := validationCacheKey("fixture-a", "candidate", "baseline")
	k2 := validationCacheKey("fixture-b", "candidate", "baseline")
	if k1 == k2 {
		t.Fatal("different fixtures should produce different keys")
	}
}

func TestValidationCacheKey_DifferentCandidate(t *testing.T) {
	k1 := validationCacheKey("fixture", "candidate-a", "baseline")
	k2 := validationCacheKey("fixture", "candidate-b", "baseline")
	if k1 == k2 {
		t.Fatal("different candidates should produce different keys")
	}
}

func TestValidationCacheKey_DifferentBaseline(t *testing.T) {
	k1 := validationCacheKey("fixture", "candidate", "baseline-a")
	k2 := validationCacheKey("fixture", "candidate", "baseline-b")
	if k1 == k2 {
		t.Fatal("different baselines should produce different keys")
	}
}

// ---------------------------------------------------------------------------
// Pure-function tests: parseJudgeResponse
// ---------------------------------------------------------------------------

func TestParseJudgeResponse_Better(t *testing.T) {
	raw := `{"verdict":"better","reasoning":"candidate is clearly better"}`
	v, err := parseJudgeResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != verdictBetter {
		t.Fatalf("expected %q, got %q", verdictBetter, v)
	}
}

func TestParseJudgeResponse_Equivalent(t *testing.T) {
	raw := `{"verdict":"equivalent","reasoning":"no difference"}`
	v, err := parseJudgeResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != verdictEquivalent {
		t.Fatalf("expected %q, got %q", verdictEquivalent, v)
	}
}

func TestParseJudgeResponse_Worse(t *testing.T) {
	raw := `{"verdict":"worse","reasoning":"baseline was better"}`
	v, err := parseJudgeResponse(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != verdictWorse {
		t.Fatalf("expected %q, got %q", verdictWorse, v)
	}
}

func TestParseJudgeResponse_UnknownVerdict(t *testing.T) {
	raw := `{"verdict":"uncertain","reasoning":"not sure"}`
	v, err := parseJudgeResponse(raw)
	if err == nil {
		t.Fatal("expected error for unknown verdict, got nil")
	}
	if v != verdictEquivalent {
		t.Fatalf("unknown verdict should degrade to equivalent, got %q", v)
	}
}

func TestParseJudgeResponse_Empty(t *testing.T) {
	_, err := parseJudgeResponse("")
	if err == nil {
		t.Fatal("expected error for empty response, got nil")
	}
}

func TestParseJudgeResponse_JSONFenced(t *testing.T) {
	raw := "```json\n{\"verdict\":\"better\",\"reasoning\":\"ok\"}\n```"
	v, err := parseJudgeResponse(raw)
	if err != nil {
		t.Fatalf("expected stripJSONNoise to handle fenced block, got error: %v", err)
	}
	if v != verdictBetter {
		t.Fatalf("expected %q, got %q", verdictBetter, v)
	}
}

// ---------------------------------------------------------------------------
// defaultSkillValidationGate integration tests (fake HTTP server)
// ---------------------------------------------------------------------------

// anthropicJudgeResponse builds a minimal Anthropic /v1/messages response
// whose text content is the given judge JSON string.
func anthropicJudgeResponse(t *testing.T, judgeJSON string) []byte {
	t.Helper()
	resp := map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": judgeJSON},
		},
	}
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal fake response: %v", err)
	}
	return b
}

// writeFixtureFile creates <wikiRoot>/team/skills/.fixtures/<slug>.md with
// content split into sections by "\n---\n".
func writeFixtureFile(t *testing.T, wikiRoot, slug string, sections []string) {
	t.Helper()
	dir := filepath.Join(wikiRoot, "team", "skills", ".fixtures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := ""
	for i, s := range sections {
		if i > 0 {
			content += "\n---\n"
		}
		content += s
	}
	if err := os.WriteFile(filepath.Join(dir, slug+".md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestValidationGate_NoFixtures_PassesThrough(t *testing.T) {
	dir := t.TempDir()
	gate := newDefaultSkillValidationGate()
	hasFixtures, err := gate.Validate(context.Background(), "missing-slug", "candidate", "", dir)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if hasFixtures {
		t.Fatal("expected hasFixtures=false when no fixture file exists")
	}
}

func TestValidationGate_RejectsOnRegression(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	dir := t.TempDir()
	writeFixtureFile(t, dir, "my-skill", []string{
		"Task A: deploy to prod.",
		"Task B: rollback after incident.",
		"Task C: coordinate hotfix under load.",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(anthropicJudgeResponse(t, `{"verdict":"worse","reasoning":"baseline was clearer"}`))
	}))
	defer srv.Close()

	gate := newDefaultSkillValidationGate()
	gate.httpClient = &http.Client{Transport: rewriteTransport{target: srv.URL}}

	hasFixtures, err := gate.Validate(context.Background(), "my-skill", "candidate body", "baseline body", dir)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true (fixture file exists)")
	}
	if err == nil {
		t.Fatal("expected rejection error for all-worse fixtures, got nil")
	}
	if !containsStr(err.Error(), "regresses") {
		t.Fatalf("expected 'regresses' in error, got: %v", err)
	}
}

func TestValidationGate_RejectsOnAllEquivalent(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	dir := t.TempDir()
	writeFixtureFile(t, dir, "my-skill", []string{
		"Task A: deploy to prod.",
		"Task B: rollback after incident.",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(anthropicJudgeResponse(t, `{"verdict":"equivalent","reasoning":"no change"}`))
	}))
	defer srv.Close()

	gate := newDefaultSkillValidationGate()
	gate.httpClient = &http.Client{Transport: rewriteTransport{target: srv.URL}}

	hasFixtures, err := gate.Validate(context.Background(), "my-skill", "candidate body", "baseline body", dir)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	if err == nil {
		t.Fatal("expected rejection error for all-equivalent fixtures, got nil")
	}
	if !containsStr(err.Error(), "no improvement") {
		t.Fatalf("expected 'no improvement' in error, got: %v", err)
	}
}

func TestValidationGate_AcceptsOneWinZeroLosses(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	dir := t.TempDir()
	writeFixtureFile(t, dir, "my-skill", []string{
		"Task A: deploy to prod.",
		"Task B: rollback after incident.",
		"Task C: coordinate hotfix under load.",
	})

	// Return "better" for the first request, "equivalent" for the rest.
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		verdict := "equivalent"
		if callCount == 1 {
			verdict = "better"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(anthropicJudgeResponse(t, fmt.Sprintf(`{"verdict":%q,"reasoning":"ok"}`, verdict)))
	}))
	defer srv.Close()

	gate := newDefaultSkillValidationGate()
	gate.httpClient = &http.Client{Transport: rewriteTransport{target: srv.URL}}

	hasFixtures, err := gate.Validate(context.Background(), "my-skill", "candidate body", "baseline body", dir)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true")
	}
	if err != nil {
		t.Fatalf("expected acceptance (1 win, 0 losses), got error: %v", err)
	}
}

func TestValidationGate_JudgeFailure_DegradesToEquivalent(t *testing.T) {
	// When all judge HTTP calls fail, all fixtures degrade to equivalent.
	// Result: 0 wins, 0 losses → "no improvement" rejection.
	// This is the correct behaviour — if we cannot evaluate, we do not accept.
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	dir := t.TempDir()
	writeFixtureFile(t, dir, "my-skill", []string{
		"Task A: deploy to prod.",
		"Task B: rollback after incident.",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	gate := newDefaultSkillValidationGate()
	gate.httpClient = &http.Client{Transport: rewriteTransport{target: srv.URL}}

	hasFixtures, err := gate.Validate(context.Background(), "my-skill", "candidate body", "baseline body", dir)
	if !hasFixtures {
		t.Fatal("expected hasFixtures=true (fixture file exists)")
	}
	if err == nil {
		t.Fatal("expected rejection (no wins when all judges fail), got nil")
	}
	if !containsStr(err.Error(), "no improvement") {
		t.Fatalf("expected 'no improvement' in error, got: %v", err)
	}
}

func TestValidationGate_CachePreventsDuplicateLLMCalls(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	dir := t.TempDir()
	writeFixtureFile(t, dir, "my-skill", []string{
		"Task A: deploy to prod.",
	})

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(anthropicJudgeResponse(t, `{"verdict":"better","reasoning":"ok"}`))
	}))
	defer srv.Close()

	gate := newDefaultSkillValidationGate()
	gate.httpClient = &http.Client{Transport: rewriteTransport{target: srv.URL}}

	// Call Validate twice with identical inputs.
	for i := range 2 {
		_, err := gate.Validate(context.Background(), "my-skill", "same-candidate", "same-baseline", dir)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i+1, err)
		}
	}
	if callCount != 1 {
		t.Fatalf("expected 1 LLM call (cache hit on second), got %d", callCount)
	}
}

// ---------------------------------------------------------------------------
// Synthesizer-level integration tests with stubValidationGate
// ---------------------------------------------------------------------------

// goodSynthBody returns a skill body that passes ScanSkill at TrustAgentCreated.
func goodSynthBody() string {
	return "## Steps\n" +
		"1. Tag the release in git.\n" +
		"2. Deploy to staging and watch for errors.\n" +
		"3. Promote to production after smoke tests pass.\n"
}

// newCandidateForGateTests creates a minimal SkillCandidate for synthesizer tests.
func newCandidateForGateTests(name string) SkillCandidate {
	return SkillCandidate{
		Source:               SourceNotebookCluster,
		SuggestedName:        name,
		SuggestedDescription: "Coordinate " + name + ".",
		SignalCount:          3,
	}
}

func TestSynthesizeOnce_GateAccepts(t *testing.T) {
	b := newTestBroker(t)
	gate := &stubValidationGate{} // default: returns (true, nil)
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm:   SkillFrontmatter{Name: "deploy-gate", Description: "Deploy something."},
			body: goodSynthBody(),
		}},
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{newCandidateForGateTests("deploy-gate")})
	synth.gate = gate

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 1 {
		t.Fatalf("Synthesized: got %d want 1 (errors: %+v)", res.Synthesized, res.Errors)
	}
	if res.RejectedByValidation != 0 {
		t.Fatalf("RejectedByValidation: got %d want 0", res.RejectedByValidation)
	}
	if gate.calls.Load() != 1 {
		t.Fatalf("gate called %d times, want 1", gate.calls.Load())
	}
}

func TestSynthesizeOnce_GateRejects(t *testing.T) {
	b := newTestBroker(t)
	gate := &stubValidationGate{rejectAll: true}
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm:   SkillFrontmatter{Name: "deploy-gate", Description: "Deploy something."},
			body: goodSynthBody(),
		}},
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{newCandidateForGateTests("deploy-gate")})
	synth.gate = gate

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	if res.Synthesized != 0 {
		t.Fatalf("Synthesized: got %d want 0 (gate should have rejected)", res.Synthesized)
	}
	if res.RejectedByValidation != 1 {
		t.Fatalf("RejectedByValidation: got %d want 1", res.RejectedByValidation)
	}
	if atomic.LoadInt64(&b.skillCompileMetrics.ValidationGateRejections) != 1 {
		t.Fatalf("ValidationGateRejections metric: got %d want 1",
			atomic.LoadInt64(&b.skillCompileMetrics.ValidationGateRejections))
	}
}

func TestSynthesizeOnce_GateNoFixtures(t *testing.T) {
	b := newTestBroker(t)
	gate := &stubValidationGate{noFixtures: true}
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm:   SkillFrontmatter{Name: "deploy-gate", Description: "Deploy something."},
			body: goodSynthBody(),
		}},
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{newCandidateForGateTests("deploy-gate")})
	synth.gate = gate

	res, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	// noFixtures=true means the gate skips but the proposal still proceeds.
	if res.Synthesized != 1 {
		t.Fatalf("Synthesized: got %d want 1 (no-fixture gate should not block; errors: %+v)",
			res.Synthesized, res.Errors)
	}
	if res.RejectedByValidation != 0 {
		t.Fatalf("RejectedByValidation: got %d want 0", res.RejectedByValidation)
	}
	if atomic.LoadInt64(&b.skillCompileMetrics.ValidationGateNoFixtures) != 1 {
		t.Fatalf("ValidationGateNoFixtures metric: got %d want 1",
			atomic.LoadInt64(&b.skillCompileMetrics.ValidationGateNoFixtures))
	}
}

func TestSynthesizeOnce_GateDisabledByEnv(t *testing.T) {
	t.Setenv("WUPHF_SKILL_VALIDATION_GATE_DISABLED", "true")
	b := newTestBroker(t)
	cands := []SkillCandidate{newCandidateForGateTests("some-skill")}
	synth := newSynthWithCandidates(t, b, &stubLLMProvider{}, cands)
	if synth.gate != nil {
		t.Fatal("expected gate==nil when WUPHF_SKILL_VALIDATION_GATE_DISABLED=true")
	}
}

func TestSynthesizeOnce_EnhancePath_GateUsesEnhanceSlug(t *testing.T) {
	b := newTestBroker(t)
	// Pre-seed an existing skill that will be the enhance target.
	b.mu.Lock()
	b.skills = append(b.skills, teamSkill{
		Name:      "existing-skill",
		Title:     "Existing Skill",
		Content:   "## Steps\n1. Do the thing.\n",
		Status:    "active",
		CreatedBy: "scanner",
	})
	b.mu.Unlock()

	gate := &stubValidationGate{} // accepts everything
	prov := &stubLLMProvider{
		queue: []stubLLMResponse{{
			fm:      SkillFrontmatter{Name: "existing-skill", Description: "Enhanced."},
			body:    goodSynthBody(),
			enhance: "existing-skill",
		}},
	}
	synth := newSynthWithCandidates(t, b, prov, []SkillCandidate{newCandidateForGateTests("new-signal")})
	synth.gate = gate

	_, err := synth.SynthesizeOnce(context.Background(), "manual")
	if err != nil {
		t.Fatalf("SynthesizeOnce: %v", err)
	}
	gate.mu.Lock()
	slug := gate.lastSlug
	gate.mu.Unlock()
	if slug != "existing-skill" {
		t.Fatalf("gate called with slug %q, want %q", slug, "existing-skill")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStrInner(s, substr))
}

func containsStrInner(s, substr string) bool {
	for i := range len(s) - len(substr) + 1 {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
