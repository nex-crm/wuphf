package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
)

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

func recentWorkflowRunArtifacts(limit int) []workflowRunArtifact {
	root := filepath.Join(filepath.Dir(config.ConfigPath()), "workflows")
	entries := []workflowRunArtifact{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
				return nil
			}
			return fmt.Errorf("walk %s: %w", path, err)
		}
		if d == nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".runs.jsonl") {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			// Race: file vanished between WalkDir listing and stat. Other
			// stat errors propagate so we don't silently miss rows.
			if errors.Is(statErr, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("stat %s: %w", path, statErr)
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
