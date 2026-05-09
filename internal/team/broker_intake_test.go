package team

// broker_intake_test.go covers Lane B build-time gate #5 of the
// multi-agent control loop success criteria: the four parse-failure / parse-
// success paths plus an integration smoke test that asserts the intake →
// ready transition fires and the spec.created event lands on the agent
// stream.
//
// Tests inject a fake IntakeProvider so no real LLM is called. Each test
// owns its own fixture broker via newTestBroker; nothing crosses test
// boundaries.

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeIntakeProvider is a deterministic IntakeProvider for the unit tests.
// Each call returns response, err in order; CallCount is observable for
// concurrency tests.
type fakeIntakeProvider struct {
	mu        sync.Mutex
	response  string
	err       error
	calls     int
	lastSys   string
	lastUser  string
	delay     time.Duration
	releaseCh chan struct{}
}

func (f *fakeIntakeProvider) CallSpecLLM(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	f.mu.Lock()
	f.calls++
	f.lastSys = systemPrompt
	f.lastUser = userPrompt
	delay := f.delay
	releaseCh := f.releaseCh
	resp := f.response
	err := f.err
	f.mu.Unlock()

	if releaseCh != nil {
		select {
		case <-releaseCh:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return resp, err
}

func (f *fakeIntakeProvider) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fenceJSON wraps a JSON string in the ```json fenced block format the
// system prompt asks the LLM to emit.
func fenceJSON(body string) string {
	return "```json\n" + body + "\n```"
}

// TestIntakeParse_MalformedJSON covers gate #5 path 1: malformed JSON
// surfaces the raw error and persists no spec.
func TestIntakeParse_MalformedJSON(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	provider := &fakeIntakeProvider{
		response: "```json\n{\"problem\": \"unbalanced\nbroken: yes\n```",
	}

	_, err := b.StartIntake(context.Background(), "do a thing", provider)
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decode spec json") {
		t.Fatalf("error should surface decode failure, got %q", err.Error())
	}

	// No spec persisted: assert the in-memory map is empty.
	for _, task := range b.allTasksSnapshot() {
		if _, ok := b.IntakeSpec(task.ID); ok {
			t.Fatalf("no spec should be persisted on parse failure, got one for task %q", task.ID)
		}
	}
	// No state transition fired: the index has nothing in LifecycleStateReady.
	idx := b.LifecycleIndexSnapshot()
	if len(idx[LifecycleStateReady]) != 0 {
		t.Fatalf("no task should be in ready state on parse failure, got %v", idx[LifecycleStateReady])
	}
}

// TestIntakeValidate_MissingRequiredFields covers gate #5 path 2: each of
// the three required-field cases (Problem, AC, Assignment) rejects the
// spec with a field-by-field reason.
func TestIntakeValidate_MissingRequiredFields(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		body   string
		expect string
	}{
		{
			name: "empty problem",
			body: `{
				"problem": "",
				"acceptance_criteria": [{"statement": "ship it"}],
				"assignment": "go"
			}`,
			expect: "problem is empty",
		},
		{
			name: "no acceptance criteria",
			body: `{
				"problem": "we need a thing",
				"acceptance_criteria": [],
				"assignment": "go"
			}`,
			expect: "acceptance_criteria has 0 entries",
		},
		{
			name: "empty assignment",
			body: `{
				"problem": "we need a thing",
				"acceptance_criteria": [{"statement": "ship it"}],
				"assignment": ""
			}`,
			expect: "assignment is empty",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newTestBroker(t)
			provider := &fakeIntakeProvider{response: fenceJSON(tc.body)}

			_, err := b.StartIntake(context.Background(), "intent", provider)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !strings.Contains(err.Error(), tc.expect) {
				t.Fatalf("error should mention %q, got %q", tc.expect, err.Error())
			}
			// No transition: the index has nothing in LifecycleStateReady.
			idx := b.LifecycleIndexSnapshot()
			if len(idx[LifecycleStateReady]) != 0 {
				t.Fatalf("no task should be in ready state on validation failure, got %v", idx[LifecycleStateReady])
			}
		})
	}
}

// TestIntakeParse_ExtraUnknownFields covers gate #5 path 3: unknown JSON
// keys are silently ignored (encoding/json's default), valid fields parse
// cleanly, and the spec passes validation.
func TestIntakeParse_ExtraUnknownFields(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	body := `{
		"problem": "we ship feature X",
		"target_outcome": "users can do Y",
		"acceptance_criteria": [
			{"statement": "tests pass"},
			{"statement": "docs updated"}
		],
		"assignment": "owner-agent picks up",
		"constraints": ["must be additive"],
		"random_key": "junk that should be ignored",
		"deeper": {"nested": "noise"},
		"another_unknown": 42
	}`
	provider := &fakeIntakeProvider{response: fenceJSON(body)}

	outcome, err := b.StartIntake(context.Background(), "ship feature X", provider)
	if err != nil {
		t.Fatalf("expected clean spec to pass, got error: %v", err)
	}
	if outcome.TaskID == "" {
		t.Fatal("expected non-empty task ID on success")
	}
	if outcome.Spec.Problem != "we ship feature X" {
		t.Fatalf("Problem: got %q, want %q", outcome.Spec.Problem, "we ship feature X")
	}
	if got, want := len(outcome.Spec.AcceptanceCriteria), 2; got != want {
		t.Fatalf("len(AcceptanceCriteria): got %d, want %d", got, want)
	}
	if outcome.Spec.Assignment != "owner-agent picks up" {
		t.Fatalf("Assignment: got %q", outcome.Spec.Assignment)
	}
	// Auto-assign empty → no countdown.
	if outcome.Countdown != nil {
		t.Fatal("expected nil Countdown when AutoAssign is empty")
	}
	// Lifecycle index advanced to ready.
	idx := b.LifecycleIndexSnapshot()
	if len(idx[LifecycleStateReady]) != 1 {
		t.Fatalf("expected 1 task in ready state, got %v", idx[LifecycleStateReady])
	}
	if idx[LifecycleStateReady][0] != outcome.TaskID {
		t.Fatalf("ready task ID: got %q, want %q", idx[LifecycleStateReady][0], outcome.TaskID)
	}
}

// TestIntake_AutoAssignCountdownInterrupted covers gate #5 path 4: when
// Spec.AutoAssign is non-empty, the driver returns a Countdown handle.
// Cancelling it before it elapses must return false (interrupted) so the
// CLI knows to fall back to manual y/n confirm.
func TestIntake_AutoAssignCountdownInterrupted(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	body := `{
		"problem": "ship the thing",
		"acceptance_criteria": [{"statement": "tests green"}],
		"assignment": "go",
		"auto_assign": "owner-eng"
	}`
	provider := &fakeIntakeProvider{response: fenceJSON(body)}

	outcome, err := b.StartIntake(context.Background(), "ship", provider)
	if err != nil {
		t.Fatalf("expected clean spec, got %v", err)
	}
	if outcome.AutoAssign != "owner-eng" {
		t.Fatalf("AutoAssign: got %q, want %q", outcome.AutoAssign, "owner-eng")
	}
	if outcome.Countdown == nil {
		t.Fatal("expected non-nil Countdown when AutoAssign is set")
	}

	// Use a longer countdown duration so the test has headroom to fire
	// the keypress before the timer elapses.
	cd := newAutoAssignCountdownWithDuration(2 * time.Second)

	// Mock the keypress: cancel immediately on a goroutine.
	go func() {
		time.Sleep(20 * time.Millisecond)
		cd.Cancel()
	}()

	start := time.Now()
	completed := cd.Wait(context.Background())
	elapsed := time.Since(start)

	if completed {
		t.Fatal("Wait should return false when Cancel fires before timer elapses")
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("Wait should return promptly after Cancel; elapsed=%v", elapsed)
	}

	// Cancel is idempotent: a second call must not panic or deadlock.
	cd.Cancel()
	cd.Cancel()
}

// TestIntake_AutoAssignCountdownElapsesCleanly is the symmetric path: no
// keypress, the countdown elapses, Wait returns true. Lane F treats this
// as "auto-confirm" and advances the task itself.
func TestIntake_AutoAssignCountdownElapsesCleanly(t *testing.T) {
	t.Parallel()
	cd := newAutoAssignCountdownWithDuration(20 * time.Millisecond)
	completed := cd.Wait(context.Background())
	if !completed {
		t.Fatal("Wait should return true when countdown elapses without cancellation")
	}
}

// TestIntake_AutoAssignCountdownHonorsContext asserts that a parent
// context cancellation also interrupts Wait. CLI uses this to abort the
// countdown when the user CTRL-C's during the prompt.
func TestIntake_AutoAssignCountdownHonorsContext(t *testing.T) {
	t.Parallel()
	cd := newAutoAssignCountdownWithDuration(2 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	completed := cd.Wait(ctx)
	if completed {
		t.Fatal("Wait should return false when context cancels before timer elapses")
	}
}

// TestIntakeHappyPath is the integration smoke test: feed a synthetic
// LLM response with a valid JSON-fenced spec, assert task transitions
// intake → ready, the manifest spec.created event is emitted on the
// intake agent's stream, and the persisted spec is retrievable.
func TestIntakeHappyPath(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	body := `{
		"problem": "the inbox can't show what's blocked",
		"target_outcome": "blocked tasks visible at a glance",
		"acceptance_criteria": [
			{"statement": "blocked tasks render with a banner"},
			{"statement": "filter chip 'blocked' shows count"}
		],
		"assignment": "frontend agent picks up the inbox view",
		"constraints": ["must work at 1000 tasks", "no new dependencies"]
	}`
	provider := &fakeIntakeProvider{response: fenceJSON(body)}

	// Subscribe to the intake agent's stream BEFORE StartIntake so we see
	// the spec.created manifest event.
	stream := b.AgentStream(intakeAgentSlug)
	if stream == nil {
		t.Fatal("expected non-nil intake agent stream buffer")
	}
	// Give the subscription buffer headroom: events drop the channel send
	// when the buffer is full.
	recent, ch, unsubscribe := stream.subscribeTaskWithRecent("")
	defer unsubscribe()
	_ = recent

	outcome, err := b.StartIntake(context.Background(), "make inbox legible", provider)
	if err != nil {
		t.Fatalf("StartIntake: %v", err)
	}
	if outcome.TaskID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if outcome.Spec.Problem != "the inbox can't show what's blocked" {
		t.Fatalf("Problem: got %q", outcome.Spec.Problem)
	}

	// Lifecycle: task is in ready, not intake.
	idx := b.LifecycleIndexSnapshot()
	if len(idx[LifecycleStateReady]) != 1 || idx[LifecycleStateReady][0] != outcome.TaskID {
		t.Fatalf("expected exactly 1 ready task with id %q, got %v", outcome.TaskID, idx)
	}
	if len(idx[LifecycleStateIntake]) != 0 {
		t.Fatalf("expected no tasks remaining in intake, got %v", idx[LifecycleStateIntake])
	}

	// Persisted spec round-trips.
	persisted, ok := b.IntakeSpec(outcome.TaskID)
	if !ok {
		t.Fatal("expected persisted spec for task ID")
	}
	if persisted.Problem != outcome.Spec.Problem {
		t.Fatalf("persisted spec drift: got %q, want %q", persisted.Problem, outcome.Spec.Problem)
	}
	if got, want := len(persisted.AcceptanceCriteria), 2; got != want {
		t.Fatalf("persisted AC count: got %d, want %d", got, want)
	}

	// Provider was called once with the hardcoded system prompt + a
	// user-turn-wrapped intent.
	if got, want := provider.Calls(), 1; got != want {
		t.Fatalf("provider Calls: got %d, want %d", got, want)
	}
	if !strings.Contains(provider.lastSys, "WUPHF intake agent") {
		t.Fatal("system prompt should be the hardcoded intake prompt")
	}
	if !strings.Contains(provider.lastUser, "make inbox legible") {
		t.Fatal("user prompt should wrap the intent")
	}

	// spec.created event landed on the stream. Drain up to 200ms; the
	// emit happens synchronously inside StartIntake, but the subscription
	// channel is buffered so we still need a small wait.
	deadline := time.After(200 * time.Millisecond)
	var seenSpecCreated bool
collect:
	for {
		select {
		case line := <-ch:
			if strings.Contains(line, "spec.created") && strings.Contains(line, "\"task_id\":\""+outcome.TaskID+"\"") {
				seenSpecCreated = true
				break collect
			}
		case <-deadline:
			break collect
		}
	}
	if !seenSpecCreated {
		// Fall back to recent buffer in case the subscription registered
		// after the emit.
		for _, line := range stream.recent() {
			if strings.Contains(line, "spec.created") && strings.Contains(line, outcome.TaskID) {
				seenSpecCreated = true
				break
			}
		}
	}
	if !seenSpecCreated {
		t.Fatal("expected spec.created manifest event on the intake agent's stream")
	}
}

// TestIntake_ProviderErrorSurfacesAndCleansUp asserts the cleanup path:
// when the provider call fails, no placeholder task is left behind in the
// inbox, and no spec is persisted.
func TestIntake_ProviderErrorSurfacesAndCleansUp(t *testing.T) {
	t.Parallel()
	b := newTestBroker(t)
	provider := &fakeIntakeProvider{err: errors.New("fake provider down")}

	_, err := b.StartIntake(context.Background(), "intent", provider)
	if err == nil {
		t.Fatal("expected provider error to surface")
	}
	if !strings.Contains(err.Error(), "fake provider down") {
		t.Fatalf("error should wrap provider error, got %q", err.Error())
	}

	// No leftover task in any lifecycle bucket.
	idx := b.LifecycleIndexSnapshot()
	for state, ids := range idx {
		if len(ids) != 0 {
			t.Fatalf("expected empty index after provider failure, found %d in %s: %v", len(ids), state, ids)
		}
	}
}

// TestIntake_NilGuards verifies the StartIntake guards on nil receiver
// and nil provider — both must return errors instead of panicking.
func TestIntake_NilGuards(t *testing.T) {
	t.Parallel()
	var nilB *Broker
	if _, err := nilB.StartIntake(context.Background(), "x", &fakeIntakeProvider{}); err == nil {
		t.Fatal("nil broker should error")
	}
	b := newTestBroker(t)
	if _, err := b.StartIntake(context.Background(), "x", nil); err == nil {
		t.Fatal("nil provider should error")
	}
}

// TestExtractFencedJSON tests the fence extractor in isolation against
// the half-dozen shapes a real LLM emits. Pure unit test against the
// parser helper.
func TestExtractFencedJSON(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "fenced with json tag",
			raw:  "```json\n{\"a\":1}\n```",
			want: `{"a":1}`,
		},
		{
			name: "fenced without language tag",
			raw:  "```\n{\"a\":1}\n```",
			want: `{"a":1}`,
		},
		{
			name: "leading prose plus fence",
			raw:  "Here is the spec:\n```json\n{\"a\":1}\n```\nHope that helps!",
			want: `{"a":1}`,
		},
		{
			name: "no fence — bare object",
			raw:  `{"a":1}`,
			want: `{"a":1}`,
		},
		{
			name: "no fence — wrapped in prose",
			raw:  `Sure! {"a":1} done.`,
			want: `{"a":1}`,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractFencedJSON(tc.raw)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestIntakeSpec_RoundTripsJSON is a quick sanity check that the Spec
// type's JSON tags match the schema described in the system prompt.
func TestIntakeSpec_RoundTripsJSON(t *testing.T) {
	t.Parallel()
	in := Spec{
		Problem:       "p",
		TargetOutcome: "t",
		AcceptanceCriteria: []ACItem{
			{Statement: "ac1"},
		},
		Assignment:  "go",
		Constraints: []string{"c"},
		AutoAssign:  "agent-a",
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wireKeys := []string{"problem", "target_outcome", "acceptance_criteria", "assignment", "constraints", "auto_assign"}
	for _, k := range wireKeys {
		if !strings.Contains(string(raw), `"`+k+`"`) {
			t.Fatalf("expected wire key %q in JSON: %s", k, raw)
		}
	}
	var out Spec
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Problem != in.Problem || out.AutoAssign != in.AutoAssign {
		t.Fatalf("round-trip drift: %+v vs %+v", in, out)
	}
}

// allTasksSnapshot is a tiny test-only helper on Broker to avoid reaching
// into b.tasks under lock from the test file. Returns a copy.
func (b *Broker) allTasksSnapshot() []teamTask {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]teamTask, len(b.tasks))
	copy(out, b.tasks)
	return out
}
