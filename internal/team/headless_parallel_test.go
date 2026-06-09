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
// instances: a turn earns its own dispatch lane when it is a task turn, keyed by
// worktree path (isolated-worktree tasks) or task id (office/external). Two
// turns therefore share a lane — and serialize — exactly when they write the
// same directory or are the same office task. Chat / channel-triage turns (no
// task) collapse to the agent's default lane. This is true for the LEAD too: a
// lead turn carrying a task id gets its own per-task lane (CEO multitasking),
// while a lead turn with no task id stays on the default triage lane.
func TestLaneForTurnKeysByWorktree(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-a", Title: "a", Owner: "eng", status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/wt/a"},
		teamTask{ID: "task-b", Title: "b", Owner: "eng", status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/wt/b"},
		teamTask{ID: "task-shared", Title: "c", Owner: "eng", status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/wt/a"},
		teamTask{ID: "task-office", Title: "d", Owner: "eng", status: "in_progress", ExecutionMode: "office"},
		teamTask{ID: "task-office2", Title: "d2", Owner: "eng", status: "in_progress", ExecutionMode: "office"},
		teamTask{ID: "task-ext", Title: "x", Owner: "eng", status: "in_progress", ExecutionMode: "live_external"},
		teamTask{ID: "task-nopath", Title: "e", Owner: "eng", status: "in_progress", ExecutionMode: "local_worktree"},
		teamTask{ID: "task-lead-office", Title: "f", Owner: "ceo", status: "in_progress", ExecutionMode: "office"},
		teamTask{ID: "task-lead-wt", Title: "g", Owner: "ceo", status: "in_progress", ExecutionMode: "local_worktree", WorktreePath: "/wt/lead"},
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
		{"lead office task -> own per-task lane", "ceo", headlessCodexTurn{TaskID: "task-lead-office"}, headlessLane{slug: "ceo", key: "task:task-lead-office"}},
		{"lead worktree task -> own worktree lane", "ceo", headlessCodexTurn{TaskID: "task-lead-wt"}, headlessLane{slug: "ceo", key: "/wt/lead"}},
		{"lead triage turn (no task) -> default lane", "ceo", headlessCodexTurn{}, headlessLane{slug: "ceo"}},
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

// TestLeadRunsNonDependentTasksConcurrently is the headline Phase 2 behavior:
// the LEAD (ceo) owning two non-dependent office tasks runs BOTH at once, each
// on its own per-task lane. Before per-task lead lanes, every lead turn
// serialized on one default lane and only one would start in the window.
func TestLeadRunsNonDependentTasksConcurrently(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-a", Title: "a", Owner: "ceo", status: "in_progress", ExecutionMode: "office"},
		teamTask{ID: "task-b", Title: "b", Owner: "ceo", status: "in_progress", ExecutionMode: "office"},
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

	l.enqueueHeadlessCodexTurnRecord("ceo", headlessCodexTurn{Prompt: "work #task-a", Channel: "general", TaskID: "task-a"})
	l.enqueueHeadlessCodexTurnRecord("ceo", headlessCodexTurn{Prompt: "work #task-b", Channel: "general", TaskID: "task-b"})

	got := map[string]bool{}
	deadline := time.After(10 * time.Second)
	for len(got) < 2 {
		select {
		case id := <-started:
			got[id] = true
		case <-deadline:
			t.Fatalf("only %d/2 lead instances started concurrently (serialized?): %v", len(got), got)
		}
	}
	if !got["task-a"] || !got["task-b"] {
		t.Fatalf("expected both lead tasks to run concurrently, got %v", got)
	}
}

// TestLeadSameTaskTurnDedupesWhileActive pins the anti-double-dispatch guard:
// even with per-task lead lanes, a second lead turn for a task already in flight
// on that lane is dropped (not piled up). urgentLeadTurn is false here (the task
// is plain in_progress, not review/blocked), so the same-task drop applies.
func TestLeadSameTaskTurnDedupesWhileActive(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-x", Title: "x", Owner: "ceo", status: "in_progress", ExecutionMode: "office"},
	)
	l := newHeadlessLauncherForTest(t)
	l.broker = b
	lane := taskLane("ceo", "task-x")
	l.headless.active[lane] = &headlessCodexActiveTurn{
		Turn:      headlessCodexTurn{Prompt: "first #task-x", TaskID: "task-x"},
		StartedAt: time.Now(),
	}

	l.enqueueHeadlessCodexTurnRecord("ceo", headlessCodexTurn{
		Prompt:     "second #task-x",
		TaskID:     "task-x",
		EnqueuedAt: time.Now(),
	})

	// Read queue depth under the lock — the pool maps are guarded by mu and a
	// worker goroutine may touch them concurrently (race-clean assertion).
	l.headless.mu.Lock()
	queued := len(l.headless.queues[lane])
	l.headless.mu.Unlock()
	if queued != 0 {
		t.Fatalf("expected duplicate lead turn for same task to be dropped, got %d queued", queued)
	}
}

// TestLeadTaskTurnNotHeldByUnrelatedSpecialist proves the per-task scoping of
// the lead-hold: a lead turn that CARRIES a task id is NOT deferred just because
// an unrelated specialist is busy — non-dependent tasks proceed concurrently.
func TestLeadTaskTurnNotHeldByUnrelatedSpecialist(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-ceo", Title: "ceo work", Owner: "ceo", status: "in_progress", ExecutionMode: "office"},
	)
	l := newHeadlessLauncherForTest(t)
	l.broker = b
	// An unrelated specialist is mid-flight on its own task.
	l.headless.active[taskLane("eng", "task-eng")] = &headlessCodexActiveTurn{
		Turn:      headlessCodexTurn{Prompt: "specialist working #task-eng", TaskID: "task-eng"},
		StartedAt: time.Now(),
	}

	l.enqueueHeadlessCodexTurnRecord("ceo", headlessCodexTurn{
		Prompt:     "advance #task-ceo",
		TaskID:     "task-ceo",
		EnqueuedAt: time.Now(),
	})

	// Snapshot the pool maps under the lock before asserting — a worker goroutine
	// spawned by the enqueue can be mutating workers/queues concurrently, so an
	// unlocked read here is a data race (and a potential map panic under -race).
	l.headless.mu.Lock()
	deferredLead := l.headless.deferredLead
	lane := taskLane("ceo", "task-ceo")
	workerRunning := l.headless.workers[lane]
	queued := len(l.headless.queues[lane])
	l.headless.mu.Unlock()

	if deferredLead != nil {
		t.Fatal("task-carrying lead turn must not be deferred by an unrelated busy specialist")
	}
	if !workerRunning && queued == 0 {
		t.Fatal("expected the lead task turn to be queued/dispatched on its own per-task lane")
	}
}

// TestLeadTriageTurnStillHeldByBusySpecialist is the other half of the scoping:
// a NO-TASK lead turn (channel triage) still respects the hold while a
// specialist is active — that is where the redundant-re-route race lives.
func TestLeadTriageTurnStillHeldByBusySpecialist(t *testing.T) {
	l := newHeadlessLauncherForTest(t) // lead = "ceo" by default
	l.headless.active[headlessLane{slug: "eng"}] = &headlessCodexActiveTurn{
		Turn:      headlessCodexTurn{Prompt: "specialist still working"},
		StartedAt: time.Now(),
	}

	// 2-arg enqueue with a prompt that has no #task- prefix → TaskID stays empty
	// → treated as channel triage, which still honors the hold.
	l.enqueueHeadlessCodexTurn("ceo", "general status, anything I should know?")

	// Read deferredLead under the lock — it is guarded by mu and may be touched
	// by a worker goroutine concurrently (race-clean assertion).
	l.headless.mu.Lock()
	deferredLead := l.headless.deferredLead
	l.headless.mu.Unlock()
	if deferredLead == nil {
		t.Fatal("expected a no-task lead triage turn to be deferred while a specialist is active")
	}
}

// TestHeadlessConcurrencyCapParksAndDrains proves the cost guard: with the
// per-agent cap set to 1, the lead's two task lanes cannot both run at once —
// one starts, the other PARKS (queued, no worker) — and the parked lane DRAINS
// once the first turn finishes and frees a slot.
func TestHeadlessConcurrencyCapParksAndDrains(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-a", Title: "a", Owner: "ceo", status: "in_progress", ExecutionMode: "office"},
		teamTask{ID: "task-b", Title: "b", Owner: "ceo", status: "in_progress", ExecutionMode: "office"},
	)
	l := newHeadlessLauncherForTest(t)
	l.broker = b
	l.headless.maxConcurrentPerAgent = 1 // CEO may run only one task at a time

	started := make(chan string, 8)
	setHeadlessCodexRunTurnForTest(t, func(_ *Launcher, ctx context.Context, _, _ string, _ ...string) error {
		select {
		case started <- headlessTurnTaskID(ctx):
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	})

	l.enqueueHeadlessCodexTurnRecord("ceo", headlessCodexTurn{Prompt: "work #task-a", Channel: "general", TaskID: "task-a"})
	l.enqueueHeadlessCodexTurnRecord("ceo", headlessCodexTurn{Prompt: "work #task-b", Channel: "general", TaskID: "task-b"})

	// Exactly one should start; the other parks under the cap.
	var first string
	select {
	case first = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the first lead task to start")
	}
	select {
	case second := <-started:
		t.Fatalf("cap=1 violated: a second task (%s) started while %s was active", second, first)
	case <-time.After(250 * time.Millisecond):
		// correct: the second lane is parked behind the cap
	}

	// Free the slot by cancelling the active turn; the parked lane must drain.
	firstLane := taskLane("ceo", first)
	l.headless.mu.Lock()
	if active := l.headless.active[firstLane]; active != nil && active.Cancel != nil {
		active.Cancel()
	}
	l.headless.mu.Unlock()

	select {
	case second := <-started:
		if second == first {
			t.Fatalf("expected the parked task to drain, but %s started again", second)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the parked lead task to start after a slot freed")
	}
}

// TestHeadlessGlobalConcurrencyCapParksAndDrains is the GLOBAL-pool sibling of
// the per-agent cap test: with maxConcurrent=1 (and the per-agent cap left
// unset/0), two non-dependent office tasks owned by DIFFERENT agents cannot both
// run — only the GLOBAL cap binds — so one starts, the other PARKS, and the
// parked lane DRAINS once the first turn finishes and frees the single slot.
func TestHeadlessGlobalConcurrencyCapParksAndDrains(t *testing.T) {
	b := brokerWithTasks(t,
		teamTask{ID: "task-eng", Title: "eng work", Owner: "eng", status: "in_progress", ExecutionMode: "office"},
		teamTask{ID: "task-gtm", Title: "gtm work", Owner: "gtm", status: "in_progress", ExecutionMode: "office"},
	)
	// Register the second owner so its task resolves to a real member lane.
	b.mu.Lock()
	b.members = append(b.members, officeMember{Slug: "gtm", Name: "gtm"})
	b.memberIndex = nil
	b.mu.Unlock()

	l := newHeadlessLauncherForTest(t)
	l.broker = b
	l.headless.maxConcurrent = 1 // only one turn may run across the whole pool

	started := make(chan string, 8)
	setHeadlessCodexRunTurnForTest(t, func(_ *Launcher, ctx context.Context, _, _ string, _ ...string) error {
		select {
		case started <- headlessTurnTaskID(ctx):
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	})

	l.enqueueHeadlessCodexTurnRecord("eng", headlessCodexTurn{Prompt: "work #task-eng", Channel: "general", TaskID: "task-eng"})
	l.enqueueHeadlessCodexTurnRecord("gtm", headlessCodexTurn{Prompt: "work #task-gtm", Channel: "general", TaskID: "task-gtm"})

	// Exactly one should start; the other parks under the global cap.
	var first string
	select {
	case first = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("expected the first task to start")
	}
	select {
	case second := <-started:
		t.Fatalf("global cap=1 violated: a second task (%s) started while %s was active", second, first)
	case <-time.After(250 * time.Millisecond):
		// correct: the second lane is parked behind the global cap
	}

	// Free the single slot by cancelling the active turn; the parked lane drains.
	firstOwner := "eng"
	if first == "task-gtm" {
		firstOwner = "gtm"
	}
	firstLane := taskLane(firstOwner, first)
	l.headless.mu.Lock()
	if active := l.headless.active[firstLane]; active != nil && active.Cancel != nil {
		active.Cancel()
	}
	l.headless.mu.Unlock()

	select {
	case second := <-started:
		if second == first {
			t.Fatalf("expected the parked task to drain, but %s started again", second)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected the parked task to start after the global slot freed")
	}
}
