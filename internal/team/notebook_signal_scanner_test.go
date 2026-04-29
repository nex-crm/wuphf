package team

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newNotebookScannerHarness wires a broker + temp wiki repo so the
// notebook signal scanner can resolve its on-disk root via b.WikiWorker.
// It returns the broker, the wiki root, and a teardown closure.
func newNotebookScannerHarness(t *testing.T) (*Broker, string, func()) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)

	b := newTestBroker(t)
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	return b, root, func() {
		cancel()
		worker.Stop()
	}
}

// writeNotebookEntry plops a markdown file at
// <root>/team/agents/<slug>/notebook/<name>.md with the given body.
func writeNotebookEntry(t *testing.T, root, slug, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "team", "agents", slug, "notebook")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, name+".md")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestNotebookSignalScanner_EmitsClusterAcrossThreeAgents(t *testing.T) {
	b, root, teardown := newNotebookScannerHarness(t)
	defer teardown()

	// Three notebooks across three agents all converging on the same topic.
	// Vocabulary overlap is intentionally high so the Jaccard threshold (0.6)
	// triggers a single cluster — natural-language entries with too much
	// idiosyncratic vocabulary fall below the cut.
	body := "deploy prod pipeline smoke tests toggle flipping deploy prod pipeline smoke tests toggle flipping"
	writeNotebookEntry(t, root, "alice", "2026-04-22", body)
	writeNotebookEntry(t, root, "bob", "2026-04-23", body)
	writeNotebookEntry(t, root, "carol", "2026-04-24", body)

	scanner := NewNotebookSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d (%+v)", len(cands), cands)
	}
	got := cands[0]
	if got.Source != SourceNotebookCluster {
		t.Errorf("source: got %q want %q", got.Source, SourceNotebookCluster)
	}
	if got.SignalCount != 3 {
		t.Errorf("signal count: got %d want 3", got.SignalCount)
	}
	if distinctAuthors(got.Excerpts) < 2 {
		t.Errorf("distinct authors: got %d want >=2", distinctAuthors(got.Excerpts))
	}
	if !strings.Contains(got.SuggestedName, "deploy") && !strings.Contains(got.SuggestedName, "prod") {
		t.Errorf("suggested name should mention deploy or prod, got %q", got.SuggestedName)
	}
}

func TestNotebookSignalScanner_RejectsSingleAuthorCluster(t *testing.T) {
	b, root, teardown := newNotebookScannerHarness(t)
	defer teardown()

	// Two notebooks, same author — should fail minDistinctAgents.
	writeNotebookEntry(t, root, "alice", "2026-04-22",
		"deploy to prod via the deploy pipeline today. smoke tests passed before flipping toggle.")
	writeNotebookEntry(t, root, "alice", "2026-04-23",
		"deploy to prod again with the deploy pipeline. smoke tests caught a regression.")

	scanner := NewNotebookSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates (single author), got %d (%+v)", len(cands), cands)
	}
}

func TestNotebookSignalScanner_RejectsSingletonCluster(t *testing.T) {
	b, root, teardown := newNotebookScannerHarness(t)
	defer teardown()

	// One lonely notebook — should fail minClusterSize.
	writeNotebookEntry(t, root, "alice", "2026-04-22",
		"deploy to prod via the deploy pipeline today. smoke tests passed.")

	scanner := NewNotebookSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates (singleton), got %d (%+v)", len(cands), cands)
	}
}

func TestNotebookSignalScanner_RejectsDissimilarEntries(t *testing.T) {
	b, root, teardown := newNotebookScannerHarness(t)
	defer teardown()

	// Three notebooks, three authors, but no shared vocabulary — must fail
	// the similarity threshold.
	writeNotebookEntry(t, root, "alice", "draft",
		"Customer feedback today: pricing concerns about premium tier upgrade requests.")
	writeNotebookEntry(t, root, "bob", "draft",
		"Engineering retrospective: caching layer rewrite shipped, latency improved markedly.")
	writeNotebookEntry(t, root, "carol", "draft",
		"Marketing review: launch pitch landed flat with founders, rewrite eyebrow copy.")

	scanner := NewNotebookSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates (dissimilar), got %d (%+v)", len(cands), cands)
	}
}

func TestNotebookSignalScanner_NoBrokerOrWorkerIsHarmless(t *testing.T) {
	// Broker exists but has no wiki worker — scan should return cleanly.
	b := newTestBroker(t)
	scanner := NewNotebookSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates with no wiki worker, got %d", len(cands))
	}
}

func TestTokenizeForCluster_DropsStopwordsAndShortTokens(t *testing.T) {
	got := tokenizeForCluster("The quick BROWN fox a in")
	for _, sw := range []string{"the", "in", "a"} {
		if _, ok := got[sw]; ok {
			t.Errorf("stopword %q leaked into tokens", sw)
		}
	}
	for _, expected := range []string{"quick", "brown", "fox"} {
		if _, ok := got[expected]; !ok {
			t.Errorf("expected token %q present", expected)
		}
	}
}

func TestJaccardSets_KnownPairs(t *testing.T) {
	a := map[string]bool{"x": true, "y": true, "z": true}
	b := map[string]bool{"y": true, "z": true, "w": true}
	got := jaccardSets(a, b)
	if got < 0.49 || got > 0.51 {
		t.Errorf("jaccard: got %v want ~0.5", got)
	}

	if jaccardSets(a, map[string]bool{}) != 0 {
		t.Error("jaccard with empty set should be 0")
	}
	if jaccardSets(a, a) != 1.0 {
		t.Errorf("jaccard with itself should be 1, got %v", jaccardSets(a, a))
	}
}

func TestNotebookAuthorFromPath(t *testing.T) {
	for input, want := range map[string]string{
		"team/agents/alice/notebook/foo.md":          "alice",
		"team/agents/deploy-bot/notebook/sub/bar.md": "deploy-bot",
		"team/customers/acme/profile.md":             "",
		"team/agents/":                               "",
	} {
		if got := notebookAuthorFromPath(input); got != want {
			t.Errorf("notebookAuthorFromPath(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestStageBNotebookCluster_OwnerAgentsUnion locks in the Stage B owner
// inference rule for notebook clusters: the candidate carries the dedup
// union of every contributing agent so the synthesized skill defaults to
// every agent who already wrote on the topic. The list is sorted so test
// order and rendered SKILL.md stay stable across runs.
func TestStageBNotebookCluster_OwnerAgentsUnion(t *testing.T) {
	b, root, teardown := newNotebookScannerHarness(t)
	defer teardown()

	// Three distinct agents converging on the same topic, with one of them
	// (alice) writing two entries. We expect the union — alice, bob, carol —
	// not the per-entry list with alice duplicated.
	body := "deploy prod pipeline smoke tests toggle flipping deploy prod pipeline smoke tests toggle flipping"
	writeNotebookEntry(t, root, "alice", "2026-04-22", body)
	writeNotebookEntry(t, root, "alice", "2026-04-25", body)
	writeNotebookEntry(t, root, "bob", "2026-04-23", body)
	writeNotebookEntry(t, root, "carol", "2026-04-24", body)

	scanner := NewNotebookSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(cands))
	}

	want := []string{"alice", "bob", "carol"}
	got := cands[0].OwnerAgents
	if len(got) != len(want) {
		t.Fatalf("OwnerAgents len: got %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("OwnerAgents[%d]: got %q, want %q (full list %v)", i, got[i], w, got)
		}
	}
}
