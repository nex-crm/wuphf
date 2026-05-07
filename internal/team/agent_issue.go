package team

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/agent"
)

const agentIssueMessageKind = "agent_issue"

var agentIssueWhitespacePattern = regexp.MustCompile(`\s+`)

type agentIssueClassification struct {
	Visible       bool
	CapabilityGap bool
	HumanAction   bool
	Severity      string
}

func (b *Broker) ReportAgentIssue(agentSlug, targetChannel, replyTo, detail string) (channelMessage, agentIssueRecord, bool, error) {
	if b == nil {
		return channelMessage{}, agentIssueRecord{}, false, nil
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return channelMessage{}, agentIssueRecord{}, false, nil
	}
	classification := classifyAgentIssue(detail)
	if !classification.Visible {
		return channelMessage{}, agentIssueRecord{}, false, nil
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	agentSlug = strings.TrimSpace(agentSlug)
	if agentSlug == "" {
		agentSlug = "agent"
	}
	channel := normalizeChannelSlug(targetChannel)
	if channel == "" {
		channel = "general"
	}
	if b.findChannelLocked(channel) == nil {
		if IsDMSlug(channel) {
			if dm := b.ensureDMConversationLocked(channel); dm != nil {
				channel = dm.Slug
			}
		}
		if b.findChannelLocked(channel) == nil {
			return channelMessage{}, agentIssueRecord{}, false, fmt.Errorf("channel not found")
		}
	}
	if !b.canAccessChannelLocked(agentSlug, channel) {
		return channelMessage{}, agentIssueRecord{}, false, fmt.Errorf("channel access denied")
	}

	taskID := b.activeTaskIDForAgentLocked(agentSlug)
	key := normalizedAgentIssueKey(agentSlug, channel, detail)
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range b.agentIssues {
		issue := &b.agentIssues[i]
		if issue.Agent != agentSlug || issue.Channel != channel || issue.NormalizedKey != key {
			continue
		}
		issue.Count++
		issue.UpdatedAt = now
		if issue.TaskID == "" {
			issue.TaskID = taskID
		}
		if classification.CapabilityGap && !classification.HumanAction {
			b.ensureSelfHealApprovalRequestLocked(issue, classification, detail)
		}
		if err := b.saveLocked(); err != nil {
			return channelMessage{}, *issue, false, err
		}
		return channelMessage{}, *issue, false, nil
	}

	b.counter++
	issueID := fmt.Sprintf("issue-%d", b.counter)
	issue := agentIssueRecord{
		ID:            issueID,
		Agent:         agentSlug,
		Channel:       channel,
		ReplyTo:       strings.TrimSpace(replyTo),
		Detail:        detail,
		NormalizedKey: key,
		Severity:      classification.Severity,
		TaskID:        taskID,
		Count:         1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	b.counter++
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      agentSlug,
		Channel:   channel,
		Kind:      agentIssueMessageKind,
		EventID:   issue.ID,
		Content:   "Issue: " + truncate(detail, 600),
		ReplyTo:   strings.TrimSpace(replyTo),
		Timestamp: now,
	}
	b.agentIssues = append(b.agentIssues, issue)
	issuePtr := &b.agentIssues[len(b.agentIssues)-1]
	b.appendMessageLocked(msg)
	b.appendActionLocked("agent_issue", "office", channel, agentSlug, truncateSummary(msg.Content, 140), issue.ID)
	if classification.CapabilityGap && !classification.HumanAction {
		b.ensureSelfHealApprovalRequestLocked(issuePtr, classification, detail)
	}
	if err := b.saveLocked(); err != nil {
		return channelMessage{}, *issuePtr, false, err
	}
	return msg, *issuePtr, true, nil
}

func (b *Broker) AgentIssues() []agentIssueRecord {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]agentIssueRecord, len(b.agentIssues))
	copy(out, b.agentIssues)
	return out
}

func (b *Broker) pruneAgentIssuesByChannelLocked(channelSlug string) {
	b.pruneAgentIssuesByChannelAndAgentLocked(channelSlug, "")
}

func (b *Broker) pruneAgentIssuesByChannelAndAgentLocked(channelSlug, agentSlug string) {
	channelSlug = normalizeChannelSlug(channelSlug)
	if channelSlug == "" || len(b.agentIssues) == 0 {
		return
	}
	agentSlug = strings.TrimSpace(agentSlug)
	removedRequestIDs := make(map[string]struct{})
	filtered := b.agentIssues[:0]
	for _, issue := range b.agentIssues {
		if normalizeChannelSlug(issue.Channel) != channelSlug || (agentSlug != "" && strings.TrimSpace(issue.Agent) != agentSlug) {
			filtered = append(filtered, issue)
			continue
		}
		if reqID := strings.TrimSpace(issue.ApprovalRequestID); reqID != "" {
			removedRequestIDs[reqID] = struct{}{}
		}
	}
	b.agentIssues = filtered
	if len(removedRequestIDs) == 0 {
		return
	}
	requests := b.requests[:0]
	for _, req := range b.requests {
		if _, remove := removedRequestIDs[strings.TrimSpace(req.ID)]; !remove {
			requests = append(requests, req)
		}
	}
	b.requests = requests
	b.pendingInterview = firstBlockingRequest(b.requests)
}

func classifyAgentIssue(detail string) agentIssueClassification {
	trimmed := strings.TrimSpace(detail)
	if trimmed == "" {
		return agentIssueClassification{}
	}
	if looksStructuredAgentIssuePayload(trimmed) {
		return agentIssueClassification{}
	}
	text := strings.ToLower(trimmed)
	visibleSignals := []string{
		"error", "failed", "failure", "unavailable", "not available", "not configured",
		"not connected", "missing", "denied", "forbidden", "unauthorized", "requires",
		"cannot", "can't", "unable", "unsupported", "timed out", "timeout",
	}
	visible := false
	for _, signal := range visibleSignals {
		if strings.Contains(text, signal) {
			visible = true
			break
		}
	}
	if !visible {
		return agentIssueClassification{}
	}
	humanAction := strings.Contains(text, "login") ||
		strings.Contains(text, "sign in") ||
		strings.Contains(text, "authenticate") ||
		strings.Contains(text, "oauth") ||
		strings.Contains(text, "two-factor") ||
		strings.Contains(text, "2fa")
	return agentIssueClassification{
		Visible:       true,
		CapabilityGap: isCapabilityGapBlocker(detail),
		HumanAction:   humanAction,
		Severity:      "warning",
	}
}

func looksStructuredAgentIssuePayload(text string) bool {
	if text == "" {
		return false
	}
	if (strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[")) && json.Valid([]byte(text)) {
		return true
	}
	return strings.Contains(text, `":`) && json.Valid([]byte(text))
}

func normalizedAgentIssueKey(agentSlug, channel, detail string) string {
	text := strings.ToLower(strings.TrimSpace(detail))
	for _, prefix := range []string{"issue:", "error:", "failed:", "failure:"} {
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	text = agentIssueWhitespacePattern.ReplaceAllString(text, " ")
	if len(text) > 180 {
		text = text[:180]
	}
	return strings.Join([]string{strings.TrimSpace(agentSlug), normalizeChannelSlug(channel), text}, "|")
}

func (b *Broker) activeTaskIDForAgentLocked(agentSlug string) string {
	agentSlug = strings.TrimSpace(agentSlug)
	if agentSlug == "" {
		return ""
	}
	for i := range b.tasks {
		task := &b.tasks[i]
		if strings.TrimSpace(task.Owner) != agentSlug {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(task.Status), "in_progress") {
			return task.ID
		}
	}
	return ""
}

func (b *Broker) ensureSelfHealApprovalRequestLocked(issue *agentIssueRecord, classification agentIssueClassification, detail string) {
	if b == nil || issue == nil || issue.SelfHealTaskID != "" {
		return
	}
	if req := b.findRequestByIDLocked(issue.ApprovalRequestID); req != nil {
		if requestIsActive(*req) {
			return
		}
		if req.Answered != nil {
			if selfHealApprovalGranted(req.Answered.ChoiceID) {
				b.maybeCreateApprovedSelfHealTaskLocked(*req)
			}
			return
		}
	}

	b.counter++
	now := time.Now().UTC().Format(time.RFC3339)
	req := humanInterview{
		ID:       fmt.Sprintf("request-%d", b.counter),
		Kind:     "approval",
		Status:   "pending",
		From:     "system",
		Channel:  issue.Channel,
		Title:    "Approve self-heal",
		Question: fmt.Sprintf("I recommend creating a self-heal task to restore @%s's missing capability. Proceed?", issue.Agent),
		Context: strings.Join([]string{
			"Agent issue: " + detail,
			"Incident: " + issue.ID,
			"Original task: " + valueOrUnknown(issue.TaskID),
		}, "\n"),
		Options: []interviewOption{
			{ID: "approve", Label: "Proceed", Description: "Create the recommended self-heal task."},
			{ID: "approve_with_note", Label: "Proceed with note", Description: "Create the task with extra constraints.", RequiresText: true, TextHint: "Type constraints or guardrails for the repair task."},
			{ID: "reject", Label: "Dismiss", Description: "Do not create repair work for this issue."},
			{ID: "reject_with_steer", Label: "Override", Description: "Do not use the default repair path. Provide different steering.", RequiresText: true, TextHint: "Type the alternate repair path or reason to skip."},
		},
		RecommendedID: "approve",
		Blocking:      false,
		Required:      false,
		ReplyTo:       strings.TrimSpace(issue.ReplyTo),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	req.Options, req.RecommendedID = normalizeRequestOptions(req.Kind, req.RecommendedID, req.Options)
	b.scheduleRequestLifecycleLocked(&req)
	b.requests = append(b.requests, req)
	b.pendingInterview = firstBlockingRequest(b.requests)
	issue.ApprovalRequestID = req.ID
	issue.UpdatedAt = now
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   issue.Channel,
		Kind:      "approval",
		EventID:   req.ID,
		Title:     req.Title,
		Content:   req.Question,
		Tagged:    uniqueSlugs([]string{issue.Agent}),
		ReplyTo:   strings.TrimSpace(issue.ReplyTo),
		Timestamp: now,
	})
	b.appendActionLocked("request_created", "office", issue.Channel, req.From, truncateSummary(req.Title+" "+req.Question, 140), req.ID)
}

func (b *Broker) findRequestByIDLocked(id string) *humanInterview {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	for i := range b.requests {
		if b.requests[i].ID == id {
			return &b.requests[i]
		}
	}
	return nil
}

func selfHealApprovalGranted(choiceID string) bool {
	switch strings.TrimSpace(choiceID) {
	case "approve", "approve_with_note", "confirm_proceed", "proceed":
		return true
	default:
		return false
	}
}

func (b *Broker) maybeCreateApprovedSelfHealTaskLocked(req humanInterview) {
	if !selfHealApprovalGranted(req.Answered.GetChoiceID()) {
		return
	}
	var issue *agentIssueRecord
	for i := range b.agentIssues {
		if b.agentIssues[i].ApprovalRequestID == req.ID {
			issue = &b.agentIssues[i]
			break
		}
	}
	if issue == nil || issue.SelfHealTaskID != "" {
		return
	}
	detail := issue.Detail
	if note := strings.TrimSpace(req.Answered.GetCustomText()); note != "" {
		detail = strings.TrimSpace(detail + "\n\nHuman constraints: " + note)
	}
	task, _, err := b.requestSelfHealingLocked(issue.Agent, issue.TaskID, agent.EscalationCapabilityGap, detail)
	if err != nil {
		log.Printf("agent-issue: create approved self-heal task for issue=%s agent=%s: %v", issue.ID, issue.Agent, err)
		errText := strings.TrimSpace(err.Error())
		if errText == "" {
			errText = "unknown error"
		}
		alreadyReported := issue.SelfHealError == errText
		now := time.Now().UTC().Format(time.RFC3339)
		issue.SelfHealError = errText
		issue.UpdatedAt = now
		if !alreadyReported {
			b.notifySelfHealCreationFailureLocked(issue, errText, now)
		}
		return
	}
	// requestSelfHealingLocked may return an overflow-merge task — when the
	// agent is at the per-agent cap and this issue's failing TaskID has no
	// self-heal of its own, the incident is merged into a different
	// (agent, taskID) self-heal task. Bind to it either way so the dedupe
	// gates above fire (otherwise the human is re-prompted and the same
	// incident is re-merged on every iteration), but record the overflow in
	// SelfHealError so the divergence between issue.TaskID and the linked
	// task's TaskID is observable instead of silent.
	expectedTitle := selfHealingTaskTitle(issue.Agent, issue.TaskID)
	issue.SelfHealTaskID = task.ID
	if task.Title == expectedTitle {
		issue.SelfHealError = ""
	} else {
		issue.SelfHealError = fmt.Sprintf("merged into agent self-heal overflow lane (%s)", task.ID)
	}
	issue.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
}

func (b *Broker) notifySelfHealCreationFailureLocked(issue *agentIssueRecord, errText, now string) {
	if b == nil || issue == nil {
		return
	}
	agentSlug := strings.TrimSpace(issue.Agent)
	if agentSlug == "" {
		agentSlug = "agent"
	}
	channel := normalizeChannelSlug(issue.Channel)
	if channel == "" {
		channel = "general"
	}
	content := fmt.Sprintf("Issue: approved self-heal for @%s could not be created: %s", agentSlug, truncate(errText, 400))
	b.counter++
	b.appendMessageLocked(channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   channel,
		Kind:      agentIssueMessageKind,
		EventID:   strings.TrimSpace(issue.ID),
		Content:   content,
		Tagged:    uniqueSlugs([]string{agentSlug}),
		ReplyTo:   strings.TrimSpace(issue.ReplyTo),
		Timestamp: now,
	})
	b.appendActionLocked("agent_issue", "office", channel, "system", truncateSummary(content, 140), strings.TrimSpace(issue.ID))
}

func (a *interviewAnswer) GetChoiceID() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.ChoiceID)
}

func (a *interviewAnswer) GetCustomText() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.CustomText)
}

func valueOrUnknown(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}
