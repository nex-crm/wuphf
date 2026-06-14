package team

import (
	"context"
	"fmt"
	"os"
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
	display := strings.TrimSpace(name)
	if display == "" {
		display = action.DisplayPlatformName(platform)
	}
	return b.ensureIntegrationDecisionLocked(integrationDecisionCard{
		Kind:      "connect",
		DedupeKey: connectRequestDedupeKey(platform),
		Platform:  platform,
		Channel:   channel,
		Agent:     agent,
		Title:     "Connect " + display,
		Question:  fmt.Sprintf("Connect %s so the team can run this action.", display),
		LogoURL:   logoURL,
		AuditKind: "integration_connect_requested",
	})
}

// integrationDecisionCard describes a blocking integration decision card
// (connect or fallback). Both share the same lifecycle: dedupe on a key, mint a
// blocking human-decision request anchored to a platform, schedule reminders,
// audit. ensureIntegrationDecisionLocked is the single mint path so the two
// kinds cannot drift in how they block, persist, or get scheduled.
type integrationDecisionCard struct {
	Kind      string
	DedupeKey string
	Platform  string
	Channel   string
	Agent     string
	Title     string
	Question  string
	Context   string
	LogoURL   string
	AuditKind string
}

// ensureIntegrationDecisionLocked returns the ID of the active card matching
// spec.DedupeKey, or mints one. Caller holds b.mu.
func (b *Broker) ensureIntegrationDecisionLocked(spec integrationDecisionCard) string {
	platform := strings.TrimSpace(spec.Platform)
	dedupeKey := strings.TrimSpace(spec.DedupeKey)
	kind := normalizeRequestKind(spec.Kind)
	if platform == "" || dedupeKey == "" || kind == "" {
		return ""
	}
	for i := range b.requests {
		if normalizeRequestKind(b.requests[i].Kind) != kind {
			continue
		}
		if strings.TrimSpace(b.requests[i].DedupeKey) != dedupeKey {
			continue
		}
		if requestIsActive(b.requests[i]) {
			return b.requests[i].ID
		}
	}

	channel := normalizeChannelSlug(spec.Channel)
	if channel == "" {
		channel = "general"
	}
	from := strings.TrimSpace(spec.Agent)
	if from == "" {
		from = "office"
	}
	title := strings.TrimSpace(spec.Title)
	options, recommended := requestOptionDefaults(kind)
	now := time.Now().UTC().Format(time.RFC3339)
	b.counter++
	req := humanInterview{
		ID:            fmt.Sprintf("request-%d", b.counter),
		Kind:          kind,
		Status:        "pending",
		From:          from,
		Channel:       channel,
		Title:         title,
		Question:      strings.TrimSpace(spec.Question),
		Context:       strings.TrimSpace(spec.Context),
		Options:       options,
		RecommendedID: recommended,
		Blocking:      true,
		Required:      true,
		Platform:      platform,
		LogoURL:       strings.TrimSpace(spec.LogoURL),
		DedupeKey:     dedupeKey,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	b.scheduleRequestLifecycleLocked(&req)
	// Announce a connect decision in its channel so it surfaces on chat-bridged
	// surfaces (Slack) as a decision message — the Slack connect card upgrades
	// this announcement to an interactive Block Kit card. Scoped to connect: the
	// fallback kind is a sibling concern and keeps its current (inbox-only)
	// surfacing. Mirrors the team_request path's loud-ask announcement; runs
	// before the append so the stamped ReplyTo persists with the request.
	if kind == "connect" {
		b.postRequestRaisedChatMessageLocked(&req)
	}
	b.requests = append(b.requests, req)
	b.pendingInterview = firstBlockingRequest(b.requests)
	auditKind := strings.TrimSpace(spec.AuditKind)
	if auditKind == "" {
		auditKind = "integration_decision_requested"
	}
	b.appendActionLocked(auditKind, "office", channel, from, truncateSummary(title, 140), req.ID)
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
	defer b.mu.Unlock()
	b.upsertConnectionRegistryLocked(connectionRegistryEntry{
		Platform:      platform,
		Provider:      "composio",
		State:         string(action.StateConnected),
		ConnectionKey: strings.TrimSpace(connectionKey),
		AccountName:   strings.TrimSpace(accountName),
	})
	pending := b.resolveConnectRequestsLocked(platform, actor, "connect", "")
	// Best-effort persist. Even if the disk write fails the cards are already
	// answered in memory and the parked work must still flush — a save error is
	// recoverable on the next probe, a wedged task is not. defer Unlock guards
	// against a mid-flight panic leaking the lock.
	if err := b.saveLocked(); err != nil {
		fmt.Fprintf(os.Stderr, "team: fanOutConnected save failed for %s: %v\n", platform, err)
	}
	b.flushPendingAutoNotebookTransitionsLocked(pending, "system")
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

// slackConnectStatus is the glanceable connection status the Slack connect card
// renders in its context block. It is the registry's last-known state mapped to
// the same three words the web ConnectIntegrationCard surfaces: "Connected",
// "Connecting…", or "Not connected". An unknown/missing platform reads as not
// connected (the safe default that keeps the Connect button live).
func (b *Broker) slackConnectStatus(platform string) string {
	entry, ok := b.lookupConnectionRegistry(platform)
	if !ok {
		return "Not connected"
	}
	switch action.ConnectionState(strings.TrimSpace(entry.State)) {
	case action.StateConnected:
		return "Connected"
	case action.StatePending, action.StateChecking:
		return "Connecting…"
	default:
		return "Not connected"
	}
}

// composioConfiguredForConnect reports whether the Composio provider has the
// credentials it needs to start an OAuth connection. The Slack connect card uses
// this to decide between a live "Connect" button (round-trips through Composio)
// and an informational card (no button) when Composio is not set up, so a click
// can never dead-end against an unconfigured provider.
func composioConfiguredForConnect() bool {
	return action.NewComposioFromEnv().Configured()
}

// startSlackIntegrationConnect initiates a Composio OAuth connection for platform
// from a Slack connect-button click. It mirrors handleIntegrationConnect's
// server side: start the connection, record the integration_connect_started
// audit, and return the result whose AuthURL the human opens to finish OAuth.
// The existing connect-status fan-out (handleIntegrationConnectStatus →
// fanOutConnected) auto-answers the parked connect card once the connection goes
// live, so this path never has to poll. Returns an error when Composio is not
// configured or the start call fails.
func (b *Broker) startSlackIntegrationConnect(ctx context.Context, platform, actor string) (action.IntegrationConnectResult, error) {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return action.IntegrationConnectResult{}, fmt.Errorf("connect: platform is empty")
	}
	composio := action.NewComposioFromEnv()
	if !composio.Configured() {
		return action.IntegrationConnectResult{}, fmt.Errorf("composio is not configured")
	}
	result, err := composio.StartIntegrationConnection(ctx, action.IntegrationConnectRequest{
		Provider: "composio",
		Platform: platform,
	})
	if err != nil {
		return action.IntegrationConnectResult{}, fmt.Errorf("start composio connection: %w", err)
	}
	if strings.TrimSpace(actor) == "" {
		actor = "system"
	}
	_ = b.RecordActionWithMetadata(
		"integration_connect_started",
		"composio",
		"general",
		actor,
		fmt.Sprintf("Started %s connection via Composio (Slack)", action.DisplayPlatformName(result.Platform)),
		result.Platform,
		nil,
		"",
		map[string]string{
			"provider": "composio",
			"platform": result.Platform,
			"status":   result.Status,
			"surface":  "slack",
		},
	)
	return result, nil
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

// integrationDecisionTimeout is how long a blocking connect/fallback card may sit
// unanswered before the broker expires it. The card blocks its channel, so an
// integration the human never connects (and never explicitly skips) would
// otherwise wedge the channel forever. The human can always Skip sooner; this is
// the backstop. 60 minutes leaves ample time to complete an OAuth round-trip.
const integrationDecisionTimeout = 60 * time.Minute

// expireStaleIntegrationDecisionsLocked terminally cancels connect/fallback
// cards older than integrationDecisionTimeout and audits the timeout, freeing
// the blocking channel. Caller holds b.mu. Returns true if anything changed.
//
// Slice 3b's "task back to backlog" reduces to cancel + audit here: the connect
// flow does not park a task (the agent already got its tool error when the
// action was blocked), so there is no task to re-queue — the realized behavior
// is unblock + an integration_*_timed_out audit trail.
func (b *Broker) expireStaleIntegrationDecisionsLocked(now time.Time) bool {
	changed := false
	for i := range b.requests {
		kind := normalizeRequestKind(b.requests[i].Kind)
		if kind != "connect" && kind != "fallback" {
			continue
		}
		if !requestIsActive(b.requests[i]) {
			continue
		}
		created, err := time.Parse(time.RFC3339, strings.TrimSpace(b.requests[i].CreatedAt))
		if err != nil || now.Sub(created) < integrationDecisionTimeout {
			continue
		}
		req := &b.requests[i]
		stamp := now.UTC().Format(time.RFC3339)
		req.Status = "canceled"
		req.UpdatedAt = stamp
		req.ReminderAt = ""
		req.FollowUpAt = ""
		req.RecheckAt = ""
		req.DueAt = ""
		b.completeSchedulerJobsLocked("request", req.ID, req.Channel)
		b.resolveWatchdogAlertsLocked("request", req.ID, req.Channel)
		auditKind := "integration_connect_timed_out"
		if kind == "fallback" {
			auditKind = "integration_fallback_timed_out"
		}
		display := strings.TrimSpace(req.Platform)
		if display == "" {
			display = "integration"
		}
		b.appendActionLocked(
			auditKind, "office", req.Channel, "system",
			truncateSummary(fmt.Sprintf("%s decision timed out after %s — card auto-canceled", display, integrationDecisionTimeout), 140),
			req.ID,
		)
		changed = true
	}
	if changed {
		b.pendingInterview = firstBlockingRequest(b.requests)
		_ = b.saveLocked()
	}
	return changed
}
