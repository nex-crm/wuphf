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
	"github.com/nex-crm/wuphf/internal/team"
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
	snapshot := m.currentArtifactSnapshot(24)
	artifacts := snapshot.Items

	if len(artifacts) == 0 {
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted))
		return append(lines,
			renderedLine{Text: ""},
			renderedLine{Text: muted.Render("  No retained execution artifacts yet.")},
			renderedLine{Text: muted.Render("  Task tool logs, workflow runs, and human decision traces will appear here.")},
		)
	}

	lines = append(lines, renderArtifactSection(contentWidth, "Task execution", snapshot.Filter(team.RuntimeArtifactTask, team.RuntimeArtifactTaskLog))...)
	lines = append(lines, renderArtifactSection(contentWidth, "Workflow runs", snapshot.Filter(team.RuntimeArtifactWorkflowRun))...)
	lines = append(lines, renderArtifactSection(contentWidth, "Requests and approvals", snapshot.Filter(team.RuntimeArtifactRequest))...)
	lines = append(lines, renderArtifactSection(contentWidth, "Action traces", snapshot.Filter(team.RuntimeArtifactHumanAction, team.RuntimeArtifactExternalAction))...)

	return lines
}

func (m channelModel) currentArtifactSummary() string {
	snapshot := m.currentArtifactSnapshot(24)
	logCount := snapshot.Count(team.RuntimeArtifactTask, team.RuntimeArtifactTaskLog)
	workflowCount := snapshot.Count(team.RuntimeArtifactWorkflowRun)
	requestCount := snapshot.Count(team.RuntimeArtifactRequest, team.RuntimeArtifactHumanAction, team.RuntimeArtifactExternalAction)
	parts := make([]string, 0, 3)
	if logCount > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", logCount, pluralizeWord(logCount, "task run", "task runs")))
	}
	if workflowCount > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", workflowCount, pluralizeWord(workflowCount, "workflow run", "workflow runs")))
	}
	if requestCount > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", requestCount, pluralizeWord(requestCount, "action trace", "action traces")))
	}
	return strings.Join(parts, " · ")
}

func (m channelModel) currentRuntimeArtifacts(limit int) []team.RuntimeArtifact {
	return m.currentArtifactSnapshot(limit).Items
}

func renderArtifactSection(contentWidth int, title string, artifacts []team.RuntimeArtifact) []renderedLine {
	if len(artifacts) == 0 {
		return nil
	}
	lines := []renderedLine{{Text: ""}, {Text: renderDateSeparator(contentWidth, title)}}
	for _, artifact := range artifacts {
		header, accent := renderArtifactHeader(artifact)
		extra := artifactExtraLines(artifact)
		for i, line := range renderRuntimeEventCard(contentWidth, header, artifact.EffectiveSummary(), accent, extra) {
			rendered := renderedLine{Text: "  " + line}
			if (artifact.Kind == team.RuntimeArtifactTask || artifact.Kind == team.RuntimeArtifactTaskLog) && i == 0 {
				rendered.TaskID = artifact.ID
			}
			if artifact.Kind == team.RuntimeArtifactRequest && i == 0 {
				rendered.RequestID = artifact.ID
			}
			lines = append(lines, rendered)
		}
	}
	return lines
}

func renderArtifactHeader(artifact team.RuntimeArtifact) (string, string) {
	clock := subtlePill(artifactClock(artifact.UpdatedAt, parseArtifactTimestamp(artifact.UpdatedAt, artifact.StartedAt)), "#E2E8F0", "#0F172A")
	title := lipgloss.NewStyle().Bold(true).Render(artifact.EffectiveTitle())
	switch artifact.Kind {
	case team.RuntimeArtifactTask:
		return clock + " " + artifactLifecyclePill(artifact.State, "#0F766E", "#B45309", "#B91C1C", "#15803D") + " " + title, artifactAccentColor(artifact.State, "#0F766E")
	case team.RuntimeArtifactTaskLog:
		return clock + " " + accentPill("log", "#0F766E") + " " + title, "#0F766E"
	case team.RuntimeArtifactWorkflowRun:
		return clock + " " + artifactLifecyclePill(artifact.State, "#7C3AED", "#B45309", "#B91C1C", "#15803D") + " " + title, artifactAccentColor(artifact.State, "#7C3AED")
	case team.RuntimeArtifactRequest:
		return clock + " " + artifactLifecyclePill(artifact.State, "#B45309", "#B45309", "#B91C1C", "#15803D") + " " + title, artifactAccentColor(artifact.State, "#B45309")
	case team.RuntimeArtifactExternalAction:
		return clock + " " + artifactLifecyclePill(artifact.State, "#1D4ED8", "#B45309", "#B91C1C", "#15803D") + " " + title, artifactAccentColor(artifact.State, "#1D4ED8")
	default:
		return clock + " " + artifactLifecyclePill(artifact.State, "#475569", "#B45309", "#B91C1C", "#15803D") + " " + title, artifactAccentColor(artifact.State, "#475569")
	}
}

func artifactExtraLines(artifact team.RuntimeArtifact) []string {
	extra := []string{}
	if progress := strings.TrimSpace(artifact.EffectiveProgress()); progress != "" && !strings.EqualFold(progress, strings.TrimSpace(artifact.EffectiveSummary())) {
		extra = append(extra, "Progress: "+progress)
	}
	if output := strings.TrimSpace(artifact.PartialOutput); output != "" {
		extra = append(extra, "Output: "+output)
	}
	if strings.TrimSpace(artifact.Owner) != "" {
		extra = append(extra, "@"+artifact.Owner)
	}
	if strings.TrimSpace(artifact.Channel) != "" {
		extra = append(extra, "#"+artifact.Channel)
	}
	if strings.TrimSpace(artifact.Worktree) != "" {
		extra = append(extra, "Worktree: "+artifact.Worktree)
	}
	if strings.TrimSpace(artifact.Path) != "" {
		extra = append(extra, "Path: "+artifact.Path)
	}
	if strings.TrimSpace(artifact.RelatedID) != "" {
		extra = append(extra, "Related: "+artifact.RelatedID)
	}
	if artifact.Blocking {
		extra = append(extra, "Blocking")
	}
	if strings.TrimSpace(artifact.ReviewHint) != "" {
		extra = append(extra, artifact.ReviewHint)
	}
	if strings.TrimSpace(artifact.ResumeHint) != "" {
		extra = append(extra, artifact.ResumeHint)
	}
	return extra
}

func artifactLifecyclePill(state, runningColor, pendingColor, failedColor, completedColor string) string {
	label := strings.ReplaceAll(fallbackString(strings.TrimSpace(state), "retained"), "_", " ")
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "blocked", "failed", "canceled", "cancelled":
		return accentPill(label, failedColor)
	case "pending", "review", "started":
		return subtlePill(label, "#FEF3C7", pendingColor)
	case "completed":
		return subtlePill(label, "#DCFCE7", completedColor)
	default:
		return subtlePill(label, "#DBEAFE", runningColor)
	}
}

func artifactAccentColor(state, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "blocked", "failed", "canceled", "cancelled":
		return "#B91C1C"
	case "pending", "review", "started":
		return "#B45309"
	case "completed":
		return "#15803D"
	default:
		return fallback
	}
}

func parseArtifactTimestamp(primary, fallback string) time.Time {
	for _, candidate := range []string{strings.TrimSpace(primary), strings.TrimSpace(fallback)} {
		if candidate == "" {
			continue
		}
		if ts, ok := parseChannelTime(candidate); ok {
			return ts
		}
	}
	return time.Time{}
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
	defer func() { _ = f.Close() }()

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
	defer func() { _ = f.Close() }()

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

func recentExecutionArtifactActions(actions []channelAction, limit int) []channelAction {
	filtered := make([]channelAction, 0, len(actions))
	for _, action := range actions {
		kind := strings.TrimSpace(action.Kind)
		if strings.HasPrefix(kind, "request_") || strings.HasPrefix(kind, "external_") || strings.HasPrefix(kind, "interrupt_") || strings.HasPrefix(kind, "human_") {
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
