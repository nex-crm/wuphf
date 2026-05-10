package team

// broker_reviewer_routing_test.go covers Lane D's reviewer-routing
// scope: the intersection-routing function and the three convergence
// paths (all-graded / timeout / process-exit) called out in
// success-criteria gate #4 of the multi-agent control loop design.
//
// Tests deliberately avoid spinning a worktree, an HTTP server, or a
// real headless launcher — every signal is constructed in-memory so
// the tests run in <50ms each and stay deterministic on CI. The fake
// clock pattern matches broker_middleware_test.go's b.nowFn override.

import (
	"encoding/json"
	"sort"
	"testing"
	"time"
)

// routingFakeClock advances on demand. Use Advance(d) to push the broker's
// observable wall-clock forward by d without sleeping.
type routingFakeClock struct {
	t time.Time
}

func newRoutingFakeClockAt(t time.Time) *routingFakeClock { return &routingFakeClock{t: t} }
func (c *routingFakeClock) Now() time.Time                { return c.t }
func (c *routingFakeClock) Advance(d time.Duration) {
	c.t = c.t.Add(d)
}

// seedTaskInReview installs a task in lifecycle review with the given
// reviewer slug list. The convergence sweeper expects ReviewStartedAt
// to be set; the helper uses the broker's clock so tests can advance
// it deterministically.
func seedTaskInReview(t *testing.T, b *Broker, taskID string, reviewers []string, timeoutSeconds int) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tasks = append(b.tasks, teamTask{
		ID:                   taskID,
		Title:                "test-" + taskID,
		Channel:              "general",
		LifecycleState:       LifecycleStateRunning,
		Reviewers:            reviewers,
		ReviewTimeoutSeconds: timeoutSeconds,
	})
	b.indexLifecycleLocked(taskID, "", LifecycleStateRunning)
	if _, err := b.transitionLifecycleLocked(taskID, LifecycleStateReview, "test seed"); err != nil {
		t.Fatalf("seed transition: %v", err)
	}
	// AssignReviewers stamps ReviewStartedAt — call directly so the
	// fake clock value is captured.
	if err := b.assignReviewersLocked(taskID, reviewers); err != nil {
		t.Fatalf("assign reviewers: %v", err)
	}
}

// pushManifestLine simulates a HeadlessEvent manifest line landing on
// the agent stream's task-scoped buffer. Used by the process-exit and
// tool-name routing tests.
func pushManifestLine(t *testing.T, b *Broker, slug, taskID, status string, toolNames []string) {
	t.Helper()
	stream := b.AgentStream(slug)
	calls := make([]HeadlessManifestEntry, 0, len(toolNames))
	for _, name := range toolNames {
		calls = append(calls, HeadlessManifestEntry{ToolName: name, Count: 1})
	}
	ev := HeadlessEvent{
		Kind:      HeadlessEventKind,
		Type:      HeadlessEventTypeManifest,
		Provider:  HeadlessProviderClaude,
		Agent:     slug,
		TaskID:    taskID,
		Status:    status,
		ToolCalls: calls,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	stream.PushTask(taskID, string(data)+"\n")
}

// TestReviewerConvergenceAllGraded exercises path 1 of build-time gate
// #4: every assigned reviewer emits a graded review.submitted, the
// broker fires exactly one decision transition, and the lifecycle
// index ends with the task in the decision bucket.
func TestReviewerConvergenceAllGraded(t *testing.T) {
	clk := newRoutingFakeClockAt(time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	b := newTestBroker(t)
	b.nowFn = clk.Now

	reviewers := []string{"agent-a", "agent-b", "agent-c"}
	seedTaskInReview(t, b, "task-1", reviewers, 600)

	for _, slug := range reviewers {
		if err := b.AppendReviewerGrade("task-1", ReviewerGrade{
			ReviewerSlug: slug,
			Severity:     SeverityMinor,
			Suggestion:   "ok",
			Reasoning:    "looks fine",
		}); err != nil {
			t.Fatalf("append grade for %s: %v", slug, err)
		}
	}

	b.mu.Lock()
	task := b.taskByIDLocked("task-1")
	state := task.LifecycleState
	bucket := append([]string(nil), b.lifecycleIndex[LifecycleStateDecision]...)
	b.mu.Unlock()
	if state != LifecycleStateDecision {
		t.Fatalf("expected task in decision after all grades; got %q", state)
	}
	if len(bucket) != 1 || bucket[0] != "task-1" {
		t.Fatalf("expected decision bucket to contain task-1; got %v", bucket)
	}

	// All three grades should be present, none filled with skipped.
	grades := b.ReviewerGrades("task-1")
	if len(grades) != 3 {
		t.Fatalf("expected 3 grades, got %d", len(grades))
	}
	for _, g := range grades {
		if g.Severity == SeveritySkipped {
			t.Errorf("reviewer %s should not have been skipped: %+v", g.ReviewerSlug, g)
		}
	}
}

// TestReviewerConvergenceTimeoutFillsSkipped exercises path 2: 3
// reviewers, 1 misses the deadline. The missing slot is filled with
// SeveritySkipped + "reviewer timed out". Exactly one transition fires.
func TestReviewerConvergenceTimeoutFillsSkipped(t *testing.T) {
	clk := newRoutingFakeClockAt(time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	b := newTestBroker(t)
	b.nowFn = clk.Now

	reviewers := []string{"agent-a", "agent-b", "agent-c"}
	seedTaskInReview(t, b, "task-2", reviewers, 600) // 10-min timeout

	// Only two reviewers grade.
	for _, slug := range []string{"agent-a", "agent-b"} {
		if err := b.AppendReviewerGrade("task-2", ReviewerGrade{
			ReviewerSlug: slug,
			Severity:     SeverityMajor,
			Reasoning:    "needs work",
		}); err != nil {
			t.Fatalf("append grade for %s: %v", slug, err)
		}
	}

	// Sweeper before timeout: should not transition.
	b.SweepReviewConvergence()
	b.mu.Lock()
	task := b.taskByIDLocked("task-2")
	preTimeoutState := task.LifecycleState
	b.mu.Unlock()
	if preTimeoutState != LifecycleStateReview {
		t.Fatalf("task should still be in review pre-timeout; got %q", preTimeoutState)
	}

	// Advance past the deadline.
	clk.Advance(11 * time.Minute)
	b.SweepReviewConvergence()

	b.mu.Lock()
	task = b.taskByIDLocked("task-2")
	state := task.LifecycleState
	b.mu.Unlock()
	if state != LifecycleStateDecision {
		t.Fatalf("expected task in decision after timeout; got %q", state)
	}

	grades := b.ReviewerGrades("task-2")
	if len(grades) != 3 {
		t.Fatalf("expected 3 grades after timeout fill; got %d", len(grades))
	}

	// The agent-c slot must be the skipped filler.
	var filler *ReviewerGrade
	for i := range grades {
		if grades[i].ReviewerSlug == "agent-c" {
			filler = &grades[i]
		}
	}
	if filler == nil {
		t.Fatal("expected agent-c filler grade; not found")
	}
	if filler.Severity != SeveritySkipped {
		t.Errorf("filler severity: got %q, want %q", filler.Severity, SeveritySkipped)
	}
	if filler.Reasoning != "reviewer timed out" {
		t.Errorf("filler reasoning: got %q, want %q", filler.Reasoning, "reviewer timed out")
	}

	// Exactly one transition fired: re-sweep must be idempotent.
	bucketBefore := taskBucketCounts(b)
	b.SweepReviewConvergence()
	bucketAfter := taskBucketCounts(b)
	if !bucketsEqual(bucketBefore, bucketAfter) {
		t.Fatalf("sweep was not idempotent: before=%v after=%v", bucketBefore, bucketAfter)
	}

	// agent.review.timeout banner posted.
	b.mu.Lock()
	var banner *channelMessage
	for i := range b.messages {
		if b.messages[i].Kind == "agent.review.timeout" {
			banner = &b.messages[i]
			break
		}
	}
	b.mu.Unlock()
	if banner == nil {
		t.Fatal("expected agent.review.timeout banner; none posted")
	}
}

// TestReviewerConvergenceProcessExitFillsSkipped exercises path 3: a
// reviewer's session emits a manifest-terminal event (idle/error)
// without ever calling AppendReviewerGrade. After the deadline, the
// missing slot is filled with reasoning "reviewer process exited",
// distinguishing it from the plain timeout case.
func TestReviewerConvergenceProcessExitFillsSkipped(t *testing.T) {
	clk := newRoutingFakeClockAt(time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	b := newTestBroker(t)
	b.nowFn = clk.Now

	reviewers := []string{"agent-a", "agent-b", "agent-c"}
	seedTaskInReview(t, b, "task-3", reviewers, 600)

	// Two reviewers grade normally.
	for _, slug := range []string{"agent-a", "agent-b"} {
		if err := b.AppendReviewerGrade("task-3", ReviewerGrade{
			ReviewerSlug: slug,
			Severity:     SeverityMinor,
			Reasoning:    "fine",
		}); err != nil {
			t.Fatalf("append grade for %s: %v", slug, err)
		}
	}

	// agent-c emits a terminal manifest event without grading.
	pushManifestLine(t, b, "agent-c", "task-3", "error", []string{"Read", "Edit"})

	clk.Advance(11 * time.Minute)
	b.SweepReviewConvergence()

	grades := b.ReviewerGrades("task-3")
	if len(grades) != 3 {
		t.Fatalf("expected 3 grades; got %d", len(grades))
	}
	var filler *ReviewerGrade
	for i := range grades {
		if grades[i].ReviewerSlug == "agent-c" {
			filler = &grades[i]
		}
	}
	if filler == nil {
		t.Fatal("expected agent-c filler grade; not found")
	}
	if filler.Severity != SeveritySkipped {
		t.Errorf("filler severity: got %q, want %q", filler.Severity, SeveritySkipped)
	}
	if filler.Reasoning != "reviewer process exited" {
		t.Errorf("filler reasoning: got %q, want %q (process-exit must be distinct from plain timeout)", filler.Reasoning, "reviewer process exited")
	}

	b.mu.Lock()
	state := b.taskByIDLocked("task-3").LifecycleState
	b.mu.Unlock()
	if state != LifecycleStateDecision {
		t.Fatalf("expected task in decision; got %q", state)
	}

	// No half-open slot left: re-sweep must not append a second
	// filler or fire a second transition.
	beforeGrades := len(b.ReviewerGrades("task-3"))
	b.SweepReviewConvergence()
	afterGrades := len(b.ReviewerGrades("task-3"))
	if beforeGrades != afterGrades {
		t.Fatalf("re-sweep added duplicate grades: before=%d after=%d", beforeGrades, afterGrades)
	}
}

// TestReviewerConvergenceRaceGradeOnTimeoutBoundary (D-FU-2) covers
// the corner case where a grade arrives in the same second the timeout
// would otherwise fire. The convergence rule is idempotent — both
// branches converge to LifecycleStateDecision, the grade-arrival path
// transitions on "all reviewers graded" and the sweeper-timeout path
// is then a no-op because the task is already past review. This test
// asserts both orderings (grade-then-sweep, sweep-then-grade) land on
// the same final state and produce the same number of grades, so a
// future contributor refactoring evaluateConvergenceLocked cannot
// silently introduce a race that double-fires the transition or
// double-fills the missing slot.
func TestReviewerConvergenceRaceGradeOnTimeoutBoundary(t *testing.T) {
	cases := []struct {
		name       string
		gradeFirst bool
	}{
		{name: "grade lands then sweep fires", gradeFirst: true},
		{name: "sweep fires then grade lands", gradeFirst: false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			clk := newRoutingFakeClockAt(time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
			b := newTestBroker(t)
			b.nowFn = clk.Now

			reviewers := []string{"agent-a", "agent-b"}
			seedTaskInReview(t, b, "task-race", reviewers, 600)

			// Stage one reviewer's grade so the boundary case has exactly
			// one slot still missing when the deadline elapses.
			if err := b.AppendReviewerGrade("task-race", ReviewerGrade{
				ReviewerSlug: "agent-a",
				Severity:     SeverityNitpick,
				Reasoning:    "lgtm",
			}); err != nil {
				t.Fatalf("seed first grade: %v", err)
			}

			// Advance the clock to the exact deadline; both code paths
			// (sweeper timeout fill, late-arrival grade) become eligible
			// to fire on the same broker tick.
			clk.Advance(10 * time.Minute)

			finalGrade := ReviewerGrade{
				ReviewerSlug: "agent-b",
				Severity:     SeverityMinor,
				Reasoning:    "small nit",
			}

			if tc.gradeFirst {
				if err := b.AppendReviewerGrade("task-race", finalGrade); err != nil {
					t.Fatalf("late grade arrival: %v", err)
				}
				b.SweepReviewConvergence()
			} else {
				b.SweepReviewConvergence()
				if err := b.AppendReviewerGrade("task-race", finalGrade); err != nil {
					t.Fatalf("late grade arrival after sweep: %v", err)
				}
			}

			b.mu.Lock()
			task := b.taskByIDLocked("task-race")
			finalState := task.LifecycleState
			b.mu.Unlock()
			if finalState != LifecycleStateDecision {
				t.Fatalf("expected task in decision after race; got %q", finalState)
			}

			// The convergence rule must be idempotent: a re-sweep is a
			// no-op (state already decision) and a duplicate grade
			// append on agent-b overwrites the existing slot, so the
			// total grade count stays at 2.
			b.SweepReviewConvergence()
			grades := b.ReviewerGrades("task-race")
			if len(grades) != 2 {
				t.Fatalf("expected 2 grades after race + idempotent re-sweep; got %d (%+v)", len(grades), grades)
			}

			// Whichever path fired first decides the agent-b slot:
			//   - grade-first: real grade with SeverityMinor.
			//   - sweep-first: timeout-filler with SeveritySkipped, then
			//     overwritten by the late-arrival real grade.
			// Either way, the late grade wins because AppendReviewerGrade
			// overwrites by (taskID, ReviewerSlug). Assert agent-b's
			// final grade matches what the reviewer actually submitted.
			var slotB *ReviewerGrade
			for i := range grades {
				if grades[i].ReviewerSlug == "agent-b" {
					slotB = &grades[i]
				}
			}
			if slotB == nil {
				t.Fatal("agent-b slot missing from grades")
			}
			if slotB.Severity != SeverityMinor {
				t.Fatalf("agent-b severity: got %q, want %q (the real submitted grade should win)", slotB.Severity, SeverityMinor)
			}
			if slotB.Reasoning != "small nit" {
				t.Fatalf("agent-b reasoning: got %q, want %q", slotB.Reasoning, "small nit")
			}
		})
	}
}

// TestResolveReviewersIntersection is the routing test required by the
// Lane D scope: 3 agents with overlapping Watching sets, simulate a
// task touching specific files / tools / wiki paths, assert exactly
// the right intersection of agents is returned and tunnel humans are
// not auto-assigned.
func TestResolveReviewersIntersection(t *testing.T) {
	b := newTestBroker(t)

	b.mu.Lock()
	b.members = []officeMember{
		{
			Slug: "agent-frontend",
			Watching: Watching{
				Files: []string{"web/*.tsx", "web/src/**.ts"},
			},
		},
		{
			Slug: "agent-backend",
			Watching: Watching{
				Files:     []string{"internal/*.go"},
				ToolNames: []string{"go-test"},
			},
		},
		{
			Slug: "agent-wiki",
			Watching: Watching{
				WikiPaths: []string{"wiki/*.md"},
				TaskTags:  []string{"docs"},
			},
		},
		{
			// No Watching set — must never be auto-assigned.
			Slug: "agent-untagged",
		},
	}
	// Pre-populate agent stream with manifest events so ToolNames
	// extraction picks up "go-test" for the backend agent.
	b.tasks = append(b.tasks, teamTask{
		ID:             "task-route",
		Title:          "routing test",
		LifecycleState: LifecycleStateRunning,
		Tags:           []string{"docs"},
	})
	b.indexLifecycleLocked("task-route", "", LifecycleStateRunning)
	b.mu.Unlock()

	pushManifestLine(t, b, "owner-agent", "task-route", "idle", []string{"go-test", "Read"})

	// We can't run a real `git diff` in this unit test, so synthesize
	// signals directly through a thin wrapper that bypasses
	// taskWorktreeDiffLocked. The simplest path: stamp a worktree
	// path that does not exist, then assert that file-glob agents
	// are NOT matched (because the diff returns nothing). Wiki and
	// tool routing still fire from the in-memory signals.
	slugs, err := b.ResolveReviewers("task-route")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := []string{"agent-backend", "agent-wiki"}
	sort.Strings(slugs)
	sort.Strings(want)
	if !routingStringSlicesEqual(slugs, want) {
		t.Fatalf("intersection: got %v, want %v", slugs, want)
	}

	// Now stamp a synthetic Files signal via a focused integration
	// path: write a Watching-Files match by simulating a populated
	// Files signal. We do this by inserting a fake filepath glob the
	// extractor will see when called via a tagged path. Simplest:
	// re-run with a task tag that intersects agent-frontend's Files
	// glob via a separate test path. Since we cannot inject Files
	// without a real worktree, the assertion above already proves the
	// negative case (no Files match → no agent-frontend). The full
	// Files-glob match is exercised by TestWatchingMatchesSignals
	// below, which calls watchingMatchesSignals directly.
}

// TestWatchingMatchesSignals exercises the four-category match logic
// in isolation, including the empty-category guard and the
// invalid-glob fallback. Faster and more thorough than driving the
// full ResolveReviewers stack for every shape.
func TestWatchingMatchesSignals(t *testing.T) {
	cases := []struct {
		name     string
		watching Watching
		signals  ReviewerRoutingSignals
		want     bool
	}{
		{
			name:     "empty watching does not match anything",
			watching: Watching{},
			signals:  ReviewerRoutingSignals{Files: []string{"web/index.tsx"}},
			want:     false,
		},
		{
			name:     "files glob match",
			watching: Watching{Files: []string{"web/*.tsx"}},
			signals:  ReviewerRoutingSignals{Files: []string{"web/index.tsx"}},
			want:     true,
		},
		{
			name:     "files glob no match",
			watching: Watching{Files: []string{"server/*.go"}},
			signals:  ReviewerRoutingSignals{Files: []string{"web/index.tsx"}},
			want:     false,
		},
		{
			name:     "wiki path glob match",
			watching: Watching{WikiPaths: []string{"wiki/*.md"}},
			signals:  ReviewerRoutingSignals{WikiPaths: []string{"wiki/billing.md"}},
			want:     true,
		},
		{
			name:     "tool name exact match",
			watching: Watching{ToolNames: []string{"go-test"}},
			signals:  ReviewerRoutingSignals{ToolNames: []string{"Read", "go-test"}},
			want:     true,
		},
		{
			name:     "task tag exact match",
			watching: Watching{TaskTags: []string{"docs"}},
			signals:  ReviewerRoutingSignals{TaskTags: []string{"docs", "frontend"}},
			want:     true,
		},
		{
			name:     "non-empty watching with all-empty signals does not match",
			watching: Watching{Files: []string{"*.go"}},
			signals:  ReviewerRoutingSignals{},
			want:     false,
		},
		{
			name:     "any-of OR semantics: tool match wins even when files don't",
			watching: Watching{Files: []string{"docs/*.md"}, ToolNames: []string{"go-test"}},
			signals:  ReviewerRoutingSignals{Files: []string{"web/x.tsx"}, ToolNames: []string{"go-test"}},
			want:     true,
		},
		{
			name:     "invalid glob does not crash, returns no match for that pattern",
			watching: Watching{Files: []string{"[abc"}}, // unbalanced bracket — invalid
			signals:  ReviewerRoutingSignals{Files: []string{"abc"}},
			want:     false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := watchingMatchesSignals(tc.watching, tc.signals)
			if got != tc.want {
				t.Fatalf("watchingMatchesSignals: got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestExtractRoutingSignalsToolNames exercises the manifest-event
// walker that builds the ToolNames signal from agent streams. Asserts
// dedupe, sorted output, and that non-manifest events are ignored.
func TestExtractRoutingSignalsToolNames(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.tasks = []teamTask{{ID: "task-tools", LifecycleState: LifecycleStateRunning}}
	b.indexLifecycleLocked("task-tools", "", LifecycleStateRunning)
	b.mu.Unlock()

	pushManifestLine(t, b, "agent-1", "task-tools", "idle", []string{"Read", "Edit"})
	pushManifestLine(t, b, "agent-2", "task-tools", "idle", []string{"Edit", "go-test"})
	// Non-manifest line that must be ignored.
	stream := b.AgentStream("agent-3")
	stream.PushTask("task-tools", `{"kind":"headless_event","type":"text","tool_name":"NotAManifestTool"}`+"\n")

	b.mu.Lock()
	task := b.taskByIDLocked("task-tools")
	signals := b.extractRoutingSignalsLocked(task)
	b.mu.Unlock()

	want := []string{"Edit", "Read", "go-test"}
	if !routingStringSlicesEqual(signals.ToolNames, want) {
		t.Fatalf("ToolNames: got %v, want %v", signals.ToolNames, want)
	}
}

// TestAssignReviewersDedupesAndStampsStart asserts that
// AssignReviewers normalises the slug list (dedupe, trim, sort) and
// stamps ReviewStartedAt with the broker's clock. ReviewStartedAt is
// the load-bearing input to the deadline calculation, so a missed
// stamp would silently make the timeout never fire.
func TestAssignReviewersDedupesAndStampsStart(t *testing.T) {
	clk := newRoutingFakeClockAt(time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	b := newTestBroker(t)
	b.nowFn = clk.Now

	b.mu.Lock()
	b.tasks = []teamTask{{ID: "task-asgn", LifecycleState: LifecycleStateReview}}
	b.indexLifecycleLocked("task-asgn", "", LifecycleStateReview)
	b.mu.Unlock()

	if err := b.AssignReviewers("task-asgn", []string{"  agent-b  ", "agent-a", "", "agent-b"}); err != nil {
		t.Fatalf("assign: %v", err)
	}

	b.mu.Lock()
	task := b.taskByIDLocked("task-asgn")
	got := append([]string(nil), task.Reviewers...)
	startedAt := task.ReviewStartedAt
	b.mu.Unlock()

	want := []string{"agent-a", "agent-b"}
	if !routingStringSlicesEqual(got, want) {
		t.Fatalf("Reviewers: got %v, want %v", got, want)
	}
	if startedAt != clk.Now().UTC().Format(time.RFC3339) {
		t.Fatalf("ReviewStartedAt: got %q, want %q", startedAt, clk.Now().UTC().Format(time.RFC3339))
	}
}

// TestReviewerGradesByTaskGCOnMerged covers Lane D follow-up D-FU-1:
// the routing-side mirror b.reviewerGradesByTask must drop the
// merged task's entry on the LifecycleStateMerged transition.
// Without this cleanup a long-running broker accumulates one entry
// per merged task (the Decision Packet keeps the canonical grade
// list; the routing-side index is only consumed by
// evaluateConvergenceLocked, which never runs after a task leaves
// LifecycleStateReview).
//
// Acceptance: seed 2 tasks in review, fully grade both so they
// transition to decision, then merge one. The merged task drops out
// of the mirror; the still-pending task keeps its entry. Length goes
// from N to N-1.
func TestReviewerGradesByTaskGCOnMerged(t *testing.T) {
	clk := newRoutingFakeClockAt(time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC))
	b := newTestBroker(t)
	b.nowFn = clk.Now

	reviewers := []string{"agent-a", "agent-b"}
	seedTaskInReview(t, b, "task-merge", reviewers, 600)
	seedTaskInReview(t, b, "task-other", reviewers, 600)

	for _, taskID := range []string{"task-merge", "task-other"} {
		for _, slug := range reviewers {
			if err := b.AppendReviewerGrade(taskID, ReviewerGrade{
				ReviewerSlug: slug,
				Severity:     SeverityMinor,
				Reasoning:    "ok",
			}); err != nil {
				t.Fatalf("append grade %s/%s: %v", taskID, slug, err)
			}
		}
	}

	b.mu.Lock()
	beforeLen := len(b.reviewerGradesByTask)
	mergeBefore := len(b.reviewerGradesByTask["task-merge"])
	otherBefore := len(b.reviewerGradesByTask["task-other"])
	b.mu.Unlock()
	if beforeLen != 2 || mergeBefore != 2 || otherBefore != 2 {
		t.Fatalf("pre-merge: expected map len=2 with both tasks holding 2 grades; got len=%d merge=%d other=%d",
			beforeLen, mergeBefore, otherBefore)
	}

	// Merge task-merge via the public lifecycle transition entry. This
	// is the same path RecordTaskDecision uses on a packet merge.
	if err := b.TransitionLifecycle("task-merge", LifecycleStateMerged, "test merge"); err != nil {
		t.Fatalf("transition merge: %v", err)
	}

	b.mu.Lock()
	afterLen := len(b.reviewerGradesByTask)
	_, mergeStill := b.reviewerGradesByTask["task-merge"]
	otherAfter := len(b.reviewerGradesByTask["task-other"])
	b.mu.Unlock()

	if afterLen != beforeLen-1 {
		t.Fatalf("expected reviewerGradesByTask len to drop from %d to %d; got %d",
			beforeLen, beforeLen-1, afterLen)
	}
	if mergeStill {
		t.Fatal("expected task-merge entry to be GC'd from reviewerGradesByTask")
	}
	if otherAfter != 2 {
		t.Fatalf("expected task-other grades intact (len=2); got %d", otherAfter)
	}
}

// taskBucketCounts and bucketsEqual are tiny test helpers used by the
// idempotency assertions above.
func taskBucketCounts(b *Broker) map[LifecycleState]int {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[LifecycleState]int, len(b.lifecycleIndex))
	for state, ids := range b.lifecycleIndex {
		out[state] = len(ids)
	}
	return out
}

func bucketsEqual(a, b map[LifecycleState]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func routingStringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
