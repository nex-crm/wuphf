package team

import (
	"os"
	"path/filepath"
	"testing"
)

// appendRawLine appends an arbitrary line to the sink so tests can inject a
// corrupt record between valid ones.
func appendRawLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(line + "\n")
	return err
}

// manifestFor builds a one-turn TurnManifest for a task with the given tools
// (each counted once) — enough to drive the shape miner in tests.
func manifestFor(taskID, agent string, tools ...string) TurnManifest {
	tc := make([]TurnToolCount, 0, len(tools))
	for _, name := range tools {
		tc = append(tc, TurnToolCount{Name: name, Count: 1})
	}
	return TurnManifest{TaskID: taskID, Agent: agent, Tools: tc}
}

// TestDetectWorkflowsRecurrenceFloor: a no-outcome read-mostly shape surfaces
// once it meets the configured floor, and not before. This is the core apps
// tuning (floor 2) vs the workflow default (floor 3).
func TestDetectWorkflowsRecurrenceFloor(t *testing.T) {
	twice := []TurnManifest{
		manifestFor("t1", "revops", "crm_fetch_leads", "score_leads"),
		manifestFor("t2", "revops", "crm_fetch_leads", "score_leads"),
	}

	// Floor 2 (apps): two runs of the same read-only shape surface.
	got := DetectWorkflows(twice, DetectOptions{RecurrenceFloor: 2})
	if len(got) != 1 {
		t.Fatalf("floor 2: want 1 candidate, got %d", len(got))
	}
	if got[0].Count != 2 || len(got[0].TaskIDs) != 2 {
		t.Fatalf("floor 2: want count 2 over 2 tasks, got count=%d tasks=%v", got[0].Count, got[0].TaskIDs)
	}
	if got[0].Outcome != "" {
		t.Fatalf("read-only shape should have no outcome, got %q", got[0].Outcome)
	}

	// Floor 3 (workflow default): the same two runs do NOT surface yet.
	if got := DetectWorkflows(twice, DetectOptions{RecurrenceFloor: 3}); len(got) != 0 {
		t.Fatalf("floor 3: want 0 candidates for a 2x read-only shape, got %d", len(got))
	}
}

// TestDetectWorkflowsSingleRunOutcome: a single end-to-end run that reaches an
// outcome verb surfaces below the recurrence floor.
func TestDetectWorkflowsSingleRunOutcome(t *testing.T) {
	got := DetectWorkflows([]TurnManifest{
		manifestFor("t1", "revops", "crm_fetch_leads", "score_leads", "gmail_send_email"),
	}, DetectOptions{RecurrenceFloor: 2})
	if len(got) != 1 {
		t.Fatalf("want 1 candidate for a single end-to-end run, got %d", len(got))
	}
	if got[0].Outcome != "gmail_send_email" {
		t.Fatalf("want outcome gmail_send_email, got %q", got[0].Outcome)
	}
}

// TestDetectWorkflowsOrderInsensitive: two runs of the same tool SET in a
// different order cluster under the apps OrderInsensitive option, but stay
// separate under the workflow lane's default exact-sequence contract.
func TestDetectWorkflowsOrderInsensitive(t *testing.T) {
	reordered := []TurnManifest{
		manifestFor("t1", "revops", "crm_fetch_leads", "score_leads"),
		manifestFor("t2", "revops", "score_leads", "crm_fetch_leads"),
	}

	// Default (exact order): two distinct single-run shapes, neither recurs to
	// the floor, so nothing surfaces.
	if got := DetectWorkflows(reordered, DetectOptions{RecurrenceFloor: 2}); len(got) != 0 {
		t.Fatalf("exact-order default: reordered runs must NOT cluster, got %d", len(got))
	}
	// OrderInsensitive: they cluster into one count=2 candidate.
	got := DetectWorkflows(reordered, DetectOptions{RecurrenceFloor: 2, OrderInsensitive: true})
	if len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("order-insensitive: want one count=2 candidate, got %d (%+v)", len(got), got)
	}
}

// TestDetectWorkflowsFuzzyTolerance: two runs whose tool sets differ by one tool
// (an extra confirm read) cluster under FuzzyToolTolerance=1, but stay separate
// when exact.
func TestDetectWorkflowsFuzzyTolerance(t *testing.T) {
	drift := []TurnManifest{
		manifestFor("t1", "revops", "crm_fetch_leads", "score_leads"),
		manifestFor("t2", "revops", "crm_fetch_leads", "score_leads", "log_result"),
	}
	if got := DetectWorkflows(drift, DetectOptions{RecurrenceFloor: 2}); len(got) != 0 {
		t.Fatalf("exact: one-tool drift must not cluster, got %d", len(got))
	}
	got := DetectWorkflows(drift, DetectOptions{RecurrenceFloor: 2, FuzzyToolTolerance: 1})
	if len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("fuzzy(1): one-tool drift must cluster to count=2, got %d (%+v)", len(got), got)
	}
	// A two-tool gap stays separate even at tolerance 1.
	farther := []TurnManifest{
		manifestFor("t1", "revops", "crm_fetch_leads", "score_leads"),
		manifestFor("t2", "revops", "fetch_tickets", "triage_ticket"),
	}
	if got := DetectWorkflows(farther, DetectOptions{RecurrenceFloor: 2, FuzzyToolTolerance: 1}); len(got) != 0 {
		t.Fatalf("fuzzy(1): unrelated shapes must not merge, got %d", len(got))
	}
}

// TestDetectWorkflowsCrossAgent: the same shape run by two DIFFERENT agents
// clusters under CrossAgent (with the agent blanked), but stays per-agent by
// default.
func TestDetectWorkflowsCrossAgent(t *testing.T) {
	twoAgents := []TurnManifest{
		manifestFor("t1", "revops", "crm_fetch_leads", "score_leads"),
		manifestFor("t2", "sales", "crm_fetch_leads", "score_leads"),
	}
	if got := DetectWorkflows(twoAgents, DetectOptions{RecurrenceFloor: 2}); len(got) != 0 {
		t.Fatalf("default: different agents must not cluster, got %d", len(got))
	}
	got := DetectWorkflows(twoAgents, DetectOptions{RecurrenceFloor: 2, CrossAgent: true})
	if len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("cross-agent: want one count=2 candidate, got %d (%+v)", len(got), got)
	}
	if got[0].Agent != "" {
		t.Fatalf("cross-agent candidate must claim no single agent, got %q", got[0].Agent)
	}
}

// TestDetectWorkflowsSingleRunExternalizingOnly: with the apps gate set, a single
// run surfaces only when it EXTERNALIZED (send/email/…); a single run ending in a
// workspace-internal verb (update/save) does not nag — but still recurs as signal.
func TestDetectWorkflowsSingleRunExternalizingOnly(t *testing.T) {
	internalRun := []TurnManifest{manifestFor("t1", "ops", "fetch_metrics", "update_dashboard")}
	externalRun := []TurnManifest{manifestFor("t1", "ops", "fetch_metrics", "post_to_slack")}

	apps := DetectOptions{RecurrenceFloor: 2, SingleRunRequiresExternalOutcome: true}

	if got := DetectWorkflows(internalRun, apps); len(got) != 0 {
		t.Fatalf("single internal-write run must NOT surface under apps gate, got %d", len(got))
	}
	if got := DetectWorkflows(externalRun, apps); len(got) != 1 {
		t.Fatalf("single externalizing run must surface, got %d", len(got))
	}
	// Without the gate (workflow lane default), the internal single run DOES
	// surface — the narrowing is opt-in.
	if got := DetectWorkflows(internalRun, DetectOptions{RecurrenceFloor: 2}); len(got) != 1 {
		t.Fatalf("workflow-lane default should still surface the internal single run, got %d", len(got))
	}
}

// TestDetectWorkflowsFiltersOrchestration: plumbing tools (bash/read/edit, the
// office MCP) never count toward a shape, so a chatty turn is not a "workflow".
func TestDetectWorkflowsFiltersOrchestration(t *testing.T) {
	got := DetectWorkflows([]TurnManifest{
		manifestFor("t1", "ceo", "Bash", "Read", "mcp__wuphf-office__team_task", "Edit"),
	}, DetectOptions{RecurrenceFloor: 2})
	if len(got) != 0 {
		t.Fatalf("orchestration-only turn must not surface, got %d candidates", len(got))
	}
}

// TestManifestToolTokenUnwrapsProxy: the generic external-action proxy unwraps
// to its domain action_id so integration tasks cluster by real operation and
// the outcome verb is visible.
func TestManifestToolTokenUnwrapsProxy(t *testing.T) {
	got := manifestToolToken("team_action_execute", `{"platform":"gmail","action_id":"GMAIL_SEND_EMAIL"}`)
	if got != "gmail_send_email" {
		t.Fatalf("proxy unwrap = %q, want gmail_send_email", got)
	}
	// Non-proxy tools pass through unchanged (trimmed).
	if got := manifestToolToken("  score_leads ", ""); got != "score_leads" {
		t.Fatalf("non-proxy passthrough = %q, want score_leads", got)
	}
	// Action-less proxy call falls back to the raw name (filtered downstream).
	if got := manifestToolToken("team_action_execute", `{}`); got != "team_action_execute" {
		t.Fatalf("action-less proxy fallback = %q, want team_action_execute", got)
	}
}

// TestTurnManifestInlineScope: a turn attributed to a task records under that
// task; a task-less (inline) turn records under a turn-scoped pseudo-task ONLY
// when it used >= 2 real work tools, so conversational turns don't pollute the
// corpus.
func TestTurnManifestInlineScope(t *testing.T) {
	man := func(taskID, turnID string, tools ...string) HeadlessEvent {
		tc := make([]HeadlessManifestEntry, 0, len(tools))
		for _, name := range tools {
			tc = append(tc, HeadlessManifestEntry{ToolName: name, Count: 1})
		}
		return HeadlessEvent{Type: HeadlessEventTypeManifest, TaskID: taskID, TurnID: turnID, Agent: "ceo", ToolCalls: tc}
	}

	// Task turn → recorded under the task id.
	if m, ok := turnManifestFromEvent(man("OFFICE-1", "tn1", "crm_fetch_leads", "score_leads")); !ok || m.TaskID != "OFFICE-1" {
		t.Fatalf("task turn: ok=%v id=%q", ok, m.TaskID)
	}
	// Task-less work-shaped turn → turn-scoped pseudo-task.
	m, ok := turnManifestFromEvent(man("", "tn2", "crm_fetch_leads", "score_leads"))
	if !ok || m.TaskID != inlineTurnScopePrefix+"tn2" || !isInlineTurnScope(m.TaskID) {
		t.Fatalf("inline turn: ok=%v id=%q", ok, m.TaskID)
	}
	// Task-less, orchestration-only → dropped.
	if _, ok := turnManifestFromEvent(man("", "tn3", "Bash", "Read", "Edit")); ok {
		t.Error("orchestration-only inline turn must not persist")
	}
	// Task-less, only one work tool → dropped (needs >= 2).
	if _, ok := turnManifestFromEvent(man("", "tn4", "score_leads")); ok {
		t.Error("single-work-tool inline turn must not persist")
	}
	// Task-less with no turn id → dropped (nothing to key on).
	if _, ok := turnManifestFromEvent(man("", "", "crm_fetch_leads", "score_leads")); ok {
		t.Error("inline turn without a turn id must not persist")
	}
}

// TestEventSinkRoundTripAndCorruption: manifests persist and read back in order,
// and a corrupt line is skipped rather than poisoning the corpus.
func TestEventSinkRoundTripAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	for _, m := range []TurnManifest{
		manifestFor("t1", "revops", "crm_fetch_leads"),
		manifestFor("t2", "revops", "score_leads"),
	} {
		if err := appendTurnManifest(path, m); err != nil {
			t.Fatalf("append: %v", err)
		}
	}
	// Inject a corrupt line between valid records.
	if err := appendRawLine(path, "{not json"); err != nil {
		t.Fatalf("append raw: %v", err)
	}
	if err := appendTurnManifest(path, manifestFor("t3", "revops", "send_digest")); err != nil {
		t.Fatalf("append: %v", err)
	}

	got, err := ReadTurnManifests(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 valid records (corrupt line skipped), got %d", len(got))
	}
	if got[0].TaskID != "t1" || got[2].TaskID != "t3" {
		t.Fatalf("order not preserved: %v", []string{got[0].TaskID, got[2].TaskID})
	}

	// Absent file is empty, not an error.
	empty, err := ReadTurnManifests(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil || len(empty) != 0 {
		t.Fatalf("absent sink: want empty/no-error, got len=%d err=%v", len(empty), err)
	}
}
