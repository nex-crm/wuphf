package team

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"
)

const memoryWorkflowReconcileInterval = 10 * time.Minute

type MemoryWorkflowReconcileReport struct {
	Checked          int                                 `json:"checked"`
	Repaired         int                                 `json:"repaired"`
	Timestamp        string                              `json:"timestamp"`
	Tasks            []MemoryWorkflowReconcileTaskResult `json:"tasks,omitempty"`
	MissingArtifacts []MemoryWorkflowArtifact            `json:"missing_artifacts,omitempty"`
}

type MemoryWorkflowReconcileTaskResult struct {
	TaskID           string                   `json:"task_id"`
	Changed          bool                     `json:"changed"`
	Repairs          []string                 `json:"repairs,omitempty"`
	MissingArtifacts []MemoryWorkflowArtifact `json:"missing_artifacts,omitempty"`
}

type MemoryWorkflowReconciler struct {
	worker *WikiWorker
	review *ReviewLog
	now    func() time.Time
}

func NewMemoryWorkflowReconciler(worker *WikiWorker, review *ReviewLog, now func() time.Time) *MemoryWorkflowReconciler {
	if now == nil {
		now = time.Now
	}
	return &MemoryWorkflowReconciler{worker: worker, review: review, now: now}
}

func (r *MemoryWorkflowReconciler) ReconcileTasks(ctx context.Context, tasks []teamTask) ([]teamTask, MemoryWorkflowReconcileReport, error) {
	timestamp := r.now().UTC().Format(time.RFC3339)
	report := MemoryWorkflowReconcileReport{Timestamp: timestamp}
	out := make([]teamTask, len(tasks))
	promotionsBySource := r.promotionsBySource()
	for i, task := range tasks {
		if err := ctx.Err(); err != nil {
			return nil, report, err
		}
		before := cloneTeamTaskForRollback(task)
		taskResult := MemoryWorkflowReconcileTaskResult{TaskID: task.ID}
		syncTaskMemoryWorkflow(&task, timestamp)
		r.repairTaskWorkflow(&task, promotionsBySource, &taskResult, timestamp)
		report.Checked++
		if !reflect.DeepEqual(before.MemoryWorkflow, task.MemoryWorkflow) {
			task.UpdatedAt = timestamp
			taskResult.Changed = true
			report.Repaired++
			report.Tasks = append(report.Tasks, taskResult)
		}
		report.MissingArtifacts = append(report.MissingArtifacts, taskResult.MissingArtifacts...)
		out[i] = task
	}
	return out, report, nil
}

func (r *MemoryWorkflowReconciler) repairTaskWorkflow(task *teamTask, promotionsBySource map[string]*Promotion, result *MemoryWorkflowReconcileTaskResult, timestamp string) {
	if task == nil || task.MemoryWorkflow == nil {
		return
	}
	wf := task.MemoryWorkflow
	r.repairCaptureArtifacts(wf, result)
	r.repairPromotionArtifacts(wf, result)
	r.repairPromotionsFromCapture(wf, promotionsBySource, result, timestamp)
	if countPresentArtifacts(wf.Captures) == 0 && len(wf.Captures) > 0 {
		wf.Capture.CompletedAt = ""
		wf.Capture.Status = MemoryWorkflowStepStatusPending
		wf.Capture.Count = 0
	}
	if countPresentArtifacts(wf.Promotions) == 0 && len(wf.Promotions) > 0 {
		wf.Promote.CompletedAt = ""
		wf.Promote.Status = MemoryWorkflowStepStatusPending
		wf.Promote.Count = 0
	}
	refreshMemoryWorkflowStatus(wf, timestamp)
}

func (r *MemoryWorkflowReconciler) repairCaptureArtifacts(wf *MemoryWorkflow, result *MemoryWorkflowReconcileTaskResult) {
	for i := range wf.Captures {
		artifact := &wf.Captures[i]
		if !artifactRequiresLocalFile(*artifact) {
			continue
		}
		exists := r.artifactExists(*artifact)
		if exists && artifact.Missing {
			artifact.Missing = false
			result.Repairs = append(result.Repairs, "capture artifact restored: "+artifact.Path)
			continue
		}
		if !exists && !artifact.Missing {
			artifact.Missing = true
			result.Repairs = append(result.Repairs, "capture artifact missing: "+artifact.Path)
			result.MissingArtifacts = append(result.MissingArtifacts, *artifact)
		} else if !exists {
			result.MissingArtifacts = append(result.MissingArtifacts, *artifact)
		}
	}
}

func (r *MemoryWorkflowReconciler) repairPromotionArtifacts(wf *MemoryWorkflow, result *MemoryWorkflowReconcileTaskResult) {
	for i := range wf.Promotions {
		artifact := &wf.Promotions[i]
		if artifact.PromotionID != "" && r.review != nil {
			p, err := r.review.Get(artifact.PromotionID)
			if err == nil {
				if artifact.Path == "" {
					artifact.Path = p.TargetPath
				}
				if artifact.State != string(p.State) {
					artifact.State = string(p.State)
					result.Repairs = append(result.Repairs, "promotion state refreshed: "+artifact.PromotionID)
				}
				if artifact.CommitSHA == "" {
					artifact.CommitSHA = p.CommitSHA
				}
				if !p.UpdatedAt.IsZero() {
					artifact.UpdatedAt = p.UpdatedAt.UTC().Format(time.RFC3339)
				}
				artifact.Missing = false
				continue
			}
			if errors.Is(err, ErrPromotionNotFound) && !artifact.Missing {
				artifact.Missing = true
				result.Repairs = append(result.Repairs, "promotion missing: "+artifact.PromotionID)
				result.MissingArtifacts = append(result.MissingArtifacts, *artifact)
				continue
			}
		}
		if !artifactRequiresLocalFile(*artifact) {
			continue
		}
		exists := r.artifactExists(*artifact)
		if exists && artifact.Missing {
			artifact.Missing = false
			result.Repairs = append(result.Repairs, "promotion artifact restored: "+artifact.Path)
			continue
		}
		if !exists && !artifact.Missing {
			artifact.Missing = true
			result.Repairs = append(result.Repairs, "promotion artifact missing: "+artifact.Path)
			result.MissingArtifacts = append(result.MissingArtifacts, *artifact)
		} else if !exists {
			result.MissingArtifacts = append(result.MissingArtifacts, *artifact)
		}
	}
}

func (r *MemoryWorkflowReconciler) repairPromotionsFromCapture(wf *MemoryWorkflow, promotionsBySource map[string]*Promotion, result *MemoryWorkflowReconcileTaskResult, timestamp string) {
	if len(promotionsBySource) == 0 {
		return
	}
	for _, capture := range wf.Captures {
		if capture.Missing || capture.Path == "" {
			continue
		}
		promotion := promotionsBySource[capture.Path]
		if promotion == nil {
			continue
		}
		artifact := MemoryWorkflowArtifact{
			Backend:     "markdown",
			Source:      "promotion",
			Path:        promotion.TargetPath,
			PromotionID: promotion.ID,
			Title:       capture.Title,
			CommitSHA:   promotion.CommitSHA,
			State:       string(promotion.State),
			RecordedAt:  promotion.CreatedAt.UTC().Format(time.RFC3339),
			UpdatedAt:   promotion.UpdatedAt.UTC().Format(time.RFC3339),
		}
		if appendMemoryWorkflowArtifact(&wf.Promotions, normalizeMemoryWorkflowArtifact(artifact, timestamp)) {
			result.Repairs = append(result.Repairs, "promotion linked from capture: "+promotion.ID)
		}
	}
}

func (r *MemoryWorkflowReconciler) promotionsBySource() map[string]*Promotion {
	if r.review == nil {
		return nil
	}
	out := make(map[string]*Promotion)
	for _, promotion := range r.review.List("all") {
		if promotion == nil || strings.TrimSpace(promotion.SourcePath) == "" {
			continue
		}
		out[strings.TrimSpace(promotion.SourcePath)] = promotion
	}
	return out
}

func (r *MemoryWorkflowReconciler) artifactExists(artifact MemoryWorkflowArtifact) bool {
	if r.worker == nil {
		return false
	}
	path := strings.TrimSpace(artifact.Path)
	if path == "" {
		return true
	}
	switch {
	case strings.HasPrefix(path, "agents/"):
		_, err := r.worker.NotebookRead(path)
		return err == nil
	case strings.HasPrefix(path, "team/"):
		_, err := readArticle(r.worker.Repo(), path)
		return err == nil
	default:
		return true
	}
}

func artifactRequiresLocalFile(artifact MemoryWorkflowArtifact) bool {
	path := strings.TrimSpace(artifact.Path)
	if strings.HasPrefix(path, "agents/") || strings.HasPrefix(path, "team/") {
		return true
	}
	source := strings.ToLower(strings.TrimSpace(artifact.Source))
	return source == "notebook" || source == "wiki"
}

func (b *Broker) startMemoryWorkflowReconcilerLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(memoryWorkflowReconcileInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = b.ReconcileMemoryWorkflows(context.Background())
			}
		}
	}()
}

func (b *Broker) ReconcileMemoryWorkflows(ctx context.Context) (MemoryWorkflowReconcileReport, error) {
	b.mu.Lock()
	tasks := make([]teamTask, len(b.tasks))
	for i := range b.tasks {
		tasks[i] = cloneTeamTaskForRollback(b.tasks[i])
	}
	worker := b.wikiWorker
	review := b.reviewLog
	b.mu.Unlock()

	reconciler := NewMemoryWorkflowReconciler(worker, review, time.Now)
	reconciled, report, err := reconciler.ReconcileTasks(ctx, tasks)
	if err != nil {
		return report, err
	}
	if report.Repaired == 0 {
		return report, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	byID := make(map[string]teamTask, len(reconciled))
	for _, task := range reconciled {
		byID[task.ID] = task
	}
	for i := range b.tasks {
		if updated, ok := byID[b.tasks[i].ID]; ok {
			b.tasks[i].MemoryWorkflow = cloneMemoryWorkflow(updated.MemoryWorkflow)
			b.tasks[i].UpdatedAt = updated.UpdatedAt
		}
	}
	if err := b.saveLocked(); err != nil {
		return report, err
	}
	return report, nil
}

func (b *Broker) runMemoryWorkflowReconciler() (MemoryWorkflowReconcileReport, error) {
	return b.ReconcileMemoryWorkflows(context.Background())
}
