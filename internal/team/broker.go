package team

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const BrokerPort = 7890
const brokerTokenFile = "/tmp/wuphf-broker-token"

var brokerStatePath = defaultBrokerStatePath

type channelMessage struct {
	ID          string   `json:"id"`
	From        string   `json:"from"`
	Kind        string   `json:"kind,omitempty"`
	Source      string   `json:"source,omitempty"`
	SourceLabel string   `json:"source_label,omitempty"`
	EventID     string   `json:"event_id,omitempty"`
	Title       string   `json:"title,omitempty"`
	Content     string   `json:"content"`
	Tagged      []string `json:"tagged"`
	ReplyTo     string   `json:"reply_to,omitempty"`
	Timestamp   string   `json:"timestamp"`
}

type interviewOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type interviewAnswer struct {
	ChoiceID   string `json:"choice_id,omitempty"`
	ChoiceText string `json:"choice_text,omitempty"`
	CustomText string `json:"custom_text,omitempty"`
	AnsweredAt string `json:"answered_at,omitempty"`
}

type humanInterview struct {
	ID            string            `json:"id"`
	From          string            `json:"from"`
	Question      string            `json:"question"`
	Context       string            `json:"context,omitempty"`
	Options       []interviewOption `json:"options,omitempty"`
	RecommendedID string            `json:"recommended_id,omitempty"`
	CreatedAt     string            `json:"created_at"`
	Answered      *interviewAnswer  `json:"answered,omitempty"`
}

type brokerState struct {
	Messages          []channelMessage `json:"messages"`
	Counter           int              `json:"counter"`
	NotificationSince string           `json:"notification_since,omitempty"`
	PendingInterview  *humanInterview  `json:"pending_interview,omitempty"`
	Usage             teamUsageState   `json:"usage,omitempty"`
}

type usageTotals struct {
	InputTokens         int     `json:"input_tokens"`
	OutputTokens        int     `json:"output_tokens"`
	CacheReadTokens     int     `json:"cache_read_tokens"`
	CacheCreationTokens int     `json:"cache_creation_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	CostUsd             float64 `json:"cost_usd"`
	Requests            int     `json:"requests"`
}

type teamUsageState struct {
	Total  usageTotals            `json:"total"`
	Agents map[string]usageTotals `json:"agents,omitempty"`
}

// Broker is a lightweight HTTP message broker for the team channel.
// All agent MCP instances connect to this shared broker.
type Broker struct {
	messages          []channelMessage
	counter           int
	notificationSince string
	pendingInterview  *humanInterview
	usage             teamUsageState
	mu                sync.Mutex
	server            *http.Server
	token             string // shared secret for authenticating requests
	addr              string // actual listen address (useful when port=0)
}

// generateToken returns a cryptographically random hex token.
func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: this should never happen on modern systems
		return fmt.Sprintf("wuphf-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// NewBroker creates a new channel broker with a random auth token.
func NewBroker() *Broker {
	b := &Broker{
		token: generateToken(),
	}
	_ = b.loadState()
	return b
}

// Token returns the shared secret that agents must include in requests.
func (b *Broker) Token() string {
	return b.token
}

// Addr returns the actual listen address (e.g. "127.0.0.1:7890").
func (b *Broker) Addr() string {
	return b.addr
}

// requireAuth wraps a handler to enforce Bearer token authentication.
// The /health endpoint is excluded so uptime checks work without credentials.
func (b *Broker) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+b.token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// Start launches the broker on localhost:7890.
func (b *Broker) Start() error {
	return b.StartOnPort(BrokerPort)
}

// StartOnPort launches the broker on the given port. Use 0 for an OS-assigned port.
func (b *Broker) StartOnPort(port int) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", b.handleHealth) // no auth — used for liveness checks
	mux.HandleFunc("/messages", b.requireAuth(b.handleMessages))
	mux.HandleFunc("/notifications/nex", b.requireAuth(b.handleNexNotifications))
	mux.HandleFunc("/members", b.requireAuth(b.handleMembers))
	mux.HandleFunc("/interview", b.requireAuth(b.handleInterview))
	mux.HandleFunc("/interview/answer", b.requireAuth(b.handleInterviewAnswer))
	mux.HandleFunc("/reset", b.requireAuth(b.handleReset))
	mux.HandleFunc("/usage", b.requireAuth(b.handleUsage))
	mux.HandleFunc("/v1/logs", b.requireAuth(b.handleOTLPLogs))

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	b.addr = ln.Addr().String()

	b.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Write token to well-known path so tests and tools can authenticate.
	// Use /tmp directly (not os.TempDir which varies by OS).
	_ = os.WriteFile(brokerTokenFile, []byte(b.token), 0600)

	go func() {
		_ = b.server.Serve(ln)
	}()
	return nil
}

// Stop shuts down the broker.
func (b *Broker) Stop() {
	if b.server != nil {
		b.server.Close()
	}
}

// Messages returns all channel messages (for the Go TUI channel view).
func (b *Broker) Messages() []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]channelMessage, len(b.messages))
	copy(out, b.messages)
	return out
}

func (b *Broker) HasPendingInterview() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pendingInterview != nil && b.pendingInterview.Answered == nil
}

func (b *Broker) Reset() {
	b.mu.Lock()
	b.messages = nil
	b.pendingInterview = nil
	b.counter = 0
	b.notificationSince = ""
	b.usage = teamUsageState{Agents: make(map[string]usageTotals)}
	_ = b.saveLocked()
	b.mu.Unlock()
}

func defaultBrokerStatePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".wuphf", "team", "broker-state.json")
	}
	return filepath.Join(home, ".wuphf", "team", "broker-state.json")
}

func (b *Broker) loadState() error {
	path := brokerStatePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var state brokerState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	b.messages = state.Messages
	b.counter = state.Counter
	b.notificationSince = state.NotificationSince
	b.pendingInterview = state.PendingInterview
	b.usage = state.Usage
	if b.usage.Agents == nil {
		b.usage.Agents = make(map[string]usageTotals)
	}
	return nil
}

func (b *Broker) saveLocked() error {
	path := brokerStatePath()
	if len(b.messages) == 0 && b.pendingInterview == nil && b.counter == 0 && b.notificationSince == "" && usageStateIsZero(b.usage) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	state := brokerState{
		Messages:          b.messages,
		Counter:           b.counter,
		NotificationSince: b.notificationSince,
		PendingInterview:  b.pendingInterview,
		Usage:             b.usage,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func usageStateIsZero(state teamUsageState) bool {
	if state.Total.TotalTokens > 0 || state.Total.CostUsd > 0 || state.Total.Requests > 0 {
		return false
	}
	for _, totals := range state.Agents {
		if totals.TotalTokens > 0 || totals.CostUsd > 0 || totals.Requests > 0 {
			return false
		}
	}
	return true
}

func (b *Broker) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func (b *Broker) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.Reset()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (b *Broker) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	b.mu.Lock()
	usage := b.usage
	if usage.Agents == nil {
		usage.Agents = make(map[string]usageTotals)
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(usage)
}

func (b *Broker) NotificationCursor() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.notificationSince
}

func (b *Broker) SetNotificationCursor(cursor string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cursor == "" || cursor == b.notificationSince {
		return nil
	}
	b.notificationSince = cursor
	return b.saveLocked()
}

func (b *Broker) handleMessages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		b.handlePostMessage(w, r)
	case http.MethodGet:
		b.handleGetMessages(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleOTLPLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
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
	json.NewEncoder(w).Encode(map[string]any{"accepted": len(events)})
}

func (b *Broker) handleNexNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		EventID     string   `json:"event_id"`
		Title       string   `json:"title"`
		Content     string   `json:"content"`
		Tagged      []string `json:"tagged"`
		ReplyTo     string   `json:"reply_to"`
		Source      string   `json:"source"`
		SourceLabel string   `json:"source_label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	msg, duplicate, err := b.PostAutomationMessage("nex", body.Title, body.Content, body.EventID, body.Source, body.SourceLabel, body.Tagged, body.ReplyTo)
	if err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":        msg.ID,
		"duplicate": duplicate,
	})
}

type usageEvent struct {
	AgentSlug           string
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	CostUsd             float64
}

func (b *Broker) recordUsageLocked(event usageEvent) {
	if b.usage.Agents == nil {
		b.usage.Agents = make(map[string]usageTotals)
	}
	agentTotal := b.usage.Agents[event.AgentSlug]
	applyUsageEvent(&agentTotal, event)
	b.usage.Agents[event.AgentSlug] = agentTotal

	total := b.usage.Total
	applyUsageEvent(&total, event)
	b.usage.Total = total
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

func (b *Broker) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From    string   `json:"from"`
		Content string   `json:"content"`
		Tagged  []string `json:"tagged"`
		ReplyTo string   `json:"reply_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	if b.pendingInterview != nil && b.pendingInterview.Answered == nil {
		b.mu.Unlock()
		http.Error(w, "interview pending; answer required before chat resumes", http.StatusConflict)
		return
	}

	b.counter++
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      body.From,
		Content:   body.Content,
		Tagged:    body.Tagged,
		ReplyTo:   strings.TrimSpace(body.ReplyTo),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.messages = append(b.messages, msg)
	total := len(b.messages)
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":    msg.ID,
		"total": total,
	})
}

func (b *Broker) PostAutomationMessage(from, title, content, eventID, source, sourceLabel string, tagged []string, replyTo string) (channelMessage, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if strings.TrimSpace(eventID) != "" {
		for _, existing := range b.messages {
			if existing.EventID != "" && existing.EventID == strings.TrimSpace(eventID) {
				return existing, true, nil
			}
		}
	}

	b.counter++
	msg := channelMessage{
		ID:          fmt.Sprintf("msg-%d", b.counter),
		From:        from,
		Kind:        "automation",
		Source:      strings.TrimSpace(source),
		SourceLabel: strings.TrimSpace(sourceLabel),
		EventID:     strings.TrimSpace(eventID),
		Title:       strings.TrimSpace(title),
		Content:     strings.TrimSpace(content),
		Tagged:      tagged,
		ReplyTo:     strings.TrimSpace(replyTo),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	if msg.Source == "" {
		msg.Source = "context_graph"
	}
	if msg.SourceLabel == "" {
		msg.SourceLabel = "Nex"
	}
	if msg.From == "" {
		msg.From = "nex"
	}

	b.messages = append(b.messages, msg)
	if err := b.saveLocked(); err != nil {
		return channelMessage{}, false, err
	}
	return msg, false, nil
}

func (b *Broker) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 20
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if limit > 100 {
		limit = 100
	}

	sinceID := q.Get("since_id")
	mySlug := q.Get("my_slug")

	b.mu.Lock()
	messages := b.messages
	if sinceID != "" {
		for i, m := range messages {
			if m.ID == sinceID {
				messages = messages[i+1:]
				break
			}
		}
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	// Copy to avoid race
	result := make([]channelMessage, len(messages))
	copy(result, messages)
	b.mu.Unlock()

	taggedCount := 0
	if mySlug != "" {
		for _, m := range result {
			for _, t := range m.Tagged {
				if t == mySlug {
					taggedCount++
					break
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"messages":     result,
		"tagged_count": taggedCount,
	})
}

func (b *Broker) handleMembers(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	members := make(map[string]struct{ lastMessage, lastTime string })
	for _, msg := range b.messages {
		if msg.Kind == "automation" || msg.From == "nex" {
			continue
		}
		content := msg.Content
		if len(content) > 80 {
			content = content[:80]
		}
		ts := msg.Timestamp
		if len(ts) > 19 {
			ts = ts[11:19]
		}
		members[msg.From] = struct{ lastMessage, lastTime string }{content, ts}
	}
	b.mu.Unlock()

	type memberEntry struct {
		Slug        string `json:"slug"`
		LastMessage string `json:"lastMessage"`
		LastTime    string `json:"lastTime"`
	}

	var list []memberEntry
	for slug, info := range members {
		list = append(list, memberEntry{
			Slug:        slug,
			LastMessage: info.lastMessage,
			LastTime:    info.lastTime,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"members": list})
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

	b.mu.Lock()
	b.counter++
	id := fmt.Sprintf("interview-%d", b.counter)
	b.pendingInterview = &humanInterview{
		ID:            id,
		From:          body.From,
		Question:      body.Question,
		Context:       body.Context,
		Options:       body.Options,
		RecommendedID: body.RecommendedID,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id": id,
	})
}

func (b *Broker) handleGetInterview(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if b.pendingInterview == nil || b.pendingInterview.Answered != nil {
		json.NewEncoder(w).Encode(map[string]any{"pending": nil})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"pending": b.pendingInterview})
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
	id := r.URL.Query().Get("id")
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if b.pendingInterview == nil || b.pendingInterview.ID != id || b.pendingInterview.Answered == nil {
		json.NewEncoder(w).Encode(map[string]any{"answered": nil})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"answered": b.pendingInterview.Answered})
}

func (b *Broker) handlePostInterviewAnswer(w http.ResponseWriter, r *http.Request) {
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
	if b.pendingInterview == nil || b.pendingInterview.ID != body.ID {
		b.mu.Unlock()
		http.Error(w, "interview not found", http.StatusNotFound)
		return
	}
	answer := &interviewAnswer{
		ChoiceID:   body.ChoiceID,
		ChoiceText: body.ChoiceText,
		CustomText: body.CustomText,
		AnsweredAt: time.Now().UTC().Format(time.RFC3339),
	}
	b.pendingInterview.Answered = answer
	b.counter++
	msg := channelMessage{
		ID:   fmt.Sprintf("msg-%d", b.counter),
		From: "you",
		Tagged: []string{
			b.pendingInterview.From,
		},
		ReplyTo:   "",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	switch {
	case strings.TrimSpace(body.CustomText) != "":
		msg.Content = fmt.Sprintf("Answered @%s's question: %s", b.pendingInterview.From, body.CustomText)
	case strings.TrimSpace(body.ChoiceText) != "":
		msg.Content = fmt.Sprintf("Answered @%s's question: %s", b.pendingInterview.From, body.ChoiceText)
	default:
		msg.Content = fmt.Sprintf("Answered @%s's question.", b.pendingInterview.From)
	}
	b.messages = append(b.messages, msg)
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// FormatChannelView returns a clean, Slack-style rendering of recent messages.
func FormatChannelView(messages []channelMessage) string {
	if len(messages) == 0 {
		return "  No messages yet. The team is getting set up..."
	}

	var sb strings.Builder
	for _, m := range messages {
		ts := m.Timestamp
		if len(ts) > 19 {
			ts = ts[11:19]
		}

		prefix := m.From
		if m.Kind == "automation" || m.From == "nex" {
			source := m.Source
			if source == "" {
				source = "context_graph"
			}
			title := m.Title
			if title != "" {
				title += ": "
			}
			sb.WriteString(fmt.Sprintf("  %s  Nex/%s: %s%s\n", ts, source, title, m.Content))
			continue
		}
		if strings.HasPrefix(m.Content, "[STATUS]") {
			sb.WriteString(fmt.Sprintf("  %s  @%s %s\n", ts, prefix, m.Content))
		} else {
			thread := ""
			if m.ReplyTo != "" {
				thread = fmt.Sprintf(" ↳ %s", m.ReplyTo)
			}
			sb.WriteString(fmt.Sprintf("  %s%s  @%s: %s\n", ts, thread, prefix, m.Content))
		}
	}
	return sb.String()
}
