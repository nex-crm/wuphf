package team

import (
	"fmt"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/action"
)

// broker_integrations_connect.go owns the typed `connect` decision card and the
// connection fan-out. When the resolver classifies a mutating action as
// `connect` (the integration is missing/expired but supported), it raises a
// single blocking Connect card per platform. When the existing OAuth flow
// reports the connection live (/integrations/connect-status), the fan-out flips
// the registry to connected, auto-answers the open card, and unblocks the work
// that was parked waiting on it — so the action resumes with zero re-asking.

// connectRequestDedupeKey scopes a connect card to a platform across the WHOLE
// workspace, not per channel: one OAuth connection serves every channel, so a
// second mutating action against the same missing platform must attach to the
// existing card instead of stacking a duplicate.
func connectRequestDedupeKey(platform string) string {
	return "connect:" + connectionRegistryKey(platform)
}

// ensureConnectRequest returns the ID of the active connect card for platform,
// creating one if none exists. Locks b.mu; callers must not already hold it.
func (b *Broker) ensureConnectRequest(platform, channel, agent, name, logoURL string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ensureConnectRequestLocked(platform, channel, agent, name, logoURL)
}

// ensureConnectRequestLocked is the idempotent body: it finds an active connect
// card for the platform (dedupe is workspace-wide) and returns its ID, or mints
// one. The card is a blocking human decision so the channel gates on it — the
// user's "block on a typed Connect decision" call. Caller holds b.mu.
func (b *Broker) ensureConnectRequestLocked(platform, channel, agent, name, logoURL string) string {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return ""
	}
	dedupeKey := connectRequestDedupeKey(platform)
	for i := range b.requests {
		if normalizeRequestKind(b.requests[i].Kind) != "connect" {
			continue
		}
		if strings.TrimSpace(b.requests[i].DedupeKey) != dedupeKey {
			continue
		}
		if requestIsActive(b.requests[i]) {
			return b.requests[i].ID
		}
	}

	display := strings.TrimSpace(name)
	if display == "" {
		display = action.DisplayPlatformName(platform)
	}
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	from := strings.TrimSpace(agent)
	if from == "" {
		from = "office"
	}
	options, recommended := requestOptionDefaults("connect")
	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	req := humanInterview{
		ID:            fmt.Sprintf("request-%d", b.counter),
		Kind:          "connect",
		Status:        "pending",
		From:          from,
		Channel:       channel,
		Title:         "Connect " + display,
		Question:      fmt.Sprintf("Connect %s so the team can run this action.", display),
		Options:       options,
		RecommendedID: recommended,
		Blocking:      true,
		Required:      true,
		Platform:      platform,
		LogoURL:       strings.TrimSpace(logoURL),
		DedupeKey:     dedupeKey,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	req = sanitizeHumanInterview(req)
	b.scheduleRequestLifecycleLocked(&req)
	b.requests = append(b.requests, req)
	b.pendingInterview = firstBlockingRequest(b.requests)
	b.appendActionLocked("integration_connect_requested", "office", channel, from, truncateSummary("Connect "+display, 140), req.ID)
	// Best-effort persist: the card is rebuildable by the next resolve probe, so a
	// failed write must not block the action gate that triggered it.
	_ = b.saveLocked()
	return req.ID
}

// fanOutConnected records a freshly connected platform and resumes everything
// parked on it: the registry flips to connected and any open connect card is
// auto-answered (the human DID connect, via OAuth), which runs the standard
// unblock cascade. Idempotent — repeat polls of connect-status are no-ops once
// the registry is current and the cards are answered. Locks b.mu.
func (b *Broker) fanOutConnected(platform, connectionKey, accountName, actor string) {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return
	}
	b.mu.Lock()
	b.upsertConnectionRegistryLocked(connectionRegistryEntry{
		Platform:      platform,
		Provider:      "composio",
		State:         string(action.StateConnected),
		ConnectionKey: strings.TrimSpace(connectionKey),
		AccountName:   strings.TrimSpace(accountName),
	})
	pending := b.resolveConnectRequestsLocked(platform, actor, "connect", "")
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		return
	}
	b.flushPendingAutoNotebookTransitionsLocked(pending, "system")
	b.mu.Unlock()
}

// resolveConnectRequestsLocked terminally answers every active connect card for
// a platform and runs the unblock cascade. choiceID is "connect" when the OAuth
// flow completed, "skip" when the human declined. Caller holds b.mu. Returns
// pending notebook transitions for the caller to flush after releasing the lock.
func (b *Broker) resolveConnectRequestsLocked(platform, actor, choiceID, reason string) []pendingTaskTransition {
	if connectionRegistryKey(platform) == "" {
		return nil
	}
	if strings.TrimSpace(actor) == "" {
		actor = "system"
	}
	dedupeKey := connectRequestDedupeKey(platform)
	now := time.Now().UTC().Format(time.RFC3339)
	auditKind := "integration_connect_resolved"
	if strings.TrimSpace(choiceID) == "skip" {
		auditKind = "integration_connect_skipped"
	}
	var pending []pendingTaskTransition
	for i := range b.requests {
		if normalizeRequestKind(b.requests[i].Kind) != "connect" {
			continue
		}
		if strings.TrimSpace(b.requests[i].DedupeKey) != dedupeKey {
			continue
		}
		if !requestIsActive(b.requests[i]) {
			continue
		}
		answer := &interviewAnswer{
			ChoiceID:   strings.TrimSpace(choiceID),
			ChoiceText: connectChoiceLabel(choiceID),
			CustomText: strings.TrimSpace(reason),
			AnsweredAt: now,
		}
		b.requests[i].Answered = answer
		b.requests[i].Status = "answered"
		b.requests[i].UpdatedAt = now
		b.requests[i].ReminderAt = ""
		b.requests[i].FollowUpAt = ""
		b.requests[i].RecheckAt = ""
		b.requests[i].DueAt = ""
		b.completeSchedulerJobsLocked("request", b.requests[i].ID, b.requests[i].Channel)
		pending = append(pending, b.unblockDependentsLocked(b.requests[i].ID)...)
		pending = append(pending, b.unblockTasksForAnsweredRequestLocked(b.requests[i])...)

		b.counter++
		msg := channelMessage{
			ID:        fmt.Sprintf("msg-%d", b.counter),
			From:      "system",
			Channel:   normalizeChannelSlug(b.requests[i].Channel),
			Tagged:    []string{b.requests[i].From},
			Timestamp: now,
		}
		msg.Content = formatRequestAnswerMessage(b.requests[i], *answer)
		msg = b.appendMessageLocked(msg)
		b.appendActionLocked(auditKind, "office", b.requests[i].Channel, actor, truncateSummary(msg.Content, 140), b.requests[i].ID)
	}
	b.pendingInterview = firstBlockingRequest(b.requests)
	return pending
}

func connectChoiceLabel(choiceID string) string {
	switch strings.TrimSpace(choiceID) {
	case "connect":
		return "Connect"
	case "skip":
		return "Skip"
	default:
		return strings.TrimSpace(choiceID)
	}
}
