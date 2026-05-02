package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var errTaskMemoryWorkflowBadRequest = errors.New("bad memory workflow request")

func (b *Broker) RecordTaskMemoryLookup(taskID, actor, query string, citations []ContextCitation) (teamTask, bool, bool, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return teamTask{}, false, false, fmt.Errorf("%w: task id required", errTaskMemoryWorkflowBadRequest)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID != taskID {
			continue
		}
		changed := recordMemoryWorkflowLookup(&b.tasks[i], actor, query, citations, now)
		if changed {
			b.tasks[i].UpdatedAt = now
			if err := b.saveLocked(); err != nil {
				return teamTask{}, true, true, err
			}
		}
		return b.tasks[i], true, changed, nil
	}
	return teamTask{}, false, false, nil
}

func (b *Broker) RecordTaskMemoryCapture(taskID, actor string, artifact MemoryWorkflowArtifact) (teamTask, bool, bool, error) {
	return b.recordTaskMemoryArtifact(taskID, actor, artifact, MemoryWorkflowStepCapture)
}

func (b *Broker) RecordTaskMemoryPromotion(taskID, actor string, artifact MemoryWorkflowArtifact) (teamTask, bool, bool, error) {
	return b.recordTaskMemoryArtifact(taskID, actor, artifact, MemoryWorkflowStepPromote)
}

func (b *Broker) recordTaskMemoryArtifact(taskID, actor string, artifact MemoryWorkflowArtifact, step MemoryWorkflowStep) (teamTask, bool, bool, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return teamTask{}, false, false, fmt.Errorf("%w: task id required", errTaskMemoryWorkflowBadRequest)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID != taskID {
			continue
		}
		var changed bool
		switch step {
		case MemoryWorkflowStepCapture:
			changed = recordMemoryWorkflowCapture(&b.tasks[i], actor, artifact, now)
		case MemoryWorkflowStepPromote:
			changed = recordMemoryWorkflowPromotion(&b.tasks[i], actor, artifact, now)
		default:
			return teamTask{}, true, false, fmt.Errorf("%w: unsupported memory workflow step %q", errTaskMemoryWorkflowBadRequest, step)
		}
		if changed {
			b.tasks[i].UpdatedAt = now
			if err := b.saveLocked(); err != nil {
				return teamTask{}, true, true, err
			}
		}
		return b.tasks[i], true, changed, nil
	}
	return teamTask{}, false, false, nil
}

func (b *Broker) handleTaskMemoryWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Action     string                   `json:"action"`
		Event      string                   `json:"event"`
		TaskID     string                   `json:"task_id"`
		Actor      string                   `json:"actor"`
		Query      string                   `json:"query"`
		Citations  []ContextCitation        `json:"citations"`
		Artifact   MemoryWorkflowArtifact   `json:"artifact"`
		Artifacts  []MemoryWorkflowArtifact `json:"artifacts"`
		SkipReason string                   `json:"skip_reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	action := strings.TrimSpace(body.Action)
	if action == "" {
		action = strings.TrimSpace(body.Event)
	}
	var (
		task    teamTask
		found   bool
		changed bool
		err     error
	)
	switch action {
	case "lookup":
		task, found, changed, err = b.RecordTaskMemoryLookup(body.TaskID, body.Actor, body.Query, body.Citations)
	case "capture", "capture_skipped":
		artifacts, validationErr := validatedMemoryWorkflowArtifacts(action, body.Artifact, body.Artifacts, body.SkipReason)
		if validationErr != nil {
			http.Error(w, "memory workflow capture artifact required", http.StatusBadRequest)
			return
		}
		for _, artifact := range artifacts {
			var artifactChanged bool
			task, found, artifactChanged, err = b.RecordTaskMemoryCapture(body.TaskID, body.Actor, artifact)
			changed = artifactChanged || changed
			if err != nil || !found {
				break
			}
		}
	case "promote", "promotion", "promote_skipped":
		artifacts, validationErr := validatedMemoryWorkflowArtifacts(action, body.Artifact, body.Artifacts, body.SkipReason)
		if validationErr != nil {
			http.Error(w, "memory workflow promotion artifact required", http.StatusBadRequest)
			return
		}
		for _, artifact := range artifacts {
			var artifactChanged bool
			task, found, artifactChanged, err = b.RecordTaskMemoryPromotion(body.TaskID, body.Actor, artifact)
			changed = artifactChanged || changed
			if err != nil || !found {
				break
			}
		}
	default:
		http.Error(w, "unknown memory workflow action", http.StatusBadRequest)
		return
	}
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, errTaskMemoryWorkflowBadRequest) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	if !found {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"task": task, "updated": changed})
}

func validatedMemoryWorkflowArtifacts(action string, artifact MemoryWorkflowArtifact, artifacts []MemoryWorkflowArtifact, skipReason string) ([]MemoryWorkflowArtifact, error) {
	candidates := artifacts
	if len(candidates) == 0 {
		candidates = []MemoryWorkflowArtifact{artifact}
	}
	validated := make([]MemoryWorkflowArtifact, 0, len(candidates))
	for _, candidate := range candidates {
		normalized := normalizeMemoryWorkflowArtifact(candidate, "")
		if strings.HasSuffix(action, "_skipped") && memoryWorkflowArtifactKey(normalized) == "" {
			reason := strings.TrimSpace(skipReason)
			normalized = normalizeMemoryWorkflowArtifact(MemoryWorkflowArtifact{Source: "skip", Title: reason, SkipReason: reason}, "")
		}
		if memoryWorkflowArtifactKey(normalized) == "" {
			return nil, fmt.Errorf("%w: artifact required", errTaskMemoryWorkflowBadRequest)
		}
		validated = append(validated, normalized)
	}
	return validated, nil
}

func (b *Broker) handleTaskMemoryWorkflowReconcile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	report, err := b.ReconcileMemoryWorkflows(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": report})
}
