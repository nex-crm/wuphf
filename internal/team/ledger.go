package team

import (
	"fmt"
	"strings"
	"time"
)

func (b *Broker) appendActionWithRefsLocked(kind, source, channel, actor, summary, relatedID string, signalIDs []string, decisionID string) {
	record := officeActionLog{
		ID:         fmt.Sprintf("action-%d", len(b.actions)+1),
		Kind:       strings.TrimSpace(kind),
		Source:     strings.TrimSpace(source),
		Channel:    normalizeChannelSlug(channel),
		Actor:      strings.TrimSpace(actor),
		Summary:    redactSecretsInText(strings.TrimSpace(summary)),
		RelatedID:  strings.TrimSpace(relatedID),
		SignalIDs:  append([]string(nil), signalIDs...),
		DecisionID: strings.TrimSpace(decisionID),
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	b.actions = append(b.actions, record)
	if len(b.actions) > 150 {
		b.actions = append([]officeActionLog(nil), b.actions[len(b.actions)-150:]...)
	}
	b.publishActionLocked(record)
}

func officeSignalDedupeKey(signal officeSignal) string {
	channel := normalizeChannelSlug(signal.Channel)
	if channel == "" {
		channel = "general"
	}
	if strings.TrimSpace(signal.ID) != "" {
		return strings.Join([]string{
			strings.TrimSpace(signal.Source),
			strings.TrimSpace(signal.ID),
		}, "::")
	}
	content := redactSecretsInText(strings.TrimSpace(signal.Content))
	return strings.Join([]string{
		strings.TrimSpace(signal.Source),
		channel,
		strings.TrimSpace(signal.Kind),
		truncateSummary(strings.ToLower(content), 140),
	}, "::")
}

func compactStringList(items []string) []string {
	var out []string
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (b *Broker) findSignalLocked(source, sourceRef, dedupeKey string) *officeSignalRecord {
	for i := range b.signals {
		sig := &b.signals[i]
		switch {
		case source != "" && sourceRef != "" && sig.Source == source && sig.SourceRef == sourceRef:
			return sig
		case dedupeKey != "" && sig.DedupeKey == dedupeKey:
			return sig
		}
	}
	return nil
}

func (b *Broker) RecordSignals(signals []officeSignal) ([]officeSignalRecord, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]officeSignalRecord, 0, len(signals))
	for _, signal := range signals {
		channel := normalizeChannelSlug(signal.Channel)
		if channel == "" {
			channel = "general"
		}
		dedupeKey := officeSignalDedupeKey(signal)
		if existing := b.findSignalLocked(strings.TrimSpace(signal.Source), strings.TrimSpace(signal.ID), dedupeKey); existing != nil {
			continue
		}
		record := officeSignalRecord{
			ID:            fmt.Sprintf("signal-%d", len(b.signals)+1),
			Source:        strings.TrimSpace(signal.Source),
			SourceRef:     strings.TrimSpace(signal.ID),
			Kind:          strings.TrimSpace(signal.Kind),
			Title:         redactSecretsInText(strings.TrimSpace(signal.Title)),
			Content:       redactSecretsInText(strings.TrimSpace(signal.Content)),
			Channel:       channel,
			Owner:         strings.TrimSpace(signal.Owner),
			Confidence:    strings.TrimSpace(signal.Confidence),
			Urgency:       strings.TrimSpace(signal.Urgency),
			DedupeKey:     dedupeKey,
			RequiresHuman: signal.RequiresHuman,
			Blocking:      signal.Blocking,
			CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		}
		b.signals = append(b.signals, record)
		out = append(out, record)
	}
	if len(b.signals) > 200 {
		b.signals = append([]officeSignalRecord(nil), b.signals[len(b.signals)-200:]...)
	}
	if err := b.saveLocked(); err != nil {
		return nil, err
	}
	return out, nil
}

func (b *Broker) RecordDecision(kind, channel, summary, reason, owner string, signalIDs []string, requiresHuman, blocking bool) (officeDecisionRecord, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	record := officeDecisionRecord{
		ID:            fmt.Sprintf("decision-%d", len(b.decisions)+1),
		Kind:          strings.TrimSpace(kind),
		Channel:       channel,
		Summary:       redactSecretsInText(strings.TrimSpace(summary)),
		Reason:        redactSecretsInText(strings.TrimSpace(reason)),
		Owner:         strings.TrimSpace(owner),
		SignalIDs:     append([]string(nil), signalIDs...),
		RequiresHuman: requiresHuman,
		Blocking:      blocking,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	b.decisions = append(b.decisions, record)
	if len(b.decisions) > 120 {
		b.decisions = append([]officeDecisionRecord(nil), b.decisions[len(b.decisions)-120:]...)
	}
	if err := b.saveLocked(); err != nil {
		return officeDecisionRecord{}, err
	}
	return record, nil
}

func (b *Broker) RecordAction(kind, source, channel, actor, summary, relatedID string, signalIDs []string, decisionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.appendActionWithRefsLocked(kind, source, channel, actor, summary, relatedID, signalIDs, decisionID)
	return b.saveLocked()
}

func (b *Broker) CreateWatchdogAlert(kind, channel, targetType, targetID, owner, summary string) (watchdogAlert, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range b.watchdogs {
		alert := &b.watchdogs[i]
		if alert.Kind == strings.TrimSpace(kind) && alert.Channel == channel && alert.TargetType == strings.TrimSpace(targetType) && alert.TargetID == strings.TrimSpace(targetID) && strings.TrimSpace(alert.Status) != "resolved" {
			alert.Owner = strings.TrimSpace(owner)
			alert.Summary = redactSecretsInText(strings.TrimSpace(summary))
			alert.UpdatedAt = now
			if err := b.saveLocked(); err != nil {
				return watchdogAlert{}, false, err
			}
			return *alert, true, nil
		}
	}

	record := watchdogAlert{
		ID:         fmt.Sprintf("watchdog-%d", len(b.watchdogs)+1),
		Kind:       strings.TrimSpace(kind),
		Channel:    channel,
		TargetType: strings.TrimSpace(targetType),
		TargetID:   strings.TrimSpace(targetID),
		Owner:      strings.TrimSpace(owner),
		Status:     "active",
		Summary:    redactSecretsInText(strings.TrimSpace(summary)),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	b.watchdogs = append(b.watchdogs, record)
	if len(b.watchdogs) > 120 {
		b.watchdogs = append([]watchdogAlert(nil), b.watchdogs[len(b.watchdogs)-120:]...)
	}
	// Mirror the alert into the agent's activity snapshot so the office rail
	// shows the stuck escalation immediately (no need to wait for the 90s
	// stale-while-active reaper). Owner is the agent slug for task/request
	// alerts. Best-effort: an alert without a known agent owner (e.g. stuck
	// on a system target) just doesn't surface a pill change.
	b.markAgentStuckFromWatchdogLocked(record)
	if err := b.saveLocked(); err != nil {
		return watchdogAlert{}, false, err
	}
	return record, false, nil
}

// markAgentStuckFromWatchdogLocked stamps the alert's Owner activity snapshot
// with Kind="stuck" and republishes it. Caller must hold b.mu. No-op when the
// owner is empty or has no live activity entry yet.
func (b *Broker) markAgentStuckFromWatchdogLocked(alert watchdogAlert) {
	slug := normalizeChannelSlug(alert.Owner)
	if slug == "" {
		return
	}
	snap, ok := b.activity[slug]
	if !ok {
		// No prior activity for this agent — synthesize a minimal snapshot
		// so the rail still surfaces the stuck signal. Status stays empty
		// (would otherwise lie about agent state); the frontend keys off
		// Kind=="stuck" for the chrome.
		snap = agentActivitySnapshot{Slug: slug}
	}
	if snap.Kind == "stuck" {
		return
	}
	snap.Kind = "stuck"
	if alert.Summary != "" {
		snap.Detail = alert.Summary
	}
	snap.LastTime = time.Now().UTC().Format(time.RFC3339)
	b.activity[slug] = snap
	b.publishActivityLocked(snap)
}

// markAgentStuckClearedFromWatchdogLocked is the inverse hook for the clear
// path in resolveWatchdogAlertsLocked. It does NOT invent a new "clear" Kind:
// the alert resolution is itself treated as a routine status update so the
// pill drops the bordered chrome and waits for the next real activity event
// to repopulate live state. Caller must hold b.mu.
func (b *Broker) markAgentStuckClearedFromWatchdogLocked(alert watchdogAlert) {
	slug := normalizeChannelSlug(alert.Owner)
	if slug == "" {
		return
	}
	snap, ok := b.activity[slug]
	if !ok {
		return
	}
	if snap.Kind != "stuck" {
		return
	}
	// If another active watchdog still claims this owner, leave the pill
	// stuck. The just-resolved alert is already marked "resolved" in
	// b.watchdogs by resolveWatchdogAlertsLocked, so the scan will skip it
	// and only catch genuinely active siblings. b.watchdogs is bounded so
	// the linear scan cost is negligible.
	for _, w := range b.watchdogs {
		if strings.TrimSpace(w.Status) == "resolved" {
			continue
		}
		if normalizeChannelSlug(w.Owner) == slug {
			return
		}
	}
	snap.Kind = "routine"
	snap.LastTime = time.Now().UTC().Format(time.RFC3339)
	b.activity[slug] = snap
	b.publishActivityLocked(snap)
}

func (b *Broker) resolveWatchdogAlertsLocked(targetType, targetID, channel string) {
	channel = normalizeChannelSlug(channel)
	for i := range b.watchdogs {
		alert := &b.watchdogs[i]
		if targetType != "" && alert.TargetType != strings.TrimSpace(targetType) {
			continue
		}
		if targetID != "" && alert.TargetID != strings.TrimSpace(targetID) {
			continue
		}
		if channel != "" && alert.Channel != "" && alert.Channel != channel {
			continue
		}
		if strings.TrimSpace(alert.Status) == "resolved" {
			continue
		}
		alert.Status = "resolved"
		alert.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		// Drop the stuck flag from the agent's activity snapshot so the
		// rail pill stops escalating. We do not try to repaint live
		// status here — the next real event from the agent owns that.
		b.markAgentStuckClearedFromWatchdogLocked(*alert)
	}
}
