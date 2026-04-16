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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/brokeraddr"
	"github.com/nex-crm/wuphf/internal/calendar"
	"github.com/nex-crm/wuphf/internal/company"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/nex"
	"github.com/nex-crm/wuphf/internal/operations"
	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/setup"
)

const (
	SessionName                     = "wuphf-team"
	tmuxSocketName                  = "wuphf"
	defaultNotificationPollInterval = 15 * time.Minute
	channelRespawnDelay             = 8 * time.Second
	ceoHeadStartDelay               = 250 * time.Millisecond
	blankSlateLaunchSlug            = "__blank_slate__"
)

// Launcher sets up and manages the multi-agent team.
type Launcher struct {
	packSlug         string
	pack             *agent.PackDefinition
	blankSlateLaunch bool
	sessionName      string
	cwd              string
	broker           *Broker
	mcpConfig        string
	unsafe           bool
	opusCEO          bool
	focusMode        bool
	sessionMode      string
	oneOnOne         string
	provider         string

	headlessMu           sync.Mutex
	headlessCtx          context.Context
	headlessCancel       context.CancelFunc
	headlessWorkers      map[string]bool
	headlessActive       map[string]*headlessCodexActiveTurn
	headlessQueues       map[string][]headlessCodexTurn
	headlessDeferredLead *headlessCodexTurn
	webMode              bool
	noOpen               bool

	notifyMu            sync.Mutex
	notifyLastDelivered map[string]time.Time
}

// SetUnsafe enables unrestricted permissions for all agents (CLI-only flag).
func (l *Launcher) SetUnsafe(v bool) { l.unsafe = v }

// SetOpusCEO upgrades the CEO agent from Sonnet to Opus.
func (l *Launcher) SetOpusCEO(v bool) { l.opusCEO = v }

// SetFocusMode enables CEO-routed delegation mode.
func (l *Launcher) SetFocusMode(v bool) { l.focusMode = v }

// SetNoOpen suppresses automatic browser launch on startup.
func (l *Launcher) SetNoOpen(v bool) { l.noOpen = v }

func (l *Launcher) SetOneOnOne(slug string) {
	l.sessionMode = SessionModeOneOnOne
	l.oneOnOne = NormalizeOneOnOneAgent(slug)
}

func isBlankSlateLaunchSlug(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "from-scratch", "blank-slate", blankSlateLaunchSlug:
		return true
	default:
		return false
	}
}

// NewLauncher creates a launcher for the given operation blueprint or legacy pack.
func NewLauncher(packSlug string) (*Launcher, error) {
	cfg, _ := config.Load()
	explicitPack := packSlug != "" // true when user passed --pack explicitly
	blankSlateLaunch := isBlankSlateLaunchSlug(packSlug) || strings.TrimSpace(os.Getenv("WUPHF_START_FROM_SCRATCH")) == "1"
	if isBlankSlateLaunchSlug(packSlug) {
		packSlug = ""
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	repoRoot := resolveRepoRoot(cwd)
	if packSlug == "" && !blankSlateLaunch {
		packSlug = cfg.ActiveBlueprint()
		if packSlug == "" {
			if manifest, err := company.LoadManifest(); err == nil {
				if refs := manifest.BlueprintRefsByKind("operation"); len(refs) > 0 {
					packSlug = refs[0].ID
				}
			}
		}
	}

	operationTemplateExists := false
	if strings.TrimSpace(packSlug) != "" {
		if _, err := operations.LoadBlueprint(repoRoot, packSlug); err == nil {
			operationTemplateExists = true
		}
	}
	var pack *agent.PackDefinition
	if !operationTemplateExists && !blankSlateLaunch {
		pack = agent.GetPack(packSlug)
	}
	if pack == nil && strings.TrimSpace(packSlug) != "" && !operationTemplateExists && !blankSlateLaunch {
		return nil, fmt.Errorf("unknown pack or operation blueprint: %s", packSlug)
	}

	// --pack is authoritative: when explicitly provided, reset company.json to
	// match the pack so the broker doesn't silently load stale members.
	if explicitPack {
		var err error
		switch {
		case operationTemplateExists:
			err = resetManifestToOperationBlueprint(repoRoot, packSlug)
		case pack != nil:
			err = resetManifestToPack(pack)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: save blueprint/pack config: %v\n", err)
		}
		// Drop stale broker state so the new pack starts clean.
		_ = os.Remove(brokerStatePath())
	}
	sessionMode, oneOnOne := loadRunningSessionMode()

	return &Launcher{
		packSlug:            packSlug,
		pack:                pack,
		blankSlateLaunch:    blankSlateLaunch,
		sessionName:         SessionName,
		cwd:                 cwd,
		sessionMode:         sessionMode,
		oneOnOne:            oneOnOne,
		provider:            config.ResolveLLMProvider(""),
		headlessWorkers:     make(map[string]bool),
		headlessActive:      make(map[string]*headlessCodexActiveTurn),
		headlessQueues:      make(map[string][]headlessCodexTurn),
		notifyLastDelivered: make(map[string]time.Time),
	}, nil
}

// Preflight checks that required tools are available.
func (l *Launcher) Preflight() error {
	if l.usesCodexRuntime() {
		if _, err := exec.LookPath("codex"); err != nil {
			return fmt.Errorf("codex not found. Install Codex CLI and run `codex login`")
		}
		return nil
	}
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
	if l.usesCodexRuntime() {
		return l.launchHeadlessCodex()
	}
	mcpConfig, err := l.ensureMCPConfig()
	if err != nil {
		return fmt.Errorf("prepare mcp config: %w", err)
	}
	l.mcpConfig = mcpConfig

	// Kill any stale broker from a previous run
	killStaleBroker()

	// Start the shared channel broker
	l.broker = NewBroker()
	l.broker.runtimeProvider = l.provider
	l.broker.packSlug = l.packSlug
	l.broker.blankSlateLaunch = l.blankSlateLaunch
	if err := l.broker.SetSessionMode(l.sessionMode, l.oneOnOne); err != nil {
		return fmt.Errorf("set session mode: %w", err)
	}
	if err := l.broker.SetFocusMode(l.focusMode); err != nil {
		return fmt.Errorf("set focus mode: %w", err)
	}
	if err := l.broker.Start(); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}

	// Pre-seed any default skills declared by the pack (idempotent).
	if l.pack != nil && len(l.pack.DefaultSkills) > 0 {
		l.broker.SeedDefaultSkills(l.pack.DefaultSkills)
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
		fmt.Sprintf("WUPHF_BROKER_BASE_URL=%s", l.BrokerBaseURL()),
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

	// Headless context for per-turn Claude invocations in TUI mode.
	l.headlessCtx, l.headlessCancel = context.WithCancel(context.Background())

	// Start the notification loop that pushes new messages to agent panes
	go l.watchChannelPaneLoop(channelCmd)
	go l.primeVisibleAgents()
	go l.notifyAgentsLoop()
	if !l.isOneOnOne() {
		go l.notifyTaskActionsLoop()
		go l.notifyOfficeChangesLoop()
		go l.pollNexNotificationsLoop()
		go l.watchdogSchedulerLoop()
	}

	return nil
}

// notifyAgentsLoop subscribes to broker messages and pushes notifications immediately.
func (l *Launcher) notifyAgentsLoop() {
	if l.broker == nil {
		return
	}
	msgs, unsubscribe := l.broker.SubscribeMessages(128)
	defer unsubscribe()

	for msg := range msgs {
		if l.broker.HasPendingInterview() {
			continue
		}
		if msg.From == "system" {
			continue
		}
		l.safeDeliverMessage(msg)
	}
}

// safeDeliverMessage wraps deliverMessageNotification in a panic recover so a
// bad message doesn't take the whole broker down. Stack is written to stderr
// and logs/panics.log so we can diagnose the next occurrence.
func (l *Launcher) safeDeliverMessage(msg channelMessage) {
	defer recoverPanicTo("deliverMessageNotification", fmt.Sprintf("msg=%+v", msg))
	l.deliverMessageNotification(msg)
}

// recoverPanicTo is the shared panic-recovery body used by broker background
// goroutines. It logs the goroutine stack to stderr and to
// ~/.wuphf/logs/panics.log so the broker stays up even if a specific action
// path blows up. Call as: defer recoverPanicTo("loopName", "extra context").
func recoverPanicTo(site, extra string) {
	r := recover()
	if r == nil {
		return
	}
	buf := make([]byte, 16<<10)
	n := runtime.Stack(buf, false)
	fmt.Fprintf(os.Stderr, "panic in %s: %v\n%s\n%s\n", site, r, extra, buf[:n])
	if home, err := os.UserHomeDir(); err == nil {
		if f, ferr := os.OpenFile(filepath.Join(home, ".wuphf", "logs", "panics.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); ferr == nil {
			fmt.Fprintf(f, "%s panic in %s: %v\n%s\n%s\n\n", time.Now().UTC().Format(time.RFC3339), site, r, extra, buf[:n])
			f.Close()
		}
	}
}

func (l *Launcher) notifyTaskActionsLoop() {
	if l.broker == nil {
		return
	}
	actions, unsubscribe := l.broker.SubscribeActions(128)
	defer unsubscribe()

	for action := range actions {
		if l.broker.HasPendingInterview() {
			continue
		}
		if action.Kind != "task_created" && action.Kind != "task_updated" && action.Kind != "task_unblocked" {
			continue
		}
		task, ok := l.taskForAction(action)
		if !ok {
			continue
		}
		// Skip "done" tasks for task_created / task_updated — the agent that completed
		// the task should send a follow-up broadcast which wakes CEO via the message
		// loop. But for task_unblocked the task status is still "in_progress" (it was
		// just unblocked), so we must never skip it regardless of status.
		if action.Kind != "task_unblocked" && strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			continue
		}
		func() {
			defer recoverPanicTo("deliverTaskNotification", fmt.Sprintf("action=%+v task=%+v", action, task))
			l.deliverTaskNotification(action, task)
		}()
	}
}

func (l *Launcher) notifyOfficeChangesLoop() {
	if l.broker == nil {
		return
	}
	changes, unsubscribe := l.broker.SubscribeOfficeChanges(128)
	defer unsubscribe()

	for evt := range changes {
		if l.broker.HasPendingInterview() {
			continue
		}
		l.deliverOfficeChangeNotification(evt)
	}
}

type officeChangeTaskNotification struct {
	Target  notificationTarget
	Action  officeActionLog
	Task    teamTask
	Content string
}

func (l *Launcher) deliverOfficeChangeNotification(evt officeChangeEvent) {
	for _, notification := range l.officeChangeTaskNotifications(evt) {
		l.sendTaskUpdate(notification.Target, notification.Action, notification.Task, notification.Content)
	}
}

func (l *Launcher) officeChangeTaskNotifications(evt officeChangeEvent) []officeChangeTaskNotification {
	if l == nil || l.broker == nil {
		return nil
	}

	kind := strings.TrimSpace(evt.Kind)
	slug := normalizeChannelSlug(evt.Slug)
	switch kind {
	case "member_created", "channel_created", "channel_updated":
	default:
		return nil
	}

	targetMap := l.agentPaneTargets()
	if len(targetMap) == 0 {
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)
	seen := make(map[string]struct{})
	var out []officeChangeTaskNotification
	for _, task := range l.broker.AllTasks() {
		owner := strings.TrimSpace(task.Owner)
		if owner == "" {
			continue
		}
		if !shouldBackfillTaskOwner(kind, slug, task) {
			continue
		}
		enabled := false
		for _, member := range l.broker.EnabledMembers(task.Channel) {
			if member == owner {
				enabled = true
				break
			}
		}
		if !enabled {
			continue
		}
		target, ok := targetMap[owner]
		if !ok {
			continue
		}
		key := owner + ":" + task.ID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		action := officeActionLog{
			Kind:      "task_updated",
			Source:    "office",
			Channel:   normalizeChannelSlug(task.Channel),
			Actor:     "system",
			RelatedID: task.ID,
			CreatedAt: now,
		}
		out = append(out, officeChangeTaskNotification{
			Target:  target,
			Action:  action,
			Task:    task,
			Content: l.taskNotificationContent(action, task),
		})
	}
	return out
}

func shouldBackfillTaskOwner(kind, slug string, task teamTask) bool {
	status := strings.ToLower(strings.TrimSpace(task.Status))
	if status == "done" || status == "canceled" || status == "cancelled" || status == "review" {
		return false
	}
	if task.Blocked {
		return false
	}
	switch kind {
	case "member_created":
		return strings.TrimSpace(task.Owner) == slug
	case "channel_created", "channel_updated":
		return normalizeChannelSlug(task.Channel) == slug
	default:
		return false
	}
}

const (
	agentNotifyCooldown      = 1 * time.Second
	agentNotifyCooldownAgent = 2 * time.Second
)

func (l *Launcher) deliverMessageNotification(msg channelMessage) {
	immediate, delayed := l.notificationTargetsForMessage(msg)

	// Debounce: use shorter cooldown for human/CEO messages, longer for agent-originated
	// to prevent agent-to-agent feedback loops (devil's advocate finding #3)
	isHumanOrCEO := msg.From == "you" || msg.From == "human" || msg.From == "nex" || msg.From == l.officeLeadSlug()
	cooldown := agentNotifyCooldownAgent
	if isHumanOrCEO {
		cooldown = agentNotifyCooldown
	}
	now := time.Now()
	filtered := make([]notificationTarget, 0, len(immediate))
	l.notifyMu.Lock()
	if l.notifyLastDelivered == nil {
		l.notifyLastDelivered = make(map[string]time.Time)
	}
	for _, t := range immediate {
		if last, ok := l.notifyLastDelivered[t.Slug]; ok && now.Sub(last) < cooldown {
			continue
		}
		l.notifyLastDelivered[t.Slug] = now
		filtered = append(filtered, t)
	}
	l.notifyMu.Unlock()
	immediate = filtered

	// Broadcast stage update only for untagged messages in public channels.
	// Never in DMs — DMs are private 1:1 conversations, no routing noise.
	isDM, _ := l.isChannelDM(normalizeChannelSlug(msg.Channel))
	if l.broker != nil && len(immediate) > 0 && (msg.From == "you" || msg.From == "human") && !l.isOneOnOne() && !isDM && len(msg.Tagged) == 0 {
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
		l.sendChannelUpdate(target, msg)
	}
	// Note: delayed is always empty for message notifications — notificationTargetsForMessage
	// only ever populates immediate. The delayed path is used only for task notifications
	// via taskNotificationTargets/deliverTaskNotification.
	_ = delayed
}

func (l *Launcher) deliverTaskNotification(action officeActionLog, task teamTask) {
	immediate, delayed := l.taskNotificationTargets(action, task)
	if len(immediate) == 0 && len(delayed) == 0 {
		return
	}
	content := l.taskNotificationContent(action, task)
	for _, target := range immediate {
		l.sendTaskUpdate(target, action, task, content)
	}
	for _, target := range delayed {
		go func(target notificationTarget, action officeActionLog, task teamTask) {
			time.Sleep(ceoHeadStartDelay)
			if !l.shouldDeliverDelayedTaskNotification(target.Slug, action, task) {
				return
			}
			l.sendTaskUpdate(target, action, task, content)
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
	//
	// Exception: do not wake the owner when the task is blocked (unresolved
	// dependencies). They have no work to do until the blocker clears. They
	// will be notified via a task_unblocked action when deps resolve.
	if (action.Kind == "task_created" || action.Kind == "watchdog_alert" || action.Kind == "task_unblocked") && owner != actor && !task.Blocked {
		addImmediate(owner)
	} else if owner != actor && action.Kind != "task_created" {
		addDelayed(owner)
	}

	if lead != "" && lead != owner && lead != actor && !(action.Kind == "task_created" && actor == lead) && shouldWakeLeadForTaskAction(action, task) {
		addImmediate(lead)
	}

	return immediate, delayed
}

func shouldWakeLeadForTaskAction(action officeActionLog, task teamTask) bool {
	if strings.TrimSpace(action.Kind) != "task_updated" {
		return true
	}
	actor := strings.TrimSpace(action.Actor)
	owner := strings.TrimSpace(task.Owner)
	if actor == "" || owner == "" || actor != owner {
		return true
	}
	if task.Blocked {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(task.Status))
	review := strings.ToLower(strings.TrimSpace(task.ReviewState))
	if status == "review" || status == "done" || status == "blocked" {
		return true
	}
	if review == "ready_for_review" || review == "approved" {
		return true
	}
	return false
}

func (l *Launcher) taskForAction(action officeActionLog) (teamTask, bool) {
	if l.broker == nil || strings.TrimSpace(action.RelatedID) == "" {
		return teamTask{}, false
	}
	id := strings.TrimSpace(action.RelatedID)
	for _, task := range l.broker.AllTasks() {
		if task.ID == id {
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
	case "task_unblocked":
		verb = "Task unblocked — dependencies resolved, ready to start"
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
	worktree := ""
	if strings.TrimSpace(task.WorktreeBranch) != "" || strings.TrimSpace(task.WorktreePath) != "" {
		parts := make([]string, 0, 2)
		if strings.TrimSpace(task.WorktreeBranch) != "" {
			parts = append(parts, "branch "+task.WorktreeBranch)
		}
		if strings.TrimSpace(task.WorktreePath) != "" {
			parts = append(parts, "path "+task.WorktreePath)
		}
		worktree = ", worktree " + strings.Join(parts, " · ")
	}
	guidance := ""
	if path := strings.TrimSpace(task.WorktreePath); path != "" {
		guidance = fmt.Sprintf(" If you own this task, use working_directory=%q for local file and bash tools.", path)
	}
	framing := ""
	if taskRequiresRealExternalExecution(&task) {
		framing = " Live business framing: describe the work as a client deliverable, approval, handoff, update, or record. Do not present it as a proof marker, eval artifact, or test artifact unless the task explicitly asks for testing or evidence capture."
	}
	capability := ""
	if taskRequiresRealExternalExecution(&task) {
		capability = "\n" + capabilityGapCoachingBlock()
	}
	hygiene := ""
	if taskLooksLikeLiveBusinessObjective(&task) {
		hygiene = "\n" + taskHygieneCoachingBlock()
	}
	return fmt.Sprintf("[%s #%s on #%s]: %s%s (owner %s, status %s%s%s%s%s). Context is included — do NOT call team_poll or team_tasks. Respond with the concrete next step immediately. Stay in your lane. Once you have posted the needed update, STOP and wait for the next pushed notification.%s%s%s%s", verb, task.ID, channel, task.Title, details, owner, status, pipeline, review, execMode, worktree, guidance, framing, capability, hygiene)
}

func (l *Launcher) sendTaskUpdate(target notificationTarget, action officeActionLog, task teamTask, content string) {
	channel := normalizeChannelSlug(task.Channel)
	if channel == "" {
		channel = "general"
	}
	notification := l.buildTaskExecutionPacket(target.Slug, action, task, content)
	if l.usesCodexRuntime() || l.webMode {
		l.enqueueHeadlessCodexTurn(target.Slug, headlessSandboxNote()+notification, channel)
		return
	}
	l.sendNotificationToPane(target.PaneTarget, notification)
}

// isChannelDM returns true if the channel is a DM (either old dm-* format or new Store type).
// agentTarget returns the agent slug that should receive the DM notification (non-human side).
func (l *Launcher) isChannelDM(channelSlug string) (isDM bool, agentTarget string) {
	// Legacy format: dm-{agent}
	if IsDMSlug(channelSlug) {
		return true, DMTargetAgent(channelSlug)
	}
	// New store format: channel type == D
	if l.broker != nil {
		cs := l.broker.ChannelStore()
		if cs != nil && cs.IsDirectMessageBySlug(channelSlug) {
			ch, ok := cs.GetBySlug(channelSlug)
			if ok {
				members := cs.Members(ch.ID)
				for _, m := range members {
					if m.Slug != "human" && m.Slug != "you" {
						return true, m.Slug
					}
				}
			}
		}
	}
	return false, ""
}

func (l *Launcher) notificationTargetsForMessage(msg channelMessage) (immediate []notificationTarget, delayed []notificationTarget) {
	targetMap := l.agentPaneTargets()
	if len(targetMap) == 0 {
		return nil, nil
	}
	// DMs are isolated: only the target agent gets notified, never CEO or others.
	if ch := normalizeChannelSlug(msg.Channel); IsDMSlug(ch) {
		agentSlug := DMTargetAgent(ch)
		if agentSlug == msg.From {
			return nil, nil // agent's own message, don't echo back
		}
		if target, ok := targetMap[agentSlug]; ok {
			return []notificationTarget{target}, nil
		}
		return nil, nil
	}
	// Also check the new Store-based DM format.
	if ch := normalizeChannelSlug(msg.Channel); !IsDMSlug(ch) {
		if isDM, agentSlug := l.isChannelDM(ch); isDM {
			if agentSlug == msg.From {
				return nil, nil
			}
			if target, ok := targetMap[agentSlug]; ok {
				return []notificationTarget{target}, nil
			}
			return nil, nil
		}
	}
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
	owner := ""
	if l.broker != nil {
		owner = l.taskOwnerForMessage(msg)
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
		// Explicit @-tag: always allow regardless of domain. Domain inference is
		// for implicit routing only — it should never suppress an explicit mention.
		if containsSlug(msg.Tagged, slug) {
			return true
		}
		if owner != "" {
			return slug == owner
		}
		if strings.TrimSpace(msg.Content) == "" && strings.TrimSpace(msg.Title) == "" {
			return false
		}
		return l.messageTargetsAgent(msg, slug)
	}

	// Focus mode (delegation): CEO routes all work. Specialists only wake
	// when explicitly tagged by CEO or human. No cross-agent chatter.
	if l.isFocusModeEnabled() {
		switch {
		case msg.From == "you" || msg.From == "human" || msg.Kind == "automation" || msg.From == "nex":
			// When the human explicitly @tags one or more specialists, deliver directly
			// to those specialists only. CEO does not need to re-route explicit assignments —
			// the specialist is already awake and acting. CEO only sees untagged human messages
			// (general questions, requests that need routing decisions).
			humanExplicitlyTaggedSpecialists := false
			for _, slug := range msg.Tagged {
				if slug == "" || slug == msg.From || slug == lead {
					continue
				}
				// Human explicitly tagged this specialist. Skip domain inference —
				// the human's intent is explicit and trumps content-based routing.
				// Only check that the specialist is an enabled channel member.
				isEnabled := len(enabledMembers) == 0
				if !isEnabled {
					_, isEnabled = enabledMembers[slug]
				}
				if isEnabled {
					if target, ok := targetMap[slug]; ok {
						immediate = append(immediate, target)
						delete(targetMap, slug)
						humanExplicitlyTaggedSpecialists = true
					}
				}
			}
			if !humanExplicitlyTaggedSpecialists {
				// No specialist tagged — CEO decides who handles this.
				addImmediate(lead)
			}
		case msg.From == lead:
			for _, slug := range msg.Tagged {
				if slug != lead && allowTarget(slug) {
					addImmediate(slug)
				}
			}
		default:
			// Specialist message: wake CEO only if it is a substantive update (not a status ping).
			// [STATUS] lines are internal progress markers — CEO does not need to re-route on them.
			isStatusOnly := strings.HasPrefix(strings.TrimSpace(msg.Content), "[STATUS]")
			if !isStatusOnly {
				addImmediate(lead)
			}
		}
		return immediate, delayed
	}

	// Collaborative mode: all agents can see domain-relevant messages
	switch {
	case msg.From == "you" || msg.From == "human" || msg.Kind == "automation" || msg.From == "nex":
		// @all: notify every agent immediately.
		if containsSlug(msg.Tagged, "all") {
			addImmediate(lead)
			for slug := range targetMap {
				addImmediate(slug)
			}
			break
		}
		addImmediate(lead)
		if owner != "" && owner != lead && allowTarget(owner) {
			addImmediate(owner)
		}
		for _, slug := range msg.Tagged {
			if allowTarget(slug) {
				addImmediate(slug)
			}
		}
	case msg.From == lead:
		for _, slug := range msg.Tagged {
			if allowTarget(slug) {
				addImmediate(slug)
			}
		}
	case containsSlug(msg.Tagged, lead):
		addImmediate(lead)
		if owner != "" && owner != lead && allowTarget(owner) {
			addImmediate(owner)
		}
		for _, slug := range msg.Tagged {
			if allowTarget(slug) {
				addImmediate(slug)
			}
		}
	default:
		// Specialist-to-channel message in collaborative mode: CEO stays in the loop
		// plus any tagged agents and the task owner.
		addImmediate(lead)
		if owner != "" && owner != lead && allowTarget(owner) {
			addImmediate(owner)
		}
		for _, slug := range msg.Tagged {
			if allowTarget(slug) {
				addImmediate(slug)
			}
		}
	}
	return immediate, delayed
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
	home := config.RuntimeHomeDir()
	if home == "" {
		return ".wuphf-channel-stderr.log"
	}
	return filepath.Join(home, ".wuphf", "logs", "channel-stderr.log")
}

func channelPaneSnapshotPath() string {
	home := config.RuntimeHomeDir()
	if home == "" {
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
	if len(msgs) > 0 {
		latest := msgs[len(msgs)-1]
		l.deliverMessageNotification(latest)
	}
	l.resumeInFlightWork()
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
	now := time.Now().UTC()
	// Blocked tasks are legitimately waiting on dependencies — skip the watchdog
	// reminder entirely. The owner cannot act until their blockers resolve, so a
	// "still waiting" nudge is both misleading and a wasted token spend. The
	// task_unblocked mechanism will wake them when deps clear.
	if task.Blocked {
		if retryAt, rateLimited := externalWorkflowRetryAfter(errors.New(task.Details), now); rateLimited && !retryAt.After(now) {
			resumeNote := "Retry window passed; resuming live external lane automatically."
			resumed, changed, err := l.broker.ResumeTask(task.ID, "watchdog", resumeNote)
			if err == nil && changed {
				_ = l.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
				l.deliverTaskNotification(officeActionLog{
					Kind:      "task_unblocked",
					Source:    "watchdog",
					Channel:   resumed.Channel,
					Actor:     "watchdog",
					RelatedID: resumed.ID,
				}, resumed)
				return
			}
		}
		nextRun := time.Now().UTC().Add(time.Duration(config.ResolveTaskReminderInterval()) * time.Minute)
		_ = l.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
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
	providerName := strings.TrimSpace(payload.Provider)
	if providerName == "" {
		providerName = strings.TrimSpace(job.Provider)
	}
	registry := action.NewRegistryFromEnv()
	provider, err := registry.ProviderNamed(providerName, action.CapabilityWorkflowExecute)
	if err != nil {
		source := providerName
		if strings.TrimSpace(source) == "" {
			source = "workflow"
		}
		summary := fmt.Sprintf("Scheduled workflow %s could not start: %v", workflowKey, err)
		_ = l.broker.RecordAction("external_workflow_failed", source, channel, "scheduler", truncate(summary, 140), workflowKey, nil, "")
		_ = l.broker.UpdateSkillExecutionByWorkflowKey(workflowKey, "failed", time.Now().UTC())
		if nextRun, hasNext := nextWorkflowRun(strings.TrimSpace(payload.ScheduleExpr), time.Now().UTC()); hasNext {
			_ = l.broker.UpdateSchedulerJobState(job.Slug, nextRun, "scheduled")
		} else {
			_ = l.broker.UpdateSchedulerJobState(job.Slug, time.Time{}, "done")
		}
		return
	}
	result, err := provider.ExecuteWorkflow(context.Background(), action.WorkflowExecuteRequest{
		KeyOrPath: workflowKey,
		Inputs:    payload.Inputs,
	})
	now := time.Now().UTC()
	nextRun, hasNext := nextWorkflowRun(strings.TrimSpace(payload.ScheduleExpr), now)
	if err != nil {
		summary := fmt.Sprintf("Scheduled workflow %s failed via %s", workflowKey, strings.Title(provider.Name()))
		_ = l.broker.RecordAction("external_workflow_failed", provider.Name(), channel, "scheduler", summary, workflowKey, nil, "")
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
	summary := fmt.Sprintf("Scheduled workflow %s ran via %s", workflowKey, strings.Title(provider.Name()))
	_ = l.broker.RecordAction("external_workflow_executed", provider.Name(), channel, "scheduler", summary, workflowKey, nil, "")
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

func (l *Launcher) usesCodexRuntime() bool {
	return strings.EqualFold(strings.TrimSpace(l.provider), "codex")
}

// memberEffectiveProviderKind returns the provider kind that should run the
// given agent's next turn. Lookup order: member's per-agent ProviderBinding
// (set via /agent create --provider=X or the hire-agent modal), then the
// install-wide l.provider fallback.
func (l *Launcher) memberEffectiveProviderKind(slug string) string {
	if l.broker != nil {
		if kind := l.broker.MemberProviderKind(slug); kind != "" {
			return normalizeProviderKind(kind)
		}
	}
	return normalizeProviderKind(l.provider)
}

// normalizeProviderKind trims and canonicalizes provider kinds while
// preserving unknown values so dispatch code can surface explicit errors.
func normalizeProviderKind(raw string) string {
	k := strings.ToLower(strings.TrimSpace(raw))
	switch k {
	case "claude", "":
		return provider.KindClaudeCode
	case "codex":
		return provider.KindCodex
	case "claude-code", "openclaw":
		return k
	default:
		return k
	}
}

func (l *Launcher) UsesTmuxRuntime() bool {
	return !l.usesCodexRuntime()
}

func (l *Launcher) BrokerToken() string {
	if l == nil || l.broker == nil {
		return ""
	}
	return l.broker.Token()
}

// OneOnOneAgent returns the active direct-session agent slug, if any.
func (l *Launcher) OneOnOneAgent() string {
	return l.oneOnOneAgent()
}

// killStaleBroker kills any process holding the configured broker port from a previous run.
func killStaleBroker() {
	out, err := exec.Command("lsof", "-i", fmt.Sprintf(":%d", brokeraddr.ResolvePort()), "-t").Output()
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

func extractTaskFileTargets(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	seen := map[string]struct{}{}
	targets := make([]string, 0, 4)
	for {
		start := strings.Index(text, "`")
		if start < 0 {
			break
		}
		text = text[start+1:]
		end := strings.Index(text, "`")
		if end < 0 {
			break
		}
		candidate := strings.TrimSpace(text[:end])
		text = text[end+1:]
		if candidate == "" {
			continue
		}
		if !strings.Contains(candidate, "/") && !strings.Contains(candidate, ".") {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		targets = append(targets, candidate)
		if len(targets) == 4 {
			break
		}
	}
	return targets
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
	if os.Getenv("TERM_PROGRAM") == "iTerm.app" {
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
	if l.headlessCancel != nil {
		l.headlessCancel()
	}
	if l.broker != nil {
		l.broker.Stop()
	}
	if l.usesCodexRuntime() {
		if err := killPersistedOfficeProcess(); err != nil {
			return err
		}
		killStaleHeadlessTaskRunners()
		_ = clearOfficePIDFile()
		return nil
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
	if l.usesCodexRuntime() {
		if l != nil && l.broker != nil {
			l.broker.Reset()
			return nil
		}
		if err := ResetBrokerState(); err != nil {
			return fmt.Errorf("reset broker state: %w", err)
		}
		return nil
	}
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
	if l.usesCodexRuntime() {
		if err := provider.ResetClaudeSessions(); err != nil {
			return fmt.Errorf("reset Claude sessions: %w", err)
		}
		if err := l.clearAgentPanes(); err != nil {
			return err
		}
		l.clearOverflowAgentWindows()
		return nil
	}
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
		cmd := l.claudeCommand(slug, l.buildPrompt(slug))

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

// buildNotificationContext returns a compact summary of recent channel messages
// for inline injection into agent notifications. Filters system and status messages.
// buildNotificationContext returns a pre-labeled context section for work packets.
//
// When threadRootID is non-empty, the function first tries to build thread-scoped
// context: messages whose ID equals threadRootID (the root) or whose ReplyTo equals
// threadRootID (direct replies). This is labeled "[Recent thread]" and gives agents
// only the relevant sub-conversation, not unrelated channel noise.
//
// When threadRootID is empty, or when the thread has no displayable messages (e.g.
// the trigger is the first message in a new thread), the function falls back to the
// last N channel messages and labels the section "[Recent channel]". Agents should
// treat "[Recent channel]" as broad ambient context rather than the specific thread
// they are responding in.
//
// The trigger message (triggerMsgID) is always excluded — it is already delivered
// explicitly as "[New from @...]" in the notification, so including it again wastes
// ~150 tokens per turn.
func (l *Launcher) buildNotificationContext(channel, triggerMsgID, threadRootID string, limit int) string {
	if l.broker == nil {
		return ""
	}
	if strings.TrimSpace(channel) == "" {
		channel = "general"
	}

	msgs := l.broker.ChannelMessages(channel)
	if len(msgs) == 0 {
		return ""
	}

	// baseFilter excludes system messages, STATUS messages, and the trigger itself.
	baseFilter := func(m channelMessage) bool {
		if m.From == "system" {
			return false
		}
		if strings.TrimSpace(triggerMsgID) != "" && strings.TrimSpace(m.ID) == strings.TrimSpace(triggerMsgID) {
			return false
		}
		if strings.HasPrefix(strings.TrimSpace(m.Content), "[STATUS]") {
			return false
		}
		return true
	}

	formatContext := func(items []channelMessage) string {
		var b strings.Builder
		for _, m := range items {
			b.WriteString(fmt.Sprintf("@%s: %s\n", m.From, truncate(m.Content, 600)))
		}
		return strings.TrimRight(b.String(), "\n")
	}

	// Thread-scoped context: show all messages in the thread tree (full BFS from
	// root), always anchoring with the root message so agents see the original
	// human ask. This matters for dependent task chains — e.g., a marketing agent
	// writing an email "based on research" needs to see the researcher's results,
	// which are grandchildren of the thread root, not direct children.
	//
	// Approach: always include the root, fill remaining slots with the most recent
	// non-root thread messages (so the latest activity is always visible).
	threadRoot := strings.TrimSpace(threadRootID)
	if threadRoot != "" {
		threadIDs := l.threadMessageIDs(channel, threadRoot)
		// Separate root from rest so we can always preserve it.
		var rootMsg *channelMessage
		var rest []channelMessage
		for i := range msgs {
			m := &msgs[i]
			if !baseFilter(*m) {
				continue
			}
			if strings.TrimSpace(m.ID) == threadRoot {
				rootMsg = m
			} else if _, inThread := threadIDs[strings.TrimSpace(m.ID)]; inThread {
				rest = append(rest, *m)
			}
		}
		if rootMsg != nil || len(rest) > 0 {
			// Take last (limit-1) from rest to leave a slot for the root.
			remaining := limit
			if rootMsg != nil {
				remaining--
			}
			if len(rest) > remaining {
				rest = rest[len(rest)-remaining:]
			}
			var thread []channelMessage
			if rootMsg != nil {
				thread = append(thread, *rootMsg)
			}
			thread = append(thread, rest...)
			return "[Recent thread]\n" + formatContext(thread)
		}
	}

	// Fall back to recent channel messages when there is no thread context.
	var filtered []channelMessage
	for _, m := range msgs {
		if baseFilter(m) {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	if len(filtered) > limit {
		filtered = filtered[len(filtered)-limit:]
	}
	return "[Recent channel]\n" + formatContext(filtered)
}

// ultimateThreadRoot walks the reply-to chain from startID up to the topmost
// ancestor (the message with no ReplyTo) and returns its ID. This ensures that
// when building thread context for a deep reply chain — e.g., human ask → CEO
// delegation → specialist response — the filtering anchors at the original human
// ask rather than a mid-thread message, so all participants see the full context.
//
// If startID is empty, startID itself is returned. The walk is capped at 8 hops
// to prevent cycles or unbounded work on pathological data.
func (l *Launcher) ultimateThreadRoot(channel, startID string) string {
	startID = strings.TrimSpace(startID)
	if startID == "" || l.broker == nil {
		return startID
	}
	msgs := l.broker.ChannelMessages(channel)
	byID := make(map[string]channelMessage, len(msgs))
	for _, m := range msgs {
		if id := strings.TrimSpace(m.ID); id != "" {
			byID[id] = m
		}
	}
	root := startID
	for depth := 0; depth < 8; depth++ {
		m, ok := byID[root]
		if !ok {
			break
		}
		parent := strings.TrimSpace(m.ReplyTo)
		if parent == "" {
			break
		}
		root = parent
	}
	return root
}

// threadMessageIDs returns the set of all message IDs that belong to the thread
// rooted at rootID (BFS traversal via replyTo reverse index). The root itself is
// included. Returns an empty set when rootID is empty or broker is nil.
func (l *Launcher) threadMessageIDs(channel, rootID string) map[string]struct{} {
	rootID = strings.TrimSpace(rootID)
	result := make(map[string]struct{})
	if rootID == "" || l.broker == nil {
		return result
	}
	msgs := l.broker.ChannelMessages(channel)
	byParent := make(map[string][]string, len(msgs))
	for _, m := range msgs {
		parent := strings.TrimSpace(m.ReplyTo)
		if parent != "" {
			byParent[parent] = append(byParent[parent], strings.TrimSpace(m.ID))
		}
	}
	result[rootID] = struct{}{}
	queue := []string{rootID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range byParent[cur] {
			if _, seen := result[child]; !seen {
				result[child] = struct{}{}
				queue = append(queue, child)
			}
		}
	}
	return result
}

func (l *Launcher) buildTaskNotificationContext(channel, slug string, limit int) string {
	if l.broker == nil || limit <= 0 {
		return ""
	}

	var tasks []teamTask
	if strings.TrimSpace(channel) == "" {
		// No specific channel: scan all tasks across all channels so non-general
		// channel tasks are not silently omitted from the CEO's task context.
		tasks = l.broker.AllTasks()
	} else {
		tasks = l.broker.ChannelTasks(channel)
	}
	if len(tasks) == 0 {
		return ""
	}

	formatTask := func(task teamTask) string {
		owner := strings.TrimSpace(task.Owner)
		switch {
		case owner == "":
			owner = "unassigned"
		default:
			owner = "@" + owner
		}
		status := strings.TrimSpace(task.Status)
		if status == "" {
			status = "open"
		}
		meta := owner + ", " + status
		if task.Blocked {
			meta += ", blocked"
		}
		if len(task.DependsOn) > 0 {
			meta += ", depends: " + strings.Join(task.DependsOn, " ")
		}
		taskChannel := normalizeChannelSlug(task.Channel)
		if taskChannel == "" {
			taskChannel = "general"
		}
		line := fmt.Sprintf("- #%s on #%s %s (%s)", task.ID, taskChannel, truncate(task.Title, 72), meta)
		if details := strings.TrimSpace(task.Details); details != "" {
			line += ": " + truncate(details, 96)
		}
		return line
	}

	// Sort tasks so the most recently updated active work surfaces first.
	// This prevents a fixed insertion-order view where the first 3 tasks created
	// always fill the slot, potentially hiding newer or more urgent work.
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].UpdatedAt > tasks[j].UpdatedAt
	})

	lines := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	addTask := func(task teamTask) {
		if len(lines) >= limit {
			return
		}
		if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			return
		}
		if _, ok := seen[task.ID]; ok {
			return
		}
		seen[task.ID] = struct{}{}
		lines = append(lines, formatTask(task))
	}

	lead := l.officeLeadSlug()
	for _, task := range tasks {
		if slug == lead || strings.TrimSpace(task.Owner) == slug {
			addTask(task)
		}
	}
	if len(lines) == 0 {
		for _, task := range tasks {
			addTask(task)
		}
	}
	if len(lines) == 0 {
		if slug == lead {
			return "Active tasks:\n- None currently active. If the overall build is not actually finished, create the next owned task(s) now instead of ending with narrative next steps."
		}
		return ""
	}

	result := "Active tasks:\n" + strings.Join(lines, "\n")
	if slug == lead {
		reviewCount := 0
		for _, task := range tasks {
			if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(task.Status), "review") || strings.EqualFold(strings.TrimSpace(task.ReviewState), "ready_for_review") {
				reviewCount++
			}
		}
		if reviewCount > 0 {
			result += fmt.Sprintf("\nLead action: %d task(s) are waiting in review. Approve them or translate them into the next owned task(s) before you stop.", reviewCount)
		}
	}
	return result
}

func (l *Launcher) relevantTaskForTarget(msg channelMessage, slug string) (teamTask, bool) {
	if l.broker == nil || slug == "" {
		return teamTask{}, false
	}
	channel := normalizeChannelSlug(msg.Channel)
	if channel == "" {
		channel = "general"
	}
	threadRoot := strings.TrimSpace(msg.ID)
	if replyTo := strings.TrimSpace(msg.ReplyTo); replyTo != "" {
		threadRoot = replyTo
	}
	// Search all channels: a specialist's task may live in a dedicated channel (e.g.
	// "engineering") even when the triggering message arrived from "general". Using
	// ChannelTasks(channel) here caused cross-channel delegations to silently omit the
	// "Active task" line from work packets and give specialists the wrong response
	// instruction ("stay quiet" instead of "you own matching work").
	var domainOwned teamTask
	bestOwnedScore := 0.0
	for _, task := range l.broker.AllTasks() {
		if strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			continue
		}
		if strings.TrimSpace(task.Owner) != slug {
			continue
		}
		if task.ThreadID != "" && (task.ThreadID == msg.ID || task.ThreadID == threadRoot) {
			return task, true
		}
		score := l.scoreMessageForTaskCandidate(msg, task)
		if score >= officeRoutingMatchThreshold && score > bestOwnedScore {
			domainOwned = task
			bestOwnedScore = score
		}
	}
	if domainOwned.ID != "" {
		return domainOwned, true
	}
	return teamTask{}, false
}

func (l *Launcher) responseInstructionForTarget(msg channelMessage, slug string) string {
	lead := l.officeLeadSlug()
	if slug == lead {
		// When the CEO is woken by a specialist (msg.From is not human/you/system),
		// the context is different: the specialist just finished work and the CEO
		// should review and deliver or coordinate, not re-initiate. When woken by
		// the human, the CEO should reply quickly and route.
		from := strings.TrimSpace(msg.From)
		isFromHuman := from == "" || from == "you" || from == "human" || from == "nex"
		if !isFromHuman {
			return fmt.Sprintf("You are @%s. A specialist just finished a lane. If the build is still underway, any task is open or in review, or the next lane is obvious, act now: approve/release review items, create the next owned team_task records, and only then stop. On a human build/ship/end-to-end request, if you approve or close an engineering/execution slice and the product is not yet runnable end to end, you MUST leave at least one engineering or execution lane active before you stop; do not let the company drift into GTM-only, rubric-only, or evaluation-only work while the build is still incomplete. Before you say a task is approved, closed, back in progress, reassigned, or blocked, you MUST make the matching team_task or team_plan call first; channel narration alone does not change durable state. If the task mutation fails, say that it failed and do not claim the state changed. Before creating any new task, inspect Active tasks in this packet: if a live task already covers that lane, reuse or update that task instead of creating an overlapping duplicate with a new title. Stay quiet only when the human already has what they need AND there is no remaining office work or obvious follow-up.", slug)
		}
		return fmt.Sprintf("You are @%s. Give the first top-level reply quickly, then pull in specialists only when needed. For build/ship/end-to-end requests, the first engineering task itself must be a single smallest runnable feature slice, not an MVP umbrella or a multi-output minimum bar. Do not put a separate repo audit, architecture, or cut-line research task in front of that first feature unless the human explicitly asked for analysis first or the repo truly has no identifiable implementation target. Do not spend the whole first turn on `pwd`, `ls`, `rg --files`, `find .`, or another repo-wide inventory; use the named docs/configs in the packet or at most one or two targeted reads, then create the first durable task/channel state in that same turn. %s", slug, capabilityGapCoachingBlock())
	}
	// DMs are direct conversations: the human chose to message this agent
	// specifically. Always respond, regardless of @tags or task ownership.
	if ch := normalizeChannelSlug(msg.Channel); IsDMSlug(ch) && DMTargetAgent(ch) == slug {
		return fmt.Sprintf("You are @%s. The human is messaging you directly in a DM. Respond helpfully from your domain expertise.", slug)
	}
	// Also check the new Store-based DM format (deterministic slug).
	if isDM, agentTarget := l.isChannelDM(normalizeChannelSlug(msg.Channel)); isDM && agentTarget == slug {
		return fmt.Sprintf("You are @%s. The human is messaging you directly in a DM. Respond helpfully from your domain expertise.", slug)
	}
	if containsSlug(msg.Tagged, slug) {
		return fmt.Sprintf("You are @%s. You were directly tagged. Reply only from your domain with concrete progress, a blocker, or a handoff.", slug)
	}
	if task, ok := l.relevantTaskForTarget(msg, slug); ok && strings.TrimSpace(task.Owner) == slug {
		if taskRequiresRealExternalExecution(&task) {
			return fmt.Sprintf("You are @%s. You already own matching work that requires a real connected-system action. Take the smallest allowed live external step now or report a blocker; repo docs, previews, local markdown, proof markers, and test artifacts do not satisfy it. Frame the result as a business deliverable, approval, handoff, or record, not as an eval or proof artifact. %s %s", slug, capabilityGapCoachingBlock(), taskHygieneCoachingBlock())
		}
		return fmt.Sprintf("You are @%s. You already own matching work. Reply only with concrete progress or a blocker; do not re-triage the thread.", slug)
	}
	return fmt.Sprintf("You are @%s. Stay quiet unless you are directly tagged, you own the active work, or you can unblock it. Prefer not to reply.", slug)
}

func (l *Launcher) buildMessageWorkPacket(msg channelMessage, slug string) string {
	channel := normalizeChannelSlug(msg.Channel)
	if channel == "" {
		channel = "general"
	}
	lines := []string{
		"Work packet:",
		fmt.Sprintf("- Thread: #%s reply_to %s", channel, msg.ID),
	}
	// Add DM context preamble when the agent is receiving a direct message.
	// This replaces the "stay quiet unless tagged" default with explicit DM semantics.
	if isDM, _ := l.isChannelDM(channel); isDM {
		dmPreamble := []string{
			"Context: DIRECT MESSAGE",
			"This is a private 1:1 conversation with the human. Respond to every message.",
			"You do not need to coordinate with other agents.",
			"---",
		}
		lines = append(dmPreamble, lines...)
	} else if l.broker != nil {
		// Check for group DM context.
		cs := l.broker.ChannelStore()
		if cs != nil {
			if storeChannel, ok := cs.GetBySlug(channel); ok && storeChannel.Type == "G" {
				members := cs.Members(storeChannel.ID)
				names := make([]string, 0, len(members))
				for _, m := range members {
					if m.Slug != slug {
						names = append(names, "@"+m.Slug)
					}
				}
				groupPreamble := []string{
					"Context: GROUP MESSAGE",
					fmt.Sprintf("This is a group conversation with: %s.", strings.Join(names, ", ")),
					"Respond to messages directed at you or within your expertise.",
					"---",
				}
				lines = append(groupPreamble, lines...)
			}
		}
	}
	if containsSlug(msg.Tagged, slug) {
		lines = append(lines, "- Trigger: you were explicitly tagged")
	}
	if task, ok := l.relevantTaskForTarget(msg, slug); ok {
		lines = append(lines, fmt.Sprintf("- Active task: #%s %s (%s)", task.ID, truncate(task.Title, 96), strings.TrimSpace(task.Status)))
		if details := strings.TrimSpace(task.Details); details != "" {
			lines = append(lines, fmt.Sprintf("- Task details: %s", truncate(details, 512)))
		}
		if path := strings.TrimSpace(task.WorktreePath); path != "" {
			lines = append(lines, fmt.Sprintf("- Working directory: %q", path))
		}
	}
	// Walk up the reply chain to find the ultimate thread root (the original human
	// ask) before filtering. This ensures that for a deep thread — human ask (X) →
	// CEO delegation (Y) → specialist response (Z) — all participants see X as the
	// anchor, not just the immediate parent. For top-level messages (ReplyTo == ""),
	// ultimateThreadRoot returns "" and the function falls back to recent channel
	// context automatically.
	threadRoot := l.ultimateThreadRoot(channel, msg.ReplyTo)
	if ctx := l.buildNotificationContext(channel, msg.ID, threadRoot, 4); ctx != "" {
		lines = append(lines, ctx)
	}
	if slug == l.officeLeadSlug() {
		// Always use AllTasks (pass "") so the CEO sees tasks across all channels,
		// not just the channel of the message that woke them. Without this, a CEO
		// woken from the "engineering" channel would miss "general"-channel tasks.
		if taskCtx := l.buildTaskNotificationContext("", slug, 3); taskCtx != "" {
			lines = append(lines, taskCtx)
		}
		// For the lead (CEO), explicitly list which specialist agents have already
		// posted in this thread OR are currently queued/active in the headless runner.
		// This prevents CEO from re-routing agents who are already working, including
		// the race-condition case where a specialist was notified but hasn't posted yet.
		// Rule: if a specialist is in this list, HOLD — do not tag them.
		activeAgents := map[string]struct{}{}
		if l.broker != nil {
			// Collect all message IDs that belong to this thread (full BFS from
			// the ultimate root). This prevents the CEO from re-routing specialists
			// who already acted at any depth, including the case of parallel
			// delegations where two specialists both replied to the original ask.
			threadRoot := strings.TrimSpace(l.ultimateThreadRoot(channel, msg.ReplyTo))
			if threadRoot == "" {
				threadRoot = strings.TrimSpace(msg.ID)
			}
			threadIDs := l.threadMessageIDs(channel, threadRoot)
			allMsgs := l.broker.ChannelMessages(channel)
			for _, tm := range allMsgs {
				if _, inThread := threadIDs[strings.TrimSpace(tm.ID)]; !inThread {
					continue
				}
				if tm.From != "" && tm.From != "you" && tm.From != "human" && tm.From != "nex" && tm.From != slug {
					activeAgents[tm.From] = struct{}{}
				}
			}
		}
		// Also include specialists who have pending or active headless turns.
		// These agents were notified but may not have posted to the broker yet,
		// causing a timing gap where the broker list misses them.
		l.headlessMu.Lock()
		for workerSlug, queue := range l.headlessQueues {
			if workerSlug == slug {
				continue
			}
			if len(queue) > 0 {
				activeAgents[workerSlug] = struct{}{}
			}
		}
		for workerSlug, active := range l.headlessActive {
			// Skip the lead itself — it should never list itself as "already active".
			if workerSlug == slug {
				continue
			}
			if active != nil {
				activeAgents[workerSlug] = struct{}{}
			}
		}
		l.headlessMu.Unlock()
		if len(activeAgents) > 0 {
			names := make([]string, 0, len(activeAgents))
			for name := range activeAgents {
				names = append(names, "@"+name)
			}
			lines = append(lines, fmt.Sprintf("- Already active in this thread (do NOT re-route): %s", strings.Join(names, ", ")))
		}
	}
	return strings.Join(lines, "\n")
}

func (l *Launcher) buildTaskExecutionPacket(slug string, action officeActionLog, task teamTask, content string) string {
	channel := normalizeChannelSlug(task.Channel)
	if channel == "" {
		channel = "general"
	}
	lines := []string{
		fmt.Sprintf("[Task update from @%s]", action.Actor),
		"Work packet:",
		fmt.Sprintf("- Task: #%s %s", task.ID, truncate(task.Title, 120)),
		fmt.Sprintf("- Status: %s", strings.TrimSpace(task.Status)),
		fmt.Sprintf("- Owner: @%s", slug),
	}
	if details := strings.TrimSpace(task.Details); details != "" {
		lines = append(lines, fmt.Sprintf("- Details: %s", truncate(details, 512)))
	}
	if targets := extractTaskFileTargets(task.Title + " " + task.Details); len(targets) > 0 {
		lines = append(lines, fmt.Sprintf("- Named file targets: %s", strings.Join(targets, ", ")))
	}
	if task.ThreadID != "" {
		lines = append(lines, fmt.Sprintf("- Thread: #%s reply_to %s", channel, task.ThreadID))
	} else {
		lines = append(lines, fmt.Sprintf("- Channel: #%s", channel))
	}
	lines = append(lines, fmt.Sprintf("- Mutation channel: use #%s when claiming or completing #%s", channel, task.ID))
	if path := strings.TrimSpace(task.WorktreePath); path != "" {
		lines = append(lines, fmt.Sprintf("- Working directory: %q", path))
	}
	if strings.EqualFold(strings.TrimSpace(task.ExecutionMode), "local_worktree") {
		lines = append(lines, "Execution rule: this is a local_worktree build task. Work inside the assigned working_directory and default to direct implementation. Do not spend this turn on another repo audit, architecture memo, or nested office launch unless the packet explicitly asks for that.")
		lines = append(lines, "First-turn rule: choose the smallest shippable implementation slice you can finish in this turn and edit files for that slice now. If the overall MVP is broad, narrow it yourself and ship the first runnable sub-piece instead of trying to map the whole system.")
		lines = append(lines, "Time rule: cut scope to something you can plausibly ship in under five minutes of focused work. If the chosen slice still needs broad exploration, cut it down again before you continue.")
		lines = append(lines, "Cut-line rule: if the task description lists multiple outputs or phases, pick exactly one contiguous slice for this turn, then post team_status naming that cut line before you read files. Example cut lines: `config -> idea queue`, `idea queue -> script drafts`, or `script drafts -> publish pack`.")
		if targets := extractTaskFileTargets(task.Title + " " + task.Details); len(targets) > 0 {
			lines = append(lines, "Startup rule: open the named file targets first and use them as your starting point. Do not broaden into repo search unless those exact files are insufficient for the chosen cut line.")
		}
		lines = append(lines, "Audit guardrail: do NOT start with `rg --files`, `find .`, repo-wide README sweeps, or broad file inventories. Read only the handful of files directly tied to this task, then begin editing once you have the first target file.")
		lines = append(lines, "Boundary rule: stay inside the assigned working_directory. Do NOT run `find ..`, `rg ..`, search sibling task worktrees, or inspect parent temp directories like `/var/folders`, `TMPDIR`, or `TemporaryItems`. Those paths are sandbox noise, not repo context.")
		lines = append(lines, "Dirty-tree rule: ignore unrelated modified or untracked files already present in the worktree unless they are directly required for your slice. They may be preexisting repo state, not part of your task.")
		lines = append(lines, "Deliverable rule: a local_worktree feature task is not satisfied by another plan, architecture memo, or audit summary unless the packet explicitly says research-only. Land code, scripts, docs for the runnable slice, or a concrete task-state blocker.")
	}
	if taskRequiresRealExternalExecution(&task) {
		lines = append(lines, "External execution rule: this task names a connected external system and expects a real action there. Use the live integration/workflow path for the smallest allowed step now instead of producing another repo doc, proof marker, or internal package first.")
		lines = append(lines, "Evidence rule: a local markdown file, preview note, repo artifact, or test output does NOT satisfy this task unless the packet explicitly says preview/mock/stub-only. Leave durable external evidence through the broker-integrated workflow/integration path before marking the task review-ready, done, or blocked.")
		lines = append(lines, "Business framing: describe the work as a client deliverable, approval, handoff, update, or record. Avoid proof/marker/test/eval language unless the task explicitly asks for testing or evidence capture.")
		lines = append(lines, "Pace rule: do the smallest safe external step first, then summarize it. Do not spend this turn building extra kickoff decks, review bundles, or substitute proof artifacts if the connected system action is already allowed.")
		lines = append(lines, capabilityGapCoachingBlock())
	}
	if taskLooksLikeLiveBusinessObjective(&task) {
		lines = append(lines, "Task hygiene rule: if this lane drifts into a proof packet, review bundle, blueprint-derived scaffold, rubric, or other internal artifact shell, rewrite it immediately into either the next real deliverable step or the exact capability-enablement task that will unlock that deliverable.")
	}
	// Walk up from task.ThreadID to the ultimate thread root (the original human ask)
	// so agents see the full ancestry, not just a mid-thread branch. The thread root
	// is never excluded (triggerMsgID = "") because the human's ask is the useful
	// anchor, not a duplicate of anything in the packet header.
	threadRoot := l.ultimateThreadRoot(channel, task.ThreadID)
	if ctx := l.buildNotificationContext(channel, "", threadRoot, 3); ctx != "" {
		lines = append(lines, ctx)
	}
	lines = append(lines, fmt.Sprintf("If you deliver the substantive result for #%s in this turn, you MUST call team_task complete or review-ready for \"%s\" before any completion post and before you stop. A channel reply alone does not unblock dependent work, and a completion post without the task mutation is a failure.", task.ID, task.ID))
	lines = append(lines, "Runtime rule: never launch another WUPHF office, copied wuphf binary, browser instance, or local web server/--web-port process from inside this turn. The office is already running; use the existing repo, broker state, and assigned worktree instead.")
	lines = append(lines, fmt.Sprintf("%s Use team_task with my_slug \"%s\" to update status as you go.", truncate(content, 1000), slug))
	return strings.Join(lines, "\n")
}

func headlessSandboxNote() string {
	return "Runtime: this office is already running. Never launch another `wuphf`, copied `wuphf` binary, `/reset`, browser instance, or local server/`--web-port` process from inside your turn. For `execution_mode=local_worktree`, make edits directly in the assigned working_directory instead of re-auditing or trying to boot a second office. Never search parent or sibling temp directories (`find ..`, `rg ..`, `/var/folders`, `TMPDIR`, `TemporaryItems`) from a task worktree; stay inside the assigned working_directory. If shell commands fail with 'operation not permitted' or 'permission denied' (go build cache, localhost bind, sandboxed writes), stop retrying them and continue from code inspection or the existing running office.\n\n"
}

func (l *Launcher) sendChannelUpdate(target notificationTarget, msg channelMessage) {
	channel := normalizeChannelSlug(msg.Channel)
	if channel == "" {
		channel = "general"
	}
	notification := ""
	if l.isOneOnOne() {
		notification = fmt.Sprintf(
			"[New from @%s]: %s\n%s Reply using team_broadcast with my_slug \"%s\" and channel \"%s\" reply_to_id \"%s\". Once you have posted the needed reply, STOP and wait for the next pushed notification.",
			msg.From, truncate(msg.Content, 1000), l.responseInstructionForTarget(msg, target.Slug), target.Slug, channel, msg.ID,
		)
	} else {
		packet := l.buildMessageWorkPacket(msg, target.Slug)
		notification = fmt.Sprintf(
			"%s\n---\n[New from @%s]: %s\n%s This packet is your complete context — do NOT call team_poll or team_tasks. Just do the work and reply via team_broadcast with my_slug \"%s\", channel \"%s\", reply_to_id \"%s\". Once you have posted the needed update, STOP and wait for the next pushed notification.",
			packet, msg.From, truncate(msg.Content, 1000), l.responseInstructionForTarget(msg, target.Slug), target.Slug, channel, msg.ID,
		)
	}

	if l.usesCodexRuntime() || l.webMode {
		l.enqueueHeadlessCodexTurn(target.Slug, headlessSandboxNote()+notification, channel)
		return
	}
	l.sendNotificationToPane(target.PaneTarget, notification)
}

// sendNotificationToPane delivers a notification to a persistent interactive
// Claude session in a tmux pane. It sends /clear first so each turn starts
// with a fresh context window — the work packet carries all required context,
// so accumulated history is not needed and only causes drift over time.
// --append-system-prompt is a CLI flag and survives /clear intact.
func (l *Launcher) sendNotificationToPane(paneTarget, notification string) {
	exec.Command("tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget, "/clear", "Enter",
	).Run()
	exec.Command("tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget, "-l", notification,
	).Run()
	exec.Command("tmux", "-L", tmuxSocketName, "send-keys",
		"-t", paneTarget, "Enter",
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
	return resetBrokerState(brokerBaseURL(), token)
}

func ClearPersistedBrokerState() error {
	path := brokerStatePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func officePIDFilePath() string {
	home := config.RuntimeHomeDir()
	if home == "" {
		return filepath.Join(".wuphf", "team", "office.pid")
	}
	return filepath.Join(home, ".wuphf", "team", "office.pid")
}

func writeOfficePIDFile() error {
	path := officePIDFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func clearOfficePIDFile() error {
	path := officePIDFilePath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func killPersistedOfficeProcess() error {
	raw, err := os.ReadFile(officePIDFilePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 0 {
		_ = clearOfficePIDFile()
		return nil
	}
	if pid == os.Getpid() {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = clearOfficePIDFile()
		return nil
	}
	_ = proc.Kill()
	return nil
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
		if isMissingTmuxSession(string(out)) {
			return nil, nil
		}
		return nil, fmt.Errorf("list panes: %w", err)
	}
	return parseAgentPaneIndices(string(out)), nil
}

// HasLiveTmuxSession returns true if a wuphf-team tmux session is running.
func HasLiveTmuxSession() bool {
	err := exec.Command("tmux", "-L", tmuxSocketName, "has-session", "-t", SessionName).Run()
	return err == nil
}

func isMissingTmuxSession(output string) bool {
	normalized := strings.ToLower(strings.TrimSpace(output))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "no server") ||
		strings.Contains(normalized, "can't find") ||
		strings.Contains(normalized, "failed to connect to server") ||
		strings.Contains(normalized, "error connecting to") ||
		strings.Contains(normalized, "no such file or directory")
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
	officeMembers := l.officeMembersSnapshot()
	lead := officeLeadSlugFrom(officeMembers)
	noNex := config.ResolveNoNex() || config.ResolveAPIKey("") == ""
	var activePolicies []officePolicy
	if l.broker != nil {
		activePolicies = l.broker.ListPolicies()
	}

	var sb strings.Builder

	companyCtx := config.CompanyContextBlock()

	if l.isOneOnOne() {
		sb.WriteString(fmt.Sprintf("You are %s in a direct one-on-one WUPHF session with the human.\n\n", agentCfg.Name))
		sb.WriteString(companyCtx)
		sb.WriteString(fmt.Sprintf("Your expertise: %s\n\n", strings.Join(agentCfg.Expertise, ", ")))
		sb.WriteString(fmt.Sprintf("Core personality: %s\n", agentCfg.Personality))
		sb.WriteString(fmt.Sprintf("Voice and vibe: %s\n\n", teamVoiceForSlug(slug)))
		sb.WriteString("== DIRECT SESSION ==\n")
		sb.WriteString("This is not the shared office. There are no teammates, no channels, and no collaboration mechanics in this mode.\n")
		sb.WriteString("You are only talking to the human.\n")
		sb.WriteString("- team_poll: LAST RESORT — read recent 1:1 messages only if the pushed notification is missing context you genuinely need. Do NOT call this by default.\n")
		sb.WriteString("- team_broadcast: Send a normal direct chat reply into the 1:1 conversation\n")
		sb.WriteString("- human_message: Send an emphasized report, recommendation, or action card directly to the human when you want it to stand out\n")
		sb.WriteString("- human_interview: Ask a blocking decision question only when you truly cannot proceed responsibly without it\n\n")
		if noNex {
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
		sb.WriteString("4. The pushed notification IS the latest state. Respond directly from it. Do NOT poll before replying.\n")
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
		sb.WriteString(companyCtx)
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
		sb.WriteString("Your tools default to the active conversation context.\n")
		sb.WriteString("- team_broadcast: Post to channel. CRITICAL: text @-mentions alone do NOT wake agents — include the slug in the `tagged` parameter.\n")
		sb.WriteString("- team_poll: LAST RESORT — read recent messages only when pushed context is genuinely missing something you need. Do NOT call this by default; the pushed notification already contains thread context, task state, and active agents.\n")
		sb.WriteString("- team_bridge: Carry context from one channel into another (CEO only).\n")
		sb.WriteString("- team_task: Create and assign tasks so ownership is explicit.\n")
		sb.WriteString("- team_skill_run: Invoke a saved skill by name when the request matches one. Use this BEFORE routing or replying — it returns the canonical playbook content to follow and logs a visible skill_invocation in the channel.\n")
		sb.WriteString("- team_action_connections / team_action_search / team_action_knowledge: inspect connected external systems and the exact action/workflow schema before you improvise. If connection listing is flaky, do NOT stop there; search/knowledge still give you the real action contract.\n")
		sb.WriteString("- team_action_execute / team_action_workflow_execute: use these for real external reads, writes, and workflow runs. Prefer dry_run only when the task or policy says preview/mock first. When the provider is One and there is exactly one connected account for that platform, you may omit connection_key and let the runtime auto-resolve it.\n")
		sb.WriteString("- human_message: Present output or a recommendation directly to the human.\n")
		sb.WriteString("- human_interview: Ask the human a blocking decision question — only when the team cannot proceed without it.\n")
		sb.WriteString("Other tools: team_tasks, team_task_status, team_requests, team_request, team_status, team_members, team_office_members, team_channels, team_channel, team_member, team_channel_member, team_action_guide, team_action_workflow_create, team_action_workflow_schedule, team_action_relays, team_action_relay_event_types, team_action_relay_create, team_action_relay_activate, team_action_relay_events, team_action_relay_event.\n\n")
		if noNex {
			sb.WriteString("Nex tools are disabled for this run. Work only with the shared office channel and human answers.\n\n")
		} else {
			sb.WriteString("Nex memory: query_context before reinventing; add_context only after a decision is actually landed.\n\n")
		}
		if len(activePolicies) > 0 {
			sb.WriteString("== ACTIVE OFFICE POLICIES ==\n")
			sb.WriteString("Treat these as hard operating constraints, not suggestions. If a policy conflicts with an older chat assumption, the active policy wins until the human changes it.\n")
			for _, policy := range activePolicies {
				sb.WriteString(fmt.Sprintf("- %s\n", policy.Rule))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("Tagged agents are expected to respond.\n\n")
		if l.isFocusModeEnabled() {
			sb.WriteString("== DELEGATION MODE ==\n")
			sb.WriteString("You are the routing hub. Specialists only act when you or the human explicitly @tag them.\n")
			sb.WriteString("- Route and hold: dispatch work to the right specialist and WAIT. Never do their work while they are working.\n")
			sb.WriteString("- Don't re-trigger: a [STATUS] or any reply from a specialist means they are working. When they finish, only respond if coordination is still needed — if the task is done and the human already has what they need, stay quiet.\n")
			sb.WriteString("- Specialists report up, not sideways: keep them out of cross-agent chatter. Coordinate through you.\n")
			sb.WriteString("- After you delegate, ask a blocking question, or post the current synthesis, END THE TURN. Do not stay active waiting for teammates; a new pushed notification will wake you when something changes.\n\n")
		}
		sb.WriteString("THREADING: Default to replying in the active thread. If you intentionally cross into another channel or start a new topic, pass channel or new_topic explicitly.\n\n")
		sb.WriteString("YOUR ROLE AS LEADER:\n")
		if noNex {
			sb.WriteString("1. Coordinate inside the office channel first and keep the team aligned there\n")
		} else {
			sb.WriteString("1. On strategy or prior decisions, call query_context early\n")
		}
		sb.WriteString("2. The pushed notification is authoritative — it contains thread context, task state, and agent activity. Respond directly from it. Do NOT call team_poll or team_tasks unless the notification explicitly says context is missing. Every unnecessary tool call burns tokens without adding value.\n")
		sb.WriteString("3. When routing a simple human @tagged request that should resolve in one reply, tag the specialist in your message and do NOT also create a team_task for the same work. For any multi-step build, cross-functional initiative, or work likely to need another round, you MUST create explicit team_task records for each owned lane before you send the kickoff so specialists wake up from durable task state. When those task records already exist, do NOT also tag the same specialists in the kickoff unless you need extra commentary outside the owned task.\n")
		sb.WriteString("4. Tag only the specialists who should weigh in. Unowned background chatter is a bug.\n")
		sb.WriteString("5. Keep specialists in their lane and mostly offstage. You make the FINAL decision.\n")
		sb.WriteString("6. Check team_requests before asking the human anything new\n")
		sb.WriteString("7. Use human_message for direct human-facing output, human_interview for blocking decisions\n")
		if noNex {
			sb.WriteString("8. Summarize final decisions clearly in-channel\n")
		} else {
			sb.WriteString("8. When you lock a decision, call add_context before claiming it is stored\n")
		}
		sb.WriteString("9. Once decided, create durable task state first, then broadcast the kickoff and assignments. If you already know multiple owned lanes, prefer one team_plan call over several separate team_task creates. Every created task should set `task_type` and `execution_mode` deliberately instead of relying on inference.\n")
		sb.WriteString("10. Choose task_type deliberately. Use `research` for audits/analysis, `launch` for GTM/rollout packages, `follow_up` for scoped office deliverables, and `feature` only for real implementation work. Do NOT label planning or audit work as `feature` just because it matters.\n")
		sb.WriteString("11. If the human asks to build, ship, get something working, or run it end to end, your task graph MUST include at least one real execution lane that changes the repo or produces runnable business artifacts. A graph made only of planning, design, recommendations, or implementation-sequence tasks is a failure.\n")
		sb.WriteString("12. Do not create engineering tasks whose only deliverable is another plan (`propose the implementation sequence`, `design the architecture`, `outline the automation`) when the repo is available now. For build/ship/end-to-end requests, do NOT put a standalone `research`, `audit`, or `cut line` task in front of the first engineering `feature` task just to decide what to build. Any minimal repo inspection belongs inside that first feature task. Only create a prerequisite research task when the human explicitly asked for analysis first or the repo truly has no identifiable implementation target.\n")
		sb.WriteString("12b. For build/ship/end-to-end requests, do NOT spend the whole first turn on `pwd`, `ls`, `rg --files`, `find .`, or a repo-wide file inventory. If the packet or thread already names relevant docs, configs, or lane files, start from those. After at most one or two targeted reads, create the first durable task/channel state in that same turn.\n")
		sb.WriteString("13. For broad engineering goals, do NOT create a first feature task with a giant title like `ship the whole MVP`, `ship the first channel-factory MVP slice`, or any other umbrella task with no cut line. The first feature task must name one smallest runnable slice only. Do not bundle idea generation, script drafting, packaging, and monetization hooks into the same first task; pick one contiguous slice, let it land, then queue the next slice. If existing docs, configs, or launch packets already name a concrete slice, use them and create the implementation task directly instead of narrating `repo audit first, implementation next`.\n")
		sb.WriteString("14. If you write any narrative like `next move`, `next step`, `operating order`, or `eng should` / `gtm should`, that same turn MUST also create the concrete owned task record(s) for that work before you stop. Narrative next steps without durable tasks are a failure.\n")
		sb.WriteString("14b. On a human build/ship request, the first turn must leave durable office state behind: at minimum the kickoff plus the first owned task, and for cross-functional work usually the execution channel too. A whole turn spent only reading docs or thinking is a failure.\n")
		sb.WriteString("15. When first-pass specialist outputs land and obvious downstream work remains, create the next owned task before you end the turn. Do not stop at synthesis if the build still has a clear next step.\n")
		sb.WriteString("15b. Before you create a new task, inspect the Active tasks in the packet. If an open, in-progress, or review task already covers that lane, reuse or update that task instead of creating an overlapping duplicate with a fresh title.\n")
		sb.WriteString("16. If a task lands in review but no human approval is actually needed, approve it or immediately translate it into the next task. Do not leave the company idle behind an internal review gate.\n")
		sb.WriteString("16b. Before you write any sentence claiming a task is approved, closed, reopened, reassigned, or blocked, you MUST make the matching team_task or team_plan call first. Channel narration does not mutate durable task state. If the mutation fails, say it failed and do not claim success.\n")
		sb.WriteString("16c. On a human build/ship/end-to-end request, after you approve or close any engineering/execution slice, if the system is not yet runnable end to end and no engineering/execution lane remains active, create the next engineering/execution task in that same turn before you stop. Do not replace the only live build lane with GTM-only packaging, eval prompts, scoring rubrics, or other sidecar work.\n")
		sb.WriteString("16d. When a task or policy allows a low-risk external step on a connected system, prefer the smallest real external action now over more internal collateral. A Slack/Notion/Drive lane is not satisfied by repo markdown, preview notes, proof markers, or substitute proof artifacts unless the task explicitly says mock/preview/stub-only.\n")
		sb.WriteString("16e. When the work is live, describe outputs as client deliverables, approvals, handoffs, updates, or records. Do not frame live business work as proof/test/eval artifacts unless the task explicitly asks for testing or evidence capture.\n")
		sb.WriteString("16f. Capability-gap rule: if the work is blocked because the needed specialist, channel, skill, or tool path does not exist yet, treat that gap as the next real work item. Do not fall back to a review bundle, proof packet, artifact shell, or local substitute deliverable. Create the missing specialist with team_member first; if the work will span more than one turn, create the missing execution channel with team_channel; propose or update the missing skill block in the same turn; and if the blocker is a tool or provider gap, open a tool-discovery/research lane named for the exact tool you need so the office can discover, validate, and enable it. Example: if the work needs video generation and you do not already have a usable path, create a discovery lane for Remotion or the exact video tool before drafting any deliverable shell.\n")
		sb.WriteString("16g. Task hygiene rule: if a live business lane gets named or reframed as a review packet, proof artifact, blueprint-derived scaffold, rubric, or other internal shell, rewrite that lane in the same turn. Replace it with either the next real deliverable/customer-facing/business-facing step or the exact capability-enablement task that unblocks that step.\n")
		sb.WriteString("17. Create channels (team_channel) or agents (team_member) when the human asks or scope genuinely warrants it. For cross-functional initiatives that will run beyond one decision cycle, create a dedicated execution channel and keep #general for top-level decisions.\n")
		sb.WriteString("17b. When the human explicitly asks to add or test integrations, generated skills, reusable workflows, or generated agents, you MUST leave durable state for that work in the same turn. Create the integration/onboarding task lane(s), propose or update the relevant skill block(s), and create any needed specialist agent(s) instead of only describing them narratively. If real accounts, credentials, spend, publishing, or other external side effects would be required, proceed with stubs/placeholders until the exact human approval is truly needed.\n")
		sb.WriteString("18. Sequence structural changes safely: create a new specialist with team_member first, wait for success, then add them to channels or tag them. When creating a new channel, only include members that already exist.\n")
		sb.WriteString("19. For `team_channel` create/remove calls, set `channel` to the explicit target slug like `youtube-factory`; it is not inferred from the current room.\n")
		sb.WriteString("20. Use team_bridge to carry context between channels when relevant\n")
		sb.WriteString("21. If a task shows a worktree path, that path is the working_directory for local file and bash tools on that task\n")
		sb.WriteString("22. After you have posted the needed update, decision, delegation, or human question for the current packet, stop. Do not linger in the same turn waiting for teammates to answer.\n\n")
		sb.WriteString("== SKILL & AGENT AWARENESS ==\n")
		sb.WriteString("When a request matches an existing skill (by name, trigger, or tags), you MUST invoke it via team_skill_run(skill_name) BEFORE doing the work. That tool bumps usage, logs a skill_invocation in the channel, and returns the skill's canonical content — follow those steps exactly, don't freelance.\n")
		sb.WriteString("When delegating to a specialist, tell them which skill to run (by slug) so they call team_skill_run before acting. Never paraphrase a skill's steps into a delegation message — the skill IS the spec.\n")
		sb.WriteString("You can propose new skills when you notice a repeated workflow worth codifying.\n")
		sb.WriteString("Format a proposal inline in your message using:\n")
		sb.WriteString("[SKILL PROPOSAL]\nName: <slug-name>\nTitle: <Short human title>\nDescription: <one-line description>\nTrigger: <when to invoke>\nTags: <tag1, tag2>\n---\n<step-by-step instructions>\n[/SKILL PROPOSAL]\n\n")
		sb.WriteString("Rules:\n")
		sb.WriteString("- Propose when you see a pattern repeated 2+ times by the team, or when the human explicitly asked for reusable workflow automation\n")
		sb.WriteString("- If the human explicitly asked for generated skills or reusable workflows, at least one concrete proposal or update must land in the same turn before you stop; narrative mentions alone are a failure\n")
		sb.WriteString("- If a recurring workflow is central to the project's operation, propose it instead of re-explaining it every round\n")
		sb.WriteString("- Keep instructions concrete and executable, not vague\n")
		sb.WriteString("- The human will be asked to approve before it becomes active\n")
		sb.WriteString("- To suggest adding a new specialist agent, use team_member with a clear expertise and rationale\n")
		sb.WriteString("- When integrations matter, make the required systems explicit in the skill instructions and agent rationale so the team knows which connected accounts or placeholders each workflow expects\n")
		sb.WriteString("- When you create a new specialist for integration/onboarding work, include the owned integrations directly in that agent's expertise so the roster clearly shows who owns Gmail, Slack, YouTube, Drive, analytics, or similar lanes\n\n")
		sb.WriteString("STYLE: Be concise, delegate, short lively messages. Use markdown tables/checklists for structured data.\n")
		if noNex {
			sb.WriteString("Do not claim you stored anything outside the office.\n")
		} else {
			sb.WriteString("Do not pretend the graph was updated; verify add_context succeeded.\n")
		}
		sb.WriteString("Never launch another WUPHF office from inside your turn (`wuphf`, `./wuphf`, `/reset`, or a new browser instance). The office is already running; inspect the current repo and UI instead.\n")
	} else {
		sb.WriteString(fmt.Sprintf("You are %s on the %s.\n", agentCfg.Name, l.PackName()))
		sb.WriteString(companyCtx)
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
		sb.WriteString("Your tools default to the active conversation context.\n")
		sb.WriteString("- team_broadcast: Post to channel. CRITICAL: text @-mentions alone do NOT wake agents — include the slug in the `tagged` parameter.\n")
		sb.WriteString("- team_poll: LAST RESORT — read recent messages only when pushed context is genuinely missing something you need. Do NOT call this by default; the pushed notification already contains thread context and task state.\n")
		sb.WriteString("- team_bridge: CEO-only bridge for cross-channel context. Ask the CEO to use it.\n")
		sb.WriteString("- team_task: Claim, complete, block, or release tasks in your domain.\n")
		sb.WriteString("- team_skill_run: When @ceo tells you to run a skill, or when the request clearly matches one, call team_skill_run(skill_name) BEFORE doing the work. It returns the canonical step-by-step content — follow it exactly instead of freelancing. Failing to invoke the skill leaves the office with no trace that the playbook was actually used.\n")
		sb.WriteString("- team_action_connections / team_action_search / team_action_knowledge: inspect connected external systems and the exact action/workflow schema before you improvise. If connection listing is flaky, do NOT stop there; search/knowledge still give you the real action contract.\n")
		sb.WriteString("- team_action_execute / team_action_workflow_execute: use these for real external reads, writes, and workflow runs. Prefer dry_run only when the task or policy says preview/mock first. When the provider is One and there is exactly one connected account for that platform, you may omit connection_key and let the runtime auto-resolve it.\n")
		sb.WriteString("- human_message: Present completion or a recommendation directly to the human.\n")
		sb.WriteString("- human_interview: Ask the human only for blocking clarifications you cannot responsibly guess.\n")
		sb.WriteString("Other tools: team_tasks, team_task_status, team_requests, team_request, team_status, team_members, team_office_members, team_channels, team_channel, team_member, team_channel_member, team_action_guide, team_action_workflow_create, team_action_workflow_schedule, team_action_relays, team_action_relay_event_types, team_action_relay_create, team_action_relay_activate, team_action_relay_events, team_action_relay_event.\n\n")
		if noNex {
			sb.WriteString("Nex tools are disabled for this run. Base your work on the office conversation and direct human answers only.\n\n")
		} else {
			sb.WriteString("Nex memory: query_context before making assumptions; add_context only for durable conclusions.\n\n")
		}
		if len(activePolicies) > 0 {
			sb.WriteString("== ACTIVE OFFICE POLICIES ==\n")
			sb.WriteString("Treat these as hard operating constraints, not suggestions. If a policy conflicts with an older chat assumption, the active policy wins until the human changes it.\n")
			for _, policy := range activePolicies {
				sb.WriteString(fmt.Sprintf("- %s\n", policy.Rule))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("Tag agents with @slug. Tagged agents must respond.\n")
		if l.isFocusModeEnabled() {
			sb.WriteString("== DELEGATION MODE ==\n")
			sb.WriteString("Delegation mode is enabled.\n")
			sb.WriteString("- You take work directly from the human only when they explicitly tag you, or from @ceo when delegated.\n")
			sb.WriteString("- Do not debate with other specialists in the channel.\n")
			sb.WriteString("- Do the work, then report completion, blockers, or handoff notes back to @ceo.\n")
			sb.WriteString("- If another specialist should get involved, tell @ceo instead of routing it yourself.\n")
			sb.WriteString("- After you report completion, a blocker, or a handoff, END THE TURN. Do not keep researching or wait for acknowledgements in the same run.\n\n")
		}
		sb.WriteString("THREADING: Default to replying in the active thread. If you intentionally cross into another channel or start a new topic, pass channel or new_topic explicitly.\n\n")
		sb.WriteString("YOUR ROLE AS SPECIALIST:\n")
		sb.WriteString("1. The pushed notification is authoritative — it contains thread context and task state. Respond directly from it. Do NOT call team_poll or team_tasks unless context is genuinely missing. Every unnecessary tool call burns tokens without adding value. Just do the work.\n")
		sb.WriteString("2. Stay in your lane. Speak only when tagged, owning a task, blocked, or adding real delta that others haven't covered. Don't jump in just because a topic matches your domain.\n")
		sb.WriteString("3. Push back when you disagree — explain why using your expertise\n")
		sb.WriteString("4. Check team_requests before asking the human anything new\n")
		sb.WriteString("5. For completion or recommendations, use human_message. For blocking human decisions, use human_interview with options.\n")
		sb.WriteString("6. When assigned a task, claim it with team_task first, use team_status to show what you're working on, then mark complete or review-ready and broadcast when done. Final sequence for owned tasks: team_task mutation first, then any completion broadcast or human_message, then stop. A task is NOT finished until team_task marks it complete or review-ready; posting a channel reply alone does not unblock downstream work, and a completion post while the task stays in_progress is a failure. If the CEO delegates a substantial workstream and the packet shows no owned task yet, do one quick team_tasks check before creating a fallback task; if a matching task already exists, claim that instead of duplicating it. Only create a fallback task when the delegated work is substantial and no matching task exists after that single check. If the result is mainly for the human, also send it via human_message.\n")
		sb.WriteString("7. You can see other channel names and descriptions, but cannot access their content unless you are a member. If context from another channel is needed, ask the CEO to bridge it.\n")
		sb.WriteString("8. If a task or status line shows a worktree path, use that as working_directory for local file and bash tools.\n")
		sb.WriteString("9. For local_worktree or feature tasks, default to direct implementation in the assigned worktree. Do not relaunch WUPHF, copied binaries, or a fresh local server just to inspect the app; use the current repo and running office instead.\n")
		sb.WriteString("10. For local_worktree feature tasks, do NOT start with `rg --files`, `find .`, or a repo-wide audit. Read only the few files directly tied to the requested slice, then start editing. If the task is broad or lists multiple outputs, narrow it yourself to one exact smallest runnable slice, post a `team_status` naming that cut line, and ship that slice now.\n")
		sb.WriteString("10b. Never search parent or sibling directories outside the assigned working_directory (`find ..`, `rg ..`, `/var/folders`, `TMPDIR`, `TemporaryItems`, or other task worktrees). If you need instructions, read `AGENTS.md` or `README.md` inside the assigned worktree only.\n")
		sb.WriteString("11. Ignore unrelated modified or untracked files already present in the assigned worktree unless they are directly needed for your slice. They may be preexisting repo state; do not audit or re-explain them.\n")
		sb.WriteString("11b. If a task names a connected external system and asks you to create, post, query, or run something there, do that live external step through the connected workflow/integration path. Repo docs, previews, local markdown, proof markers, or test artifacts do not count unless the task explicitly says mock/preview/stub-only.\n")
		sb.WriteString("11c. When the work is live, phrase it as a client deliverable, approval, handoff, update, or record. Avoid proof/test/marker/eval language unless the task explicitly asks for testing or evidence capture.\n")
		sb.WriteString("11d. When a task calls for Slack, Notion, Drive, or another connected system, use the `team_action_*` tools first. Do NOT probe localhost broker routes, curl the provider directly, or fall back to shell-side API experiments when the office action tools can do the job.\n")
		sb.WriteString("11e. Capability-gap rule: if the work is blocked because the needed specialist, channel, skill, or tool path does not exist yet, treat that gap as the next real work item. Do not fall back to a review bundle, proof packet, artifact shell, or local substitute deliverable. Create the missing specialist with team_member first; if the work will span more than one turn, create the missing execution channel with team_channel; propose or update the missing skill block in the same turn; and if the blocker is a tool or provider gap, open a tool-discovery/research lane named for the exact tool you need so the office can discover, validate, and enable it. Example: if the work needs video generation and you do not already have a usable path, create a discovery lane for Remotion or the exact video tool before drafting any deliverable shell.\n")
		sb.WriteString("11f. Task hygiene rule: if a live business lane gets named or reframed as a review packet, proof artifact, blueprint-derived scaffold, rubric, or other internal shell, rewrite that lane in the same turn. Replace it with either the next real deliverable/customer-facing/business-facing step or the exact capability-enablement task that unblocks that step.\n")
		if noNex {
			sb.WriteString("12. Don't fake outside memory. Surface uncertainty in-channel and keep outcomes explicit in-thread.\n")
			sb.WriteString("13. Once you have posted the needed update for the current packet, stop. A later pushed notification will wake you again if more is needed.\n\n")
		} else {
			sb.WriteString("12. Use query_context when prior knowledge matters. Only use add_context for durable conclusions, and don't claim something stored unless add_context actually succeeded.\n")
			sb.WriteString("13. Once you have posted the needed update for the current packet, stop. A later pushed notification will wake you again if more is needed.\n\n")
		}
		sb.WriteString("STYLE: Be concise, stay in lane, short lively messages. Use markdown tables/checklists for structured data.\n")
		sb.WriteString("Never launch another WUPHF office from inside your turn (`wuphf`, `./wuphf`, `/reset`, or a new browser instance). The office is already running; inspect the current repo and UI instead.\n")
	}

	return sb.String()
}

// claudeCommand builds the shell command string for spawning a claude session.
// Sets WUPHF_AGENT_SLUG so the MCP knows which agent this session serves.
func (l *Launcher) claudeCommand(slug, systemPrompt string) string {
	escaped := strings.ReplaceAll(systemPrompt, "'", "'\\''")
	agentMCP := l.mcpConfig
	if path, err := l.ensureAgentMCPConfig(slug); err == nil {
		agentMCP = path
	}
	mcpConfig := strings.ReplaceAll(agentMCP, "'", "'\\''")
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

	model := l.headlessClaudeModel(slug)

	return fmt.Sprintf(
		"%s%s%sWUPHF_AGENT_SLUG=%s WUPHF_BROKER_TOKEN=%s WUPHF_BROKER_BASE_URL=%s WUPHF_NO_NEX=%t ANTHROPIC_PROMPT_CACHING=1 CLAUDE_CODE_ENABLE_TELEMETRY=1 OTEL_METRICS_EXPORTER=none OTEL_LOGS_EXPORTER=otlp OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/json OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=%s/v1/logs OTEL_EXPORTER_OTLP_HEADERS='Authorization=Bearer %s' OTEL_RESOURCE_ATTRIBUTES='agent.slug=%s,wuphf.channel=office' claude --model %s %s --append-system-prompt '%s' --mcp-config '%s' --strict-mcp-config -n '%s'",
		oneOnOneEnv,
		oneSecretEnv,
		oneIdentityEnv,
		slug,
		brokerToken,
		l.BrokerBaseURL(),
		config.ResolveNoNex(),
		l.BrokerBaseURL(),
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

// codingAgentSlugs lists agents that default to a minimal coding-focused MCP set.
// Task-level local_worktree isolation is driven by execution_mode, not this list.
var codingAgentSlugs = map[string]bool{
	"eng":       true,
	"fe":        true,
	"be":        true,
	"ai":        true,
	"qa":        true,
	"tech-lead": true,
}

// agentMCPServers returns the MCP server keys that a given agent should receive.
func agentMCPServers(slug string) []string {
	channel := strings.TrimSpace(os.Getenv("WUPHF_CHANNEL"))
	// DM mode: only wuphf-office (minimal tool set, no nex overhead)
	if strings.HasPrefix(channel, "dm-") {
		return []string{"wuphf-office"}
	}
	if codingAgentSlugs[slug] {
		return []string{"wuphf-office"}
	}
	return []string{"wuphf-office", "nex"}
}

// buildMCPServerMap constructs the full set of MCP server entries.
// This is the shared helper used by both ensureMCPConfig and ensureAgentMCPConfig.
func (l *Launcher) buildMCPServerMap() (map[string]any, error) {
	apiKey := config.ResolveAPIKey("")
	servers := map[string]any{}
	wuphfBinary, err := os.Executable()
	if err != nil {
		return nil, err
	}

	office := map[string]any{
		"command": wuphfBinary,
		"args":    []string{"mcp-team"},
	}
	servers["wuphf-office"] = office
	if oneSecret := strings.TrimSpace(config.ResolveOneSecret()); oneSecret != "" {
		office["env"] = map[string]string{
			"ONE_SECRET": oneSecret,
		}
	}
	if identity := strings.TrimSpace(config.ResolveOneIdentity()); identity != "" {
		env, _ := office["env"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		env["ONE_IDENTITY"] = identity
		if identityType := strings.TrimSpace(config.ResolveOneIdentityType()); identityType != "" {
			env["ONE_IDENTITY_TYPE"] = identityType
		}
		office["env"] = env
	}

	switch config.ResolveMemoryBackend("") {
	case config.MemoryBackendNex:
		if apiKey != "" {
			env, _ := office["env"].(map[string]string)
			if env == nil {
				env = map[string]string{}
			}
			env["WUPHF_API_KEY"] = apiKey
			env["NEX_API_KEY"] = apiKey
			office["env"] = env
		}
	case config.MemoryBackendGBrain:
		env, _ := office["env"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		for key, value := range gbrainMCPEnv() {
			env[key] = value
		}
		office["env"] = env
	}

	if memoryServer, err := resolvedMemoryMCPServer(); err != nil {
		return nil, err
	} else if memoryServer != nil && len(memoryServer.Env) > 0 {
		env, _ := office["env"].(map[string]string)
		if env == nil {
			env = map[string]string{}
		}
		for key, value := range memoryServer.Env {
			env[key] = value
		}
		office["env"] = env
	}

	if !config.ResolveNoNex() && apiKey != "" {
		if nexMCP, err := exec.LookPath("nex-mcp"); err == nil {
			servers["nex"] = map[string]any{
				"command": nexMCP,
				"env": map[string]string{
					"WUPHF_API_KEY": apiKey,
					"NEX_API_KEY":   apiKey,
				},
			}
		}
	}

	return servers, nil
}

func (l *Launcher) ensureMCPConfig() (string, error) {
	servers, err := l.buildMCPServerMap()
	if err != nil {
		return "", err
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

// ensureAgentMCPConfig writes a per-agent MCP config containing only the servers
// that agent needs. Returns the config file path.
func (l *Launcher) ensureAgentMCPConfig(slug string) (string, error) {
	allServers, err := l.buildMCPServerMap()
	if err != nil {
		return "", err
	}

	allowed := agentMCPServers(slug)
	filtered := make(map[string]any, len(allowed))
	for _, key := range allowed {
		if srv, ok := allServers[key]; ok {
			filtered[key] = srv
		}
	}

	cfg := map[string]any{
		"mcpServers": filtered,
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}

	path := filepath.Join(os.TempDir(), "wuphf-mcp-"+slug+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// agentActiveTask returns the first in_progress task owned by the given agent slug.
// AllTasks() is used so agents working in non-general channels still get their
// worktree set up correctly.
func (l *Launcher) agentActiveTask(slug string) *teamTask {
	if l.broker == nil {
		return nil
	}
	tasks := l.broker.AllTasks()
	for i := range tasks {
		if tasks[i].Owner == slug && tasks[i].Status == "in_progress" {
			return &tasks[i]
		}
	}
	return nil
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
	mergePackMembers := func(members []officeMember) []officeMember {
		if l == nil || l.pack == nil || len(l.pack.Agents) == 0 {
			return members
		}
		bySlug := make(map[string]struct{}, len(members))
		for _, member := range members {
			bySlug[member.Slug] = struct{}{}
		}
		for _, cfg := range l.pack.Agents {
			if _, ok := bySlug[cfg.Slug]; ok {
				continue
			}
			member := officeMember{
				Slug:           cfg.Slug,
				Name:           cfg.Name,
				Role:           cfg.Name,
				Expertise:      append([]string(nil), cfg.Expertise...),
				Personality:    cfg.Personality,
				PermissionMode: cfg.PermissionMode,
				AllowedTools:   append([]string(nil), cfg.AllowedTools...),
				BuiltIn:        cfg.Slug == l.pack.LeadSlug || cfg.Slug == "ceo",
			}
			applyOfficeMemberDefaults(&member)
			members = append(members, member)
		}
		return members
	}
	if l.broker != nil {
		if members := l.broker.OfficeMembers(); len(members) > 0 {
			return mergePackMembers(members)
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
				Slug:           cfg.Slug,
				Name:           cfg.Name,
				Role:           cfg.Name,
				Expertise:      append([]string(nil), cfg.Expertise...),
				Personality:    cfg.Personality,
				PermissionMode: cfg.PermissionMode,
				AllowedTools:   append([]string(nil), cfg.AllowedTools...),
				BuiltIn:        cfg.Slug == l.pack.LeadSlug || cfg.Slug == "ceo",
			}
			applyOfficeMemberDefaults(&member)
			members = append(members, member)
		}
		return mergePackMembers(members)
	}
	if manifest, err := company.LoadRuntimeManifest(resolveRepoRoot(l.cwd)); err == nil && len(manifest.Members) > 0 {
		members := make([]officeMember, 0, len(manifest.Members))
		for _, cfg := range manifest.Members {
			member := officeMember{
				Slug:           cfg.Slug,
				Name:           cfg.Name,
				Role:           cfg.Role,
				Expertise:      append([]string(nil), cfg.Expertise...),
				Personality:    cfg.Personality,
				PermissionMode: cfg.PermissionMode,
				AllowedTools:   append([]string(nil), cfg.AllowedTools...),
				BuiltIn:        cfg.System,
			}
			applyOfficeMemberDefaults(&member)
			members = append(members, member)
		}
		return mergePackMembers(members)
	}
	return mergePackMembers(defaultOfficeMembers())
}

// resetManifestToPack overwrites company.json with the members defined in the
// given legacy pack. Called when the user passes --pack explicitly so the flag
// remains authoritative over any previously saved company configuration.
func resetManifestToPack(pack *agent.PackDefinition) error {
	members := make([]company.MemberSpec, 0, len(pack.Agents))
	for _, cfg := range pack.Agents {
		members = append(members, company.MemberSpec{
			Slug:           cfg.Slug,
			Name:           cfg.Name,
			Role:           cfg.Name,
			Expertise:      append([]string(nil), cfg.Expertise...),
			Personality:    cfg.Personality,
			PermissionMode: cfg.PermissionMode,
			AllowedTools:   append([]string(nil), cfg.AllowedTools...),
			System:         cfg.Slug == pack.LeadSlug || cfg.Slug == "ceo",
		})
	}
	manifest := company.Manifest{
		Name:    pack.Name,
		Lead:    pack.LeadSlug,
		Members: members,
	}
	return company.SaveManifest(manifest)
}

func resetManifestToOperationBlueprint(repoRoot, blueprintID string) error {
	manifest := company.Manifest{
		BlueprintRefs: []company.BlueprintRef{{
			Kind:   "operation",
			ID:     blueprintID,
			Source: "launcher",
		}},
	}
	resolved, ok := company.MaterializeManifest(manifest, repoRoot)
	if !ok {
		return fmt.Errorf("materialize operation blueprint %q", blueprintID)
	}
	return company.SaveManifest(resolved)
}

func resolveRepoRoot(start string) string {
	start = strings.TrimSpace(start)
	if start == "" {
		start = "."
	}
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
		if _, err := os.Stat(filepath.Join(current, "templates")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return start
		}
		current = parent
	}
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

func (l *Launcher) isFocusModeEnabled() bool {
	if l != nil && l.broker != nil {
		return l.broker.FocusModeEnabled()
	}
	if l == nil {
		return false
	}
	return l.focusMode
}

func brokerBaseURL() string {
	return brokeraddr.ResolveBaseURL()
}

func (l *Launcher) BrokerBaseURL() string {
	if l != nil && l.broker != nil {
		if addr := strings.TrimSpace(l.broker.Addr()); addr != "" {
			return "http://" + addr
		}
	}
	return brokerBaseURL()
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
	if l.pack != nil && strings.TrimSpace(l.pack.LeadSlug) != "" {
		return l.pack.LeadSlug
	}
	return officeLeadSlugFrom(l.activeSessionMembers())
}

// officeLeadSlugFrom derives the lead slug from an already-loaded member
// snapshot, avoiding a redundant officeMembersSnapshot call.
func officeLeadSlugFrom(members []officeMember) string {
	for _, member := range members {
		if member.Slug == "ceo" {
			return "ceo"
		}
	}
	for _, member := range members {
		if member.BuiltIn {
			return member.Slug
		}
	}
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

func (l *Launcher) activeSessionMembers() []officeMember {
	members := l.officeMembersSnapshot()
	if l == nil || l.pack == nil || len(l.pack.Agents) == 0 {
		return members
	}
	bySlug := make(map[string]officeMember, len(members))
	for _, member := range members {
		bySlug[member.Slug] = member
	}
	filtered := make([]officeMember, 0, len(l.pack.Agents))
	for _, cfg := range l.pack.Agents {
		if member, ok := bySlug[cfg.Slug]; ok {
			filtered = append(filtered, member)
			continue
		}
		member := officeMember{
			Slug:           cfg.Slug,
			Name:           cfg.Name,
			Role:           cfg.Name,
			Expertise:      append([]string(nil), cfg.Expertise...),
			Personality:    cfg.Personality,
			PermissionMode: cfg.PermissionMode,
			AllowedTools:   append([]string(nil), cfg.AllowedTools...),
			BuiltIn:        cfg.Slug == l.pack.LeadSlug || cfg.Slug == "ceo",
		}
		applyOfficeMemberDefaults(&member)
		filtered = append(filtered, member)
	}
	if len(filtered) > 0 {
		return filtered
	}
	return members
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

// ═══════════════════════════════════════════════════════════════
// Web View Mode
// ═══════════════════════════════════════════════════════════════

// PreflightWeb checks only for claude (no tmux requirement for web mode).
func (l *Launcher) PreflightWeb() error {
	if l.usesCodexRuntime() {
		if _, err := exec.LookPath("codex"); err != nil {
			return fmt.Errorf("codex not found. Install Codex CLI and run `codex login`")
		}
		return nil
	}
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("claude not found in PATH. Install Claude Code CLI first")
	}
	return nil
}

// LaunchWeb starts the broker, web UI server, and background agents without tmux.
func (l *Launcher) LaunchWeb(webPort int) error {
	// Offer to wire Nex when the user hasn't opted out and nex-cli isn't yet
	// installed. `nex setup` handles detection and wiring for us — we just
	// surface the prompt.
	if !config.ResolveNoNex() && !nex.IsInstalled() {
		fmt.Println()
		fmt.Print("  Connect Nex for memory and context? [Y/n] ")
		var answer string
		fmt.Scanln(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "" || answer == "y" || answer == "yes" {
			fmt.Println()
			fmt.Println("  Nex CLI not found. Installing...")
			if _, installErr := setup.InstallLatestCLI(); installErr != nil {
				fmt.Printf("  Could not install: %v\n", installErr)
				fmt.Println("  Continuing without Nex.")
			}
			if nexBin := nex.BinaryPath(); nexBin != "" {
				cmd := exec.Command(nexBin, "setup")
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					fmt.Printf("  Setup did not complete: %v\n", err)
					fmt.Println("  Continuing without Nex.")
				} else {
					fmt.Println("  Nex connected.")
				}
			}
			fmt.Println()
		} else {
			fmt.Println("  Skipping Nex. Agents will work without organizational memory.")
			fmt.Println()
		}
	}

	mcpConfig, err := l.ensureMCPConfig()
	if err != nil {
		return fmt.Errorf("prepare mcp config: %w", err)
	}
	l.mcpConfig = mcpConfig
	l.webMode = true

	killStaleBroker()

	l.broker = NewBroker()
	l.broker.runtimeProvider = l.provider
	l.broker.packSlug = l.packSlug
	l.broker.blankSlateLaunch = l.blankSlateLaunch
	if err := l.broker.SetSessionMode(l.sessionMode, l.oneOnOne); err != nil {
		return fmt.Errorf("set session mode: %w", err)
	}
	if err := l.broker.SetFocusMode(l.focusMode); err != nil {
		return fmt.Errorf("set focus mode: %w", err)
	}
	if err := l.broker.Start(); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}
	if err := writeOfficePIDFile(); err != nil {
		return fmt.Errorf("write office pid: %w", err)
	}

	// Pre-seed any default skills declared by the pack (idempotent).
	if l.pack != nil && len(l.pack.DefaultSkills) > 0 {
		l.broker.SeedDefaultSkills(l.pack.DefaultSkills)
	}

	l.broker.SetGenerateMemberFn(l.GenerateMemberTemplateFromPrompt)
	l.broker.SetGenerateChannelFn(l.GenerateChannelTemplateFromPrompt)
	l.broker.ServeWebUI(webPort)

	// Web mode always uses queued headless turns so notifications can push
	// scoped work directly instead of relying on long-lived agents polling.
	l.headlessCtx, l.headlessCancel = context.WithCancel(context.Background())
	l.resumeInFlightWork()

	go l.notifyAgentsLoop()
	go l.notifyTaskActionsLoop()
	go l.notifyOfficeChangesLoop()
	go l.pollNexNotificationsLoop()
	go l.watchdogSchedulerLoop()

	webURL := fmt.Sprintf("http://localhost:%d", webPort)
	fmt.Printf("\n  Web UI:  %s\n", webURL)
	fmt.Printf("  Broker:  %s\n", l.BrokerBaseURL())
	fmt.Printf("  Press Ctrl+C to stop.\n\n")

	if !l.noOpen {
		openBrowser(webURL)
	}

	select {}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		return
	}
	_ = cmd.Start()
}

// spawnBackgroundAgents starts all agents as headless background processes (no tmux).
// Kept for compatibility with older tooling; LaunchWeb now uses queued headless
// turns instead so notifications can interrupt stale work immediately.
func (l *Launcher) spawnBackgroundAgents() {
	for _, member := range l.visibleOfficeMembers() {
		cmdStr := l.headlessClaudeCommand(member.Slug, l.buildPrompt(member.Slug))
		go l.runBackgroundAgent(member.Slug, cmdStr)
	}
	for _, member := range l.overflowOfficeMembers() {
		cmdStr := l.headlessClaudeCommand(member.Slug, l.buildPrompt(member.Slug))
		go l.runBackgroundAgent(member.Slug, cmdStr)
	}
}

// headlessClaudeCommand builds a non-interactive claude command for web mode.
func (l *Launcher) headlessClaudeCommand(slug, systemPrompt string) string {
	escaped := strings.ReplaceAll(systemPrompt, "'", "'\\''")
	agentMCPPath, err := l.ensureAgentMCPConfig(slug)
	if err != nil {
		agentMCPPath = l.mcpConfig
	}
	mcpConfig := strings.ReplaceAll(agentMCPPath, "'", "'\\''")
	permFlags := l.resolvePermissionFlags(slug)
	brokerToken := ""
	if l.broker != nil {
		brokerToken = l.broker.Token()
	}
	model := l.headlessClaudeModel(slug)
	initialPrompt := "You are now active in the WUPHF office. Notifications are pushed to you — do NOT poll for messages. Focus entirely on the work described in each pushed notification. Use team_broadcast to post replies. Only use team_poll if a pushed notification explicitly tells you context is missing."
	return fmt.Sprintf(
		"WUPHF_AGENT_SLUG=%s WUPHF_BROKER_TOKEN=%s WUPHF_BROKER_BASE_URL=%s WUPHF_NO_NEX=%t ANTHROPIC_PROMPT_CACHING=1 claude --model %s --print %s --append-system-prompt '%s' --mcp-config '%s' --strict-mcp-config -p '%s'",
		slug, brokerToken, l.BrokerBaseURL(), config.ResolveNoNex(), model, permFlags, escaped, mcpConfig,
		strings.ReplaceAll(initialPrompt, "'", "'\\''"),
	)
}

// runBackgroundAgent runs a single agent as a headless OS process.
func (l *Launcher) runBackgroundAgent(slug, cmdStr string) {
	logDir := filepath.Join(os.TempDir(), "wuphf-agents")
	os.MkdirAll(logDir, 0o700)
	logPath := filepath.Join(logDir, slug+".log")

	for {
		logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[%s] failed to open log %s: %v\n", slug, logPath, err)
			time.Sleep(5 * time.Second)
			continue
		}
		cmd := exec.Command("bash", "-c", cmdStr)
		cmd.Dir = l.cwd
		cmd.Env = append(os.Environ(),
			fmt.Sprintf("WUPHF_BROKER_TOKEN=%s", l.broker.Token()),
			fmt.Sprintf("WUPHF_BROKER_BASE_URL=%s", l.BrokerBaseURL()),
		)
		cmd.Stdin = nil

		// Fan-out stdout+stderr to the log file AND the broker's per-agent
		// stream buffer so the web UI can tail it in real time via SSE.
		pr, pw := io.Pipe()
		cmd.Stdout = io.MultiWriter(logFile, pw)
		cmd.Stderr = io.MultiWriter(logFile, pw)

		stream := l.broker.AgentStream(slug)
		go func() {
			scanner := bufio.NewScanner(pr)
			scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
			for scanner.Scan() {
				stream.Push(scanner.Text())
			}
		}()

		fmt.Fprintf(os.Stderr, "[%s] agent starting (log: %s)\n", slug, logPath)
		runErr := cmd.Run()
		pw.Close() // signals scanner goroutine to exit
		logFile.Close()

		if runErr != nil {
			fmt.Fprintf(os.Stderr, "[%s] agent exited: %v, restarting in 5s\n", slug, runErr)
		}
		time.Sleep(5 * time.Second)
	}
}

// postEscalation writes a system message to #general when an agent is stuck
// or has blown its retry budget. The Slack-style UI renders this as a normal
// message so humans see it without needing to open a panel.
func (l *Launcher) postEscalation(slug, taskID string, reason agent.EscalationReason, detail string) {
	if l.broker == nil {
		return
	}
	who := strings.TrimSpace(slug)
	if who == "" {
		who = "an agent"
	}
	var body string
	switch reason {
	case agent.EscalationStuck:
		body = fmt.Sprintf("Heads up: %s looks stuck. Task %s — %s. Needs eyes.", who, taskID, detail)
	case agent.EscalationMaxRetries:
		body = fmt.Sprintf("Heads up: %s keeps erroring on task %s. Last error: %s. Needs eyes.", who, taskID, detail)
	default:
		body = fmt.Sprintf("Heads up: %s escalation on %s: %s", who, taskID, detail)
	}
	l.broker.PostSystemMessage("general", body, "escalation")
}
