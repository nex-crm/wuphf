package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockStreamFn returns a StreamFn that yields a single text chunk.
func mockStreamFn(response string) StreamFn {
	return func(msgs []Message, tools []AgentTool) <-chan StreamChunk {
		ch := make(chan StreamChunk, 1)
		go func() {
			defer close(ch)
			ch <- StreamChunk{Type: "text", Content: response}
		}()
		return ch
	}
}

// mockToolCallStreamFn returns a StreamFn that yields a tool_call chunk.
func mockToolCallStreamFn(toolName string, params map[string]any) StreamFn {
	calls := 0
	var mu sync.Mutex
	return func(msgs []Message, tools []AgentTool) <-chan StreamChunk {
		ch := make(chan StreamChunk, 1)
		mu.Lock()
		c := calls
		calls++
		mu.Unlock()
		go func() {
			defer close(ch)
			if c == 0 {
				ch <- StreamChunk{Type: "tool_call", ToolName: toolName, ToolParams: params}
			} else {
				ch <- StreamChunk{Type: "text", Content: "done after tool"}
			}
		}()
		return ch
	}
}

func newTestLoop(t *testing.T, streamFn StreamFn) (*AgentLoop, string) {
	t.Helper()
	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	tools := NewToolRegistry()
	queues := NewMessageQueues()

	config := AgentConfig{
		Slug:        "test-agent",
		Name:        "Test Agent",
		Expertise:   []string{"testing"},
		Personality: "You are a test agent.",
	}

	loop := NewAgentLoop(config, tools, sessions, queues, streamFn, nil, nil)
	return loop, dir
}

func TestFullTickCycle(t *testing.T) {
	loop, _ := newTestLoop(t, mockStreamFn("hello world"))

	// Track events.
	var events []EventName
	var mu sync.Mutex
	handler := func(args ...any) {
		// EventHandler is called with mu held by Tick, so we use a separate lock.
	}
	_ = handler

	var phaseChanges []AgentPhase
	loop.On(EventPhaseChange, func(args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if len(args) >= 2 {
			if p, ok := args[1].(AgentPhase); ok {
				phaseChanges = append(phaseChanges, p)
			}
		}
	})

	var messages []string
	loop.On(EventMessage, func(args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if len(args) >= 1 {
			if s, ok := args[0].(string); ok {
				messages = append(messages, s)
			}
		}
	})

	doneCalled := false
	loop.On(EventDone, func(args ...any) {
		mu.Lock()
		defer mu.Unlock()
		doneCalled = true
		events = append(events, EventDone)
	})

	// Enqueue a follow-up message so there's user input.
	loop.queues.FollowUp("test-agent", "run the test")

	loop.Start()

	// Tick 1: idle → build_context (creates session, drains follow-up)
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	state := loop.GetState()
	if state.Phase != PhaseBuildContext {
		t.Fatalf("after tick 1: expected build_context, got %s", state.Phase)
	}
	if state.SessionID == "" {
		t.Fatal("session ID should be set after build_context")
	}

	// Tick 2: build_context → stream_llm (streams LLM response)
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	state = loop.GetState()
	if state.Phase != PhaseStreamLLM {
		t.Fatalf("after tick 2: expected stream_llm, got %s", state.Phase)
	}

	// Tick 3: stream_llm (no tool call) → done
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 3: %v", err)
	}
	state = loop.GetState()
	if state.Phase != PhaseDone {
		t.Fatalf("after tick 3: expected done, got %s", state.Phase)
	}

	mu.Lock()
	if !doneCalled {
		t.Error("EventDone should have been emitted")
	}
	if len(messages) == 0 {
		t.Error("expected at least one message event")
	}
	if messages[0] != "hello world" {
		t.Errorf("expected message 'hello world', got %q", messages[0])
	}
	mu.Unlock()

	// Verify session has entries.
	entries, err := loop.sessions.GetHistory(state.SessionID, 0, "")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 session entries (user + assistant), got %d", len(entries))
	}

	// Check entry types.
	hasUser := false
	hasAssistant := false
	for _, e := range entries {
		if e.Type == "user" {
			hasUser = true
		}
		if e.Type == "assistant" {
			hasAssistant = true
		}
	}
	if !hasUser {
		t.Error("session should have a user entry")
	}
	if !hasAssistant {
		t.Error("session should have an assistant entry")
	}
}

func TestSteerInjectsSystemMessage(t *testing.T) {
	loop, _ := newTestLoop(t, mockStreamFn("ack"))

	loop.queues.FollowUp("test-agent", "start task")
	loop.queues.Steer("test-agent", "change priority to high")

	loop.Start()

	// Tick 1: idle → build_context (creates session and drains steer immediately)
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 1: %v", err)
	}

	state := loop.GetState()
	entries, err := loop.sessions.GetHistory(state.SessionID, 0, "")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.Type == "system" && e.Content == "[STEER] change priority to high" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected steer message as system entry in session")
		for _, e := range entries {
			t.Logf("  entry: type=%s content=%q", e.Type, e.Content)
		}
	}
}

func TestSteerOnlyMessageSurvivesFirstTick(t *testing.T) {
	loop, _ := newTestLoop(t, mockStreamFn("ack"))
	loop.queues.Steer("test-agent", "do the urgent thing")
	loop.Start()

	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 1: %v", err)
	}

	state := loop.GetState()
	entries, err := loop.sessions.GetHistory(state.SessionID, 0, "")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.Type == "system" && e.Content == "[STEER] do the urgent thing" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected steer-only message to survive first tick")
	}
}

func TestToolCallCycle(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	tools := NewToolRegistry()
	queues := NewMessageQueues()

	// Register a test tool.
	tools.Register(AgentTool{
		Name:        "echo",
		Description: "Echoes input",
		Schema: map[string]any{
			"type":     "object",
			"required": []any{"text"},
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
		},
		Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
			text, _ := params["text"].(string)
			return "echo: " + text, nil
		},
	})

	config := AgentConfig{
		Slug:        "test-agent",
		Name:        "Test Agent",
		Expertise:   []string{"testing"},
		Personality: "You are a test agent.",
		Tools:       []string{"echo"},
	}

	streamFn := mockToolCallStreamFn("echo", map[string]any{"text": "hi"})
	loop := NewAgentLoop(config, tools, sessions, queues, streamFn, nil, nil)

	var toolCalls []string
	loop.On(EventToolCall, func(args ...any) {
		if len(args) >= 1 {
			if name, ok := args[0].(string); ok {
				toolCalls = append(toolCalls, name)
			}
		}
	})

	doneCalled := false
	loop.On(EventDone, func(args ...any) {
		doneCalled = true
	})

	queues.FollowUp("test-agent", "call echo")
	loop.Start()

	// Tick 1: idle → build_context
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	state := loop.GetState()
	if state.TaskID == "" {
		t.Fatal("expected task ID after follow-up was drained")
	}

	// Tick 2: build_context → stream_llm (yields tool_call)
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	state = loop.GetState()
	if state.Phase != PhaseStreamLLM {
		t.Fatalf("expected stream_llm, got %s", state.Phase)
	}

	// Tick 3: stream_llm with pending tool → execute_tool
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 3: %v", err)
	}
	state = loop.GetState()
	if state.Phase != PhaseExecuteTool {
		t.Fatalf("expected execute_tool, got %s", state.Phase)
	}

	// Tick 4: execute_tool → stream_llm (second call yields text)
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 4: %v", err)
	}
	state = loop.GetState()
	if state.Phase != PhaseStreamLLM {
		t.Fatalf("expected stream_llm, got %s", state.Phase)
	}

	// Tick 5: stream_llm (no tool) → done
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 5: %v", err)
	}
	state = loop.GetState()
	if state.Phase != PhaseDone {
		t.Fatalf("expected done, got %s", state.Phase)
	}

	if len(toolCalls) != 1 || toolCalls[0] != "echo" {
		t.Errorf("expected one tool call to 'echo', got %v", toolCalls)
	}
	if !doneCalled {
		t.Error("expected done event")
	}

	// Verify session has tool_call and tool_result entries.
	entries, err := sessions.GetHistory(state.SessionID, 0, "")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}
	hasToolCall := false
	hasToolResult := false
	for _, e := range entries {
		if e.Type == "tool_call" {
			hasToolCall = true
		}
		if e.Type == "tool_result" && e.Content == "echo: hi" {
			hasToolResult = true
		}
	}
	if !hasToolCall {
		t.Error("expected tool_call entry in session")
	}
	if !hasToolResult {
		t.Error("expected tool_result entry with 'echo: hi'")
	}
}

func TestToolCallCycleWritesTaskOutputLog(t *testing.T) {
	t.Setenv(taskLogRootEnv, t.TempDir())

	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	tools := NewToolRegistry()
	queues := NewMessageQueues()

	tools.Register(AgentTool{
		Name:        "echo",
		Description: "Echoes input",
		Schema: map[string]any{
			"type":     "object",
			"required": []any{"text"},
			"properties": map[string]any{
				"text": map[string]any{"type": "string"},
			},
		},
		Execute: func(params map[string]any, ctx context.Context, onUpdate func(string)) (string, error) {
			return "echo: " + params["text"].(string), nil
		},
	})

	loop := NewAgentLoop(AgentConfig{
		Slug:  "logger",
		Name:  "Logger",
		Tools: []string{"echo"},
	}, tools, sessions, queues, mockToolCallStreamFn("echo", map[string]any{"text": "hi"}), nil, nil)

	queues.FollowUp("logger", "call echo")
	loop.Start()

	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 1: %v", err)
	}
	taskID := loop.GetState().TaskID
	if taskID == "" {
		t.Fatal("expected task ID after build context")
	}

	for i := 0; i < 4; i++ {
		if err := loop.Tick(); err != nil {
			t.Fatalf("tick %d: %v", i+2, err)
		}
	}

	logPath := filepath.Join(os.Getenv(taskLogRootEnv), taskID, "output.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read output log: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, `"tool_name":"echo"`) {
		t.Fatalf("expected tool log to contain tool name, got %q", text)
	}
	if !strings.Contains(text, `"result":"echo: hi"`) {
		t.Fatalf("expected tool log to contain raw result, got %q", text)
	}
}

func TestStreamLLMCompactsOldContext(t *testing.T) {
	t.Setenv(compactionTokenLimitEnv, "80")

	loop, _ := newTestLoop(t, mockStreamFn("ok"))
	loop.queues.FollowUp("test-agent", "summarize prior work")
	loop.Start()

	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 1: %v", err)
	}

	state := loop.GetState()
	for i := 0; i < 12; i++ {
		if _, err := loop.sessions.Append(state.SessionID, SessionEntry{
			Type:    "assistant",
			Content: strings.Repeat("history line ", 12),
		}); err != nil {
			t.Fatalf("append history %d: %v", i, err)
		}
	}

	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 2: %v", err)
	}

	entries, err := loop.sessions.GetHistory(state.SessionID, 0, "")
	if err != nil {
		t.Fatalf("get history: %v", err)
	}

	found := false
	for _, entry := range entries {
		if entry.Type == "system" && strings.Contains(entry.Content, "Office Insight") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected compaction to persist an Office Insight system entry")
	}
}

func TestPauseResume(t *testing.T) {
	loop, _ := newTestLoop(t, mockStreamFn("test"))
	loop.queues.FollowUp("test-agent", "go")
	loop.Start()

	// Pause and verify tick is no-op.
	loop.Pause()
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick while paused: %v", err)
	}
	state := loop.GetState()
	if state.Phase != PhaseIdle {
		t.Fatalf("expected idle while paused, got %s", state.Phase)
	}

	// Resume and tick should advance.
	loop.Resume()
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick after resume: %v", err)
	}
	state = loop.GetState()
	if state.Phase != PhaseBuildContext {
		t.Fatalf("expected build_context after resume, got %s", state.Phase)
	}
}

func TestCredibilityRecordOnDone(t *testing.T) {
	dir := t.TempDir()
	sessions := NewSessionStoreAt(dir)
	tools := NewToolRegistry()
	queues := NewMessageQueues()
	tracker := NewCredibilityTracker(t.TempDir())

	config := AgentConfig{
		Slug:        "cred-agent",
		Name:        "Cred Agent",
		Personality: "You test credibility.",
	}

	loop := NewAgentLoop(config, tools, sessions, queues, mockStreamFn("ok"), nil, tracker)
	queues.FollowUp("cred-agent", "task")
	loop.Start()

	// Run through full cycle.
	for i := 0; i < 4; i++ {
		if err := loop.Tick(); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}

	state := loop.GetState()
	if state.Phase != PhaseDone {
		t.Fatalf("expected done, got %s", state.Phase)
	}

	cred := tracker.GetCredibility("cred-agent")
	if cred != 1.0 {
		t.Errorf("expected credibility 1.0 (success), got %f", cred)
	}
}

// mockErrorStreamFn returns a StreamFn that yields a single error chunk.
func mockErrorStreamFn(errMsg string) StreamFn {
	return func(msgs []Message, tools []AgentTool) <-chan StreamChunk {
		ch := make(chan StreamChunk, 1)
		go func() {
			defer close(ch)
			ch <- StreamChunk{Type: "error", Content: errMsg}
		}()
		return ch
	}
}

func TestStreamLLMErrorChunk(t *testing.T) {
	loop, _ := newTestLoop(t, mockErrorStreamFn("provider exploded"))

	var errorEvents []string
	loop.On(EventError, func(args ...any) {
		if len(args) >= 1 {
			if s, ok := args[0].(string); ok {
				errorEvents = append(errorEvents, s)
			}
		}
	})

	loop.queues.FollowUp("test-agent", "do something")
	loop.Start()

	// Tick 1: idle → build_context
	if err := loop.Tick(); err != nil {
		t.Fatalf("tick 1: %v", err)
	}

	// Tick 2: build_context → stream_llm (error chunk should trigger PhaseError)
	err := loop.Tick()
	if err == nil {
		t.Fatal("tick 2 should have returned an error")
	}
	if err.Error() != "provider error: provider exploded" {
		t.Fatalf("unexpected error: %v", err)
	}

	state := loop.GetState()
	if state.Phase != PhaseError {
		t.Fatalf("expected phase error, got %s", state.Phase)
	}
	if state.Error != "provider exploded" {
		t.Fatalf("expected state.Error = 'provider exploded', got %q", state.Error)
	}
	if len(errorEvents) != 1 || errorEvents[0] != "provider exploded" {
		t.Fatalf("expected one error event with 'provider exploded', got %v", errorEvents)
	}
}

func TestOffRemovesHandler(t *testing.T) {
	loop, _ := newTestLoop(t, mockStreamFn("test"))

	callCount := 0
	handler := func(args ...any) {
		callCount++
	}

	loop.On(EventDone, handler)
	loop.Off(EventDone, handler)

	queues := loop.queues
	queues.FollowUp("test-agent", "task")
	loop.Start()

	// Run full cycle.
	for i := 0; i < 4; i++ {
		if err := loop.Tick(); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}

	if callCount != 0 {
		t.Errorf("handler should not have been called after Off, was called %d times", callCount)
	}
}

func TestLoopDetectionTriggersAfterThreeIdenticalCalls(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(AgentTool{
		Name:        "echo",
		Description: "echoes input",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"msg": map[string]any{"type": "string"},
			},
		},
		Execute: func(params map[string]any, ctx context.Context, stream func(string)) (string, error) {
			return "ok", nil
		},
	})
	sessions := NewSessionStoreAt(t.TempDir())
	q := NewMessageQueues()
	loop := NewAgentLoop(AgentConfig{Slug: "test", Name: "Test"}, tools, sessions, q, nil, nil, nil)
	loop.Start()
	sid, _ := sessions.Create("test")
	loop.state.SessionID = sid

	// Simulate 3 identical tool calls
	for i := 0; i < 3; i++ {
		loop.pendingToolCalls = []*ToolCall{
			{
				ToolName:  "echo",
				Params:    map[string]any{"msg": "hello"},
				StartedAt: time.Now().UnixMilli(),
			},
		}
		loop.executeTools()
	}

	if !loop.taskHadError {
		t.Fatal("expected taskHadError after 3 identical tool calls")
	}
}

func TestLoopDetectionResetsOnNewTask(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(AgentTool{
		Name:        "echo",
		Description: "echoes input",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"msg": map[string]any{"type": "string"},
			},
		},
		Execute: func(params map[string]any, ctx context.Context, stream func(string)) (string, error) {
			return "ok", nil
		},
	})
	sessions := NewSessionStoreAt(t.TempDir())
	q := NewMessageQueues()
	loop := NewAgentLoop(AgentConfig{Slug: "test", Name: "Test"}, tools, sessions, q, nil, nil, nil)
	loop.Start()
	sid, _ := sessions.Create("test")
	loop.state.SessionID = sid

	// 2 identical calls (not enough to trigger)
	for i := 0; i < 2; i++ {
		loop.pendingToolCalls = []*ToolCall{
			{
				ToolName:  "echo",
				Params:    map[string]any{"msg": "hello"},
				StartedAt: time.Now().UnixMilli(),
			},
		}
		loop.executeTools()
	}

	// Simulate new task starting (resets fingerprints)
	loop.recentToolFingerprints = nil

	// One more identical call — should NOT trigger (reset happened)
	loop.pendingToolCalls = []*ToolCall{
		{
			ToolName:  "echo",
			Params:    map[string]any{"msg": "hello"},
			StartedAt: time.Now().UnixMilli(),
		},
	}
	loop.executeTools()

	if loop.taskHadError {
		t.Fatal("did not expect loop detection after reset")
	}
}

func TestParallelToolExecution(t *testing.T) {
	var callOrder []string
	var mu sync.Mutex

	tools := NewToolRegistry()
	for _, name := range []string{"tool_a", "tool_b", "tool_c"} {
		n := name
		tools.Register(AgentTool{
			Name:        n,
			Description: n,
			Schema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Execute: func(params map[string]any, ctx context.Context, stream func(string)) (string, error) {
				time.Sleep(50 * time.Millisecond) // simulate work
				mu.Lock()
				callOrder = append(callOrder, n)
				mu.Unlock()
				return n + " done", nil
			},
		})
	}

	sessions := NewSessionStoreAt(t.TempDir())
	q := NewMessageQueues()
	loop := NewAgentLoop(AgentConfig{Slug: "test", Name: "Test"}, tools, sessions, q, nil, nil, nil)
	loop.Start()
	sid, _ := sessions.Create("test")
	loop.state.SessionID = sid

	// Set up 3 pending tool calls
	loop.pendingToolCalls = []*ToolCall{
		{ToolName: "tool_a", Params: map[string]any{}, StartedAt: time.Now().UnixMilli()},
		{ToolName: "tool_b", Params: map[string]any{}, StartedAt: time.Now().UnixMilli()},
		{ToolName: "tool_c", Params: map[string]any{}, StartedAt: time.Now().UnixMilli()},
	}

	start := time.Now()
	loop.executeTools()
	elapsed := time.Since(start)

	// If sequential, ~150ms. If parallel, ~50ms.
	if elapsed > 120*time.Millisecond {
		t.Fatalf("expected parallel execution (~50ms), took %v", elapsed)
	}

	if len(loop.pendingToolCalls) != 0 {
		t.Fatal("expected pendingToolCalls to be cleared")
	}

	// Check all 3 tools executed
	mu.Lock()
	if len(callOrder) != 3 {
		t.Fatalf("expected 3 tool calls, got %d", len(callOrder))
	}
	mu.Unlock()

	// Check results in session
	entries, _ := sessions.GetHistory(sid, 0, "")
	toolResults := 0
	for _, e := range entries {
		if e.Type == "tool_result" {
			toolResults++
		}
	}
	if toolResults != 3 {
		t.Fatalf("expected 3 tool_result entries, got %d", toolResults)
	}
}

func TestSingleToolCallStaysSequential(t *testing.T) {
	tools := NewToolRegistry()
	tools.Register(AgentTool{
		Name:        "solo",
		Description: "solo tool",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		Execute: func(params map[string]any, ctx context.Context, stream func(string)) (string, error) {
			return "done", nil
		},
	})

	sessions := NewSessionStoreAt(t.TempDir())
	q := NewMessageQueues()
	loop := NewAgentLoop(AgentConfig{Slug: "test", Name: "Test"}, tools, sessions, q, nil, nil, nil)
	loop.Start()
	sid, _ := sessions.Create("test")
	loop.state.SessionID = sid

	loop.pendingToolCalls = []*ToolCall{
		{ToolName: "solo", Params: map[string]any{}, StartedAt: time.Now().UnixMilli()},
	}

	loop.executeTools()

	if len(loop.pendingToolCalls) != 0 {
		t.Fatal("expected pendingToolCalls to be cleared")
	}
}
