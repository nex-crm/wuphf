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
	ID          string `json:"id"`
	From        string `json:"from"`
	Channel     string `json:"channel,omitempty"`
	Kind        string `json:"kind,omitempty"`
	Source      string `json:"source,omitempty"`
	SourceLabel string `json:"source_label,omitempty"`
	EventID     string `json:"event_id,omitempty"`
	Title       string `json:"title,omitempty"`
	Content     string `json:"content"`
	// Redacted, RedactionCount, and RedactionReasons describe secret
	// redactions applied before chat content reached storage, APIs, external
	// transports, or future agent context. The raw values are intentionally
	// not retained.
	Redacted         bool              `json:"redacted,omitempty"`
	RedactionCount   int               `json:"redaction_count,omitempty"`
	RedactionReasons []string          `json:"redaction_reasons,omitempty"`
	Tagged           []string          `json:"tagged"`
	ReplyTo          string            `json:"reply_to,omitempty"`
	Timestamp        string            `json:"timestamp"`
	Usage            *messageUsage     `json:"usage,omitempty"`
	Reactions        []messageReaction `json:"reactions,omitempty"`
	// SourceTaskID is the lifecycle-tracked task the sender was
	// actively working on when this message was posted. Empty for
	// free conversation, system messages, and human posts. Used by
	// the agent-context builder to suppress pre-review chatter from
	// agents who are NOT the task's owner or a reviewer — this is
	// what prevents Agent B from working off Agent A's unreviewed
	// in-stream commentary.
	SourceTaskID string `json:"source_task_id,omitempty"`
	// Payload carries the structured card payload for CEO onboarding message
	// kinds (ceo_form_field, ceo_chip_row, ceo_checklist, ceo_team_trim,
	// ceo_scan_chip). Empty for all other kinds. Added in Phase 2.
	// The frontend's kind-dispatcher routes these kinds to the appropriate
	// card renderer component.
	Payload json.RawMessage `json:"payload,omitempty"`
}

type incidentRecord struct {
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
	// Redacted is set true when sanitizeHumanInterview stripped at least one
	// secret from any field. The UI surfaces a badge so humans know the
	// question/context/options they are reading has been partially censored.
	Redacted         bool     `json:"redacted,omitempty"`
	RedactionCount   int      `json:"redaction_count,omitempty"`
	RedactionReasons []string `json:"redaction_reasons,omitempty"`
	DedupeKey        string   `json:"dedupe_key,omitempty"`
	// IssueID links this request back to the parent Issue (team_task)
	// that scopes the work. Populated by team_action_execute via the
	// auto-resolve gate (resolveActionIssue) so every approval card has
	// an Issue to anchor its audit trail to. Empty when the request was
	// not action-execute-driven (e.g. raw team_request from an agent).
	IssueID string `json:"issue_id,omitempty"`
	// Platform and LogoURL anchor integration-scoped cards (connect, and later
	// the external-action approval card) to a concrete toolkit. The web Connect
	// card reads Platform to drive the existing Composio OAuth flow and LogoURL
	// to render the toolkit logo. Empty for non-integration requests.
	Platform string `json:"platform,omitempty"`
	LogoURL  string `json:"logo_url,omitempty"`
	// Action is the structured external-action payload (slice 4b): typed fields
	// plus the masked raw HTTP envelope the approval card renders behind its raw
	// toggle. Nil for non-approval requests and for legacy approvals that only
	// carry the parsed context string.
	Action *approvalActionPayload `json:"action,omitempty"`
	// ConnectionUnverified is set when the action gate could not reach the
	// resolver and degraded to approval-only, so the connection state is
	// unconfirmed. The card surfaces a warning (review LOW #5).
	ConnectionUnverified bool             `json:"connection_unverified,omitempty"`
	DueAt                string           `json:"due_at,omitempty"`
	FollowUpAt           string           `json:"follow_up_at,omitempty"`
	ReminderAt           string           `json:"reminder_at,omitempty"`
	RecheckAt            string           `json:"recheck_at,omitempty"`
	CreatedAt            string           `json:"created_at"`
	UpdatedAt            string           `json:"updated_at,omitempty"`
	Answered             *interviewAnswer `json:"answered,omitempty"`
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
	ID      string `json:"id"`
	Channel string `json:"channel,omitempty"`
	Title   string `json:"title"`
	Details string `json:"details,omitempty"`
	Owner   string `json:"owner,omitempty"`
	// status, reviewState, pipelineStage, and blocked are unexported by
	// design: only the lifecycle transition layer (broker_lifecycle_transition.go)
	// is permitted to write them, and read access from outside the team
	// package goes through the Status()/ReviewState()/PipelineStage()/Blocked()
	// accessor methods. The wire format is preserved verbatim by the custom
	// MarshalJSON / UnmarshalJSON below, so persisted broker state and HTTP
	// responses look identical to pre-Lane-A clients.
	status        string
	CreatedBy     string `json:"created_by"`
	ThreadID      string `json:"thread_id,omitempty"`
	TaskType      string `json:"task_type,omitempty"`
	PipelineID    string `json:"pipeline_id,omitempty"`
	pipelineStage string
	ExecutionMode string `json:"execution_mode,omitempty"`
	// Effort is the per-task reasoning-effort override chosen in the
	// new-task composer. It is model-specific: the composer only offers the
	// levels the selected runtime supports, so the stored value is already
	// validated against that runtime. It is applied at dispatch time —
	// claude-code passes it as `--effort <level>`, codex as
	// `-c model_reasoning_effort=<level>`. Empty means "use the runtime's
	// default effort". The wire key "effort" is stable per the migration plan.
	Effort string `json:"effort,omitempty"`
	// Provider and Model are the per-task LLM runtime override chosen in the
	// new-task composer. The model/provider is a property of the TASK, not the
	// agent: dispatch prefers these over the owner agent's binding, which is
	// now only a soft default. Provider is a runtime kind ("claude-code",
	// "codex", …); Model is the runtime-specific model id. Empty means "fall
	// back to the owner's binding, then the global default". Wire keys
	// "provider"/"model" are additive + stable per the migration plan.
	Provider         string `json:"provider,omitempty"`
	Model            string `json:"model,omitempty"`
	reviewState      string
	SourceSignalID   string   `json:"source_signal_id,omitempty"`
	SourceDecisionID string   `json:"source_decision_id,omitempty"`
	WorktreePath     string   `json:"worktree_path,omitempty"`
	WorktreeBranch   string   `json:"worktree_branch,omitempty"`
	DependsOn        []string `json:"depends_on,omitempty"`
	// BlockedOn is the typed-blocker list that supersedes DependsOn for
	// the multi-agent harness path (Lane A foundation). Entries are task IDs
	// or PR identifiers that must resolve before the task can leave
	// blocked_on_pr_merge. DependsOn is preserved for legacy unblock paths;
	// the extended unblockDependentsLocked sweeps the union of both.
	BlockedOn []string `json:"blocked_on,omitempty"`
	// ParentIssueID is the id of the parent Issue when this task is a
	// sub-issue. Empty for top-level Issues. The FE Issue detail surface
	// shows sub-issues inline under their parent (Linear-style).
	ParentIssueID string `json:"parent_issue_id,omitempty"`
	blocked       bool
	// LifecycleState is the source of truth for the multi-agent control loop.
	// Direct callers must NOT write this field — route through the broker's
	// transition layer (b.transitionLifecycleLocked / b.TransitionLifecycle)
	// so derived fields, the indexed lookup, and self-heal gating all stay
	// in sync.
	LifecycleState LifecycleState `json:"lifecycle_state,omitempty"`
	// Reviewers is the auto-assigned agent slug list resolved by Lane D's
	// reviewer-routing logic at the running → review transition. The CLI
	// (`wuphf task review --invite <slug>`) appends tunnel-human slugs to
	// this same list as additional reviewers. Convergence rule fires
	// when every slug here has emitted a graded review.submitted event.
	// Lane E (indexed inbox + REST handlers) also reads this for auth
	// filtering on /tasks/inbox and /tasks/{id}: human sessions whose
	// slug is not in Reviewers see the task hidden from the inbox and
	// 403 on the packet view. Owner/broker token bypasses the filter.
	Reviewers []string `json:"reviewers,omitempty"`
	// Tags carries spec-level domain tags (e.g. "frontend", "billing")
	// matched against officeMember.Watching.TaskTags during reviewer
	// routing. Lane B's Spec is the authoritative source once it lands;
	// for v1 the task carries its own copy so the routing logic does not
	// need a Lane-B dependency.
	Tags []string `json:"tags,omitempty"`
	// ReviewStartedAt is the RFC3339 timestamp at which the task entered
	// the review state. The convergence sweeper compares this against
	// ReviewTimeoutSeconds (or the package-level default) to decide
	// whether the timeout has elapsed. Empty for tasks not currently in
	// review.
	ReviewStartedAt string `json:"review_started_at,omitempty"`
	// ReviewTimeoutSeconds optionally overrides
	// reviewConvergenceDefaultTimeoutSeconds for this task. Zero or
	// negative means "use the package default". This is the
	// task.review_timeout_seconds knob from the design doc.
	ReviewTimeoutSeconds int             `json:"review_timeout_seconds,omitempty"`
	AckedAt              string          `json:"acked_at,omitempty"`
	DueAt                string          `json:"due_at,omitempty"`
	FollowUpAt           string          `json:"follow_up_at,omitempty"`
	ReminderAt           string          `json:"reminder_at,omitempty"`
	RecheckAt            string          `json:"recheck_at,omitempty"`
	MemoryWorkflow       *MemoryWorkflow `json:"memory_workflow,omitempty"`
	// Verification is the machine-checkable definition of done (U1.1,
	// task_verification.go). When Required, the broker runs the check and
	// blocks complete/approve until it passes. VerificationResult is the
	// stamped outcome of the most recent run (pass or fail), kept so the
	// failure output rides into the owner's next execution packet and the
	// done card can show proof.
	Verification       *TaskVerification       `json:"verification,omitempty"`
	VerificationResult *TaskVerificationResult `json:"verification_result,omitempty"`
	// Definition is the R4 structured intake contract (task_definition.go):
	// goal, deliverables (+format), success criteria, and the tool/context
	// access the work needs. Set by the CEO/human via team_task action=define
	// before the task is staffed; rendered prominently in execution packets.
	// Nil on legacy tasks and on work created before intake defined it.
	Definition *TaskDefinition `json:"definition,omitempty"`
	// Artifact is the delivered-artifact reference for the completion hook
	// (core-loop B1, task_completion_hook.go): a wiki-relative path or
	// visual-artifact id recorded via TaskPostRequest.ArtifactPath. Tasks
	// WITH a Definition cannot reach done until this is set; tasks without
	// one keep legacy behavior. Wire key "artifact" is additive.
	Artifact string `json:"artifact,omitempty"`
	// Ledger is the per-turn journal (task_ledger.go, U2.3): distilled
	// records of what each headless turn on this task said, mutated, and
	// how it ended. Rendered into every participant's packet as the living
	// task brief. Bounded to taskLedgerMaxEntries.
	Ledger      []TaskLedgerEntry `json:"ledger,omitempty"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
	CompletedAt string            `json:"completed_at,omitempty"`
	// System marks a task as a permanent system-owned task that may not
	// be deleted or removed. The "Backup & Migration" task (ID: task-general)
	// is the only current system task; it owns the #general channel so all
	// 141 fallback call sites that post to "general" keep working unchanged.
	System bool `json:"system,omitempty"`
}

// Status returns the persisted status string. Read accessor for callers
// outside the team package; in-package writers must go through the
// lifecycle transition layer.
func (t *teamTask) Status() string {
	if t == nil {
		return ""
	}
	return t.status
}

// ReviewState returns the persisted review state string.
func (t *teamTask) ReviewState() string {
	if t == nil {
		return ""
	}
	return t.reviewState
}

// PipelineStage returns the persisted pipeline stage string.
func (t *teamTask) PipelineStage() string {
	if t == nil {
		return ""
	}
	return t.pipelineStage
}

// Blocked reports whether the task is in a blocked state.
func (t *teamTask) Blocked() bool {
	if t == nil {
		return false
	}
	return t.blocked
}

// teamTaskWire mirrors teamTask's JSON shape with exported field names so
// encoding/json can serialise the unexported derived fields without breaking
// the on-disk and HTTP wire formats. teamTask.MarshalJSON / UnmarshalJSON
// route through this shadow type.
type teamTaskWire struct {
	ID                   string                  `json:"id"`
	Channel              string                  `json:"channel,omitempty"`
	Title                string                  `json:"title"`
	Details              string                  `json:"details,omitempty"`
	Owner                string                  `json:"owner,omitempty"`
	Status               string                  `json:"status"`
	CreatedBy            string                  `json:"created_by"`
	ThreadID             string                  `json:"thread_id,omitempty"`
	TaskType             string                  `json:"task_type,omitempty"`
	PipelineID           string                  `json:"pipeline_id,omitempty"`
	PipelineStage        string                  `json:"pipeline_stage,omitempty"`
	ExecutionMode        string                  `json:"execution_mode,omitempty"`
	Effort               string                  `json:"effort,omitempty"`
	Provider             string                  `json:"provider,omitempty"`
	Model                string                  `json:"model,omitempty"`
	ReviewState          string                  `json:"review_state,omitempty"`
	SourceSignalID       string                  `json:"source_signal_id,omitempty"`
	SourceDecisionID     string                  `json:"source_decision_id,omitempty"`
	WorktreePath         string                  `json:"worktree_path,omitempty"`
	WorktreeBranch       string                  `json:"worktree_branch,omitempty"`
	DependsOn            []string                `json:"depends_on,omitempty"`
	BlockedOn            []string                `json:"blocked_on,omitempty"`
	ParentIssueID        string                  `json:"parent_issue_id,omitempty"`
	Blocked              bool                    `json:"blocked,omitempty"`
	LifecycleState       LifecycleState          `json:"lifecycle_state,omitempty"`
	Reviewers            []string                `json:"reviewers,omitempty"`
	Tags                 []string                `json:"tags,omitempty"`
	ReviewStartedAt      string                  `json:"review_started_at,omitempty"`
	ReviewTimeoutSeconds int                     `json:"review_timeout_seconds,omitempty"`
	AckedAt              string                  `json:"acked_at,omitempty"`
	DueAt                string                  `json:"due_at,omitempty"`
	FollowUpAt           string                  `json:"follow_up_at,omitempty"`
	ReminderAt           string                  `json:"reminder_at,omitempty"`
	RecheckAt            string                  `json:"recheck_at,omitempty"`
	MemoryWorkflow       *MemoryWorkflow         `json:"memory_workflow,omitempty"`
	Verification         *TaskVerification       `json:"verification,omitempty"`
	VerificationResult   *TaskVerificationResult `json:"verification_result,omitempty"`
	Definition           *TaskDefinition         `json:"definition,omitempty"`
	Artifact             string                  `json:"artifact,omitempty"`
	Ledger               []TaskLedgerEntry       `json:"ledger,omitempty"`
	CreatedAt            string                  `json:"created_at"`
	UpdatedAt            string                  `json:"updated_at"`
	CompletedAt          string                  `json:"completed_at,omitempty"`
	System               bool                    `json:"system,omitempty"`
}

// MarshalJSON preserves the pre-Lane-A wire format (status/review_state/
// pipeline_stage/blocked as top-level keys) while keeping the underlying
// fields unexported on the Go struct.
func (t teamTask) MarshalJSON() ([]byte, error) {
	return json.Marshal(teamTaskWire{
		ID:                   t.ID,
		Channel:              t.Channel,
		Title:                t.Title,
		Details:              t.Details,
		Owner:                t.Owner,
		Status:               t.status,
		CreatedBy:            t.CreatedBy,
		ThreadID:             t.ThreadID,
		TaskType:             t.TaskType,
		PipelineID:           t.PipelineID,
		PipelineStage:        t.pipelineStage,
		ExecutionMode:        t.ExecutionMode,
		Effort:               t.Effort,
		Provider:             t.Provider,
		Model:                t.Model,
		ReviewState:          t.reviewState,
		SourceSignalID:       t.SourceSignalID,
		SourceDecisionID:     t.SourceDecisionID,
		WorktreePath:         t.WorktreePath,
		WorktreeBranch:       t.WorktreeBranch,
		DependsOn:            t.DependsOn,
		BlockedOn:            t.BlockedOn,
		ParentIssueID:        t.ParentIssueID,
		Blocked:              t.blocked,
		LifecycleState:       t.LifecycleState,
		Reviewers:            t.Reviewers,
		Tags:                 t.Tags,
		ReviewStartedAt:      t.ReviewStartedAt,
		ReviewTimeoutSeconds: t.ReviewTimeoutSeconds,
		AckedAt:              t.AckedAt,
		DueAt:                t.DueAt,
		FollowUpAt:           t.FollowUpAt,
		ReminderAt:           t.ReminderAt,
		RecheckAt:            t.RecheckAt,
		MemoryWorkflow:       t.MemoryWorkflow,
		Verification:         t.Verification,
		VerificationResult:   t.VerificationResult,
		Definition:           t.Definition,
		Artifact:             t.Artifact,
		Ledger:               t.Ledger,
		CreatedAt:            t.CreatedAt,
		UpdatedAt:            t.UpdatedAt,
		CompletedAt:          t.CompletedAt,
		System:               t.System,
	})
}

// UnmarshalJSON inverts MarshalJSON.
func (t *teamTask) UnmarshalJSON(data []byte) error {
	var w teamTaskWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	t.ID = w.ID
	t.Channel = w.Channel
	t.Title = w.Title
	t.Details = w.Details
	t.Owner = w.Owner
	t.status = w.Status
	t.CreatedBy = w.CreatedBy
	t.ThreadID = w.ThreadID
	t.TaskType = w.TaskType
	t.PipelineID = w.PipelineID
	t.pipelineStage = w.PipelineStage
	t.ExecutionMode = w.ExecutionMode
	t.Effort = w.Effort
	t.Provider = w.Provider
	t.Model = w.Model
	t.reviewState = w.ReviewState
	t.SourceSignalID = w.SourceSignalID
	t.SourceDecisionID = w.SourceDecisionID
	t.WorktreePath = w.WorktreePath
	t.WorktreeBranch = w.WorktreeBranch
	t.DependsOn = w.DependsOn
	t.BlockedOn = w.BlockedOn
	t.ParentIssueID = w.ParentIssueID
	t.Reviewers = w.Reviewers
	t.blocked = w.Blocked
	t.LifecycleState = normalizeLegacyLifecycleStateName(w.LifecycleState)
	t.Tags = w.Tags
	t.ReviewStartedAt = w.ReviewStartedAt
	t.ReviewTimeoutSeconds = w.ReviewTimeoutSeconds
	t.AckedAt = w.AckedAt
	t.DueAt = w.DueAt
	t.FollowUpAt = w.FollowUpAt
	t.ReminderAt = w.ReminderAt
	t.RecheckAt = w.RecheckAt
	t.MemoryWorkflow = w.MemoryWorkflow
	t.Verification = w.Verification
	t.VerificationResult = w.VerificationResult
	t.Definition = w.Definition
	t.Artifact = w.Artifact
	t.Ledger = w.Ledger
	t.CreatedAt = w.CreatedAt
	t.UpdatedAt = w.UpdatedAt
	t.CompletedAt = w.CompletedAt
	t.System = w.System
	return nil
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
	// TaskID links a per-task dedicated channel back to its owning task.
	// Empty for shared channels (e.g. #general) and DM channels.
	// Set to the owning task's ID when createPerTaskChannelLocked creates
	// a task-<id> channel during task creation.
	TaskID string `json:"task_id,omitempty"`
}

type officeMember struct {
	Slug         string                   `json:"slug"`
	Name         string                   `json:"name"`
	Role         string                   `json:"role,omitempty"`
	Expertise    []string                 `json:"expertise,omitempty"`
	Personality  string                   `json:"personality,omitempty"`
	AllowedTools []string                 `json:"allowed_tools,omitempty"`
	CreatedBy    string                   `json:"created_by,omitempty"`
	CreatedAt    string                   `json:"created_at,omitempty"`
	BuiltIn      bool                     `json:"built_in,omitempty"`
	Provider     provider.ProviderBinding `json:"provider,omitempty"`
	// Watching declares the file-glob, wiki-glob, tool-name, and task-tag
	// categories this agent should be auto-assigned as a reviewer for when
	// a task enters review. See broker_reviewer_routing.go (Lane D) for
	// the intersection logic. omitempty keeps existing brokers' wire
	// format unchanged on disk for agents that have not been configured
	// with a watching set.
	Watching Watching `json:"watching,omitempty"`
}

type officeActionLog struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Source     string            `json:"source,omitempty"`
	Channel    string            `json:"channel,omitempty"`
	Actor      string            `json:"actor,omitempty"`
	Summary    string            `json:"summary"`
	RelatedID  string            `json:"related_id,omitempty"`
	SignalIDs  []string          `json:"signal_ids,omitempty"`
	DecisionID string            `json:"decision_id,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  string            `json:"created_at"`
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
	// Kind tags the activity event for the frontend bubble UI:
	//   "routine"   — ordinary in-flight progress
	//   "milestone" — user-visible progress worth highlighting (build/test, error, deploy)
	//   "stuck"     — emitted by the watchdog or stale-while-active reaper
	// The classifier in headless_activity_classifier.go assigns "routine"|"milestone".
	// "stuck" is set only by the broker's reaper / watchdog hooks. Empty means the
	// caller did not classify (treated as "routine" by the frontend).
	Kind string `json:"kind,omitempty"`
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
	// RelatedSkills lists slugs of other skills this skill overlaps with.
	// Populated by the semantic dedup gate and the consolidation endpoint.
	RelatedSkills []string `json:"related_skills,omitempty"`
	// OwnerAgents is the set of agent slugs this skill is assigned to —
	// only assigned skills surface in an agent's AVAILABLE SKILLS prompt
	// block and can be invoked via team_skill_run. Unassigned skills are
	// invisible to that agent (core-loop step 8).
	//
	// Defaults: compilation and seeding auto-assign the whole office
	// roster; the human or CEO narrows the set via the agent Skills tab
	// (/skills/{name}/enable-for and /disable-for).
	OwnerAgents []string `json:"owner_agents,omitempty"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type brokerState struct {
	ChannelStore json.RawMessage  `json:"channel_store,omitempty"`
	Messages     []channelMessage `json:"messages"`
	// Incidents keeps the legacy wire key "agent_issues" so broker-state.json
	// files written before the Issue→Incident rename still load unchanged. The
	// in-memory/Go name is Incident; only the persisted JSON tag is frozen.
	Incidents          []incidentRecord                   `json:"agent_issues,omitempty"`
	Members            []officeMember                     `json:"members,omitempty"`
	Channels           []teamChannel                      `json:"channels,omitempty"`
	SessionMode        string                             `json:"session_mode,omitempty"`
	OneOnOneAgent      string                             `json:"one_on_one_agent,omitempty"`
	FocusMode          bool                               `json:"focus_mode,omitempty"`
	Tasks              []teamTask                         `json:"tasks,omitempty"`
	Requests           []humanInterview                   `json:"requests,omitempty"`
	ApprovalAudit      []ApprovalAuditEntry               `json:"approval_audit,omitempty"`
	ConnectionRegistry map[string]connectionRegistryEntry `json:"connection_registry,omitempty"`
	ActionGrants       []actionGrant                      `json:"action_grants,omitempty"`
	Actions            []officeActionLog                  `json:"actions,omitempty"`
	Signals            []officeSignalRecord               `json:"signals,omitempty"`
	Decisions          []officeDecisionRecord             `json:"decisions,omitempty"`
	Watchdogs          []watchdogAlert                    `json:"watchdogs,omitempty"`
	Scheduler          []schedulerJob                     `json:"scheduler,omitempty"`
	SchedulerRuns      map[string][]schedulerRun          `json:"scheduler_runs,omitempty"`
	SchedulerActivity  map[string][]schedulerActivity     `json:"scheduler_activity,omitempty"`
	SchedulerRevisions map[string][]schedulerRevision     `json:"scheduler_revisions,omitempty"`
	Skills             []teamSkill                        `json:"skills,omitempty"`
	HumanInvites       []humanInvite                      `json:"human_invites,omitempty"`
	HumanSessions      []humanSession                     `json:"human_sessions,omitempty"`
	SharedMemory       map[string]map[string]string       `json:"shared_memory,omitempty"`
	Counter            int                                `json:"counter"`
	NotificationSince  string                             `json:"notification_since,omitempty"`
	InsightsSince      string                             `json:"insights_since,omitempty"`
	PendingInterview   *humanInterview                    `json:"pending_interview,omitempty"`
	Usage              teamUsageState                     `json:"usage,omitempty"`
	Policies           []officePolicy                     `json:"policies,omitempty"`
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
