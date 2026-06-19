package team

import (
	"fmt"
	"strings"
	"testing"
)

// referralManifest builds a realistic per-turn manifest for the Brex referral
// outreach shape (5 steps) for one task.
func referralManifest(taskID, agent string) HeadlessEvent {
	return HeadlessEvent{
		Type: HeadlessEventTypeManifest, TaskID: taskID, Agent: agent, TurnID: "t1",
		ToolCalls: []HeadlessManifestEntry{
			{ToolName: "crm_lookup", Count: 1},
			{ToolName: "owner_resolve", Count: 1},
			{ToolName: "slack_draft", Count: 1},
			{ToolName: "slack_send", Count: 1},
			{ToolName: "referral_track", Count: 1},
		},
	}
}

// TestDetectionEndToEnd drives the REAL production path: realistic manifest
// events go through pushHeadlessEvent (which persists them), then the miner
// reads the on-disk sink and spots the repeated workflow. This proves the whole
// substrate -> persist -> detect pipeline, not just the helpers.
func TestDetectionEndToEnd(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}

	// 14 real referral runs by revops (the Brex anchor).
	for i := 1; i <= 14; i++ {
		pushHeadlessEvent(stream, referralManifest(fmt.Sprintf("OFFICE-%d", i), "revops"))
	}
	// Noise that must NOT surface:
	//  - a 2-task read-only pattern: no outcome step, below recurrenceFloor
	for i := 100; i <= 101; i++ {
		pushHeadlessEvent(stream, HeadlessEvent{
			Type: HeadlessEventTypeManifest, TaskID: fmt.Sprintf("OFFICE-%d", i), Agent: "revops",
			ToolCalls: []HeadlessManifestEntry{{ToolName: "calendar_check", Count: 1}, {ToolName: "slot_lookup", Count: 1}},
		})
	}
	//  - a single-tool task (below MinSteps)
	pushHeadlessEvent(stream, HeadlessEvent{
		Type: HeadlessEventTypeManifest, TaskID: "OFFICE-200", Agent: "revops",
		ToolCalls: []HeadlessManifestEntry{{ToolName: "echo", Count: 1}},
	})
	//  - the referral shape by a different agent, only once. It ends in an
	//    outcome (referral_track), so it SURFACES on the single end-to-end run.
	pushHeadlessEvent(stream, referralManifest("OFFICE-300", "pam"))

	sink := EventSinkPath()
	if sink == "" {
		t.Fatal("EventSinkPath empty; WUPHF_RUNTIME_HOME not honored")
	}

	got, err := DetectWorkflowsFromSink(sink, DetectOptions{})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	// Two candidates: revops' 14 repeats and pam's single end-to-end run. The
	// read-only 2-task pattern and the single-tool task stay noise.
	if len(got) != 2 {
		t.Fatalf("want 2 spotted workflows (revops x14, pam x1), got %d: %+v", len(got), got)
	}
	byAgent := map[string]DetectionCandidate{}
	for _, c := range got {
		byAgent[c.Agent] = c
	}
	wantShape := "crm_lookup>owner_resolve>slack_draft>slack_send>referral_track"
	rev, ok := byAgent["revops"]
	if !ok || rev.Count != 14 || rev.Fingerprint != wantShape {
		t.Fatalf("want revops count 14 shape %q, got %+v", wantShape, rev)
	}
	if rev.Outcome != "referral_track" {
		t.Fatalf("want revops outcome referral_track, got %q", rev.Outcome)
	}
	pam, ok := byAgent["pam"]
	if !ok || pam.Count != 1 || pam.Outcome != "referral_track" {
		t.Fatalf("want pam single end-to-end run with outcome referral_track, got %+v", pam)
	}

	t.Logf("SPOTTED: %s repeats [%s] %d times; %s ran it once end-to-end to %q",
		rev.Agent, strings.Join(rev.Shape, " -> "), rev.Count, pam.Agent, pam.Outcome)
}

// TestDetectRecurrenceFloor: a shape with no recognized outcome step still needs
// recurrenceFloor recurrences before it surfaces (so opaque read-only loops do
// not nag after a single run).
func TestDetectRecurrenceFloor(t *testing.T) {
	var ms []TurnManifest
	for i := 1; i <= 2; i++ {
		ms = append(ms, manifestFor(fmt.Sprintf("T%d", i), "a", "x", "y"))
	}
	if got := DetectWorkflows(ms, DetectOptions{}); len(got) != 0 {
		t.Fatalf("2 no-outcome repeats should not surface (floor 3): %+v", got)
	}
	ms = append(ms, manifestFor("T3", "a", "x", "y"))
	if got := DetectWorkflows(ms, DetectOptions{}); len(got) != 1 || got[0].Count != 3 {
		t.Fatalf("3 no-outcome repeats should surface once with count 3: %+v", got)
	}
}

// TestDetectSingleOutcome is the core of the "ditch the 3-repeat mandate"
// change: a single task that ran a multi-step shape to a final outcome (the
// digest was composed/sent) surfaces immediately, while a single read-only
// shape does not.
func TestDetectSingleOutcome(t *testing.T) {
	digest := []TurnManifest{
		manifestFor("DIGEST-1", "ceo", "gmail_fetch_emails", "summarize_threads", "compose_digest"),
	}
	got := DetectWorkflows(digest, DetectOptions{})
	if len(got) != 1 {
		t.Fatalf("a single end-to-end digest run should surface once: %+v", got)
	}
	if got[0].Count != 1 || got[0].Outcome != "compose_digest" {
		t.Fatalf("want count 1 outcome compose_digest, got %+v", got[0])
	}

	// A single read-only run (no outcome) must NOT surface.
	readOnly := []TurnManifest{
		manifestFor("LOOK-1", "ceo", "inbox_scan", "sender_lookup"),
	}
	if got := DetectWorkflows(readOnly, DetectOptions{}); len(got) != 0 {
		t.Fatalf("a single read-only run must not surface: %+v", got)
	}
}

func TestDetectMinSteps(t *testing.T) {
	var ms []TurnManifest
	for i := 1; i <= 5; i++ {
		ms = append(ms, manifestFor(fmt.Sprintf("T%d", i), "a", "only_one")) // 1 tool
	}
	if got := DetectWorkflows(ms, DetectOptions{}); len(got) != 0 {
		t.Fatalf("single-tool tasks are not workflows: %+v", got)
	}
}

func TestDetectPerAgentClustering(t *testing.T) {
	var ms []TurnManifest
	for i := 1; i <= 3; i++ {
		ms = append(ms, manifestFor(fmt.Sprintf("A%d", i), "alice", "x", "y"))
	}
	for i := 1; i <= 2; i++ { // bob runs the same shape only twice
		ms = append(ms, manifestFor(fmt.Sprintf("B%d", i), "bob", "x", "y"))
	}
	got := DetectWorkflows(ms, DetectOptions{})
	if len(got) != 1 || got[0].Agent != "alice" {
		t.Fatalf("only alice (>=3) should surface, per-agent: %+v", got)
	}
}

func TestDetectEmpty(t *testing.T) {
	if got := DetectWorkflows(nil, DetectOptions{}); len(got) != 0 {
		t.Fatalf("nil input -> no candidates, got %+v", got)
	}
}

// TestDetectFiltersOrchestration proves the miner drops the agent's own
// plumbing (Bash, ToolSearch, the office coordination MCP) so those turns do
// not masquerade as workflows, while a real domain shape still surfaces.
func TestDetectFiltersOrchestration(t *testing.T) {
	ms := []TurnManifest{
		// Pure plumbing: must NOT surface (every step is orchestration).
		manifestFor("PLUMB-1", "ceo",
			"Bash", "ToolSearch", "mcp__wuphf-office__team_task",
			"mcp__wuphf-office__team_broadcast", "CronCreate"),
		// Real domain work mixed with plumbing: the plumbing drops out, leaving
		// the clean gmail -> summarize -> compose shape ending in an outcome.
		manifestFor("DIGEST-1", "ceo",
			"ToolSearch", "gmail_fetch_emails", "Bash", "summarize_threads", "compose_digest"),
	}
	got := DetectWorkflows(ms, DetectOptions{})
	if len(got) != 1 {
		t.Fatalf("only the domain workflow should surface, got %d: %+v", len(got), got)
	}
	if got[0].Fingerprint != "gmail_fetch_emails>summarize_threads>compose_digest" {
		t.Fatalf("plumbing should be stripped from the shape, got %q", got[0].Fingerprint)
	}
	if got[0].Outcome != "compose_digest" {
		t.Fatalf("want outcome compose_digest, got %q", got[0].Outcome)
	}
}

// manifestFor is a tiny builder for unit tests.
func manifestFor(taskID, agent string, tools ...string) TurnManifest {
	tc := make([]TurnToolCount, 0, len(tools))
	for _, name := range tools {
		tc = append(tc, TurnToolCount{Name: name, Count: 1})
	}
	return TurnManifest{TaskID: taskID, Agent: agent, Tools: tc}
}

// ─── action-proxy unwrap (manifest capture fix) ────────────────────────────

// TestManifestToolToken pins the transform that makes integration work visible
// to the miner: the generic external-action proxy is unwrapped to its real
// action_id; everything else passes through untouched.
func TestManifestToolToken(t *testing.T) {
	cases := []struct {
		name, tool, input, want string
	}{
		{"proxy with action_id", "mcp__wuphf-office__team_action_execute",
			`{"platform":"gmail","action_id":"GMAIL_SEND_EMAIL"}`, "gmail_send_email"},
		{"bare proxy name", "team_action_execute",
			`{"action_id":"SLACK_SEND_MESSAGE"}`, "slack_send_message"},
		{"proxy platform-only fallback", "mcp__wuphf-office__team_action_execute",
			`{"platform":"hubspot"}`, "hubspot_action"},
		{"proxy unparseable input keeps raw name (stays filtered)",
			"mcp__wuphf-office__team_action_execute", "not json",
			"mcp__wuphf-office__team_action_execute"},
		{"non-proxy tool unchanged", "mcp__wuphf-office__team_action_search",
			`{"platform":"gmail"}`, "mcp__wuphf-office__team_action_search"},
		{"plain harness tool unchanged", "Bash", "", "Bash"},
	}
	for _, c := range cases {
		if got := manifestToolToken(c.tool, c.input); got != c.want {
			t.Errorf("%s: manifestToolToken(%q,%q) = %q, want %q", c.name, c.tool, c.input, got, c.want)
		}
	}
}

// emitDigestTurn replays one Gmail-digest turn the way the claude runner now
// records it: each tool_use is mapped through manifestToolToken, then the
// terminal manifest is emitted through the REAL persist path (emitHeadlessManifest
// -> pushHeadlessEvent -> sink). unwrap=false simulates the pre-fix behavior
// (raw proxy names) so the same task can be tested both ways.
func emitDigestTurn(stream *agentStreamBuffer, taskID string, unwrap bool) {
	raw := []struct{ name, input string }{
		{"mcp__wuphf-office__team_action_search", `{"platform":"gmail"}`},                                   // plumbing read
		{"mcp__wuphf-office__team_action_execute", `{"platform":"gmail","action_id":"GMAIL_FETCH_EMAILS"}`}, // read
		{"mcp__wuphf-office__team_action_execute", `{"platform":"gmail","action_id":"GMAIL_SEND_EMAIL"}`},   // outcome
	}
	tokens := make([]string, 0, len(raw))
	for _, r := range raw {
		if unwrap {
			tokens = append(tokens, manifestToolToken(r.name, r.input))
		} else {
			tokens = append(tokens, r.name)
		}
	}
	emitHeadlessManifest(stream, "t1", HeadlessProviderClaude, "ceo", taskID, "", tokens, 0, headlessProgressMetrics{}, nil)
}

// TestDetectionBlindToOpaqueProxy documents the bug the fix closes: when every
// integration action is recorded under the same opaque proxy name, a real
// Gmail-digest task collapses to pure office plumbing and the miner sees
// nothing.
func TestDetectionBlindToOpaqueProxy(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}
	emitDigestTurn(stream, "OFFICE-DIGEST-OPAQUE", false)

	cands, err := DetectWorkflowsFromSink(EventSinkPath(), DetectOptions{})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("opaque proxy must surface NO workflow, got %d: %+v", len(cands), cands)
	}
}

// TestDetectionUnwrapsActionProxy is the regression: the SAME Gmail-digest task,
// recorded the way the fixed runner records it, now surfaces a real candidate
// whose steps are the actual domain actions and whose outcome is the send.
func TestDetectionUnwrapsActionProxy(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	stream := &agentStreamBuffer{subs: make(map[int]agentStreamSubscriber)}
	emitDigestTurn(stream, "OFFICE-DIGEST-REAL", true)

	cands, err := DetectWorkflowsFromSink(EventSinkPath(), DetectOptions{})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("unwrapped proxy must surface exactly 1 workflow, got %d: %+v", len(cands), cands)
	}
	got := cands[0]
	if got.Outcome != "gmail_send_email" {
		t.Errorf("outcome = %q, want gmail_send_email", got.Outcome)
	}
	shape := strings.Join(got.Shape, ",")
	for _, step := range []string{"gmail_fetch_emails", "gmail_send_email"} {
		if !strings.Contains(shape, step) {
			t.Errorf("shape %q missing domain step %q", shape, step)
		}
	}
	if strings.Contains(shape, "team_action_execute") {
		t.Errorf("shape %q must not carry the opaque proxy name", shape)
	}
}
