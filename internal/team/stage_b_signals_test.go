package team

import (
	"context"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

func TestStageBSignalAggregator_NotebookAndSelfHealUnion(t *testing.T) {
	b, root, teardown := newNotebookScannerHarness(t)
	defer teardown()

	// Notebook side: three agents, one cluster. High-overlap vocab so the
	// Jaccard threshold (0.6) triggers a single cluster.
	body := "deploy prod pipeline smoke tests toggle flipping deploy prod pipeline smoke tests toggle flipping"
	writeNotebookEntry(t, root, "alice", "2026-04-22", body)
	writeNotebookEntry(t, root, "bob", "2026-04-23", body)
	writeNotebookEntry(t, root, "carol", "2026-04-24", body)

	// Self-heal side: one resolved incident.
	now := time.Now().UTC()
	seedSelfHealTask(t, b, teamTask{
		ID:         "task-101",
		Title:      selfHealingTaskTitle("deploy-bot", "task-7"),
		Details:    selfHealingTaskDetails("deploy-bot", "task-7", agent.EscalationCapabilityGap, "missing deploy specialist"),
		Owner:      "deploy-bot",
		Status:     "done",
		PipelineID: "incident",
		CreatedAt:  now.Add(-time.Hour).Format(time.RFC3339),
		UpdatedAt:  now.Format(time.RFC3339),
	})

	agg := NewStageBSignalAggregator(b)
	cands, err := agg.Scan(context.Background(), 0) // 0 -> default 15
	if err != nil {
		t.Fatalf("agg scan: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates (1 notebook + 1 self-heal), got %d", len(cands))
	}

	var notebook, selfHeal int
	for _, c := range cands {
		switch c.Source {
		case SourceNotebookCluster:
			notebook++
		case SourceSelfHealResolved:
			selfHeal++
		}
	}
	if notebook != 1 {
		t.Errorf("notebook candidates: got %d want 1", notebook)
	}
	if selfHeal != 1 {
		t.Errorf("self-heal candidates: got %d want 1", selfHeal)
	}
}

func TestStageBSignalAggregator_RespectsMaxTotal(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		seedSelfHealTask(t, b, teamTask{
			ID:         "task-200-" + string(rune('a'+i)),
			Title:      selfHealingTaskTitle("deploy-bot", "task-7"),
			Details:    selfHealingTaskDetails("deploy-bot", "task-7", agent.EscalationCapabilityGap, "missing relay"),
			Owner:      "deploy-bot",
			Status:     "done",
			PipelineID: "incident",
			CreatedAt:  now.Add(-time.Hour).Format(time.RFC3339),
			UpdatedAt:  now.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
		})
	}

	agg := NewStageBSignalAggregator(b)
	cands, err := agg.Scan(context.Background(), 2)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 2 {
		t.Fatalf("expected cap of 2, got %d", len(cands))
	}
}

func TestStageBSignalAggregator_NilBrokerSurfacesEmpty(t *testing.T) {
	var agg *StageBSignalAggregator
	cands, err := agg.Scan(context.Background(), 5)
	if err != nil {
		t.Fatalf("nil agg should not error: %v", err)
	}
	if cands != nil {
		t.Fatalf("nil agg should return nil candidates, got %v", cands)
	}
}
