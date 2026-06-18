package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func requestIsActive(req humanInterview) bool {
	status := strings.ToLower(strings.TrimSpace(req.Status))
	if req.Answered != nil {
		return false
	}
	return status == "" || status == "pending" || status == "open"
}

// recentApprovalReuseWindow controls how long an already-approved
// request can short-circuit a new same-dedupe-key call. Within this
// window, any retry of the same external action by the same agent
// reuses the existing approval and proceeds straight to execute —
// no new request, no human re-prompt. Outside the window, the agent
// gets a fresh prompt because the human's earlier intent may have
// gone stale. 5 minutes balances "obvious retry" with "explicit
// re-confirm" for slow tool loops.
const recentApprovalReuseWindow = 5 * time.Minute

func requestBlocksMessages(req humanInterview) bool {
	if !requestIsActive(req) || !req.Blocking {
		return false
	}
	return normalizeRequestKind(req.Kind) != "interview"
}

func requestIsHumanInterview(req humanInterview) bool {
	return normalizeRequestKind(req.Kind) == "interview"
}

func requestNeedsHumanDecision(req humanInterview) bool {
	switch strings.TrimSpace(req.Kind) {
	case "approval", "confirm", "choice", "connect", "fallback":
		return true
	default:
		return req.Required
	}
}

func requestOptionDefaults(kind string) ([]interviewOption, string) {
	switch normalizeRequestKind(kind) {
	case "approval":
		return []interviewOption{
			{ID: "approve", Label: "Approve", Description: "Green-light this and let the team execute immediately."},
			{ID: "approve_with_note", Label: "Approve with note", Description: "Proceed, but attach explicit constraints or guardrails.", RequiresText: true, TextHint: "Type the conditions, constraints, or guardrails the team must follow."},
			{ID: "needs_more_info", Label: "Need more info", Description: "Gather more context before making the approval call."},
			{ID: "reject", Label: "Reject", Description: "Do not proceed with this."},
			{ID: "reject_with_steer", Label: "Reject with steer", Description: "Do not proceed as proposed. Redirect the team with clearer steering.", RequiresText: true, TextHint: "Type the steering, redirect, or rationale for rejecting this request."},
		}, "approve"
	case "connect":
		// A typed connection decision (the user's "block on a Connect decision"
		// call). connect drives the existing Composio OAuth flow; skip abandons
		// the parked external action. Neither needs free text.
		return []interviewOption{
			{ID: "connect", Label: "Connect", Description: "Authorize this integration so the team can run the action."},
			{ID: "skip", Label: "Skip", Description: "Do not connect. Cancel this external action."},
		}, "connect"
	case "fallback":
		// A platform with no Composio path: the human completes the action
		// manually (mark_done) or abandons it (skip). One CLI is product-removed,
		// so manual handoff is the only fallback.
		return []interviewOption{
			{ID: "mark_done", Label: "Mark done", Description: "I completed this manually outside the team."},
			{ID: "skip", Label: "Skip", Description: "Do not do this. Cancel the action."},
		}, "mark_done"
	case "confirm":
		return []interviewOption{
			{ID: "confirm_proceed", Label: "Confirm", Description: "Looks good. Proceed as planned."},
			{ID: "adjust", Label: "Adjust", Description: "Proceed only after applying the changes you specify.", RequiresText: true, TextHint: "Type the changes that must happen before proceeding."},
			{ID: "reassign", Label: "Reassign", Description: "Move this to a different owner or scope.", RequiresText: true, TextHint: "Type who should own this instead, or how the scope should change."},
			{ID: "hold", Label: "Hold", Description: "Do not act yet. Keep this pending for review."},
		}, "confirm_proceed"
	case "choice":
		return []interviewOption{
			{ID: "move_fast", Label: "Move fast", Description: "Bias toward speed. Ship now and iterate later."},
			{ID: "balanced", Label: "Balanced", Description: "Balance speed, risk, and quality."},
			{ID: "be_careful", Label: "Be careful", Description: "Bias toward caution and a tighter review loop."},
			{ID: "needs_more_info", Label: "Need more info", Description: "Gather more context before deciding.", RequiresText: true, TextHint: "Type what is missing or what should be investigated next."},
			{ID: "delegate", Label: "Delegate", Description: "Hand this to a specific owner for a closer call.", RequiresText: true, TextHint: "Type who should own this decision and any guidance for them."},
		}, "balanced"
	case "interview":
		return []interviewOption{
			{ID: "answer_directly", Label: "Answer directly", Description: "Respond in your own words below.", RequiresText: true, TextHint: "Type your answer for the team."},
			{ID: "need_more_context", Label: "Need more context", Description: "Ask the office to bring back more context before you decide.", RequiresText: true, TextHint: "Type what context is missing or what should be clarified next."},
		}, "answer_directly"
	case "notice":
		// Non-blocking FYI raised by the deterministic completion hook
		// (task_completion_hook.go): "<task> delivered ...". One option —
		// acknowledging clears the row from the Inbox. Never blocking,
		// never required (requestNeedsHumanDecision falls through to
		// req.Required for this kind).
		return []interviewOption{
			{ID: "acknowledge", Label: "Acknowledge", Description: "Got it — clear this notice."},
		}, "acknowledge"
	case "freeform", "secret":
		return []interviewOption{
			{ID: "proceed", Label: "Proceed", Description: "Let the team handle it with their best judgment."},
			{ID: "give_direction", Label: "Give direction", Description: "Proceed, but only after you provide specific guidance.", RequiresText: true, TextHint: "Type the direction or constraints the team should follow."},
			{ID: "delegate", Label: "Delegate", Description: "Route this to a specific person.", RequiresText: true, TextHint: "Type who should own this and what they should do."},
			{ID: "hold", Label: "Hold", Description: "Pause until you review this further."},
		}, "proceed"
	default:
		return []interviewOption{
			{ID: "proceed", Label: "Proceed", Description: "Let the team handle it with their best judgment."},
			{ID: "give_direction", Label: "Give direction", Description: "Add specific guidance the team should follow.", RequiresText: true, TextHint: "Provide the direction or constraints the team should follow."},
			{ID: "delegate", Label: "Delegate", Description: "Route this to a specific person or role.", RequiresText: true, TextHint: "Name the person or role that should own the next call."},
			{ID: "hold", Label: "Hold", Description: "Pause until you review this further."},
		}, "proceed"
	}
}

func enrichRequestOptions(kind string, options []interviewOption) []interviewOption {
	if len(options) == 0 {
		defaults, _ := requestOptionDefaults(kind)
		return defaults
	}
	defaults, _ := requestOptionDefaults(kind)
	meta := make(map[string]interviewOption, len(defaults))
	for _, option := range defaults {
		meta[strings.TrimSpace(option.ID)] = option
	}
	out := make([]interviewOption, 0, len(options))
	for _, option := range options {
		id := strings.TrimSpace(option.ID)
		option.Label = strings.TrimSpace(option.Label)
		option.Description = strings.TrimSpace(option.Description)
		option.TextHint = strings.TrimSpace(option.TextHint)
		if id == "" && option.Label != "" {
			id = normalizeRequestOptionID(option.Label)
			option.ID = id
		}
		if base, ok := meta[id]; ok {
			if !option.RequiresText {
				option.RequiresText = base.RequiresText
			}
			if strings.TrimSpace(option.TextHint) == "" {
				option.TextHint = base.TextHint
			}
			if strings.TrimSpace(option.Label) == "" {
				option.Label = base.Label
			}
			if strings.TrimSpace(option.Description) == "" {
				option.Description = base.Description
			}
		}
		out = append(out, option)
	}
	return out
}

func normalizeRequestOptions(kind, recommendedID string, options []interviewOption) ([]interviewOption, string) {
	normalized := enrichRequestOptions(kind, options)
	recommendedID = strings.TrimSpace(recommendedID)
	if recommendedID != "" {
		for _, option := range normalized {
			if strings.TrimSpace(option.ID) == recommendedID {
				return normalized, recommendedID
			}
		}
	}
	_, fallback := requestOptionDefaults(kind)
	for _, option := range normalized {
		if strings.TrimSpace(option.ID) == fallback {
			return normalized, fallback
		}
	}
	if len(normalized) > 0 {
		return normalized, strings.TrimSpace(normalized[0].ID)
	}
	return normalized, fallback
}

func findRequestOption(req humanInterview, choiceID string) *interviewOption {
	choiceID = strings.TrimSpace(choiceID)
	if choiceID == "" {
		return nil
	}
	for i := range req.Options {
		if strings.TrimSpace(req.Options[i].ID) == choiceID {
			return &req.Options[i]
		}
	}
	return nil
}

func formatRequestAnswerMessage(req humanInterview, answer interviewAnswer) string {
	if req.Secret {
		return fmt.Sprintf("Answered @%s's request privately.", req.From)
	}
	custom := strings.TrimSpace(answer.CustomText)
	switch strings.TrimSpace(answer.ChoiceID) {
	case "approve":
		return fmt.Sprintf("Approved @%s's request.", req.From)
	case "approve_with_note":
		if custom != "" {
			return fmt.Sprintf("Approved @%s's request with note: %s", req.From, custom)
		}
		return fmt.Sprintf("Approved @%s's request with a note.", req.From)
	case "reject":
		return fmt.Sprintf("Rejected @%s's request.", req.From)
	case "reject_with_steer":
		if custom != "" {
			return fmt.Sprintf("Rejected @%s's request with steering: %s", req.From, custom)
		}
		return fmt.Sprintf("Rejected @%s's request with steering.", req.From)
	case "confirm_proceed":
		return fmt.Sprintf("Confirmed @%s's request.", req.From)
	case "adjust":
		if custom != "" {
			return fmt.Sprintf("Requested adjustments from @%s: %s", req.From, custom)
		}
		return fmt.Sprintf("Requested adjustments from @%s.", req.From)
	case "reassign":
		if custom != "" {
			return fmt.Sprintf("Reassigned @%s's request: %s", req.From, custom)
		}
		return fmt.Sprintf("Reassigned @%s's request.", req.From)
	case "hold":
		return fmt.Sprintf("Put @%s's request on hold.", req.From)
	case "delegate":
		if custom != "" {
			return fmt.Sprintf("Delegated @%s's request: %s", req.From, custom)
		}
		return fmt.Sprintf("Delegated @%s's request.", req.From)
	case "needs_more_info":
		if custom != "" {
			return fmt.Sprintf("Asked @%s for more information: %s", req.From, custom)
		}
		return fmt.Sprintf("Asked @%s for more information.", req.From)
	case "connect":
		return fmt.Sprintf("Connected the integration @%s needs.", req.From)
	case "mark_done":
		return fmt.Sprintf("Marked @%s's manual handoff done.", req.From)
	case "acknowledge":
		return fmt.Sprintf("Acknowledged @%s's notice.", req.From)
	case "skip":
		return fmt.Sprintf("Skipped @%s's request.", req.From)
	}
	if custom != "" && strings.TrimSpace(answer.ChoiceText) != "" {
		return fmt.Sprintf("Answered @%s's request with %s: %s", req.From, answer.ChoiceText, custom)
	}
	if custom != "" {
		return fmt.Sprintf("Answered @%s's request: %s", req.From, custom)
	}
	if strings.TrimSpace(answer.ChoiceText) != "" {
		return fmt.Sprintf("Answered @%s's request: %s", req.From, answer.ChoiceText)
	}
	return fmt.Sprintf("Answered @%s's request.", req.From)
}

func activeRequests(requests []humanInterview) []humanInterview {
	out := make([]humanInterview, 0, len(requests))
	for _, req := range requests {
		if requestIsActive(req) {
			out = append(out, req)
		}
	}
	return out
}

func firstBlockingRequest(requests []humanInterview) *humanInterview {
	for i := range requests {
		if requestBlocksMessages(requests[i]) {
			req := requests[i]
			return &req
		}
	}
	return nil
}

// firstBlockingRequestInChannel scopes the chat gate to one channel: a
// blocking request parks NEW chat in ITS channel until answered, but must
// not gag the human everywhere else (ICP-eval v3 fix family #2: one buried
// card must never wedge the office). Requests with an empty channel are
// treated as #general.
func firstBlockingRequestInChannel(requests []humanInterview, channel string) *humanInterview {
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	for i := range requests {
		if !requestBlocksMessages(requests[i]) {
			continue
		}
		reqChannel := normalizeChannelSlug(requests[i].Channel)
		if reqChannel == "" {
			reqChannel = "general"
		}
		if reqChannel == channel {
			req := requests[i]
			return &req
		}
	}
	return nil
}

// AgentAwaitingInterviewAnswer reports whether slug has an active
// human_interview pending — its current turn is parked in the
// /interview/answer poll loop. The notifier delivery path uses this to
// suppress NEW turns for the asking agent only, instead of the old
// office-wide drop (v3 [19:23:59]: one unanswered interview silenced every
// agent, including a librarian directly @-mentioned in another channel).
func (b *Broker) AgentAwaitingInterviewAnswer(slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if b == nil || slug == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, req := range b.requests {
		if !requestIsHumanInterview(req) || !requestIsActive(req) {
			continue
		}
		if strings.ToLower(strings.TrimSpace(req.From)) == slug {
			return true
		}
		// Subscribers merged onto this interview (also_asking) are parked
		// in the same /interview/answer poll loop as the original asker.
		for _, also := range req.AlsoAsking {
			if strings.ToLower(strings.TrimSpace(also)) == slug {
				return true
			}
		}
	}
	return false
}

func firstActiveHumanInterview(requests []humanInterview) *humanInterview {
	for i := range requests {
		if requestIsHumanInterview(requests[i]) && requestIsActive(requests[i]) {
			req := requests[i]
			return &req
		}
	}
	return nil
}

func humanSenderMayCancelInterviews(sender string) bool {
	return isHumanMessageSender(sender)
}

func (b *Broker) cancelRequestLocked(req *humanInterview, actor, reason string) {
	if req == nil || !requestIsActive(*req) {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	req.Status = "canceled"
	req.UpdatedAt = now
	req.ReminderAt = ""
	req.FollowUpAt = ""
	req.RecheckAt = ""
	req.DueAt = ""
	b.completeSchedulerJobsLocked("request", req.ID, req.Channel)
	b.resolveWatchdogAlertsLocked("request", req.ID, req.Channel)
	summary := truncateSummary(strings.TrimSpace(reason+" "+req.Title+" "+req.Question), 140)
	b.appendActionLocked("request_canceled", "office", req.Channel, actor, summary, req.ID)
	b.pendingInterview = firstBlockingRequest(b.requests)
}

func (b *Broker) cancelActiveHumanInterviewsLocked(actor, reason, channel, replyTo string) int {
	count := 0
	targetChannel := normalizeChannelSlug(channel)
	targetReplyTo := strings.TrimSpace(replyTo)
	for i := range b.requests {
		if !requestIsHumanInterview(b.requests[i]) || !requestIsActive(b.requests[i]) {
			continue
		}
		reqChannel := normalizeChannelSlug(b.requests[i].Channel)
		if targetChannel != "" && reqChannel != targetChannel {
			continue
		}
		if targetReplyTo != "" && strings.TrimSpace(b.requests[i].ReplyTo) != targetReplyTo {
			continue
		}
		b.cancelRequestLocked(&b.requests[i], actor, reason)
		count++
		break
	}
	return count
}

// raiseDefinitionGapInterviewLocked is the deterministic E5 intake gate
// (ten-out-of-ten Wave E): when an agent lands a Definition that still
// carries placeholder markers ("[CONTACT NAME]", "NEEDS CONFIRMATION",
// "TBD") or names access the team does not have, the broker raises ONE
// batched human interview for the task — by contract, not prompt-hope
// (v3's CEO wrote around the holes without asking). Caller holds b.mu.
//
// Idempotent: an active human interview already linked to the task (any
// asker) or an earlier gap-interview with the same dedupe key short-
// circuits, so a re-define never stacks duplicates. Human-authored
// definitions never raise — the human wrote the placeholder knowingly.
// Returns the request id ("" when nothing was raised).
func (b *Broker) raiseDefinitionGapInterviewLocked(task *teamTask, actor string) string {
	if task == nil || task.Definition == nil || isHumanMessageSender(actor) {
		return ""
	}
	gaps := definitionGapMarkers(task.Definition)
	if len(gaps) == 0 && len(task.Definition.AccessNeeded) == 0 {
		return ""
	}
	dedupeKey := "definition-gap:" + task.ID
	for i := range b.requests {
		if !requestIsActive(b.requests[i]) {
			continue
		}
		if strings.TrimSpace(b.requests[i].DedupeKey) == dedupeKey {
			return b.requests[i].ID
		}
		if requestIsHumanInterview(b.requests[i]) && strings.TrimSpace(b.requests[i].IssueID) == task.ID {
			return b.requests[i].ID
		}
	}

	channel := normalizeChannelSlug(task.Channel)
	if channel == "" {
		channel = "general"
	}
	from := strings.TrimSpace(actor)
	if from == "" {
		from = "office"
	}
	var qb strings.Builder
	fmt.Fprintf(&qb, "Before the team starts %q, a few details are missing:\n", strings.TrimSpace(task.Title))
	for _, gap := range gaps {
		qb.WriteString("- " + gap + "\n")
	}
	for _, access := range task.Definition.AccessNeeded {
		qb.WriteString("- access needed: " + access + "\n")
	}
	qb.WriteString("Reply with the missing details (or tell the team to proceed as-is).")

	options, recommended := requestOptionDefaults("interview")
	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	req := humanInterview{
		ID:            fmt.Sprintf("request-%d", b.counter),
		Kind:          "interview",
		Status:        "pending",
		From:          from,
		Channel:       channel,
		Title:         "Missing details for " + task.ID,
		Question:      qb.String(),
		Options:       options,
		RecommendedID: recommended,
		DedupeKey:     dedupeKey,
		IssueID:       task.ID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	b.scheduleRequestLifecycleLocked(&req)
	// Loud ask: announce in the task channel and anchor the thread so a
	// reply routes back as the answer (same contract as handlePostRequest).
	b.postRequestRaisedChatMessageLocked(&req)
	b.requests = append(b.requests, req)
	b.pendingInterview = firstBlockingRequest(b.requests)
	b.appendActionLocked("request_created", "office", channel, from,
		truncateSummary(req.Title+" "+req.Question, 140), req.ID)
	return req.ID
}

func normalizeRequestKind(kind string) string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" {
		return "choice"
	}
	return kind
}

func normalizeRequestOptionID(label string) string {
	label = strings.TrimSpace(strings.ToLower(label))
	label = strings.ReplaceAll(label, "-", "_")
	label = strings.ReplaceAll(label, " ", "_")
	return label
}

func (b *Broker) handleRequests(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetRequests(w, r)
	case http.MethodPost:
		b.handlePostRequest(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleGetRequests(w http.ResponseWriter, r *http.Request) {
	channel := normalizeChannelSlug(r.URL.Query().Get("channel"))
	scope := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope")))
	// scope=all returns requests across every channel the viewer can access. The
	// broker's blocking check (handlePostMessage, PostMessage) is global, so the
	// web UI's overlay/interview bar need the same cross-channel view to render
	// what's actually blocking the human.
	allChannels := scope == "all" || scope == "global"
	if !allChannels && channel == "" {
		channel = "general"
	}
	viewerSlug := strings.TrimSpace(r.URL.Query().Get("viewer_slug"))
	includeResolved := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_resolved")), "true")
	b.mu.Lock()
	if !allChannels && !b.canAccessChannelLocked(viewerSlug, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	requests := make([]humanInterview, 0, len(b.requests))
	for _, req := range b.requests {
		reqChannel := normalizeChannelSlug(req.Channel)
		if reqChannel == "" {
			reqChannel = "general"
		}
		if allChannels {
			if !b.canAccessChannelLocked(viewerSlug, reqChannel) {
				continue
			}
		} else if reqChannel != channel {
			continue
		}
		if !includeResolved && !requestIsActive(req) {
			continue
		}
		requests = append(requests, cloneHumanInterview(req))
	}
	pending := firstBlockingRequest(requests)
	b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"channel":  channel,
		"scope":    scope,
		"requests": requests,
		"pending":  pending,
	})
}

func (b *Broker) handlePostRequest(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action        string            `json:"action"`
		ID            string            `json:"id"`
		Kind          string            `json:"kind"`
		From          string            `json:"from"`
		Channel       string            `json:"channel"`
		Title         string            `json:"title"`
		Question      string            `json:"question"`
		Context       string            `json:"context"`
		Options       []interviewOption `json:"options"`
		RecommendedID string            `json:"recommended_id"`
		Blocking      bool              `json:"blocking"`
		Required      bool              `json:"required"`
		Secret        bool              `json:"secret"`
		ReplyTo       string            `json:"reply_to"`
		DedupeKey     string            `json:"dedupe_key"`
		IssueID       string            `json:"issue_id"`
		// IntegrationAction carries the structured external-action payload
		// (slice 4b) for approval cards: typed fields + the masked raw envelope.
		IntegrationAction    *approvalActionPayload `json:"integration_action,omitempty"`
		ConnectionUnverified bool                   `json:"connection_unverified,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	action := strings.TrimSpace(body.Action)
	if action == "" {
		action = "create"
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	switch action {
	case "create":
		if strings.TrimSpace(body.From) == "" || strings.TrimSpace(body.Question) == "" {
			http.Error(w, "from and question required", http.StatusBadRequest)
			return
		}
		channel := normalizeChannelSlug(body.Channel)
		if channel == "" {
			channel = "general"
		}
		if b.findChannelLocked(channel) == nil {
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
		if !b.canAccessChannelLocked(body.From, channel) {
			http.Error(w, "channel access denied", http.StatusForbidden)
			return
		}
		// Dedupe: if a dedupe_key is set and an active request with the
		// same key already exists in this channel, return that existing
		// request instead of stacking a duplicate. Without this, agent
		// retries of the same external action (or multiple agents
		// hitting the same gate) pile up dozens of identical pending
		// approvals — observed as 100+ stacked "Approve gmail action"
		// requests after a single connect-and-retry sequence.
		//
		// Two cases:
		//   (a) Active request with same key → return it.
		//   (b) RECENTLY-ANSWERED request (within recentApprovalReuseWindow)
		//       with same key AND the answer was approve → return the
		//       answered request directly. The agent's poll loop sees
		//       the existing approval immediately and proceeds to
		//       execute without re-prompting the human. This is what
		//       fixes the "I approved it but it asked again" loop:
		//       after the human approves, any same-key retry within
		//       the window auto-proceeds.
		dedupeKey := strings.TrimSpace(body.DedupeKey)
		if dedupeKey != "" {
			for i := range b.requests {
				if normalizeChannelSlug(b.requests[i].Channel) != channel {
					continue
				}
				if strings.TrimSpace(b.requests[i].DedupeKey) != dedupeKey {
					continue
				}
				// Case (a): active request — return it.
				if requestIsActive(b.requests[i]) {
					existing := cloneHumanInterview(b.requests[i])
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"request":    existing,
						"id":         existing.ID,
						"deduped":    true,
						"dedupe_key": dedupeKey,
					})
					return
				}
				// Case (b): recently approved — short-circuit the
				// poll loop by returning the answered request. Only
				// applies to approval-kind requests (the
				// "I approved but it asked again" loop the user
				// reported); interviews / freeform / choice keep the
				// old behavior of always firing a fresh request after
				// an answer.
				existingKind := normalizeRequestKind(b.requests[i].Kind)
				if existingKind != "approval" && normalizeRequestKind(body.Kind) != "approval" {
					continue
				}
				if answered := b.requests[i].Answered; answered != nil {
					ansAt, err := time.Parse(time.RFC3339, answered.AnsweredAt)
					if err != nil {
						continue
					}
					if time.Since(ansAt) > recentApprovalReuseWindow {
						continue
					}
					choice := strings.ToLower(strings.TrimSpace(answered.ChoiceID))
					if choice != "approve" && choice != "approve_with_note" && choice != "confirm_proceed" {
						continue
					}
					existing := cloneHumanInterview(b.requests[i])
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"request":         existing,
						"id":              existing.ID,
						"deduped":         true,
						"dedupe_key":      dedupeKey,
						"reused_approval": true,
					})
					return
				}
			}
		}
		// Cross-agent semantic dedupe for HUMAN-directed interviews ONLY
		// (see interview_dedup.go). Approval-style gates are excluded by
		// the kind check — their distinct payloads must never collapse;
		// the exact DedupeKey above already absorbs their retries.
		if normalizeRequestKind(body.Kind) == "interview" && !body.Secret {
			question := strings.TrimSpace(body.Question)
			asker := strings.ToLower(strings.TrimSpace(body.From))
			// (a) ACTIVE similar interview → no second card. Attach the
			// asker as a subscriber; it polls the existing request id, so
			// the human's one answer reaches it exactly like the original.
			if existing := b.findActiveSimilarInterviewLocked(question); existing != nil {
				if !interviewAskerKnown(*existing, asker) {
					existing.AlsoAsking = append(existing.AlsoAsking, asker)
					existing.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					b.appendActionLocked("request_subscribed", "office", existing.Channel, asker,
						truncateSummary("Joined "+existing.ID+" — same question already pending: "+existing.Question, 140), existing.ID)
					if err := b.saveLocked(); err != nil {
						http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
						return
					}
				}
				clone := cloneHumanInterview(*existing)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"request":     clone,
					"id":          clone.ID,
					"deduped":     true,
					"merged_into": clone.ID,
				})
				return
			}
			// (b) recently ANSWERED similar interview → hand the existing
			// answer straight back; do not re-prompt the human.
			if answered, answer := b.recentlyAnsweredSimilarInterviewLocked(question); answered != nil {
				clone := cloneHumanInterview(*answered)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"request":          clone,
					"id":               clone.ID,
					"deduped":          true,
					"already_answered": true,
					"answer":           answer,
				})
				return
			}
		}
		b.counter++
		req := humanInterview{
			ID:                   fmt.Sprintf("request-%d", b.counter),
			Kind:                 normalizeRequestKind(body.Kind),
			Status:               "pending",
			From:                 strings.TrimSpace(body.From),
			Channel:              channel,
			Title:                strings.TrimSpace(body.Title),
			Question:             strings.TrimSpace(body.Question),
			Context:              strings.TrimSpace(body.Context),
			Options:              body.Options,
			RecommendedID:        "",
			Blocking:             body.Blocking,
			Required:             body.Required,
			Secret:               body.Secret,
			ReplyTo:              strings.TrimSpace(body.ReplyTo),
			DedupeKey:            dedupeKey,
			IssueID:              strings.TrimSpace(body.IssueID),
			Action:               sanitizeApprovalActionPayload(body.IntegrationAction),
			ConnectionUnverified: body.ConnectionUnverified,
			CreatedAt:            time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:            time.Now().UTC().Format(time.RFC3339),
		}
		req.Options, req.RecommendedID = normalizeRequestOptions(req.Kind, strings.TrimSpace(body.RecommendedID), req.Options)
		if requestNeedsHumanDecision(req) {
			req.Blocking = true
			req.Required = true
		}
		if requestIsHumanInterview(req) {
			req.Blocking = false
			req.Required = false
		}
		if req.Title == "" {
			req.Title = "Request"
		}
		b.scheduleRequestLifecycleLocked(&req)
		// Loud ask (v3 fix family #2): announce the request in its channel
		// and, when it has no thread anchor, make the announcement the
		// anchor so a thread reply routes back as the answer. Must run
		// before the append below so the stamped ReplyTo persists.
		b.postRequestRaisedChatMessageLocked(&req)
		b.requests = append(b.requests, req)
		b.pendingInterview = firstBlockingRequest(b.requests)
		b.appendActionLocked("request_created", "office", channel, req.From, truncateSummary(req.Title+" "+req.Question, 140), req.ID)
		if err := b.saveLocked(); err != nil {
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"request": req, "id": req.ID})
	case "cancel":
		id := strings.TrimSpace(body.ID)
		if id == "" {
			http.Error(w, "id required", http.StatusBadRequest)
			return
		}
		for i := range b.requests {
			if b.requests[i].ID != id {
				continue
			}
			b.cancelRequestLocked(&b.requests[i], body.From, "")
			if err := b.saveLocked(); err != nil {
				http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"request": cloneHumanInterview(b.requests[i])})
			return
		}
		http.Error(w, "request not found", http.StatusNotFound)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}

func (b *Broker) handleRequestAnswer(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetRequestAnswer(w, r)
	case http.MethodPost:
		b.handlePostRequestAnswer(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleGetRequestAnswer(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	for _, req := range b.requests {
		if req.ID != id {
			continue
		}
		if req.Answered != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"answered": req.Answered, "status": req.Status})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answered": nil, "status": req.Status})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"answered": nil, "status": "not_found"})
}

func (b *Broker) handlePostRequestAnswer(w http.ResponseWriter, r *http.Request) {
	answerActor := "you"
	if actor, ok := requestActorFromContext(r.Context()); ok && actor.Kind == requestActorKindHuman {
		answerActor = humanMessageSender(actor.Slug)
	}

	var body struct {
		ID         string `json:"id"`
		ChoiceID   string `json:"choice_id"`
		ChoiceText string `json:"choice_text"`
		CustomText string `json:"custom_text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	status, msg := b.answerRequestFromActor(answerActor, body.ID, body.ChoiceID, body.ChoiceText, body.CustomText)
	if status != http.StatusOK {
		http.Error(w, msg, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// answerRequestFromActor resolves an active human interview through the broker's
// canonical answer path and returns an HTTP-style (status, message) pair. It is
// the single home of the answer side effects — scheduler completion, dependent
// unblocking, skill activation/archival, self-heal task creation, the answer
// chat message, and the audit log entry — so every caller (the HTTP handler and
// the Slack Block Kit gate) fires the exact same effects in the exact same
// order. answerActor is the resolved sender string (e.g. "human:mira" or "you"),
// already namespaced by the caller; only a human actor reaches this path because
// the approval gate is human-only. On success it returns (http.StatusOK, "").
func (b *Broker) answerRequestFromActor(answerActor, id, choiceIDRaw, choiceTextRaw, customTextRaw string) (int, string) {
	b.mu.Lock()
	for i := range b.requests {
		if b.requests[i].ID != id {
			continue
		}
		// Reject answers for requests that are no longer active. Without
		// this gate, a second POST against an already-answered or
		// already-canceled request would mutate terminal state and replay
		// the answer side effects (scheduler completion, dependent
		// unblocking, skill activation) for a request that should be
		// immutable after it lands.
		if !requestIsActive(b.requests[i]) {
			b.mu.Unlock()
			return http.StatusConflict, "request is not active"
		}
		choiceID := strings.TrimSpace(choiceIDRaw)
		choiceText := strings.TrimSpace(choiceTextRaw)
		customText := strings.TrimSpace(customTextRaw)
		option := findRequestOption(b.requests[i], choiceID)
		if choiceID != "" && option == nil {
			b.mu.Unlock()
			return http.StatusBadRequest, "unknown request option"
		}
		if option != nil {
			if choiceText == "" {
				choiceText = strings.TrimSpace(option.Label)
			}
			if option.RequiresText && customText == "" {
				hint := strings.TrimSpace(option.TextHint)
				if hint == "" {
					hint = "custom_text required for this response"
				}
				b.mu.Unlock()
				return http.StatusBadRequest, hint
			}
		}
		if choiceID == "" && choiceText == "" && customText == "" {
			b.mu.Unlock()
			return http.StatusBadRequest, "choice_text or custom_text required"
		}
		answer := &interviewAnswer{
			ChoiceID:   choiceID,
			ChoiceText: choiceText,
			CustomText: customText,
			AnsweredAt: time.Now().UTC().Format(time.RFC3339),
		}
		pendingCascade := b.applyRequestAnswerLocked(&b.requests[i], answer, answerActor)

		b.counter++
		// Tag the original asker AND every also_asking subscriber: the
		// answer fans out to all agents merged onto this interview.
		tagged := append([]string{b.requests[i].From}, b.requests[i].AlsoAsking...)
		msg := channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      answerActor,
			Channel:   normalizeChannelSlug(b.requests[i].Channel),
			Tagged:    tagged,
			ReplyTo:   strings.TrimSpace(b.requests[i].ReplyTo),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		msg.Content = formatRequestAnswerMessage(b.requests[i], *answer)
		msg = b.appendMessageLocked(msg)
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			return http.StatusInternalServerError, "failed to persist broker state"
		}
		b.flushPendingAutoNotebookTransitionsLocked(pendingCascade, "system")
		b.mu.Unlock()
		return http.StatusOK, ""
	}
	b.mu.Unlock()
	return http.StatusNotFound, "request not found"
}

// applyRequestAnswerLocked is the single mutation core for answering a
// request: stamps the answer, completes scheduler jobs, releases dependents,
// recomputes the blocking pointer, and appends the request_answered action.
// Shared by the /requests/answer HTTP handler and the human thread-reply
// routing below so both paths have identical side effects. Caller must hold
// b.mu and is responsible for saveLocked + flushing the returned cascade.
func (b *Broker) applyRequestAnswerLocked(req *humanInterview, answer *interviewAnswer, actor string) []pendingTaskTransition {
	if req == nil || answer == nil {
		return nil
	}
	req.Answered = answer
	req.Status = "answered"
	req.UpdatedAt = answer.AnsweredAt
	req.ReminderAt = ""
	req.FollowUpAt = ""
	req.RecheckAt = ""
	req.DueAt = ""
	b.completeSchedulerJobsLocked("request", req.ID, req.Channel)
	pendingCascade := b.unblockDependentsLocked(req.ID)
	b.pendingInterview = firstBlockingRequest(b.requests)
	pendingCascade = append(pendingCascade, b.unblockTasksForAnsweredRequestLocked(*req)...)
	b.maybeCreateApprovedSelfHealTaskLocked(*req)
	// Plan-approval gate: approving a planning task's plan starts it
	// (Planning→Running + dispatch). Any other answer leaves it in Planning.
	b.applyPlanApprovalAnswerLocked(*req, answer, actor)
	b.appendActionLocked("request_answered", "office", req.Channel, actor,
		truncateSummary(formatRequestAnswerMessage(*req, *answer), 140), req.ID)
	return pendingCascade
}

// threadRootLocked walks the reply_to chain of a message id to its thread
// root within one channel. Returns the input id when it has no parent (it
// IS the root) and guards against reply cycles. Caller must hold b.mu.
func (b *Broker) threadRootLocked(channel, msgID string) string {
	msgID = strings.TrimSpace(msgID)
	if msgID == "" {
		return ""
	}
	channel = normalizeChannelSlug(channel)
	byID := make(map[string]string, len(b.messages)) // id -> reply_to
	for _, m := range b.messages {
		if normalizeChannelSlug(m.Channel) != channel {
			continue
		}
		if id := strings.TrimSpace(m.ID); id != "" {
			byID[id] = strings.TrimSpace(m.ReplyTo)
		}
	}
	seen := map[string]bool{}
	current := msgID
	for {
		if seen[current] {
			return current
		}
		seen[current] = true
		parent, ok := byID[current]
		if !ok || parent == "" {
			return current
		}
		current = parent
	}
}

// answerInterviewFromHumanThreadReplyLocked routes a human THREAD reply to
// an active interview as the interview's answer. ICP-eval v3 [19:24:53]:
// the Inbox interview card's only affordance was a "Reply to thread…" box,
// and the submitted reply reached no agent — the requesting agent kept
// polling /interview/answer while the human's answer sat as an ordinary
// chat message. Matching is thread-anchored: the reply's thread root must
// equal the interview's anchor (req.ReplyTo, which request creation now
// guarantees via the raised-chat announcement below). The polling agent's
// human_interview tool returns the reply text in its current turn — the
// answer reaches the agent without any extra wake. Returns the answered
// request's unblock cascade (nil when nothing matched). Caller holds b.mu
// and is responsible for saveLocked + flushing the cascade.
func (b *Broker) answerInterviewFromHumanThreadReplyLocked(msg channelMessage) (answered bool, cascade []pendingTaskTransition) {
	if !isHumanMessageSender(msg.From) {
		return false, nil
	}
	content := strings.TrimSpace(msg.Content)
	replyTo := strings.TrimSpace(msg.ReplyTo)
	if content == "" || replyTo == "" {
		return false, nil
	}
	channel := normalizeChannelSlug(msg.Channel)
	msgRoot := b.threadRootLocked(channel, replyTo)
	for i := range b.requests {
		req := &b.requests[i]
		if !requestIsHumanInterview(*req) || !requestIsActive(*req) {
			continue
		}
		if normalizeChannelSlug(req.Channel) != channel {
			continue
		}
		anchor := strings.TrimSpace(req.ReplyTo)
		if anchor == "" {
			continue
		}
		if anchor != replyTo && b.threadRootLocked(channel, anchor) != msgRoot {
			continue
		}
		answer := &interviewAnswer{
			ChoiceID:   "answer_directly",
			ChoiceText: "Answer directly",
			CustomText: content,
			AnsweredAt: time.Now().UTC().Format(time.RFC3339),
		}
		return true, b.applyRequestAnswerLocked(req, answer, strings.TrimSpace(msg.From))
	}
	return false, nil
}

// postRequestRaisedChatMessageLocked announces a newly raised
// human-decision request in its channel as a system chat message, so the
// ask is visible where the human actually works — not only as an Inbox row
// (ICP-eval v3 [19:23:59]: a blocking interview sat buried in the Inbox for
// 44 minutes with no push while the office stalled behind it). From
// "system" so the announcement can never wake other agents
// (notifyAgentsLoop skips system senders). For requests with no thread
// anchor, the announcement message BECOMES the anchor (req.ReplyTo) so a
// human "Reply to thread…" on it routes back as the interview answer.
// Caller must hold b.mu; the caller's saveLocked persists the message.
func (b *Broker) postRequestRaisedChatMessageLocked(req *humanInterview) {
	if b == nil || req == nil || req.Secret {
		return
	}
	if !requestIsHumanInterview(*req) && !requestNeedsHumanDecision(*req) {
		return
	}
	channel := normalizeChannelSlug(req.Channel)
	if channel == "" {
		channel = "general"
	}
	label := "request"
	if requestIsHumanInterview(*req) {
		label = "interview"
	}
	urgency := ""
	if req.Blocking || req.Required {
		urgency = " (blocking)"
	}
	question := strings.TrimSpace(req.Question)
	content := fmt.Sprintf("❓ @%s asks you%s (%s %s): %s\nAnswer it in the Inbox, or reply in this thread.", req.From, urgency, label, req.ID, question)
	b.counter++
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   channel,
		Kind:      "human_request_raised",
		Title:     strings.TrimSpace(req.Title),
		Content:   content,
		ReplyTo:   strings.TrimSpace(req.ReplyTo),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	msg = b.appendMessageLocked(msg)
	if strings.TrimSpace(req.ReplyTo) == "" {
		req.ReplyTo = msg.ID
	}
}

func (b *Broker) unblockTasksForAnsweredRequestLocked(req humanInterview) []pendingTaskTransition {
	reqID := strings.TrimSpace(req.ID)
	if reqID == "" {
		return nil
	}
	var pending []pendingTaskTransition
	now := time.Now().UTC().Format(time.RFC3339)
	answerText := strings.TrimSpace(reqAnswerSummary(req.Answered))
	for i := range b.tasks {
		task := &b.tasks[i]
		if !task.blocked || strings.EqualFold(strings.TrimSpace(task.status), "done") {
			continue
		}
		haystack := strings.ToLower(strings.TrimSpace(task.Title + "\n" + task.Details))
		if !taskMentionsRequestID(haystack, strings.ToLower(reqID)) {
			continue
		}
		// Even though the title/details mention the answered request, the
		// task may have other unresolved dependencies (a different
		// request, an upstream task). Always record the answer in
		// Details, but only flip Blocked → false when no other deps
		// remain — otherwise the task would race ahead of work it still
		// needs.
		stillBlocked := b.hasUnresolvedDepsLocked(task)
		if !stillBlocked {
			beforeStatus := task.status
			task.blocked = false
			if strings.EqualFold(strings.TrimSpace(task.status), "blocked") {
				if strings.TrimSpace(task.Owner) != "" {
					task.status = "in_progress"
				} else {
					task.status = "open"
				}
			}
			b.queueTaskBehindActiveOwnerLaneLocked(task)
			pending = append(pending, pendingTaskTransition{
				taskID:       task.ID,
				beforeStatus: beforeStatus,
			})
		}
		if answerText != "" && !strings.Contains(task.Details, answerText) {
			task.Details = strings.TrimSpace(task.Details)
			if task.Details != "" {
				task.Details += "\n\n"
			}
			task.Details += fmt.Sprintf("Human answer for %s: %s", reqID, answerText)
		}
		task.UpdatedAt = now
		if !stillBlocked {
			b.appendActionLocked(
				"task_unblocked",
				"office",
				task.Channel,
				req.From,
				truncateSummary(task.Title+" unblocked by answered "+reqID, 140),
				task.ID,
			)
		}
	}
	return pending
}

// taskMentionsRequestID returns true when haystack contains needle as a
// standalone token. Without the boundary check, "request-1" would match
// "request-10", letting an answer to request-1 incorrectly unblock tasks
// that only reference request-10. A token boundary is any non-id-char
// (anything outside [a-z0-9-_]) or the start/end of the string.
func taskMentionsRequestID(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for {
		idx := strings.Index(haystack, needle)
		if idx < 0 {
			return false
		}
		left := idx == 0 || !isRequestIDChar(haystack[idx-1])
		end := idx + len(needle)
		right := end == len(haystack) || !isRequestIDChar(haystack[end])
		if left && right {
			return true
		}
		// Skip one char to keep searching past this rejected match.
		haystack = haystack[idx+1:]
	}
}

func isRequestIDChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '-' || c == '_':
		return true
	}
	return false
}

func reqAnswerSummary(answer *interviewAnswer) string {
	if answer == nil {
		return ""
	}
	if text := strings.TrimSpace(answer.CustomText); text != "" {
		return text
	}
	if text := strings.TrimSpace(answer.ChoiceText); text != "" {
		return text
	}
	return strings.TrimSpace(answer.ChoiceID)
}

func (b *Broker) handleInterview(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetInterview(w, r)
	case http.MethodPost:
		b.handlePostInterview(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handlePostInterview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From          string            `json:"from"`
		Channel       string            `json:"channel"`
		Question      string            `json:"question"`
		Context       string            `json:"context"`
		Options       []interviewOption `json:"options"`
		RecommendedID string            `json:"recommended_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.From) == "" || strings.TrimSpace(body.Question) == "" {
		http.Error(w, "from and question required", http.StatusBadRequest)
		return
	}
	reqBody, _ := json.Marshal(map[string]any{
		"action":         "create",
		"kind":           "interview",
		"title":          "Human interview",
		"from":           body.From,
		"channel":        body.Channel,
		"question":       body.Question,
		"context":        body.Context,
		"options":        body.Options,
		"recommended_id": body.RecommendedID,
		"blocking":       false,
		"required":       false,
	})
	r2 := r.Clone(r.Context())
	r2.Body = io.NopCloser(bytes.NewReader(reqBody))
	b.handlePostRequest(w, r2)
}

func (b *Broker) handleGetInterview(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	pending := firstActiveHumanInterview(b.requests)
	if pending == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"pending": nil})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"pending": pending})
}

func (b *Broker) handleInterviewAnswer(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		b.handleGetInterviewAnswer(w, r)
	case http.MethodPost:
		b.handlePostInterviewAnswer(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleGetInterviewAnswer(w http.ResponseWriter, r *http.Request) {
	b.handleGetRequestAnswer(w, r)
}

func (b *Broker) handlePostInterviewAnswer(w http.ResponseWriter, r *http.Request) {
	b.handlePostRequestAnswer(w, r)
}
