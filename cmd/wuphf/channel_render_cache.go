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

	"github.com/nex-crm/wuphf/cmd/wuphf/channelui"
)

const (
	mainLinesCacheLimit = 48
	sidebarCacheLimit   = 24
	markdownCacheLimit  = 256
	threadedCacheLimit  = 96
	viewportBlockLimit  = 2048
)

type channelRenderCacheStore struct {
	mu sync.Mutex

	mainLines map[uint64][]channelui.RenderedLine
	sidebars  map[uint64]string
	markdown  map[uint64]string
	renderers map[int]*glamour.TermRenderer
	threaded  map[uint64][]channelui.ThreadedMessage
	blocks    map[uint64][]channelui.RenderedLine
}

var channelRenderCache = channelRenderCacheStore{
	mainLines: make(map[uint64][]channelui.RenderedLine),
	sidebars:  make(map[uint64]string),
	markdown:  make(map[uint64]string),
	renderers: make(map[int]*glamour.TermRenderer),
	threaded:  make(map[uint64][]channelui.ThreadedMessage),
	blocks:    make(map[uint64][]channelui.RenderedLine),
}

func cachedSidebarRender(channels []channelui.ChannelInfo, members []channelui.Member, tasks []channelui.Task, activeChannel string, activeApp channelui.OfficeApp, cursor int, rosterOffset int, focused bool, quickJump quickJumpTarget, workspace channelui.WorkspaceUIState, width, height int, checklist ...onboardingChecklist) string {
	key := hashSidebarState(channels, members, tasks, activeChannel, activeApp, cursor, rosterOffset, focused, quickJump, workspace, width, height)
	// Checklist is dynamic per render — bypass cache when it's active.
	checklistActive := len(checklist) > 0 && !checklist[0].Dismissed
	if !checklistActive {
		if cached, ok := channelRenderCache.getSidebar(key); ok {
			return cached
		}
	}
	rendered := renderSidebar(channels, members, tasks, activeChannel, activeApp, cursor, rosterOffset, focused, quickJump, workspace, width, height, checklist...)
	if !checklistActive {
		channelRenderCache.putSidebar(key, rendered)
	}
	return rendered
}

func (m channelModel) cachedMainLines(contentWidth int) []channelui.RenderedLine {
	key := m.hashMainLinesState(contentWidth)
	if cached, ok := channelRenderCache.getMainLines(key); ok {
		return cached
	}

	var lines []channelui.RenderedLine
	if m.isOneOnOne() {
		switch m.activeApp {
		case channelui.OfficeAppRecovery:
			lines = m.buildRecoveryLines(contentWidth)
		case channelui.OfficeAppInbox:
			lines = buildInboxLines(channelui.FilterMessagesForViewerScope(m.messages, m.oneOnOneAgentSlug(), "inbox"), m.requests, contentWidth)
		case channelui.OfficeAppOutbox:
			lines = buildOutboxLines(channelui.FilterMessagesForViewerScope(m.messages, m.oneOnOneAgentSlug(), "outbox"), m.actions, contentWidth)
		default:
			lines = m.buildDirectFeedLines(contentWidth)
		}
	} else {
		switch m.activeApp {
		case channelui.OfficeAppInbox:
			lines = buildInboxLines(m.messages, m.requests, contentWidth)
		case channelui.OfficeAppOutbox:
			lines = buildOutboxLines(m.messages, m.actions, contentWidth)
		case channelui.OfficeAppRecovery:
			lines = m.buildRecoveryLines(contentWidth)
		case channelui.OfficeAppTasks:
			lines = channelui.BuildTaskLines(m.tasks, contentWidth)
		case channelui.OfficeAppRequests:
			lines = channelui.BuildRequestLines(m.requests, contentWidth)
		case channelui.OfficeAppPolicies:
			lines = channelui.BuildPolicyLines(m.signals, m.decisions, m.watchdogs, m.actions, contentWidth)
		case channelui.OfficeAppCalendar:
			lines = channelui.BuildCalendarLines(m.actions, m.scheduler, m.tasks, m.requests, m.activeChannel, m.members, m.calendarRange, m.calendarFilter, contentWidth)
		case channelui.OfficeAppArtifacts:
			lines = m.buildArtifactLines(contentWidth)
		case channelui.OfficeAppSkills:
			lines = channelui.BuildSkillLines(m.skills, contentWidth)
		default:
			lines = m.buildOfficeFeedLines(contentWidth)
		}
	}

	channelRenderCache.putMainLines(key, lines)
	return channelui.CloneRenderedLines(lines)
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
	h.addInt64(channelui.RenderTimeBucket(m.activeApp, m.isOneOnOne()))

	if m.isOneOnOne() || m.activeApp == channelui.OfficeAppMessages || m.activeApp == channelui.OfficeAppInbox || m.activeApp == channelui.OfficeAppOutbox || m.activeApp == channelui.OfficeAppRecovery {
		workspace := m.currentWorkspaceUIState()
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
		h.add(string(workspace.Readiness.Level), workspace.Readiness.Headline, workspace.Readiness.Detail, workspace.Readiness.NextStep)
		h.add(workspace.Focus, workspace.NextStep)
		if workspace.NeedsYou != nil {
			h.add(workspace.NeedsYou.ID, workspace.NeedsYou.TitleOrQuestion(), workspace.NeedsYou.Status)
		}
		return h.sum()
	}

	switch m.activeApp {
	case channelui.OfficeAppInbox:
		h.addMessages(m.messages)
		h.addRequests(m.requests)
	case channelui.OfficeAppOutbox:
		h.addMessages(m.messages)
		h.addActions(m.actions)
	case channelui.OfficeAppTasks:
		h.addTasks(m.tasks)
	case channelui.OfficeAppRequests:
		h.addRequests(m.requests)
	case channelui.OfficeAppPolicies:
		h.addSignals(m.signals)
		h.addDecisions(m.decisions)
		h.addWatchdogs(m.watchdogs)
		h.addActions(m.actions)
	case channelui.OfficeAppCalendar:
		h.addActions(m.actions)
		h.addScheduler(m.scheduler)
		h.addTasks(m.tasks)
		h.addRequests(m.requests)
		h.addMembers(m.members)
	case channelui.OfficeAppArtifacts:
		h.addTasks(m.tasks)
		h.addRequests(m.requests)
		h.addActions(m.actions)
		h.addInt64(time.Now().Unix() / 10)
	case channelui.OfficeAppSkills:
		h.addSkills(m.skills)
	}
	return h.sum()
}

func hashSidebarState(channels []channelui.ChannelInfo, members []channelui.Member, tasks []channelui.Task, activeChannel string, activeApp channelui.OfficeApp, cursor int, rosterOffset int, focused bool, quickJump quickJumpTarget, workspace channelui.WorkspaceUIState, width, height int) uint64 {
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
	h.add(workspace.Memory.SelectedKind, workspace.Memory.SelectedLabel, workspace.Memory.ActiveKind, workspace.Memory.ActiveLabel, workspace.Memory.Detail, workspace.Memory.NextStep)
	h.add(string(workspace.Readiness.Level), workspace.Readiness.Headline, workspace.Readiness.Detail, workspace.Readiness.NextStep)
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

func (c *channelRenderCacheStore) getMainLines(key uint64) ([]channelui.RenderedLine, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lines, ok := c.mainLines[key]
	if !ok {
		return nil, false
	}
	return channelui.CloneRenderedLines(lines), true
}

func (c *channelRenderCacheStore) putMainLines(key uint64, lines []channelui.RenderedLine) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.mainLines) >= mainLinesCacheLimit {
		c.mainLines = make(map[uint64][]channelui.RenderedLine)
	}
	c.mainLines[key] = channelui.CloneRenderedLines(lines)
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

func (c *channelRenderCacheStore) getThreaded(key uint64) ([]channelui.ThreadedMessage, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	items, ok := c.threaded[key]
	if !ok {
		return nil, false
	}
	return channelui.CloneThreadedMessages(items), true
}

func (c *channelRenderCacheStore) putThreaded(key uint64, items []channelui.ThreadedMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.threaded) >= threadedCacheLimit {
		c.threaded = make(map[uint64][]channelui.ThreadedMessage)
	}
	c.threaded[key] = channelui.CloneThreadedMessages(items)
}

func (c *channelRenderCacheStore) getViewportBlock(key uint64) ([]channelui.RenderedLine, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	lines, ok := c.blocks[key]
	if !ok {
		return nil, false
	}
	return channelui.CloneRenderedLines(lines), true
}

func (c *channelRenderCacheStore) putViewportBlock(key uint64, lines []channelui.RenderedLine) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.blocks) >= viewportBlockLimit {
		c.blocks = make(map[uint64][]channelui.RenderedLine)
	}
	c.blocks[key] = channelui.CloneRenderedLines(lines)
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

func (s stateHasher) addMessages(messages []channelui.BrokerMessage) {
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

func (s stateHasher) addMembers(members []channelui.Member) {
	s.addInt(len(members))
	for _, member := range members {
		s.add(member.Slug, member.Name, member.Role, member.LastMessage, member.LastTime, member.LiveActivity)
		s.addBool(member.Disabled)
	}
}

func (s stateHasher) addChannels(channels []channelui.ChannelInfo) {
	s.addInt(len(channels))
	for _, channel := range channels {
		s.add(channel.Slug, channel.Name, channel.Description)
		s.add(strings.Join(channel.Members, ","))
		s.add(strings.Join(channel.Disabled, ","))
	}
}

func (s stateHasher) addTasks(tasks []channelui.Task) {
	s.addInt(len(tasks))
	for _, task := range tasks {
		s.add(task.ID, task.Channel, task.Title, task.Owner, task.Status, task.TaskType, task.PipelineStage, task.ExecutionMode, task.ReviewState, task.DueAt, task.UpdatedAt)
		s.addTaskMemoryWorkflow(task.MemoryWorkflow)
	}
}

func (s stateHasher) addTaskMemoryWorkflow(workflow *channelui.TaskMemoryWorkflow) {
	s.addBool(workflow != nil)
	if workflow == nil {
		return
	}
	s.add(workflow.Status, workflow.RequirementReason, workflow.CreatedAt, workflow.UpdatedAt, workflow.CompletedAt)
	s.addBool(workflow.Required)
	s.add(strings.Join(workflow.RequiredSteps, ","))
	s.addTaskMemoryWorkflowStep(workflow.Lookup)
	s.addTaskMemoryWorkflowStep(workflow.Capture)
	s.addTaskMemoryWorkflowStep(workflow.Promote)
	s.addInt(len(workflow.Citations))
	for _, citation := range workflow.Citations {
		s.add(
			citation.Backend,
			citation.Source,
			citation.SourceID,
			citation.Path,
			citation.PageID,
			citation.ChunkID,
			citation.SourceURL,
			citation.Title,
			citation.Snippet,
			citation.RetrievedAt,
		)
		s.addInt(citation.LineStart)
		s.addInt(citation.LineEnd)
		s.addBool(citation.Score != nil)
		if citation.Score != nil {
			s.add(fmt.Sprintf("%g", *citation.Score))
		}
		s.addBool(citation.Stale != nil)
		if citation.Stale != nil {
			s.addBool(*citation.Stale)
		}
	}
	s.addTaskMemoryWorkflowArtifacts(workflow.Captures)
	s.addTaskMemoryWorkflowArtifacts(workflow.Promotions)
	s.addBool(workflow.Override != nil)
	if workflow.Override != nil {
		s.add(workflow.Override.Actor, workflow.Override.Reason, workflow.Override.Timestamp)
	}
	s.addInt(len(workflow.PartialErrors))
	for _, partialErr := range workflow.PartialErrors {
		s.add(partialErr.Step, partialErr.Code, partialErr.Message, partialErr.Detail, partialErr.Timestamp)
	}
}

func (s stateHasher) addTaskMemoryWorkflowStep(step channelui.TaskMemoryWorkflowStepState) {
	s.add(step.Status, step.Actor, step.Query, step.CompletedAt, step.UpdatedAt)
	s.addBool(step.Required)
	s.addInt(step.Count)
}

func (s stateHasher) addTaskMemoryWorkflowArtifacts(artifacts []channelui.TaskMemoryWorkflowArtifact) {
	s.addInt(len(artifacts))
	for _, artifact := range artifacts {
		s.add(
			artifact.Backend,
			artifact.Source,
			artifact.Path,
			artifact.PageID,
			artifact.PromotionID,
			artifact.EntityKind,
			artifact.EntitySlug,
			artifact.PlaybookSlug,
			artifact.Title,
			artifact.SkipReason,
			artifact.Snippet,
			artifact.CommitSHA,
			artifact.State,
			artifact.RecordedAt,
			artifact.UpdatedAt,
		)
		s.addBool(artifact.Missing)
	}
}

func (s stateHasher) addActions(actions []channelui.Action) {
	s.addInt(len(actions))
	for _, action := range actions {
		s.add(action.ID, action.Kind, action.Source, action.Channel, action.Actor, action.Summary, action.RelatedID, action.DecisionID, action.CreatedAt)
		s.add(strings.Join(action.SignalIDs, ","))
	}
}

func (s stateHasher) addRequests(requests []channelui.Interview) {
	s.addInt(len(requests))
	for _, req := range requests {
		s.add(req.ID, req.Kind, req.Status, req.From, req.Channel, req.Title, req.Question, req.Context, req.RecommendedID, req.ReplyTo, req.CreatedAt, req.DueAt, req.FollowUpAt, req.ReminderAt, req.RecheckAt)
		s.addBool(req.Blocking)
		s.addBool(req.Required)
		s.addBool(req.Secret)
		s.addBool(req.Redacted)
		s.addInt(req.RedactionCount)
		for _, reason := range req.RedactionReasons {
			s.add(reason)
		}
		for _, opt := range req.Options {
			s.add(opt.ID, opt.Label, opt.Description)
		}
	}
}

func (s stateHasher) addDecisions(decisions []channelui.Decision) {
	s.addInt(len(decisions))
	for _, decision := range decisions {
		s.add(decision.ID, decision.Kind, decision.Channel, decision.Summary, decision.Reason, decision.Owner, decision.CreatedAt)
		s.addBool(decision.RequiresHuman)
		s.addBool(decision.Blocking)
		s.add(strings.Join(decision.SignalIDs, ","))
	}
}

func (s stateHasher) addSignals(signals []channelui.Signal) {
	s.addInt(len(signals))
	for _, signal := range signals {
		s.add(signal.ID, signal.Source, signal.SourceRef, signal.Kind, signal.Title, signal.Content, signal.Channel, signal.Owner, signal.Confidence, signal.Urgency, signal.DedupeKey, signal.CreatedAt)
		s.addBool(signal.RequiresHuman)
		s.addBool(signal.Blocking)
	}
}

func (s stateHasher) addWatchdogs(alerts []channelui.Watchdog) {
	s.addInt(len(alerts))
	for _, alert := range alerts {
		s.add(alert.ID, alert.Kind, alert.Channel, alert.TargetType, alert.TargetID, alert.Owner, alert.Status, alert.Summary, alert.CreatedAt, alert.UpdatedAt)
	}
}

func (s stateHasher) addScheduler(jobs []channelui.SchedulerJob) {
	s.addInt(len(jobs))
	for _, job := range jobs {
		s.add(job.Slug, job.Label, job.Kind, job.TargetType, job.TargetID, job.Channel, job.Provider, job.ScheduleExpr, job.WorkflowKey, job.SkillName, job.DueAt, job.NextRun, job.LastRun, job.Status)
		s.addInt(job.IntervalMinutes)
	}
}

func (s stateHasher) addSkills(skills []channelui.Skill) {
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
