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
		Summary:    strings.TrimSpace(summary),
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
	return strings.Join([]string{
		strings.TrimSpace(signal.Source),
		channel,
		strings.TrimSpace(signal.Kind),
		truncateSummary(strings.ToLower(strings.TrimSpace(signal.Content)), 140),
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
			Title:         strings.TrimSpace(signal.Title),
			Content:       strings.TrimSpace(signal.Content),
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
		Summary:       strings.TrimSpace(summary),
		Reason:        strings.TrimSpace(reason),
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
			alert.Summary = strings.TrimSpace(summary)
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
		Summary:    strings.TrimSpace(summary),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	b.watchdogs = append(b.watchdogs, record)
	if len(b.watchdogs) > 120 {
		b.watchdogs = append([]watchdogAlert(nil), b.watchdogs[len(b.watchdogs)-120:]...)
	}
	if err := b.saveLocked(); err != nil {
		return watchdogAlert{}, false, err
	}
	return record, false, nil
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
	}
}
