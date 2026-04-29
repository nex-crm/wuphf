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
	case "approval", "confirm", "choice":
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
			{ID: "answer_directly", Label: "Answer directly", Description: "Respond in your own words below."},
			{ID: "need_more_context", Label: "Need more context", Description: "Ask the office to bring back more context before you decide.", RequiresText: true, TextHint: "Type what context is missing or what should be clarified next."},
		}, "answer_directly"
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
	switch normalizeActorSlug(sender) {
	case "", "you", "human":
		return true
	default:
		return false
	}
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
		requests = append(requests, req)
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
		b.counter++
		req := humanInterview{
			ID:            fmt.Sprintf("request-%d", b.counter),
			Kind:          normalizeRequestKind(body.Kind),
			Status:        "pending",
			From:          strings.TrimSpace(body.From),
			Channel:       channel,
			Title:         strings.TrimSpace(body.Title),
			Question:      strings.TrimSpace(body.Question),
			Context:       strings.TrimSpace(body.Context),
			Options:       body.Options,
			RecommendedID: "",
			Blocking:      body.Blocking,
			Required:      body.Required,
			Secret:        body.Secret,
			ReplyTo:       strings.TrimSpace(body.ReplyTo),
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
			UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
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
			_ = json.NewEncoder(w).Encode(map[string]any{"request": b.requests[i]})
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

	b.mu.Lock()
	for i := range b.requests {
		if b.requests[i].ID != body.ID {
			continue
		}
		choiceID := strings.TrimSpace(body.ChoiceID)
		choiceText := strings.TrimSpace(body.ChoiceText)
		customText := strings.TrimSpace(body.CustomText)
		option := findRequestOption(b.requests[i], choiceID)
		if choiceID != "" && option == nil {
			b.mu.Unlock()
			http.Error(w, "unknown request option", http.StatusBadRequest)
			return
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
				http.Error(w, hint, http.StatusBadRequest)
				return
			}
		}
		if choiceID == "" && choiceText == "" && customText == "" {
			b.mu.Unlock()
			http.Error(w, "choice_text or custom_text required", http.StatusBadRequest)
			return
		}
		answer := &interviewAnswer{
			ChoiceID:   choiceID,
			ChoiceText: choiceText,
			CustomText: customText,
			AnsweredAt: time.Now().UTC().Format(time.RFC3339),
		}
		b.requests[i].Answered = answer
		b.requests[i].Status = "answered"
		b.requests[i].UpdatedAt = answer.AnsweredAt
		b.requests[i].ReminderAt = ""
		b.requests[i].FollowUpAt = ""
		b.requests[i].RecheckAt = ""
		b.requests[i].DueAt = ""
		b.completeSchedulerJobsLocked("request", b.requests[i].ID, b.requests[i].Channel)
		b.unblockDependentsLocked(b.requests[i].ID)
		b.pendingInterview = firstBlockingRequest(b.requests)
		b.unblockTasksForAnsweredRequestLocked(b.requests[i])

		// Skill proposal callback: accept activates the skill, reject archives it.
		if b.requests[i].Kind == "skill_proposal" {
			replyTo := strings.TrimSpace(b.requests[i].ReplyTo)
			for j := range b.skills {
				if b.skills[j].Name == replyTo && b.skills[j].Status != "archived" {
					activatedAt := time.Now().UTC().Format(time.RFC3339)
					if choiceID == "accept" {
						b.skills[j].Status = "active"
						b.skills[j].UpdatedAt = activatedAt
						b.counter++
						b.appendMessageLocked(channelMessage{
							ID:        fmt.Sprintf("msg-%d", b.counter),
							From:      "system",
							Channel:   normalizeChannelSlug(b.requests[i].Channel),
							Kind:      "skill_activated",
							Title:     "Skill Activated: " + b.skills[j].Title,
							Content:   fmt.Sprintf("Skill **%s** is now active and ready to use.", b.skills[j].Title),
							Timestamp: activatedAt,
						})
					} else {
						b.skills[j].Status = "archived"
						b.skills[j].UpdatedAt = activatedAt
					}
					break
				}
			}
		}

		b.counter++
		msg := channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      "you",
			Channel:   normalizeChannelSlug(b.requests[i].Channel),
			Tagged:    []string{b.requests[i].From},
			ReplyTo:   strings.TrimSpace(b.requests[i].ReplyTo),
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		msg.Content = formatRequestAnswerMessage(b.requests[i], *answer)
		b.appendMessageLocked(msg)
		b.appendActionLocked("request_answered", "office", b.requests[i].Channel, "you", truncateSummary(msg.Content, 140), b.requests[i].ID)
		if err := b.saveLocked(); err != nil {
			b.mu.Unlock()
			http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
			return
		}
		b.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		return
	}
	b.mu.Unlock()
	http.Error(w, "request not found", http.StatusNotFound)
}

func (b *Broker) unblockTasksForAnsweredRequestLocked(req humanInterview) {
	reqID := strings.TrimSpace(req.ID)
	if reqID == "" {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	answerText := strings.TrimSpace(reqAnswerSummary(req.Answered))
	for i := range b.tasks {
		task := &b.tasks[i]
		if !task.Blocked || strings.EqualFold(strings.TrimSpace(task.Status), "done") {
			continue
		}
		haystack := strings.ToLower(strings.TrimSpace(task.Title + "\n" + task.Details))
		if !strings.Contains(haystack, strings.ToLower(reqID)) {
			continue
		}
		task.Blocked = false
		if strings.EqualFold(strings.TrimSpace(task.Status), "blocked") {
			if strings.TrimSpace(task.Owner) != "" {
				task.Status = "in_progress"
			} else {
				task.Status = "open"
			}
		}
		b.queueTaskBehindActiveOwnerLaneLocked(task)
		if answerText != "" && !strings.Contains(task.Details, answerText) {
			task.Details = strings.TrimSpace(task.Details)
			if task.Details != "" {
				task.Details += "\n\n"
			}
			task.Details += fmt.Sprintf("Human answer for %s: %s", reqID, answerText)
		}
		task.UpdatedAt = now
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
