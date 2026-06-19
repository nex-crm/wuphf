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
//   - It assembles the completed task's transcript + the existing-apps catalog and
//     runs ONE bounded LLM call that only JUDGES + DRAFTS — it returns structured
//     JSON, it cannot call tools.
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

	if strings.TrimSpace(transcript) == "" {
		return
	}

	// Existing apps (read outside b.mu — the app store has its own lock) ground
	// the judge so it improves a related app instead of proposing a duplicate.
	existing := b.existingAppsForDetection()

	system, prompt := buildWorkflowDetectPrompt(t, transcript, existing)
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
		Name:        decision.Name,
		Summary:     decision.Summary,
		Description: decision.Description,
		AppID:       decision.RelatedAppID,
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
// the broker actuates.
func buildWorkflowDetectPrompt(task teamTask, transcript string, existing []detectedApp) (system, user string) {
	system = "You analyze a COMPLETED office task to decide whether it represents a REPEATABLE workflow worth turning into a small internal tool (an \"App\"). " +
		"An App is a read-mostly React tool over the workspace's own data, the user's connected integrations (read actions), and a bounded one-shot ai() step; it can also create a task on a button. " +
		"Propose ONLY when BOTH hold: (1) the work is genuinely repeatable — the human signalled recurrence (\"every week/month/sprint\", \"again\", \"each morning\") or it is plainly periodic; and (2) an App would meaningfully cut the manual effort. " +
		"If an existing app in the list already covers it, set related_app_id to that app's id to propose an IMPROVEMENT instead of a duplicate; if an existing app ALREADY fully covers it, set worth_building=false. " +
		"Ground the draft in what the transcript shows actually happened — name the real inputs, rules, and outputs you observed. Do NOT invent capabilities or data the workspace does not have. " +
		"Respond with EXACTLY ONE JSON object and nothing else: {\"worth_building\": boolean, \"name\": string, \"summary\": string, \"description\": string, \"related_app_id\": string, \"reason\": string}. " +
		"name: short tool name. summary: one line. description: what it does + the workflow it automates + the key inputs/rules/outputs from the transcript. related_app_id: an existing app id to improve, else \"\". reason: one line on the recurrence signal. " +
		"When in doubt, set worth_building=false — a missed suggestion is cheaper than a noisy one."

	var u strings.Builder
	fmt.Fprintf(&u, "COMPLETED TASK\nTitle: %s\n", strings.TrimSpace(task.Title))
	if d := strings.TrimSpace(task.Details); d != "" {
		fmt.Fprintf(&u, "Brief: %s\n", d)
	}
	u.WriteString("\nTRANSCRIPT (human + agents)\n")
	u.WriteString(transcript)
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
