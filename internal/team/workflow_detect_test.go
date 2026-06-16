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
	//  - a 2-task pattern (below MinRepeats)
	for i := 100; i <= 101; i++ {
		pushHeadlessEvent(stream, HeadlessEvent{
			Type: HeadlessEventTypeManifest, TaskID: fmt.Sprintf("OFFICE-%d", i), Agent: "revops",
			ToolCalls: []HeadlessManifestEntry{{ToolName: "calendar_check", Count: 1}, {ToolName: "note_write", Count: 1}},
		})
	}
	//  - a single-tool task (below MinSteps)
	pushHeadlessEvent(stream, HeadlessEvent{
		Type: HeadlessEventTypeManifest, TaskID: "OFFICE-200", Agent: "revops",
		ToolCalls: []HeadlessManifestEntry{{ToolName: "echo", Count: 1}},
	})
	//  - the referral shape by a different agent, only once (per-agent clustering)
	pushHeadlessEvent(stream, referralManifest("OFFICE-300", "pam"))

	sink := EventSinkPath()
	if sink == "" {
		t.Fatal("EventSinkPath empty; WUPHF_RUNTIME_HOME not honored")
	}

	got, err := DetectWorkflowsFromSink(sink, DetectOptions{})
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly 1 spotted workflow, got %d: %+v", len(got), got)
	}
	c := got[0]
	if c.Count != 14 {
		t.Fatalf("want count 14, got %d", c.Count)
	}
	if c.Agent != "revops" {
		t.Fatalf("want agent revops, got %q", c.Agent)
	}
	wantShape := "crm_lookup>owner_resolve>slack_draft>slack_send>referral_track"
	if c.Fingerprint != wantShape {
		t.Fatalf("want shape %q, got %q", wantShape, c.Fingerprint)
	}

	t.Logf("SPOTTED: %s repeats [%s] %d times (tasks %s..%s)",
		c.Agent, strings.Join(c.Shape, " -> "), c.Count, c.TaskIDs[0], c.TaskIDs[len(c.TaskIDs)-1])
}

func TestDetectMinRepeats(t *testing.T) {
	var ms []TurnManifest
	for i := 1; i <= 2; i++ {
		ms = append(ms, manifestFor(fmt.Sprintf("T%d", i), "a", "x", "y"))
	}
	if got := DetectWorkflows(ms, DetectOptions{}); len(got) != 0 {
		t.Fatalf("2 repeats should not surface (MinRepeats 3): %+v", got)
	}
	ms = append(ms, manifestFor("T3", "a", "x", "y"))
	if got := DetectWorkflows(ms, DetectOptions{}); len(got) != 1 || got[0].Count != 3 {
		t.Fatalf("3 repeats should surface once with count 3: %+v", got)
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

// manifestFor is a tiny builder for unit tests.
func manifestFor(taskID, agent string, tools ...string) TurnManifest {
	tc := make([]TurnToolCount, 0, len(tools))
	for _, name := range tools {
		tc = append(tc, TurnToolCount{Name: name, Count: 1})
	}
	return TurnManifest{TaskID: taskID, Agent: agent, Tools: tc}
}
