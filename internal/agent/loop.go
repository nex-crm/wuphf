package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EventName identifies an agent loop event.
type EventName string

const (
	EventPhaseChange EventName = "phase_change"
	EventToolCall    EventName = "tool_call"
	EventMessage     EventName = "message"
	EventError       EventName = "error"
	EventDone        EventName = "done"
	EventThinking    EventName = "thinking"
	EventToolUse     EventName = "tool_use"
	EventToolResult  EventName = "tool_result"
)

const (
	defaultMaxStuckTicks   = 20
	defaultMaxErrorRetries = 3
)

// EventHandler is a callback for agent loop events.
type EventHandler func(args ...any)

// AgentLoop is the core state machine for agent execution.
type AgentLoop struct {
	state              AgentState
	tools              *ToolRegistry
	sessions           *SessionStore
	queues             *MessageQueues
	streamFn           StreamFn
	gossipLayer        *GossipLayer
	credibilityTracker *CredibilityTracker

	running           bool
	paused            bool
	eventHandlers     map[EventName][]EventHandler
	pendingToolCall   *ToolCall
	cancelFunc        context.CancelFunc
	taskHadError      bool
	collectedInsights []string
	taskLogRoot       string
	lastCompactionAt  int

	// Stuck detection and retry cap.
	lastPhase       AgentPhase
	stuckTicks      int
	errorCount      int
	errorTaskID     string
	escalator       Escalator
	maxStuckTicks   int
	maxErrorRetries int

	mu sync.Mutex
}

// NewAgentLoop creates a new agent loop with the given dependencies.
// gossipLayer and credibilityTracker may be nil.
func NewAgentLoop(
	config AgentConfig,
	tools *ToolRegistry,
	sessions *SessionStore,
	queues *MessageQueues,
	streamFn StreamFn,
	gossipLayer *GossipLayer,
	credibilityTracker *CredibilityTracker,
) *AgentLoop {
	return &AgentLoop{
		state: AgentState{
			Phase:  PhaseIdle,
			Config: config,
		},
		tools:              tools,
		sessions:           sessions,
		queues:             queues,
		streamFn:           streamFn,
		gossipLayer:        gossipLayer,
		credibilityTracker: credibilityTracker,
		eventHandlers:      make(map[EventName][]EventHandler),
		taskLogRoot:        defaultTaskLogRoot(),
	}
}

// On registers an event handler for the given event.
func (l *AgentLoop) On(event EventName, handler EventHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.eventHandlers[event] = append(l.eventHandlers[event], handler)
}

// Off removes the given handler from the event. Comparison is by pointer.
func (l *AgentLoop) Off(event EventName, handler EventHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	handlers := l.eventHandlers[event]
	target := fmt.Sprintf("%p", handler)
	for i, h := range handlers {
		if fmt.Sprintf("%p", h) == target {
			l.eventHandlers[event] = append(handlers[:i], handlers[i+1:]...)
			return
		}
	}
}

// GetState returns a copy of the current agent state.
func (l *AgentLoop) GetState() AgentState {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

// appendSession writes a session journal entry and logs (rather than drops)
// any error. Best-effort: we never want a journal-write failure to stop an
// agent turn, but silently swallowing the error has historically hidden real
// corruption. Logging preserves "don't crash the loop" while making drift
// debuggable after the fact.
func (l *AgentLoop) appendSession(entry SessionEntry) {
	if _, err := l.sessions.Append(l.state.SessionID, entry); err != nil {
		log.Printf("agent loop: session append (session=%s type=%s): %v",
			l.state.SessionID, entry.Type, err)
	}
}

// CanProcess reports whether the loop is started and not paused.
func (l *AgentLoop) CanProcess() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.running && !l.paused
}

// IsBusy reports whether the loop is actively processing a turn.
func (l *AgentLoop) IsBusy() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch l.state.Phase {
	case PhaseBuildContext, PhaseStreamLLM, PhaseExecuteTool:
		return l.running && !l.paused
	default:
		return false
	}
}

// Interrupt cancels in-flight provider or tool work so newer queued work can start.
func (l *AgentLoop) Interrupt() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.cancelFunc == nil {
		return false
	}
	l.cancelFunc()
	l.cancelFunc = nil
	return true
}

// Start marks the loop as running and sets phase to idle.
func (l *AgentLoop) Start() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.running = true
	l.paused = false
	l.setPhase(PhaseIdle)
}

// Stop cancels any in-flight context and marks the loop as not running.
func (l *AgentLoop) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.running = false
	if l.cancelFunc != nil {
		l.cancelFunc()
		l.cancelFunc = nil
	}
}

// Pause pauses tick processing without stopping the loop.
func (l *AgentLoop) Pause() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paused = true
}

// Resume unpauses tick processing.
func (l *AgentLoop) Resume() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paused = false
}

// AddInsight records an insight to be published via gossip when the task completes.
func (l *AgentLoop) AddInsight(insight string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.collectedInsights = append(l.collectedInsights, insight)
}

// Tick advances the state machine by one step. Called by the service's tick loop.
func (l *AgentLoop) Tick() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.paused || !l.running {
		return nil
	}

	switch l.state.Phase {
	case PhaseIdle:
		return l.buildContext()
	case PhaseBuildContext:
		return l.streamLLM()
	case PhaseStreamLLM:
		if l.pendingToolCall != nil {
			return l.executeTool()
		}
		return l.handleDone()
	case PhaseExecuteTool:
		return l.streamLLM()
	case PhaseDone:
		return l.handleDone()
	case PhaseError:
		taskID := strings.TrimSpace(l.state.TaskID)
		errMsg := l.state.Error

		// Track retries per-task. Reset counter when we see a fresh task id.
		if taskID != l.errorTaskID {
			l.errorTaskID = taskID
			l.errorCount = 0
		}
		l.errorCount++

		if l.errorCount >= l.resolveMaxErrorRetries() {
			if l.escalator != nil {
				// Capture locals before releasing the lock so the callback can
				// safely call back into the broker without deadlocking.
				escalator := l.escalator
				slug := l.state.Config.Slug
				detail := errMsg
				l.mu.Unlock()
				escalator(slug, taskID, EscalationMaxRetries, detail)
				l.mu.Lock()
			}
			// Reset so a future task starts fresh.
			l.errorTaskID = ""
			l.errorCount = 0
		}

		// Always reset to idle so the agent can process new messages.
		l.state.Error = ""
		l.setPhase(PhaseIdle)
		return nil
	}
	return nil
}

// setPhase updates the phase and emits a phase_change event. Must be called with mu held.
func (l *AgentLoop) setPhase(phase AgentPhase) {
	old := l.state.Phase
	l.state.Phase = phase
	l.emit(EventPhaseChange, old, phase)
}

// emit fires all handlers for the given event. Must be called with mu held.
func (l *AgentLoop) emit(event EventName, args ...any) {
	for _, h := range l.eventHandlers[event] {
		h(args...)
	}
}

// SetEscalator wires a callback for stuck/retry escalation. Safe to call
// before or after Start(); takes effect on the next error or stuck event.
func (l *AgentLoop) SetEscalator(fn Escalator) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.escalator = fn
}

// SetStuckLimits overrides the default thresholds for stuck detection and
// error retries. Pass 0 for either to keep the default.
func (l *AgentLoop) SetStuckLimits(maxStuckTicks, maxErrorRetries int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maxStuckTicks = maxStuckTicks
	l.maxErrorRetries = maxErrorRetries
}

func (l *AgentLoop) resolveMaxStuckTicks() int {
	if l.maxStuckTicks > 0 {
		return l.maxStuckTicks
	}
	return defaultMaxStuckTicks
}

func (l *AgentLoop) resolveMaxErrorRetries() int {
	if l.maxErrorRetries > 0 {
		return l.maxErrorRetries
	}
	return defaultMaxErrorRetries
}

// NotifyTick is called by the worker goroutine (or a test) once per tick so
// the loop can detect when it's stuck in the same phase. Callers should invoke
// this every cycle regardless of whether Tick runs.
func (l *AgentLoop) NotifyTick() {
	l.mu.Lock()
	phase := l.state.Phase
	if phase != l.lastPhase {
		l.lastPhase = phase
		l.stuckTicks = 0
		l.mu.Unlock()
		return
	}

	// Happy-path phases — the agent is allowed to sit there indefinitely.
	if phase == PhaseIdle || phase == PhaseDone {
		l.mu.Unlock()
		return
	}

	l.stuckTicks++
	threshold := l.resolveMaxStuckTicks()
	if l.stuckTicks < threshold {
		l.mu.Unlock()
		return
	}

	escalator := l.escalator
	slug := l.state.Config.Slug
	taskID := l.state.TaskID
	l.stuckTicks = 0 // reset so we don't spam
	l.mu.Unlock()

	if escalator != nil {
		escalator(slug, taskID, EscalationStuck, fmt.Sprintf("agent stuck in %s for %d ticks", phase, threshold))
	}
}

// forcePhaseErrorForTest drives a single PhaseError tick for the given task id.
// Test-only helper; lives here so it has access to unexported fields.
func (l *AgentLoop) forcePhaseErrorForTest(taskID, errMsg string) {
	l.mu.Lock()
	l.state.TaskID = taskID
	l.state.Error = errMsg
	l.setPhase(PhaseError)
	l.mu.Unlock()
	_ = l.Tick()
}

// setPhaseForTest is a test-only accessor that sets the current phase without
// running through Tick. Used by stuck-detection tests to hold the loop in a
// non-idle phase.
func (l *AgentLoop) setPhaseForTest(phase AgentPhase) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.setPhase(phase)
}

// buildContext prepares the session and context for LLM streaming.
func (l *AgentLoop) buildContext() error {
	l.setPhase(PhaseBuildContext)
	l.taskHadError = false
	l.collectedInsights = nil

	slug := l.state.Config.Slug

	// Create session if none exists.
	if l.state.SessionID == "" {
		sessionID, err := l.sessions.Create(slug)
		if err != nil {
			l.state.Error = fmt.Sprintf("create session: %v", err)
			l.setPhase(PhaseError)
			l.emit(EventError, l.state.Error)
			return err
		}
		l.state.SessionID = sessionID
	}

	// Inject system prompt if not already present in session.
	entries, _ := l.sessions.GetHistory(l.state.SessionID, 0, "")
	hasSystem := false
	for _, e := range entries {
		if e.Type == "system" {
			hasSystem = true
			break
		}
	}
	if !hasSystem && l.state.Config.Personality != "" {
		l.appendSession(SessionEntry{
			Type:    "system",
			Content: l.state.Config.Personality,
		})
	}

	// Drain steer messages after the session exists so the first user task is not lost.
	if msg, ok := l.queues.DrainSteer(slug); ok {
		l.appendSession(SessionEntry{
			Type:    "system",
			Content: "[STEER] " + msg,
		})
	}

	// Drain follow-up message and append as user entry.
	if msg, ok := l.queues.DrainFollowUp(slug); ok {
		l.state.CurrentTask = msg
		l.state.TaskID = nextTaskID(slug)
		l.lastCompactionAt = 0
		l.appendSession(SessionEntry{
			Type:    "user",
			Content: msg,
		})
	}

	// Inject gossip insights if gossip layer is available.
	if l.gossipLayer != nil {
		l.injectGossipInsights()
	}

	l.emit(EventThinking, l.progressNote(PhaseBuildContext))

	return nil
}

// injectGossipInsights queries gossip and injects scored insights into the session.
func (l *AgentLoop) injectGossipInsights() {
	slug := l.state.Config.Slug
	// Use the first expertise topic for gossip queries.
	topic := slug
	if len(l.state.Config.Expertise) > 0 {
		topic = l.state.Config.Expertise[0]
	}

	insights, err := l.gossipLayer.Query(slug, topic)
	if err != nil {
		return // Gossip errors are non-fatal.
	}

	for _, insight := range insights {
		var srcCred *float64
		if l.credibilityTracker != nil && insight.Source != "" {
			c := l.credibilityTracker.GetCredibility(insight.Source)
			srcCred = &c
		}

		score := ScoreInsight(insight, "", srcCred)
		switch score.Decision {
		case "adopt":
			l.appendSession(SessionEntry{
				Type:    "system",
				Content: fmt.Sprintf("[GOSSIP:ADOPTED] (from %s, score=%.2f) %s", insight.Source, score.Total, insight.Content),
			})
		case "test":
			l.appendSession(SessionEntry{
				Type:    "system",
				Content: fmt.Sprintf("[GOSSIP:TEST] (from %s, score=%.2f) %s", insight.Source, score.Total, insight.Content),
			})
		}
		// "reject" is silently dropped.
	}
}

// streamLLM streams output from the LLM and processes chunks.
func (l *AgentLoop) streamLLM() error {
	l.setPhase(PhaseStreamLLM)
	l.emit(EventThinking, l.progressNote(PhaseStreamLLM))

	// Get session history and convert to messages.
	entries, err := l.sessions.GetHistory(l.state.SessionID, 0, "")
	if err != nil {
		l.state.Error = fmt.Sprintf("get history: %v", err)
		l.setPhase(PhaseError)
		l.emit(EventError, l.state.Error)
		return err
	}
	entries = l.prepareEntriesForStreaming(entries)

	messages := entriesToMessages(entries)

	// If no messages, inject system message with agent personality.
	if len(messages) == 0 {
		personality := l.state.Config.Personality
		if personality == "" {
			personality = fmt.Sprintf("You are %s, an AI agent.", l.state.Config.Name)
		}
		messages = []Message{{Role: "system", Content: personality}}
	}

	// Create cancellable context.
	ctx, cancel := context.WithCancel(context.Background())
	l.cancelFunc = cancel

	// Filter tools by agent config's tool list.
	var allowedTools []AgentTool
	if len(l.state.Config.Tools) > 0 {
		for _, name := range l.state.Config.Tools {
			if tool, ok := l.tools.Get(name); ok {
				allowedTools = append(allowedTools, tool)
			}
		}
	} else {
		allowedTools = l.tools.List()
	}

	// Call streamFn — unlock mu while waiting on channel to avoid deadlock.
	ch := l.streamFn(messages, allowedTools)

	var fullText strings.Builder
	for chunk := range ch {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				l.pendingToolCall = nil
				l.setPhase(PhaseIdle)
				return nil
			}
			return ctx.Err()
		default:
		}

		switch chunk.Type {
		case "text":
			fullText.WriteString(chunk.Content)
			l.emit(EventMessage, chunk.Content)
		case "thinking":
			l.emit(EventThinking, chunk.Content)
		case "tool_use":
			l.emit(EventToolUse, chunk.ToolName, chunk.ToolInput)
		case "tool_result":
			l.emit(EventToolResult, chunk.Content)
		case "tool_call":
			l.pendingToolCall = &ToolCall{
				ToolName:  chunk.ToolName,
				Params:    chunk.ToolParams,
				StartedAt: time.Now().UnixMilli(),
			}
			// Stop reading — tool needs to execute before continuing.
			goto done
		case "error":
			l.state.Error = chunk.Content
			l.setPhase(PhaseError)
			l.emit(EventError, chunk.Content)
			return fmt.Errorf("provider error: %s", chunk.Content)
		}
	}

done:
	if errors.Is(ctx.Err(), context.Canceled) {
		l.pendingToolCall = nil
		l.setPhase(PhaseIdle)
		return nil
	}
	// Append assistant text to session.
	if fullText.Len() > 0 {
		l.appendSession(SessionEntry{
			Type:    "assistant",
			Content: fullText.String(),
		})
	}

	return nil
}

// executeTool runs the pending tool call and records results in the session.
func (l *AgentLoop) executeTool() error {
	if l.pendingToolCall == nil {
		return nil
	}

	l.setPhase(PhaseExecuteTool)
	l.emit(EventThinking, l.progressNote(PhaseExecuteTool))
	tc := l.pendingToolCall

	l.emit(EventToolCall, tc.ToolName, tc.Params)

	// Lookup and validate.
	tool, ok := l.tools.Get(tc.ToolName)
	if !ok {
		errMsg := fmt.Sprintf("unknown tool: %q", tc.ToolName)
		l.appendSession(SessionEntry{
			Type:    "tool_result",
			Content: errMsg,
			Metadata: map[string]any{
				"toolName": tc.ToolName,
				"error":    true,
			},
		})
		l.taskHadError = true
		l.pendingToolCall = nil
		return nil
	}

	if valid, errs := l.tools.Validate(tc.ToolName, tc.Params); !valid {
		errMsg := fmt.Sprintf("invalid params for %q: %s", tc.ToolName, strings.Join(errs, "; "))
		l.appendSession(SessionEntry{
			Type:    "tool_result",
			Content: errMsg,
			Metadata: map[string]any{
				"toolName": tc.ToolName,
				"error":    true,
			},
		})
		l.taskHadError = true
		l.pendingToolCall = nil
		return nil
	}

	// Append tool_call entry.
	l.appendSession(SessionEntry{
		Type:    "tool_call",
		Content: tc.ToolName,
		Metadata: map[string]any{
			"toolName": tc.ToolName,
			"params":   tc.Params,
		},
	})

	// Execute tool.
	ctx := context.Background()
	if l.cancelFunc != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		l.cancelFunc = cancel
	}

	result, err := tool.Execute(tc.Params, ctx, func(s string) {})
	tc.CompletedAt = time.Now().UnixMilli()

	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		l.pendingToolCall = nil
		l.setPhase(PhaseIdle)
		return nil
	}

	if err != nil {
		tc.Error = err.Error()
		l.emit(EventToolResult, err.Error())
		l.appendSession(SessionEntry{
			Type:    "tool_result",
			Content: err.Error(),
			Metadata: map[string]any{
				"toolName": tc.ToolName,
				"error":    true,
			},
		})
		l.taskHadError = true
	} else {
		tc.Result = result
		l.emit(EventToolResult, result)
		l.appendSession(SessionEntry{
			Type:    "tool_result",
			Content: result,
			Metadata: map[string]any{
				"toolName": tc.ToolName,
			},
		})

		// Collect gossip_publish insights.
		if tc.ToolName == "nex_gossip_publish" {
			if insight, ok := tc.Params["insight"].(string); ok {
				l.collectedInsights = append(l.collectedInsights, insight)
			}
		}
	}
	l.logToolExecution(*tc)

	l.pendingToolCall = nil
	return nil
}

// handleDone finishes the current task cycle.
func (l *AgentLoop) handleDone() error {
	slug := l.state.Config.Slug

	// If queues have more messages, go back to idle for another cycle.
	if l.queues.HasMessages(slug) {
		l.setPhase(PhaseIdle)
		return nil
	}

	// Publish collected insights via gossip.
	if l.gossipLayer != nil && len(l.collectedInsights) > 0 {
		for _, insight := range l.collectedInsights {
			if _, err := l.gossipLayer.Publish(slug, insight, ""); err != nil {
				log.Printf("agent loop: gossip publish (slug=%s): %v", slug, err)
			}
		}
		l.collectedInsights = nil
	}

	// Record outcome in credibility tracker.
	if l.credibilityTracker != nil {
		l.credibilityTracker.RecordOutcome(slug, !l.taskHadError)
	}

	l.state.CurrentTask = ""
	l.state.TaskID = ""
	l.setPhase(PhaseDone)
	l.emit(EventDone)
	return nil
}

func (l *AgentLoop) progressNote(phase AgentPhase) string {
	name := l.state.Config.Name
	task := strings.TrimSpace(l.state.CurrentTask)
	task = summarizeProgressTask(task)

	switch phase {
	case PhaseBuildContext:
		if task != "" {
			return fmt.Sprintf("%s is reviewing the task: %s", name, task)
		}
		return fmt.Sprintf("%s is reviewing the latest task.", name)
	case PhaseStreamLLM:
		if l.state.Config.Slug == "ceo" || strings.Contains(strings.ToLower(strings.Join(l.state.Config.Expertise, " ")), "delegation") {
			return fmt.Sprintf("%s is coordinating the next move.", name)
		}
		if task != "" {
			return fmt.Sprintf("%s is working on: %s", name, task)
		}
		return fmt.Sprintf("%s is drafting a response.", name)
	case PhaseExecuteTool:
		if l.pendingToolCall != nil && l.pendingToolCall.ToolName != "" {
			return fmt.Sprintf("%s is using %s.", name, l.pendingToolCall.ToolName)
		}
		return fmt.Sprintf("%s is using tools.", name)
	default:
		return ""
	}
}

func summarizeProgressTask(task string) string {
	task = strings.TrimSpace(task)
	if task == "" {
		return ""
	}
	if len(task) <= 72 {
		return task
	}
	cut := task[:72]
	if idx := strings.LastIndex(cut, " "); idx > 36 {
		cut = cut[:idx]
	}
	return strings.TrimSpace(cut) + "..."
}

// entriesToMessages converts session entries into LLM messages.
func entriesToMessages(entries []SessionEntry) []Message {
	var msgs []Message
	for _, e := range entries {
		switch e.Type {
		case "user":
			msgs = append(msgs, Message{Role: "user", Content: e.Content})
		case "assistant":
			msgs = append(msgs, Message{Role: "assistant", Content: e.Content})
		case "system":
			msgs = append(msgs, Message{Role: "system", Content: e.Content})
		case "tool_call":
			msgs = append(msgs, Message{Role: "assistant", Content: "[tool_call] " + e.Content})
		case "tool_result":
			msgs = append(msgs, Message{Role: "user", Content: "[tool_result] " + e.Content})
		}
	}
	return msgs
}

func (l *AgentLoop) prepareEntriesForStreaming(entries []SessionEntry) []SessionEntry {
	if !shouldCompactEntries(entries, l.state.Config.Slug) {
		l.lastCompactionAt = 0
		return entries
	}
	if l.lastCompactionAt == len(entries) {
		return entries
	}

	prefix, archived, recent := splitEntriesForCompaction(entries)
	summary := buildOfficeInsightSummary(archived)
	if len(archived) == 0 || strings.TrimSpace(summary) == "" {
		return entries
	}

	summaryEntry := SessionEntry{
		Type:    "system",
		Content: summary,
		Metadata: map[string]any{
			"officeInsight":   true,
			"archivedEntries": len(archived),
			"taskId":          l.state.TaskID,
		},
	}
	if stored, err := l.sessions.Append(l.state.SessionID, summaryEntry); err == nil {
		summaryEntry = stored
	}

	l.lastCompactionAt = len(entries)
	l.emit(EventThinking, "Context nearing capacity; archived older context into an Office Insight.")
	l.rememberOfficeInsight(summary)

	compacted := make([]SessionEntry, 0, len(prefix)+1+len(recent))
	compacted = append(compacted, prefix...)
	compacted = append(compacted, summaryEntry)
	compacted = append(compacted, recent...)
	return compacted
}

func (l *AgentLoop) rememberOfficeInsight(summary string) {
	tool, ok := l.tools.Get("nex_remember")
	if !ok {
		return
	}

	content := strings.TrimSpace(summary)
	if content == "" {
		return
	}

	go func() {
		_, _ = tool.Execute(map[string]any{
			"content": content,
			"tags":    []string{"office-insight", "compaction"},
		}, context.Background(), func(string) {})
	}()
}

func (l *AgentLoop) logToolExecution(call ToolCall) {
	taskID := strings.TrimSpace(l.state.TaskID)
	if taskID == "" {
		taskID = "adhoc"
	}

	dir := filepath.Join(l.taskLogRoot, taskID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}

	path := filepath.Join(dir, "output.log")
	record := map[string]any{
		"task_id":      taskID,
		"agent_slug":   l.state.Config.Slug,
		"tool_name":    call.ToolName,
		"params":       call.Params,
		"result":       call.Result,
		"error":        call.Error,
		"started_at":   call.StartedAt,
		"completed_at": call.CompletedAt,
	}
	line, err := json.Marshal(record)
	if err != nil {
		return
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	_, _ = f.Write(append(line, '\n'))
}
