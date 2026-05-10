package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
)

// captureStderr swaps os.Stderr for a pipe so test assertions can
// inspect the human-readable transcript the CLI emits.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	prev := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = prev }()
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	w.Close()
	return <-done
}

// captureStdout is the stdout twin of captureStderr.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	prev := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = prev }()
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	fn()
	w.Close()
	return <-done
}

// TestPrintInboxPayloadGroupsByLifecycleState exercises the formatter
// directly with a synthetic payload — no broker round trip needed for
// the grouping/sort assertion.
func TestPrintInboxPayloadGroupsByLifecycleState(t *testing.T) {
	payload := team.InboxPayload{
		Rows: []team.InboxRow{
			{TaskID: "task-1", Title: "fix the cache", LifecycleState: team.LifecycleStateRunning, ElapsedMs: 60_000},
			{TaskID: "task-2", Title: "ship the docs", LifecycleState: team.LifecycleStateDecision, ElapsedMs: 120_000},
			{TaskID: "task-3", Title: "spec the wedge", LifecycleState: team.LifecycleStateRunning, ElapsedMs: 30_000},
		},
		Counts:      team.InboxCounts{NeedsDecision: 1, Running: 2, Blocked: 0, MergedToday: 0},
		RefreshedAt: time.Now().UTC().Format(time.RFC3339),
	}
	out := captureStdout(t, func() { printInboxPayload(payload) })
	if !strings.Contains(out, "RUNNING") || !strings.Contains(out, "DECISION") {
		t.Fatalf("expected RUNNING and DECISION group headings; got:\n%s", out)
	}
	if !strings.Contains(out, "task-1") || !strings.Contains(out, "task-2") || !strings.Contains(out, "task-3") {
		t.Fatalf("expected all three task IDs in output; got:\n%s", out)
	}
	if !strings.Contains(out, "Needs decision: 1") {
		t.Fatalf("expected counts footer; got:\n%s", out)
	}
}

// TestFormatElapsed sanity-checks the human-readable elapsed-time
// formatter the inbox printer uses.
func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		dur  time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{2 * time.Minute, "2m"},
		{3 * time.Hour, "3h"},
		{50 * time.Hour, "2d"},
	}
	for _, c := range cases {
		if got := formatElapsed(c.dur); got != c.want {
			t.Errorf("formatElapsed(%s) = %q, want %q", c.dur, got, c.want)
		}
	}
}

// TestPrintSpecRendersAllSections asserts the Spec printer handles the
// optional fields and the AC list.
func TestPrintSpecRendersAllSections(t *testing.T) {
	spec := team.Spec{
		Problem:       "the cache is stale",
		TargetOutcome: "hits drop after invalidation",
		Assignment:    "audit invalidation",
		AcceptanceCriteria: []team.ACItem{
			{Statement: "hits drop"},
			{Statement: "no stale reads"},
		},
		Constraints: []string{"no breaking changes"},
		AutoAssign:  "owner-eng",
	}
	out := captureStdout(t, func() { printSpec(spec) })
	for _, want := range []string{"the cache is stale", "audit invalidation", "hits drop", "no stale reads", "owner-eng"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q; got:\n%s", want, out)
		}
	}
}

// TestRunTaskCmdHelp verifies the dispatcher prints help when called
// with no args or an explicit help flag.
func TestRunTaskCmdHelp(t *testing.T) {
	out := captureStderr(t, func() { runTaskCmd([]string{"--help"}) })
	if !strings.Contains(out, "wuphf task") || !strings.Contains(out, "start") {
		t.Errorf("expected help text mentioning wuphf task and start; got:\n%s", out)
	}
}

// TestAutoAssignCountdownInterrupt verifies that a keypress during the
// countdown cancels the auto-confirm and falls back to manual y/n.
func TestAutoAssignCountdownInterrupt(t *testing.T) {
	c := team.NewAutoAssignCountdown()
	go func() {
		time.Sleep(10 * time.Millisecond)
		c.Cancel()
	}()
	if c.Wait(context.Background()) {
		t.Errorf("expected Wait to return false on Cancel, got true")
	}
}

// TestIsSafeTaskID locks down the allowlist used to guard `open` /
// `xdg-open` / `rundll32` arguments and the block POST path.
func TestIsSafeTaskID(t *testing.T) {
	good := []string{"task-1", "abc_123", "Task42", "T-1"}
	for _, id := range good {
		if !isSafeTaskID(id) {
			t.Errorf("isSafeTaskID(%q) = false, want true", id)
		}
	}
	bad := []string{"", "task with space", "task;rm", "task/.", "task'$", strings.Repeat("a", 200)}
	for _, id := range bad {
		if isSafeTaskID(id) {
			t.Errorf("isSafeTaskID(%q) = true, want false", id)
		}
	}
}

// fakeBrokerClient is a test double for the brokerClient interface.
// Each method records the call and returns canned data.
type fakeBrokerClient struct {
	startIntake          func(ctx context.Context, intent string) (*team.IntakeOutcome, error)
	transitionLifecycle  func(ctx context.Context, taskID string, to team.LifecycleState, reason string) error
	blockTask            func(ctx context.Context, taskID, on, reason string) error
	listInbox            func(ctx context.Context, filter string) (team.InboxPayload, error)
	transitions          []team.LifecycleState
	transitionRecordedID string
}

func (f *fakeBrokerClient) StartIntake(ctx context.Context, intent string) (*team.IntakeOutcome, error) {
	if f.startIntake != nil {
		return f.startIntake(ctx, intent)
	}
	return nil, errors.New("startIntake not stubbed")
}

func (f *fakeBrokerClient) TransitionLifecycle(ctx context.Context, taskID string, to team.LifecycleState, reason string) error {
	f.transitionRecordedID = taskID
	f.transitions = append(f.transitions, to)
	if f.transitionLifecycle != nil {
		return f.transitionLifecycle(ctx, taskID, to, reason)
	}
	return nil
}

func (f *fakeBrokerClient) BlockTask(ctx context.Context, taskID, on, reason string) error {
	if f.blockTask != nil {
		return f.blockTask(ctx, taskID, on, reason)
	}
	return nil
}

func (f *fakeBrokerClient) ListInbox(ctx context.Context, filter string) (team.InboxPayload, error) {
	if f.listInbox != nil {
		return f.listInbox(ctx, filter)
	}
	return team.InboxPayload{}, nil
}

// TestRunTaskStartIntakeErrorBubblesUp confirms the runner surfaces
// brokerClient errors instead of silently succeeding.
func TestRunTaskStartIntakeErrorBubblesUp(t *testing.T) {
	client := &fakeBrokerClient{
		startIntake: func(_ context.Context, _ string) (*team.IntakeOutcome, error) {
			return nil, team.ErrIntakeNoProvider
		},
	}
	err := runTaskStartWithClient(context.Background(), client, "anything", "", strings.NewReader(""))
	if err == nil {
		t.Fatalf("expected error from missing provider, got nil")
	}
	if !strings.Contains(err.Error(), "no LLM provider") {
		t.Fatalf("expected no-provider error; got %q", err.Error())
	}
}

// TestRunTaskStartHappyPathWithClient drives the full happy path end-
// to-end: intake returns a valid outcome, user confirms with "y", and
// the runner posts intake → ready → running transitions in order.
func TestRunTaskStartHappyPathWithClient(t *testing.T) {
	client := &fakeBrokerClient{
		startIntake: func(_ context.Context, intent string) (*team.IntakeOutcome, error) {
			return &team.IntakeOutcome{
				TaskID: "task-7411",
				Spec: team.Spec{
					Problem:    "fix the cache",
					Assignment: "audit cache.go",
					AcceptanceCriteria: []team.ACItem{
						{Statement: "stale entries no longer return"},
					},
				},
			}, nil
		},
	}
	err := runTaskStartWithClient(context.Background(), client, "fix the cache", "", strings.NewReader("y\n"))
	if err != nil {
		t.Fatalf("happy path: %v", err)
	}
	if got, want := len(client.transitions), 2; got != want {
		t.Fatalf("expected 2 transitions, got %d (%v)", got, client.transitions)
	}
	if client.transitions[0] != team.LifecycleStateReady {
		t.Errorf("first transition: got %q, want %q", client.transitions[0], team.LifecycleStateReady)
	}
	if client.transitions[1] != team.LifecycleStateRunning {
		t.Errorf("second transition: got %q, want %q", client.transitions[1], team.LifecycleStateRunning)
	}
	if client.transitionRecordedID != "task-7411" {
		t.Errorf("transition target: got %q, want task-7411", client.transitionRecordedID)
	}
}

// TestRunTaskStartUserDeclinesLeavesInIntake covers the "n" branch:
// intake succeeds, user types "n", the runner does NOT post any
// transitions and reports the task is left in intake.
func TestRunTaskStartUserDeclinesLeavesInIntake(t *testing.T) {
	client := &fakeBrokerClient{
		startIntake: func(_ context.Context, _ string) (*team.IntakeOutcome, error) {
			return &team.IntakeOutcome{
				TaskID: "task-9999",
				Spec: team.Spec{
					Problem:            "irrelevant",
					Assignment:         "ignore",
					AcceptanceCriteria: []team.ACItem{{Statement: "no-op"}},
				},
			}, nil
		},
	}
	err := runTaskStartWithClient(context.Background(), client, "intent", "", strings.NewReader("n\n"))
	if err != nil {
		t.Fatalf("decline path returned error: %v", err)
	}
	if len(client.transitions) != 0 {
		t.Fatalf("expected no transitions on decline; got %v", client.transitions)
	}
}

// TestPromptYesNo locks down the consolidated y/n helper (F-FU-4).
func TestPromptYesNo(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"Y\n", true},
		{"yes\n", true},
		{"  y  \n", true},
		{"n\n", false},
		{"no\n", false},
		{"\n", false},
		{"maybe\n", false},
	}
	for _, tc := range cases {
		got, err := promptYesNo(strings.NewReader(tc.input), "")
		if err != nil {
			t.Errorf("promptYesNo(%q): unexpected error: %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("promptYesNo(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
