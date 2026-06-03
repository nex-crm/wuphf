package team

import (
	"context"
	"testing"
	"time"
)

// brokerWithTasks builds a test broker with member "eng" plus "ceo" (lead) and
// the given tasks, all owned/registered, for lane-resolution tests.
func brokerWithTasks(t *testing.T, tasks ...teamTask) *Broker {
	t.Helper()
	b := newTestBroker(t)
	b.mu.Lock()
	b.members = append(b.members,
		officeMember{Slug: "eng", Name: "eng"},
		officeMember{Slug: "ceo", Name: "ceo"},
	)
	b.memberIndex = nil
	b.tasks = append(b.tasks, tasks...)
	b.mu.Unlock()
	return b
}

// TestLaneForTurnKeysByWorktree pins the core safety invariant of parallel
// instances: a turn earns its own dispatch lane only when it is a non-lead turn
// for an isolated-worktree task, and the lane key is that worktree path. Two
// turns therefore share a lane — and serialize — exactly when they write the
// same directory. Everything else (office-mode, chat, lead) collapses to the
// agent's default lane.
func TestLaneForTurnKeysByWorktree(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-a", Title: "a", Owner: "eng", status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/wt/a"},
		teamTask{ID: "task-b", Title: "b", Owner: "eng", status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/wt/b"},
		teamTask{ID: "task-shared", Title: "c", Owner: "eng", status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/wt/a"},
		teamTask{ID: "task-office", Title: "d", Owner: "eng", status: "in_progress", ExecutionMode: "office"},
		teamTask{ID: "task-office2", Title: "d2", Owner: "eng", status: "in_progress", ExecutionMode: "office"},
		teamTask{ID: "task-ext", Title: "x", Owner: "eng", status: "in_progress", ExecutionMode: "live_external"},
		teamTask{ID: "task-nopath", Title: "e", Owner: "eng", status: "in_progress", ExecutionMode: "local_worktree"},
		teamTask{ID: "task-lead", Title: "f", Owner: "ceo", status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/wt/lead"},
	)
	l := newHeadlessLauncherForTest(t)
	l.broker = b

	cases := []struct {
		name string
		slug string
		turn headlessCodexTurn
		want headlessLane
	}{
		{"worktree A", "eng", headlessCodexTurn{TaskID: "task-a"}, headlessLane{slug: "eng", key: "/wt/a"}},
		{"worktree B distinct from A", "eng", headlessCodexTurn{TaskID: "task-b"}, headlessLane{slug: "eng", key: "/wt/b"}},
		{"shared worktree collapses onto A's lane", "eng", headlessCodexTurn{TaskID: "task-shared"}, headlessLane{slug: "eng", key: "/wt/a"}},
		{"office task -> own per-task lane", "eng", headlessCodexTurn{TaskID: "task-office"}, headlessLane{slug: "eng", key: "task:task-office"}},
		{"second office task -> distinct per-task lane", "eng", headlessCodexTurn{TaskID: "task-office2"}, headlessLane{slug: "eng", key: "task:task-office2"}},
		{"live_external task -> own per-task lane", "eng", headlessCodexTurn{TaskID: "task-ext"}, headlessLane{slug: "eng", key: "task:task-ext"}},
		{"worktree without a path yet -> default lane", "eng", headlessCodexTurn{TaskID: "task-nopath"}, headlessLane{slug: "eng"}},
		{"chat turn (no task) -> default lane", "eng", headlessCodexTurn{}, headlessLane{slug: "eng"}},
		{"lead never forks -> default lane", "ceo", headlessCodexTurn{TaskID: "task-lead"}, headlessLane{slug: "ceo"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := l.laneForTurn(tc.slug, tc.turn); got != tc.want {
				t.Fatalf("laneForTurn(%q, %+v) = %+v, want %+v", tc.slug, tc.turn, got, tc.want)
			}
		})
	}
}

// TestParallelInstancesRunDistinctWorktreesConcurrently proves the feature: one
// agent owning two isolated-worktree tasks runs BOTH at once. The run-turn
// stub signals when it starts and then parks on ctx; if the scheduler still
// serialized the agent, only one instance would start within the window and the
// test would fail.
func TestParallelInstancesRunDistinctWorktreesConcurrently(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-a", Title: "a", Owner: "eng", status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/wt/a"},
		teamTask{ID: "task-b", Title: "b", Owner: "eng", status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/wt/b"},
	)
	l := newHeadlessLauncherForTest(t)
	l.broker = b

	started := make(chan string, 8)
	setHeadlessCodexRunTurnForTest(t, func(_ *Launcher, ctx context.Context, _, _ string, _ ...string) error {
		select {
		case started <- headlessTurnTaskID(ctx):
		default:
		}
		// Park until the test's cleanup cancels the turn; returning a context
		// error keeps the queue worker on the cancel path (no durability /
		// recovery churn).
		<-ctx.Done()
		return ctx.Err()
	})

	l.enqueueHeadlessCodexTurnRecord("eng", headlessCodexTurn{Prompt: "work #task-a", Channel: "general", TaskID: "task-a"})
	l.enqueueHeadlessCodexTurnRecord("eng", headlessCodexTurn{Prompt: "work #task-b", Channel: "general", TaskID: "task-b"})

	got := map[string]bool{}
	deadline := time.After(10 * time.Second)
	for len(got) < 2 {
		select {
		case id := <-started:
			got[id] = true
		case <-deadline:
			t.Fatalf("only %d/2 instances started concurrently (serialized?): %v", len(got), got)
		}
	}
	if !got["task-a"] || !got["task-b"] {
		t.Fatalf("expected task-a and task-b to run concurrently, got %v", got)
	}
}

// TestParallelInstancesRunNonDependentOfficeTasksConcurrently proves the rule
// "non-dependent tasks run together" extends to office/external work, not just
// worktree tasks: one agent with two non-dependent office tasks runs both at
// once. They share cwd — the same concurrency the system already runs across
// different agents — so each gets its own per-task lane.
func TestParallelInstancesRunNonDependentOfficeTasksConcurrently(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-a", Title: "a", Owner: "eng", status: "in_progress", ExecutionMode: "office"},
		teamTask{ID: "task-b", Title: "b", Owner: "eng", status: "in_progress", ExecutionMode: "office"},
	)
	l := newHeadlessLauncherForTest(t)
	l.broker = b

	started := make(chan string, 8)
	setHeadlessCodexRunTurnForTest(t, func(_ *Launcher, ctx context.Context, _, _ string, _ ...string) error {
		select {
		case started <- headlessTurnTaskID(ctx):
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	})

	l.enqueueHeadlessCodexTurnRecord("eng", headlessCodexTurn{Prompt: "work #task-a", Channel: "general", TaskID: "task-a"})
	l.enqueueHeadlessCodexTurnRecord("eng", headlessCodexTurn{Prompt: "work #task-b", Channel: "general", TaskID: "task-b"})

	got := map[string]bool{}
	deadline := time.After(10 * time.Second)
	for len(got) < 2 {
		select {
		case id := <-started:
			got[id] = true
		case <-deadline:
			t.Fatalf("only %d/2 office instances started concurrently (serialized?): %v", len(got), got)
		}
	}
	if !got["task-a"] || !got["task-b"] {
		t.Fatalf("expected both office tasks to run concurrently, got %v", got)
	}
}
