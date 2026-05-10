//go:build icp

// Package main hosts the Lane F CLI ICP acceptance tests.
//
// Run with: go test -tags icp ./cmd/wuphf/... -timeout 5m
//
// These tests exercise the three Sam tutorials documented in
// /tmp/wuphf-icp-tutorials.md (also tracked in the design doc) end-to-
// end against a real broker process started in-process, exposed over
// real HTTP, and exercised through the production httpBrokerClient.
//
// What this covers (and does not cover):
//
//   - The HTTP wire shape: every CLI subcommand goes through the same
//     newBrokerRequest helper as `wuphf log`, against a broker started
//     via b.StartOnPort(0). No in-process Broker pointer leaks into the
//     CLI execution path.
//   - The lifecycle transitions: intake → ready → running → review →
//     decision → merged. The convergence rule fires on grade arrival;
//     the timeout filler fires on sweep with the deadline elapsed.
//   - The block-and-unblock cascade: task A blocked on task B's PR
//     merge auto-transitions blocked_on_pr_merge → review when task B
//     merges.
//   - The intake parse path: the fake provider returns canned JSON
//     matching the Spec schema, the validator + parser run, and the
//     CLI's confirm prompt advances the task.
//
// What this does NOT cover (deferred to dogfood gate #12):
//
//   - Real-LLM intake against the live anthropic/ollama/openai chain.
//   - Browser-side Decision Packet rendering (Lane G's Vitest +
//     Playwright suite covers that path separately).
//   - Worktree-side owner-agent execution (the design's "Lane H"
//     headless runner is out of scope for v1).
//
// The build tag keeps these tests out of the default Go test suite so
// CI does not pay the broker-spinup cost on every run; they are
// deliberately invoked via `go test -tags icp` when validating the
// ICP gates.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/team"
)

// fakeICPIntakeProvider is the canned-JSON IntakeProvider the broker
// invokes inside b.StartIntake when the test seam is installed. It
// returns a Spec keyed off the intent string so each tutorial gets a
// realistic payload without standing up a real LLM.
type fakeICPIntakeProvider struct{}

func (fakeICPIntakeProvider) CallSpecLLM(_ context.Context, _, userPrompt string) (string, error) {
	intent := extractFakeIntent(userPrompt)
	switch {
	case strings.Contains(intent, "cache invalidation"):
		return fenceFakeICPSpec(map[string]any{
			"problem":       "The cache invalidation logic does not invalidate stale entries.",
			"targetOutcome": "Cache hit-rate drops to expected after invalidation events.",
			"acceptanceCriteria": []map[string]string{
				{"statement": "Stale entries no longer return on subsequent reads."},
				{"statement": "Existing cache tests pass."},
			},
			"assignment": "Audit the invalidation function in cache.go and fix the bug.",
		}), nil
	case strings.Contains(intent, "JWT validation"):
		return fenceFakeICPSpec(map[string]any{
			"problem":       "JWT validation accepts expired tokens with a stale clock skew window.",
			"targetOutcome": "Expired tokens are rejected within 30s of expiry.",
			"acceptanceCriteria": []map[string]string{
				{"statement": "Expired JWTs are rejected after the configured leeway."},
				{"statement": "JWT-related security tests pass under racy clock conditions."},
			},
			"assignment": "Audit jwt.go and tighten the clock-skew check.",
		}), nil
	case strings.Contains(intent, "auth header"):
		return fenceFakeICPSpec(map[string]any{
			"problem":       "Outbound API calls do not yet attach the new auth header.",
			"targetOutcome": "Every external request carries the new x-wuphf-auth header.",
			"acceptanceCriteria": []map[string]string{
				{"statement": "All outbound HTTP calls attach the new header."},
				{"statement": "Existing integration tests pass."},
			},
			"assignment": "Wire the new header through the outbound client middleware.",
		}), nil
	case strings.Contains(intent, "dependency upgrade"):
		return fenceFakeICPSpec(map[string]any{
			"problem":       "The dependency upgrade PR has been sitting open and needs landing.",
			"targetOutcome": "The upgrade ships green and unblocks downstream work.",
			"acceptanceCriteria": []map[string]string{
				{"statement": "PR #742 merges cleanly."},
				{"statement": "Lockfile resolves to the new versions."},
			},
			"assignment": "Land PR #742, resolving any merge conflicts.",
		}), nil
	}
	// Fallback: return a generic Spec so unrelated intents still parse.
	return fenceFakeICPSpec(map[string]any{
		"problem": "Unparsed test intent: " + intent,
		"acceptanceCriteria": []map[string]string{
			{"statement": "Address the intent."},
		},
		"assignment": "Address: " + intent,
	}), nil
}

func extractFakeIntent(userPrompt string) string {
	const marker = "---\n"
	first := strings.Index(userPrompt, marker)
	if first < 0 {
		return userPrompt
	}
	rest := userPrompt[first+len(marker):]
	end := strings.Index(rest, marker)
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

func fenceFakeICPSpec(spec map[string]any) string {
	body, _ := json.Marshal(spec)
	return "```json\n" + string(body) + "\n```"
}

// startICPBroker spins up a real team.Broker on an OS-assigned port,
// installs the fake intake provider, sets the brokerClientFactory so
// the CLI exercises its production HTTP path against the live
// listener, and returns a teardown closure.
func startICPBroker(t *testing.T) (*team.Broker, string) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "broker-state.json")
	b := team.NewBrokerAt(statePath)
	b.SetIntakeProviderFactory(func() team.IntakeProvider { return fakeICPIntakeProvider{} })

	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("StartOnPort: %v", err)
	}
	t.Cleanup(b.Stop)

	addr := b.Addr()
	if addr == "" {
		t.Fatalf("broker addr is empty")
	}
	baseURL := "http://" + addr
	token := b.Token()

	// Point the CLI's environment-derived base URL + token at this broker
	// so newBrokerRequest authenticates correctly. t.Setenv restores the
	// originals at end of test.
	t.Setenv("WUPHF_BROKER_BASE_URL", baseURL)
	t.Setenv("WUPHF_BROKER_TOKEN", token)

	// Guard rail: probe /tasks/inbox so we know the listener is healthy
	// before any tutorial step runs. Saves debugging time when a
	// runtime-home permission issue would otherwise surface as a flaky
	// transition error mid-tutorial.
	if err := waitForBrokerReady(baseURL, token, 5*time.Second); err != nil {
		t.Fatalf("broker not ready: %v", err)
	}
	return b, baseURL
}

func waitForBrokerReady(baseURL, token string, deadline time.Duration) error {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/tasks/inbox?filter=all", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("broker did not become ready in time")
}

// brokerHTTPPost is a tight helper for the test-only paths that need to
// punch through the broker over raw HTTP (lifecycle transitions for
// task B in tutorial 3, etc.). The CLI uses its own client; this is
// for the agent-side test choreography.
func brokerHTTPPost(t *testing.T, baseURL, path, token string, body any) (*http.Response, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, respBody
}

// newProductionLikeClient returns the same httpBrokerClient the
// production CLI uses, configured with a short test timeout to keep
// latent failures snappy.
func newProductionLikeClient() brokerClient {
	return &httpBrokerClient{httpClient: &http.Client{Timeout: 10 * time.Second}}
}

// TestICP_Tutorial1_SingleTaskHappyPath drives Sam's first scenario
// from /tmp/wuphf-icp-tutorials.md against a live broker:
//
//  1. wuphf task start "fix the broken cache invalidation"
//  2. confirm with "y"
//  3. simulate a reviewer grade (1 minor finding, 0 critical)
//  4. assert the task converges to LifecycleStateDecision.
func TestICP_Tutorial1_SingleTaskHappyPath(t *testing.T) {
	b, baseURL := startICPBroker(t)
	token := b.Token()
	client := newProductionLikeClient()

	// Step 1+2: task start over the live HTTP wire, "y" piped via stdin.
	err := runTaskStartWithClient(context.Background(), client,
		"fix the broken cache invalidation", "", strings.NewReader("y\n"))
	if err != nil {
		t.Fatalf("task start: %v", err)
	}

	// Find the task in the inbox over the live HTTP wire.
	payload, err := client.ListInbox(context.Background(), "all")
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(payload.Rows) != 1 {
		t.Fatalf("expected 1 task in inbox after intake; got %d (%+v)", len(payload.Rows), payload.Rows)
	}
	taskID := payload.Rows[0].TaskID
	if !strings.HasPrefix(taskID, "task-") {
		t.Fatalf("unexpected task id: %q", taskID)
	}
	if got := payload.Rows[0].LifecycleState; got != team.LifecycleStateRunning {
		t.Fatalf("post-confirm state: got %q, want %q", got, team.LifecycleStateRunning)
	}

	// Step 3: simulate the owner agent finishing → reviewer-routing
	// fires automatically via the broker's internal layer once we
	// transition to review. We do this via the live HTTP transition
	// endpoint to mirror the design contract.
	resp, body := brokerHTTPPost(t, baseURL, "/tasks/"+taskID+"/transition", token, map[string]string{
		"to":     string(team.LifecycleStateReview),
		"reason": "owner agent committed session report",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("transition running → review: %d %s", resp.StatusCode, body)
	}

	// Append a grade directly via the broker API (Lane D's owner-side
	// AppendReviewerGrade is callable for tests; in production this
	// would arrive via the headless runner). Use the in-process
	// b.AppendReviewerGrade because the on-the-wire grade-submission
	// endpoint is reserved for Lane G's tunnel-human flow.
	if err := b.AppendReviewerGrade(taskID, team.ReviewerGrade{
		ReviewerSlug: "reviewer",
		Severity:     team.SeverityMinor,
		Reasoning:    "small style nit",
	}); err != nil {
		t.Fatalf("append grade: %v", err)
	}

	// Step 4: assert convergence to LifecycleStateDecision. The grade
	// arrival path runs evaluateConvergenceLocked synchronously under
	// b.mu, so the next inbox fetch must observe the decision state.
	payload2, err := client.ListInbox(context.Background(), "needs_decision")
	if err != nil {
		t.Fatalf("list inbox post-grade: %v", err)
	}
	if len(payload2.Rows) != 1 || payload2.Rows[0].TaskID != taskID {
		t.Fatalf("expected task %q in needs_decision filter; got %+v", taskID, payload2.Rows)
	}
	if got := payload2.Rows[0].LifecycleState; got != team.LifecycleStateDecision {
		t.Fatalf("post-grade lifecycle state: got %q, want %q", got, team.LifecycleStateDecision)
	}
}

// TestICP_Tutorial2_ReviewerTimeoutPath exercises the reviewer-process-
// exit / timeout convergence path. Three reviewers are assigned; two
// grade; the third never does. After the deadline elapses, the
// sweeper fills the missing slot with SeveritySkipped and the task
// transitions to decision.
func TestICP_Tutorial2_ReviewerTimeoutPath(t *testing.T) {
	b, baseURL := startICPBroker(t)
	token := b.Token()
	client := newProductionLikeClient()

	// Fast-forward the broker's reviewer clock so the timeout fires
	// without us needing to wait the full real-world duration. The
	// reviewerNow override is the same seam Lane D's unit tests use.
	clk := newTestClock(time.Now().UTC())
	b.SetReviewerNowFn(clk.Now)

	if err := runTaskStartWithClient(context.Background(), client,
		"tighten the JWT validation", "", strings.NewReader("y\n")); err != nil {
		t.Fatalf("task start: %v", err)
	}

	payload, err := client.ListInbox(context.Background(), "all")
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(payload.Rows) != 1 {
		t.Fatalf("expected exactly 1 task; got %d", len(payload.Rows))
	}
	taskID := payload.Rows[0].TaskID

	resp, body := brokerHTTPPost(t, baseURL, "/tasks/"+taskID+"/transition", token, map[string]string{
		"to":     string(team.LifecycleStateReview),
		"reason": "owner agent committed session report",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("transition running → review: %d %s", resp.StatusCode, body)
	}

	// Pin a known three-reviewer roster AFTER the transition so the
	// roster stamping wins over the routing layer's derivation (which
	// runs synchronously inside transitionLifecycleLocked when
	// task.Reviewers is empty). The helper also re-stamps
	// ReviewStartedAt so the deadline anchors at clk.Now() rather
	// than wall time.
	b.SetTaskReviewersForTest(taskID, []string{"reviewer", "security", "perf"})

	// Two reviewers grade; the third (security) goes silent.
	for _, slug := range []string{"reviewer", "perf"} {
		if err := b.AppendReviewerGrade(taskID, team.ReviewerGrade{
			ReviewerSlug: slug,
			Severity:     team.SeverityNitpick,
			Reasoning:    "lgtm",
		}); err != nil {
			t.Fatalf("append grade %s: %v", slug, err)
		}
	}

	// Advance the clock past the deadline and sweep.
	clk.Advance(15 * time.Minute)
	b.SweepReviewConvergence()

	// Decision state must be reached, and the missing slot must be
	// filled with SeveritySkipped.
	packet, err := b.GetDecisionPacket(taskID)
	if err != nil {
		t.Fatalf("get decision packet: %v", err)
	}
	if packet.LifecycleState != team.LifecycleStateDecision {
		t.Fatalf("packet lifecycle state: got %q, want %q", packet.LifecycleState, team.LifecycleStateDecision)
	}
	var sawSkipped bool
	for _, g := range packet.ReviewerGrades {
		if g.ReviewerSlug == "security" && g.Severity == team.SeveritySkipped {
			sawSkipped = true
			break
		}
	}
	if !sawSkipped {
		t.Fatalf("expected security reviewer slot filled with skipped; got grades %+v", packet.ReviewerGrades)
	}
}

// TestICP_Tutorial3_BlockAndUnblockCascade exercises the block-and-
// unblock cascade. Task A is blocked on task B; when B merges, A
// auto-transitions blocked_on_pr_merge → review.
//
// The CLI / HTTP block path now records the blocker reference into
// task.BlockedOn directly (BlockTask signature carries the blockerID
// since the v1 BlockedOn fix). unblockDependentsLocked sweeps that
// list, so the cascade fires end-to-end without any test-only helper.
func TestICP_Tutorial3_BlockAndUnblockCascade(t *testing.T) {
	b, _ := startICPBroker(t)
	client := newProductionLikeClient()

	// Create task A.
	if err := runTaskStartWithClient(context.Background(), client,
		"wire the new auth header", "", strings.NewReader("y\n")); err != nil {
		t.Fatalf("task A start: %v", err)
	}
	// Create task B.
	if err := runTaskStartWithClient(context.Background(), client,
		"ship the dependency upgrade PR", "", strings.NewReader("y\n")); err != nil {
		t.Fatalf("task B start: %v", err)
	}

	payload, err := client.ListInbox(context.Background(), "all")
	if err != nil {
		t.Fatalf("list inbox: %v", err)
	}
	if len(payload.Rows) != 2 {
		t.Fatalf("expected 2 tasks; got %d", len(payload.Rows))
	}

	var taskA, taskB string
	for _, row := range payload.Rows {
		if strings.Contains(row.Title, "auth header") {
			taskA = row.TaskID
		}
		if strings.Contains(row.Title, "dependency upgrade") {
			taskB = row.TaskID
		}
	}
	if taskA == "" || taskB == "" {
		t.Fatalf("could not identify tasks A and B from inbox %+v", payload.Rows)
	}

	// Block A on B. The HTTP /tasks/{id}/block route reads the {on}
	// field and forwards it to BlockTask, which appends the blocker
	// reference to task.BlockedOn under b.mu. No test-only helper
	// needed; the cascade picks it up from the typed list.
	if err := client.BlockTask(context.Background(), taskA, taskB, "A depends on B's merge"); err != nil {
		t.Fatalf("block A on B: %v", err)
	}
	if got := blockedOnForTask(b, taskA); !containsTaskID(got, taskB) {
		t.Fatalf("expected task A.BlockedOn to contain %q after CLI block, got %v", taskB, got)
	}

	// Verify A is blocked.
	blockedPayload, err := client.ListInbox(context.Background(), "blocked")
	if err != nil {
		t.Fatalf("list blocked: %v", err)
	}
	var sawA bool
	for _, row := range blockedPayload.Rows {
		if row.TaskID == taskA {
			sawA = true
			break
		}
	}
	if !sawA {
		t.Fatalf("expected task A in blocked filter; got %+v", blockedPayload.Rows)
	}

	// Drive B through to merged via the same path the human merge
	// action takes. RecordTaskDecision("merge") transitions decision
	// → merged AND fires OnDecisionRecorded, which is the load-
	// bearing hook for the unblock cascade. The two intermediate
	// transitions (running → review, review → decision) are issued
	// directly because no human is in the loop yet.
	for _, target := range []team.LifecycleState{
		team.LifecycleStateReview,
		team.LifecycleStateDecision,
	} {
		if err := b.TransitionLifecycle(taskB, target, "tutorial 3: drive B to "+string(target)); err != nil {
			t.Fatalf("transition B to %s: %v", target, err)
		}
	}
	if err := b.RecordTaskDecision(taskB, "merge"); err != nil {
		t.Fatalf("RecordTaskDecision merge B: %v", err)
	}

	// A must auto-unblock (blocked_on_pr_merge → review).
	finalPayload, err := client.ListInbox(context.Background(), "all")
	if err != nil {
		t.Fatalf("list inbox post-merge: %v", err)
	}
	var stateA team.LifecycleState
	for _, row := range finalPayload.Rows {
		if row.TaskID == taskA {
			stateA = row.LifecycleState
		}
	}
	switch stateA {
	case team.LifecycleStateReview, team.LifecycleStateDecision, team.LifecycleStateMerged:
		// any of these is acceptable — A unblocked. Tutorial 3 specifies
		// review as the immediate target, but if the test broker has
		// reviewer routing wired in such a way that convergence fires
		// instantly (no reviewers assigned, etc.) the cascade may
		// continue further. The load-bearing assertion is that A is
		// no longer in blocked_on_pr_merge.
	default:
		t.Fatalf("task A did not auto-unblock; final state = %q", stateA)
	}
}

// testClock is a minimal monotonic-ish clock for the timeout test.
type testClock struct {
	now time.Time
}

func newTestClock(t time.Time) *testClock { return &testClock{now: t} }

func (c *testClock) Now() time.Time {
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

// String makes the test output a bit friendlier when a deadline assertion
// trips and the test prints the clock value.
func (c *testClock) String() string {
	return fmt.Sprintf("testClock(%s)", c.now.Format(time.RFC3339Nano))
}

// Compile-time guard: the production CLI must satisfy brokerClient. If
// the interface drifts, this assertion fails before any ICP test runs.
var _ brokerClient = (*httpBrokerClient)(nil)

// blockedOnForTask returns the task.BlockedOn snapshot for the given
// task id by walking the live broker state. Used by Tutorial 3 to
// assert the CLI / HTTP block path populated the typed BlockedOn list
// (rather than relying on a test-only helper).
func blockedOnForTask(b *team.Broker, taskID string) []string {
	for _, task := range b.AllTasks() {
		if task.ID == taskID {
			out := make([]string, len(task.BlockedOn))
			copy(out, task.BlockedOn)
			return out
		}
	}
	return nil
}

func containsTaskID(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
