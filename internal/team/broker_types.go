package team

import (
	"encoding/json"
	"strings"

	"github.com/nex-crm/wuphf/internal/provider"
)

// Wire-shape entity types that the broker persists and serves over
// HTTP. Pulled out of broker.go so a reader can scan the office's
// data model in one file: every JSON-tagged struct the broker reads
// from disk, returns from the API, or stamps into office state lives
// here.
//
// Coupling notes:
//   - brokerState is the persisted snapshot — it composes every other
//     entity type below. Loaded by broker_persistence.go's loadState
//     and written by saveLocked.
//   - usageTotals + teamUsageState track per-agent cost/token aggregates.
//     The "session" subtotal is reset on broker restart; "total" is
//     monotonic across the workspace lifetime.
//   - officeMember.Provider is the per-agent runtime binding consumed
//     by the launcher's dispatch switch (see broker_provider_binding.go).
//
// Methods on these types: TitleOrDefault on humanInterview is the
// only one that lives here — it's a tiny formatter the watchdog
// scheduler uses for stalled-interview announcements. Other behavior
// lives in entity-themed files (broker_messages.go, broker_human.go,
// broker_tasks.go, etc.).

type messageReaction struct {
	Emoji string `json:"emoji"`
	From  string `json:"from"`
}

type channelMessage struct {
	ID          string            `json:"id"`
	From        string            `json:"from"`
	Channel     string            `json:"channel,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Source      string            `json:"source,omitempty"`
	SourceLabel string            `json:"source_label,omitempty"`
	EventID     string            `json:"event_id,omitempty"`
	Title       string            `json:"title,omitempty"`
	Content     string            `json:"content"`
	Tagged      []string          `json:"tagged"`
	ReplyTo     string            `json:"reply_to,omitempty"`
	Timestamp   string            `json:"timestamp"`
	Usage       *messageUsage     `json:"usage,omitempty"`
	Reactions   []messageReaction `json:"reactions,omitempty"`
}

type agentIssueRecord struct {
	ID                string `json:"id"`
	Agent             string `json:"agent"`
	Channel           string `json:"channel"`
	ReplyTo           string `json:"reply_to,omitempty"`
	Detail            string `json:"detail"`
	NormalizedKey     string `json:"normalized_key"`
	Severity          string `json:"severity,omitempty"`
	TaskID            string `json:"task_id,omitempty"`
	SelfHealTaskID    string `json:"self_heal_task_id,omitempty"`
	SelfHealError     string `json:"self_heal_error,omitempty"`
	ApprovalRequestID string `json:"approval_request_id,omitempty"`
	Count             int    `json:"count"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type messageUsage struct {
	InputTokens         int `json:"input_tokens,omitempty"`
	OutputTokens        int `json:"output_tokens,omitempty"`
	CacheReadTokens     int `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int `json:"cache_creation_tokens,omitempty"`
	TotalTokens         int `json:"total_tokens,omitempty"`
}

type interviewOption struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	RequiresText bool   `json:"requires_text,omitempty"`
	TextHint     string `json:"text_hint,omitempty"`
}

type interviewAnswer struct {
	ChoiceID   string `json:"choice_id,omitempty"`
	ChoiceText string `json:"choice_text,omitempty"`
	CustomText string `json:"custom_text,omitempty"`
	AnsweredAt string `json:"answered_at,omitempty"`
}

type humanInterview struct {
	ID            string            `json:"id"`
	Kind          string            `json:"kind,omitempty"`
	Status        string            `json:"status,omitempty"`
	From          string            `json:"from"`
	Channel       string            `json:"channel,omitempty"`
	Title         string            `json:"title,omitempty"`
	Question      string            `json:"question"`
	Context       string            `json:"context,omitempty"`
	Options       []interviewOption `json:"options,omitempty"`
	RecommendedID string            `json:"recommended_id,omitempty"`
	Blocking      bool              `json:"blocking,omitempty"`
	Required      bool              `json:"required,omitempty"`
	Secret        bool              `json:"secret,omitempty"`
	ReplyTo       string            `json:"reply_to,omitempty"`
	// DedupeKey collapses duplicate POSTs with the same key onto the
	// existing active request. Used by the action approval gate so a
	// retry of the same (agent, platform, action_id, connection_key)
	// tuple does not produce a fresh blocking request each time the
	// agent loop reconnects.
	DedupeKey  string           `json:"dedupe_key,omitempty"`
	DueAt      string           `json:"due_at,omitempty"`
	FollowUpAt string           `json:"follow_up_at,omitempty"`
	ReminderAt string           `json:"reminder_at,omitempty"`
	RecheckAt  string           `json:"recheck_at,omitempty"`
	CreatedAt  string           `json:"created_at"`
	UpdatedAt  string           `json:"updated_at,omitempty"`
	Answered   *interviewAnswer `json:"answered,omitempty"`
}

type humanInvite struct {
	ID         string `json:"id"`
	TokenHash  string `json:"token_hash"`
	CreatedAt  string `json:"created_at"`
	ExpiresAt  string `json:"expires_at"`
	AcceptedAt string `json:"accepted_at,omitempty"`
	AcceptedBy string `json:"accepted_by,omitempty"`
	RevokedAt  string `json:"revoked_at,omitempty"`
}

type humanSession struct {
	ID          string `json:"id"`
	TokenHash   string `json:"token_hash"`
	InviteID    string `json:"invite_id"`
	HumanSlug   string `json:"human_slug"`
	DisplayName string `json:"display_name"`
	Device      string `json:"device,omitempty"`
	CreatedAt   string `json:"created_at"`
	ExpiresAt   string `json:"expires_at"`
	RevokedAt   string `json:"revoked_at,omitempty"`
	LastSeenAt  string `json:"last_seen_at,omitempty"`
}

// TitleOrDefault returns req.Title trimmed, or "Request" if empty.
// Used by the watchdog scheduler when announcing a stalled interview.
func (req humanInterview) TitleOrDefault() string {
	if t := strings.TrimSpace(req.Title); t != "" {
		return t
	}
	return "Request"
}

type teamTask struct {
	ID               string          `json:"id"`
	Channel          string          `json:"channel,omitempty"`
	Title            string          `json:"title"`
	Details          string          `json:"details,omitempty"`
	Owner            string          `json:"owner,omitempty"`
	Status           string          `json:"status"`
	CreatedBy        string          `json:"created_by"`
	ThreadID         string          `json:"thread_id,omitempty"`
	TaskType         string          `json:"task_type,omitempty"`
	PipelineID       string          `json:"pipeline_id,omitempty"`
	PipelineStage    string          `json:"pipeline_stage,omitempty"`
	ExecutionMode    string          `json:"execution_mode,omitempty"`
	ReviewState      string          `json:"review_state,omitempty"`
	SourceSignalID   string          `json:"source_signal_id,omitempty"`
	SourceDecisionID string          `json:"source_decision_id,omitempty"`
	WorktreePath     string          `json:"worktree_path,omitempty"`
	WorktreeBranch   string          `json:"worktree_branch,omitempty"`
	DependsOn        []string        `json:"depends_on,omitempty"`
	Blocked          bool            `json:"blocked,omitempty"`
	AckedAt          string          `json:"acked_at,omitempty"`
	DueAt            string          `json:"due_at,omitempty"`
	FollowUpAt       string          `json:"follow_up_at,omitempty"`
	ReminderAt       string          `json:"reminder_at,omitempty"`
	RecheckAt        string          `json:"recheck_at,omitempty"`
	MemoryWorkflow   *MemoryWorkflow `json:"memory_workflow,omitempty"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
	CompletedAt      string          `json:"completed_at,omitempty"`
}

type channelSurface struct {
	Provider    string `json:"provider,omitempty"`
	RemoteID    string `json:"remote_id,omitempty"`
	RemoteTitle string `json:"remote_title,omitempty"`
	Mode        string `json:"mode,omitempty"`
	BotTokenEnv string `json:"bot_token_env,omitempty"`
	WebhookURL  string `json:"webhook_url,omitempty"`
}

type teamChannel struct {
	Slug        string          `json:"slug"`
	Name        string          `json:"name"`
	Type        string          `json:"type,omitempty"` // "channel" (default) or "dm"
	Description string          `json:"description,omitempty"`
	Members     []string        `json:"members,omitempty"`
	Disabled    []string        `json:"disabled,omitempty"`
	Surface     *channelSurface `json:"surface,omitempty"`
	CreatedBy   string          `json:"created_by,omitempty"`
	CreatedAt   string          `json:"created_at,omitempty"`
	UpdatedAt   string          `json:"updated_at,omitempty"`
}

type officeMember struct {
	Slug           string                   `json:"slug"`
	Name           string                   `json:"name"`
	Role           string                   `json:"role,omitempty"`
	Expertise      []string                 `json:"expertise,omitempty"`
	Personality    string                   `json:"personality,omitempty"`
	PermissionMode string                   `json:"permission_mode,omitempty"`
	AllowedTools   []string                 `json:"allowed_tools,omitempty"`
	CreatedBy      string                   `json:"created_by,omitempty"`
	CreatedAt      string                   `json:"created_at,omitempty"`
	BuiltIn        bool                     `json:"built_in,omitempty"`
	Provider       provider.ProviderBinding `json:"provider,omitempty"`
}

type officeActionLog struct {
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

type agentActivitySnapshot struct {
	Slug         string `json:"slug"`
	Status       string `json:"status,omitempty"`
	Activity     string `json:"activity,omitempty"`
	Detail       string `json:"detail,omitempty"`
	LastTime     string `json:"lastTime,omitempty"`
	TotalMs      int64  `json:"totalMs,omitempty"`
	FirstEventMs int64  `json:"firstEventMs,omitempty"`
	FirstTextMs  int64  `json:"firstTextMs,omitempty"`
	FirstToolMs  int64  `json:"firstToolMs,omitempty"`
}

type officeSignalRecord struct {
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

type officeDecisionRecord struct {
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

type watchdogAlert struct {
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

type schedulerJob struct {
	Slug            string `json:"slug"`
	Kind            string `json:"kind,omitempty"`
	Label           string `json:"label"`
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
	Payload         string `json:"payload,omitempty"`
	// PR 8 Lane G: cron registry fields. SystemManaged crons are
	// self-registered at broker startup and surfaced in the Calendar app's
	// System Schedules panel; humans can disable/throttle them but cannot
	// delete them. IntervalOverride lets the human dial the cadence without
	// touching env / config — when non-zero, the run-loop uses it instead
	// of the env-resolved default.
	Enabled          bool   `json:"enabled"`
	IntervalOverride int    `json:"interval_override,omitempty"`
	LastRunStatus    string `json:"last_run_status,omitempty"`
	SystemManaged    bool   `json:"system_managed,omitempty"`
}

type teamSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content"`
	CreatedBy   string `json:"created_by"`
	// SourceArticle is the wiki-relative path of the article that drove
	// this skill (e.g. "team/playbooks/customer-refund.md"). Stage A
	// (article-rooted) compiles populate it; Stage B (signal-derived)
	// synthesised skills legitimately leave it empty. Surfaces as
	// metadata.wuphf.source_articles[0] in the rendered SKILL.md and as
	// source_article in the /skills JSON response.
	SourceArticle       string   `json:"source_article,omitempty"`
	Channel             string   `json:"channel,omitempty"`
	Tags                []string `json:"tags,omitempty"`
	Trigger             string   `json:"trigger,omitempty"`
	WorkflowProvider    string   `json:"workflow_provider,omitempty"`
	WorkflowKey         string   `json:"workflow_key,omitempty"`
	WorkflowDefinition  string   `json:"workflow_definition,omitempty"`
	WorkflowSchedule    string   `json:"workflow_schedule,omitempty"`
	RelayID             string   `json:"relay_id,omitempty"`
	RelayPlatform       string   `json:"relay_platform,omitempty"`
	RelayEventTypes     []string `json:"relay_event_types,omitempty"`
	LastExecutionAt     string   `json:"last_execution_at,omitempty"`
	LastExecutionStatus string   `json:"last_execution_status,omitempty"`
	UsageCount          int      `json:"usage_count"`
	Status              string   `json:"status"`
	DisabledFromStatus  string   `json:"disabled_from_status,omitempty"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
}

type brokerState struct {
	ChannelStore      json.RawMessage              `json:"channel_store,omitempty"`
	Messages          []channelMessage             `json:"messages"`
	AgentIssues       []agentIssueRecord           `json:"agent_issues,omitempty"`
	Members           []officeMember               `json:"members,omitempty"`
	Channels          []teamChannel                `json:"channels,omitempty"`
	SessionMode       string                       `json:"session_mode,omitempty"`
	OneOnOneAgent     string                       `json:"one_on_one_agent,omitempty"`
	FocusMode         bool                         `json:"focus_mode,omitempty"`
	Tasks             []teamTask                   `json:"tasks,omitempty"`
	Requests          []humanInterview             `json:"requests,omitempty"`
	Actions           []officeActionLog            `json:"actions,omitempty"`
	Signals           []officeSignalRecord         `json:"signals,omitempty"`
	Decisions         []officeDecisionRecord       `json:"decisions,omitempty"`
	Watchdogs         []watchdogAlert              `json:"watchdogs,omitempty"`
	Scheduler         []schedulerJob               `json:"scheduler,omitempty"`
	Skills            []teamSkill                  `json:"skills,omitempty"`
	HumanInvites      []humanInvite                `json:"human_invites,omitempty"`
	HumanSessions     []humanSession               `json:"human_sessions,omitempty"`
	SharedMemory      map[string]map[string]string `json:"shared_memory,omitempty"`
	Counter           int                          `json:"counter"`
	NotificationSince string                       `json:"notification_since,omitempty"`
	InsightsSince     string                       `json:"insights_since,omitempty"`
	PendingInterview  *humanInterview              `json:"pending_interview,omitempty"`
	Usage             teamUsageState               `json:"usage,omitempty"`
	Policies          []officePolicy               `json:"policies,omitempty"`
}

type usageTotals struct {
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	CostUsd             float64 `json:"cost_usd"`
	Requests            int     `json:"requests"`
}

type teamUsageState struct {
	Session usageTotals            `json:"session,omitempty"`
	Total   usageTotals            `json:"total"`
	Agents  map[string]usageTotals `json:"agents,omitempty"`
	Since   string                 `json:"since,omitempty"`
}
