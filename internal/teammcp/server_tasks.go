package teammcp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/team"
)

func handleTeamTasks(ctx context.Context, _ *mcp.CallToolRequest, args TeamTasksArgs) (*mcp.CallToolResult, any, error) {
	channel, tasks, err := fetchTeamTasks(ctx, args)
	if err != nil {
		return toolError(err), nil, nil
	}
	if len(tasks) == 0 {
		return textResult("No active team tasks."), nil, nil
	}
	lines := make([]string, 0, len(tasks))
	for _, task := range tasks {
		lines = append(lines, formatTaskRuntimeLine(task))
	}
	status := summarizeTaskRuntime(channel, tasks)
	return textResult(status + "\n\nCurrent team tasks:\n" + strings.Join(lines, "\n")), nil, nil
}

func handleTeamTaskStatus(ctx context.Context, _ *mcp.CallToolRequest, args TeamTasksArgs) (*mcp.CallToolResult, any, error) {
	channel, tasks, err := fetchTeamTasks(ctx, args)
	if err != nil {
		return toolError(err), nil, nil
	}
	return textResult(summarizeTaskRuntime(channel, tasks)), nil, nil
}

func handleTeamRuntimeState(ctx context.Context, _ *mcp.CallToolRequest, args TeamRuntimeStateArgs) (*mcp.CallToolResult, any, error) {
	slug := resolveSlugOptional(args.MySlug)
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	taskChannel, tasks, err := fetchTeamTasks(ctx, TeamTasksArgs{
		Channel:     channel,
		MySlug:      args.MySlug,
		IncludeDone: false,
	})
	if err != nil {
		return toolError(err), nil, nil
	}

	requests, err := fetchRuntimeRequests(ctx, channel, args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	recent, err := fetchRuntimeMessages(ctx, channel, args.MySlug, args.MessageLimit)
	if err != nil {
		return toolError(err), nil, nil
	}

	mode := team.SessionModeOffice
	directAgent := ""
	if isOneOnOneMode() {
		mode = team.SessionModeOneOnOne
		directAgent = team.NormalizeOneOnOneAgent(os.Getenv("WUPHF_ONE_ON_ONE_AGENT"))
	}

	snapshot := team.BuildRuntimeSnapshot(team.RuntimeSnapshotInput{
		Channel:     taskChannel,
		SessionMode: mode,
		DirectAgent: directAgent,
		Tasks:       convertRuntimeTasks(tasks),
		Requests:    requests,
		Recent:      recent,
		Capabilities: team.DetectRuntimeCapabilitiesWithOptions(team.CapabilityProbeOptions{
			IncludeConnections: true,
			ConnectionLimit:    5,
			ConnectionTimeout:  3 * time.Second,
		}),
	})
	return textResult(snapshot.FormatText()), snapshot, nil
}

func handleTeamTask(ctx context.Context, _ *mcp.CallToolRequest, args TeamTaskArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := strings.TrimSpace(args.Channel)
	if channel == "" && strings.TrimSpace(args.ID) != "" {
		channel = findTaskContextByID(ctx, mySlug, args.ID).Channel
	}
	channel = resolveConversationChannel(ctx, mySlug, channel)
	action := strings.TrimSpace(args.Action)
	payload := map[string]any{
		"action":     action,
		"channel":    channel,
		"id":         strings.TrimSpace(args.ID),
		"title":      strings.TrimSpace(args.Title),
		"details":    strings.TrimSpace(args.Details),
		"thread_id":  strings.TrimSpace(args.ThreadID),
		"created_by": mySlug,
	}
	if taskType := strings.TrimSpace(args.TaskType); taskType != "" {
		payload["task_type"] = taskType
	}
	if executionMode := strings.TrimSpace(args.ExecutionMode); executionMode != "" {
		payload["execution_mode"] = executionMode
	}
	if action == "create" && len(args.DependsOn) > 0 {
		payload["depends_on"] = args.DependsOn
	}
	switch action {
	case "claim":
		payload["owner"] = mySlug
	case "assign":
		payload["owner"] = strings.TrimSpace(args.Owner)
	default:
		if owner := strings.TrimSpace(args.Owner); owner != "" {
			payload["owner"] = owner
		}
	}

	var result struct {
		Task struct {
			ID             string `json:"id"`
			Title          string `json:"title"`
			Owner          string `json:"owner"`
			Status         string `json:"status"`
			ExecutionMode  string `json:"execution_mode"`
			WorktreePath   string `json:"worktree_path"`
			WorktreeBranch string `json:"worktree_branch"`
		} `json:"task"`
	}
	if err := brokerPostJSON(ctx, "/tasks", payload, &result); err != nil {
		return toolError(err), nil, nil
	}
	text := fmt.Sprintf("Task %s in #%s is now %s", result.Task.ID, channel, result.Task.Status)
	if result.Task.Owner != "" {
		text += " @" + result.Task.Owner
	}
	if branch := strings.TrimSpace(result.Task.WorktreeBranch); branch != "" {
		text += " · branch " + branch
	}
	if path := strings.TrimSpace(result.Task.WorktreePath); path != "" {
		text += " · working_directory " + path
	}
	text += " — " + result.Task.Title
	return textResult(text), nil, nil
}

func fetchTeamTasks(ctx context.Context, args TeamTasksArgs) (string, []brokerTaskSummary, error) {
	mySlug := strings.TrimSpace(resolveSlugOptional(args.MySlug))
	channel := resolveConversationChannel(ctx, mySlug, args.Channel)
	values := url.Values{}
	values.Set("channel", channel)
	if mySlug != "" {
		values.Set("viewer_slug", mySlug)
		values.Set("my_slug", mySlug)
	}
	if args.IncludeDone {
		values.Set("include_done", "true")
	}
	var result brokerTasksResponse
	path := "/tasks"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return "", nil, err
	}
	return channel, result.Tasks, nil
}

func summarizeTaskRuntime(channel string, tasks []brokerTaskSummary) string {
	if len(tasks) == 0 {
		return "No active team tasks."
	}

	running := 0
	isolated := 0
	reviewing := 0
	for _, task := range tasks {
		if taskCountsAsRunning(task) {
			running++
		}
		if taskUsesIsolation(task) {
			isolated++
		}
		if strings.TrimSpace(task.ReviewState) != "" && task.ReviewState != "not_required" && task.ReviewState != "approved" {
			reviewing++
		}
	}

	lines := []string{
		fmt.Sprintf("Team task status in #%s:", channel),
		fmt.Sprintf("- Running tasks: %d of %d", running, len(tasks)),
		fmt.Sprintf("- Isolated worktrees: %d", isolated),
		fmt.Sprintf("- In review flow: %d", reviewing),
	}

	isolatedTasks := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if !taskUsesIsolation(task) {
			continue
		}
		line := fmt.Sprintf("- %s", task.ID)
		if task.Owner != "" {
			line += " @" + task.Owner
		}
		if branch := strings.TrimSpace(task.WorktreeBranch); branch != "" {
			line += " · branch " + branch
		}
		if path := strings.TrimSpace(task.WorktreePath); path != "" {
			line += " · working_directory " + path
		}
		isolatedTasks = append(isolatedTasks, line)
	}
	if len(isolatedTasks) > 0 {
		lines = append(lines, "", "Isolated task worktrees:")
		lines = append(lines, isolatedTasks...)
		lines = append(lines, "", "For isolated tasks, use the listed worktree path as working_directory for local file and bash tools.")
	}

	return strings.Join(lines, "\n")
}

func fetchRuntimeRequests(ctx context.Context, channel, mySlug string) ([]team.RuntimeRequest, error) {
	values := url.Values{}
	values.Set("channel", channel)
	if viewer := strings.TrimSpace(resolveSlugOptional(mySlug)); viewer != "" {
		values.Set("viewer_slug", viewer)
	}
	var result brokerRequestsResponse
	path := "/requests"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return nil, err
	}

	requests := make([]team.RuntimeRequest, 0, len(result.Requests)+1)
	seen := map[string]bool{}
	if result.Pending != nil {
		req := team.RuntimeRequest{
			ID:       result.Pending.ID,
			Kind:     result.Pending.Kind,
			Title:    result.Pending.Title,
			Question: result.Pending.Question,
			From:     result.Pending.From,
			Blocking: result.Pending.Blocking,
			Required: result.Pending.Required,
			Status:   "pending",
			Channel:  result.Pending.Channel,
			Secret:   result.Pending.Secret,
		}
		requests = append(requests, req)
		seen[req.ID] = true
	}
	for _, req := range result.Requests {
		if seen[req.ID] {
			continue
		}
		requests = append(requests, team.RuntimeRequest{
			ID:       req.ID,
			Kind:     req.Kind,
			Title:    req.Title,
			Question: req.Question,
			From:     req.From,
			Blocking: req.Blocking,
			Required: req.Required,
			Status:   req.Status,
			Channel:  req.Channel,
			Secret:   req.Secret,
		})
	}
	return requests, nil
}

func fetchRuntimeMessages(ctx context.Context, channel, mySlug string, limit int) ([]team.RuntimeMessage, error) {
	values := url.Values{}
	values.Set("channel", channel)
	if slug := strings.TrimSpace(resolveSlugOptional(mySlug)); slug != "" {
		values.Set("my_slug", slug)
		applyAgentMessageScope(values, slug, "agent")
	}
	switch {
	case limit <= 0:
		values.Set("limit", "12")
	case limit > 40:
		values.Set("limit", "40")
	default:
		values.Set("limit", fmt.Sprintf("%d", limit))
	}
	var result brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?"+values.Encode(), &result); err != nil {
		return nil, err
	}
	messages := make([]team.RuntimeMessage, 0, len(result.Messages))
	for i := len(result.Messages) - 1; i >= 0; i-- {
		msg := result.Messages[i]
		messages = append(messages, team.RuntimeMessage{
			ID:        msg.ID,
			From:      msg.From,
			Title:     msg.Title,
			Content:   msg.Content,
			ReplyTo:   msg.ReplyTo,
			Timestamp: msg.Timestamp,
		})
	}
	return messages, nil
}

func convertRuntimeTasks(tasks []brokerTaskSummary) []team.RuntimeTask {
	out := make([]team.RuntimeTask, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, team.RuntimeTask{
			ID:             task.ID,
			Title:          task.Title,
			Owner:          task.Owner,
			Status:         task.Status,
			PipelineStage:  task.PipelineStage,
			ReviewState:    task.ReviewState,
			ExecutionMode:  task.ExecutionMode,
			WorktreePath:   task.WorktreePath,
			WorktreeBranch: task.WorktreeBranch,
			Blocked:        task.Blocked,
		})
	}
	return out
}

func formatTaskRuntimeLine(task brokerTaskSummary) string {
	line := fmt.Sprintf("- %s [%s]", task.ID, task.Status)
	if task.Owner != "" {
		line += " @" + task.Owner
	}
	if task.PipelineStage != "" {
		line += " · stage " + task.PipelineStage
	}
	if task.ReviewState != "" && task.ReviewState != "not_required" {
		line += " · review " + task.ReviewState
	}
	if task.ExecutionMode != "" {
		line += " · " + task.ExecutionMode
	}
	if branch := strings.TrimSpace(task.WorktreeBranch); branch != "" {
		line += " · branch " + branch
	}
	if path := strings.TrimSpace(task.WorktreePath); path != "" {
		line += " · working_directory " + path
	}
	line += " — " + task.Title
	if task.ThreadID != "" {
		line += " ↳ " + task.ThreadID
	}
	if task.Details != "" {
		line += " (" + task.Details + ")"
	}
	return line
}

func taskUsesIsolation(task brokerTaskSummary) bool {
	return strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") ||
		strings.TrimSpace(task.WorktreePath) != "" ||
		strings.TrimSpace(task.WorktreeBranch) != ""
}

func taskCountsAsRunning(task brokerTaskSummary) bool {
	status := strings.ToLower(strings.TrimSpace(task.Status))
	switch status {
	case "", "done", "completed", "canceled", "cancelled":
		return false
	default:
		return true
	}
}

func handleTeamPlan(ctx context.Context, _ *mcp.CallToolRequest, args TeamPlanArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, mySlug, args.Channel)
	if len(args.Tasks) == 0 {
		return toolError(fmt.Errorf("tasks list is empty")), nil, nil
	}

	type planItem struct {
		Title         string   `json:"title"`
		Assignee      string   `json:"assignee"`
		Details       string   `json:"details,omitempty"`
		TaskType      string   `json:"task_type,omitempty"`
		ExecutionMode string   `json:"execution_mode,omitempty"`
		DependsOn     []string `json:"depends_on,omitempty"`
	}
	items := make([]planItem, 0, len(args.Tasks))
	for _, t := range args.Tasks {
		items = append(items, planItem{
			Title:         strings.TrimSpace(t.Title),
			Assignee:      strings.TrimSpace(t.Assignee),
			Details:       strings.TrimSpace(t.Details),
			TaskType:      strings.TrimSpace(t.TaskType),
			ExecutionMode: strings.TrimSpace(t.ExecutionMode),
			DependsOn:     t.DependsOn,
		})
	}

	var result struct {
		Tasks []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Owner   string `json:"owner"`
			Status  string `json:"status"`
			Blocked bool   `json:"blocked"`
		} `json:"tasks"`
	}
	if err := brokerPostJSON(ctx, "/task-plan", map[string]any{
		"channel":    channel,
		"created_by": mySlug,
		"tasks":      items,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}

	lines := make([]string, 0, len(result.Tasks))
	for _, t := range result.Tasks {
		line := fmt.Sprintf("- %s [%s]", t.ID, t.Status)
		if t.Blocked {
			line += " BLOCKED"
		}
		if t.Owner != "" {
			line += " @" + t.Owner
		}
		line += " — " + t.Title
		lines = append(lines, line)
	}
	return textResult(fmt.Sprintf("Created %d tasks in #%s:\n%s", len(result.Tasks), channel, strings.Join(lines, "\n"))), nil, nil
}

func handleTeamTaskAck(ctx context.Context, _ *mcp.CallToolRequest, args TeamTaskAckArgs) (*mcp.CallToolResult, any, error) {
	mySlug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := strings.TrimSpace(args.Channel)
	if channel == "" {
		channel = findTaskContextByID(ctx, mySlug, args.ID).Channel
	}
	channel = resolveConversationChannel(ctx, mySlug, channel)
	taskID := strings.TrimSpace(args.ID)
	if taskID == "" {
		return toolError(fmt.Errorf("task ID is required")), nil, nil
	}
	var result struct {
		Task struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"task"`
	}
	if err := brokerPostJSON(ctx, "/tasks/ack", map[string]any{
		"id":      taskID,
		"channel": channel,
		"slug":    mySlug,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Acknowledged task %s — %s", result.Task.ID, result.Task.Title)), nil, nil
}

func formatTaskSummary(ctx context.Context, mySlug string, channel string) string {
	values := url.Values{}
	values.Set("channel", channel)
	if strings.TrimSpace(mySlug) != "" {
		values.Set("my_slug", mySlug)
	}
	var result brokerTasksResponse
	path := "/tasks"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil || len(result.Tasks) == 0 {
		return "Open tasks: none"
	}
	lines := make([]string, 0, len(result.Tasks))
	for _, task := range result.Tasks {
		line := fmt.Sprintf("- %s [%s]", task.ID, task.Status)
		if task.Owner != "" {
			line += " @" + task.Owner
		}
		line += " — " + task.Title
		lines = append(lines, line)
	}
	return "Open tasks:\n" + strings.Join(lines, "\n")
}
