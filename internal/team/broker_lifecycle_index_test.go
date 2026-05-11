package team

// broker_lifecycle_index_test.go covers build-time gate #7 (indexed
// lifecycle lookup invariant) of the Lane A success criteria: 1000
// random transitions across many tasks. After the run, every task ID
// must appear in exactly one bucket of b.lifecycleIndex, with no
// duplicates and no orphans. Anything else means the index has drifted
// from the primary task map and the inbox is lying.

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestLifecycleIndexInvariantAfterRandomTransitions(t *testing.T) {
	// Acceptance: 1000 random (task, target lifecycle state) transitions
	// must keep the lifecycleIndex in lock-step with the primary task
	// map. The closing assertion enumerates the index and confirms:
	//   - every task ID appears exactly once across all buckets
	//   - no bucket carries an ID that is not in b.tasks
	//   - the bucket each task lives in matches its current LifecycleState
	const taskCount = 50
	const transitions = 1000

	b := newTestBroker(t)
	b.mu.Lock()
	for i := 0; i < taskCount; i++ {
		b.tasks = append(b.tasks, teamTask{ID: fmt.Sprintf("task-%d", i), LifecycleState: LifecycleStateIntake})
		b.indexLifecycleLocked(b.tasks[i].ID, "", LifecycleStateIntake)
	}
	b.mu.Unlock()

	canonical := CanonicalLifecycleStates()
	rng := rand.New(rand.NewSource(1)) // deterministic for CI; flake-proof
	for i := 0; i < transitions; i++ {
		taskID := fmt.Sprintf("task-%d", rng.Intn(taskCount))
		newState := canonical[rng.Intn(len(canonical))]
		b.mu.Lock()
		_, err := b.transitionLifecycleLocked(taskID, newState, "random sweep")
		b.mu.Unlock()
		if err != nil {
			t.Fatalf("transition %d: task=%s state=%s err=%v", i, taskID, newState, err)
		}
	}

	// Now check the invariant.
	b.mu.Lock()
	defer b.mu.Unlock()

	taskState := make(map[string]LifecycleState, taskCount)
	for i := range b.tasks {
		taskState[b.tasks[i].ID] = b.tasks[i].LifecycleState
	}
	if len(taskState) != taskCount {
		t.Fatalf("task count changed: got %d, want %d", len(taskState), taskCount)
	}

	seen := make(map[string]LifecycleState, taskCount)
	for state, bucket := range b.lifecycleIndex {
		for _, id := range bucket {
			if prev, dup := seen[id]; dup {
				t.Errorf("task %q appears in two buckets: %q and %q", id, prev, state)
				continue
			}
			seen[id] = state
			expected, ok := taskState[id]
			if !ok {
				t.Errorf("orphan: task %q is in lifecycle bucket %q but does not appear in b.tasks", id, state)
				continue
			}
			if expected != state {
				t.Errorf("task %q is in bucket %q but has LifecycleState %q", id, state, expected)
			}
		}
	}
	if len(seen) != taskCount {
		// Find the missing IDs to make the failure debuggable.
		var missing []string
		for id := range taskState {
			if _, ok := seen[id]; !ok {
				missing = append(missing, id)
			}
		}
		t.Fatalf("expected every task ID to appear in exactly one bucket; missing %v (got %d, want %d)", missing, len(seen), taskCount)
	}
}

func TestLifecycleIndexRebuildsBucketWhenLastIDLeaves(t *testing.T) {
	// Acceptance: when the last task in a bucket transitions out, the
	// bucket entry must be deleted (not left as an empty slice). This
	// keeps len(b.lifecycleIndex) == "number of states currently in
	// use", which the inbox query relies on for cardinality checks.
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{{ID: "solo"}}
	b.indexLifecycleLocked("solo", "", LifecycleStateRunning)
	if _, ok := b.lifecycleIndex[LifecycleStateRunning]; !ok {
		b.mu.Unlock()
		t.Fatal("expected running bucket to exist after first transition")
	}
	b.indexLifecycleLocked("solo", LifecycleStateRunning, LifecycleStateApproved)
	_, stillThere := b.lifecycleIndex[LifecycleStateRunning]
	b.mu.Unlock()
	if stillThere {
		t.Fatal("expected running bucket to be deleted after last task leaves")
	}
}
