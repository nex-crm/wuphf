package workflow

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	overrideBaseDir = dir
	t.Cleanup(func() { overrideBaseDir = "" })
	return dir
}

func TestLogExecution_HappyPath(t *testing.T) {
	dir := setupTestDir(t)

	log := WorkflowExecutionLog{
		WorkflowID: "email-triage",
		RunID:      "run-001",
		Status:     "completed",
		StartedAt:  time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 4, 1, 10, 5, 0, 0, time.UTC),
		Duration:   "5m0s",
		Steps: []StepEvent{
			{StepID: "list", Action: "a", Timestamp: time.Now()},
			{StepID: "triage", Action: "Enter", Timestamp: time.Now()},
		},
		StepCount:  2,
		ErrorCount: 0,
	}

	if err := LogExecution("email-triage", log); err != nil {
		t.Fatalf("LogExecution: %v", err)
	}

	// Verify per-workflow file exists.
	perWF := filepath.Join(dir, "interactive", "email-triage.runs.jsonl")
	if _, err := os.Stat(perWF); err != nil {
		t.Fatalf("per-workflow log not created: %v", err)
	}

	// Verify global events file exists.
	global := filepath.Join(dir, "events.jsonl")
	if _, err := os.Stat(global); err != nil {
		t.Fatalf("global events log not created: %v", err)
	}

	// Read back and verify.
	logs, err := ListExecutions("email-triage")
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(logs))
	}
	if logs[0].RunID != "run-001" {
		t.Errorf("expected run-001, got %s", logs[0].RunID)
	}
	if logs[0].StepCount != 2 {
		t.Errorf("expected 2 steps, got %d", logs[0].StepCount)
	}
	if logs[0].Status != "completed" {
		t.Errorf("expected completed, got %s", logs[0].Status)
	}
}

func TestLogExecution_MultipleRuns(t *testing.T) {
	setupTestDir(t)

	for i, runID := range []string{"run-001", "run-002", "run-003"} {
		log := WorkflowExecutionLog{
			WorkflowID: "deploy-check",
			RunID:      runID,
			Status:     "completed",
			StartedAt:  time.Now(),
			FinishedAt: time.Now(),
			Duration:   "1m0s",
			StepCount:  i + 1,
			ErrorCount: 0,
		}
		if err := LogExecution("deploy-check", log); err != nil {
			t.Fatalf("LogExecution(%s): %v", runID, err)
		}
	}

	logs, err := ListExecutions("deploy-check")
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(logs) != 3 {
		t.Fatalf("expected 3 executions, got %d", len(logs))
	}
	if logs[2].RunID != "run-003" {
		t.Errorf("expected last run to be run-003, got %s", logs[2].RunID)
	}
}

func TestListExecutions_MissingFile(t *testing.T) {
	setupTestDir(t)

	logs, err := ListExecutions("nonexistent-workflow")
	if err != nil {
		t.Fatalf("ListExecutions for missing file should not error: %v", err)
	}
	if logs != nil {
		t.Errorf("expected nil for missing file, got %v", logs)
	}
}

func TestLogExecution_WithErrors(t *testing.T) {
	setupTestDir(t)

	log := WorkflowExecutionLog{
		WorkflowID: "failing-workflow",
		RunID:      "run-err-001",
		Status:     "error",
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
		Duration:   "30s",
		Steps: []StepEvent{
			{StepID: "step1", Action: "run", Error: "connection refused"},
		},
		StepCount:  1,
		ErrorCount: 1,
	}

	if err := LogExecution("failing-workflow", log); err != nil {
		t.Fatalf("LogExecution: %v", err)
	}

	logs, err := ListExecutions("failing-workflow")
	if err != nil {
		t.Fatalf("ListExecutions: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(logs))
	}
	if logs[0].ErrorCount != 1 {
		t.Errorf("expected 1 error, got %d", logs[0].ErrorCount)
	}
	if logs[0].Status != "error" {
		t.Errorf("expected error status, got %s", logs[0].Status)
	}
}

func TestSanitizeKey(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"email-triage", "email-triage"},
		{"Deploy Check", "deploy-check"},
		{"", "workflow"},
		{"  UPPER  ", "upper"},
		{"a/b/c", "a-b-c"},
		{"---", "workflow"},
	}
	for _, tt := range tests {
		got := sanitizeKey(tt.input)
		if got != tt.expected {
			t.Errorf("sanitizeKey(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
