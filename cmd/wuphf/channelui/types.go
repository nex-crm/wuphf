package channelui

import "strings"

// BrokerReaction is a single emoji reaction on a broker message.
type BrokerReaction struct {
	Emoji string `json:"emoji"`
	From  string `json:"from"`
}

// BrokerMessageUsage counts the LLM tokens (and cache hits) attributed
// to a single message. All fields are optional; zero values mean the
// broker did not report that dimension for the message.
type BrokerMessageUsage struct {
	InputTokens         int `json:"input_tokens,omitempty"`
	OutputTokens        int `json:"output_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
	TotalTokens         int `json:"total_tokens,omitempty"`
}

// BrokerMessage is a single message record as the broker returns it.
// The shape mirrors the broker's JSON contract so it round-trips
// directly through encoding/json without an intermediate DTO.
type BrokerMessage struct {
	ID          string              `json:"id"`
	From        string              `json:"from"`
	Kind        string              `json:"kind,omitempty"`
	Source      string              `json:"source,omitempty"`
	SourceLabel string              `json:"source_label,omitempty"`
	EventID     string              `json:"event_id,omitempty"`
	Title       string              `json:"title,omitempty"`
	Content     string              `json:"content"`
	Tagged      []string            `json:"tagged"`
	ReplyTo     string              `json:"reply_to"`
	Timestamp   string              `json:"timestamp"`
	Usage       *BrokerMessageUsage `json:"usage,omitempty"`
	Reactions   []BrokerReaction    `json:"reactions,omitempty"`
}

// RenderedLine is a single line of pre-styled output destined for the
// main panel. The metadata fields (ThreadID/TaskID/…) let the mouse
// layer route a click on the line back to the underlying entity.
type RenderedLine struct {
	Text        string
	ThreadID    string
	TaskID      string
	RequestID   string
	AgentSlug   string
	PromptValue string
}

// ThreadedMessage decorates a BrokerMessage with the structural context
// the thread-view renderer needs: how deep in the reply chain it sits,
// the human-readable label of its parent, and whether the renderer has
// chosen to collapse its descendants behind a "+N hidden" affordance.
type ThreadedMessage struct {
	Message            BrokerMessage
	Depth              int
	ParentLabel        string
	Collapsed          bool
	HiddenReplies      int
	ThreadParticipants []string
}

// Member is a single channel member's roster entry. The lastMessage /
// lastTime fields drive the inline activity blurb under each sidebar
// row; LiveActivity is the broker-supplied "talking / coding / idle"
// state used by the avatar dot.
type Member struct {
	Slug         string `json:"slug"`
	Name         string `json:"name,omitempty"`
	Role         string `json:"role,omitempty"`
	Disabled     bool   `json:"disabled,omitempty"`
	LastMessage  string `json:"lastMessage"`
	LastTime     string `json:"lastTime"`
	LiveActivity string `json:"liveActivity,omitempty"`
}

// ChannelInfo describes a single broker-side channel. Type "O" is the
// shared office; "D" / "G" are direct or group surfaces (treat as DMs).
type ChannelInfo struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Type        string   `json:"type,omitempty"` // "O", "D", or "G" (channel store types)
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members"`
	Disabled    []string `json:"disabled"`
}

// IsDM reports whether the channel is a direct or group surface (rather
// than the shared office).
func (ch ChannelInfo) IsDM() bool {
	return ch.Type == "D" || ch.Type == "G"
}

// InterviewOption is a single multiple-choice option in an Interview.
// RequiresText flips the UI into a free-form text mode after the option
// is selected, with TextHint shown as the placeholder.
type InterviewOption struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	RequiresText bool   `json:"requires_text,omitempty"`
	TextHint     string `json:"text_hint,omitempty"`
}

// Interview is a human-facing question the team has paused on. The
// scheduling fields (DueAt, FollowUpAt, ReminderAt, RecheckAt) drive the
// calendar view and the "needs your attention" banner.
type Interview struct {
	ID            string            `json:"id"`
	Kind          string            `json:"kind,omitempty"`
	Status        string            `json:"status,omitempty"`
	From          string            `json:"from"`
	Channel       string            `json:"channel"`
	Title         string            `json:"title,omitempty"`
	Question      string            `json:"question"`
	Context       string            `json:"context"`
	Options       []InterviewOption `json:"options"`
	RecommendedID string            `json:"recommended_id"`
	Blocking      bool              `json:"blocking,omitempty"`
	Required      bool              `json:"required,omitempty"`
	Secret        bool              `json:"secret,omitempty"`
	ReplyTo       string            `json:"reply_to,omitempty"`
	CreatedAt     string            `json:"created_at"`
	DueAt         string            `json:"due_at,omitempty"`
	FollowUpAt    string            `json:"follow_up_at,omitempty"`
	ReminderAt    string            `json:"reminder_at,omitempty"`
	RecheckAt     string            `json:"recheck_at,omitempty"`
}

// TitleOrQuestion returns Title if non-blank, else Question. Used by
// list views that want a one-line label per interview.
func (req Interview) TitleOrQuestion() string {
	if strings.TrimSpace(req.Title) != "" {
		return req.Title
	}
	return req.Question
}

// UsageTotals tracks token spend along the broker's accounting axes.
type UsageTotals struct {
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	CostUsd             float64 `json:"cost_usd"`
	Requests            int     `json:"requests"`
}

// UsageState aggregates per-agent and rolled-up totals for the usage
// strip. Session is the current run; Total is lifetime; Agents holds the
// per-agent breakdown the strip renders.
type UsageState struct {
	Session UsageTotals            `json:"session,omitempty"`
	Total   UsageTotals            `json:"total"`
	Agents  map[string]UsageTotals `json:"agents"`
	Since   string                 `json:"since,omitempty"`
}

// Task is a unit of tracked work. Pipeline / execution / review fields
// are populated for tasks that ride a configured pipeline; everything
// else stays blank for free-form work.
type Task struct {
	ID               string `json:"id"`
	Channel          string `json:"channel,omitempty"`
	Title            string `json:"title"`
	Details          string `json:"details,omitempty"`
	Owner            string `json:"owner,omitempty"`
	Status           string `json:"status"`
	CreatedBy        string `json:"created_by"`
	ThreadID         string `json:"thread_id,omitempty"`
	TaskType         string `json:"task_type,omitempty"`
	PipelineID       string `json:"pipeline_id,omitempty"`
	PipelineStage    string `json:"pipeline_stage,omitempty"`
	ExecutionMode    string `json:"execution_mode,omitempty"`
	ReviewState      string `json:"review_state,omitempty"`
	SourceSignalID   string `json:"source_signal_id,omitempty"`
	SourceDecisionID string `json:"source_decision_id,omitempty"`
	WorktreePath     string `json:"worktree_path,omitempty"`
	WorktreeBranch   string `json:"worktree_branch,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
	DueAt            string `json:"due_at,omitempty"`
	FollowUpAt       string `json:"follow_up_at,omitempty"`
	ReminderAt       string `json:"reminder_at,omitempty"`
	RecheckAt        string `json:"recheck_at,omitempty"`
}

// Action describes an external event observed by the broker (a GitHub
// PR opened, an integration ack, a workflow tick, etc.). Surfaces as
// rows in the activity / outbox lanes.
type Action struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Source     string   `json:"source,omitempty"`
	Channel    string   `json:"channel,omitempty"`
	Actor      string   `json:"actor,omitempty"`
	Summary    string   `json:"summary"`
	RelatedID  string   `json:"related_id,omitempty"`
	SignalIDs  []string `json:"signal_ids,omitempty"`
	DecisionID string   `json:"decision_id,omitempty"`
	CreatedAt  string   `json:"created_at"`
}

// Signal is a piece of office intelligence the team has captured —
// usually a directive, observation, or pattern worth reasoning over.
type Signal struct {
	ID            string `json:"id"`
	Source        string `json:"source"`
	SourceRef     string `json:"source_ref,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Title         string `json:"title,omitempty"`
	Content       string `json:"content"`
	Channel       string `json:"channel,omitempty"`
	Owner         string `json:"owner,omitempty"`
	Confidence    string `json:"confidence,omitempty"`
	Urgency       string `json:"urgency,omitempty"`
	DedupeKey     string `json:"dedupe_key,omitempty"`
	RequiresHuman bool   `json:"requires_human,omitempty"`
	Blocking      bool   `json:"blocking,omitempty"`
	CreatedAt     string `json:"created_at"`
}

// Decision records a directive the office has committed to. Reasons
// thread back through SignalIDs for audit.
type Decision struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Channel       string   `json:"channel,omitempty"`
	Summary       string   `json:"summary"`
	Reason        string   `json:"reason,omitempty"`
	Owner         string   `json:"owner,omitempty"`
	SignalIDs     []string `json:"signal_ids,omitempty"`
	RequiresHuman bool     `json:"requires_human,omitempty"`
	Blocking      bool     `json:"blocking,omitempty"`
	CreatedAt     string   `json:"created_at"`
}

// Watchdog is an ongoing alert / monitor the office is tracking.
type Watchdog struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Channel    string `json:"channel,omitempty"`
	TargetType string `json:"target_type,omitempty"`
	TargetID   string `json:"target_id,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Status     string `json:"status,omitempty"`
	Summary    string `json:"summary"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at,omitempty"`
}

// SchedulerJob is a recurring (or scheduled-once) job the broker runs.
// Surfaces in the calendar view and in skill-execution histories.
type SchedulerJob struct {
	Slug            string `json:"slug"`
	Label           string `json:"label"`
	Kind            string `json:"kind,omitempty"`
	TargetType      string `json:"target_type,omitempty"`
	TargetID        string `json:"target_id,omitempty"`
	Channel         string `json:"channel,omitempty"`
	Provider        string `json:"provider,omitempty"`
	ScheduleExpr    string `json:"schedule_expr,omitempty"`
	WorkflowKey     string `json:"workflow_key,omitempty"`
	SkillName       string `json:"skill_name,omitempty"`
	IntervalMinutes int    `json:"interval_minutes"`
	DueAt           string `json:"due_at,omitempty"`
	NextRun         string `json:"next_run,omitempty"`
	LastRun         string `json:"last_run,omitempty"`
	Status          string `json:"status,omitempty"`
}

// Skill is a saved prompt the team has defined and may schedule against
// a workflow / relay. Drives the "skills" pane and the /skill commands.
type Skill struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	Content             string   `json:"content"`
	CreatedBy           string   `json:"created_by"`
	Channel             string   `json:"channel"`
	Tags                []string `json:"tags"`
	Trigger             string   `json:"trigger"`
	WorkflowProvider    string   `json:"workflow_provider"`
	WorkflowKey         string   `json:"workflow_key"`
	WorkflowDefinition  string   `json:"workflow_definition"`
	WorkflowSchedule    string   `json:"workflow_schedule"`
	RelayID             string   `json:"relay_id"`
	RelayPlatform       string   `json:"relay_platform"`
	RelayEventTypes     []string `json:"relay_event_types"`
	LastExecutionAt     string   `json:"last_execution_at"`
	LastExecutionStatus string   `json:"last_execution_status"`
	UsageCount          int      `json:"usage_count"`
	Status              string   `json:"status"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
}
