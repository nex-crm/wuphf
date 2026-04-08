package team

import (
	"strings"
	"time"
)

type SessionMemoryTaskSummary struct {
	ID             string   `json:"id"`
	Title          string   `json:"title,omitempty"`
	Owner          string   `json:"owner,omitempty"`
	Status         string   `json:"status,omitempty"`
	PipelineStage  string   `json:"pipeline_stage,omitempty"`
	ReviewState    string   `json:"review_state,omitempty"`
	ExecutionMode  string   `json:"execution_mode,omitempty"`
	WorktreePath   string   `json:"worktree_path,omitempty"`
	WorktreeBranch string   `json:"worktree_branch,omitempty"`
	ThreadID       string   `json:"thread_id,omitempty"`
	Blocked        bool     `json:"blocked,omitempty"`
	DependsOn      []string `json:"depends_on,omitempty"`
	Summary        string   `json:"summary,omitempty"`
}

type SessionMemoryRequestSummary struct {
	ID            string `json:"id"`
	Kind          string `json:"kind,omitempty"`
	Status        string `json:"status,omitempty"`
	From          string `json:"from,omitempty"`
	Title         string `json:"title,omitempty"`
	Question      string `json:"question,omitempty"`
	Channel       string `json:"channel,omitempty"`
	ReplyTo       string `json:"reply_to,omitempty"`
	RecommendedID string `json:"recommended_id,omitempty"`
	Blocking      bool   `json:"blocking,omitempty"`
	Required      bool   `json:"required,omitempty"`
	Secret        bool   `json:"secret,omitempty"`
	Summary       string `json:"summary,omitempty"`
}

type SessionMemoryActionSummary struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind,omitempty"`
	Source     string   `json:"source,omitempty"`
	Channel    string   `json:"channel,omitempty"`
	Actor      string   `json:"actor,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	RelatedID  string   `json:"related_id,omitempty"`
	SignalIDs  []string `json:"signal_ids,omitempty"`
	DecisionID string   `json:"decision_id,omitempty"`
	CreatedAt  string   `json:"created_at,omitempty"`
}

type SessionMemoryMessageSummary struct {
	ID        string `json:"id"`
	From      string `json:"from,omitempty"`
	Title     string `json:"title,omitempty"`
	Content   string `json:"content,omitempty"`
	ReplyTo   string `json:"reply_to,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Summary   string `json:"summary,omitempty"`
}

type SessionMemorySnapshot struct {
	Version     int                           `json:"version"`
	SessionMode string                        `json:"session_mode,omitempty"`
	DirectAgent string                        `json:"direct_agent,omitempty"`
	GeneratedAt string                        `json:"generated_at,omitempty"`
	Focus       string                        `json:"focus,omitempty"`
	NextSteps   []string                      `json:"next_steps,omitempty"`
	Highlights  []string                      `json:"highlights,omitempty"`
	Tasks       []SessionMemoryTaskSummary    `json:"tasks,omitempty"`
	Requests    []SessionMemoryRequestSummary `json:"requests,omitempty"`
	Actions     []SessionMemoryActionSummary  `json:"actions,omitempty"`
	Messages    []SessionMemoryMessageSummary `json:"messages,omitempty"`
}

type SessionRestoreContext struct {
	Focus              string   `json:"focus,omitempty"`
	NextSteps          []string `json:"next_steps,omitempty"`
	ActiveTaskIDs      []string `json:"active_task_ids,omitempty"`
	PendingRequestIDs  []string `json:"pending_request_ids,omitempty"`
	WorkingDirectories []string `json:"working_directories,omitempty"`
	ThreadIDs          []string `json:"thread_ids,omitempty"`
}

func BuildSessionMemorySnapshot(sessionMode, directAgent string, tasks []RuntimeTask, requests []RuntimeRequest, recent []RuntimeMessage) SessionMemorySnapshot {
	sessionMode = NormalizeSessionMode(sessionMode)
	directAgent = NormalizeOneOnOneAgent(directAgent)
	if sessionMode != SessionModeOneOnOne {
		directAgent = ""
	}
	recovery := buildSessionRecovery(sessionMode, directAgent, tasks, requests, recent)
	snapshot := SessionMemorySnapshot{
		Version:     1,
		SessionMode: sessionMode,
		DirectAgent: directAgent,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Focus:       recovery.Focus,
		NextSteps:   append([]string(nil), recovery.NextSteps...),
		Highlights:  append([]string(nil), recovery.Highlights...),
		Tasks:       make([]SessionMemoryTaskSummary, 0, len(tasks)),
		Requests:    make([]SessionMemoryRequestSummary, 0, len(requests)),
		Messages:    make([]SessionMemoryMessageSummary, 0, len(recent)),
	}
	for _, task := range tasks {
		snapshot.Tasks = append(snapshot.Tasks, SessionMemoryTaskSummary{
			ID:             strings.TrimSpace(task.ID),
			Title:          strings.TrimSpace(task.Title),
			Owner:          strings.TrimSpace(task.Owner),
			Status:         strings.TrimSpace(task.Status),
			PipelineStage:  strings.TrimSpace(task.PipelineStage),
			ReviewState:    strings.TrimSpace(task.ReviewState),
			ExecutionMode:  strings.TrimSpace(task.ExecutionMode),
			WorktreePath:   strings.TrimSpace(task.WorktreePath),
			WorktreeBranch: strings.TrimSpace(task.WorktreeBranch),
			Blocked:        task.Blocked,
			Summary:        summarizeTask(task),
		})
	}
	for _, req := range requests {
		snapshot.Requests = append(snapshot.Requests, SessionMemoryRequestSummary{
			ID:            strings.TrimSpace(req.ID),
			Kind:          strings.TrimSpace(req.Kind),
			Status:        strings.TrimSpace(req.Status),
			From:          strings.TrimSpace(req.From),
			Title:         strings.TrimSpace(req.Title),
			Question:      strings.TrimSpace(req.Question),
			Channel:       strings.TrimSpace(req.Channel),
			RecommendedID: "",
			Blocking:      req.Blocking,
			Required:      req.Required,
			Secret:        req.Secret,
			Summary:       summarizeRequest(req),
		})
	}
	for _, msg := range recent {
		snapshot.Messages = append(snapshot.Messages, SessionMemoryMessageSummary{
			ID:        strings.TrimSpace(msg.ID),
			From:      strings.TrimSpace(msg.From),
			Title:     strings.TrimSpace(msg.Title),
			Content:   strings.TrimSpace(msg.Content),
			ReplyTo:   strings.TrimSpace(msg.ReplyTo),
			Timestamp: strings.TrimSpace(msg.Timestamp),
			Summary:   summarizeRuntimeMessage(msg),
		})
	}
	return snapshot
}

func BuildSessionMemorySnapshotFromOfficeState(sessionMode, directAgent string, tasks []teamTask, requests []humanInterview, actions []officeActionLog, messages []channelMessage) SessionMemorySnapshot {
	runtimeTasks := make([]RuntimeTask, 0, len(tasks))
	taskSummaries := make([]SessionMemoryTaskSummary, 0, len(tasks))
	for _, task := range tasks {
		if !sessionMemoryTaskRelevant(task) {
			continue
		}
		runtimeTasks = append(runtimeTasks, RuntimeTask{
			ID:             strings.TrimSpace(task.ID),
			Title:          strings.TrimSpace(task.Title),
			Owner:          strings.TrimSpace(task.Owner),
			Status:         strings.TrimSpace(task.Status),
			PipelineStage:  strings.TrimSpace(task.PipelineStage),
			ReviewState:    strings.TrimSpace(task.ReviewState),
			ExecutionMode:  strings.TrimSpace(task.ExecutionMode),
			WorktreePath:   strings.TrimSpace(task.WorktreePath),
			WorktreeBranch: strings.TrimSpace(task.WorktreeBranch),
			Blocked:        task.Blocked,
		})
		taskSummaries = append(taskSummaries, SessionMemoryTaskSummary{
			ID:             strings.TrimSpace(task.ID),
			Title:          strings.TrimSpace(task.Title),
			Owner:          strings.TrimSpace(task.Owner),
			Status:         strings.TrimSpace(task.Status),
			PipelineStage:  strings.TrimSpace(task.PipelineStage),
			ReviewState:    strings.TrimSpace(task.ReviewState),
			ExecutionMode:  strings.TrimSpace(task.ExecutionMode),
			WorktreePath:   strings.TrimSpace(task.WorktreePath),
			WorktreeBranch: strings.TrimSpace(task.WorktreeBranch),
			ThreadID:       strings.TrimSpace(task.ThreadID),
			Blocked:        task.Blocked,
			DependsOn:      append([]string(nil), task.DependsOn...),
			Summary:        summarizeTask(RuntimeTask{ID: task.ID, Title: task.Title, Owner: task.Owner, Status: task.Status, PipelineStage: task.PipelineStage, ReviewState: task.ReviewState, ExecutionMode: task.ExecutionMode, WorktreePath: task.WorktreePath, WorktreeBranch: task.WorktreeBranch, Blocked: task.Blocked}),
		})
	}

	runtimeRequests := make([]RuntimeRequest, 0, len(requests))
	requestSummaries := make([]SessionMemoryRequestSummary, 0, len(requests))
	for _, req := range requests {
		if !requestIsActive(req) {
			continue
		}
		runtimeRequests = append(runtimeRequests, RuntimeRequest{
			ID:       strings.TrimSpace(req.ID),
			Kind:     strings.TrimSpace(req.Kind),
			Title:    strings.TrimSpace(req.Title),
			Question: strings.TrimSpace(req.Question),
			From:     strings.TrimSpace(req.From),
			Blocking: req.Blocking,
			Required: req.Required,
			Status:   strings.TrimSpace(req.Status),
			Channel:  strings.TrimSpace(req.Channel),
			Secret:   req.Secret,
		})
		requestSummaries = append(requestSummaries, SessionMemoryRequestSummary{
			ID:            strings.TrimSpace(req.ID),
			Kind:          strings.TrimSpace(req.Kind),
			Status:        strings.TrimSpace(req.Status),
			From:          strings.TrimSpace(req.From),
			Title:         strings.TrimSpace(req.Title),
			Question:      strings.TrimSpace(req.Question),
			Channel:       strings.TrimSpace(req.Channel),
			ReplyTo:       strings.TrimSpace(req.ReplyTo),
			RecommendedID: strings.TrimSpace(req.RecommendedID),
			Blocking:      req.Blocking,
			Required:      req.Required,
			Secret:        req.Secret,
			Summary:       summarizeRequest(RuntimeRequest{ID: req.ID, Kind: req.Kind, Title: req.Title, Question: req.Question, From: req.From, Blocking: req.Blocking, Required: req.Required, Status: req.Status, Channel: req.Channel, Secret: req.Secret}),
		})
	}

	runtimeRecent := make([]RuntimeMessage, 0, 5)
	messageSummaries := make([]SessionMemoryMessageSummary, 0, 5)
	for _, msg := range latestSessionMemoryMessages(messages, 5) {
		runtimeRecent = append(runtimeRecent, RuntimeMessage{
			ID:        strings.TrimSpace(msg.ID),
			From:      strings.TrimSpace(msg.From),
			Title:     strings.TrimSpace(msg.Title),
			Content:   strings.TrimSpace(msg.Content),
			ReplyTo:   strings.TrimSpace(msg.ReplyTo),
			Timestamp: strings.TrimSpace(msg.Timestamp),
		})
		messageSummaries = append(messageSummaries, SessionMemoryMessageSummary{
			ID:        strings.TrimSpace(msg.ID),
			From:      strings.TrimSpace(msg.From),
			Title:     strings.TrimSpace(msg.Title),
			Content:   strings.TrimSpace(msg.Content),
			ReplyTo:   strings.TrimSpace(msg.ReplyTo),
			Timestamp: strings.TrimSpace(msg.Timestamp),
			Summary:   summarizeChannelMessage(msg),
		})
	}

	snapshot := BuildSessionMemorySnapshot(sessionMode, directAgent, runtimeTasks, runtimeRequests, runtimeRecent)
	snapshot.Tasks = taskSummaries
	snapshot.Requests = requestSummaries
	snapshot.Messages = messageSummaries
	snapshot.Actions = latestSessionMemoryActions(actions, 6)
	return snapshot
}

func (s SessionMemorySnapshot) ToRecovery() SessionRecovery {
	return SessionRecovery{
		Focus:      strings.TrimSpace(s.Focus),
		NextSteps:  append([]string(nil), s.NextSteps...),
		Highlights: append([]string(nil), s.Highlights...),
	}
}

func (s SessionMemorySnapshot) RestorationContext() SessionRestoreContext {
	ctx := SessionRestoreContext{
		Focus:     strings.TrimSpace(s.Focus),
		NextSteps: append([]string(nil), s.NextSteps...),
	}
	for _, task := range s.Tasks {
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if status == "" || status == "open" || status == "in_progress" || status == "review" || task.Blocked {
			ctx.ActiveTaskIDs = appendUnique(ctx.ActiveTaskIDs, task.ID)
		}
		if path := strings.TrimSpace(task.WorktreePath); path != "" {
			ctx.WorkingDirectories = appendUnique(ctx.WorkingDirectories, path)
		}
		if threadID := strings.TrimSpace(task.ThreadID); threadID != "" {
			ctx.ThreadIDs = appendUnique(ctx.ThreadIDs, threadID)
		}
	}
	for _, req := range s.Requests {
		status := strings.ToLower(strings.TrimSpace(req.Status))
		if status == "" || status == "pending" || status == "open" {
			if req.Blocking || req.Required {
				ctx.PendingRequestIDs = appendUnique(ctx.PendingRequestIDs, req.ID)
			}
			if replyTo := strings.TrimSpace(req.ReplyTo); replyTo != "" {
				ctx.ThreadIDs = appendUnique(ctx.ThreadIDs, replyTo)
			}
		}
	}
	return ctx
}

func sessionMemoryTaskRelevant(task teamTask) bool {
	status := strings.ToLower(strings.TrimSpace(task.Status))
	if task.Blocked {
		return true
	}
	return status == "" || status == "open" || status == "in_progress" || status == "review"
}

func latestSessionMemoryMessages(messages []channelMessage, limit int) []channelMessage {
	if limit <= 0 {
		return nil
	}
	recent := make([]channelMessage, 0, limit)
	for i := len(messages) - 1; i >= 0 && len(recent) < limit; i-- {
		msg := messages[i]
		if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.Title) == "" {
			continue
		}
		recent = append([]channelMessage{msg}, recent...)
	}
	return recent
}

func latestSessionMemoryActions(actions []officeActionLog, limit int) []SessionMemoryActionSummary {
	if limit <= 0 {
		return nil
	}
	start := 0
	if len(actions) > limit {
		start = len(actions) - limit
	}
	summaries := make([]SessionMemoryActionSummary, 0, len(actions)-start)
	for _, action := range actions[start:] {
		summaries = append(summaries, SessionMemoryActionSummary{
			ID:         strings.TrimSpace(action.ID),
			Kind:       strings.TrimSpace(action.Kind),
			Source:     strings.TrimSpace(action.Source),
			Channel:    strings.TrimSpace(action.Channel),
			Actor:      strings.TrimSpace(action.Actor),
			Summary:    strings.TrimSpace(action.Summary),
			RelatedID:  strings.TrimSpace(action.RelatedID),
			SignalIDs:  append([]string(nil), action.SignalIDs...),
			DecisionID: strings.TrimSpace(action.DecisionID),
			CreatedAt:  strings.TrimSpace(action.CreatedAt),
		})
	}
	return summaries
}

func summarizeRuntimeMessage(msg RuntimeMessage) string {
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		content = strings.TrimSpace(msg.Title)
	}
	if content == "" {
		return ""
	}
	return "@" + strings.TrimSpace(msg.From) + ": " + truncateRecoveryText(content, 120)
}

func summarizeChannelMessage(msg channelMessage) string {
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		content = strings.TrimSpace(msg.Title)
	}
	if content == "" {
		return ""
	}
	return "@" + strings.TrimSpace(msg.From) + ": " + truncateRecoveryText(content, 120)
}
