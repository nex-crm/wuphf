package team

// broker_onboarding_wiki_index_regen_test.go — regression coverage for
// nex-crm/wuphf#941. After a seed or scan boundary writes wiki articles
// directly to disk (bypassing the WikiWorker), index/all.md must reflect
// the new files. The fix wires (*Broker).regenWikiIndexAfterSeed into the
// seed phase, the website-scan path, and the boot company-seed job. These
// tests cover the on-disk shape, not the higher-level orchestration.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRegenWikiIndexAfterSeed_PicksUpDirectDiskWrites simulates what
// operations.SeedCompanyContext does on the scan path: it atomicWrite()s
// team/about/README.md straight to disk. Before #941 the
// (*WikiWorker).maybeReconcileIndex per-commit IndexRegen never fired for
// that path, so index/all.md stayed at its boot-reconcile snapshot
// ("_No articles yet._"). This test pins the post-fix behavior: calling
// regenWikiIndexAfterSeed once is enough to list the file.
func TestRegenWikiIndexAfterSeed_PicksUpDirectDiskWrites(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	ctx := context.Background()
	if err := repo.Init(ctx); err != nil {
		t.Fatalf("repo init: %v", err)
	}

	b := newTestBroker(t)
	worker := NewWikiWorker(repo, b)
	workerCtx, cancel := context.WithCancel(context.Background())
	worker.Start(workerCtx)
	// defers run LIFO: cancel must fire BEFORE we await Done, otherwise
	// the goroutine never exits and the test hangs.
	t.Cleanup(func() {
		cancel()
		<-worker.Done()
	})
	b.mu.Lock()
	b.wikiWorker = worker
	b.mu.Unlock()

	// Bootstrap the index with no articles yet. This mirrors the
	// post-init reconcile snapshot that boot does.
	if err := repo.IndexRegen(ctx); err != nil {
		t.Fatalf("bootstrap index regen: %v", err)
	}
	indexPath := repo.IndexAllPath()
	pre, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read pre-state index: %v", err)
	}
	if strings.Contains(string(pre), "team/about/README.md") {
		t.Fatalf("pre-state index already lists README: %q", string(pre))
	}

	// Simulate SeedCompanyContext's atomic-write side effect: drop a
	// README.md under team/about/ without touching the worker queue.
	if err := os.MkdirAll(filepath.Join(root, "team", "about"), 0o755); err != nil {
		t.Fatalf("mkdir team/about: %v", err)
	}
	readmePath := filepath.Join(root, "team", "about", "README.md")
	if err := os.WriteFile(readmePath, []byte("# About\n\nseeded\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}

	// Fire the seed-boundary regen the production code now calls.
	b.regenWikiIndexAfterSeed(ctx, "test")

	got, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read post-state index: %v", err)
	}
	body := string(got)
	if !strings.Contains(body, "team/about/README.md") {
		t.Fatalf("expected index/all.md to list team/about/README.md after regen, got:\n%s", body)
	}
	if strings.Contains(body, "_No articles yet._") {
		t.Fatalf("expected index/all.md to drop empty-state marker after regen, got:\n%s", body)
	}
}

// TestRegenWikiIndexAfterSeed_NoWorkerIsNoop covers the non-markdown
// backend branch. Helpers callers rely on must not panic when the broker
// has no wiki worker attached (e.g. SQLite-only deployments). The fix's
// guard is the only thing standing between this state and a nil-deref.
func TestRegenWikiIndexAfterSeed_NoWorkerIsNoop(t *testing.T) {
	b := newTestBroker(t)
	// No worker installed — exercise the early return.
	b.regenWikiIndexAfterSeed(context.Background(), "test no worker")
}
