package team

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	wuphf "github.com/nex-crm/wuphf"
	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/buildinfo"
	"github.com/nex-crm/wuphf/internal/channel"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/nex"
	"github.com/nex-crm/wuphf/internal/onboarding"
	"github.com/nex-crm/wuphf/internal/operations"
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

var brokerStatePath = defaultBrokerStatePath

// studioPackageGenerator routes Studio package generation through the
// install-wide LLM provider so opencode-only or claude-code-only setups
// aren't forced to have `codex` installed.
var studioPackageGenerator = provider.RunConfiguredOneShot

var externalRetryAfterPattern = regexp.MustCompile(`(?i)retry after ([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.+-]+Z?)`)

// agentStreamBuffer holds recent stdout/stderr lines from a headless agent
// process and fans them out to SSE subscribers in real time.
type agentStreamBuffer struct {
	mu     sync.Mutex
	lines  []string
	subs   map[int]chan string
	nextID int
}

func (s *agentStreamBuffer) Push(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, line)
	if len(s.lines) > 2000 {
		s.lines = s.lines[len(s.lines)-2000:]
	}
	for _, ch := range s.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func (s *agentStreamBuffer) subscribe() (<-chan string, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	ch := make(chan string, 128)
	s.subs[id] = ch
	return ch, func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

func (s *agentStreamBuffer) recent() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.lines))
	copy(out, s.lines)
	return out
}

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

type teamTask struct {
	ID               string   `json:"id"`
	Channel          string   `json:"channel,omitempty"`
	Title            string   `json:"title"`
	Details          string   `json:"details,omitempty"`
	Owner            string   `json:"owner,omitempty"`
	Status           string   `json:"status"`
	CreatedBy        string   `json:"created_by"`
	ThreadID         string   `json:"thread_id,omitempty"`
	TaskType         string   `json:"task_type,omitempty"`
	PipelineID       string   `json:"pipeline_id,omitempty"`
	PipelineStage    string   `json:"pipeline_stage,omitempty"`
	ExecutionMode    string   `json:"execution_mode,omitempty"`
	ReviewState      string   `json:"review_state,omitempty"`
	SourceSignalID   string   `json:"source_signal_id,omitempty"`
	SourceDecisionID string   `json:"source_decision_id,omitempty"`
	WorktreePath     string   `json:"worktree_path,omitempty"`
	WorktreeBranch   string   `json:"worktree_branch,omitempty"`
	DependsOn        []string `json:"depends_on,omitempty"`
	Blocked          bool     `json:"blocked,omitempty"`
	AckedAt          string   `json:"acked_at,omitempty"`
	DueAt            string   `json:"due_at,omitempty"`
	FollowUpAt       string   `json:"follow_up_at,omitempty"`
	ReminderAt       string   `json:"reminder_at,omitempty"`
	RecheckAt        string   `json:"recheck_at,omitempty"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
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
}

type teamSkill struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Title               string   `json:"title"`
	Description         string   `json:"description,omitempty"`
	Content             string   `json:"content"`
	CreatedBy           string   `json:"created_by"`
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
	entityGraph             *EntityGraph
	entitySynthesizer       *EntitySynthesizer
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
	shutdownOnce       sync.Once
	shutdownCh         chan struct{}
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

	stopCh   chan struct{} // closed by Stop(); signals background goroutines to exit
	stopOnce sync.Once
}

func taskNeedsLocalWorktree(task *teamTask) bool {
	if task == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return false
	}
	if strings.TrimSpace(task.Owner) == "" {
		return false
	}
	switch strings.TrimSpace(task.Status) {
	case "", "open":
		return false
	case "done":
		return strings.TrimSpace(task.WorktreePath) != "" || strings.TrimSpace(task.WorktreeBranch) != ""
	default:
		return true
	}
}

func taskBlockReasonLooksLikeWorkspaceWriteIssue(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return false
	}
	markers := []string{
		"read-only",
		"read only",
		"writable workspace",
		"write access",
		"filesystem sandbox",
		"workspace sandbox",
		"operation not permitted",
		"permission denied",
	}
	for _, marker := range markers {
		if strings.Contains(reason, marker) {
			return true
		}
	}
	return false
}

func rejectFalseLocalWorktreeBlock(task *teamTask, reason string) error {
	if task == nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		return nil
	}
	if !taskBlockReasonLooksLikeWorkspaceWriteIssue(reason) {
		return nil
	}
	worktreePath := strings.TrimSpace(task.WorktreePath)
	if worktreePath == "" {
		return nil
	}
	if err := verifyTaskWorktreeWritable(worktreePath); err == nil {
		return fmt.Errorf("assigned local worktree is writable at %s; do not request writable-workspace approval; continue implementation in that worktree", worktreePath)
	}
	return nil
}

func taskRequiresExclusiveOwnerTurn(task *teamTask) bool {
	if task == nil {
		return false
	}
	if strings.TrimSpace(task.Owner) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(task.ExecutionMode)) {
	case "local_worktree", "live_external":
		return true
	default:
		return false
	}
}

func taskStatusConsumesExclusiveOwnerTurn(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "in_progress", "review":
		return true
	default:
		return false
	}
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

func taskChannelCandidateOwnerAllowed(ch *teamChannel, owner string) bool {
	if ch == nil {
		return false
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return true
	}
	return stringSliceContainsFold(ch.Members, owner) || strings.EqualFold(strings.TrimSpace(ch.CreatedBy), owner)
}

func (b *Broker) syncTaskWorktreeLocked(task *teamTask) error {
	if task == nil {
		return nil
	}
	// Automatically assign local_worktree mode when a coding agent claims a task.
	if task.ExecutionMode == "" && codingAgentSlugs[strings.TrimSpace(task.Owner)] {
		switch strings.TrimSpace(task.Status) {
		case "", "open", "done":
			// not yet in-progress; leave mode unset
		default:
			task.ExecutionMode = "local_worktree"
		}
	}
	if taskNeedsLocalWorktree(task) {
		if strings.TrimSpace(task.WorktreePath) != "" && strings.TrimSpace(task.WorktreeBranch) != "" {
			if taskWorktreeSourceLooksUsable(task.WorktreePath) {
				return nil
			}
			if err := cleanupTaskWorktree(task.WorktreePath, task.WorktreeBranch); err != nil {
				return err
			}
			task.WorktreePath = ""
			task.WorktreeBranch = ""
		}
		if path, branch := b.reusableDependencyWorktreeLocked(task); path != "" && branch != "" {
			task.WorktreePath = path
			task.WorktreeBranch = branch
			return nil
		}
		path, branch, err := prepareTaskWorktree(task.ID)
		if err != nil {
			return err
		}
		task.WorktreePath = path
		task.WorktreeBranch = branch
		return nil
	}

	if strings.TrimSpace(task.WorktreePath) == "" && strings.TrimSpace(task.WorktreeBranch) == "" {
		return nil
	}
	if err := cleanupTaskWorktree(task.WorktreePath, task.WorktreeBranch); err != nil {
		return err
	}
	task.WorktreePath = ""
	task.WorktreeBranch = ""
	return nil
}

func (b *Broker) reusableDependencyWorktreeLocked(task *teamTask) (string, string) {
	if b == nil || task == nil || len(task.DependsOn) == 0 {
		return "", ""
	}
	owner := strings.TrimSpace(task.Owner)
	var fallbackPath string
	var fallbackBranch string
	for _, depID := range task.DependsOn {
		depID = strings.TrimSpace(depID)
		if depID == "" {
			continue
		}
		for i := range b.tasks {
			dep := &b.tasks[i]
			if strings.TrimSpace(dep.ID) != depID {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(dep.ExecutionMode), "local_worktree") {
				continue
			}
			path := strings.TrimSpace(dep.WorktreePath)
			branch := strings.TrimSpace(dep.WorktreeBranch)
			if path == "" || branch == "" {
				continue
			}
			status := strings.ToLower(strings.TrimSpace(dep.Status))
			review := strings.ToLower(strings.TrimSpace(dep.ReviewState))
			if status != "review" && status != "done" && review != "ready_for_review" && review != "approved" {
				continue
			}
			if owner != "" && strings.TrimSpace(dep.Owner) == owner {
				return path, branch
			}
			if fallbackPath == "" && fallbackBranch == "" {
				fallbackPath = path
				fallbackBranch = branch
			}
		}
	}
	return fallbackPath, fallbackBranch
}

func (b *Broker) activeExclusiveOwnerTaskLocked(owner, excludeTaskID string) *teamTask {
	owner = strings.TrimSpace(owner)
	excludeTaskID = strings.TrimSpace(excludeTaskID)
	if b == nil || owner == "" {
		return nil
	}
	for i := range b.tasks {
		task := &b.tasks[i]
		if excludeTaskID != "" && strings.TrimSpace(task.ID) == excludeTaskID {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(task.Owner), owner) {
			continue
		}
		if !taskRequiresExclusiveOwnerTurn(task) {
			continue
		}
		if !taskStatusConsumesExclusiveOwnerTurn(task.Status) {
			continue
		}
		return task
	}
	return nil
}

func (b *Broker) queueTaskBehindActiveOwnerLaneLocked(task *teamTask) {
	if b == nil || task == nil {
		return
	}
	if !taskRequiresExclusiveOwnerTurn(task) {
		return
	}
	if !taskStatusConsumesExclusiveOwnerTurn(task.Status) {
		return
	}
	active := b.activeExclusiveOwnerTaskLocked(task.Owner, task.ID)
	if active == nil {
		return
	}
	if !stringSliceContainsFold(task.DependsOn, active.ID) {
		task.DependsOn = append(task.DependsOn, active.ID)
	}
	task.Blocked = true
	task.Status = "open"
	queueNote := fmt.Sprintf("Queued behind %s so @%s only carries one active %s lane at a time.", active.ID, strings.TrimSpace(task.Owner), strings.TrimSpace(task.ExecutionMode))
	switch existing := strings.TrimSpace(task.Details); {
	case existing == "":
		task.Details = queueNote
	case !strings.Contains(existing, queueNote):
		task.Details = existing + "\n\n" + queueNote
	}
}

func (b *Broker) preferredTaskChannelLocked(requestedChannel, createdBy, owner, title, details string) string {
	channel := normalizeChannelSlug(requestedChannel)
	if channel == "" {
		channel = "general"
	}
	if channel != "general" || b == nil {
		return channel
	}
	createdBy = strings.TrimSpace(createdBy)
	if createdBy == "" {
		return channel
	}
	probe := teamTask{
		Channel: channel,
		Owner:   strings.TrimSpace(owner),
		Title:   strings.TrimSpace(title),
		Details: strings.TrimSpace(details),
	}
	if !taskLooksLikeLiveBusinessObjective(&probe) {
		return channel
	}
	now := time.Now().UTC()
	var best *teamChannel
	var bestCreated time.Time
	for i := range b.channels {
		ch := &b.channels[i]
		slug := normalizeChannelSlug(ch.Slug)
		if slug == "" || slug == "general" || ch.isDM() {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(ch.CreatedBy), createdBy) {
			continue
		}
		if !taskChannelCandidateOwnerAllowed(ch, owner) {
			continue
		}
		createdAt := parseBrokerTimestamp(ch.CreatedAt)
		if !createdAt.IsZero() && now.Sub(createdAt) > 20*time.Minute {
			continue
		}
		if best == nil || (!createdAt.IsZero() && createdAt.After(bestCreated)) {
			best = ch
			bestCreated = createdAt
		}
	}
	if best == nil {
		return channel
	}
	return normalizeChannelSlug(best.Slug)
}

// generateToken returns a cryptographically random hex token.
func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: this should never happen on modern systems
		return fmt.Sprintf("wuphf-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// AgentStream returns (or lazily creates) the stream buffer for a given agent slug.
// It is safe to call concurrently.
func (b *Broker) AgentStream(slug string) *agentStreamBuffer {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.agentStreams == nil {
		b.agentStreams = make(map[string]*agentStreamBuffer)
	}
	s, ok := b.agentStreams[slug]
	if !ok {
		s = &agentStreamBuffer{subs: make(map[int]chan string)}
		b.agentStreams[slug] = s
	}
	return s
}

// NewBroker creates a new channel broker with a random auth token.
// skipBrokerStateLoadOnConstruct gates the lazy read of brokerStatePath()
// inside NewBroker. Production keeps it false so the CLI resumes from disk
// state. A *_test.go init flips it to true so tests that call NewBroker get
// a fresh broker by default, immune to state leaked by prior tests via a
// shared broker-state.json. Persistence tests that want the load call
// b.loadState() explicitly after construction.
var skipBrokerStateLoadOnConstruct = false

func NewBroker() *Broker {
	b := &Broker{
		channelStore:        channel.NewStore(),
		token:               generateToken(),
		shutdownCh:          make(chan struct{}),
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

func (b *Broker) appendMessageLocked(msg channelMessage) {
	b.messages = append(b.messages, msg)
	b.publishMessageLocked(msg)
}

func (b *Broker) publishMessageLocked(msg channelMessage) {
	for _, ch := range b.messageSubscribers {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (b *Broker) publishActionLocked(action officeActionLog) {
	for _, ch := range b.actionSubscribers {
		select {
		case ch <- action:
		default:
		}
	}
}

func (b *Broker) publishActivityLocked(activity agentActivitySnapshot) {
	for _, ch := range b.activitySubscribers {
		select {
		case ch <- activity:
		default:
		}
	}
}

func (b *Broker) SubscribeMessages(buffer int) (<-chan channelMessage, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan channelMessage, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.messageSubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.messageSubscribers[id]; ok {
			delete(b.messageSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

func (b *Broker) SubscribeActions(buffer int) (<-chan officeActionLog, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan officeActionLog, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.actionSubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.actionSubscribers[id]; ok {
			delete(b.actionSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

func (b *Broker) SubscribeActivity(buffer int) (<-chan agentActivitySnapshot, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan agentActivitySnapshot, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.activitySubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.activitySubscribers[id]; ok {
			delete(b.activitySubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

type officeChangeEvent struct {
	Kind string `json:"kind"` // "member_created", "member_removed", "channel_created", "channel_removed", "channel_updated", "office_reseeded"
	Slug string `json:"slug"`
}

func (b *Broker) SubscribeOfficeChanges(buffer int) (<-chan officeChangeEvent, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan officeChangeEvent, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.officeSubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.officeSubscribers[id]; ok {
			delete(b.officeSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

func (b *Broker) publishOfficeChangeLocked(evt officeChangeEvent) {
	for _, ch := range b.officeSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// SubscribeWikiEvents returns a channel of wiki commit notifications plus an
// unsubscribe func. The web UI's SSE loop uses this to push "wiki:write"
// events to the browser.
func (b *Broker) SubscribeWikiEvents(buffer int) (<-chan wikiWriteEvent, func()) {
	if buffer <= 0 {
		buffer = 1
	}
	ch := make(chan wikiWriteEvent, buffer)

	b.mu.Lock()
	id := b.nextSubscriberID
	b.nextSubscriberID++
	b.wikiSubscribers[id] = ch
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if existing, ok := b.wikiSubscribers[id]; ok {
			delete(b.wikiSubscribers, id)
			close(existing)
		}
		b.mu.Unlock()
	}
}

// PublishWikiEvent fans out a commit notification to all SSE subscribers.
// Implements the wikiEventPublisher interface consumed by WikiWorker.
func (b *Broker) PublishWikiEvent(evt wikiWriteEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.wikiSubscribers {
		select {
		case ch <- evt:
		default:
		}
	}
}

// WikiWorker returns the broker's attached wiki worker, or nil when the
// active memory backend is not markdown.
func (b *Broker) WikiWorker() *WikiWorker {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wikiWorker
}

// WikiIndex returns the broker's derived wiki index, or nil when the active
// memory backend is not markdown. HTTP handlers use this to run search queries
// against the structured fact store without going through the write worker.
func (b *Broker) WikiIndex() *WikiIndex {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.wikiIndex
}

func (b *Broker) UpdateAgentActivity(update agentActivitySnapshot) {
	slug := normalizeChannelSlug(update.Slug)
	if slug == "" {
		return
	}
	if update.LastTime == "" {
		update.LastTime = time.Now().UTC().Format(time.RFC3339)
	}
	update.Slug = slug

	b.mu.Lock()
	current := b.activity[slug]
	current.Slug = slug
	if update.Status != "" {
		current.Status = update.Status
	}
	if update.Activity != "" {
		current.Activity = update.Activity
	}
	if update.Detail != "" {
		current.Detail = update.Detail
	}
	if update.LastTime != "" {
		current.LastTime = update.LastTime
	}
	if update.TotalMs > 0 {
		current.TotalMs = update.TotalMs
	}
	if update.FirstEventMs >= 0 {
		current.FirstEventMs = update.FirstEventMs
	}
	if update.FirstTextMs >= 0 {
		current.FirstTextMs = update.FirstTextMs
	}
	if update.FirstToolMs >= 0 {
		current.FirstToolMs = update.FirstToolMs
	}
	b.activity[slug] = current
	b.publishActivityLocked(current)
	b.mu.Unlock()
}

// duplicateBroadcastWindow is how recent an earlier broadcast from the same
// agent in the same channel+thread must be to count as a duplicate. Set tight
// so legitimate quick follow-ups still land, but tight enough to catch the
// "same turn, paraphrased again" pattern that agents produce.
const duplicateBroadcastWindow = 30 * time.Second

// duplicateBroadcastSimilarity is the lower bound at which two messages are
// considered near-duplicates. 1.0 means "byte-identical"; we pick 0.85 to
// catch paraphrased restatements while letting actual new content through.
const duplicateBroadcastSimilarity = 0.85

// isDuplicateAgentBroadcastLocked returns true when the agent has already
// posted a nearly-identical message to the same (channel, thread) pair within
// duplicateBroadcastWindow. Must be called with b.mu held.
func (b *Broker) isDuplicateAgentBroadcastLocked(sender, channel, replyTo, content string) bool {
	newNorm := normalizeBroadcastContent(content)
	if newNorm == "" {
		return false
	}
	cutoff := time.Now().UTC().Add(-duplicateBroadcastWindow)
	// Walk backwards — most recent messages are at the end.
	for i := len(b.messages) - 1; i >= 0; i-- {
		prev := b.messages[i]
		ts, err := time.Parse(time.RFC3339, prev.Timestamp)
		if err == nil && ts.Before(cutoff) {
			break
		}
		if prev.From != sender {
			continue
		}
		if normalizeChannelSlug(prev.Channel) != channel {
			continue
		}
		if strings.TrimSpace(prev.ReplyTo) != strings.TrimSpace(replyTo) {
			continue
		}
		prevNorm := normalizeBroadcastContent(prev.Content)
		if prevNorm == "" {
			continue
		}
		if jaccardWordSimilarity(newNorm, prevNorm) >= duplicateBroadcastSimilarity {
			return true
		}
	}
	return false
}

// normalizeBroadcastContent lowercases and collapses whitespace so trivial
// formatting drift does not defeat the dedup check.
func normalizeBroadcastContent(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

// jaccardWordSimilarity returns the Jaccard similarity of the two strings'
// whitespace-split word sets. 1.0 = identical word sets; 0.0 = disjoint.
// Cheap and good enough to catch "ship it" / "ship it 🚀" style paraphrases.
func jaccardWordSimilarity(a, b string) float64 {
	wa := uniqueWordSet(a)
	wb := uniqueWordSet(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	inter := 0
	for w := range wa {
		if _, ok := wb[w]; ok {
			inter++
		}
	}
	union := len(wa) + len(wb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func uniqueWordSet(s string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, w := range strings.Fields(s) {
		// Strip leading/trailing ASCII punctuation so "court," and "court"
		// collapse to the same token for Jaccard. Keeps intra-word characters
		// like apostrophes inside "reviewer's".
		w = strings.TrimFunc(w, func(r rune) bool {
			switch r {
			case '.', ',', ';', ':', '!', '?', '—', '–', '-', '"', '\'', '(', ')', '[', ']', '`':
				return true
			}
			return false
		})
		if w == "" {
			continue
		}
		out[w] = struct{}{}
	}
	return out
}

// activityWatchdogEnabled controls whether NewBroker starts the background
// activity-watchdog goroutine. Tests that create many short-lived brokers set
// this to false via TestMain so goroutines don't accumulate and cause
// goleak/timeout failures. Production always runs with the default (true).
var activityWatchdogEnabled = true

// staleActivityThreshold is how long an agent can stay in a non-idle/non-error
// activity state before the watchdog forcibly resets it to idle. Set long
// enough to cover normal long turns (tool chains, big edits) but short enough
// that a crashed spawn does not leave the agent looking "active" for hours —
// which blocks the CEO's "Already active in this thread" re-route guard and
// prevents the specialist from being dispatched again.
const staleActivityThreshold = 5 * time.Minute

// reapStaleActivityLocked transitions any agent whose LastTime is older than
// staleActivityThreshold from "active"/"thinking"/"tool_use" back to "idle".
// Must be called with b.mu held. Returns the slugs that were reset so the
// caller can emit activity-change events after releasing the lock.
func (b *Broker) reapStaleActivityLocked(now time.Time) []agentActivitySnapshot {
	if len(b.activity) == 0 {
		return nil
	}
	var reset []agentActivitySnapshot
	for slug, snap := range b.activity {
		status := strings.ToLower(strings.TrimSpace(snap.Status))
		if status == "" || status == "idle" || status == "error" {
			continue
		}
		lastTime, err := time.Parse(time.RFC3339, snap.LastTime)
		if err != nil {
			// Unparseable LastTime means we cannot age the entry safely; leave it.
			continue
		}
		if now.Sub(lastTime) < staleActivityThreshold {
			continue
		}
		snap.Status = "idle"
		snap.Activity = "idle"
		snap.Detail = "stale activity reaped (no progress for " + staleActivityThreshold.String() + ")"
		snap.LastTime = now.UTC().Format(time.RFC3339)
		b.activity[slug] = snap
		reset = append(reset, snap)
	}
	return reset
}

// runActivityWatchdog scans the in-memory activity map every minute and
// resets agents that have been stuck in a non-terminal state past
// staleActivityThreshold. Stops when ctx is done so NewBroker can tear it
// down alongside the rest of the broker's lifecycle.
func (b *Broker) runActivityWatchdog(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			b.mu.Lock()
			reset := b.reapStaleActivityLocked(now)
			for _, snap := range reset {
				b.publishActivityLocked(snap)
			}
			b.mu.Unlock()
		}
	}
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

// requireAuth wraps a handler to enforce Bearer token authentication.
// Accepts token via Authorization header or ?token= query parameter (for EventSource which can't set headers).
func (b *Broker) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if b.requestHasBrokerAuth(r) {
			next(w, r)
			return
		}
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}
}

// Start launches the broker on the configured localhost port.
func (b *Broker) Start() error {
	b.ensureWikiWorker()
	b.ensureWikiSectionsCache()
	b.ensureReviewLog()
	b.ensureEntitySynthesizer()
	b.ensurePlaybookExecutionLog()
	b.ensurePlaybookSynthesizer()
	b.startReviewExpiryLoop(context.Background())
	return b.StartOnPort(brokeraddr.ResolvePort())
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
	b.mu.Unlock()

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
}

// StartOnPort launches the broker on the given port. Use 0 for an OS-assigned port.
func (b *Broker) StartOnPort(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", b.handleHealth) // no auth — used for liveness checks
	mux.HandleFunc("/version", b.handleVersion)
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
	mux.HandleFunc("/pam/actions", b.requireAuth(b.handlePamActions))
	mux.HandleFunc("/pam/action", b.requireAuth(b.handlePamAction))
	mux.HandleFunc("/scan/start", b.requireAuth(b.handleScanStart))
	mux.HandleFunc("/scan/status", b.requireAuth(b.handleScanStatus))
	mux.HandleFunc("/studio/generate-package", b.requireAuth(b.handleStudioGeneratePackage))
	mux.HandleFunc("/studio/bootstrap-package", b.requireAuth(b.handleOperationBootstrapPackage))
	mux.HandleFunc("/operations/bootstrap-package", b.requireAuth(b.handleOperationBootstrapPackage))
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
	mux.HandleFunc("/skills", b.requireAuth(b.handleSkills))
	mux.HandleFunc("/skills/", b.requireAuth(b.handleSkillsSubpath))
	// GET /commands — slash-command registry mirror so the web composer
	// renders the same command set as the TUI. See broker_commands.go.
	mux.HandleFunc("/commands", b.requireAuth(b.handleCommands))
	mux.HandleFunc("/telegram/groups", b.requireAuth(b.handleTelegramGroups))
	mux.HandleFunc("/bridges", b.requireAuth(b.handleBridge))
	mux.HandleFunc("/queue", b.requireAuth(b.handleQueue))
	mux.HandleFunc("/company", b.requireAuth(b.handleCompany))
	mux.HandleFunc("/config", b.requireAuth(b.handleConfig))
	mux.HandleFunc("/nex/register", b.requireAuth(b.handleNexRegister))
	mux.HandleFunc("/v1/logs", b.requireAuth(b.handleOTLPLogs))
	mux.HandleFunc("/events", b.handleEvents)
	mux.HandleFunc("/agent-stream/", b.requireAuth(b.handleAgentStream))
	mux.HandleFunc("/agent-tool-event", b.requireAuth(b.handleAgentToolEvent))
	mux.HandleFunc("/web-token", b.handleWebToken)
	// Onboarding: state/progress/complete + prereqs/templates/validate-key + checklist.
	// completeFn posts the first task as a human message and seeds the team.
	onboarding.RegisterRoutes(mux, b.onboardingCompleteFn, b.packSlug, b.requireAuth)
	// Workspace wipes: POST /workspace/reset (narrow) and /workspace/shred (full).
	// Shred requests launcher shutdown after the response so live in-memory
	// broker state cannot write stale messages back onto disk.
	// Auth-gated via requireAuth because shred permanently deletes state and
	// must not be reachable without the broker token.
	workspace.RegisterRoutesWithOptions(mux, workspace.RouteOptions{
		AuthMiddleware: b.requireAuth,
		AfterShred: func(workspace.Result) {
			b.requestShutdown()
		},
	})

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
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
		_ = os.WriteFile(tokenFile, []byte(b.token), 0o600)
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

// ShutdownRequested is closed when a destructive operation needs the launcher
// process to stop after the HTTP response has reached the client.
func (b *Broker) ShutdownRequested() <-chan struct{} {
	if b.shutdownCh == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return b.shutdownCh
}

func (b *Broker) requestShutdown() {
	if b == nil || b.shutdownCh == nil {
		return
	}
	b.shutdownOnce.Do(func() {
		close(b.shutdownCh)
	})
}

func (b *Broker) rateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Exempt liveness and version checks from all rate limiting.
		if isLivenessPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		authenticated := b.requestHasBrokerAuth(r)

		// Authenticated callers bypass the IP-scoped bucket (web UI and trusted
		// tools must not share a bucket with anonymous callers), but authenticated
		// *agent* traffic is still subject to a separate per-agent bucket below.
		if !authenticated {
			retryAfter, limited := b.consumeRateLimit(clientIPFromRequest(r))
			if limited {
				writeRateLimitedResponse(w, retryAfter)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// Authenticated — check the per-agent bucket so a prompt-injected agent
		// cannot loop forever on team_broadcast / team_action_execute. Operator
		// traffic (web UI) does not set X-WUPHF-Agent and is exempt.
		agentSlug := strings.TrimSpace(r.Header.Get(agentRateLimitHeader))
		if agentSlug == "" || isAgentBucketExemptPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		retryAfter, limited := b.consumeAgentRateLimit(agentSlug)
		if limited {
			log.Printf("broker: agent %q tripped per-agent rate limit (%d req / %s) on %s — possible runaway loop", agentSlug, b.agentRateLimitRequests, b.agentRateLimitWindow, r.URL.Path)
			writeRateLimitedResponse(w, retryAfter)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLivenessPath reports whether the request path is a pure liveness or
// version probe that must never be rate-limited (operators need these even
// when the broker is saturated). /web-token is NOT on this list — it
// dispenses the broker bearer and an unthrottled enumeration path would be
// surprising, even though the handler itself is loopback+Host gated.
func isLivenessPath(path string) bool {
	return path == "/health" || path == "/version"
}

// isAgentBucketExemptPath reports whether the path is an open SSE stream or
// otherwise doesn't represent a tool-call-shaped loopable request. These
// connections stay open for a long time rather than spinning on request
// count, so counting them against the agent bucket would be incorrect.
func isAgentBucketExemptPath(path string) bool {
	if path == "/events" {
		return true
	}
	if strings.HasPrefix(path, "/agent-stream/") {
		return true
	}
	return false
}

func writeRateLimitedResponse(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = io.WriteString(w, `{"error":"rate_limited"}`)
}

func (b *Broker) consumeRateLimit(clientIP string) (time.Duration, bool) {
	limit := b.rateLimitRequests
	if limit <= 0 {
		limit = defaultRateLimitRequestsPerWindow
	}
	window := b.rateLimitWindow
	if window <= 0 {
		window = defaultRateLimitWindow
	}

	now := time.Now()
	key := rateLimitKey(clientIP)
	cutoff := now.Add(-window)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.rateLimitBuckets == nil {
		b.rateLimitBuckets = make(map[string]ipRateLimitBucket)
	}
	if b.lastRateLimitPrune.IsZero() || now.Sub(b.lastRateLimitPrune) >= window {
		for ip, bucket := range b.rateLimitBuckets {
			bucket.timestamps = pruneRateLimitEntries(bucket.timestamps, cutoff)
			if len(bucket.timestamps) == 0 {
				delete(b.rateLimitBuckets, ip)
				continue
			}
			b.rateLimitBuckets[ip] = bucket
		}
		b.lastRateLimitPrune = now
	}

	bucket := b.rateLimitBuckets[key]
	bucket.timestamps = pruneRateLimitEntries(bucket.timestamps, cutoff)
	if len(bucket.timestamps) >= limit {
		retryAfter := bucket.timestamps[0].Add(window).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		b.rateLimitBuckets[key] = bucket
		return retryAfter, true
	}

	bucket.timestamps = append(bucket.timestamps, now)
	b.rateLimitBuckets[key] = bucket
	return 0, false
}

// consumeAgentRateLimit counts an authenticated request against the per-agent
// bucket keyed by the X-WUPHF-Agent header. It mirrors consumeRateLimit but
// lives in its own bucket so agent traffic cannot starve operator traffic and
// vice versa.
func (b *Broker) consumeAgentRateLimit(agentSlug string) (time.Duration, bool) {
	agentSlug = strings.TrimSpace(agentSlug)
	if agentSlug == "" {
		return 0, false
	}

	limit := b.agentRateLimitRequests
	if limit <= 0 {
		limit = defaultAgentRateLimitRequestsPerWindow
	}
	window := b.agentRateLimitWindow
	if window <= 0 {
		window = defaultAgentRateLimitWindow
	}

	now := time.Now()
	cutoff := now.Add(-window)

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.agentRateLimitBuckets == nil {
		b.agentRateLimitBuckets = make(map[string]ipRateLimitBucket)
	}
	if b.lastAgentRateLimitPrune.IsZero() || now.Sub(b.lastAgentRateLimitPrune) >= window {
		for slug, bucket := range b.agentRateLimitBuckets {
			bucket.timestamps = pruneRateLimitEntries(bucket.timestamps, cutoff)
			if len(bucket.timestamps) == 0 {
				delete(b.agentRateLimitBuckets, slug)
				continue
			}
			b.agentRateLimitBuckets[slug] = bucket
		}
		b.lastAgentRateLimitPrune = now
	}

	bucket := b.agentRateLimitBuckets[agentSlug]
	bucket.timestamps = pruneRateLimitEntries(bucket.timestamps, cutoff)
	if len(bucket.timestamps) >= limit {
		retryAfter := bucket.timestamps[0].Add(window).Sub(now)
		if retryAfter < time.Second {
			retryAfter = time.Second
		}
		b.agentRateLimitBuckets[agentSlug] = bucket
		return retryAfter, true
	}

	bucket.timestamps = append(bucket.timestamps, now)
	b.agentRateLimitBuckets[agentSlug] = bucket
	return 0, false
}

func externalWorkflowRetryAfter(err error, now time.Time) (time.Time, bool) {
	if err == nil {
		return time.Time{}, false
	}
	matches := externalRetryAfterPattern.FindStringSubmatch(err.Error())
	if len(matches) < 2 {
		return time.Time{}, false
	}
	retryAt, parseErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(matches[1]))
	if parseErr != nil {
		return time.Time{}, false
	}
	if retryAt.Before(now) {
		return now, true
	}
	return retryAt, true
}

func pruneRateLimitEntries(entries []time.Time, cutoff time.Time) []time.Time {
	keepIdx := 0
	for keepIdx < len(entries) && !entries[keepIdx].After(cutoff) {
		keepIdx++
	}
	if keepIdx == 0 {
		return entries
	}
	if keepIdx >= len(entries) {
		return nil
	}
	return entries[keepIdx:]
}

func rateLimitKey(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil && strings.TrimSpace(host) != "" {
		return host
	}
	return remoteAddr
}

func clientIPFromRequest(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	if trustForwardedClientIP(r.RemoteAddr) {
		if forwarded := firstForwardedIP(r.Header.Get("X-Forwarded-For")); forwarded != "" {
			return forwarded
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
			return rateLimitKey(realIP)
		}
	}
	return rateLimitKey(r.RemoteAddr)
}

func firstForwardedIP(value string) string {
	for _, part := range strings.Split(value, ",") {
		candidate := rateLimitKey(part)
		if candidate == "" || candidate == "unknown" {
			continue
		}
		if ip := net.ParseIP(candidate); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func trustForwardedClientIP(remoteAddr string) bool {
	host := rateLimitKey(remoteAddr)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func setProxyClientIPHeaders(header http.Header, remoteAddr string) {
	if header == nil {
		return
	}
	if clientIP := rateLimitKey(remoteAddr); clientIP != "unknown" {
		header.Set("X-Forwarded-For", clientIP)
		header.Set("X-Real-IP", clientIP)
	}
}

func (b *Broker) requestAuthToken(r *http.Request) string {
	if r == nil {
		return ""
	}
	if token := strings.TrimSpace(r.URL.Query().Get("token")); token != "" {
		return token
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	return ""
}

func (b *Broker) requestHasBrokerAuth(r *http.Request) bool {
	return b.requestAuthToken(r) == b.token
}

// corsMiddleware adds CORS headers only for the web UI origin.
// If no web UI origins are configured, no CORS headers are set.
//
// Requests with empty or "null" Origin are same-origin or non-browser callers
// (curl, Go tests, CLI tools). They do not need a CORS header to succeed. We
// intentionally do NOT set Access-Control-Allow-Origin: * for them — that
// would let a file:// page or sandboxed iframe make authenticated cross-origin
// reads once it has the Bearer token.
func (b *Broker) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "null" && len(b.webUIOrigins) > 0 {
			for _, allowed := range b.webUIOrigins {
				if origin == allowed {
					w.Header().Set("Access-Control-Allow-Origin", allowed)
					w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
					break
				}
			}
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackRemote reports whether r.RemoteAddr is loopback (127.0.0.0/8, ::1).
// Returns false if RemoteAddr is empty or unparseable — fail closed.
func isLoopbackRemote(r *http.Request) bool {
	if r == nil {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}

// hostHeaderIsLoopback reports whether the HTTP Host header is a loopback
// hostname (localhost, 127.0.0.1, ::1). DNS-rebinding attacks rely on r.Host
// being an attacker-controlled name like rebind.example.com that only
// resolves to 127.0.0.1 at request time; Go's default mux routes by path and
// ignores Host, so routes that sit on 127.0.0.1 will happily serve responses
// to the attacker's origin. Validating Host on sensitive handlers closes this.
//
// The port component is intentionally not validated — the broker and web UI
// run on different ports and dev setups may proxy through 80/443. The
// loopback hostname is the security boundary. Assumes no trusted reverse
// proxy sits in front of the listener; operators adding one must re-evaluate
// (r.Host would then reflect the proxy's upstream, not the browser origin).
func hostHeaderIsLoopback(r *http.Request) bool {
	if r == nil {
		return false
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// webUIRebindGuard wraps a handler with a DNS-rebinding / cross-origin gate.
// It rejects any request whose RemoteAddr is not loopback or whose Host header
// is not a recognized localhost form. Applied on the web UI mux because that
// mux auto-attaches the broker's Bearer token on forwarded requests; without
// this gate, a malicious website can use DNS rebinding to ride the token.
func webUIRebindGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackRemote(r) || !hostHeaderIsLoopback(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleWebToken returns the broker token to localhost clients without requiring auth.
// This lets the web UI fetch the token to authenticate subsequent API calls.
//
// DNS rebinding: even though the listener binds 127.0.0.1, an attacker's
// DNS record with a short TTL can point rebind.example.com at 127.0.0.1
// after the browser's origin check passes. Go's default mux routes purely
// on path, so without an explicit Host check the response would flow back
// to the attacker's origin. Validate both RemoteAddr AND Host here.
func (b *Broker) handleWebToken(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRemote(r) || !hostHeaderIsLoopback(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": b.token})
}

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
	if stream == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	line := formatAgentToolEvent(body.Phase, body.Tool, body.Args, body.Result, body.Error)
	if line != "" {
		stream.Push(line)
	}
	w.WriteHeader(http.StatusOK)
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
func (b *Broker) ServeWebUI(port int) {
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
	mux.Handle("/", cacheControlMiddleware(fileServer))
	go func() {
		if err := http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), mux); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("broker web UI proxy: listen on :%d: %v", port, err)
		}
	}()
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

		proxyReq, err := http.NewRequest(r.Method, target, r.Body)
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
	return b.HasBlockingRequest()
}

func (b *Broker) HasBlockingRequest() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, req := range b.requests {
		if requestIsActive(req) && req.Blocking {
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

func (b *Broker) dueSchedulerJobsLocked(now time.Time) []schedulerJob {
	now = now.UTC()
	var out []schedulerJob
	for _, job := range b.scheduler {
		if strings.EqualFold(strings.TrimSpace(job.Status), "done") || strings.EqualFold(strings.TrimSpace(job.Status), "canceled") {
			continue
		}
		target := strings.TrimSpace(job.NextRun)
		if target == "" {
			continue
		}
		dueAt, err := time.Parse(time.RFC3339, target)
		if err != nil {
			continue
		}
		if !dueAt.After(now) {
			out = append(out, job)
		}
	}
	return out
}

func (b *Broker) Reset() {
	b.mu.Lock()
	mode := b.sessionMode
	agent := b.oneOnOneAgent
	b.messages = nil
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
	_ = os.Remove(brokerStateSnapshotPath())
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

func brokerStateSnapshotPath() string {
	return brokerStatePath() + ".last-good"
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
	path := brokerStatePath()
	state, err := loadBrokerStateFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		state = brokerState{}
	}
	snapshotPath := brokerStateSnapshotPath()
	if snapshot, snapErr := loadBrokerStateFile(snapshotPath); snapErr == nil {
		if brokerStateActivityScore(snapshot) > brokerStateActivityScore(state) {
			state = snapshot
		}
	}
	b.messages = state.Messages
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
	b.ensureDefaultChannelsLocked()
	b.ensureDefaultOfficeMembersLocked()
	b.normalizeLoadedStateLocked()
	return nil
}

func (b *Broker) saveLocked() error {
	path := brokerStatePath()
	snapshotPath := brokerStateSnapshotPath()
	if len(b.messages) == 0 && len(b.tasks) == 0 && len(activeRequests(b.requests)) == 0 && len(b.actions) == 0 && len(b.signals) == 0 && len(b.decisions) == 0 && len(b.watchdogs) == 0 && len(b.policies) == 0 && len(b.scheduler) == 0 && len(b.skills) == 0 && len(b.sharedMemory) == 0 && isDefaultChannelState(b.channels) && isDefaultOfficeMemberState(b.members) && b.counter == 0 && b.notificationSince == "" && b.insightsSince == "" && usageStateIsZero(b.usage) && b.sessionMode == SessionModeOffice && b.oneOnOneAgent == DefaultOneOnOneAgent {
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
// owns one path) but broke the test suite: 22 *_test.go files override
// brokerStatePath, plus a leaked tempdir from worktree_guard_test.go init()
// is shared across every test that doesn't override. Two saves landing on
// the same path could interleave like A.WriteFile / B.WriteFile / A.Rename /
// B.Rename — and B's Rename failed with "no such file or directory" because
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
		if b.requests[i].Blocking || strings.TrimSpace(b.requests[i].Kind) == "interview" {
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
		b.ensureTaskOwnerChannelMembershipLocked(b.tasks[i].Channel, b.tasks[i].Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(&b.tasks[i])
		b.scheduleTaskLifecycleLocked(&b.tasks[i])
		_ = b.syncTaskWorktreeLocked(&b.tasks[i])
	}
	b.pendingInterview = firstBlockingRequest(b.requests)
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

func requestIsActive(req humanInterview) bool {
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if req.Answered != nil {
		return false
	}
	return status == "" || status == "pending" || status == "open"
}

func requestNeedsHumanDecision(req humanInterview) bool {
	switch strings.TrimSpace(req.Kind) {
	case "interview", "approval", "confirm", "choice":
		return true
	default:
		return req.Required
	}
}

func requestOptionDefaults(kind string) ([]interviewOption, string) {
	switch normalizeRequestKind(kind) {
	case "approval":
		return []interviewOption{
			{ID: "approve", Label: "Approve", Description: "Green-light this and let the team execute immediately."},
			{ID: "approve_with_note", Label: "Approve with note", Description: "Proceed, but attach explicit constraints or guardrails.", RequiresText: true, TextHint: "Type the conditions, constraints, or guardrails the team must follow."},
			{ID: "needs_more_info", Label: "Need more info", Description: "Gather more context before making the approval call."},
			{ID: "reject", Label: "Reject", Description: "Do not proceed with this."},
			{ID: "reject_with_steer", Label: "Reject with steer", Description: "Do not proceed as proposed. Redirect the team with clearer steering.", RequiresText: true, TextHint: "Type the steering, redirect, or rationale for rejecting this request."},
		}, "approve"
	case "confirm":
		return []interviewOption{
			{ID: "confirm_proceed", Label: "Confirm", Description: "Looks good. Proceed as planned."},
			{ID: "adjust", Label: "Adjust", Description: "Proceed only after applying the changes you specify.", RequiresText: true, TextHint: "Type the changes that must happen before proceeding."},
			{ID: "reassign", Label: "Reassign", Description: "Move this to a different owner or scope.", RequiresText: true, TextHint: "Type who should own this instead, or how the scope should change."},
			{ID: "hold", Label: "Hold", Description: "Do not act yet. Keep this pending for review."},
		}, "confirm_proceed"
	case "choice":
		return []interviewOption{
			{ID: "move_fast", Label: "Move fast", Description: "Bias toward speed. Ship now and iterate later."},
			{ID: "balanced", Label: "Balanced", Description: "Balance speed, risk, and quality."},
			{ID: "be_careful", Label: "Be careful", Description: "Bias toward caution and a tighter review loop."},
			{ID: "needs_more_info", Label: "Need more info", Description: "Gather more context before deciding.", RequiresText: true, TextHint: "Type what is missing or what should be investigated next."},
			{ID: "delegate", Label: "Delegate", Description: "Hand this to a specific owner for a closer call.", RequiresText: true, TextHint: "Type who should own this decision and any guidance for them."},
		}, "balanced"
	case "interview":
		return []interviewOption{
			{ID: "answer_directly", Label: "Answer directly", Description: "Respond in your own words below."},
			{ID: "need_more_context", Label: "Need more context", Description: "Ask the office to bring back more context before you decide.", RequiresText: true, TextHint: "Type what context is missing or what should be clarified next."},
		}, "answer_directly"
	case "freeform", "secret":
		return []interviewOption{
			{ID: "proceed", Label: "Proceed", Description: "Let the team handle it with their best judgment."},
			{ID: "give_direction", Label: "Give direction", Description: "Proceed, but only after you provide specific guidance.", RequiresText: true, TextHint: "Type the direction or constraints the team should follow."},
			{ID: "delegate", Label: "Delegate", Description: "Route this to a specific person.", RequiresText: true, TextHint: "Type who should own this and what they should do."},
			{ID: "hold", Label: "Hold", Description: "Pause until you review this further."},
		}, "proceed"
	default:
		return []interviewOption{
			{ID: "proceed", Label: "Proceed", Description: "Let the team handle it with their best judgment."},
			{ID: "give_direction", Label: "Give direction", Description: "Add specific guidance the team should follow.", RequiresText: true, TextHint: "Provide the direction or constraints the team should follow."},
			{ID: "delegate", Label: "Delegate", Description: "Route this to a specific person or role.", RequiresText: true, TextHint: "Name the person or role that should own the next call."},
			{ID: "hold", Label: "Hold", Description: "Pause until you review this further."},
		}, "proceed"
	}
}

func enrichRequestOptions(kind string, options []interviewOption) []interviewOption {
	if len(options) == 0 {
		defaults, _ := requestOptionDefaults(kind)
		return defaults
	}
	defaults, _ := requestOptionDefaults(kind)
	meta := make(map[string]interviewOption, len(defaults))
	for _, option := range defaults {
		meta[strings.TrimSpace(option.ID)] = option
	}
	out := make([]interviewOption, 0, len(options))
	for _, option := range options {
		id := strings.TrimSpace(option.ID)
		option.Label = strings.TrimSpace(option.Label)
		option.Description = strings.TrimSpace(option.Description)
		option.TextHint = strings.TrimSpace(option.TextHint)
		if id == "" && option.Label != "" {
			id = normalizeRequestOptionID(option.Label)
			option.ID = id
		}
		if base, ok := meta[id]; ok {
			if !option.RequiresText {
				option.RequiresText = base.RequiresText
			}
			if strings.TrimSpace(option.TextHint) == "" {
				option.TextHint = base.TextHint
			}
			if strings.TrimSpace(option.Label) == "" {
				option.Label = base.Label
			}
			if strings.TrimSpace(option.Description) == "" {
				option.Description = base.Description
			}
		}
		out = append(out, option)
	}
	return out
}

func normalizeRequestOptions(kind, recommendedID string, options []interviewOption) ([]interviewOption, string) {
	normalized := enrichRequestOptions(kind, options)
	recommendedID = strings.TrimSpace(recommendedID)
	if recommendedID != "" {
		for _, option := range normalized {
			if strings.TrimSpace(option.ID) == recommendedID {
				return normalized, recommendedID
			}
		}
	}
	_, fallback := requestOptionDefaults(kind)
	for _, option := range normalized {
		if strings.TrimSpace(option.ID) == fallback {
			return normalized, fallback
		}
	}
	if len(normalized) > 0 {
		return normalized, strings.TrimSpace(normalized[0].ID)
	}
	return normalized, fallback
}

func findRequestOption(req humanInterview, choiceID string) *interviewOption {
	choiceID = strings.TrimSpace(choiceID)
	if choiceID == "" {
		return nil
	}
	for i := range req.Options {
		if strings.TrimSpace(req.Options[i].ID) == choiceID {
			return &req.Options[i]
		}
	}
	return nil
}

func formatRequestAnswerMessage(req humanInterview, answer interviewAnswer) string {
	if req.Secret {
		return fmt.Sprintf("Answered @%s's request privately.", req.From)
	}
	custom := strings.TrimSpace(answer.CustomText)
	switch strings.TrimSpace(answer.ChoiceID) {
	case "approve":
		return fmt.Sprintf("Approved @%s's request.", req.From)
	case "approve_with_note":
		if custom != "" {
			return fmt.Sprintf("Approved @%s's request with note: %s", req.From, custom)
		}
		return fmt.Sprintf("Approved @%s's request with a note.", req.From)
	case "reject":
		return fmt.Sprintf("Rejected @%s's request.", req.From)
	case "reject_with_steer":
		if custom != "" {
			return fmt.Sprintf("Rejected @%s's request with steering: %s", req.From, custom)
		}
		return fmt.Sprintf("Rejected @%s's request with steering.", req.From)
	case "confirm_proceed":
		return fmt.Sprintf("Confirmed @%s's request.", req.From)
	case "adjust":
		if custom != "" {
			return fmt.Sprintf("Requested adjustments from @%s: %s", req.From, custom)
		}
		return fmt.Sprintf("Requested adjustments from @%s.", req.From)
	case "reassign":
		if custom != "" {
			return fmt.Sprintf("Reassigned @%s's request: %s", req.From, custom)
		}
		return fmt.Sprintf("Reassigned @%s's request.", req.From)
	case "hold":
		return fmt.Sprintf("Put @%s's request on hold.", req.From)
	case "delegate":
		if custom != "" {
			return fmt.Sprintf("Delegated @%s's request: %s", req.From, custom)
		}
		return fmt.Sprintf("Delegated @%s's request.", req.From)
	case "needs_more_info":
		if custom != "" {
			return fmt.Sprintf("Asked @%s for more information: %s", req.From, custom)
		}
		return fmt.Sprintf("Asked @%s for more information.", req.From)
	}
	if custom != "" && strings.TrimSpace(answer.ChoiceText) != "" {
		return fmt.Sprintf("Answered @%s's request with %s: %s", req.From, answer.ChoiceText, custom)
	}
	if custom != "" {
		return fmt.Sprintf("Answered @%s's request: %s", req.From, custom)
	}
	if strings.TrimSpace(answer.ChoiceText) != "" {
		return fmt.Sprintf("Answered @%s's request: %s", req.From, answer.ChoiceText)
	}
	return fmt.Sprintf("Answered @%s's request.", req.From)
}

func activeRequests(requests []humanInterview) []humanInterview {
	out := make([]humanInterview, 0, len(requests))
	for _, req := range requests {
		if requestIsActive(req) {
			out = append(out, req)
		}
	}
	return out
}

func firstBlockingRequest(requests []humanInterview) *humanInterview {
	for i := range requests {
		if requestIsActive(requests[i]) && requests[i].Blocking {
			req := requests[i]
			return &req
		}
	}
	return nil
}

func normalizeRequestKind(kind string) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" {
		return "choice"
	}
	return kind
}

func normalizeRequestOptionID(label string) string {
	label = strings.TrimSpace(strings.ToLower(label))
	label = strings.ReplaceAll(label, "-", "_")
	label = strings.ReplaceAll(label, " ", "_")
	return label
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

func (b *Broker) canAccessChannelLocked(slug, channel string) bool {
	slug = normalizeActorSlug(slug)
	channel = normalizeChannelSlug(channel)
	if b.sessionMode == SessionModeOneOnOne {
		if slug == "" || slug == "you" || slug == "human" {
			return true
		}
		return slug == b.oneOnOneAgent
	}
	if slug == "" || slug == "you" || slug == "human" || slug == "nex" {
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

func (b *Broker) SetSchedulerJob(job schedulerJob) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	job = normalizeSchedulerJob(job)
	if job.Slug == "" {
		return fmt.Errorf("job slug required")
	}
	if err := b.scheduleJobLocked(job); err != nil {
		return err
	}
	return b.saveLocked()
}

func (b *Broker) ScheduleTaskFollowUp(taskID, channel, owner, label, payload string, when time.Time) error {
	return b.scheduleJob(schedulerJob{
		Slug:            normalizeSchedulerSlug("task_follow_up", channel, taskID),
		Kind:            "task_follow_up",
		Label:           label,
		TargetType:      "task",
		TargetID:        strings.TrimSpace(taskID),
		Channel:         normalizeChannelSlug(channel),
		IntervalMinutes: 0,
		DueAt:           when.UTC().Format(time.RFC3339),
		NextRun:         when.UTC().Format(time.RFC3339),
		Status:          "scheduled",
		Payload:         payload,
	})
}

func (b *Broker) ScheduleRequestFollowUp(requestID, channel, label, payload string, when time.Time) error {
	return b.scheduleJob(schedulerJob{
		Slug:            normalizeSchedulerSlug("request_follow_up", channel, requestID),
		Kind:            "request_follow_up",
		Label:           label,
		TargetType:      "request",
		TargetID:        strings.TrimSpace(requestID),
		Channel:         normalizeChannelSlug(channel),
		IntervalMinutes: 0,
		DueAt:           when.UTC().Format(time.RFC3339),
		NextRun:         when.UTC().Format(time.RFC3339),
		Status:          "scheduled",
		Payload:         payload,
	})
}

func (b *Broker) ScheduleRecheck(channel, targetType, targetID, label, payload string, when time.Time) error {
	return b.scheduleJob(schedulerJob{
		Slug:            normalizeSchedulerSlug("recheck", channel, targetType, targetID),
		Kind:            "recheck",
		Label:           label,
		TargetType:      strings.TrimSpace(targetType),
		TargetID:        strings.TrimSpace(targetID),
		Channel:         normalizeChannelSlug(channel),
		IntervalMinutes: 0,
		DueAt:           when.UTC().Format(time.RFC3339),
		NextRun:         when.UTC().Format(time.RFC3339),
		Status:          "scheduled",
		Payload:         payload,
	})
}

func (b *Broker) scheduleJob(job schedulerJob) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	job = normalizeSchedulerJob(job)
	if job.Slug == "" {
		return fmt.Errorf("job slug required")
	}
	if job.Channel == "" {
		job.Channel = "general"
	}
	if err := b.scheduleJobLocked(job); err != nil {
		return err
	}
	return b.saveLocked()
}

func (b *Broker) scheduleJobLocked(job schedulerJob) error {
	for i := range b.scheduler {
		if !schedulerJobMatches(b.scheduler[i], job) {
			continue
		}
		b.scheduler[i] = job
		return nil
	}
	b.scheduler = append(b.scheduler, job)
	return nil
}

func normalizeSchedulerSlug(parts ...string) string {
	var filtered []string
	for _, part := range parts {
		part = normalizeSlugPart(part)
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return strings.Join(filtered, ":")
}

func normalizeSlugPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

func normalizeSchedulerJob(job schedulerJob) schedulerJob {
	job.Slug = strings.TrimSpace(job.Slug)
	job.Kind = strings.TrimSpace(job.Kind)
	job.Label = strings.TrimSpace(job.Label)
	job.TargetType = strings.TrimSpace(job.TargetType)
	job.TargetID = strings.TrimSpace(job.TargetID)
	job.Channel = normalizeChannelSlug(job.Channel)
	job.Provider = strings.TrimSpace(job.Provider)
	job.ScheduleExpr = strings.TrimSpace(job.ScheduleExpr)
	job.WorkflowKey = strings.TrimSpace(job.WorkflowKey)
	job.SkillName = strings.TrimSpace(job.SkillName)
	if job.Channel == "" {
		job.Channel = "general"
	}
	job.Payload = strings.TrimSpace(job.Payload)
	job.Status = strings.TrimSpace(job.Status)
	if job.Status == "" {
		job.Status = "scheduled"
	}
	if job.IntervalMinutes < 0 {
		job.IntervalMinutes = 0
	}
	if job.DueAt == "" && job.NextRun != "" {
		job.DueAt = job.NextRun
	}
	if job.NextRun == "" && job.DueAt != "" {
		job.NextRun = job.DueAt
	}
	return job
}

func schedulerJobMatches(existing, candidate schedulerJob) bool {
	if existing.Slug != "" && candidate.Slug != "" && existing.Slug == candidate.Slug {
		return true
	}
	if existing.Kind != "" && candidate.Kind != "" && existing.Kind != candidate.Kind {
		return false
	}
	if existing.TargetType != "" && candidate.TargetType != "" && existing.TargetType != candidate.TargetType {
		return false
	}
	if existing.TargetID != "" && candidate.TargetID != "" && existing.TargetID != candidate.TargetID {
		return false
	}
	if existing.Channel != "" && candidate.Channel != "" && existing.Channel != candidate.Channel {
		return false
	}
	return existing.Kind != "" && existing.Kind == candidate.Kind && existing.TargetType == candidate.TargetType && existing.TargetID == candidate.TargetID && existing.Channel == candidate.Channel
}

func schedulerJobDue(job schedulerJob, now time.Time) bool {
	if strings.EqualFold(job.Status, "done") || strings.EqualFold(job.Status, "canceled") {
		return false
	}
	if job.DueAt != "" {
		if due, err := time.Parse(time.RFC3339, job.DueAt); err == nil && !due.After(now) {
			return true
		}
	}
	if job.NextRun != "" {
		if due, err := time.Parse(time.RFC3339, job.NextRun); err == nil && !due.After(now) {
			return true
		}
	}
	return false
}

func (b *Broker) completeSchedulerJobsLocked(targetType, targetID, channel string) {
	for i := range b.scheduler {
		job := &b.scheduler[i]
		if targetType != "" && job.TargetType != targetType {
			continue
		}
		if targetID != "" && job.TargetID != targetID {
			continue
		}
		if channel != "" && job.Channel != "" && normalizeChannelSlug(job.Channel) != normalizeChannelSlug(channel) {
			continue
		}
		job.Status = "done"
		job.DueAt = ""
		job.NextRun = ""
		job.LastRun = time.Now().UTC().Format(time.RFC3339)
	}
}

func (b *Broker) scheduleTaskLifecycleLocked(task *teamTask) {
	if task == nil {
		return
	}
	normalizeTaskPlan(task)
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	followUpMinutes := config.ResolveTaskFollowUpInterval()
	recheckMinutes := config.ResolveTaskRecheckInterval()
	reminderMinutes := config.ResolveTaskReminderInterval()
	now := time.Now().UTC()
	if strings.EqualFold(task.Status, "done") || strings.EqualFold(task.Status, "canceled") || strings.EqualFold(task.Status, "cancelled") {
		task.FollowUpAt = ""
		task.ReminderAt = ""
		task.RecheckAt = ""
		task.DueAt = ""
		b.completeSchedulerJobsLocked("task", task.ID, taskChannel)
		b.resolveWatchdogAlertsLocked("task", task.ID, taskChannel)
		return
	}
	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case "in_progress":
		due := now.Add(time.Duration(followUpMinutes) * time.Minute)
		task.FollowUpAt = due.Format(time.RFC3339)
		task.ReminderAt = due.Add(time.Duration(reminderMinutes) * time.Minute).Format(time.RFC3339)
		task.RecheckAt = due.Add(time.Duration(recheckMinutes) * time.Minute).Format(time.RFC3339)
		task.DueAt = task.FollowUpAt
		_ = b.scheduleJobLocked(normalizeSchedulerJob(schedulerJob{
			Slug:       normalizeSchedulerSlug("task_follow_up", taskChannel, task.ID),
			Kind:       "task_follow_up",
			Label:      "Follow up on " + task.Title,
			TargetType: "task",
			TargetID:   task.ID,
			Channel:    taskChannel,
			DueAt:      task.FollowUpAt,
			NextRun:    task.FollowUpAt,
			Status:     "scheduled",
			Payload:    task.Details,
		}))
	default:
		due := now.Add(time.Duration(recheckMinutes) * time.Minute)
		task.RecheckAt = due.Format(time.RFC3339)
		task.ReminderAt = due.Add(time.Duration(reminderMinutes) * time.Minute).Format(time.RFC3339)
		task.FollowUpAt = task.RecheckAt
		task.DueAt = task.RecheckAt
		_ = b.scheduleJobLocked(normalizeSchedulerJob(schedulerJob{
			Slug:       normalizeSchedulerSlug("recheck", taskChannel, "task", task.ID),
			Kind:       "recheck",
			Label:      "Recheck task " + truncateSummary(task.Title, 48),
			TargetType: "task",
			TargetID:   task.ID,
			Channel:    taskChannel,
			DueAt:      task.RecheckAt,
			NextRun:    task.RecheckAt,
			Status:     "scheduled",
			Payload:    task.Details,
		}))
	}
}

func (b *Broker) scheduleRequestLifecycleLocked(req *humanInterview) {
	if req == nil {
		return
	}
	reqChannel := normalizeChannelSlug(req.Channel)
	if reqChannel == "" {
		reqChannel = "general"
	}
	reminderMinutes := config.ResolveTaskReminderInterval()
	followUpMinutes := config.ResolveTaskFollowUpInterval()
	now := time.Now().UTC()
	if strings.EqualFold(req.Status, "answered") || strings.EqualFold(req.Status, "canceled") {
		req.DueAt = ""
		req.ReminderAt = ""
		req.RecheckAt = ""
		req.FollowUpAt = ""
		b.completeSchedulerJobsLocked("request", req.ID, reqChannel)
		b.resolveWatchdogAlertsLocked("request", req.ID, reqChannel)
		return
	}
	due := now.Add(time.Duration(reminderMinutes) * time.Minute)
	req.ReminderAt = due.Format(time.RFC3339)
	req.FollowUpAt = due.Add(time.Duration(followUpMinutes) * time.Minute).Format(time.RFC3339)
	req.RecheckAt = req.ReminderAt
	req.DueAt = req.ReminderAt
	_ = b.scheduleJobLocked(normalizeSchedulerJob(schedulerJob{
		Slug:       normalizeSchedulerSlug("request_follow_up", reqChannel, req.ID),
		Kind:       "request_follow_up",
		Label:      "Follow up on " + req.Title,
		TargetType: "request",
		TargetID:   req.ID,
		Channel:    reqChannel,
		DueAt:      req.ReminderAt,
		NextRun:    req.ReminderAt,
		Status:     "scheduled",
		Payload:    req.Question,
	}))
}

func (b *Broker) handleHealth(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	mode := b.sessionMode
	agent := b.oneOnOneAgent
	focus := b.focusMode
	provider := b.runtimeProvider
	b.mu.Unlock()
	if strings.TrimSpace(provider) == "" {
		provider = config.ResolveLLMProvider("")
	}
	memoryStatus := ResolveMemoryBackendStatus()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":                "ok",
		"session_mode":          mode,
		"one_on_one_agent":      agent,
		"focus_mode":            focus,
		"provider":              provider,
		"provider_model":        resolveProviderModel(provider),
		"memory_backend":        memoryStatus.SelectedKind,
		"memory_backend_active": memoryStatus.ActiveKind,
		"memory_backend_ready":  memoryStatus.ActiveKind != config.MemoryBackendNone,
		"nex_connected":         memoryStatus.ActiveKind == config.MemoryBackendNex && nex.Connected(),
		"build":                 buildinfo.Current(),
	})
}

// resolveProviderModel returns the effective model id for the active LLM
// provider so the web UI's status bar can show, e.g.
// "opencode · ollama/qwen2.5-coder:1.5b". Returns "" when the provider has
// no resolvable model (claude-code uses the CLI's bundled default unless the
// user overrides via --model; we don't parse that out here).
func resolveProviderModel(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "codex":
		// Empty cwd keeps the home-dir config lookup but skips the
		// workspace-relative walk — Broker doesn't know which workspace the
		// caller is in, and the status bar is a coarse indicator anyway.
		return config.ResolveCodexModel("")
	case "opencode":
		return config.ResolveOpencodeModel()
	default:
		return ""
	}
}

func (b *Broker) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(buildinfo.Current())
}

func (b *Broker) handleSessionMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		mode, agent := b.SessionModeState()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_mode":     mode,
			"one_on_one_agent": agent,
		})
	case http.MethodPost:
		var body struct {
			Mode  string `json:"mode"`
			Agent string `json:"agent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := b.SetSessionMode(body.Mode, body.Agent); err != nil {
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		mode, agent := b.SessionModeState()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_mode":     mode,
			"one_on_one_agent": agent,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleFocusMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"focus_mode": b.FocusModeEnabled(),
		})
	case http.MethodPost:
		var body struct {
			FocusMode bool `json:"focus_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if err := b.SetFocusMode(body.FocusMode); err != nil {
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"focus_mode": b.FocusModeEnabled(),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.Reset()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (b *Broker) handleResetDM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Agent   string `json:"agent"`
		Channel string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	agent := strings.TrimSpace(body.Agent)
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}

	b.mu.Lock()
	// Keep only messages that are NOT direct exchanges between human and agent
	filtered := make([]channelMessage, 0, len(b.messages))
	removed := 0
	for _, msg := range b.messages {
		if normalizeChannelSlug(msg.Channel) != channel {
			filtered = append(filtered, msg)
			continue
		}
		// Remove if: human->agent or agent->human (direct messages only)
		isHuman := msg.From == "you" || msg.From == "human"
		isAgent := msg.From == agent
		if isHuman || isAgent {
			// Check if it's a direct message (not a delegation to others)
			if isAgent && len(msg.Tagged) > 0 {
				taggedHuman := false
				for _, t := range msg.Tagged {
					if t == "you" || t == "human" {
						taggedHuman = true
						break
					}
				}
				if !taggedHuman {
					// Agent message to other agents — keep it
					filtered = append(filtered, msg)
					continue
				}
			}
			removed++
			continue
		}
		filtered = append(filtered, msg)
	}
	b.messages = filtered
	_ = b.saveLocked()
	b.mu.Unlock()

	// Respawn the agent's Claude Code session to clear its context
	go respawnAgentPane(agent)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "removed": removed})
}

// respawnAgentPane restarts an agent's Claude Code session in its tmux pane.
func respawnAgentPane(slug string) {
	manifest := company.DefaultManifest()
	loaded, err := company.LoadManifest()
	if err == nil && len(loaded.Members) > 0 {
		manifest = loaded
	}

	for i, agent := range manifest.Members {
		if agent.Slug == slug {
			paneIdx := i + 1 // pane 0 is channel view
			target := fmt.Sprintf("wuphf-team:team.%d", paneIdx)
			// Send Ctrl+C to interrupt, then exit to terminate
			_ = exec.Command("tmux", "-L", "wuphf", "send-keys", "-t", target, "C-c", "").Run()
			time.Sleep(500 * time.Millisecond)
			_ = exec.Command("tmux", "-L", "wuphf", "send-keys", "-t", target, "C-c", "").Run()
			time.Sleep(500 * time.Millisecond)
			// Respawn the pane with a fresh claude session
			_ = exec.Command("tmux", "-L", "wuphf", "respawn-pane", "-k", "-t", target).Run()
			return
		}
	}
}

func (b *Broker) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	usage := b.usage
	if usage.Agents == nil {
		usage.Agents = make(map[string]usageTotals)
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(usage)
}

// RecordPolicy adds a new active policy. Deduplicates by exact rule text.
func (b *Broker) RecordPolicy(source, rule string) (officePolicy, error) {
	rule = strings.TrimSpace(rule)
	if rule == "" {
		return officePolicy{}, fmt.Errorf("rule cannot be empty")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// Dedupe: don't add the same rule twice.
	for i, p := range b.policies {
		if strings.EqualFold(p.Rule, rule) {
			b.policies[i].Active = true
			_ = b.saveLocked()
			return b.policies[i], nil
		}
	}
	p := newOfficePolicy(source, rule)
	b.policies = append(b.policies, p)
	_ = b.saveLocked()
	return p, nil
}

// ListPolicies returns all active policies.
func (b *Broker) ListPolicies() []officePolicy {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officePolicy, 0, len(b.policies))
	for _, p := range b.policies {
		if p.Active {
			out = append(out, p)
		}
	}
	return out
}

func (b *Broker) handlePolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		out := make([]officePolicy, 0, len(b.policies))
		for _, p := range b.policies {
			if p.Active {
				out = append(out, p)
			}
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"policies": out})

	case http.MethodPost:
		var body struct {
			Source string `json:"source"`
			Rule   string `json:"rule"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Rule) == "" {
			http.Error(w, "rule is required", http.StatusBadRequest)
			return
		}
		p, err := b.RecordPolicy(body.Source, body.Rule)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(p)

	case http.MethodDelete:
		id := strings.TrimPrefix(r.URL.Path, "/policies/")
		id = strings.TrimSpace(id)
		if id == "" || id == "/policies" {
			// Parse from body
			var body struct {
				ID string `json:"id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			id = strings.TrimSpace(body.ID)
		}
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		for i, p := range b.policies {
			if p.ID == id {
				b.policies[i].Active = false
				_ = b.saveLocked()
				break
			}
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleSignals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	signals := make([]officeSignalRecord, len(b.signals))
	copy(signals, b.signals)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"signals": signals})
}

func (b *Broker) handleDecisions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	decisions := make([]officeDecisionRecord, len(b.decisions))
	copy(decisions, b.decisions)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"decisions": decisions})
}

func (b *Broker) handleWatchdogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	alerts := make([]watchdogAlert, len(b.watchdogs))
	copy(alerts, b.watchdogs)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"watchdogs": alerts})
}

func (b *Broker) handleActions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		actions := make([]officeActionLog, len(b.actions))
		copy(actions, b.actions)
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"actions": actions})
	case http.MethodPost:
		var body struct {
			Kind       string   `json:"kind"`
			Source     string   `json:"source"`
			Channel    string   `json:"channel"`
			Actor      string   `json:"actor"`
			Summary    string   `json:"summary"`
			RelatedID  string   `json:"related_id"`
			SignalIDs  []string `json:"signal_ids"`
			DecisionID string   `json:"decision_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Kind) == "" || strings.TrimSpace(body.Summary) == "" {
			http.Error(w, "kind and summary required", http.StatusBadRequest)
			return
		}
		if err := b.RecordAction(
			body.Kind,
			body.Source,
			body.Channel,
			body.Actor,
			body.Summary,
			body.RelatedID,
			body.SignalIDs,
			body.DecisionID,
		); err != nil {
			http.Error(w, "failed to persist action", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type studioGeneratedPackage map[string]map[string]any

type studioGeneratedArtifact struct {
	Kind  string         `json:"kind"`
	Title string         `json:"title,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

type studioStubExecution struct {
	ID           string         `json:"id"`
	Provider     string         `json:"provider"`
	WorkflowKey  string         `json:"workflow_key"`
	Status       string         `json:"status"`
	Mode         string         `json:"mode"`
	Integrations []string       `json:"integrations,omitempty"`
	Summary      string         `json:"summary"`
	Input        map[string]any `json:"input,omitempty"`
	Output       map[string]any `json:"output,omitempty"`
}

func decodeStudioGeneratedPackage(raw string, requiredArtifacts []string) (studioGeneratedPackage, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("empty codex response")
	}
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
			lines = lines[1:]
			if last := len(lines) - 1; last >= 0 && strings.HasPrefix(strings.TrimSpace(lines[last]), "```") {
				lines = lines[:last]
			}
			trimmed = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end >= start {
		trimmed = trimmed[start : end+1]
	}
	var pkg studioGeneratedPackage
	if err := json.Unmarshal([]byte(trimmed), &pkg); err != nil {
		return nil, err
	}
	for _, artifactID := range requiredArtifacts {
		if len(pkg[artifactID]) == 0 {
			return nil, fmt.Errorf("missing required artifact %q", artifactID)
		}
	}
	return pkg, nil
}

func buildStudioFollowUpStubExecutions(runTitle string, offers []any, pkg studioGeneratedPackage) []studioStubExecution {
	offerNames := extractStudioOfferNames(offers)
	artifactIDs := studioPackageArtifactIDs(pkg)
	primaryArtifactID, primaryArtifact := studioPrimaryPackageArtifact(pkg, artifactIDs)
	primarySummary := firstStudioString(
		primaryArtifact["summary"],
		primaryArtifact["objective"],
		primaryArtifact["title"],
		primaryArtifact["name"],
		runTitle,
	)
	return []studioStubExecution{
		{
			ID:           fmt.Sprintf("followup-review-%d", time.Now().UTC().UnixNano()),
			Provider:     "one",
			WorkflowKey:  "artifact-review-sync",
			Status:       "success",
			Mode:         "dry_run",
			Integrations: []string{"artifact-review"},
			Summary:      fmt.Sprintf("Prepared a review sync payload for %s.", runTitle),
			Input: map[string]any{
				"run_title":        runTitle,
				"artifact_ids":     artifactIDs,
				"primary_artifact": primaryArtifactID,
			},
			Output: map[string]any{
				"destination":  "review queue",
				"draft_status": "ready_for_review",
			},
		},
		{
			ID:           fmt.Sprintf("followup-offers-%d", time.Now().UTC().UnixNano()+1),
			Provider:     "one",
			WorkflowKey:  "offer-alignment-check",
			Status:       "success",
			Mode:         "dry_run",
			Integrations: []string{"offer-alignment"},
			Summary:      fmt.Sprintf("Prepared offer alignment notes for %s.", runTitle),
			Input: map[string]any{
				"run_title":    runTitle,
				"offer_names":  offerNames,
				"artifact_ids": artifactIDs,
			},
			Output: map[string]any{
				"destination":  "offer queue",
				"draft_status": "ready_for_review",
			},
		},
		{
			ID:           fmt.Sprintf("followup-approval-%d", time.Now().UTC().UnixNano()+2),
			Provider:     "one",
			WorkflowKey:  "approval-gate-review",
			Status:       "success",
			Mode:         "dry_run",
			Integrations: []string{"approval-gates"},
			Summary:      fmt.Sprintf("Prepared approval gates for %s.", runTitle),
			Input: map[string]any{
				"run_title":        runTitle,
				"primary_artifact": primaryArtifactID,
				"primary_summary":  primarySummary,
			},
			Output: map[string]any{
				"destination":  "approval queue",
				"draft_status": "ready_for_review",
			},
		},
	}
}

func studioDefaultArtifactDefinitions() []operations.ArtifactType {
	return []operations.ArtifactType{
		{ID: "objective_brief", Name: "Objective brief", Description: "Problem statement, constraints, and desired outcome for one run."},
		{ID: "execution_packet", Name: "Execution packet", Description: "Checklist, dependencies, outputs, and handoff details for one run."},
		{ID: "approval_checklist", Name: "Approval checklist", Description: "Review gates and required human approvals before live action."},
	}
}

func studioNormalizeArtifactDefinitions(defs []operations.ArtifactType) []operations.ArtifactType {
	normalized := make([]operations.ArtifactType, 0, len(defs))
	seen := make(map[string]struct{}, len(defs))
	for _, def := range defs {
		def.ID = strings.TrimSpace(def.ID)
		if def.ID == "" {
			continue
		}
		if _, ok := seen[def.ID]; ok {
			continue
		}
		seen[def.ID] = struct{}{}
		normalized = append(normalized, def)
	}
	if len(normalized) == 0 {
		return studioDefaultArtifactDefinitions()
	}
	return normalized
}

func studioArtifactIDs(defs []operations.ArtifactType) []string {
	ids := make([]string, 0, len(defs))
	for _, def := range defs {
		ids = append(ids, def.ID)
	}
	return ids
}

func buildStudioGeneratedArtifacts(runTitle string, pkg studioGeneratedPackage, defs []operations.ArtifactType) []studioGeneratedArtifact {
	artifacts := make([]studioGeneratedArtifact, 0, len(defs))
	for _, def := range studioNormalizeArtifactDefinitions(defs) {
		artifacts = append(artifacts, studioGeneratedArtifact{
			Kind:  def.ID,
			Title: runTitle,
			Data:  pkg[def.ID],
		})
	}
	return artifacts
}

func extractStudioOfferNames(offers []any) []string {
	names := make([]string, 0, len(offers))
	for _, item := range offers {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(fmt.Sprintf("%v", record["name"]))
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func extractStudioStringSlice(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprintf("%v", item))
		if text == "" || text == "<nil>" {
			continue
		}
		values = append(values, text)
	}
	return values
}

func firstStudioString(values ...any) string {
	for _, value := range values {
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func (b *Broker) handleStudioGeneratePackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Channel   string                    `json:"channel"`
		Actor     string                    `json:"actor"`
		Workspace map[string]any            `json:"workspace"`
		Run       map[string]any            `json:"run"`
		Offers    []any                     `json:"offers"`
		Artifacts []operations.ArtifactType `json:"artifacts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	actor := strings.TrimSpace(body.Actor)
	if actor == "" {
		actor = "human"
	}
	runTitle := strings.TrimSpace(fmt.Sprintf("%v", body.Run["title"]))
	if runTitle == "" {
		http.Error(w, "run.title required", http.StatusBadRequest)
		return
	}

	artifactDefs := studioNormalizeArtifactDefinitions(body.Artifacts)
	systemPrompt := strings.TrimSpace(`You generate structured operation artifacts for a reusable workflow.
Return valid JSON only. No markdown fences. No prose outside JSON.
The top-level object must contain exactly:
` + strings.Join(func() []string {
		items := make([]string, 0, len(artifactDefs))
		for _, def := range artifactDefs {
			items = append(items, "- "+def.ID)
		}
		return items
	}(), "\n"))

	promptPayload, _ := json.Marshal(map[string]any{
		"workspace": body.Workspace,
		"run":       body.Run,
		"offers":    body.Offers,
		"artifacts": artifactDefs,
	})
	prompt := strings.TrimSpace(`Turn this run into a production-ready artifact bundle for the active operation.

Rules:
- Keep claims concrete and production-safe.
- Use short, scannable fields.
- For each requested artifact, use the provided id, name, and description to shape the object.
- Prefer compact objects with fields like summary, goals, checklist, dependencies, outputs, risks, approvals, notes, links, or tags when they fit the artifact purpose.
- Only return the requested artifact ids as top-level keys.

Input JSON:
` + string(promptPayload))

	raw, err := studioPackageGenerator(systemPrompt, prompt, "")
	if err != nil {
		http.Error(w, "package generation failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	pkg, err := decodeStudioGeneratedPackage(raw, studioArtifactIDs(artifactDefs))
	if err != nil {
		http.Error(w, "invalid codex package output: "+err.Error(), http.StatusBadGateway)
		return
	}
	stubExecutions := buildStudioFollowUpStubExecutions(runTitle, body.Offers, pkg)
	artifacts := buildStudioGeneratedArtifacts(runTitle, pkg, artifactDefs)
	summary := truncateSummary("Generated operation artifacts for "+runTitle, 140)
	if err := b.RecordAction("studio_package_generated", "studio", channel, actor, summary, runTitle, nil, ""); err != nil {
		http.Error(w, "failed to persist action", http.StatusInternalServerError)
		return
	}
	for _, execution := range stubExecutions {
		if err := b.RecordAction("studio_followup_stub_executed", "studio", channel, actor, truncateSummary(execution.Summary, 140), runTitle, nil, ""); err != nil {
			http.Error(w, "failed to persist follow-up stub action", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":              true,
		"package":         pkg,
		"artifacts":       artifacts,
		"stub_executions": stubExecutions,
	})
}

func studioPackageArtifactIDs(pkg studioGeneratedPackage) []string {
	ids := make([]string, 0, len(pkg))
	for id := range pkg {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func studioPrimaryPackageArtifact(pkg studioGeneratedPackage, artifactIDs []string) (string, map[string]any) {
	for _, id := range artifactIDs {
		if item, ok := pkg[id]; ok && len(item) > 0 {
			return id, item
		}
	}
	for id, item := range pkg {
		if len(item) > 0 {
			return strings.TrimSpace(id), item
		}
	}
	return "", map[string]any{}
}

func normalizeStudioWorkflowDefinition(raw json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var encoded string
		if err := json.Unmarshal(trimmed, &encoded); err != nil {
			return nil, err
		}
		trimmed = []byte(strings.TrimSpace(encoded))
	}
	if len(trimmed) == 0 {
		return nil, nil
	}
	return trimmed, nil
}

func studioWorkflowHints(definition []byte) (dryRun bool, mock bool, integrations []string) {
	var parsed struct {
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(definition, &parsed); err != nil {
		return false, false, nil
	}
	seen := make(map[string]struct{})
	for _, step := range parsed.Steps {
		if v, ok := step["dry_run"].(bool); ok && v {
			dryRun = true
		}
		if v, ok := step["mock"].(bool); ok && v {
			mock = true
		}
		platform := strings.TrimSpace(fmt.Sprintf("%v", step["platform"]))
		if platform == "" || platform == "<nil>" {
			continue
		}
		if _, exists := seen[platform]; exists {
			continue
		}
		seen[platform] = struct{}{}
		integrations = append(integrations, platform)
	}
	return dryRun, mock, integrations
}

func workflowCreateConflict(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "already exists") ||
		strings.Contains(text, "duplicate") ||
		strings.Contains(text, "conflict")
}

func uniqueStrings(values ...[]string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, group := range values {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func workflowRunModeLabel(dryRun, mock bool) string {
	switch {
	case dryRun && mock:
		return "dry-run + mock"
	case dryRun:
		return "dry-run"
	case mock:
		return "mock"
	default:
		return "live"
	}
}

func mustMarshalStudioJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{"error":"marshal_failed"}`)
	}
	return json.RawMessage(data)
}

func executeStudioWorkflowStub(workflowKey string, definition []byte, inputs map[string]any, dryRun, mock bool) (action.WorkflowExecuteResult, error) {
	var parsed struct {
		Steps []map[string]any `json:"steps"`
	}
	if err := json.Unmarshal(definition, &parsed); err != nil {
		return action.WorkflowExecuteResult{}, err
	}
	now := time.Now().UTC()
	runID := fmt.Sprintf("studiowf_%d", now.UnixNano())
	stepLogs := make(map[string]json.RawMessage, len(parsed.Steps))
	events := make([]json.RawMessage, 0, len(parsed.Steps)+2)
	events = append(events, mustMarshalStudioJSON(map[string]any{
		"event":        "workflow_started",
		"provider":     "studio_stub",
		"workflow_key": workflowKey,
		"run_id":       runID,
	}))
	status := "success"
	for i, step := range parsed.Steps {
		stepID := strings.TrimSpace(fmt.Sprintf("%v", step["id"]))
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", i+1)
		}
		stepType := strings.TrimSpace(fmt.Sprintf("%v", step["kind"]))
		if stepType == "" || stepType == "<nil>" {
			stepType = strings.TrimSpace(fmt.Sprintf("%v", step["type"]))
		}
		if stepType == "" || stepType == "<nil>" {
			stepType = "action"
		}
		stepStatus := "completed"
		if dryRun {
			stepStatus = "planned"
		}
		if mock {
			stepStatus = "mocked"
		}
		payload := map[string]any{
			"id":       stepID,
			"type":     stepType,
			"status":   stepStatus,
			"platform": strings.TrimSpace(fmt.Sprintf("%v", step["platform"])),
			"action":   strings.TrimSpace(fmt.Sprintf("%v", step["action"])),
			"inputs":   inputs,
		}
		stepLogs[stepID] = mustMarshalStudioJSON(payload)
		events = append(events, mustMarshalStudioJSON(map[string]any{
			"event":   "workflow_step_completed",
			"step_id": stepID,
			"type":    stepType,
			"status":  stepStatus,
		}))
	}
	events = append(events, mustMarshalStudioJSON(map[string]any{
		"event":  "workflow_finished",
		"run_id": runID,
		"status": status,
	}))
	return action.WorkflowExecuteResult{
		RunID:  runID,
		Status: status,
		Steps:  stepLogs,
		Events: events,
	}, nil
}

func (b *Broker) recordStudioWorkflowExecution(channel, actor, skillName, workflowKey, providerName, title, status string, when time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var skill *teamSkill
	if strings.TrimSpace(skillName) != "" {
		skill = b.findSkillByNameLocked(skillName)
	}
	if skill == nil && strings.TrimSpace(workflowKey) != "" {
		skill = b.findSkillByWorkflowKeyLocked(workflowKey)
	}
	if skill != nil {
		skill.UsageCount++
		skill.LastExecutionStatus = strings.TrimSpace(status)
		skill.LastExecutionAt = when.UTC().Format(time.RFC3339)
		skill.UpdatedAt = when.UTC().Format(time.RFC3339)
		if strings.TrimSpace(title) == "" {
			title = skill.Title
		}
	}
	if strings.TrimSpace(title) == "" {
		title = workflowKey
	}
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      actor,
		Channel:   channel,
		Kind:      "skill_invocation",
		Title:     title,
		Content:   fmt.Sprintf("Workflow %q executed via %s (%s)", workflowKey, providerName, status),
		Timestamp: when.UTC().Format(time.RFC3339),
	})
	return b.saveLocked()
}

func (b *Broker) handleStudioRunWorkflow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Channel            string          `json:"channel"`
		Actor              string          `json:"actor"`
		SkillName          string          `json:"skill_name"`
		WorkflowKey        string          `json:"workflow_key"`
		WorkflowProvider   string          `json:"workflow_provider"`
		WorkflowDefinition json.RawMessage `json:"workflow_definition"`
		Inputs             map[string]any  `json:"inputs"`
		DryRun             *bool           `json:"dry_run"`
		Mock               *bool           `json:"mock"`
		AllowBash          bool            `json:"allow_bash"`
		Integrations       []string        `json:"integrations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	actor := strings.TrimSpace(body.Actor)
	if actor == "" {
		actor = "human"
	}

	var (
		skillName          = strings.TrimSpace(body.SkillName)
		workflowKey        = strings.TrimSpace(body.WorkflowKey)
		workflowProvider   = strings.TrimSpace(body.WorkflowProvider)
		workflowDefinition []byte
		title              string
	)
	definition, err := normalizeStudioWorkflowDefinition(body.WorkflowDefinition)
	if err != nil {
		http.Error(w, "invalid workflow_definition: "+err.Error(), http.StatusBadRequest)
		return
	}
	workflowDefinition = definition

	b.mu.Lock()
	if skillName != "" || workflowKey != "" {
		var skill *teamSkill
		if skillName != "" {
			skill = b.findSkillByNameLocked(skillName)
		}
		if skill == nil && workflowKey != "" {
			skill = b.findSkillByWorkflowKeyLocked(workflowKey)
		}
		if skill != nil {
			if skillName == "" {
				skillName = strings.TrimSpace(skill.Name)
			}
			if workflowKey == "" {
				workflowKey = strings.TrimSpace(skill.WorkflowKey)
			}
			if workflowProvider == "" {
				workflowProvider = strings.TrimSpace(skill.WorkflowProvider)
			}
			if len(workflowDefinition) == 0 {
				workflowDefinition = []byte(strings.TrimSpace(skill.WorkflowDefinition))
			}
			title = strings.TrimSpace(skill.Title)
		}
	}
	b.mu.Unlock()

	if workflowKey == "" {
		http.Error(w, "workflow_key required", http.StatusBadRequest)
		return
	}
	if workflowProvider == "" {
		workflowProvider = "one"
	}
	if len(workflowDefinition) == 0 {
		http.Error(w, "workflow_definition required", http.StatusBadRequest)
		return
	}

	inferredDryRun, inferredMock, inferredIntegrations := studioWorkflowHints(workflowDefinition)
	dryRun := inferredDryRun
	if body.DryRun != nil {
		dryRun = *body.DryRun
	}
	mock := inferredMock
	if body.Mock != nil {
		mock = *body.Mock
	}
	integrations := uniqueStrings(body.Integrations, inferredIntegrations)

	providerLabel := workflowProvider
	registry := action.NewRegistryFromEnv()
	provider, err := registry.ProviderNamed(workflowProvider, action.CapabilityWorkflowExecute)
	var execution action.WorkflowExecuteResult
	if err != nil {
		if dryRun || mock {
			execution, err = executeStudioWorkflowStub(workflowKey, workflowDefinition, body.Inputs, dryRun, mock)
			if err != nil {
				http.Error(w, "workflow stub execution failed: "+err.Error(), http.StatusBadGateway)
				return
			}
		} else {
			http.Error(w, "workflow provider unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
	} else {
		providerLabel = provider.Name()
		if provider.Supports(action.CapabilityWorkflowCreate) {
			if _, err := provider.CreateWorkflow(r.Context(), action.WorkflowCreateRequest{
				Key:        workflowKey,
				Definition: workflowDefinition,
			}); err != nil && !workflowCreateConflict(err) {
				if dryRun || mock {
					execution, err = executeStudioWorkflowStub(workflowKey, workflowDefinition, body.Inputs, dryRun, mock)
					if err != nil {
						http.Error(w, "workflow stub execution failed: "+err.Error(), http.StatusBadGateway)
						return
					}
				} else {
					http.Error(w, "workflow registration failed: "+err.Error(), http.StatusBadGateway)
					return
				}
			}
		}
		if execution.RunID == "" {
			execution, err = provider.ExecuteWorkflow(r.Context(), action.WorkflowExecuteRequest{
				KeyOrPath: workflowKey,
				Inputs:    body.Inputs,
				DryRun:    dryRun,
				Mock:      mock,
				AllowBash: body.AllowBash,
			})
			if err != nil {
				if dryRun || mock {
					execution, err = executeStudioWorkflowStub(workflowKey, workflowDefinition, body.Inputs, dryRun, mock)
					if err != nil {
						http.Error(w, "workflow stub execution failed: "+err.Error(), http.StatusBadGateway)
						return
					}
				} else {
					now := time.Now().UTC()
					mode := workflowRunModeLabel(dryRun, mock)
					retryAt, rateLimited := externalWorkflowRetryAfter(err, now)
					failKind := "external_workflow_failed"
					failStatus := "failed"
					failSummary := truncateSummary(fmt.Sprintf("Studio workflow %s failed via %s (%s)", workflowKey, titleCaser.String(providerLabel), mode), 140)
					if rateLimited {
						failKind = "external_workflow_rate_limited"
						failStatus = "rate_limited"
						failSummary = truncateSummary(fmt.Sprintf("Studio workflow %s rate-limited via %s (%s)", workflowKey, titleCaser.String(providerLabel), mode), 140)
						retryDelay := time.Until(retryAt)
						if retryDelay < time.Second {
							retryDelay = time.Second
						}
						w.Header().Set("Retry-After", strconv.Itoa(int((retryDelay+time.Second-1)/time.Second)))
					}
					_ = b.RecordAction(failKind, providerLabel, channel, actor, failSummary, workflowKey, nil, "")
					_ = b.UpdateSkillExecutionByWorkflowKey(workflowKey, failStatus, now)
					if rateLimited {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusTooManyRequests)
						_ = json.NewEncoder(w).Encode(map[string]any{
							"ok":           false,
							"workflow_key": workflowKey,
							"provider":     providerLabel,
							"status":       "rate_limited",
							"error":        err.Error(),
							"retry_after":  retryAt.UTC().Format(time.RFC3339Nano),
						})
						return
					}
					http.Error(w, "workflow execution failed: "+err.Error(), http.StatusBadGateway)
					return
				}
			}
		}
	}
	now := time.Now().UTC()
	mode := workflowRunModeLabel(dryRun, mock)
	status := strings.TrimSpace(execution.Status)
	if status == "" {
		status = "completed"
	}
	summary := truncateSummary(fmt.Sprintf("Studio workflow %s ran via %s (%s)", workflowKey, titleCaser.String(providerLabel), mode), 140)
	if err := b.RecordAction("external_workflow_executed", providerLabel, channel, actor, summary, workflowKey, nil, ""); err != nil {
		http.Error(w, "failed to record workflow action", http.StatusInternalServerError)
		return
	}
	if err := b.recordStudioWorkflowExecution(channel, actor, skillName, workflowKey, providerLabel, title, status, now); err != nil {
		http.Error(w, "failed to persist workflow execution", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":           true,
		"skill_name":   skillName,
		"workflow_key": workflowKey,
		"provider":     providerLabel,
		"mode":         mode,
		"status":       status,
		"integrations": integrations,
		"execution": map[string]any{
			"run_id":   execution.RunID,
			"log_file": execution.LogFile,
			"status":   status,
			"steps":    execution.Steps,
			"events":   execution.Events,
		},
	})
}

func (b *Broker) handleScheduler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		jobs := make([]schedulerJob, 0, len(b.scheduler))
		dueOnly := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("due_only")), "true")
		now := time.Now().UTC()
		for _, job := range b.scheduler {
			if dueOnly && !schedulerJobDue(job, now) {
				continue
			}
			jobs = append(jobs, job)
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jobs": jobs})
	case http.MethodPost:
		var body schedulerJob
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Slug) == "" || strings.TrimSpace(body.Label) == "" {
			http.Error(w, "slug and label required", http.StatusBadRequest)
			return
		}
		if err := b.SetSchedulerJob(body); err != nil {
			http.Error(w, "failed to persist scheduler job", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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

func (b *Broker) handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(b.QueueSnapshot())
}

func (b *Broker) handleCompany(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, _ := config.Load()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":        cfg.CompanyName,
			"description": cfg.CompanyDescription,
			"goals":       cfg.CompanyGoals,
			"size":        cfg.CompanySize,
			"priority":    cfg.CompanyPriority,
		})
	case http.MethodPost:
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Goals       string `json:"goals"`
			Size        string `json:"size"`
			Priority    string `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		cfg, _ := config.Load()
		if body.Name != "" {
			cfg.CompanyName = strings.TrimSpace(body.Name)
		}
		if body.Description != "" {
			cfg.CompanyDescription = strings.TrimSpace(body.Description)
		}
		if body.Goals != "" {
			cfg.CompanyGoals = strings.TrimSpace(body.Goals)
		}
		if body.Size != "" {
			cfg.CompanySize = strings.TrimSpace(body.Size)
		}
		if body.Priority != "" {
			cfg.CompanyPriority = strings.TrimSpace(body.Priority)
		}
		if err := config.Save(cfg); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleConfig exposes GET/POST over ~/.wuphf/config.json for the web UI
// settings page and onboarding wizard. All POST fields are optional; clients
// can update one without touching the others. Secret fields (API keys, tokens)
// are returned as boolean flags on GET and accepted as plain values on POST.
func (b *Broker) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := config.Load()
		if err != nil {
			http.Error(w, "failed to read config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			// Runtime
			"llm_provider":          config.ResolveLLMProvider(""),
			"llm_provider_priority": cfg.LLMProviderPriority,
			"memory_backend":        config.ResolveMemoryBackend(""),
			"action_provider":       config.ResolveActionProvider(),
			"team_lead_slug":        cfg.TeamLeadSlug,
			"max_concurrent_agents": cfg.MaxConcurrent,
			"default_format":        config.ResolveFormat(""),
			"default_timeout":       config.ResolveTimeout(""),
			"blueprint":             cfg.ActiveBlueprint(),
			// Workspace
			"email":          cfg.Email,
			"workspace_id":   cfg.WorkspaceID,
			"workspace_slug": cfg.WorkspaceSlug,
			"dev_url":        cfg.DevURL,
			// Company
			"company_name":        cfg.CompanyName,
			"company_description": cfg.CompanyDescription,
			"company_goals":       cfg.CompanyGoals,
			"company_size":        cfg.CompanySize,
			"company_priority":    cfg.CompanyPriority,
			// Polling intervals
			"insights_poll_minutes":  config.ResolveInsightsPollInterval(),
			"task_follow_up_minutes": config.ResolveTaskFollowUpInterval(),
			"task_reminder_minutes":  config.ResolveTaskReminderInterval(),
			"task_recheck_minutes":   config.ResolveTaskRecheckInterval(),
			// Integrations — secret fields as booleans
			"api_key_set":          config.ResolveAPIKey("") != "",
			"openai_key_set":       config.ResolveOpenAIAPIKey() != "",
			"anthropic_key_set":    config.ResolveAnthropicAPIKey() != "",
			"gemini_key_set":       config.ResolveGeminiAPIKey() != "",
			"minimax_key_set":      config.ResolveMinimaxAPIKey() != "",
			"one_key_set":          config.ResolveOneSecret() != "",
			"composio_key_set":     config.ResolveComposioAPIKey() != "",
			"telegram_token_set":   config.ResolveTelegramBotToken() != "",
			"openclaw_token_set":   config.ResolveOpenclawToken() != "",
			"openclaw_gateway_url": config.ResolveOpenclawGatewayURL(),
			// Config file path (informational)
			"config_path": config.ConfigPath(),
		})
	case http.MethodPost:
		// Serialize POST reads/writes; config.Save is not atomic against
		// concurrent writers and two parallel calls can corrupt the file.
		b.configMu.Lock()
		defer b.configMu.Unlock()
		var body struct {
			LLMProvider         *string   `json:"llm_provider,omitempty"`
			LLMProviderPriority *[]string `json:"llm_provider_priority,omitempty"`
			MemoryBackend       *string   `json:"memory_backend,omitempty"`
			ActionProvider      *string   `json:"action_provider,omitempty"`
			TeamLeadSlug        *string   `json:"team_lead_slug,omitempty"`
			MaxConcurrent       *int      `json:"max_concurrent_agents,omitempty"`
			DefaultFormat       *string   `json:"default_format,omitempty"`
			DefaultTimeout      *int      `json:"default_timeout,omitempty"`
			Blueprint           *string   `json:"blueprint,omitempty"`
			Email               *string   `json:"email,omitempty"`
			DevURL              *string   `json:"dev_url,omitempty"`
			CompanyName         *string   `json:"company_name,omitempty"`
			CompanyDesc         *string   `json:"company_description,omitempty"`
			CompanyGoals        *string   `json:"company_goals,omitempty"`
			CompanySize         *string   `json:"company_size,omitempty"`
			CompanyPriority     *string   `json:"company_priority,omitempty"`
			InsightsPoll        *int      `json:"insights_poll_minutes,omitempty"`
			TaskFollowUp        *int      `json:"task_follow_up_minutes,omitempty"`
			TaskReminder        *int      `json:"task_reminder_minutes,omitempty"`
			TaskRecheck         *int      `json:"task_recheck_minutes,omitempty"`
			// Secret fields
			APIKey          *string `json:"api_key,omitempty"`
			OpenAIAPIKey    *string `json:"openai_api_key,omitempty"`
			AnthropicAPIKey *string `json:"anthropic_api_key,omitempty"`
			GeminiAPIKey    *string `json:"gemini_api_key,omitempty"`
			MinimaxAPIKey   *string `json:"minimax_api_key,omitempty"`
			OneAPIKey       *string `json:"one_api_key,omitempty"`
			ComposioAPIKey  *string `json:"composio_api_key,omitempty"`
			TelegramToken   *string `json:"telegram_bot_token,omitempty"`
			OpenclawToken   *string `json:"openclaw_token,omitempty"`
			OpenclawGateway *string `json:"openclaw_gateway_url,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Validate enum fields before touching config.
		var provider string
		if body.LLMProvider != nil {
			provider = strings.TrimSpace(strings.ToLower(*body.LLMProvider))
			switch provider {
			case "claude-code", "codex", "opencode":
				// ok
			default:
				http.Error(w, "unsupported llm_provider", http.StatusBadRequest)
				return
			}
		}
		var providerPriority []string
		if body.LLMProviderPriority != nil {
			// Normalize + validate each entry. Unknown entries are rejected so
			// the stored list only contains provider ids the resolver knows how
			// to dispatch. Empty list is accepted (means "clear").
			seen := make(map[string]bool, len(*body.LLMProviderPriority))
			for _, raw := range *body.LLMProviderPriority {
				id := strings.TrimSpace(strings.ToLower(raw))
				if id == "" {
					continue
				}
				switch id {
				case "claude-code", "codex", "opencode":
					// ok
				default:
					http.Error(w, "unsupported entry in llm_provider_priority: "+id, http.StatusBadRequest)
					return
				}
				if seen[id] {
					continue
				}
				seen[id] = true
				providerPriority = append(providerPriority, id)
			}
		}
		var memory string
		if body.MemoryBackend != nil {
			memory = config.NormalizeMemoryBackend(*body.MemoryBackend)
			if memory == "" {
				http.Error(w, "unsupported memory_backend", http.StatusBadRequest)
				return
			}
		}

		cfg, _ := config.Load()
		changed := false

		// Enum/string fields
		if provider != "" {
			cfg.LLMProvider = provider
			changed = true
		}
		if body.LLMProviderPriority != nil {
			// Explicit pointer set means the client wanted to write this field,
			// even if the final list is empty (which clears the stored order).
			cfg.LLMProviderPriority = providerPriority
			changed = true
		}
		if memory != "" {
			cfg.MemoryBackend = memory
			changed = true
		}
		if body.ActionProvider != nil {
			ap := strings.TrimSpace(strings.ToLower(*body.ActionProvider))
			switch ap {
			case "auto", "one", "composio", "":
				cfg.ActionProvider = ap
				changed = true
			default:
				http.Error(w, "unsupported action_provider", http.StatusBadRequest)
				return
			}
		}
		if body.TeamLeadSlug != nil {
			cfg.TeamLeadSlug = strings.TrimSpace(*body.TeamLeadSlug)
			changed = true
		}
		if body.MaxConcurrent != nil {
			cfg.MaxConcurrent = *body.MaxConcurrent
			changed = true
		}
		if body.DefaultFormat != nil {
			cfg.DefaultFormat = strings.TrimSpace(*body.DefaultFormat)
			changed = true
		}
		if body.DefaultTimeout != nil {
			cfg.DefaultTimeout = *body.DefaultTimeout
			changed = true
		}
		if body.Blueprint != nil {
			cfg.SetActiveBlueprint(*body.Blueprint)
			changed = true
		}
		if body.Email != nil {
			cfg.Email = strings.TrimSpace(*body.Email)
			changed = true
		}
		if body.DevURL != nil {
			cfg.DevURL = strings.TrimSpace(*body.DevURL)
			changed = true
		}
		// Company
		if body.CompanyName != nil {
			cfg.CompanyName = strings.TrimSpace(*body.CompanyName)
			changed = true
		}
		if body.CompanyDesc != nil {
			cfg.CompanyDescription = strings.TrimSpace(*body.CompanyDesc)
			changed = true
		}
		if body.CompanyGoals != nil {
			cfg.CompanyGoals = strings.TrimSpace(*body.CompanyGoals)
			changed = true
		}
		if body.CompanySize != nil {
			cfg.CompanySize = strings.TrimSpace(*body.CompanySize)
			changed = true
		}
		if body.CompanyPriority != nil {
			cfg.CompanyPriority = strings.TrimSpace(*body.CompanyPriority)
			changed = true
		}
		// Polling intervals (minimum 2 minutes, matching resolve functions)
		if body.InsightsPoll != nil {
			if *body.InsightsPoll < 2 {
				http.Error(w, "insights_poll_minutes must be >= 2", http.StatusBadRequest)
				return
			}
			cfg.InsightsPollMinutes = *body.InsightsPoll
			changed = true
		}
		if body.TaskFollowUp != nil {
			if *body.TaskFollowUp < 2 {
				http.Error(w, "task_follow_up_minutes must be >= 2", http.StatusBadRequest)
				return
			}
			cfg.TaskFollowUpMinutes = *body.TaskFollowUp
			changed = true
		}
		if body.TaskReminder != nil {
			if *body.TaskReminder < 2 {
				http.Error(w, "task_reminder_minutes must be >= 2", http.StatusBadRequest)
				return
			}
			cfg.TaskReminderMinutes = *body.TaskReminder
			changed = true
		}
		if body.TaskRecheck != nil {
			if *body.TaskRecheck < 2 {
				http.Error(w, "task_recheck_minutes must be >= 2", http.StatusBadRequest)
				return
			}
			cfg.TaskRecheckMinutes = *body.TaskRecheck
			changed = true
		}
		// Secret fields
		if body.APIKey != nil {
			cfg.APIKey = strings.TrimSpace(*body.APIKey)
			changed = true
		}
		if body.OpenAIAPIKey != nil {
			cfg.OpenAIAPIKey = strings.TrimSpace(*body.OpenAIAPIKey)
			changed = true
		}
		if body.AnthropicAPIKey != nil {
			cfg.AnthropicAPIKey = strings.TrimSpace(*body.AnthropicAPIKey)
			changed = true
		}
		if body.GeminiAPIKey != nil {
			cfg.GeminiAPIKey = strings.TrimSpace(*body.GeminiAPIKey)
			changed = true
		}
		if body.MinimaxAPIKey != nil {
			cfg.MinimaxAPIKey = strings.TrimSpace(*body.MinimaxAPIKey)
			changed = true
		}
		if body.OneAPIKey != nil {
			cfg.OneAPIKey = strings.TrimSpace(*body.OneAPIKey)
			changed = true
		}
		if body.ComposioAPIKey != nil {
			cfg.ComposioAPIKey = strings.TrimSpace(*body.ComposioAPIKey)
			changed = true
		}
		if body.TelegramToken != nil {
			cfg.TelegramBotToken = strings.TrimSpace(*body.TelegramToken)
			changed = true
		}
		if body.OpenclawToken != nil {
			cfg.OpenclawToken = strings.TrimSpace(*body.OpenclawToken)
			changed = true
		}
		if body.OpenclawGateway != nil {
			cfg.OpenclawGatewayURL = strings.TrimSpace(*body.OpenclawGateway)
			changed = true
		}

		if !changed {
			http.Error(w, "no fields to update", http.StatusBadRequest)
			return
		}

		if err := config.Save(cfg); err != nil {
			http.Error(w, "save failed", http.StatusInternalServerError)
			return
		}
		// Keep /health in sync for this process so the wizard choice is
		// reflected immediately without requiring a broker restart.
		if provider != "" {
			b.mu.Lock()
			providerChanged := b.runtimeProvider != provider
			b.runtimeProvider = provider
			if providerChanged {
				b.publishOfficeChangeLocked(officeChangeEvent{Kind: "office_reseeded"})
			}
			b.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleNexRegister wraps `nex-cli --cmd "setup <email>"` so the onboarding
// wizard can register a Nex identity without the user dropping to the terminal.
// Body: {"email": "..."}. Returns whatever the CLI prints on success, or the
// CLI's stderr on failure. Requires nex-cli to be installed and on PATH.
func (b *Broker) handleNexRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	email := strings.TrimSpace(body.Email)
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	output, err := nex.Register(r.Context(), email)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"email":  email,
		"output": output,
	})
}

func (b *Broker) handleOfficeMembers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		type officeMemberResponse struct {
			officeMember
			Status       string `json:"status,omitempty"`
			Activity     string `json:"activity,omitempty"`
			Detail       string `json:"detail,omitempty"`
			Task         string `json:"task,omitempty"`
			LiveActivity string `json:"liveActivity,omitempty"`
			LastTime     string `json:"lastTime,omitempty"`
		}
		now := time.Now()
		members := make([]officeMemberResponse, 0, len(b.members))
		for _, member := range b.members {
			entry := officeMemberResponse{officeMember: member}
			if snapshot, ok := b.activity[member.Slug]; ok {
				entry.Status = snapshot.Status
				entry.Activity = snapshot.Activity
				entry.Detail = snapshot.Detail
				entry.LiveActivity = snapshot.Detail
				entry.Task = snapshot.Detail
				entry.LastTime = snapshot.LastTime
			}
			if entry.Status == "" && b.lastTaggedAt != nil {
				if taggedAt, ok := b.lastTaggedAt[member.Slug]; ok && now.Sub(taggedAt) < 60*time.Second {
					entry.Status = "active"
					entry.Activity = "queued"
					entry.Detail = "active"
					entry.LiveActivity = "active"
					entry.Task = "active"
					entry.LastTime = taggedAt.UTC().Format(time.RFC3339)
				}
			}
			if entry.Status == "" {
				entry.Status = "idle"
			}
			if entry.Activity == "" {
				entry.Activity = "idle"
			}
			members = append(members, entry)
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"members": members})
	case http.MethodPost:
		var body struct {
			Action         string                    `json:"action"`
			Slug           string                    `json:"slug"`
			Name           string                    `json:"name"`
			Role           string                    `json:"role"`
			Expertise      []string                  `json:"expertise"`
			Personality    string                    `json:"personality"`
			PermissionMode string                    `json:"permission_mode"`
			AllowedTools   []string                  `json:"allowed_tools"`
			CreatedBy      string                    `json:"created_by"`
			Provider       *provider.ProviderBinding `json:"provider,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.TrimSpace(body.Action)
		slug := normalizeChannelSlug(body.Slug)
		if slug == "" {
			http.Error(w, "slug required", http.StatusBadRequest)
			return
		}
		now := time.Now().UTC().Format(time.RFC3339)

		b.mu.Lock()
		defer b.mu.Unlock()
		switch action {
		case "create":
			if b.findMemberLocked(slug) != nil {
				http.Error(w, "member already exists", http.StatusConflict)
				return
			}
			if body.Provider != nil {
				if err := provider.ValidateKind(body.Provider.Kind); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
			}
			member := officeMember{
				Slug:           slug,
				Name:           strings.TrimSpace(body.Name),
				Role:           strings.TrimSpace(body.Role),
				Expertise:      normalizeStringList(body.Expertise),
				Personality:    strings.TrimSpace(body.Personality),
				PermissionMode: strings.TrimSpace(body.PermissionMode),
				AllowedTools:   normalizeStringList(body.AllowedTools),
				CreatedBy:      strings.TrimSpace(body.CreatedBy),
				CreatedAt:      now,
			}
			if body.Provider != nil {
				member.Provider = *body.Provider
			}
			applyOfficeMemberDefaults(&member)

			// For openclaw agents, reach the gateway BEFORE we persist: if the
			// caller didn't supply a session key, auto-create one; either way,
			// attach the bridge subscription. If the gateway is unreachable we
			// fail the whole create so we don't persist a half-configured
			// member that can't actually talk.
			if member.Provider.Kind == provider.KindOpenclaw {
				if member.Provider.Openclaw == nil {
					member.Provider.Openclaw = &provider.OpenclawProviderBinding{}
				}
				bridge := b.openclawBridgeLocked()
				if bridge == nil {
					http.Error(w, "openclaw bridge not active; cannot create openclaw member", http.StatusServiceUnavailable)
					return
				}
				if member.Provider.Openclaw.SessionKey == "" {
					agentID := member.Provider.Openclaw.AgentID
					if agentID == "" {
						agentID = "main"
					}
					label := fmt.Sprintf("wuphf-%s-%d", slug, time.Now().UnixNano())
					key, err := bridge.CreateSession(r.Context(), agentID, label)
					if err != nil {
						http.Error(w, fmt.Sprintf("openclaw sessions.create: %v", err), http.StatusBadGateway)
						return
					}
					member.Provider.Openclaw.SessionKey = key
				}
				if err := bridge.AttachSlug(r.Context(), slug, member.Provider.Openclaw.SessionKey); err != nil {
					http.Error(w, fmt.Sprintf("openclaw subscribe: %v", err), http.StatusBadGateway)
					return
				}
			}

			b.members = append(b.members, member)
			b.memberIndex[member.Slug] = len(b.members) - 1
			// Add the new hire to every non-DM channel's Members list so they
			// can actually POST replies. canAccessChannelLocked enforces
			// ch.Members for every non-CEO agent sender; without this, a
			// wizard-hired specialist can be tagged and dispatched but its
			// reply is 403'd with "channel access denied" and the user sees
			// nothing. DM channels are intentionally skipped — DMs encode
			// the target agent in the slug and go through a different
			// membership gate.
			//
			// Policy note: this is broader than normalizeLoadedStateLocked's
			// seed (which only fills #general). A wizard hire joins every
			// topical channel by default; admins can narrow via
			// /channel-members action=remove afterwards. The rationale is
			// that an office member who can't post to any non-default
			// channel without a second configuration step violates the
			// principle of least surprise — the hire UI does not surface a
			// channel-scope picker, so the implicit default has to be
			// "office-wide."
			//
			// We also clear any stale Disabled entry for this slug. A fresh
			// hire shouldn't inherit a mute left over from a prior lifecycle.
			updatedChannels := make([]string, 0, len(b.channels))
			for i := range b.channels {
				if b.channels[i].isDM() {
					continue
				}
				mutated := false
				if !containsString(b.channels[i].Members, slug) {
					b.channels[i].Members = uniqueSlugs(append(b.channels[i].Members, slug))
					mutated = true
				}
				if containsString(b.channels[i].Disabled, slug) {
					// Allocate a fresh slice instead of reusing the
					// backing array via [:0]+append. The in-place form
					// is safe but reads as if it could clobber the
					// range — readability over one extra alloc on a
					// rare re-hire path.
					next := make([]string, 0, len(b.channels[i].Disabled))
					for _, d := range b.channels[i].Disabled {
						if d != slug {
							next = append(next, d)
						}
					}
					b.channels[i].Disabled = next
					mutated = true
				}
				if mutated {
					b.channels[i].UpdatedAt = now
					updatedChannels = append(updatedChannels, b.channels[i].Slug)
				}
			}
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "member_created", Slug: slug})
			// Notify SSE subscribers that these channels' rosters changed so
			// the UI sidebar refreshes without requiring a separate trigger.
			for _, chSlug := range updatedChannels {
				b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_updated", Slug: chSlug})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"member": member})
		case "update":
			member := b.findMemberLocked(slug)
			if member == nil {
				http.Error(w, "member not found", http.StatusNotFound)
				return
			}
			if body.Name != "" {
				member.Name = strings.TrimSpace(body.Name)
			}
			if body.Role != "" {
				member.Role = strings.TrimSpace(body.Role)
			}
			if body.Expertise != nil {
				member.Expertise = normalizeStringList(body.Expertise)
			}
			if body.Personality != "" {
				member.Personality = strings.TrimSpace(body.Personality)
			}
			if body.PermissionMode != "" {
				member.PermissionMode = strings.TrimSpace(body.PermissionMode)
			}
			if body.AllowedTools != nil {
				member.AllowedTools = normalizeStringList(body.AllowedTools)
			}
			if body.Provider != nil {
				if err := provider.ValidateKind(body.Provider.Kind); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				oldBinding := member.Provider
				newBinding := *body.Provider

				// Provider switch: reconcile the bridge state best-effort. We
				// don't block the update on gateway failures — the persisted
				// binding is the new truth, and a leaked old session is
				// recoverable via `openclaw sessions list` out-of-band.
				bridge := b.openclawBridgeLocked()

				fromOpenclaw := oldBinding.Kind == provider.KindOpenclaw
				toOpenclaw := newBinding.Kind == provider.KindOpenclaw

				if toOpenclaw {
					if bridge == nil {
						http.Error(w, "openclaw bridge not active; cannot switch agent to openclaw", http.StatusServiceUnavailable)
						return
					}
					if newBinding.Openclaw == nil {
						newBinding.Openclaw = &provider.OpenclawProviderBinding{}
					}
					if newBinding.Openclaw.SessionKey == "" {
						agentID := newBinding.Openclaw.AgentID
						if agentID == "" {
							agentID = "main"
						}
						label := fmt.Sprintf("wuphf-%s-%d", member.Slug, time.Now().UnixNano())
						key, err := bridge.CreateSession(r.Context(), agentID, label)
						if err != nil {
							http.Error(w, fmt.Sprintf("openclaw sessions.create: %v", err), http.StatusBadGateway)
							return
						}
						newBinding.Openclaw.SessionKey = key
					}
				}

				if fromOpenclaw && bridge != nil {
					// Detach old session from subscriptions. Best-effort; log via
					// the bridge's own system-message channel on failure. The
					// daemon-side session lingers (no sessions.end method); user
					// can prune via the OpenClaw CLI if they care.
					if err := bridge.DetachSlug(r.Context(), member.Slug); err != nil {
						go bridge.postSystemMessage(fmt.Sprintf("agent %q provider-switch: detach warning: %v", member.Slug, err))
					}
				}

				if toOpenclaw {
					if err := bridge.AttachSlug(r.Context(), member.Slug, newBinding.Openclaw.SessionKey); err != nil {
						http.Error(w, fmt.Sprintf("openclaw subscribe: %v", err), http.StatusBadGateway)
						return
					}
				}

				member.Provider = newBinding
			}
			applyOfficeMemberDefaults(member)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"member": member})
		case "remove":
			member := b.findMemberLocked(slug)
			if member == nil {
				http.Error(w, "member not found", http.StatusNotFound)
				return
			}
			if member.BuiltIn || slug == "ceo" {
				http.Error(w, "cannot remove built-in member", http.StatusBadRequest)
				return
			}
			// If the member was bridged to OpenClaw, unsubscribe from the
			// gateway. Best-effort: member removal must succeed even when
			// the gateway is unreachable. We do NOT call sessions.end because
			// the current daemon doesn't expose that method — the session
			// lingers daemon-side and the user can clean it up via the
			// OpenClaw CLI if they want to reclaim the slot.
			if member.Provider.Kind == provider.KindOpenclaw {
				if bridge := b.openclawBridgeLocked(); bridge != nil {
					if err := bridge.DetachSlug(r.Context(), member.Slug); err != nil {
						go bridge.postSystemMessage(fmt.Sprintf("agent %q removed: detach warning: %v", member.Slug, err))
					}
				}
			}
			filteredMembers := b.members[:0]
			for _, existing := range b.members {
				if existing.Slug != slug {
					filteredMembers = append(filteredMembers, existing)
				}
			}
			b.members = filteredMembers
			b.rebuildMemberIndexLocked()
			// Symmetry with action:create — skip DM channels (they encode
			// their target in the slug and go through a different
			// membership gate) and emit a channel_updated event per
			// actually-mutated channel so SSE subscribers refresh the
			// roster. Without this, the UI sidebar gets a half-signal
			// lifecycle (create emits channel_updated, remove does not).
			removedChannels := make([]string, 0, len(b.channels))
			for i := range b.channels {
				if b.channels[i].isDM() {
					continue
				}
				mutated := false
				if containsString(b.channels[i].Members, slug) {
					next := make([]string, 0, len(b.channels[i].Members))
					for _, existing := range b.channels[i].Members {
						if existing != slug {
							next = append(next, existing)
						}
					}
					b.channels[i].Members = next
					mutated = true
				}
				if containsString(b.channels[i].Disabled, slug) {
					next := make([]string, 0, len(b.channels[i].Disabled))
					for _, existing := range b.channels[i].Disabled {
						if existing != slug {
							next = append(next, existing)
						}
					}
					b.channels[i].Disabled = next
					mutated = true
				}
				if mutated {
					b.channels[i].UpdatedAt = now
					removedChannels = append(removedChannels, b.channels[i].Slug)
				}
			}
			for i := range b.tasks {
				if b.tasks[i].Owner == slug {
					b.tasks[i].Owner = ""
					b.tasks[i].Status = "open"
					b.tasks[i].UpdatedAt = now
				}
			}
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "member_removed", Slug: slug})
			for _, chSlug := range removedChannels {
				b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_updated", Slug: chSlug})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleGenerateMember(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if b.generateMemberFn == nil {
		http.Error(w, "generate not available", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		http.Error(w, "prompt required", http.StatusBadRequest)
		return
	}
	tmpl, err := b.generateMemberFn(prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tmpl)
}

func (b *Broker) handleGenerateChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if b.generateChannelFn == nil {
		http.Error(w, "generate not available", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	prompt := strings.TrimSpace(body.Prompt)
	if prompt == "" {
		http.Error(w, "prompt required", http.StatusBadRequest)
		return
	}
	tmpl, err := b.generateChannelFn(prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(tmpl)
}

func (b *Broker) handleChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		typeFilter := r.URL.Query().Get("type") // "dm" to see DMs, default excludes them
		b.mu.Lock()
		channels := make([]teamChannel, 0, len(b.channels))
		for _, ch := range b.channels {
			if typeFilter == "dm" {
				if ch.isDM() {
					channels = append(channels, ch)
				}
			} else {
				// Default: only return real channels, never DMs
				if !ch.isDM() {
					channels = append(channels, ch)
				}
			}
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"channels": channels})
	case http.MethodPost:
		var body struct {
			Action      string          `json:"action"`
			Slug        string          `json:"slug"`
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Members     []string        `json:"members"`
			CreatedBy   string          `json:"created_by"`
			Surface     *channelSurface `json:"surface,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		action := strings.TrimSpace(body.Action)
		slug := normalizeChannelSlug(body.Slug)
		now := time.Now().UTC().Format(time.RFC3339)
		b.mu.Lock()
		defer b.mu.Unlock()
		validateMembers := func(members []string) ([]string, string) {
			members = uniqueSlugs(members)
			if len(members) == 0 {
				return nil, ""
			}
			validated := make([]string, 0, len(members))
			var missing []string
			for _, member := range members {
				if b.findMemberLocked(member) == nil {
					missing = append(missing, member)
					continue
				}
				validated = append(validated, member)
			}
			return validated, strings.Join(missing, ", ")
		}
		switch action {
		case "create":
			if slug == "" {
				http.Error(w, "slug required", http.StatusBadRequest)
				return
			}
			if b.findChannelLocked(slug) != nil {
				http.Error(w, "channel already exists", http.StatusConflict)
				return
			}
			members, missing := validateMembers(body.Members)
			if missing != "" {
				http.Error(w, "unknown members: "+missing, http.StatusNotFound)
				return
			}
			members = append([]string{"ceo"}, members...)
			if creator := normalizeChannelSlug(body.CreatedBy); creator != "" && creator != "ceo" && b.findMemberLocked(creator) != nil {
				members = append(members, creator)
			}
			ch := teamChannel{
				Slug:        slug,
				Name:        strings.TrimSpace(body.Name),
				Description: strings.TrimSpace(body.Description),
				Members:     uniqueSlugs(members),
				Surface:     body.Surface,
				CreatedBy:   strings.TrimSpace(body.CreatedBy),
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if ch.Name == "" {
				ch.Name = slug
			}
			if ch.Description == "" {
				ch.Description = defaultTeamChannelDescription(ch.Slug, ch.Name)
			}
			b.channels = append(b.channels, ch)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_created", Slug: slug})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"channel": ch})
		case "update":
			if slug == "" {
				http.Error(w, "slug required", http.StatusBadRequest)
				return
			}
			ch := b.findChannelLocked(slug)
			if ch == nil {
				http.Error(w, "channel not found", http.StatusNotFound)
				return
			}
			if name := strings.TrimSpace(body.Name); name != "" {
				ch.Name = name
			}
			if description := strings.TrimSpace(body.Description); description != "" {
				ch.Description = description
			}
			if body.Surface != nil {
				ch.Surface = body.Surface
			}
			if body.Members != nil {
				members, missing := validateMembers(body.Members)
				if missing != "" {
					http.Error(w, "unknown members: "+missing, http.StatusNotFound)
					return
				}
				ch.Members = uniqueSlugs(append([]string{"ceo"}, members...))
				if len(ch.Disabled) > 0 {
					filtered := make([]string, 0, len(ch.Disabled))
					for _, disabled := range ch.Disabled {
						if !containsString(ch.Members, disabled) {
							filtered = append(filtered, disabled)
						}
					}
					ch.Disabled = filtered
				}
			}
			ch.UpdatedAt = now
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_updated", Slug: slug})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"channel": ch})
		case "remove":
			if slug == "" || slug == "general" {
				http.Error(w, "cannot remove channel", http.StatusBadRequest)
				return
			}
			idx := -1
			for i := range b.channels {
				if b.channels[i].Slug == slug {
					idx = i
					break
				}
			}
			if idx == -1 {
				http.Error(w, "channel not found", http.StatusNotFound)
				return
			}
			b.channels = append(b.channels[:idx], b.channels[idx+1:]...)
			filteredMessages := b.messages[:0]
			for _, msg := range b.messages {
				if normalizeChannelSlug(msg.Channel) != slug {
					filteredMessages = append(filteredMessages, msg)
				}
			}
			b.messages = filteredMessages
			filteredTasks := b.tasks[:0]
			for _, task := range b.tasks {
				if normalizeChannelSlug(task.Channel) != slug {
					filteredTasks = append(filteredTasks, task)
				}
			}
			b.tasks = filteredTasks
			filteredRequests := b.requests[:0]
			for _, req := range b.requests {
				if normalizeChannelSlug(req.Channel) != slug {
					filteredRequests = append(filteredRequests, req)
				}
			}
			b.requests = filteredRequests
			b.pendingInterview = firstBlockingRequest(b.requests)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			b.publishOfficeChangeLocked(officeChangeEvent{Kind: "channel_removed", Slug: slug})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCreateDM creates or returns an existing DM channel.
// POST /channels/dm — body: {members: ["human", "engineering"], type: "direct"|"group"}
func (b *Broker) handleCreateDM(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Members []string `json:"members"`
		Type    string   `json:"type"` // "direct" or "group"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if len(body.Members) < 2 {
		http.Error(w, "at least 2 members required", http.StatusBadRequest)
		return
	}
	// Validate: at least one member must be "human" (no agent-to-agent DMs).
	hasHuman := false
	for _, m := range body.Members {
		if m == "human" || m == "you" {
			hasHuman = true
			break
		}
	}
	if !hasHuman {
		http.Error(w, "DM must include a human member; agent-to-agent DMs are not allowed", http.StatusBadRequest)
		return
	}

	if b.channelStore == nil {
		http.Error(w, "channel store not initialized", http.StatusInternalServerError)
		return
	}

	var (
		ch      *channel.Channel
		err     error
		created bool
	)
	dmType := strings.TrimSpace(strings.ToLower(body.Type))
	if dmType == "group" && len(body.Members) > 2 {
		existing, ok := b.channelStore.FindDirectByMembers(body.Members[0], body.Members[1])
		if !ok || existing == nil {
			created = true
		}
		ch, err = b.channelStore.GetOrCreateGroup(body.Members, "human")
	} else {
		// Default: direct (1:1). For >2 members use group.
		if len(body.Members) > 2 {
			existing, ok := b.channelStore.FindDirectByMembers(body.Members[0], body.Members[1])
			if !ok || existing == nil {
				created = true
			}
			ch, err = b.channelStore.GetOrCreateGroup(body.Members, "human")
		} else {
			// Normalize: find the non-human member for the slug.
			agentSlug := ""
			for _, m := range body.Members {
				if m != "human" && m != "you" {
					agentSlug = m
					break
				}
			}
			if agentSlug == "" {
				http.Error(w, "could not determine agent member", http.StatusBadRequest)
				return
			}
			_, exists := b.channelStore.FindDirectByMembers("human", agentSlug)
			created = !exists
			ch, err = b.channelStore.GetOrCreateDirect("human", agentSlug)
		}
	}
	if err != nil {
		http.Error(w, "failed to create DM: "+err.Error(), http.StatusInternalServerError)
		return
	}

	b.mu.Lock()
	if b.findChannelLocked(ch.Slug) == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		target := DMTargetAgent(ch.Slug)
		description := "Group direct messages"
		memberSlugs := append([]string(nil), body.Members...)
		if target != "" {
			description = "Direct messages with " + target
			memberSlugs = []string{"human", target}
		}
		name := strings.TrimSpace(ch.Name)
		if name == "" {
			name = ch.Slug
		}
		b.channels = append(b.channels, teamChannel{
			Slug:        ch.Slug,
			Name:        name,
			Type:        "dm",
			Description: description,
			Members:     uniqueSlugs(memberSlugs),
			CreatedBy:   "human",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	_ = b.saveLocked()
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":      ch.ID,
		"slug":    ch.Slug,
		"type":    ch.Type,
		"name":    ch.Name,
		"created": created,
	})
}

func (b *Broker) handleChannelMembers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
		b.mu.Lock()
		ch := b.findChannelLocked(channel)
		if ch == nil {
			b.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"channel": channel, "members": []map[string]any{}})
			return
		}
		memberInfo := make([]map[string]any, 0, len(ch.Members))
		for _, member := range ch.Members {
			memberInfo = append(memberInfo, map[string]any{
				"slug":     member,
				"disabled": !b.channelMemberEnabledLocked(channel, member),
			})
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"channel": channel, "members": memberInfo})
	case http.MethodPost:
		var body struct {
			Channel string `json:"channel"`
			Action  string `json:"action"`
			Slug    string `json:"slug"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		channel := normalizeChannelSlug(body.Channel)
		member := normalizeChannelSlug(body.Slug)
		action := strings.TrimSpace(body.Action)
		if member == "" {
			http.Error(w, "slug required", http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		ch := b.findChannelLocked(channel)
		if ch == nil {
			b.mu.Unlock()
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		memberRecord := b.findMemberLocked(member)
		if memberRecord == nil {
			b.mu.Unlock()
			http.Error(w, "member not found", http.StatusNotFound)
			return
		}
		// Lead agents (BuiltIn) cannot be disabled or removed from any
		// channel. The blueprint's lead is the tag target for the onboarding
		// kickoff and the default owner for channel membership; the UI locks
		// these interactions too. Keeps the "ceo" literal as a legacy guard
		// for team states that predate the BuiltIn field.
		if (memberRecord.BuiltIn || member == "ceo") && (action == "remove" || action == "disable") {
			b.mu.Unlock()
			http.Error(w, "cannot remove or disable lead agent", http.StatusBadRequest)
			return
		}
		switch action {
		case "add":
			ch.Members = uniqueSlugs(append(ch.Members, member))
		case "remove":
			filtered := ch.Members[:0]
			for _, existing := range ch.Members {
				if existing != member {
					filtered = append(filtered, existing)
				}
			}
			ch.Members = filtered
			disabled := ch.Disabled[:0]
			for _, existing := range ch.Disabled {
				if existing != member {
					disabled = append(disabled, existing)
				}
			}
			ch.Disabled = disabled
		case "disable":
			if !b.channelHasMemberLocked(channel, member) {
				ch.Members = uniqueSlugs(append(ch.Members, member))
			}
			ch.Disabled = uniqueSlugs(append(ch.Disabled, member))
		case "enable":
			filtered := ch.Disabled[:0]
			for _, existing := range ch.Disabled {
				if existing != member {
					filtered = append(filtered, existing)
				}
			}
			ch.Disabled = filtered
		default:
			b.mu.Unlock()
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		ch.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		state := map[string]any{
			"channel":  ch.Slug,
			"members":  ch.Members,
			"disabled": ch.Disabled,
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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

func (b *Broker) handleMessages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		b.handlePostMessage(w, r)
	case http.MethodGet:
		b.handleGetMessages(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleOTLPLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	events := parseOTLPUsageEvents(payload)
	b.mu.Lock()
	for _, event := range events {
		if strings.TrimSpace(event.AgentSlug) == "" {
			continue
		}
		b.recordUsageLocked(event)
	}
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"accepted": len(events)})
}

func (b *Broker) handleNexNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Channel     string   `json:"channel"`
		EventID     string   `json:"event_id"`
		Title       string   `json:"title"`
		Content     string   `json:"content"`
		Tagged      []string `json:"tagged"`
		ReplyTo     string   `json:"reply_to"`
		Source      string   `json:"source"`
		SourceLabel string   `json:"source_label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	msg, duplicate, err := b.PostAutomationMessage("nex", body.Channel, body.Title, body.Content, body.EventID, body.Source, body.SourceLabel, body.Tagged, body.ReplyTo)
	if err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":        msg.ID,
		"duplicate": duplicate,
	})
}

type usageEvent struct {
	AgentSlug           string
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	CostUsd             float64
}

const messageUsageAttachMaxAge = 15 * time.Minute

func (b *Broker) recordUsageLocked(event usageEvent) {
	if b.usage.Agents == nil {
		b.usage.Agents = make(map[string]usageTotals)
	}
	if b.usage.Since == "" {
		b.usage.Since = time.Now().UTC().Format(time.RFC3339)
	}
	agentTotal := b.usage.Agents[event.AgentSlug]
	applyUsageEvent(&agentTotal, event)
	b.usage.Agents[event.AgentSlug] = agentTotal

	session := b.usage.Session
	applyUsageEvent(&session, event)
	b.usage.Session = session

	total := b.usage.Total
	applyUsageEvent(&total, event)
	b.usage.Total = total
	b.attachUsageToRecentMessagesLocked(event)
}

func applyUsageEvent(dst *usageTotals, event usageEvent) {
	dst.InputTokens += event.InputTokens
	dst.OutputTokens += event.OutputTokens
	dst.CacheReadTokens += event.CacheReadTokens
	dst.CacheCreationTokens += event.CacheCreationTokens
	dst.TotalTokens += event.InputTokens + event.OutputTokens + event.CacheReadTokens + event.CacheCreationTokens
	dst.CostUsd += event.CostUsd
	dst.Requests++
}

func usageEventToMessageUsage(event usageEvent) *messageUsage {
	total := event.InputTokens + event.OutputTokens + event.CacheReadTokens + event.CacheCreationTokens
	if total == 0 {
		return nil
	}
	return &messageUsage{
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
		TotalTokens:         total,
	}
}

func cloneMessageUsage(src *messageUsage) *messageUsage {
	if src == nil {
		return nil
	}
	cp := *src
	return &cp
}

func messageIsWithinUsageAttachWindow(timestamp string, now time.Time) bool {
	ts := strings.TrimSpace(timestamp)
	if ts == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return true
		}
	}
	return now.Sub(parsed) <= messageUsageAttachMaxAge
}

func (b *Broker) attachUsageToRecentMessagesLocked(event usageEvent) {
	usage := usageEventToMessageUsage(event)
	if usage == nil {
		return
	}
	slug := strings.TrimSpace(event.AgentSlug)
	if slug == "" {
		return
	}
	now := time.Now().UTC()
	for i := len(b.messages) - 1; i >= 0; i-- {
		msg := &b.messages[i]
		if strings.TrimSpace(msg.From) != slug {
			continue
		}
		if msg.Usage != nil {
			break
		}
		if !messageIsWithinUsageAttachWindow(msg.Timestamp, now) {
			break
		}
		msg.Usage = cloneMessageUsage(usage)
	}
}

// RecordAgentUsage records token usage from a provider stream result for a given agent.
func (b *Broker) RecordAgentUsage(slug, model string, usage provider.ClaudeUsage) {
	event := usageEvent{
		AgentSlug:           slug,
		InputTokens:         usage.InputTokens,
		OutputTokens:        usage.OutputTokens,
		CacheReadTokens:     usage.CacheReadTokens,
		CacheCreationTokens: usage.CacheCreationTokens,
		CostUsd:             usage.CostUSD,
	}
	b.mu.Lock()
	b.recordUsageLocked(event)
	_ = b.saveLocked()
	b.mu.Unlock()
}

func parseOTLPUsageEvents(payload map[string]any) []usageEvent {
	resourceLogs, _ := payload["resourceLogs"].([]any)
	var events []usageEvent
	for _, resourceLog := range resourceLogs {
		resourceMap, _ := resourceLog.(map[string]any)
		resourceAttrs := otlpAttributesMap(nestedMap(resourceMap, "resource"))
		scopeLogs, _ := resourceMap["scopeLogs"].([]any)
		for _, scopeLog := range scopeLogs {
			scopeMap, _ := scopeLog.(map[string]any)
			logRecords, _ := scopeMap["logRecords"].([]any)
			for _, logRecord := range logRecords {
				recordMap, _ := logRecord.(map[string]any)
				attrs := otlpAttributesMap(recordMap)
				for k, v := range resourceAttrs {
					if _, exists := attrs[k]; !exists {
						attrs[k] = v
					}
				}
				if attrs["event.name"] != "api_request" && attrs["event_name"] != "api_request" {
					continue
				}
				slug := attrs["agent.slug"]
				if slug == "" {
					slug = attrs["agent_slug"]
				}
				if slug == "" {
					continue
				}
				events = append(events, usageEvent{
					AgentSlug:           slug,
					InputTokens:         otlpIntValue(attrs["input_tokens"]),
					OutputTokens:        otlpIntValue(attrs["output_tokens"]),
					CacheReadTokens:     otlpIntValue(attrs["cache_read_tokens"]),
					CacheCreationTokens: otlpIntValue(attrs["cache_creation_tokens"]),
					CostUsd:             otlpFloatValue(attrs["cost_usd"]),
				})
			}
		}
	}
	return events
}

func nestedMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	child, _ := m[key].(map[string]any)
	return child
}

func otlpAttributesMap(record map[string]any) map[string]string {
	out := make(map[string]string)
	if record == nil {
		return out
	}
	attrs, _ := record["attributes"].([]any)
	for _, attr := range attrs {
		attrMap, _ := attr.(map[string]any)
		key, _ := attrMap["key"].(string)
		if key == "" {
			continue
		}
		out[key] = otlpAnyValue(attrMap["value"])
	}
	return out
}

func otlpAnyValue(raw any) string {
	valMap, _ := raw.(map[string]any)
	for _, key := range []string{"stringValue", "intValue", "doubleValue", "boolValue"} {
		if value, ok := valMap[key]; ok {
			return fmt.Sprintf("%v", value)
		}
	}
	return ""
}

func otlpIntValue(raw string) int {
	if raw == "" {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	return n
}

func otlpFloatValue(raw string) float64 {
	if raw == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(raw, 64)
	return v
}

func (b *Broker) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From    string   `json:"from"`
		Channel string   `json:"channel"`
		Kind    string   `json:"kind"`
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tagged  []string `json:"tagged"`
		ReplyTo string   `json:"reply_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	if firstBlockingRequest(b.requests) != nil {
		b.mu.Unlock()
		http.Error(w, "request pending; answer required before chat resumes", http.StatusConflict)
		return
	}

	b.counter++
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	// Auto-create DM conversations on first message (like Slack's conversations.open)
	if b.findChannelLocked(channel) == nil {
		if IsDMSlug(channel) {
			if dm := b.ensureDMConversationLocked(channel); dm != nil {
				channel = dm.Slug
			}
		} else if b.channelStore != nil {
			if _, ok := b.channelStore.GetBySlug(channel); !ok {
				b.mu.Unlock()
				http.Error(w, "channel not found", http.StatusNotFound)
				return
			}
		} else {
			b.mu.Unlock()
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
	}
	if !b.canAccessChannelLocked(body.From, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	// Auto-promote @slug mentions in the body into the tagged array. If a
	// user or agent typed `@pm`, treat it as a tag — `extractMentionedSlugs`
	// already restricts to registered agent slugs, so conversational use of
	// an @ that doesn't match an agent is untouched. Previously this ran for
	// agent posts only, on the theory that humans might want @ to be merely
	// conversational. In practice humans expect every @agent to notify, and
	// the web composer does not always commit typed @-text into an explicit
	// tag chip.
	//
	// Senders allowed to auto-promote: empty / "you" / "human" (humans) and
	// any registered agent slug. Everything else — "system", "nex", future
	// synthetic senders — is excluded by default so automation posts do not
	// accidentally wake agents on every @-reference they quote.
	//
	// Exception: when the human explicitly tags the lead (CEO), do not
	// auto-promote OTHER agents mentioned in the body. Example:
	// "@ceo ask @reviewer to ..." — the human's intent is for CEO to route,
	// not for the broker to fan out in parallel. Without this guard the
	// reviewer gets notified twice (by auto-promote AND later by CEO's
	// explicit tag), spawning two turns with nearly identical answers.
	tagged := uniqueSlugs(body.Tagged)
	sender := normalizeActorSlug(body.From)
	isHuman := sender == "" || sender == "you" || sender == "human"
	leadSlug := officeLeadSlugFrom(b.members)
	mentionedSlugs := extractMentionedSlugs(body.Content)
	leadExplicitlyTagged := leadSlug != "" && containsString(tagged, leadSlug)
	if isHuman && !leadExplicitlyTagged && leadSlug != "" && containsString(mentionedSlugs, leadSlug) && b.findMemberLocked(leadSlug) != nil {
		tagged = append(tagged, leadSlug)
		leadExplicitlyTagged = true
	}
	suppressAutoPromote := isHuman && leadExplicitlyTagged
	if b.senderMayAutoPromoteLocked(sender) && !suppressAutoPromote {
		for _, slug := range mentionedSlugs {
			if slug == sender {
				continue
			}
			if b.findMemberLocked(slug) == nil {
				continue
			}
			if !containsString(tagged, slug) {
				tagged = append(tagged, slug)
			}
		}
	}
	for _, taggedSlug := range tagged {
		switch taggedSlug {
		case "you", "human", "system":
			continue
		}
		if b.findMemberLocked(taggedSlug) == nil {
			b.mu.Unlock()
			http.Error(w, "unknown tagged member", http.StatusBadRequest)
			return
		}
	}

	// Thread auto-tagging: when a HUMAN replies in a thread, notify all
	// other agents who have already participated. This keeps the team
	// aligned without requiring the human to re-tag on every reply.
	// Agent-to-agent auto-tagging is intentionally skipped: focus mode
	// routing (specialist → lead only) already handles that path, and
	// auto-tagging agent replies causes broadcast loops.
	replyTo := strings.TrimSpace(body.ReplyTo)
	isHumanSender := body.From == "you" || body.From == "human"
	if replyTo != "" && isHumanSender {
		threadRoot := replyTo
		threadParticipants := []string{}
		for _, existing := range b.messages {
			inThread := existing.ID == threadRoot || existing.ReplyTo == threadRoot
			if inThread && existing.From != body.From {
				// Include agents (skip "you"/"human" — they see via the web UI poll)
				if existing.From != "you" && existing.From != "human" && b.findMemberLocked(existing.From) != nil {
					threadParticipants = append(threadParticipants, existing.From)
				}
			}
		}
		tagged = uniqueSlugs(append(tagged, threadParticipants...))
	}

	// Dedup near-identical consecutive broadcasts from the same agent in the
	// same channel + thread within a short window. Observed symptom: a single
	// CEO turn emits 2-3 team_broadcast calls with the same content in
	// slightly different wording, each costing a full round-trip downstream.
	// The prompt tells the model "at most one broadcast per turn", but that
	// rule is routinely ignored; this is the broker-side safety net.
	//
	// Humans and system senders are exempt — this only fires for agent posts.
	if !isHuman && sender != "" && sender != "system" && sender != "nex" {
		if b.isDuplicateAgentBroadcastLocked(sender, channel, replyTo, body.Content) {
			b.counter--
			b.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "",
				"deduped":    true,
				"total":      len(b.messages),
				"suppressed": "duplicate broadcast from the same agent in the same thread within the dedup window",
			})
			return
		}
	}

	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      body.From,
		Channel:   channel,
		Kind:      strings.TrimSpace(body.Kind),
		Title:     strings.TrimSpace(body.Title),
		Content:   body.Content,
		Tagged:    tagged,
		ReplyTo:   replyTo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)
	total := len(b.messages)

	// Track which agents were tagged — they should show "typing" immediately
	if len(msg.Tagged) > 0 && (msg.From == "you" || msg.From == "human") {
		if b.lastTaggedAt == nil {
			b.lastTaggedAt = make(map[string]time.Time)
		}
		for _, slug := range msg.Tagged {
			b.lastTaggedAt[slug] = time.Now()
		}
	}

	// Clear typing indicator when an agent posts a reply
	if msg.From != "you" && msg.From != "human" && b.lastTaggedAt != nil {
		delete(b.lastTaggedAt, msg.From)
	}

	// Auto-detect skill proposals from CEO messages
	b.parseSkillProposalLocked(msg)

	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":    msg.ID,
		"total": total,
	})
}

func (b *Broker) handleReactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		MessageID string `json:"message_id"`
		Emoji     string `json:"emoji"`
		From      string `json:"from"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.MessageID == "" || body.Emoji == "" || body.From == "" {
		http.Error(w, "message_id, emoji, and from are required", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	found := false
	for i := range b.messages {
		if b.messages[i].ID == body.MessageID {
			// Don't duplicate: same emoji from same agent
			for _, r := range b.messages[i].Reactions {
				if r.Emoji == body.Emoji && r.From == body.From {
					b.mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "duplicate": true})
					return
				}
			}
			b.messages[i].Reactions = append(b.messages[i].Reactions, messageReaction{
				Emoji: body.Emoji,
				From:  body.From,
			})
			found = true
			break
		}
	}
	if !found {
		b.mu.Unlock()
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}
	_ = b.saveLocked()
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// RecordTelegramGroup saves a group chat ID and title seen by the transport.
func (b *Broker) RecordTelegramGroup(chatID int64, title string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seenTelegramGroups == nil {
		b.seenTelegramGroups = make(map[int64]string)
	}
	b.seenTelegramGroups[chatID] = title
}

// SeenTelegramGroups returns all group chats the transport has seen.
func (b *Broker) SeenTelegramGroups() map[int64]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seenTelegramGroups == nil {
		return nil
	}
	out := make(map[int64]string, len(b.seenTelegramGroups))
	for k, v := range b.seenTelegramGroups {
		out[k] = v
	}
	return out
}

// MarkRoutingTargets records implicit routing recipients as active so the UI
// can show typing/thinking state without persisting a routing banner message.
func (b *Broker) MarkRoutingTargets(slugs []string) {
	if len(slugs) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lastTaggedAt == nil {
		b.lastTaggedAt = make(map[string]time.Time)
	}
	now := time.Now()
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		b.lastTaggedAt[slug] = now
	}
}

// PostSystemMessage posts a lightweight system message that shows progress without blocking.
func (b *Broker) PostSystemMessage(channel, content, kind string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counter++
	if channel == "" {
		channel = "general"
	}
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   normalizeChannelSlug(channel),
		Kind:      kind,
		Content:   content,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)
}

func (b *Broker) PostMessage(from, channel, content string, tagged []string, replyTo string) (channelMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if firstBlockingRequest(b.requests) != nil {
		return channelMessage{}, fmt.Errorf("request pending; answer required before chat resumes")
	}
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	if b.findChannelLocked(channel) == nil {
		if IsDMSlug(channel) {
			if dm := b.ensureDMConversationLocked(channel); dm != nil {
				channel = dm.Slug
			}
		}
		if b.findChannelLocked(channel) == nil {
			return channelMessage{}, fmt.Errorf("channel not found")
		}
	}
	if !b.canAccessChannelLocked(from, channel) {
		return channelMessage{}, fmt.Errorf("channel access denied")
	}
	b.counter++
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      from,
		Channel:   channel,
		Kind:      "",
		Title:     "",
		Content:   strings.TrimSpace(content),
		Tagged:    uniqueSlugs(tagged),
		ReplyTo:   strings.TrimSpace(replyTo),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)
	// Clear typing indicator — agent has replied
	if b.lastTaggedAt != nil {
		delete(b.lastTaggedAt, msg.From)
	}
	b.appendActionLocked("automation", msg.Source, channel, msg.From, truncateSummary(msg.Title+" "+msg.Content, 140), msg.ID)
	if err := b.saveLocked(); err != nil {
		return channelMessage{}, err
	}
	return msg, nil
}

func (b *Broker) PostAutomationMessage(from, channel, title, content, eventID, source, sourceLabel string, tagged []string, replyTo string) (channelMessage, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if strings.TrimSpace(eventID) != "" {
		for _, existing := range b.messages {
			if existing.EventID != "" && existing.EventID == strings.TrimSpace(eventID) {
				return existing, true, nil
			}
		}
	}

	b.counter++
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	msg := channelMessage{
		ID:          fmt.Sprintf("msg-%d", b.counter),
		From:        from,
		Channel:     channel,
		Kind:        "automation",
		Source:      strings.TrimSpace(source),
		SourceLabel: strings.TrimSpace(sourceLabel),
		EventID:     strings.TrimSpace(eventID),
		Title:       strings.TrimSpace(title),
		Content:     strings.TrimSpace(content),
		Tagged:      tagged,
		ReplyTo:     strings.TrimSpace(replyTo),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	if msg.Source == "" {
		msg.Source = "context_graph"
	}
	if msg.SourceLabel == "" {
		msg.SourceLabel = "Nex"
	}
	if msg.From == "" {
		msg.From = "nex"
	}

	b.appendMessageLocked(msg)
	if err := b.saveLocked(); err != nil {
		return channelMessage{}, false, err
	}
	return msg, false, nil
}

func (b *Broker) CreateRequest(req humanInterview) (humanInterview, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel := normalizeChannelSlug(req.Channel)
	if channel == "" {
		channel = "general"
	}
	if b.findChannelLocked(channel) == nil {
		return humanInterview{}, fmt.Errorf("channel not found")
	}
	b.counter++
	now := time.Now().UTC().Format(time.RFC3339)
	req.ID = fmt.Sprintf("request-%d", b.counter)
	req.Channel = channel
	req.CreatedAt = now
	req.UpdatedAt = now
	req.Kind = normalizeRequestKind(req.Kind)
	req.Options, req.RecommendedID = normalizeRequestOptions(req.Kind, req.RecommendedID, req.Options)
	if strings.TrimSpace(req.Status) == "" {
		req.Status = "pending"
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = "Request"
	}
	b.requests = append(b.requests, req)
	b.pendingInterview = firstBlockingRequest(b.requests)
	b.appendActionLocked("request_created", "office", channel, req.From, truncateSummary(req.Title+" "+req.Question, 140), req.ID)
	if err := b.saveLocked(); err != nil {
		return humanInterview{}, err
	}
	return req, nil
}

func (b *Broker) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 10
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if limit > 100 {
		limit = 100
	}

	sinceID := q.Get("since_id")
	mySlug := strings.TrimSpace(q.Get("my_slug"))
	viewerSlug := strings.TrimSpace(q.Get("viewer_slug"))
	threadID := strings.TrimSpace(q.Get("thread_id"))
	if threadID == "" {
		threadID = strings.TrimSpace(q.Get("reply_to"))
	}
	scope := normalizeMessageScope(q.Get("scope"))
	if rawScope := strings.TrimSpace(q.Get("scope")); rawScope != "" && scope == "" {
		http.Error(w, "invalid message scope", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(q.Get("channel"))
	if channel == "" {
		channel = "general"
	}
	accessSlug := mySlug
	if accessSlug == "" {
		accessSlug = viewerSlug
	}

	b.mu.Lock()
	// Auto-create DM conversation on read (user opens DM before sending)
	if IsDMSlug(channel) && b.findChannelLocked(channel) == nil {
		if dm := b.ensureDMConversationLocked(channel); dm != nil {
			channel = dm.Slug
		}
	}
	if !b.canAccessChannelLocked(accessSlug, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	channelMessages := make([]channelMessage, 0, len(b.messages))
	for _, msg := range b.messages {
		if normalizeChannelSlug(msg.Channel) != channel {
			continue
		}
		channelMessages = append(channelMessages, msg)
	}
	messageIndex := make(map[string]channelMessage, len(channelMessages))
	for _, msg := range channelMessages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			messageIndex[id] = msg
		}
	}
	messages := make([]channelMessage, 0, len(channelMessages))
	for _, msg := range channelMessages {
		if b.sessionMode == SessionModeOneOnOne && !b.isOneOnOneDMMessage(msg) {
			continue
		}
		if threadID != "" && !messageInThread(msg, threadID) {
			continue
		}
		if scope != "" && viewerSlug != "" && !messageMatchesViewerScope(msg, viewerSlug, scope, messageIndex) {
			continue
		}
		messages = append(messages, msg)
	}
	if sinceID != "" {
		for i, m := range messages {
			if m.ID == sinceID {
				messages = messages[i+1:]
				break
			}
		}
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	// Copy to avoid race
	result := make([]channelMessage, len(messages))
	copy(result, messages)
	b.mu.Unlock()

	taggedCount := 0
	taggedSlug := mySlug
	if taggedSlug == "" {
		taggedSlug = viewerSlug
	}
	if taggedSlug != "" {
		for _, m := range result {
			for _, t := range m.Tagged {
				if t == taggedSlug {
					taggedCount++
					break
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"channel":      channel,
		"messages":     result,
		"tagged_count": taggedCount,
	})
}

func messageInThread(msg channelMessage, threadID string) bool {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return true
	}
	return strings.TrimSpace(msg.ID) == threadID || strings.TrimSpace(msg.ReplyTo) == threadID
}

func normalizeMessageScope(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "all", "channel":
		return ""
	case "agent", "inbox", "outbox":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return ""
	}
}

func messageMatchesViewerScope(msg channelMessage, viewerSlug, scope string, messagesByID map[string]channelMessage) bool {
	scope = normalizeMessageScope(scope)
	switch scope {
	case "inbox":
		return messageBelongsToViewerInbox(msg, viewerSlug, messagesByID)
	case "outbox":
		return messageBelongsToViewerOutbox(msg, viewerSlug)
	case "agent":
		return messageVisibleToViewer(msg, viewerSlug, messagesByID)
	default:
		return true
	}
}

func messageVisibleToViewer(msg channelMessage, viewerSlug string, messagesByID map[string]channelMessage) bool {
	return messageBelongsToViewerOutbox(msg, viewerSlug) || messageBelongsToViewerInbox(msg, viewerSlug, messagesByID)
}

func messageBelongsToViewerOutbox(msg channelMessage, viewerSlug string) bool {
	viewerSlug = strings.TrimSpace(viewerSlug)
	if viewerSlug == "" || viewerSlug == "ceo" {
		return true
	}
	return strings.TrimSpace(msg.From) == viewerSlug
}

func messageBelongsToViewerInbox(msg channelMessage, viewerSlug string, messagesByID map[string]channelMessage) bool {
	viewerSlug = strings.TrimSpace(viewerSlug)
	if viewerSlug == "" || viewerSlug == "ceo" {
		return true
	}
	from := strings.TrimSpace(msg.From)
	switch from {
	case viewerSlug:
		return false
	case "you", "human", "ceo":
		return true
	}
	for _, tagged := range msg.Tagged {
		tagged = strings.TrimSpace(tagged)
		if tagged == viewerSlug || tagged == "all" {
			return true
		}
	}
	return messageRepliesToViewerThread(msg, viewerSlug, messagesByID)
}

func messageRepliesToViewerThread(msg channelMessage, viewerSlug string, messagesByID map[string]channelMessage) bool {
	replyTo := strings.TrimSpace(msg.ReplyTo)
	if replyTo == "" || viewerSlug == "" {
		return false
	}
	seen := map[string]bool{}
	for replyTo != "" {
		if seen[replyTo] {
			return false
		}
		seen[replyTo] = true
		parent, ok := messagesByID[replyTo]
		if !ok {
			return false
		}
		if strings.TrimSpace(parent.From) == viewerSlug {
			return true
		}
		replyTo = strings.TrimSpace(parent.ReplyTo)
	}
	return false
}

// isOneOnOneDMMessage returns true if msg belongs in the 1:1 DM conversation.
// Only messages exclusively between the human and the 1:1 agent pass through.
// Caller must hold b.mu.
func (b *Broker) isOneOnOneDMMessage(msg channelMessage) bool {
	agent := b.oneOnOneAgent

	switch msg.From {
	case "you", "human":
		// Human messages: only if untagged (direct conversation) or
		// explicitly tagging the 1:1 agent.
		if len(msg.Tagged) == 0 {
			return true
		}
		for _, t := range msg.Tagged {
			if t == agent {
				return true
			}
		}
		return false

	case agent:
		// Agent messages: only if untagged (direct reply to human) or
		// explicitly tagging the human.
		if len(msg.Tagged) == 0 {
			return true
		}
		for _, t := range msg.Tagged {
			if t == "you" || t == "human" {
				return true
			}
		}
		return false

	case "system":
		// System messages: only if they mention the 1:1 agent or human,
		// or are general system announcements (no routing indicators).
		if msg.Kind == "routing" {
			return false
		}
		return true

	default:
		// Messages from any other agent do not belong in this DM.
		return false
	}
}

// capturePaneActivity captures tmux pane content for each agent and detects
// activity by comparing with the previous snapshot. If content changed,
// the agent is active and we return the last 5 non-empty lines as a stream.
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
		paneOut, err := exec.Command("tmux", "-L", "wuphf", "capture-pane",
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

func (b *Broker) handleMembers(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
	if channel == "" {
		channel = "general"
	}
	viewerSlug := strings.TrimSpace(r.URL.Query().Get("viewer_slug"))
	if !b.canAccessChannelLocked(viewerSlug, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	type memberView struct {
		name        string
		role        string
		lastMessage string
		lastTime    string
		disabled    bool
	}
	members := make(map[string]memberView)
	if ch := b.findChannelLocked(channel); ch != nil {
		for _, member := range ch.Members {
			if b.sessionMode == SessionModeOneOnOne && member != b.oneOnOneAgent {
				continue
			}
			info := memberView{disabled: containsString(ch.Disabled, member)}
			if office := b.findMemberLocked(member); office != nil {
				info.name = office.Name
				info.role = office.Role
			}
			members[member] = info
		}
	}
	for _, msg := range b.messages {
		if normalizeChannelSlug(msg.Channel) != channel {
			continue
		}
		if b.sessionMode == SessionModeOneOnOne && msg.From != b.oneOnOneAgent {
			continue
		}
		if msg.Kind == "automation" || msg.From == "nex" {
			continue
		}
		content := msg.Content
		if len(content) > 80 {
			content = content[:80]
		}
		info := members[msg.From]
		info.lastMessage = content
		info.lastTime = msg.Timestamp
		if info.name == "" {
			if office := b.findMemberLocked(msg.From); office != nil {
				info.name = office.Name
				info.role = office.Role
			}
		}
		members[msg.From] = info
	}
	isOneOnOne := b.sessionMode == SessionModeOneOnOne
	oneOnOneSlug := b.oneOnOneAgent
	taggedAt := make(map[string]time.Time, len(b.lastTaggedAt))
	for slug, ts := range b.lastTaggedAt {
		taggedAt[slug] = ts
	}
	activity := make(map[string]agentActivitySnapshot, len(b.activity))
	for slug, snapshot := range b.activity {
		activity[slug] = snapshot
	}
	b.mu.Unlock()

	type memberEntry struct {
		Slug         string `json:"slug"`
		Name         string `json:"name,omitempty"`
		Role         string `json:"role,omitempty"`
		Disabled     bool   `json:"disabled,omitempty"`
		LastMessage  string `json:"lastMessage"`
		LastTime     string `json:"lastTime"`
		LiveActivity string `json:"liveActivity,omitempty"`
		Status       string `json:"status,omitempty"`
		Activity     string `json:"activity,omitempty"`
		Detail       string `json:"detail,omitempty"`
		TotalMs      int64  `json:"totalMs,omitempty"`
		FirstEventMs int64  `json:"firstEventMs,omitempty"`
		FirstTextMs  int64  `json:"firstTextMs,omitempty"`
		FirstToolMs  int64  `json:"firstToolMs,omitempty"`
	}

	// Capture pane activity via diff detection.
	// If content changed since last poll, agent is active — return last 5 lines.
	var paneActivity map[string]string
	if isOneOnOne && oneOnOneSlug != "" {
		paneActivity = b.capturePaneActivity(oneOnOneSlug)
	} else {
		paneActivity = b.capturePaneActivity("")
	}

	var list []memberEntry
	for slug, info := range members {
		entry := memberEntry{
			Slug:        slug,
			Name:        info.name,
			Role:        info.role,
			Disabled:    info.disabled,
			LastMessage: info.lastMessage,
			LastTime:    info.lastTime,
		}
		if snapshot, ok := activity[slug]; ok {
			entry.Status = snapshot.Status
			entry.Activity = snapshot.Activity
			entry.Detail = snapshot.Detail
			entry.TotalMs = snapshot.TotalMs
			entry.FirstEventMs = snapshot.FirstEventMs
			entry.FirstTextMs = snapshot.FirstTextMs
			entry.FirstToolMs = snapshot.FirstToolMs
			if snapshot.LastTime != "" {
				entry.LastTime = snapshot.LastTime
			}
			if snapshot.Detail != "" {
				entry.LiveActivity = snapshot.Detail
			}
		}
		if live, ok := paneActivity[slug]; ok {
			entry.Status = "active"
			if entry.Activity == "" {
				entry.Activity = "text"
			}
			entry.LiveActivity = live
			entry.Detail = live
			if entry.LastTime == "" {
				entry.LastTime = time.Now().UTC().Format(time.RFC3339)
			}
		}
		// Also mark as active if tagged recently and hasn't replied yet
		if entry.LiveActivity == "" && taggedAt != nil {
			if t, ok := taggedAt[slug]; ok && time.Since(t) < 60*time.Second {
				entry.Status = "active"
				if entry.Activity == "" {
					entry.Activity = "queued"
				}
				entry.LiveActivity = "active"
			}
		}
		if entry.Status == "" {
			entry.Status = "idle"
		}
		if entry.Activity == "" {
			entry.Activity = "idle"
		}
		list = append(list, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"channel": channel, "members": list})
}

func (b *Broker) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetTasks(w, r)
	case http.MethodPost:
		b.handlePostTask(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleAgentLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	b.mu.Lock()
	root := b.agentLogRoot
	b.mu.Unlock()
	if root == "" {
		root = agent.DefaultTaskLogRoot()
	}

	task := strings.TrimSpace(r.URL.Query().Get("task"))
	if task != "" {
		// Guard against path traversal — the task id is a single directory name.
		if strings.Contains(task, "..") || strings.ContainsAny(task, `/\`) {
			http.Error(w, "invalid task id", http.StatusBadRequest)
			return
		}
		entries, err := agent.ReadTaskLog(root, task)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.Error(w, "task not found", http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task":    task,
			"entries": entries,
		})
		return
	}

	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	tasks, err := agent.ListRecentTasks(root, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
}

func (b *Broker) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	mySlug := strings.TrimSpace(r.URL.Query().Get("my_slug"))
	viewerSlug := strings.TrimSpace(r.URL.Query().Get("viewer_slug"))
	channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
	allChannels := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("all_channels")), "true")
	if channel == "" && !allChannels {
		channel = "general"
	}
	includeDone := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_done")), "true")

	b.mu.Lock()
	if !allChannels && !b.canAccessChannelLocked(viewerSlug, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	result := make([]teamTask, 0, len(b.tasks))
	for _, task := range b.tasks {
		if !allChannels && normalizeChannelSlug(task.Channel) != channel {
			continue
		}
		if task.Status == "done" && !includeDone && statusFilter == "" {
			continue
		}
		if statusFilter != "" && task.Status != statusFilter {
			continue
		}
		if mySlug != "" && task.Owner != "" && task.Owner != mySlug {
			continue
		}
		result = append(result, task)
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"channel": channel, "tasks": result})
}

func (b *Broker) handlePostTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action           string   `json:"action"`
		Channel          string   `json:"channel"`
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		Details          string   `json:"details"`
		Owner            string   `json:"owner"`
		CreatedBy        string   `json:"created_by"`
		ThreadID         string   `json:"thread_id"`
		TaskType         string   `json:"task_type"`
		PipelineID       string   `json:"pipeline_id"`
		ExecutionMode    string   `json:"execution_mode"`
		ReviewState      string   `json:"review_state"`
		SourceSignalID   string   `json:"source_signal_id"`
		SourceDecisionID string   `json:"source_decision_id"`
		WorktreePath     string   `json:"worktree_path"`
		WorktreeBranch   string   `json:"worktree_branch"`
		DependsOn        []string `json:"depends_on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	action := strings.TrimSpace(body.Action)
	now := time.Now().UTC().Format(time.RFC3339)
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.findChannelLocked(channel) == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	if !b.canAccessChannelLocked(body.CreatedBy, channel) {
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}

	if action == "create" {
		if strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.CreatedBy) == "" {
			http.Error(w, "title and created_by required", http.StatusBadRequest)
			return
		}
		if existing := b.findReusableTaskLocked(taskReuseMatch{
			Channel:          channel,
			Title:            strings.TrimSpace(body.Title),
			ThreadID:         strings.TrimSpace(body.ThreadID),
			Owner:            strings.TrimSpace(body.Owner),
			PipelineID:       strings.TrimSpace(body.PipelineID),
			SourceSignalID:   strings.TrimSpace(body.SourceSignalID),
			SourceDecisionID: strings.TrimSpace(body.SourceDecisionID),
		}); existing != nil {
			if details := strings.TrimSpace(body.Details); details != "" {
				existing.Details = details
			}
			if owner := strings.TrimSpace(body.Owner); owner != "" {
				existing.Owner = owner
				existing.Status = "in_progress"
			}
			if taskType := strings.TrimSpace(body.TaskType); taskType != "" {
				existing.TaskType = taskType
			}
			if pipelineID := strings.TrimSpace(body.PipelineID); pipelineID != "" {
				existing.PipelineID = pipelineID
			}
			if executionMode := strings.TrimSpace(body.ExecutionMode); executionMode != "" {
				existing.ExecutionMode = executionMode
			}
			if reviewState := strings.TrimSpace(body.ReviewState); reviewState != "" {
				existing.ReviewState = reviewState
			}
			if sourceSignalID := strings.TrimSpace(body.SourceSignalID); sourceSignalID != "" {
				existing.SourceSignalID = sourceSignalID
			}
			if sourceDecisionID := strings.TrimSpace(body.SourceDecisionID); sourceDecisionID != "" {
				existing.SourceDecisionID = sourceDecisionID
			}
			if worktreePath := strings.TrimSpace(body.WorktreePath); worktreePath != "" {
				existing.WorktreePath = worktreePath
			}
			if worktreeBranch := strings.TrimSpace(body.WorktreeBranch); worktreeBranch != "" {
				existing.WorktreeBranch = worktreeBranch
			}
			if existing.ThreadID == "" && strings.TrimSpace(body.ThreadID) != "" {
				existing.ThreadID = strings.TrimSpace(body.ThreadID)
			}
			b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
			existing.UpdatedAt = now
			b.scheduleTaskLifecycleLocked(existing)
			if err := b.syncTaskWorktreeLocked(existing); err != nil {
				http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
				return
			}
			b.appendActionLocked("task_updated", "office", channel, strings.TrimSpace(body.CreatedBy), truncateSummary(existing.Title+" ["+existing.Status+"]", 140), existing.ID)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"task": *existing})
			return
		}
		b.counter++
		task := teamTask{
			ID:               fmt.Sprintf("task-%d", b.counter),
			Channel:          channel,
			Title:            strings.TrimSpace(body.Title),
			Details:          strings.TrimSpace(body.Details),
			Owner:            strings.TrimSpace(body.Owner),
			Status:           "open",
			CreatedBy:        strings.TrimSpace(body.CreatedBy),
			ThreadID:         strings.TrimSpace(body.ThreadID),
			TaskType:         strings.TrimSpace(body.TaskType),
			PipelineID:       strings.TrimSpace(body.PipelineID),
			ExecutionMode:    strings.TrimSpace(body.ExecutionMode),
			ReviewState:      strings.TrimSpace(body.ReviewState),
			SourceSignalID:   strings.TrimSpace(body.SourceSignalID),
			SourceDecisionID: strings.TrimSpace(body.SourceDecisionID),
			WorktreePath:     strings.TrimSpace(body.WorktreePath),
			WorktreeBranch:   strings.TrimSpace(body.WorktreeBranch),
			DependsOn:        body.DependsOn,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(&task) {
			task.Blocked = true
		} else if task.Owner != "" {
			task.Status = "in_progress"
		}
		b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(&task)
		if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		b.scheduleTaskLifecycleLocked(&task)
		if err := b.syncTaskWorktreeLocked(&task); err != nil {
			http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
			return
		}
		b.tasks = append(b.tasks, task)
		b.appendActionLocked("task_created", "office", channel, task.CreatedBy, truncateSummary(task.Title, 140), task.ID)
		if err := b.saveLocked(); err != nil {
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"task": task})
		return
	}

	requestedID := strings.TrimSpace(body.ID)
	for i := range b.tasks {
		if b.tasks[i].ID != requestedID {
			continue
		}
		task := &b.tasks[i]
		taskChannel := normalizeChannelSlug(task.Channel)
		reassignPrevOwner := ""
		reassignTriggered := false
		cancelTriggered := false
		cancelPrevOwner := ""
		switch action {
		case "claim", "assign":
			if strings.TrimSpace(body.Owner) == "" {
				http.Error(w, "owner required", http.StatusBadRequest)
				return
			}
			task.Owner = strings.TrimSpace(body.Owner)
			task.Status = "in_progress"
			if taskNeedsStructuredReview(task) {
				task.ReviewState = "pending_review"
			} else {
				task.ReviewState = "not_required"
			}
		case "reassign":
			if strings.TrimSpace(body.Owner) == "" {
				http.Error(w, "owner required", http.StatusBadRequest)
				return
			}
			reassignPrevOwner = strings.TrimSpace(task.Owner)
			newOwner := strings.TrimSpace(body.Owner)
			task.Owner = newOwner
			status := strings.ToLower(strings.TrimSpace(task.Status))
			if status != "done" && status != "review" {
				task.Status = "in_progress"
			}
			if taskNeedsStructuredReview(task) && strings.TrimSpace(task.ReviewState) == "" {
				task.ReviewState = "pending_review"
			}
			reassignTriggered = reassignPrevOwner != newOwner
		case "complete":
			if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
				if taskNeedsStructuredReview(task) {
					task.ReviewState = "approved"
				}
				task.Blocked = false
			} else if strings.EqualFold(strings.TrimSpace(task.Status), "review") ||
				strings.EqualFold(strings.TrimSpace(task.ReviewState), "ready_for_review") {
				task.Status = "done"
				if taskNeedsStructuredReview(task) {
					task.ReviewState = "approved"
				}
				task.Blocked = false
			} else if taskNeedsStructuredReview(task) {
				task.Status = "review"
				task.ReviewState = "ready_for_review"
			} else {
				task.Status = "done"
			}
		case "review":
			task.Status = "review"
			task.ReviewState = "ready_for_review"
		case "approve":
			task.Status = "done"
			if taskNeedsStructuredReview(task) {
				task.ReviewState = "approved"
			}
		case "block":
			if err := rejectFalseLocalWorktreeBlock(task, body.Details); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			task.Status = "blocked"
			task.Blocked = true
		case "release":
			task.Owner = ""
			task.Status = "open"
			task.Blocked = false
		case "cancel":
			cancelPrevOwner = strings.TrimSpace(task.Owner)
			task.Status = "canceled"
			task.Blocked = false
			task.FollowUpAt = ""
			task.ReminderAt = ""
			task.RecheckAt = ""
			cancelTriggered = true
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Details) != "" {
			task.Details = strings.TrimSpace(body.Details)
		}
		if taskType := strings.TrimSpace(body.TaskType); taskType != "" {
			task.TaskType = taskType
		}
		if pipelineID := strings.TrimSpace(body.PipelineID); pipelineID != "" {
			task.PipelineID = pipelineID
		}
		if executionMode := strings.TrimSpace(body.ExecutionMode); executionMode != "" {
			task.ExecutionMode = executionMode
		}
		if reviewState := strings.TrimSpace(body.ReviewState); reviewState != "" {
			task.ReviewState = reviewState
		}
		if sourceSignalID := strings.TrimSpace(body.SourceSignalID); sourceSignalID != "" {
			task.SourceSignalID = sourceSignalID
		}
		if sourceDecisionID := strings.TrimSpace(body.SourceDecisionID); sourceDecisionID != "" {
			task.SourceDecisionID = sourceDecisionID
		}
		if worktreePath := strings.TrimSpace(body.WorktreePath); worktreePath != "" {
			task.WorktreePath = worktreePath
		}
		if worktreeBranch := strings.TrimSpace(body.WorktreeBranch); worktreeBranch != "" {
			task.WorktreeBranch = worktreeBranch
		}
		b.ensureTaskOwnerChannelMembershipLocked(taskChannel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(task)
		task.UpdatedAt = now
		if err := rejectTheaterTaskForLiveBusiness(task); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if task.Status == "done" {
			b.unblockDependentsLocked(task.ID)
		}
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
			return
		}
		b.appendActionLocked("task_updated", "office", taskChannel, strings.TrimSpace(body.CreatedBy), truncateSummary(task.Title+" ["+task.Status+"]", 140), task.ID)
		if reassignTriggered {
			b.postTaskReassignNotificationsLocked(strings.TrimSpace(body.CreatedBy), task, reassignPrevOwner)
		}
		if cancelTriggered {
			b.postTaskCancelNotificationsLocked(strings.TrimSpace(body.CreatedBy), task, cancelPrevOwner)
		}
		if err := b.saveLocked(); err != nil {
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"task": *task})
		return
	}

	http.Error(w, "task not found", http.StatusNotFound)
}

// postTaskReassignNotificationsLocked posts the channel announcement plus DMs
// to the new owner and previous owner whenever a task ownership change happens.
// The CEO is tagged in the channel message rather than DM'd (CEO is the human
// user; human↔ceo self-DM is not a valid DM target).
//
// Must be called while b.mu is held for write.
func (b *Broker) postTaskReassignNotificationsLocked(actor string, task *teamTask, prevOwner string) {
	if task == nil {
		return
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	newOwner := strings.TrimSpace(task.Owner)
	prevOwner = strings.TrimSpace(prevOwner)
	if newOwner == prevOwner {
		return
	}
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	now := time.Now().UTC().Format(time.RFC3339)

	newLabel := "(unassigned)"
	if newOwner != "" {
		newLabel = "@" + newOwner
	}
	prevLabel := "(unassigned)"
	if prevOwner != "" {
		prevLabel = "@" + prevOwner
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      actor,
		Channel:   taskChannel,
		Kind:      "task_reassigned",
		Title:     title,
		Content:   fmt.Sprintf("Task %q reassigned: %s → %s. (by @%s, cc @ceo)", title, prevLabel, newLabel, actor),
		Tagged:    dedupeReassignTags([]string{"ceo", newOwner, prevOwner}),
		Timestamp: now,
	})

	if isDMTargetSlug(newOwner) {
		b.postTaskDMLocked(actor, newOwner, "task_reassigned", title,
			fmt.Sprintf("Task %q is yours now. Details live in #%s.", title, taskChannel))
	}
	if isDMTargetSlug(prevOwner) && prevOwner != newOwner {
		b.postTaskDMLocked(actor, prevOwner, "task_reassigned", title,
			fmt.Sprintf("Task %q is off your plate — it moved to %s.", title, newLabel))
	}
}

// postTaskCancelNotificationsLocked posts a channel announcement plus a DM
// to the (previous) owner whenever a task is closed as "won't do".
// Must be called while b.mu is held for write.
func (b *Broker) postTaskCancelNotificationsLocked(actor string, task *teamTask, prevOwner string) {
	if task == nil {
		return
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	prevOwner = strings.TrimSpace(prevOwner)
	taskChannel := normalizeChannelSlug(task.Channel)
	if taskChannel == "" {
		taskChannel = "general"
	}
	title := strings.TrimSpace(task.Title)
	if title == "" {
		title = task.ID
	}
	now := time.Now().UTC().Format(time.RFC3339)

	ownerLabel := "(no owner)"
	if prevOwner != "" {
		ownerLabel = "@" + prevOwner
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      actor,
		Channel:   taskChannel,
		Kind:      "task_canceled",
		Title:     title,
		Content:   fmt.Sprintf("Task %q closed as won't do. Owner was %s. (by @%s, cc @ceo)", title, ownerLabel, actor),
		Tagged:    dedupeReassignTags([]string{"ceo", prevOwner}),
		Timestamp: now,
	})

	if isDMTargetSlug(prevOwner) {
		b.postTaskDMLocked(actor, prevOwner, "task_canceled", title,
			fmt.Sprintf("Heads up — task %q was closed as won't do. Take it off your list.", title))
	}
}

// postTaskDMLocked appends a direct-message notification to the DM channel
// between "human" and targetSlug, creating the channel if necessary.
// Must be called while b.mu is held for write.
func (b *Broker) postTaskDMLocked(from, targetSlug, kind, title, content string) {
	targetSlug = strings.TrimSpace(targetSlug)
	if targetSlug == "" || b.channelStore == nil {
		return
	}
	ch, err := b.channelStore.GetOrCreateDirect("human", targetSlug)
	if err != nil {
		return
	}
	if b.findChannelLocked(ch.Slug) == nil {
		now := time.Now().UTC().Format(time.RFC3339)
		b.channels = append(b.channels, teamChannel{
			Slug:        ch.Slug,
			Name:        ch.Slug,
			Type:        "dm",
			Description: "Direct messages with " + targetSlug,
			Members:     []string{"human", targetSlug},
			CreatedBy:   "wuphf",
			CreatedAt:   now,
			UpdatedAt:   now,
		})
	}
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      strings.TrimSpace(from),
		Channel:   ch.Slug,
		Kind:      strings.TrimSpace(kind),
		Title:     title,
		Content:   content,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
}

// isDMTargetSlug reports whether slug is a valid recipient for a human-to-agent DM.
// The human user ("human"/"you") and the CEO seat ("ceo", which is the human)
// are excluded because they would create self-DMs.
func isDMTargetSlug(slug string) bool {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return false
	}
	switch slug {
	case "human", "you", "ceo":
		return false
	}
	return true
}

func dedupeReassignTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

func (b *Broker) BlockTask(taskID, actor, reason string) (teamTask, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := strings.TrimSpace(taskID)
	if id == "" {
		return teamTask{}, false, fmt.Errorf("task id required")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	reason = strings.TrimSpace(reason)
	now := time.Now().UTC().Format(time.RFC3339)

	for i := range b.tasks {
		task := &b.tasks[i]
		if task.ID != id {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(task.Status))
		if status == "done" || status == "completed" || status == "canceled" || status == "cancelled" {
			return *task, false, nil
		}
		if err := rejectFalseLocalWorktreeBlock(task, reason); err != nil {
			return *task, false, err
		}
		if reason != "" {
			switch existing := strings.TrimSpace(task.Details); {
			case existing == "":
				task.Details = reason
			case !strings.Contains(existing, reason):
				task.Details = existing + "\n\n" + reason
			}
		}
		task.Status = "blocked"
		task.Blocked = true
		task.UpdatedAt = now
		if err := rejectTheaterTaskForLiveBusiness(task); err != nil {
			return *task, false, err
		}
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			return teamTask{}, false, err
		}
		b.appendActionLocked("task_updated", "office", normalizeChannelSlug(task.Channel), actor, truncateSummary(task.Title+" ["+task.Status+"]", 140), task.ID)
		if err := b.saveLocked(); err != nil {
			return teamTask{}, false, err
		}
		return *task, true, nil
	}

	return teamTask{}, false, fmt.Errorf("task not found")
}

func (b *Broker) ResumeTask(taskID, actor, reason string) (teamTask, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	id := strings.TrimSpace(taskID)
	if id == "" {
		return teamTask{}, false, fmt.Errorf("task id required")
	}
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = "system"
	}
	reason = strings.TrimSpace(reason)
	now := time.Now().UTC().Format(time.RFC3339)

	for i := range b.tasks {
		task := &b.tasks[i]
		if task.ID != id {
			continue
		}
		changed := false
		if task.Blocked {
			task.Blocked = false
			changed = true
		}
		if strings.EqualFold(strings.TrimSpace(task.Status), "blocked") {
			if strings.TrimSpace(task.Owner) != "" {
				task.Status = "in_progress"
			} else {
				task.Status = "open"
			}
			changed = true
		}
		if !changed {
			return *task, false, nil
		}
		if reason != "" && !strings.Contains(task.Details, reason) {
			task.Details = strings.TrimSpace(task.Details)
			if task.Details != "" {
				task.Details += "\n\n"
			}
			task.Details += reason
		}
		b.ensureTaskOwnerChannelMembershipLocked(task.Channel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(task)
		task.UpdatedAt = now
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			return teamTask{}, false, err
		}
		b.appendActionLocked("task_unblocked", "office", normalizeChannelSlug(task.Channel), actor, truncateSummary(task.Title+" resumed", 140), task.ID)
		if err := b.saveLocked(); err != nil {
			return teamTask{}, false, err
		}
		return *task, true, nil
	}

	return teamTask{}, false, fmt.Errorf("task not found")
}

func (b *Broker) handleTaskPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Channel   string `json:"channel"`
		CreatedBy string `json:"created_by"`
		Tasks     []struct {
			Title         string   `json:"title"`
			Assignee      string   `json:"assignee"`
			Details       string   `json:"details"`
			TaskType      string   `json:"task_type"`
			ExecutionMode string   `json:"execution_mode"`
			DependsOn     []string `json:"depends_on"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	createdBy := strings.TrimSpace(body.CreatedBy)
	if createdBy == "" || len(body.Tasks) == 0 {
		http.Error(w, "created_by and tasks required", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.findChannelLocked(channel) == nil {
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}

	// Map title → task ID for resolving depends_on by title
	titleToID := map[string]string{}
	now := time.Now().UTC().Format(time.RFC3339)
	created := make([]teamTask, 0, len(body.Tasks))

	for _, item := range body.Tasks {
		taskChannel := b.preferredTaskChannelLocked(channel, createdBy, item.Assignee, item.Title, item.Details)
		if b.findChannelLocked(taskChannel) == nil {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}

		// Resolve depends_on: accept both task IDs and titles
		resolvedDeps := make([]string, 0, len(item.DependsOn))
		for _, dep := range item.DependsOn {
			dep = strings.TrimSpace(dep)
			if id, ok := titleToID[dep]; ok {
				resolvedDeps = append(resolvedDeps, id)
			} else {
				resolvedDeps = append(resolvedDeps, dep) // assume it's a task ID
			}
		}
		if existing := b.findReusableTaskLocked(taskReuseMatch{
			Channel: taskChannel,
			Title:   strings.TrimSpace(item.Title),
			Owner:   strings.TrimSpace(item.Assignee),
		}); existing != nil {
			titleToID[strings.TrimSpace(item.Title)] = existing.ID
			if details := strings.TrimSpace(item.Details); details != "" {
				existing.Details = details
			}
			if taskType := strings.TrimSpace(item.TaskType); taskType != "" {
				existing.TaskType = taskType
			}
			if executionMode := strings.TrimSpace(item.ExecutionMode); executionMode != "" {
				existing.ExecutionMode = executionMode
			}
			existing.DependsOn = resolvedDeps
			if len(existing.DependsOn) > 0 && b.hasUnresolvedDepsLocked(existing) {
				existing.Blocked = true
				existing.Status = "open"
			} else if strings.TrimSpace(existing.Owner) != "" {
				existing.Blocked = false
				existing.Status = "in_progress"
			}
			b.ensureTaskOwnerChannelMembershipLocked(taskChannel, existing.Owner)
			b.queueTaskBehindActiveOwnerLaneLocked(existing)
			existing.UpdatedAt = now
			if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			b.scheduleTaskLifecycleLocked(existing)
			if err := b.syncTaskWorktreeLocked(existing); err != nil {
				http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
				return
			}
			b.appendActionLocked("task_updated", "office", taskChannel, createdBy, truncateSummary(existing.Title+" ["+existing.Status+"]", 140), existing.ID)
			created = append(created, *existing)
			continue
		}

		b.counter++
		taskID := fmt.Sprintf("task-%d", b.counter)
		titleToID[strings.TrimSpace(item.Title)] = taskID

		task := teamTask{
			ID:            taskID,
			Channel:       taskChannel,
			Title:         strings.TrimSpace(item.Title),
			Details:       strings.TrimSpace(item.Details),
			Owner:         strings.TrimSpace(item.Assignee),
			Status:        "open",
			CreatedBy:     createdBy,
			TaskType:      strings.TrimSpace(item.TaskType),
			ExecutionMode: strings.TrimSpace(item.ExecutionMode),
			DependsOn:     resolvedDeps,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if task.Owner != "" && len(resolvedDeps) == 0 {
			task.Status = "in_progress"
		}
		if len(resolvedDeps) > 0 && b.hasUnresolvedDepsLocked(&task) {
			task.Blocked = true
		}
		b.ensureTaskOwnerChannelMembershipLocked(taskChannel, task.Owner)
		b.queueTaskBehindActiveOwnerLaneLocked(&task)
		if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		b.scheduleTaskLifecycleLocked(&task)
		if err := b.syncTaskWorktreeLocked(&task); err != nil {
			http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
			return
		}
		b.tasks = append(b.tasks, task)
		b.appendActionLocked("task_created", "office", taskChannel, createdBy, truncateSummary(task.Title, 140), task.ID)
		created = append(created, task)
	}

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": created})
}

func (b *Broker) handleMemory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
		query := strings.TrimSpace(r.URL.Query().Get("query"))
		keyFilter := strings.TrimSpace(r.URL.Query().Get("key"))
		limit := 5
		if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				limit = parsed
			}
		}
		b.mu.Lock()
		mem := b.sharedMemory
		b.mu.Unlock()
		if mem == nil {
			mem = make(map[string]map[string]string)
		}
		w.Header().Set("Content-Type", "application/json")
		if namespace != "" {
			entries := mem[namespace]
			switch {
			case keyFilter != "":
				var payload []brokerMemoryEntry
				if raw, ok := entries[keyFilter]; ok {
					payload = append(payload, brokerEntryFromNote(decodePrivateMemoryNote(keyFilter, raw)))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"namespace": namespace,
					"entries":   payload,
				})
				return
			case query != "":
				matches := searchPrivateMemory(entries, query, limit)
				payload := make([]brokerMemoryEntry, 0, len(matches))
				for _, note := range matches {
					payload = append(payload, brokerEntryFromNote(note))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"namespace": namespace,
					"entries":   payload,
				})
				return
			default:
				matches := searchPrivateMemory(entries, "", len(entries))
				payload := make([]brokerMemoryEntry, 0, len(matches))
				for _, note := range matches {
					payload = append(payload, brokerEntryFromNote(note))
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"namespace": namespace,
					"entries":   payload,
				})
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"memory": mem})
	case http.MethodPost:
		var body struct {
			Namespace string `json:"namespace"`
			Key       string `json:"key"`
			Value     any    `json:"value"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		ns := strings.TrimSpace(body.Namespace)
		key := strings.TrimSpace(body.Key)
		if ns == "" || key == "" {
			http.Error(w, "namespace and key required", http.StatusBadRequest)
			return
		}
		b.mu.Lock()
		if b.sharedMemory == nil {
			b.sharedMemory = make(map[string]map[string]string)
		}
		if b.sharedMemory[ns] == nil {
			b.sharedMemory[ns] = make(map[string]string)
		}
		value := ""
		switch typed := body.Value.(type) {
		case string:
			value = typed
		default:
			data, err := json.Marshal(typed)
			if err != nil {
				b.mu.Unlock()
				http.Error(w, "invalid value", http.StatusBadRequest)
				return
			}
			value = string(data)
		}
		b.sharedMemory[ns][key] = value
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "namespace": ns, "key": key})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleTaskAck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID      string `json:"id"`
		Channel string `json:"channel"`
		Slug    string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	taskID := strings.TrimSpace(body.ID)
	slug := strings.TrimSpace(body.Slug)
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	if taskID == "" || slug == "" {
		http.Error(w, "id and slug required", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == taskID && normalizeChannelSlug(b.tasks[i].Channel) == channel {
			if b.tasks[i].Owner != slug {
				http.Error(w, "only the task owner can ack", http.StatusForbidden)
				return
			}
			now := time.Now().UTC().Format(time.RFC3339)
			b.tasks[i].AckedAt = now
			b.tasks[i].UpdatedAt = now
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"task": b.tasks[i]})
			return
		}
	}
	http.Error(w, "task not found", http.StatusNotFound)
}

func (b *Broker) EnsureTask(channel, title, details, owner, createdBy, threadID string, dependsOn ...string) (teamTask, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = b.preferredTaskChannelLocked(channel, createdBy, owner, title, details)
	if b.findChannelLocked(channel) == nil {
		return teamTask{}, false, fmt.Errorf("channel not found")
	}
	if !b.canAccessChannelLocked(createdBy, channel) {
		return teamTask{}, false, fmt.Errorf("channel access denied")
	}
	title = strings.TrimSpace(title)
	if existing := b.findReusableTaskLocked(taskReuseMatch{
		Channel:  channel,
		Title:    title,
		ThreadID: strings.TrimSpace(threadID),
		Owner:    strings.TrimSpace(owner),
	}); existing != nil {
		if existing.Details == "" && strings.TrimSpace(details) != "" {
			existing.Details = strings.TrimSpace(details)
		}
		if existing.Owner == "" && strings.TrimSpace(owner) != "" {
			existing.Owner = strings.TrimSpace(owner)
			if !existing.Blocked {
				existing.Status = "in_progress"
			}
		}
		if existing.ThreadID == "" && strings.TrimSpace(threadID) != "" {
			existing.ThreadID = strings.TrimSpace(threadID)
		}
		b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		b.queueTaskBehindActiveOwnerLaneLocked(existing)
		if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
			return teamTask{}, false, err
		}
		b.scheduleTaskLifecycleLocked(existing)
		if err := b.syncTaskWorktreeLocked(existing); err != nil {
			return teamTask{}, false, err
		}
		if err := b.saveLocked(); err != nil {
			return teamTask{}, false, err
		}
		return *existing, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	task := teamTask{
		ID:        fmt.Sprintf("task-%d", b.counter),
		Channel:   channel,
		Title:     title,
		Details:   strings.TrimSpace(details),
		Owner:     strings.TrimSpace(owner),
		Status:    "open",
		CreatedBy: strings.TrimSpace(createdBy),
		ThreadID:  strings.TrimSpace(threadID),
		DependsOn: dependsOn,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(&task) {
		task.Blocked = true
	} else if task.Owner != "" {
		task.Status = "in_progress"
	}
	b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
	b.queueTaskBehindActiveOwnerLaneLocked(&task)
	if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
		return teamTask{}, false, err
	}
	b.scheduleTaskLifecycleLocked(&task)
	if err := b.syncTaskWorktreeLocked(&task); err != nil {
		return teamTask{}, false, err
	}
	b.tasks = append(b.tasks, task)
	b.appendActionLocked("task_created", "office", channel, createdBy, truncateSummary(task.Title, 140), task.ID)
	if err := b.saveLocked(); err != nil {
		return teamTask{}, false, err
	}
	return task, false, nil
}

type plannedTaskInput struct {
	Channel          string
	Title            string
	Details          string
	Owner            string
	CreatedBy        string
	ThreadID         string
	TaskType         string
	PipelineID       string
	ExecutionMode    string
	ReviewState      string
	SourceSignalID   string
	SourceDecisionID string
	DependsOn        []string
}

func (b *Broker) EnsurePlannedTask(input plannedTaskInput) (teamTask, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel := b.preferredTaskChannelLocked(input.Channel, input.CreatedBy, input.Owner, input.Title, input.Details)
	if b.findChannelLocked(channel) == nil {
		return teamTask{}, false, fmt.Errorf("channel not found")
	}
	if !b.canAccessChannelLocked(input.CreatedBy, channel) {
		return teamTask{}, false, fmt.Errorf("channel access denied")
	}
	title := strings.TrimSpace(input.Title)
	if existing := b.findReusableTaskLocked(taskReuseMatch{
		Channel:          channel,
		Title:            title,
		ThreadID:         strings.TrimSpace(input.ThreadID),
		Owner:            strings.TrimSpace(input.Owner),
		PipelineID:       strings.TrimSpace(input.PipelineID),
		SourceSignalID:   strings.TrimSpace(input.SourceSignalID),
		SourceDecisionID: strings.TrimSpace(input.SourceDecisionID),
	}); existing != nil {
		if existing.Details == "" && strings.TrimSpace(input.Details) != "" {
			existing.Details = strings.TrimSpace(input.Details)
		}
		if existing.Owner == "" && strings.TrimSpace(input.Owner) != "" {
			existing.Owner = strings.TrimSpace(input.Owner)
			existing.Status = "in_progress"
		}
		if existing.ThreadID == "" && strings.TrimSpace(input.ThreadID) != "" {
			existing.ThreadID = strings.TrimSpace(input.ThreadID)
		}
		if existing.TaskType == "" && strings.TrimSpace(input.TaskType) != "" {
			existing.TaskType = strings.TrimSpace(input.TaskType)
		}
		if existing.PipelineID == "" && strings.TrimSpace(input.PipelineID) != "" {
			existing.PipelineID = strings.TrimSpace(input.PipelineID)
		}
		if existing.ExecutionMode == "" && strings.TrimSpace(input.ExecutionMode) != "" {
			existing.ExecutionMode = strings.TrimSpace(input.ExecutionMode)
		}
		if existing.ReviewState == "" && strings.TrimSpace(input.ReviewState) != "" {
			existing.ReviewState = strings.TrimSpace(input.ReviewState)
		}
		if existing.SourceSignalID == "" && strings.TrimSpace(input.SourceSignalID) != "" {
			existing.SourceSignalID = strings.TrimSpace(input.SourceSignalID)
		}
		if existing.SourceDecisionID == "" && strings.TrimSpace(input.SourceDecisionID) != "" {
			existing.SourceDecisionID = strings.TrimSpace(input.SourceDecisionID)
		}
		b.ensureTaskOwnerChannelMembershipLocked(channel, existing.Owner)
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		b.queueTaskBehindActiveOwnerLaneLocked(existing)
		if err := rejectTheaterTaskForLiveBusiness(existing); err != nil {
			return teamTask{}, false, err
		}
		b.scheduleTaskLifecycleLocked(existing)
		if err := b.syncTaskWorktreeLocked(existing); err != nil {
			return teamTask{}, false, err
		}
		if err := b.saveLocked(); err != nil {
			return teamTask{}, false, err
		}
		return *existing, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	task := teamTask{
		ID:               fmt.Sprintf("task-%d", b.counter),
		Channel:          channel,
		Title:            title,
		Details:          strings.TrimSpace(input.Details),
		Owner:            strings.TrimSpace(input.Owner),
		Status:           "open",
		CreatedBy:        strings.TrimSpace(input.CreatedBy),
		ThreadID:         strings.TrimSpace(input.ThreadID),
		TaskType:         strings.TrimSpace(input.TaskType),
		PipelineID:       strings.TrimSpace(input.PipelineID),
		ExecutionMode:    strings.TrimSpace(input.ExecutionMode),
		ReviewState:      strings.TrimSpace(input.ReviewState),
		SourceSignalID:   strings.TrimSpace(input.SourceSignalID),
		SourceDecisionID: strings.TrimSpace(input.SourceDecisionID),
		DependsOn:        input.DependsOn,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if len(task.DependsOn) > 0 && b.hasUnresolvedDepsLocked(&task) {
		task.Blocked = true
	} else if task.Owner != "" {
		task.Status = "in_progress"
	}
	b.ensureTaskOwnerChannelMembershipLocked(channel, task.Owner)
	b.queueTaskBehindActiveOwnerLaneLocked(&task)
	if err := rejectTheaterTaskForLiveBusiness(&task); err != nil {
		return teamTask{}, false, err
	}
	b.scheduleTaskLifecycleLocked(&task)
	if err := b.syncTaskWorktreeLocked(&task); err != nil {
		return teamTask{}, false, err
	}
	b.tasks = append(b.tasks, task)
	b.appendActionWithRefsLocked("task_created", "office", channel, input.CreatedBy, truncateSummary(task.Title, 140), task.ID, compactStringList([]string{task.SourceSignalID}), task.SourceDecisionID)
	if err := b.saveLocked(); err != nil {
		return teamTask{}, false, err
	}
	return task, false, nil
}

// hasUnresolvedDepsLocked returns true if any of the task's dependencies are not done.
func (b *Broker) hasUnresolvedDepsLocked(task *teamTask) bool {
	for _, depID := range task.DependsOn {
		if requestIsResolvedLocked(b.requests, depID) {
			continue
		}
		found := false
		for j := range b.tasks {
			if b.tasks[j].ID == depID {
				found = true
				if b.tasks[j].Status != "done" {
					return true
				}
				break
			}
		}
		if !found {
			return true // dependency doesn't exist yet — treat as unresolved
		}
	}
	return false
}

// unblockDependentsLocked checks all blocked tasks and unblocks those whose
// dependencies are now resolved. For each newly unblocked task, it appends a
// "task_unblocked" action so the launcher can deliver a notification to the owner.
func (b *Broker) unblockDependentsLocked(completedTaskID string) {
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range b.tasks {
		if !b.tasks[i].Blocked {
			continue
		}
		hasDep := false
		for _, depID := range b.tasks[i].DependsOn {
			if depID == completedTaskID {
				hasDep = true
				break
			}
		}
		if !hasDep {
			continue
		}
		if !b.hasUnresolvedDepsLocked(&b.tasks[i]) {
			b.tasks[i].Blocked = false
			if strings.TrimSpace(b.tasks[i].Owner) != "" {
				b.tasks[i].Status = "in_progress"
			} else {
				b.tasks[i].Status = "open"
			}
			b.queueTaskBehindActiveOwnerLaneLocked(&b.tasks[i])
			b.tasks[i].UpdatedAt = now
			b.scheduleTaskLifecycleLocked(&b.tasks[i])
			_ = b.syncTaskWorktreeLocked(&b.tasks[i])
			b.appendActionLocked(
				"task_unblocked",
				"office",
				normalizeChannelSlug(b.tasks[i].Channel),
				"system",
				truncateSummary(b.tasks[i].Title+" unblocked by "+completedTaskID, 140),
				b.tasks[i].ID,
			)
		}
	}
}

type taskReuseMatch struct {
	Channel          string
	Title            string
	ThreadID         string
	Owner            string
	PipelineID       string
	SourceSignalID   string
	SourceDecisionID string
}

func (m taskReuseMatch) hasScopedIdentity() bool {
	return strings.TrimSpace(m.SourceSignalID) != "" ||
		strings.TrimSpace(m.SourceDecisionID) != ""
}

func hasScopedTaskIdentity(task *teamTask) bool {
	if task == nil {
		return false
	}
	return strings.TrimSpace(task.SourceSignalID) != "" ||
		strings.TrimSpace(task.SourceDecisionID) != ""
}

func taskOwnerMatches(task *teamTask, owner string) bool {
	if task == nil {
		return false
	}
	taskOwner := strings.TrimSpace(task.Owner)
	return owner == "" || taskOwner == owner || taskOwner == ""
}

func scopedTaskIdentityMatches(task *teamTask, match taskReuseMatch) bool {
	if task == nil {
		return false
	}
	if match.PipelineID != "" && strings.TrimSpace(task.PipelineID) != "" && strings.TrimSpace(task.PipelineID) != match.PipelineID {
		return false
	}
	if match.SourceSignalID != "" && strings.TrimSpace(task.SourceSignalID) != match.SourceSignalID {
		return false
	}
	if match.SourceDecisionID != "" && strings.TrimSpace(task.SourceDecisionID) != match.SourceDecisionID {
		return false
	}
	return true
}

func (b *Broker) findReusableTaskLocked(match taskReuseMatch) *teamTask {
	channel := normalizeChannelSlug(match.Channel)
	title := strings.TrimSpace(match.Title)
	threadID := strings.TrimSpace(match.ThreadID)
	owner := strings.TrimSpace(match.Owner)
	scopedIdentity := match.hasScopedIdentity()
	for i := range b.tasks {
		task := &b.tasks[i]
		if normalizeChannelSlug(task.Channel) != channel {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			continue
		}
		sameTitle := title != "" && strings.EqualFold(strings.TrimSpace(task.Title), title)
		if threadID != "" && strings.TrimSpace(task.ThreadID) == threadID {
			if sameTitle && taskOwnerMatches(task, owner) {
				taskHasScopedIdentity := hasScopedTaskIdentity(task)
				if scopedIdentity || taskHasScopedIdentity {
					if !scopedIdentity || !taskHasScopedIdentity {
						continue
					}
					if scopedTaskIdentityMatches(task, match) {
						return task
					}
					continue
				}
				return task
			}
			continue
		}
		if !sameTitle || !taskOwnerMatches(task, owner) {
			continue
		}
		taskHasScopedIdentity := hasScopedTaskIdentity(task)
		if scopedIdentity || taskHasScopedIdentity {
			if !scopedIdentity || !taskHasScopedIdentity {
				continue
			}
			if scopedTaskIdentityMatches(task, match) {
				return task
			}
			continue
		}
		return task
	}
	return nil
}

func (b *Broker) handleRequests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetRequests(w, r)
	case http.MethodPost:
		b.handlePostRequest(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleGetRequests(w http.ResponseWriter, r *http.Request) {
	channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	// scope=all returns requests across every channel the viewer can access. The
	// broker's blocking check (handlePostMessage, PostMessage) is global, so the
	// web UI's overlay/interview bar need the same cross-channel view to render
	// what's actually blocking the human.
	allChannels := scope == "all" || scope == "global"
	if !allChannels && channel == "" {
		channel = "general"
	}
	viewerSlug := strings.TrimSpace(r.URL.Query().Get("viewer_slug"))
	includeResolved := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_resolved")), "true")
	b.mu.Lock()
	if !allChannels && !b.canAccessChannelLocked(viewerSlug, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	requests := make([]humanInterview, 0, len(b.requests))
	for _, req := range b.requests {
		reqChannel := normalizeChannelSlug(req.Channel)
		if reqChannel == "" {
			reqChannel = "general"
		}
		if allChannels {
			if !b.canAccessChannelLocked(viewerSlug, reqChannel) {
				continue
			}
		} else if reqChannel != channel {
			continue
		}
		if !includeResolved && !requestIsActive(req) {
			continue
		}
		requests = append(requests, req)
	}
	pending := firstBlockingRequest(requests)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"channel":  channel,
		"scope":    scope,
		"requests": requests,
		"pending":  pending,
	})
}

func (b *Broker) handlePostRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action        string            `json:"action"`
		ID            string            `json:"id"`
		Kind          string            `json:"kind"`
		From          string            `json:"from"`
		Channel       string            `json:"channel"`
		Title         string            `json:"title"`
		Question      string            `json:"question"`
		Context       string            `json:"context"`
		Options       []interviewOption `json:"options"`
		RecommendedID string            `json:"recommended_id"`
		Blocking      bool              `json:"blocking"`
		Required      bool              `json:"required"`
		Secret        bool              `json:"secret"`
		ReplyTo       string            `json:"reply_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	action := strings.TrimSpace(body.Action)
	if action == "" {
		action = "create"
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	switch action {
	case "create":
		if strings.TrimSpace(body.From) == "" || strings.TrimSpace(body.Question) == "" {
			http.Error(w, "from and question required", http.StatusBadRequest)
			return
		}
		channel := normalizeChannelSlug(body.Channel)
		if channel == "" {
			channel = "general"
		}
		if b.findChannelLocked(channel) == nil {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		if !b.canAccessChannelLocked(body.From, channel) {
			http.Error(w, "channel access denied", http.StatusForbidden)
			return
		}
		b.counter++
		req := humanInterview{
			ID:            fmt.Sprintf("request-%d", b.counter),
			Kind:          normalizeRequestKind(body.Kind),
			Status:        "pending",
			From:          strings.TrimSpace(body.From),
			Channel:       channel,
			Title:         strings.TrimSpace(body.Title),
			Question:      strings.TrimSpace(body.Question),
			Context:       strings.TrimSpace(body.Context),
			Options:       body.Options,
			RecommendedID: "",
			Blocking:      body.Blocking,
			Required:      body.Required,
			Secret:        body.Secret,
			ReplyTo:       strings.TrimSpace(body.ReplyTo),
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		}
		req.Options, req.RecommendedID = normalizeRequestOptions(req.Kind, strings.TrimSpace(body.RecommendedID), req.Options)
		if requestNeedsHumanDecision(req) {
			req.Blocking = true
			req.Required = true
		}
		if req.Title == "" {
			req.Title = "Request"
		}
		b.scheduleRequestLifecycleLocked(&req)
		b.requests = append(b.requests, req)
		b.pendingInterview = firstBlockingRequest(b.requests)
		b.appendActionLocked("request_created", "office", channel, req.From, truncateSummary(req.Title+" "+req.Question, 140), req.ID)
		if err := b.saveLocked(); err != nil {
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"request": req, "id": req.ID})
	case "cancel":
		id := strings.TrimSpace(body.ID)
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		for i := range b.requests {
			if b.requests[i].ID != id {
				continue
			}
			b.requests[i].Status = "canceled"
			b.requests[i].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
			b.requests[i].ReminderAt = ""
			b.requests[i].FollowUpAt = ""
			b.requests[i].RecheckAt = ""
			b.requests[i].DueAt = ""
			b.completeSchedulerJobsLocked("request", b.requests[i].ID, b.requests[i].Channel)
			b.pendingInterview = firstBlockingRequest(b.requests)
			b.appendActionLocked("request_canceled", "office", b.requests[i].Channel, b.requests[i].From, truncateSummary(b.requests[i].Title+" "+b.requests[i].Question, 140), b.requests[i].ID)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"request": b.requests[i]})
			return
		}
		http.Error(w, "request not found", http.StatusNotFound)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

func (b *Broker) handleRequestAnswer(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetRequestAnswer(w, r)
	case http.MethodPost:
		b.handlePostRequestAnswer(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleGetRequestAnswer(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	for _, req := range b.requests {
		if req.ID == id && req.Answered != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"answered": req.Answered})
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"answered": nil})
}

func (b *Broker) handlePostRequestAnswer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID         string `json:"id"`
		ChoiceID   string `json:"choice_id"`
		ChoiceText string `json:"choice_text"`
		CustomText string `json:"custom_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	for i := range b.requests {
		if b.requests[i].ID != body.ID {
			continue
		}
		choiceID := strings.TrimSpace(body.ChoiceID)
		choiceText := strings.TrimSpace(body.ChoiceText)
		customText := strings.TrimSpace(body.CustomText)
		option := findRequestOption(b.requests[i], choiceID)
		if choiceID != "" && option == nil {
			b.mu.Unlock()
			http.Error(w, "unknown request option", http.StatusBadRequest)
			return
		}
		if option != nil {
			if choiceText == "" {
				choiceText = strings.TrimSpace(option.Label)
			}
			if option.RequiresText && customText == "" {
				hint := strings.TrimSpace(option.TextHint)
				if hint == "" {
					hint = "custom_text required for this response"
				}
				b.mu.Unlock()
				http.Error(w, hint, http.StatusBadRequest)
				return
			}
		}
		if choiceID == "" && choiceText == "" && customText == "" {
			b.mu.Unlock()
			http.Error(w, "choice_text or custom_text required", http.StatusBadRequest)
			return
		}
		answer := &interviewAnswer{
			ChoiceID:   choiceID,
			ChoiceText: choiceText,
			CustomText: customText,
			AnsweredAt: time.Now().UTC().Format(time.RFC3339),
		}
		b.requests[i].Answered = answer
		b.requests[i].Status = "answered"
		b.requests[i].UpdatedAt = answer.AnsweredAt
		b.requests[i].ReminderAt = ""
		b.requests[i].FollowUpAt = ""
		b.requests[i].RecheckAt = ""
		b.requests[i].DueAt = ""
		b.completeSchedulerJobsLocked("request", b.requests[i].ID, b.requests[i].Channel)
		b.unblockDependentsLocked(b.requests[i].ID)
		b.pendingInterview = firstBlockingRequest(b.requests)
		b.unblockTasksForAnsweredRequestLocked(b.requests[i])

		// Skill proposal callback: accept activates the skill, reject archives it.
		if b.requests[i].Kind == "skill_proposal" {
			replyTo := strings.TrimSpace(b.requests[i].ReplyTo)
			for j := range b.skills {
				if b.skills[j].Name == replyTo && b.skills[j].Status != "archived" {
					activatedAt := time.Now().UTC().Format(time.RFC3339)
					if choiceID == "accept" {
						b.skills[j].Status = "active"
						b.skills[j].UpdatedAt = activatedAt
						b.counter++
						b.appendMessageLocked(channelMessage{
							ID:        fmt.Sprintf("msg-%d", b.counter),
							From:      "system",
							Channel:   normalizeChannelSlug(b.requests[i].Channel),
							Kind:      "skill_activated",
							Title:     "Skill Activated: " + b.skills[j].Title,
							Content:   fmt.Sprintf("Skill **%s** is now active and ready to use.", b.skills[j].Title),
							Timestamp: activatedAt,
						})
					} else {
						b.skills[j].Status = "archived"
						b.skills[j].UpdatedAt = activatedAt
					}
					break
				}
			}
		}

		b.counter++
		msg := channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      "you",
			Channel:   normalizeChannelSlug(b.requests[i].Channel),
			Tagged:    []string{b.requests[i].From},
			ReplyTo:   strings.TrimSpace(b.requests[i].ReplyTo),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		msg.Content = formatRequestAnswerMessage(b.requests[i], *answer)
		b.appendMessageLocked(msg)
		b.appendActionLocked("request_answered", "office", b.requests[i].Channel, "you", truncateSummary(msg.Content, 140), b.requests[i].ID)
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		b.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	}
	b.mu.Unlock()
	http.Error(w, "request not found", http.StatusNotFound)
}

func (b *Broker) unblockTasksForAnsweredRequestLocked(req humanInterview) {
	reqID := strings.TrimSpace(req.ID)
	if reqID == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	answerText := strings.TrimSpace(reqAnswerSummary(req.Answered))
	for i := range b.tasks {
		task := &b.tasks[i]
		if !task.Blocked || strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			continue
		}
		haystack := strings.ToLower(strings.TrimSpace(task.Title + "\n" + task.Details))
		if !strings.Contains(haystack, strings.ToLower(reqID)) {
			continue
		}
		task.Blocked = false
		if strings.EqualFold(strings.TrimSpace(task.Status), "blocked") {
			if strings.TrimSpace(task.Owner) != "" {
				task.Status = "in_progress"
			} else {
				task.Status = "open"
			}
		}
		b.queueTaskBehindActiveOwnerLaneLocked(task)
		if answerText != "" && !strings.Contains(task.Details, answerText) {
			task.Details = strings.TrimSpace(task.Details)
			if task.Details != "" {
				task.Details += "\n\n"
			}
			task.Details += fmt.Sprintf("Human answer for %s: %s", reqID, answerText)
		}
		task.UpdatedAt = now
		b.appendActionLocked(
			"task_unblocked",
			"office",
			task.Channel,
			req.From,
			truncateSummary(task.Title+" unblocked by answered "+reqID, 140),
			task.ID,
		)
	}
}

func reqAnswerSummary(answer *interviewAnswer) string {
	if answer == nil {
		return ""
	}
	if text := strings.TrimSpace(answer.CustomText); text != "" {
		return text
	}
	if text := strings.TrimSpace(answer.ChoiceText); text != "" {
		return text
	}
	return strings.TrimSpace(answer.ChoiceID)
}

func (b *Broker) handleInterview(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetInterview(w, r)
	case http.MethodPost:
		b.handlePostInterview(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handlePostInterview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From          string            `json:"from"`
		Channel       string            `json:"channel"`
		Question      string            `json:"question"`
		Context       string            `json:"context"`
		Options       []interviewOption `json:"options"`
		RecommendedID string            `json:"recommended_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.From) == "" || strings.TrimSpace(body.Question) == "" {
		http.Error(w, "from and question required", http.StatusBadRequest)
		return
	}
	reqBody, _ := json.Marshal(map[string]any{
		"action":         "create",
		"kind":           "interview",
		"title":          "Human interview",
		"from":           body.From,
		"channel":        body.Channel,
		"question":       body.Question,
		"context":        body.Context,
		"options":        body.Options,
		"recommended_id": body.RecommendedID,
		"blocking":       true,
		"required":       true,
	})
	r2 := r.Clone(r.Context())
	r2.Body = io.NopCloser(bytes.NewReader(reqBody))
	b.handlePostRequest(w, r2)
}

func (b *Broker) handleGetInterview(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	pending := firstBlockingRequest(b.requests)
	if pending == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"pending": nil})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"pending": pending})
}

func (b *Broker) handleInterviewAnswer(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetInterviewAnswer(w, r)
	case http.MethodPost:
		b.handlePostInterviewAnswer(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleGetInterviewAnswer(w http.ResponseWriter, r *http.Request) {
	b.handleGetRequestAnswer(w, r)
}

func (b *Broker) handlePostInterviewAnswer(w http.ResponseWriter, r *http.Request) {
	b.handlePostRequestAnswer(w, r)
}

// FormatChannelView returns a clean, Slack-style rendering of recent messages.
func FormatChannelView(messages []channelMessage) string {
	if len(messages) == 0 {
		return "  No messages yet. The team is getting set up..."
	}

	var sb strings.Builder
	for _, m := range messages {
		ts := m.Timestamp
		if len(ts) > 19 {
			ts = ts[11:19]
		}

		prefix := m.From
		if m.Kind == "automation" || m.From == "nex" {
			source := m.Source
			if source == "" {
				source = "context_graph"
			}
			title := m.Title
			if title != "" {
				title += ": "
			}
			sb.WriteString(fmt.Sprintf("  %s  Nex/%s: %s%s\n", ts, source, title, m.Content))
			continue
		}
		if strings.HasPrefix(m.Content, "[STATUS]") {
			sb.WriteString(fmt.Sprintf("  %s  @%s %s%s\n", ts, prefix, m.Content, formatMessageUsageSuffix(m.Usage)))
		} else {
			thread := ""
			if m.ReplyTo != "" {
				thread = fmt.Sprintf(" ↳ %s", m.ReplyTo)
			}
			sb.WriteString(fmt.Sprintf("  %s%s  @%s: %s%s\n", ts, thread, prefix, m.Content, formatMessageUsageSuffix(m.Usage)))
		}
	}
	return sb.String()
}

func formatMessageUsageSuffix(usage *messageUsage) string {
	if usage == nil {
		return ""
	}
	total := usage.TotalTokens
	if total == 0 {
		total = usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheCreationTokens
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf(" [%d tok]", total)
}

// --------------- Skills ---------------

func (b *Broker) handleTelegramGroups(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	groups := make([]map[string]any, 0)
	for chatID, title := range b.seenTelegramGroups {
		groups = append(groups, map[string]any{"chat_id": chatID, "title": title})
	}
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"groups": groups})
}

func (b *Broker) handleSkills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetSkills(w, r)
	case http.MethodPost:
		b.handlePostSkill(w, r)
	case http.MethodPut:
		b.handlePutSkill(w, r)
	case http.MethodDelete:
		b.handleDeleteSkill(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleSkillsSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/skills/")
	if strings.HasSuffix(path, "/invoke") {
		b.handleInvokeSkill(w, r)
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func skillSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}

func (b *Broker) findSkillByNameLocked(name string) *teamSkill {
	slug := skillSlug(name)
	for i := range b.skills {
		if skillSlug(b.skills[i].Name) == slug && b.skills[i].Status != "archived" {
			return &b.skills[i]
		}
	}
	return nil
}

func (b *Broker) findSkillByWorkflowKeyLocked(key string) *teamSkill {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	for i := range b.skills {
		if strings.TrimSpace(b.skills[i].WorkflowKey) == key && b.skills[i].Status != "archived" {
			return &b.skills[i]
		}
	}
	return nil
}

func (b *Broker) handleGetSkills(w http.ResponseWriter, r *http.Request) {
	channelFilter := normalizeChannelSlug(r.URL.Query().Get("channel"))

	b.mu.Lock()
	result := make([]teamSkill, 0, len(b.skills))
	for _, sk := range b.skills {
		if sk.Status == "archived" {
			continue
		}
		if channelFilter != "" && normalizeChannelSlug(sk.Channel) != channelFilter {
			continue
		}
		result = append(result, sk)
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"skills": result})
}

func (b *Broker) handlePostSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action              string   `json:"action"`
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
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	action := strings.TrimSpace(body.Action)
	if action == "" {
		action = "create"
	}
	if action != "create" && action != "propose" {
		http.Error(w, "action must be create or propose", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Content) == "" || strings.TrimSpace(body.CreatedBy) == "" {
		http.Error(w, "name, content, and created_by required", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}

	status := "active"
	msgKind := "skill_update"
	if action == "propose" {
		status = "proposed"
		msgKind = "skill_proposal"
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if existing := b.findSkillByNameLocked(body.Name); existing != nil {
		http.Error(w, "skill with this name already exists", http.StatusConflict)
		return
	}

	title := strings.TrimSpace(body.Title)
	if title == "" {
		title = strings.TrimSpace(body.Name)
	}

	b.counter++
	sk := teamSkill{
		ID:                  fmt.Sprintf("skill-%s", skillSlug(body.Name)),
		Name:                strings.TrimSpace(body.Name),
		Title:               title,
		Description:         strings.TrimSpace(body.Description),
		Content:             strings.TrimSpace(body.Content),
		CreatedBy:           strings.TrimSpace(body.CreatedBy),
		Channel:             channel,
		Tags:                body.Tags,
		Trigger:             strings.TrimSpace(body.Trigger),
		WorkflowProvider:    strings.TrimSpace(body.WorkflowProvider),
		WorkflowKey:         strings.TrimSpace(body.WorkflowKey),
		WorkflowDefinition:  strings.TrimSpace(body.WorkflowDefinition),
		WorkflowSchedule:    strings.TrimSpace(body.WorkflowSchedule),
		RelayID:             strings.TrimSpace(body.RelayID),
		RelayPlatform:       strings.TrimSpace(body.RelayPlatform),
		RelayEventTypes:     append([]string(nil), body.RelayEventTypes...),
		LastExecutionAt:     strings.TrimSpace(body.LastExecutionAt),
		LastExecutionStatus: strings.TrimSpace(body.LastExecutionStatus),
		UsageCount:          0,
		Status:              status,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	b.skills = append(b.skills, sk)

	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      sk.CreatedBy,
		Channel:   channel,
		Kind:      msgKind,
		Title:     sk.Title,
		Content:   fmt.Sprintf("Skill %q %sd by @%s", sk.Name, action, sk.CreatedBy),
		Timestamp: now,
	})
	b.appendActionLocked(msgKind, "office", channel, sk.CreatedBy, truncateSummary(sk.Title, 140), sk.ID)

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"skill": sk})
}

func (b *Broker) handlePutSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name                string   `json:"name"`
		Title               string   `json:"title"`
		Description         string   `json:"description"`
		Content             string   `json:"content"`
		Channel             string   `json:"channel"`
		Tags                []string `json:"tags"`
		Trigger             string   `json:"trigger"`
		Status              string   `json:"status"`
		WorkflowProvider    string   `json:"workflow_provider"`
		WorkflowKey         string   `json:"workflow_key"`
		WorkflowDefinition  string   `json:"workflow_definition"`
		WorkflowSchedule    string   `json:"workflow_schedule"`
		RelayID             string   `json:"relay_id"`
		RelayPlatform       string   `json:"relay_platform"`
		RelayEventTypes     []string `json:"relay_event_types"`
		LastExecutionAt     string   `json:"last_execution_at"`
		LastExecutionStatus string   `json:"last_execution_status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Name) == "" && strings.TrimSpace(body.WorkflowKey) == "" {
		http.Error(w, "name or workflow_key required", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	defer b.mu.Unlock()

	sk := b.findSkillByNameLocked(body.Name)
	if sk == nil {
		sk = b.findSkillByWorkflowKeyLocked(body.WorkflowKey)
	}
	if sk == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	if t := strings.TrimSpace(body.Title); t != "" {
		sk.Title = t
	}
	if d := strings.TrimSpace(body.Description); d != "" {
		sk.Description = d
	}
	if c := strings.TrimSpace(body.Content); c != "" {
		sk.Content = c
	}
	if ch := normalizeChannelSlug(body.Channel); ch != "" {
		sk.Channel = ch
	}
	if body.Tags != nil {
		sk.Tags = body.Tags
	}
	if t := strings.TrimSpace(body.Trigger); t != "" {
		sk.Trigger = t
	}
	if p := strings.TrimSpace(body.WorkflowProvider); p != "" {
		sk.WorkflowProvider = p
	}
	if key := strings.TrimSpace(body.WorkflowKey); key != "" {
		sk.WorkflowKey = key
	}
	if def := strings.TrimSpace(body.WorkflowDefinition); def != "" {
		sk.WorkflowDefinition = def
	}
	if sched := strings.TrimSpace(body.WorkflowSchedule); sched != "" {
		sk.WorkflowSchedule = sched
	}
	if relayID := strings.TrimSpace(body.RelayID); relayID != "" {
		sk.RelayID = relayID
	}
	if relayPlatform := strings.TrimSpace(body.RelayPlatform); relayPlatform != "" {
		sk.RelayPlatform = relayPlatform
	}
	if body.RelayEventTypes != nil {
		sk.RelayEventTypes = append([]string(nil), body.RelayEventTypes...)
	}
	if ts := strings.TrimSpace(body.LastExecutionAt); ts != "" {
		sk.LastExecutionAt = ts
	}
	if status := strings.TrimSpace(body.LastExecutionStatus); status != "" {
		sk.LastExecutionStatus = status
	}
	if s := strings.TrimSpace(body.Status); s != "" {
		sk.Status = s
	}
	sk.UpdatedAt = now

	channel := normalizeChannelSlug(sk.Channel)
	if channel == "" {
		channel = "general"
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      sk.CreatedBy,
		Channel:   channel,
		Kind:      "skill_update",
		Title:     sk.Title,
		Content:   fmt.Sprintf("Skill %q updated", sk.Name),
		Timestamp: now,
	})
	b.appendActionLocked("skill_update", "office", channel, sk.CreatedBy, truncateSummary(sk.Title+" [updated]", 140), sk.ID)

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"skill": *sk})
}

func (b *Broker) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	defer b.mu.Unlock()

	sk := b.findSkillByNameLocked(body.Name)
	if sk == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	sk.Status = "archived"
	sk.UpdatedAt = now

	channel := normalizeChannelSlug(sk.Channel)
	if channel == "" {
		channel = "general"
	}

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      sk.CreatedBy,
		Channel:   channel,
		Kind:      "skill_update",
		Title:     sk.Title,
		Content:   fmt.Sprintf("Skill %q archived", sk.Name),
		Timestamp: now,
	})
	b.appendActionLocked("skill_update", "office", channel, sk.CreatedBy, truncateSummary(sk.Title+" [archived]", 140), sk.ID)

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (b *Broker) handleInvokeSkill(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract skill name from path: /skills/{name}/invoke
	path := strings.TrimPrefix(r.URL.Path, "/skills/")
	skillName := strings.TrimSuffix(path, "/invoke")
	if strings.TrimSpace(skillName) == "" {
		http.Error(w, "skill name required in path", http.StatusBadRequest)
		return
	}

	var body struct {
		InvokedBy string `json:"invoked_by"`
		Channel   string `json:"channel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	b.mu.Lock()
	defer b.mu.Unlock()

	sk := b.findSkillByNameLocked(skillName)
	if sk == nil {
		http.Error(w, "skill not found", http.StatusNotFound)
		return
	}

	sk.UsageCount++
	sk.UpdatedAt = now

	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = normalizeChannelSlug(sk.Channel)
	}
	if channel == "" {
		channel = "general"
	}

	invoker := strings.TrimSpace(body.InvokedBy)
	if invoker == "" {
		invoker = "you"
	}
	sk.LastExecutionAt = now
	sk.LastExecutionStatus = "invoked"
	sk.UpdatedAt = now

	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      invoker,
		Channel:   channel,
		Kind:      "skill_invocation",
		Title:     sk.Title,
		Content:   fmt.Sprintf("Skill %q invoked by @%s (usage #%d)", sk.Name, invoker, sk.UsageCount),
		Timestamp: now,
	})
	b.appendActionLocked("skill_invocation", "office", channel, invoker, truncateSummary(sk.Title+" [invoked]", 140), sk.ID)

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"skill": *sk, "channel": channel})
}

// parseSkillProposalLocked extracts a [SKILL PROPOSAL] block from a message
// and creates a proposed skill. Must be called with b.mu held.
func (b *Broker) parseSkillProposalLocked(msg channelMessage) {
	// Only the team lead (CEO) may propose skills via message blocks.
	// If no lead exists (empty office), reject all proposals to prevent injection.
	lead := officeLeadSlugFrom(b.members)
	if lead == "" || msg.From != lead {
		return
	}

	const startTag = "[SKILL PROPOSAL]"
	const endTag = "[/SKILL PROPOSAL]"

	channel := msg.Channel
	if channel == "" {
		channel = "general"
	}

	content := msg.Content
	searchFrom := 0
	for {
		startIdx := strings.Index(content[searchFrom:], startTag)
		if startIdx < 0 {
			return
		}
		startIdx += searchFrom
		blockStart := startIdx + len(startTag)
		endRel := strings.Index(content[blockStart:], endTag)
		if endRel < 0 {
			return
		}
		endIdx := blockStart + endRel
		block := strings.TrimSpace(content[blockStart:endIdx])
		searchFrom = endIdx + len(endTag)

		// Split on "---" separator between metadata and instructions.
		parts := strings.SplitN(block, "---", 2)
		if len(parts) < 2 {
			continue
		}

		meta := strings.TrimSpace(parts[0])
		instructions := strings.TrimSpace(parts[1])

		// Parse metadata fields.
		var name, title, description, trigger string
		var tags []string
		for _, line := range strings.Split(meta, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Name:") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
			} else if strings.HasPrefix(line, "Title:") {
				title = strings.TrimSpace(strings.TrimPrefix(line, "Title:"))
			} else if strings.HasPrefix(line, "Description:") {
				description = strings.TrimSpace(strings.TrimPrefix(line, "Description:"))
			} else if strings.HasPrefix(line, "Trigger:") {
				trigger = strings.TrimSpace(strings.TrimPrefix(line, "Trigger:"))
			} else if strings.HasPrefix(line, "Tags:") {
				raw := strings.TrimSpace(strings.TrimPrefix(line, "Tags:"))
				for _, t := range strings.Split(raw, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
			}
		}

		if name == "" || title == "" {
			continue
		}

		slug := skillSlug(name)

		// Check for duplicate (skip archived).
		duplicate := false
		for _, s := range b.skills {
			if s.Name == slug && s.Status != "archived" {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}

		now := time.Now().UTC().Format(time.RFC3339)
		skill := teamSkill{
			ID:          slug,
			Name:        slug,
			Title:       title,
			Description: description,
			Content:     instructions,
			CreatedBy:   msg.From,
			Channel:     channel,
			Tags:        tags,
			Trigger:     trigger,
			UsageCount:  0,
			Status:      "proposed",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		b.skills = append(b.skills, skill)

		// Announce the proposal.
		b.counter++
		b.appendMessageLocked(channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      "system",
			Channel:   channel,
			Kind:      "skill_proposal",
			Title:     "Skill Proposed: " + title,
			Content:   fmt.Sprintf("@%s proposed a new skill **%s**: %s. Use /skills to review and approve.", msg.From, title, description),
			Timestamp: now,
		})

		// Surface the proposal in the Requests panel as a non-blocking human decision.
		b.counter++
		interview := humanInterview{
			ID:        fmt.Sprintf("request-%d", b.counter),
			Kind:      "skill_proposal",
			Status:    "pending",
			From:      msg.From,
			Channel:   channel,
			Title:     "Approve skill: " + title,
			Question:  fmt.Sprintf("@%s proposed skill **%s**: %s\n\nActivate it?", msg.From, title, description),
			ReplyTo:   slug,
			Blocking:  false,
			CreatedAt: now,
			UpdatedAt: now,
		}
		interview.Options, interview.RecommendedID = normalizeRequestOptions(interview.Kind, "accept", []interviewOption{
			{ID: "accept", Label: "Accept"},
			{ID: "reject", Label: "Reject"},
		})
		b.requests = append(b.requests, interview)
	}
}

// SeedDefaultSkills pre-populates the broker with the pack's default skills.
// It is idempotent: skills whose name already exists (by slug) are skipped.
// Call this after broker.Start() from the Launcher so that the first time a
// pack is launched the team has its playbooks ready to reference.
func (b *Broker) SeedDefaultSkills(specs []agent.PackSkillSpec) {
	if len(specs) == 0 {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		if b.findSkillByNameLocked(name) != nil {
			continue // already exists, skip
		}
		title := strings.TrimSpace(spec.Title)
		if title == "" {
			title = name
		}
		b.counter++
		sk := teamSkill{
			ID:          fmt.Sprintf("skill-%s", skillSlug(name)),
			Name:        name,
			Title:       title,
			Description: strings.TrimSpace(spec.Description),
			Content:     strings.TrimSpace(spec.Content),
			CreatedBy:   "system",
			Tags:        append([]string(nil), spec.Tags...),
			Trigger:     strings.TrimSpace(spec.Trigger),
			Status:      "active",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		b.skills = append(b.skills, sk)
	}
	if err := b.saveLocked(); err != nil {
		log.Printf("broker: saveLocked after seeding skills: %v", err)
	}
}

// dirExists returns true if path exists and is a directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
