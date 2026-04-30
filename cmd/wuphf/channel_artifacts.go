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

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/team"
)

func (m channelModel) buildArtifactLines(contentWidth int) []channelui.RenderedLine {
	lines := []channelui.RenderedLine{{Text: channelui.RenderDateSeparator(contentWidth, "Execution artifacts")}}
	snapshot := m.currentArtifactSnapshot(24)
	artifacts := snapshot.Items

	if len(artifacts) == 0 {
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color(channelui.SlackMuted))
		return append(lines,
			channelui.RenderedLine{Text: ""},
			channelui.RenderedLine{Text: muted.Render("  No retained execution artifacts yet.")},
			channelui.RenderedLine{Text: muted.Render("  Task tool logs, workflow runs, and human decision traces will appear here.")},
		)
	}

	lines = append(lines, channelui.RenderArtifactSection(contentWidth, "Task execution", snapshot.Filter(team.RuntimeArtifactTask, team.RuntimeArtifactTaskLog))...)
	lines = append(lines, channelui.RenderArtifactSection(contentWidth, "Workflow runs", snapshot.Filter(team.RuntimeArtifactWorkflowRun))...)
	lines = append(lines, channelui.RenderArtifactSection(contentWidth, "Requests and approvals", snapshot.Filter(team.RuntimeArtifactRequest))...)
	lines = append(lines, channelui.RenderArtifactSection(contentWidth, "Action traces", snapshot.Filter(team.RuntimeArtifactHumanAction, team.RuntimeArtifactExternalAction))...)

	return lines
}

func (m channelModel) currentArtifactSummary() string {
	snapshot := m.currentArtifactSnapshot(24)
	logCount := snapshot.Count(team.RuntimeArtifactTask, team.RuntimeArtifactTaskLog)
	workflowCount := snapshot.Count(team.RuntimeArtifactWorkflowRun)
	requestCount := snapshot.Count(team.RuntimeArtifactRequest, team.RuntimeArtifactHumanAction, team.RuntimeArtifactExternalAction)
	parts := make([]string, 0, 3)
	if logCount > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", logCount, channelui.PluralizeWord(logCount, "task run", "task runs")))
	}
	if workflowCount > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", workflowCount, channelui.PluralizeWord(workflowCount, "workflow run", "workflow runs")))
	}
	if requestCount > 0 {
		parts = append(parts, fmt.Sprintf("%d %s", requestCount, channelui.PluralizeWord(requestCount, "action trace", "action traces")))
	}
	return strings.Join(parts, " · ")
}

func (m channelModel) currentRuntimeArtifacts(limit int) []team.RuntimeArtifact {
	return m.currentArtifactSnapshot(limit).Items
}

func (m channelModel) recentTaskLogArtifacts(limit int) []channelui.TaskLogArtifact {
	root := channelui.TaskLogRoot()
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	taskIndex := make(map[string]channelui.Task, len(m.tasks))
	for _, task := range m.tasks {
		taskIndex[task.ID] = task
	}

	artifacts := make([]channelui.TaskLogArtifact, 0, len(entries))
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

func readTaskLogArtifact(path string, info fs.FileInfo) (channelui.TaskLogArtifact, bool) {
	f, err := os.Open(path)
	if err != nil {
		return channelui.TaskLogArtifact{}, false
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
		return channelui.TaskLogArtifact{}, false
	}

	var record channelui.TaskLogRecord
	if err := json.Unmarshal([]byte(last), &record); err != nil {
		return channelui.TaskLogArtifact{
			TaskID:     filepath.Base(filepath.Dir(path)),
			Summary:    channelui.TruncateText(last, 160),
			LogPath:    path,
			EntryCount: entryCount,
			UpdatedAt:  info.ModTime(),
		}, true
	}

	taskID := strings.TrimSpace(record.TaskID)
	if taskID == "" {
		taskID = filepath.Base(filepath.Dir(path))
	}
	return channelui.TaskLogArtifact{
		TaskID:      taskID,
		AgentSlug:   strings.TrimSpace(record.AgentSlug),
		ToolName:    strings.TrimSpace(record.ToolName),
		Summary:     channelui.SummarizeTaskLogRecord(record),
		StartedAt:   strings.TrimSpace(record.StartedAt),
		CompletedAt: strings.TrimSpace(record.CompletedAt),
		LogPath:     path,
		EntryCount:  entryCount,
		UpdatedAt:   info.ModTime(),
	}, true
}

func recentWorkflowRunArtifacts(limit int) []channelui.WorkflowRunArtifact {
	root := filepath.Join(filepath.Dir(config.ConfigPath()), "workflows")
	entries := []channelui.WorkflowRunArtifact{}
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

func readWorkflowRunArtifact(path string, info fs.FileInfo) (channelui.WorkflowRunArtifact, bool) {
	f, err := os.Open(path)
	if err != nil {
		return channelui.WorkflowRunArtifact{}, false
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
		return channelui.WorkflowRunArtifact{}, false
	}

	var artifact channelui.WorkflowRunArtifact
	if err := json.Unmarshal([]byte(last), &artifact); err != nil {
		return channelui.WorkflowRunArtifact{}, false
	}
	artifact.Path = path
	artifact.UpdatedAt = info.ModTime()
	return artifact, true
}
