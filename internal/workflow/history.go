package workflow

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
)

// WorkflowExecutionLog captures a complete execution with per-step detail.
type WorkflowExecutionLog struct {
	WorkflowID string      `json:"workflow_id"`
	RunID      string      `json:"run_id"`
	Status     string      `json:"status"` // completed, aborted, error
	StartedAt  time.Time   `json:"started_at"`
	FinishedAt time.Time   `json:"finished_at"`
	Duration   string      `json:"duration"`
	Steps      []StepEvent `json:"steps"`
	StepCount  int         `json:"step_count"`
	ErrorCount int         `json:"error_count"`
}

// storageBasePath returns the root directory for workflow storage, derived
// from the config path the same way workflow_store.go does.
func storageBasePath() string {
	return filepath.Join(filepath.Dir(config.ConfigPath()), "workflows")
}

// overrideBaseDir is set during tests to redirect storage to a temp directory.
var overrideBaseDir string

func effectiveBasePath() string {
	if overrideBaseDir != "" {
		return overrideBaseDir
	}
	return storageBasePath()
}

// executionLogPath returns the JSONL file path for per-workflow execution history.
func executionLogPath(workflowID string) string {
	return filepath.Join(effectiveBasePath(), "interactive", sanitizeKey(workflowID)+".runs.jsonl")
}

// globalEventsPath returns the path to the cross-workflow events log.
func globalEventsPath() string {
	return filepath.Join(effectiveBasePath(), "events.jsonl")
}

// sanitizeKey normalises a workflow key for safe file system use, matching
// the sanitizeWorkflowKey logic in workflow_store.go.
func sanitizeKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return "workflow"
	}
	var b strings.Builder
	lastDash := false
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "workflow"
	}
	return out
}

// LogExecution writes a completed workflow execution to the per-workflow JSONL file
// AND the global events log.
func LogExecution(workflowID string, log WorkflowExecutionLog) error {
	// Ensure per-workflow log directory exists.
	path := executionLogPath(workflowID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	raw, err := json.Marshal(log)
	if err != nil {
		return err
	}

	// Append to per-workflow JSONL.
	if err := appendLine(path, raw); err != nil {
		return err
	}

	// Append a summary event to the global log.
	event := map[string]any{
		"type":        "execution",
		"workflow_id": log.WorkflowID,
		"run_id":      log.RunID,
		"status":      log.Status,
		"started_at":  log.StartedAt.Format(time.RFC3339),
		"finished_at": log.FinishedAt.Format(time.RFC3339),
		"duration":    log.Duration,
		"step_count":  log.StepCount,
		"error_count": log.ErrorCount,
	}
	return appendGlobalEvent(event)
}

// ListExecutions reads all executions for a workflow from the per-workflow JSONL.
func ListExecutions(workflowID string) ([]WorkflowExecutionLog, error) {
	path := executionLogPath(workflowID)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var logs []WorkflowExecutionLog
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry WorkflowExecutionLog
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // skip malformed lines
		}
		logs = append(logs, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return logs, nil
}

// appendGlobalEvent appends to ~/.wuphf/workflows/events.jsonl for cross-workflow analytics.
func appendGlobalEvent(event map[string]any) error {
	path := globalEventsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return appendLine(path, raw)
}

// appendLine appends a JSON line to a JSONL file.
func appendLine(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
}
