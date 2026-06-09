package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// lifecycle_stage_oracle_test.go pins the lifecycle_state -> board stage mapping
// to a single shared source of truth: web/src/lib/types/lifecycleStageMap.json.
// The Go board grouping (lifecycleStageFor) and the web board grouping
// (stageForState in lifecycle.ts) are independent copies of the same 13->7
// table; the JSON + this test + the TS lifecycle.stagemap.test.ts make any
// drift between the two languages a red test on at least one side.

// wireLifecycleStates is every LifecycleState that crosses the wire to the web
// board. It intentionally excludes the Go-only LifecycleStateUnknown migration
// fallback (which has no web counterpart and maps to backlog by default). Add a
// new wire state here when one is introduced — the oracle asserts the shared
// JSON covers exactly this set.
var wireLifecycleStates = []LifecycleState{
	LifecycleStateDrafting,
	LifecycleStateIntake,
	LifecycleStateReady,
	LifecycleStatePlanning,
	LifecycleStateRunning,
	LifecycleStateReview,
	LifecycleStateChangesRequested,
	LifecycleStateBlockedOnPRMerge,
	LifecycleStateQueuedBehindOwner,
	LifecycleStateDecision,
	LifecycleStateApproved,
	LifecycleStateRejected,
	LifecycleStateArchived,
}

func TestLifecycleStageMapOracle(t *testing.T) {
	// go test runs with the package dir (internal/team) as the working
	// directory, so the repo root is two levels up.
	path := filepath.Join("..", "..", "web", "src", "lib", "types", "lifecycleStageMap.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read shared stage map %s: %v", path, err)
	}
	var shared map[string]string
	if err := json.Unmarshal(raw, &shared); err != nil {
		t.Fatalf("parse shared stage map %s: %v", path, err)
	}

	// 1. Every entry in the shared map agrees with lifecycleStageFor.
	for state, stage := range shared {
		if got := lifecycleStageFor(LifecycleState(state)); string(got) != stage {
			t.Errorf("lifecycleStageFor(%q) = %q, shared map says %q", state, got, stage)
		}
	}

	// 2. The shared map covers exactly the wire states — no missing, no extra.
	if len(shared) != len(wireLifecycleStates) {
		t.Errorf("shared map has %d entries, want %d wire states", len(shared), len(wireLifecycleStates))
	}
	for _, state := range wireLifecycleStates {
		if _, ok := shared[string(state)]; !ok {
			t.Errorf("shared stage map is missing wire state %q", state)
		}
	}
}
