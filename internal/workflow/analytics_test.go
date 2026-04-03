package workflow

import (
	"testing"
	"time"
)

func TestComputeStats_HappyPath(t *testing.T) {
	setupTestDir(t)

	// Log executions for two different workflows.
	logs := []WorkflowExecutionLog{
		{
			WorkflowID: "email-triage",
			RunID:      "run-1",
			Status:     "completed",
			StartedAt:  time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
			FinishedAt: time.Date(2026, 4, 1, 10, 2, 0, 0, time.UTC),
			Duration:   "2m0s",
			StepCount:  3,
			ErrorCount: 0,
		},
		{
			WorkflowID: "email-triage",
			RunID:      "run-2",
			Status:     "completed",
			StartedAt:  time.Date(2026, 4, 1, 11, 0, 0, 0, time.UTC),
			FinishedAt: time.Date(2026, 4, 1, 11, 4, 0, 0, time.UTC),
			Duration:   "4m0s",
			StepCount:  5,
			ErrorCount: 1,
		},
		{
			WorkflowID: "deploy-check",
			RunID:      "run-3",
			Status:     "error",
			StartedAt:  time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
			FinishedAt: time.Date(2026, 4, 1, 12, 1, 0, 0, time.UTC),
			Duration:   "1m0s",
			StepCount:  2,
			ErrorCount: 1,
		},
	}
	for _, l := range logs {
		if err := LogExecution(l.WorkflowID, l); err != nil {
			t.Fatalf("LogExecution(%s): %v", l.RunID, err)
		}
	}

	stats, err := ComputeStats()
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}

	if stats.TotalWorkflows != 2 {
		t.Errorf("expected 2 workflows, got %d", stats.TotalWorkflows)
	}
	if stats.TotalExecutions != 3 {
		t.Errorf("expected 3 executions, got %d", stats.TotalExecutions)
	}
	if stats.TotalErrors != 2 {
		t.Errorf("expected 2 total errors, got %d", stats.TotalErrors)
	}

	// Avg duration: (2m + 4m + 1m) / 3 = ~2m20s
	expectedAvg := (2*time.Minute + 4*time.Minute + 1*time.Minute) / 3
	if stats.AvgDuration != expectedAvg {
		t.Errorf("expected avg duration %v, got %v", expectedAvg, stats.AvgDuration)
	}

	if len(stats.Workflows) != 2 {
		t.Fatalf("expected 2 workflow summaries, got %d", len(stats.Workflows))
	}

	// Check email-triage summary.
	var emailSummary *WorkflowSummary
	for i := range stats.Workflows {
		if stats.Workflows[i].ID == "email-triage" {
			emailSummary = &stats.Workflows[i]
			break
		}
	}
	if emailSummary == nil {
		t.Fatal("email-triage summary not found")
	}
	if emailSummary.Executions != 2 {
		t.Errorf("expected 2 executions for email-triage, got %d", emailSummary.Executions)
	}
	expectedEmailAvg := (2*time.Minute + 4*time.Minute) / 2
	if emailSummary.AvgDuration != expectedEmailAvg {
		t.Errorf("expected email-triage avg duration %v, got %v", expectedEmailAvg, emailSummary.AvgDuration)
	}
	// Error rate: 1 error / 2 executions = 0.5
	if emailSummary.ErrorRate != 0.5 {
		t.Errorf("expected email-triage error rate 0.5, got %f", emailSummary.ErrorRate)
	}
}

func TestComputeStats_EmptyEventsFile(t *testing.T) {
	setupTestDir(t)

	stats, err := ComputeStats()
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.TotalWorkflows != 0 {
		t.Errorf("expected 0 workflows, got %d", stats.TotalWorkflows)
	}
	if stats.TotalExecutions != 0 {
		t.Errorf("expected 0 executions, got %d", stats.TotalExecutions)
	}
	if len(stats.Workflows) != 0 {
		t.Errorf("expected 0 workflow summaries, got %d", len(stats.Workflows))
	}
}

func TestComputeStats_SingleWorkflow(t *testing.T) {
	setupTestDir(t)

	log := WorkflowExecutionLog{
		WorkflowID: "solo-wf",
		RunID:      "run-solo",
		Status:     "completed",
		StartedAt:  time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 4, 1, 10, 10, 0, 0, time.UTC),
		Duration:   "10m0s",
		StepCount:  5,
		ErrorCount: 0,
	}
	if err := LogExecution("solo-wf", log); err != nil {
		t.Fatalf("LogExecution: %v", err)
	}

	stats, err := ComputeStats()
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	if stats.TotalWorkflows != 1 {
		t.Errorf("expected 1 workflow, got %d", stats.TotalWorkflows)
	}
	if stats.TotalExecutions != 1 {
		t.Errorf("expected 1 execution, got %d", stats.TotalExecutions)
	}
	if stats.TotalErrors != 0 {
		t.Errorf("expected 0 errors, got %d", stats.TotalErrors)
	}
	if stats.AvgDuration != 10*time.Minute {
		t.Errorf("expected avg 10m, got %v", stats.AvgDuration)
	}
	if len(stats.Workflows) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(stats.Workflows))
	}
	if stats.Workflows[0].ErrorRate != 0 {
		t.Errorf("expected 0 error rate, got %f", stats.Workflows[0].ErrorRate)
	}
}

func TestComputeStats_ZeroErrorRate(t *testing.T) {
	setupTestDir(t)

	for _, runID := range []string{"r1", "r2"} {
		log := WorkflowExecutionLog{
			WorkflowID: "clean-wf",
			RunID:      runID,
			Status:     "completed",
			StartedAt:  time.Now(),
			FinishedAt: time.Now(),
			Duration:   "1m0s",
			StepCount:  1,
			ErrorCount: 0,
		}
		if err := LogExecution("clean-wf", log); err != nil {
			t.Fatalf("LogExecution(%s): %v", runID, err)
		}
	}

	stats, err := ComputeStats()
	if err != nil {
		t.Fatalf("ComputeStats: %v", err)
	}
	for _, wf := range stats.Workflows {
		if wf.ErrorRate != 0 {
			t.Errorf("expected 0 error rate for %s, got %f", wf.ID, wf.ErrorRate)
		}
	}
}
