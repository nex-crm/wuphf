package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatChannelViewIncludesThreadReference(t *testing.T) {
	got := FormatChannelView([]channelMessage{
		{ID: "msg-1", From: "ceo", Content: "Root topic", Timestamp: "2026-03-24T10:00:00Z"},
		{ID: "msg-2", From: "fe", Content: "Replying here", ReplyTo: "msg-1", Timestamp: "2026-03-24T10:01:00Z"},
	})

	if !strings.Contains(got, "10:01:00 ↳ msg-1  @fe: Replying here") {
		t.Fatalf("expected threaded message to include reply marker, got %q", got)
	}
}

func TestBrokerPersistsAndReloadsState(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.messages = []channelMessage{{ID: "msg-1", From: "ceo", Content: "Persist me", Timestamp: "2026-03-24T10:00:00Z"}}
	b.counter = 1
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked failed: %v", err)
	}
	b.mu.Unlock()

	reloaded := NewBroker()
	msgs := reloaded.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(msgs))
	}
	if msgs[0].Content != "Persist me" {
		t.Fatalf("expected persisted content, got %q", msgs[0].Content)
	}

	reloaded.Reset()
	empty := NewBroker()
	if len(empty.Messages()) != 0 {
		t.Fatalf("expected reset to clear persisted messages, got %d", len(empty.Messages()))
	}
}

func TestBrokerPersistsNotificationCursorWithoutMessages(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.SetNotificationCursor("2026-03-24T10:00:00Z"); err != nil {
		t.Fatalf("SetNotificationCursor failed: %v", err)
	}

	reloaded := NewBroker()
	if got := reloaded.NotificationCursor(); got != "2026-03-24T10:00:00Z" {
		t.Fatalf("expected persisted notification cursor, got %q", got)
	}
}

func TestBrokerAuthRejectsUnauthenticated(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())

	// Health should work without auth
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on /health, got %d", resp.StatusCode)
	}

	// Messages without auth should be rejected
	resp, err = http.Get(base + "/messages")
	if err != nil {
		t.Fatalf("messages request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /messages without auth, got %d", resp.StatusCode)
	}

	// Messages with correct token should succeed
	req, _ := http.NewRequest("GET", base+"/messages", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("authenticated request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 on /messages with auth, got %d: %s", resp.StatusCode, body)
	}

	// Messages with wrong token should be rejected
	req, _ = http.NewRequest("GET", base+"/messages", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bad token request failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 on /messages with wrong token, got %d", resp.StatusCode)
	}
}

func TestParseOTLPUsageEvents(t *testing.T) {
	payload := map[string]any{
		"resourceLogs": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{"key": "agent.slug", "value": map[string]any{"stringValue": "fe"}},
					},
				},
				"scopeLogs": []any{
					map[string]any{
						"logRecords": []any{
							map[string]any{
								"attributes": []any{
									map[string]any{"key": "event.name", "value": map[string]any{"stringValue": "api_request"}},
									map[string]any{"key": "input_tokens", "value": map[string]any{"intValue": "1200"}},
									map[string]any{"key": "output_tokens", "value": map[string]any{"intValue": "300"}},
									map[string]any{"key": "cache_read_tokens", "value": map[string]any{"intValue": "50"}},
									map[string]any{"key": "cache_creation_tokens", "value": map[string]any{"intValue": "25"}},
									map[string]any{"key": "cost_usd", "value": map[string]any{"doubleValue": 0.42}},
								},
							},
						},
					},
				},
			},
		},
	}

	events := parseOTLPUsageEvents(payload)
	if len(events) != 1 {
		t.Fatalf("expected 1 usage event, got %d", len(events))
	}
	if events[0].AgentSlug != "fe" {
		t.Fatalf("expected fe slug, got %q", events[0].AgentSlug)
	}
	if events[0].InputTokens != 1200 || events[0].OutputTokens != 300 {
		t.Fatalf("unexpected token counts: %+v", events[0])
	}
	if events[0].CostUsd != 0.42 {
		t.Fatalf("unexpected cost: %+v", events[0])
	}
}

func TestBrokerUsageEndpointAggregatesTelemetry(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	payload := map[string]any{
		"resourceLogs": []any{
			map[string]any{
				"resource": map[string]any{
					"attributes": []any{
						map[string]any{"key": "agent.slug", "value": map[string]any{"stringValue": "be"}},
					},
				},
				"scopeLogs": []any{
					map[string]any{
						"logRecords": []any{
							map[string]any{
								"attributes": []any{
									map[string]any{"key": "event.name", "value": map[string]any{"stringValue": "api_request"}},
									map[string]any{"key": "input_tokens", "value": map[string]any{"intValue": "800"}},
									map[string]any{"key": "output_tokens", "value": map[string]any{"intValue": "200"}},
									map[string]any{"key": "cost_usd", "value": map[string]any{"doubleValue": 0.18}},
								},
							},
						},
					},
				},
			},
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, base+"/v1/logs", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("telemetry post failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from usage ingest, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/usage", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("usage request failed: %v", err)
	}
	defer resp.Body.Close()
	var usage teamUsageState
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		t.Fatalf("decode usage response: %v", err)
	}
	if usage.Total.TotalTokens != 1000 {
		t.Fatalf("expected 1000 total tokens, got %d", usage.Total.TotalTokens)
	}
	if usage.Agents["be"].CostUsd != 0.18 {
		t.Fatalf("expected backend cost 0.18, got %+v", usage.Agents["be"])
	}
}

func TestBrokerPostsAndDedupesNexNotifications(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	body := map[string]any{
		"event_id":     "feed-item-1",
		"title":        "Context alert",
		"content":      "Important: Acme mentioned budget pressure",
		"tagged":       []string{"ceo"},
		"source":       "context_graph",
		"source_label": "Nex",
	}
	payload, _ := json.Marshal(body)

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodPost, base+"/notifications/nex", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("notification post failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from nex notification ingest, got %d", resp.StatusCode)
		}
	}

	msgs := b.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected deduped single notification, got %d", len(msgs))
	}
	if msgs[0].Kind != "automation" || msgs[0].From != "nex" {
		t.Fatalf("expected automation message from nex, got %+v", msgs[0])
	}
	if msgs[0].EventID != "feed-item-1" {
		t.Fatalf("expected event id to persist, got %+v", msgs[0])
	}
}
