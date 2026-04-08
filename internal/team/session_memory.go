package team

import (
	"fmt"
	"strings"
)

type SessionRecovery struct {
	Focus      string
	NextSteps  []string
	Highlights []string
}

func BuildSessionRecovery(sessionMode, directAgent string, tasks []RuntimeTask, requests []RuntimeRequest, recent []RuntimeMessage) SessionRecovery {
	return buildSessionRecovery(sessionMode, directAgent, tasks, requests, recent)
}

func buildSessionRecovery(sessionMode, directAgent string, tasks []RuntimeTask, requests []RuntimeRequest, recent []RuntimeMessage) SessionRecovery {
	recovery := SessionRecovery{}

	if req, ok := firstPendingBlockingRuntimeRequest(requests); ok {
		recovery.Focus = summarizeRequest(req)
		recovery.NextSteps = append(recovery.NextSteps, "Answer the blocking human request before moving more work.")
	}
	if recovery.Focus == "" {
		if task, ok := firstRunningTask(tasks); ok {
			recovery.Focus = summarizeTask(task)
		}
	}
	if recovery.Focus == "" {
		if sessionMode == SessionModeOneOnOne {
			recovery.Focus = fmt.Sprintf("Stay focused on the direct session with @%s.", directAgent)
		} else {
			recovery.Focus = "No blocking work detected. Scan the latest channel activity before speaking."
		}
	}

	for _, task := range tasks {
		if !runtimeTaskIsRunning(task) {
			continue
		}
		if runtimeTaskUsesIsolation(task) && strings.TrimSpace(task.WorktreePath) != "" {
			recovery.NextSteps = appendUnique(recovery.NextSteps, fmt.Sprintf("Use working_directory %s for local file and bash tools.", task.WorktreePath))
		}
		if strings.TrimSpace(task.ReviewState) != "" && task.ReviewState != "not_required" && task.ReviewState != "approved" {
			recovery.NextSteps = appendUnique(recovery.NextSteps, fmt.Sprintf("Review flow is active on %s (%s).", task.ID, task.ReviewState))
		}
		if task.Blocked {
			recovery.NextSteps = appendUnique(recovery.NextSteps, fmt.Sprintf("%s is blocked; check dependencies before continuing.", task.ID))
		}
	}

	for _, msg := range recentHighlights(recent) {
		recovery.Highlights = append(recovery.Highlights, msg)
	}

	return recovery
}

func runtimeRequestIsOpen(req RuntimeRequest) bool {
	status := strings.ToLower(strings.TrimSpace(req.Status))
	return status == "" || status == "pending" || status == "open" || status == "draft"
}

func firstPendingBlockingRuntimeRequest(requests []RuntimeRequest) (RuntimeRequest, bool) {
	for _, req := range requests {
		status := strings.ToLower(strings.TrimSpace(req.Status))
		if status != "" && status != "pending" && status != "open" {
			continue
		}
		if req.Blocking || req.Required {
			return req, true
		}
	}
	return RuntimeRequest{}, false
}

func firstRunningTask(tasks []RuntimeTask) (RuntimeTask, bool) {
	for _, task := range tasks {
		if runtimeTaskIsRunning(task) {
			return task, true
		}
	}
	return RuntimeTask{}, false
}

func summarizeRequest(req RuntimeRequest) string {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = strings.TrimSpace(req.Question)
	}
	if title == "" {
		title = "Human decision pending"
	}
	if req.From != "" {
		return fmt.Sprintf("%s from @%s.", title, req.From)
	}
	return title + "."
}

func summarizeTask(task RuntimeTask) string {
	text := strings.TrimSpace(task.Title)
	if text == "" {
		text = task.ID
	}
	if task.Owner != "" {
		text += " owned by @" + task.Owner
	}
	if stage := strings.TrimSpace(task.PipelineStage); stage != "" {
		text += " at stage " + stage
	}
	return text + "."
}

func recentHighlights(recent []RuntimeMessage) []string {
	highlights := make([]string, 0, 3)
	for _, msg := range recent {
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			content = strings.TrimSpace(msg.Title)
		}
		if content == "" {
			continue
		}
		highlights = append(highlights, fmt.Sprintf("@%s: %s", msg.From, truncateRecoveryText(content, 120)))
		if len(highlights) == 3 {
			break
		}
	}
	return highlights
}

func truncateRecoveryText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}

func appendUnique(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}
