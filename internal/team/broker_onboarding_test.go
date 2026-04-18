package team

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/operations"
)

// locateRepoRoot walks up from the test's cwd looking for the
// templates/operations directory, so LoadBlueprint can find curated
// blueprint YAML on disk. Returns "" if not found — callers fall back to
// setting up an embedded FS.
func locateRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for dir := cwd; dir != "/" && dir != ""; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "templates", "operations")); err == nil {
			return dir
		}
	}
	return ""
}

// ensureOperationsFallbackFS points operations at the repo's
// templates/operations tree if LoadBlueprint("", ...) would otherwise miss
// it (the wuphf root package's init() sets this, but that init does not
// run in team-package tests).
func ensureOperationsFallbackFS(t *testing.T) {
	t.Helper()
	root := locateRepoRoot(t)
	if root == "" {
		t.Skip("templates/operations not reachable from test cwd; skipping")
	}
	sub, err := fs.Sub(os.DirFS(root), ".")
	if err != nil {
		t.Fatalf("sub fs: %v", err)
	}
	operations.SetFallbackFS(sub)
}

// withIsolatedBrokerState gives the test a broker with its own state file
// and a clean broker state on disk, then cleans up when done.
func withIsolatedBrokerState(t *testing.T) func() {
	t.Helper()
	old := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	return func() { brokerStatePath = old }
}

// TestOnboardingCompleteSeedsFromPickedBlueprint verifies that when the
// wizard POSTs a curated blueprint id, the broker seeds the exact member
// list from that blueprint's starter.agents — not ceo/planner/executor/
// reviewer from DefaultManifest.
func TestOnboardingCompleteSeedsFromPickedBlueprint(t *testing.T) {
	ensureOperationsFallbackFS(t)
	defer withIsolatedBrokerState(t)()

	b := NewBroker()
	if err := b.onboardingCompleteFn("Stand up niche CRM", false, "niche-crm", nil); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	want := map[string]bool{
		"operator": true, "planner": true, "builder": true,
		"growth": true, "reviewer": true,
	}
	got := map[string]bool{}
	b.mu.Lock()
	for _, m := range b.members {
		got[m.Slug] = true
	}
	b.mu.Unlock()

	for slug := range want {
		if !got[slug] {
			t.Errorf("expected niche-crm slug %q in roster; got %v", slug, got)
		}
	}
	for slug := range got {
		if slug == "ceo" || slug == "executor" {
			t.Errorf("DefaultManifest slug %q leaked into blueprint roster; got %v", slug, got)
		}
	}

	b.mu.Lock()
	var lead string
	for _, m := range b.members {
		if m.BuiltIn {
			lead = m.Slug
			break
		}
	}
	b.mu.Unlock()
	if lead != "operator" {
		t.Errorf("expected BuiltIn lead to be operator (blueprint's lead_slug), got %q", lead)
	}
}

// TestOnboardingCompleteHonorsAgentFilter verifies the wizard's per-agent
// toggle state: agents=[operator, builder] should seed only those two,
// dropping the blueprint's other specialists.
func TestOnboardingCompleteHonorsAgentFilter(t *testing.T) {
	ensureOperationsFallbackFS(t)
	defer withIsolatedBrokerState(t)()

	b := NewBroker()
	if err := b.onboardingCompleteFn("Stand up niche CRM", false, "niche-crm", []string{"operator", "builder"}); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	b.mu.Lock()
	slugs := make([]string, 0, len(b.members))
	for _, m := range b.members {
		slugs = append(slugs, m.Slug)
	}
	b.mu.Unlock()

	hasOperator := false
	hasBuilder := false
	for _, s := range slugs {
		switch s {
		case "operator":
			hasOperator = true
		case "builder":
			hasBuilder = true
		case "planner", "growth", "reviewer":
			t.Errorf("unselected slug %q leaked into roster; got %v", s, slugs)
		}
	}
	if !hasOperator {
		t.Errorf("expected operator (selected) in roster; got %v", slugs)
	}
	if !hasBuilder {
		t.Errorf("expected builder (selected) in roster; got %v", slugs)
	}
}

// TestOnboardingCompleteAgentsEmptySeedsLeadOnly verifies that an empty
// agents array (user unchecked every toggle) seeds only the blueprint's
// lead and posts a system message explaining the fallback.
func TestOnboardingCompleteAgentsEmptySeedsLeadOnly(t *testing.T) {
	ensureOperationsFallbackFS(t)
	defer withIsolatedBrokerState(t)()

	b := NewBroker()
	if err := b.onboardingCompleteFn("Stand up niche CRM", false, "niche-crm", []string{}); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	b.mu.Lock()
	memberCount := len(b.members)
	var leadSlug string
	if memberCount == 1 {
		leadSlug = b.members[0].Slug
	}
	var foundSystemMsg bool
	for _, msg := range b.messages {
		if msg.Kind == "system" && strings.Contains(msg.Content, "lead only") {
			foundSystemMsg = true
			break
		}
	}
	b.mu.Unlock()

	if memberCount != 1 {
		t.Fatalf("expected lead-only roster (1 member), got %d", memberCount)
	}
	if leadSlug != "operator" {
		t.Errorf("expected lead slug operator, got %q", leadSlug)
	}
	if !foundSystemMsg {
		t.Errorf("expected system message explaining lead-only fallback; messages seen")
	}
}

// TestOnboardingCompleteFromScratchSynthesizes verifies that when blueprint
// id is empty, the broker synthesizes a blueprint from the user's goal and
// seeds the resulting team — NOT the DefaultManifest roster.
func TestOnboardingCompleteFromScratchSynthesizes(t *testing.T) {
	ensureOperationsFallbackFS(t)
	defer withIsolatedBrokerState(t)()

	b := NewBroker()
	if err := b.onboardingCompleteFn("Build an automated customer-support operation", false, "", nil); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	b.mu.Lock()
	slugs := make([]string, 0, len(b.members))
	for _, m := range b.members {
		slugs = append(slugs, m.Slug)
	}
	b.mu.Unlock()

	// The synthesized team must not be the DefaultManifest roster exactly.
	// Sanity: DefaultManifest is ceo/planner/executor/reviewer. A synthesized
	// team should differ in composition.
	if len(slugs) == 4 && slugs[0] == "ceo" && slugs[1] == "planner" && slugs[2] == "executor" && slugs[3] == "reviewer" {
		t.Errorf("from-scratch produced DefaultManifest roster, not a synthesized team; got %v", slugs)
	}
	if len(slugs) == 0 {
		t.Fatalf("from-scratch produced empty roster")
	}
}

// TestOnboardingCompleteSkipTaskSeedsNoKickoff verifies that skip_task=true
// seeds the team but does not post an onboarding_origin message.
func TestOnboardingCompleteSkipTaskSeedsNoKickoff(t *testing.T) {
	ensureOperationsFallbackFS(t)
	defer withIsolatedBrokerState(t)()

	b := NewBroker()
	if err := b.onboardingCompleteFn("", true, "niche-crm", nil); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	b.mu.Lock()
	memberCount := len(b.members)
	var kickoff bool
	for _, msg := range b.messages {
		if msg.Kind == "onboarding_origin" {
			kickoff = true
			break
		}
	}
	b.mu.Unlock()

	if memberCount == 0 {
		t.Fatalf("expected team seeded even with skip_task=true, got empty members")
	}
	if kickoff {
		t.Errorf("expected no onboarding_origin message with skip_task=true, found one")
	}
}

// REGRESSION: TestOnboardingCompleteSkipTaskPersistsTeam verifies that
// skip_task=true actually persists the seeded team to disk. The previous
// rewrite returned nil from postKickoffLocked before saveLocked(), so a
// user who clicked "skip first task" would lose their entire blueprint
// team on the next broker restart.
func TestOnboardingCompleteSkipTaskPersistsTeam(t *testing.T) {
	ensureOperationsFallbackFS(t)
	defer withIsolatedBrokerState(t)()

	b := NewBroker()
	if err := b.onboardingCompleteFn("", true, "niche-crm", nil); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	// Fresh broker instance re-reads state from disk.
	reloaded := NewBroker()
	reloaded.mu.Lock()
	slugs := make([]string, 0, len(reloaded.members))
	for _, m := range reloaded.members {
		slugs = append(slugs, m.Slug)
	}
	reloaded.mu.Unlock()

	want := map[string]bool{"operator": true, "planner": true, "builder": true, "growth": true, "reviewer": true}
	for slug := range want {
		found := false
		for _, got := range slugs {
			if got == slug {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected niche-crm slug %q to persist across restart; got %v", slug, slugs)
		}
	}
	for _, slug := range slugs {
		if slug == "ceo" || slug == "executor" {
			t.Errorf("DefaultManifest slug %q leaked into persisted roster %v", slug, slugs)
		}
	}
}

// TestOnboardingCompleteLoadBlueprintErrorReturnsError verifies that a bad
// blueprint id produces a non-nil error (which HandleComplete surfaces as
// HTTP 500). No partial state should be seeded.
func TestOnboardingCompleteLoadBlueprintErrorReturnsError(t *testing.T) {
	ensureOperationsFallbackFS(t)
	defer withIsolatedBrokerState(t)()

	b := NewBroker()
	err := b.onboardingCompleteFn("go", false, "definitely-not-a-real-blueprint", nil)
	if err == nil {
		t.Fatalf("expected error for unknown blueprint, got nil")
	}
	if !strings.Contains(err.Error(), "definitely-not-a-real-blueprint") && !strings.Contains(err.Error(), "blueprint") {
		t.Errorf("expected error to reference the blueprint id, got %v", err)
	}
}

// REGRESSION: TestOnboardingCompleteDedupesDuplicateTaskMessage verifies
// that calling onboardingCompleteFn twice with the same task only posts a
// single onboarding_origin message — existing crash-recovery behavior at
// broker_onboarding.go:49-53 (pre-rewrite) must survive the unified flow.
func TestOnboardingCompleteDedupesDuplicateTaskMessage(t *testing.T) {
	ensureOperationsFallbackFS(t)
	defer withIsolatedBrokerState(t)()

	b := NewBroker()
	if err := b.onboardingCompleteFn("hello world", false, "niche-crm", nil); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := b.onboardingCompleteFn("hello world", false, "niche-crm", nil); err != nil {
		t.Fatalf("second call: %v", err)
	}

	b.mu.Lock()
	var count int
	for _, msg := range b.messages {
		if msg.Kind == "onboarding_origin" && msg.Content == "hello world" {
			count++
		}
	}
	b.mu.Unlock()

	if count != 1 {
		t.Errorf("expected dedupe to keep exactly one onboarding_origin message, got %d", count)
	}
}

// TestTaskIDsUseBlueprintPrefix verifies that seeded tasks use a
// blueprint-id prefix (e.g. "niche-crm-1") instead of the generic
// "blank-slate-N" prefix, so persisted rows are self-describing.
func TestTaskIDsUseBlueprintPrefix(t *testing.T) {
	ensureOperationsFallbackFS(t)
	defer withIsolatedBrokerState(t)()

	b := NewBroker()
	if err := b.onboardingCompleteFn("Stand up niche CRM", false, "niche-crm", nil); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	b.mu.Lock()
	ids := make([]string, 0, len(b.tasks))
	for _, tk := range b.tasks {
		ids = append(ids, tk.ID)
	}
	b.mu.Unlock()

	if len(ids) == 0 {
		t.Fatalf("expected niche-crm blueprint to seed at least one task, got 0")
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, "niche-crm-") {
			t.Errorf("expected task id to start with blueprint prefix; got %q (all: %v)", id, ids)
			break
		}
	}
}

// TestSeedFromBlueprintNilAgentsKeepsFullRoster verifies the internal /
// synthesis-path contract: nil selectedAgents means no filtering applied.
func TestSeedFromBlueprintNilAgentsKeepsFullRoster(t *testing.T) {
	ensureOperationsFallbackFS(t)
	defer withIsolatedBrokerState(t)()

	b := NewBroker()
	if err := b.onboardingCompleteFn("go", false, "niche-crm", nil); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	// niche-crm blueprint defines 5 starter agents. nil filter must keep all.
	b.mu.Lock()
	seen := make(map[string]bool)
	for _, m := range b.members {
		seen[m.Slug] = true
	}
	b.mu.Unlock()

	for _, slug := range []string{"operator", "planner", "builder", "growth", "reviewer"} {
		if !seen[slug] {
			t.Errorf("nil agents filter should keep all blueprint agents; missing %q (roster: %v)", slug, seen)
		}
	}
}

var _ = fmt.Sprintf
