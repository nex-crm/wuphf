package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// extract.go is the completion-time workflow EXTRACTOR. Where DraftSpec turns a
// bare tool-shape into a thin scaffold, the extractor reads a COMPLETED task's
// real trace (the human's ask + each integration call's masked args + response
// shape) and produces a named, parameterized, ordered contract. The model call
// is injected (Extractor) so the structure-building + grounding here stay pure
// and testable; the broker supplies the live provider.
//
// Grounding is the safety invariant: the model may only describe steps whose
// action_id actually appears in the gated shape. GroundExtraction enforces it,
// so an LLM cannot invent a step the task never ran. This is the verification
// discipline applied to extraction (docs/specs/large-io-framework.md).

// ExtractInput is the completion-time corpus handed to the model.
type ExtractInput struct {
	// Goal is the human's originating ask for the task.
	Goal string `json:"goal"`
	// Shape is the allow-list of action tokens (lowercased action ids) the task
	// actually ran. The model may not introduce steps outside it.
	Shape []string `json:"shape"`
	// Traces are the ordered integration calls with masked args + response shape.
	Traces []TraceStep `json:"traces"`
}

// TraceStep is one integration call in the corpus (the model-facing projection
// of an ActionTrace).
type TraceStep struct {
	ActionID string         `json:"action_id"`
	Platform string         `json:"platform"`
	Args     map[string]any `json:"args,omitempty"`
	Result   string         `json:"result,omitempty"`
}

// ExtractedTrigger is how the model believes the workflow should fire. It is
// proposal metadata (the schedule itself lives in the office scheduler on
// freeze), not part of the contract's state machine.
type ExtractedTrigger struct {
	Kind            string `json:"kind"` // manual | schedule | webhook | context
	IntervalMinutes int    `json:"interval_minutes,omitempty"`
	Rationale       string `json:"rationale,omitempty"`
}

// ExtractedStep is one workflow step the model reconstructed from the trace.
type ExtractedStep struct {
	// Kind is "integration" (a real provider call — default) or "llm" (a
	// synthesis/reasoning step the assistant did in its head — summarize, draft,
	// decide. That work is NOT a tool call, so it is NOT in the gated shape; the
	// extractor is allowed to re-insert it as the connective intelligence a
	// fetch→post workflow needs to actually produce real content).
	Kind       string         `json:"kind,omitempty"`
	ActionID   string         `json:"action_id"`
	Platform   string         `json:"platform"`
	Params     map[string]any `json:"params,omitempty"`
	ResultPath string         `json:"result_path,omitempty"`
	Expose     []string       `json:"expose,omitempty"`
	// Instruction is the prompt for an llm step: what to produce from the prior
	// steps' data (e.g. "Summarize the urgent emails into a short Slack alert").
	Instruction string `json:"instruction,omitempty"`
	// FeedsFrom names an earlier step's action_id whose output feeds this step,
	// or "" for an independent step. Surfaced for review; the linear contract is
	// built from step order.
	FeedsFrom string `json:"feeds_from,omitempty"`
}

// isLLMStep reports whether a step is a synthesis/reasoning step (not a real
// integration call) — exempt from the grounded-to-shape requirement.
func (s ExtractedStep) isLLMStep() bool {
	return strings.EqualFold(strings.TrimSpace(s.Kind), "llm")
}

// Extraction is the model's judgment + reconstruction for one completed task.
type Extraction struct {
	// IsWorkflow is the model's judgment that this task is a reusable workflow
	// worth automating (vs a one-off). A proposal is surfaced only when true.
	IsWorkflow bool    `json:"is_workflow"`
	Confidence float64 `json:"confidence"`
	Name       string  `json:"name"`
	// Description is a one-to-two sentence human-readable summary of what the
	// workflow does (provenance: shown on the workflow card and binding).
	Description string           `json:"description,omitempty"`
	Trigger     ExtractedTrigger `json:"trigger"`
	Steps       []ExtractedStep  `json:"steps"`
	// Reason is WHY this was judged a workflow — the rationale shown as the
	// "why generated" provenance line.
	Reason string `json:"reason,omitempty"`
}

// Extractor is the injected model call. The broker implements it with the live
// provider; tests pass a stub.
type Extractor interface {
	Extract(in ExtractInput) (Extraction, error)
}

// GroundExtraction enforces the safety invariant: drop any step whose action_id
// is not in the gated shape (the allow-list of what actually ran), de-dupe
// repeated steps keeping first position, and downgrade IsWorkflow to false when
// nothing real survives. The shape entries are lowercased action tokens
// (e.g. "gmail_fetch_emails"); a step's action_id is matched case-insensitively.
func GroundExtraction(e Extraction, shape []string) Extraction {
	allowed := make(map[string]bool, len(shape))
	for _, s := range shape {
		allowed[strings.ToLower(strings.TrimSpace(s))] = true
	}
	seen := map[string]bool{}
	grounded := make([]ExtractedStep, 0, len(e.Steps))
	hasIntegration := false
	for _, st := range e.Steps {
		key := strings.ToLower(strings.TrimSpace(st.ActionID))
		if key == "" || seen[key] {
			continue
		}
		if st.isLLMStep() {
			// Synthesis step (summarize/draft/decide) — connective intelligence
			// that was the assistant's reasoning, not a tool call, so it is NOT
			// in the gated shape. Keep it; it cannot mint an integration call.
			seen[key] = true
			grounded = append(grounded, st)
			continue
		}
		if !allowed[key] {
			continue // an integration step must be grounded in what actually ran
		}
		seen[key] = true
		hasIntegration = true
		grounded = append(grounded, st)
	}
	e.Steps = grounded
	// A workflow needs at least one real integration action; pure-llm "workflows"
	// (no provider calls grounded) are not surfaced.
	if !hasIntegration {
		e.IsWorkflow = false
	}
	return e
}

// BuildSpecFromExtraction turns a grounded extraction into a real linear-chain
// contract: one state hop per step (start -> … -> done), each step bound to its
// platform/action_id with the model's Params/ResultPath/Expose, reads
// allow-listed + deterministic and writes external. The result validates and
// passes Shipcheck. knownPlatforms gates binding to real platforms (nil = bind
// from the step's own platform field, which comes from the real trace).
func BuildSpecFromExtraction(id, operator string, e Extraction, knownPlatforms map[string]bool) (Spec, error) {
	if len(e.Steps) == 0 {
		return Spec{}, fmt.Errorf("extraction has no grounded steps")
	}
	n := len(e.Steps)
	states := make([]State, 0, n+1)
	events := make([]Event, 0, n)
	transitions := make([]Transition, 0, n)
	actions := make([]Action, 0, n)
	var allowedReads []ActionRef
	seenRead := map[string]bool{}
	scenarioEvents := make([]ScenarioEvent, 0, n)
	expectStates := make([]string, 0, n+1)
	expectActions := make([]string, 0, n)

	stateID := func(i int) string {
		switch i {
		case 0:
			return "start"
		case n:
			return "done"
		default:
			return fmt.Sprintf("step_%d", i)
		}
	}
	states = append(states, State{ID: "start", Label: "Start"})
	expectStates = append(expectStates, "start")

	for i, st := range e.Steps {
		token := actionToken(st.ActionID)
		// Action binding.
		a := Action{ID: token, Description: "Step: " + token}
		switch {
		case st.isLLMStep():
			// Connective synthesis step: a real model call at run time
			// (execLLMAction renders the prior steps' data into the prompt). No
			// integration binding, never allow-listed.
			a.Kind = ActionLLM
			if instr := strings.TrimSpace(st.Instruction); instr != "" {
				a.Description = instr
			}
		default:
			// A non-LLM step here is grounded in the gated shape, so it provably
			// ran against a real integration. Bind it to that integration. Prefer
			// the model's platform / a known-platform match, but never let a
			// shape-grounded step fall through to an unbound deterministic no-op:
			// the action_id is the canonical PLATFORM_VERB_... slug, so its
			// leading segment is the platform. A spec with an unbound read/send
			// step looks runnable but fetches/sends nothing (the original
			// "extracted workflow posted a placeholder" failure mode), and an
			// unbound deterministic step also slips past the AllowedReads gate
			// because IsIntegrationRead requires a platform+action_id.
			platform := strings.ToLower(strings.TrimSpace(st.Platform))
			if platform == "" {
				if p, _, ok := bindIntegrationAction(token, knownPlatforms); ok {
					platform = p
				}
			}
			if platform == "" {
				if seg := strings.SplitN(token, "_", 2); len(seg) == 2 {
					platform = strings.ToLower(seg[0])
				}
			}
			if platform == "" {
				return Spec{}, fmt.Errorf("integration step %q cannot be bound to a platform (action_id must be PLATFORM_VERB_...)", token)
			}
			a.Platform = platform
			a.ActionID = strings.ToUpper(token)
			a.Params = st.Params
			a.ResultPath = st.ResultPath
			a.Expose = st.Expose
			if isReadAction(token) {
				a.Kind = ActionDeterministic
				if key := platform + "\x00" + a.ActionID; !seenRead[key] {
					seenRead[key] = true
					allowedReads = append(allowedReads, ActionRef{Platform: platform, ActionID: a.ActionID})
				}
			} else {
				a.Kind = ActionExternal
			}
		}
		actions = append(actions, a)
		expectActions = append(expectActions, token)

		// Event driving this hop: "run" for the entry, prev-step-done after.
		var evID string
		if i == 0 {
			evID = "run"
		} else {
			evID = actionToken(e.Steps[i-1].ActionID) + "_done"
		}
		events = append(events, Event{ID: evID, Label: eventLabel(evID)})
		scenarioEvents = append(scenarioEvents, ScenarioEvent{Event: evID, DedupKey: fmt.Sprintf("sample-%d", i)})

		from := stateID(i)
		to := stateID(i + 1)
		states = append(states, State{ID: to, Label: stateLabel(to)})
		expectStates = append(expectStates, to)
		transitions = append(transitions, Transition{From: from, To: to, On: evID, Actions: []string{token}})
	}

	goal := strings.TrimSpace(e.Name)
	if goal == "" {
		goal = "Detected workflow"
	}
	if operator == "" {
		operator = "operator"
	}
	spec := Spec{
		Version:      "1",
		ID:           id,
		Goal:         goal,
		Operator:     operator,
		States:       states,
		Initial:      "start",
		Terminal:     []string{"done"},
		Events:       events,
		Actions:      actions,
		AllowedReads: allowedReads,
		Transitions:  transitions,
		Scenarios: []Scenario{{
			Name:          "happy_path",
			Events:        scenarioEvents,
			ExpectStates:  expectStates,
			ExpectActions: expectActions,
		}},
		ImprovementSignals: []string{"run_count", "exception_rate"},
	}
	if err := spec.Validate(); err != nil {
		return Spec{}, fmt.Errorf("built spec invalid: %w", err)
	}
	return spec, nil
}

// actionToken normalizes an action id to its lowercase shape token.
func actionToken(actionID string) string {
	return strings.ToLower(strings.TrimSpace(actionID))
}

func stateLabel(id string) string {
	if id == "done" {
		return "Done"
	}
	return titleCase(strings.ReplaceAll(id, "_", " "))
}

func eventLabel(id string) string {
	return titleCase(strings.ReplaceAll(id, "_", " "))
}

// titleCase upper-cases the first letter of each space-separated word (ASCII
// ids only — replaces the deprecated strings.Title for our slug labels).
func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if w == "" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// BuildExtractPrompt returns the (system, user) prompt pair for the model. The
// system prompt pins the grounding rule and the output schema; the user prompt
// carries the completed task's real corpus. Kept here so the prompt travels
// with the schema it must satisfy.
func BuildExtractPrompt(in ExtractInput) (system, user string) {
	system = strings.TrimSpace(`
You analyze ONE completed task from an AI office and decide whether it is a reusable WORKFLOW worth automating.

A workflow is a repeatable, multi-step procedure with a clear outcome (e.g. "fetch unread email -> summarize -> post to Slack"). A one-off question, a single lookup, or pure exploration is NOT a workflow.

HARD RULES:
- Integration steps (kind:"integration", the default) must be GROUNDED: only use action_id values that appear in the ALLOWED list. Never invent an integration call.
- Fill each integration step's params from the REAL arguments you see in the trace (e.g. the gmail query, the slack channel). Use result_path + expose to name the response fields the next step consumes — infer them from the response shape, never copy raw values.
- INSERT THE THINKING. The trace only shows tool calls, but the real work also includes what the assistant DID IN ITS HEAD between them — summarizing the fetched data, deciding what's urgent, writing the message. When a later step's CONTENT is produced from earlier data (a summary, a digest, a drafted reply), add a step with kind:"llm", a short action_id (e.g. "summarize_urgent_emails"), and an "instruction" describing what to produce. Place it BEFORE the step that uses it. This is the only kind of step allowed to be outside the ALLOWED list.
- WIRE THE CONTENT. A send/post step must NOT contain a placeholder like "<summary here>". Reference the llm step's output by putting "{{<that step's action_id>}}" in the field (e.g. the slack message text becomes "{{summarize_urgent_emails}}"). At run time that token is replaced with the real produced text.
- Keep steps in the order they ran (with the llm step inserted in its logical place). Set feeds_from to the earlier step whose output a step consumes.
- Judge honestly: set is_workflow=false (with a reason) for one-offs. Confidence is 0..1.
- Suggest a trigger: manual, schedule (with interval_minutes if the ask implies a cadence like "every morning"=1440), webhook, or context.

WRITING STYLE for name, description, and reason — these are read by a BUSY, NON-TECHNICAL operator, so write them like you're explaining to a smart 10-year-old:
- Plain everyday English. Short sentences. Talk about the actual work ("your unread emails", "the #general Slack channel"), NOT the plumbing.
- NEVER use these words or anything like them: trace, action_id, pipeline, canonical, allowed, agent, retry, dedupe, parameters, payload, shape, "two-step procedure", "collapsed into". They mean nothing to an operator.
- name: a short title an operator recognizes (e.g. "Morning inbox digest", "Urgent email → Slack alert").
- description: one or two sentences on what it does for them, in their terms.
- reason: this is the PROVENANCE — explain how we spotted this by describing what the OPERATOR was actually doing in this task that revealed the workflow. Ground it in the human's ask and the real work you see in the trace: name the actual things they touched (their Gmail inbox, the #general Slack channel, their Notion notes, their HubSpot records) and what they were trying to get done, and call out any repetition (they had the assistant do the same thing several times). It should read like "We noticed this while you were …, where you kept having the assistant …" so the operator RECOGNIZES their own work and trusts the suggestion. Example shape: "We spotted this while you were emailing YC partners and kept asking your assistant to pull the earlier VC conversations from Notion and HubSpot before each reply — the same pull-context-then-draft routine every time." Describe what HAPPENED, do not pitch the benefit.

Reply with ONLY a JSON object of this shape:
{"is_workflow":bool,"confidence":number,"name":string,"description":string,"trigger":{"kind":string,"interval_minutes":number,"rationale":string},"steps":[{"kind":"integration"|"llm","action_id":string,"platform":string,"params":object,"result_path":string,"expose":[string],"instruction":string,"feeds_from":string}],"reason":string}`)

	var b strings.Builder
	b.WriteString("HUMAN'S ASK:\n")
	b.WriteString(strings.TrimSpace(in.Goal))
	b.WriteString("\n\nALLOWED action_id values (use only these):\n")
	b.WriteString(strings.Join(in.Shape, ", "))
	b.WriteString("\n\nTRACE (each integration call in order, args masked, response shape only):\n")
	for i, tr := range in.Traces {
		argsJSON, _ := json.Marshal(tr.Args)
		fmt.Fprintf(&b, "%d. %s (%s)\n   args: %s\n   result: %s\n",
			i+1, tr.ActionID, tr.Platform, string(argsJSON), truncateForPrompt(tr.Result, 600))
	}
	return system, b.String()
}

func truncateForPrompt(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// ParseExtraction decodes a model response into an Extraction, tolerating a
// ```json code fence and surrounding prose.
func ParseExtraction(raw string) (Extraction, error) {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		s = strings.TrimPrefix(s, "json")
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	// Narrow to the outermost JSON object.
	if a, b := strings.Index(s, "{"), strings.LastIndex(s, "}"); a >= 0 && b > a {
		s = s[a : b+1]
	}
	var e Extraction
	if err := json.Unmarshal([]byte(strings.TrimSpace(s)), &e); err != nil {
		return Extraction{}, fmt.Errorf("parse extraction: %w", err)
	}
	return e, nil
}
