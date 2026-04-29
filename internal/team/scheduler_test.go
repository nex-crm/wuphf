package team

// Tests for the extracted watchdogScheduler type. Per PLAN.md §C4 this is
// the first goroutine extraction; tests use a manual clock plus an
// onTickDone signal channel to drive deterministic assertions without
// time.Sleep (the user's hard rule).
//
// processDueWorkflowJob is intentionally not exercised here — it shells
// out to action.NewRegistryFromEnv() + provider.ExecuteWorkflow, which
// would require a live external workflow registry. Its scheduling
// primitive (nextWorkflowRun) is tested via direct calls.

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNextWorkflowRun_EmptyExpressionReturnsFalse(t *testing.T) {
	if _, ok := nextWorkflowRun("", time.Now()); ok {
		t.Errorf("empty schedule expr should return ok=false")
	}
}

func TestNextWorkflowRun_InvalidExpressionReturnsFalse(t *testing.T) {
	if _, ok := nextWorkflowRun("not-a-cron", time.Now()); ok {
		t.Errorf("invalid schedule expr should return ok=false")
	}
}

func TestNextWorkflowRun_ValidCronAdvances(t *testing.T) {
	after := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	next, ok := nextWorkflowRun("0 * * * *", after) // every hour at minute 0
	if !ok {
		t.Fatalf("expected ok=true for valid cron")
	}
	if !next.After(after) {
		t.Errorf("next run should be after the reference time; got %v <= %v", next, after)
	}
}

func TestSchedulerProcessOnce_TaskUnclaimedFiresAlert(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	b.tasks = []teamTask{{
		ID: "t1", Channel: "general", Title: "do thing", Owner: "", Status: "in_progress",
	}}
	b.dueJobs = []schedulerJob{{
		Slug: "j1", Channel: "general", TargetType: "task", TargetID: "t1",
	}}

	delivered := 0
	sched := &watchdogScheduler{
		broker:      b,
		clock:       newManualClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)),
		deliverTask: func(officeActionLog, teamTask) { delivered++ },
	}
	sched.processOnce()

	if len(b.alerts) != 1 || b.alerts[0].kind != "task_unclaimed" {
		t.Fatalf("expected one task_unclaimed alert; got %+v", b.alerts)
	}
	if delivered != 1 {
		t.Errorf("expected one notification delivery; got %d", delivered)
	}
	if len(b.jobStateUpdates) == 0 || b.jobStateUpdates[len(b.jobStateUpdates)-1].status != "scheduled" {
		t.Errorf("expected job rescheduled; got %+v", b.jobStateUpdates)
	}
}

func TestSchedulerProcessOnce_TaskOwnedFiresStalled(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	b.tasks = []teamTask{{
		ID: "t1", Channel: "general", Title: "do thing", Owner: "eng", Status: "in_progress",
	}}
	b.dueJobs = []schedulerJob{{
		Slug: "j1", Channel: "general", TargetType: "task", TargetID: "t1",
	}}

	sched := &watchdogScheduler{
		broker:      b,
		clock:       newManualClock(time.Now()),
		deliverTask: func(officeActionLog, teamTask) {},
	}
	sched.processOnce()

	if len(b.alerts) != 1 || b.alerts[0].kind != "task_stalled" {
		t.Fatalf("expected one task_stalled alert; got %+v", b.alerts)
	}
}

func TestSchedulerProcessOnce_DoneTaskMarksJobDone(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	b.tasks = []teamTask{{
		ID: "t1", Channel: "general", Title: "do thing", Owner: "eng", Status: "done",
	}}
	b.dueJobs = []schedulerJob{{
		Slug: "j1", Channel: "general", TargetType: "task", TargetID: "t1",
	}}

	sched := &watchdogScheduler{broker: b, clock: newManualClock(time.Now()), deliverTask: func(officeActionLog, teamTask) {}}
	sched.processOnce()

	if len(b.alerts) != 0 {
		t.Errorf("done task should not trigger an alert; got %+v", b.alerts)
	}
	if len(b.jobStateUpdates) != 1 || b.jobStateUpdates[0].status != "done" {
		t.Errorf("expected job marked done; got %+v", b.jobStateUpdates)
	}
}

func TestSchedulerProcessOnce_BlockedTaskSkipsAlert(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	b.tasks = []teamTask{{
		ID: "t1", Channel: "general", Title: "do thing", Owner: "eng", Status: "in_progress", Blocked: true,
	}}
	b.dueJobs = []schedulerJob{{
		Slug: "j1", Channel: "general", TargetType: "task", TargetID: "t1",
	}}
	sched := &watchdogScheduler{broker: b, clock: newManualClock(time.Now()), deliverTask: func(officeActionLog, teamTask) {}}
	sched.processOnce()
	if len(b.alerts) != 0 {
		t.Errorf("blocked tasks must not trigger watchdog reminders; got %+v", b.alerts)
	}
	if len(b.jobStateUpdates) != 1 || b.jobStateUpdates[0].status != "scheduled" {
		t.Errorf("blocked task job should reschedule; got %+v", b.jobStateUpdates)
	}
}

func TestSchedulerProcessOnce_RequestActiveFiresAlertAndPostsAutomation(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	b.requests = []humanInterview{{
		ID: "r1", Channel: "general", Title: "approve", From: "ceo",
		Question: "do this?", Blocking: true,
		Status: "pending",
	}}
	b.dueJobs = []schedulerJob{{
		Slug: "j1", Channel: "general", TargetType: "request", TargetID: "r1",
	}}
	sched := &watchdogScheduler{broker: b, clock: newManualClock(time.Now()), deliverTask: func(officeActionLog, teamTask) {}}
	sched.processOnce()

	if len(b.alerts) != 1 || b.alerts[0].kind != "request_waiting" {
		t.Fatalf("expected one request_waiting alert; got %+v", b.alerts)
	}
	if len(b.automationPosts) != 1 {
		t.Errorf("expected one automation post for blocking request; got %+v", b.automationPosts)
	}
}

func TestSchedulerProcessOnce_RequestInactiveMarksJobDone(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	b.requests = []humanInterview{{
		ID: "r1", Channel: "general", Title: "approve", Status: "answered",
	}}
	b.dueJobs = []schedulerJob{{
		Slug: "j1", Channel: "general", TargetType: "request", TargetID: "r1",
	}}
	sched := &watchdogScheduler{broker: b, clock: newManualClock(time.Now()), deliverTask: func(officeActionLog, teamTask) {}}
	sched.processOnce()
	if len(b.jobStateUpdates) != 1 || b.jobStateUpdates[0].status != "done" {
		t.Errorf("inactive request job should be marked done; got %+v", b.jobStateUpdates)
	}
}

func TestSchedulerProcessOnce_UnknownTargetTypeReschedules(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	b.dueJobs = []schedulerJob{{
		Slug: "j1", Channel: "general", TargetType: "mystery", TargetID: "x",
	}}
	sched := &watchdogScheduler{broker: b, clock: newManualClock(time.Now()), deliverTask: func(officeActionLog, teamTask) {}}
	sched.processOnce()
	if len(b.jobStateUpdates) != 1 || b.jobStateUpdates[0].status != "scheduled" {
		t.Errorf("unknown target type should reschedule; got %+v", b.jobStateUpdates)
	}
}

func TestSchedulerRecordLedger_EmptyOwnerEscalates(t *testing.T) {
	// Verify the escalate_to_ceo branch when owner is blank — the
	// scheduler should ask the CEO to re-triage instead of nudging an
	// empty owner.
	var got struct{ kind, owner string }
	b := &recordingLedgerBroker{
		signals: []officeSignalRecord{{ID: "sig-1"}},
		onDecision: func(kind, owner string) {
			got.kind = kind
			got.owner = owner
		},
	}
	w := &watchdogScheduler{broker: b, clock: newManualClock(time.Now())}
	_, decisionID := w.recordLedger("general", "task_unclaimed", "t1", "  ", "summary", "src-1")
	if got.kind != "escalate_to_ceo" || got.owner != "ceo" {
		t.Errorf("expected escalate_to_ceo for ceo; got kind=%q owner=%q", got.kind, got.owner)
	}
	if decisionID == "" {
		t.Errorf("expected non-empty decision ID")
	}
}

func TestSchedulerRecordLedger_RequestWaitingAsksHuman(t *testing.T) {
	var got struct{ kind, owner string }
	b := &recordingLedgerBroker{
		signals: []officeSignalRecord{{ID: "sig-1"}},
		onDecision: func(kind, owner string) {
			got.kind = kind
			got.owner = owner
		},
	}
	w := &watchdogScheduler{broker: b, clock: newManualClock(time.Now())}
	_, _ = w.recordLedger("general", "request_waiting", "r1", "ceo", "summary", "")
	if got.kind != "ask_human" || got.owner != "ceo" {
		t.Errorf("expected ask_human routed to ceo; got kind=%q owner=%q", got.kind, got.owner)
	}
}

func TestSchedulerUpdateJob_PersistsToBroker(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	// Capture SetSchedulerJob into the fixture's deliveredActions log so
	// the assertion is concrete.
	var captured schedulerJob
	b2 := &capturingSetJobBroker{
		schedulerFixtureBroker: b,
		captureSet: func(j schedulerJob) {
			captured = j
		},
	}
	w := &watchdogScheduler{
		broker: b2,
		clock:  newManualClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
	}
	w.updateJob("watchdog", "Watchdog", 5*time.Minute, time.Date(2026, 6, 1, 1, 0, 0, 0, time.UTC), "sleeping")
	if captured.Slug != "watchdog" || captured.Label != "Watchdog" {
		t.Errorf("SetSchedulerJob received wrong identity: %+v", captured)
	}
	if captured.IntervalMinutes != 5 {
		t.Errorf("expected interval=5min; got %d", captured.IntervalMinutes)
	}
	if captured.Status != "sleeping" || captured.LastRun == "" {
		t.Errorf("sleeping status should populate LastRun; got %+v", captured)
	}
}

func TestSchedulerProcessOnce_NilBrokerNoOp(t *testing.T) {
	w := &watchdogScheduler{clock: newManualClock(time.Now())}
	w.processOnce() // must not panic
}

func TestSchedulerProcessOnce_EmptyJobsNoOp(t *testing.T) {
	b := newSchedulerFixtureBroker(t)
	w := &watchdogScheduler{broker: b, clock: newManualClock(time.Now())}
	w.processOnce()
	if len(b.jobStateUpdates) != 0 || len(b.alerts) != 0 {
		t.Errorf("expected no side effects on empty job list")
	}
}

func TestSchedulerSignalTick_NilChannelNoOp(t *testing.T) {
	w := &watchdogScheduler{}
	w.signalTick() // must not panic when onTickDone is nil
}

func TestSchedulerStop_IdempotentBeforeStart(t *testing.T) {
	w := &watchdogScheduler{}
	w.Stop() // must not panic when stopCh is nil
	w.Stop() // and must be idempotent
}

func TestSchedulerStart_IdempotentDoubleStartIsNoOp(t *testing.T) {
	w := &watchdogScheduler{
		clock:        newManualClock(time.Now()),
		initialDelay: time.Hour,
		pollEvery:    time.Hour,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	w.Start(ctx) // second call must hit startOnce and not spawn another goroutine
	cancel()
	w.Stop()
}

func TestSchedulerUpdateJob_NilBrokerNoOp(t *testing.T) {
	w := &watchdogScheduler{clock: newManualClock(time.Now())}
	w.updateJob("x", "X", time.Minute, time.Now(), "scheduled")
}

func TestSchedulerRun_NilBrokerExits(t *testing.T) {
	// run() with broker=nil should exit immediately without calling After.
	w := &watchdogScheduler{
		clock:        newManualClock(time.Now()),
		initialDelay: time.Minute,
	}
	w.stopCh = make(chan struct{})
	w.done.Add(1)
	go w.run(context.Background())
	w.done.Wait() // must complete without us advancing the clock
}

func TestSchedulerWait_StopChCancels(t *testing.T) {
	clk := newManualClock(time.Now())
	w := &watchdogScheduler{clock: clk, stopCh: make(chan struct{})}
	close(w.stopCh)
	if got := w.wait(context.Background(), time.Hour); got {
		t.Errorf("wait() should return false when stopCh is closed")
	}
}

func TestRealClockAfterFires(t *testing.T) {
	// Cheap exercise of realClock.After so its production path is
	// represented in coverage. Uses a 1ms timer instead of time.Sleep —
	// the only blocking primitive is the timer channel itself.
	c := realClock{}
	select {
	case <-c.After(time.Millisecond):
	case <-time.After(time.Second):
		t.Fatalf("realClock.After never fired within 1s")
	}
	if c.Now().IsZero() {
		t.Errorf("realClock.Now should never return zero time")
	}
}

func TestSchedulerStartStop_DeterministicTickAndShutdown(t *testing.T) {
	// Lifecycle test: Start() spawns the goroutine, manual clock advance
	// triggers the first processOnce (signaled via onTickDone), Stop()
	// drains the goroutine deterministically.
	b := newSchedulerFixtureBroker(t)
	b.dueJobs = []schedulerJob{{
		Slug: "j1", Channel: "general", TargetType: "mystery", TargetID: "x",
	}}
	clk := newManualClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	tickDone := make(chan struct{}, 4)
	sched := &watchdogScheduler{
		broker:       b,
		clock:        clk,
		initialDelay: 15 * time.Second,
		pollEvery:    20 * time.Second,
		deliverTask:  func(officeActionLog, teamTask) {},
		onTickDone:   tickDone,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)

	// Wait for the goroutine to register its initial-delay sleeper before
	// advancing the clock. Without this, Advance can race ahead of the
	// goroutine and the sleeper never fires. No time.Sleep needed — the
	// manual clock's `registered` channel signals each After() call.
	select {
	case <-clk.registered:
	case <-time.After(2 * time.Second):
		t.Fatalf("scheduler goroutine never registered its first sleeper")
	}

	// Advance past the initial delay; the loop should fire processOnce and
	// signal tickDone exactly once.
	clk.Advance(20 * time.Second)
	select {
	case <-tickDone:
	case <-time.After(2 * time.Second):
		t.Fatalf("scheduler did not signal first tick within 2s")
	}

	if len(b.jobStateUpdates) < 1 {
		t.Errorf("expected at least one job state update after first tick; got %+v", b.jobStateUpdates)
	}

	sched.Stop()

	// After Stop(), advance the clock and confirm no further ticks fire.
	clk.Advance(60 * time.Second)
	select {
	case <-tickDone:
		t.Fatalf("expected no tick after Stop() drained the worker")
	case <-time.After(50 * time.Millisecond):
	}
}

// ── manual clock + fixture broker for deterministic scheduler tests ──

// manualClock is a minimal clock that exposes Advance() to release sleepers
// past their deadline. After registers a one-shot sleeper that fires when
// Advance crosses its deadline. registered is a buffered notification
// channel: each After() call sends a struct, so tests can synchronously
// wait for the goroutine to register its sleeper before advancing — this
// is what kills the race between Start() and Advance() in lifecycle tests
// without resorting to time.Sleep (the user's hard rule).
type manualClock struct {
	mu         sync.Mutex
	now        time.Time
	pending    []manualSleeper
	stopOnce   sync.Once
	registered chan struct{}
}

type manualSleeper struct {
	deadline time.Time
	fire     chan time.Time
}

func newManualClock(start time.Time) *manualClock {
	return &manualClock{now: start, registered: make(chan struct{}, 16)}
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	deadline := c.now.Add(d)
	ch := make(chan time.Time, 1)
	if !c.now.Before(deadline) {
		ch <- c.now
		c.mu.Unlock()
		select {
		case c.registered <- struct{}{}:
		default:
		}
		return ch
	}
	c.pending = append(c.pending, manualSleeper{deadline: deadline, fire: ch})
	c.mu.Unlock()
	select {
	case c.registered <- struct{}{}:
	default:
	}
	return ch
}

// Advance moves the clock forward by d and releases any pending sleepers
// whose deadline has been passed. Each released channel receives the
// "now" value the scheduler would observe.
func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	now := c.now
	keep := c.pending[:0]
	for _, s := range c.pending {
		if !s.deadline.After(now) {
			s.fire <- now
		} else {
			keep = append(keep, s)
		}
	}
	c.pending = keep
	c.mu.Unlock()
}

// schedulerFixtureBroker is a recording stub for the scheduler-broker
// surface. Each call appends to its log slice; tests assert on the log
// rather than on a real Broker fixture (which would require dragging in
// JSON state files and the full broker init).
type schedulerFixtureBroker struct {
	tasks            []teamTask
	requests         []humanInterview
	dueJobs          []schedulerJob
	alerts           []alertCall
	jobStateUpdates  []jobStateCall
	automationPosts  []automationCall
	deliveredActions []string
}

type alertCall struct {
	kind, channel, targetType, targetID, owner, summary string
}
type jobStateCall struct {
	slug, status string
}
type automationCall struct {
	channel, body string
}

func newSchedulerFixtureBroker(t *testing.T) *schedulerFixtureBroker {
	t.Helper()
	return &schedulerFixtureBroker{}
}

func (b *schedulerFixtureBroker) DueSchedulerJobs() []schedulerJob { return b.dueJobs }

func (b *schedulerFixtureBroker) FindTask(channel, id string) (teamTask, bool) {
	want := normalizeChannelSlug(channel)
	for _, t := range b.tasks {
		if t.ID == id && normalizeChannelSlug(t.Channel) == want {
			return t, true
		}
	}
	return teamTask{}, false
}

func (b *schedulerFixtureBroker) FindRequest(channel, id string) (humanInterview, bool) {
	want := normalizeChannelSlug(channel)
	for _, r := range b.requests {
		if r.ID == id && normalizeChannelSlug(r.Channel) == want {
			return r, true
		}
	}
	return humanInterview{}, false
}

func (b *schedulerFixtureBroker) UpdateSchedulerJobState(slug string, _ time.Time, status string) error {
	b.jobStateUpdates = append(b.jobStateUpdates, jobStateCall{slug: slug, status: status})
	return nil
}

func (b *schedulerFixtureBroker) CreateWatchdogAlert(kind, channel, targetType, targetID, owner, summary string) (watchdogAlert, bool, error) {
	b.alerts = append(b.alerts, alertCall{kind: kind, channel: channel, targetType: targetType, targetID: targetID, owner: owner, summary: summary})
	return watchdogAlert{ID: "alert-1", Kind: kind, Channel: channel}, false, nil
}

func (b *schedulerFixtureBroker) RecordSignals(_ []officeSignal) ([]officeSignalRecord, error) {
	return nil, nil
}

func (b *schedulerFixtureBroker) RecordDecision(kind, channel, summary, reason, owner string, _ []string, _, _ bool) (officeDecisionRecord, error) {
	return officeDecisionRecord{ID: "decision-1"}, nil
}

func (b *schedulerFixtureBroker) RecordAction(kind, source, channel, actor, summary, related string, _ []string, _ string) error {
	b.deliveredActions = append(b.deliveredActions, kind+":"+related)
	return nil
}

func (b *schedulerFixtureBroker) ResumeTask(id, by, note string) (teamTask, bool, error) {
	return teamTask{}, false, nil
}

func (b *schedulerFixtureBroker) PostAutomationMessage(_ string, channel string, _, body, _, _, _ string, _ []string, _ string) (channelMessage, bool, error) {
	b.automationPosts = append(b.automationPosts, automationCall{channel: channel, body: body})
	return channelMessage{}, false, nil
}

func (b *schedulerFixtureBroker) UpdateSkillExecutionByWorkflowKey(_, _ string, _ time.Time) error {
	return nil
}

func (b *schedulerFixtureBroker) SetSchedulerJob(_ schedulerJob) error { return nil }

// recordingLedgerBroker is a minimal stub that captures the kind+owner
// passed to RecordDecision. Used by recordLedger branch tests.
type recordingLedgerBroker struct {
	signals    []officeSignalRecord
	onDecision func(kind, owner string)
}

func (b *recordingLedgerBroker) DueSchedulerJobs() []schedulerJob         { return nil }
func (b *recordingLedgerBroker) FindTask(string, string) (teamTask, bool) { return teamTask{}, false }
func (b *recordingLedgerBroker) FindRequest(string, string) (humanInterview, bool) {
	return humanInterview{}, false
}
func (b *recordingLedgerBroker) UpdateSchedulerJobState(string, time.Time, string) error { return nil }
func (b *recordingLedgerBroker) CreateWatchdogAlert(string, string, string, string, string, string) (watchdogAlert, bool, error) {
	return watchdogAlert{}, false, nil
}
func (b *recordingLedgerBroker) RecordSignals(_ []officeSignal) ([]officeSignalRecord, error) {
	return b.signals, nil
}
func (b *recordingLedgerBroker) RecordDecision(kind, _, _, _, owner string, _ []string, _, _ bool) (officeDecisionRecord, error) {
	if b.onDecision != nil {
		b.onDecision(kind, owner)
	}
	return officeDecisionRecord{ID: "decision-1"}, nil
}
func (b *recordingLedgerBroker) RecordAction(string, string, string, string, string, string, []string, string) error {
	return nil
}
func (b *recordingLedgerBroker) ResumeTask(string, string, string) (teamTask, bool, error) {
	return teamTask{}, false, nil
}
func (b *recordingLedgerBroker) PostAutomationMessage(string, string, string, string, string, string, string, []string, string) (channelMessage, bool, error) {
	return channelMessage{}, false, nil
}
func (b *recordingLedgerBroker) UpdateSkillExecutionByWorkflowKey(string, string, time.Time) error {
	return nil
}
func (b *recordingLedgerBroker) SetSchedulerJob(schedulerJob) error { return nil }

// capturingSetJobBroker wraps schedulerFixtureBroker and intercepts
// SetSchedulerJob so updateJob persistence can be asserted on directly.
type capturingSetJobBroker struct {
	*schedulerFixtureBroker
	captureSet func(schedulerJob)
}

func (b *capturingSetJobBroker) SetSchedulerJob(j schedulerJob) error {
	if b.captureSet != nil {
		b.captureSet(j)
	}
	return nil
}

// Sanity: assert that the launcher wires the scheduler with broker, clock,
// and deliverTask actually populated. Catches regressions where scheduler()
// stops plumbing one of these fields and processOnce silently degrades.
func TestLauncher_SchedulerWiring_PopulatesBrokerClockAndDeliver(t *testing.T) {
	l := &Launcher{broker: &Broker{}}
	s := l.scheduler()
	if s == nil {
		t.Fatalf("scheduler() should return non-nil")
	}
	// Same scheduler instance on subsequent calls (lazy + cached).
	if s2 := l.scheduler(); s2 != s {
		t.Errorf("scheduler() not memoized: first=%p second=%p", s, s2)
	}
	if s.broker == nil {
		t.Error("scheduler.broker should be wired from Launcher.broker")
	}
	if s.clock == nil {
		t.Error("scheduler.clock should default to realClock")
	}
	if _, ok := s.clock.(realClock); !ok {
		t.Errorf("scheduler.clock should be realClock; got %T", s.clock)
	}
	if s.deliverTask == nil {
		t.Error("scheduler.deliverTask should be wired to Launcher.deliverTaskNotification")
	}
}

// launcherSchedulerSentinel exists only so the test above can reference a
// known package-level string without importing internal packages. Trivial
// const so the wiring test has something concrete to assert against.
const launcherSchedulerSentinel = "watchdog scheduler"

// Regression: Launcher.Kill() must drain the watchdog scheduler goroutine
// before returning. The pre-refactor loop was for{ ... time.Sleep } with no
// exit; C4 added Stop() but the first cut of Kill() did not call it, so the
// goroutine outlived Kill and held a reference to the broker. This test
// observes goroutine exit via done.Wait and asserts it closes promptly
// after Kill returns. With the bug present, the goroutine sits forever on
// the 1h initialDelay sleeper and exited never closes within the timeout.
func TestLauncher_KillDrainsScheduler(t *testing.T) {
	clk := newManualClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	sched := &watchdogScheduler{
		broker:       newSchedulerFixtureBroker(t),
		clock:        clk,
		initialDelay: time.Hour, // intentionally never released by the manual clock
		pollEvery:    time.Hour,
		deliverTask:  func(officeActionLog, teamTask) {},
	}
	l := &Launcher{schedulerWorker: sched}
	sched.Start(context.Background())

	select {
	case <-clk.registered:
	case <-time.After(2 * time.Second):
		t.Fatalf("scheduler never registered its initial sleeper")
	}

	// Watch for goroutine exit. done is incremented by Start and decremented
	// by run() at exit; if Kill drains via Stop, this fires immediately.
	exited := make(chan struct{})
	go func() {
		sched.done.Wait()
		close(exited)
	}()

	// Bare &Launcher{} takes the non-tmux Kill path; ignore the error —
	// the only thing we care about is that Stop got invoked along the way.
	_ = l.Kill()

	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Fatalf("scheduler goroutine did not exit within 2s after Kill — Kill is not draining the scheduler")
	}
}
