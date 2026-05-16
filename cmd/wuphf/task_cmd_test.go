package main

import (
	"bytes"
	"context"
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
		Counts:      team.InboxCounts{DecisionRequired: 1, Running: 2, Blocked: 0, ApprovedToday: 0},
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
	if !strings.Contains(out, "Approved today: 0") {
		t.Fatalf("expected approved-today footer label/count; got:\n%s", out)
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
	timer := time.AfterFunc(10*time.Millisecond, c.Cancel)
	defer timer.Stop()
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

// TestRunTaskStartHookErrorBubblesUp confirms the runner surfaces hook
// errors instead of silently succeeding.
func TestRunTaskStartHookErrorBubblesUp(t *testing.T) {
	prev := taskStartHook
	taskStartHook = func(_ context.Context) (*team.Broker, team.IntakeProvider, error) {
		return nil, nil, team.ErrIntakeNoProvider
	}
	defer func() { taskStartHook = prev }()

	err := runTaskStartInProcess(context.Background(), "anything", "")
	if err == nil {
		t.Fatalf("expected error from missing provider, got nil")
	}
}
