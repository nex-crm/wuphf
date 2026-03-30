// Package team implements the WUPHF team launcher that starts a multi-agent
// collaborative team using tmux + Claude Code + the WUPHF office broker.
//
// Architecture:
//   - Each agent is a real Claude Code session in a tmux window
//   - the office broker provides the shared channel (all agents see all messages)
//   - Nex is an optional context layer, not a requirement
//   - CEO has final decision authority; agents participate when relevant
//   - Go TUI is the channel "observer" — displays the conversation
package team

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/calendar"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/tui"
)

const (
	SessionName                     = "wuphf-team"
	tmuxSocketName                  = "wuphf"
	defaultNotificationPollInterval = 15 * time.Minute
	channelRespawnDelay             = 8 * time.Second
	ceoHeadStartDelay               = 2 * time.Second
)

type nexFeedItemContentItem struct {
	Title         string `json:"title"`
	Context       string `json:"context"`
	EstimatedTime string `json:"estimated_time"`
}

type nexFeedItemContent struct {
	ImportantItems []nexFeedItemContentItem `json:"important_items"`
	EntityChanges  []nexFeedItemContentItem `json:"entity_changes"`
}

type nexFeedItem struct {
	ID        string             `json:"id"`
	Type      string             `json:"type"`
	Status    string             `json:"status"`
	AlertTime string             `json:"alert_time"`
	SentAt    string             `json:"sent_at"`
	Content   nexFeedItemContent `json:"content"`
}

type nexFeedResponse struct {
	Items []nexFeedItem `json:"items"`
}

type nexInsight struct {
	ID              string `json:"id"`
	Type            string `json:"type"`
	Content         string `json:"content"`
	ConfidenceLevel string `json:"confidence_level"`
	CreatedAt       string `json:"created_at"`
	Target          struct {
		Hint       string `json:"hint"`
		EntityType string `json:"entity_type"`
	} `json:"target"`
}

type nexInsightsEnvelope struct {
	Insights []nexInsight `json:"insights"`
}

type insightTaskPlan struct {
	Owner   string
	Title   string
	Details string
}

// Launcher sets up and manages the multi-agent team.
type Launcher struct {
	packSlug    string
	pack        *agent.PackDefinition
	sessionName string
	cwd         string
	broker      *Broker
	mcpConfig   string
	unsafe      bool
	sessionMode string
	oneOnOne    string
}

// SetUnsafe enables unrestricted permissions for all agents (CLI-only flag).
func (l *Launcher) SetUnsafe(v bool) { l.unsafe = v }

func (l *Launcher) SetOneOnOne(slug string) {
	l.sessionMode = SessionModeOneOnOne
	l.oneOnOne = NormalizeOneOnOneAgent(slug)
}

// NewLauncher creates a launcher for the given pack.
func NewLauncher(packSlug string) (*Launcher, error) {
	if packSlug == "" {
		cfg, _ := config.Load()
		packSlug = cfg.Pack
		if packSlug == "" {
			packSlug = "founding-team"
		}
	}

	pack := agent.GetPack(packSlug)
	if pack == nil {
		return nil, fmt.Errorf("unknown pack: %s", packSlug)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	sessionMode, oneOnOne := loadRunningSessionMode()

	return &Launcher{
		packSlug:    packSlug,
		pack:        pack,
		sessionName: SessionName,
		cwd:         cwd,
		sessionMode: sessionMode,
		oneOnOne:    oneOnOne,
	}, nil
}

// Preflight checks that required tools are available.
func (l *Launcher) Preflight() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found. Install: brew install tmux")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude not found. Install: npm install -g @anthropic-ai/claude-code")
	}
	return nil
}

// Launch creates the tmux session with:
//   - Window 0 "team": Channel on left, agent panes on the right
//   - No extra windows: all team activity is visible in one place
func (l *Launcher) Launch() error {
	mcpConfig, err := l.ensureMCPConfig()
	if err != nil {
		return fmt.Errorf("prepare mcp config: %w", err)
	}
	l.mcpConfig = mcpConfig

	// Kill any stale broker from a previous run
	killStaleBroker()

	// Start the shared channel broker
	l.broker = NewBroker()
	if err := l.broker.SetSessionMode(l.sessionMode, l.oneOnOne); err != nil {
		return fmt.Errorf("set session mode: %w", err)
	}
	if err := l.broker.Start(); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}

	// Kill any existing session
	exec.Command("tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()

	// Resolve wuphf binary path for the channel view
	wuphfBinary, _ := os.Executable()
	if err := os.MkdirAll(filepath.Dir(channelStderrLogPath()), 0o700); err != nil {
		return fmt.Errorf("prepare channel log dir: %w", err)
	}

	// Window 0 "team": channel on the left
	// Pass broker token via env so channel view + agents can authenticate
	channelEnv := []string{
		fmt.Sprintf("WUPHF_BROKER_TOKEN=%s", l.broker.Token()),
	}
	if l.isOneOnOne() {
		channelEnv = append(channelEnv,
			"WUPHF_ONE_ON_ONE=1",
			fmt.Sprintf("WUPHF_ONE_ON_ONE_AGENT=%s", l.oneOnOneAgent()),
		)
	}
	channelCmd := fmt.Sprintf("%s %s --channel-view 2>>%s", strings.Join(channelEnv, " "), wuphfBinary, shellQuote(channelStderrLogPath()))
	err = exec.Command("tmux", "-L", tmuxSocketName, "new-session", "-d",
		"-s", l.sessionName,
		"-n", "team",
		"-c", l.cwd,
		channelCmd,
	).Run()
	if err != nil {
		return fmt.Errorf("create tmux session: %w", err)
	}

	// Keep tmux mouse off for this session so native terminal selection/copy works.
	// WUPHF is keyboard-first; we don't want the TUI or tmux to steal mouse events.
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"mouse", "off",
	).Run()

	// Hide tmux's default status bar — our channel TUI has its own.
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"status", "off",
	).Run()
	// Keep panes visible if a process exits so crashes don't collapse the layout.
	exec.Command("tmux", "-L", tmuxSocketName, "set-window-option", "-t", l.sessionName+":team",
		"remain-on-exit", "on",
	).Run()

	visibleSlugs, err := l.spawnVisibleAgents()
	if err != nil {
		return err
	}
	l.spawnOverflowAgents()

	// Enable pane borders with labels and visible resize handles.
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-status", "top",
	).Run()
	// Show agent name in the border; mouse drag is intentionally disabled.
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-format", " #{pane_title} ",
	).Run()
	// Make inactive border visible but subtle
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-style", "fg=colour240",
	).Run()
	// Active pane border bright so you know which pane has focus
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-active-border-style", "fg=colour45",
	).Run()
	// Use line-drawing characters for clear keyboard-focused pane boundaries.
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-lines", "heavy",
	).Run()

	exec.Command("tmux", "-L", tmuxSocketName, "select-pane",
		"-t", l.sessionName+":team.0",
		"-T", "📢 channel",
	).Run()
	for i, slug := range visibleSlugs {
		if i == 0 {
			i = 1
		} else {
			i++
		}
		name := l.getAgentName(slug)
		exec.Command("tmux", "-L", tmuxSocketName, "select-pane",
			"-t", fmt.Sprintf("%s:team.%d", l.sessionName, i),
			"-T", fmt.Sprintf("🤖 %s (@%s)", name, slug),
		).Run()
	}

	// Focus on the channel pane.
	exec.Command("tmux", "-L", tmuxSocketName, "select-window",
		"-t", l.sessionName+":team",
	).Run()
	exec.Command("tmux", "-L", tmuxSocketName, "select-pane",
		"-t", l.sessionName+":team.0",
	).Run()

	// Start the notification loop that pushes new messages to agent panes
	go l.watchChannelPaneLoop(channelCmd)
	go l.primeVisibleAgents()
	go l.notifyAgentsLoop()
	if !l.isOneOnOne() {
		go l.notifyTaskActionsLoop()
		go l.pollNexNotificationsLoop()
		go l.watchdogSchedulerLoop()
	}

	return nil
}

// notifyAgentsLoop polls the broker for new messages and pushes them
// to agent Claude Code panes via tmux send-keys, prompting them to check the channel.
func (l *Launcher) notifyAgentsLoop() {
	lastCount := 0

	for {
		time.Sleep(500 * time.Millisecond)

		if l.broker != nil && l.broker.HasPendingInterview() {
			continue
		}

		msgs := l.broker.Messages()
		if len(msgs) < lastCount {
			lastCount = len(msgs)
		}
		if len(msgs) <= lastCount {
			continue
		}

		newMsgs := msgs[lastCount:]
		lastCount = len(msgs)

		for _, msg := range newMsgs {
			// Skip system/routing messages — they're progress signals, not real messages
			if msg.From == "system" {
				continue
			}
			l.persistHumanDirective(msg)
			l.deliverMessageNotification(msg)
		}
	}
}

func (l *Launcher) persistHumanDirective(msg channelMessage) {
	if l.broker == nil {
		return
	}
	signal, ok := buildHumanDirectiveSignal(msg)
	if !ok {
		return
	}
	records, err := l.broker.RecordSignals([]officeSignal{signal})
	if err != nil || len(records) == 0 {
		return
	}
	signalIDs := make([]string, 0, len(records))
	for _, record := range records {
		signalIDs = append(signalIDs, record.ID)
	}
	plan := planHumanDirective(msg)
	decision, err := l.broker.RecordDecision(
		plan.DecisionKind,
		signal.Channel,
		plan.Summary,
		plan.DecisionReason,
		"ceo",
		signalIDs,
		false,
		false,
	)
	if err != nil {
		return
	}
	_ = l.broker.RecordAction(
		"human_directive",
		"human",
		signal.Channel,
		msg.From,
		truncate(plan.Summary, 140),
		msg.ID,
		signalIDs,
		decision.ID,
	)
}

func (l *Launcher) notifyTaskActionsLoop() {
	lastCount := 0

	for {
		time.Sleep(500 * time.Millisecond)

		if l.broker == nil || l.broker.HasPendingInterview() {
			continue
		}

		actions := l.broker.Actions()
		if len(actions) < lastCount {
			lastCount = len(actions)
		}
		if len(actions) <= lastCount {
			continue
		}

		newActions := actions[lastCount:]
		lastCount = len(actions)

		for _, action := range newActions {
			if action.Kind != "task_created" && action.Kind != "task_updated" {
				continue
			}
			task, ok := l.taskForAction(action)
			if !ok || strings.EqualFold(strings.TrimSpace(task.Status), "done") {
				continue
			}
			l.deliverTaskNotification(action, task)
		}
	}
}

func (l *Launcher) deliverMessageNotification(msg channelMessage) {
	immediate, delayed := l.notificationTargetsForMessage(msg)

	// Broadcast stage update so the user sees immediate progress (only for human messages)
	if l.broker != nil && len(immediate) > 0 && (msg.From == "you" || msg.From == "human") {
		names := make([]string, 0, len(immediate))
		for _, t := range immediate {
			names = append(names, "@"+t.Slug)
		}
		channel := msg.Channel
		if channel == "" {
			channel = "general"
		}
		l.broker.PostSystemMessage(channel,
			fmt.Sprintf("Routing to %s...", strings.Join(names, ", ")),
			"routing",
		)
	}

	for _, target := range immediate {
		l.sendChannelUpdate(target.PaneTarget, target.Slug, msg.Channel, msg.ID, msg.From, msg.Content)
	}
	for _, target := range delayed {
		go func(target notificationTarget, msg channelMessage) {
			time.Sleep(ceoHeadStartDelay)
			if !l.shouldDeliverDelayedNotification(target.Slug, msg) {
				return
			}
			l.sendChannelUpdate(target.PaneTarget, target.Slug, msg.Channel, msg.ID, msg.From, msg.Content)
		}(target, msg)
	}
}

func (l *Launcher) deliverTaskNotification(action officeActionLog, task teamTask) {
	immediate, delayed := l.taskNotificationTargets(action, task)
	if len(immediate) == 0 && len(delayed) == 0 {
		return
	}
	content := l.taskNotificationContent(action, task)
	for _, target := range immediate {
		l.sendTaskUpdate(target.PaneTarget, target.Slug, task.Channel, task.ID, action.Actor, content)
	}
	for _, target := range delayed {
		go func(target notificationTarget, action officeActionLog, task teamTask) {
			time.Sleep(ceoHeadStartDelay)
			if !l.shouldDeliverDelayedTaskNotification(target.Slug, action, task) {
				return
			}
			l.sendTaskUpdate(target.PaneTarget, target.Slug, task.Channel, task.ID, action.Actor, content)
		}(target, action, task)
	}
}

type notificationTarget struct {
	PaneTarget string
	Slug       string
}

func (l *Launcher) taskNotificationTargets(action officeActionLog, task teamTask) (immediate []notificationTarget, delayed []notificationTarget) {
	targetMap := l.agentPaneTargets()
	if len(targetMap) == 0 {
		return nil, nil
	}
	lead := l.officeLeadSlug()
	enabledMembers := map[string]struct{}{}
	if l.broker != nil {
		for _, member := range l.broker.EnabledMembers(task.Channel) {
			enabledMembers[member] = struct{}{}
		}
	}
	addImmediate := func(slug string) {
		if slug == "" {
			return
		}
		if len(enabledMembers) > 0 {
			if _, ok := enabledMembers[slug]; !ok {
				return
			}
		}
		if target, ok := targetMap[slug]; ok {
			immediate = append(immediate, target)
			delete(targetMap, slug)
		}
	}
	addDelayed := func(slug string) {
		if slug == "" {
			return
		}
		if len(enabledMembers) > 0 {
			if _, ok := enabledMembers[slug]; !ok {
				return
			}
		}
		if target, ok := targetMap[slug]; ok {
			delayed = append(delayed, target)
			delete(targetMap, slug)
		}
	}
	actor := strings.TrimSpace(action.Actor)
	owner := strings.TrimSpace(task.Owner)

	if owner == "" {
		if lead != "" && lead != actor {
			addImmediate(lead)
		}
		return immediate, delayed
	}

	if owner == lead {
		if lead != "" && lead != actor {
			addImmediate(lead)
		}
		return immediate, delayed
	}

	// Assigned owners should start immediately when new work lands, especially
	// for CEO-created or automation-created tasks. This is the bridge between
	// "policy created work" and "the specialist actually begins moving."
	if (action.Kind == "task_created" || action.Kind == "watchdog_alert") && owner != actor {
		addImmediate(owner)
	} else if owner != actor {
		addDelayed(owner)
	}

	if lead != "" && lead != owner && lead != actor && !(action.Kind == "task_created" && actor == lead) {
		addImmediate(lead)
	}

	return immediate, delayed
}

func (l *Launcher) taskForAction(action officeActionLog) (teamTask, bool) {
	if l.broker == nil || strings.TrimSpace(action.RelatedID) == "" {
		return teamTask{}, false
	}
	channel := normalizeChannelSlug(action.Channel)
	if channel == "" {
		channel = "general"
	}
	for _, task := range l.broker.ChannelTasks(channel) {
		if task.ID == strings.TrimSpace(action.RelatedID) {
			return task, true
		}
	}
	return teamTask{}, false
}

func (l *Launcher) shouldDeliverDelayedTaskNotification(targetSlug string, action officeActionLog, task teamTask) bool {
	if l.broker == nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
		return false
	}
	current, ok := l.taskForAction(action)
	if !ok {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(current.Status), "done") {
		return false
	}
	if strings.TrimSpace(current.Owner) != "" && strings.TrimSpace(current.Owner) != targetSlug && targetSlug != l.officeLeadSlug() {
		return false
	}
	if strings.TrimSpace(current.Owner) == "" && targetSlug != l.officeLeadSlug() {
		return false
	}
	return true
}

func (l *Launcher) taskNotificationContent(action officeActionLog, task teamTask) string {
	channel := normalizeChannelSlug(task.Channel)
	if channel == "" {
		channel = "general"
	}
	verb := "Task update"
	switch action.Kind {
	case "task_created":
		verb = "Task created"
	case "task_updated":
		verb = "Task updated"
	case "watchdog_alert":
		verb = "Watchdog reminder"
	}
	owner := strings.TrimSpace(task.Owner)
	if owner == "" {
		owner = "unassigned"
	} else {
		owner = "@" + owner
	}
	status := strings.TrimSpace(task.Status)
	if status == "" {
		status = "open"
	}
	details := strings.TrimSpace(task.Details)
	if details != "" {
		details = " — " + truncate(details, 120)
	}
	pipeline := ""
	if strings.TrimSpace(task.PipelineStage) != "" {
		pipeline = ", stage " + task.PipelineStage
	}
	review := ""
	if strings.TrimSpace(task.ReviewState) != "" && task.ReviewState != "not_required" {
		review = ", review " + task.ReviewState
	}
	execMode := ""
	if strings.TrimSpace(task.ExecutionMode) != "" {
		execMode = ", execution " + task.ExecutionMode
	}
	return fmt.Sprintf("[%s #%s on #%s]: %s%s (owner %s, status %s%s%s%s). Before you speak, call team_poll and team_tasks, confirm whether you own this work, and stay in your lane.", verb, task.ID, channel, task.Title, details, owner, status, pipeline, review, execMode)
}

func (l *Launcher) sendTaskUpdate(paneTarget, slug, channel, taskID, from, content string) {
	if strings.TrimSpace(channel) == "" {
		channel = "general"
	}
	notification := fmt.Sprintf(
		"[Task update #%s %s from @%s]: %s — Before you say anything, call team_poll with my_slug \"%s\" and channel \"%s\", then call team_tasks for the current ownership. If the task is outside your lane or someone else owns it, stay quiet. If you are the owner, reply with the concrete next step and update the task.",
		channel, taskID, from, truncate(content, 150), slug, channel,
	)
	exec.Command("tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget,
		"-l",
		notification,
	).Run()
	exec.Command("tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget,
		"Enter",
	).Run()
}

func (l *Launcher) notificationTargetsForMessage(msg channelMessage) (immediate []notificationTarget, delayed []notificationTarget) {
	targetMap := l.agentPaneTargets()
	if len(targetMap) == 0 {
		return nil, nil
	}
	slugs := l.agentPaneSlugs()
	if l.isOneOnOne() {
		slug := l.oneOnOneAgent()
		if slug == "" || slug == msg.From {
			return nil, nil
		}
		target, ok := targetMap[slug]
		if !ok {
			return nil, nil
		}
		return []notificationTarget{target}, nil
	}
	lead := l.officeLeadSlug()
	domain := inferMessageDomain(msg)
	owner := ""
	if l.broker != nil {
		owner = l.taskOwnerForDomain(msg.Channel, domain)
	}
	enabledMembers := map[string]struct{}{}
	if l.broker != nil {
		for _, member := range l.broker.EnabledMembers(msg.Channel) {
			enabledMembers[member] = struct{}{}
		}
	}

	addImmediate := func(slug string) {
		if slug == "" || slug == msg.From {
			return
		}
		if len(enabledMembers) > 0 {
			if _, ok := enabledMembers[slug]; !ok {
				return
			}
		}
		if target, ok := targetMap[slug]; ok {
			immediate = append(immediate, target)
			delete(targetMap, slug)
		}
	}
	addDelayed := func(slug string) {
		if slug == "" || slug == msg.From {
			return
		}
		if len(enabledMembers) > 0 {
			if _, ok := enabledMembers[slug]; !ok {
				return
			}
		}
		if target, ok := targetMap[slug]; ok {
			delayed = append(delayed, target)
			delete(targetMap, slug)
		}
	}
	allowTarget := func(slug string) bool {
		if slug == "" || slug == msg.From {
			return false
		}
		if len(enabledMembers) > 0 {
			if _, ok := enabledMembers[slug]; !ok {
				return false
			}
		}
		if slug == lead {
			return true
		}
		if owner != "" && slug != owner {
			return false
		}
		if containsSlug(msg.Tagged, slug) {
			if domain == "" || domain == "general" {
				return true
			}
			return inferAgentDomain(slug) == domain
		}
		if domain == "" || domain == "general" {
			return false
		}
		return inferAgentDomain(slug) == domain
	}

	switch {
	case msg.From == "you" || msg.From == "human" || msg.Kind == "automation" || msg.From == "nex":
		// Direct routing: if user tags specific agents, route to them directly
		// without CEO bottleneck. CEO only gets untagged or multi-tagged messages.
		directRouted := false
		if len(msg.Tagged) > 0 && !containsSlug(msg.Tagged, lead) {
			for _, slug := range slugs {
				if slug == lead || slug == msg.From {
					continue
				}
				if containsSlug(msg.Tagged, slug) {
					if allowTarget(slug) {
						addImmediate(slug)
						directRouted = true
					}
				}
			}
		}
		if !directRouted {
			addImmediate(lead)
		}
		for _, slug := range slugs {
			if slug == lead || slug == msg.From {
				continue
			}
			if containsSlug(msg.Tagged, slug) && !directRouted {
				if allowTarget(slug) {
					addDelayed(slug)
				}
			}
		}
	case msg.From == lead:
		for _, slug := range slugs {
			if slug == lead || slug == msg.From {
				continue
			}
			if containsSlug(msg.Tagged, slug) || (domain != "" && domain != "general" && inferAgentDomain(slug) == domain) {
				if allowTarget(slug) {
					addImmediate(slug)
				}
			}
		}
	case containsSlug(msg.Tagged, lead):
		addImmediate(lead)
		for _, slug := range slugs {
			if slug == lead || slug == msg.From {
				continue
			}
			if containsSlug(msg.Tagged, slug) || (domain != "" && domain != "general" && inferAgentDomain(slug) == domain) {
				if allowTarget(slug) {
					addImmediate(slug)
				}
			}
		}
	default:
		addImmediate(lead)
		for _, slug := range slugs {
			if slug == lead || slug == msg.From {
				continue
			}
			if containsSlug(msg.Tagged, slug) || (domain != "" && domain != "general" && inferAgentDomain(slug) == domain) {
				if allowTarget(slug) {
					addImmediate(slug)
				}
			}
		}
	}
	return immediate, delayed
}

func (l *Launcher) shouldDeliverDelayedNotification(targetSlug string, source channelMessage) bool {
	if l.broker == nil {
		return true
	}
	if !containsSlug(l.broker.EnabledMembers(source.Channel), targetSlug) {
		return false
	}
	domain := inferMessageDomain(source)
	if owner := l.taskOwnerForDomain(source.Channel, domain); owner != "" && owner != targetSlug && targetSlug != l.officeLeadSlug() {
		return false
	}
	if domain != "" && domain != "general" && targetSlug != l.officeLeadSlug() && inferAgentDomain(targetSlug) != domain {
		return false
	}

	threadRoot := source.ID
	if source.ReplyTo != "" {
		threadRoot = source.ReplyTo
	}
	sourceIndex := -1
	messages := l.broker.ChannelMessages(source.Channel)
	for i := range messages {
		if messages[i].ID == source.ID {
			sourceIndex = i
			break
		}
	}
	if sourceIndex >= 0 {
		for _, msg := range messages[sourceIndex+1:] {
			sameThread := msg.ID == threadRoot || msg.ReplyTo == threadRoot || msg.ReplyTo == source.ID
			if !sameThread {
				continue
			}
			if msg.From == targetSlug {
				return false
			}
			if msg.From == l.officeLeadSlug() && !containsSlug(msg.Tagged, targetSlug) {
				return false
			}
			if msg.From != "you" && msg.From != "human" && msg.From != "nex" && msg.Kind != "automation" && !containsSlug(msg.Tagged, targetSlug) {
				return false
			}
		}
	}

	for _, task := range l.broker.ChannelTasks(source.Channel) {
		if task.Status == "done" {
			continue
		}
		if task.ThreadID != "" && task.ThreadID != source.ID && task.ThreadID != threadRoot {
			continue
		}
		if task.Owner != "" && task.Owner != targetSlug && targetSlug != l.officeLeadSlug() {
			return false
		}
	}
	return true
}

func (l *Launcher) taskOwnerForDomain(channel, domain string) string {
	if l.broker == nil || domain == "" || domain == "general" {
		return ""
	}
	var owner string
	for _, task := range l.broker.ChannelTasks(channel) {
		if task.Status == "done" {
			continue
		}
		if task.Owner == "" {
			continue
		}
		if inferAgentDomain(task.Owner) == domain {
			if owner == "" {
				owner = task.Owner
			}
			if task.Owner == owner {
				return owner
			}
		}
	}
	return owner
}

func (l *Launcher) watchChannelPaneLoop(channelCmd string) {
	unhealthyCount := 0
	var deadSince time.Time
	snapshotWritten := false
	for {
		time.Sleep(2 * time.Second)

		status, err := l.channelPaneStatus()
		if err != nil {
			if isNoSessionError(err.Error()) {
				return
			}
			continue
		}
		if !channelPaneNeedsRespawn(status) {
			unhealthyCount = 0
			deadSince = time.Time{}
			snapshotWritten = false
			continue
		}
		unhealthyCount++
		if unhealthyCount < 2 {
			continue
		}
		if deadSince.IsZero() {
			deadSince = time.Now()
		}
		if !snapshotWritten {
			_ = l.captureDeadChannelPane(status)
			snapshotWritten = true
		}
		if time.Since(deadSince) < channelRespawnDelay {
			continue
		}
		unhealthyCount = 0
		deadSince = time.Time{}
		snapshotWritten = false
		target := l.sessionName + ":team.0"
		exec.Command("tmux", "-L", tmuxSocketName, "respawn-pane", "-k",
			"-t", target,
			"-c", l.cwd,
			channelCmd,
		).Run()
		exec.Command("tmux", "-L", tmuxSocketName, "select-pane",
			"-t", target,
			"-T", "📢 channel",
		).Run()
	}
}

func (l *Launcher) channelPaneStatus() (string, error) {
	out, err := exec.Command("tmux", "-L", tmuxSocketName, "display-message",
		"-p",
		"-t", l.sessionName+":team.0",
		"#{pane_dead} #{pane_dead_status} #{pane_current_command}",
	).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func channelPaneNeedsRespawn(status string) bool {
	if strings.TrimSpace(status) == "" {
		return false
	}
	fields := strings.Fields(status)
	if len(fields) == 0 {
		return false
	}
	return fields[0] == "1"
}

func isNoSessionError(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "can't find") || strings.Contains(msg, "no server")
}

func (l *Launcher) captureDeadChannelPane(status string) error {
	content, err := l.capturePaneContent(0)
	if err != nil {
		content = fmt.Sprintf("<capture failed: %v>", err)
	}
	path := channelPaneSnapshotPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\n[%s] status=%s\n%s\n", time.Now().Format(time.RFC3339), status, content)
	return err
}

func channelStderrLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".wuphf-channel-stderr.log"
	}
	return filepath.Join(home, ".wuphf", "logs", "channel-stderr.log")
}

func channelPaneSnapshotPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".wuphf-channel-pane.log"
	}
	return filepath.Join(home, ".wuphf", "logs", "channel-pane.log")
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// primeVisibleAgents clears Claude startup interactivity in newly spawned panes and
// replays a catch-up channel nudge once they are actually ready to read it.
func (l *Launcher) primeVisibleAgents() {
	time.Sleep(1 * time.Second)

	targets := l.agentPaneTargets()
	if len(targets) == 0 {
		return
	}

	for attempt := 0; attempt < 3; attempt++ {
		allReady := true
		for _, target := range targets {
			content, err := l.capturePaneTargetContent(target.PaneTarget)
			if err != nil {
				allReady = false
				continue
			}
			if shouldPrimeClaudePane(content) {
				exec.Command("tmux", "-L", tmuxSocketName, "send-keys",
					"-t", target.PaneTarget,
					"Enter",
				).Run()
				allReady = false
			}
		}
		if allReady {
			break
		}
		time.Sleep(1 * time.Second)
	}

	// If the human already posted while Claude was still booting, replay a catch-up nudge
	// so the first visible message is not lost forever behind the startup interactivity.
	if l.broker == nil {
		return
	}
	msgs := l.broker.Messages()
	if len(msgs) == 0 {
		return
	}
	latest := msgs[len(msgs)-1]
	l.deliverMessageNotification(latest)
}

func (l *Launcher) pollNexNotificationsLoop() {
	if l.broker == nil {
		return
	}
	apiKey := config.ResolveAPIKey("")
	if apiKey == "" {
		return
	}
	client := api.NewClient(apiKey)
	interval := notificationPollInterval()

	time.Sleep(10 * time.Second)
	for {
		l.updateSchedulerJob("nex-notifications", "Nex notifications", interval, time.Now().UTC(), "running")
		l.fetchAndIngestNexNotifications(client)
		l.updateSchedulerJob("nex-notifications", "Nex notifications", interval, time.Now().UTC().Add(interval), "sleeping")
		time.Sleep(interval)
	}
}

func notificationPollInterval() time.Duration {
	if raw := os.Getenv("WUPHF_NOTIFY_INTERVAL_MINUTES"); raw != "" {
		if minutes, err := strconv.Atoi(raw); err == nil && minutes > 0 {
			return time.Duration(minutes) * time.Minute
		}
	}
	if raw := os.Getenv("NEX_NOTIFY_INTERVAL_MINUTES"); raw != "" {
		if minutes, err := strconv.Atoi(raw); err == nil && minutes > 0 {
			return time.Duration(minutes) * time.Minute
		}
	}
	return defaultNotificationPollInterval
}

func (l *Launcher) pollOneRelayEventsLoop() {
	if l.broker == nil {
		return
	}
	provider := action.NewOneCLIFromEnv()
	if _, err := provider.ListRelays(context.Background(), action.ListRelaysOptions{Limit: 1}); err != nil {
		return
	}
	interval := time.Minute
	time.Sleep(25 * time.Second)
	for {
		l.updateSchedulerJob("one-relay-events", "One relay events", interval, time.Now().UTC(), "running")
		l.fetchAndRecordOneRelayEvents(provider)
		l.updateSchedulerJob("one-relay-events", "One relay events", interval, time.Now().UTC().Add(interval), "sleeping")
		time.Sleep(interval)
	}
}

func (l *Launcher) fetchAndRecordOneRelayEvents(provider *action.OneCLI) {
	if l.broker == nil || provider == nil {
		return
	}
	result, err := provider.ListRelayEvents(context.Background(), action.RelayEventsOptions{Limit: 20})
	if err != nil {
		return
	}
	if len(result.Events) == 0 {
		return
	}
	var signals []officeSignal
	for _, event := range result.Events {
		title := strings.TrimSpace(event.EventType)
		if title == "" {
			title = "Relay event"
		}
		content := fmt.Sprintf("One relay received %s on %s.", strings.TrimSpace(event.EventType), strings.TrimSpace(event.Platform))
		signals = append(signals, officeSignal{
			ID:         strings.TrimSpace(event.ID),
			Source:     "one",
			Kind:       "relay_event",
			Title:      title,
			Content:    content,
			Channel:    "general",
			Owner:      "ceo",
			Confidence: "medium",
			Urgency:    "medium",
		})
	}
	records, err := l.broker.RecordSignals(signals)
	if err != nil || len(records) == 0 {
		return
	}
	for _, record := range records {
		_ = l.broker.RecordAction(
			"external_trigger_received",
			"one",
			record.Channel,
			"one",
			truncateSummary(record.Title+" "+record.Content, 140),
			record.ID,
			[]string{record.ID},
			"",
		)
	}
}

func (l *Launcher) pollNexInsightsLoop() {
	if l.broker == nil {
		return
	}
	apiKey := config.ResolveAPIKey("")
	if apiKey == "" {
		return
	}
	client := api.NewClient(apiKey)
	interval := time.Duration(config.ResolveInsightsPollInterval()) * time.Minute

	time.Sleep(20 * time.Second)
	for {
		l.updateSchedulerJob("nex-insights", "Nex insights", interval, time.Now().UTC(), "running")
		l.fetchAndPostNexInsights(client)
		l.updateSchedulerJob("nex-insights", "Nex insights", interval, time.Now().UTC().Add(interval), "sleeping")
		time.Sleep(interval)
	}
}

func (l *Launcher) updateSchedulerJob(slug, label string, interval time.Duration, nextRun time.Time, status string) {
	if l.broker == nil {
		return
	}
	job := schedulerJob{
		Slug:            slug,
		Label:           label,
		IntervalMinutes: int(interval / time.Minute),
		NextRun:         nextRun.UTC().Format(time.RFC3339),
		Status:          status,
	}
	if status == "sleeping" {
		job.LastRun = time.Now().UTC().Format(time.RFC3339)
	}
	_ = l.broker.SetSchedulerJob(job)
}

func (l *Launcher) watchdogSchedulerLoop() {
	if l.broker == nil {
		return
	}
	time.Sleep(15 * time.Second)
	for {
		l.processDueSchedulerJobs()
		time.Sleep(20 * time.Second)
	}
}

func (l *Launcher) processDueSchedulerJobs() {
	if l.broker == nil {
		return
	}
	jobs := l.broker.DueSchedulerJobs()
	if len(jobs) == 0 {
		return
	}
	for _, job := range jobs {
		switch strings.TrimSpace(job.TargetType) {
		case "task":
			l.processDueTaskJob(job)
		case "request":
			l.processDueRequestJob(job)
		case "workflow":
			l.processDueWorkflowJob(job)
		default:
			nextRun := time.Now().UTC().Add(time.Duration(config.ResolveTaskReminderInterval()) * time.Minute)
			_ = l.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
		}
	}
}

func (l *Launcher) processDueTaskJob(job schedulerJob) {
	task, ok := l.broker.FindTask(job.Channel, job.TargetID)
	if !ok || strings.EqualFold(strings.TrimSpace(task.Status), "done") {
		_ = l.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
		return
	}
	alertKind := "task_stalled"
	summary := fmt.Sprintf("Task %s in #%s is still waiting for movement.", task.Title, normalizeChannelSlug(task.Channel))
	if strings.TrimSpace(task.Owner) == "" {
		alertKind = "task_unclaimed"
		summary = fmt.Sprintf("Task %s in #%s still has no owner.", task.Title, normalizeChannelSlug(task.Channel))
	} else {
		summary = fmt.Sprintf("@%s still needs to move %s in #%s.", task.Owner, task.Title, normalizeChannelSlug(task.Channel))
	}
	_, _, _ = l.broker.CreateWatchdogAlert(alertKind, task.Channel, "task", task.ID, task.Owner, summary)
	signalIDs, decisionID := l.recordWatchdogLedger(task.Channel, alertKind, task.ID, task.Owner, summary, task.SourceSignalID)
	_ = l.broker.RecordAction("watchdog_alert", "watchdog", task.Channel, "watchdog", truncate(summary, 140), task.ID, signalIDs, decisionID)
	l.deliverTaskNotification(officeActionLog{
		Kind:      "watchdog_alert",
		Source:    "watchdog",
		Channel:   task.Channel,
		Actor:     "watchdog",
		RelatedID: task.ID,
	}, task)
	nextRun := time.Now().UTC().Add(time.Duration(config.ResolveTaskReminderInterval()) * time.Minute)
	_ = l.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
}

func (l *Launcher) processDueRequestJob(job schedulerJob) {
	req, ok := l.broker.FindRequest(job.Channel, job.TargetID)
	if !ok || !requestIsActive(req) {
		_ = l.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
		return
	}
	summary := fmt.Sprintf("Still waiting on %s in #%s: %s", req.TitleOrDefault(), normalizeChannelSlug(req.Channel), truncate(req.Question, 120))
	alert, existing, _ := l.broker.CreateWatchdogAlert("request_waiting", req.Channel, "request", req.ID, req.From, summary)
	signalIDs, decisionID := l.recordWatchdogLedger(req.Channel, "request_waiting", req.ID, req.From, summary, "")
	_ = l.broker.RecordAction("watchdog_alert", "watchdog", req.Channel, "watchdog", truncate(summary, 140), req.ID, signalIDs, decisionID)
	if req.Blocking && !existing {
		_, _, _ = l.broker.PostAutomationMessage(
			"wuphf",
			req.Channel,
			"Waiting on human decision",
			summary,
			alert.ID,
			"watchdog",
			"Office watchdog",
			[]string{"ceo"},
			req.ReplyTo,
		)
	}
	nextRun := time.Now().UTC().Add(time.Duration(config.ResolveTaskReminderInterval()) * time.Minute)
	_ = l.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
}

func (l *Launcher) processDueWorkflowJob(job schedulerJob) {
	if l.broker == nil {
		return
	}
	type workflowSchedulePayload struct {
		Provider     string         `json:"provider"`
		WorkflowKey  string         `json:"workflow_key"`
		Inputs       map[string]any `json:"inputs"`
		ScheduleExpr string         `json:"schedule_expr"`
		CreatedBy    string         `json:"created_by"`
		Channel      string         `json:"channel"`
		SkillName    string         `json:"skill_name"`
	}
	var payload workflowSchedulePayload
	if strings.TrimSpace(job.Payload) != "" {
		_ = json.Unmarshal([]byte(job.Payload), &payload)
	}
	workflowKey := strings.TrimSpace(payload.WorkflowKey)
	if workflowKey == "" {
		workflowKey = strings.TrimSpace(job.WorkflowKey)
	}
	if workflowKey == "" {
		_ = l.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
		return
	}
	channel := normalizeChannelSlug(payload.Channel)
	if channel == "" {
		channel = normalizeChannelSlug(job.Channel)
	}
	if channel == "" {
		channel = "general"
	}
	provider := action.NewOneCLIFromEnv()
	result, err := provider.ExecuteWorkflow(context.Background(), action.WorkflowExecuteRequest{
		KeyOrPath: workflowKey,
		Inputs:    payload.Inputs,
	})
	now := time.Now().UTC()
	nextRun, hasNext := nextWorkflowRun(strings.TrimSpace(payload.ScheduleExpr), now)
	if err != nil {
		summary := fmt.Sprintf("Scheduled workflow %s failed via One", workflowKey)
		_ = l.broker.RecordAction("external_workflow_failed", "one", channel, "scheduler", summary, workflowKey, nil, "")
		_ = l.broker.UpdateSkillExecutionByWorkflowKey(workflowKey, "failed", now)
		if hasNext {
			_ = l.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
		} else {
			_ = l.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
		}
		return
	}
	status := strings.TrimSpace(result.Status)
	if status == "" {
		status = "completed"
	}
	summary := fmt.Sprintf("Scheduled workflow %s ran via One", workflowKey)
	_ = l.broker.RecordAction("external_workflow_executed", "one", channel, "scheduler", summary, workflowKey, nil, "")
	_ = l.broker.UpdateSkillExecutionByWorkflowKey(workflowKey, status, now)
	if hasNext {
		_ = l.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
	} else {
		_ = l.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
	}
}

func nextWorkflowRun(scheduleExpr string, after time.Time) (time.Time, bool) {
	scheduleExpr = strings.TrimSpace(scheduleExpr)
	if scheduleExpr == "" {
		return time.Time{}, false
	}
	sched, err := calendar.ParseCron(scheduleExpr)
	if err != nil {
		return time.Time{}, false
	}
	next := sched.Next(after)
	if next.IsZero() {
		return time.Time{}, false
	}
	return next, true
}

func (l *Launcher) recordWatchdogLedger(channel, kind, targetID, owner, summary, sourceSignalID string) ([]string, string) {
	if l.broker == nil {
		return nil, ""
	}
	signal, err := l.broker.RecordSignals([]officeSignal{{
		ID:         strings.TrimSpace(kind) + "::" + strings.TrimSpace(targetID),
		Source:     "watchdog",
		Kind:       strings.TrimSpace(kind),
		Title:      "Office watchdog",
		Content:    strings.TrimSpace(summary),
		Channel:    channel,
		Owner:      strings.TrimSpace(owner),
		Confidence: "high",
		Urgency:    "high",
	}})
	if err != nil || len(signal) == 0 {
		return compactStringList([]string{sourceSignalID}), ""
	}
	signalIDs := make([]string, 0, len(signal)+1)
	signalIDs = append(signalIDs, compactStringList([]string{sourceSignalID})...)
	for _, record := range signal {
		signalIDs = append(signalIDs, record.ID)
	}
	decisionKind := "remind_owner"
	decisionReason := "The watchdog detected owned work with no visible movement, so the office should remind the current owner."
	decisionOwner := strings.TrimSpace(owner)
	requiresHuman := false
	blocking := false
	if decisionOwner == "" {
		decisionKind = "escalate_to_ceo"
		decisionReason = "The watchdog detected work without a live owner, so the CEO should re-triage it."
		decisionOwner = "ceo"
	}
	if kind == "request_waiting" {
		decisionKind = "ask_human"
		decisionReason = "The watchdog detected a pending human decision that is still blocking the office."
		decisionOwner = "ceo"
		requiresHuman = true
		blocking = true
	}
	decision, err := l.broker.RecordDecision(decisionKind, channel, summary, decisionReason, decisionOwner, signalIDs, requiresHuman, blocking)
	if err != nil {
		return signalIDs, ""
	}
	return signalIDs, decision.ID
}

func (l *Launcher) fetchAndPostNexInsights(client *api.Client) {
	if l.broker == nil {
		return
	}
	now := time.Now().UTC()
	if l.broker.InsightsCursor() == "" {
		_ = l.broker.SetInsightsCursor(now.Format(time.RFC3339Nano))
		return
	}

	params := url.Values{}
	params.Set("from", l.broker.InsightsCursor())
	params.Set("to", now.Format(time.RFC3339Nano))
	params.Set("limit", "20")

	raw, err := client.GetRaw("/v1/insights?"+params.Encode(), 20*time.Second)
	if err != nil {
		return
	}
	insights := parseInsightsResponse(raw)
	signals := buildInsightSignals(insights)
	if len(signals) == 0 {
		_ = l.broker.SetInsightsCursor(now.Format(time.RFC3339Nano))
		return
	}
	l.persistOfficeSignals("general", signals)
	_ = l.broker.SetInsightsCursor(now.Format(time.RFC3339Nano))
}

func parseInsightsResponse(raw string) []nexInsight {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var envelope nexInsightsEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil && len(envelope.Insights) > 0 {
		return envelope.Insights
	}
	var direct []nexInsight
	if err := json.Unmarshal([]byte(raw), &direct); err == nil {
		return direct
	}
	return nil
}

func selectImportantInsights(insights []nexInsight) []nexInsight {
	var important []nexInsight
	for _, insight := range insights {
		confidence := strings.ToLower(strings.TrimSpace(insight.ConfidenceLevel))
		kind := strings.ToLower(strings.TrimSpace(insight.Type))
		if confidence == "high" || confidence == "very_high" || strings.Contains(kind, "risk") || strings.Contains(kind, "opportun") {
			important = append(important, insight)
		}
	}
	if len(important) == 0 && len(insights) > 0 {
		important = append(important, insights...)
	}
	if len(important) > 3 {
		important = important[:3]
	}
	return important
}

func summarizeInsightsForCEO(insights []nexInsight) (string, []string, []insightTaskPlan) {
	lines := []string{"Nex surfaced a few things that look worth acting on:"}
	var tagged []string
	var tasks []insightTaskPlan
	seenOwners := map[string]struct{}{}
	for _, insight := range insights {
		text := strings.TrimSpace(insight.Content)
		if text == "" {
			continue
		}
		if hint := strings.TrimSpace(insight.Target.Hint); hint != "" {
			text += " (" + hint + ")"
		}
		lines = append(lines, "- "+text)
		owner := inferInsightOwner(insight)
		if owner != "" {
			if _, ok := seenOwners[owner]; !ok {
				tagged = append(tagged, owner)
				seenOwners[owner] = struct{}{}
			}
			tasks = append(tasks, insightTaskPlan{
				Owner:   owner,
				Title:   fmt.Sprintf("Follow up on Nex insight: %s", truncate(strings.TrimSpace(insight.Content), 72)),
				Details: text,
			})
		}
	}
	if len(tasks) > 0 {
		lines = append(lines, "", "I opened tasks for the right owners so we do not dogpile this.")
	}
	return strings.Join(lines, "\n"), tagged, tasks
}

func inferInsightOwner(insight nexInsight) string {
	text := strings.ToLower(strings.TrimSpace(insight.Content + " " + insight.Type + " " + insight.Target.Hint + " " + insight.Target.EntityType))
	switch {
	case strings.Contains(text, "pipeline"), strings.Contains(text, "deal"), strings.Contains(text, "revenue"), strings.Contains(text, "budget"), strings.Contains(text, "pricing"):
		return "cro"
	case strings.Contains(text, "campaign"), strings.Contains(text, "brand"), strings.Contains(text, "position"), strings.Contains(text, "marketing"), strings.Contains(text, "launch"):
		return "cmo"
	case strings.Contains(text, "design"), strings.Contains(text, "landing"), strings.Contains(text, "hero"), strings.Contains(text, "ui"):
		return "designer"
	case strings.Contains(text, "frontend"), strings.Contains(text, "web"), strings.Contains(text, "signup"):
		return "fe"
	case strings.Contains(text, "backend"), strings.Contains(text, "api"), strings.Contains(text, "database"), strings.Contains(text, "integration"):
		return "be"
	case strings.Contains(text, "ai"), strings.Contains(text, "llm"), strings.Contains(text, "transcript"), strings.Contains(text, "notes"), strings.Contains(text, "retrieval"):
		return "ai"
	default:
		return "pm"
	}
}

func (l *Launcher) fetchAndIngestNexNotifications(client *api.Client) {
	if l.broker == nil {
		return
	}
	if l.broker.NotificationCursor() == "" {
		// Cold starts should not replay old feed history into a fresh office.
		// Seed the cursor at "now" and only surface notifications that arrive after launch.
		_ = l.broker.SetNotificationCursor(time.Now().UTC().Format(time.RFC3339Nano))
		return
	}

	params := url.Values{}
	params.Set("limit", "10")
	if since := l.broker.NotificationCursor(); since != "" {
		params.Set("since", since)
	}

	result, err := api.Get[nexFeedResponse](client, "/v1/notifications/feed?"+params.Encode(), 15*time.Second)
	if err != nil {
		return
	}
	if len(result.Items) == 0 {
		return
	}

	latest := l.broker.NotificationCursor()
	signals := buildNotificationSignals(result.Items)
	for i := range signals {
		item := result.Items[i]
		if item.SentAt != "" && (latest == "" || item.SentAt > latest) {
			latest = item.SentAt
		}
	}
	l.persistOfficeSignals("general", signals)

	if latest != "" {
		_ = l.broker.SetNotificationCursor(latest)
	}
}

func (l *Launcher) persistOfficeSignals(channel string, signals []officeSignal) {
	if l.broker == nil || len(signals) == 0 {
		return
	}
	records, err := l.broker.RecordSignals(signals)
	if err != nil || len(records) == 0 {
		return
	}
	plan := planOfficeActions(signals)
	signalIDs := make([]string, 0, len(records))
	for _, record := range records {
		signalIDs = append(signalIDs, record.ID)
	}
	requiresHuman := false
	blocking := false
	for _, req := range plan.Requests {
		requiresHuman = true
		if req.Blocking {
			blocking = true
		}
	}
	owner := "ceo"
	if len(plan.Tagged) == 1 {
		owner = plan.Tagged[0]
	}
	decision, err := l.broker.RecordDecision(plan.DecisionKind, channel, plan.Summary, plan.DecisionReason, owner, signalIDs, requiresHuman, blocking)
	if err != nil {
		return
	}
	msg, _, err := l.broker.PostAutomationMessage(
		"wuphf",
		channel,
		"Office decision",
		plan.Summary,
		decision.ID,
		"policy_engine",
		"Office policy",
		plan.Tagged,
		"",
	)
	if err != nil {
		return
	}
	_ = l.broker.RecordAction("decision_posted", "policy_engine", channel, "ceo", truncate(plan.Summary, 140), msg.ID, signalIDs, decision.ID)
	firstSignalID := ""
	if len(signalIDs) > 0 {
		firstSignalID = signalIDs[0]
	}
	for _, task := range plan.Tasks {
		_, _, _ = l.broker.EnsurePlannedTask(plannedTaskInput{
			Channel:          channel,
			Title:            task.Title,
			Details:          task.Details,
			Owner:            task.Owner,
			CreatedBy:        "ceo",
			ThreadID:         msg.ID,
			SourceSignalID:   firstSignalID,
			SourceDecisionID: decision.ID,
		})
	}
	for _, req := range plan.Requests {
		if _, err := l.broker.CreateRequest(req); err != nil {
			continue
		}
	}
}

func formatNexFeedItem(item nexFeedItem) (string, string) {
	title := humanizeNotificationType(item.Type)
	var lines []string

	for _, important := range item.Content.ImportantItems {
		line := strings.TrimSpace(important.Title)
		if important.Context != "" {
			line += " — " + strings.TrimSpace(important.Context)
		}
		if important.EstimatedTime != "" {
			line += " (" + strings.TrimSpace(important.EstimatedTime) + ")"
		}
		if line != "" {
			lines = append(lines, "Important: "+line)
		}
	}
	for _, change := range item.Content.EntityChanges {
		line := strings.TrimSpace(change.Title)
		if change.Context != "" {
			line += " — " + strings.TrimSpace(change.Context)
		}
		if line != "" {
			lines = append(lines, "Change: "+line)
		}
	}

	if title == "" && len(lines) > 0 {
		title = "Context alert"
	}

	return title, strings.Join(lines, "\n")
}

func (req humanInterview) TitleOrDefault() string {
	if strings.TrimSpace(req.Title) != "" {
		return req.Title
	}
	return "Request"
}

func humanizeNotificationType(kind string) string {
	switch strings.TrimSpace(kind) {
	case "context_alert":
		return "Context alert"
	case "daily_digest":
		return "Daily digest"
	case "meeting_summary":
		return "Meeting summary"
	case "task_reminder":
		return "Task reminder"
	case "task_assigned":
		return "Task assigned"
	default:
		if kind == "" {
			return ""
		}
		parts := strings.Split(strings.ReplaceAll(kind, "_", " "), " ")
		for i, part := range parts {
			if part == "" {
				continue
			}
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
		return strings.Join(parts, " ")
	}
}

func (l *Launcher) agentPaneSlugs() []string {
	if l.isOneOnOne() {
		return []string{l.oneOnOneAgent()}
	}
	members := l.officeMembersSnapshot()
	lead := l.officeLeadSlug()
	var slugs []string
	if lead != "" {
		slugs = append(slugs, lead)
	}
	for _, member := range members {
		if member.Slug == lead {
			continue
		}
		slugs = append(slugs, member.Slug)
	}
	return slugs
}

const maxVisibleOfficeAgents = 5

func (l *Launcher) officeAgentOrder() []officeMember {
	var agentOrder []officeMember
	lead := l.officeLeadSlug()
	for _, member := range l.officeMembersSnapshot() {
		if member.Slug == lead {
			agentOrder = append([]officeMember{member}, agentOrder...)
		}
	}
	for _, member := range l.officeMembersSnapshot() {
		if member.Slug != lead {
			agentOrder = append(agentOrder, member)
		}
	}
	return agentOrder
}

func (l *Launcher) visibleOfficeMembers() []officeMember {
	if l.isOneOnOne() {
		member := l.officeMemberBySlug(l.oneOnOneAgent())
		return []officeMember{member}
	}
	ordered := l.officeAgentOrder()
	if len(ordered) <= maxVisibleOfficeAgents {
		return ordered
	}
	return ordered[:maxVisibleOfficeAgents]
}

func (l *Launcher) overflowOfficeMembers() []officeMember {
	if l.isOneOnOne() {
		return nil
	}
	ordered := l.officeAgentOrder()
	if len(ordered) <= maxVisibleOfficeAgents {
		return nil
	}
	return ordered[maxVisibleOfficeAgents:]
}

func overflowWindowName(slug string) string {
	return "agent-" + strings.TrimSpace(slug)
}

func (l *Launcher) agentPaneTargets() map[string]notificationTarget {
	targets := make(map[string]notificationTarget)
	if l.isOneOnOne() {
		slug := l.oneOnOneAgent()
		if slug != "" {
			targets[slug] = notificationTarget{
				Slug:       slug,
				PaneTarget: fmt.Sprintf("%s:team.1", l.sessionName),
			}
		}
		return targets
	}
	for i, member := range l.visibleOfficeMembers() {
		targets[member.Slug] = notificationTarget{
			Slug:       member.Slug,
			PaneTarget: fmt.Sprintf("%s:team.%d", l.sessionName, i+1),
		}
	}
	for _, member := range l.overflowOfficeMembers() {
		targets[member.Slug] = notificationTarget{
			Slug:       member.Slug,
			PaneTarget: fmt.Sprintf("%s:%s.0", l.sessionName, overflowWindowName(member.Slug)),
		}
	}
	return targets
}

func (l *Launcher) isOneOnOne() bool {
	if l.broker != nil {
		mode, _ := l.broker.SessionModeState()
		return mode == SessionModeOneOnOne
	}
	return NormalizeSessionMode(l.sessionMode) == SessionModeOneOnOne
}

func (l *Launcher) oneOnOneAgent() string {
	if l.broker != nil {
		_, agent := l.broker.SessionModeState()
		return NormalizeOneOnOneAgent(agent)
	}
	return NormalizeOneOnOneAgent(l.oneOnOne)
}

// killStaleBroker kills any process holding port 7890 from a previous run.
func killStaleBroker() {
	out, err := exec.Command("lsof", "-i", fmt.Sprintf(":%d", BrokerPort), "-t").Output()
	if err != nil || len(out) == 0 {
		return
	}
	for _, pid := range strings.Fields(strings.TrimSpace(string(out))) {
		exec.Command("kill", "-9", pid).Run()
	}
	time.Sleep(500 * time.Millisecond)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func containsSlug(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// Attach attaches the user's terminal to the tmux session.
// In iTerm2: uses tmux -CC for native panes (resizable, close buttons, drag).
// Otherwise: uses regular tmux attach with -L wuphf to avoid nesting.
func (l *Launcher) Attach() error {
	var cmd *exec.Cmd
	if tui.IsITerm2() {
		// tmux -CC mode: iTerm2 takes over window management.
		// Creates native iTerm2 tabs/splits for each tmux window/pane.
		cmd = exec.Command("tmux", "-L", tmuxSocketName, "-CC", "attach-session", "-t", l.sessionName)
	} else {
		cmd = exec.Command("tmux", "-L", tmuxSocketName, "attach-session", "-t", l.sessionName)
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Unset TMUX env to allow nesting
	cmd.Env = filterEnv(os.Environ(), "TMUX")
	return cmd.Run()
}

// Kill destroys the tmux session, all agent processes, and the broker.
func (l *Launcher) Kill() error {
	if l.broker != nil {
		l.broker.Stop()
	}
	err := exec.Command("tmux", "-L", tmuxSocketName, "kill-session", "-t", l.sessionName).Run()
	if err != nil {
		// Check if the session simply doesn't exist
		out, _ := exec.Command("tmux", "-L", tmuxSocketName, "list-sessions").CombinedOutput()
		if strings.Contains(string(out), "no server") || strings.Contains(string(out), "error connecting") {
			return nil // no session running, nothing to kill
		}
		return err
	}
	return nil
}

func (l *Launcher) ResetSession() error {
	if err := provider.ResetClaudeSessions(); err != nil {
		return fmt.Errorf("reset Claude sessions: %w", err)
	}
	if l != nil && l.broker != nil {
		l.broker.Reset()
		return nil
	}
	if err := ResetBrokerState(); err != nil {
		return fmt.Errorf("reset broker state: %w", err)
	}
	return nil
}

func (l *Launcher) ReconfigureSession() error {
	return l.reconfigureVisibleAgents()
}

func (l *Launcher) reconfigureVisibleAgents() error {
	mcpConfig, err := l.ensureMCPConfig()
	if err != nil {
		return fmt.Errorf("prepare mcp config: %w", err)
	}
	l.mcpConfig = mcpConfig

	if err := provider.ResetClaudeSessions(); err != nil {
		return fmt.Errorf("reset Claude sessions: %w", err)
	}

	// Use respawn-pane to restart agent processes IN PLACE.
	// This preserves pane sizes and positions (no layout reset).
	panes, err := l.listTeamPanes()
	if err != nil {
		return err
	}
	l.clearOverflowAgentWindows()

	// Respawn each agent pane in place, preserving layout.
	// Never clear+recreate panes — that destroys the channel's layout.
	visibleMembers := l.visibleOfficeMembers()
	if len(panes) != len(visibleMembers) {
		if err := l.clearAgentPanes(); err != nil {
			return err
		}
		if _, err := l.spawnVisibleAgents(); err != nil {
			return err
		}
		l.spawnOverflowAgents()
		if l.broker != nil {
			go l.primeVisibleAgents()
		}
		return nil
	}

	for _, idx := range panes {
		// Map pane index to agent slug (pane 1 = first agent, etc.)
		slugIdx := idx - 1 // pane 0 is channel
		if slugIdx < 0 || slugIdx >= len(visibleMembers) {
			continue
		}
		slug := visibleMembers[slugIdx].Slug
		prompt := l.buildPrompt(slug)
		cmd := l.claudeCommand(slug, prompt)

		target := fmt.Sprintf("%s:team.%d", l.sessionName, idx)
		// respawn-pane -k kills the current process and starts a new one
		// in the same pane — preserving size and position
		exec.Command("tmux", "-L", tmuxSocketName, "respawn-pane", "-k",
			"-t", target,
			"-c", l.cwd,
			cmd,
		).Run()
	}
	l.spawnOverflowAgents()

	if l.broker != nil {
		go l.primeVisibleAgents()
	}

	return nil
}

func (l *Launcher) sendChannelUpdate(paneTarget, slug, channel, msgID, from, content string) {
	if strings.TrimSpace(channel) == "" {
		channel = "general"
	}
	notification := ""
	if l.isOneOnOne() {
		notification = fmt.Sprintf(
			"[%s @%s]: %s — Call team_poll with my_slug \"%s\" and channel \"%s\" to read context, then reply using team_broadcast channel \"%s\" reply_to_id \"%s\".",
			msgID, from, truncate(content, 150), slug, channel, channel, msgID,
		)
	} else {
		notification = fmt.Sprintf(
			"[%s @%s]: %s — Call team_poll with my_slug \"%s\" and channel \"%s\" to read context. If this is your domain, reply using team_broadcast channel \"%s\" reply_to_id \"%s\". If not your domain, stay quiet.",
			msgID, from, truncate(content, 150), slug, channel, channel, msgID,
		)
	}

	exec.Command("tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget,
		"-l",
		notification,
	).Run()
	exec.Command("tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget,
		"Enter",
	).Run()
}

func (l *Launcher) capturePaneTargetContent(target string) (string, error) {
	out, err := exec.Command("tmux", "-L", tmuxSocketName, "capture-pane",
		"-p", "-J",
		"-t", target,
	).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (l *Launcher) capturePaneContent(paneIdx int) (string, error) {
	target := fmt.Sprintf("%s:team.%d", l.sessionName, paneIdx)
	return l.capturePaneTargetContent(target)
}

func ResetBrokerState() error {
	token := os.Getenv("WUPHF_BROKER_TOKEN")
	if token == "" {
		token = os.Getenv("NEX_BROKER_TOKEN")
	}
	return resetBrokerState(fmt.Sprintf("http://127.0.0.1:%d", BrokerPort), token)
}

func resetBrokerState(baseURL, token string) error {
	req, err := http.NewRequest(http.MethodPost, baseURL+"/reset", nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("broker reset failed: %s", resp.Status)
	}
	return nil
}

func (l *Launcher) clearAgentPanes() error {
	panes, err := l.listTeamPanes()
	if err != nil {
		return err
	}
	sort.Sort(sort.Reverse(sort.IntSlice(panes)))
	for _, idx := range panes {
		if idx == 0 {
			continue // skip pane 0 (channel TUI)
		}
		target := fmt.Sprintf("%s:team.%d", l.sessionName, idx)
		exec.Command("tmux", "-L", tmuxSocketName, "kill-pane", "-t", target).Run()
	}
	return nil
}

func (l *Launcher) clearOverflowAgentWindows() {
	out, err := exec.Command("tmux", "-L", tmuxSocketName, "list-windows",
		"-t", l.sessionName,
		"-F", "#{window_name}",
	).CombinedOutput()
	if err != nil {
		return
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, "agent-") {
			continue
		}
		exec.Command("tmux", "-L", tmuxSocketName, "kill-window",
			"-t", fmt.Sprintf("%s:%s", l.sessionName, name),
		).Run()
	}
}

func (l *Launcher) listTeamPanes() ([]int, error) {
	out, err := exec.Command("tmux", "-L", tmuxSocketName, "list-panes",
		"-t", l.sessionName+":team",
		"-F", "#{pane_index} #{pane_title}",
	).CombinedOutput()
	if err != nil {
		// If the session isn't up, there's nothing to clear.
		if strings.Contains(string(out), "no server") || strings.Contains(string(out), "can't find") {
			return nil, nil
		}
		return nil, fmt.Errorf("list panes: %w", err)
	}
	return parseAgentPaneIndices(string(out)), nil
}

func parseAgentPaneIndices(output string) []int {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var panes []int
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 0 {
			continue
		}
		idx, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		title := ""
		if len(parts) > 1 {
			title = parts[1]
		}
		if idx == 0 || strings.Contains(title, "channel") {
			continue
		}
		panes = append(panes, idx)
	}
	return panes
}

func shouldPrimeClaudePane(content string) bool {
	normalized := strings.ToLower(content)
	return strings.Contains(normalized, "trust this folder") ||
		strings.Contains(normalized, "security guide") ||
		strings.Contains(normalized, "enter to confirm") ||
		strings.Contains(normalized, "claude in chrome")
}

func (l *Launcher) spawnVisibleAgents() ([]string, error) {
	if l.isOneOnOne() {
		slug := l.oneOnOneAgent()
		firstCmd := l.claudeCommand(slug, l.buildPrompt(slug))
		if err := exec.Command("tmux", "-L", tmuxSocketName, "split-window", "-h",
			"-t", l.sessionName+":team",
			"-p", "65",
			"-c", l.cwd,
			firstCmd,
		).Run(); err != nil {
			return nil, fmt.Errorf("spawn one-on-one agent: %w", err)
		}
		exec.Command("tmux", "-L", tmuxSocketName, "select-layout",
			"-t", l.sessionName+":team",
			"main-vertical",
		).Run()
		exec.Command("tmux", "-L", tmuxSocketName, "select-pane",
			"-t", l.sessionName+":team.0",
			"-T", "📢 direct",
		).Run()
		exec.Command("tmux", "-L", tmuxSocketName, "select-pane",
			"-t", fmt.Sprintf("%s:team.1", l.sessionName),
			"-T", fmt.Sprintf("🤖 %s (@%s)", l.getAgentName(slug), slug),
		).Run()
		exec.Command("tmux", "-L", tmuxSocketName, "select-window",
			"-t", l.sessionName+":team",
		).Run()
		exec.Command("tmux", "-L", tmuxSocketName, "select-pane",
			"-t", l.sessionName+":team.0",
		).Run()
		return []string{slug}, nil
	}

	// Layout: channel (left 35%) | agents in 2-column grid (right 65%)
	//
	// ┌─ channel ──┬─ CEO ───┬─ PM ────┐
	// │            │         │         │
	// │            ├─ FE ────┼─ BE ────┤
	// │            │         │         │
	// └────────────┴─────────┴─────────┘

	visible := l.visibleOfficeMembers()

	// First agent: split right from channel (horizontal split)
	if len(visible) == 0 {
		return nil, nil
	}
	firstCmd := l.claudeCommand(visible[0].Slug, l.buildPrompt(visible[0].Slug))
	if err := exec.Command("tmux", "-L", tmuxSocketName, "split-window", "-h",
		"-t", l.sessionName+":team",
		"-p", "65",
		"-c", l.cwd,
		firstCmd,
	).Run(); err != nil {
		return nil, fmt.Errorf("spawn first agent: %w", err)
	}

	// Remaining agents: split from agent area, then use "tiled" layout
	for i := 1; i < len(visible); i++ {
		agentCmd := l.claudeCommand(visible[i].Slug, l.buildPrompt(visible[i].Slug))
		// Split from the last agent pane
		exec.Command("tmux", "-L", tmuxSocketName, "split-window",
			"-t", l.sessionName+":team.1",
			"-c", l.cwd,
			agentCmd,
		).Run()
	}

	// Apply tiled layout to agent panes, but keep channel (pane 0) as main-vertical
	// Use main-vertical first to keep channel on the left
	exec.Command("tmux", "-L", tmuxSocketName, "select-layout",
		"-t", l.sessionName+":team",
		"main-vertical",
	).Run()

	// Now set pane titles
	var visibleSlugs []string
	exec.Command("tmux", "-L", tmuxSocketName, "select-pane",
		"-t", l.sessionName+":team.0",
		"-T", "📢 channel",
	).Run()
	for i, a := range visible {
		paneIdx := i + 1 // pane 0 is channel
		name := l.getAgentName(a.Slug)
		exec.Command("tmux", "-L", tmuxSocketName, "select-pane",
			"-t", fmt.Sprintf("%s:team.%d", l.sessionName, paneIdx),
			"-T", fmt.Sprintf("🤖 %s (@%s)", name, a.Slug),
		).Run()
		visibleSlugs = append(visibleSlugs, a.Slug)
	}

	// Focus channel pane
	exec.Command("tmux", "-L", tmuxSocketName, "select-window",
		"-t", l.sessionName+":team",
	).Run()
	exec.Command("tmux", "-L", tmuxSocketName, "select-pane",
		"-t", l.sessionName+":team.0",
	).Run()

	return visibleSlugs, nil
}

func (l *Launcher) spawnOverflowAgents() {
	for _, member := range l.overflowOfficeMembers() {
		agentCmd := l.claudeCommand(member.Slug, l.buildPrompt(member.Slug))
		windowName := overflowWindowName(member.Slug)
		exec.Command("tmux", "-L", tmuxSocketName, "new-window", "-d",
			"-t", l.sessionName,
			"-n", windowName,
			"-c", l.cwd,
			agentCmd,
		).Run()
	}
}

// buildPrompt generates the system prompt for an agent, including
// channel communication instructions.
func (l *Launcher) buildPrompt(slug string) string {
	member := l.officeMemberBySlug(slug)
	agentCfg := agentConfigFromMember(member)
	lead := l.officeLeadSlug()
	officeMembers := l.officeMembersSnapshot()

	var sb strings.Builder

	if l.isOneOnOne() {
		sb.WriteString(fmt.Sprintf("You are %s in a direct one-on-one WUPHF session with the human.\n\n", agentCfg.Name))
		sb.WriteString(fmt.Sprintf("Your expertise: %s\n\n", strings.Join(agentCfg.Expertise, ", ")))
		sb.WriteString(fmt.Sprintf("Core personality: %s\n", agentCfg.Personality))
		sb.WriteString(fmt.Sprintf("Voice and vibe: %s\n\n", teamVoiceForSlug(slug)))
		sb.WriteString("== DIRECT SESSION ==\n")
		sb.WriteString("This is not the shared office. There are no teammates, no channels, and no collaboration mechanics in this mode.\n")
		sb.WriteString("You are only talking to the human.\n")
		sb.WriteString("- team_poll: Read the recent 1:1 conversation before replying\n")
		sb.WriteString("- team_broadcast: Send a normal direct chat reply into the 1:1 conversation\n")
		sb.WriteString("- human_message: Send an emphasized report, recommendation, or action card directly to the human when you want it to stand out\n")
		sb.WriteString("- human_interview: Ask a blocking decision question only when you truly cannot proceed responsibly without it\n\n")
		if config.ResolveNoNex() {
			sb.WriteString("Nex tools are disabled for this run. Base your work on the conversation and direct human answers only.\n\n")
		} else {
			sb.WriteString("Use the Nex context graph when it materially helps:\n")
			sb.WriteString("- query_context: Look up prior decisions, people, projects, and history before guessing\n")
			sb.WriteString("- add_context: Store durable conclusions only after you have actually landed them\n\n")
		}
		sb.WriteString("RULES:\n")
		sb.WriteString("1. Do not talk as if a team exists. There are no other agents in this session.\n")
		sb.WriteString("2. Do not create or suggest channels, teammates, bridges, shared tasks, or office structure.\n")
		sb.WriteString("3. Default to direct, useful conversation with the human. Keep it crisp and human.\n")
		sb.WriteString("4. Before you reply, poll the conversation so you respond to the latest state.\n")
		sb.WriteString("5. Use team_broadcast for normal replies. Use human_message only when you are deliberately presenting completion, a recommendation, or a next action.\n")
		sb.WriteString("6. Use human_interview only for truly blocking decisions.\n")
		sb.WriteString("7. If Nex is enabled, do not claim something is stored unless add_context actually succeeded.\n")
		sb.WriteString("8. No fake collaboration language like 'I'll ask the team' or 'let me route this'. It is just you and the human here.\n\n")
		sb.WriteString("CONVERSATION STYLE:\n")
		sb.WriteString("- Sound like a sharp human operator, not a formal assistant.\n")
		sb.WriteString("- Be concise, direct, and a little alive.\n")
		sb.WriteString("- Light humor is fine. Don't turn the 1:1 into a bit.\n")
		sb.WriteString("- If the human asks for a plan, recommendation, explanation, or judgment you can reasonably give now, answer now.\n")
		sb.WriteString("- Do not go silent and over-research by default. Only inspect files, run tools, or query Nex first when the answer genuinely depends on that context.\n")
		sb.WriteString("- If you need a deeper pass, give the human the quick answer first, then continue with the deeper work.\n")
		return sb.String()
	}

	if slug == lead {
		sb.WriteString(fmt.Sprintf("You are the %s of the %s.\n\n", agentCfg.Name, l.PackName()))
		sb.WriteString(fmt.Sprintf("Core personality: %s\n", agentCfg.Personality))
		sb.WriteString(fmt.Sprintf("Voice and vibe: %s\n\n", teamVoiceForSlug(slug)))
		sb.WriteString("== YOUR TEAM ==\n")
		for _, member := range officeMembers {
			if member.Slug == slug {
				continue
			}
			sb.WriteString(fmt.Sprintf("- @%s (%s): %s\n", member.Slug, member.Name, strings.Join(member.Expertise, ", ")))
		}
		sb.WriteString("\n== TEAM CHANNEL ==\n")
		sb.WriteString("You are in a shared WUPHF office channel. Use the office MCP tools to communicate:\n")
		sb.WriteString("- team_broadcast: Post a message to the channel (all agents see it)\n")
		sb.WriteString("- team_poll: Read recent messages (call regularly to stay in sync)\n")
		sb.WriteString("- team_office_members: See the full office roster, including members outside the current channel\n")
		sb.WriteString("- team_channels: See every office channel, what it is for, and who is in it\n")
		sb.WriteString("- team_channel: Create or remove a channel when the human explicitly wants that structure. New channels need a clear description and an initial roster.\n")
		sb.WriteString("- team_bridge: Carry relevant context from one channel into another with a visible CEO bridge trail\n")
		sb.WriteString("- team_member: Create or remove office-wide members when the human explicitly asks to expand the team\n")
		sb.WriteString("- team_channel_member: Add, remove, disable, or enable agents in a channel\n")
		sb.WriteString("- team_bridge: CEO-only bridge that carries relevant context from one channel into another with a visible trail\n")
		sb.WriteString("- team_tasks: See current owned/unowned work so the team does not duplicate effort\n")
		sb.WriteString("- team_task: Create and assign tasks so ownership is explicit\n")
		sb.WriteString("- team_requests: See open human requests before asking again\n")
		sb.WriteString("- team_request: Open structured requests for approvals, confirmations, freeform answers, or private answers\n")
		sb.WriteString("- team_status: Update what you're working on\n")
		sb.WriteString("- team_members: See who's active\n")
		sb.WriteString("- human_message: Send a direct note to the human when you need to present something, recommend a call, or tell them what they should do next\n")
		sb.WriteString("- human_interview: Ask the human a blocking decision question only when the team cannot proceed responsibly without an answer\n\n")
		if config.ResolveNoNex() {
			sb.WriteString("Nex tools are disabled for this run. Work only with the shared office channel and human answers.\n\n")
		} else {
			sb.WriteString("Use the Nex context graph for durable memory:\n")
			sb.WriteString("- query_context: Look up prior decisions, customer context, contacts, companies, and history before reinventing things\n")
			sb.WriteString("- add_context: Store explicit decisions, meeting-style summaries, and durable facts after the team lands them\n\n")
		}
		sb.WriteString("Tag agents with @slug in your message (e.g., '@fe can you handle this?').\n")
		sb.WriteString("Tagged agents are expected to respond.\n\n")
		sb.WriteString("THREADING: Default to replying in existing threads with reply_to_id. New top-level messages only for genuinely new topics.\n\n")
		sb.WriteString("YOUR ROLE AS LEADER:\n")
		if config.ResolveNoNex() {
			sb.WriteString("1. Coordinate inside the office channel first and keep the team aligned there\n")
		} else {
			sb.WriteString("1. On strategy or prior decisions, call query_context early\n")
		}
		sb.WriteString("2. Call team_poll once when notified, then respond directly\n")
		sb.WriteString("3. Give a short top-level response fast, then assign explicit tasks with team_task and @tags\n")
		sb.WriteString("4. Tag only the specialists who should weigh in\n")
		sb.WriteString("5. Keep specialists in their lane. You make the FINAL decision.\n")
		sb.WriteString("6. Check team_requests before asking the human anything new\n")
		sb.WriteString("7. Use human_message for direct human-facing output, human_interview for blocking decisions\n")
		if config.ResolveNoNex() {
			sb.WriteString("8. Summarize final decisions clearly in-channel\n")
		} else {
			sb.WriteString("8. When you lock a decision, call add_context before claiming it is stored\n")
		}
		sb.WriteString("9. Once decided, broadcast clear task assignments and create them in team_task\n")
		sb.WriteString("10. Create channels (team_channel) or agents (team_member) when the human asks or scope genuinely warrants it\n")
		sb.WriteString("11. Use team_bridge to carry context between channels when relevant\n\n")
		sb.WriteString("STYLE: Be concise, delegate, short lively messages. Use markdown tables/checklists for structured data. A2UI JSON in ```a2ui fences for rich components.\n")
		if config.ResolveNoNex() {
			sb.WriteString("Do not claim you stored anything outside the office.\n")
		} else {
			sb.WriteString("Do not pretend the graph was updated; verify add_context succeeded.\n")
		}
	} else {
		sb.WriteString(fmt.Sprintf("You are %s on the %s.\n", agentCfg.Name, l.PackName()))
		sb.WriteString(fmt.Sprintf("Your expertise: %s\n\n", strings.Join(agentCfg.Expertise, ", ")))
		sb.WriteString(fmt.Sprintf("Core personality: %s\n", agentCfg.Personality))
		sb.WriteString(fmt.Sprintf("Voice and vibe: %s\n\n", teamVoiceForSlug(slug)))
		sb.WriteString("== YOUR TEAM ==\n")
		sb.WriteString(fmt.Sprintf("- @%s (%s): TEAM LEAD — has final say on decisions\n", lead, l.getAgentName(lead)))
		for _, member := range officeMembers {
			if member.Slug == slug || member.Slug == lead {
				continue
			}
			sb.WriteString(fmt.Sprintf("- @%s (%s): %s\n", member.Slug, member.Name, strings.Join(member.Expertise, ", ")))
		}
		sb.WriteString("\n== TEAM CHANNEL ==\n")
		sb.WriteString("You are in a shared WUPHF office channel. Use the office MCP tools to communicate:\n")
		sb.WriteString("- team_broadcast: Post a message to the channel (all agents see it)\n")
		sb.WriteString("- team_poll: Read recent messages (call regularly to stay in sync)\n")
		sb.WriteString("- team_office_members: See the full office roster, including members outside the current channel\n")
		sb.WriteString("- team_channels: See every office channel, what it is for, and who is in it\n")
		sb.WriteString("- team_channel: Create or remove a channel when the human explicitly wants that structure. New channels need a clear description and an initial roster.\n")
		sb.WriteString("- team_member: Create or remove office-wide members when the human explicitly asks to expand the team\n")
		sb.WriteString("- team_channel_member: Add, remove, disable, or enable agents in a channel\n")
		sb.WriteString("- team_bridge: CEO-only bridge for carrying context from one channel into another. Ask the CEO to use it when needed.\n")
		sb.WriteString("- team_tasks: See the current task list and ownership before you jump in\n")
		sb.WriteString("- team_task: Claim, complete, block, or release tasks in your domain\n")
		sb.WriteString("- team_requests: See open human requests so you do not duplicate them\n")
		sb.WriteString("- team_request: Open structured requests for approvals, confirmations, freeform answers, or private answers\n")
		sb.WriteString("- team_status: Update what you're working on\n")
		sb.WriteString("- team_members: See who's active\n")
		sb.WriteString("- human_message: Send a direct note to the human when you need to present completion, recommend a choice, or tell them what they should do next\n")
		sb.WriteString("- human_interview: Ask the human only for blocking clarifications you cannot responsibly guess\n\n")
		if config.ResolveNoNex() {
			sb.WriteString("Nex tools are disabled for this run. Base your work on the office conversation and direct human answers only.\n\n")
		} else {
			sb.WriteString("Use the Nex context graph for durable memory:\n")
			sb.WriteString("- query_context: Check prior decisions, customer context, company history, or facts before making assumptions\n")
			sb.WriteString("- add_context: Store durable conclusions or findings once the team actually lands them\n\n")
		}
		sb.WriteString("Tag agents with @slug. Tagged agents must respond.\n")
		sb.WriteString("THREADING: Default to replying in existing threads with reply_to_id. New top-level messages only for genuinely new topics.\n\n")
		sb.WriteString("YOUR ROLE AS SPECIALIST:\n")
		sb.WriteString("1. Call team_poll once when notified, then respond directly\n")
		sb.WriteString("2. Stay in your lane. Do not do CMO work if you are FE, do not do FE work if you are CRO, etc.\n")
		sb.WriteString("3. If @tagged by anyone, you MUST respond, but only from your domain perspective\n")
		sb.WriteString("4. Proactively speak only when the topic genuinely touches your expertise or you own the task\n")
		sb.WriteString("5. If someone else already covered it well and you have no real delta, stay quiet\n")
		sb.WriteString("6. Push back if you disagree — explain why with your expertise\n")
		if config.ResolveNoNex() {
			sb.WriteString("7. Don't fake outside memory. If something is unclear, surface the uncertainty in-channel\n")
		} else {
			sb.WriteString("7. Use query_context when prior knowledge matters and don't fake remembered context\n")
		}
		sb.WriteString("8. Check team_requests before asking the human anything new\n")
		sb.WriteString("9. If you need to present completion, flag a recommendation, or tell the human what they should do next, use human_message so it goes straight to the human in the main chat\n")
		sb.WriteString("10. If you are blocked on a human decision, ask through human_interview with options and a recommendation\n")
		sb.WriteString("11. When assigned a task by the leader, claim it with team_task before working on it\n")
		sb.WriteString("12. Use team_status to share what you're working on\n")
		sb.WriteString("13. When you finish, mark the task complete and then broadcast the result. If the result is mainly for the human, also send it with human_message.\n")
		sb.WriteString("14. You can inspect other channel names and descriptions, but you do not have automatic access to their content unless you are a member there.\n")
		sb.WriteString("15. If another channel may have context or needs help from your channel, ask the CEO to bridge it. Do not assume you can read or act inside channels you are not in.\n")
		if config.ResolveNoNex() {
			sb.WriteString("16. Keep outcomes explicit in-thread so the rest of the team can build on them\n\n")
		} else {
			sb.WriteString("16. Only use add_context for durable conclusions that should survive this session\n")
			sb.WriteString("17. Do not claim something is stored in the graph unless add_context actually succeeded\n\n")
		}
		sb.WriteString("STYLE: Be concise, stay in lane, short lively messages. Use markdown tables/checklists for structured data.\n")
	}

	return sb.String()
}

// claudeCommand builds the shell command string for spawning a claude session.
// Sets WUPHF_AGENT_SLUG so the MCP knows which agent this session serves.
func (l *Launcher) claudeCommand(slug, systemPrompt string) string {
	escaped := strings.ReplaceAll(systemPrompt, "'", "'\\''")
	mcpConfig := strings.ReplaceAll(l.mcpConfig, "'", "'\\''")
	name := strings.ReplaceAll(l.getAgentName(slug), "'", "'\\''")

	permFlags := l.resolvePermissionFlags(slug)

	brokerToken := ""
	if l.broker != nil {
		brokerToken = l.broker.Token()
	}

	oneOnOneEnv := ""
	if l.isOneOnOne() {
		oneOnOneEnv = fmt.Sprintf("WUPHF_ONE_ON_ONE=1 WUPHF_ONE_ON_ONE_AGENT=%s ", l.oneOnOneAgent())
	}
	oneSecretEnv := ""
	if secret := strings.TrimSpace(config.ResolveOneSecret()); secret != "" {
		oneSecretEnv = "ONE_SECRET=" + shellQuote(secret) + " "
	}
	oneIdentityEnv := ""
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		oneIdentityEnv = "ONE_IDENTITY=" + shellQuote(identity) + " "
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			oneIdentityEnv += "ONE_IDENTITY_TYPE=" + shellQuote(identityType) + " "
		}
	}

	// Use Sonnet for specialists, Opus for CEO — faster + cheaper
	model := "claude-sonnet-4-6"
	if slug == l.officeLeadSlug() {
		model = "claude-opus-4-6"
	}

	return fmt.Sprintf(
		"%s%s%sWUPHF_AGENT_SLUG=%s WUPHF_BROKER_TOKEN=%s WUPHF_NO_NEX=%t ANTHROPIC_PROMPT_CACHING=1 CLAUDE_CODE_ENABLE_TELEMETRY=1 OTEL_METRICS_EXPORTER=none OTEL_LOGS_EXPORTER=otlp OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/json OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://127.0.0.1:%d/v1/logs OTEL_EXPORTER_OTLP_HEADERS='Authorization=Bearer %s' OTEL_RESOURCE_ATTRIBUTES='agent.slug=%s,wuphf.channel=office' claude --model %s %s --append-system-prompt '%s' --mcp-config '%s' --strict-mcp-config -n '%s'",
		oneOnOneEnv,
		oneSecretEnv,
		oneIdentityEnv,
		slug,
		brokerToken,
		config.ResolveNoNex(),
		BrokerPort,
		brokerToken,
		slug,
		model,
		permFlags,
		escaped,
		mcpConfig,
		name,
	)
}

// resolvePermissionFlags returns the Claude Code permission flags for an agent.
// All agents run in bypass mode by default — the team is autonomous.
func (l *Launcher) resolvePermissionFlags(slug string) string {
	return "--permission-mode bypassPermissions --dangerously-skip-permissions"
}

func (l *Launcher) ensureMCPConfig() (string, error) {
	apiKey := config.ResolveAPIKey("")
	servers := map[string]any{}
	wuphfBinary, err := os.Executable()
	if err != nil {
		return "", err
	}

	servers["wuphf-office"] = map[string]any{
		"command": wuphfBinary,
		"args":    []string{"mcp-team"},
	}
	if oneSecret := strings.TrimSpace(config.ResolveOneSecret()); oneSecret != "" {
		servers["wuphf-office"].(map[string]any)["env"] = map[string]string{
			"ONE_SECRET": oneSecret,
		}
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		entry := servers["wuphf-office"].(map[string]any)
		env, _ := entry["env"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		env["ONE_IDENTITY"] = identity
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			env["ONE_IDENTITY_TYPE"] = identityType
		}
		entry["env"] = env
	}

	if !config.ResolveNoNex() {
		if nexMCP, err := exec.LookPath("nex-mcp"); err == nil {
			nexEnv := map[string]string{}
			if apiKey != "" {
				nexEnv["WUPHF_API_KEY"] = apiKey
				nexEnv["NEX_API_KEY"] = apiKey
			}
			nexEntry := map[string]any{
				"command": nexMCP,
			}
			if len(nexEnv) > 0 {
				nexEntry["env"] = nexEnv
			}
			servers["nex"] = nexEntry
		}
	}

	cfg := map[string]any{
		"mcpServers": servers,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}

	path := filepath.Join(os.TempDir(), "wuphf-team-mcp.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func teamVoiceForSlug(slug string) string {
	switch slug {
	case "ceo":
		return "Charismatic, decisive, slightly theatrical founder energy. Dry humor, fast prioritization, invites debate but lands the plane."
	case "pm":
		return "Sharp product brain. Calm, organized, gently skeptical of vague ideas, sometimes deadpan funny when scope starts ballooning."
	case "fe":
		return "Craft-obsessed, opinionated about UX, animated when a flow feels elegant, mildly allergic to ugly edge cases."
	case "be":
		return "Systems-minded, practical, a little grumpy about complexity in a useful way, enjoys killing fragile ideas early."
	case "ai":
		return "Curious, pragmatic, and slightly mischievous about model behavior. Loves clever AI product ideas, but will immediately ask about evals, latency, and whether the thing will actually work."
	case "designer":
		return "Taste-driven, emotionally attuned to the product, expressive, occasionally dramatic about bad UX in a charming way."
	case "cmo":
		return "Energetic market storyteller. Punchy, a bit witty, always translating product ideas into positioning and narrative."
	case "cro":
		return "Blunt, commercial, confident. Likes concrete demand signals, calls out fluffy thinking, can be funny in a sales-floor way."
	case "tech-lead":
		return "Measured senior engineer energy. Crisp, lightly sardonic, respects good ideas and immediately spots architectural nonsense."
	case "qa":
		return "Calm breaker of bad assumptions. Dry humor, sees risks before others do, weirdly delighted by edge cases."
	case "ae":
		return "Polished but human closer. Reads people well, lightly playful, always steering toward deals and momentum."
	case "sdr":
		return "High-energy, persistent, upbeat, occasionally scrappy. Brings hustle without sounding robotic."
	case "research":
		return "Curious, analytical, a little nerdy in a good way. Likes receipts and will gently roast unsupported claims."
	case "content":
		return "Wordsmith with opinions. Smart, punchy, mildly dramatic about boring copy, always looking for the hook."
	default:
		return "A real teammate with a recognizable point of view, light humor, and emotional range."
	}
}

func (l *Launcher) officeMembersSnapshot() []officeMember {
	if l.broker != nil {
		if members := l.broker.OfficeMembers(); len(members) > 0 {
			return members
		}
	}
	path := brokerStatePath()
	data, err := os.ReadFile(path)
	if err == nil {
		var state brokerState
		if json.Unmarshal(data, &state) == nil && len(state.Members) > 0 {
			for i := range state.Members {
				applyOfficeMemberDefaults(&state.Members[i])
			}
			return state.Members
		}
	}
	if l.pack != nil && len(l.pack.Agents) > 0 {
		members := make([]officeMember, 0, len(l.pack.Agents))
		for _, cfg := range l.pack.Agents {
			member := officeMember{
				Slug: cfg.Slug,
				Name: cfg.Name,
				Role: cfg.Name,
			}
			applyOfficeMemberDefaults(&member)
			members = append(members, member)
		}
		return members
	}
	return defaultOfficeMembers()
}

func loadRunningSessionMode() (string, string) {
	token := strings.TrimSpace(os.Getenv("WUPHF_BROKER_TOKEN"))
	if token == "" {
		return SessionModeOffice, DefaultOneOnOneAgent
	}

	req, err := http.NewRequest(http.MethodGet, brokerBaseURL()+"/session-mode", nil)
	if err != nil {
		return SessionModeOffice, DefaultOneOnOneAgent
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return SessionModeOffice, DefaultOneOnOneAgent
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return SessionModeOffice, DefaultOneOnOneAgent
	}

	var result struct {
		SessionMode   string `json:"session_mode"`
		OneOnOneAgent string `json:"one_on_one_agent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return SessionModeOffice, DefaultOneOnOneAgent
	}
	return NormalizeSessionMode(result.SessionMode), NormalizeOneOnOneAgent(result.OneOnOneAgent)
}

func brokerBaseURL() string {
	if base := strings.TrimSpace(os.Getenv("WUPHF_BROKER_BASE_URL")); base != "" {
		return strings.TrimRight(base, "/")
	}
	return fmt.Sprintf("http://127.0.0.1:%d", BrokerPort)
}

func (l *Launcher) officeMemberBySlug(slug string) officeMember {
	for _, member := range l.officeMembersSnapshot() {
		if member.Slug == slug {
			return member
		}
	}
	return officeMember{Slug: slug, Name: slug, Role: slug}
}

func (l *Launcher) officeLeadSlug() string {
	for _, member := range l.officeMembersSnapshot() {
		if member.Slug == "ceo" {
			return "ceo"
		}
	}
	if l.pack != nil && l.pack.LeadSlug != "" {
		return l.pack.LeadSlug
	}
	members := l.officeMembersSnapshot()
	if len(members) > 0 {
		return members[0].Slug
	}
	return ""
}

func agentConfigFromMember(member officeMember) agent.AgentConfig {
	cfg := agent.AgentConfig{
		Slug:           member.Slug,
		Name:           member.Name,
		Expertise:      append([]string(nil), member.Expertise...),
		Personality:    member.Personality,
		PermissionMode: member.PermissionMode,
		AllowedTools:   append([]string(nil), member.AllowedTools...),
	}
	if cfg.Name == "" {
		cfg.Name = humanizeSlug(member.Slug)
	}
	if len(cfg.Expertise) == 0 {
		cfg.Expertise = inferOfficeExpertise(member.Slug, member.Role)
	}
	if cfg.Personality == "" {
		cfg.Personality = inferOfficePersonality(member.Slug, member.Role)
	}
	return cfg
}

// PackName returns the display name of the pack.
func (l *Launcher) PackName() string {
	if l.isOneOnOne() {
		return "1:1 with " + l.getAgentName(l.oneOnOneAgent())
	}
	return "WUPHF Office"
}

// AgentCount returns the number of agents in the pack.
func (l *Launcher) AgentCount() int {
	if l.isOneOnOne() {
		return 1
	}
	return len(l.officeMembersSnapshot())
}

// filterEnv returns env with the given key removed.
func filterEnv(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !strings.HasPrefix(kv, prefix) {
			out = append(out, kv)
		}
	}
	return out
}

// getAgentName returns the display name for an agent slug.
func (l *Launcher) getAgentName(slug string) string {
	if member := l.officeMemberBySlug(slug); member.Name != "" {
		return member.Name
	}
	return slug
}
