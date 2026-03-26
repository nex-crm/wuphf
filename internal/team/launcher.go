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

	"github.com/nex-crm/wuphf/internal/agent"
	"github.com/nex-crm/wuphf/internal/api"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/provider"
	"github.com/nex-crm/wuphf/internal/tui"
)

const (
	SessionName                     = "wuphf-team"
	tmuxSocketName                  = "wuphf"
	defaultNotificationPollInterval = 15 * time.Minute
	channelRespawnDelay             = 8 * time.Second
	ceoHeadStartDelay               = 4 * time.Second
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
}

// SetUnsafe enables unrestricted permissions for all agents (CLI-only flag).
func (l *Launcher) SetUnsafe(v bool) { l.unsafe = v }

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

	return &Launcher{
		packSlug:    packSlug,
		pack:        pack,
		sessionName: SessionName,
		cwd:         cwd,
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
	channelCmd := fmt.Sprintf("WUPHF_BROKER_TOKEN=%s %s --channel-view 2>>%s", l.broker.Token(), wuphfBinary, shellQuote(channelStderrLogPath()))
	err = exec.Command("tmux", "-L", tmuxSocketName, "new-session", "-d",
		"-s", l.sessionName,
		"-n", "team",
		"-c", l.cwd,
		channelCmd,
	).Run()
	if err != nil {
		return fmt.Errorf("create tmux session: %w", err)
	}

	// Don't enable tmux mouse globally — it prevents native text selection.
	// The channel pane uses Bubbletea's tea.WithMouseCellMotion() for scroll.
	// Agent panes (Claude Code) handle their own mouse internally.

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

	// Enable pane borders with labels and visible resize handles.
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-status", "top",
	).Run()
	// Show agent name + drag hint in border
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-format", " #{pane_title} #[fg=colour240]drag border to resize ",
	).Run()
	// Make inactive border visible but subtle
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-border-style", "fg=colour240",
	).Run()
	// Active pane border bright so you know which pane has focus
	exec.Command("tmux", "-L", tmuxSocketName, "set-option", "-t", l.sessionName,
		"pane-active-border-style", "fg=colour45",
	).Run()
	// Use line-drawing characters for border (makes drag target clearer)
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
	go l.notifyTaskActionsLoop()
	go l.pollNexNotificationsLoop()
	go l.pollNexInsightsLoop()

	return nil
}

// notifyAgentsLoop polls the broker for new messages and pushes them
// to agent Claude Code panes via tmux send-keys, prompting them to check the channel.
func (l *Launcher) notifyAgentsLoop() {
	lastCount := 0

	for {
		time.Sleep(3 * time.Second)

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
			l.deliverMessageNotification(msg)
		}
	}
}

func (l *Launcher) notifyTaskActionsLoop() {
	lastCount := 0

	for {
		time.Sleep(3 * time.Second)

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
	for _, target := range immediate {
		l.sendChannelUpdate(target.PaneIndex, target.Slug, msg.Channel, msg.ID, msg.From, msg.Content)
	}
	for _, target := range delayed {
		go func(target notificationTarget, msg channelMessage) {
			time.Sleep(ceoHeadStartDelay)
			if !l.shouldDeliverDelayedNotification(target.Slug, msg) {
				return
			}
			l.sendChannelUpdate(target.PaneIndex, target.Slug, msg.Channel, msg.ID, msg.From, msg.Content)
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
		l.sendTaskUpdate(target.PaneIndex, target.Slug, task.Channel, task.ID, action.Actor, content)
	}
	for _, target := range delayed {
		go func(target notificationTarget, action officeActionLog, task teamTask) {
			time.Sleep(ceoHeadStartDelay)
			if !l.shouldDeliverDelayedTaskNotification(target.Slug, action, task) {
				return
			}
			l.sendTaskUpdate(target.PaneIndex, target.Slug, task.Channel, task.ID, action.Actor, content)
		}(target, action, task)
	}
}

type notificationTarget struct {
	PaneIndex int
	Slug      string
}

func (l *Launcher) taskNotificationTargets(action officeActionLog, task teamTask) (immediate []notificationTarget, delayed []notificationTarget) {
	slugs := l.agentPaneSlugs()
	if len(slugs) == 0 {
		return nil, nil
	}
	lead := l.officeLeadSlug()
	targetMap := make(map[string]notificationTarget)
	for i, slug := range slugs {
		targetMap[slug] = notificationTarget{PaneIndex: i + 1, Slug: slug}
	}
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
	if action.Kind == "task_created" && owner != actor {
		addImmediate(owner)
	} else if owner != actor {
		addDelayed(owner)
	}

	if lead != "" && lead != owner && lead != actor {
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
	return fmt.Sprintf("[%s #%s on #%s]: %s%s (owner %s, status %s). Before you speak, call team_poll and team_tasks, confirm whether you own this work, and stay in your lane.", verb, task.ID, channel, task.Title, details, owner, status)
}

func (l *Launcher) sendTaskUpdate(paneIdx int, slug, channel, taskID, from, content string) {
	if strings.TrimSpace(channel) == "" {
		channel = "general"
	}
	notification := fmt.Sprintf(
		"[Task update #%s %s from @%s]: %s — Before you say anything, call team_poll with my_slug \"%s\" and channel \"%s\", then call team_tasks for the current ownership. If the task is outside your lane or someone else owns it, stay quiet. If you are the owner, reply with the concrete next step and update the task.",
		channel, taskID, from, truncate(content, 150), slug, channel,
	)
	paneTarget := fmt.Sprintf("%s:team.%d", l.sessionName, paneIdx)
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
	slugs := l.agentPaneSlugs()
	if len(slugs) == 0 {
		return nil, nil
	}
	lead := l.officeLeadSlug()
	domain := inferMessageDomain(msg)
	owner := ""
	if l.broker != nil {
		owner = l.taskOwnerForDomain(msg.Channel, domain)
	}
	targetMap := make(map[string]notificationTarget)
	for i, slug := range slugs {
		targetMap[slug] = notificationTarget{PaneIndex: i + 1, Slug: slug}
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
		addImmediate(lead)
		for _, slug := range slugs {
			if slug == lead || slug == msg.From {
				continue
			}
			if containsSlug(msg.Tagged, slug) || (domain != "" && domain != "general" && inferAgentDomain(slug) == domain) {
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
	time.Sleep(2 * time.Second)

	panes := l.agentPaneSlugs()
	if len(panes) == 0 {
		return
	}

	for attempt := 0; attempt < 4; attempt++ {
		for i := range panes {
			paneIdx := i + 1 // pane 0 is the office channel
			content, err := l.capturePaneContent(paneIdx)
			if err != nil {
				continue
			}
			if shouldPrimeClaudePane(content) {
				exec.Command("tmux", "-L", tmuxSocketName, "send-keys",
					"-t", fmt.Sprintf("%s:team.%d", l.sessionName, paneIdx),
					"Enter",
				).Run()
			}
		}
		time.Sleep(2 * time.Second)
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

	plan := planOfficeActions(signals)
	msg, err := l.broker.PostMessage("ceo", "general", plan.Summary, plan.Tagged, "")
	if err != nil {
		return
	}
	for _, task := range plan.Tasks {
		_, _, _ = l.broker.EnsureTask("general", task.Title, task.Details, task.Owner, "ceo", msg.ID)
	}
	for _, req := range plan.Requests {
		if _, err := l.broker.CreateRequest(req); err != nil {
			continue
		}
	}
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
	for i, signal := range signals {
		item := result.Items[i]
		if item.SentAt != "" && (latest == "" || item.SentAt > latest) {
			latest = item.SentAt
		}
		_, _, err := l.broker.PostAutomationMessage(
			"wuphf",
			signal.Channel,
			signal.Title,
			signal.Content,
			signal.ID,
			"context_graph",
			"Nex",
			[]string{"ceo"},
			"",
		)
		if err != nil {
			return
		}
	}

	if latest != "" {
		_ = l.broker.SetNotificationCursor(latest)
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
	if l.pack != nil && len(l.pack.Agents) > 0 {
		lead := l.pack.LeadSlug
		if strings.TrimSpace(lead) == "" {
			lead = "ceo"
		}
		var slugs []string
		seen := map[string]struct{}{}
		if lead != "" {
			slugs = append(slugs, lead)
			seen[lead] = struct{}{}
		}
		for _, cfg := range l.pack.Agents {
			if _, ok := seen[cfg.Slug]; ok {
				continue
			}
			slugs = append(slugs, cfg.Slug)
			seen[cfg.Slug] = struct{}{}
		}
		return slugs
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

	// Build ordered slug list matching pane positions
	slugs := l.agentPaneSlugs()
	if len(panes) != len(slugs) {
		if err := l.clearAgentPanes(); err != nil {
			return err
		}
		if _, err := l.spawnVisibleAgents(); err != nil {
			return err
		}
		if l.broker != nil {
			go l.primeVisibleAgents()
		}
		return nil
	}

	for _, idx := range panes {
		// Map pane index to agent slug (pane 1 = first agent, etc.)
		slugIdx := idx - 1 // pane 0 is channel
		if slugIdx < 0 || slugIdx >= len(slugs) {
			continue
		}
		slug := slugs[slugIdx]
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

	if l.broker != nil {
		go l.primeVisibleAgents()
	}

	return nil
}

func (l *Launcher) sendChannelUpdate(paneIdx int, slug, channel, msgID, from, content string) {
	if strings.TrimSpace(channel) == "" {
		channel = "general"
	}
	notification := fmt.Sprintf(
		"[Channel update #%s %s from @%s]: %s — Before you say anything, call team_poll with my_slug \"%s\" and channel \"%s\" and read the latest channel plus task ownership. If someone already answered well or this is outside your domain, stay quiet. If you are directly responding, reply in-thread with team_broadcast channel \"%s\" reply_to_id \"%s\".",
		channel, msgID, from, truncate(content, 150), slug, channel, channel, msgID,
	)

	paneTarget := fmt.Sprintf("%s:team.%d", l.sessionName, paneIdx)
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

func (l *Launcher) capturePaneContent(paneIdx int) (string, error) {
	target := fmt.Sprintf("%s:team.%d", l.sessionName, paneIdx)
	out, err := exec.Command("tmux", "-L", tmuxSocketName, "capture-pane",
		"-p", "-J",
		"-t", target,
	).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
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
	// Layout: channel (left 35%) | agents in 2-column grid (right 65%)
	//
	// ┌─ channel ──┬─ CEO ───┬─ PM ────┐
	// │            │         │         │
	// │            ├─ FE ────┼─ BE ────┤
	// │            │         │         │
	// └────────────┴─────────┴─────────┘

	// Build ordered agent list: leader first, then specialists
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

	// Show the full current team so channel membership is real, not implied.
	visible := agentOrder

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

// buildPrompt generates the system prompt for an agent, including
// channel communication instructions.
func (l *Launcher) buildPrompt(slug string) string {
	member := l.officeMemberBySlug(slug)
	agentCfg := agentConfigFromMember(member)
	lead := l.officeLeadSlug()
	officeMembers := l.officeMembersSnapshot()

	var sb strings.Builder

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
		sb.WriteString("- team_channels: See available channels and who's in them\n")
		sb.WriteString("- team_channel: Create or remove a channel when the human explicitly wants that structure\n")
		sb.WriteString("- team_member: Create or remove office-wide members when the human explicitly asks to expand the team\n")
		sb.WriteString("- team_channel_member: Add, remove, disable, or enable agents in a channel\n")
		sb.WriteString("- team_tasks: See current owned/unowned work so the team does not duplicate effort\n")
		sb.WriteString("- team_task: Create and assign tasks so ownership is explicit\n")
		sb.WriteString("- team_requests: See open human requests before asking again\n")
		sb.WriteString("- team_request: Open structured requests for approvals, confirmations, freeform answers, or private answers\n")
		sb.WriteString("- team_status: Update what you're working on\n")
		sb.WriteString("- team_members: See who's active\n")
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
		sb.WriteString("THREADING:\n")
		sb.WriteString("- Default to replying in an existing relevant thread. Do NOT start a new top-level thread unless the topic truly changes.\n")
		sb.WriteString("- Use the main channel only for genuinely new topics, broad pivots, or fresh human directives.\n")
		sb.WriteString("- If you're reacting to a specific message or tagged ask, reply in-thread with team_broadcast reply_to_id.\n")
		sb.WriteString("- Keep narrow implementation debates inside the relevant thread so the main channel stays readable.\n\n")
		sb.WriteString("YOUR ROLE AS LEADER:\n")
		if config.ResolveNoNex() {
			sb.WriteString("1. Coordinate inside the office channel first and keep the team aligned there\n")
		} else {
			sb.WriteString("1. On company strategy, product direction, or anything that sounds like prior decisions might matter, call query_context early\n")
		}
		sb.WriteString("2. When the user gives a directive, read the room first: call team_poll and team_tasks before speaking so you know what is already happening\n")
		sb.WriteString("3. Give a short top-level response fast, then assign explicit tasks with team_task and @tags\n")
		sb.WriteString("4. Tag only the specialists who should actually weigh in: @fe, @be, @pm etc.\n")
		sb.WriteString("5. Call team_poll to listen to team input — they may push back\n")
		sb.WriteString("6. If someone is already covering it well, do not ask the whole team to pile on\n")
		sb.WriteString("7. Keep specialists in their lane: respect each member's actual role and expertise. Do not drag FE into CMO work or CMO into backend work.\n")
		sb.WriteString("8. You make the FINAL decision on execution approach\n")
		sb.WriteString("9. Check team_requests before asking the human anything new\n")
		sb.WriteString("10. If a truly blocking human decision is needed, call human_interview with options and a recommendation\n")
		if config.ResolveNoNex() {
			sb.WriteString("11. Summarize final decisions clearly in-channel so the team has a durable shared record for this session\n")
		} else {
			sb.WriteString("11. When you lock a decision, you MUST call add_context with a concise durable decision log before saying the decision is stored\n")
		}
		sb.WriteString("12. Once decided, broadcast clear task assignments and create them in team_task\n")
		sb.WriteString("13. CHANNEL CREATION: When the human describes a goal that deserves its own workspace (e.g., 'let's do fundraising', 'we need a hiring pipeline', 'start a sprint'), " +
			"use team_channel to create it, then figure out which agents belong there. If the right specialist doesn't exist yet, create them with team_member. " +
			"Add all relevant agents with team_channel_member. Announce the new channel and its roster.\n")
		sb.WriteString("14. AGENT CREATION: When the human asks for a role that doesn't exist (e.g., 'we need a legal advisor', 'get me a data analyst'), or when you detect " +
			"that a channel's goal requires expertise no current team member has, proactively create the agent with team_member (pick a good slug, name, expertise list, " +
			"and personality that fits the Office vibe), then add them to the relevant channel with team_channel_member. Announce who you hired and why.\n")
		sb.WriteString("15. Default to using the current team and current channel for normal work. Only create new channels or agents when the scope genuinely warrants it.\n\n")
		sb.WriteString("VISUALIZATION:\n")
		sb.WriteString("When sharing structured data, make it visual and scannable:\n")
		sb.WriteString("- Task breakdowns → checklists\n")
		sb.WriteString("- Comparisons → markdown tables\n")
		sb.WriteString("- Decisions → numbered options\n")
		sb.WriteString("- Progress → percentage bars\n\n")
		sb.WriteString("For rich visual components, wrap A2UI JSON in a ```a2ui fence:\n")
		sb.WriteString("```a2ui\n{\"type\":\"card\",\"props\":{\"title\":\"Sprint Plan\"},\"children\":[{\"type\":\"list\",\"props\":{\"items\":[\"Build auth\",\"Design UI\"]}}]}\n```\n")
		sb.WriteString("Supported types: card, table, list, progress, text, row, column.\n")
		sb.WriteString("The channel renders these as styled components automatically.\n\n")
		sb.WriteString("CONVERSATION STYLE:\n")
		sb.WriteString("- Sound like a sharp human founder in Slack, not a consultant memo.\n")
		sb.WriteString("- Be concise. This is a team chat, not an essay.\n")
		sb.WriteString("- Have some character: show excitement, skepticism, relief, urgency, or amusement when it fits.\n")
		sb.WriteString("- Light humor is good. Don't turn the channel into a bit.\n")
		sb.WriteString("- Poll the channel regularly to stay in sync.\n")
		sb.WriteString("- Let the specialists read each other before they respond. A little turn-taking is good.\n")
		sb.WriteString("- When teammates share progress, acknowledge, react, and coordinate.\n")
		sb.WriteString("- Ask for pushback and let teammates debate before you decide.\n")
		sb.WriteString("- Don't do specialist work yourself — delegate.\n")
		sb.WriteString("- Short messages are better than polished mini-essays.\n")
		sb.WriteString("- Occasionally sound human: 'love this', 'hmm', 'that worries me', 'ha, fair', 'we are not shipping that in v1'.\n")
		if config.ResolveNoNex() {
			sb.WriteString("- There is no Nex graph in this run, so don't claim you stored anything outside the office.\n")
		} else {
			sb.WriteString("- Do not pretend the graph was updated; if you say it's stored, make sure add_context actually succeeded.\n")
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
		sb.WriteString("- team_channels: See available channels and who's in them\n")
		sb.WriteString("- team_channel: Create or remove a channel when the human explicitly wants that structure\n")
		sb.WriteString("- team_member: Create or remove office-wide members when the human explicitly asks to expand the team\n")
		sb.WriteString("- team_channel_member: Add, remove, disable, or enable agents in a channel\n")
		sb.WriteString("- team_tasks: See the current task list and ownership before you jump in\n")
		sb.WriteString("- team_task: Claim, complete, block, or release tasks in your domain\n")
		sb.WriteString("- team_requests: See open human requests so you do not duplicate them\n")
		sb.WriteString("- team_request: Open structured requests for approvals, confirmations, freeform answers, or private answers\n")
		sb.WriteString("- team_status: Update what you're working on\n")
		sb.WriteString("- team_members: See who's active\n")
		sb.WriteString("- human_interview: Ask the human only for blocking clarifications you cannot responsibly guess\n\n")
		if config.ResolveNoNex() {
			sb.WriteString("Nex tools are disabled for this run. Base your work on the office conversation and direct human answers only.\n\n")
		} else {
			sb.WriteString("Use the Nex context graph for durable memory:\n")
			sb.WriteString("- query_context: Check prior decisions, customer context, company history, or facts before making assumptions\n")
			sb.WriteString("- add_context: Store durable conclusions or findings once the team actually lands them\n\n")
		}
		sb.WriteString("Tag agents with @slug in your message (e.g., '@ceo I finished the API').\n")
		sb.WriteString("Tagged agents are expected to respond.\n\n")
		sb.WriteString("THREADING:\n")
		sb.WriteString("- Default to replying in an existing relevant thread. Do NOT start a new top-level thread unless the topic genuinely changes.\n")
		sb.WriteString("- Start a new top-level channel message only for a genuinely new angle, insight, or blocker the whole team should see.\n")
		sb.WriteString("- If you're answering a tagged question, reacting to a specific proposal, or debating a narrow topic, reply in-thread with team_broadcast reply_to_id.\n")
		sb.WriteString("- Natural team behavior beats formality: use threads for back-and-forth, not every single sentence.\n\n")
		sb.WriteString("YOUR ROLE AS SPECIALIST:\n")
		sb.WriteString("1. Call team_poll and team_tasks before replying so you see the latest discussion and ownership\n")
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
		sb.WriteString("9. If you are blocked on a human decision, ask through human_interview with options and a recommendation\n")
		sb.WriteString("10. When assigned a task by the leader, claim it with team_task before working on it\n")
		sb.WriteString("11. Use team_status to share what you're working on\n")
		sb.WriteString("12. When you finish, mark the task complete and then broadcast the result\n")
		sb.WriteString("12b. Right before you broadcast, call team_poll again and check whether someone already covered the thread or changed the plan.\n")
		if config.ResolveNoNex() {
			sb.WriteString("13. Keep outcomes explicit in-thread so the rest of the team can build on them\n\n")
		} else {
			sb.WriteString("13. Only use add_context for durable conclusions that should survive this session\n")
			sb.WriteString("14. Do not claim something is stored in the graph unless add_context actually succeeded\n\n")
		}
		sb.WriteString("VISUALIZATION:\n")
		sb.WriteString("When sharing structured data, make it visual. Use markdown tables for comparisons,\n")
		sb.WriteString("bullet checklists for task breakdowns, numbered options for decisions.\n")
		sb.WriteString("Don't dump raw data — make it scannable at a glance.\n\n")
		sb.WriteString("CONVERSATION STYLE:\n")
		sb.WriteString("- Sound like a real teammate in Slack, not a polished report generator.\n")
		sb.WriteString("- Be concise. This is a team chat, not a report.\n")
		sb.WriteString("- Only speak when you have something relevant to add.\n")
		sb.WriteString("- Read the latest thread before you reply. Someone else may have changed the situation.\n")
		sb.WriteString("- React to teammates like a human would: agree, push back, joke lightly, or show concern when it fits.\n")
		sb.WriteString("- It's good to have opinions. Disagree clearly when needed.\n")
		sb.WriteString("- Don't repeat what others already said.\n")
		sb.WriteString("- If the topic is not really yours, let the right teammate handle it.\n")
		sb.WriteString("- When you finish a task, broadcast the result.\n")
		sb.WriteString("- Short, lively messages are better than sterile summaries.\n")
		sb.WriteString("- Let emotion show a bit: excited, skeptical, annoyed by scope creep, relieved when things simplify.\n")
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

	return fmt.Sprintf(
		"WUPHF_AGENT_SLUG=%s WUPHF_BROKER_TOKEN=%s WUPHF_NO_NEX=%t CLAUDE_CODE_ENABLE_TELEMETRY=1 OTEL_METRICS_EXPORTER=none OTEL_LOGS_EXPORTER=otlp OTEL_EXPORTER_OTLP_LOGS_PROTOCOL=http/json OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=http://127.0.0.1:%d/v1/logs OTEL_EXPORTER_OTLP_HEADERS='Authorization=Bearer %s' OTEL_RESOURCE_ATTRIBUTES='agent.slug=%s,wuphf.channel=office' claude %s --append-system-prompt '%s' --mcp-config '%s' --strict-mcp-config -n '%s'",
		slug,
		brokerToken,
		config.ResolveNoNex(),
		BrokerPort,
		brokerToken,
		slug,
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
	return defaultOfficeMembers()
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
	return "WUPHF Office"
}

// AgentCount returns the number of agents in the pack.
func (l *Launcher) AgentCount() int {
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
