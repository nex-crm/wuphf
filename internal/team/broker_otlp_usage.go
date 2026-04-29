package team

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

func (b *Broker) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	// Deep-clone the Agents map: `usage := b.usage` only copies the
	// teamUsageState header, so the inner map still aliases broker state.
	// Encoding it after Unlock would race a concurrent recordUsageLocked
	// and panic with "concurrent map iteration and map write".
	usage := b.usage
	cloned := make(map[string]usageTotals, len(b.usage.Agents))
	for k, v := range b.usage.Agents {
		cloned[k] = v
	}
	usage.Agents = cloned
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(usage)
}

// otlpLogsMaxBodyBytes caps incoming OTLP-log payloads so an authenticated
// agent that emits a runaway batch can't grow broker memory unbounded.
// 4 MiB comfortably fits a normal claude-code turn's telemetry; anything
// larger is almost certainly a bug or hostile input.
const otlpLogsMaxBodyBytes = 4 << 20

func (b *Broker) handleOTLPLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Bound the body BEFORE decoding so a 1 GiB payload can't tie up
	// the decoder's read path. Distinguish the cap-overflow case (413,
	// canonical "request too large") from a generic decode failure
	// (400, "invalid json") so clients can tell them apart — matches
	// the broker_pam.go pattern.
	r.Body = http.MaxBytesReader(w, r.Body, otlpLogsMaxBodyBytes)
	defer r.Body.Close()

	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	events := parseOTLPUsageEvents(payload)
	b.mu.Lock()
	for _, event := range events {
		if strings.TrimSpace(event.AgentSlug) == "" {
			continue
		}
		b.recordUsageLocked(event)
	}
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"accepted": len(events)})
}

type usageEvent struct {
	AgentSlug           string
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	CostUsd             float64
	// Timestamp is the OTLP record's nanos-since-epoch when populated;
	// zero value means "use ingest time at attach time". A delayed or
	// retried /v1/logs batch can therefore land on the message that was
	// active when the event happened, not whatever turn is in-flight on
	// the broker at the moment we receive it.
	Timestamp time.Time
}

const messageUsageAttachMaxAge = 15 * time.Minute

func (b *Broker) recordUsageLocked(event usageEvent) {
	if b.usage.Agents == nil {
		b.usage.Agents = make(map[string]usageTotals)
	}
	if b.usage.Since == "" {
		b.usage.Since = time.Now().UTC().Format(time.RFC3339)
	}
	agentTotal := b.usage.Agents[event.AgentSlug]
	applyUsageEvent(&agentTotal, event)
	b.usage.Agents[event.AgentSlug] = agentTotal

	session := b.usage.Session
	applyUsageEvent(&session, event)
	b.usage.Session = session

	total := b.usage.Total
	applyUsageEvent(&total, event)
	b.usage.Total = total
	b.attachUsageToRecentMessagesLocked(event)
}

func applyUsageEvent(dst *usageTotals, event usageEvent) {
	dst.InputTokens += event.InputTokens
	dst.OutputTokens += event.OutputTokens
	dst.CacheReadTokens += event.CacheReadTokens
	dst.CacheCreationTokens += event.CacheCreationTokens
	dst.TotalTokens += event.InputTokens + event.OutputTokens + event.CacheReadTokens + event.CacheCreationTokens
	dst.CostUsd += event.CostUsd
	dst.Requests++
}

func usageEventToMessageUsage(event usageEvent) *messageUsage {
	total := event.InputTokens + event.OutputTokens + event.CacheReadTokens + event.CacheCreationTokens
	if total == 0 {
		return nil
	}
	return &messageUsage{
		InputTokens:         event.InputTokens,
		OutputTokens:        event.OutputTokens,
		CacheReadTokens:     event.CacheReadTokens,
		CacheCreationTokens: event.CacheCreationTokens,
		TotalTokens:         total,
	}
}

func cloneMessageUsage(src *messageUsage) *messageUsage {
	if src == nil {
		return nil
	}
	cp := *src
	return &cp
}

func messageIsWithinUsageAttachWindow(timestamp string, now time.Time) bool {
	ts := strings.TrimSpace(timestamp)
	if ts == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return true
		}
	}
	return now.Sub(parsed) <= messageUsageAttachMaxAge
}

func (b *Broker) attachUsageToRecentMessagesLocked(event usageEvent) {
	usage := usageEventToMessageUsage(event)
	if usage == nil {
		return
	}
	slug := strings.TrimSpace(event.AgentSlug)
	if slug == "" {
		return
	}
	// Reference time = the event's wall clock when supplied, else ingest
	// time. This makes the attach-window predicate compare a delayed/
	// retried batch against the message that was current when the event
	// happened, not whatever just got posted on the broker.
	now := event.Timestamp
	if now.IsZero() {
		now = time.Now().UTC()
	}
	for i := len(b.messages) - 1; i >= 0; i-- {
		msg := &b.messages[i]
		if strings.TrimSpace(msg.From) != slug {
			continue
		}
		if msg.Usage != nil {
			break
		}
		if !messageIsWithinUsageAttachWindow(msg.Timestamp, now) {
			break
		}
		msg.Usage = cloneMessageUsage(usage)
	}
}

// RecordAgentUsage records token usage from a provider stream result for a given agent.
//
// model is currently unused; it's kept on the signature so callers can pass
// the model name without a future per-model attribution change rippling
// through every headless launcher's call site.
func (b *Broker) RecordAgentUsage(slug, model string, usage provider.ClaudeUsage) {
	_ = model
	event := usageEvent{
		AgentSlug:           slug,
		InputTokens:         usage.InputTokens,
		OutputTokens:        usage.OutputTokens,
		CacheReadTokens:     usage.CacheReadTokens,
		CacheCreationTokens: usage.CacheCreationTokens,
		CostUsd:             usage.CostUSD,
	}
	b.mu.Lock()
	b.recordUsageLocked(event)
	// RecordAgentUsage is called from headless launchers that have no
	// HTTP path to surface a 500. Log the persistence failure so it
	// shows up in operator logs; in-memory state still reflects the
	// usage and the next saveLocked call (next message, next request)
	// will retry the snapshot.
	if err := b.saveLocked(); err != nil {
		log.Printf("broker: saveLocked after RecordAgentUsage(%q): %v", slug, err)
	}
	b.mu.Unlock()
}

func parseOTLPUsageEvents(payload map[string]any) []usageEvent {
	resourceLogs, _ := payload["resourceLogs"].([]any)
	var events []usageEvent
	for _, resourceLog := range resourceLogs {
		resourceMap, _ := resourceLog.(map[string]any)
		resourceAttrs := otlpAttributesMap(nestedMap(resourceMap, "resource"))
		scopeLogs, _ := resourceMap["scopeLogs"].([]any)
		for _, scopeLog := range scopeLogs {
			scopeMap, _ := scopeLog.(map[string]any)
			logRecords, _ := scopeMap["logRecords"].([]any)
			for _, logRecord := range logRecords {
				recordMap, _ := logRecord.(map[string]any)
				attrs := otlpAttributesMap(recordMap)
				for k, v := range resourceAttrs {
					if _, exists := attrs[k]; !exists {
						attrs[k] = v
					}
				}
				if attrs["event.name"] != "api_request" && attrs["event_name"] != "api_request" {
					continue
				}
				slug := attrs["agent.slug"]
				if slug == "" {
					slug = attrs["agent_slug"]
				}
				if slug == "" {
					continue
				}
				events = append(events, usageEvent{
					AgentSlug:           slug,
					InputTokens:         otlpIntValue(attrs["input_tokens"]),
					OutputTokens:        otlpIntValue(attrs["output_tokens"]),
					CacheReadTokens:     otlpIntValue(attrs["cache_read_tokens"]),
					CacheCreationTokens: otlpIntValue(attrs["cache_creation_tokens"]),
					CostUsd:             otlpFloatValue(attrs["cost_usd"]),
					Timestamp:           otlpRecordTimestamp(recordMap),
				})
			}
		}
	}
	return events
}

func nestedMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	child, _ := m[key].(map[string]any)
	return child
}

// otlpRecordTimestamp extracts the OTLP log record's wall-clock time
// from the standard `timeUnixNano` / `observedTimeUnixNano` fields.
// Returns the zero time when neither is present or parseable so callers
// fall back to ingest time.
func otlpRecordTimestamp(record map[string]any) time.Time {
	if record == nil {
		return time.Time{}
	}
	for _, key := range []string{"timeUnixNano", "observedTimeUnixNano"} {
		raw, ok := record[key]
		if !ok || raw == nil {
			continue
		}
		nanos := otlpIntValue(fmt.Sprintf("%v", raw))
		if nanos > 0 {
			return time.Unix(0, int64(nanos)).UTC()
		}
	}
	return time.Time{}
}

func otlpAttributesMap(record map[string]any) map[string]string {
	out := make(map[string]string)
	if record == nil {
		return out
	}
	attrs, _ := record["attributes"].([]any)
	for _, attr := range attrs {
		attrMap, _ := attr.(map[string]any)
		key, _ := attrMap["key"].(string)
		if key == "" {
			continue
		}
		out[key] = otlpAnyValue(attrMap["value"])
	}
	return out
}

func otlpAnyValue(raw any) string {
	valMap, _ := raw.(map[string]any)
	for _, key := range []string{"stringValue", "intValue", "doubleValue", "boolValue"} {
		if value, ok := valMap[key]; ok {
			return fmt.Sprintf("%v", value)
		}
	}
	return ""
}

func otlpIntValue(raw string) int {
	if raw == "" {
		return 0
	}
	n, _ := strconv.Atoi(raw)
	return n
}

func otlpFloatValue(raw string) float64 {
	if raw == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(raw, 64)
	return v
}
