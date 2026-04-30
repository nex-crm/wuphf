package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	wuphf "github.com/nex-crm/wuphf"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/channel"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
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

func (ch *teamChannel) isDM() bool {
	return ch.Type == "dm" || IsDMSlug(ch.Slug)
}

// IsDMSlug checks whether a channel slug represents a direct message.
func IsDMSlug(slug string) bool {
	slug = normalizeChannelSlug(slug)
	return strings.HasPrefix(slug, "dm-") || canonicalDMTargetAgent(slug) != ""
}

// DMSlugFor returns the DM channel slug for a given agent.
func DMSlugFor(agentSlug string) string {
	agentSlug = normalizeActorSlug(agentSlug)
	if agentSlug == "" {
		return ""
	}
	return channel.DirectSlug("human", agentSlug)
}

// DMTargetAgent extracts the agent slug from a DM channel slug.
// Returns "" if the slug is not a DM.
func DMTargetAgent(slug string) string {
	slug = normalizeChannelSlug(slug)
	if strings.HasPrefix(slug, "dm-human-") {
		return strings.TrimPrefix(slug, "dm-human-")
	}
	if strings.HasPrefix(slug, "dm-") {
		return strings.TrimPrefix(slug, "dm-")
	}
	return canonicalDMTargetAgent(slug)
}

func canonicalDMTargetAgent(slug string) string {
	parts := strings.Split(normalizeChannelSlug(slug), "__")
	if len(parts) != 2 {
		return ""
	}
	switch {
	case parts[0] == "human" || parts[0] == "you":
		return parts[1]
	case parts[1] == "human" || parts[1] == "you":
		return parts[0]
	default:
		return ""
	}
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

// ensureWikiWorker initializes the markdown-backend wiki worker when the
// resolved memory backend is "markdown". Runs once. Never crashes the
// broker on wiki init failure — the worker is advisory; writes simply fail
// with ErrWorkerStopped until a user runs `wuphf` with git installed.
func (b *Broker) ensureWikiWorker() {
	if config.ResolveMemoryBackend("") != config.MemoryBackendMarkdown {
		return
	}
	b.mu.Lock()
	if b.wikiWorker != nil {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	repo := NewRepo()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := repo.Init(ctx); err != nil {
		log.Printf("wiki: init failed, markdown backend unavailable: %v", err)
		return
	}
	// Belt-and-suspenders: recover any dirty tree from a crashed prior run.
	if err := repo.RecoverDirtyTree(ctx); err != nil {
		log.Printf("wiki: recover-dirty-tree failed: %v", err)
	}
	// Double-fault recovery: if fsck fails, try the backup mirror; otherwise
	// leave the worker un-initialized so writes fail cleanly.
	if err := repo.Fsck(ctx); err != nil {
		log.Printf("wiki: fsck failed (%v); attempting restore from backup", err)
		if restoreErr := repo.RestoreFromBackup(ctx); restoreErr != nil {
			log.Printf("wiki: double-fault (repo corrupt + backup missing): %v", restoreErr)
			return
		}
	}

	idx := NewWikiIndex(repo.Root())

	worker := NewWikiWorkerWithIndex(repo, b, idx)
	worker.Start(context.Background())

	// Wire the extraction loop: artifact commits → extract_entities_lite →
	// WikiIndex. DLQ lives under <wiki>/.dlq/. Extractor failures never
	// fail the commit path — DLQ absorbs everything per §11.13.
	dlq := NewDLQ(repo.Root())
	extractor := NewExtractor(brokerQueryProvider{}, worker, dlq, idx)
	worker.SetExtractor(extractor)

	b.mu.Lock()
	b.wikiWorker = worker
	b.wikiIndex = idx
	b.wikiExtractor = extractor
	b.wikiDLQ = dlq
	b.readLog = NewReadLog(WikiRootDir())
	b.mu.Unlock()

	// Skill status reconciliation: now that the wiki worker is wired,
	// prefer the on-disk SKILL.md frontmatter status over the potentially
	// stale broker-state.json snapshot. This closes the race window where a
	// restart after an archive (or approve) call that missed saveLocked would
	// silently revert the in-memory status.
	b.reconcileSkillStatusFromDisk()

	// Boot reconcile: walk the full wiki tree and populate the index from
	// existing markdown + jsonl. Runs async so it does not delay broker
	// startup. The per-commit ReconcilePath calls keep the index live once
	// the reconcile finishes. If reconcile fails the index is empty but
	// readable — it will self-heal on the next ReconcilePath call.
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := idx.ReconcileFromMarkdown(bgCtx); err != nil {
			log.Printf("wiki_index: boot reconcile failed: %v", err)
		} else {
			log.Printf("wiki_index: boot reconcile complete")
		}
	}()

	// Daily lint cron. The schedule is controlled by WUPHF_LINT_CRON (default
	// "09:00" local time). Empty string disables the cron (useful in tests).
	// The goroutine is cancelled by the background context when the broker
	// shuts down.
	b.startLintCron(context.Background(), idx, worker)

	// Stage A skill-compile cron. Walks the wiki and asks the LLM to extract
	// candidate skills. Cron runs at WUPHF_SKILL_COMPILE_INTERVAL (default
	// 30m); cooldown gates back-to-back ticks via WUPHF_SKILL_COMPILE_COOLDOWN
	// (default 25m). Set the interval to "0" or "disabled" to silence the cron.
	b.startSkillCompileCron(context.Background())
	b.startSkillCompileEventListener(context.Background())

	// Stage B synthesizer: lazily constructed alongside the Stage A scanner so
	// the compile cron drives both passes from a single trigger. Tests can
	// inject a fake via SetSkillSynthesizer.
	b.ensureSkillSynthesizer()
}

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

func (b *Broker) handleEvents(w http.ResponseWriter, r *http.Request) {
	if !b.requestHasBrokerAuth(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	messages, unsubscribeMessages := b.SubscribeMessages(256)
	defer unsubscribeMessages()
	actions, unsubscribeActions := b.SubscribeActions(256)
	defer unsubscribeActions()
	activity, unsubscribeActivity := b.SubscribeActivity(256)
	defer unsubscribeActivity()
	officeChanges, unsubscribeOffice := b.SubscribeOfficeChanges(64)
	defer unsubscribeOffice()
	wikiEvents, unsubscribeWiki := b.SubscribeWikiEvents(64)
	defer unsubscribeWiki()
	notebookEvents, unsubscribeNotebook := b.SubscribeNotebookEvents(64)
	defer unsubscribeNotebook()
	entityEvents, unsubscribeEntity := b.SubscribeEntityBriefEvents(64)
	defer unsubscribeEntity()
	factEvents, unsubscribeFacts := b.SubscribeEntityFactEvents(64)
	defer unsubscribeFacts()
	sectionsEvents, unsubscribeSections := b.SubscribeWikiSectionsUpdated(16)
	defer unsubscribeSections()
	playbookEvents, unsubscribePlaybook := b.SubscribePlaybookExecutionEvents(64)
	defer unsubscribePlaybook()
	playbookSynthEvents, unsubscribePlaybookSynth := b.SubscribePlaybookSynthesizedEvents(64)
	defer unsubscribePlaybookSynth()
	pamStarted, pamDone, pamFailed, unsubscribePam := b.SubscribePamActionEvents(64)
	defer unsubscribePam()

	writeEvent := func(name string, payload any) error {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if err := writeEvent("ready", map[string]string{"status": "ok"}); err != nil {
		return
	}

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-messages:
			if !ok || writeEvent("message", map[string]any{"message": msg}) != nil {
				return
			}
		case action, ok := <-actions:
			if !ok || writeEvent("action", map[string]any{"action": action}) != nil {
				return
			}
		case snapshot, ok := <-activity:
			if !ok || writeEvent("activity", map[string]any{"activity": snapshot}) != nil {
				return
			}
		case evt, ok := <-officeChanges:
			if !ok || writeEvent("office_changed", evt) != nil {
				return
			}
		case evt, ok := <-wikiEvents:
			if !ok || writeEvent("wiki:write", evt) != nil {
				return
			}
		case evt, ok := <-notebookEvents:
			if !ok || writeEvent("notebook:write", evt) != nil {
				return
			}
		case evt, ok := <-entityEvents:
			if !ok || writeEvent("entity:brief_synthesized", evt) != nil {
				return
			}
		case evt, ok := <-factEvents:
			if !ok || writeEvent("entity:fact_recorded", evt) != nil {
				return
			}
		case evt, ok := <-sectionsEvents:
			if !ok || writeEvent(wikiSectionsEventName, evt) != nil {
				return
			}
		case evt, ok := <-playbookEvents:
			if !ok || writeEvent("playbook:execution_recorded", evt) != nil {
				return
			}
		case evt, ok := <-playbookSynthEvents:
			if !ok || writeEvent("playbook:synthesized", evt) != nil {
				return
			}
		case evt, ok := <-pamStarted:
			if !ok || writeEvent("pam:action_started", evt) != nil {
				return
			}
		case evt, ok := <-pamDone:
			if !ok || writeEvent("pam:action_done", evt) != nil {
				return
			}
		case evt, ok := <-pamFailed:
			if !ok || writeEvent("pam:action_failed", evt) != nil {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// handleAgentToolEvent appends a tool-call log line to the agent's stream so
// the per-agent activity panel shows which MCP tool was invoked with what
// arguments. Without this, the stream only shows raw pane-captured stdout —
// useless for agents whose work happens entirely through MCP tool calls.
//
// Body: {"slug":"ceo","phase":"call|result|error","tool":"team_broadcast","args":"...","result":"...","error":"..."}
// Phase is informational; all fields but slug are optional.
func (b *Broker) handleAgentToolEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Slug   string `json:"slug"`
		Phase  string `json:"phase,omitempty"`
		Tool   string `json:"tool,omitempty"`
		Args   string `json:"args,omitempty"`
		Result string `json:"result,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	slug := strings.TrimSpace(body.Slug)
	if slug == "" {
		http.Error(w, "missing slug", http.StatusBadRequest)
		return
	}
	stream := b.AgentStream(slug)
	if stream != nil {
		line := formatAgentToolEvent(body.Phase, body.Tool, body.Args, body.Result, body.Error)
		if line != "" {
			stream.Push(line)
		}
	}

	// Drive the Hermes counter. We only count once per tool call so we
	// branch on phase=="call" — the call/result/error fan-out from a
	// single tool invocation must NOT triple-count. team_skill_create /
	// team_skill_patch reset the tally; everything else increments and
	// may fire a skill_review_nudge task.
	b.recordAgentToolEvent(slug, body.Phase, body.Tool, body.Args)

	w.WriteHeader(http.StatusOK)
}

// recordAgentToolEvent updates the broker's SkillCounter for one
// tool-event payload. It is split out from handleAgentToolEvent so tests
// can drive the counter directly without going through HTTP, and so the
// b.mu acquisition for the nudge task creation is centralized in one
// place.
//
// We only act on phase=="call" — the call/result/error fan-out per tool
// invocation must not triple-count. Empty phase is treated as "call" for
// backward compatibility; older agents that didn't tag the phase still
// only post one event per call.
func (b *Broker) recordAgentToolEvent(slug, phase, tool, args string) {
	slug = strings.TrimSpace(slug)
	tool = strings.TrimSpace(tool)
	if slug == "" || tool == "" {
		return
	}
	phase = strings.TrimSpace(phase)
	if phase != "" && phase != "call" {
		// result / error events from the same call — already counted.
		return
	}
	counter := b.ensureSkillCounter()
	if counter == nil {
		return
	}

	// Skill-authoring tools reset the counter (the agent just codified
	// something). Everything else increments.
	if IsSkillAuthoringTool(tool) {
		counter.Reset(slug)
		return
	}

	summary := skillCounterSummaryFromArgs(tool, args)
	shouldNudge, _ := counter.Increment(slug, tool, summary)
	if !shouldNudge {
		return
	}

	b.mu.Lock()
	taskID, err := b.fireSkillReviewNudgeLocked(slug)
	if err == nil {
		atomic.AddInt64(&b.skillCompileMetrics.CounterNudgesFiredTotal, 1)
		if saveErr := b.saveLocked(); saveErr != nil {
			slog.Warn("skill_counter_nudge_persist_failed",
				"agent", slug, "task_id", taskID, "err", saveErr)
		}
	}
	b.mu.Unlock()
	if err != nil {
		slog.Warn("skill_counter_nudge_create_failed",
			"agent", slug, "err", err)
	}
}

// ensureSkillCounter lazily constructs the SkillCounter. Like the other
// skill-* singletons, the counter is built on first use so tests that
// never trigger an agent tool call pay no cost.
func (b *Broker) ensureSkillCounter() *SkillCounter {
	b.mu.Lock()
	if b.skillCounter != nil {
		c := b.skillCounter
		b.mu.Unlock()
		return c
	}
	c := NewSkillCounter()
	b.skillCounter = c
	b.mu.Unlock()
	return c
}

// SetSkillCounter replaces the broker's counter — used by tests to inject
// a counter with a specific threshold / cooldown / clock without going
// through env vars.
func (b *Broker) SetSkillCounter(c *SkillCounter) {
	b.mu.Lock()
	b.skillCounter = c
	b.mu.Unlock()
}

// skillCounterSummaryFromArgs renders a one-line summary of a tool call
// for the per-agent ring buffer. The args field is a JSON-encoded
// string (as posted by the MCP client), so we don't try to parse it —
// we just trim and truncate. Real human review of the nudge task uses
// the agent's own activity stream for full detail.
func skillCounterSummaryFromArgs(tool, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return tool
	}
	// Collapse whitespace runs so the summary stays single-line.
	args = strings.Join(strings.Fields(args), " ")
	return truncateSummary(args, 120)
}

// formatAgentToolEvent renders one structured audit record for the per-agent
// stream. SSE data lines must stay single-line; JSON encoding preserves exact
// arguments/results while escaping embedded newlines.
func formatAgentToolEvent(phase, tool, args, result, errStr string) string {
	tool = strings.TrimSpace(tool)
	phase = strings.TrimSpace(phase)
	if phase == "" {
		phase = "tool"
	}
	if tool == "" {
		return ""
	}
	payload := map[string]any{
		"type":  "mcp_tool_event",
		"phase": phase,
		"tool":  tool,
	}
	if args != "" {
		payload["arguments"] = decodeToolEventField(args)
	}
	if result != "" {
		payload["result"] = decodeToolEventField(result)
	}
	if errStr != "" {
		payload["error"] = decodeToolEventField(errStr)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

func decodeToolEventField(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
		return decoded
	}
	return raw
}

// handleAgentStream serves a per-agent stdout SSE stream.
// Recent lines are replayed as initial history, then new lines are pushed live.
// Path: /agent-stream/{slug}
func (b *Broker) handleAgentStream(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/agent-stream/")
	if slug == "" {
		http.Error(w, "missing agent slug", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	stream := b.AgentStream(slug)

	// Replay recent history so the client sees context immediately.
	history := stream.recent()
	for _, line := range history {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
			return
		}
	}
	// If no history, send a connected event so the client knows the stream is live.
	if len(history) == 0 {
		if _, err := fmt.Fprintf(w, "data: [connected]\n\n"); err != nil {
			return
		}
	}
	flusher.Flush()

	lines, unsubscribe := stream.subscribe()
	defer unsubscribe()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-lines:
			if !ok {
				return
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", line); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ServeWebUI starts a static file server for the web UI on the given port.
// Returns an error if the port cannot be bound (e.g. already in use).
func (b *Broker) ServeWebUI(port int) error {
	b.webUIOrigins = []string{
		fmt.Sprintf("http://localhost:%d", port),
		fmt.Sprintf("http://127.0.0.1:%d", port),
	}

	// Resolution order for the web UI assets:
	//   1. filesystem web/dist/ (local dev after `npm run build`)
	//   2. embedded FS (single-binary installs via curl | bash)
	exePath, _ := os.Executable()
	webDir := filepath.Join(filepath.Dir(exePath), "web")
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		webDir = "web"
	}
	var fileServer http.Handler
	distDir := filepath.Join(webDir, "dist")
	distIndex := filepath.Join(distDir, "index.html")
	if _, err := os.Stat(distIndex); err == nil {
		// Real Vite build output on disk — use it.
		fileServer = http.FileServer(http.Dir(distDir))
	} else if embeddedFS, ok := wuphf.WebFS(); ok {
		// No on-disk build; use embedded assets.
		fileServer = http.FileServer(http.FS(embeddedFS))
	} else {
		// Nothing available; serve webDir as-is so 404s come from the actual FS.
		fileServer = http.FileServer(http.Dir(webDir))
	}
	mux := http.NewServeMux()
	brokerURL := brokeraddr.ResolveBaseURL()
	if addr := strings.TrimSpace(b.Addr()); addr != "" {
		brokerURL = "http://" + addr
	}
	// Same-origin proxy to the broker for app API routes and onboarding wizard routes.
	// Both are wrapped in webUIRebindGuard: the proxy auto-attaches the broker's
	// Bearer token server-side, so without a Host/RemoteAddr check, a DNS-rebinding
	// attack against an attacker-controlled hostname that resolves to 127.0.0.1
	// would ride the token and control the entire office.
	mux.Handle("/api/", webUIRebindGuard(b.webUIProxyHandler(brokerURL, "/api")))
	mux.Handle("/onboarding/", webUIRebindGuard(b.webUIProxyHandler(brokerURL, "")))
	// Token endpoint — no auth needed, but we require a same-origin loopback request.
	// Otherwise this endpoint leaks the broker bearer to any browser page that
	// can reach the web UI port via DNS rebinding.
	mux.Handle("/api-token", webUIRebindGuard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":      b.token,
			"broker_url": brokerURL,
		})
	})))
	// Cache policy: index.html must be re-fetched every load so users pick up
	// new JS/CSS bundle hashes immediately after an upgrade. Hashed assets
	// under /assets/ are content-addressed and safe to cache aggressively.
	// Without this, users stay pinned to a stale bundle for days because
	// Chrome's heuristic cache revalidates HTML only occasionally.
	// Serve generated images from ~/.wuphf/office/artist/ so the BoardRoom
	// can render them inline via standard markdown <img>. Browsers can't
	// fetch file:// URLs and don't carry the broker's bearer token on
	// <img> requests, so this mount must live on the web-UI port (no auth)
	// rather than the API mux. Path traversal is bounded by http.FileServer
	// + http.Dir; we strip the prefix so requests resolve relative to the
	// artist root.
	artistRoot := imagegenArtistRoot()
	mux.Handle("/artist-files/", http.StripPrefix(
		"/artist-files/",
		http.FileServer(http.Dir(artistRoot)),
	))

	mux.Handle("/", cacheControlMiddleware(fileServer))
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	// noctx: net.Listen is the blocking primitive; the lint rule is meant
	// for HTTP clients. Use ListenConfig.Listen with a Background context
	// so the linter's intent (no caller-controllable cancellation lost) is
	// satisfied without changing the actual lifecycle.
	lc := &net.ListenConfig{}
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		return fmt.Errorf("web UI: listen on %s: %w", addr, err)
	}
	srv := &http.Server{Handler: mux}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("broker web UI proxy: serve on :%d: %v", port, err)
		}
	}()
	return nil
}

// cacheControlMiddleware sets conservative cache headers on the web UI so
// clients always receive fresh HTML and mutable assets, while long-cached
// hashed bundles under /assets/ stay immutable for efficiency.
func cacheControlMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasPrefix(path, "/assets/"):
			// Vite bundles hashed filenames; they never change for a given URL.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			// Everything else (index.html, themes/*.css, favicons that share a
			// stable path) must re-validate on each load so upgrades land
			// immediately.
			w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		}
		next.ServeHTTP(w, r)
	})
}

func (b *Broker) webUIProxyHandler(brokerURL, stripPrefix string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetPath := r.URL.Path
		if stripPrefix != "" {
			targetPath = strings.TrimPrefix(targetPath, stripPrefix)
		}
		if targetPath == "" {
			targetPath = "/"
		}
		target := brokerURL + targetPath
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}

		proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
		if err != nil {
			http.Error(w, "proxy error", http.StatusBadGateway)
			return
		}
		setProxyClientIPHeaders(proxyReq.Header, r.RemoteAddr)
		proxyReq.Header.Set("Authorization", "Bearer "+b.token)
		proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))

		client := http.DefaultClient
		if r.Header.Get("Accept") == "text/event-stream" {
			client = &http.Client{Timeout: 0}
		}
		resp, err := client.Do(proxyReq)
		if err != nil {
			http.Error(w, "broker unreachable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, v := range resp.Header {
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}
		w.WriteHeader(resp.StatusCode)

		if resp.Header.Get("Content-Type") == "text/event-stream" {
			flusher, canFlush := w.(http.Flusher)
			buf := make([]byte, 4096)
			for {
				n, readErr := resp.Body.Read(buf)
				if n > 0 {
					w.Write(buf[:n]) //nolint:errcheck
					if canFlush {
						flusher.Flush()
					}
				}
				if readErr != nil {
					break
				}
			}
			return
		}
		_, _ = io.Copy(w, resp.Body)
	})
}

// Messages returns all channel messages (for the Go TUI channel view).
func (b *Broker) Messages() []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]channelMessage, len(b.messages))
	copy(out, b.messages)
	return out
}

func (b *Broker) HasPendingInterview() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, req := range b.requests {
		if requestIsHumanInterview(req) && requestIsActive(req) {
			return true
		}
	}
	return false
}

func (b *Broker) HasBlockingRequest() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, req := range b.requests {
		if requestBlocksMessages(req) {
			return true
		}
	}
	return false
}

// HasRecentlyTaggedAgents returns true if any agent was @mentioned within
// the given duration and has not yet replied (i.e. is presumably "typing").
func (b *Broker) HasRecentlyTaggedAgents(within time.Duration) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lastTaggedAt) == 0 {
		return false
	}
	cutoff := time.Now().Add(-within)
	for _, t := range b.lastTaggedAt {
		if t.After(cutoff) {
			return true
		}
	}
	return false
}

func (b *Broker) EnabledMembers(channel string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessionMode == SessionModeOneOnOne {
		return []string{b.oneOnOneAgent}
	}
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	if ch := b.findChannelLocked(channel); ch != nil {
		return b.enabledChannelMembersLocked(channel, ch.Members)
	}
	return nil
}

// DisabledMembers returns the slugs explicitly disabled for a channel —
// members who were present in ch.Members at some point but have been muted
// for this channel. Callers use this to distinguish "never added" (which an
// explicit @-tag can bypass) from "deliberately muted" (which an @-tag must
// respect — muting an agent is the user's explicit intent to silence them).
func (b *Broker) DisabledMembers(channel string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	ch := b.findChannelLocked(channel)
	if ch == nil || len(ch.Disabled) == 0 {
		return nil
	}
	return append([]string(nil), ch.Disabled...)
}

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

func (b *Broker) OfficeMembers() []officeMember {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeMember, len(b.members))
	copy(out, b.members)
	return out
}

func (b *Broker) ChannelMessages(channel string) []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	out := make([]channelMessage, 0, len(b.messages))
	for _, msg := range b.messages {
		if normalizeChannelSlug(msg.Channel) == channel {
			out = append(out, msg)
		}
	}
	return out
}

// AllMessages returns a copy of all messages across all channels, ordered by
// creation time. Use this when the caller needs to search across channels rather
// than in a single known channel.
func (b *Broker) AllMessages() []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]channelMessage, len(b.messages))
	copy(out, b.messages)
	return out
}

// SurfaceChannels returns all channels that have a surface configured for the given provider.
func (b *Broker) SurfaceChannels(provider string) []teamChannel {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []teamChannel
	for _, ch := range b.channels {
		if ch.Surface != nil && ch.Surface.Provider == provider {
			cp := ch
			cp.Members = append([]string(nil), ch.Members...)
			cp.Disabled = append([]string(nil), ch.Disabled...)
			s := *ch.Surface
			cp.Surface = &s
			out = append(out, cp)
		}
	}
	return out
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

// DMPartner returns the non-human member slug of a 1:1 DM channel. Returns
// "" if the channel is not a DM, does not exist, or is a group DM. Used by
// surface bridges (OpenClaw, Slack, etc.) to resolve "who is the human
// talking to" when routing DM posts to the right agent without requiring an
// @mention.
func (b *Broker) DMPartner(channelSlug string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := b.findChannelLocked(normalizeChannelSlug(channelSlug))
	if ch == nil || !ch.isDM() {
		return ""
	}
	if len(ch.Members) != 2 {
		return ""
	}
	for _, m := range ch.Members {
		if m != "human" && m != "you" {
			return m
		}
	}
	return ""
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

func (b *Broker) ChannelTasks(channel string) []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	out := make([]teamTask, 0, len(b.tasks))
	for _, task := range b.tasks {
		if normalizeChannelSlug(task.Channel) == channel {
			out = append(out, task)
		}
	}
	return out
}

// AllTasks returns a copy of all tasks across all channels. Use this when the
// caller needs to search across channels rather than in a single known channel.
func (b *Broker) AllTasks() []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]teamTask, len(b.tasks))
	copy(out, b.tasks)
	return out
}

// InFlightTasks returns tasks that have an assigned owner and a non-terminal
// status (anything except "done", "completed", "canceled", or "cancelled").
func (b *Broker) InFlightTasks() []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]teamTask, 0)
	for _, task := range b.tasks {
		if task.Owner == "" {
			continue
		}
		s := strings.ToLower(strings.TrimSpace(task.Status))
		if s == "done" || s == "completed" || s == "canceled" || s == "cancelled" {
			continue
		}
		out = append(out, task)
	}
	return out
}

// RecentHumanMessages returns up to limit messages sent by a human or external
// sender ("you", "human", or "nex"). The returned slice contains the most
// recent messages in chronological order (earliest first).
func (b *Broker) RecentHumanMessages(limit int) []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	var human []channelMessage
	for _, msg := range b.messages {
		f := strings.ToLower(strings.TrimSpace(msg.From))
		if f == "you" || f == "human" || f == "nex" {
			human = append(human, msg)
		}
	}
	if len(human) <= limit {
		return human
	}
	return human[len(human)-limit:]
}

// UnackedTasks returns in_progress tasks with an owner that have not been acked
// and were created more than the given duration ago.
func (b *Broker) UnackedTasks(timeout time.Duration) []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	cutoff := time.Now().UTC().Add(-timeout)
	out := make([]teamTask, 0)
	for _, task := range b.tasks {
		if task.Status != "in_progress" || task.Owner == "" || task.AckedAt != "" {
			continue
		}
		created, err := time.Parse(time.RFC3339, task.CreatedAt)
		if err != nil {
			continue
		}
		if created.Before(cutoff) {
			out = append(out, task)
		}
	}
	return out
}

func (b *Broker) Requests(channel string, includeResolved bool) []humanInterview {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	out := make([]humanInterview, 0, len(b.requests))
	for _, req := range b.requests {
		reqChannel := normalizeChannelSlug(req.Channel)
		if reqChannel == "" {
			reqChannel = "general"
		}
		if reqChannel != channel {
			continue
		}
		if !includeResolved && !requestIsActive(req) {
			continue
		}
		out = append(out, req)
	}
	return out
}

func (b *Broker) Actions() []officeActionLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeActionLog, len(b.actions))
	copy(out, b.actions)
	return out
}

func (b *Broker) Signals() []officeSignalRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeSignalRecord, len(b.signals))
	copy(out, b.signals)
	return out
}

func (b *Broker) Decisions() []officeDecisionRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeDecisionRecord, len(b.decisions))
	copy(out, b.decisions)
	return out
}

func (b *Broker) Watchdogs() []watchdogAlert {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]watchdogAlert, len(b.watchdogs))
	copy(out, b.watchdogs)
	return out
}

func (b *Broker) Scheduler() []schedulerJob {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]schedulerJob, len(b.scheduler))
	copy(out, b.scheduler)
	return out
}

type queueSnapshot struct {
	Actions   []officeActionLog      `json:"actions"`
	Signals   []officeSignalRecord   `json:"signals,omitempty"`
	Decisions []officeDecisionRecord `json:"decisions,omitempty"`
	Watchdogs []watchdogAlert        `json:"watchdogs,omitempty"`
	Scheduler []schedulerJob         `json:"scheduler"`
	Due       []schedulerJob         `json:"due,omitempty"`
}

func (b *Broker) QueueSnapshot() queueSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return queueSnapshot{
		Actions:   append([]officeActionLog(nil), b.actions...),
		Signals:   append([]officeSignalRecord(nil), b.signals...),
		Decisions: append([]officeDecisionRecord(nil), b.decisions...),
		Watchdogs: append([]watchdogAlert(nil), b.watchdogs...),
		Scheduler: append([]schedulerJob(nil), b.scheduler...),
		Due:       append([]schedulerJob(nil), b.dueSchedulerJobsLocked(time.Now().UTC())...),
	}
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

func defaultBrokerStatePath() string {
	// Env override lets probes and test harnesses isolate broker state from
	// the user's real ~/.wuphf/team/ dir without needing to remap HOME (which
	// breaks macOS keychain-backed auth for bundled CLIs like Claude Code).
	if p := strings.TrimSpace(os.Getenv("WUPHF_BROKER_STATE_PATH")); p != "" {
		return p
	}
	home := config.RuntimeHomeDir()
	if home == "" {
		return filepath.Join(".wuphf", "team", "broker-state.json")
	}
	return filepath.Join(home, ".wuphf", "team", "broker-state.json")
}

// stateSnapshotPath returns the path the Broker writes its last-good
// crash-recovery snapshot to. Bound to b.statePath (set at construction).
func (b *Broker) stateSnapshotPath() string {
	return b.statePath + ".last-good"
}

func loadBrokerStateFile(path string) (brokerState, error) {
	var state brokerState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	return state, nil
}

func brokerStateActivityScore(state brokerState) int {
	score := 0
	score += len(state.Messages) * 10
	score += len(state.AgentIssues) * 8
	score += len(state.Tasks) * 20
	score += len(activeRequests(state.Requests)) * 10
	score += len(state.Actions) * 4
	score += len(state.Signals) * 4
	score += len(state.Decisions) * 4
	score += len(state.Skills) * 2
	score += len(state.Policies)
	for _, ns := range state.SharedMemory {
		score += len(ns)
	}
	if state.PendingInterview != nil {
		score += 5
	}
	return score
}

func brokerStateShouldSnapshot(state brokerState) bool {
	return brokerStateActivityScore(state) > 0
}

func (b *Broker) loadState() error {
	if b.statePath == "" {
		// Direct &Broker{} literal in a unit test that exercises only
		// in-memory logic — no file to load from. Treat as a fresh
		// no-state broker and let the caller proceed.
		return nil
	}
	path := b.statePath
	state, err := loadBrokerStateFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		state = brokerState{}
	}
	snapshotPath := b.stateSnapshotPath()
	if snapshot, snapErr := loadBrokerStateFile(snapshotPath); snapErr == nil {
		if brokerStateActivityScore(snapshot) > brokerStateActivityScore(state) {
			state = snapshot
		}
	}
	b.messages = state.Messages
	b.agentIssues = state.AgentIssues
	b.members = state.Members
	b.channels = state.Channels
	b.sessionMode = state.SessionMode
	b.oneOnOneAgent = state.OneOnOneAgent
	b.focusMode = state.FocusMode
	b.tasks = state.Tasks
	b.requests = state.Requests
	b.actions = state.Actions
	b.signals = state.Signals
	b.decisions = state.Decisions
	b.watchdogs = state.Watchdogs
	b.policies = state.Policies
	b.scheduler = state.Scheduler
	b.skills = state.Skills
	b.sharedMemory = state.SharedMemory
	b.counter = state.Counter
	b.notificationSince = state.NotificationSince
	b.insightsSince = state.InsightsSince
	b.pendingInterview = state.PendingInterview
	b.usage = state.Usage
	if b.usage.Agents == nil {
		b.usage.Agents = make(map[string]usageTotals)
	}
	b.usage.Session = usageTotals{}
	if len(b.requests) == 0 && b.pendingInterview != nil {
		b.requests = []humanInterview{*b.pendingInterview}
	}
	// Load channel store: if present, unmarshal it.
	// Legacy states without channel_store start with an empty store; DMs are created on demand.
	if len(state.ChannelStore) > 0 {
		if err := json.Unmarshal(state.ChannelStore, b.channelStore); err != nil {
			return fmt.Errorf("unmarshal channel_store: %w", err)
		}
		b.channelStore.MigrateLegacyDM()
	}
	// Migrate channel refs from dm-* to deterministic pair slugs across all entities.
	// Messages are the primary data loss risk: legacy Channel:"dm-engineering" would not
	// match Store lookups keyed by "engineering__human".
	for i := range b.messages {
		b.messages[i].Channel = channel.MigrateDMSlugString(b.messages[i].Channel)
	}
	for i := range b.tasks {
		b.tasks[i].Channel = channel.MigrateDMSlugString(b.tasks[i].Channel)
	}
	for i := range b.requests {
		b.requests[i].Channel = channel.MigrateDMSlugString(b.requests[i].Channel)
	}
	// b.ensureDefaultChannelsLocked() // channels come from saved state
	b.ensureDefaultOfficeMembersLocked()
	b.normalizeLoadedStateLocked()
	return nil
}

func (b *Broker) saveLocked() error {
	if b.statePath == "" {
		// A direct &Broker{} literal (no NewBrokerAt/NewBroker) reaching the
		// persistence path means a test wired in-memory state but accidentally
		// triggered a save — without this guard the empty path would silently
		// resolve to "" + cwd-adjacent files. Fail loudly so the caller fixes
		// the construction site instead of corrupting the test workdir.
		return errors.New("broker: saveLocked requires a non-empty statePath; construct via NewBrokerAt(path)")
	}
	path := b.statePath
	snapshotPath := b.stateSnapshotPath()
	if len(b.messages) == 0 && len(b.agentIssues) == 0 && len(b.tasks) == 0 && len(activeRequests(b.requests)) == 0 && len(b.actions) == 0 && len(b.signals) == 0 && len(b.decisions) == 0 && len(b.watchdogs) == 0 && len(b.policies) == 0 && len(b.scheduler) == 0 && len(b.skills) == 0 && len(b.sharedMemory) == 0 && isDefaultChannelState(b.channels) && isDefaultOfficeMemberState(b.members) && b.counter == 0 && b.notificationSince == "" && b.insightsSince == "" && usageStateIsZero(b.usage) && b.sessionMode == SessionModeOffice && b.oneOnOneAgent == DefaultOneOnOneAgent {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Remove(snapshotPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var channelStoreRaw json.RawMessage
	if b.channelStore != nil {
		if raw, err := json.Marshal(b.channelStore); err == nil {
			channelStoreRaw = raw
		}
	}
	state := brokerState{
		ChannelStore:      channelStoreRaw,
		Messages:          b.messages,
		AgentIssues:       b.agentIssues,
		Members:           b.members,
		Channels:          b.channels,
		SessionMode:       b.sessionMode,
		OneOnOneAgent:     b.oneOnOneAgent,
		FocusMode:         b.focusMode,
		Tasks:             b.tasks,
		Requests:          b.requests,
		Actions:           b.actions,
		Signals:           b.signals,
		Decisions:         b.decisions,
		Watchdogs:         b.watchdogs,
		Policies:          b.policies,
		Scheduler:         b.scheduler,
		Skills:            b.skills,
		SharedMemory:      b.sharedMemory,
		Counter:           b.counter,
		NotificationSince: b.notificationSince,
		InsightsSince:     b.insightsSince,
		PendingInterview:  firstBlockingRequest(b.requests),
		Usage: func() teamUsageState {
			usage := b.usage
			usage.Session = usageTotals{}
			return usage
		}(),
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWriteFile(path, data); err != nil {
		return err
	}
	if brokerStateShouldSnapshot(state) {
		if err := atomicWriteFile(snapshotPath, data); err != nil {
			return err
		}
	}
	return nil
}

// atomicWriteFile writes data to path atomically by creating a uniquely-named
// sibling tmp file (mode 0o600 via os.CreateTemp) and renaming. Each call
// uses a fresh tmp filename, so concurrent writers to the same destination
// cannot race on the source path of the rename.
//
// The previous fixed `<path>.tmp` filename was safe in production (one broker
// owns one path) but broke the test suite: many *_test.go files used to
// monkey-patch the package-level state-path var and a leaked tempdir from
// worktree_guard_test.go init() was shared across every unisolated test.
// Two saves landing on the same path could interleave like A.WriteFile /
// B.WriteFile / A.Rename / B.Rename — and B's Rename failed with
// "no such file or directory" because
// A had already renamed the shared tmp out from under it. That was the CI
// flake on PR #281's `test` job. See broker_save_race_test.go for the
// regression repro.
//
// 0o600 is hard-coded because the only callers (broker state file +
// snapshot) want exactly that mode; CreateTemp already produces it on the
// platforms we support, so no os.Chmod is needed.
func atomicWriteFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	tmpf, err := os.CreateTemp(dir, base+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpf.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmpf.Write(data); err != nil {
		_ = tmpf.Close()
		cleanup()
		return err
	}
	if err := tmpf.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

func defaultOfficeMembers() []officeMember {
	now := time.Now().UTC().Format(time.RFC3339)
	manifest, err := company.LoadRuntimeManifest(repoRootForRuntimeDefaults())
	if err != nil || len(manifest.Members) == 0 {
		manifest = company.DefaultManifest()
	}
	members := make([]officeMember, 0, len(manifest.Members))
	for _, cfg := range manifest.Members {
		builtIn := cfg.System || cfg.Slug == manifest.Lead || cfg.Slug == "ceo"
		members = append(members, memberFromSpec(cfg, "wuphf", now, builtIn))
	}
	return members
}

func defaultOfficeMemberSlugs() []string {
	members := defaultOfficeMembers()
	slugs := make([]string, 0, len(members))
	for _, member := range members {
		slugs = append(slugs, member.Slug)
	}
	return slugs
}

func defaultTeamChannels() []teamChannel {
	now := time.Now().UTC().Format(time.RFC3339)
	manifest, err := company.LoadRuntimeManifest(repoRootForRuntimeDefaults())
	if err != nil || len(manifest.Channels) == 0 {
		manifest = company.DefaultManifest()
	}
	channels := make([]teamChannel, 0, len(manifest.Channels))
	for _, channel := range manifest.Channels {
		tc := teamChannel{
			Slug:        channel.Slug,
			Name:        channel.Name,
			Description: channel.Description,
			Members:     append([]string(nil), channel.Members...),
			Disabled:    append([]string(nil), channel.Disabled...),
			CreatedBy:   "wuphf",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if channel.Surface != nil {
			tc.Surface = &channelSurface{
				Provider:    channel.Surface.Provider,
				RemoteID:    channel.Surface.RemoteID,
				RemoteTitle: channel.Surface.RemoteTitle,
				Mode:        channel.Surface.Mode,
				BotTokenEnv: channel.Surface.BotTokenEnv,
			}
		}
		channels = append(channels, tc)
	}
	return channels
}

func repoRootForRuntimeDefaults() string {
	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

func isDefaultChannelState(channels []teamChannel) bool {
	defaults := defaultTeamChannels()
	if len(channels) != len(defaults) {
		return false
	}
	for i := range defaults {
		if channels[i].Slug != defaults[i].Slug || channels[i].Name != defaults[i].Name || channels[i].Description != defaults[i].Description {
			return false
		}
		if strings.Join(channels[i].Members, ",") != strings.Join(defaults[i].Members, ",") {
			return false
		}
		if strings.Join(channels[i].Disabled, ",") != strings.Join(defaults[i].Disabled, ",") {
			return false
		}
	}
	return true
}

func isDefaultOfficeMemberState(members []officeMember) bool {
	defaults := defaultOfficeMembers()
	if len(members) != len(defaults) {
		return false
	}
	for i := range defaults {
		if members[i].Slug != defaults[i].Slug || members[i].Name != defaults[i].Name || members[i].Role != defaults[i].Role {
			return false
		}
	}
	return true
}

func normalizeChannelSlug(slug string) string {
	slug = strings.ToLower(strings.TrimSpace(slug))
	slug = strings.TrimLeft(slug, "#")
	slug = strings.ReplaceAll(slug, " ", "-")
	// Preserve "__" (DM slug separator) before replacing single underscores.
	const placeholder = "\x00"
	slug = strings.ReplaceAll(slug, "__", placeholder)
	slug = strings.ReplaceAll(slug, "_", "-")
	slug = strings.ReplaceAll(slug, placeholder, "__")
	if slug == "" {
		return "general"
	}
	return slug
}

func normalizeActorSlug(slug string) string {
	slug = strings.ToLower(strings.TrimSpace(slug))
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
	return slug
}

func (b *Broker) ensureDefaultChannelsLocked() {
	if len(b.channels) == 0 {
		b.channels = defaultTeamChannels()
		return
	}
	hasGeneral := false
	for _, ch := range b.channels {
		if ch.Slug == "general" {
			hasGeneral = true
			break
		}
	}
	if !hasGeneral {
		b.channels = append(defaultTeamChannels(), b.channels...)
		return
	}
	// Merge surface metadata from manifest into existing channels
	// (handles case where state was saved without surfaces by an older binary)
	defaults := defaultTeamChannels()
	for _, def := range defaults {
		if def.Surface == nil {
			continue
		}
		found := false
		for i := range b.channels {
			if b.channels[i].Slug == def.Slug {
				if b.channels[i].Surface == nil {
					b.channels[i].Surface = def.Surface
				}
				found = true
				break
			}
		}
		if !found {
			b.channels = append(b.channels, def)
		}
	}
}

// ensureDefaultOfficeMembersLocked seeds the DefaultManifest roster ONLY when
// no members exist. Prior implementation appended any missing default slug to
// a non-empty roster, which caused ceo/planner/executor/reviewer to leak back
// into blueprint-seeded teams (e.g. niche-crm) on every Broker.Load(). The
// function is called from broker init (line 831) and post-load normalization
// (line 2260) as a true recovery hook: if state was corrupted or never
// seeded, fall back to defaults.
func (b *Broker) ensureDefaultOfficeMembersLocked() {
	if len(b.members) > 0 {
		return
	}
	b.members = defaultOfficeMembers()
}

func (b *Broker) normalizeLoadedStateLocked() {
	b.sessionMode = NormalizeSessionMode(b.sessionMode)
	b.oneOnOneAgent = NormalizeOneOnOneAgent(b.oneOnOneAgent)
	if b.findMemberLocked(b.oneOnOneAgent) == nil {
		b.oneOnOneAgent = DefaultOneOnOneAgent
	}
	seenMembers := make(map[string]struct{}, len(b.members))
	normalizedMembers := make([]officeMember, 0, len(b.members))
	for _, member := range b.members {
		member.Slug = normalizeChannelSlug(member.Slug)
		if member.Slug == "" {
			continue
		}
		if _, ok := seenMembers[member.Slug]; ok {
			continue
		}
		seenMembers[member.Slug] = struct{}{}
		member.Name = strings.TrimSpace(member.Name)
		if member.Name == "" {
			member.Name = humanizeSlug(member.Slug)
		}
		member.Role = strings.TrimSpace(member.Role)
		if member.Role == "" {
			member.Role = member.Name
		}
		member.BuiltIn = member.Slug == "ceo"
		member.Expertise = normalizeStringList(member.Expertise)
		member.AllowedTools = normalizeStringList(member.AllowedTools)
		normalizedMembers = append(normalizedMembers, member)
	}
	b.members = normalizedMembers
	for i := range b.channels {
		b.channels[i].Slug = normalizeChannelSlug(b.channels[i].Slug)
		if strings.TrimSpace(b.channels[i].Name) == "" {
			b.channels[i].Name = b.channels[i].Slug
		}
		if strings.TrimSpace(b.channels[i].Description) == "" {
			b.channels[i].Description = defaultTeamChannelDescription(b.channels[i].Slug, b.channels[i].Name)
		}
		if b.channels[i].Slug == "general" && len(b.channels[i].Members) < len(b.members) {
			// Re-populate general channel with all office members.
			// This fixes stale state where only CEO survived a previous normalization.
			allSlugs := make([]string, 0, len(b.members))
			for _, m := range b.members {
				allSlugs = append(allSlugs, m.Slug)
			}
			b.channels[i].Members = allSlugs
		}
		filteredMembers := make([]string, 0, len(b.channels[i].Members))
		for _, slug := range uniqueSlugs(b.channels[i].Members) {
			if b.findMemberLocked(slug) != nil {
				filteredMembers = append(filteredMembers, slug)
			}
		}
		b.channels[i].Members = uniqueSlugs(append([]string{"ceo"}, filteredMembers...))
		filteredDisabled := make([]string, 0, len(b.channels[i].Disabled))
		for _, slug := range uniqueSlugs(b.channels[i].Disabled) {
			if slug == "ceo" {
				continue
			}
			if b.findMemberLocked(slug) != nil && containsString(b.channels[i].Members, slug) {
				filteredDisabled = append(filteredDisabled, slug)
			}
		}
		b.channels[i].Disabled = filteredDisabled
	}
	for i := range b.messages {
		if strings.TrimSpace(b.messages[i].Channel) == "" {
			b.messages[i].Channel = "general"
		}
	}
	for i := range b.agentIssues {
		issueChannel := normalizeChannelSlug(channel.MigrateDMSlugString(b.agentIssues[i].Channel))
		if issueChannel == "" {
			issueChannel = "general"
		}
		b.agentIssues[i].Channel = issueChannel
		if strings.TrimSpace(b.agentIssues[i].UpdatedAt) == "" {
			b.agentIssues[i].UpdatedAt = b.agentIssues[i].CreatedAt
		}
		if b.agentIssues[i].Count <= 0 {
			b.agentIssues[i].Count = 1
		}
	}
	for i := range b.tasks {
		if strings.TrimSpace(b.tasks[i].Channel) == "" {
			b.tasks[i].Channel = "general"
		}
	}
	for i := range b.requests {
		if strings.TrimSpace(b.requests[i].Channel) == "" {
			b.requests[i].Channel = "general"
		}
		if strings.TrimSpace(b.requests[i].Kind) == "" {
			b.requests[i].Kind = "choice"
		}
		if strings.TrimSpace(b.requests[i].Status) == "" {
			if b.requests[i].Answered != nil {
				b.requests[i].Status = "answered"
			} else {
				b.requests[i].Status = "pending"
			}
		}
		if requestIsHumanInterview(b.requests[i]) {
			b.requests[i].Blocking = false
			b.requests[i].Required = false
		} else if b.requests[i].Blocking {
			b.requests[i].Blocking = true
		}
		if strings.TrimSpace(b.requests[i].UpdatedAt) == "" {
			b.requests[i].UpdatedAt = b.requests[i].CreatedAt
		}
		b.scheduleRequestLifecycleLocked(&b.requests[i])
	}
	for i := range b.tasks {
		if strings.TrimSpace(b.tasks[i].Channel) == "" {
			b.tasks[i].Channel = "general"
		}
		normalizeTaskPlan(&b.tasks[i])
		syncTaskMemoryWorkflow(&b.tasks[i], "")
		b.ensureTaskOwnerChannelMembershipLocked(b.tasks[i].Channel, b.tasks[i].Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(&b.tasks[i])
		b.scheduleTaskLifecycleLocked(&b.tasks[i])
		_ = b.syncTaskWorktreeLocked(&b.tasks[i])
	}
	b.reconcileOrphanedBlockedTasksLocked()
	b.pendingInterview = firstBlockingRequest(b.requests)
}

// reconcileOrphanedBlockedTasksLocked unblocks tasks whose dependencies
// have all reached a terminal status (done/completed/canceled/cancelled)
// but who never received the unblock notification because the parent
// terminated under the pre-fix semantics where only Status="done" fired
// unblockDependentsLocked. This is a one-shot migration: tasks blocked
// by a still-active dependency are left alone. Idempotent — running it
// twice has no effect since the second pass finds nothing blocked.
//
// Caller must hold b.mu. Called from normalizeLoadedStateLocked so it
// runs once per broker boot against persisted state.
func (b *Broker) reconcileOrphanedBlockedTasksLocked() {
	for i := range b.tasks {
		t := &b.tasks[i]
		if !t.Blocked || t.Status != "blocked" {
			continue
		}
		if b.hasUnresolvedDepsLocked(t) {
			continue
		}
		t.Blocked = false
		t.Status = "in_progress"
		t.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		b.appendActionLocked("task_unblocked", "office", t.Channel, "system",
			truncateSummary("Reconciled: parent dep terminated while task was blocked", 140), t.ID)
	}
}

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

// memberFromSpec builds an officeMember from a manifest MemberSpec, threading
// Provider through. Used by defaultOfficeMembers and by HTTP create paths so
// field-copy logic lives in one place.
func memberFromSpec(spec company.MemberSpec, createdBy, createdAt string, builtIn bool) officeMember {
	return officeMember{
		Slug:           spec.Slug,
		Name:           spec.Name,
		Role:           spec.Role,
		Expertise:      append([]string(nil), spec.Expertise...),
		Personality:    spec.Personality,
		PermissionMode: spec.PermissionMode,
		AllowedTools:   append([]string(nil), spec.AllowedTools...),
		CreatedBy:      createdBy,
		CreatedAt:      createdAt,
		BuiltIn:        builtIn,
		Provider:       spec.Provider,
	}
}

// mentionPattern matches @slug tokens in free-form message text. Slugs are
// alphanumeric plus hyphen, 2–30 chars (to dodge false positives from
// conversational @ use). A preceding word character (e.g. email "a@b.com")
// is excluded via the lookahead-free alternative: we anchor to start of
// string or a non-alphanumeric rune.
var mentionPattern = regexp.MustCompile(`(?:^|[^a-zA-Z0-9_])@([a-z0-9][a-z0-9-]{1,29})\b`)

// extractMentionedSlugs pulls @-mention slugs out of body content. Duplicates
// are removed. The caller is responsible for validating whether each slug is
// a real office member.
func extractMentionedSlugs(content string) []string {
	matches := mentionPattern.FindAllStringSubmatch(strings.ToLower(content), -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		slug := m[1]
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	return out
}

func uniqueSlugs(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = normalizeChannelSlug(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeStringList(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func humanizeSlug(slug string) string {
	parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(slug), "-", " "), " ")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func defaultTeamChannelDescription(slug, name string) string {
	manifest, err := company.LoadManifest()
	if err == nil {
		for _, ch := range manifest.Channels {
			if normalizeChannelSlug(ch.Slug) == normalizeChannelSlug(slug) && strings.TrimSpace(ch.Description) != "" {
				return strings.TrimSpace(ch.Description)
			}
		}
	}
	if normalizeChannelSlug(slug) == "general" {
		return "The default company-wide room for top-level coordination, announcements, and cross-functional discussion."
	}
	label := strings.TrimSpace(name)
	if label == "" {
		label = humanizeSlug(slug)
	}
	return label + " focused work. Use this channel for discussion, decisions, and execution specific to that stream."
}

// reservedChannelSlugs are slug values that canAccessChannelLocked treats as
// universally trusted senders. Any user-created channel sharing one of these
// slugs would inherit that trust — every actor in the trust list could read
// every message in that channel without an explicit Members entry. The
// channel-create handler guards against this by rejecting create requests
// whose slug matches this set; keep the two lists in sync.
var reservedChannelSlugs = map[string]bool{
	"system": true,
	"nex":    true,
	"you":    true,
	"human":  true,
}

func (b *Broker) canAccessChannelLocked(slug, channel string) bool {
	slug = normalizeActorSlug(slug)
	channel = normalizeChannelSlug(channel)
	if b.sessionMode == SessionModeOneOnOne {
		if slug == "" || slug == "you" || slug == "human" {
			return true
		}
		return slug == b.oneOnOneAgent
	}
	// NOTE: any new entry added here MUST also be added to
	// reservedChannelSlugs above so the channel-create handler keeps the
	// invariant "no user channel can shadow a trusted sender slug".
	if slug == "" || slug == "you" || slug == "human" || slug == "nex" || slug == "system" {
		return true
	}
	if slug == "ceo" {
		return true
	}
	return b.channelHasMemberLocked(channel, slug)
}

func truncateSummary(s string, max int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 1 {
		return "…"
	}
	return string(runes[:max-1]) + "…"
}

func applyOfficeMemberDefaults(member *officeMember) {
	if member == nil {
		return
	}
	if member.Name == "" {
		member.Name = humanizeSlug(member.Slug)
	}
	if member.Role == "" {
		member.Role = member.Name
	}
	if len(member.Expertise) == 0 {
		member.Expertise = inferOfficeExpertise(member.Slug, member.Role)
	}
	if member.Personality == "" {
		member.Personality = inferOfficePersonality(member.Slug, member.Role)
	}
	if member.PermissionMode == "" {
		member.PermissionMode = "plan"
	}
}

func inferOfficeExpertise(slug, role string) []string {
	text := strings.ToLower(strings.TrimSpace(slug + " " + role))
	switch {
	case strings.Contains(text, "front"), strings.Contains(text, "ui"), strings.Contains(text, "design eng"):
		return []string{"frontend", "UI", "interaction design", "components", "accessibility"}
	case strings.Contains(text, "back"), strings.Contains(text, "api"), strings.Contains(text, "infra"):
		return []string{"backend", "APIs", "systems", "infrastructure", "databases"}
	case strings.Contains(text, "ai"), strings.Contains(text, "ml"), strings.Contains(text, "llm"):
		return []string{"AI", "LLMs", "agents", "retrieval", "evaluations"}
	case strings.Contains(text, "market"), strings.Contains(text, "brand"), strings.Contains(text, "growth"):
		return []string{"marketing", "growth", "positioning", "campaigns", "brand"}
	case strings.Contains(text, "revenue"), strings.Contains(text, "sales"), strings.Contains(text, "cro"):
		return []string{"sales", "revenue", "pipeline", "partnerships", "closing"}
	case strings.Contains(text, "product"), strings.Contains(text, "pm"):
		return []string{"product", "roadmap", "requirements", "prioritization", "scope"}
	case strings.Contains(text, "design"):
		return []string{"design", "UX", "visual systems", "prototyping", "brand"}
	default:
		return []string{strings.ToLower(strings.TrimSpace(role))}
	}
}

func inferOfficePersonality(slug, role string) string {
	text := strings.ToLower(strings.TrimSpace(slug + " " + role))
	switch {
	case strings.Contains(text, "front"):
		return "Frontend specialist focused on polished user-facing work and sharp interaction details."
	case strings.Contains(text, "back"):
		return "Systems-minded engineer who keeps complexity under control and worries about reliability."
	case strings.Contains(text, "ai"), strings.Contains(text, "ml"), strings.Contains(text, "llm"):
		return "AI engineer who likes ambitious ideas but immediately asks how they will actually work."
	case strings.Contains(text, "market"), strings.Contains(text, "brand"), strings.Contains(text, "growth"):
		return "Growth and positioning operator who translates product work into market momentum."
	case strings.Contains(text, "revenue"), strings.Contains(text, "sales"):
		return "Commercial operator who thinks in demand, objections, and revenue consequences."
	case strings.Contains(text, "product"), strings.Contains(text, "pm"):
		return "Product thinker who turns ambiguity into scope, sequencing, and crisp tradeoffs."
	case strings.Contains(text, "design"):
		return "Taste-driven designer who cares about clarity, craft, and how the product actually feels."
	default:
		return "A sharp teammate with a clear specialty, strong point of view, and enough personality to feel human."
	}
}

func (b *Broker) channelHasMemberLocked(channel, slug string) bool {
	ch := b.findChannelLocked(channel)
	if ch == nil {
		// Fall back to channelStore for new-format channels (e.g. "eng__human")
		if b.channelStore != nil {
			return b.channelStore.IsMemberBySlug(channel, slug)
		}
		return false
	}
	for _, member := range ch.Members {
		if member == slug {
			return true
		}
	}
	return false
}

func (b *Broker) channelMemberEnabledLocked(channel, slug string) bool {
	if !b.channelHasMemberLocked(channel, slug) {
		return false
	}
	ch := b.findChannelLocked(channel)
	if ch == nil {
		return false
	}
	for _, disabled := range ch.Disabled {
		if disabled == slug {
			return false
		}
	}
	return true
}

func (b *Broker) enabledChannelMembersLocked(channel string, candidates []string) []string {
	var out []string
	for _, candidate := range candidates {
		if b.channelMemberEnabledLocked(channel, candidate) {
			out = append(out, candidate)
		}
	}
	return out
}

func (b *Broker) ensureTaskOwnerChannelMembershipLocked(channel, owner string) {
	channel = normalizeChannelSlug(channel)
	owner = normalizeChannelSlug(owner)
	if channel == "" || owner == "" {
		return
	}
	if b.findMemberLocked(owner) == nil {
		return
	}
	ch := b.findChannelLocked(channel)
	if ch == nil {
		return
	}
	if !containsString(ch.Members, owner) {
		ch.Members = uniqueSlugs(append(ch.Members, owner))
		ch.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if len(ch.Disabled) > 0 {
		filtered := ch.Disabled[:0]
		for _, disabled := range ch.Disabled {
			if disabled != owner {
				filtered = append(filtered, disabled)
			}
		}
		ch.Disabled = filtered
	}
}

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
