package team

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// benchNotebookClient is a no-op writer client used by the benchmark — git
// commits would otherwise dominate runtime and the writer's queue would
// saturate, pushing the benchmark onto the drop path instead of the enqueue
// path it is trying to measure.
type benchNotebookClient struct{}

func (benchNotebookClient) NotebookWrite(context.Context, string, string, string, string, string) (string, int, error) {
	return "deadbeef", 0, nil
}

// BenchmarkPostMessage_WithAutoNotebook measures the added latency of the
// auto-notebook writer hook on the PostMessage hot path. Target: <1ms p50
// per the eng review test plan (T1A). Uses a no-op writer client so the
// benchmark stays on the steady-state enqueue path; the consumer drains
// fast enough that QueueSaturated stays at 0 even at large b.N. Asserted
// after the loop so a regression that re-introduces drops fails loudly.
func BenchmarkPostMessage_WithAutoNotebook(b *testing.B) {
	br := NewBrokerAt(filepath.Join(b.TempDir(), "broker-state.json"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	writer := NewAutoNotebookWriter(benchNotebookClient{}, nil)
	writer.Start(ctx)
	defer writer.Stop(2 * time.Second)

	br.mu.Lock()
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
	b.StopTimer()
	if got := writer.Counters().QueueSaturated; got != 0 {
		b.Fatalf("benchmark drifted into saturated-drop path; QueueSaturated=%d", got)
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
