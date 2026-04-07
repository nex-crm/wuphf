package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/config"
)

type taskLogRecord struct {
	TaskID      string          `json:"task_id"`
	AgentSlug   string          `json:"agent_slug"`
	ToolName    string          `json:"tool_name"`
	Params      json.RawMessage `json:"params"`
	Result      json.RawMessage `json:"result"`
	Error       json.RawMessage `json:"error"`
	StartedAt   string          `json:"started_at"`
	CompletedAt string          `json:"completed_at"`
}

type taskLogArtifact struct {
	TaskID       string
	AgentSlug    string
	ToolName     string
	Summary      string
	StartedAt    string
	CompletedAt  string
	LogPath      string
	EntryCount   int
	UpdatedAt    time.Time
	WorktreePath string
	TaskTitle    string
}

type workflowRunArtifact struct {
	Provider    string `json:"provider"`
	WorkflowKey string `json:"workflow_key"`
	RunID       string `json:"run_id"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at"`
	FinishedAt  string `json:"finished_at"`
	Path        string
	UpdatedAt   time.Time
}

func (m channelModel) buildArtifactLines(contentWidth int) []renderedLine {
	lines := []renderedLine{{Text: renderDateSeparator(contentWidth, "Execution artifacts")}}
	taskLogs := m.recentTaskLogArtifacts(6)
	workflowRuns := recentWorkflowRunArtifacts(6)
	requests := recentHumanArtifactRequests(m.requests, 6)
	requestActions := recentRequestArtifactActions(m.actions, 6)

	if len(taskLogs) == 0 && len(workflowRuns) == 0 && len(requests) == 0 && len(requestActions) == 0 {
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
		return append(lines,
			renderedLine{Text: ""},
			renderedLine{Text: muted.Render("  No retained execution artifacts yet.")},
			renderedLine{Text: muted.Render("  Task tool logs, workflow runs, and human decision traces will appear here.")},
		)
	}

	if len(taskLogs) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Task output logs")})
		for _, artifact := range taskLogs {
			header := subtlePill(artifactClock(artifact.CompletedAt, artifact.UpdatedAt), "#E2E8F0", "#0F172A") +
				" " + accentPill("log", "#0F766E") +
				" " + lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("Task %s · %s", artifact.TaskID, fallbackString(artifact.ToolName, "tool run")))
			extra := []string{
				strings.TrimSpace(fmt.Sprintf("@%s · %d entr%s · %s", fallbackString(artifact.AgentSlug, "unknown"), artifact.EntryCount, pluralSuffix(artifact.EntryCount), prettyRelativeTime(artifactTime(artifact.CompletedAt, artifact.UpdatedAt)))),
			}
			if strings.TrimSpace(artifact.TaskTitle) != "" {
				extra = append(extra, "Title: "+artifact.TaskTitle)
			}
			if strings.TrimSpace(artifact.WorktreePath) != "" {
				extra = append(extra, "Worktree: "+artifact.WorktreePath)
			}
			if strings.TrimSpace(artifact.LogPath) != "" {
				extra = append(extra, "Log: "+artifact.LogPath)
			}
			for i, line := range renderRuntimeEventCard(contentWidth, header, artifact.Summary, "#0F766E", extra) {
				rendered := renderedLine{Text: "  " + line}
				if i == 0 {
					rendered.TaskID = artifact.TaskID
				}
				lines = append(lines, rendered)
			}
		}
	}

	if len(workflowRuns) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Workflow runs")})
		for _, run := range workflowRuns {
			header := subtlePill(artifactClock(run.FinishedAt, run.UpdatedAt), "#E2E8F0", "#0F172A") +
				" " + actionStatePill("external_workflow_"+fallbackString(run.Status, "finished")) +
				" " + lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%s · %s", fallbackString(run.WorkflowKey, "workflow"), fallbackString(run.Status, "finished")))
			body := strings.TrimSpace(fmt.Sprintf("%s via %s", fallbackString(run.RunID, "run"), fallbackString(run.Provider, "provider")))
			extra := []string{
				prettyRelativeTime(artifactTime(run.FinishedAt, run.UpdatedAt)),
			}
			if strings.TrimSpace(run.Path) != "" {
				extra = append(extra, "Run log: "+run.Path)
			}
			for _, line := range renderRuntimeEventCard(contentWidth, header, body, "#7C3AED", extra) {
				lines = append(lines, renderedLine{Text: "  " + line})
			}
		}
	}

	if len(requests) > 0 || len(requestActions) > 0 {
		lines = append(lines, renderedLine{Text: ""})
		lines = append(lines, renderedLine{Text: renderDateSeparator(contentWidth, "Human decisions")})
		for _, req := range requests {
			status := strings.TrimSpace(req.Status)
			if status == "" {
				status = "pending"
			}
			header := subtlePill(strings.ReplaceAll(status, "_", " "), "#FEF3C7", "#78350F") +
				" " + accentPill(req.Kind, "#92400E") +
				" " + lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("%s · %s", req.ID, req.TitleOrQuestion()))
			extra := []string{"Asked by @" + fallbackString(req.From, "unknown")}
			if strings.TrimSpace(req.RecommendedID) != "" {
				extra = append(extra, "Recommended: "+req.RecommendedID)
			}
			if due := strings.TrimSpace(req.DueAt); due != "" {
				extra = append(extra, "Due "+prettyRelativeTime(due))
			}
			for _, line := range renderRuntimeEventCard(contentWidth, header, req.Context, "#B45309", extra) {
				lines = append(lines, renderedLine{Text: "  " + line, RequestID: req.ID})
			}
		}
		for _, action := range requestActions {
			header := subtlePill(artifactClock(action.CreatedAt, time.Time{}), "#E2E8F0", "#0F172A") +
				" " + actionStatePill(action.Kind) +
				" " + lipgloss.NewStyle().Bold(true).Render(fallbackString(action.Summary, strings.ReplaceAll(action.Kind, "_", " ")))
			extra := []string{}
			if actor := strings.TrimSpace(action.Actor); actor != "" {
				extra = append(extra, "@"+actor)
			}
			if channel := strings.TrimSpace(action.Channel); channel != "" {
				extra = append(extra, "#"+channel)
			}
			if related := strings.TrimSpace(action.RelatedID); related != "" {
				extra = append(extra, related)
			}
			if source := strings.TrimSpace(action.Source); source != "" {
				extra = append(extra, source)
			}
			for _, line := range renderRuntimeEventCard(contentWidth, header, prettyRelativeTime(action.CreatedAt), "#1D4ED8", extra) {
				lines = append(lines, renderedLine{Text: "  " + line})
			}
		}
	}

	return lines
}

func (m channelModel) currentArtifactSummary() string {
	logCount := len(m.recentTaskLogArtifacts(4))
	workflowCount := len(recentWorkflowRunArtifacts(4))
	requestCount := len(recentHumanArtifactRequests(m.requests, 4)) + len(recentRequestArtifactActions(m.actions, 4))
	parts := make([]string, 0, 3)
	if logCount > 0 {
		parts = append(parts, fmt.Sprintf("%d task log%s", logCount, pluralSuffix(logCount)))
	}
	if workflowCount > 0 {
		parts = append(parts, fmt.Sprintf("%d workflow run%s", workflowCount, pluralSuffix(workflowCount)))
	}
	if requestCount > 0 {
		parts = append(parts, fmt.Sprintf("%d decision trace%s", requestCount, pluralSuffix(requestCount)))
	}
	return strings.Join(parts, " · ")
}

func (m channelModel) recentTaskLogArtifacts(limit int) []taskLogArtifact {
	root := taskLogRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	taskIndex := make(map[string]channelTask, len(m.tasks))
	for _, task := range m.tasks {
		taskIndex[task.ID] = task
	}

	artifacts := make([]taskLogArtifact, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name(), "output.log")
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		artifact, ok := readTaskLogArtifact(path, info)
		if !ok {
			continue
		}
		if task, ok := taskIndex[artifact.TaskID]; ok {
			artifact.TaskTitle = strings.TrimSpace(task.Title)
			artifact.WorktreePath = strings.TrimSpace(task.WorktreePath)
		}
		artifacts = append(artifacts, artifact)
	}

	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].UpdatedAt.After(artifacts[j].UpdatedAt)
	})
	if limit > 0 && len(artifacts) > limit {
		artifacts = artifacts[:limit]
	}
	return artifacts
}

func readTaskLogArtifact(path string, info fs.FileInfo) (taskLogArtifact, bool) {
	f, err := os.Open(path)
	if err != nil {
		return taskLogArtifact{}, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 128*1024)
	scanner.Buffer(buf, 1024*1024)

	var last string
	entryCount := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		last = line
		entryCount++
	}
	if scanner.Err() != nil || last == "" {
		return taskLogArtifact{}, false
	}

	var record taskLogRecord
	if err := json.Unmarshal([]byte(last), &record); err != nil {
		return taskLogArtifact{
			TaskID:     filepath.Base(filepath.Dir(path)),
			Summary:    truncateText(last, 160),
			LogPath:    path,
			EntryCount: entryCount,
			UpdatedAt:  info.ModTime(),
		}, true
	}

	taskID := strings.TrimSpace(record.TaskID)
	if taskID == "" {
		taskID = filepath.Base(filepath.Dir(path))
	}
	return taskLogArtifact{
		TaskID:      taskID,
		AgentSlug:   strings.TrimSpace(record.AgentSlug),
		ToolName:    strings.TrimSpace(record.ToolName),
		Summary:     summarizeTaskLogRecord(record),
		StartedAt:   strings.TrimSpace(record.StartedAt),
		CompletedAt: strings.TrimSpace(record.CompletedAt),
		LogPath:     path,
		EntryCount:  entryCount,
		UpdatedAt:   info.ModTime(),
	}, true
}

func summarizeTaskLogRecord(record taskLogRecord) string {
	if text := summarizeJSONField(record.Error, 120); text != "" && text != "null" {
		return "Error: " + text
	}
	if text := summarizeJSONField(record.Result, 160); text != "" && text != "null" {
		return text
	}
	if text := summarizeJSONField(record.Params, 120); text != "" && text != "null" {
		return "Params: " + text
	}
	return "Tool execution finished."
}

func summarizeJSONField(raw json.RawMessage, max int) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return ""
	}
	var plain string
	if err := json.Unmarshal(raw, &plain); err == nil {
		return truncateText(strings.TrimSpace(plain), max)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err == nil {
		return truncateText(compact.String(), max)
	}
	return truncateText(text, max)
}

func recentWorkflowRunArtifacts(limit int) []workflowRunArtifact {
	root := filepath.Join(filepath.Dir(config.ConfigPath()), "workflows")
	entries := []workflowRunArtifact{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".runs.jsonl") {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		artifact, ok := readWorkflowRunArtifact(path, info)
		if ok {
			entries = append(entries, artifact)
		}
		return nil
	})

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

func readWorkflowRunArtifact(path string, info fs.FileInfo) (workflowRunArtifact, bool) {
	f, err := os.Open(path)
	if err != nil {
		return workflowRunArtifact{}, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 128*1024)
	scanner.Buffer(buf, 1024*1024)

	var last string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		last = line
	}
	if scanner.Err() != nil || last == "" {
		return workflowRunArtifact{}, false
	}

	var artifact workflowRunArtifact
	if err := json.Unmarshal([]byte(last), &artifact); err != nil {
		return workflowRunArtifact{}, false
	}
	artifact.Path = path
	artifact.UpdatedAt = info.ModTime()
	return artifact, true
}

func recentHumanArtifactRequests(requests []channelInterview, limit int) []channelInterview {
	filtered := make([]channelInterview, 0, len(requests))
	for _, req := range requests {
		kind := strings.TrimSpace(req.Kind)
		switch kind {
		case "approval", "confirm", "choice", "interview":
			filtered = append(filtered, req)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		left, lok := parseChannelTime(filtered[i].CreatedAt)
		right, rok := parseChannelTime(filtered[j].CreatedAt)
		switch {
		case lok && rok:
			return left.After(right)
		case lok:
			return true
		case rok:
			return false
		default:
			return filtered[i].ID > filtered[j].ID
		}
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func recentRequestArtifactActions(actions []channelAction, limit int) []channelAction {
	filtered := make([]channelAction, 0, len(actions))
	for _, action := range actions {
		if strings.HasPrefix(strings.TrimSpace(action.Kind), "request_") {
			filtered = append(filtered, action)
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	out := append([]channelAction(nil), filtered...)
	reverseAny(out)
	return out
}

func taskLogRoot() string {
	if root := strings.TrimSpace(os.Getenv("WUPHF_TASK_LOG_ROOT")); root != "" {
		return root
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".wuphf", "office", "tasks")
	}
	return filepath.Join(home, ".wuphf", "office", "tasks")
}

func artifactClock(timestamp string, fallback time.Time) string {
	if clock := strings.TrimSpace(shortClock(timestamp)); clock != "" {
		return clock
	}
	if !fallback.IsZero() {
		return fallback.Local().Format("15:04")
	}
	return "artifact"
}

func artifactTime(timestamp string, fallback time.Time) string {
	if strings.TrimSpace(timestamp) != "" {
		return timestamp
	}
	if !fallback.IsZero() {
		return fallback.Format(time.RFC3339)
	}
	return ""
}
