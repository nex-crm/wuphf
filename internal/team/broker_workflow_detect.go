package team

// broker_workflow_detect.go — post-task App discovery.
//
// Mid-work discovery failed in practice: a worker agent told to "propose an App
// if you notice repetition" deferred the propose_app call to a "next turn" that
// never came, or narrated an approval card it never raised. The fix is to take
// discovery OFF the worker entirely and run it DETERMINISTICALLY after a task
// completes:
//
//   - The broker (not an agent) fires this on every task that reaches done.
//   - It GATES on real evidence first: the deterministic workflow miner
//     (workflow_detect.go) clusters the persisted per-turn tool manifests
//     (event_sink.go) and only yields a candidate when this task's tool-shape
//     either recurred across tasks or ran end-to-end to a final outcome. No
//     candidate → no judge call, no proposal. This replaces "ask an LLM to
//     infer repeatability from one transcript" with proven recurrence.
//   - When a candidate exists, it assembles the proven shape + the task
//     transcript + the existing-apps catalog and runs ONE bounded LLM call that
//     only JUDGES + DRAFTS — it returns structured JSON, it cannot call tools.
//   - If the judge says "worth building", the BROKER raises the propose_app card.
//
// Because the broker does the raising, a card either exists or it doesn't — the
// "phantom card" hallucination is structurally impossible. Explicit human asks
// (/create-app, "build an app") still go straight to a build; this is only the
// proactive, post-hoc path.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	workflowDetectTimeout            = 45 * time.Second
	workflowDetectMaxTranscriptMsgs  = 40
	workflowDetectMaxTranscriptBytes = 12 << 10
	workflowDetectMaxAppsListed      = 40
	// appWorkflowRecurrenceFloor is how many times a no-outcome shape must
	// recur before it is App-worthy. Apps are read-mostly tools that may never
	// "send" anything, so unlike the workflow miner's default floor (3) a shape
	// the agent rebuilt twice already justifies a tool. A single task that ran
	// end-to-end to a real outcome verb still surfaces below this floor.
	appWorkflowRecurrenceFloor = 2
	// appWorkflowMinSteps is the distinct work-tool count that makes a task
	// shape a "workflow" worth turning into a tool (below it is a one-liner).
	appWorkflowMinSteps = 2
)

// queueWorkflowAppDetection fires post-completion workflow→App detection for a
// just-delivered task, asynchronously. It is a no-op unless detection is enabled
// (the production web-serve path turns it on), so the unit suite — which completes
// many tasks — never makes a live LLM call here.
func (b *Broker) queueWorkflowAppDetection(taskID string) {
	if b == nil || !b.workflowDetectionEnabled {
		return
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return
	}
	go func() {
		defer recoverPanicTo("detectWorkflowApp", "task="+taskID)
		b.detectWorkflowAppForTask(taskID)
	}()
}

// workflowDetectDecision is the strict shape the judge LLM must return.
type workflowDetectDecision struct {
	WorthBuilding bool   `json:"worth_building"`
	Name          string `json:"name"`
	Summary       string `json:"summary"`
	Description   string `json:"description"`
	RelatedAppID  string `json:"related_app_id"`
	Reason        string `json:"reason"`
}

func (b *Broker) detectWorkflowAppForTask(taskID string) {
	b.mu.Lock()
	task := b.taskByIDLocked(taskID)
	if task == nil || task.System {
		b.mu.Unlock()
		return
	}
	t := *task
	// Skip the App Builder's OWN build/edit tasks — that work IS an app, not a
	// workflow to turn into one.
	if strings.EqualFold(strings.TrimSpace(t.Owner), appBuilderSlug) {
		b.mu.Unlock()
		return
	}
	channel := normalizeChannelSlug(t.Channel)
	transcript := b.renderTaskTranscriptLocked(channel)
	leadSlug := strings.TrimSpace(officeLeadSlugFrom(b.members))
	b.mu.Unlock()

	// Evidence gate: only continue when the deterministic miner shows this
	// task's tool-shape recurred or ran end-to-end. No candidate → no judge.
	cand := detectionCandidateForTask(taskID)
	if cand == nil {
		log.Printf("workflow detect %s: no recurring/end-to-end shape in corpus — skipping", taskID)
		return
	}

	// Existing apps (read outside b.mu — the app store has its own lock) ground
	// the judge so it improves a related app instead of proposing a duplicate.
	existing := b.existingAppsForDetection()

	system, prompt := buildWorkflowDetectPrompt(t, transcript, *cand, existing)
	ctx, cancel := context.WithTimeout(context.Background(), workflowDetectTimeout)
	defer cancel()
	out, err := currentAppsLLMCompleter()(ctx, system, prompt)
	if err != nil {
		log.Printf("workflow detect %s: judge unavailable: %v", taskID, err)
		return // provider unavailable → no proposal, no noise
	}
	decision, ok := parseWorkflowDetectDecision(out)
	if !ok {
		log.Printf("workflow detect %s: could not parse judge output", taskID)
		return
	}
	if !decision.WorthBuilding {
		log.Printf("workflow detect %s: not app-worthy (%s)", taskID, strings.TrimSpace(decision.Reason))
		return
	}
	log.Printf("workflow detect %s: proposing %q (improve=%q)", taskID, decision.Name, decision.RelatedAppID)

	spec := sanitizeAppProposalSpec(&appProposalSpec{
		Name:          decision.Name,
		Summary:       decision.Summary,
		Description:   decision.Description,
		AppID:         decision.RelatedAppID,
		ObservedSteps: cand.Shape,
	})
	if spec == nil || strings.TrimSpace(spec.Description) == "" {
		return
	}
	// A bogus related id → propose a NEW app rather than a broken "improve".
	if spec.AppID != "" && !existingAppHasID(existing, spec.AppID) {
		spec.AppID = ""
	}
	from := leadSlug
	if from == "" {
		from = appBuilderSlug
	}
	b.raiseDetectedAppProposal(channel, from, *spec, existing)
}

// detectionCandidateForTask runs the deterministic miner over the persisted
// tool-manifest corpus and returns the candidate whose cluster includes taskID,
// or nil when this task's shape neither recurred (>= appWorkflowRecurrenceFloor)
// nor ran end-to-end to a final outcome. Read-only; the corpus has its own
// append lock. A missing/empty sink yields nil (no candidates, no error).
func detectionCandidateForTask(taskID string) *DetectionCandidate {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	sinkPath := EventSinkPath()
	if sinkPath == "" {
		// No runtime home → the manifest corpus is never written, so detection
		// can never fire. Log it once-per-task so a silently-disabled detector is
		// diagnosable rather than looking like correct precision.
		log.Printf("workflow detect %s: no runtime home — detection corpus unavailable", taskID)
		return nil
	}
	cands, err := DetectWorkflowsFromSink(sinkPath, DetectOptions{
		MinSteps:                         appWorkflowMinSteps,
		RecurrenceFloor:                  appWorkflowRecurrenceFloor,
		SingleRunRequiresExternalOutcome: true,
	})
	if err != nil {
		log.Printf("workflow detect %s: read corpus: %v", taskID, err)
		return nil
	}
	for i := range cands {
		for _, id := range cands[i].TaskIDs {
			if strings.TrimSpace(id) == taskID {
				c := cands[i]
				return &c
			}
		}
	}
	return nil
}

// renderTaskTranscriptLocked renders the recent human+agent messages in a task's
// channel into a capped plain-text transcript for the judge. Caller holds b.mu.
func (b *Broker) renderTaskTranscriptLocked(channel string) string {
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		return ""
	}
	msgs := make([]channelMessage, 0, workflowDetectMaxTranscriptMsgs)
	for _, m := range b.messages {
		if normalizeChannelSlug(m.Channel) != channel {
			continue
		}
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		msgs = append(msgs, m)
	}
	if len(msgs) > workflowDetectMaxTranscriptMsgs {
		msgs = msgs[len(msgs)-workflowDetectMaxTranscriptMsgs:]
	}
	var b2 strings.Builder
	for _, m := range msgs {
		from := strings.TrimSpace(m.From)
		if from == "" {
			from = "system"
		}
		line := fmt.Sprintf("[%s] %s\n", from, strings.TrimSpace(m.Content))
		if b2.Len()+len(line) > workflowDetectMaxTranscriptBytes {
			break
		}
		b2.WriteString(line)
	}
	return b2.String()
}

// detectedApp is the minimal existing-app shape the judge + dedupe need.
type detectedApp struct {
	ID      string
	Name    string
	Slug    string
	Summary string
}

// existingAppsForDetection lists current apps (capped) for grounding + dedupe.
// Reads the app store (its own lock); never call while holding b.mu.
func (b *Broker) existingAppsForDetection() []detectedApp {
	apps, err := b.appStore().List()
	if err != nil {
		return nil
	}
	out := make([]detectedApp, 0, len(apps))
	for _, a := range apps {
		out = append(out, detectedApp{ID: a.ID, Name: a.Name, Slug: a.Slug, Summary: a.Summary})
		if len(out) >= workflowDetectMaxAppsListed {
			break
		}
	}
	return out
}

func existingAppHasID(apps []detectedApp, id string) bool {
	id = strings.TrimSpace(id)
	for _, a := range apps {
		if a.ID == id {
			return true
		}
	}
	return false
}

// appProposalDedupeKey matches the MCP propose_app key so a detected proposal and
// an agent-raised one collapse onto a single card.
func appProposalDedupeKey(from, name, appID string) string {
	key := "app-proposal:" + strings.ToLower(strings.TrimSpace(from)) + ":" + strings.ToLower(strings.TrimSpace(name))
	if id := strings.TrimSpace(appID); id != "" {
		key += ":" + id
	}
	return key
}

// raiseDetectedAppProposal raises the non-blocking propose_app card for a detected
// workflow, deduped against active proposals and already-built apps.
func (b *Broker) raiseDetectedAppProposal(channel, from string, spec appProposalSpec, existing []detectedApp) {
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	// Don't propose a NEW app whose name collides with one already built.
	if spec.AppID == "" {
		wantSlug := slugifyNotebookEntry(spec.Name)
		for _, a := range existing {
			if a.Slug != "" && a.Slug == wantSlug {
				return
			}
		}
	}
	dedupeKey := appProposalDedupeKey(from, spec.Name, spec.AppID)

	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.requests {
		if normalizeRequestKind(b.requests[i].Kind) != "approval" || b.requests[i].AppProposal == nil {
			continue
		}
		if strings.TrimSpace(b.requests[i].DedupeKey) == dedupeKey && requestIsActive(b.requests[i]) {
			return // already on the board
		}
	}

	verb := "Build a new internal tool"
	if spec.AppID != "" {
		verb = "Improve the app"
	}
	question := fmt.Sprintf("%s: %s?", verb, spec.Name)
	var ctxText strings.Builder
	if s := strings.TrimSpace(spec.Summary); s != "" {
		ctxText.WriteString(s)
		ctxText.WriteString("\n\n")
	}
	ctxText.WriteString("What it does:\n")
	ctxText.WriteString(spec.Description)
	ctxText.WriteString("\n\nSpotted from a completed task that looked like a repeatable workflow. On approval, the App Builder builds it. Approve, Approve with note (to add constraints), or Reject.")

	options, recommended := requestOptionDefaults("approval")
	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	specCopy := spec
	req := humanInterview{
		ID:            fmt.Sprintf("request-%d", b.counter),
		Kind:          "approval",
		Status:        "pending",
		From:          from,
		Channel:       channel,
		Title:         question,
		Question:      question,
		Context:       ctxText.String(),
		Options:       options,
		RecommendedID: recommended,
		Blocking:      false,
		Required:      false,
		AppProposal:   &specCopy,
		DedupeKey:     dedupeKey,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	b.scheduleRequestLifecycleLocked(&req)
	b.postRequestRaisedChatMessageLocked(&req)
	b.requests = append(b.requests, req)
	b.appendActionLocked("app_proposal_detected", "office", channel, from, truncateSummary(req.Title, 140), req.ID)
	if err := b.saveLocked(); err != nil {
		log.Printf("workflow detect: persist proposal: %v", err)
	}
}

// buildWorkflowDetectPrompt composes the judge's system + user prompt. The judge
// only decides + drafts; it never calls a tool. The strict JSON contract is what
// the broker actuates. Recurrence is ALREADY established by the mined shape
// (cand) — the judge's job is whether an App is the right surface for it and to
// draft it grounded in the proven steps.
func buildWorkflowDetectPrompt(task teamTask, transcript string, cand DetectionCandidate, existing []detectedApp) (system, user string) {
	system = "You analyze a COMPLETED office task whose tool-shape was ALREADY proven to recur or run end-to-end, to decide whether it is worth turning into a small internal tool (an \"App\"). " +
		"An App is a read-mostly React tool over the workspace's own data, the user's connected integrations (read actions), and a bounded one-shot ai() step; it can also create a task on a button. " +
		"Repeatability is NOT yours to re-judge — the OBSERVED WORKFLOW SHAPE below is deterministic evidence the work recurs. Decide instead: (1) would an App over this exact shape meaningfully cut the manual effort, and (2) is it App-shaped (a tool a human opens and reads/acts on) rather than pure unattended automation. " +
		"If an existing app in the list already covers it, set related_app_id to that app's id to propose an IMPROVEMENT instead of a duplicate; if an existing app ALREADY fully covers it, set worth_building=false. " +
		"Ground the draft in the observed shape AND what the transcript shows actually happened — name the real inputs, rules, and outputs you observed. Do NOT invent capabilities or data the workspace does not have. " +
		"Respond with EXACTLY ONE JSON object and nothing else: {\"worth_building\": boolean, \"name\": string, \"summary\": string, \"description\": string, \"related_app_id\": string, \"reason\": string}. " +
		"name: short tool name. summary: one line. description: what it does + the workflow it automates + the key inputs/rules/outputs from the shape and transcript. related_app_id: an existing app id to improve, else \"\". reason: one line tying the proposal to the observed shape. " +
		"When in doubt, set worth_building=false — a missed suggestion is cheaper than a noisy one."

	var u strings.Builder
	fmt.Fprintf(&u, "COMPLETED TASK\nTitle: %s\n", strings.TrimSpace(task.Title))
	if d := strings.TrimSpace(task.Details); d != "" {
		fmt.Fprintf(&u, "Brief: %s\n", d)
	}

	u.WriteString("\nOBSERVED WORKFLOW SHAPE (mined deterministically from real tool calls)\n")
	if len(cand.Shape) > 0 {
		fmt.Fprintf(&u, "Steps: %s\n", strings.Join(cand.Shape, " -> "))
	}
	switch {
	case cand.Count > 1:
		agentLabel := strings.TrimSpace(cand.Agent)
		if agentLabel == "" {
			agentLabel = "the same agent"
		}
		fmt.Fprintf(&u, "Evidence: this exact shape recurred across %d tasks by %s.\n", cand.Count, agentLabel)
	case strings.TrimSpace(cand.Outcome) != "":
		fmt.Fprintf(&u, "Evidence: this task ran end-to-end to a final outcome step (%s).\n", strings.TrimSpace(cand.Outcome))
	default:
		u.WriteString("Evidence: this shape met the recurrence floor.\n")
	}

	u.WriteString("\nTRANSCRIPT (human + agents)\n")
	if strings.TrimSpace(transcript) == "" {
		u.WriteString("(no chat transcript — ground the draft in the observed shape and task brief)\n")
	} else {
		u.WriteString(transcript)
	}
	u.WriteString("\nEXISTING APPS (improve one of these instead of duplicating)\n")
	if len(existing) == 0 {
		u.WriteString("(none)\n")
	} else {
		for _, a := range existing {
			fmt.Fprintf(&u, "- %s (id=%s): %s\n", a.Name, a.ID, strings.TrimSpace(a.Summary))
		}
	}
	return system, u.String()
}

// parseWorkflowDetectDecision extracts the judge's JSON verdict, tolerating any
// prose or fences the model adds.
func parseWorkflowDetectDecision(out string) (workflowDetectDecision, bool) {
	raw, ok := extractFirstJSON(out)
	if !ok {
		return workflowDetectDecision{}, false
	}
	var d workflowDetectDecision
	if err := json.Unmarshal(raw, &d); err != nil {
		return workflowDetectDecision{}, false
	}
	return d, true
}
