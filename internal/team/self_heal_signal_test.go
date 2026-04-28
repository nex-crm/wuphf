package team

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

// seedSelfHealTask injects a teamTask onto the broker without going through
// the request flow, so tests can simulate any task/incident shape directly.
func seedSelfHealTask(t *testing.T, b *Broker, task teamTask) {
	t.Helper()
	b.mu.Lock()
	b.tasks = append(b.tasks, task)
	b.mu.Unlock()
}

func TestSelfHealSignalScanner_EmitsCandidateForResolvedIncident(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC()
	seedSelfHealTask(t, b, teamTask{
		ID:         "task-77",
		Title:      selfHealingTaskTitle("deploy-bot", "task-7"),
		Details:    selfHealingTaskDetails("deploy-bot", "task-7", agent.EscalationCapabilityGap, "missing deploy specialist"),
		Owner:      "deploy-bot",
		Status:     "done",
		PipelineID: "incident",
		TaskType:   "incident",
		CreatedAt:  now.Add(-30 * time.Minute).Format(time.RFC3339),
		UpdatedAt:  now.Format(time.RFC3339),
	})

	scanner := NewSelfHealSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate, got %d (%+v)", len(cands), cands)
	}

	got := cands[0]
	if got.Source != SourceSelfHealResolved {
		t.Errorf("source: got %q want %q", got.Source, SourceSelfHealResolved)
	}
	if got.SignalCount != 1 {
		t.Errorf("signal count: got %d want 1", got.SignalCount)
	}
	if !strings.HasPrefix(got.SuggestedName, "handle-") {
		t.Errorf("suggested name should start with handle-, got %q", got.SuggestedName)
	}
	if !strings.Contains(got.SuggestedName, "capability-gap") {
		t.Errorf("suggested name should encode the reason, got %q", got.SuggestedName)
	}
	if len(got.Excerpts) != 1 {
		t.Errorf("expected 1 excerpt, got %d", len(got.Excerpts))
	}
	if got.Excerpts[0].Path != "task-77" {
		t.Errorf("excerpt path: got %q want %q", got.Excerpts[0].Path, "task-77")
	}
}

func TestSelfHealSignalScanner_SkipsOpenIncidents(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC()
	seedSelfHealTask(t, b, teamTask{
		ID:         "task-78",
		Title:      selfHealingTaskTitle("deploy-bot", "task-99"),
		Details:    "still investigating",
		Owner:      "deploy-bot",
		Status:     "in_progress", // not done
		PipelineID: "incident",
		CreatedAt:  now.Format(time.RFC3339),
		UpdatedAt:  now.Format(time.RFC3339),
	})

	scanner := NewSelfHealSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates (open incident), got %d (%+v)", len(cands), cands)
	}
}

func TestSelfHealSignalScanner_SkipsNonIncidentTasks(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC()
	seedSelfHealTask(t, b, teamTask{
		ID:         "task-79",
		Title:      "Refactor onboarding flow",
		Details:    "general engineering work",
		Owner:      "eng-bot",
		Status:     "done",
		PipelineID: "general", // not incident
		CreatedAt:  now.Format(time.RFC3339),
		UpdatedAt:  now.Format(time.RFC3339),
	})

	scanner := NewSelfHealSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates (non-incident), got %d (%+v)", len(cands), cands)
	}
}

func TestSelfHealSignalScanner_SkipsIncidentWithoutSelfHealPrefix(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC()
	seedSelfHealTask(t, b, teamTask{
		ID:         "task-80",
		Title:      "Outage triage: payments down", // missing the Self-heal prefix
		Details:    "post-mortem complete",
		Owner:      "ops-bot",
		Status:     "done",
		PipelineID: "incident",
		CreatedAt:  now.Format(time.RFC3339),
		UpdatedAt:  now.Format(time.RFC3339),
	})

	scanner := NewSelfHealSignalScanner(b)
	cands, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("expected 0 candidates (non-self-heal), got %d (%+v)", len(cands), cands)
	}
}

func TestSelfHealSignalScanner_AdvancesCutoffAcrossPasses(t *testing.T) {
	b := newTestBroker(t)

	now := time.Now().UTC()
	seedSelfHealTask(t, b, teamTask{
		ID:         "task-81",
		Title:      selfHealingTaskTitle("deploy-bot", "task-7"),
		Details:    selfHealingTaskDetails("deploy-bot", "task-7", agent.EscalationCapabilityGap, "missing relay"),
		Owner:      "deploy-bot",
		Status:     "done",
		PipelineID: "incident",
		CreatedAt:  now.Add(-time.Hour).Format(time.RFC3339),
		UpdatedAt:  now.Format(time.RFC3339),
	})

	scanner := NewSelfHealSignalScanner(b)
	first, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("first scan: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("first scan: expected 1 candidate, got %d", len(first))
	}

	// Second pass should be empty because the cutoff moved forward.
	second, err := scanner.Scan(context.Background())
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second scan: expected 0 (already-emitted), got %d", len(second))
	}
}

func TestParseSelfHealReason(t *testing.T) {
	details := selfHealingTaskDetails("deploy-bot", "task-7", agent.EscalationCapabilityGap, "missing deploy specialist")
	got := parseSelfHealReason(details)
	if got != string(agent.EscalationCapabilityGap) {
		t.Errorf("parseSelfHealReason: got %q want %q", got, agent.EscalationCapabilityGap)
	}
}

func TestReasonSlug(t *testing.T) {
	cases := map[string]string{
		"capability_gap":  "capability-gap",
		"Capability Gap":  "capability-gap",
		"Max  Retries!!!": "max-retries",
		"":                "self-heal",
	}
	for in, want := range cases {
		if got := reasonSlug(in); got != want {
			t.Errorf("reasonSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
