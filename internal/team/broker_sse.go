package team

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Server-Sent Events: per-broker fanout (handleEvents) and per-agent
// stdout streaming (handleAgentStream). Plus the tool-call audit channel
// (handleAgentToolEvent + recordAgentToolEvent) which writes into the
// per-agent stream and drives the SkillCounter nudge for skill_review.
//
// SSE wire shape:
//   - Content-Type: text/event-stream
//   - 15s heartbeat as a comment line ": ping" — keeps proxies alive
//     without producing an event the client has to handle.
//   - Cancellation: r.Context().Done() — every loop checks it first.

func (b *Broker) handleEvents(w http.ResponseWriter, r *http.Request) {
	actor, ok := b.requestActorFromRequest(r)
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	r = requestWithActor(r, actor)

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

	actorStillAuthorized := func() bool {
		if actor.Kind != requestActorKindHuman {
			return true
		}
		return b.humanSessionIDActive(actor.SessionID)
	}

	writeEvent := func(name string, payload any) error {
		if !actorStillAuthorized() {
			return fmt.Errorf("human session revoked")
		}
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
			if !ok {
				return
			}
			if actor.Kind == requestActorKindHuman {
				continue
			}
			if writeEvent("notebook:write", evt) != nil {
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
			if !actorStillAuthorized() {
				return
			}
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
		TaskID string `json:"task_id,omitempty"`
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
	if stream != nil {
		line := formatAgentToolEvent(body.Phase, body.Tool, body.Args, body.Result, body.Error)
		if line != "" {
			taskID := strings.TrimSpace(body.TaskID)
			if taskID == "" {
				b.mu.Lock()
				taskID = b.activeTaskIDForAgentLocked(slug)
				b.mu.Unlock()
			}
			stream.PushTask(taskID, line+"\n")
		}
	}

	// Drive the Hermes counter. We only count once per tool call so we
	// branch on phase=="call" — the call/result/error fan-out from a
	// single tool invocation must NOT triple-count. team_skill_create /
	// team_skill_patch reset the tally; everything else increments and
	// may fire a skill_review_nudge task.
	b.recordAgentToolEvent(slug, body.Phase, body.Tool, body.Args)

	w.WriteHeader(http.StatusOK)
}

// recordAgentToolEvent updates the broker's SkillCounter for one
// tool-event payload. It is split out from handleAgentToolEvent so tests
// can drive the counter directly without going through HTTP, and so the
// b.mu acquisition for the nudge task creation is centralized in one
// place.
//
// We only act on phase=="call" — the call/result/error fan-out per tool
// invocation must not triple-count. Empty phase is treated as "call" for
// backward compatibility; older agents that didn't tag the phase still
// only post one event per call.
func (b *Broker) recordAgentToolEvent(slug, phase, tool, args string) {
	slug = strings.TrimSpace(slug)
	tool = strings.TrimSpace(tool)
	if slug == "" || tool == "" {
		return
	}
	phase = strings.TrimSpace(phase)
	if phase != "" && phase != "call" {
		// result / error events from the same call — already counted.
		return
	}
	counter := b.ensureSkillCounter()
	if counter == nil {
		return
	}

	// Skill-authoring tools reset the counter (the agent just codified
	// something). Everything else increments.
	if IsSkillAuthoringTool(tool) {
		counter.Reset(slug)
		return
	}

	summary := skillCounterSummaryFromArgs(tool, args)
	shouldNudge, _ := counter.Increment(slug, tool, summary)
	if !shouldNudge {
		return
	}

	b.mu.Lock()
	taskID, err := b.fireSkillReviewNudgeLocked(slug)
	if err == nil {
		atomic.AddInt64(&b.skillCompileMetrics.CounterNudgesFiredTotal, 1)
		if saveErr := b.saveLocked(); saveErr != nil {
			slog.Warn("skill_counter_nudge_persist_failed",
				"agent", slug, "task_id", taskID, "err", saveErr)
		}
	}
	b.mu.Unlock()
	if err != nil {
		slog.Warn("skill_counter_nudge_create_failed",
			"agent", slug, "err", err)
	}
}

// ensureSkillCounter lazily constructs the SkillCounter. Like the other
// skill-* singletons, the counter is built on first use so tests that
// never trigger an agent tool call pay no cost.
func (b *Broker) ensureSkillCounter() *SkillCounter {
	b.mu.Lock()
	if b.skillCounter != nil {
		c := b.skillCounter
		b.mu.Unlock()
		return c
	}
	c := NewSkillCounter()
	b.skillCounter = c
	b.mu.Unlock()
	return c
}

// SetSkillCounter replaces the broker's counter — used by tests to inject
// a counter with a specific threshold / cooldown / clock without going
// through env vars.
func (b *Broker) SetSkillCounter(c *SkillCounter) {
	b.mu.Lock()
	b.skillCounter = c
	b.mu.Unlock()
}

// skillCounterSummaryFromArgs renders a one-line summary of a tool call
// for the per-agent ring buffer. The args field is a JSON-encoded
// string (as posted by the MCP client), so we don't try to parse it —
// we just trim and truncate. Real human review of the nudge task uses
// the agent's own activity stream for full detail.
func skillCounterSummaryFromArgs(tool, args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return tool
	}
	// Collapse whitespace runs so the summary stays single-line.
	args = strings.Join(strings.Fields(args), " ")
	return truncateSummary(args, 120)
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
	if stream == nil {
		http.Error(w, "agent stream not found", http.StatusNotFound)
		return
	}

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
