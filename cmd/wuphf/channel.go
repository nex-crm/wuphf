package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/setup"
	"github.com/nex-crm/wuphf/internal/team"
	"github.com/nex-crm/wuphf/internal/tui"
)

type channelMsg struct {
	messages []brokerMessage
}

type channelMembersMsg struct {
	members []channelMember
}

type channelOfficeMembersMsg struct {
	members []officeMemberInfo
}

type channelChannelsMsg struct {
	channels []channelInfo
}

type channelRequestsMsg struct {
	requests []channelInterview
	pending  *channelInterview
}

type channelTasksMsg struct {
	tasks []channelTask
}

type channelActionsMsg struct {
	actions []channelAction
}

type channelSignalsMsg struct {
	signals []channelSignal
}

type channelDecisionsMsg struct {
	decisions []channelDecision
}

type channelWatchdogsMsg struct {
	alerts []channelWatchdog
}

type channelSchedulerMsg struct {
	jobs []channelSchedulerJob
}

type channelSkill struct {
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

type channelSkillsMsg struct {
	skills []channelSkill
}

type channelUsageMsg struct {
	usage channelUsageState
}

func appendUniqueMessages(existing, incoming []brokerMessage) ([]brokerMessage, int) {
	if len(incoming) == 0 {
		return existing, 0
	}
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	out := make([]brokerMessage, 0, len(existing)+len(incoming))
	for _, msg := range existing {
		out = append(out, msg)
		if strings.TrimSpace(msg.ID) != "" {
			seen[msg.ID] = struct{}{}
		}
	}
	added := 0
	for _, msg := range incoming {
		if id := strings.TrimSpace(msg.ID); id != "" {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
		}
		out = append(out, msg)
		added++
	}
	return out, added
}

func normalizeSidebarSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "-")
	value = strings.ReplaceAll(value, "_", "-")
	return value
}

type channelHealthMsg struct {
	Connected     bool
	SessionMode   string
	OneOnOneAgent string
}

type brokerReaction struct {
	Emoji string `json:"emoji"`
	From  string `json:"from"`
}

type brokerMessage struct {
	ID          string           `json:"id"`
	From        string           `json:"from"`
	Kind        string           `json:"kind,omitempty"`
	Source      string           `json:"source,omitempty"`
	SourceLabel string           `json:"source_label,omitempty"`
	EventID     string           `json:"event_id,omitempty"`
	Title       string           `json:"title,omitempty"`
	Content     string           `json:"content"`
	Tagged      []string         `json:"tagged"`
	ReplyTo     string           `json:"reply_to"`
	Timestamp   string           `json:"timestamp"`
	Reactions   []brokerReaction `json:"reactions,omitempty"`
}

type channelMember struct {
	Slug         string `json:"slug"`
	Name         string `json:"name,omitempty"`
	Role         string `json:"role,omitempty"`
	Disabled     bool   `json:"disabled,omitempty"`
	LastMessage  string `json:"lastMessage"`
	LastTime     string `json:"lastTime"`
	LiveActivity string `json:"liveActivity,omitempty"`
}

type officeMemberInfo struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Expertise   []string `json:"expertise,omitempty"`
	Personality string   `json:"personality,omitempty"`
	BuiltIn     bool     `json:"built_in,omitempty"`
}

type channelInfo struct {
	Slug        string   `json:"slug"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members"`
	Disabled    []string `json:"disabled"`
}

type channelInterviewOption struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	RequiresText bool   `json:"requires_text,omitempty"`
	TextHint     string `json:"text_hint,omitempty"`
}

type channelInterview struct {
	ID            string                   `json:"id"`
	Kind          string                   `json:"kind,omitempty"`
	Status        string                   `json:"status,omitempty"`
	From          string                   `json:"from"`
	Channel       string                   `json:"channel"`
	Title         string                   `json:"title,omitempty"`
	Question      string                   `json:"question"`
	Context       string                   `json:"context"`
	Options       []channelInterviewOption `json:"options"`
	RecommendedID string                   `json:"recommended_id"`
	Blocking      bool                     `json:"blocking,omitempty"`
	Required      bool                     `json:"required,omitempty"`
	Secret        bool                     `json:"secret,omitempty"`
	ReplyTo       string                   `json:"reply_to,omitempty"`
	CreatedAt     string                   `json:"created_at"`
	DueAt         string                   `json:"due_at,omitempty"`
	FollowUpAt    string                   `json:"follow_up_at,omitempty"`
	ReminderAt    string                   `json:"reminder_at,omitempty"`
	RecheckAt     string                   `json:"recheck_at,omitempty"`
}

type channelUsageTotals struct {
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	CostUsd             float64 `json:"cost_usd"`
	Requests            int     `json:"requests"`
}

type channelUsageState struct {
	Session channelUsageTotals            `json:"session,omitempty"`
	Total   channelUsageTotals            `json:"total"`
	Agents  map[string]channelUsageTotals `json:"agents"`
	Since   string                        `json:"since,omitempty"`
}

type channelTask struct {
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

type channelAction struct {
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

type channelSignal struct {
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

type channelDecision struct {
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

type channelWatchdog struct {
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

type channelSchedulerJob struct {
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

type channelTickMsg time.Time
type channelPostDoneMsg struct {
	err    error
	notice string
	action string
	slug   string
}
type channelInterviewAnswerDoneMsg struct{ err error }
type channelInterruptDoneMsg struct{ err error }
type channelResetDoneMsg struct {
	err           error
	notice        string
	sessionMode   string
	oneOnOneAgent string
}
type channelResetDMDoneMsg struct {
	err     error
	removed int
}
type channelInitDoneMsg struct {
	err    error
	notice string
}
type channelIntegrationDoneMsg struct {
	label string
	url   string
	err   error
}
type telegramDiscoverMsg struct {
	botName string
	groups  []team.TelegramGroup
	token   string
	err     error
}
type telegramConnectDoneMsg struct {
	channelSlug string
	groupTitle  string
	err         error
}

type channelTaskMutationDoneMsg struct {
	notice string
	err    error
}

type channelMemberDraftDoneMsg struct {
	err    error
	notice string
}

type channelMemberDraft struct {
	Mode           string
	OriginalSlug   string
	Step           int
	Slug           string
	Name           string
	Role           string
	Expertise      string
	Personality    string
	PermissionMode string
}

var mentionPattern = regexp.MustCompile(`@([A-Za-z0-9_-]+)`)

var brokerTokenPath = "/tmp/wuphf-broker-token"
var officeDirectory = map[string]officeMemberInfo{}

func currentBrokerAuthToken() string {
	if token := strings.TrimSpace(os.Getenv("WUPHF_BROKER_TOKEN")); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv("NEX_BROKER_TOKEN")); token != "" {
		return token
	}
	data, err := os.ReadFile(brokerTokenPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// newBrokerRequest creates an HTTP request with the broker auth header.
func newBrokerRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	if brokerAuthToken := currentBrokerAuthToken(); brokerAuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+brokerAuthToken)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

var channelSlashCommands = []tui.SlashCommand{
	{Name: "init", Description: "Run setup", Category: "setup"},
	{Name: "provider", Description: "Switch LLM provider", Category: "setup"},
	{Name: "doctor", Description: "Check readiness, integrations, and runtime health", Category: "setup"},
	{Name: "integrate", Description: "Connect an integration", Category: "setup"},
	{Name: "connect", Description: "Connect an external channel (Telegram, Slack, Discord)", Category: "setup"},
	{Name: "1o1", Description: "Enable, switch, or disable direct 1:1 mode", Category: "session"},
	{Name: "messages", Description: "Show the main office feed", Category: "navigate"},
	{Name: "inbox", Description: "Show the selected agent inbox lane in 1:1 mode", Category: "navigate"},
	{Name: "outbox", Description: "Show the selected agent outbox lane in 1:1 mode", Category: "navigate"},
	{Name: "recover", Description: "Open the session recovery summary", Category: "navigate"},
	{Name: "resume", Description: "Alias for /recover", Category: "navigate"},
	{Name: "rewind", Description: "Insert a summarize-from-here recovery prompt", Category: "navigate"},
	{Name: "search", Description: "Search channels, tasks, requests, and threads", Category: "navigate"},
	{Name: "insert", Description: "Insert a channel, task, request, or message reference", Category: "navigate"},
	{Name: "switcher", Description: "Open the unified office/direct switcher", Category: "navigate"},
	{Name: "tasks", Description: "Show active work in this channel", Category: "navigate"},
	{Name: "switch", Description: "Switch to another channel", Category: "navigate"},
	{Name: "channels", Description: "Browse and manage channels", Category: "navigate"},
	{Name: "channel", Description: "Create or remove a channel", Category: "channels"},
	{Name: "agents", Description: "Manage channel agents", Category: "people"},
	{Name: "agent", Description: "Add, remove, enable, or disable an agent", Category: "people"},
	{Name: "agent prompt", Description: "Generate a new agent from a prompt", Category: "people"},
	{Name: "task", Description: "Claim, release, or complete a task", Category: "work"},
	{Name: "policies", Description: "Show signals, decisions, external actions, and watchdogs", Category: "navigate"},
	{Name: "calendar", Description: "Show the office schedule and team calendars", Category: "navigate"},
	{Name: "queue", Description: "Alias for /calendar", Category: "navigate"},
	{Name: "artifacts", Description: "Show recent task logs, approvals, and workflow artifacts", Category: "navigate"},
	{Name: "skills", Description: "Show available skills", Category: "navigate"},
	{Name: "skill", Description: "Create, invoke, or manage a skill", Category: "work"},
	{Name: "reply", Description: "Reply in thread by message ID", Category: "conversation"},
	{Name: "threads", Description: "Browse and manage threads", Category: "conversation"},
	{Name: "expand", Description: "Expand a collapsed thread", Category: "conversation"},
	{Name: "collapse", Description: "Collapse a thread", Category: "conversation"},
	{Name: "cancel", Description: "Exit reply/setup/doctor mode", Category: "conversation"},
	{Name: "collab", Description: "Switch to collaborative mode (all agents see all messages)", Category: "session"},
	{Name: "focus", Description: "Switch to delegation mode (CEO routes, specialists execute)", Category: "session"},
	{Name: "reset", Description: "Reset channel and agents", Category: "session"},
	{Name: "reset-dm", Description: "Clear direct messages with an agent", Category: "session"},
	{Name: "quit", Description: "Exit WUPHF", Category: "session"},
}

// oneOnOneBlacklist lists command names blocked in 1:1 mode.
var oneOnOneBlacklist = map[string]bool{
	"tasks":        true,
	"task":         true,
	"channels":     true,
	"channel":      true,
	"agents":       true,
	"agent":        true,
	"agent prompt": true,
	"reply":        true,
	"threads":      true,
	"expand":       true,
	"collapse":     true,
	"collab":       true,
	"focus":        true,
}

func buildOneOnOneSlashCommands() []tui.SlashCommand {
	var cmds []tui.SlashCommand
	for _, cmd := range channelSlashCommands {
		if oneOnOneBlacklist[cmd.Name] {
			continue
		}
		cmds = append(cmds, cmd)
	}
	return cmds
}

type channelPickerMode string

const (
	channelPickerNone           channelPickerMode = ""
	channelPickerInitProvider   channelPickerMode = "init_provider"
	channelPickerInitPack       channelPickerMode = "init_pack"
	channelPickerProvider       channelPickerMode = "provider"
	channelPickerIntegrations   channelPickerMode = "integrations"
	channelPickerRequests       channelPickerMode = "requests"
	channelPickerTasks          channelPickerMode = "tasks"
	channelPickerTaskAction     channelPickerMode = "task_action"
	channelPickerRequestAction  channelPickerMode = "request_action"
	channelPickerThreads        channelPickerMode = "threads"
	channelPickerThreadAction   channelPickerMode = "thread_action"
	channelPickerChannels       channelPickerMode = "channels"
	channelPickerSwitcher       channelPickerMode = "switcher"
	channelPickerInsert         channelPickerMode = "insert"
	channelPickerSearch         channelPickerMode = "search"
	channelPickerRewind         channelPickerMode = "rewind"
	channelPickerAgents         channelPickerMode = "agents"
	channelPickerCalendarAgent  channelPickerMode = "calendar_agent"
	channelPickerOneOnOneMode   channelPickerMode = "one_on_one_mode"
	channelPickerOneOnOneAgent  channelPickerMode = "one_on_one_agent"
	channelPickerTelegramGroup  channelPickerMode = "telegram_group"
	channelPickerConnect        channelPickerMode = "connect"
	channelPickerTelegramToken  channelPickerMode = "telegram_token"
	channelPickerTelegramChatID channelPickerMode = "telegram_chat_id"
)

type officeApp string

const (
	officeAppMessages  officeApp = "messages"
	officeAppInbox     officeApp = "inbox"
	officeAppOutbox    officeApp = "outbox"
	officeAppRecovery  officeApp = "recovery"
	officeAppTasks     officeApp = "tasks"
	officeAppRequests  officeApp = "requests"
	officeAppPolicies  officeApp = "policies"
	officeAppCalendar  officeApp = "calendar"
	officeAppArtifacts officeApp = "artifacts"
	officeAppSkills    officeApp = "skills"
)

type quickJumpTarget string

const (
	quickJumpNone     quickJumpTarget = ""
	quickJumpChannels quickJumpTarget = "channels"
	quickJumpApps     quickJumpTarget = "apps"
)

type calendarRange string

const (
	calendarRangeDay  calendarRange = "day"
	calendarRangeWeek calendarRange = "week"
)

type channelIntegrationSpec struct {
	Label       string
	Value       string
	Type        string
	Provider    string
	Description string
}

var channelIntegrationSpecs = []channelIntegrationSpec{
	{Label: "Gmail", Value: "gmail", Type: "email", Provider: "google", Description: "Connect Google email"},
	{Label: "Google Calendar", Value: "google-calendar", Type: "calendar", Provider: "google", Description: "Connect Google Calendar and the WUPHF Meeting Bot"},
	{Label: "Outlook", Value: "outlook", Type: "email", Provider: "microsoft", Description: "Connect Microsoft email"},
	{Label: "Outlook Calendar", Value: "outlook-calendar", Type: "calendar", Provider: "microsoft", Description: "Connect Outlook Calendar and the WUPHF Meeting Bot"},
	{Label: "Slack", Value: "slack", Type: "messaging", Provider: "slack", Description: "Connect Slack workspace messaging"},
	{Label: "Salesforce", Value: "salesforce", Type: "crm", Provider: "salesforce", Description: "Connect Salesforce CRM"},
	{Label: "HubSpot", Value: "hubspot", Type: "crm", Provider: "hubspot", Description: "Connect HubSpot CRM"},
	{Label: "Attio", Value: "attio", Type: "crm", Provider: "attio", Description: "Connect Attio CRM"},
}

// focusArea identifies which panel currently owns keyboard input.
type focusArea int

const (
	focusMain    focusArea = 0
	focusSidebar focusArea = 1
	focusThread  focusArea = 2
)

type channelModel struct {
	messages             []brokerMessage
	members              []channelMember
	officeMembers        []officeMemberInfo
	channels             []channelInfo
	requests             []channelInterview
	tasks                []channelTask
	actions              []channelAction
	signals              []channelSignal
	decisions            []channelDecision
	watchdogs            []channelWatchdog
	scheduler            []channelSchedulerJob
	skills               []channelSkill
	pending              *channelInterview
	lastID               string
	activeChannel        string
	activeApp            officeApp
	replyToID            string
	expandedThreads      map[string]bool
	clickableThreads     map[int]string // rendered line index → message ID for click-to-expand
	threadsDefaultExpand bool           // true = expand threads by default
	tickFrame            int            // incremented each tick for animations
	autocomplete         tui.AutocompleteModel
	mention              tui.MentionModel
	input                []rune
	inputPos             int
	inputHistory         composerHistory
	width                int
	height               int
	scroll               int
	unreadCount          int
	unreadAnchorID       string
	awaySummary          string
	posting              bool
	selectedOption       int
	notice               string
	noticeExpireAt       time.Time
	snoozedInterview     string
	confirm              *channelConfirm
	doctor               *channelDoctorReport
	memberDraft          *channelMemberDraft
	initFlow             tui.InitFlowModel
	picker               tui.PickerModel
	pickerMode           channelPickerMode

	// 3-column layout state
	focus               focusArea
	sidebarCollapsed    bool
	sidebarCursor       int
	sidebarRosterOffset int
	threadPanelOpen     bool
	threadPanelID       string
	threadInput         []rune
	threadInputPos      int
	threadInputHistory  composerHistory
	threadScroll        int
	usage               channelUsageState
	brokerConnected     bool
	sessionMode         string
	oneOnOneAgent       string
	lastCtrlCAt         time.Time
	quickJumpTarget     quickJumpTarget
	calendarRange       calendarRange
	calendarFilter      string

	// Telegram connect flow state
	telegramGroups []team.TelegramGroup
	telegramToken  string

	// lastAgentContent tracks the latest streaming text per agent for sidebar display.
	lastAgentContent map[string]string
}

func newChannelModel(threadsCollapsed bool) channelModel {
	return newChannelModelWithApp(threadsCollapsed, officeAppMessages)
}

func newChannelModelWithApp(threadsCollapsed bool, initialApp officeApp) channelModel {
	manifest, _ := company.LoadManifest()
	officeMembers := officeMembersFromManifest(manifest)
	channels := channelInfosFromManifest(manifest)
	sessionMode := team.SessionModeOffice
	oneOnOneAgent := ""
	if strings.EqualFold(strings.TrimSpace(os.Getenv("WUPHF_ONE_ON_ONE")), "1") || strings.EqualFold(strings.TrimSpace(os.Getenv("WUPHF_ONE_ON_ONE")), "true") {
		sessionMode = team.SessionModeOneOnOne
		oneOnOneAgent = strings.TrimSpace(os.Getenv("WUPHF_ONE_ON_ONE_AGENT"))
		if oneOnOneAgent == "" {
			oneOnOneAgent = team.DefaultOneOnOneAgent
		}
		initialApp = officeAppMessages
	}
	officeDirectory = make(map[string]officeMemberInfo, len(officeMembers))
	for _, member := range officeMembers {
		officeDirectory[member.Slug] = member
	}
	m := channelModel{
		expandedThreads:      make(map[string]bool),
		threadsDefaultExpand: !threadsCollapsed,
		autocomplete:         tui.NewAutocomplete(channelSlashCommands),
		mention:              tui.NewMention(channelMentionAgents(nil)),
		inputHistory:         newComposerHistory(),
		initFlow:             tui.NewInitFlow(),
		activeChannel:        "general",
		activeApp:            initialApp,
		calendarRange:        calendarRangeWeek,
		officeMembers:        officeMembers,
		channels:             channels,
		sessionMode:          sessionMode,
		oneOnOneAgent:        oneOnOneAgent,
		threadInputHistory:   newComposerHistory(),
		lastAgentContent:     make(map[string]string),
	}
	if m.isOneOnOne() {
		m.sidebarCollapsed = true
		m.threadsDefaultExpand = true
		m.autocomplete = tui.NewAutocomplete(buildOneOnOneSlashCommands())
		m.notice = "Direct session reset. Agent pane reloaded in place."
	}
	if config.ResolveNoNex() {
		m.notice = "Running in office-only mode. Nex tools are disabled for this session."
	} else if config.ResolveAPIKey("") == "" {
		m.notice = "No WUPHF API key configured. Starting setup..."
		m.initFlow, _ = m.initFlow.Start()
	}
	m.syncSidebarCursorToActive()
	return m
}

// setTransientNotice sets a notice that auto-clears after the next few ticks.
func (m *channelModel) setTransientNotice(text string) {
	m.notice = text
	m.noticeExpireAt = time.Now().Add(4 * time.Second)
}

func (m channelModel) isOneOnOne() bool {
	return team.NormalizeSessionMode(m.sessionMode) == team.SessionModeOneOnOne
}

func (m channelModel) oneOnOneAgentSlug() string {
	return team.NormalizeOneOnOneAgent(m.oneOnOneAgent)
}

func (m channelModel) oneOnOneAgentName() string {
	slug := m.oneOnOneAgentSlug()
	for _, member := range mergeOfficeMembers(m.officeMembers, m.members, nil) {
		if member.Slug == slug && strings.TrimSpace(member.Name) != "" {
			return member.Name
		}
	}
	return displayName(slug)
}

func (m *channelModel) refreshSlashCommands() {
	var activeInput []rune
	activeCursor := 0
	preserveOverlays := false
	if m.focus == focusThread && m.threadPanelOpen {
		activeInput = append([]rune(nil), m.threadInput...)
		activeCursor = m.threadInputPos
		preserveOverlays = true
	} else if m.focus == focusMain {
		activeInput = append([]rune(nil), m.input...)
		activeCursor = m.inputPos
		preserveOverlays = true
	}
	var base []tui.SlashCommand
	if m.isOneOnOne() {
		base = buildOneOnOneSlashCommands()
	} else {
		base = make([]tui.SlashCommand, len(channelSlashCommands))
		copy(base, channelSlashCommands)
	}
	var skillCommands []tui.SlashCommand
	for _, sk := range m.skills {
		if sk.Status != "active" {
			continue
		}
		skillCommands = append(skillCommands, tui.SlashCommand{
			Name:        sk.Name,
			Description: sk.Description,
			Category:    "skills",
		})
	}
	base = append(skillCommands, base...)
	m.autocomplete = tui.NewAutocomplete(base)
	if preserveOverlays {
		m.updateOverlaysForInput(activeInput, activeCursor)
		return
	}
	m.updateOverlaysForCurrentInput()
}

func (m channelModel) pollCurrentState() tea.Cmd {
	if m.isOneOnOne() {
		return tea.Sequence(
			pollHealth(),
			pollBroker(m.lastID, m.activeChannel),
			pollMembers(m.activeChannel),
			tickChannel(),
		)
	}
	return tea.Sequence(
		pollHealth(),
		pollChannels(),
		pollOfficeMembers(),
		pollBroker(m.lastID, m.activeChannel),
		pollMembers(m.activeChannel),
		pollRequests(m.activeChannel),
		pollTasks(m.activeChannel),
		pollSkills(""),
		pollOfficeLedger(),
		pollUsage(),
		tickChannel(),
	)
}

func (m channelModel) Init() tea.Cmd {
	m.lastID = ""
	return m.pollCurrentState()
}

func (m channelModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, tea.ClearScreen

	case tea.MouseMsg:
		layout := computeLayout(m.width, m.height, m.threadPanelOpen, m.sidebarCollapsed)
		inSidebar := layout.ShowSidebar && msg.X < layout.SidebarW
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.focus == focusThread && m.threadPanelOpen {
				m.threadScroll++
			} else if inSidebar {
				if m.sidebarRosterOffset > 0 {
					m.sidebarRosterOffset--
				}
			} else {
				m.scroll++
			}
		case tea.MouseButtonWheelDown:
			if m.focus == focusThread && m.threadPanelOpen {
				if m.threadScroll > 0 {
					m.threadScroll--
				}
			} else if inSidebar {
				m.sidebarRosterOffset++
			} else {
				if m.scroll > 0 {
					m.scroll--
					if m.scroll == 0 {
						m.clearUnreadState()
					}
				}
			}
		case tea.MouseButtonLeft:
			if action, ok := m.mouseActionAt(msg.X, msg.Y); ok {
				switch action.Kind {
				case "focus":
					switch action.Value {
					case "sidebar":
						m.focus = focusSidebar
					case "thread":
						m.focus = focusThread
					default:
						m.focus = focusMain
					}
					m.updateOverlaysForCurrentInput()
					return m, nil
				case "thread":
					m.threadPanelOpen = true
					m.threadPanelID = action.Value
					m.replyToID = action.Value
					m.focus = focusThread
					m.threadScroll = 0
					m.notice = fmt.Sprintf("Replying in thread %s", action.Value)
					return m, nil
				case "jump-latest":
					m.scroll = 0
					m.clearUnreadState()
					return m, nil
				case "autocomplete":
					if idx, ok := popupActionIndex(action.Value); ok {
						for m.autocomplete.SelectedIndex() != idx {
							m.autocomplete.Next()
						}
						if name := m.autocomplete.Accept(); name != "" {
							return m.runActiveCommand("/" + name)
						}
					}
					return m, nil
				case "mention":
					if idx, ok := popupActionIndex(action.Value); ok {
						for m.mention.SelectedIndex() != idx {
							m.mention.Next()
						}
						if mention := m.mention.Accept(); mention != "" {
							m.insertAcceptedMention(mention)
						}
					}
					return m, nil
				case "task":
					if task, ok := m.findTaskByID(action.Value); ok {
						m.focus = focusMain
						return m, m.openTaskActionPicker(task)
					}
					return m, nil
				case "request":
					if req, ok := m.findRequestByID(action.Value); ok {
						m.focus = focusMain
						return m, m.openRequestActionPicker(req)
					}
					return m, nil
				case "prompt":
					m.focus = focusMain
					m.applyRecoveryPrompt(action.Value)
					return m, nil
				case "channel", "app":
					items := m.sidebarItems()
					for idx, item := range items {
						if item.Kind == action.Kind && item.Value == action.Value {
							m.sidebarCursor = idx
							break
						}
					}
					m.focus = focusSidebar
					return m, m.selectSidebarItem(sidebarItem{Kind: action.Kind, Value: action.Value})
				}
			}
		}
		return m, nil

	case tea.KeyMsg:
		// ── Global keys (always active) ───────────────────────────────
		key := msg.String()
		if msg.Type == tea.KeyCtrlJ {
			key = "ctrl+j"
		}
		switch key {
		case "ctrl+c":
			now := time.Now()
			if !m.lastCtrlCAt.IsZero() && now.Sub(m.lastCtrlCAt) <= 2*time.Second {
				killTeamSession()
				return m, tea.Quit
			}
			m.lastCtrlCAt = now
			m.setTransientNotice("Press Ctrl+C again to quit WUPHF.")
			return m, nil
		case "ctrl+b":
			if m.isOneOnOne() {
				return m, nil
			}
			m.sidebarCollapsed = !m.sidebarCollapsed
			return m, nil
		case "ctrl+g":
			if m.isOneOnOne() {
				m.setTransientNotice("1:1 mode has no channel sidebar.")
				return m, nil
			}
			if m.quickJumpTarget == quickJumpChannels {
				m.quickJumpTarget = quickJumpNone
			} else {
				m.quickJumpTarget = quickJumpChannels
				m.setTransientNotice("Quick nav: 1-9 switches channels.")
			}
			return m, nil
		case "ctrl+o":
			if m.isOneOnOne() {
				m.setTransientNotice("1:1 mode is just the direct conversation.")
				return m, nil
			}
			if m.quickJumpTarget == quickJumpApps {
				m.quickJumpTarget = quickJumpNone
			} else {
				m.quickJumpTarget = quickJumpApps
				m.setTransientNotice("Quick nav: 1-9 switches office apps.")
			}
			return m, nil
		case "ctrl+d":
			// Return to #general from a DM channel.
			if strings.HasPrefix(m.activeChannel, "dm-") {
				m.activeChannel = "general"
				m.lastID = ""
				m.messages = nil
				m.setTransientNotice("Back to #general")
				return m, pollBroker("", m.activeChannel)
			}
			return m, nil
		}

		if m.quickJumpTarget != quickJumpNone {
			target := m.quickJumpTarget
			items := m.quickJumpItems()
			switch msg.String() {
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				idx := int(msg.String()[0] - '1')
				m.quickJumpTarget = quickJumpNone
				if idx >= 0 && idx < len(items) {
					m.setSidebarCursorForItem(items[idx])
					return m, m.selectSidebarItem(items[idx])
				}
				if target == quickJumpChannels {
					m.setTransientNotice("No channel on that number.")
				} else {
					m.setTransientNotice("No app on that number.")
				}
				return m, nil
			case "esc":
				m.quickJumpTarget = quickJumpNone
			default:
				m.quickJumpTarget = quickJumpNone
			}
		}

		// ── Esc: close overlays/thread, then cycle ────────────────────
		if msg.String() == "esc" {
			switch m.activeInteractionContext() {
			case contextConfirm:
				if m.confirm != nil && m.confirm.Action == confirmActionSubmitRequest {
					m.confirm = nil
					m.notice = "Review closed. Keep editing before you send."
					return m, nil
				}
				m.confirm = nil
				m.notice = "Canceled."
				return m, nil
			case contextPicker:
				m.picker.SetActive(false)
				if m.pickerMode == channelPickerIntegrations {
					m.notice = "Integration canceled."
				} else {
					m.initFlow = tui.NewInitFlow()
					m.notice = "Setup canceled."
				}
				m.pickerMode = channelPickerNone
				return m, nil
			case contextAutocomplete, contextMention:
				var cmd tea.Cmd
				m.autocomplete, cmd = m.autocomplete.Update(msg)
				_ = cmd
				m.mention, _ = m.mention.Update(msg)
				return m, nil
			case contextMemberDraft:
				m.memberDraft = nil
				m.input = nil
				m.inputPos = 0
				m.notice = "Agent setup canceled."
				return m, nil
			case contextDoctor:
				m.doctor = nil
				m.notice = "Doctor closed."
				return m, nil
			case contextInterview:
				if m.pending.Blocking || m.pending.Required {
					m.notice = "Human decision required. Answer it before the team can continue."
					return m, nil
				}
				m.snoozedInterview = m.pending.ID
				m.notice = "Request snoozed. Team remains paused until it is answered."
				return m, nil
			case contextThread:
				m.threadPanelOpen = false
				m.threadPanelID = ""
				m.threadInput = nil
				m.threadInputPos = 0
				m.threadScroll = 0
				if m.focus == focusThread {
					m.focus = focusMain
				}
				return m, nil
			}
			// Nothing to close — fire human interrupt to pause the whole team
			if m.pending == nil {
				m.posting = true
				m.notice = "Pausing team..."
				return m, postHumanInterrupt(m.activeChannel)
			}
			return m, nil
		}

		// ── Tab: cycle focus 0→1→2→0 (only visible panels) ───────────
		if msg.String() == "tab" && !m.autocomplete.IsVisible() && !m.mention.IsVisible() && !m.picker.IsActive() {
			m.focus = m.nextFocus()
			m.quickJumpTarget = quickJumpNone
			m.updateOverlaysForCurrentInput()
			return m, nil
		}

		// ── Global overlays/pickers before panel-specific handling ────
		if m.confirm != nil {
			switch msg.String() {
			case "enter":
				return m.executeConfirmation(*m.confirm)
			case "ctrl+c", "esc":
				m.confirm = nil
				m.notice = "Canceled."
				return m, nil
			default:
				return m, nil
			}
		}
		if m.picker.IsActive() {
			var cmd tea.Cmd
			m.picker, cmd = m.picker.Update(msg)
			return m, cmd
		}
		if m.initFlow.IsActive() && m.initFlow.Phase() == tui.InitAPIKey {
			var cmd tea.Cmd
			m.initFlow, cmd = m.initFlow.Update(msg)
			return m, cmd
		}
		if m.autocomplete.IsVisible() {
			switch msg.String() {
			case "tab":
				if name := m.autocomplete.Accept(); name != "" {
					m.setActiveInput("/" + name + " ")
				}
				return m, nil
			case "enter":
				if name := m.autocomplete.Accept(); name != "" {
					return m.runActiveCommand("/" + name)
				}
			case "up", "down", "shift+tab":
				var cmd tea.Cmd
				m.autocomplete, cmd = m.autocomplete.Update(msg)
				_ = cmd
				return m, nil
			default:
				var cmd tea.Cmd
				m.autocomplete, cmd = m.autocomplete.Update(msg)
				_ = cmd
			}
		}
		if m.mention.IsVisible() {
			switch msg.String() {
			case "tab", "enter":
				if mention := m.mention.Accept(); mention != "" {
					m.insertAcceptedMention(mention)
				}
				return m, nil
			case "up", "down", "shift+tab":
				var cmd tea.Cmd
				m.mention, cmd = m.mention.Update(msg)
				_ = cmd
				return m, nil
			default:
				var cmd tea.Cmd
				m.mention, cmd = m.mention.Update(msg)
				_ = cmd
			}
		}

		if m.focus == focusMain && m.activeApp == officeAppCalendar && len(m.input) == 0 && !m.posting {
			switch msg.String() {
			case "d":
				m.calendarRange = calendarRangeDay
				m.notice = "Calendar now shows today."
				return m, nil
			case "w":
				m.calendarRange = calendarRangeWeek
				m.notice = "Calendar now shows this week."
				return m, nil
			case "f":
				options := m.buildCalendarAgentPickerOptions()
				if len(options) == 0 {
					m.notice = "No teammate filters available."
					return m, nil
				}
				m.picker = tui.NewPicker("Filter Calendar", options)
				m.picker.SetActive(true)
				m.pickerMode = channelPickerCalendarAgent
				return m, nil
			case "a":
				m.calendarFilter = ""
				m.notice = "Showing all teammate calendars."
				return m, nil
			}
		}

		// ── Route by focus area ───────────────────────────────────────
		if m.focus == focusThread && m.threadPanelOpen {
			return m.updateThread(msg)
		}
		if m.focus == focusSidebar && !m.sidebarCollapsed {
			return m.updateSidebar(msg)
		}

		// ── focusMain: existing behavior ──────────────────────────────
		if motionKey, ok := composerMotionKey(msg); ok {
			m.lastCtrlCAt = time.Time{}
			if nextPos, handled := moveComposerCursor(m.input, m.inputPos, motionKey); handled {
				m.inputPos = nextPos
				m.updateInputOverlays()
			}
			return m, nil
		}
		switch msg.String() {
		case "enter":
			m.lastCtrlCAt = time.Time{}
			if m.memberDraft != nil {
				return m.submitMemberDraft()
			}
			if len(m.input) > 0 {
				text := string(m.input)
				trimmed := strings.TrimSpace(text)
				m.inputHistory.record(m.input, m.inputPos)
				if trimmed == "/quit" || trimmed == "/exit" || trimmed == "/q" {
					killTeamSession()
					return m, tea.Quit
				}
				if strings.HasPrefix(trimmed, "/") {
					return m.runActiveCommand(trimmed)
				}
				if m.pending != nil {
					m.confirm = confirmationForInterviewAnswer(*m.pending, m.selectedInterviewOption(), text)
					m.notice = "Review your answer before sending."
					return m, nil
				}

				m.input = nil
				m.inputPos = 0
				m.notice = ""
				m.posting = true
				return m, postToChannel(text, m.replyToID, m.activeChannel)
			}
			if m.pending != nil {
				if opt := m.selectedInterviewOption(); opt != nil {
					if interviewOptionRequiresText(opt) {
						m.notice = interviewOptionTextHint(opt)
						return m, nil
					}
					m.confirm = confirmationForInterviewAnswer(*m.pending, opt, "")
					m.notice = "Review your answer before sending."
					return m, nil
				}
				m.notice = "Choose an option or type your own answer before sending."
				return m, nil
			}
		case "backspace":
			m.lastCtrlCAt = time.Time{}
			if m.inputPos > 0 {
				m.inputHistory.resetRecall()
				m.input = append(m.input[:m.inputPos-1], m.input[m.inputPos:]...)
				m.inputPos--
				m.updateInputOverlays()
			}
		case "ctrl+u":
			m.lastCtrlCAt = time.Time{}
			m.inputHistory.resetRecall()
			m.input = nil
			m.inputPos = 0
			m.updateInputOverlays()
		case "ctrl+p":
			m.lastCtrlCAt = time.Time{}
			if snapshot, ok := m.inputHistory.previous(m.input, m.inputPos); ok {
				m.restoreMainSnapshot(snapshot)
			}
		case "ctrl+n":
			m.lastCtrlCAt = time.Time{}
			if snapshot, ok := m.inputHistory.next(); ok {
				m.restoreMainSnapshot(snapshot)
			}
		case "ctrl+a":
			m.lastCtrlCAt = time.Time{}
			m.inputPos = 0
			m.updateInputOverlays()
		case "ctrl+e":
			m.lastCtrlCAt = time.Time{}
			m.inputPos = len(m.input)
			m.updateInputOverlays()
		case "ctrl+j":
			m.lastCtrlCAt = time.Time{}
			m.inputHistory.resetRecall()
			ch := []rune{'\n'}
			tail := make([]rune, len(m.input[m.inputPos:]))
			copy(tail, m.input[m.inputPos:])
			m.input = append(m.input[:m.inputPos], append(ch, tail...)...)
			m.inputPos++
			m.updateInputOverlays()
		case "left":
			m.lastCtrlCAt = time.Time{}
			if m.inputPos > 0 {
				m.inputPos--
				m.updateInputOverlays()
			}
		case "right":
			m.lastCtrlCAt = time.Time{}
			if m.inputPos < len(m.input) {
				m.inputPos++
				m.updateInputOverlays()
			}
		case "up":
			m.lastCtrlCAt = time.Time{}
			if m.pending != nil && m.selectedOption > 0 {
				m.selectedOption--
			} else {
				m.scroll++
			}
		case "down":
			m.lastCtrlCAt = time.Time{}
			if m.pending != nil && m.selectedOption < m.interviewOptionCount()-1 {
				m.selectedOption++
			} else {
				m.scroll--
				if m.scroll < 0 {
					m.scroll = 0
				}
			}
		case "home":
			m.lastCtrlCAt = time.Time{}
			m.scroll = 1 << 30
		case "end":
			m.lastCtrlCAt = time.Time{}
			m.scroll = 0
			m.clearUnreadState()
		case "pgup":
			m.lastCtrlCAt = time.Time{}
			m.scroll += maxInt(10, m.height/2)
		case "pgdown":
			m.lastCtrlCAt = time.Time{}
			m.scroll -= maxInt(10, m.height/2)
			if m.scroll < 0 {
				m.scroll = 0
			}
			if m.scroll == 0 {
				m.clearUnreadState()
			}
		default:
			m.lastCtrlCAt = time.Time{}
			if ch := composerInsertRunes(msg); len(ch) > 0 {
				m.inputHistory.resetRecall()
				m.input, m.inputPos = insertComposerRunes(m.input, m.inputPos, ch)
				m.updateInputOverlays()
			} else if len(msg.String()) == 1 || msg.Type == tea.KeyRunes {
				ch := msg.Runes
				if len(ch) == 0 {
					ch = []rune(msg.String())
				}
				if len(ch) > 0 {
					m.inputHistory.resetRecall()
					tail := make([]rune, len(m.input[m.inputPos:]))
					copy(tail, m.input[m.inputPos:])
					m.input = append(m.input[:m.inputPos], append(ch, tail...)...)
					m.inputPos += len(ch)
					m.updateInputOverlays()
				}
			}
			if m.maybeActivateChannelPickerFromInput() {
				return m, nil
			}
		}

	case channelPostDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Send failed: " + msg.err.Error()
		} else if strings.TrimSpace(msg.notice) != "" {
			m.notice = msg.notice
		} else if m.replyToID != "" {
			m.notice = fmt.Sprintf("Reply sent to %s. Use /cancel to leave the thread.", m.replyToID)
		}
		switch strings.TrimSpace(msg.action) {
		case "create":
			if slug := normalizeSidebarSlug(msg.slug); slug != "" {
				m.activeChannel = slug
				m.activeApp = officeAppMessages
				m.messages = nil
				m.members = nil
				m.tasks = nil
				m.requests = nil
				m.lastID = ""
				m.replyToID = ""
				m.threadPanelOpen = false
				m.threadPanelID = ""
				m.scroll = 0
				m.clearUnreadState()
				m.syncSidebarCursorToActive()
			}
		case "remove":
			if normalizeSidebarSlug(msg.slug) == normalizeSidebarSlug(m.activeChannel) {
				m.activeChannel = "general"
				m.activeApp = officeAppMessages
				m.messages = nil
				m.members = nil
				m.tasks = nil
				m.requests = nil
				m.lastID = ""
				m.replyToID = ""
				m.threadPanelOpen = false
				m.threadPanelID = ""
				m.scroll = 0
				m.clearUnreadState()
				m.syncSidebarCursorToActive()
			}
		}
		return m, tea.Batch(pollChannels(), pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel), pollOfficeLedger())

	case channelInterviewAnswerDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Request answer failed: " + msg.err.Error()
		} else {
			m.pending = nil
			m.input = nil
			m.inputPos = 0
			return m, tea.Batch(pollBroker("", m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel), pollOfficeLedger())
		}

	case channelInterruptDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Failed to pause team: " + msg.err.Error()
		} else {
			m.notice = "Team paused. Answer the interrupt to resume."
		}
		return m, tea.Batch(pollRequests(m.activeChannel), pollBroker(m.lastID, m.activeChannel))

	case channelResetDoneMsg:
		m.posting = false
		m.confirm = nil
		if msg.err == nil {
			if normalized := team.NormalizeSessionMode(msg.sessionMode); normalized != "" {
				m.sessionMode = normalized
			}
			if strings.TrimSpace(msg.oneOnOneAgent) != "" || m.sessionMode == team.SessionModeOneOnOne {
				m.oneOnOneAgent = team.NormalizeOneOnOneAgent(msg.oneOnOneAgent)
			}
			m.messages = nil
			m.members = nil
			m.requests = nil
			m.pending = nil
			m.lastID = ""
			m.replyToID = ""
			m.expandedThreads = make(map[string]bool)
			m.input = nil
			m.inputPos = 0
			m.scroll = 0
			m.clearUnreadState()
			m.notice = ""
			m.initFlow = tui.NewInitFlow()
			m.picker.SetActive(false)
			m.threadPanelOpen = false
			m.threadPanelID = ""
			m.threadInput = nil
			m.threadInputPos = 0
			m.threadScroll = 0
			m.focus = focusMain
			m.pickerMode = channelPickerNone
			m.snoozedInterview = ""
			m.doctor = nil
			m.tasks = nil
			m.actions = nil
			m.scheduler = nil
			m.refreshSlashCommands()
			if m.isOneOnOne() {
				m.activeApp = officeAppMessages
				m.sidebarCollapsed = true
				m.threadPanelOpen = false
				m.threadPanelID = ""
				m.replyToID = ""
			}
			m.notice = strings.TrimSpace(msg.notice)
			if m.notice == "" {
				m.notice = "Office reset. Team panes reloaded in place."
			}
			return m, m.pollCurrentState()
		} else {
			m.notice = "Reset failed: " + msg.err.Error()
		}

	case channelResetDMDoneMsg:
		m.posting = false
		m.confirm = nil
		if msg.err != nil {
			m.notice = "Failed to clear DMs: " + msg.err.Error()
		} else {
			m.notice = fmt.Sprintf("Cleared %d direct messages.", msg.removed)
			m.messages = nil
			m.lastID = ""
		}
		return m, m.pollCurrentState()

	case channelInitDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Setup failed: " + msg.err.Error()
		} else {
			m.notice = strings.TrimSpace(msg.notice)
			if m.notice == "" {
				m.notice = "Setup applied. Team reloaded with the new configuration."
			}
		}
		m.initFlow = tui.NewInitFlow()
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone

	case channelIntegrationDoneMsg:
		m.posting = false
		m.picker.SetActive(false)
		m.pickerMode = channelPickerNone
		if msg.err != nil {
			m.notice = "Integration failed: " + msg.err.Error()
		} else if msg.url != "" {
			m.notice = fmt.Sprintf("%s connected. Browser opened at %s", msg.label, msg.url)
		} else {
			m.notice = fmt.Sprintf("%s connected.", msg.label)
		}

	case channelDoctorDoneMsg:
		if msg.err != nil {
			m.notice = "Doctor failed: " + msg.err.Error()
			m.doctor = nil
		} else {
			report := msg.report
			m.doctor = &report
			m.notice = "Doctor: " + report.StatusLine()
		}

	case telegramDiscoverMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Telegram error: " + msg.err.Error()
			return m, nil
		}
		m.telegramToken = msg.token

		// Merge discovered groups with existing manifest channels
		allGroups := msg.groups
		manifest, _ := company.LoadManifest()
		for _, ch := range manifest.Channels {
			if ch.Surface == nil || ch.Surface.Provider != "telegram" || ch.Surface.RemoteID == "" || ch.Surface.RemoteID == "0" {
				continue
			}
			// Check if already discovered
			found := false
			for _, g := range allGroups {
				if fmt.Sprintf("%d", g.ChatID) == ch.Surface.RemoteID {
					found = true
					break
				}
			}
			if !found {
				chatID, _ := strconv.ParseInt(ch.Surface.RemoteID, 10, 64)
				if chatID != 0 {
					title := ch.Surface.RemoteTitle
					if title == "" {
						title = ch.Name
					}
					allGroups = append(allGroups, team.TelegramGroup{
						ChatID: chatID,
						Title:  title,
						Type:   "group",
					})
				}
			}
		}
		m.telegramGroups = allGroups

		// Build picker: DM + discovered groups + manual group entry
		options := []tui.PickerOption{
			{Label: "Direct message with Telegram bot", Value: "dm", Description: "Anyone can DM the bot to reach the office"},
		}
		for _, g := range allGroups {
			options = append(options, tui.PickerOption{
				Label:       g.Title,
				Value:       fmt.Sprintf("%d", g.ChatID),
				Description: fmt.Sprintf("Shared %s channel", g.Type),
			})
		}
		if len(allGroups) == 0 {
			options = append(options, tui.PickerOption{
				Label:       "Waiting for groups...",
				Value:       "retry",
				Description: "Add the bot to a Telegram group and send a message, then try again",
			})
		}
		m.picker = tui.NewPicker(fmt.Sprintf("Bot \"%s\" verified. Choose how to connect:", msg.botName), options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerTelegramGroup
		return m, nil

	case telegramConnectDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Telegram connect failed: " + msg.err.Error()
			return m, nil
		}
		m.notice = fmt.Sprintf("Connected \"%s\" as #%s. Restart WUPHF to activate the Telegram bridge.", msg.groupTitle, msg.channelSlug)
		m.activeChannel = msg.channelSlug
		m.activeApp = officeAppMessages
		m.messages = nil
		m.members = nil
		m.tasks = nil
		m.requests = nil
		m.lastID = ""
		m.replyToID = ""
		m.threadPanelOpen = false
		m.threadPanelID = ""
		m.scroll = 0
		m.clearUnreadState()
		m.syncSidebarCursorToActive()
		manifest, _ := company.LoadManifest()
		m.channels = channelInfosFromManifest(manifest)
		return m, tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollChannels())

	case channelMemberDraftDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Agent update failed: " + msg.err.Error()
		} else {
			m.notice = msg.notice
			m.memberDraft = nil
			m.input = nil
			m.inputPos = 0
			return m, tea.Batch(pollOfficeMembers(), pollChannels(), pollMembers(m.activeChannel), pollBroker("", m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel), pollOfficeLedger())
		}

	case channelTaskMutationDoneMsg:
		m.posting = false
		if msg.err != nil {
			m.notice = "Task update failed: " + msg.err.Error()
		} else if strings.TrimSpace(msg.notice) != "" {
			m.notice = msg.notice
		}
		return m, tea.Batch(pollTasks(m.activeChannel), pollOfficeLedger())

	case channelMsg:
		if len(msg.messages) > 0 {
			hadHistory := m.lastID != ""
			uniqueMessages, added := appendUniqueMessages(m.messages, msg.messages)
			if added == 0 {
				break
			}
			addedMessages := uniqueMessages[len(m.messages):]
			latestHumanFacing := latestHumanFacingMessage(addedMessages)
			if m.scroll > 0 {
				m.scroll += added
			}
			m.messages = uniqueMessages
			m.lastID = msg.messages[len(msg.messages)-1].ID
			// Track latest streaming text per agent for sidebar display.
			if m.lastAgentContent == nil {
				m.lastAgentContent = make(map[string]string)
			}
			for _, bm := range addedMessages {
				if bm.From != "" && bm.From != "you" && bm.From != "human" && bm.Content != "" {
					snippet := strings.TrimSpace(bm.Content)
					if len([]rune(snippet)) > 38 {
						runes := []rune(snippet)
						snippet = "…" + string(runes[len(runes)-37:])
					}
					m.lastAgentContent[bm.From] = snippet
				}
			}
			if m.scroll > 0 || m.focus != focusMain || m.threadPanelOpen {
				m.noteIncomingMessages(addedMessages)
			} else {
				m.clearUnreadState()
			}
			if latestHumanFacing != nil && hadHistory {
				m.activeApp = officeAppMessages
				m.notice = fmt.Sprintf("@%s has something for you", latestHumanFacing.From)
			}
		}

	case channelMembersMsg:
		m.members = msg.members
		// Overlay last-seen streaming content into LiveActivity when the broker
		// hasn't set it yet (e.g. between polls or when liveActivity is stale).
		if m.lastAgentContent != nil {
			for i, mem := range m.members {
				if snippet, ok := m.lastAgentContent[mem.Slug]; ok && snippet != "" && mem.LiveActivity == "" {
					m.members[i].LiveActivity = snippet
				}
			}
		}
		m.updateOverlaysForCurrentInput()

	case channelOfficeMembersMsg:
		if len(msg.members) == 0 {
			msg.members = officeMembersFallback(m.officeMembers)
		}
		m.officeMembers = msg.members
		officeDirectory = make(map[string]officeMemberInfo, len(msg.members))
		for _, member := range msg.members {
			officeDirectory[member.Slug] = member
		}
		m.updateOverlaysForCurrentInput()

	case channelChannelsMsg:
		if len(msg.channels) == 0 {
			msg.channels = channelInfosFallback(m.channels)
		}
		m.channels = msg.channels
		m.clampSidebarCursor()
		if m.activeChannel == "" {
			m.activeChannel = "general"
		}
		if !channelExists(msg.channels, m.activeChannel) && len(msg.channels) > 0 {
			m.activeChannel = msg.channels[0].Slug
			m.lastID = ""
			return m, tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollRequests(m.activeChannel))
		}

	case channelUsageMsg:
		m.usage = msg.usage
		if m.usage.Agents == nil {
			m.usage.Agents = make(map[string]channelUsageTotals)
		}

	case channelHealthMsg:
		m.brokerConnected = msg.Connected
		if msg.Connected {
			nextMode := team.NormalizeSessionMode(msg.SessionMode)
			nextAgent := team.NormalizeOneOnOneAgent(msg.OneOnOneAgent)
			modeChanged := nextMode != m.sessionMode || nextAgent != m.oneOnOneAgent
			m.sessionMode = nextMode
			m.oneOnOneAgent = nextAgent
			if m.isOneOnOne() {
				m.activeApp = officeAppMessages
				m.sidebarCollapsed = true
				m.threadPanelOpen = false
				m.threadPanelID = ""
				m.replyToID = ""
			}
			if modeChanged {
				m.messages = nil
				m.members = nil
				m.tasks = nil
				m.requests = nil
				m.lastID = ""
				m.scroll = 0
				m.clearUnreadState()
				m.refreshSlashCommands()
				if m.isOneOnOne() && strings.TrimSpace(m.notice) == "" {
					m.notice = "Direct session reset. Agent pane reloaded in place."
				}
				return m, m.pollCurrentState()
			}
		}

	case channelTasksMsg:
		m.tasks = msg.tasks

	case channelSkillsMsg:
		m.skills = msg.skills
		m.refreshSlashCommands()
		return m, nil

	case channelActionsMsg:
		m.actions = msg.actions

	case channelSignalsMsg:
		m.signals = msg.signals

	case channelDecisionsMsg:
		m.decisions = msg.decisions

	case channelWatchdogsMsg:
		m.watchdogs = msg.alerts

	case channelSchedulerMsg:
		m.scheduler = msg.jobs

	case tui.PickerSelectMsg:
		switch m.pickerMode {
		case channelPickerIntegrations:
			spec, ok := findChannelIntegration(msg.Value)
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			if !ok {
				m.notice = "Unknown integration selection."
				return m, nil
			}
			m.posting = true
			m.notice = fmt.Sprintf("Opening %s OAuth flow in your browser...", spec.Label)
			return m, connectIntegration(spec)
		case channelPickerChannels:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			switch {
			case strings.HasPrefix(msg.Value, "app:"):
				switch officeApp(strings.TrimPrefix(msg.Value, "app:")) {
				case officeAppMessages:
					m.activeApp = officeAppMessages
					m.notice = "Viewing messages."
					m.syncSidebarCursorToActive()
					return m, tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel))
				case officeAppTasks:
					m.activeApp = officeAppTasks
					m.notice = "Viewing tasks in #" + m.activeChannel + "."
					m.syncSidebarCursorToActive()
					return m, pollTasks(m.activeChannel)
				case officeAppRequests:
					m.activeApp = officeAppRequests
					m.notice = "Viewing requests in #" + m.activeChannel + "."
					m.syncSidebarCursorToActive()
					return m, pollRequests(m.activeChannel)
				case officeAppPolicies:
					m.activeApp = officeAppPolicies
					m.notice = "Viewing policies and decisions."
					m.syncSidebarCursorToActive()
					return m, pollOfficeLedger()
				case officeAppCalendar:
					m.activeApp = officeAppCalendar
					m.notice = "Viewing the office calendar."
					m.syncSidebarCursorToActive()
					return m, nil
				}
			case strings.HasPrefix(msg.Value, "session:1o1:"):
				agent := strings.TrimSpace(strings.TrimPrefix(msg.Value, "session:1o1:"))
				if agent == "" {
					agent = team.DefaultOneOnOneAgent
				}
				m.confirm = confirmationForSessionSwitch(team.SessionModeOneOnOne, agent)
				m.notice = "Confirm the direct session switch."
				return m, nil
			case msg.Value == "session:office":
				m.confirm = confirmationForSessionSwitch(team.SessionModeOffice, team.DefaultOneOnOneAgent)
				m.notice = "Confirm the session switch."
				return m, nil
			case strings.HasPrefix(msg.Value, "switch:"):
				m.activeChannel = strings.TrimPrefix(msg.Value, "switch:")
				m.lastID = ""
				m.messages = nil
				m.members = nil
				m.replyToID = ""
				m.threadPanelOpen = false
				m.threadPanelID = ""
				m.notice = "Switched to #" + m.activeChannel
				return m, tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel))
			case strings.HasPrefix(msg.Value, "remove:"):
				m.posting = true
				return m, mutateChannel("remove", strings.TrimPrefix(msg.Value, "remove:"), "")
			}
			return m, nil
		case channelPickerSwitcher:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			return m, m.applyWorkspaceSwitcherSelection(msg.Value)
		case channelPickerInsert:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			if strings.TrimSpace(msg.Value) == "" {
				m.notice = "Nothing inserted."
				return m, nil
			}
			m.insertIntoActiveComposer(msg.Value)
			m.notice = "Inserted reference into the composer."
			return m, nil
		case channelPickerSearch:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			return m, m.applySearchSelection(msg.Value, msg.Label)
		case channelPickerRewind:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			m.applyRecoveryPrompt(msg.Value)
			return m, nil
		case channelPickerAgents:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			if msg.Value == "create:new" {
				m.notice = "Use /agent create <slug> <Display Name> to add a new office member."
				return m, nil
			}
			parts := strings.SplitN(msg.Value, ":", 2)
			if len(parts) != 2 {
				return m, nil
			}
			if parts[0] == "edit" {
				draft, ok := m.startEditMemberDraft(parts[1])
				if !ok {
					m.notice = fmt.Sprintf("Office member %s not found.", parts[1])
					return m, nil
				}
				m.memberDraft = draft
				m.notice = "Editing teammate profile."
				return m, nil
			}
			m.posting = true
			return m, mutateChannelMember(m.activeChannel, parts[0], parts[1])
		case channelPickerRequests:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			for _, req := range m.requests {
				if req.ID == msg.Value {
					return m, m.openRequestActionPicker(req)
				}
			}
			return m, nil
		case channelPickerCalendarAgent:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			if msg.Value == "all" {
				m.calendarFilter = ""
				m.notice = "Showing all teammate calendars."
				return m, nil
			}
			m.calendarFilter = strings.TrimSpace(msg.Value)
			if m.calendarFilter == "" {
				m.notice = "Showing all teammate calendars."
			} else {
				m.notice = "Filtering calendar for " + displayName(m.calendarFilter) + "."
			}
			return m, nil
		case channelPickerOneOnOneMode:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			switch strings.TrimSpace(msg.Value) {
			case "enable":
				options := m.buildOneOnOneAgentPickerOptions()
				if len(options) == 0 {
					m.notice = "No office agents are available for direct mode."
					return m, nil
				}
				m.picker = tui.NewPicker("Choose Direct Agent", options)
				m.picker.SetActive(true)
				m.pickerMode = channelPickerOneOnOneAgent
				return m, nil
			case "disable":
				if !m.isOneOnOne() {
					m.notice = "Already running the full office team."
					return m, nil
				}
				m.confirm = confirmationForSessionSwitch(team.SessionModeOffice, team.DefaultOneOnOneAgent)
				m.notice = "Confirm the session switch."
				return m, nil
			default:
				return m, nil
			}
		case channelPickerOneOnOneAgent:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			agent := strings.TrimSpace(msg.Value)
			if agent == "" {
				agent = team.DefaultOneOnOneAgent
			}
			m.confirm = confirmationForSessionSwitch(team.SessionModeOneOnOne, agent)
			m.notice = "Confirm the direct session switch."
			return m, nil
		case channelPickerConnect:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			switch msg.Value {
			case "telegram":
				return m, m.startTelegramConnect()
			default:
				m.notice = msg.Label + " is not available yet."
				return m, nil
			}
		case channelPickerTelegramToken:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			token := strings.TrimSpace(msg.Value)
			if token == "" {
				m.notice = "Telegram connection canceled."
				return m, nil
			}
			_ = os.Setenv("WUPHF_TELEGRAM_BOT_TOKEN", token)
			config.SaveTelegramBotToken(token)
			m.posting = true
			m.notice = "Verifying bot token..."
			return m, discoverTelegramGroups(token)
		case channelPickerTelegramChatID:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			chatIDStr := strings.TrimSpace(msg.Value)
			if chatIDStr == "" {
				m.notice = "Canceled."
				return m, nil
			}
			chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
			if err != nil {
				m.notice = "Invalid chat ID. Must be a number like -5093020979."
				return m, nil
			}
			// Verify the chat exists using getChat
			title, verifyErr := team.VerifyChat(m.telegramToken, chatID)
			if verifyErr != nil {
				m.notice = "Could not verify chat: " + verifyErr.Error()
				return m, nil
			}
			if title == "" {
				title = fmt.Sprintf("Telegram %d", chatID)
			}
			m.posting = true
			m.notice = fmt.Sprintf("Connecting \"%s\"...", title)
			return m, connectTelegramGroup(m.telegramToken, team.TelegramGroup{
				ChatID: chatID,
				Title:  title,
				Type:   "group",
			})
		case channelPickerTelegramGroup:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone

			if msg.Value == "dm" {
				m.posting = true
				m.notice = "Setting up direct message channel..."
				dmGroup := team.TelegramGroup{ChatID: 0, Title: "Telegram DM", Type: "private"}
				return m, connectTelegramGroup(m.telegramToken, dmGroup)
			}

			if msg.Value == "retry" {
				m.posting = true
				m.notice = "Checking for groups..."
				return m, discoverTelegramGroups(m.telegramToken)
			}

			var selected *team.TelegramGroup
			for i := range m.telegramGroups {
				if fmt.Sprintf("%d", m.telegramGroups[i].ChatID) == msg.Value {
					selected = &m.telegramGroups[i]
					break
				}
			}
			if selected == nil {
				m.notice = "Unknown group selection."
				return m, nil
			}
			m.posting = true
			m.notice = fmt.Sprintf("Connecting \"%s\"...", selected.Title)
			return m, connectTelegramGroup(m.telegramToken, *selected)
		case channelPickerTasks:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			for _, task := range m.tasks {
				if task.ID == msg.Value {
					return m, m.openTaskActionPicker(task)
				}
			}
			return m, nil
		case channelPickerTaskAction:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			parts := strings.SplitN(msg.Value, ":", 2)
			if len(parts) != 2 {
				return m, nil
			}
			action, taskID := parts[0], parts[1]
			switch action {
			case "claim", "release", "complete", "block":
				m.posting = true
				return m, mutateTask(action, taskID, "you", m.activeChannel)
			case "open":
				if task, ok := m.findTaskByID(taskID); ok && task.ThreadID != "" {
					m.threadPanelOpen = true
					m.threadPanelID = task.ThreadID
					m.replyToID = task.ThreadID
				}
				return m, nil
			}
			return m, nil
		case channelPickerRequestAction:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			parts := strings.SplitN(msg.Value, ":", 2)
			if len(parts) != 2 {
				return m, nil
			}
			action, reqID := parts[0], parts[1]
			switch action {
			case "focus":
				if req, ok := m.findRequestByID(reqID); ok {
					return m.focusRequest(req, "Focused request "+req.ID)
				}
			case "answer":
				if req, ok := m.findRequestByID(reqID); ok {
					return m.answerRequest(req)
				}
			case "snooze":
				if req, ok := m.findRequestByID(reqID); ok && (req.Blocking || req.Required) {
					m.notice = "This decision cannot be snoozed. Answer it before the team continues."
					return m, nil
				}
				m.snoozedInterview = reqID
				m.notice = "Request snoozed."
				return m, nil
			case "open":
				if req, ok := m.findRequestByID(reqID); ok && req.ReplyTo != "" {
					m.threadPanelOpen = true
					m.threadPanelID = req.ReplyTo
					m.replyToID = req.ReplyTo
					m.notice = "Opened thread for request " + req.ID
				}
				return m, nil
			}
			return m, nil
		case channelPickerThreads:
			// User selected a thread — show action sub-picker
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			selectedMsgID := msg.Value
			actions := []tui.PickerOption{
				{Label: "Reply in thread", Value: "reply:" + selectedMsgID, Description: "Set reply mode for this thread"},
			}
			if m.expandedThreads[selectedMsgID] {
				actions = append(actions, tui.PickerOption{Label: "Collapse thread", Value: "collapse:" + selectedMsgID, Description: "Hide replies inline"})
			} else {
				actions = append(actions, tui.PickerOption{Label: "Expand thread", Value: "expand:" + selectedMsgID, Description: "Show replies inline"})
			}
			m.picker = tui.NewPicker("Thread: "+truncateText(msg.Label, 40), actions)
			m.picker.SetActive(true)
			m.pickerMode = channelPickerThreadAction
			return m, nil
		case channelPickerThreadAction:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			parts := strings.SplitN(msg.Value, ":", 2)
			if len(parts) != 2 {
				return m, nil
			}
			action, msgID := parts[0], parts[1]
			switch action {
			case "reply":
				m.replyToID = msgID
				m.expandedThreads[msgID] = true // auto-expand so you see the thread
				m.notice = fmt.Sprintf("Replying in thread %s — type your reply and press Enter", msgID)
			case "expand":
				m.expandedThreads[msgID] = true
			case "collapse":
				delete(m.expandedThreads, msgID)
			}
			return m, nil
		case channelPickerProvider:
			m.picker.SetActive(false)
			m.pickerMode = channelPickerNone
			m.posting = true
			return m, applyProviderSelection(msg.Value)
		default:
			m.picker.SetActive(false)
			var cmd tea.Cmd
			m.initFlow, cmd = m.initFlow.Update(msg)
			return m, cmd
		}

	case tui.InitFlowMsg:
		var cmd tea.Cmd
		m.initFlow, cmd = m.initFlow.Update(msg)
		switch m.initFlow.Phase() {
		case tui.InitProviderChoice:
			m.picker = tui.NewPicker("Choose LLM Provider", tui.ProviderOptions())
			m.picker.SetActive(true)
			m.pickerMode = channelPickerInitProvider
		case tui.InitPackChoice:
			m.picker = tui.NewPicker("Choose Agent Pack", tui.PackOptions())
			m.picker.SetActive(true)
			m.pickerMode = channelPickerInitPack
		case tui.InitDone:
			m.posting = true
			return m, tea.Batch(cmd, applyTeamSetup())
		}
		return m, cmd

	case channelRequestsMsg:
		prevID := ""
		if m.pending != nil {
			prevID = m.pending.ID
		}
		m.requests = msg.requests
		m.pending = msg.pending
		if m.pending == nil {
			m.snoozedInterview = ""
		}
		if m.pending != nil && m.pending.ID != prevID {
			m.selectedOption = m.recommendedOptionIndex()
			m.input = nil
			m.inputPos = 0
			m.snoozedInterview = ""
			if m.pending.Blocking || m.pending.Required {
				m.activeApp = officeAppMessages
				m.syncSidebarCursorToActive()
				m.notice = "Human decision needed. Team is paused until you answer."
				if m.pending.ReplyTo != "" {
					m.threadPanelOpen = true
					m.threadPanelID = m.pending.ReplyTo
				}
			}
		}

	case channelTickMsg:
		m.tickFrame++
		if m.notice != "" && !m.noticeExpireAt.IsZero() && time.Now().After(m.noticeExpireAt) {
			m.notice = ""
			m.noticeExpireAt = time.Time{}
		}
		return m, m.pollCurrentState()
	}

	return m, nil
}

func (m channelModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	layout := computeLayout(m.width, m.height, m.threadPanelOpen && !m.isOneOnOne(), m.sidebarCollapsed || m.isOneOnOne())
	workspaceState := m.currentWorkspaceUIState()

	// ── Sidebar ──────────────────────────────────────────────────────
	sidebar := ""
	if layout.ShowSidebar && !m.isOneOnOne() {
		sidebar = cachedSidebarRender(m.channels, mergeOfficeMembers(m.officeMembers, m.members, m.currentChannelInfo()), m.tasks, m.activeChannel, m.activeApp, m.sidebarCursor, m.sidebarRosterOffset, m.focus == focusSidebar, m.quickJumpTarget, workspaceState, layout.SidebarW, layout.ContentH)
	}

	// ── Thread panel ─────────────────────────────────────────────────
	thread := ""
	if layout.ShowThread && !m.isOneOnOne() {
		threadPopup := ""
		if m.focus == focusThread {
			threadPopup = m.renderActivePopup(maxInt(layout.ThreadW-4, 24))
		}
		thread = renderThreadPanel(m.messages, m.threadPanelID,
			layout.ThreadW, layout.ContentH,
			m.threadInput, m.threadInputPos, m.threadScroll,
			threadPopup, m.focus == focusThread, len(m.threadInputHistory.entries) > 0)
	}

	activePending := m.visiblePendingRequest()
	// ── Main panel: header + messages + composer ─────────────────────
	mainW := layout.MainW
	if mainW < 1 {
		mainW = 1
	}

	// Channel header (2 lines)
	headerStyle := channelHeaderStyle(mainW)
	headerLine1 := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).
		Render(appIcon(m.activeApp) + " " + m.currentHeaderTitle())
	headerMeta := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted)).
		Render(m.currentHeaderMeta())
	if m.usage.Total.TotalTokens > 0 || m.usage.Total.CostUsd > 0 || m.usage.Session.TotalTokens > 0 || m.usage.Session.CostUsd > 0 {
		sinceLabel := ""
		if m.usage.Since != "" {
			if t, err := time.Parse(time.RFC3339, m.usage.Since); err == nil {
				sinceLabel = " since " + t.Local().Format("Jan 2 15:04")
			}
		}
		headerMeta += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(slackActive)).
			Render(fmt.Sprintf("Session %s · %s  Total %s · %s%s",
				formatUsd(m.usage.Session.CostUsd),
				formatTokenCount(m.usage.Session.TotalTokens),
				formatUsd(m.usage.Total.CostUsd),
				formatTokenCount(m.usage.Total.TotalTokens),
				sinceLabel,
			))
	}
	if m.activeApp == officeAppMessages && m.unreadCount > 0 && m.scroll > 0 {
		headerMeta += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color(slackActive)).
			Padding(0, 1).
			Bold(true).
			Render(fmt.Sprintf("%d new", m.unreadCount))
		if awaySummary := m.currentAwaySummary(); strings.TrimSpace(awaySummary) != "" {
			headerMeta += "  " + lipgloss.NewStyle().
				Foreground(lipgloss.Color("#BFDBFE")).
				Render(awaySummary)
		}
	}
	if m.pending != nil {
		headerMeta += "  " + accentPill("request pending", "#B45309")
	} else if len(m.requests) > 0 {
		headerMeta += "  " + subtlePill(fmt.Sprintf("%d open requests", len(m.requests)), "#FDE68A", "#78350F")
	}
	channelHeader := headerStyle.Render(headerLine1 + headerMeta)
	if usageLine := renderUsageStrip(m.usage, m.members, mainW); usageLine != "" {
		channelHeader += "\n" + usageLine
	}
	headerH := lipgloss.Height(channelHeader)
	runtimeStrip := ""
	if m.activeApp == officeAppMessages || m.isOneOnOne() {
		focusSlug := ""
		if m.isOneOnOne() {
			focusSlug = m.oneOnOneAgentSlug()
		}
		runtimeStrip = renderRuntimeStrip(m.members, m.tasks, m.requests, m.actions, mainW-4, focusSlug)
	}
	runtimeH := lipgloss.Height(runtimeStrip)

	// Composer
	typingAgents := typingAgentsFromMembers(m.members)
	liveActivities := liveActivityFromMembers(m.members)
	composerStr := renderComposer(mainW, m.input, m.inputPos, m.composerTargetLabel(),
		m.replyToID, typingAgents, liveActivities, activePending, m.selectedOption, m.composerHint(m.composerTargetLabel(), m.replyToID, activePending),
		m.focus == focusMain, m.tickFrame)
	if m.memberDraft != nil {
		composerStr = renderComposer(mainW, m.input, m.inputPos, memberDraftComposerLabel(*m.memberDraft),
			"", typingAgents, nil, nil, 0, m.composerHint(memberDraftComposerLabel(*m.memberDraft), "", nil), m.focus == focusMain, m.tickFrame)
	}

	// Interview card (above composer)
	interviewCard := ""
	if activePending != nil {
		interviewCard = renderInterviewCard(*activePending, m.selectedOption, m.interviewPhaseTitle(), mainW-4)
	}
	memberDraftCard := ""
	if m.memberDraft != nil {
		memberDraftCard = renderMemberDraftCard(*m.memberDraft, mainW-4)
	}
	doctorCard := ""
	if m.doctor != nil {
		doctorCard = renderDoctorCard(*m.doctor, mainW-4)
	}
	confirmCard := ""
	if m.confirm != nil {
		confirmCard = renderConfirmCard(*m.confirm, mainW-4)
	}

	// Init/picker overlays
	initPanel := ""
	if confirmCard != "" {
		initPanel = confirmCard
	} else if m.picker.IsActive() {
		initPanel = m.picker.View()
	} else if m.initFlow.IsActive() || m.initFlow.Phase() == tui.InitDone {
		initPanel = m.initFlow.View()
	}

	composerH := lipgloss.Height(composerStr)
	interviewH := lipgloss.Height(interviewCard)
	memberDraftH := lipgloss.Height(memberDraftCard)
	doctorH := lipgloss.Height(doctorCard)
	initH := lipgloss.Height(initPanel)

	// Message area height
	msgH := layout.ContentH - headerH - runtimeH - composerH - interviewH - memberDraftH - doctorH - initH - 1 // 1 for status bar
	if msgH < 1 {
		msgH = 1
	}

	contentWidth := mainW - 2
	if contentWidth < 32 {
		contentWidth = 32
	}
	allLines := m.currentMainViewportLines(contentWidth, msgH)
	visibleRows, scroll, _, _ := sliceRenderedLines(allLines, msgH, m.scroll)
	var visible []string
	for _, row := range visibleRows {
		visible = append(visible, row.Text)
	}
	for len(visible) < msgH {
		visible = append(visible, "")
	}
	if m.activeApp == officeAppMessages && m.unreadCount > 0 && scroll > 0 && len(visible) > 0 {
		visible[0] = renderAwayStrip(contentWidth, m.unreadCount, m.currentAwaySummary())
	}
	if popup := m.renderActivePopup(contentWidth); popup != "" && m.focus == focusMain {
		visible = overlayBottomLines(visible, strings.Split(popup, "\n"))
	}

	msgPanel := mainPanelStyle(mainW, msgH).Render(strings.Join(visible, "\n"))

	// Assemble main column
	mainParts := []string{channelHeader}
	if runtimeStrip != "" {
		mainParts = append(mainParts, runtimeStrip)
	}
	mainParts = append(mainParts, msgPanel)
	if interviewCard != "" {
		mainParts = append(mainParts, interviewCard)
	}
	if memberDraftCard != "" {
		mainParts = append(mainParts, memberDraftCard)
	}
	if doctorCard != "" {
		mainParts = append(mainParts, doctorCard)
	}
	if initPanel != "" {
		mainParts = append(mainParts, initPanel)
	}
	if m.activeApp == officeAppMessages || m.memberDraft != nil {
		mainParts = append(mainParts, composerStr)
	}
	mainCol := strings.Join(mainParts, "\n")

	// ── Compose 3 columns ────────────────────────────────────────────
	border := renderVerticalBorder(layout.ContentH, slackBorder)
	var panels []string
	if sidebar != "" {
		panels = append(panels, sidebar, border)
	}
	panels = append(panels, mainCol)
	if thread != "" {
		panels = append(panels, border, thread)
	}

	content := lipgloss.NewStyle().MaxWidth(m.width).Render(
		lipgloss.JoinHorizontal(lipgloss.Top, panels...))

	// ── Status bar ───────────────────────────────────────────────────
	onlineCount := len(m.members)
	scrollHint := "PgUp/PgDn"
	if scroll > 0 {
		scrollHint = fmt.Sprintf("%d above", scroll)
	}
	focusLabel := "main"
	if m.focus == focusSidebar {
		focusLabel = "sidebar"
	} else if m.focus == focusThread {
		focusLabel = "thread"
	}
	statusBar := statusBarStyle(m.width).Render(fmt.Sprintf(
		" %s %d online │ %d msgs │ focus:%s │ %s │ Ctrl+J newline │ /doctor",
		"\u25CF", onlineCount, len(m.messages), focusLabel, scrollHint,
	))
	if m.pending != nil {
		statusText := m.interviewStatusLine()
		if m.pending.ID == m.snoozedInterview {
			statusText = " Request paused │ Esc snoozed it │ team remains blocked until answered"
		}
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Render(statusText),
		)
	} else if m.usage.Total.TotalTokens > 0 || m.usage.Total.CostUsd > 0 || m.usage.Session.TotalTokens > 0 || m.usage.Session.CostUsd > 0 {
		sinceStatus := ""
		if m.usage.Since != "" {
			if t, err := time.Parse(time.RFC3339, m.usage.Since); err == nil {
				sinceStatus = " since " + t.Local().Format("Jan 2 15:04")
			}
		}
		statusBar = statusBarStyle(m.width).Render(fmt.Sprintf(
			" %s %d online │ session %s · %s │ total %s · %s%s │ %s │ Ctrl+J newline │ /doctor",
			"\u25CF", onlineCount,
			formatUsd(m.usage.Session.CostUsd), formatTokenCount(m.usage.Session.TotalTokens),
			formatUsd(m.usage.Total.CostUsd), formatTokenCount(m.usage.Total.TotalTokens),
			sinceStatus, scrollHint,
		))
	} else if m.quickJumpTarget != quickJumpNone {
		label := "channels"
		if m.quickJumpTarget == quickJumpApps {
			label = "apps"
		}
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(slackActive)).Render(
				fmt.Sprintf(" Quick nav │ Ctrl+G channels · Ctrl+O apps │ 1-9 switch %s │ Esc cancel", label),
			),
		)
	} else if m.notice != "" {
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(slackActive)).Render(" " + m.notice),
		)
	} else if m.isOneOnOne() {
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(slackActive)).Render(
				workspaceState.defaultStatusLine(scrollHint),
			),
		)
	} else if !m.brokerConnected {
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B")).Render(workspaceState.defaultStatusLine(scrollHint)),
		)
	} else if m.replyToID != "" {
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(slackActive)).Render(
				fmt.Sprintf(" ↩ Reply mode │ thread %s │ Ctrl+J newline │ /cancel to return", m.replyToID),
			),
		)
	} else if m.activeApp != officeAppMessages {
		message := fmt.Sprintf(" Viewing %s │ %s │ /messages to return │ /doctor", m.currentAppLabel(), scrollHint)
		if m.activeApp == officeAppCalendar {
			filter := "all"
			if strings.TrimSpace(m.calendarFilter) != "" {
				filter = "@" + m.calendarFilter
			}
			message = fmt.Sprintf(" Calendar │ d day · w week · f filter · a all │ current %s/%s", m.calendarRange, filter)
		}
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(slackActive)).Render(
				message,
			),
		)
	} else {
		statusBar = statusBarStyle(m.width).Render(
			lipgloss.NewStyle().Foreground(lipgloss.Color(slackActive)).Render(workspaceState.defaultStatusLine(scrollHint)),
		)
	}

	return content + "\n" + statusBar
}

func (m channelModel) currentHeaderTitle() string {
	if m.isOneOnOne() && m.activeApp != officeAppRecovery && m.activeApp != officeAppInbox && m.activeApp != officeAppOutbox {
		return "1:1 with " + m.oneOnOneAgentName()
	}
	switch m.activeApp {
	case officeAppRecovery:
		if m.isOneOnOne() {
			return "1:1 with " + m.oneOnOneAgentName() + " · Recovery"
		}
		return "# " + m.activeChannel + " · Recovery"
	case officeAppInbox:
		if m.isOneOnOne() {
			return "1:1 with " + m.oneOnOneAgentName() + " · Inbox"
		}
		return "# " + m.activeChannel + " · Inbox"
	case officeAppOutbox:
		if m.isOneOnOne() {
			return "1:1 with " + m.oneOnOneAgentName() + " · Outbox"
		}
		return "# " + m.activeChannel + " · Outbox"
	case officeAppArtifacts:
		return "# " + m.activeChannel + " · Artifacts"
	case officeAppTasks:
		return "# " + m.activeChannel + " · Tasks"
	case officeAppRequests:
		return "# " + m.activeChannel + " · Requests"
	case officeAppPolicies:
		return "# " + m.activeChannel + " · Insights"
	case officeAppCalendar:
		return "# " + m.activeChannel + " · Calendar"
	case officeAppSkills:
		return "# " + m.activeChannel + " · Skills"
	default:
		return "# " + m.activeChannel
	}
}

func (m channelModel) currentHeaderMeta() string {
	workspace := m.currentWorkspaceUIState()
	if m.activeApp == officeAppRecovery {
		snapshot := workspace.Runtime
		if m.isOneOnOne() {
			return fmt.Sprintf("  Re-entry summary for %s · %d running tasks · %d open requests · %d new since you looked", m.oneOnOneAgentName(), workspace.RunningTasks, workspace.OpenRequests, workspace.UnreadCount)
		}
		parts := []string{
			fmt.Sprintf("Re-entry summary for #%s", fallbackString(snapshot.Channel, m.activeChannel)),
			fmt.Sprintf("%d blocking requests", workspace.BlockingCount),
			fmt.Sprintf("%d running tasks", workspace.RunningTasks),
			fmt.Sprintf("%d new since you looked", workspace.UnreadCount),
		}
		if workspace.Readiness.Level != workspaceReadinessReady && strings.TrimSpace(workspace.Readiness.Headline) != "" {
			parts = append(parts, strings.ToLower(workspace.Readiness.Headline))
		}
		return "  " + strings.Join(parts, " · ")
	}
	if m.isOneOnOne() && (m.activeApp == officeAppInbox || m.activeApp == officeAppOutbox) {
		scopeLabel := "inbox"
		if m.activeApp == officeAppOutbox {
			scopeLabel = "outbox"
		}
		scopeCount := len(filterMessagesForViewerScope(m.messages, m.oneOnOneAgentSlug(), scopeLabel))
		parts := []string{
			fmt.Sprintf("%s lane for %s", strings.Title(scopeLabel), m.oneOnOneAgentName()),
			fmt.Sprintf("%d visible messages", scopeCount),
		}
		if workspace.RunningTasks > 0 {
			parts = append(parts, fmt.Sprintf("%d running tasks", workspace.RunningTasks))
		}
		if strings.TrimSpace(workspace.Focus) != "" {
			parts = append(parts, "focus: "+workspace.Focus)
		}
		return "  " + strings.Join(parts, " · ")
	}
	if m.isOneOnOne() {
		return workspace.headerMeta()
	}
	switch m.activeApp {
	case officeAppInbox:
		return fmt.Sprintf("  Inbox lane · %d visible messages · %d open requests", len(m.messages), len(m.requests))
	case officeAppOutbox:
		return fmt.Sprintf("  Outbox lane · %d visible messages · %d recent actions", len(m.messages), len(m.actions))
	case officeAppTasks:
		open, inProgress, review, blocked, overdue := 0, 0, 0, 0, 0
		for _, task := range m.tasks {
			switch task.Status {
			case "in_progress":
				inProgress++
			case "review":
				review++
			case "blocked":
				blocked++
			default:
				open++
			}
			if parsed, ok := parseChannelTime(task.DueAt); ok && parsed.Before(time.Now()) && task.Status != "done" {
				overdue++
			}
		}
		return fmt.Sprintf("  Clear ownership, no duplicate work · %d open · %d moving · %d in review · %d blocked · %d overdue", open, inProgress, review, blocked, overdue)
	case officeAppRequests:
		blocking, urgent := 0, 0
		for _, req := range m.requests {
			if req.Blocking {
				blocking++
			}
			if parsed, ok := parseChannelTime(req.DueAt); ok && parsed.Before(time.Now().Add(2*time.Hour)) {
				urgent++
			}
		}
		return fmt.Sprintf("  Decisions and approvals the team is waiting on · %d open · %d blocking · %d soon", len(m.requests), blocking, urgent)
	case officeAppPolicies:
		highSignal := 0
		for _, signal := range m.signals {
			if signal.Urgency == "high" || signal.Blocking || signal.RequiresHuman {
				highSignal++
			}
		}
		activeWatchdogs := 0
		for _, alert := range m.watchdogs {
			if strings.TrimSpace(alert.Status) != "resolved" {
				activeWatchdogs++
			}
		}
		external := 0
		for _, action := range m.actions {
			if strings.HasPrefix(strings.TrimSpace(action.Kind), "external_") {
				external++
			}
		}
		return fmt.Sprintf("  Signals, Decisions, External Actions, and Watchdogs driving the office · %d signals · %d decisions · %d external · %d active watchdogs · %d high signal", len(m.signals), len(m.decisions), external, activeWatchdogs, highSignal)
	case officeAppCalendar:
		events := filterCalendarEvents(collectCalendarEvents(m.scheduler, m.tasks, m.requests, m.activeChannel, m.members), m.calendarRange, m.calendarFilter)
		dueSoon := 0
		now := time.Now()
		for _, event := range events {
			if !event.When.After(now.Add(15 * time.Minute)) {
				dueSoon++
			}
		}
		view := "week"
		if m.calendarRange == calendarRangeDay {
			view = "day"
		}
		filter := "everyone"
		if strings.TrimSpace(m.calendarFilter) != "" {
			filter = displayName(m.calendarFilter)
		}
		scheduledWorkflows := 0
		for _, job := range m.scheduler {
			if strings.TrimSpace(job.Kind) == "one_workflow" {
				scheduledWorkflows++
			}
		}
		return fmt.Sprintf("  %s view · %s · %d upcoming · %d due soon · %d scheduled workflows · %d recent actions", view, filter, len(events), dueSoon, scheduledWorkflows, len(m.actions))
	case officeAppSkills:
		active := 0
		workflowBacked := 0
		for _, skill := range m.skills {
			if skill.Status == "" || skill.Status == "active" {
				active++
			}
			if strings.TrimSpace(skill.WorkflowKey) != "" {
				workflowBacked++
			}
		}
		return fmt.Sprintf("  Reusable team skills · %d total · %d active · %d workflow-backed", len(m.skills), active, workflowBacked)
	case officeAppArtifacts:
		summary := m.currentArtifactSummary()
		if summary == "" {
			return "  Retained task logs, approvals, and workflow history for this office"
		}
		return "  " + summary
	default:
		return workspace.headerMeta()
	}
}

func (m channelModel) currentAppLabel() string {
	if m.isOneOnOne() && m.activeApp != officeAppRecovery && m.activeApp != officeAppInbox && m.activeApp != officeAppOutbox {
		return "messages"
	}
	switch m.activeApp {
	case officeAppRecovery:
		return "recovery"
	case officeAppInbox:
		return "inbox"
	case officeAppOutbox:
		return "outbox"
	case officeAppTasks:
		return "tasks"
	case officeAppRequests:
		return "requests"
	case officeAppPolicies:
		return "policies"
	case officeAppCalendar:
		return "calendar"
	case officeAppArtifacts:
		return "artifacts"
	case officeAppSkills:
		return "skills"
	default:
		return "messages"
	}
}

func (m channelModel) currentMainLines(contentWidth int) []renderedLine {
	return m.cachedMainLines(contentWidth)
}

func filterInsightMessages(messages []brokerMessage) []brokerMessage {
	filtered := make([]brokerMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Kind == "automation" || msg.From == "nex" {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

func latestHumanFacingMessage(messages []brokerMessage) *brokerMessage {
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(messages[i].Kind), "human_") {
			return &messages[i]
		}
	}
	return nil
}

type mouseAction struct {
	Kind  string
	Value string
}

func popupActionIndex(raw string) (int, bool) {
	var idx int
	if _, err := fmt.Sscanf(raw, "%d", &idx); err != nil || idx < 0 {
		return 0, false
	}
	return idx, true
}

func (m channelModel) mouseActionAt(x, y int) (mouseAction, bool) {
	if m.width == 0 || m.height == 0 || y >= m.height-1 {
		return mouseAction{}, false
	}

	layout := computeLayout(m.width, m.height, m.threadPanelOpen, m.sidebarCollapsed)
	sidebarW := 0
	if layout.ShowSidebar {
		sidebarW = layout.SidebarW
		if x < sidebarW {
			if item, ok := m.sidebarItemAt(y); ok {
				return mouseAction{Kind: item.Kind, Value: item.Value}, true
			}
			return mouseAction{Kind: "focus", Value: "sidebar"}, true
		}
		x -= sidebarW + 1
	}

	mainW := layout.MainW
	if mainW < 1 {
		mainW = 1
	}
	if x >= 0 && x < mainW {
		if action, ok := m.mainPanelMouseAction(x, y, mainW, layout.ContentH); ok {
			return action, true
		}
		return mouseAction{Kind: "focus", Value: "main"}, true
	}

	if layout.ShowThread {
		threadStart := mainW + 1
		if x >= threadStart {
			return mouseAction{Kind: "focus", Value: "thread"}, true
		}
	}

	return mouseAction{}, false
}

func (m channelModel) sidebarItemAt(y int) (sidebarItem, bool) {
	lines := 0
	lines++ // blank
	lines++ // WUPHF
	lines++ // subtitle
	lines++ // blank
	lines++ // Channels header
	items := m.sidebarItems()
	channelCount := len(m.channels)
	if channelCount == 0 {
		channelCount = 1
	}
	for i := 0; i < channelCount; i++ {
		if y == lines {
			return items[i], true
		}
		lines++
	}
	lines++ // blank before Apps
	lines++ // Apps header
	for i := channelCount; i < len(items); i++ {
		if y == lines {
			return items[i], true
		}
		lines++
	}
	return sidebarItem{}, false
}

func (m channelModel) mainPanelMouseAction(x, y, mainW, contentH int) (mouseAction, bool) {
	headerH, msgH, popupRows := m.mainPanelGeometry(mainW, contentH)
	if y < headerH {
		return mouseAction{}, false
	}

	msgTop := headerH
	msgBottom := headerH + msgH
	if y >= msgTop && y < msgBottom {
		row := y - msgTop
		if m.activeApp == officeAppMessages && m.unreadCount > 0 && m.scroll > 0 && row == 0 {
			return mouseAction{Kind: "jump-latest"}, true
		}
		if len(popupRows) > 0 {
			popupStart := msgBottom - len(popupRows)
			if y >= popupStart {
				idx := y - popupStart
				if m.autocomplete.IsVisible() {
					if idx < 0 || idx >= len(m.autocomplete.Matches()) {
						return mouseAction{}, false
					}
					return mouseAction{Kind: "autocomplete", Value: fmt.Sprintf("%d", idx)}, true
				}
				if m.mention.IsVisible() {
					if idx < 0 || idx >= len(m.mention.Matches()) {
						return mouseAction{}, false
					}
					return mouseAction{Kind: "mention", Value: fmt.Sprintf("%d", idx)}, true
				}
			}
		}

		contentWidth := mainW - 2
		if contentWidth < 32 {
			contentWidth = 32
		}
		allLines := m.currentMainViewportLines(contentWidth, msgH)
		visibleRows, _, _, _ := sliceRenderedLines(allLines, msgH, m.scroll)
		if row >= 0 && row < len(visibleRows) {
			if visibleRows[row].PromptValue != "" {
				return mouseAction{Kind: "prompt", Value: visibleRows[row].PromptValue}, true
			}
			switch m.activeApp {
			case officeAppMessages:
				if visibleRows[row].ThreadID != "" {
					return mouseAction{Kind: "thread", Value: visibleRows[row].ThreadID}, true
				}
			case officeAppInbox, officeAppOutbox:
				if visibleRows[row].ThreadID != "" {
					return mouseAction{Kind: "thread", Value: visibleRows[row].ThreadID}, true
				}
				if visibleRows[row].RequestID != "" {
					return mouseAction{Kind: "request", Value: visibleRows[row].RequestID}, true
				}
			case officeAppTasks:
				if visibleRows[row].TaskID != "" {
					return mouseAction{Kind: "task", Value: visibleRows[row].TaskID}, true
				}
			case officeAppRequests:
				if visibleRows[row].RequestID != "" {
					return mouseAction{Kind: "request", Value: visibleRows[row].RequestID}, true
				}
			case officeAppCalendar:
				if visibleRows[row].ThreadID != "" {
					return mouseAction{Kind: "thread", Value: visibleRows[row].ThreadID}, true
				}
				if visibleRows[row].TaskID != "" {
					return mouseAction{Kind: "task", Value: visibleRows[row].TaskID}, true
				}
				if visibleRows[row].RequestID != "" {
					return mouseAction{Kind: "request", Value: visibleRows[row].RequestID}, true
				}
			case officeAppRecovery, officeAppArtifacts:
				if visibleRows[row].ThreadID != "" {
					return mouseAction{Kind: "thread", Value: visibleRows[row].ThreadID}, true
				}
				if visibleRows[row].TaskID != "" {
					return mouseAction{Kind: "task", Value: visibleRows[row].TaskID}, true
				}
				if visibleRows[row].RequestID != "" {
					return mouseAction{Kind: "request", Value: visibleRows[row].RequestID}, true
				}
			}
		}
	}

	return mouseAction{}, false
}

func (m *channelModel) applyRecoveryPrompt(prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		m.notice = "Nothing inserted."
		return
	}
	m.activeApp = officeAppMessages
	m.syncSidebarCursorToActive()
	m.focus = focusMain
	m.insertIntoActiveComposer(prompt)
	m.notice = "Inserted a recovery prompt into the composer."
}

func (m channelModel) mainPanelGeometry(mainW, contentH int) (headerH, msgH int, popupRows []string) {
	headerStyle := channelHeaderStyle(mainW)
	headerLine1 := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F8FAFC")).
		Render(m.currentHeaderTitle())
	headerMeta := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted)).
		Render(m.currentHeaderMeta())
	if m.usage.Total.TotalTokens > 0 || m.usage.Total.CostUsd > 0 || m.usage.Session.TotalTokens > 0 || m.usage.Session.CostUsd > 0 {
		sinceLabel := ""
		if m.usage.Since != "" {
			if t, err := time.Parse(time.RFC3339, m.usage.Since); err == nil {
				sinceLabel = " since " + t.Local().Format("Jan 2 15:04")
			}
		}
		headerMeta += "  " + lipgloss.NewStyle().
			Foreground(lipgloss.Color(slackActive)).
			Render(fmt.Sprintf("Session %s · %s  Total %s · %s%s",
				formatUsd(m.usage.Session.CostUsd),
				formatTokenCount(m.usage.Session.TotalTokens),
				formatUsd(m.usage.Total.CostUsd),
				formatTokenCount(m.usage.Total.TotalTokens),
				sinceLabel,
			))
	}
	channelHeader := headerStyle.Render(headerLine1 + headerMeta)
	if usageLine := renderUsageStrip(m.usage, m.members, mainW); usageLine != "" {
		channelHeader += "\n" + usageLine
	}
	headerH = lipgloss.Height(channelHeader)

	activePending := m.visiblePendingRequest()
	typingAgents := typingAgentsFromMembers(m.members)
	liveActivities := liveActivityFromMembers(m.members)
	composerStr := renderComposer(mainW, m.input, m.inputPos, m.composerTargetLabel(),
		m.replyToID, typingAgents, liveActivities, activePending, m.selectedOption, m.composerHint(m.composerTargetLabel(), m.replyToID, activePending),
		m.focus == focusMain, m.tickFrame)
	if m.memberDraft != nil {
		composerStr = renderComposer(mainW, m.input, m.inputPos, memberDraftComposerLabel(*m.memberDraft),
			"", typingAgents, nil, nil, 0, m.composerHint(memberDraftComposerLabel(*m.memberDraft), "", nil), m.focus == focusMain, m.tickFrame)
	}
	interviewCard := ""
	if activePending != nil {
		interviewCard = renderInterviewCard(*activePending, m.selectedOption, m.interviewPhaseTitle(), mainW-4)
	}
	memberDraftCard := ""
	if m.memberDraft != nil {
		memberDraftCard = renderMemberDraftCard(*m.memberDraft, mainW-4)
	}
	initPanel := ""
	if m.confirm != nil {
		initPanel = renderConfirmCard(*m.confirm, mainW-4)
	} else if m.picker.IsActive() {
		initPanel = m.picker.View()
	} else if m.initFlow.IsActive() || m.initFlow.Phase() == tui.InitDone {
		initPanel = m.initFlow.View()
	}
	msgH = contentH - headerH - lipgloss.Height(composerStr) - lipgloss.Height(interviewCard) - lipgloss.Height(memberDraftCard) - lipgloss.Height(initPanel) - 1
	if msgH < 1 {
		msgH = 1
	}

	contentWidth := mainW - 2
	if contentWidth < 32 {
		contentWidth = 32
	}
	if popup := m.renderActivePopup(contentWidth); popup != "" && m.focus == focusMain {
		popupRows = strings.Split(popup, "\n")
	}
	return headerH, msgH, popupRows
}

func (m channelModel) visiblePendingRequest() *channelInterview {
	if m.pending == nil {
		return nil
	}
	if m.pending.ID == m.snoozedInterview {
		return nil
	}
	if m.pending.Channel != "" && m.pending.Channel != m.activeChannel {
		return nil
	}
	return m.pending
}

func (m channelModel) composerTargetLabel() string {
	if m.isOneOnOne() {
		return "1:1 with " + m.oneOnOneAgentName()
	}
	if strings.HasPrefix(m.activeChannel, "dm-") {
		slug := strings.TrimPrefix(m.activeChannel, "dm-")
		return "DM→" + slug
	}
	return m.activeChannel
}

func (m channelModel) recommendedOptionIndex() int {
	if m.pending == nil {
		return 0
	}
	for i, option := range m.pending.Options {
		if option.ID == m.pending.RecommendedID {
			return i
		}
	}
	return 0
}

func (m channelModel) interviewOptionCount() int {
	if m.pending == nil {
		return 0
	}
	return len(m.pending.Options) + 1
}

func (m channelModel) selectedInterviewOption() *channelInterviewOption {
	if m.pending == nil {
		return nil
	}
	if len(m.pending.Options) == 0 {
		return nil
	}
	if m.selectedOption < 0 {
		return &m.pending.Options[0]
	}
	if m.selectedOption >= len(m.pending.Options) {
		return nil
	}
	return &m.pending.Options[m.selectedOption]
}

func countUniqueAgents(messages []brokerMessage) int {
	seen := make(map[string]bool)
	for _, m := range messages {
		if m.From == "you" || m.From == "nex" || m.Kind == "automation" {
			continue
		}
		seen[m.From] = true
	}
	return len(seen)
}

func formatUsd(cost float64) string {
	return fmt.Sprintf("$%.2f", cost)
}

func formatTokenCount(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fM tok", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk tok", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d tok", tokens)
	}
}

func renderUsageStrip(usage channelUsageState, members []channelMember, width int) string {
	if len(usage.Agents) == 0 || width < 40 {
		return ""
	}

	var ordered []string
	seen := make(map[string]bool)
	for _, member := range members {
		if _, ok := usage.Agents[member.Slug]; ok && !seen[member.Slug] {
			ordered = append(ordered, member.Slug)
			seen[member.Slug] = true
		}
	}
	for _, slug := range []string{"ceo", "pm", "fe", "be", "ai", "designer", "cmo", "cro"} {
		if _, ok := usage.Agents[slug]; ok && !seen[slug] {
			ordered = append(ordered, slug)
			seen[slug] = true
		}
	}
	for slug := range usage.Agents {
		if !seen[slug] {
			ordered = append(ordered, slug)
		}
	}

	pillStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#CBD5E1")).
		Background(lipgloss.Color("#111827")).
		Padding(0, 1)

	var pills []string
	for _, slug := range ordered {
		totals := usage.Agents[slug]
		if totals.TotalTokens == 0 && totals.CostUsd == 0 {
			continue
		}
		label := fmt.Sprintf("%s %s · %s", agentAvatar(slug), formatTokenCount(totals.TotalTokens), formatUsd(totals.CostUsd))
		pills = append(pills, pillStyle.Render(label))
	}
	if len(pills) == 0 {
		return ""
	}
	prefix := lipgloss.NewStyle().Foreground(lipgloss.Color(slackMuted)).Render("  Spend by teammate")
	return prefix + "  " + strings.Join(pills, " ")
}

// nextFocus cycles through visible panels: main → sidebar → thread → main.
func (m channelModel) nextFocus() focusArea {
	order := []focusArea{focusMain}
	if !m.sidebarCollapsed {
		order = append(order, focusSidebar)
	}
	if m.threadPanelOpen {
		order = append(order, focusThread)
	}
	for i, f := range order {
		if f == m.focus {
			return order[(i+1)%len(order)]
		}
	}
	return focusMain
}

// updateThread handles key events when the thread panel is focused.
func (m channelModel) updateThread(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if motionKey, ok := composerMotionKey(msg); ok {
		if nextPos, handled := moveComposerCursor(m.threadInput, m.threadInputPos, motionKey); handled {
			m.threadInputPos = nextPos
			m.updateThreadOverlays()
		}
		return m, nil
	}
	key := msg.String()
	if msg.Type == tea.KeyCtrlJ {
		key = "ctrl+j"
	}
	switch key {
	case "enter":
		if len(m.threadInput) > 0 {
			text := string(m.threadInput)
			trimmed := strings.TrimSpace(text)
			if strings.HasPrefix(trimmed, "/") {
				m.threadInputHistory.record(m.threadInput, m.threadInputPos)
				return m.runCommand(trimmed, m.threadPanelID)
			}
			m.threadInputHistory.record(m.threadInput, m.threadInputPos)
			m.threadInput = nil
			m.threadInputPos = 0
			m.posting = true
			return m, postToChannel(text, m.threadPanelID, m.activeChannel)
		}
	case "backspace":
		if m.threadInputPos > 0 {
			m.threadInputHistory.resetRecall()
			m.threadInput = append(m.threadInput[:m.threadInputPos-1], m.threadInput[m.threadInputPos:]...)
			m.threadInputPos--
			m.updateThreadOverlays()
		}
	case "ctrl+u":
		m.threadInputHistory.resetRecall()
		m.threadInput = nil
		m.threadInputPos = 0
		m.updateThreadOverlays()
	case "ctrl+p":
		if snapshot, ok := m.threadInputHistory.previous(m.threadInput, m.threadInputPos); ok {
			m.restoreThreadSnapshot(snapshot)
		}
	case "ctrl+n":
		if snapshot, ok := m.threadInputHistory.next(); ok {
			m.restoreThreadSnapshot(snapshot)
		}
	case "ctrl+a":
		m.threadInputPos = 0
		m.updateThreadOverlays()
	case "ctrl+e":
		m.threadInputPos = len(m.threadInput)
		m.updateThreadOverlays()
	case "ctrl+j":
		m.threadInputHistory.resetRecall()
		ch := []rune{'\n'}
		tail := make([]rune, len(m.threadInput[m.threadInputPos:]))
		copy(tail, m.threadInput[m.threadInputPos:])
		m.threadInput = append(m.threadInput[:m.threadInputPos], append(ch, tail...)...)
		m.threadInputPos++
		m.updateThreadOverlays()
	case "left":
		if m.threadInputPos > 0 {
			m.threadInputPos--
			m.updateThreadOverlays()
		}
	case "right":
		if m.threadInputPos < len(m.threadInput) {
			m.threadInputPos++
			m.updateThreadOverlays()
		}
	case "up":
		m.threadScroll++
	case "down":
		m.threadScroll--
		if m.threadScroll < 0 {
			m.threadScroll = 0
		}
	case "pgup":
		m.threadScroll += 5
	case "pgdown":
		m.threadScroll -= 5
		if m.threadScroll < 0 {
			m.threadScroll = 0
		}
	default:
		if ch := composerInsertRunes(msg); len(ch) > 0 {
			m.threadInputHistory.resetRecall()
			m.threadInput, m.threadInputPos = insertComposerRunes(m.threadInput, m.threadInputPos, ch)
			m.updateThreadOverlays()
		} else if len(msg.String()) == 1 || msg.Type == tea.KeyRunes {
			ch := msg.Runes
			if len(ch) == 0 {
				ch = []rune(msg.String())
			}
			if len(ch) > 0 {
				m.threadInputHistory.resetRecall()
				tail := make([]rune, len(m.threadInput[m.threadInputPos:]))
				copy(tail, m.threadInput[m.threadInputPos:])
				m.threadInput = append(m.threadInput[:m.threadInputPos], append(ch, tail...)...)
				m.threadInputPos += len(ch)
				m.updateThreadOverlays()
			}
		}
	}
	return m, nil
}

// updateSidebar handles key events when the sidebar is focused.
func (m channelModel) updateSidebar(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	roster := mergeOfficeMembers(m.officeMembers, m.members, m.currentChannelInfo())
	switch msg.String() {
	case "up", "k":
		m.sidebarCursor--
		m.clampSidebarCursor()
	case "down", "j":
		m.sidebarCursor++
		m.clampSidebarCursor()
	case "pgup":
		m.sidebarRosterOffset -= 3
		if m.sidebarRosterOffset < 0 {
			m.sidebarRosterOffset = 0
		}
	case "pgdown":
		m.sidebarRosterOffset += 3
		maxOffset := maxInt(0, len(roster)-1)
		if m.sidebarRosterOffset > maxOffset {
			m.sidebarRosterOffset = maxOffset
		}
	case "home":
		m.sidebarRosterOffset = 0
	case "end":
		m.sidebarRosterOffset = maxInt(0, len(roster)-1)
	case "enter":
		items := m.sidebarItems()
		m.clampSidebarCursor()
		if len(items) == 0 {
			return m, nil
		}
		return m, m.selectSidebarItem(items[m.sidebarCursor])
	case "d":
		// Switch the main channel view to the per-agent DM channel.
		// The office continues running; this is just a channel switch.
		roster := mergeOfficeMembers(m.officeMembers, m.members, m.currentChannelInfo())
		if len(roster) > 0 {
			idx := m.sidebarRosterOffset
			if idx < 0 {
				idx = 0
			}
			if idx >= len(roster) {
				idx = len(roster) - 1
			}
			target := roster[idx]
			name := target.Name
			if name == "" {
				name = target.Slug
			}
			m.activeChannel = "dm-" + target.Slug
			m.focus = focusMain
			m.lastID = ""
			m.messages = nil
			m.notice = fmt.Sprintf("DM with %s — Ctrl+D to return to #general", name)
		}
	}
	return m, nil
}

func appendWrapped(lines []string, width int, text string) []string {
	if width <= 0 {
		return append(lines, strings.Split(text, "\n")...)
	}
	wrapped := ansi.Wrap(text, width, "")
	return append(lines, strings.Split(wrapped, "\n")...)
}

type threadedMessage struct {
	Message            brokerMessage
	Depth              int
	ParentLabel        string
	Collapsed          bool
	HiddenReplies      int
	ThreadParticipants []string
}

type sidebarItem struct {
	Kind  string
	Value string
	Label string
}

type officeSidebarApp struct {
	App   officeApp
	Label string
}

func officeSidebarApps() []officeSidebarApp {
	return []officeSidebarApp{
		{App: officeAppMessages, Label: "Messages"},
		{App: officeAppRecovery, Label: "Recovery"},
		{App: officeAppTasks, Label: "Tasks"},
		{App: officeAppRequests, Label: "Requests"},
		{App: officeAppPolicies, Label: "Policies"},
		{App: officeAppCalendar, Label: "Calendar"},
		{App: officeAppArtifacts, Label: "Artifacts"},
		{App: officeAppSkills, Label: "Skills"},
	}
}

func (m channelModel) sidebarItems() []sidebarItem {
	if m.isOneOnOne() {
		return nil
	}
	items := make([]sidebarItem, 0, len(m.channels)+5)
	items = append(items, m.channelSidebarItems()...)
	items = append(items, m.appSidebarItems()...)
	return items
}

func (m channelModel) channelSidebarItems() []sidebarItem {
	items := make([]sidebarItem, 0, len(m.channels))
	channels := m.channels
	if len(channels) == 0 {
		channels = []channelInfo{{Slug: "general", Name: "general"}}
	}
	for _, ch := range channels {
		items = append(items, sidebarItem{Kind: "channel", Value: ch.Slug, Label: "# " + ch.Slug})
	}
	return items
}

func (m channelModel) appSidebarItems() []sidebarItem {
	apps := officeSidebarApps()
	items := make([]sidebarItem, 0, len(apps))
	for _, app := range apps {
		items = append(items, sidebarItem{Kind: "app", Value: string(app.App), Label: app.Label})
	}
	return items
}

func sidebarShortcutLabel(index int) string {
	if index < 0 || index > 8 {
		return ""
	}
	return fmt.Sprintf("%d", index+1)
}

func (m channelModel) quickJumpItems() []sidebarItem {
	switch m.quickJumpTarget {
	case quickJumpChannels:
		return m.channelSidebarItems()
	case quickJumpApps:
		return m.appSidebarItems()
	default:
		return nil
	}
}

func (m *channelModel) setSidebarCursorForItem(target sidebarItem) {
	items := m.sidebarItems()
	for i, item := range items {
		if item.Kind == target.Kind && item.Value == target.Value {
			m.sidebarCursor = i
			return
		}
	}
}

func (m *channelModel) clampSidebarCursor() {
	items := m.sidebarItems()
	if len(items) == 0 {
		m.sidebarCursor = 0
		return
	}
	if m.sidebarCursor < 0 {
		m.sidebarCursor = 0
	}
	if m.sidebarCursor >= len(items) {
		m.sidebarCursor = len(items) - 1
	}
}

func (m *channelModel) selectSidebarItem(item sidebarItem) tea.Cmd {
	switch item.Kind {
	case "channel":
		m.activeChannel = item.Value
		m.activeApp = officeAppMessages
		m.syncSidebarCursorToActive()
		m.lastID = ""
		m.messages = nil
		m.members = nil
		m.requests = nil
		m.tasks = nil
		m.replyToID = ""
		m.threadPanelOpen = false
		m.threadPanelID = ""
		m.notice = "Switched to #" + m.activeChannel
		return tea.Batch(pollBroker("", m.activeChannel), pollMembers(m.activeChannel), pollRequests(m.activeChannel), pollTasks(m.activeChannel))
	case "app":
		m.activeApp = officeApp(item.Value)
		m.syncSidebarCursorToActive()
		switch m.activeApp {
		case officeAppMessages:
			m.notice = "Viewing #" + m.activeChannel + "."
			return pollBroker("", m.activeChannel)
		case officeAppInbox:
			m.notice = "Viewing the selected agent inbox."
			return pollBroker("", m.activeChannel)
		case officeAppOutbox:
			m.notice = "Viewing the selected agent outbox."
			return pollBroker("", m.activeChannel)
		case officeAppRecovery:
			m.notice = "Viewing the recovery summary."
			return m.pollCurrentState()
		case officeAppTasks:
			m.notice = "Viewing tasks in #" + m.activeChannel + "."
			return pollTasks(m.activeChannel)
		case officeAppRequests:
			m.notice = "Viewing requests in #" + m.activeChannel + "."
			return pollRequests(m.activeChannel)
		case officeAppPolicies:
			m.notice = "Viewing Nex and office insights."
			return pollOfficeLedger()
		case officeAppCalendar:
			m.notice = "Viewing the office calendar."
			return pollOfficeLedger()
		case officeAppArtifacts:
			m.notice = "Viewing recent execution artifacts."
			return m.pollCurrentState()
		case officeAppSkills:
			m.notice = "Viewing skills."
			return pollSkills("")
		}
	}
	return nil
}

func (m *channelModel) syncSidebarCursorToActive() {
	items := m.sidebarItems()
	for i, item := range items {
		if item.Kind == "channel" && item.Value == m.activeChannel && m.activeApp == officeAppMessages {
			m.sidebarCursor = i
			return
		}
		if item.Kind == "app" && item.Value == string(m.activeApp) {
			m.sidebarCursor = i
			return
		}
	}
	m.clampSidebarCursor()
}

func flattenThreadMessages(messages []brokerMessage, expanded map[string]bool) []threadedMessage {
	if len(messages) == 0 {
		return nil
	}

	// Sort all messages by timestamp first
	sorted := make([]brokerMessage, len(messages))
	copy(sorted, messages)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Timestamp < sorted[j].Timestamp
	})

	byID := make(map[string]brokerMessage, len(sorted))
	children := make(map[string][]brokerMessage)
	var roots []brokerMessage

	for _, msg := range sorted {
		byID[msg.ID] = msg
	}
	for _, msg := range sorted {
		if msg.ReplyTo != "" {
			if _, ok := byID[msg.ReplyTo]; ok {
				children[msg.ReplyTo] = append(children[msg.ReplyTo], msg)
				continue
			}
		}
		roots = append(roots, msg)
	}

	var out []threadedMessage
	var walk func(msg brokerMessage, depth int)
	walk = func(msg brokerMessage, depth int) {
		parentLabel := ""
		if msg.ReplyTo != "" {
			parentLabel = msg.ReplyTo
			if parent, ok := byID[msg.ReplyTo]; ok {
				parentLabel = "@" + parent.From
			}
		}
		tm := threadedMessage{
			Message:     msg,
			Depth:       depth,
			ParentLabel: parentLabel,
		}
		// Threads are expanded by default. Only collapse if explicitly set to false.
		if len(children[msg.ID]) > 0 {
			isExpanded, explicit := expanded[msg.ID]
			if explicit && !isExpanded {
				tm.Collapsed = true
				tm.HiddenReplies = countThreadReplies(children, msg.ID)
				tm.ThreadParticipants = threadParticipants(children, msg.ID)
			}
		}
		out = append(out, tm)
		if tm.Collapsed {
			return
		}
		for _, child := range children[msg.ID] {
			walk(child, depth+1)
		}
	}

	for _, root := range roots {
		walk(root, 0)
	}
	return out
}

func countThreadReplies(children map[string][]brokerMessage, rootID string) int {
	count := 0
	for _, child := range children[rootID] {
		count++
		count += countThreadReplies(children, child.ID)
	}
	return count
}

func threadParticipants(children map[string][]brokerMessage, rootID string) []string {
	seen := make(map[string]bool)
	var participants []string
	var walk func(id string)
	walk = func(id string) {
		for _, child := range children[id] {
			name := displayName(child.From)
			if !seen[name] {
				seen[name] = true
				participants = append(participants, name)
			}
			walk(child.ID)
		}
	}
	walk(rootID)
	return participants
}

func findMessageByID(messages []brokerMessage, id string) (brokerMessage, bool) {
	for _, msg := range messages {
		if msg.ID == id {
			return msg, true
		}
	}
	return brokerMessage{}, false
}

// buildThreadPickerOptions returns picker options for all root messages that have replies.
func (m channelModel) buildThreadPickerOptions() []tui.PickerOption {
	// Find root messages with replies
	replyCount := make(map[string]int)
	for _, msg := range m.messages {
		if msg.ReplyTo != "" {
			replyCount[msg.ReplyTo]++
		}
	}

	var options []tui.PickerOption
	for _, msg := range m.messages {
		count, hasReplies := replyCount[msg.ID]
		if !hasReplies || msg.ReplyTo != "" {
			continue // skip non-root or messages without replies
		}

		preview := truncateText(msg.Content, 50)
		status := "collapsed"
		if m.expandedThreads[msg.ID] {
			status = "expanded"
		}

		options = append(options, tui.PickerOption{
			Label:       fmt.Sprintf("@%s: %s", msg.From, preview),
			Value:       msg.ID,
			Description: fmt.Sprintf("%d replies · %s", count, status),
		})
	}
	return options
}

func (m channelModel) buildRequestPickerOptions() []tui.PickerOption {
	options := make([]tui.PickerOption, 0, len(m.requests))
	for _, req := range m.requests {
		if req.Channel != "" && req.Channel != m.activeChannel {
			continue
		}
		if req.Status != "" && req.Status != "pending" && req.Status != "open" {
			continue
		}
		label := req.Question
		if strings.TrimSpace(req.Title) != "" {
			label = req.Title
		}
		desc := fmt.Sprintf("%s from @%s", req.Kind, req.From)
		if req.Blocking {
			desc += " · blocking"
		}
		options = append(options, tui.PickerOption{
			Label:       truncateText(label, 56),
			Value:       req.ID,
			Description: desc,
		})
	}
	return options
}

func (m channelModel) buildTaskPickerOptions() []tui.PickerOption {
	options := make([]tui.PickerOption, 0, len(m.tasks))
	for _, task := range m.tasks {
		taskChannel := strings.ToLower(strings.TrimSpace(task.Channel))
		if taskChannel == "" {
			taskChannel = "general"
		}
		if taskChannel != strings.ToLower(strings.TrimSpace(m.activeChannel)) {
			continue
		}
		label := task.Title
		if strings.TrimSpace(task.Owner) != "" {
			label = fmt.Sprintf("%s · %s", task.Title, displayName(task.Owner))
		}
		desc := task.Status
		if task.ThreadID != "" {
			desc += " · thread " + task.ThreadID
		}
		options = append(options, tui.PickerOption{
			Label:       truncateText(label, 56),
			Value:       task.ID,
			Description: desc,
		})
	}
	return options
}

func (m channelModel) buildTaskActionPickerOptions(task channelTask) []tui.PickerOption {
	options := []tui.PickerOption{
		{Label: "Claim task", Value: "claim:" + task.ID, Description: "Take ownership as you"},
		{Label: "Release task", Value: "release:" + task.ID, Description: "Clear the current owner"},
	}
	if task.ReviewState == "ready_for_review" || task.Status == "review" {
		options = append(options, tui.PickerOption{Label: "Approve task", Value: "approve:" + task.ID, Description: "Mark this review-ready task done"})
	} else if task.ReviewState == "pending_review" || task.ExecutionMode == "local_worktree" {
		options = append(options, tui.PickerOption{Label: "Ready for review", Value: "complete:" + task.ID, Description: "Move this task into review"})
	} else {
		options = append(options, tui.PickerOption{Label: "Complete task", Value: "complete:" + task.ID, Description: "Mark this task done"})
	}
	if task.Status != "done" {
		options = append(options, tui.PickerOption{Label: "Block task", Value: "block:" + task.ID, Description: "Mark this work blocked"})
	}
	if task.ThreadID != "" {
		options = append(options, tui.PickerOption{Label: "Open thread", Value: "open:" + task.ID, Description: "Jump to the thread for this task"})
	}
	return options
}

func (m channelModel) buildRequestActionPickerOptions(req channelInterview) []tui.PickerOption {
	options := []tui.PickerOption{
		{Label: "Focus request", Value: "focus:" + req.ID, Description: "Open this request in the app"},
		{Label: "Answer request", Value: "answer:" + req.ID, Description: "Bring it into the composer"},
		{Label: "Snooze request", Value: "snooze:" + req.ID, Description: "Hide it locally until you revisit it"},
	}
	if req.ReplyTo != "" {
		options = append(options, tui.PickerOption{Label: "Open thread", Value: "open:" + req.ID, Description: "Jump to the related thread"})
	}
	return options
}

func (m channelModel) findTaskByID(id string) (channelTask, bool) {
	for _, task := range m.tasks {
		if task.ID == id {
			return task, true
		}
	}
	return channelTask{}, false
}

func (m channelModel) findRequestByID(id string) (channelInterview, bool) {
	for _, req := range m.requests {
		if req.ID == id {
			return req, true
		}
	}
	return channelInterview{}, false
}

func (m channelModel) focusRequest(req channelInterview, notice string) (tea.Model, tea.Cmd) {
	if req.Blocking || req.Required {
		m.activeApp = officeAppMessages
	} else {
		m.activeApp = officeAppRequests
	}
	m.syncSidebarCursorToActive()
	m.pending = &req
	m.snoozedInterview = ""
	m.selectedOption = m.recommendedOptionIndex()
	m.notice = notice
	if req.ReplyTo != "" {
		m.threadPanelOpen = true
		m.threadPanelID = req.ReplyTo
	}
	return m, tea.Batch(pollRequests(m.activeChannel))
}

func (m channelModel) answerRequest(req channelInterview) (tea.Model, tea.Cmd) {
	if req.Blocking || req.Required {
		m.activeApp = officeAppMessages
	} else {
		m.activeApp = officeAppRequests
	}
	m.syncSidebarCursorToActive()
	m.pending = &req
	m.snoozedInterview = ""
	m.selectedOption = m.recommendedOptionIndex()
	m.notice = "Answering request " + req.ID + ". Type your answer and press Enter."
	if req.ReplyTo != "" {
		m.threadPanelOpen = true
		m.threadPanelID = req.ReplyTo
	}
	return m, nil
}

func (m *channelModel) openTaskActionPicker(task channelTask) tea.Cmd {
	actions := m.buildTaskActionPickerOptions(task)
	if len(actions) == 0 {
		return nil
	}
	m.picker = tui.NewPicker("Task: "+truncateText(task.Title, 40), actions)
	m.picker.SetActive(true)
	m.pickerMode = channelPickerTaskAction
	m.notice = "Choose a task action."
	return nil
}

func (m *channelModel) openRequestActionPicker(req channelInterview) tea.Cmd {
	actions := m.buildRequestActionPickerOptions(req)
	if len(actions) == 0 {
		return nil
	}
	m.picker = tui.NewPicker("Request: "+truncateText(req.TitleOrQuestion(), 40), actions)
	m.picker.SetActive(true)
	m.pickerMode = channelPickerRequestAction
	m.notice = "Choose a request action."
	return nil
}

func (req channelInterview) TitleOrQuestion() string {
	if strings.TrimSpace(req.Title) != "" {
		return req.Title
	}
	return req.Question
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func hasThreadReplies(messages []brokerMessage, id string) bool {
	for _, msg := range messages {
		if msg.ReplyTo == id {
			return true
		}
	}
	return false
}

func containsSlug(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func pluralizeWord(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	if strings.TrimSpace(plural) != "" {
		return plural
	}
	return singular + "s"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampScroll(total, viewHeight, scroll int) int {
	if scroll < 0 {
		return 0
	}
	maxScroll := total - viewHeight
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		return maxScroll
	}
	return scroll
}

// mergeOfficeMembers returns all current channel members enriched with office roster
// metadata and broker activity. Members who have not posted yet still appear as idle.
func mergeOfficeMembers(officeMembers []officeMemberInfo, brokerMembers []channelMember, channel *channelInfo) []channelMember {
	memberOrder := make([]string, 0)
	if channel != nil && len(channel.Members) > 0 {
		memberOrder = append(memberOrder, channel.Members...)
	} else {
		for _, member := range officeMembers {
			memberOrder = append(memberOrder, member.Slug)
		}
	}

	officeMap := make(map[string]officeMemberInfo, len(officeMembers))
	for _, member := range officeMembers {
		officeMap[member.Slug] = member
	}
	brokerMap := make(map[string]channelMember, len(brokerMembers))
	for _, member := range brokerMembers {
		brokerMap[member.Slug] = member
	}

	result := make([]channelMember, 0, len(memberOrder))
	for _, slug := range memberOrder {
		member := brokerMap[slug]
		member.Slug = slug
		if meta, ok := officeMap[slug]; ok {
			if member.Name == "" {
				member.Name = meta.Name
			}
			if member.Role == "" {
				member.Role = meta.Role
			}
		}
		if member.Name == "" {
			member.Name = displayName(slug)
		}
		if member.Role == "" {
			member.Role = roleLabel(slug)
		}
		result = append(result, member)
	}
	for _, member := range brokerMembers {
		if containsSlug(memberOrder, member.Slug) {
			continue
		}
		result = append(result, member)
	}
	return result
}

func officeMembersFromManifest(manifest company.Manifest) []officeMemberInfo {
	members := make([]officeMemberInfo, 0, len(manifest.Members))
	for _, member := range manifest.Members {
		members = append(members, officeMemberInfo{
			Slug:        member.Slug,
			Name:        member.Name,
			Role:        member.Role,
			Expertise:   append([]string(nil), member.Expertise...),
			Personality: member.Personality,
			BuiltIn:     member.System,
		})
	}
	return members
}

func channelInfosFromManifest(manifest company.Manifest) []channelInfo {
	channels := make([]channelInfo, 0, len(manifest.Channels))
	for _, channel := range manifest.Channels {
		channels = append(channels, channelInfo{
			Slug:     channel.Slug,
			Name:     channel.Name,
			Members:  append([]string(nil), channel.Members...),
			Disabled: append([]string(nil), channel.Disabled...),
		})
	}
	return channels
}

func officeMembersFallback(existing []officeMemberInfo) []officeMemberInfo {
	if len(existing) > 0 {
		return existing
	}
	manifest, err := company.LoadManifest()
	if err != nil {
		manifest = company.DefaultManifest()
	}
	return officeMembersFromManifest(manifest)
}

func channelInfosFallback(existing []channelInfo) []channelInfo {
	if len(existing) > 0 {
		return existing
	}
	manifest, err := company.LoadManifest()
	if err != nil {
		manifest = company.DefaultManifest()
	}
	return channelInfosFromManifest(manifest)
}

func displayName(slug string) string {
	if member, ok := officeDirectory[slug]; ok && member.Name != "" {
		return member.Name
	}
	switch slug {
	case "ceo":
		return "CEO"
	case "pm":
		return "Product Manager"
	case "fe":
		return "Frontend Engineer"
	case "be":
		return "Backend Engineer"
	case "ai":
		return "AI Engineer"
	case "designer":
		return "Designer"
	case "cmo":
		return "CMO"
	case "cro":
		return "CRO"
	case "nex":
		return "Nex"
	case "you":
		return "You"
	default:
		return "@" + slug
	}
}

func roleLabel(slug string) string {
	if member, ok := officeDirectory[slug]; ok && member.Role != "" {
		return member.Role
	}
	switch slug {
	case "ceo":
		return "strategy"
	case "pm":
		return "product"
	case "fe":
		return "frontend"
	case "be":
		return "backend"
	case "ai":
		return "AI Engineer"
	case "designer":
		return "design"
	case "cmo":
		return "marketing"
	case "cro":
		return "revenue"
	case "nex":
		return "context graph"
	case "you":
		return "human"
	default:
		return "teammate"
	}
}

func renderDateSeparator(width int, label string) string {
	lineWidth := width - len(label) - 8
	if lineWidth < 4 {
		lineWidth = 4
	}
	segment := strings.Repeat("─", lineWidth/2)
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#64748B")).
		Render(fmt.Sprintf("%s  %s  %s", segment, label, segment))
}

func inferMood(text string) string {
	lower := strings.ToLower(text)
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "love this") || strings.Contains(lower, "excited") || strings.Contains(lower, "let's go") || strings.Contains(lower, "great wedge"):
		return "energized"
	case strings.Contains(lower, "hmm") || strings.Contains(lower, "skept") || strings.Contains(lower, "push back") || strings.Contains(lower, "bloodbath") || strings.Contains(lower, "crowded"):
		return "skeptical"
	case strings.Contains(lower, "worr") || strings.Contains(lower, "risk") || strings.Contains(lower, "concern"):
		return "concerned"
	case strings.Contains(lower, "blocked") || strings.Contains(lower, "stuck") || strings.Contains(lower, "hard part"):
		return "tense"
	case strings.Contains(lower, "done") || strings.Contains(lower, "shipped") || strings.Contains(lower, "works"):
		return "relieved"
	case strings.Contains(lower, "need") || strings.Contains(lower, "should") || strings.Contains(lower, "must") || strings.Contains(lower, "v1"):
		return "focused"
	default:
		return ""
	}
}

func renderInterviewCard(interview channelInterview, selected int, phaseTitle string, width int) string {
	cardWidth := width
	if cardWidth < 40 {
		cardWidth = 40
	}
	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24")).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F8FAFC")).Bold(true)
	textStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#E2E8F0"))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#94A3B8"))

	cardLabel := "Request"
	switch strings.TrimSpace(interview.Kind) {
	case "interview":
		cardLabel = "Human Interview"
	case "approval":
		cardLabel = "Approval Request"
	case "confirm":
		cardLabel = "Confirmation Request"
	case "secret":
		cardLabel = "Private Request"
	case "freeform":
		cardLabel = "Open Question"
	}
	title := fmt.Sprintf("@%s needs your decision", interview.From)
	if strings.TrimSpace(interview.Title) != "" {
		title = interview.Title + " · @" + interview.From
	}
	headerBits := []string{labelStyle.Render(cardLabel)}
	if strings.TrimSpace(phaseTitle) != "" {
		headerBits = append(headerBits, subtlePill(phaseTitle, "#DBEAFE", "#1D4ED8"))
	}
	if interview.Blocking {
		headerBits = append(headerBits, accentPill("blocking", "#B45309"))
	}
	if interview.Secret {
		headerBits = append(headerBits, accentPill("private", "#6D28D9"))
	}
	lines := []string{
		strings.Join(headerBits, "  "),
		titleStyle.Render(title),
		"",
		textStyle.Width(cardWidth - 4).Render(interview.Question),
	}
	if strings.TrimSpace(interview.Context) != "" {
		lines = append(lines, "")
		lines = append(lines, muted.Width(cardWidth-4).Render(interview.Context))
	}
	if timing := renderTimingSummary(interview.DueAt, interview.FollowUpAt, interview.ReminderAt, interview.RecheckAt); timing != "" {
		lines = append(lines, "", muted.Render(timing))
	}
	lines = append(lines, "", muted.Render("Options"))
	for i, option := range interview.Options {
		prefix := "  "
		if i == selected {
			prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true).Render("→ ")
		}
		label := option.Label
		if option.ID == interview.RecommendedID {
			label += " (Recommended)"
		}
		lines = append(lines, prefix+titleStyle.Render(label))
		if strings.TrimSpace(option.Description) != "" {
			lines = append(lines, "    "+muted.Width(cardWidth-8).Render(option.Description))
		}
	}
	if hint := interviewOptionTextHint(selectedInterviewOption(interview.Options, selected)); hint != "" {
		lines = append(lines, "", muted.Width(cardWidth-4).Render(hint))
	}
	customPrefix := "  "
	if selected >= len(interview.Options) {
		customPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#60A5FA")).Bold(true).Render("→ ")
	}
	customLine := lipgloss.NewStyle().
		Foreground(lipgloss.Color(slackMuted)).
		Render("Something else")
	lines = append(lines, customPrefix+customLine)
	lines = append(lines, "    "+muted.Width(cardWidth-8).Render("Type your own answer directly in the composer below."))
	lines = append(lines, "", muted.Render("Press Enter to accept the selected option, or type your own answer below."))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#F59E0B")).
		Padding(0, 1).
		Width(cardWidth).
		Render(strings.Join(lines, "\n")) + "\n"
}

func highlightMentions(text string, agentColors map[string]string) string {
	return mentionPattern.ReplaceAllStringFunc(text, func(match string) string {
		slug := strings.TrimPrefix(strings.ToLower(match), "@")
		color := agentColors[slug]
		if color == "" {
			return match
		}
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(color)).
			Bold(true).
			Render(match)
	})
}

func postToChannel(text string, replyTo string, channel string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"channel":  channel,
			"from":     "you",
			"content":  text,
			"tagged":   extractTagsFromText(text),
			"reply_to": strings.TrimSpace(replyTo),
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/messages", bytes.NewReader(body))
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			if len(body) == 0 {
				return channelPostDoneMsg{err: fmt.Errorf("broker returned %s", resp.Status)}
			}
			return channelPostDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		return channelPostDoneMsg{}
	}
}

func channelMentionAgents(members []channelMember) []tui.AgentMention {
	defaults := []tui.AgentMention{
		{Slug: "all", Name: "All agents"},
		{Slug: "ceo", Name: "CEO"},
		{Slug: "pm", Name: "Product Manager"},
		{Slug: "fe", Name: "Frontend Engineer"},
		{Slug: "be", Name: "Backend Engineer"},
		{Slug: "ai", Name: "AI Engineer"},
		{Slug: "designer", Name: "Designer"},
		{Slug: "cmo", Name: "CMO"},
		{Slug: "cro", Name: "CRO"},
	}
	seen := make(map[string]bool, len(defaults))
	mentions := make([]tui.AgentMention, 0, len(defaults)+len(members))
	for _, ag := range defaults {
		seen[ag.Slug] = true
		mentions = append(mentions, ag)
	}
	for _, member := range members {
		if seen[member.Slug] {
			continue
		}
		seen[member.Slug] = true
		mentions = append(mentions, tui.AgentMention{Slug: member.Slug, Name: displayName(member.Slug)})
	}
	return mentions
}

func (m *channelModel) updateOverlaysForInput(input []rune, cursor int) {
	text := string(input)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(input) {
		cursor = len(input)
	}
	m.autocomplete.UpdateQuery(strings.TrimLeft(text[:cursor], " "))
	m.mention.UpdateAgents(channelMentionAgents(m.members))
	m.mention.UpdateQuery(text[:cursor])
}

func (m *channelModel) updateInputOverlays() {
	m.updateOverlaysForInput(m.input, m.inputPos)
}

func (m *channelModel) updateThreadOverlays() {
	m.updateOverlaysForInput(m.threadInput, m.threadInputPos)
}

func (m *channelModel) updateOverlaysForCurrentInput() {
	if m.focus == focusThread && m.threadPanelOpen {
		m.updateThreadOverlays()
		return
	}
	if m.focus == focusMain {
		m.updateInputOverlays()
		m.maybeActivateChannelPickerFromInput()
		return
	}
	m.autocomplete.Dismiss()
	m.mention.Dismiss()
}

func (m *channelModel) setActiveInput(text string) {
	if m.focus == focusThread && m.threadPanelOpen {
		m.threadInput = []rune(text)
		m.threadInputPos = len(m.threadInput)
		m.threadInputHistory.resetRecall()
		m.updateThreadOverlays()
		return
	}
	m.input = []rune(text)
	m.inputPos = len(m.input)
	m.inputHistory.resetRecall()
	m.updateInputOverlays()
	m.maybeActivateChannelPickerFromInput()
}

func (m *channelModel) activeInputString() string {
	if m.focus == focusThread && m.threadPanelOpen {
		return string(m.threadInput)
	}
	return string(m.input)
}

func (m *channelModel) insertAcceptedMention(mention string) {
	if m.focus == focusThread && m.threadPanelOpen {
		m.threadInputHistory.resetRecall()
		m.threadInput, m.threadInputPos = replaceMentionInInput(m.threadInput, m.threadInputPos, mention)
		m.updateThreadOverlays()
		return
	}
	m.inputHistory.resetRecall()
	m.input, m.inputPos = replaceMentionInInput(m.input, m.inputPos, mention)
	m.updateInputOverlays()
}

func (m *channelModel) restoreMainSnapshot(snapshot composerSnapshot) {
	m.input = append([]rune(nil), snapshot.input...)
	m.inputPos = normalizeCursorPos(m.input, snapshot.pos)
	m.updateInputOverlays()
}

func (m *channelModel) restoreThreadSnapshot(snapshot composerSnapshot) {
	m.threadInput = append([]rune(nil), snapshot.input...)
	m.threadInputPos = normalizeCursorPos(m.threadInput, snapshot.pos)
	m.updateThreadOverlays()
}

func replaceMentionInInput(input []rune, pos int, mention string) ([]rune, int) {
	text := string(input)
	if pos < 0 {
		pos = 0
	}
	if pos > len(input) {
		pos = len(input)
	}
	atIdx := strings.LastIndex(text[:pos], "@")
	if atIdx < 0 {
		return input, pos
	}
	updated := []rune(text[:atIdx] + mention + " " + text[pos:])
	return updated, atIdx + len([]rune(mention)) + 1
}

func normalizeCursorPos(input []rune, pos int) int {
	if pos < 0 {
		return 0
	}
	if pos > len(input) {
		return len(input)
	}
	return pos
}

func isComposerWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

func moveCursorBackwardWord(input []rune, pos int) int {
	pos = normalizeCursorPos(input, pos)
	for pos > 0 && !isComposerWordRune(input[pos-1]) {
		pos--
	}
	for pos > 0 && isComposerWordRune(input[pos-1]) {
		pos--
	}
	return pos
}

func moveCursorForwardWord(input []rune, pos int) int {
	pos = normalizeCursorPos(input, pos)
	for pos < len(input) && isComposerWordRune(input[pos]) {
		pos++
	}
	for pos < len(input) && !isComposerWordRune(input[pos]) {
		pos++
	}
	return pos
}

func moveComposerCursor(input []rune, pos int, key string) (int, bool) {
	pos = normalizeCursorPos(input, pos)
	switch key {
	case "left", "ctrl+b", "alt+h":
		if pos > 0 {
			pos--
		}
		return pos, true
	case "right", "ctrl+f", "alt+l":
		if pos < len(input) {
			pos++
		}
		return pos, true
	case "ctrl+a", "alt+0":
		return 0, true
	case "ctrl+e", "alt+$":
		return len(input), true
	case "alt+b":
		return moveCursorBackwardWord(input, pos), true
	case "alt+w":
		return moveCursorForwardWord(input, pos), true
	default:
		return pos, false
	}
}

func composerMotionKey(msg tea.KeyMsg) (string, bool) {
	if msg.Alt && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case 'h':
			return "alt+h", true
		case 'l':
			return "alt+l", true
		case 'b':
			return "alt+b", true
		case 'w':
			return "alt+w", true
		case '0':
			return "alt+0", true
		case '$':
			return "alt+$", true
		}
	}
	switch msg.String() {
	case "ctrl+a", "ctrl+e", "ctrl+b", "ctrl+f", "left", "right", "alt+h", "alt+l", "alt+b", "alt+w", "alt+0", "alt+$":
		return msg.String(), true
	default:
		return "", false
	}
}

func composerInsertRunes(msg tea.KeyMsg) []rune {
	if msg.Type == tea.KeySpace || msg.String() == " " {
		return []rune{' '}
	}
	if msg.Alt {
		return nil
	}
	if len(msg.Runes) > 0 {
		return msg.Runes
	}
	return nil
}

func insertComposerRunes(input []rune, pos int, ch []rune) ([]rune, int) {
	pos = normalizeCursorPos(input, pos)
	if len(ch) == 0 {
		return input, pos
	}
	tail := make([]rune, len(input[pos:]))
	copy(tail, input[pos:])
	input = append(input[:pos], append(ch, tail...)...)
	return input, pos + len(ch)
}

func (m *channelModel) maybeActivateChannelPickerFromInput() bool {
	if m.focus != focusMain || m.picker.IsActive() || m.isOneOnOne() {
		return false
	}
	switch string(m.input) {
	case "/switch ", "/s ":
		options := m.buildSwitchChannelPickerOptions()
		if len(options) == 0 {
			m.notice = "No channels yet."
			return false
		}
		m.input = nil
		m.inputPos = 0
		m.autocomplete.Dismiss()
		m.mention.Dismiss()
		m.picker = tui.NewPicker("Switch Channel", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerChannels
		m.notice = "Choose a channel to switch to."
		return true
	default:
		return false
	}
}

func (m channelModel) renderActivePopup(width int) string {
	if width < 24 {
		width = 24
	}
	if m.autocomplete.IsVisible() {
		var options []composerPopupOption
		for _, cmd := range m.autocomplete.Matches() {
			meta := cmd.Description
			if strings.TrimSpace(cmd.Category) != "" {
				meta = strings.ToUpper(cmd.Category) + " · " + meta
			}
			options = append(options, composerPopupOption{
				Label: "/" + cmd.Name,
				Meta:  meta,
			})
		}
		return renderComposerPopup(options, m.autocomplete.SelectedIndex(), width, slackActive)
	}
	if m.mention.IsVisible() {
		var options []composerPopupOption
		for _, ag := range m.mention.Matches() {
			options = append(options, composerPopupOption{
				Label: "@" + ag.Slug,
				Meta:  ag.Name,
			})
		}
		return renderComposerPopup(options, m.mention.SelectedIndex(), width, "#2BAC76")
	}
	return ""
}

func overlayBottomLines(base, overlay []string) []string {
	if len(base) == 0 || len(overlay) == 0 {
		return base
	}
	out := append([]string(nil), base...)
	start := len(out) - len(overlay)
	if start < 0 {
		start = 0
		overlay = overlay[len(overlay)-len(out):]
	}
	for i, line := range overlay {
		out[start+i] = line
	}
	return out
}

func (m channelModel) runActiveCommand(trimmed string) (tea.Model, tea.Cmd) {
	threadTarget := ""
	if m.focus == focusThread && m.threadPanelOpen {
		threadTarget = m.threadPanelID
	}
	return m.runCommand(trimmed, threadTarget)
}

func (m channelModel) runCommand(trimmed, threadTarget string) (tea.Model, tea.Cmd) {
	clearMain := func() {
		m.input = nil
		m.inputPos = 0
	}
	clearThread := func() {
		m.threadInput = nil
		m.threadInputPos = 0
	}
	clearCurrent := func() {
		m.doctor = nil
		m.confirm = nil
		if threadTarget != "" {
			clearThread()
			m.updateThreadOverlays()
			return
		}
		clearMain()
		m.updateInputOverlays()
	}

	if m.isOneOnOne() && strings.HasPrefix(trimmed, "/") {
		// Blacklist: commands that only make sense in team/office mode
		teamOnly := []string{
			"/tasks", "/task ", "/task\n",
			"/channels", "/channel ", "/channel\n",
			"/agents", "/agent ", "/agent\n", "/agent prompt",
			"/reply ", "/reply\n",
			"/threads", "/expand ", "/expand\n", "/collapse ", "/collapse\n",
		}
		blocked := false
		for _, prefix := range teamOnly {
			if trimmed == strings.TrimSpace(prefix) || strings.HasPrefix(trimmed, prefix) {
				blocked = true
				break
			}
		}
		if blocked {
			m.notice = "1:1 mode disables office collaboration commands."
			return m, nil
		}
	}

	switch {
	case trimmed == "/quit" || trimmed == "/exit" || trimmed == "/q":
		killTeamSession()
		return m, tea.Quit
	case trimmed == "/1o1":
		clearCurrent()
		m.picker = tui.NewPicker("Direct Session", m.buildOneOnOneModePickerOptions())
		m.picker.SetActive(true)
		m.pickerMode = channelPickerOneOnOneMode
		return m, nil
	case strings.HasPrefix(trimmed, "/1o1 "):
		clearCurrent()
		agent := strings.TrimSpace(strings.TrimPrefix(trimmed, "/1o1"))
		if agent == "" {
			agent = team.DefaultOneOnOneAgent
		}
		m.posting = true
		return m, switchSessionMode(team.SessionModeOneOnOne, agent)
	case trimmed == "/messages" || trimmed == "/general":
		clearCurrent()
		m.activeApp = officeAppMessages
		m.syncSidebarCursorToActive()
		if m.isOneOnOne() {
			m.notice = "Viewing your direct session."
		} else {
			m.notice = "Viewing #general."
		}
		return m, nil
	case trimmed == "/inbox":
		clearCurrent()
		if !m.isOneOnOne() {
			m.notice = "/inbox only applies in direct 1:1 mode."
			return m, nil
		}
		m.activeApp = officeAppInbox
		m.syncSidebarCursorToActive()
		m.notice = "Viewing the selected agent inbox."
		return m, nil
	case trimmed == "/outbox":
		clearCurrent()
		if !m.isOneOnOne() {
			m.notice = "/outbox only applies in direct 1:1 mode."
			return m, nil
		}
		m.activeApp = officeAppOutbox
		m.syncSidebarCursorToActive()
		m.notice = "Viewing the selected agent outbox."
		return m, nil
	case trimmed == "/recover" || trimmed == "/resume":
		clearCurrent()
		m.activeApp = officeAppRecovery
		m.syncSidebarCursorToActive()
		if m.isOneOnOne() {
			m.notice = "Viewing the direct-session recovery summary."
		} else {
			m.notice = "Viewing the office recovery summary."
		}
		return m, m.pollCurrentState()
	case trimmed == "/rewind":
		clearCurrent()
		options := m.buildRecoveryPromptPickerOptions()
		if len(options) == 0 {
			m.notice = "Nothing recent to rewind from yet."
			return m, nil
		}
		m.picker = tui.NewPicker("Rewind From...", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerRewind
		m.notice = "Choose where recovery should start."
		return m, nil
	case trimmed == "/insert":
		clearCurrent()
		options := m.buildInsertPickerOptions()
		if len(options) == 0 {
			m.notice = "Nothing useful to insert right now."
			return m, nil
		}
		m.picker = tui.NewPicker("Insert Reference", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerInsert
		m.notice = "Choose a reference to insert into the composer."
		return m, nil
	case trimmed == "/search":
		clearCurrent()
		options := m.buildSearchPickerOptions()
		if len(options) == 0 {
			m.notice = "Nothing searchable yet."
			return m, nil
		}
		m.picker = tui.NewPicker("Search Workspace", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerSearch
		m.notice = "Choose where to jump next."
		return m, nil
	case trimmed == "/tasks":
		clearCurrent()
		m.activeApp = officeAppTasks
		m.syncSidebarCursorToActive()
		m.notice = "Viewing tasks in #" + m.activeChannel + "."
		return m, tea.Batch(pollTasks(m.activeChannel))
	case trimmed == "/task":
		clearCurrent()
		options := m.buildTaskPickerOptions()
		if len(options) == 0 {
			m.notice = "No open tasks in #" + m.activeChannel + "."
			return m, nil
		}
		m.picker = tui.NewPicker("Tasks in #"+m.activeChannel, options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerTasks
		return m, nil
	case strings.HasPrefix(trimmed, "/task "):
		clearCurrent()
		parts := strings.Fields(trimmed)
		if len(parts) < 3 {
			m.notice = "Usage: /task <claim|release|complete|review|approve|block> <task-id>"
			return m, nil
		}
		action, taskID := parts[1], parts[2]
		switch action {
		case "claim", "release", "complete", "review", "approve", "block":
			m.posting = true
			return m, mutateTask(action, taskID, "you", m.activeChannel)
		default:
			m.notice = "Usage: /task <claim|release|complete|review|approve|block> <task-id>"
			return m, nil
		}
	case trimmed == "/collab":
		clearCurrent()
		m.notice = "Switched to collaborative mode. All agents see all messages."
		return m, switchFocusMode(false)
	case trimmed == "/focus":
		clearCurrent()
		m.notice = "Switched to delegation mode. CEO routes, specialists execute."
		return m, switchFocusMode(true)
	case trimmed == "/reset":
		clearCurrent()
		m.confirm = m.confirmationForReset()
		m.notice = "Confirm reset."
		return m, nil
	case trimmed == "/reset-dm" || strings.HasPrefix(trimmed, "/reset-dm "):
		clearCurrent()
		agent := ""
		if strings.HasPrefix(trimmed, "/reset-dm ") {
			agent = strings.TrimSpace(strings.TrimPrefix(trimmed, "/reset-dm "))
			agent = strings.TrimPrefix(agent, "@")
		}
		if m.isOneOnOne() {
			agent = m.oneOnOneAgentSlug()
		}
		if agent == "" {
			m.notice = "Usage: /reset-dm <agent> or use in 1:1 mode"
			return m, nil
		}
		m.confirm = confirmationForResetDM(agent, m.activeChannel)
		m.notice = "Confirm clearing the direct transcript."
		return m, nil
	case trimmed == "/integrate":
		clearCurrent()
		if config.ResolveNoNex() {
			m.notice = "Nex integration is disabled for this session (--no-nex)."
			return m, nil
		}
		if config.ResolveAPIKey("") == "" {
			m.notice = "Run /init first. No WUPHF API key is configured."
			m.initFlow, _ = m.initFlow.Start()
			return m, nil
		}
		m.picker = tui.NewPicker("Choose Integration", channelIntegrationOptions())
		m.picker.SetActive(true)
		m.pickerMode = channelPickerIntegrations
		m.notice = "Choose an integration to connect."
		return m, nil
	case trimmed == "/doctor":
		clearCurrent()
		m.notice = "Checking readiness..."
		return m, runDoctorChecks()
	case trimmed == "/connect":
		clearCurrent()
		m.picker = tui.NewPicker("Connect a channel", []tui.PickerOption{
			{Label: "Telegram", Value: "telegram", Description: "Connect a Telegram group as a shared office channel"},
			{Label: "Slack (coming soon)", Value: "slack", Description: "Connect a Slack workspace channel"},
			{Label: "Discord (coming soon)", Value: "discord", Description: "Connect a Discord server channel"},
		})
		m.picker.SetActive(true)
		m.pickerMode = channelPickerConnect
		return m, nil
	case trimmed == "/connect telegram":
		clearCurrent()
		return m, m.startTelegramConnect()
	case trimmed == "/switch" || trimmed == "/s":
		clearCurrent()
		options := m.buildSwitchChannelPickerOptions()
		if len(options) == 0 {
			m.notice = "No channels yet."
			return m, nil
		}
		m.picker = tui.NewPicker("Switch Channel", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerChannels
		m.notice = "Choose a channel to switch to."
		return m, nil
	case trimmed == "/switcher":
		clearCurrent()
		options := m.buildWorkspaceSwitcherOptions()
		if len(options) == 0 {
			m.notice = "No destinations are available."
			return m, nil
		}
		m.picker = tui.NewPicker("Workspace Switcher", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerSwitcher
		m.notice = "Choose where to jump next."
		return m, nil
	case trimmed == "/channels":
		clearCurrent()
		options := m.buildChannelPickerOptions()
		if len(options) == 0 {
			m.notice = "No channels yet."
			return m, nil
		}
		m.picker = tui.NewPicker("Channels", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerChannels
		return m, nil
	case trimmed == "/requests":
		clearCurrent()
		m.activeApp = officeAppRequests
		m.syncSidebarCursorToActive()
		m.notice = "Viewing requests in #" + m.activeChannel + "."
		return m, tea.Batch(pollRequests(m.activeChannel))
	case trimmed == "/request":
		clearCurrent()
		options := m.buildRequestPickerOptions()
		if len(options) == 0 {
			m.notice = "No open requests in #" + m.activeChannel + "."
			return m, nil
		}
		m.picker = tui.NewPicker("Requests in #"+m.activeChannel, options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerRequests
		return m, nil
	case strings.HasPrefix(trimmed, "/request "):
		clearCurrent()
		parts := strings.Fields(trimmed)
		if len(parts) < 3 {
			m.notice = "Usage: /request <focus|answer|snooze> <request-id>"
			return m, nil
		}
		action, reqID := parts[1], parts[2]
		req, ok := m.findRequestByID(reqID)
		if !ok {
			m.notice = "Request not found: " + reqID
			return m, nil
		}
		switch action {
		case "focus":
			return m.focusRequest(req, "Focused request "+req.ID)
		case "answer":
			return m.answerRequest(req)
		case "snooze":
			if req.Blocking || req.Required {
				m.notice = "This decision cannot be snoozed. Answer it before the team continues."
				return m, nil
			}
			m.snoozedInterview = req.ID
			m.notice = "Request snoozed."
			return m, nil
		default:
			m.notice = "Usage: /request <focus|answer|snooze> <request-id>"
			return m, nil
		}
	case trimmed == "/policies":
		clearCurrent()
		m.activeApp = officeAppPolicies
		m.syncSidebarCursorToActive()
		m.notice = "Viewing Nex and office insights."
		return m, pollOfficeLedger()
	case trimmed == "/calendar" || trimmed == "/queue":
		clearCurrent()
		m.activeApp = officeAppCalendar
		m.syncSidebarCursorToActive()
		m.notice = "Viewing the office calendar."
		return m, pollOfficeLedger()
	case strings.HasPrefix(trimmed, "/calendar "):
		clearCurrent()
		parts := strings.Fields(trimmed)
		m.activeApp = officeAppCalendar
		m.syncSidebarCursorToActive()
		if len(parts) < 2 {
			m.notice = "Usage: /calendar [day|week|all|@agent|agent]"
			return m, nil
		}
		arg := strings.TrimSpace(parts[1])
		switch {
		case arg == "day" || arg == "today":
			m.calendarRange = calendarRangeDay
			m.notice = "Calendar now shows today."
			return m, pollOfficeLedger()
		case arg == "week":
			m.calendarRange = calendarRangeWeek
			m.notice = "Calendar now shows this week."
			return m, pollOfficeLedger()
		case arg == "all":
			m.calendarFilter = ""
			m.notice = "Showing all teammate calendars."
			return m, pollOfficeLedger()
		case arg == "filter":
			options := m.buildCalendarAgentPickerOptions()
			if len(options) == 0 {
				m.notice = "No teammate filters available."
				return m, nil
			}
			m.picker = tui.NewPicker("Filter Calendar", options)
			m.picker.SetActive(true)
			m.pickerMode = channelPickerCalendarAgent
			return m, nil
		default:
			filter := strings.TrimPrefix(arg, "@")
			if filter == "" {
				m.notice = "Usage: /calendar [day|week|all|@agent|agent]"
				return m, nil
			}
			m.calendarFilter = filter
			m.notice = "Filtering calendar for " + displayName(filter) + "."
			return m, pollOfficeLedger()
		}
	case trimmed == "/skills":
		clearCurrent()
		m.activeApp = officeAppSkills
		m.syncSidebarCursorToActive()
		m.notice = "Viewing skills."
		return m, pollSkills("")
	case trimmed == "/artifacts":
		clearCurrent()
		m.activeApp = officeAppArtifacts
		m.syncSidebarCursorToActive()
		m.notice = "Viewing recent execution artifacts."
		return m, m.pollCurrentState()
	case strings.HasPrefix(trimmed, "/skill create "):
		clearCurrent()
		desc := strings.TrimSpace(strings.TrimPrefix(trimmed, "/skill create "))
		if desc == "" {
			m.notice = "Usage: /skill create <description>"
			return m, nil
		}
		m.posting = true
		return m, createSkill(desc, m.activeChannel)
	case strings.HasPrefix(trimmed, "/skill invoke "):
		clearCurrent()
		name := strings.TrimSpace(strings.TrimPrefix(trimmed, "/skill invoke "))
		if name == "" {
			m.notice = "Usage: /skill invoke <name>"
			return m, nil
		}
		m.posting = true
		return m, invokeSkill(name)
	case trimmed == "/skill":
		clearCurrent()
		m.notice = "Usage: /skill create <description> or /skill invoke <name>"
		return m, nil
	case strings.HasPrefix(trimmed, "/skill "):
		clearCurrent()
		m.notice = "Usage: /skill create <description> or /skill invoke <name>"
		return m, nil
	case strings.HasPrefix(trimmed, "/channel "):
		clearCurrent()
		parts := strings.Fields(trimmed)
		if len(parts) < 3 {
			m.notice = "Usage: /channel add <slug> <description...> or /channel remove <slug>"
			return m, nil
		}
		switch parts[1] {
		case "add":
			description := strings.TrimSpace(strings.Join(parts[3:], " "))
			if description == "" {
				m.notice = "Usage: /channel add <slug> <description...>"
				return m, nil
			}
			m.posting = true
			return m, mutateChannel("create", parts[2], description)
		case "remove":
			m.posting = true
			return m, mutateChannel("remove", parts[2], "")
		default:
			m.notice = "Usage: /channel add <slug> <description...> or /channel remove <slug>"
			return m, nil
		}
	case trimmed == "/agents":
		clearCurrent()
		options := m.buildAgentPickerOptions()
		if len(options) == 0 {
			m.notice = "No agent actions available for this channel."
			return m, nil
		}
		m.picker = tui.NewPicker("Agents in #"+m.activeChannel, options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerAgents
		return m, nil
	case strings.HasPrefix(trimmed, "/agent "):
		clearCurrent()
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			m.notice = "Usage: /agent <add|remove|disable|enable> <slug>, /agent create, /agent edit <slug>, or /agent prompt <request>"
			return m, nil
		}
		if parts[1] == "prompt" {
			prompt := strings.TrimSpace(strings.TrimPrefix(trimmed, "/agent prompt"))
			if prompt == "" {
				m.notice = "Usage: /agent prompt <describe the teammate you want>"
				return m, nil
			}
			m.posting = true
			return m, generateOfficeMemberFromPrompt(prompt, m.activeChannel)
		}
		if parts[1] == "create" {
			if len(parts) == 2 {
				m.memberDraft = &channelMemberDraft{Mode: "create"}
				m.input = nil
				m.inputPos = 0
				m.notice = "New teammate setup started."
				return m, nil
			}
			if len(parts) < 4 {
				m.notice = "Usage: /agent create <slug> <Display Name>"
				return m, nil
			}
			m.posting = true
			return m, mutateOfficeMemberSpec(channelMemberDraft{
				Mode: "create",
				Slug: parts[2],
				Name: strings.Join(parts[3:], " "),
				Role: strings.Join(parts[3:], " "),
			}, m.activeChannel)
		}
		if parts[1] == "edit" {
			if len(parts) < 3 {
				m.notice = "Usage: /agent edit <slug>"
				return m, nil
			}
			draft, ok := m.startEditMemberDraft(parts[2])
			if !ok {
				m.notice = fmt.Sprintf("Office member %s not found.", parts[2])
				return m, nil
			}
			m.memberDraft = draft
			m.input = nil
			m.inputPos = 0
			m.notice = "Editing teammate profile."
			return m, nil
		}
		if parts[1] == "retire" {
			m.posting = true
			return m, mutateOfficeMember("remove", parts[2], "")
		}
		m.posting = true
		return m, mutateChannelMember(m.activeChannel, parts[1], parts[2])
	case trimmed == "/init":
		clearCurrent()
		if config.ResolveNoNex() {
			m.notice = "Nex integration is disabled for this session (--no-nex). Restart WUPHF without --no-nex to run setup."
			return m, nil
		}
		m.notice = "Starting setup..."
		var cmd tea.Cmd
		m.initFlow, cmd = m.initFlow.Start()
		return m, cmd
	case trimmed == "/provider":
		clearCurrent()
		m.picker = tui.NewPicker("Switch LLM Provider", tui.ProviderOptions())
		m.picker.SetActive(true)
		m.pickerMode = channelPickerProvider
		m.notice = "Choose an LLM provider."
		return m, nil
	case trimmed == "/cancel":
		clearCurrent()
		if m.replyToID != "" {
			m.replyToID = ""
			m.threadPanelOpen = false
			m.threadPanelID = ""
			clearThread()
			m.threadScroll = 0
			if m.focus == focusThread {
				m.focus = focusMain
			}
			m.notice = "Reply mode cleared."
		} else if m.doctor != nil {
			m.doctor = nil
			m.notice = "Doctor closed."
		} else if m.initFlow.IsActive() || m.initFlow.Phase() == tui.InitDone || m.picker.IsActive() {
			m.initFlow = tui.NewInitFlow()
			m.picker.SetActive(false)
			m.notice = "Setup canceled."
		} else {
			m.notice = "Nothing to cancel."
		}
		return m, nil
	case strings.HasPrefix(trimmed, "/reply"):
		clearCurrent()
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, "/reply"))
		if target == "" {
			m.notice = "Usage: /reply <message-id>"
			return m, nil
		}
		if _, ok := findMessageByID(m.messages, target); !ok {
			m.notice = fmt.Sprintf("Message %s not found.", target)
			return m, nil
		}
		m.replyToID = target
		m.threadPanelOpen = true
		m.threadPanelID = target
		clearThread()
		m.threadScroll = 0
		m.focus = focusThread
		m.notice = fmt.Sprintf("Replying in thread %s.", target)
		m.updateThreadOverlays()
		return m, nil
	case strings.HasPrefix(trimmed, "/expand"):
		clearCurrent()
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, "/expand"))
		if target == "" {
			m.notice = "Usage: /expand <message-id|all>"
			return m, nil
		}
		if target == "all" {
			for _, msg := range m.messages {
				if hasThreadReplies(m.messages, msg.ID) {
					m.expandedThreads[msg.ID] = true
				}
			}
			m.notice = "Expanded all threads."
			return m, nil
		}
		if _, ok := findMessageByID(m.messages, target); !ok {
			m.notice = fmt.Sprintf("Message %s not found.", target)
			return m, nil
		}
		m.expandedThreads[target] = true
		m.notice = fmt.Sprintf("Expanded thread %s.", target)
		return m, nil
	case strings.HasPrefix(trimmed, "/collapse"):
		clearCurrent()
		target := strings.TrimSpace(strings.TrimPrefix(trimmed, "/collapse"))
		if target == "" {
			m.notice = "Usage: /collapse <message-id|all>"
			return m, nil
		}
		if target == "all" {
			m.expandedThreads = make(map[string]bool)
			m.notice = "Collapsed all threads."
			return m, nil
		}
		if _, ok := findMessageByID(m.messages, target); !ok {
			m.notice = fmt.Sprintf("Message %s not found.", target)
			return m, nil
		}
		delete(m.expandedThreads, target)
		m.notice = fmt.Sprintf("Collapsed thread %s.", target)
		return m, nil
	case trimmed == "/threads":
		clearCurrent()
		options := m.buildThreadPickerOptions()
		if len(options) == 0 {
			m.notice = "No threads yet."
			return m, nil
		}
		m.picker = tui.NewPicker("Threads", options)
		m.picker.SetActive(true)
		m.pickerMode = channelPickerThreads
		return m, nil
	default:
		// Check if the command matches a skill name
		cmdName := strings.TrimPrefix(trimmed, "/")
		cmdName = strings.Fields(cmdName)[0] // first word only
		for _, sk := range m.skills {
			if sk.Name == cmdName && sk.Status == "active" {
				clearCurrent()
				m.posting = true
				m.notice = "Invoking skill: " + sk.Title
				return m, invokeSkill(sk.Name)
			}
		}
		return m, nil
	}
}

func extractTagsFromText(text string) []string {
	var tags []string
	for _, word := range strings.Fields(text) {
		if strings.HasPrefix(word, "@") && len(word) > 1 {
			tag := strings.TrimRight(word[1:], ".,!?;:")
			tags = append(tags, tag)
		}
	}
	return tags
}

func pollBroker(sinceID string, channel string) tea.Cmd {
	return func() tea.Msg {
		url := "http://127.0.0.1:7890/messages?limit=100&channel=" + channel
		if sinceID != "" {
			url += "&since_id=" + sinceID
		}
		req, err := newBrokerRequest(http.MethodGet, url, nil)
		if err != nil {
			return channelMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelMsg{}
		}

		var result struct {
			Messages []brokerMessage `json:"messages"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelMsg{}
		}
		return channelMsg{messages: result.Messages}
	}
}

func pollMembers(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/members?channel="+channel, nil)
		if err != nil {
			return channelMembersMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelMembersMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelMembersMsg{}
		}

		var result struct {
			Members []channelMember `json:"members"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelMembersMsg{}
		}
		return channelMembersMsg{members: result.Members}
	}
}

func pollOfficeMembers() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/office-members", nil)
		if err != nil {
			return channelOfficeMembersMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelOfficeMembersMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelOfficeMembersMsg{}
		}

		var result struct {
			Members []officeMemberInfo `json:"members"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelOfficeMembersMsg{}
		}
		return channelOfficeMembersMsg{members: result.Members}
	}
}

func pollChannels() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/channels", nil)
		if err != nil {
			return channelChannelsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelChannelsMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelChannelsMsg{}
		}

		var result struct {
			Channels []channelInfo `json:"channels"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelChannelsMsg{}
		}
		return channelChannelsMsg{channels: result.Channels}
	}
}

func channelExists(channels []channelInfo, slug string) bool {
	for _, ch := range channels {
		if ch.Slug == slug {
			return true
		}
	}
	return false
}

func (m channelModel) currentChannelInfo() *channelInfo {
	for i := range m.channels {
		if m.channels[i].Slug == m.activeChannel {
			return &m.channels[i]
		}
	}
	return nil
}

func (m channelModel) buildChannelPickerOptions() []tui.PickerOption {
	var options []tui.PickerOption
	for _, ch := range m.channels {
		description := strings.TrimSpace(ch.Description)
		if description == "" {
			description = fmt.Sprintf("%d members", len(ch.Members))
		} else {
			description = fmt.Sprintf("%s · %d members", description, len(ch.Members))
		}
		options = append(options, tui.PickerOption{
			Label:       "#" + ch.Slug,
			Value:       "switch:" + ch.Slug,
			Description: description,
		})
		if ch.Slug != "general" {
			options = append(options, tui.PickerOption{
				Label:       "Remove #" + ch.Slug,
				Value:       "remove:" + ch.Slug,
				Description: "Delete this channel and its messages/tasks",
			})
		}
	}
	return options
}

func (m channelModel) buildSwitchChannelPickerOptions() []tui.PickerOption {
	options := []tui.PickerOption{
		{Label: "Main office feed", Value: "app:messages", Description: "Return to the shared message stream"},
		{Label: "Tasks", Value: "app:tasks", Description: "Review active work for this channel"},
		{Label: "Requests", Value: "app:requests", Description: "Open pending approvals and interviews"},
		{Label: "Policies", Value: "app:policies", Description: "Show signals, decisions, and watchdogs"},
		{Label: "Calendar", Value: "app:calendar", Description: "View the office schedule and teammate calendars"},
	}
	if m.isOneOnOne() {
		options = append(options, tui.PickerOption{
			Label:       "Return to main office",
			Value:       "session:office",
			Description: "Leave direct mode and restore the shared office session",
		})
	} else {
		for _, member := range m.officeMembers {
			name := strings.TrimSpace(member.Name)
			if name == "" {
				name = displayName(member.Slug)
			}
			options = append(options, tui.PickerOption{
				Label:       "1:1 with " + name,
				Value:       "session:1o1:" + member.Slug,
				Description: "Jump into a direct session with " + name,
			})
		}
	}
	for _, option := range m.buildChannelPickerOptions() {
		if strings.HasPrefix(option.Value, "switch:") {
			options = append(options, option)
		}
	}
	return options
}

func (m channelModel) buildAgentPickerOptions() []tui.PickerOption {
	ch := m.currentChannelInfo()
	if ch == nil {
		return nil
	}
	officeMap := make(map[string]officeMemberInfo, len(m.officeMembers))
	for _, member := range m.officeMembers {
		officeMap[member.Slug] = member
	}
	disabled := make(map[string]bool, len(ch.Disabled))
	for _, slug := range ch.Disabled {
		disabled[slug] = true
	}
	var options []tui.PickerOption
	for _, slug := range ch.Members {
		name := displayName(slug)
		if meta, ok := officeMap[slug]; ok && meta.Name != "" {
			name = meta.Name
		}
		if slug != "ceo" && disabled[slug] {
			options = append(options, tui.PickerOption{
				Label:       "Enable " + name,
				Value:       "enable:" + slug,
				Description: "Allow this teammate to participate in #" + m.activeChannel,
			})
		} else if slug != "ceo" {
			options = append(options, tui.PickerOption{
				Label:       "Disable " + name,
				Value:       "disable:" + slug,
				Description: "Keep them in the channel but stop notifications there",
			})
		}
		if slug != "ceo" {
			options = append(options, tui.PickerOption{
				Label:       "Remove " + name,
				Value:       "remove:" + slug,
				Description: "Take them out of #" + m.activeChannel,
			})
		}
	}
	for _, member := range m.officeMembers {
		slug := member.Slug
		found := false
		for _, member := range ch.Members {
			if member == slug {
				found = true
				break
			}
		}
		if !found {
			options = append(options, tui.PickerOption{
				Label:       "Add " + member.Name,
				Value:       "add:" + slug,
				Description: "Add them to #" + m.activeChannel,
			})
		}
		if !member.BuiltIn {
			options = append(options, tui.PickerOption{
				Label:       "Edit " + member.Name,
				Value:       "edit:" + slug,
				Description: "Update role, expertise, personality, and permissions",
			})
		}
	}
	options = append(options, tui.PickerOption{
		Label:       "Create new office member…",
		Value:       "create:new",
		Description: "Use /agent create <slug> <Display Name> to add a brand-new teammate",
	})
	return options
}

func (m channelModel) buildOneOnOneModePickerOptions() []tui.PickerOption {
	enableDescription := "Restart WUPHF in direct mode with one selected agent and kill the rest of the Claude sessions"
	if m.isOneOnOne() {
		enableDescription = "Pick a different single agent for this direct session"
	}
	disableDescription := "Restart WUPHF with the full office team"
	if !m.isOneOnOne() {
		disableDescription = "Already using the full office team"
	}
	return []tui.PickerOption{
		{
			Label:       "Enable 1:1 mode",
			Value:       "enable",
			Description: enableDescription,
		},
		{
			Label:       "Disable 1:1 mode",
			Value:       "disable",
			Description: disableDescription,
		},
	}
}

func (m channelModel) buildOneOnOneAgentPickerOptions() []tui.PickerOption {
	options := make([]tui.PickerOption, 0, len(m.officeMembers))
	for _, member := range m.officeMembers {
		name := member.Name
		if strings.TrimSpace(name) == "" {
			name = displayName(member.Slug)
		}
		description := strings.TrimSpace(member.Role)
		if description == "" {
			description = "Direct session with " + name
		}
		options = append(options, tui.PickerOption{
			Label:       name,
			Value:       member.Slug,
			Description: description,
		})
	}
	return options
}

func (m channelModel) buildCalendarAgentPickerOptions() []tui.PickerOption {
	options := []tui.PickerOption{{
		Label:       "All teammates",
		Value:       "all",
		Description: "Show every participant across the office calendar",
	}}
	for _, member := range m.members {
		name := member.Name
		if strings.TrimSpace(name) == "" {
			name = displayName(member.Slug)
		}
		description := member.Role
		if strings.TrimSpace(description) == "" {
			description = "Show only " + name + "'s calendar"
		}
		options = append(options, tui.PickerOption{
			Label:       name,
			Value:       member.Slug,
			Description: description,
		})
	}
	return options
}

func pollHealth() tea.Cmd {
	return func() tea.Msg {
		client := &http.Client{Timeout: 1200 * time.Millisecond}
		resp, err := client.Get("http://127.0.0.1:7890/health")
		if err != nil {
			return channelHealthMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelHealthMsg{}
		}
		var result struct {
			Status        string `json:"status"`
			SessionMode   string `json:"session_mode"`
			OneOnOneAgent string `json:"one_on_one_agent"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelHealthMsg{Connected: true}
		}
		return channelHealthMsg{
			Connected:     true,
			SessionMode:   result.SessionMode,
			OneOnOneAgent: result.OneOnOneAgent,
		}
	}
}

func mutateChannel(action, slug, description string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":      action,
			"slug":        slug,
			"name":        slug,
			"description": description,
			"created_by":  "you",
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/channels", bytes.NewReader(body))
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return channelPostDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		if err := reconfigureLiveOfficeSession(); err != nil {
			return channelPostDoneMsg{err: err}
		}
		notice := ""
		switch action {
		case "create":
			notice = fmt.Sprintf("Created #%s.", normalizeSidebarSlug(slug))
		case "remove":
			notice = fmt.Sprintf("Removed #%s.", normalizeSidebarSlug(slug))
		}
		return channelPostDoneMsg{notice: notice, action: action, slug: normalizeSidebarSlug(slug)}
	}
}

func mutateChannelMember(channel, action, slug string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":  action,
			"channel": channel,
			"slug":    slug,
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/channel-members", bytes.NewReader(body))
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return channelPostDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		if err := reconfigureLiveOfficeSession(); err != nil {
			return channelPostDoneMsg{err: err}
		}
		notice := fmt.Sprintf("%s @%s in #%s.", strings.Title(action), normalizeSidebarSlug(slug), normalizeSidebarSlug(channel))
		return channelPostDoneMsg{notice: notice}
	}
}

func mutateOfficeMember(action, slug, name string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":     action,
			"slug":       slug,
			"name":       name,
			"role":       name,
			"created_by": "you",
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/office-members", bytes.NewReader(body))
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelPostDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return channelPostDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		if err := reconfigureLiveOfficeSession(); err != nil {
			return channelPostDoneMsg{err: err}
		}
		notice := fmt.Sprintf("%s @%s.", strings.Title(action), normalizeSidebarSlug(slug))
		return channelPostDoneMsg{notice: notice}
	}
}

func reconfigureLiveOfficeSession() error {
	l, err := team.NewLauncher("")
	if err != nil {
		return err
	}
	return l.ReconfigureSession()
}

func mutateTask(action, taskID, owner, channel string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":     action,
			"channel":    channel,
			"id":         taskID,
			"owner":      owner,
			"created_by": "you",
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/tasks", bytes.NewReader(body))
		if err != nil {
			return channelTaskMutationDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelTaskMutationDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			return channelTaskMutationDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		label := map[string]string{
			"claim":    "Task claimed.",
			"assign":   "Task assigned.",
			"complete": "Task completed.",
			"review":   "Task moved into review.",
			"approve":  "Task approved.",
			"block":    "Task marked blocked.",
			"release":  "Task released.",
		}[action]
		if label == "" {
			label = "Task updated."
		}
		return channelTaskMutationDoneMsg{notice: label}
	}
}

func pollUsage() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/usage", nil)
		if err != nil {
			return channelUsageMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelUsageMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelUsageMsg{}
		}

		var result channelUsageState
		if err := json.Unmarshal(body, &result); err != nil {
			return channelUsageMsg{}
		}
		if result.Agents == nil {
			result.Agents = make(map[string]channelUsageTotals)
		}
		return channelUsageMsg{usage: result}
	}
}

func pollTasks(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/tasks?channel="+channel, nil)
		if err != nil {
			return channelTasksMsg{}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return channelTasksMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelTasksMsg{}
		}
		var result struct {
			Tasks []channelTask `json:"tasks"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelTasksMsg{}
		}
		return channelTasksMsg{tasks: result.Tasks}
	}
}

func pollSkills(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/skills?channel="+channel, nil)
		if err != nil {
			return channelSkillsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelSkillsMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelSkillsMsg{}
		}
		var result struct {
			Skills []channelSkill `json:"skills"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelSkillsMsg{}
		}
		return channelSkillsMsg{skills: result.Skills}
	}
}

func createSkill(description, channel string) tea.Cmd {
	return func() tea.Msg {
		payload := map[string]string{
			"action":      "create",
			"description": description,
			"channel":     channel,
		}
		body, _ := json.Marshal(payload)
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/skills", bytes.NewReader(body))
		if err != nil {
			return channelSkillsMsg{}
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelSkillsMsg{}
		}
		defer resp.Body.Close()
		return channelSkillsMsg{}
	}
}

func invokeSkill(name string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/skills/"+name+"/invoke", nil)
		if err != nil {
			return channelSkillsMsg{}
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelSkillsMsg{}
		}
		defer resp.Body.Close()
		return channelSkillsMsg{}
	}
}

func pollOfficeLedger() tea.Cmd {
	return tea.Batch(
		pollActions(),
		pollSignals(),
		pollDecisions(),
		pollWatchdogs(),
		pollScheduler(),
	)
}

func pollActions() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/actions", nil)
		if err != nil {
			return channelActionsMsg{}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return channelActionsMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelActionsMsg{}
		}
		var result struct {
			Actions []channelAction `json:"actions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelActionsMsg{}
		}
		return channelActionsMsg{actions: result.Actions}
	}
}

func pollSignals() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/signals", nil)
		if err != nil {
			return channelSignalsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelSignalsMsg{}
		}
		defer resp.Body.Close()
		var result struct {
			Signals []channelSignal `json:"signals"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelSignalsMsg{}
		}
		return channelSignalsMsg{signals: result.Signals}
	}
}

func pollDecisions() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/decisions", nil)
		if err != nil {
			return channelDecisionsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelDecisionsMsg{}
		}
		defer resp.Body.Close()
		var result struct {
			Decisions []channelDecision `json:"decisions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelDecisionsMsg{}
		}
		return channelDecisionsMsg{decisions: result.Decisions}
	}
}

func pollWatchdogs() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/watchdogs", nil)
		if err != nil {
			return channelWatchdogsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelWatchdogsMsg{}
		}
		defer resp.Body.Close()
		var result struct {
			Watchdogs []channelWatchdog `json:"watchdogs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelWatchdogsMsg{}
		}
		return channelWatchdogsMsg{alerts: result.Watchdogs}
	}
}

func pollScheduler() tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/scheduler", nil)
		if err != nil {
			return channelSchedulerMsg{}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return channelSchedulerMsg{}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return channelSchedulerMsg{}
		}
		var result struct {
			Jobs []channelSchedulerJob `json:"jobs"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return channelSchedulerMsg{}
		}
		return channelSchedulerMsg{jobs: result.Jobs}
	}
}

func pollRequests(channel string) tea.Cmd {
	return func() tea.Msg {
		req, err := newBrokerRequest(http.MethodGet, "http://127.0.0.1:7890/requests?channel="+channel, nil)
		if err != nil {
			return channelRequestsMsg{}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelRequestsMsg{}
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return channelRequestsMsg{}
		}

		var result struct {
			Requests []channelInterview `json:"requests"`
			Pending  *channelInterview  `json:"pending"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return channelRequestsMsg{}
		}
		return channelRequestsMsg{requests: result.Requests, pending: result.Pending}
	}
}

func postHumanInterrupt(channel string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"action":   "create",
			"from":     "human",
			"channel":  channel,
			"question": "Human pressed Esc — all work paused. What should the team do now?",
			"kind":     "interrupt",
			"blocking": true,
			"required": true,
			"options": []map[string]string{
				{"id": "resume", "label": "Resume — carry on where you left off"},
				{"id": "stop", "label": "Stop — drop current tasks and wait"},
				{"id": "redirect", "label": "Redirect — I'll type new instructions"},
			},
		})
		req, _ := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/requests", bytes.NewReader(body))
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelInterruptDoneMsg{err: err}
		}
		defer resp.Body.Close()
		return channelInterruptDoneMsg{}
	}
}

func postInterviewAnswer(interview channelInterview, choiceID, choiceText, customText string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"id":          interview.ID,
			"choice_id":   choiceID,
			"choice_text": choiceText,
			"custom_text": customText,
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/requests/answer", bytes.NewReader(body))
		if err != nil {
			return channelInterviewAnswerDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelInterviewAnswerDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			if len(body) == 0 {
				return channelInterviewAnswerDoneMsg{err: fmt.Errorf("broker returned %s", resp.Status)}
			}
			return channelInterviewAnswerDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(body)))}
		}
		return channelInterviewAnswerDoneMsg{}
	}
}

func channelIntegrationOptions() []tui.PickerOption {
	options := make([]tui.PickerOption, 0, len(channelIntegrationSpecs))
	for _, spec := range channelIntegrationSpecs {
		options = append(options, tui.PickerOption{
			Label:       spec.Label,
			Value:       spec.Value,
			Description: spec.Description,
		})
	}
	return options
}

func findChannelIntegration(value string) (channelIntegrationSpec, bool) {
	for _, spec := range channelIntegrationSpecs {
		if spec.Value == value {
			return spec, true
		}
	}
	return channelIntegrationSpec{}, false
}

func connectIntegration(spec channelIntegrationSpec) tea.Cmd {
	return func() tea.Msg {
		apiKey := config.ResolveAPIKey("")
		if apiKey == "" {
			return channelIntegrationDoneMsg{err: errors.New("run /init first to configure your WUPHF API key")}
		}
		client := api.NewClient(apiKey)
		result, err := api.Post[map[string]any](client,
			fmt.Sprintf("/v1/integrations/%s/%s/connect", spec.Type, spec.Provider),
			nil,
			30*time.Second,
		)
		if err != nil {
			return channelIntegrationDoneMsg{err: err}
		}

		authURL := mapString(result, "auth_url")
		if authURL != "" {
			_ = openBrowserURL(authURL)
		}
		connectID := mapString(result, "connect_id")
		if connectID == "" {
			return channelIntegrationDoneMsg{label: spec.Label, url: authURL}
		}

		deadline := time.Now().Add(5 * time.Minute)
		for time.Now().Before(deadline) {
			time.Sleep(3 * time.Second)
			statusResp, err := api.Get[map[string]any](client,
				fmt.Sprintf("/v1/integrations/connect/%s/status", connectID),
				15*time.Second,
			)
			if err != nil {
				if _, ok := err.(*api.AuthError); ok {
					return channelIntegrationDoneMsg{err: err}
				}
				continue
			}
			status := strings.ToLower(mapString(statusResp, "status"))
			switch status {
			case "connected", "complete", "completed", "active":
				return channelIntegrationDoneMsg{label: spec.Label, url: authURL}
			case "failed", "error":
				reason := mapString(statusResp, "error")
				if reason == "" {
					reason = status
				}
				return channelIntegrationDoneMsg{err: fmt.Errorf("%s connection failed: %s", spec.Label, reason)}
			}
		}

		if authURL != "" {
			return channelIntegrationDoneMsg{err: fmt.Errorf("%s connection timed out. Finish OAuth at %s", spec.Label, authURL)}
		}
		return channelIntegrationDoneMsg{err: fmt.Errorf("%s connection timed out", spec.Label)}
	}
}

func (m *channelModel) startTelegramConnect() tea.Cmd {
	token := os.Getenv("WUPHF_TELEGRAM_BOT_TOKEN")
	if token == "" {
		token = config.ResolveTelegramBotToken()
	}
	if token != "" {
		m.posting = true
		m.notice = "Verifying bot token and discovering groups..."
		return discoverTelegramGroups(token)
	}
	// Show token input inside the picker overlay
	m.picker = tui.NewPicker("Connect Telegram", nil)
	m.picker.TextInput = true
	m.picker.TextPrompt = "Paste your bot token from @BotFather:"
	m.picker.SetActive(true)
	m.pickerMode = channelPickerTelegramToken
	return nil
}

func discoverTelegramGroups(token string) tea.Cmd {
	return func() tea.Msg {
		botName, err := team.VerifyBot(token)
		if err != nil {
			return telegramDiscoverMsg{err: fmt.Errorf("bot verification failed: %w", err)}
		}
		// Try getUpdates first
		groups, _ := team.DiscoverGroups(token)

		// Also fetch groups the transport has seen (via broker API)
		seen := make(map[int64]bool)
		for _, g := range groups {
			seen[g.ChatID] = true
		}
		req, reqErr := newBrokerRequest("GET", "http://127.0.0.1:7890/telegram/groups", nil)
		if reqErr == nil {
			client := &http.Client{Timeout: 2 * time.Second}
			if resp, err := client.Do(req); err == nil {
				defer resp.Body.Close()
				var result struct {
					Groups []struct {
						ChatID int64  `json:"chat_id"`
						Title  string `json:"title"`
					} `json:"groups"`
				}
				if json.NewDecoder(resp.Body).Decode(&result) == nil {
					for _, g := range result.Groups {
						if !seen[g.ChatID] {
							groups = append(groups, team.TelegramGroup{
								ChatID: g.ChatID,
								Title:  g.Title,
								Type:   "group",
							})
						}
					}
				}
			}
		}

		return telegramDiscoverMsg{
			botName: botName,
			groups:  groups,
			token:   token,
		}
	}
}

func connectTelegramGroup(token string, group team.TelegramGroup) tea.Cmd {
	return func() tea.Msg {
		slug := slugifyGroupTitle(group.Title)

		// Load manifest and add the new channel with telegram surface
		manifest, err := company.LoadManifest()
		if err != nil {
			manifest = company.DefaultManifest()
		}

		// Check if channel already exists (by slug or remote_id)
		remoteID := fmt.Sprintf("%d", group.ChatID)
		for _, ch := range manifest.Channels {
			if ch.Slug == slug {
				return telegramConnectDoneMsg{
					channelSlug: ch.Slug,
					groupTitle:  group.Title,
				}
			}
			if ch.Surface != nil && ch.Surface.Provider == "telegram" && ch.Surface.RemoteID == remoteID {
				return telegramConnectDoneMsg{
					channelSlug: ch.Slug,
					groupTitle:  group.Title,
				}
			}
		}

		// Build default members: lead + all manifest members
		members := []string{manifest.Lead}
		for _, member := range manifest.Members {
			if member.Slug != manifest.Lead {
				members = append(members, member.Slug)
			}
		}

		newChannel := company.ChannelSpec{
			Slug:        slug,
			Name:        group.Title,
			Description: fmt.Sprintf("Telegram bridge for %s.", group.Title),
			Members:     members,
			Surface: &company.ChannelSurfaceSpec{
				Provider:    "telegram",
				RemoteID:    fmt.Sprintf("%d", group.ChatID),
				RemoteTitle: group.Title,
				BotTokenEnv: "WUPHF_TELEGRAM_BOT_TOKEN",
			},
		}
		manifest.Channels = append(manifest.Channels, newChannel)
		if err := company.SaveManifest(manifest); err != nil {
			return telegramConnectDoneMsg{err: fmt.Errorf("failed to save manifest: %w", err)}
		}

		// Create channel in the live broker WITH surface metadata
		body, _ := json.Marshal(map[string]any{
			"action":      "create",
			"slug":        slug,
			"name":        group.Title,
			"description": fmt.Sprintf("Telegram bridge for %s.", group.Title),
			"members":     members,
			"created_by":  "you",
			"surface": map[string]any{
				"provider":      "telegram",
				"remote_id":     fmt.Sprintf("%d", group.ChatID),
				"remote_title":  group.Title,
				"mode":          group.Type,
				"bot_token_env": "WUPHF_TELEGRAM_BOT_TOKEN",
			},
		})
		req, reqErr := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/channels", bytes.NewReader(body))
		if reqErr == nil {
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
			}
		}

		// Send confirmation message to the Telegram group
		if group.ChatID != 0 {
			_ = team.SendTelegramMessage(token, group.ChatID,
				"Connected to WUPHF Office. Messages here will be visible to the team.")
		}

		// Clear broker state so next restart picks up the manifest with surfaces
		os.Remove(filepath.Join(os.Getenv("HOME"), ".wuphf", "team", "broker-state.json"))

		return telegramConnectDoneMsg{
			channelSlug: slug,
			groupTitle:  group.Title,
		}
	}
}

func slugifyGroupTitle(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "telegram"
	}
	// Prefix with tg- to make it clear it's a telegram channel
	return "tg-" + slug
}

func resetDMSession(agent string, channel string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"agent":   agent,
			"channel": channel,
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/reset-dm", bytes.NewReader(body))
		if err != nil {
			return channelResetDMDoneMsg{err: err}
		}
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return channelResetDMDoneMsg{err: err}
		}
		defer resp.Body.Close()
		var result struct {
			Removed int `json:"removed"`
		}
		json.NewDecoder(resp.Body).Decode(&result)
		return channelResetDMDoneMsg{removed: result.Removed}
	}
}

func resetTeamSession(oneOnOne bool) tea.Cmd {
	return func() tea.Msg {
		// Clear broker + Claude resume state and then rebuild the visible
		// team panes in place so reset does not leave dead panes behind.
		l, err := team.NewLauncher("")
		if err != nil {
			return channelResetDoneMsg{err: err}
		}
		if err := l.ResetSession(); err != nil {
			return channelResetDoneMsg{err: err}
		}
		if err := l.ReconfigureSession(); err != nil {
			return channelResetDoneMsg{err: err}
		}
		mode := team.SessionModeOffice
		agent := ""
		if oneOnOne {
			mode = team.SessionModeOneOnOne
		}
		if oneOnOne {
			return channelResetDoneMsg{notice: "Direct session reset. Agent pane reloaded in place.", sessionMode: mode, oneOnOneAgent: agent}
		}
		return channelResetDoneMsg{notice: "Office reset. Team panes reloaded in place.", sessionMode: mode, oneOnOneAgent: agent}
	}
}

func switchSessionMode(mode, agent string) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"mode":  mode,
			"agent": agent,
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/session-mode", bytes.NewReader(body))
		if err != nil {
			return channelResetDoneMsg{err: err}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return channelResetDoneMsg{err: err}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			raw, _ := io.ReadAll(resp.Body)
			return channelResetDoneMsg{err: fmt.Errorf("%s", strings.TrimSpace(string(raw)))}
		}
		var result struct {
			SessionMode   string `json:"session_mode"`
			OneOnOneAgent string `json:"one_on_one_agent"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			result.SessionMode = mode
			result.OneOnOneAgent = agent
		}

		l, err := team.NewLauncher("")
		if err != nil {
			return channelResetDoneMsg{err: err}
		}
		if err := l.ResetSession(); err != nil {
			return channelResetDoneMsg{err: err}
		}
		if err := l.ReconfigureSession(); err != nil {
			return channelResetDoneMsg{err: err}
		}
		switch team.NormalizeSessionMode(result.SessionMode) {
		case team.SessionModeOneOnOne:
			return channelResetDoneMsg{
				notice:        "Direct 1:1 with " + displayName(team.NormalizeOneOnOneAgent(result.OneOnOneAgent)) + " is ready.",
				sessionMode:   result.SessionMode,
				oneOnOneAgent: result.OneOnOneAgent,
			}
		default:
			return channelResetDoneMsg{
				notice:        "Office mode is ready.",
				sessionMode:   result.SessionMode,
				oneOnOneAgent: result.OneOnOneAgent,
			}
		}
	}
}

func switchFocusMode(enabled bool) tea.Cmd {
	return func() tea.Msg {
		body, _ := json.Marshal(map[string]any{
			"focus_mode": enabled,
		})
		req, err := newBrokerRequest(http.MethodPost, "http://127.0.0.1:7890/focus-mode", bytes.NewReader(body))
		if err != nil {
			return nil
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil
		}
		resp.Body.Close()
		return nil
	}
}

func applyTeamSetup() tea.Cmd {
	return func() tea.Msg {
		notice, err := setup.InstallLatestCLI()
		if err != nil {
			return channelInitDoneMsg{err: err}
		}
		cfg, _ := config.Load()
		if current := strings.TrimSpace(os.Getenv("WUPHF_HEADLESS_PROVIDER")); current != "" {
			return channelInitDoneMsg{notice: notice + " Setup saved. Restart WUPHF to reload the " + current + " office runtime with the new configuration."}
		}
		if config.ResolveLLMProvider("") == "codex" || strings.TrimSpace(cfg.LLMProvider) == "codex" {
			return channelInitDoneMsg{notice: notice + " Codex was saved as the LLM provider. Restart WUPHF to launch the headless Codex office runtime."}
		}
		l, err := team.NewLauncher("")
		if err != nil {
			return channelInitDoneMsg{err: err}
		}
		if err := l.ReconfigureSession(); err != nil {
			return channelInitDoneMsg{err: err}
		}
		return channelInitDoneMsg{notice: notice + " Setup applied. Team reloaded with the new configuration."}
	}
}

func applyProviderSelection(providerName string) tea.Cmd {
	return func() tea.Msg {
		providerName = strings.TrimSpace(providerName)
		if providerName == "" {
			return channelInitDoneMsg{err: errors.New("choose a provider")}
		}

		cfg, _ := config.Load()
		currentProvider := config.ResolveLLMProvider("")
		cfg.LLMProvider = providerName
		if err := config.Save(cfg); err != nil {
			return channelInitDoneMsg{err: err}
		}

		if current := strings.TrimSpace(os.Getenv("WUPHF_HEADLESS_PROVIDER")); current != "" {
			return channelInitDoneMsg{notice: "Provider switched to " + providerName + ". Restart WUPHF to reload the office runtime with the new configuration."}
		}
		if providerName == "codex" {
			l, err := team.NewLauncher("")
			if err != nil {
				return channelInitDoneMsg{err: err}
			}
			if err := l.ReconfigureSession(); err != nil {
				return channelInitDoneMsg{err: err}
			}
			return channelInitDoneMsg{notice: "Provider switched to codex. Claude teammate panes were stopped. Restart WUPHF to launch the headless Codex office runtime."}
		}
		if currentProvider == "codex" {
			return channelInitDoneMsg{notice: "Provider switched to " + providerName + ". Restart WUPHF to reload the office runtime with the new configuration."}
		}

		l, err := team.NewLauncher("")
		if err != nil {
			return channelInitDoneMsg{err: err}
		}
		if err := l.ReconfigureSession(); err != nil {
			return channelInitDoneMsg{err: err}
		}
		return channelInitDoneMsg{notice: "Provider switched to " + providerName + ". Team reloaded with the new configuration."}
	}
}

func tickChannel() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return channelTickMsg(t)
	})
}

func mapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		return fmt.Sprintf("%v", val)
	}
}

func openBrowserURL(url string) error {
	var cmd *exec.Cmd
	switch {
	case url == "":
		return nil
	case isDarwin():
		cmd = exec.Command("open", url)
	case isLinux():
		cmd = exec.Command("xdg-open", url)
	case isWindows():
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}

func isDarwin() bool  { return runtime.GOOS == "darwin" }
func isLinux() bool   { return runtime.GOOS == "linux" }
func isWindows() bool { return runtime.GOOS == "windows" }

// killTeamSession kills the entire wuphf-team tmux session and all agent processes.
func killTeamSession() {
	// Kill tmux session (kills all agent processes in all panes/windows)
	exec.Command("tmux", "-L", "wuphf", "kill-session", "-t", "wuphf-team").Run()
	// Stop the broker
	http.Get("http://127.0.0.1:7890/health") // just to check; broker stops with the process
}

func resolveInitialOfficeApp(name string) officeApp {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "insights" {
		return officeAppPolicies
	}
	switch officeApp(normalized) {
	case officeAppMessages, officeAppInbox, officeAppOutbox, officeAppRecovery, officeAppTasks, officeAppRequests, officeAppPolicies, officeAppCalendar, officeAppArtifacts:
		return officeApp(normalized)
	default:
		return officeAppMessages
	}
}

func runChannelView(threadsCollapsed bool, initialApp officeApp, skipSplash bool) {
	defer func() {
		if r := recover(); r != nil {
			reportChannelCrash(fmt.Sprintf("panic: %v\n\n%s", r, debug.Stack()))
		}
	}()

	if !skipSplash && os.Getenv("WUPHF_NO_SPLASH") == "" {
		splash := tea.NewProgram(newSplashModel(), tea.WithAltScreen())
		if _, err := splash.Run(); err != nil {
			reportChannelCrash(fmt.Sprintf("splash error: %v\n", err))
			return
		}
	}

	p := tea.NewProgram(newChannelModelWithApp(threadsCollapsed, initialApp), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		reportChannelCrash(fmt.Sprintf("channel view error: %v\n", err))
	}
}

func reportChannelCrash(details string) {
	_ = appendChannelCrashLog(details)
	fmt.Fprintln(os.Stderr, "WUPHF channel crashed.")
	fmt.Fprintln(os.Stderr, "Log:", channelCrashLogPath())
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "The rest of the team is still running.")
	if strings.TrimSpace(os.Getenv("WUPHF_HEADLESS_PROVIDER")) != "" {
		fmt.Fprintln(os.Stderr, "Restart WUPHF when ready to reconnect to the headless office runtime.")
	} else {
		fmt.Fprintln(os.Stderr, "Use `tmux -L wuphf attach -t wuphf-team` to inspect panes,")
		fmt.Fprintln(os.Stderr, "then restart WUPHF when ready.")
	}
	select {}
}

func appendChannelCrashLog(details string) error {
	path := channelCrashLogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n[%s]\n%s\n", time.Now().Format(time.RFC3339), details)
	return err
}

func channelCrashLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".wuphf-channel-crash.log"
	}
	return filepath.Join(home, ".wuphf", "logs", "channel-crash.log")
}
