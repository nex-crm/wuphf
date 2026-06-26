package team

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestOnboardingCompleteMaterializesWiki verifies the Lane B integration
// hook: picking a blueprint whose wiki_schema declares bootstrap articles
// causes those articles to land under $HOME/.wuphf/wiki/ after
// onboarding completes. The broker state is isolated and HOME is
// redirected to a temp dir so we never touch the real wiki.
func TestOnboardingCompleteMaterializesWiki(t *testing.T) {
	ensureOperationsFallbackFS(t)

	// Redirect runtime home so the onboarding hook writes into the test
	// tempdir instead of ~/.wuphf. Set both HOME (for legacy test-isolation
	// patterns) AND WUPHF_RUNTIME_HOME (post-Phase-0, materializeBlueprintWiki
	// resolves through config.RuntimeHomeDir which checks WUPHF_RUNTIME_HOME first).
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)

	b := newTestBroker(t)
	if err := b.onboardingCompleteFn("Stand up niche CRM", false, "niche-crm", nil, ""); err != nil {
		t.Fatalf("onboardingCompleteFn: %v", err)
	}

	wikiRoot := filepath.Join(tmpHome, ".wuphf", "wiki")

	// The niche-crm blueprint declares multiple bootstrap articles; we
	// only need to spot-check one to confirm the hook fired and the
	// transactional write succeeded.
	onboarding := filepath.Join(wikiRoot, "team", "customers", "onboarding.md")
	info, err := os.Stat(onboarding)
	if err != nil {
		t.Fatalf("expected wiki article at %q to exist after onboarding, got err=%v", onboarding, err)
	}
	if info.Size() == 0 {
		t.Fatalf("wiki article %q is empty — skeleton bytes did not land", onboarding)
	}
	repo := NewRepoAt(wikiRoot, filepath.Join(tmpHome, ".wuphf", "wiki.bak"))
	refs, err := repo.Log(context.Background(), "team/customers/onboarding.md")
	if err != nil {
		t.Fatalf("expected materialized article to have git history: %v", err)
	}
	if len(refs) == 0 {
		t.Fatal("expected materialized article to have a bootstrap commit")
	}
	if refs[0].Author != "wuphf-bootstrap" {
		t.Fatalf("expected bootstrap author, got %q", refs[0].Author)
	}

	// Temp dir cleanup invariant: no .wiki.tmp.* siblings left behind.
	entries, err := os.ReadDir(wikiRoot)
	if err != nil {
		t.Fatalf("read wiki root: %v", err)
	}
	for _, e := range entries {
		if len(e.Name()) > 10 && e.Name()[:10] == ".wiki.tmp." {
			t.Fatalf("materializer left temp dir behind: %s", e.Name())
		}
	}
}

// TestOnboardingCompleteWikiIsIdempotent verifies the re-pick scenario:
// running onboarding twice against the same blueprint does not overwrite
// existing article bytes. A user who re-selects a blueprint keeps their
// earlier agent notes.
func TestOnboardingCompleteWikiIsIdempotent(t *testing.T) {
	ensureOperationsFallbackFS(t)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)

	// First run.
	b := newTestBroker(t)
	if err := b.onboardingCompleteFn("Stand up niche CRM", false, "niche-crm", nil, ""); err != nil {
		t.Fatalf("first onboardingCompleteFn: %v", err)
	}
	wikiRoot := filepath.Join(tmpHome, ".wuphf", "wiki")
	article := filepath.Join(wikiRoot, "team", "customers", "onboarding.md")

	// User edits an article between runs.
	customBytes := []byte("# Onboarding\n\nOur team filled this in.\n")
	if err := os.WriteFile(article, customBytes, 0o644); err != nil {
		t.Fatalf("user-edit simulation: %v", err)
	}

	// Second run — e.g. the user re-picks the blueprint. Pinning to the
	// same broker-state.json as `b` so normalizeLoadedStateLocked observes
	// the persistence from the first run (matches production behavior
	// where a CLI restart reads ~/.wuphf/team/broker-state.json).
	b2 := NewBrokerAt(b.statePath)
	if err := b2.onboardingCompleteFn("Re-pick niche CRM", false, "niche-crm", nil, ""); err != nil {
		t.Fatalf("second onboardingCompleteFn: %v", err)
	}

	got, err := os.ReadFile(article)
	if err != nil {
		t.Fatalf("read article after re-run: %v", err)
	}
	if string(got) != string(customBytes) {
		t.Fatalf("re-pick overwrote user content:\nwant %q\ngot  %q", string(customBytes), string(got))
	}
}

// TestOnboardingCompleteSynthesizedBlueprintSeedsGettingStarted verifies the
// from-scratch path (blueprintID=""): the synthesized blueprint carries no
// WikiSchema so materializeBlueprintWiki no-ops, but onboardingCompleteFn now
// also seeds the team/getting-started/ pages so a wizard-onboarded scratch
// office still lands on a populated Getting Started wiki section rather than
// an empty one. (Previously this path created no wiki at all, which left the
// office wiki blank — the gap this seed closes.)
func TestOnboardingCompleteSynthesizedBlueprintSeedsGettingStarted(t *testing.T) {
	ensureOperationsFallbackFS(t)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("WUPHF_RUNTIME_HOME", tmpHome)

	b := newTestBroker(t)
	if err := b.onboardingCompleteFn("Run a bespoke operation", false, "", nil, ""); err != nil {
		t.Fatalf("onboardingCompleteFn (synthesized): %v", err)
	}

	// The Getting Started pages must materialize even on the synthesized
	// (no-WikiSchema) path so the office wiki is never empty.
	gsIndex := filepath.Join(tmpHome, ".wuphf", "wiki", "team", "getting-started", "index.md")
	if _, err := os.Stat(gsIndex); err != nil {
		t.Fatalf("expected getting-started/index.md to be seeded on the synthesized path, got err=%v", err)
	}
}
