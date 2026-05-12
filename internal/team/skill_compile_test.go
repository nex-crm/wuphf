package team

// skill_compile_test.go covers the orchestration semantics: coalesce,
// cooldown, and trigger-aware metric counting. The scanner's wiki-walking
// behaviour is exercised separately; here we keep the LLM stubbed so the
// test suite stays hermetic.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingProvider is a stub llmProvider whose AskIsSkill blocks on a
// channel until release() is called. It always returns is_skill=false so we
// can isolate orchestration semantics from proposal-write paths.
type blockingProvider struct {
	gate    chan struct{}
	calls   int
	callsMu sync.Mutex
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{gate: make(chan struct{})}
}

func (p *blockingProvider) AskIsSkill(ctx context.Context, _, _, _ string) (bool, SkillFrontmatter, string, string, error) {
	p.callsMu.Lock()
	p.calls++
	p.callsMu.Unlock()
	select {
	case <-p.gate:
	case <-ctx.Done():
	}
	return false, SkillFrontmatter{}, "", "", nil
}

func (p *blockingProvider) release() { close(p.gate) }

// instantProvider classifies every article as not-a-skill without blocking.
// Used when we want compileWikiSkills to return immediately.
type instantProvider struct{}

func (p *instantProvider) AskIsSkill(_ context.Context, _, _, _ string) (bool, SkillFrontmatter, string, string, error) {
	return false, SkillFrontmatter{}, "", "", nil
}

// withScannedTestBroker returns a broker with the skill scanner pre-injected
// to use the given provider. It does NOT initialise a wiki worker, so the
// scanner short-circuits with zero candidates and we exercise the
// orchestration layer cleanly.
func withScannedTestBroker(t *testing.T, p llmProvider) *Broker {
	t.Helper()
	b := newTestBroker(t)
	b.SetSkillScanner(NewSkillScanner(b, p, 100))
	return b
}

func TestCompileWikiSkills_CoalescesConcurrentRequests(t *testing.T) {
	prov := newBlockingProvider()
	b := withScannedTestBroker(t, prov)

	// Start the first compile. It blocks because the wiki worker is nil and
	// the scanner returns immediately — but we still need to test the
	// inflight branch, so we manually flip the flag.
	b.mu.Lock()
	b.skillCompileInflight = true
	b.mu.Unlock()

	_, err := b.compileWikiSkills(context.Background(), "", true, "manual")
	if !errors.Is(err, ErrCompileCoalesced) {
		t.Fatalf("expected ErrCompileCoalesced, got %v", err)
	}

	b.mu.Lock()
	coalesced := b.skillCompileCoalesced
	b.mu.Unlock()
	if !coalesced {
		t.Fatalf("expected skillCompileCoalesced=true after coalesce hit")
	}

	// Cleanup so we don't leak goroutines waiting on the gate.
	prov.release()
	b.mu.Lock()
	b.skillCompileInflight = false
	b.skillCompileCoalesced = false
	b.mu.Unlock()
}

func TestCompileWikiSkills_CooldownBlocksCronTickWithinWindow(t *testing.T) {
	t.Setenv("WUPHF_SKILL_COMPILE_COOLDOWN", "5m")

	b := withScannedTestBroker(t, &instantProvider{})
	// Stamp a recent successful pass (within the 5m cooldown window).
	atomic.StoreInt64(&b.skillCompileMetrics.LastSkillCompilePassAtNano, time.Now().UTC().Add(-1*time.Minute).UnixNano())

	_, err := b.compileWikiSkills(context.Background(), "", true, "cron")
	if !errors.Is(err, ErrCompileCooldown) {
		t.Fatalf("expected ErrCompileCooldown for cron within window, got %v", err)
	}
}

func TestCompileWikiSkills_CooldownDoesNotBlockManualClick(t *testing.T) {
	t.Setenv("WUPHF_SKILL_COMPILE_COOLDOWN", "5m")

	b := withScannedTestBroker(t, &instantProvider{})
	atomic.StoreInt64(&b.skillCompileMetrics.LastSkillCompilePassAtNano, time.Now().UTC().Add(-1*time.Minute).UnixNano())

	res, err := b.compileWikiSkills(context.Background(), "", true, "manual")
	if err != nil {
		t.Fatalf("expected manual trigger to bypass cooldown, got %v", err)
	}
	if res.Trigger != "manual" {
		t.Fatalf("expected trigger=manual on result, got %q", res.Trigger)
	}

	b.mu.Lock()
	manual := b.skillCompileMetrics.ManualClicksTotal
	b.mu.Unlock()
	if manual != 1 {
		t.Fatalf("expected ManualClicksTotal=1, got %d", manual)
	}
}

func TestCompileWikiSkills_CooldownExpiresAllowsCronTick(t *testing.T) {
	t.Setenv("WUPHF_SKILL_COMPILE_COOLDOWN", "1ms")

	b := withScannedTestBroker(t, &instantProvider{})
	atomic.StoreInt64(&b.skillCompileMetrics.LastSkillCompilePassAtNano, time.Now().UTC().Add(-10*time.Millisecond).UnixNano())

	res, err := b.compileWikiSkills(context.Background(), "", true, "cron")
	if err != nil {
		t.Fatalf("expected cron pass after cooldown expired, got %v", err)
	}
	if res.Trigger != "cron" {
		t.Fatalf("expected trigger=cron, got %q", res.Trigger)
	}

	b.mu.Lock()
	cron := b.skillCompileMetrics.CronTicksTotal
	b.mu.Unlock()
	if cron != 1 {
		t.Fatalf("expected CronTicksTotal=1, got %d", cron)
	}
}

func TestCompileWikiSkills_UpdatesLastPassAtOnSuccess(t *testing.T) {
	b := withScannedTestBroker(t, &instantProvider{})

	before := time.Now().UTC()
	if _, err := b.compileWikiSkills(context.Background(), "", true, "manual"); err != nil {
		t.Fatalf("compile: %v", err)
	}

	lastNano := atomic.LoadInt64(&b.skillCompileMetrics.LastSkillCompilePassAtNano)
	last := time.Unix(0, lastNano)
	if last.Before(before) {
		t.Fatalf("LastSkillCompilePassAtNano not updated: got %s, expected >= %s", last, before)
	}
}

func TestSkillCompileEnv_DefaultsAndOverrides(t *testing.T) {
	t.Run("interval default", func(t *testing.T) {
		t.Setenv("WUPHF_SKILL_COMPILE_INTERVAL", "")
		if got := skillCompileIntervalFromEnv(); got != defaultSkillCompileInterval {
			t.Fatalf("default interval: got %s, want %s", got, defaultSkillCompileInterval)
		}
	})
	t.Run("interval disabled", func(t *testing.T) {
		t.Setenv("WUPHF_SKILL_COMPILE_INTERVAL", "0")
		if got := skillCompileIntervalFromEnv(); got != 0 {
			t.Fatalf("disabled interval: got %s, want 0", got)
		}
	})
	t.Run("interval custom", func(t *testing.T) {
		t.Setenv("WUPHF_SKILL_COMPILE_INTERVAL", "1h")
		if got := skillCompileIntervalFromEnv(); got != time.Hour {
			t.Fatalf("custom interval: got %s, want 1h", got)
		}
	})
	t.Run("interval invalid falls back", func(t *testing.T) {
		t.Setenv("WUPHF_SKILL_COMPILE_INTERVAL", "not-a-duration")
		if got := skillCompileIntervalFromEnv(); got != defaultSkillCompileInterval {
			t.Fatalf("invalid interval should fall back: got %s, want %s", got, defaultSkillCompileInterval)
		}
	})
	t.Run("cooldown default", func(t *testing.T) {
		t.Setenv("WUPHF_SKILL_COMPILE_COOLDOWN", "")
		if got := skillCompileCooldownFromEnv(); got != defaultSkillCompileCooldown {
			t.Fatalf("default cooldown: got %s, want %s", got, defaultSkillCompileCooldown)
		}
	})
	t.Run("budget default", func(t *testing.T) {
		t.Setenv("WUPHF_SKILL_COMPILE_LLM_BUDGET", "")
		if got := skillCompileBudgetFromEnv(); got != defaultSkillCompileBudget {
			t.Fatalf("default budget: got %d, want %d", got, defaultSkillCompileBudget)
		}
	})
	t.Run("budget custom", func(t *testing.T) {
		t.Setenv("WUPHF_SKILL_COMPILE_LLM_BUDGET", "12")
		if got := skillCompileBudgetFromEnv(); got != 12 {
			t.Fatalf("custom budget: got %d, want 12", got)
		}
	})
	t.Run("budget invalid falls back", func(t *testing.T) {
		t.Setenv("WUPHF_SKILL_COMPILE_LLM_BUDGET", "abc")
		if got := skillCompileBudgetFromEnv(); got != defaultSkillCompileBudget {
			t.Fatalf("invalid budget should fall back: got %d", got)
		}
	})
}

// fixedSkillProvider is a stub llmProvider that classifies every article as
// a skill with a deterministic frontmatter derived from the path. Used to
// exercise Stage A's proposal write path without an LLM round-trip.
type fixedSkillProvider struct{}

func (p *fixedSkillProvider) AskIsSkill(_ context.Context, articlePath, _, _ string) (bool, SkillFrontmatter, string, string, error) {
	base := strings.TrimSuffix(filepath.Base(articlePath), ".md")
	fm := SkillFrontmatter{
		Name:        base,
		Description: "Auto-classified skill for " + base + ".",
		Version:     "1.0.0",
		License:     "MIT",
	}
	return true, fm, "## Steps\n\n1. Do the thing.\n", "", nil
}

type statusSpoofingSkillProvider struct{}

func (p *statusSpoofingSkillProvider) AskIsSkill(_ context.Context, articlePath, _, _ string) (bool, SkillFrontmatter, string, string, error) {
	base := strings.TrimSuffix(filepath.Base(articlePath), ".md")
	fm := SkillFrontmatter{
		Name:        base,
		Description: "Frontmatter attempts to bypass approval.",
		Version:     "1.0.0",
		License:     "MIT",
		Metadata: SkillMetadata{
			Wuphf: SkillWuphfMeta{
				Status:             "active",
				DisabledFromStatus: "proposed",
			},
		},
	}
	return true, fm, "## Steps\n\n1. Do the thing.\n", "", nil
}

func TestSkillScannerScopePathValidation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}

	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	worker.Start(context.Background())
	defer worker.Stop()
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	articleRel := "team/doc..v2.md"
	if _, _, err := repo.Commit(context.Background(), "ceo", articleRel, "# Doc\n\nbody\n", "create", "seed scoped article"); err != nil {
		t.Fatalf("seed article: %v", err)
	}

	scanner := NewSkillScanner(b, &instantProvider{}, 10)
	res, err := scanner.Scan(context.Background(), articleRel, true, "manual")
	if err != nil {
		t.Fatalf("scope with embedded dots should scan: %v", err)
	}
	if res.Scanned != 1 {
		t.Fatalf("scanned = %d, want 1", res.Scanned)
	}

	for _, scopePath := range []string{
		"../team/doc.md",
		"team/../outside.md",
		"teamfoo/doc.md",
		"/team/../../outside.md",
	} {
		t.Run(scopePath, func(t *testing.T) {
			if _, err := scanner.Scan(context.Background(), scopePath, true, "manual"); err == nil {
				t.Fatalf("expected scope %q to be rejected", scopePath)
			}
		})
	}
}

// TestStageACompilerSetsSourceArticle asserts that Stage A scans thread the
// wiki-relative article path through to the proposed skill's SourceArticle
// field — both on the in-memory record and on the rendered SKILL.md
// frontmatter (metadata.wuphf.source_articles[0]). The provenance chain
// (notebook → wiki → skill) is what drift detection, the UI source link,
// and read-based staleness all key off, so this regression test guards
// against the field being silently dropped on the write path.
func TestStageACompilerSetsSourceArticle(t *testing.T) {
	// Build a real wiki repo and seed an article that the scanner will
	// classify as a skill via the deterministic stub provider.
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}

	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	worker.Start(context.Background())
	defer worker.Stop()
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	// Seed a playbook article. Frontmatter not strictly required for this
	// test (the stub provider returns is_skill=true regardless), but
	// matching the agent-authored shape keeps the test scenario realistic.
	articleRel := "team/playbooks/customer-refund.md"
	body := "---\nname: customer-refund\ndescription: Issue a refund.\n---\n# Customer Refund\n\nbody\n"
	if _, _, err := repo.Commit(context.Background(), "ceo", articleRel, body, "create", "seed playbook"); err != nil {
		t.Fatalf("seed playbook: %v", err)
	}

	scanner := NewSkillScanner(b, &fixedSkillProvider{}, 10)
	res, err := scanner.Scan(context.Background(), "", false, "manual")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Proposed == 0 {
		t.Fatalf("expected at least one proposed skill, got %+v", res)
	}

	// In-memory proposal carries SourceArticle.
	b.mu.Lock()
	var found *teamSkill
	for i := range b.skills {
		if b.skills[i].Name == "customer-refund" {
			found = &b.skills[i]
			break
		}
	}
	b.mu.Unlock()
	if found == nil {
		t.Fatalf("proposed skill customer-refund not found in broker.skills")
	}
	if found.SourceArticle != articleRel {
		t.Fatalf("SourceArticle: got %q, want %q", found.SourceArticle, articleRel)
	}

	// On-disk SKILL.md surfaces it under metadata.wuphf.source_articles.
	skillBytes, err := os.ReadFile(filepath.Join(root, "team/skills/customer-refund.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillBytes), "source_articles:") {
		t.Fatalf("SKILL.md missing source_articles key: %q", string(skillBytes))
	}
	if !strings.Contains(string(skillBytes), articleRel) {
		t.Fatalf("SKILL.md missing source article path %q: %q", articleRel, string(skillBytes))
	}
}

func TestStageACompilerClampsFrontmatterStatusToProposed(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}

	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	worker.Start(context.Background())
	defer worker.Stop()
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	articleRel := "team/playbooks/status-spoof.md"
	body := "---\nname: status-spoof\ndescription: Attempts lifecycle spoofing.\n---\n# Status Spoof\n\nbody\n"
	if _, _, err := repo.Commit(context.Background(), "ceo", articleRel, body, "create", "seed playbook"); err != nil {
		t.Fatalf("seed playbook: %v", err)
	}

	scanner := NewSkillScanner(b, &statusSpoofingSkillProvider{}, 10)
	res, err := scanner.Scan(context.Background(), "", false, "manual")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if res.Proposed == 0 {
		t.Fatalf("expected at least one proposed skill, got %+v", res)
	}

	b.mu.Lock()
	found := b.findSkillByNameLocked("status-spoof")
	b.mu.Unlock()
	if found == nil {
		t.Fatal("proposed skill status-spoof not found in broker.skills")
	}
	if found.Status != "proposed" {
		t.Errorf("Status: got %q, want proposed", found.Status)
	}
	if found.DisabledFromStatus != "" {
		t.Errorf("DisabledFromStatus: got %q, want empty", found.DisabledFromStatus)
	}

	skillBytes, err := os.ReadFile(filepath.Join(root, "team/skills/status-spoof.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillBytes), "status: proposed") {
		t.Fatalf("SKILL.md did not clamp status to proposed: %q", string(skillBytes))
	}
	if strings.Contains(string(skillBytes), "disabled_from_status:") {
		t.Fatalf("SKILL.md should not preserve spoofed disabled_from_status: %q", string(skillBytes))
	}
}

// TestStageBSynthLeavesSourceArticleEmpty asserts that the Stage B synth
// path — which is signal-derived rather than rooted in a specific wiki
// page — does NOT set SourceArticle. Stage B's provenance lives in
// metadata.wuphf.source_signals + the Signals body footer; coupling it to
// a single source article would be incorrect.
func TestStageBSynthLeavesSourceArticleEmpty(t *testing.T) {
	fm := SkillFrontmatter{
		Name:        "synth-skill",
		Description: "A synthesized skill.",
	}
	cand := SkillCandidate{
		Source:        SourceNotebookCluster,
		SuggestedName: "synth-skill",
		SignalCount:   2,
		Excerpts: []SkillCandidateExcerpt{
			{Path: "team/agents/eng/notebook/x.md", Snippet: "x", Author: "eng"},
		},
	}
	spec := stageBCandToSpec(fm, "## Body\n", cand)
	if spec.SourceArticle != "" {
		t.Fatalf("Stage B spec.SourceArticle should be empty, got %q", spec.SourceArticle)
	}
}

func TestParseSkillJSON_TolerantDecoder(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantOK  bool
		wantSk  bool
		wantErr bool
	}{
		{
			name:   "is_skill false",
			raw:    `{"is_skill": false}`,
			wantOK: true, wantSk: false,
		},
		{
			name:   "is_skill true full",
			raw:    `{"is_skill": true, "name": "Daily Digest", "description": "Send daily summary.", "body": "## Steps"}`,
			wantOK: true, wantSk: true,
		},
		{
			name:   "fenced json",
			raw:    "```json\n{\"is_skill\": false}\n```",
			wantOK: true, wantSk: false,
		},
		{
			name:    "is_skill true missing name",
			raw:     `{"is_skill": true, "description": "missing name"}`,
			wantErr: true,
		},
		{
			name:    "malformed",
			raw:     `not json at all`,
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isSk, fm, _, err := parseSkillJSON(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got fm=%+v", fm)
				}
				return
			}
			if !tc.wantOK {
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if isSk != tc.wantSk {
				t.Fatalf("isSkill: got %v, want %v", isSk, tc.wantSk)
			}
			if isSk {
				if fm.Name == "" || fm.Description == "" {
					t.Fatalf("expected name + description on positive result, got %+v", fm)
				}
			}
		})
	}
}
