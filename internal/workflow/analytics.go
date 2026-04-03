package workflow

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"
)

// WorkflowStats summarizes usage across all workflows.
type WorkflowStats struct {
	TotalWorkflows  int               `json:"total_workflows"`
	TotalExecutions int               `json:"total_executions"`
	TotalErrors     int               `json:"total_errors"`
	AvgDuration     time.Duration     `json:"avg_duration"`
	Workflows       []WorkflowSummary `json:"workflows"`
}

// WorkflowSummary summarizes a single workflow's execution history.
type WorkflowSummary struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Executions  int           `json:"executions"`
	LastRun     time.Time     `json:"last_run"`
	AvgDuration time.Duration `json:"avg_duration"`
	ErrorRate   float64       `json:"error_rate"`
}

// globalEvent is the internal representation of an event read from events.jsonl.
type globalEvent struct {
	Type       string `json:"type"`
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Duration   string `json:"duration"`
	StepCount  int    `json:"step_count"`
	ErrorCount int    `json:"error_count"`
}

// ComputeStats reads all execution logs from the global events.jsonl and computes analytics.
func ComputeStats() (*WorkflowStats, error) {
	events, err := readGlobalEvents()
	if err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return &WorkflowStats{}, nil
	}

	// Aggregate per-workflow.
	type aggregate struct {
		executions   int
		errors       int
		totalDur     time.Duration
		lastRun      time.Time
	}

	byWorkflow := make(map[string]*aggregate)
	order := make([]string, 0) // preserve first-seen order

	for _, ev := range events {
		if ev.Type != "execution" {
			continue
		}
		agg, exists := byWorkflow[ev.WorkflowID]
		if !exists {
			agg = &aggregate{}
			byWorkflow[ev.WorkflowID] = agg
			order = append(order, ev.WorkflowID)
		}

		agg.executions++
		agg.errors += ev.ErrorCount

		dur, _ := time.ParseDuration(ev.Duration)
		agg.totalDur += dur

		finished, _ := time.Parse(time.RFC3339, ev.FinishedAt)
		if finished.After(agg.lastRun) {
			agg.lastRun = finished
		}
	}

	stats := &WorkflowStats{}
	var totalDur time.Duration

	for _, wfID := range order {
		agg := byWorkflow[wfID]
		stats.TotalExecutions += agg.executions
		stats.TotalErrors += agg.errors
		totalDur += agg.totalDur

		var avgDur time.Duration
		if agg.executions > 0 {
			avgDur = agg.totalDur / time.Duration(agg.executions)
		}

		var errorRate float64
		if agg.executions > 0 {
			errorRate = float64(agg.errors) / float64(agg.executions)
		}

		stats.Workflows = append(stats.Workflows, WorkflowSummary{
			ID:          wfID,
			Executions:  agg.executions,
			LastRun:     agg.lastRun,
			AvgDuration: avgDur,
			ErrorRate:   errorRate,
		})
	}

	stats.TotalWorkflows = len(byWorkflow)
	if stats.TotalExecutions > 0 {
		stats.AvgDuration = totalDur / time.Duration(stats.TotalExecutions)
	}

	return stats, nil
}

// readGlobalEvents reads and parses all events from the global events.jsonl file.
func readGlobalEvents() ([]globalEvent, error) {
	path := globalEventsPath()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []globalEvent
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev globalEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue // skip malformed lines
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}
