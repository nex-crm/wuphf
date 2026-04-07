package main

import (
	"fmt"
	"hash"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/glamour"
)

const (
	mainLinesCacheLimit = 48
	sidebarCacheLimit   = 24
	markdownCacheLimit  = 256
)

type channelRenderCacheStore struct {
	mu sync.Mutex

	mainLines map[uint64][]renderedLine
	sidebars  map[uint64]string
	markdown  map[uint64]string
	renderers map[int]*glamour.TermRenderer
}

var channelRenderCache = channelRenderCacheStore{
	mainLines: make(map[uint64][]renderedLine),
	sidebars:  make(map[uint64]string),
	markdown:  make(map[uint64]string),
	renderers: make(map[int]*glamour.TermRenderer),
}

func cachedSidebarRender(channels []channelInfo, members []channelMember, tasks []channelTask, activeChannel string, activeApp officeApp, cursor int, rosterOffset int, focused bool, quickJump quickJumpTarget, workspace workspaceUIState, width, height int) string {
	key := hashSidebarState(channels, members, tasks, activeChannel, activeApp, cursor, rosterOffset, focused, quickJump, workspace, width, height)
	if cached, ok := channelRenderCache.getSidebar(key); ok {
		return cached
	}
	rendered := renderSidebar(channels, members, tasks, activeChannel, activeApp, cursor, rosterOffset, focused, quickJump, workspace, width, height)
	channelRenderCache.putSidebar(key, rendered)
	return rendered
}

func (m channelModel) cachedMainLines(contentWidth int) []renderedLine {
	key := m.hashMainLinesState(contentWidth)
	if cached, ok := channelRenderCache.getMainLines(key); ok {
		return cached
	}

	var lines []renderedLine
	if m.isOneOnOne() {
		if m.activeApp == officeAppRecovery {
			lines = m.buildRecoveryLines(contentWidth)
		} else {
			lines = m.buildDirectFeedLines(contentWidth)
		}
	} else {
		switch m.activeApp {
		case officeAppRecovery:
			lines = m.buildRecoveryLines(contentWidth)
		case officeAppTasks:
			lines = buildTaskLines(m.tasks, contentWidth)
		case officeAppRequests:
			lines = buildRequestLines(m.requests, contentWidth)
		case officeAppPolicies:
			lines = buildPolicyLines(m.signals, m.decisions, m.watchdogs, m.actions, contentWidth)
		case officeAppCalendar:
			lines = buildCalendarLines(m.actions, m.scheduler, m.tasks, m.requests, m.activeChannel, m.members, m.calendarRange, m.calendarFilter, contentWidth)
		case officeAppArtifacts:
			lines = m.buildArtifactLines(contentWidth)
		case officeAppSkills:
			lines = buildSkillLines(m.skills, contentWidth)
		default:
			lines = m.buildOfficeFeedLines(contentWidth)
		}
	}

	channelRenderCache.putMainLines(key, lines)
	return cloneRenderedLines(lines)
}

func (m channelModel) hashMainLinesState(contentWidth int) uint64 {
	h := newStateHasher()
	h.add("main-lines")
	h.addInt(contentWidth)
	h.add(string(m.activeApp))
	h.add(m.activeChannel)
	h.add(string(m.calendarRange))
	h.add(m.calendarFilter)
	h.add(m.sessionMode)
	h.add(m.oneOnOneAgent)
	h.addInt64(renderTimeBucket(m.activeApp, m.isOneOnOne()))

	if m.isOneOnOne() || m.activeApp == officeAppMessages || m.activeApp == officeAppRecovery {
		h.addMessages(m.messages)
		h.addExpandedThreads(m.expandedThreads)
		h.add(m.unreadAnchorID)
		h.addInt(m.unreadCount)
		h.add(m.awaySummary)
		h.addMembers(m.members)
		h.addTasks(m.tasks)
		h.addRequests(m.requests)
		h.addActions(m.actions)
		if m.isOneOnOne() {
			h.add(m.oneOnOneAgentName())
		}
		h.addBool(m.brokerConnected)
		return h.sum()
	}

	switch m.activeApp {
	case officeAppTasks:
		h.addTasks(m.tasks)
	case officeAppRequests:
		h.addRequests(m.requests)
	case officeAppPolicies:
		h.addSignals(m.signals)
		h.addDecisions(m.decisions)
		h.addWatchdogs(m.watchdogs)
		h.addActions(m.actions)
	case officeAppCalendar:
		h.addActions(m.actions)
		h.addScheduler(m.scheduler)
		h.addTasks(m.tasks)
		h.addRequests(m.requests)
		h.addMembers(m.members)
	case officeAppArtifacts:
		h.addTasks(m.tasks)
		h.addRequests(m.requests)
		h.addActions(m.actions)
		h.addInt64(time.Now().Unix() / 10)
	case officeAppSkills:
		h.addSkills(m.skills)
	}
	return h.sum()
}

func renderTimeBucket(activeApp officeApp, direct bool) int64 {
	if direct || activeApp == officeAppMessages {
		return time.Now().Unix()
	}
	return time.Now().Unix() / 30
}

func hashSidebarState(channels []channelInfo, members []channelMember, tasks []channelTask, activeChannel string, activeApp officeApp, cursor int, rosterOffset int, focused bool, quickJump quickJumpTarget, workspace workspaceUIState, width, height int) uint64 {
	h := newStateHasher()
	h.add("sidebar")
	h.addInt(width)
	h.addInt(height)
	h.add(activeChannel)
	h.add(string(activeApp))
	h.addInt(cursor)
	h.addInt(rosterOffset)
	h.addBool(focused)
	h.add(string(quickJump))
	h.addBool(workspace.BrokerConnected)
	h.addBool(workspace.Direct)
	h.add(workspace.Channel, workspace.AgentName, workspace.AgentSlug, workspace.AwaySummary, workspace.Focus, workspace.NextStep)
	h.addInt(workspace.PeerCount)
	h.addInt(workspace.RunningTasks)
	h.addInt(workspace.OpenRequests)
	h.addInt(workspace.BlockingCount)
	h.addInt(workspace.IsolatedCount)
	h.addInt(workspace.UnreadCount)
	h.addBool(workspace.NoNex)
	h.addBool(workspace.APIConfigured)
	if workspace.NeedsYou != nil {
		h.add(workspace.NeedsYou.ID, workspace.NeedsYou.TitleOrQuestion())
	}
	if workspace.PrimaryTask != nil {
		h.add(workspace.PrimaryTask.ID, workspace.PrimaryTask.Title, workspace.PrimaryTask.Status)
	}
	h.addInt64(time.Now().Unix())
	h.addChannels(channels)
	h.addMembers(members)
	h.addTasks(tasks)
	return h.sum()
}

func cloneRenderedLines(lines []renderedLine) []renderedLine {
	if len(lines) == 0 {
		return nil
	}
	out := make([]renderedLine, len(lines))
	copy(out, lines)
	return out
}

func (c *channelRenderCacheStore) getMainLines(key uint64) ([]renderedLine, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lines, ok := c.mainLines[key]
	if !ok {
		return nil, false
	}
	return cloneRenderedLines(lines), true
}

func (c *channelRenderCacheStore) putMainLines(key uint64, lines []renderedLine) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.mainLines) >= mainLinesCacheLimit {
		c.mainLines = make(map[uint64][]renderedLine)
	}
	c.mainLines[key] = cloneRenderedLines(lines)
}

func (c *channelRenderCacheStore) getSidebar(key uint64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rendered, ok := c.sidebars[key]
	return rendered, ok
}

func (c *channelRenderCacheStore) putSidebar(key uint64, rendered string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sidebars) >= sidebarCacheLimit {
		c.sidebars = make(map[uint64]string)
	}
	c.sidebars[key] = rendered
}

func (c *channelRenderCacheStore) getMarkdown(key uint64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rendered, ok := c.markdown[key]
	return rendered, ok
}

func (c *channelRenderCacheStore) putMarkdown(key uint64, rendered string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.markdown) >= markdownCacheLimit {
		c.markdown = make(map[uint64]string)
	}
	c.markdown[key] = rendered
}

func (c *channelRenderCacheStore) renderer(width int) (*glamour.TermRenderer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if renderer, ok := c.renderers[width]; ok {
		return renderer, nil
	}
	renderer, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	c.renderers[width] = renderer
	return renderer, nil
}

type stateHasher struct {
	h hash.Hash64
}

func newStateHasher() stateHasher {
	return stateHasher{h: fnv.New64a()}
}

func (s stateHasher) add(parts ...string) {
	for _, part := range parts {
		_, _ = s.h.Write([]byte(part))
		_, _ = s.h.Write([]byte{0})
	}
}

func (s stateHasher) addInt(v int) {
	s.add(strconv.Itoa(v))
}

func (s stateHasher) addInt64(v int64) {
	s.add(strconv.FormatInt(v, 10))
}

func (s stateHasher) addBool(v bool) {
	if v {
		s.add("1")
		return
	}
	s.add("0")
}

func (s stateHasher) sum() uint64 {
	return s.h.Sum64()
}

func (s stateHasher) addMessages(messages []brokerMessage) {
	s.addInt(len(messages))
	for _, msg := range messages {
		s.add(msg.ID, msg.From, msg.Kind, msg.Source, msg.Title, msg.ReplyTo, msg.Timestamp, msg.Content)
		s.add(strings.Join(msg.Tagged, ","))
	}
}

func (s stateHasher) addExpandedThreads(expanded map[string]bool) {
	if len(expanded) == 0 {
		s.add("no-expanded")
		return
	}
	keys := make([]string, 0, len(expanded))
	for key, expanded := range expanded {
		if expanded {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	s.add(strings.Join(keys, ","))
}

func (s stateHasher) addMembers(members []channelMember) {
	s.addInt(len(members))
	for _, member := range members {
		s.add(member.Slug, member.Name, member.Role, member.LastMessage, member.LastTime, member.LiveActivity)
		s.addBool(member.Disabled)
	}
}

func (s stateHasher) addChannels(channels []channelInfo) {
	s.addInt(len(channels))
	for _, channel := range channels {
		s.add(channel.Slug, channel.Name, channel.Description)
		s.add(strings.Join(channel.Members, ","))
		s.add(strings.Join(channel.Disabled, ","))
	}
}

func (s stateHasher) addTasks(tasks []channelTask) {
	s.addInt(len(tasks))
	for _, task := range tasks {
		s.add(task.ID, task.Channel, task.Title, task.Owner, task.Status, task.TaskType, task.PipelineStage, task.ExecutionMode, task.ReviewState, task.DueAt, task.UpdatedAt)
	}
}

func (s stateHasher) addActions(actions []channelAction) {
	s.addInt(len(actions))
	for _, action := range actions {
		s.add(action.ID, action.Kind, action.Source, action.Channel, action.Actor, action.Summary, action.RelatedID, action.DecisionID, action.CreatedAt)
		s.add(strings.Join(action.SignalIDs, ","))
	}
}

func (s stateHasher) addRequests(requests []channelInterview) {
	s.addInt(len(requests))
	for _, req := range requests {
		s.add(req.ID, req.Kind, req.Status, req.From, req.Channel, req.Title, req.Question, req.Context, req.RecommendedID, req.ReplyTo, req.CreatedAt, req.DueAt, req.FollowUpAt, req.ReminderAt, req.RecheckAt)
		s.addBool(req.Blocking)
		s.addBool(req.Required)
		s.addBool(req.Secret)
		for _, opt := range req.Options {
			s.add(opt.ID, opt.Label, opt.Description)
		}
	}
}

func (s stateHasher) addDecisions(decisions []channelDecision) {
	s.addInt(len(decisions))
	for _, decision := range decisions {
		s.add(decision.ID, decision.Kind, decision.Channel, decision.Summary, decision.Reason, decision.Owner, decision.CreatedAt)
		s.addBool(decision.RequiresHuman)
		s.addBool(decision.Blocking)
		s.add(strings.Join(decision.SignalIDs, ","))
	}
}

func (s stateHasher) addSignals(signals []channelSignal) {
	s.addInt(len(signals))
	for _, signal := range signals {
		s.add(signal.ID, signal.Source, signal.SourceRef, signal.Kind, signal.Title, signal.Content, signal.Channel, signal.Owner, signal.Confidence, signal.Urgency, signal.DedupeKey, signal.CreatedAt)
		s.addBool(signal.RequiresHuman)
		s.addBool(signal.Blocking)
	}
}

func (s stateHasher) addWatchdogs(alerts []channelWatchdog) {
	s.addInt(len(alerts))
	for _, alert := range alerts {
		s.add(alert.ID, alert.Kind, alert.Channel, alert.TargetType, alert.TargetID, alert.Owner, alert.Status, alert.Summary, alert.CreatedAt, alert.UpdatedAt)
	}
}

func (s stateHasher) addScheduler(jobs []channelSchedulerJob) {
	s.addInt(len(jobs))
	for _, job := range jobs {
		s.add(job.Slug, job.Label, job.Kind, job.TargetType, job.TargetID, job.Channel, job.Provider, job.ScheduleExpr, job.WorkflowKey, job.SkillName, job.DueAt, job.NextRun, job.LastRun, job.Status)
		s.addInt(job.IntervalMinutes)
	}
}

func (s stateHasher) addSkills(skills []channelSkill) {
	s.addInt(len(skills))
	for _, skill := range skills {
		s.add(skill.ID, skill.Name, skill.Title, skill.Description, skill.Channel, skill.WorkflowProvider, skill.WorkflowKey, skill.WorkflowSchedule, skill.RelayID, skill.RelayPlatform, skill.LastExecutionAt, skill.LastExecutionStatus, skill.Status, skill.UpdatedAt)
		s.add(strings.Join(skill.Tags, ","))
		s.add(strings.Join(skill.RelayEventTypes, ","))
		s.addInt(skill.UsageCount)
	}
}

func markdownCacheKey(width int, text string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("%d|%s", width, text)))
	return h.Sum64()
}
