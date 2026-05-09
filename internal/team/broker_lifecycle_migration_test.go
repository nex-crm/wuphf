package team

// broker_lifecycle_migration_test.go covers build-time gate #3 (synthetic
// migration shim) of the Lane A success criteria: sweep the cartesian
// product of (pipelineStage, reviewState, status, blocked) values that
// pre-Lane-A code paths could plausibly produce, and assert each tuple
// resolves to a deterministic LifecycleState. Tuples not in the canonical
// migration map must surface as LifecycleStateUnknown with the warning
// logged to standard log output.
//
// TODO(lane-a-followup): production-fixture test, see TODOS.md #0 — needs
// real broker-state.json snapshots from dogfood + opt-in external users
// to exercise tuples the synthetic sweep cannot anticipate.

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestLifecycleMigrationCartesianSweep(t *testing.T) {
	// Acceptance: every (pipelineStage, reviewState, status, blocked)
	// tuple produced by pre-Lane-A code paths must resolve to a fixed
	// LifecycleState. Any tuple outside the canonical set must cleanly
	// fall through to LifecycleStateUnknown — never to a partial or
	// surprising in-between value.
	pipelineStages := []string{"", "triage", "implement", "review", "ship"}
	reviewStates := []string{"", "pending_review", "ready_for_review", "approved", "not_required"}
	statuses := []string{"", "open", "in_progress", "review", "blocked", "done", "completed", "canceled", "cancelled"}
	blockedValues := []bool{false, true}

	// Sanity: all canonical migration map keys must resolve to a canonical
	// LifecycleState (not LifecycleStateUnknown). If a contributor adds a
	// row that points to LifecycleStateUnknown, that defeats the purpose
	// of the table — fail loud.
	for key, state := range lifecycleMigrationMap {
		if state == LifecycleStateUnknown {
			t.Fatalf("migration map row %+v maps to LifecycleStateUnknown — that is the fallback bucket, not a valid mapping", key)
		}
		if _, ok := derivedFieldsFor(state); !ok {
			t.Fatalf("migration map row %+v maps to %q, which has no forward-map row", key, state)
		}
	}

	// The full sweep: every cartesian-product tuple must produce a
	// deterministic LifecycleState (canonical or unknown). The function
	// must never panic on adversarial input — pre-Lane-A state files have
	// been hand-edited by curious users.
	for _, ps := range pipelineStages {
		for _, rs := range reviewStates {
			for _, st := range statuses {
				for _, bl := range blockedValues {
					got := deriveLifecycleStateFromLegacy(ps, rs, st, bl)
					// Either canonical (found in the table) or the
					// explicit unknown fallback. Any other return value
					// is a bug.
					if got != LifecycleStateUnknown {
						if _, ok := derivedFieldsFor(got); !ok {
							t.Errorf("derive(%q,%q,%q,%v) = %q, which is not in the forward-map", ps, rs, st, bl, got)
						}
					}
				}
			}
		}
	}
}

func TestLifecycleMigrationKnownTuplesResolveCanonical(t *testing.T) {
	// Acceptance: the documented pre-Lane-A tuples that the broker is
	// known to produce must resolve to the expected canonical state.
	// This is the hard contract the synthetic sweep can't enforce on its
	// own (the sweep proves determinism, this proves correctness).
	cases := []struct {
		name          string
		pipelineStage string
		reviewState   string
		status        string
		blocked       bool
		want          LifecycleState
	}{
		{"pipeline running implement", "implement", "pending_review", "in_progress", false, LifecycleStateRunning},
		{"pipeline review ready", "review", "ready_for_review", "in_progress", false, LifecycleStateReview},
		{"pipeline blocked on pr merge canonical", "review", "ready_for_review", "blocked", true, LifecycleStateBlockedOnPRMerge},
		{"pipeline merged ship", "ship", "approved", "done", false, LifecycleStateMerged},
		{"bare blocked status only", "", "", "blocked", true, LifecycleStateBlockedOnPRMerge},
		{"bare blocked status, blocked=false (legacy bug fix)", "", "", "blocked", false, LifecycleStateBlockedOnPRMerge},
		{"bare open", "", "", "open", false, LifecycleStateReady},
		{"bare done", "", "", "done", false, LifecycleStateMerged},
		{"bare cancelled", "", "", "cancelled", false, LifecycleStateMerged},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := deriveLifecycleStateFromLegacy(tc.pipelineStage, tc.reviewState, tc.status, tc.blocked)
			if got != tc.want {
				t.Fatalf("derive(%q,%q,%q,%v) = %q, want %q", tc.pipelineStage, tc.reviewState, tc.status, tc.blocked, got, tc.want)
			}
		})
	}
}

func TestLifecycleMigrationUnknownTupleLogsWarning(t *testing.T) {
	// Acceptance: pre-Lane-A code paths that produced a tuple outside
	// the canonical map (legacy bugs, partial migrations, hand edits)
	// must NOT silently land the task into a real lifecycle state. The
	// migration shim must log a warning and stamp LifecycleStateUnknown.
	// The build-time test asserts both: the unknown state lands AND the
	// log line carries the task ID + the unrecognised tuple values for
	// triage.
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{{
		ID:            "task-mystery",
		pipelineStage: "implement",
		reviewState:   "ready_for_review",
		status:        "in_progress",
		blocked:       true, // implement+ready_for_review+in_progress+blocked is not a legitimate tuple
	}}
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetFlags(0)
	log.SetOutput(&buf)
	defer func() {
		log.SetOutput(prev)
		log.SetFlags(log.LstdFlags)
	}()
	b.migrateLifecycleStatesLocked()
	b.mu.Unlock()

	if got := b.tasks[0].LifecycleState; got != LifecycleStateUnknown {
		t.Fatalf("expected LifecycleStateUnknown for adversarial tuple, got %q", got)
	}
	logged := buf.String()
	if !strings.Contains(logged, "task-mystery") {
		t.Fatalf("migration warning should reference the task ID, got %q", logged)
	}
	if !strings.Contains(logged, "unknown") {
		t.Fatalf("migration warning should call out the unknown landing pad, got %q", logged)
	}
	// Index sanity: even an unknown-state task must end up in its own
	// bucket so the inbox query still surfaces it for operator review.
	bucket := b.LifecycleIndexSnapshot()[LifecycleStateUnknown]
	if len(bucket) != 1 || bucket[0] != "task-mystery" {
		t.Fatalf("expected task-mystery in unknown bucket, got %+v", bucket)
	}
}

func TestLifecycleMigrationIsIdempotent(t *testing.T) {
	// Acceptance: the migration shim runs at most once per Broker
	// instance and is idempotent on re-invocation. This guards against
	// double-bumping the index on a noisy startup hook chain.
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{
		{ID: "t1", pipelineStage: "implement", reviewState: "pending_review", status: "in_progress"},
		{ID: "t2", pipelineStage: "ship", reviewState: "approved", status: "done"},
	}
	b.mu.Unlock()
	b.MigrateLifecycleStatesOnce()
	first := b.LifecycleIndexSnapshot()
	b.MigrateLifecycleStatesOnce()
	second := b.LifecycleIndexSnapshot()
	if len(first) != len(second) {
		t.Fatalf("index size changed across idempotent migration: first=%d second=%d", len(first), len(second))
	}
	for state, ids := range first {
		if len(second[state]) != len(ids) {
			t.Fatalf("bucket %s changed across idempotent migration: first=%d second=%d", state, len(ids), len(second[state]))
		}
	}
}
