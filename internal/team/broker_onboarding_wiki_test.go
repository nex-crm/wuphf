package team

import (
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

	// Redirect HOME so the onboarding hook writes into the test tempdir
	// instead of ~/.wuphf. os.UserHomeDir respects $HOME on unix.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	b := newTestBroker(t)
	if err := b.onboardingCompleteFn("Stand up niche CRM", false, "niche-crm", nil); err != nil {
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

	// First run.
	b := newTestBroker(t)
	if err := b.onboardingCompleteFn("Stand up niche CRM", false, "niche-crm", nil); err != nil {
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
	if err := b2.onboardingCompleteFn("Re-pick niche CRM", false, "niche-crm", nil); err != nil {
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

// TestOnboardingCompleteSynthesizedBlueprintSkipsWiki verifies the
// from-scratch path (blueprintID=""): synthesized blueprints do not
// carry a WikiSchema so the materializer silently no-ops. The user gets
// a functional empty wiki rather than a partial one.
func TestOnboardingCompleteSynthesizedBlueprintSkipsWiki(t *testing.T) {
	ensureOperationsFallbackFS(t)

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	b := newTestBroker(t)
	if err := b.onboardingCompleteFn("Run a bespoke operation", false, "", nil); err != nil {
		t.Fatalf("onboardingCompleteFn (synthesized): %v", err)
	}

	// No wiki dir expected — materializer never ran.
	wikiRoot := filepath.Join(tmpHome, ".wuphf", "wiki")
	if _, err := os.Stat(wikiRoot); !os.IsNotExist(err) {
		t.Fatalf("expected no wiki root for synthesized blueprint, got err=%v", err)
	}
}
