package team

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const BrokerPort = 7890
const brokerTokenFile = "/tmp/wuphf-broker-token"

var brokerStatePath = defaultBrokerStatePath

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
	Reactions   []messageReaction `json:"reactions,omitempty"`
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
	Description string          `json:"description,omitempty"`
	Members     []string        `json:"members,omitempty"`
	Disabled    []string        `json:"disabled,omitempty"`
	Surface     *channelSurface `json:"surface,omitempty"`
	CreatedBy   string          `json:"created_by,omitempty"`
	CreatedAt   string          `json:"created_at,omitempty"`
	UpdatedAt   string          `json:"updated_at,omitempty"`
}

type officeMember struct {
	Slug           string   `json:"slug"`
	Name           string   `json:"name"`
	Role           string   `json:"role,omitempty"`
	Expertise      []string `json:"expertise,omitempty"`
	Personality    string   `json:"personality,omitempty"`
	PermissionMode string   `json:"permission_mode,omitempty"`
	AllowedTools   []string `json:"allowed_tools,omitempty"`
	CreatedBy      string   `json:"created_by,omitempty"`
	CreatedAt      string   `json:"created_at,omitempty"`
	BuiltIn        bool     `json:"built_in,omitempty"`
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
	Messages          []channelMessage             `json:"messages"`
	Members           []officeMember               `json:"members,omitempty"`
	Channels          []teamChannel                `json:"channels,omitempty"`
	SessionMode       string                       `json:"session_mode,omitempty"`
	OneOnOneAgent     string                       `json:"one_on_one_agent,omitempty"`
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

// Broker is a lightweight HTTP message broker for the team channel.
// All agent MCP instances connect to this shared broker.
type Broker struct {
	messages           []channelMessage
	members            []officeMember
	channels           []teamChannel
	sessionMode        string
	oneOnOneAgent      string
	tasks              []teamTask
	requests           []humanInterview
	actions            []officeActionLog
	signals            []officeSignalRecord
	decisions          []officeDecisionRecord
	watchdogs          []watchdogAlert
	scheduler          []schedulerJob
	skills             []teamSkill
	sharedMemory       map[string]map[string]string // namespace → key → value
	lastTaggedAt       map[string]time.Time         // when each agent was last @mentioned
	lastPaneSnapshot   map[string]string            // last captured pane content per agent (for change detection)
	seenTelegramGroups map[int64]string             // chat_id -> title, populated by transport
	counter            int
	notificationSince  string
	insightsSince      string
	pendingInterview   *humanInterview
	usage              teamUsageState
	externalDelivered  map[string]struct{} // message IDs already queued for external delivery
	mu                 sync.Mutex
	server             *http.Server
	token              string // shared secret for authenticating requests
	addr               string // actual listen address (useful when port=0)
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
	case "", "open", "done":
		return false
	default:
		return true
	}
}

func (b *Broker) syncTaskWorktreeLocked(task *teamTask) error {
	if task == nil {
		return nil
	}
	if taskNeedsLocalWorktree(task) {
		if strings.TrimSpace(task.WorktreePath) != "" && strings.TrimSpace(task.WorktreeBranch) != "" {
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

// generateToken returns a cryptographically random hex token.
func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: this should never happen on modern systems
		return fmt.Sprintf("wuphf-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// NewBroker creates a new channel broker with a random auth token.
func NewBroker() *Broker {
	b := &Broker{
		token: generateToken(),
	}
	_ = b.loadState()
	b.mu.Lock()
	b.ensureDefaultOfficeMembersLocked()
	b.ensureDefaultChannelsLocked()
	b.normalizeLoadedStateLocked()
	b.mu.Unlock()
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

// requireAuth wraps a handler to enforce Bearer token authentication.
// The /health endpoint is excluded so uptime checks work without credentials.
func (b *Broker) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+b.token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// Start launches the broker on localhost:7890.
func (b *Broker) Start() error {
	return b.StartOnPort(BrokerPort)
}

// StartOnPort launches the broker on the given port. Use 0 for an OS-assigned port.
func (b *Broker) StartOnPort(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", b.handleHealth) // no auth — used for liveness checks
	mux.HandleFunc("/session-mode", b.requireAuth(b.handleSessionMode))
	mux.HandleFunc("/messages", b.requireAuth(b.handleMessages))
	mux.HandleFunc("/reactions", b.requireAuth(b.handleReactions))
	mux.HandleFunc("/notifications/nex", b.requireAuth(b.handleNexNotifications))
	mux.HandleFunc("/office-members", b.requireAuth(b.handleOfficeMembers))
	mux.HandleFunc("/channels", b.requireAuth(b.handleChannels))
	mux.HandleFunc("/channel-members", b.requireAuth(b.handleChannelMembers))
	mux.HandleFunc("/members", b.requireAuth(b.handleMembers))
	mux.HandleFunc("/tasks", b.requireAuth(b.handleTasks))
	mux.HandleFunc("/tasks/ack", b.requireAuth(b.handleTaskAck))
	mux.HandleFunc("/task-plan", b.requireAuth(b.handleTaskPlan))
	mux.HandleFunc("/memory", b.requireAuth(b.handleMemory))
	mux.HandleFunc("/requests", b.requireAuth(b.handleRequests))
	mux.HandleFunc("/requests/answer", b.requireAuth(b.handleRequestAnswer))
	mux.HandleFunc("/interview", b.requireAuth(b.handleInterview))
	mux.HandleFunc("/interview/answer", b.requireAuth(b.handleInterviewAnswer))
	mux.HandleFunc("/reset", b.requireAuth(b.handleReset))
	mux.HandleFunc("/reset-dm", b.requireAuth(b.handleResetDM))
	mux.HandleFunc("/usage", b.requireAuth(b.handleUsage))
	mux.HandleFunc("/signals", b.requireAuth(b.handleSignals))
	mux.HandleFunc("/decisions", b.requireAuth(b.handleDecisions))
	mux.HandleFunc("/watchdogs", b.requireAuth(b.handleWatchdogs))
	mux.HandleFunc("/actions", b.requireAuth(b.handleActions))
	mux.HandleFunc("/scheduler", b.requireAuth(b.handleScheduler))
	mux.HandleFunc("/skills", b.requireAuth(b.handleSkills))
	mux.HandleFunc("/skills/", b.requireAuth(b.handleSkillsSubpath))
	mux.HandleFunc("/telegram/groups", b.requireAuth(b.handleTelegramGroups))
	mux.HandleFunc("/bridges", b.requireAuth(b.handleBridge))
	mux.HandleFunc("/queue", b.requireAuth(b.handleQueue))
	mux.HandleFunc("/v1/logs", b.requireAuth(b.handleOTLPLogs))
	mux.HandleFunc("/web-token", b.handleWebToken)

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	b.addr = ln.Addr().String()

	b.server = &http.Server{
		Addr:         addr,
		Handler:      corsMiddleware(mux),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Write token to well-known path so tests and tools can authenticate.
	// Use /tmp directly (not os.TempDir which varies by OS).
	_ = os.WriteFile(brokerTokenFile, []byte(b.token), 0600)

	go func() {
		_ = b.server.Serve(ln)
	}()
	return nil
}

// Stop shuts down the broker.
func (b *Broker) Stop() {
	if b.server != nil {
		b.server.Close()
	}
}

// corsMiddleware adds CORS headers so the web UI on a different port can call the broker API.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleWebToken returns the broker token to localhost clients without requiring auth.
// This lets the web UI fetch the token to authenticate subsequent API calls.
func (b *Broker) handleWebToken(w http.ResponseWriter, r *http.Request) {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	if host != "127.0.0.1" && host != "::1" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": b.token})
}

// ServeWebUI starts a static file server for the web UI on the given port.
func (b *Broker) ServeWebUI(port int) {
	exePath, _ := os.Executable()
	webDir := filepath.Join(filepath.Dir(exePath), "web")
	if _, err := os.Stat(webDir); os.IsNotExist(err) {
		webDir = "web"
	}
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(webDir)))
	go http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), mux)
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

// PostInboundSurfaceMessage posts a message from an external surface into the broker channel.
func (b *Broker) PostInboundSurfaceMessage(from, channel, content, provider string) (channelMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		return channelMessage{}, fmt.Errorf("channel required for surface message")
	}
	if b.findChannelLocked(channel) == nil {
		return channelMessage{}, fmt.Errorf("channel not found: %s", channel)
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
	b.messages = append(b.messages, msg)
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
	b.scheduler = nil
	b.pendingInterview = nil
	b.counter = 0
	b.notificationSince = ""
	b.insightsSince = ""
	b.usage = teamUsageState{Agents: make(map[string]usageTotals)}
	b.normalizeLoadedStateLocked()
	_ = b.saveLocked()
	b.mu.Unlock()
}

func defaultBrokerStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".wuphf", "team", "broker-state.json")
	}
	return filepath.Join(home, ".wuphf", "team", "broker-state.json")
}

func (b *Broker) loadState() error {
	path := brokerStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var state brokerState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	b.messages = state.Messages
	b.members = state.Members
	b.channels = state.Channels
	b.sessionMode = state.SessionMode
	b.oneOnOneAgent = state.OneOnOneAgent
	b.tasks = state.Tasks
	b.requests = state.Requests
	b.actions = state.Actions
	b.signals = state.Signals
	b.decisions = state.Decisions
	b.watchdogs = state.Watchdogs
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
	b.ensureDefaultChannelsLocked()
	b.ensureDefaultOfficeMembersLocked()
	b.normalizeLoadedStateLocked()
	return nil
}

func (b *Broker) saveLocked() error {
	path := brokerStatePath()
	if len(b.messages) == 0 && len(b.tasks) == 0 && len(activeRequests(b.requests)) == 0 && len(b.actions) == 0 && len(b.signals) == 0 && len(b.decisions) == 0 && len(b.watchdogs) == 0 && len(b.scheduler) == 0 && len(b.skills) == 0 && len(b.sharedMemory) == 0 && isDefaultChannelState(b.channels) && isDefaultOfficeMemberState(b.members) && b.counter == 0 && b.notificationSince == "" && b.insightsSince == "" && usageStateIsZero(b.usage) && b.sessionMode == SessionModeOffice && b.oneOnOneAgent == DefaultOneOnOneAgent {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	state := brokerState{
		Messages:          b.messages,
		Members:           b.members,
		Channels:          b.channels,
		SessionMode:       b.sessionMode,
		OneOnOneAgent:     b.oneOnOneAgent,
		Tasks:             b.tasks,
		Requests:          b.requests,
		Actions:           b.actions,
		Signals:           b.signals,
		Decisions:         b.decisions,
		Watchdogs:         b.watchdogs,
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func defaultOfficeMembers() []officeMember {
	now := time.Now().UTC().Format(time.RFC3339)
	manifest, err := company.LoadManifest()
	if err != nil || len(manifest.Members) == 0 {
		manifest = company.DefaultManifest()
	}
	members := make([]officeMember, 0, len(manifest.Members))
	for _, cfg := range manifest.Members {
		members = append(members, officeMember{
			Slug:           cfg.Slug,
			Name:           cfg.Name,
			Role:           cfg.Role,
			Expertise:      append([]string(nil), cfg.Expertise...),
			Personality:    cfg.Personality,
			PermissionMode: cfg.PermissionMode,
			AllowedTools:   append([]string(nil), cfg.AllowedTools...),
			CreatedBy:      "wuphf",
			CreatedAt:      now,
			BuiltIn:        cfg.System || cfg.Slug == manifest.Lead || cfg.Slug == "ceo",
		})
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
	manifest, err := company.LoadManifest()
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
	slug = strings.ReplaceAll(slug, " ", "-")
	slug = strings.ReplaceAll(slug, "_", "-")
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

func (b *Broker) ensureDefaultOfficeMembersLocked() {
	if len(b.members) == 0 {
		b.members = defaultOfficeMembers()
		return
	}
	defaults := defaultOfficeMembers()
	for _, member := range defaults {
		if b.findMemberLocked(member.Slug) == nil {
			b.members = append(b.members, member)
		}
	}
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
		b.scheduleTaskLifecycleLocked(&b.tasks[i])
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

func (b *Broker) findChannelLocked(slug string) *teamChannel {
	slug = normalizeChannelSlug(slug)
	for i := range b.channels {
		if b.channels[i].Slug == slug {
			return &b.channels[i]
		}
	}
	return nil
}

func (b *Broker) findMemberLocked(slug string) *officeMember {
	slug = normalizeChannelSlug(slug)
	for i := range b.members {
		if b.members[i].Slug == slug {
			return &b.members[i]
		}
	}
	return nil
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
	if strings.EqualFold(task.Status, "done") {
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
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":           "ok",
		"session_mode":     mode,
		"one_on_one_agent": agent,
	})
}

func (b *Broker) handleSessionMode(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		mode, agent := b.SessionModeState()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
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
		json.NewEncoder(w).Encode(map[string]any{
			"session_mode":     mode,
			"one_on_one_agent": agent,
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
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
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
	json.NewEncoder(w).Encode(map[string]any{"ok": true, "removed": removed})
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
			exec.Command("tmux", "-L", "wuphf", "send-keys", "-t", target, "C-c", "").Run()
			time.Sleep(500 * time.Millisecond)
			exec.Command("tmux", "-L", "wuphf", "send-keys", "-t", target, "C-c", "").Run()
			time.Sleep(500 * time.Millisecond)
			// Respawn the pane with a fresh claude session
			exec.Command("tmux", "-L", "wuphf", "respawn-pane", "-k", "-t", target).Run()
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
	json.NewEncoder(w).Encode(usage)
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
	json.NewEncoder(w).Encode(map[string]any{"signals": signals})
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
	json.NewEncoder(w).Encode(map[string]any{"decisions": decisions})
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
	json.NewEncoder(w).Encode(map[string]any{"watchdogs": alerts})
}

func (b *Broker) handleActions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		actions := make([]officeActionLog, len(b.actions))
		copy(actions, b.actions)
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"actions": actions})
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
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
		json.NewEncoder(w).Encode(map[string]any{"jobs": jobs})
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
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
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
	json.NewEncoder(w).Encode(map[string]any{
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
	json.NewEncoder(w).Encode(b.QueueSnapshot())
}

func (b *Broker) handleOfficeMembers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		members := make([]officeMember, len(b.members))
		copy(members, b.members)
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"members": members})
	case http.MethodPost:
		var body struct {
			Action         string   `json:"action"`
			Slug           string   `json:"slug"`
			Name           string   `json:"name"`
			Role           string   `json:"role"`
			Expertise      []string `json:"expertise"`
			Personality    string   `json:"personality"`
			PermissionMode string   `json:"permission_mode"`
			AllowedTools   []string `json:"allowed_tools"`
			CreatedBy      string   `json:"created_by"`
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
			applyOfficeMemberDefaults(&member)
			b.members = append(b.members, member)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"member": member})
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
			applyOfficeMemberDefaults(member)
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"member": member})
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
			filteredMembers := b.members[:0]
			for _, existing := range b.members {
				if existing.Slug != slug {
					filteredMembers = append(filteredMembers, existing)
				}
			}
			b.members = filteredMembers
			for i := range b.channels {
				nextMembers := b.channels[i].Members[:0]
				for _, existing := range b.channels[i].Members {
					if existing != slug {
						nextMembers = append(nextMembers, existing)
					}
				}
				b.channels[i].Members = nextMembers
				nextDisabled := b.channels[i].Disabled[:0]
				for _, existing := range b.channels[i].Disabled {
					if existing != slug {
						nextDisabled = append(nextDisabled, existing)
					}
				}
				b.channels[i].Disabled = nextDisabled
				b.channels[i].UpdatedAt = now
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
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.mu.Lock()
		channels := make([]teamChannel, len(b.channels))
		copy(channels, b.channels)
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"channels": channels})
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
			members := uniqueSlugs(body.Members)
			if len(members) > 0 {
				validated := make([]string, 0, len(members))
				var missing []string
				for _, member := range members {
					if b.findMemberLocked(member) == nil {
						missing = append(missing, member)
						continue
					}
					validated = append(validated, member)
				}
				if len(missing) > 0 {
					http.Error(w, "unknown members: "+strings.Join(missing, ", "), http.StatusNotFound)
					return
				}
				members = validated
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
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"channel": ch})
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
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleChannelMembers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
		b.mu.Lock()
		ch := b.findChannelLocked(channel)
		if ch == nil {
			b.mu.Unlock()
			http.Error(w, "channel not found", http.StatusNotFound)
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
		json.NewEncoder(w).Encode(map[string]any{"channel": channel, "members": memberInfo})
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
		if b.findMemberLocked(member) == nil {
			b.mu.Unlock()
			http.Error(w, "member not found", http.StatusNotFound)
			return
		}
		if member == "ceo" && (action == "remove" || action == "disable") {
			b.mu.Unlock()
			http.Error(w, "cannot remove or disable CEO", http.StatusBadRequest)
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
		json.NewEncoder(w).Encode(state)
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
	json.NewEncoder(w).Encode(map[string]any{"accepted": len(events)})
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
	json.NewEncoder(w).Encode(map[string]any{
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
	if b.findChannelLocked(channel) == nil {
		b.mu.Unlock()
		http.Error(w, "channel not found", http.StatusNotFound)
		return
	}
	if !b.canAccessChannelLocked(body.From, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      body.From,
		Channel:   channel,
		Kind:      strings.TrimSpace(body.Kind),
		Title:     strings.TrimSpace(body.Title),
		Content:   body.Content,
		Tagged:    uniqueSlugs(body.Tagged),
		ReplyTo:   strings.TrimSpace(body.ReplyTo),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.messages = append(b.messages, msg)
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
	json.NewEncoder(w).Encode(map[string]any{
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
					json.NewEncoder(w).Encode(map[string]any{"ok": true, "duplicate": true})
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
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
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
	b.messages = append(b.messages, msg)
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
		return channelMessage{}, fmt.Errorf("channel not found")
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
	b.messages = append(b.messages, msg)
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

	b.messages = append(b.messages, msg)
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
	json.NewEncoder(w).Encode(map[string]any{
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
		ts := msg.Timestamp
		if len(ts) > 19 {
			ts = ts[11:19]
		}
		info := members[msg.From]
		info.lastMessage = content
		info.lastTime = ts
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
	taggedAt := b.lastTaggedAt
	b.mu.Unlock()

	type memberEntry struct {
		Slug         string `json:"slug"`
		Name         string `json:"name,omitempty"`
		Role         string `json:"role,omitempty"`
		Disabled     bool   `json:"disabled,omitempty"`
		LastMessage  string `json:"lastMessage"`
		LastTime     string `json:"lastTime"`
		LiveActivity string `json:"liveActivity,omitempty"`
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
		if activity, ok := paneActivity[slug]; ok {
			entry.LiveActivity = activity
		}
		// Also mark as active if tagged recently and hasn't replied yet
		if entry.LiveActivity == "" && taggedAt != nil {
			if t, ok := taggedAt[slug]; ok && time.Since(t) < 60*time.Second {
				entry.LiveActivity = "active"
			}
		}
		list = append(list, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"channel": channel, "members": list})
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

func (b *Broker) handleGetTasks(w http.ResponseWriter, r *http.Request) {
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	mySlug := strings.TrimSpace(r.URL.Query().Get("my_slug"))
	viewerSlug := strings.TrimSpace(r.URL.Query().Get("viewer_slug"))
	channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
	if channel == "" {
		channel = "general"
	}
	includeDone := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_done")), "true")

	b.mu.Lock()
	if !b.canAccessChannelLocked(viewerSlug, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	result := make([]teamTask, 0, len(b.tasks))
	for _, task := range b.tasks {
		if normalizeChannelSlug(task.Channel) != channel {
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
	json.NewEncoder(w).Encode(map[string]any{"channel": channel, "tasks": result})
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
		if existing := b.findReusableTaskLocked(channel, strings.TrimSpace(body.Title), strings.TrimSpace(body.ThreadID), strings.TrimSpace(body.Owner)); existing != nil {
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
			json.NewEncoder(w).Encode(map[string]any{"task": *existing})
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
		json.NewEncoder(w).Encode(map[string]any{"task": task})
		return
	}

	for i := range b.tasks {
		if b.tasks[i].ID != strings.TrimSpace(body.ID) {
			continue
		}
		if normalizeChannelSlug(b.tasks[i].Channel) != channel {
			continue
		}
		task := &b.tasks[i]
		switch action {
		case "claim", "assign":
			if strings.TrimSpace(body.Owner) == "" {
				http.Error(w, "owner required", http.StatusBadRequest)
				return
			}
			task.Owner = strings.TrimSpace(body.Owner)
			task.Status = "in_progress"
			if taskNeedsStructuredReview(task) && strings.TrimSpace(task.ReviewState) == "" {
				task.ReviewState = "pending_review"
			}
		case "complete":
			if taskNeedsStructuredReview(task) {
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
			task.Status = "blocked"
		case "release":
			task.Owner = ""
			task.Status = "open"
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
		task.UpdatedAt = now
		if task.Status == "done" {
			b.unblockDependentsLocked(task.ID)
		}
		b.scheduleTaskLifecycleLocked(task)
		if err := b.syncTaskWorktreeLocked(task); err != nil {
			http.Error(w, "failed to manage task worktree", http.StatusInternalServerError)
			return
		}
		b.appendActionLocked("task_updated", "office", channel, strings.TrimSpace(body.CreatedBy), truncateSummary(task.Title+" ["+task.Status+"]", 140), task.ID)
		if err := b.saveLocked(); err != nil {
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"task": *task})
		return
	}

	http.Error(w, "task not found", http.StatusNotFound)
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
			Title     string   `json:"title"`
			Assignee  string   `json:"assignee"`
			Details   string   `json:"details"`
			DependsOn []string `json:"depends_on"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	createdBy := strings.TrimSpace(body.CreatedBy)
	if createdBy == "" || len(body.Tasks) == 0 {
		http.Error(w, "created_by and tasks required", http.StatusBadRequest)
		return
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
		b.counter++
		taskID := fmt.Sprintf("task-%d", b.counter)
		titleToID[strings.TrimSpace(item.Title)] = taskID

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

		task := teamTask{
			ID:        taskID,
			Channel:   channel,
			Title:     strings.TrimSpace(item.Title),
			Details:   strings.TrimSpace(item.Details),
			Owner:     strings.TrimSpace(item.Assignee),
			Status:    "open",
			CreatedBy: createdBy,
			DependsOn: resolvedDeps,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if task.Owner != "" && len(resolvedDeps) == 0 {
			task.Status = "in_progress"
		}
		if len(resolvedDeps) > 0 && b.hasUnresolvedDepsLocked(&task) {
			task.Blocked = true
		}
		b.scheduleTaskLifecycleLocked(&task)
		b.tasks = append(b.tasks, task)
		b.appendActionLocked("task_created", "office", channel, createdBy, truncateSummary(task.Title, 140), task.ID)
		created = append(created, task)
	}

	if err := b.saveLocked(); err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"tasks": created})
}

func (b *Broker) handleMemory(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
		if channel == "" {
			channel = "general"
		}
		b.mu.Lock()
		mem := b.sharedMemory
		b.mu.Unlock()
		if mem == nil {
			mem = make(map[string]map[string]string)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"memory": mem})
	case http.MethodPost:
		var body struct {
			Namespace string `json:"namespace"`
			Key       string `json:"key"`
			Value     string `json:"value"`
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
		b.sharedMemory[ns][key] = body.Value
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			http.Error(w, "failed to persist", http.StatusInternalServerError)
			return
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "namespace": ns, "key": key})
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
			json.NewEncoder(w).Encode(map[string]any{"task": b.tasks[i]})
			return
		}
	}
	http.Error(w, "task not found", http.StatusNotFound)
}

func (b *Broker) EnsureTask(channel, title, details, owner, createdBy, threadID string) (teamTask, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	if b.findChannelLocked(channel) == nil {
		return teamTask{}, false, fmt.Errorf("channel not found")
	}
	if !b.canAccessChannelLocked(createdBy, channel) {
		return teamTask{}, false, fmt.Errorf("channel access denied")
	}
	title = strings.TrimSpace(title)
	if existing := b.findReusableTaskLocked(channel, title, strings.TrimSpace(threadID), strings.TrimSpace(owner)); existing != nil {
		if existing.Details == "" && strings.TrimSpace(details) != "" {
			existing.Details = strings.TrimSpace(details)
		}
		if existing.Owner == "" && strings.TrimSpace(owner) != "" {
			existing.Owner = strings.TrimSpace(owner)
			existing.Status = "in_progress"
		}
		if existing.ThreadID == "" && strings.TrimSpace(threadID) != "" {
			existing.ThreadID = strings.TrimSpace(threadID)
		}
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
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
		CreatedAt: now,
		UpdatedAt: now,
	}
	if task.Owner != "" {
		task.Status = "in_progress"
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
}

func (b *Broker) EnsurePlannedTask(input plannedTaskInput) (teamTask, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel := normalizeChannelSlug(input.Channel)
	if channel == "" {
		channel = "general"
	}
	if b.findChannelLocked(channel) == nil {
		return teamTask{}, false, fmt.Errorf("channel not found")
	}
	if !b.canAccessChannelLocked(input.CreatedBy, channel) {
		return teamTask{}, false, fmt.Errorf("channel access denied")
	}
	title := strings.TrimSpace(input.Title)
	if existing := b.findReusableTaskLocked(channel, title, strings.TrimSpace(input.ThreadID), strings.TrimSpace(input.Owner)); existing != nil {
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
		existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
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
	task := teamTask{
		ID:               fmt.Sprintf("task-%d", b.counter+1),
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
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	b.counter++
	if task.Owner != "" {
		task.Status = "in_progress"
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

// unblockDependentsLocked checks all blocked tasks and unblocks those whose dependencies are now resolved.
func (b *Broker) unblockDependentsLocked(completedTaskID string) {
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
		}
	}
}

func (b *Broker) findReusableTaskLocked(channel, title, threadID, owner string) *teamTask {
	channel = normalizeChannelSlug(channel)
	title = strings.TrimSpace(title)
	threadID = strings.TrimSpace(threadID)
	owner = strings.TrimSpace(owner)
	for i := range b.tasks {
		task := &b.tasks[i]
		if normalizeChannelSlug(task.Channel) != channel {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			continue
		}
		if threadID != "" && strings.TrimSpace(task.ThreadID) == threadID {
			return task
		}
		if title != "" && strings.EqualFold(strings.TrimSpace(task.Title), title) {
			if owner == "" || strings.TrimSpace(task.Owner) == owner || strings.TrimSpace(task.Owner) == "" {
				return task
			}
		}
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
	if channel == "" {
		channel = "general"
	}
	viewerSlug := strings.TrimSpace(r.URL.Query().Get("viewer_slug"))
	includeResolved := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_resolved")), "true")
	b.mu.Lock()
	if !b.canAccessChannelLocked(viewerSlug, channel) {
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
		if reqChannel != channel {
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
	json.NewEncoder(w).Encode(map[string]any{
		"channel":  channel,
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
		json.NewEncoder(w).Encode(map[string]any{"request": req, "id": req.ID})
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
			json.NewEncoder(w).Encode(map[string]any{"request": b.requests[i]})
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
			json.NewEncoder(w).Encode(map[string]any{"answered": req.Answered})
			return
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"answered": nil})
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
		b.pendingInterview = firstBlockingRequest(b.requests)

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
		b.messages = append(b.messages, msg)
		b.appendActionLocked("request_answered", "office", b.requests[i].Channel, "you", truncateSummary(msg.Content, 140), b.requests[i].ID)
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		b.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	}
	b.mu.Unlock()
	http.Error(w, "request not found", http.StatusNotFound)
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
		json.NewEncoder(w).Encode(map[string]any{"pending": nil})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"pending": pending})
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
			sb.WriteString(fmt.Sprintf("  %s  @%s %s\n", ts, prefix, m.Content))
		} else {
			thread := ""
			if m.ReplyTo != "" {
				thread = fmt.Sprintf(" ↳ %s", m.ReplyTo)
			}
			sb.WriteString(fmt.Sprintf("  %s%s  @%s: %s\n", ts, thread, prefix, m.Content))
		}
	}
	return sb.String()
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
	json.NewEncoder(w).Encode(map[string]any{"groups": groups})
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
	json.NewEncoder(w).Encode(map[string]any{"skills": result})
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

	b.messages = append(b.messages, channelMessage{
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
	json.NewEncoder(w).Encode(map[string]any{"skill": sk})
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
	b.messages = append(b.messages, channelMessage{
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
	json.NewEncoder(w).Encode(map[string]any{"skill": *sk})
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
	b.messages = append(b.messages, channelMessage{
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
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
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
		invoker = "unknown"
	}

	b.counter++
	b.messages = append(b.messages, channelMessage{
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
	json.NewEncoder(w).Encode(map[string]any{"skill": *sk})
}

// parseSkillProposalLocked extracts a [SKILL PROPOSAL] block from a message
// and creates a proposed skill. Must be called with b.mu held.
func (b *Broker) parseSkillProposalLocked(msg channelMessage) {
	const startTag = "[SKILL PROPOSAL]"
	const endTag = "[/SKILL PROPOSAL]"

	idx := strings.Index(msg.Content, startTag)
	if idx < 0 {
		return
	}
	endIdx := strings.Index(msg.Content, endTag)
	if endIdx < 0 {
		return
	}

	block := msg.Content[idx+len(startTag) : endIdx]
	block = strings.TrimSpace(block)

	// Split on "---" separator between metadata and instructions
	parts := strings.SplitN(block, "---", 2)
	if len(parts) < 2 {
		return
	}

	meta := strings.TrimSpace(parts[0])
	instructions := strings.TrimSpace(parts[1])

	// Parse metadata fields
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
		return
	}

	slug := skillSlug(name)

	// Check for duplicate (skip archived)
	for _, s := range b.skills {
		if s.Name == slug && s.Status != "archived" {
			return
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	channel := msg.Channel
	if channel == "" {
		channel = "general"
	}

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

	// Announce the proposal
	b.counter++
	b.messages = append(b.messages, channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   channel,
		Kind:      "skill_proposal",
		Title:     "Skill Proposed: " + title,
		Content:   fmt.Sprintf("@%s proposed a new skill **%s**: %s. Use /skills to review and approve.", msg.From, title, description),
		Timestamp: now,
	})
}
