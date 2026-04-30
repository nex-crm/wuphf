package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/channel"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/onboarding"
	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/workspace"
)

const BrokerPort = brokeraddr.DefaultPort

// brokerTokenFilePath is the path where the broker writes its auth token on start.
// Tests can redirect this to a temp directory to avoid clobbering the live broker token.
var brokerTokenFilePath = brokeraddr.DefaultTokenFile

const defaultRateLimitRequestsPerWindow = 600
const defaultRateLimitWindow = time.Minute

// Per-agent rate limit. Applies even to authenticated requests that identify
// themselves via the X-WUPHF-Agent header. The threshold is high enough that
// well-behaved agents will never trip it, but low enough that a prompt-injected
// agent stuck in a tool-call loop gets throttled before it burns the budget.
const defaultAgentRateLimitRequestsPerWindow = 1000
const defaultAgentRateLimitWindow = time.Minute

// agentRateLimitHeader is the HTTP header the MCP server sets on every outbound
// broker call so the broker can attribute cost back to the agent. Must match
// the value set by internal/teammcp/server.go authHeaders().
const agentRateLimitHeader = "X-WUPHF-Agent"

// agentStreamBuffer holds recent stdout/stderr lines from a headless agent
// process and fans them out to SSE subscribers in real time.

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
	DueAt         string            `json:"due_at,omitempty"`
	FollowUpAt    string            `json:"follow_up_at,omitempty"`
	ReminderAt    string            `json:"reminder_at,omitempty"`
	RecheckAt     string            `json:"recheck_at,omitempty"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at,omitempty"`
	Answered      *interviewAnswer  `json:"answered,omitempty"`
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

// DM slug helpers (isDM, IsDMSlug, DMSlugFor, DMTargetAgent,
// canonicalDMTargetAgent) moved to broker_dm.go.

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

type ipRateLimitBucket struct {
	timestamps []time.Time
}

// Broker is a lightweight HTTP message broker for the team channel.
// All agent MCP instances connect to this shared broker.
type Broker struct {
	channelStore            *channel.Store
	messages                []channelMessage
	agentIssues             []agentIssueRecord
	members                 []officeMember
	memberIndex             map[string]int // slug → index into members; guarded by mu
	channels                []teamChannel
	sessionMode             string
	oneOnOneAgent           string
	focusMode               bool
	tasks                   []teamTask
	requests                []humanInterview
	actions                 []officeActionLog
	signals                 []officeSignalRecord
	decisions               []officeDecisionRecord
	watchdogs               []watchdogAlert
	scheduler               []schedulerJob
	skills                  []teamSkill
	sharedMemory            map[string]map[string]string // namespace → key → value
	lastTaggedAt            map[string]time.Time         // when each agent was last @mentioned
	lastPaneSnapshot        map[string]string            // last captured pane content per agent (for change detection)
	seenTelegramGroups      map[int64]string             // chat_id -> title, populated by transport
	counter                 int
	notificationSince       string
	insightsSince           string
	pendingInterview        *humanInterview
	usage                   teamUsageState
	externalDelivered       map[string]struct{} // message IDs already queued for external delivery
	messageSubscribers      map[int]chan channelMessage
	actionSubscribers       map[int]chan officeActionLog
	activity                map[string]agentActivitySnapshot
	activitySubscribers     map[int]chan agentActivitySnapshot
	officeSubscribers       map[int]chan officeChangeEvent
	wikiSubscribers         map[int]chan wikiWriteEvent
	notebookSubscribers     map[int]chan notebookWriteEvent
	reviewSubscribers       map[int]chan ReviewStateChangeEvent
	entitySubscribers       map[int]chan EntityBriefSynthesizedEvent
	factSubscribers         map[int]chan EntityFactRecordedEvent
	wikiSectionsSubscribers map[int]chan WikiSectionsUpdatedEvent
	wikiWorker              *WikiWorker
	wikiIndex               *WikiIndex
	wikiExtractor           *Extractor
	wikiDLQ                 *DLQ
	wikiSectionsCache       *wikiSectionsCache
	reviewLog               *ReviewLog
	reviewResolver          ReviewerResolver
	factLog                 *FactLog
	readLog                 *ReadLog
	entityGraph             *EntityGraph
	entitySynthesizer       *EntitySynthesizer
	teamLearningLog         *LearningLog
	playbookSynthesizer     *PlaybookSynthesizer
	pamDispatcher           *PamDispatcher
	scanTracker             *scanStatusTracker
	nextSubscriberID        int
	agentStreams            map[string]*agentStreamBuffer
	mu                      sync.Mutex
	// configMu serializes handleConfig POST reads/writes so concurrent
	// /config calls don't corrupt ~/.wuphf/config.json. config.Save uses
	// os.WriteFile (O_TRUNC) without locking, so two parallel POSTs can
	// produce a truncated/overlaid file.
	configMu           sync.Mutex
	server             *http.Server
	token              string          // shared secret for authenticating requests
	addr               string          // actual listen address (useful when port=0)
	webUIOrigins       []string        // allowed CORS origins for web UI (set by ServeWebUI)
	runtimeProvider    string          // "codex" or "claude" — set by launcher
	packSlug           string          // active agent pack slug ("founding-team", "revops", ...) — set by launcher
	blankSlateLaunch   bool            // start without a saved blueprint and synthesize the first operation
	openclawBridge     *OpenclawBridge // nil until the bridge attaches itself; used by handleOfficeMembers for live add/remove
	generateMemberFn   func(prompt string) (generatedMemberTemplate, error)
	generateChannelFn  func(prompt string) (generatedChannelTemplate, error)
	policies           []officePolicy // active office operating rules
	rateLimitBuckets   map[string]ipRateLimitBucket
	rateLimitWindow    time.Duration
	rateLimitRequests  int
	lastRateLimitPrune time.Time

	// Agent-scoped buckets — applied to authenticated agent traffic even though
	// the IP-scoped bucket above exempts callers with a valid Bearer token. This
	// is the containment for a prompt-injected agent that loops on MCP tools.
	agentRateLimitBuckets   map[string]ipRateLimitBucket
	agentRateLimitWindow    time.Duration
	agentRateLimitRequests  int
	lastAgentRateLimitPrune time.Time
	agentLogRoot            string // override for tests; empty means agent.DefaultTaskLogRoot()

	// nowFn is the clock used by rate-limit logic. nil means time.Now.
	// Inject a fake clock in tests to avoid real-time sleeps.
	nowFn func() time.Time

	stopCh   chan struct{} // closed by Stop(); signals background goroutines to exit
	stopOnce sync.Once

	// Skill compile (Stage A) plumbing. The scanner is lazily constructed on
	// first compile; metrics + flags coordinate concurrent triggers. All four
	// fields are guarded by b.mu except where the metric body uses sync/atomic.
	skillCompileMetrics   SkillCompileMetrics
	skillCompileInflight  bool
	skillCompileCoalesced bool
	skillScanner          *SkillScanner
	// Skill synthesizer (Stage B) plumbing. Same coalesce semantics as
	// Stage A; metrics + counters live on skillCompileMetrics.StageBProposalsTotal.
	skillSynthesizer    *SkillSynthesizer
	skillSynthInflight  bool
	skillSynthCoalesced bool
	// Hermes-style per-agent activity counter (Stage B'). Increments on
	// every agent MCP tool call; resets on team_skill_create / team_skill_patch;
	// fires a "skill_review_nudge" task when the threshold is crossed. Owns
	// its own mutex so it can be hit from the tool-event hot path without
	// blocking on b.mu. Lazily constructed by ensureSkillCounter so tests
	// that never spawn a real agent pay no cost.
	skillCounter *SkillCounter
	// recentlyRejectedSkills holds in-memory snapshots of skills rejected in
	// the last 60s so /skills/reject/undo can restore them. Keyed by undo
	// token. Guarded by b.mu. See skill_crud_endpoints.go for GC semantics.
	recentlyRejectedSkills map[string]rejectedSkillSnapshot

	// upgradeRunInFlight serialises POST /upgrade/run so two parallel
	// clicks (or two browser tabs racing) cannot launch concurrent
	// `npm install` against the same node_modules. npm's own lockfile
	// usually recovers, but we've seen partial-extract corruption when
	// two installs target the same prefix simultaneously — this guard
	// is the cheap belt to npm's suspenders.
	upgradeRunInFlight atomic.Bool

	// statePath is the on-disk broker-state.json path bound at construction.
	// NewBrokerAt(path) sets this directly; NewBroker() resolves
	// defaultBrokerStatePath() once and pins the result. A later-arriving
	// goroutine writing via a stale closure (or a sibling broker built at
	// a different path) cannot retarget this broker's saves.
	statePath string

	// Multi-workspace plumbing. All three are nil until SetWorkspaceOrchestrator /
	// SetLauncherDrainer / SetAdminPauseExitFn wire concrete impls. nil is
	// the expected state on a broker started without multi-workspace
	// support — handlers degrade to 503 (orchestrator) or fall back to
	// os.Exit(0) (exit hook). Lane B owns the orchestrator + Launcher.Drain
	// implementations; this broker only depends on the interfaces in
	// broker_workspaces.go.
	workspaces       workspaceOrchestrator
	launcherDrain    launcherDrainer
	adminPauseExitFn func(int)
}

func stringSliceContainsFold(values []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func parseBrokerTimestamp(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return ts.UTC()
}

// skipBrokerStateLoadOnConstruct gates the auto-load of disk state
// inside NewBrokerAt. Production keeps it false so the CLI resumes from
// disk state. A *_test.go init flips it to true so tests that call
// NewBrokerAt / NewBroker get a fresh broker by default, immune to state
// leaked by prior tests via a shared broker-state.json. Persistence
// tests that want the load call b.loadState() explicitly after
// construction (or use reloadedBroker(t, b)).
var skipBrokerStateLoadOnConstruct = false

// NewBroker constructs a Broker bound to defaultBrokerStatePath() resolved
// at call time. Production code uses this so the CLI resumes from the
// default ~/.wuphf/team/broker-state.json (or its WUPHF_BROKER_STATE_PATH /
// WUPHF_RUNTIME_HOME override). Tests should prefer NewBrokerAt or the
// newTestBroker(t) helper — both pin a per-test path explicitly.
func NewBroker() *Broker {
	return NewBrokerAt(defaultBrokerStatePath())
}

// NewBrokerAt constructs a Broker whose state is persisted to statePath.
// The path is bound at construction time and stored on the Broker, so
// late-arriving goroutines (or sibling brokers built at other paths in
// the same process) cannot retarget this broker's saves. Use this instead
// of NewBroker() everywhere that needs path isolation — notably tests
// that want to pin state under t.TempDir.
//
// Panics on an empty statePath. With "" the broker would silently write
// `.last-good` and `<empty>.tmp.<rand>` files into the process cwd, which
// is the kind of foot-gun that only surfaces in production when a CI
// runner happens to execute from a writable directory.
func NewBrokerAt(statePath string) *Broker {
	if strings.TrimSpace(statePath) == "" {
		panic("team.NewBrokerAt: statePath must not be empty (use defaultBrokerStatePath() if no explicit path)")
	}
	b := &Broker{
		channelStore:        channel.NewStore(),
		token:               generateToken(),
		messageSubscribers:  make(map[int]chan channelMessage),
		actionSubscribers:   make(map[int]chan officeActionLog),
		activity:            make(map[string]agentActivitySnapshot),
		activitySubscribers: make(map[int]chan agentActivitySnapshot),
		officeSubscribers:   make(map[int]chan officeChangeEvent),
		wikiSubscribers:     make(map[int]chan wikiWriteEvent),
		notebookSubscribers: make(map[int]chan notebookWriteEvent),
		reviewSubscribers:   make(map[int]chan ReviewStateChangeEvent),
		entitySubscribers:   make(map[int]chan EntityBriefSynthesizedEvent),
		factSubscribers:     make(map[int]chan EntityFactRecordedEvent),
		agentStreams:        make(map[string]*agentStreamBuffer),
		rateLimitBuckets:    make(map[string]ipRateLimitBucket),
		rateLimitWindow:     defaultRateLimitWindow,
		rateLimitRequests:   defaultRateLimitRequestsPerWindow,

		agentRateLimitBuckets:  make(map[string]ipRateLimitBucket),
		agentRateLimitWindow:   defaultAgentRateLimitWindow,
		agentRateLimitRequests: defaultAgentRateLimitRequestsPerWindow,

		statePath: statePath,
	}
	if !skipBrokerStateLoadOnConstruct {
		_ = b.loadState()
	}
	b.mu.Lock()
	b.ensureDefaultOfficeMembersLocked()
	b.ensureDefaultChannelsLocked()
	b.normalizeLoadedStateLocked()
	b.mu.Unlock()
	b.stopCh = make(chan struct{})
	if activityWatchdogEnabled {
		// Watchdog: reap agents stuck in "active"/"thinking" when the spawn
		// crashed before reaching the idle transition. Stopped via b.stopCh.
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-b.stopCh
			cancel()
		}()
		go b.runActivityWatchdog(ctx)
	}
	return b
}

// Token returns the shared secret that agents must include in requests.
func (b *Broker) Token() string {
	return b.token
}

// Addr returns the actual listen address (e.g. "127.0.0.1:7890").
func (b *Broker) Addr() string {
	return b.addr
}

// ChannelStore returns the channel store for DM type checks and member lookups.
func (b *Broker) ChannelStore() *channel.Store {
	return b.channelStore
}

// Start launches the broker on the configured localhost port.
func (b *Broker) Start() error {
	b.ensureWikiWorker()
	b.ensureWikiSectionsCache()
	b.ensureReviewLog()
	b.ensureEntitySynthesizer()
	b.ensurePlaybookExecutionLog()
	b.ensurePlaybookSynthesizer()
	// PR 8 Lane G: register system-managed crons AFTER review log + wiki
	// worker init so the registry reflects subsystems that are actually up.
	// Registration is idempotent — pre-existing entries keep their
	// IntervalOverride and Enabled choices.
	b.registerSystemCrons()
	b.startReviewExpiryLoop(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		select {
		case <-b.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	b.startMemoryWorkflowReconcilerLoop(ctx)
	if err := b.StartOnPort(brokeraddr.ResolvePort()); err != nil {
		cancel()
		return err
	}
	return nil
}

// WikiReadLog returns the broker's ReadLog under b.mu, matching the pattern
// used by ReviewLog(). Handlers must use this accessor — not b.readLog directly —
// to avoid a data race with ensureWikiWorker's write under b.mu.
func (b *Broker) WikiReadLog() *ReadLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.readLog
}

// ensureWikiWorker moved to broker_wiki_lifecycle.go.

// StartOnPort launches the broker on the given port. Use 0 for an OS-assigned port.
func (b *Broker) StartOnPort(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", b.handleHealth) // no auth — used for liveness checks
	mux.HandleFunc("/version", b.handleVersion)
	mux.HandleFunc("/upgrade-check", b.requireAuth(b.handleUpgradeCheck))
	mux.HandleFunc("/upgrade-changelog", b.requireAuth(b.handleUpgradeChangelog))
	mux.HandleFunc("/upgrade/run", b.requireAuth(b.handleUpgradeRun))
	mux.HandleFunc("/session-mode", b.requireAuth(b.handleSessionMode))
	mux.HandleFunc("/focus-mode", b.requireAuth(b.handleFocusMode))
	mux.HandleFunc("/messages", b.requireAuth(b.handleMessages))
	mux.HandleFunc("/reactions", b.requireAuth(b.handleReactions))
	mux.HandleFunc("/notifications/nex", b.requireAuth(b.handleNexNotifications))
	mux.HandleFunc("/office-members", b.requireAuth(b.handleOfficeMembers))
	mux.HandleFunc("/office-members/generate", b.requireAuth(b.handleGenerateMember))
	mux.HandleFunc("/channels", b.requireAuth(b.handleChannels))
	mux.HandleFunc("/channels/dm", b.requireAuth(b.handleCreateDM))
	mux.HandleFunc("/channels/generate", b.requireAuth(b.handleGenerateChannel))
	mux.HandleFunc("/channel-members", b.requireAuth(b.handleChannelMembers))
	mux.HandleFunc("/members", b.requireAuth(b.handleMembers))
	mux.HandleFunc("/tasks", b.requireAuth(b.handleTasks))
	mux.HandleFunc("/tasks/ack", b.requireAuth(b.handleTaskAck))
	mux.HandleFunc("/tasks/memory-workflow", b.requireAuth(b.handleTaskMemoryWorkflow))
	mux.HandleFunc("/tasks/memory-workflow/reconcile", b.requireAuth(b.handleTaskMemoryWorkflowReconcile))
	mux.HandleFunc("/agent-logs", b.requireAuth(b.handleAgentLogs))
	mux.HandleFunc("/task-plan", b.requireAuth(b.handleTaskPlan))
	mux.HandleFunc("/memory", b.requireAuth(b.handleMemory))
	mux.HandleFunc("/wiki/write", b.requireAuth(b.handleWikiWrite))
	mux.HandleFunc("/wiki/write-human", b.requireAuth(b.handleWikiWriteHuman))
	mux.HandleFunc("/humans", b.requireAuth(b.handleHumans))
	mux.HandleFunc("/wiki/read", b.requireAuth(b.handleWikiRead))
	mux.HandleFunc("/wiki/search", b.requireAuth(b.handleWikiSearch))
	mux.HandleFunc("/wiki/lookup", b.requireAuth(b.handleWikiLookup))
	mux.HandleFunc("/wiki/list", b.requireAuth(b.handleWikiList))
	mux.HandleFunc("/wiki/article", b.requireAuth(b.handleWikiArticle))
	mux.HandleFunc("/wiki/catalog", b.requireAuth(b.handleWikiCatalog))
	mux.HandleFunc("/wiki/audit", b.requireAuth(b.handleWikiAudit))
	mux.HandleFunc("/wiki/sections", b.requireAuth(b.handleWikiSections))
	mux.HandleFunc("/wiki/lint/run", b.requireAuth(b.handleLintRun))
	mux.HandleFunc("/wiki/lint/resolve", b.requireAuth(b.handleLintResolve))
	mux.HandleFunc("/wiki/extract/replay", b.requireAuth(b.handleWikiExtractReplay))
	mux.HandleFunc("/wiki/dlq", b.requireAuth(b.handleWikiDLQ))
	mux.HandleFunc("/notebook/write", b.requireAuth(b.handleNotebookWrite))
	mux.HandleFunc("/notebook/read", b.requireAuth(b.handleNotebookRead))
	mux.HandleFunc("/notebook/list", b.requireAuth(b.handleNotebookList))
	mux.HandleFunc("/notebook/catalog", b.requireAuth(b.handleNotebookCatalog))
	mux.HandleFunc("/notebook/search", b.requireAuth(b.handleNotebookSearch))
	mux.HandleFunc("/notebook/promote", b.requireAuth(b.handleNotebookPromote))
	mux.HandleFunc("/review/list", b.requireAuth(b.handleReviewList))
	mux.HandleFunc("/review/", b.requireAuth(b.handleReviewSubpath))
	mux.HandleFunc("/entity/fact", b.requireAuth(b.handleEntityFact))
	mux.HandleFunc("/entity/brief/synthesize", b.requireAuth(b.handleEntityBriefSynthesize))
	mux.HandleFunc("/entity/facts", b.requireAuth(b.handleEntityFactsList))
	mux.HandleFunc("/entity/briefs", b.requireAuth(b.handleEntityBriefsList))
	mux.HandleFunc("/entity/graph", b.requireAuth(b.handleEntityGraph))
	mux.HandleFunc("/entity/graph/all", b.requireAuth(b.handleEntityGraphAll))
	mux.HandleFunc("/playbook/list", b.requireAuth(b.handlePlaybookList))
	mux.HandleFunc("/playbook/compile", b.requireAuth(b.handlePlaybookCompile))
	mux.HandleFunc("/playbook/execution", b.requireAuth(b.handlePlaybookExecution))
	mux.HandleFunc("/playbook/executions", b.requireAuth(b.handlePlaybookExecutionsList))
	mux.HandleFunc("/playbook/synthesize", b.requireAuth(b.handlePlaybookSynthesize))
	mux.HandleFunc("/playbook/synthesis-status", b.requireAuth(b.handlePlaybookSynthesisStatus))
	mux.HandleFunc("/learning/record", b.requireAuth(b.handleLearningRecord))
	mux.HandleFunc("/learning/search", b.requireAuth(b.handleLearningSearch))
	mux.HandleFunc("/pam/actions", b.requireAuth(b.handlePamActions))
	mux.HandleFunc("/pam/action", b.requireAuth(b.handlePamAction))
	mux.HandleFunc("/scan/start", b.requireAuth(b.handleScanStart))
	mux.HandleFunc("/scan/status", b.requireAuth(b.handleScanStatus))
	mux.HandleFunc("/studio/generate-package", b.requireAuth(b.handleStudioGeneratePackage))
	mux.HandleFunc("/studio/bootstrap-package", b.requireAuth(handleOperationBootstrapPackage))
	mux.HandleFunc("/operations/bootstrap-package", b.requireAuth(handleOperationBootstrapPackage))
	mux.HandleFunc("/studio/run-workflow", b.requireAuth(b.handleStudioRunWorkflow))
	mux.HandleFunc("/requests", b.requireAuth(b.handleRequests))
	mux.HandleFunc("/requests/answer", b.requireAuth(b.handleRequestAnswer))
	mux.HandleFunc("/interview", b.requireAuth(b.handleInterview))
	mux.HandleFunc("/interview/answer", b.requireAuth(b.handleInterviewAnswer))
	mux.HandleFunc("/reset", b.requireAuth(b.handleReset))
	mux.HandleFunc("/reset-dm", b.requireAuth(b.handleResetDM))
	mux.HandleFunc("/usage", b.requireAuth(b.handleUsage))
	mux.HandleFunc("/policies", b.requireAuth(b.handlePolicies))
	mux.HandleFunc("/signals", b.requireAuth(b.handleSignals))
	mux.HandleFunc("/decisions", b.requireAuth(b.handleDecisions))
	mux.HandleFunc("/watchdogs", b.requireAuth(b.handleWatchdogs))
	mux.HandleFunc("/actions", b.requireAuth(b.handleActions))
	mux.HandleFunc("/scheduler", b.requireAuth(b.handleScheduler))
	mux.HandleFunc("/scheduler/", b.requireAuth(b.handleSchedulerSubpath))
	mux.HandleFunc("/skills", b.requireAuth(b.handleSkills))
	// /skills/compile lives ABOVE the wildcard subpath route so the
	// ServeMux longest-match wins for the compile endpoints.
	mux.HandleFunc("/skills/compile", b.requireAuth(b.handlePostSkillCompile))
	mux.HandleFunc("/skills/compile/stats", b.requireAuth(b.handleGetSkillCompileStats))
	mux.HandleFunc("/skills/", b.requireAuth(b.handleSkillsSubpath))
	// GET /commands — slash-command registry mirror so the web composer
	// renders the same command set as the TUI. See broker_commands.go.
	mux.HandleFunc("/commands", b.requireAuth(b.handleCommands))
	mux.HandleFunc("/telegram/groups", b.requireAuth(b.handleTelegramGroups))
	mux.HandleFunc("/bridges", b.requireAuth(b.handleBridge))
	mux.HandleFunc("/queue", b.requireAuth(b.handleQueue))
	mux.HandleFunc("/company", b.requireAuth(b.handleCompany))
	mux.HandleFunc("/config", b.requireAuth(b.handleConfig))
	mux.HandleFunc("/status/local-providers", b.requireAuth(b.handleLocalProvidersStatus))
	mux.HandleFunc("/image-providers", b.requireAuth(b.handleImageProviders))
	mux.HandleFunc("/nex/register", b.requireAuth(b.handleNexRegister))
	mux.HandleFunc("/v1/logs", b.requireAuth(b.handleOTLPLogs))
	mux.HandleFunc("/events", b.handleEvents)
	mux.HandleFunc("/agent-stream/", b.requireAuth(b.handleAgentStream))
	mux.HandleFunc("/agent-tool-event", b.requireAuth(b.handleAgentToolEvent))
	// Multi-workspace routes (broker_workspaces.go). Every route below is
	// wrapped through b.withAuth so the design's "every protected route
	// requires bearer" assertion holds. /admin/pause additionally requires
	// loopback RemoteAddr (defense-in-depth, applied inside the handler).
	mux.HandleFunc("/workspaces/list", b.withAuth(b.handleWorkspacesList))
	mux.HandleFunc("/workspaces/create", b.withAuth(b.handleWorkspacesCreate))
	mux.HandleFunc("/workspaces/switch", b.withAuth(b.handleWorkspacesSwitch))
	mux.HandleFunc("/workspaces/pause", b.withAuth(b.handleWorkspacesPause))
	mux.HandleFunc("/workspaces/resume", b.withAuth(b.handleWorkspacesResume))
	mux.HandleFunc("/workspaces/shred", b.withAuth(b.handleWorkspacesShred))
	mux.HandleFunc("/workspaces/restore", b.withAuth(b.handleWorkspacesRestore))
	mux.HandleFunc("/workspaces/trash", b.withAuth(b.handleWorkspacesTrash))
	mux.HandleFunc("/workspaces/onboarding", b.withAuth(b.handleWorkspacesOnboarding))
	mux.HandleFunc("/admin/pause", b.withAuth(b.handleAdminPause))
	mux.HandleFunc("/web-token", b.handleWebToken)
	// Onboarding: state/progress/complete + prereqs/templates/validate-key + checklist.
	// completeFn posts the first task as a human message and seeds the team.
	onboarding.RegisterRoutes(mux, b.onboardingCompleteFn, b.packSlug, b.requireAuth)
	// Workspace wipes: POST /workspace/reset (narrow) and /workspace/shred (full).
	// After a successful wipe, b.Reset clears live in-memory broker state so the
	// broker stays up without repersisting stale messages back onto disk.
	// Auth-gated via requireAuth because shred permanently deletes state and
	// must not be reachable without the broker token.
	workspace.RegisterRoutesWithOptions(mux, workspace.RouteOptions{
		AuthMiddleware: b.requireAuth,
		ResetRuntime:   b.Reset,
	})

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return err
	}
	b.addr = ln.Addr().String()

	b.server = &http.Server{
		Addr:        addr,
		Handler:     b.corsMiddleware(b.rateLimitMiddleware(mux)),
		ReadTimeout: 5 * time.Second,
		// No WriteTimeout — SSE streams (agent-stream, events) are open-ended.
	}

	// Write token to a well-known path so tests and tools can authenticate.
	// Use /tmp directly (not os.TempDir which varies by OS).
	tokenFile := strings.TrimSpace(brokerTokenFilePath)
	if tokenFile == "" || tokenFile == brokeraddr.DefaultTokenFile {
		tokenFile = brokeraddr.ResolveTokenFile()
	}
	if tokenFile != "" {
		if err := os.WriteFile(tokenFile, []byte(b.token), 0o600); err != nil {
			log.Printf("broker: failed to write token file %s: %v", tokenFile, err)
		}
	}

	go func() {
		_ = b.server.Serve(ln)
	}()
	return nil
}

// Stop shuts down the broker.
func (b *Broker) Stop() {
	if b.stopCh != nil {
		b.stopOnce.Do(func() {
			close(b.stopCh)
		})
	}
	if b.server != nil {
		_ = b.server.Close()
	}
	b.mu.Lock()
	synth := b.entitySynthesizer
	pbSynth := b.playbookSynthesizer
	pamDisp := b.pamDispatcher
	b.mu.Unlock()
	if synth != nil {
		synth.Stop()
	}
	if pbSynth != nil {
		pbSynth.Stop()
	}
	if pamDisp != nil {
		pamDisp.Stop()
	}
}

// handleWebToken returns the broker token to localhost clients without requiring auth.
// This lets the web UI fetch the token to authenticate subsequent API calls.
//
// DNS rebinding: even though the listener binds 127.0.0.1, an attacker's
// DNS record with a short TTL can point rebind.example.com at 127.0.0.1
// after the browser's origin check passes. Go's default mux routes purely
// on path, so without an explicit Host check the response would flow back
// to the attacker's origin. Validate both RemoteAddr AND Host here.

// SSE handlers and the tool-event audit channel (handleEvents,
// handleAgentStream, handleAgentToolEvent, recordAgentToolEvent,
// SkillCounter helpers) moved to broker_sse.go.

// ServeWebUI starts a static file server for the web UI on the given port.
// Returns an error if the port cannot be bound (e.g. already in use).
// Web UI server (ServeWebUI, cacheControlMiddleware, webUIProxyHandler)
// moved to broker_web_proxy.go.

// senderMayAutoPromoteLocked reports whether a `from` value is allowed to have
// its @slug body text auto-promoted into the tagged array. Allowlist shape:
// humans (empty / "you" / "human") and any registered agent slug are allowed;
// synthetic senders ("system", "nex", bridges, automation kinds) are not. A
// denylist would silently let every future synthetic identity leak through.
// Sender is normalized first so case drift ("PM", "Human") matches the
// allowlist the same way channel access does.
// Caller must hold b.mu.
func (b *Broker) senderMayAutoPromoteLocked(from string) bool {
	from = normalizeActorSlug(from)
	switch from {
	case "", "you", "human":
		return true
	}
	return b.findMemberLocked(from) != nil
}

// ExternalQueue returns messages that need to be sent to external surfaces
// for the given provider. Each message is returned at most once.
func (b *Broker) ExternalQueue(provider string) []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.externalDelivered == nil {
		b.externalDelivered = make(map[string]struct{})
	}
	surfaceChannels := make(map[string]struct{})
	for _, ch := range b.channels {
		if ch.Surface != nil && ch.Surface.Provider == provider {
			surfaceChannels[ch.Slug] = struct{}{}
		}
	}
	var out []channelMessage
	for _, msg := range b.messages {
		ch := normalizeChannelSlug(msg.Channel)
		if _, ok := surfaceChannels[ch]; !ok {
			continue
		}
		if _, delivered := b.externalDelivered[msg.ID]; delivered {
			continue
		}
		b.externalDelivered[msg.ID] = struct{}{}
		out = append(out, msg)
	}
	return out
}

// EnsureBridgedMember registers a bridged external agent as an office member
// so it appears in the sidebar and can be @mentioned. Idempotent — calling with
// an existing slug is a no-op. CreatedBy tags the source (e.g. "openclaw") so
// the UI can distinguish bridged agents from built-ins or user-generated ones.
func (b *Broker) EnsureBridgedMember(slug, name, createdBy string) error {
	slug = normalizeChannelSlug(slug)
	if slug == "" {
		return fmt.Errorf("slug required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.findMemberLocked(slug) != nil {
		return nil
	}
	member := officeMember{
		Slug:      slug,
		Name:      strings.TrimSpace(name),
		Role:      "Bridged agent",
		CreatedBy: strings.TrimSpace(createdBy),
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if member.Name == "" {
		member.Name = slug
	}
	applyOfficeMemberDefaults(&member)
	b.members = append(b.members, member)
	// Make sure the bridged agent shows up in #general so @mentions work.
	for i := range b.channels {
		if b.channels[i].Slug == "general" {
			if !containsString(b.channels[i].Members, slug) {
				b.channels[i].Members = append(b.channels[i].Members, slug)
			}
			break
		}
	}
	if err := b.saveLocked(); err != nil {
		return err
	}
	b.publishOfficeChangeLocked(officeChangeEvent{Kind: "member_created", Slug: slug})
	return nil
}

// EnsureDirectChannel opens (or returns) the 1:1 DM channel between the
// default human member and agentSlug. Returns the canonical channel slug
// (pair-sorted via channel.DirectSlug). Safe to call repeatedly; the DM row
// is upserted in both the channel store and the in-memory broker table so
// it shows up in the sidebar and findChannelLocked resolves it.
func (b *Broker) EnsureDirectChannel(agentSlug string) (string, error) {
	agentSlug = normalizeActorSlug(agentSlug)
	if agentSlug == "" {
		return "", fmt.Errorf("agent slug required")
	}
	if b.channelStore == nil {
		return "", fmt.Errorf("channel store not initialized")
	}
	ch, err := b.channelStore.GetOrCreateDirect("human", agentSlug)
	if err != nil {
		return "", fmt.Errorf("channel store GetOrCreateDirect: %w", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.findChannelLocked(ch.Slug) == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		b.channels = append(b.channels, teamChannel{
			Slug:        ch.Slug,
			Name:        ch.Slug,
			Type:        "dm",
			Description: "Direct messages with " + agentSlug,
			Members:     []string{"human", agentSlug},
			CreatedBy:   "wuphf",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		if err := b.saveLocked(); err != nil {
			return "", err
		}
	}
	return ch.Slug, nil
}

// PostInboundSurfaceMessage posts a message from an external surface into the broker channel.
func (b *Broker) PostInboundSurfaceMessage(from, channel, content, provider string) (channelMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		return channelMessage{}, fmt.Errorf("channel required for surface message")
	}
	if b.findChannelLocked(channel) == nil {
		if IsDMSlug(channel) {
			if dm := b.ensureDMConversationLocked(channel); dm != nil {
				channel = dm.Slug
			}
		} else {
			return channelMessage{}, fmt.Errorf("channel not found: %s", channel)
		}
	}
	b.counter++
	msg := channelMessage{
		ID:          fmt.Sprintf("msg-%d", b.counter),
		From:        from,
		Channel:     channel,
		Kind:        "surface",
		Source:      provider,
		SourceLabel: provider,
		Content:     strings.TrimSpace(content),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)
	// Mark as already delivered so it doesn't bounce back to the same surface
	if b.externalDelivered == nil {
		b.externalDelivered = make(map[string]struct{})
	}
	b.externalDelivered[msg.ID] = struct{}{}
	if err := b.saveLocked(); err != nil {
		return channelMessage{}, err
	}
	return msg, nil
}

func (b *Broker) Reset() {
	b.mu.Lock()
	mode := b.sessionMode
	agent := b.oneOnOneAgent
	b.messages = nil
	b.agentIssues = nil
	b.members = defaultOfficeMembers()
	b.channels = defaultTeamChannels()
	b.sessionMode = mode
	b.oneOnOneAgent = agent
	b.tasks = nil
	b.requests = nil
	b.actions = nil
	b.signals = nil
	b.decisions = nil
	b.watchdogs = nil
	b.policies = nil
	b.scheduler = nil
	b.pendingInterview = nil
	b.activity = make(map[string]agentActivitySnapshot)
	b.counter = 0
	b.notificationSince = ""
	b.insightsSince = ""
	b.usage = teamUsageState{Agents: make(map[string]usageTotals)}
	b.normalizeLoadedStateLocked()
	// Restore session preferences after normalization: Reset() clears content but
	// should not re-validate the user's explicit 1:1 agent choice against the
	// current default member list (which may differ from the active pack).
	b.sessionMode = mode
	b.oneOnOneAgent = agent
	_ = b.saveLocked()
	_ = os.Remove(b.stateSnapshotPath())
	b.mu.Unlock()
}

// State persistence (defaultBrokerStatePath, stateSnapshotPath,
// loadBrokerStateFile, brokerStateActivityScore, brokerStateShouldSnapshot,
// loadState, saveLocked, atomicWriteFile) moved to broker_persistence.go.

// Defaults + state normalization (defaultOfficeMembers, defaultTeamChannels,
// repoRootForRuntimeDefaults, isDefaultChannelState, isDefaultOfficeMemberState,
// normalizeChannelSlug, normalizeActorSlug, ensureDefaultChannelsLocked,
// ensureDefaultOfficeMembersLocked, normalizeLoadedStateLocked,
// reconcileOrphanedBlockedTasksLocked) moved to broker_defaults.go.

func (b *Broker) SessionModeState() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sessionMode, b.oneOnOneAgent
}

func (b *Broker) SetSessionMode(mode, agent string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sessionMode = NormalizeSessionMode(mode)
	b.oneOnOneAgent = NormalizeOneOnOneAgent(agent)
	if b.findMemberLocked(b.oneOnOneAgent) == nil {
		b.oneOnOneAgent = DefaultOneOnOneAgent
	}
	return b.saveLocked()
}

func (b *Broker) SetFocusMode(enabled bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.focusMode = enabled
	return b.saveLocked()
}

func (b *Broker) SetGenerateMemberFn(fn func(string) (generatedMemberTemplate, error)) {
	b.generateMemberFn = fn
}

func (b *Broker) SetGenerateChannelFn(fn func(string) (generatedChannelTemplate, error)) {
	b.generateChannelFn = fn
}

// SetAgentLogRoot overrides where /agent-logs reads task JSONL from.
// Used by tests; production uses agent.DefaultTaskLogRoot().
func (b *Broker) SetAgentLogRoot(root string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.agentLogRoot = root
}

func (b *Broker) FocusModeEnabled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.focusMode
}

func (b *Broker) findChannelLocked(slug string) *teamChannel {
	slug = normalizeChannelSlug(slug)
	for i := range b.channels {
		if b.channels[i].Slug == slug {
			return &b.channels[i]
		}
	}
	return nil
}

// ensureDMConversationLocked returns the DM conversation for the given slug,
// creating it on the fly if it doesn't exist. Mirrors Slack's conversations.open.
// It delegates creation to channelStore so DM channels have proper types and members.
func (b *Broker) ensureDMConversationLocked(slug string) *teamChannel {
	if ch := b.findChannelLocked(slug); ch != nil {
		return ch
	}
	if !IsDMSlug(slug) {
		return nil
	}
	agentSlug := DMTargetAgent(slug)
	if agentSlug == "" {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	// Register in channelStore for proper type-based DM detection.
	if b.channelStore != nil {
		newSlug := DMSlugFor(agentSlug)
		if _, err := b.channelStore.GetOrCreateDirect("human", agentSlug); err == nil {
			// Update slug in broker to the new deterministic format if different.
			if newSlug != slug {
				slug = newSlug
			}
		}
	}
	b.channels = append(b.channels, teamChannel{
		Slug:        slug,
		Name:        slug,
		Type:        "dm",
		Description: "Direct messages with " + agentSlug,
		Members:     []string{"human", agentSlug},
		CreatedBy:   "wuphf",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	return &b.channels[len(b.channels)-1]
}

func (b *Broker) findMemberLocked(slug string) *officeMember {
	slug = normalizeChannelSlug(slug)
	if len(b.memberIndex) != len(b.members) {
		b.rebuildMemberIndexLocked()
	}
	if i, ok := b.memberIndex[slug]; ok && i < len(b.members) && b.members[i].Slug == slug {
		return &b.members[i]
	}
	return nil
}

// rebuildMemberIndexLocked rebuilds memberIndex from b.members. Callers must
// hold b.mu. Called on load and after any structural mutation (remove, reorder)
// to keep the map in sync with the slice. Appends and in-place updates are
// handled by findMemberLocked's length-check lazy rebuild.
func (b *Broker) rebuildMemberIndexLocked() {
	b.memberIndex = make(map[string]int, len(b.members))
	for i, m := range b.members {
		b.memberIndex[m.Slug] = i
	}
}

// AttachOpenclawBridge wires the OpenClaw bridge into the broker so
// handleOfficeMembers can drive live subscribe/unsubscribe/sessions.create/
// sessions.end calls as members are hired and fired. Called by the launcher
// after StartOpenclawBridgeFromConfig succeeds. Safe to call with nil to
// detach (tests).
func (b *Broker) AttachOpenclawBridge(bridge *OpenclawBridge) {
	b.mu.Lock()
	b.openclawBridge = bridge
	b.mu.Unlock()
}

// openclawBridgeLocked returns the attached bridge pointer. Callers must
// hold b.mu. Kept as a small helper so the field is never read without the
// lock (and so we have one place to note the invariant).
func (b *Broker) openclawBridgeLocked() *OpenclawBridge {
	return b.openclawBridge
}

// SetMemberProvider attaches or replaces the ProviderBinding on the given
// office member and persists broker state. Used by the OpenClaw bootstrap
// migration (moving legacy config.OpenclawBridges onto members) and by the
// handleOfficeMembers update path. Returns an error if the member doesn't
// exist; callers should ensure the member exists first.
func (b *Broker) SetMemberProvider(slug string, binding provider.ProviderBinding) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.findMemberLocked(slug)
	if m == nil {
		return fmt.Errorf("set member provider: unknown slug %q", slug)
	}
	m.Provider = binding
	return b.saveLocked()
}

// MemberProviderBinding returns the per-agent provider binding for slug, or
// the zero value if the member does not exist. Safe to call from outside the
// broker; takes the mutex internally.
func (b *Broker) MemberProviderBinding(slug string) provider.ProviderBinding {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.findMemberLocked(slug)
	if m == nil {
		return provider.ProviderBinding{}
	}
	return m.Provider
}

// MemberProviderKind returns the effective runtime kind for the given slug,
// falling back to the global runtime when the member has no explicit binding.
// Used by the launcher's dispatch switch so each agent can run on its own
// provider (e.g., one Codex agent + one Claude Code agent in the same team).
func (b *Broker) MemberProviderKind(slug string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.findMemberLocked(slug)
	if m == nil {
		return ""
	}
	return m.Provider.Kind
}

// Member construction, text helpers, and channel-access predicates moved to
// broker_member_construction.go, broker_text.go, and broker_channel_access.go.

func usageStateIsZero(state teamUsageState) bool {
	if state.Total.TotalTokens > 0 || state.Total.CostUsd > 0 || state.Total.Requests > 0 {
		return false
	}
	for _, totals := range state.Agents {
		if totals.TotalTokens > 0 || totals.CostUsd > 0 || totals.Requests > 0 {
			return false
		}
	}
	return true
}

func (b *Broker) appendActionLocked(kind, source, channel, actor, summary, relatedID string) {
	b.appendActionWithRefsLocked(kind, source, channel, actor, summary, relatedID, nil, "")
}

func (b *Broker) handleBridge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Actor         string   `json:"actor"`
		SourceChannel string   `json:"source_channel"`
		TargetChannel string   `json:"target_channel"`
		Summary       string   `json:"summary"`
		Tagged        []string `json:"tagged"`
		ReplyTo       string   `json:"reply_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	actor := normalizeActorSlug(body.Actor)
	if actor != "ceo" {
		http.Error(w, "only the CEO can bridge channel context", http.StatusForbidden)
		return
	}
	source := normalizeChannelSlug(body.SourceChannel)
	target := normalizeChannelSlug(body.TargetChannel)
	if source == "" || target == "" {
		http.Error(w, "source_channel and target_channel required", http.StatusBadRequest)
		return
	}
	summary := strings.TrimSpace(body.Summary)
	if summary == "" {
		http.Error(w, "summary required", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	sourceExists := b.findChannelLocked(source) != nil
	targetExists := b.findChannelLocked(target) != nil
	b.mu.Unlock()
	if !sourceExists || !targetExists {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	records, err := b.RecordSignals([]officeSignal{{
		ID:         fmt.Sprintf("bridge:%s:%s:%s", source, target, truncateSummary(strings.ToLower(summary), 48)),
		Source:     "channel_bridge",
		Kind:       "bridge",
		Title:      "Cross-channel bridge",
		Content:    fmt.Sprintf("CEO bridged context from #%s to #%s: %s", source, target, summary),
		Channel:    target,
		Owner:      "ceo",
		Confidence: "explicit",
		Urgency:    "normal",
	}})
	if err != nil {
		http.Error(w, "failed to record bridge signal", http.StatusInternalServerError)
		return
	}
	signalIDs := make([]string, 0, len(records))
	for _, record := range records {
		signalIDs = append(signalIDs, record.ID)
	}
	decision, err := b.RecordDecision(
		"bridge_channel",
		target,
		fmt.Sprintf("CEO bridged context from #%s to #%s.", source, target),
		"Relevant context existed in another channel, so the CEO carried it into this channel explicitly.",
		"ceo",
		signalIDs,
		false,
		false,
	)
	if err != nil {
		http.Error(w, "failed to record bridge decision", http.StatusInternalServerError)
		return
	}
	content := summary + fmt.Sprintf("\n\nCEO bridged this context from #%s to help #%s.", source, target)
	msg, _, err := b.PostAutomationMessage(
		"wuphf",
		target,
		"Bridge from #"+source,
		content,
		decision.ID,
		"ceo_bridge",
		"CEO bridge",
		uniqueSlugs(body.Tagged),
		strings.TrimSpace(body.ReplyTo),
	)
	if err != nil {
		http.Error(w, "failed to persist bridge message", http.StatusInternalServerError)
		return
	}
	if err := b.RecordAction("bridge_channel", "ceo_bridge", target, actor, truncateSummary(summary, 140), msg.ID, signalIDs, decision.ID); err != nil {
		http.Error(w, "failed to persist bridge action", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":          msg.ID,
		"decision_id": decision.ID,
		"signal_ids":  signalIDs,
	})
}

func (b *Broker) NotificationCursor() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.notificationSince
}

func (b *Broker) SetNotificationCursor(cursor string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cursor == "" || cursor == b.notificationSince {
		return nil
	}
	b.notificationSince = cursor
	return b.saveLocked()
}

func (b *Broker) InsightsCursor() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.insightsSince
}

func (b *Broker) SetInsightsCursor(cursor string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cursor == "" || cursor == b.insightsSince {
		return nil
	}
	b.insightsSince = cursor
	return b.saveLocked()
}

// If content is the same as last time, agent is idle — return nothing.
func (b *Broker) capturePaneActivity(slugOverride string) map[string]string {
	result := make(map[string]string)

	type paneCheck struct {
		slug   string
		target string
	}

	var checks []paneCheck
	if slugOverride != "" {
		// 1:1 mode: only check pane 1
		checks = append(checks, paneCheck{slug: slugOverride, target: fmt.Sprintf("%s:team.1", SessionName)})
	} else {
		manifest := company.DefaultManifest()
		loaded, loadErr := company.LoadManifest()
		if loadErr == nil && len(loaded.Members) > 0 {
			manifest = loaded
		}
		for i, agent := range manifest.Members {
			checks = append(checks, paneCheck{
				slug:   agent.Slug,
				target: fmt.Sprintf("wuphf-team:team.%d", i+1),
			})
		}
	}

	b.mu.Lock()
	if b.lastPaneSnapshot == nil {
		b.lastPaneSnapshot = make(map[string]string)
	}
	b.mu.Unlock()

	for _, check := range checks {
		paneOut, err := exec.CommandContext(context.Background(), "tmux", "-L", "wuphf", "capture-pane",
			"-p", "-J",
			"-t", check.target).CombinedOutput()
		if err != nil {
			continue
		}

		content := string(paneOut)

		// Compare with previous snapshot
		b.mu.Lock()
		prev := b.lastPaneSnapshot[check.slug]
		b.lastPaneSnapshot[check.slug] = content
		b.mu.Unlock()

		if content == prev {
			// No change — agent is idle
			continue
		}

		// Content changed — agent is active. Extract last 5 meaningful lines.
		lines := strings.Split(content, "\n")
		var meaningful []string
		for i := len(lines) - 1; i >= 0 && len(meaningful) < 5; i-- {
			trimmed := strings.TrimSpace(lines[i])
			if trimmed == "" {
				continue
			}
			meaningful = append(meaningful, trimmed)
		}
		// Reverse to chronological order
		for i, j := 0, len(meaningful)-1; i < j; i, j = i+1, j-1 {
			meaningful[i], meaningful[j] = meaningful[j], meaningful[i]
		}
		if len(meaningful) > 0 {
			result[check.slug] = strings.Join(meaningful, "\n")
		}
	}
	return result
}

// FormatChannelView returns a clean, Slack-style rendering of recent messages.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
