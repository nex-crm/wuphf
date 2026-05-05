package team

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// BenchmarkPostMessage_WithAutoNotebook measures the added latency of the
// auto-notebook writer hook on the PostMessage hot path. Target: <1ms p50
// per the eng review test plan (T1A). The writer runs but its internal
// goroutine processes events asynchronously, so this benchmark exercises the
// enqueue cost only — exactly the surface that mattered to the eng review.
func BenchmarkPostMessage_WithAutoNotebook(b *testing.B) {
	root := filepath.Join(b.TempDir(), "wiki")
	backup := filepath.Join(b.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		b.Fatalf("repo init: %v", err)
	}

	br := NewBrokerAt(filepath.Join(b.TempDir(), "broker-state.json"))
	worker := NewWikiWorker(repo, br)
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	defer func() {
		cancel()
		<-worker.Done()
	}()

	writer := NewAutoNotebookWriter(worker, nil)
	writer.Start(ctx)
	defer writer.Stop(2 * time.Second)

	br.mu.Lock()
	br.wikiWorker = worker
	br.autoNotebookWriter = writer
	br.mu.Unlock()

	if !br.IsAgentMemberSlug("ceo") {
		b.Skip("default manifest missing 'ceo'")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := br.PostMessage("ceo", "general", "benchmark traffic", nil, ""); err != nil {
			b.Fatalf("PostMessage: %v", err)
		}
	}
}

// BenchmarkPostMessage_WithoutAutoNotebook is the baseline — same broker
// without the writer attached. Compare these two to attribute the writer's
// cost on the hot path.
func BenchmarkPostMessage_WithoutAutoNotebook(b *testing.B) {
	br := NewBrokerAt(filepath.Join(b.TempDir(), "broker-state.json"))
	if !br.IsAgentMemberSlug("ceo") {
		b.Skip("default manifest missing 'ceo'")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := br.PostMessage("ceo", "general", "benchmark traffic", nil, ""); err != nil {
			b.Fatalf("PostMessage: %v", err)
		}
	}
}
