package team

import (
	"fmt"
	"strings"
	"time"
)

type RuntimeTask struct {
	ID             string
	Title          string
	Owner          string
	Status         string
	PipelineStage  string
	ReviewState    string
	ExecutionMode  string
	WorktreePath   string
	WorktreeBranch string
	Blocked        bool
}

type RuntimeRequest struct {
	ID       string
	Kind     string
	Title    string
	Question string
	From     string
	Blocking bool
	Required bool
	Status   string
	Channel  string
	Secret   bool
}

type RuntimeMessage struct {
	ID        string
	From      string
	Title     string
	Content   string
	ReplyTo   string
	Timestamp string
}

type RuntimeSnapshot struct {
	Channel      string
	SessionMode  string
	DirectAgent  string
	GeneratedAt  time.Time
	Tasks        []RuntimeTask
	Requests     []RuntimeRequest
	Recent       []RuntimeMessage
	Artifacts    []RuntimeArtifact
	Capabilities RuntimeCapabilities
	Registry     CapabilityRegistry
	Memory       SessionMemorySnapshot
	Recovery     SessionRecovery
}

type RuntimeSnapshotInput struct {
	Channel      string
	SessionMode  string
	DirectAgent  string
	Tasks        []RuntimeTask
	Requests     []RuntimeRequest
	Recent       []RuntimeMessage
	Artifacts    []RuntimeArtifact
	Capabilities RuntimeCapabilities
	Registry     CapabilityRegistry
	Now          time.Time
}

func BuildRuntimeSnapshot(input RuntimeSnapshotInput) RuntimeSnapshot {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	sessionMode := NormalizeSessionMode(input.SessionMode)
	directAgent := NormalizeOneOnOneAgent(input.DirectAgent)
	if sessionMode != SessionModeOneOnOne {
		directAgent = ""
	}
	snapshot := RuntimeSnapshot{
		Channel:      strings.TrimSpace(input.Channel),
		SessionMode:  sessionMode,
		DirectAgent:  directAgent,
		GeneratedAt:  now,
		Tasks:        append([]RuntimeTask(nil), input.Tasks...),
		Requests:     append([]RuntimeRequest(nil), input.Requests...),
		Recent:       append([]RuntimeMessage(nil), input.Recent...),
		Artifacts:    append([]RuntimeArtifact(nil), input.Artifacts...),
		Capabilities: input.Capabilities,
		Registry:     input.Registry,
	}
	if len(snapshot.Registry.Entries) == 0 {
		snapshot.Registry = BuildCapabilityRegistry(snapshot.Capabilities)
	}
	snapshot.Memory = BuildSessionMemorySnapshot(sessionMode, directAgent, snapshot.Tasks, snapshot.Requests, snapshot.Recent)
	snapshot.Recovery = snapshot.Memory.ToRecovery()
	return snapshot
}

func (s RuntimeSnapshot) FormatText() string {
	channel := s.Channel
	if channel == "" {
		channel = "general"
	}

	lines := []string{
		fmt.Sprintf("Runtime state for #%s", channel),
	}
	if s.SessionMode == SessionModeOneOnOne {
		lines = append(lines, fmt.Sprintf("- Session mode: 1:1 with @%s", s.DirectAgent))
	} else {
		lines = append(lines, "- Session mode: office")
	}
	lines = append(lines,
		fmt.Sprintf("- Running tasks: %d of %d", s.runningTaskCount(), len(s.Tasks)),
		fmt.Sprintf("- Isolated worktrees: %d", s.isolatedTaskCount()),
		fmt.Sprintf("- Pending human requests: %d", s.pendingRequestCount()),
	)
	if len(s.Artifacts) > 0 {
		lines = append(lines, fmt.Sprintf("- Retained execution artifacts: %d", len(s.Artifacts)))
	}

	if focus := strings.TrimSpace(s.Recovery.Focus); focus != "" {
		lines = append(lines, fmt.Sprintf("- Current focus: %s", focus))
	}

	if len(s.Recovery.NextSteps) > 0 {
		lines = append(lines, "", "Next steps:")
		for _, step := range s.Recovery.NextSteps {
			lines = append(lines, "- "+step)
		}
	}

	if len(s.Recovery.Highlights) > 0 {
		lines = append(lines, "", "Recent highlights:")
		for _, line := range s.Recovery.Highlights {
			lines = append(lines, "- "+line)
		}
	}

	if len(s.Artifacts) > 0 {
		lines = append(lines, "", "Execution artifacts:")
		for _, artifact := range firstRuntimeArtifacts(s.Artifacts, 4) {
			line := fmt.Sprintf("- %s [%s]", artifact.EffectiveTitle(), artifact.Kind)
			if state := strings.TrimSpace(artifact.State); state != "" {
				line += " " + strings.ReplaceAll(state, "_", " ")
			}
			if summary := strings.TrimSpace(artifact.Summary); summary != "" {
				line += ": " + summary
			}
			lines = append(lines, line)
		}
	}

	if tmuxLines := s.Capabilities.Tmux.FormatLines(); len(tmuxLines) > 0 {
		lines = append(lines, "", "Tmux runtime:")
		lines = append(lines, tmuxLines...)
	}

	if len(s.Capabilities.Items) > 0 {
		lines = append(lines, "", "Runtime capabilities:")
		for _, item := range s.Capabilities.Items {
			line := fmt.Sprintf("- %s [%s]: %s", item.Name, item.Level, item.Detail)
			if next := strings.TrimSpace(item.NextStep); next != "" {
				line += " Next: " + next
			}
			lines = append(lines, line)
		}
	}

	if len(s.Registry.Entries) > 0 {
		lines = append(lines, "", "Capability registry:")
		for _, item := range s.Registry.Entries {
			line := fmt.Sprintf("- %s (%s) [%s]: %s", item.Label, item.Category, item.Level, item.Detail)
			if next := strings.TrimSpace(item.NextStep); next != "" {
				line += " Next: " + next
			}
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n")
}

func (s RuntimeSnapshot) runningTaskCount() int {
	count := 0
	for _, task := range s.Tasks {
		if runtimeTaskIsRunning(task) {
			count++
		}
	}
	return count
}

func (s RuntimeSnapshot) isolatedTaskCount() int {
	count := 0
	for _, task := range s.Tasks {
		if runtimeTaskUsesIsolation(task) {
			count++
		}
	}
	return count
}

func (s RuntimeSnapshot) pendingRequestCount() int {
	count := 0
	for _, req := range s.Requests {
		status := strings.ToLower(strings.TrimSpace(req.Status))
		if status == "" || status == "pending" || status == "open" {
			count++
		}
	}
	return count
}

func runtimeTaskIsRunning(task RuntimeTask) bool {
	status := strings.ToLower(strings.TrimSpace(task.Status))
	switch status {
	case "", "done", "completed", "canceled", "cancelled":
		return false
	default:
		return true
	}
}

func runtimeTaskUsesIsolation(task RuntimeTask) bool {
	return strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") ||
		strings.TrimSpace(task.WorktreePath) != "" ||
		strings.TrimSpace(task.WorktreeBranch) != ""
}

func firstRuntimeArtifacts(artifacts []RuntimeArtifact, limit int) []RuntimeArtifact {
	if limit <= 0 || len(artifacts) <= limit {
		return append([]RuntimeArtifact(nil), artifacts...)
	}
	return append([]RuntimeArtifact(nil), artifacts[:limit]...)
}
