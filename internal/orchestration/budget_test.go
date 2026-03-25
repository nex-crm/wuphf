package orchestration

import "testing"

func TestBudgetTracker_RecordAndSnapshot(t *testing.T) {
	bt := NewBudgetTracker(BudgetLimit{MaxTokens: 1000, MaxCostUsd: 10.0})

	bt.Record("alice", 500, 5.0)
	snap := bt.GetSnapshot("alice")

	if snap.TokensUsed != 500 {
		t.Errorf("expected 500 tokens, got %d", snap.TokensUsed)
	}
	if snap.CostUsd != 5.0 {
		t.Errorf("expected $5.00, got %f", snap.CostUsd)
	}
	if snap.PercentUsed != 0.5 {
		t.Errorf("expected 50%%, got %f", snap.PercentUsed)
	}
	if snap.Warning {
		t.Error("should not be a warning at 50%")
	}
	if snap.Exceeded {
		t.Error("should not be exceeded at 50%")
	}
}

func TestBudgetTracker_WarningThreshold(t *testing.T) {
	bt := NewBudgetTracker(BudgetLimit{MaxTokens: 100})
	bt.Record("alice", 85, 0)
	snap := bt.GetSnapshot("alice")
	if !snap.Warning {
		t.Error("should warn at 85%")
	}
	if snap.Exceeded {
		t.Error("should not be exceeded at 85%")
	}
}

func TestBudgetTracker_Exceeded(t *testing.T) {
	bt := NewBudgetTracker(BudgetLimit{MaxTokens: 100})
	bt.Record("alice", 110, 0)
	if bt.CanProceed("alice") {
		t.Error("should not be able to proceed when exceeded")
	}
	if !bt.IsWarning("alice") {
		t.Error("exceeded also implies warning")
	}
}

func TestBudgetTracker_Reset(t *testing.T) {
	bt := NewBudgetTracker(BudgetLimit{MaxTokens: 100})
	bt.Record("alice", 90, 1.0)
	bt.Reset("alice")
	snap := bt.GetSnapshot("alice")
	if snap.TokensUsed != 0 || snap.CostUsd != 0 {
		t.Error("reset should clear usage")
	}
	if !bt.CanProceed("alice") {
		t.Error("should be able to proceed after reset")
	}
}

func TestBudgetTracker_GetAllSnapshots(t *testing.T) {
	bt := NewBudgetTracker(BudgetLimit{MaxTokens: 1000})
	bt.Record("alice", 100, 1.0)
	bt.Record("bob", 200, 2.0)

	snaps := bt.GetAllSnapshots()
	if len(snaps) != 2 {
		t.Errorf("expected 2 snapshots, got %d", len(snaps))
	}
}

func TestBudgetTracker_GlobalUsage(t *testing.T) {
	bt := NewBudgetTracker(BudgetLimit{MaxTokens: 1000, MaxCostUsd: 10.0})
	bt.Record("alice", 300, 3.0)
	bt.Record("bob", 200, 2.0)

	g := bt.GetGlobalUsage()
	if g.Tokens != 500 {
		t.Errorf("expected 500 global tokens, got %d", g.Tokens)
	}
	if g.Cost != 5.0 {
		t.Errorf("expected $5.00 global cost, got %f", g.Cost)
	}
	if g.PercentTokens != 0.5 {
		t.Errorf("expected 50%% tokens, got %f", g.PercentTokens)
	}
}
