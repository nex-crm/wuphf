package team

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// fakeTaskOrchestrator is an injected stand-in for the deepagents DispatchClient
// so the broker wiring is testable without a live Python sidecar.
type fakeTaskOrchestrator struct {
	runCalls    int
	resumeCalls int
	coordCalls  int
	lastRun     provider.DispatchRequest
	lastCoord   provider.CoordinateRequest
	result      *provider.StepResult
	plan        *provider.CoordinationPlan
	coordSignal chan string
	err         error
}

func (f *fakeTaskOrchestrator) Run(_ context.Context, req provider.DispatchRequest) (*provider.StepResult, error) {
	f.runCalls++
	f.lastRun = req
	return f.result, f.err
}

func (f *fakeTaskOrchestrator) Resume(_ context.Context, _ provider.ResumeRequest) (*provider.StepResult, error) {
	f.resumeCalls++
	return f.result, f.err
}

func (f *fakeTaskOrchestrator) Coordinate(_ context.Context, req provider.CoordinateRequest) (*provider.CoordinationPlan, error) {
	f.coordCalls++
	f.lastCoord = req
	if f.coordSignal != nil {
		f.coordSignal <- req.GoalID
	}
	return f.plan, f.err
}

func lifecycleOf(t *testing.T, b *Broker, taskID string) LifecycleState {
	t.Helper()
	for _, task := range b.AllTasks() {
		if task.ID == taskID {
			return task.LifecycleState
		}
	}
	t.Fatalf("task %q not found", taskID)
	return ""
}

func runningTaskBroker(t *testing.T, taskID, owner string) (*Launcher, *Broker) {
	t.Helper()
	b := newTestBroker(t)
	b.tasks = append(b.tasks, teamTask{
		ID:             taskID,
		Owner:          owner,
		Title:          "demo",
		LifecycleState: LifecycleStateRunning,
		Orchestrator:   orchestratorLangGraph,
	})
	l := &Launcher{broker: b}
	return l, b
}

func TestTaskUsesOrchestrator(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"langgraph":   true,
		"LangGraph":   true,
		" langgraph ": true,
		"":            false,
		"broker":      false,
		"deepagents":  false, // the binding kind, not the flag value
	}
	for in, want := range cases {
		if got := taskUsesOrchestrator(teamTask{Orchestrator: in}); got != want {
			t.Errorf("taskUsesOrchestrator(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestTeamTaskOrchestratorWireRoundTrip(t *testing.T) {
	t.Parallel()
	in := teamTask{ID: "t1", Orchestrator: orchestratorLangGraph}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"orchestrator":"langgraph"`) {
		t.Fatalf("orchestrator key missing from wire: %s", data)
	}
	var got teamTask
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Orchestrator != orchestratorLangGraph {
		t.Fatalf("round-trip lost orchestrator: %q", got.Orchestrator)
	}
	// A broker-owned task must not emit the key (omitempty), so existing
	// broker-state.json stays byte-identical.
	plain, _ := json.Marshal(teamTask{ID: "t2"})
	if strings.Contains(string(plain), "orchestrator") {
		t.Fatalf("broker-owned task leaked orchestrator key: %s", plain)
	}
}

func TestDispatchViaOrchestrator_AppliesProjection(t *testing.T) {
	t.Parallel()
	l, b := runningTaskBroker(t, "task-1", "eng")
	fake := &fakeTaskOrchestrator{
		result: &provider.StepResult{
			Status:   provider.StepStatusDone,
			ThreadID: "task-1",
			Projection: provider.Projection{
				TaskID: "task-1", LifecycleState: "review",
				PipelineStage: "review", ReviewState: "ready_for_review", Status: "in_progress",
			},
		},
	}
	l.SetTaskOrchestrator(fake)

	task := b.AllTasks()[0]
	l.dispatchTaskViaOrchestrator("eng", task)

	if fake.runCalls != 1 {
		t.Fatalf("Run called %d times, want 1", fake.runCalls)
	}
	// The record is the re-hydrate source: it carries the CURRENT lifecycle.
	if got := fake.lastRun.Record["lifecycle_state"]; got != "running" {
		t.Fatalf("dispatched record lifecycle_state = %v, want running", got)
	}
	// Secrets cross by name only — never a value.
	office := fake.lastRun.MCP["wuphf-office"]
	if len(office.EnvPassthrough) == 0 || office.EnvPassthrough[0] != "WUPHF_BROKER_TOKEN" {
		t.Fatalf("expected WUPHF_BROKER_TOKEN in env_passthrough, got %v", office.EnvPassthrough)
	}
	if got := lifecycleOf(t, b, "task-1"); got != LifecycleStateReview {
		t.Fatalf("task lifecycle = %q, want review (projection applied)", got)
	}
}

func TestApplyProjection_UnknownLeavesUnchanged(t *testing.T) {
	t.Parallel()
	l, b := runningTaskBroker(t, "task-2", "eng")
	l.applyOrchestratorProjection("task-2", &provider.StepResult{
		Status:     provider.StepStatusDone,
		Projection: provider.Projection{TaskID: "task-2", LifecycleState: "unknown"},
	})
	if got := lifecycleOf(t, b, "task-2"); got != LifecycleStateRunning {
		t.Fatalf("unknown projection should not transition; got %q", got)
	}
}

func TestApplyProjection_NonCanonicalLeavesUnchanged(t *testing.T) {
	t.Parallel()
	l, b := runningTaskBroker(t, "task-3", "eng")
	l.applyOrchestratorProjection("task-3", &provider.StepResult{
		Status:     provider.StepStatusDone,
		Projection: provider.Projection{TaskID: "task-3", LifecycleState: "not_a_real_state"},
	})
	if got := lifecycleOf(t, b, "task-3"); got != LifecycleStateRunning {
		t.Fatalf("non-canonical projection should not transition; got %q", got)
	}
}

func TestDispatchViaOrchestrator_RunErrorLeavesUnchanged(t *testing.T) {
	t.Parallel()
	l, b := runningTaskBroker(t, "task-4", "eng")
	l.SetTaskOrchestrator(&fakeTaskOrchestrator{err: context.DeadlineExceeded})

	l.dispatchTaskViaOrchestrator("eng", b.AllTasks()[0])

	if got := lifecycleOf(t, b, "task-4"); got != LifecycleStateRunning {
		t.Fatalf("Run error should not transition; got %q", got)
	}
}

func TestDispatchViaOrchestrator_InterruptedTransitionsToGate(t *testing.T) {
	t.Parallel()
	l, b := runningTaskBroker(t, "task-5", "eng")
	l.SetTaskOrchestrator(&fakeTaskOrchestrator{
		result: &provider.StepResult{
			Status:     provider.StepStatusInterrupted,
			ThreadID:   "task-5",
			Projection: provider.Projection{TaskID: "task-5", LifecycleState: "decision"},
			Interrupt:  map[string]any{"gate_kind": "review", "summary": "approve?"},
		},
	})

	l.dispatchTaskViaOrchestrator("eng", b.AllTasks()[0])

	if got := lifecycleOf(t, b, "task-5"); got != LifecycleStateDecision {
		t.Fatalf("interrupted dispatch should land the gate state; got %q", got)
	}
}

// TestApplyProjection_TerminalTaskNotResurrected pins Finding C: a stale
// dispatch's "running" projection must NOT transition a task that has already
// reached a terminal lifecycle (approved/archived) in the meantime.
func TestApplyProjection_TerminalTaskNotResurrected(t *testing.T) {
	t.Parallel()
	for _, terminal := range []LifecycleState{LifecycleStateApproved, LifecycleStateArchived} {
		terminal := terminal
		t.Run(string(terminal), func(t *testing.T) {
			t.Parallel()
			b := newTestBroker(t)
			b.tasks = append(b.tasks, teamTask{
				ID:             "task-term",
				Owner:          "eng",
				Title:          "demo",
				LifecycleState: terminal,
				Orchestrator:   orchestratorLangGraph,
			})
			l := &Launcher{broker: b}

			// A stale dispatch projects "running" — applying it would revive the
			// closed task. The terminal-resurrection guard must refuse.
			l.applyOrchestratorProjection("task-term", &provider.StepResult{
				Status:     provider.StepStatusDone,
				Projection: provider.Projection{TaskID: "task-term", LifecycleState: "running"},
			})
			if got := lifecycleOf(t, b, "task-term"); got != terminal {
				t.Fatalf("terminal task must not be resurrected; state = %q, want %q", got, terminal)
			}
		})
	}
}

// blockingOrchestrator holds the first Run call open on a channel so a test can
// observe the single-flight window: it signals when the first call enters and
// blocks until released.
type blockingOrchestrator struct {
	mu       sync.Mutex
	runCalls int
	entered  chan struct{}
	release  chan struct{}
}

func (f *blockingOrchestrator) Run(_ context.Context, req provider.DispatchRequest) (*provider.StepResult, error) {
	f.mu.Lock()
	f.runCalls++
	first := f.runCalls == 1
	f.mu.Unlock()
	if first {
		close(f.entered)
		<-f.release
	}
	return &provider.StepResult{
		Status:     provider.StepStatusDone,
		Projection: provider.Projection{TaskID: req.TaskID, LifecycleState: "running"},
	}, nil
}

func (f *blockingOrchestrator) Resume(_ context.Context, _ provider.ResumeRequest) (*provider.StepResult, error) {
	return nil, nil
}

func (f *blockingOrchestrator) Coordinate(_ context.Context, _ provider.CoordinateRequest) (*provider.CoordinationPlan, error) {
	return nil, nil
}

// TestDispatchViaOrchestrator_SingleFlightSkipsConcurrent pins Finding D3: while
// a dispatch for a task is in flight, a second concurrent dispatch for the same
// task is skipped (no duplicate /run), and a fresh dispatch is allowed once the
// first releases the slot.
func TestDispatchViaOrchestrator_SingleFlightSkipsConcurrent(t *testing.T) {
	t.Parallel()
	l, b := runningTaskBroker(t, "task-sf", "eng")
	fake := &blockingOrchestrator{entered: make(chan struct{}), release: make(chan struct{})}
	l.SetTaskOrchestrator(fake)
	task := b.AllTasks()[0]

	done := make(chan struct{})
	go func() {
		l.dispatchTaskViaOrchestrator("eng", task)
		close(done)
	}()

	// Wait until the first dispatch is inside Run, holding the single-flight slot.
	<-fake.entered

	// A second concurrent dispatch for the same task must be skipped.
	l.dispatchTaskViaOrchestrator("eng", task)
	fake.mu.Lock()
	calls := fake.runCalls
	fake.mu.Unlock()
	if calls != 1 {
		t.Fatalf("second concurrent dispatch should be skipped; Run calls = %d, want 1", calls)
	}

	// Release the first dispatch and let it finish (frees the slot).
	close(fake.release)
	<-done

	// With the slot freed, a fresh dispatch runs again (release already closed,
	// so Run returns without blocking).
	l.dispatchTaskViaOrchestrator("eng", task)
	fake.mu.Lock()
	calls = fake.runCalls
	fake.mu.Unlock()
	if calls != 2 {
		t.Fatalf("dispatch after slot release should run; Run calls = %d, want 2", calls)
	}
}

func TestNilOrchestrator_NoOp(t *testing.T) {
	t.Parallel()
	l, b := runningTaskBroker(t, "task-6", "eng")
	l.orchestrator = nil // explicit: dispatcher not wired
	l.dispatchTaskViaOrchestrator("eng", b.AllTasks()[0])
	if got := lifecycleOf(t, b, "task-6"); got != LifecycleStateRunning {
		t.Fatalf("nil dispatcher must be a no-op; got %q", got)
	}
}

// goalBroker builds an orchestrator-owned goal (parent) with two children: c1
// ready, c2 ready-but-depends-on-c1.
func goalBroker(t *testing.T) (*Launcher, *Broker) {
	t.Helper()
	b := newTestBroker(t)
	b.tasks = append(b.tasks,
		teamTask{ID: "GOAL", Owner: "ceo", Title: "ship it", LifecycleState: LifecycleStateRunning, Orchestrator: orchestratorLangGraph},
		teamTask{ID: "c1", ParentIssueID: "GOAL", Owner: "eng", Title: "step one", LifecycleState: LifecycleStateReady, Orchestrator: orchestratorLangGraph},
		teamTask{ID: "c2", ParentIssueID: "GOAL", Owner: "eng", Title: "step two", LifecycleState: LifecycleStateReady, Orchestrator: orchestratorLangGraph, DependsOn: []string{"c1"}},
	)
	return &Launcher{broker: b}, b
}

func TestTaskIsGoal(t *testing.T) {
	t.Parallel()
	l, b := goalBroker(t)
	goal := b.TaskByID("GOAL")
	child := b.TaskByID("c1")
	if !l.taskIsGoal(*goal) {
		t.Fatal("a parent with children must be a goal")
	}
	if l.taskIsGoal(*child) {
		t.Fatal("a child task must never be a goal")
	}
	// A childless top-level task is a leaf, not a goal.
	b.tasks = append(b.tasks, teamTask{ID: "solo", LifecycleState: LifecycleStateRunning})
	if l.taskIsGoal(*b.TaskByID("solo")) {
		t.Fatal("a childless top-level task is a leaf, not a goal")
	}
}

func TestCoordinateGoal_AppliesActionPlan(t *testing.T) {
	t.Parallel()
	l, b := goalBroker(t)
	fake := &fakeTaskOrchestrator{plan: &provider.CoordinationPlan{
		GoalID:  "GOAL",
		Actions: map[string]string{"c1": provider.CoordStart, "c2": provider.CoordBlock},
		Ready:   []string{"c1"},
	}}
	l.SetTaskOrchestrator(fake)

	l.coordinateGoalViaOrchestrator("ceo", *b.TaskByID("GOAL"))

	if fake.coordCalls != 1 {
		t.Fatalf("Coordinate calls = %d, want 1", fake.coordCalls)
	}
	// The children were sent with task_id + lifecycle_state + union deps.
	if got := len(fake.lastCoord.Children); got != 2 {
		t.Fatalf("sent %d children, want 2", got)
	}
	// START activated c1 to running.
	if got := lifecycleOf(t, b, "c1"); got != LifecycleStateRunning {
		t.Fatalf("c1 (START) = %q, want running", got)
	}
	// BLOCK left c2 untouched.
	if got := lifecycleOf(t, b, "c2"); got != LifecycleStateReady {
		t.Fatalf("c2 (BLOCK) = %q, want unchanged ready", got)
	}
}

func TestCoordinateGoal_SendsUnionOfDependsOnAndBlockedOn(t *testing.T) {
	t.Parallel()
	l, b := goalBroker(t)
	// c2 already depends_on c1; also block it on an external id (c1 dup collapses).
	for i := range b.tasks {
		if b.tasks[i].ID == "c2" {
			b.tasks[i].BlockedOn = []string{"ext-1", "c1"}
		}
	}
	fake := &fakeTaskOrchestrator{plan: &provider.CoordinationPlan{GoalID: "GOAL", Actions: map[string]string{}, Ready: []string{}}}
	l.SetTaskOrchestrator(fake)

	l.coordinateGoalViaOrchestrator("ceo", *b.TaskByID("GOAL"))

	var c2Deps []string
	for _, rec := range fake.lastCoord.Children {
		if rec["task_id"] == "c2" {
			c2Deps, _ = rec["depends_on"].([]string)
		}
	}
	// Union, first-seen order: DependsOn(["c1"]) then BlockedOn(["ext-1","c1"]).
	want := []string{"c1", "ext-1"}
	if len(c2Deps) != len(want) || c2Deps[0] != want[0] || c2Deps[1] != want[1] {
		t.Fatalf("c2 depends_on = %v, want union %v", c2Deps, want)
	}
}

func TestGoalCompletes_WhenAllChildrenTerminal(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.tasks = append(b.tasks,
		teamTask{ID: "GOAL", Owner: "ceo", Title: "ship", LifecycleState: LifecycleStateRunning, Orchestrator: orchestratorLangGraph},
		teamTask{ID: "c1", ParentIssueID: "GOAL", Owner: "eng", LifecycleState: LifecycleStateApproved, Orchestrator: orchestratorLangGraph},
		teamTask{ID: "c2", ParentIssueID: "GOAL", Owner: "eng", LifecycleState: LifecycleStateApproved, Orchestrator: orchestratorLangGraph},
	)
	l := &Launcher{broker: b}
	l.SetTaskOrchestrator(&fakeTaskOrchestrator{})

	// Both children terminal -> coordinate returns all-idle; the goal completes.
	plan := &provider.CoordinationPlan{
		GoalID:  "GOAL",
		Actions: map[string]string{"c1": provider.CoordIdle, "c2": provider.CoordIdle},
		Ready:   []string{},
	}
	l.applyCoordinationPlan("ceo", "GOAL", l.taskGoalChildren("GOAL"), plan)

	if got := lifecycleOf(t, b, "GOAL"); got != LifecycleStateReview {
		t.Fatalf("goal with all-terminal children should complete to review; got %q", got)
	}
}

func TestGoalDoesNotComplete_WhenAChildIsStillActive(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.tasks = append(b.tasks,
		teamTask{ID: "GOAL", Owner: "ceo", LifecycleState: LifecycleStateRunning, Orchestrator: orchestratorLangGraph},
		teamTask{ID: "c1", ParentIssueID: "GOAL", Owner: "eng", LifecycleState: LifecycleStateApproved, Orchestrator: orchestratorLangGraph},
		teamTask{ID: "c2", ParentIssueID: "GOAL", Owner: "eng", LifecycleState: LifecycleStateRunning, Orchestrator: orchestratorLangGraph},
	)
	l := &Launcher{broker: b}
	l.SetTaskOrchestrator(&fakeTaskOrchestrator{})

	plan := &provider.CoordinationPlan{GoalID: "GOAL", Actions: map[string]string{"c1": provider.CoordIdle, "c2": provider.CoordDispatch}, Ready: []string{"c2"}}
	l.applyCoordinationPlan("ceo", "GOAL", l.taskGoalChildren("GOAL"), plan)

	if got := lifecycleOf(t, b, "GOAL"); got != LifecycleStateRunning {
		t.Fatalf("goal with an active child must stay running; got %q", got)
	}
}

func TestOrchestratorGoalParent_Guards(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.tasks = append(b.tasks,
		teamTask{ID: "GOAL", LifecycleState: LifecycleStateRunning, Orchestrator: orchestratorLangGraph},
		teamTask{ID: "DONEGOAL", LifecycleState: LifecycleStateReview, Orchestrator: orchestratorLangGraph},
		teamTask{ID: "PLAINGOAL", LifecycleState: LifecycleStateRunning}, // not orchestrator-owned
	)
	l := &Launcher{broker: b}
	l.SetTaskOrchestrator(&fakeTaskOrchestrator{})

	cases := []struct {
		name  string
		child teamTask
		want  bool
	}{
		{"child of running orchestrator goal re-ticks", teamTask{ID: "c", ParentIssueID: "GOAL"}, true},
		{"top-level task never re-ticks a parent", teamTask{ID: "c"}, false},
		{"child of a completed (review) goal does not re-tick", teamTask{ID: "c", ParentIssueID: "DONEGOAL"}, false},
		{"child of a non-orchestrator parent does not re-tick", teamTask{ID: "c", ParentIssueID: "PLAINGOAL"}, false},
		{"child of a missing parent does not re-tick", teamTask{ID: "c", ParentIssueID: "ghost"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := l.orchestratorGoalParent(tc.child)
			if ok != tc.want {
				t.Fatalf("orchestratorGoalParent ok = %v, want %v", ok, tc.want)
			}
		})
	}
}

func TestRetick_CoordinatesParentGoal(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	b.tasks = append(b.tasks,
		teamTask{ID: "GOAL", Owner: "ceo", LifecycleState: LifecycleStateRunning, Orchestrator: orchestratorLangGraph},
		teamTask{ID: "c1", ParentIssueID: "GOAL", Owner: "eng", LifecycleState: LifecycleStateRunning, Orchestrator: orchestratorLangGraph},
	)
	l := &Launcher{broker: b}
	fake := &fakeTaskOrchestrator{
		coordSignal: make(chan string, 1),
		plan:        &provider.CoordinationPlan{GoalID: "GOAL", Actions: map[string]string{}, Ready: []string{}},
	}
	l.SetTaskOrchestrator(fake)

	// A child's state change must re-coordinate its parent goal.
	l.retickOrchestratorGoalParent(teamTask{ID: "c1", ParentIssueID: "GOAL"})

	select {
	case gid := <-fake.coordSignal:
		if gid != "GOAL" {
			t.Fatalf("re-tick coordinated %q, want GOAL", gid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("re-tick did not coordinate the parent goal")
	}
}

func TestCoordinateGoal_CycleCoordinatesNothing(t *testing.T) {
	t.Parallel()
	l, b := goalBroker(t)
	fake := &fakeTaskOrchestrator{plan: &provider.CoordinationPlan{
		GoalID:  "GOAL",
		Actions: map[string]string{"c1": provider.CoordBlock, "c2": provider.CoordBlock},
		Ready:   []string{},
		Cycle:   []string{"c1", "c2", "c1"},
	}}
	l.SetTaskOrchestrator(fake)

	l.coordinateGoalViaOrchestrator("ceo", *b.TaskByID("GOAL"))

	if got := lifecycleOf(t, b, "c1"); got != LifecycleStateReady {
		t.Fatalf("a cycle must coordinate nothing; c1 = %q, want ready", got)
	}
}
