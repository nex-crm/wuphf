package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/workflow"
)

// broker_workflow_run.go is the runtime: it executes a frozen contract and
// records the run. The state machine runs deterministically; actions are
// dispatched by kind through workflowActionExec, which is where the real agent
// draft (llm) and the ExternalActionApprovalCard gate (external) plug in.

func workflowRunsPath(id string) string {
	home := strings.TrimSpace(config.RuntimeHomeDir())
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".wuphf", "office", "workflows", id+".runs.jsonl")
}

// workflowGate decides (and, on proceed, executes) one external action. The
// production gate is externalGateDecision; tests inject a fake.
type workflowGate func(ctx context.Context, agent, platform, actionID string, data map[string]any) workflow.ActionOutcome

// makeWorkflowActionExec builds the ActionExec the runtime executes the spec
// with, bound to the operating agent + the real external gate.
func (b *Broker) makeWorkflowActionExec(ctx context.Context, agent string, spec *workflow.Spec) workflow.ActionExec {
	return b.makeWorkflowActionExecWithGate(ctx, agent, b.externalGateDecision, spec)
}

// makeWorkflowActionExecWithGate is the testable core: it routes actions by kind
// and delegates external actions to the supplied gate. The spec supplies the
// integration-read allow-list (D6) — a read for a (platform, action_id) not on
// AllowedReads is refused before any provider call.
func (b *Broker) makeWorkflowActionExecWithGate(ctx context.Context, agent string, gate workflowGate, spec *workflow.Spec) workflow.ActionExec {
	allow := func(platform, actionID string) bool {
		return spec != nil && spec.IsReadAllowed(platform, actionID)
	}
	return func(a workflow.Action, data map[string]any) workflow.ActionOutcome {
		// Generic integration read: any deterministic step that declares a
		// provider target (Platform+ActionID). One executor runs every provider;
		// the spec (agent-authored) carries the params + projection. No
		// per-integration Go.
		if a.IsIntegrationRead() {
			return b.execIntegrationAction(ctx, a, allow)
		}
		switch a.Kind {
		case workflow.ActionLLM:
			// Genuine AI: run the step through the office's configured default
			// provider (same model the agents use), not a templated stub. Falls
			// back to a deterministic baseline if no provider is available.
			return b.execLLMAction(ctx, a, data)
		case workflow.ActionExternal:
			if strings.TrimSpace(a.Platform) == "" || strings.TrimSpace(a.ActionID) == "" {
				// No integration target wired: this is a DRAFT, not a send. Say so
				// honestly in the run record instead of reporting a phantom success.
				// Keys are per-action so multiple sends in one run don't collide.
				return workflow.ActionOutcome{OK: true, Output: map[string]any{
					a.ID + "_sent": false,
					a.ID + "_note": "drafted — no integration connected for " + a.ID + "; nothing was sent",
				}}
			}
			// The send body is the action's authored params with {{step}} tokens
			// replaced by the real upstream output (e.g. the summary an llm step
			// produced) — NOT the raw context map. That is what turns a contract
			// with "{{summarize_emails}}" into an actual message.
			return gate(ctx, agent, a.Platform, a.ActionID, renderActionParams(a.Params, data))
		default:
			// A deterministic step with no domain handler. If it looks like a
			// send/post/notify, do NOT report a silent success — a real send needs
			// an external (gated) action with a connected integration. Record that
			// it was drafted only, so the audit never claims an email/post that
			// did not happen.
			if looksLikeSendAction(a) {
				return workflow.ActionOutcome{OK: true, Output: map[string]any{
					a.ID + "_sent": false,
					a.ID + "_note": "drafted — " + a.ID + " has no connected integration; make it an external step to actually send",
				}}
			}
			return workflow.ActionOutcome{OK: true}
		}
	}
}

// externalGateDecision runs the real deterministic-integrations gate: classify
// the action against the live connection state + scoped grants. On "proceed"
// (connected + granted, or read-only) it executes the send via Composio; any
// other decision blocks the send and records that a human must act.
func (b *Broker) externalGateDecision(ctx context.Context, agent, platform, actionID string, data map[string]any) workflow.ActionOutcome {
	resp := b.resolveExternalAction(ctx, integrationResolveRequest{
		Provider: "composio", Platform: platform, ActionID: actionID, Agent: agent, Data: data,
	})
	if resp.Decision != string(action.DecisionProceed) {
		// The gate requires human action (connect / approve / wait). The send
		// does not fire; the run records the stop so the workflow can resume
		// once the human resolves it.
		return workflow.ActionOutcome{OK: false, Output: map[string]any{
			"decision": resp.Decision, "needs_approval": true, "request_id": resp.RequestID,
		}}
	}
	composio := action.NewComposioFromEnv()
	if !composio.Configured() || resp.Account == nil {
		return workflow.ActionOutcome{OK: true, Output: map[string]any{"sent": false, "note": "no connected account"}}
	}
	if _, err := composio.ExecuteAction(ctx, action.ExecuteRequest{
		Platform: platform, ActionID: actionID, ConnectionKey: resp.Account.Key, Data: data,
	}); err != nil {
		return workflow.ActionOutcome{OK: false, Err: err.Error()}
	}
	return workflow.ActionOutcome{OK: true, Output: map[string]any{"sent": true}}
}

// runWorkflowSpec loads a stored contract, executes it over the events, persists
// the run observation, and updates the binding's last-execution. The run is the
// raw material the improvement loop mines.
func (b *Broker) runWorkflowSpec(specID, trigger string, events []workflow.ScenarioEvent) (workflow.RunRecord, error) {
	path := workflowSpecPath(specID)
	if path == "" {
		return workflow.RunRecord{}, fmt.Errorf("no runtime home")
	}
	spec, err := workflow.LoadSpec(path)
	if err != nil {
		return workflow.RunRecord{}, err
	}
	if len(events) == 0 {
		events = runToCompletionEvents(spec)
	}

	exec := b.makeWorkflowActionExec(context.Background(), strings.TrimSpace(spec.Operator), spec)
	rec := workflow.Execute(spec, trigger, events, exec)
	rec.At = time.Now().UTC().Format(time.RFC3339)
	_ = workflow.AppendRun(workflowRunsPath(specID), rec)

	b.mu.Lock()
	if sk := b.findSkillByNameLocked(specID); sk != nil {
		sk.LastExecutionAt = rec.At
		sk.LastExecutionStatus = "ok"
		sk.UsageCount++
	}
	_ = b.saveLocked()
	b.mu.Unlock()
	return rec, nil
}

// RunWorkflowSpec is the scheduler entry point (TargetType=workflow_spec).
func (b *Broker) RunWorkflowSpec(specID, trigger string) error {
	_, err := b.runWorkflowSpec(specID, trigger, nil)
	return err
}

// handleWorkflowsProposals closes the self-healing loop: it mines a contract's
// run records for recurring exceptions and returns drafted overlays the operator
// can review and accept (via /workflows/improve). Read-only; nothing is applied.
func (b *Broker) handleWorkflowsProposals(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var body struct {
		SpecID string `json:"spec_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	id := strings.TrimSpace(body.SpecID)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "spec_id_required"})
		return
	}
	path := workflowSpecPath(id)
	if path == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "no_runtime_home"})
		return
	}
	spec, err := workflow.LoadSpec(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "spec_not_found"})
		return
	}
	runs, _ := workflow.ReadRuns(workflowRunsPath(id))
	proposals := workflow.ProposeOverlays(spec, runs, workflow.ProposeOptions{})
	writeJSON(w, http.StatusOK, map[string]any{"spec_id": id, "runs": len(runs), "proposals": proposals})
}

// workflowTrigger describes one way a frozen workflow fires, for the graph view.
type workflowTrigger struct {
	Kind            string `json:"kind"` // manual | schedule | event
	Label           string `json:"label"`
	Enabled         bool   `json:"enabled,omitempty"`
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	NextRun         string `json:"next_run,omitempty"`
}

// handleWorkflowsSpec returns a frozen contract plus its triggers so the panel
// can render the workflow as nodes (triggers -> states -> actions). Read-only.
func (b *Broker) handleWorkflowsSpec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("spec_id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "spec_id_required"})
		return
	}
	path := workflowSpecPath(id)
	if path == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "no_runtime_home"})
		return
	}
	spec, err := workflow.LoadSpec(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "spec_not_found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"spec": spec, "triggers": b.workflowTriggersFor(id, spec)})
}

// workflowTriggersFor returns the canonical, user-facing trigger set for a
// workflow. There are exactly four kinds — manual, schedule, webhook, and
// context change — and nothing else. The raw state-machine events are NOT
// triggers: the entry event is just the mechanism the manual run fires, and
// later events drive mid-run hops, so surfacing them as chips read as random
// noise. Manual is always present; schedule is present when a scheduler job
// targets the spec; webhook / context are present when the contract's entry is
// classified as one (by event naming) until an explicit trigger field exists.
func (b *Broker) workflowTriggersFor(id string, spec *workflow.Spec) []workflowTrigger {
	triggers := []workflowTrigger{{Kind: "manual", Label: "Manual"}}

	b.mu.Lock()
	for i := range b.scheduler {
		j := b.scheduler[i]
		if j.TargetType == "workflow_spec" && j.TargetID == id {
			triggers = append(triggers, workflowTrigger{
				Kind: "schedule", Label: "Schedule", Enabled: j.Enabled,
				IntervalMinutes: j.IntervalMinutes, NextRun: j.NextRun,
			})
		}
	}
	b.mu.Unlock()

	// Classify the workflow's ENTRY events (those leaving the initial state).
	// A generic run/manual event is the manual mechanism and adds no chip; an
	// event named like an inbound hook or a context change surfaces that kind.
	seen := map[string]bool{"manual": true, "schedule": true}
	entry := entryTriggerEvents(spec)
	for _, ev := range spec.Events {
		if !entry[ev.ID] {
			continue
		}
		kind := classifyTriggerEvent(ev)
		if kind == "" || seen[kind] {
			continue
		}
		seen[kind] = true
		label := "Webhook"
		if kind == "context" {
			label = "Context change"
		}
		triggers = append(triggers, workflowTrigger{Kind: kind, Label: label})
	}
	return triggers
}

// classifyTriggerEvent maps an entry event to a canonical trigger kind by its
// naming, or "" when it is just the generic manual-run mechanism.
func classifyTriggerEvent(ev workflow.Event) string {
	hay := strings.ToLower(ev.ID + " " + ev.Label)
	switch {
	case strings.Contains(hay, "webhook") || strings.Contains(hay, "hook") ||
		strings.Contains(hay, "received") || strings.Contains(hay, "incoming") ||
		strings.Contains(hay, "inbound"):
		return "webhook"
	case strings.Contains(hay, "context") || strings.Contains(hay, "changed") ||
		strings.Contains(hay, "updated") || strings.Contains(hay, "detected") ||
		strings.Contains(hay, "new_"):
		return "context"
	default:
		return "" // generic run/start event — folded into Manual
	}
}

// handleWorkflowsRuns returns a frozen workflow's run history, newest first, for
// the run-history list. Read-only.
func (b *Broker) handleWorkflowsRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("spec_id"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "spec_id_required"})
		return
	}
	runs, _ := workflow.ReadRuns(workflowRunsPath(id))
	// Newest first.
	for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
		runs[i], runs[j] = runs[j], runs[i]
	}
	writeJSON(w, http.StatusOK, map[string]any{"spec_id": id, "runs": runs})
}

// entryTriggerEvents returns the set of event IDs that ENTER the workflow —
// i.e. label a transition leaving the initial state. Only these are real
// triggers; events that drive later hops fire mid-run.
func entryTriggerEvents(spec *workflow.Spec) map[string]bool {
	entry := map[string]bool{}
	for _, t := range spec.Transitions {
		if t.From == spec.Initial && strings.TrimSpace(t.On) != "" {
			entry[t.On] = true
		}
	}
	return entry
}

// runToCompletionEvents builds the event sequence that drives a manual run from
// the initial state THROUGH every single-exit hop to a terminal state, so
// "Run now" exercises the whole contract (fetch → summarize → compose → email →
// announce) instead of stopping at the first state. It walks single-outgoing
// transitions only — a state with a real branch (multiple outgoing transitions)
// needs run data to decide, so the walk stops there. Cycle- and length-guarded.
func runToCompletionEvents(spec *workflow.Spec) []workflow.ScenarioEvent {
	terminal := map[string]bool{}
	for _, t := range spec.Terminal {
		terminal[t] = true
	}
	outgoing := map[string][]workflow.Transition{}
	for _, t := range spec.Transitions {
		outgoing[t.From] = append(outgoing[t.From], t)
	}
	var events []workflow.ScenarioEvent
	seen := map[string]bool{}
	state := spec.Initial
	for hops := 0; hops <= len(spec.States); hops++ {
		if seen[state] || terminal[state] {
			break
		}
		seen[state] = true
		outs := outgoing[state]
		if len(outs) != 1 || strings.TrimSpace(outs[0].On) == "" {
			break // terminal, dead end, or an undecidable branch
		}
		events = append(events, workflow.ScenarioEvent{Event: outs[0].On})
		state = outs[0].To
	}
	if len(events) == 0 {
		// No usable chain — fall back to the legacy single-event seed so the run
		// still does something.
		ev := "run"
		if len(spec.Events) > 0 {
			ev = spec.Events[0].ID
		}
		events = []workflow.ScenarioEvent{{Event: ev}}
	}
	return events
}

// handleWorkflowsRunToTask kicks off a new task from a workflow run: it composes
// the run's outcome (path + produced digest/outputs) into the task context and
// appends the operator's own start prompt, then creates the task so an agent
// picks it up. This is the "do something with this run" bridge — e.g. run the
// digest, then spin a task to draft the replies it flagged.
func (b *Broker) handleWorkflowsRunToTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var body struct {
		SpecID string             `json:"spec_id"`
		Prompt string             `json:"prompt"`
		Run    workflow.RunRecord `json:"run"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_body"})
		return
	}
	specID := strings.TrimSpace(body.SpecID)
	prompt := strings.TrimSpace(body.Prompt)
	if specID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "spec_id_required"})
		return
	}

	goal := specID
	if path := workflowSpecPath(specID); path != "" {
		if spec, err := workflow.LoadSpec(path); err == nil && strings.TrimSpace(spec.Goal) != "" {
			goal = strings.TrimSpace(spec.Goal)
		}
	}

	title := runTaskTitleLine(prompt, 80)
	if title == "" {
		title = "Follow up on the " + goal + " run"
	}
	details := composeRunTaskDetails(goal, body.Run, prompt)

	res, err := b.MutateTask(TaskPostRequest{
		Action:    "create",
		Title:     title,
		Details:   details,
		Owner:     "ceo", // the CEO triages + routes the kicked-off task
		CreatedBy: "human",
		TaskType:  "office",
	})
	if err != nil {
		writeTaskMutationHTTPError(w, err)
		return
	}
	// Seed the task's channel with the run context + prompt as the opening
	// message, so the task chat is not empty when the operator jumps to it and
	// the owner wakes on the kickoff. (Details alone is the spec; the chat is
	// the surface the human and agent actually read.)
	if ch := strings.TrimSpace(res.Task.Channel); ch != "" {
		if _, perr := b.PostMessage("human", ch, details, []string{"ceo"}, ""); perr != nil {
			log.Printf("run-to-task: seed channel %s: %v", ch, perr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task_id": res.Task.ID,
		"channel": res.Task.Channel,
		"title":   res.Task.Title,
	})
}

// composeRunTaskDetails folds a workflow run's outcome + the operator's prompt
// into a task brief.
func composeRunTaskDetails(goal string, run workflow.RunRecord, prompt string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Kicked off from a run of the **%s** workflow", goal)
	if strings.TrimSpace(run.At) != "" {
		fmt.Fprintf(&b, " (%s)", run.At)
	}
	b.WriteString(".\n\n")
	if path := run.Result.StateSeq; len(path) > 0 {
		fmt.Fprintf(&b, "**Run path:** %s\n", strings.Join(path, " → "))
	}
	if digest := asString(run.Result.Outputs["digest"]); strings.TrimSpace(digest) != "" {
		if n, ok := run.Result.Outputs["email_count"]; ok {
			fmt.Fprintf(&b, "**Produced (%v items):**\n", n)
		} else {
			b.WriteString("**Produced:**\n")
		}
		b.WriteString(digest)
		b.WriteString("\n")
	}
	if strings.TrimSpace(prompt) != "" {
		b.WriteString("\n---\n\n**What to do:**\n")
		b.WriteString(prompt)
		b.WriteString("\n")
	}
	return b.String()
}

// runTaskTitleLine returns the first non-empty line of s, capped to max runes.
func runTaskTitleLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if r := []rune(s); len(r) > max {
		return strings.TrimSpace(string(r[:max])) + "…"
	}
	return s
}

// asString coerces an any to a string (empty when not a string). Local helper
// for run-output extraction.
func asString(v any) string {
	s, _ := v.(string)
	return s
}

// handleWorkflowsChannel ensures the per-workflow conversation channel exists
// and returns its slug. The channel (`workflow-<spec_id>`) is where the human
// chats with the Workflow Builder about THIS contract from the
// Workflows page — @mentioning @workflow-builder there wakes its headless turn,
// and its freeze/edit notes post back to the same channel. Idempotent: a second
// call no-ops and returns the existing slug.
func (b *Broker) handleWorkflowsChannel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var body struct {
		SpecID string `json:"spec_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_body"})
		return
	}
	id := strings.TrimSpace(body.SpecID)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "spec_id_required"})
		return
	}
	// Derive a human channel name from the spec title when available.
	name := "Workflow · " + id
	if path := workflowSpecPath(id); path != "" {
		if spec, err := workflow.LoadSpec(path); err == nil && strings.TrimSpace(spec.Goal) != "" {
			name = "Workflow · " + strings.TrimSpace(spec.Goal)
		}
	}
	slug := b.ensureWorkflowChannel(id, name)
	writeJSON(w, http.StatusOK, map[string]any{"channel": slug})
}

// ensureWorkflowChannel creates the per-workflow channel if it does not exist
// and returns its normalized slug. The Workflow Builder is seeded as a member
// (the CEO has all-channel access via the reserved-slug bypass, so it is not
// listed explicitly). createChannelLocked persists on success, so no extra save
// is needed here. Safe to call repeatedly — an existing channel is a no-op.
func (b *Broker) ensureWorkflowChannel(specID, name string) string {
	slug := normalizeChannelSlug("workflow-" + specID)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.findChannelLocked(slug) != nil {
		return slug
	}
	members := []string{}
	if b.findMemberLocked(WorkflowBuilderSlug) != nil {
		members = append(members, WorkflowBuilderSlug)
	}
	if _, cerr := b.createChannelLocked(channelCreateInput{
		Slug:        slug,
		Name:        name,
		Description: "Chat with @workflow-builder about this workflow contract.",
		Members:     members,
		CreatedBy:   "system",
	}); cerr != nil {
		// On a race or validation hiccup, fall back to the slug; the message
		// post will surface any real error to the operator.
		return slug
	}
	return slug
}

// handleWorkflowsRun is the manual / programmatic trigger. Schedule and event
// triggers fire the same runtime via RunWorkflowSpec.
func (b *Broker) handleWorkflowsRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed"})
		return
	}
	var body struct {
		SpecID   string         `json:"spec_id"`
		Event    string         `json:"event,omitempty"`
		Data     map[string]any `json:"data,omitempty"`
		DedupKey string         `json:"dedup_key,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid_json"})
		return
	}
	id := strings.TrimSpace(body.SpecID)
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "spec_id_required"})
		return
	}
	var events []workflow.ScenarioEvent
	if strings.TrimSpace(body.Event) != "" {
		events = []workflow.ScenarioEvent{{Event: body.Event, Data: body.Data, DedupKey: body.DedupKey}}
	}
	rec, err := b.runWorkflowSpec(id, "manual", events)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "run_failed", "detail": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": rec})
}
