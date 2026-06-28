package team

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// newMemoryWorkflowReconcilerFixture returns a WikiWorker backed by a repo
// that already holds a canonical wiki article, so the reconciler's on-disk
// existence check (team/ paths) can verify present artifacts.
func newMemoryWorkflowReconcilerFixture(t *testing.T) *WikiWorker {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init repo: %v", err)
	}
	if _, _, err := repo.Commit(context.Background(), "pm", "team/process/onboarding.md", "# Onboarding\n\nReusable note.\n", "create", "seed wiki"); err != nil {
		t.Fatalf("seed wiki: %v", err)
	}
	return NewWikiWorker(repo, noopPublisher{})
}

func TestMemoryWorkflowReconcilerNoOp(t *testing.T) {
	worker := newMemoryWorkflowReconcilerFixture(t)
	now := "2026-04-30T10:00:00Z"
	task := teamTask{
		ID:        "task-1",
		TaskType:  "research",
		Title:     "Research prior context for onboarding",
		status:    "in_progress",
		CreatedAt: now,
		UpdatedAt: now,
	}
	syncTaskMemoryWorkflow(&task, now)
	recordMemoryWorkflowLookup(&task, "pm", "prior onboarding", []ContextCitation{{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}}, now)
	recordMemoryWorkflowCapture(&task, "pm", MemoryWorkflowArtifact{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}, now)
	recordMemoryWorkflowPromotion(&task, "pm", MemoryWorkflowArtifact{
		Backend:    "markdown",
		Source:     "wiki",
		Path:       "team/process/onboarding.md",
		RecordedAt: now,
		UpdatedAt:  now,
	}, now)

	reconciler := NewMemoryWorkflowReconciler(worker, func() time.Time {
		return time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	})
	_, report, err := reconciler.ReconcileTasks(context.Background(), []teamTask{task})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.Checked != 1 || report.Repaired != 0 {
		t.Fatalf("expected no-op report, got %+v", report)
	}
}

func TestMemoryWorkflowReconcilerRepairsStaleWorkflow(t *testing.T) {
	worker := newMemoryWorkflowReconcilerFixture(t)
	task := teamTask{
		ID:        "task-1",
		TaskType:  "research",
		Title:     "Research prior context for onboarding",
		status:    "in_progress",
		CreatedAt: "2026-04-30T09:59:00Z",
		UpdatedAt: "2026-04-30T09:59:00Z",
		MemoryWorkflow: &MemoryWorkflow{
			Required:          true,
			Status:            MemoryWorkflowStatusPending,
			RequirementReason: "stale",
			RequiredSteps:     []MemoryWorkflowStep{MemoryWorkflowStepLookup, MemoryWorkflowStepCapture, MemoryWorkflowStepPromote},
			Lookup:            MemoryWorkflowStepState{Required: true, Status: MemoryWorkflowStepStatusPending, Query: "prior onboarding"},
			Capture:           MemoryWorkflowStepState{Required: true, Status: MemoryWorkflowStepStatusPending},
			Promote:           MemoryWorkflowStepState{Required: true, Status: MemoryWorkflowStepStatusPending},
			Citations:         []ContextCitation{{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}},
			Captures:          []MemoryWorkflowArtifact{{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}},
			Promotions:        []MemoryWorkflowArtifact{{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}},
		},
	}

	reconciler := NewMemoryWorkflowReconciler(worker, func() time.Time {
		return time.Date(2026, 4, 30, 10, 5, 0, 0, time.UTC)
	})
	updated, report, err := reconciler.ReconcileTasks(context.Background(), []teamTask{task})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.Repaired != 1 {
		t.Fatalf("expected one repaired task, got %+v", report)
	}
	wf := updated[0].MemoryWorkflow
	if wf.Status != MemoryWorkflowStatusSatisfied {
		t.Fatalf("expected satisfied workflow, got %+v", wf)
	}
	if wf.Lookup.Status != MemoryWorkflowStepStatusSatisfied || wf.Capture.Status != MemoryWorkflowStepStatusSatisfied || wf.Promote.Status != MemoryWorkflowStepStatusSatisfied {
		t.Fatalf("expected all steps satisfied, got %+v", wf)
	}
}

func TestMemoryWorkflowReconcilerMarksMissingArtifacts(t *testing.T) {
	worker := newMemoryWorkflowReconcilerFixture(t)
	task := teamTask{
		ID:        "task-1",
		TaskType:  "research",
		Title:     "Research prior context for onboarding",
		status:    "in_progress",
		CreatedAt: "2026-04-30T09:59:00Z",
		UpdatedAt: "2026-04-30T09:59:00Z",
		MemoryWorkflow: &MemoryWorkflow{
			Required:      true,
			Status:        MemoryWorkflowStatusSatisfied,
			RequiredSteps: []MemoryWorkflowStep{MemoryWorkflowStepLookup, MemoryWorkflowStepCapture, MemoryWorkflowStepPromote},
			Lookup:        MemoryWorkflowStepState{Required: true, Status: MemoryWorkflowStepStatusSatisfied, Query: "prior onboarding", CompletedAt: "2026-04-30T10:00:00Z"},
			Capture:       MemoryWorkflowStepState{Required: true, Status: MemoryWorkflowStepStatusSatisfied, CompletedAt: "2026-04-30T10:01:00Z"},
			Promote:       MemoryWorkflowStepState{Required: true, Status: MemoryWorkflowStepStatusPending},
			Captures:      []MemoryWorkflowArtifact{{Backend: "markdown", Source: "wiki", Path: "team/process/missing.md"}},
		},
	}

	reconciler := NewMemoryWorkflowReconciler(worker, func() time.Time {
		return time.Date(2026, 4, 30, 10, 5, 0, 0, time.UTC)
	})
	updated, report, err := reconciler.ReconcileTasks(context.Background(), []teamTask{task})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.Repaired != 1 || len(report.MissingArtifacts) != 1 {
		t.Fatalf("expected missing artifact repair, got %+v", report)
	}
	wf := updated[0].MemoryWorkflow
	if !wf.Captures[0].Missing {
		t.Fatalf("expected capture marked missing: %+v", wf.Captures[0])
	}
	if wf.Capture.Status != MemoryWorkflowStepStatusPending || wf.Status != MemoryWorkflowStatusPending {
		t.Fatalf("expected workflow back to pending, got %+v", wf)
	}
}

func TestMemoryWorkflowReconcilerNilWorkerSkipsArtifactExistenceRepairs(t *testing.T) {
	now := "2026-04-30T10:00:00Z"
	task := teamTask{
		ID:        "task-1",
		TaskType:  "research",
		Title:     "Research prior context for onboarding",
		status:    "in_progress",
		CreatedAt: now,
		UpdatedAt: now,
	}
	syncTaskMemoryWorkflow(&task, now)
	recordMemoryWorkflowLookup(&task, "pm", "prior onboarding", []ContextCitation{{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}}, now)
	recordMemoryWorkflowCapture(&task, "pm", MemoryWorkflowArtifact{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}, now)
	recordMemoryWorkflowPromotion(&task, "pm", MemoryWorkflowArtifact{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}, now)

	reconciler := NewMemoryWorkflowReconciler(nil, func() time.Time {
		return time.Date(2026, 4, 30, 10, 5, 0, 0, time.UTC)
	})
	updated, report, err := reconciler.ReconcileTasks(context.Background(), []teamTask{task})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if report.Repaired != 0 || len(report.MissingArtifacts) != 0 {
		t.Fatalf("nil worker should skip file existence repairs, got %+v", report)
	}
	wf := updated[0].MemoryWorkflow
	if wf.Captures[0].Missing || wf.Promotions[0].Missing || wf.Status != MemoryWorkflowStatusSatisfied {
		t.Fatalf("nil worker should preserve satisfied workflow, got %+v", wf)
	}
}

func TestReconciledTaskNewerRequiresStrictlyNewerTimestamp(t *testing.T) {
	current := teamTask{ID: "task-1", UpdatedAt: "2026-04-30T10:05:00Z"}
	if reconciledTaskNewer(teamTask{ID: "task-1", UpdatedAt: "2026-04-30T10:05:00Z"}, current) {
		t.Fatal("equal reconciler timestamp should not overwrite current task")
	}
	if reconciledTaskNewer(teamTask{ID: "task-1", UpdatedAt: "2026-04-30T10:04:59Z"}, current) {
		t.Fatal("older reconciler timestamp should not overwrite current task")
	}
	if !reconciledTaskNewer(teamTask{ID: "task-1", UpdatedAt: "2026-04-30T10:05:01Z"}, current) {
		t.Fatal("newer reconciler timestamp should apply")
	}
}

func TestBrokerRunMemoryWorkflowReconcilerManualTrigger(t *testing.T) {
	worker := newMemoryWorkflowReconcilerFixture(t)
	b := newTestBroker(t)
	b.mu.Lock()
	b.wikiWorker = worker
	b.tasks = []teamTask{
		{
			ID:        "task-1",
			TaskType:  "research",
			Title:     "Research prior context for onboarding",
			status:    "in_progress",
			CreatedAt: "2026-04-30T09:59:00Z",
			UpdatedAt: "2026-04-30T09:59:00Z",
			MemoryWorkflow: &MemoryWorkflow{
				Required:      true,
				Status:        MemoryWorkflowStatusPending,
				RequiredSteps: []MemoryWorkflowStep{MemoryWorkflowStepLookup, MemoryWorkflowStepCapture, MemoryWorkflowStepPromote},
				Lookup:        MemoryWorkflowStepState{Required: true, Status: MemoryWorkflowStepStatusSatisfied, Query: "prior onboarding", CompletedAt: "2026-04-30T10:00:00Z"},
				Capture:       MemoryWorkflowStepState{Required: true, Status: MemoryWorkflowStepStatusPending},
				Promote:       MemoryWorkflowStepState{Required: true, Status: MemoryWorkflowStepStatusPending},
				Citations:     []ContextCitation{{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}},
				Captures:      []MemoryWorkflowArtifact{{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}},
				Promotions:    []MemoryWorkflowArtifact{{Backend: "markdown", Source: "wiki", Path: "team/process/onboarding.md"}},
			},
		},
	}
	b.mu.Unlock()

	report, err := b.runMemoryWorkflowReconciler()
	if err != nil {
		t.Fatalf("manual reconciler: %v", err)
	}
	if report.Repaired != 1 {
		t.Fatalf("expected one repair from manual trigger, got %+v", report)
	}
	tasks := b.AllTasks()
	if len(tasks) != 1 || tasks[0].MemoryWorkflow == nil || tasks[0].MemoryWorkflow.Status != MemoryWorkflowStatusSatisfied {
		t.Fatalf("expected broker task repaired, got %+v", tasks)
	}
}
