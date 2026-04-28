package team

// skill_compile_test.go covers the orchestration semantics: coalesce,
// cooldown, and trigger-aware metric counting. The scanner's wiki-walking
// behaviour is exercised separately; here we keep the LLM stubbed so the
// test suite stays hermetic.

import (
	"context"
	"errors"
	"sync"
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

func (p *blockingProvider) AskIsSkill(ctx context.Context, articlePath, articleContent string) (bool, SkillFrontmatter, string, error) {
	p.callsMu.Lock()
	p.calls++
	p.callsMu.Unlock()
	select {
	case <-p.gate:
	case <-ctx.Done():
	}
	return false, SkillFrontmatter{}, "", nil
}

func (p *blockingProvider) release() { close(p.gate) }

// instantProvider classifies every article as not-a-skill without blocking.
// Used when we want compileWikiSkills to return immediately.
type instantProvider struct{}

func (p *instantProvider) AskIsSkill(_ context.Context, _, _ string) (bool, SkillFrontmatter, string, error) {
	return false, SkillFrontmatter{}, "", nil
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
	b.mu.Lock()
	b.skillCompileMetrics.LastSkillCompilePassAt = time.Now().UTC().Add(-1 * time.Minute)
	b.mu.Unlock()

	_, err := b.compileWikiSkills(context.Background(), "", true, "cron")
	if !errors.Is(err, ErrCompileCooldown) {
		t.Fatalf("expected ErrCompileCooldown for cron within window, got %v", err)
	}
}

func TestCompileWikiSkills_CooldownDoesNotBlockManualClick(t *testing.T) {
	t.Setenv("WUPHF_SKILL_COMPILE_COOLDOWN", "5m")

	b := withScannedTestBroker(t, &instantProvider{})
	b.mu.Lock()
	b.skillCompileMetrics.LastSkillCompilePassAt = time.Now().UTC().Add(-1 * time.Minute)
	b.mu.Unlock()

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
	b.mu.Lock()
	b.skillCompileMetrics.LastSkillCompilePassAt = time.Now().UTC().Add(-10 * time.Millisecond)
	b.mu.Unlock()

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

	b.mu.Lock()
	last := b.skillCompileMetrics.LastSkillCompilePassAt
	b.mu.Unlock()
	if last.Before(before) {
		t.Fatalf("LastSkillCompilePassAt not updated: got %s, expected >= %s", last, before)
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
