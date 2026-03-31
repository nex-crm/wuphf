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
	"time"

	"github.com/nex-crm/wuphf/internal/config"
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

func TestBrokerSessionModePersistsAndSurvivesReset(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.SetSessionMode(SessionModeOneOnOne, "pm"); err != nil {
		t.Fatalf("SetSessionMode failed: %v", err)
	}
	if _, err := b.PostMessage("pm", "general", "hello", nil, ""); err != nil {
		t.Fatalf("seed direct message: %v", err)
	}

	reloaded := NewBroker()
	mode, agent := reloaded.SessionModeState()
	if mode != SessionModeOneOnOne {
		t.Fatalf("expected persisted 1o1 mode, got %q", mode)
	}
	if agent != "pm" {
		t.Fatalf("expected persisted 1o1 agent pm, got %q", agent)
	}

	reloaded.Reset()
	mode, agent = reloaded.SessionModeState()
	if mode != SessionModeOneOnOne {
		t.Fatalf("expected reset to preserve 1o1 mode, got %q", mode)
	}
	if agent != "pm" {
		t.Fatalf("expected reset to preserve 1o1 agent pm, got %q", agent)
	}
	if len(reloaded.Messages()) != 0 {
		t.Fatalf("expected reset to clear direct messages, got %d", len(reloaded.Messages()))
	}
}

func TestBrokerMessageKindAndTitleRoundTrip(t *testing.T) {
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
	body, _ := json.Marshal(map[string]any{
		"from":    "fe",
		"channel": "general",
		"kind":    "human_report",
		"title":   "Frontend ready for review",
		"content": "The launch page skeleton is ready for you to review.",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/messages", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post message failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 posting message, got %d: %s", resp.StatusCode, raw)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/messages?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get messages failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 listing messages, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Messages []channelMessage `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if got := result.Messages[0].Kind; got != "human_report" {
		t.Fatalf("expected human_report kind, got %q", got)
	}
	if got := result.Messages[0].Title; got != "Frontend ready for review" {
		t.Fatalf("expected title to round-trip, got %q", got)
	}
}

func TestNewBrokerSeedsDefaultOfficeRosterOnFreshState(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	members := b.OfficeMembers()
	if len(members) < 2 {
		t.Fatalf("expected default office roster on fresh state, got %d members", len(members))
	}
	b.mu.Lock()
	ceo := b.findMemberLocked("ceo")
	general := b.findChannelLocked("general")
	b.mu.Unlock()
	if members[0].Slug != "ceo" && ceo == nil {
		t.Fatalf("expected ceo to be present in default office roster")
	}
	if general == nil {
		t.Fatal("expected general channel to exist")
	}
	if len(general.Members) < len(members) {
		t.Fatalf("expected general channel to include office roster, got %v for %d members", general.Members, len(members))
	}
}

func TestOfficeMemberLifecycle(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.members = append(b.members, officeMember{
		Slug:      "growthops",
		Name:      "Growth Ops",
		Role:      "Growth Ops",
		CreatedBy: "you",
	})
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked failed: %v", err)
	}
	b.mu.Unlock()

	reloaded := NewBroker()
	if reloaded.findMemberLocked("growthops") == nil {
		t.Fatal("expected custom office member to persist")
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

func TestChannelMembersRejectUnknownOfficeMember(t *testing.T) {
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
	body, _ := json.Marshal(map[string]any{
		"action":  "add",
		"channel": "general",
		"slug":    "ghost",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/channel-members", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown member, got %d", resp.StatusCode)
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
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("expected 200 on /health, got %d", resp.StatusCode)
	}
	var health struct {
		SessionMode   string `json:"session_mode"`
		OneOnOneAgent string `json:"one_on_one_agent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		resp.Body.Close()
		t.Fatalf("decode health: %v", err)
	}
	resp.Body.Close()
	if health.SessionMode != SessionModeOffice {
		t.Fatalf("expected health to report office mode, got %q", health.SessionMode)
	}
	if health.OneOnOneAgent != DefaultOneOnOneAgent {
		t.Fatalf("expected health to report default 1o1 agent %q, got %q", DefaultOneOnOneAgent, health.OneOnOneAgent)
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

func TestChannelDescriptionsAreVisibleButContentStaysRestricted(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()
	b := NewBroker()
	b.mu.Lock()
	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "CEO", BuiltIn: true},
		{Slug: "pm", Name: "Product Manager", Role: "Product Manager"},
		{Slug: "fe", Name: "Frontend Engineer", Role: "Frontend Engineer"},
		{Slug: "cmo", Name: "CMO", Role: "CMO"},
	}
	b.channels = []teamChannel{{
		Slug:        "general",
		Name:        "general",
		Description: "Company-wide room",
		Members:     []string{"ceo", "pm", "fe", "cmo"},
	}}
	b.mu.Unlock()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())

	createBody, _ := json.Marshal(map[string]any{
		"action":      "create",
		"slug":        "launch",
		"name":        "launch",
		"description": "Launch planning and launch-readiness work.",
		"members":     []string{"pm", "fe"},
		"created_by":  "ceo",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/channels", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create channel failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 creating channel, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/channels", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get channels failed: %v", err)
	}
	defer resp.Body.Close()
	var channelList struct {
		Channels []teamChannel `json:"channels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&channelList); err != nil {
		t.Fatalf("decode channels: %v", err)
	}
	var launch *teamChannel
	for i := range channelList.Channels {
		if channelList.Channels[i].Slug == "launch" {
			launch = &channelList.Channels[i]
			break
		}
	}
	if launch == nil {
		t.Fatal("expected launch channel in channel list")
	}
	if launch.Description != "Launch planning and launch-readiness work." {
		t.Fatalf("unexpected launch description: %q", launch.Description)
	}
	if !containsString(launch.Members, "ceo") || !containsString(launch.Members, "pm") || !containsString(launch.Members, "fe") {
		t.Fatalf("expected create payload members plus CEO in new channel, got %+v", launch.Members)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/messages?channel=launch&my_slug=cmo", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get messages as non-member failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member channel messages, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/messages?channel=launch&my_slug=ceo", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get messages as ceo failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for CEO channel messages, got %d", resp.StatusCode)
	}
}

func TestNormalizeLoadedStateRepopulatesGeneralFromOfficeRoster(t *testing.T) {
	b := NewBroker()
	b.mu.Lock()
	defer b.mu.Unlock()

	b.members = []officeMember{
		{Slug: "ceo", Name: "CEO", Role: "CEO", BuiltIn: true},
		{Slug: "pm", Name: "Product Manager", Role: "Product Manager"},
		{Slug: "fe", Name: "Frontend Engineer", Role: "Frontend Engineer"},
	}
	b.channels = []teamChannel{{
		Slug:        "general",
		Name:        "general",
		Description: "Company-wide room",
		Members:     []string{"ceo"},
	}}

	b.normalizeLoadedStateLocked()

	ch := b.findChannelLocked("general")
	if ch == nil {
		t.Fatal("expected general channel after normalization")
	}
	if !containsString(ch.Members, "ceo") || !containsString(ch.Members, "pm") || !containsString(ch.Members, "fe") {
		t.Fatalf("expected general channel to be repopulated from office roster, got %+v", ch.Members)
	}
}

func TestTaskAndRequestViewsRejectNonMembers(t *testing.T) {
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
	createBody, _ := json.Marshal(map[string]any{
		"action":      "create",
		"slug":        "deals",
		"name":        "deals",
		"description": "Deal strategy and pipeline work.",
		"created_by":  "ceo",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/channels", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create channel failed: %v", err)
	}
	resp.Body.Close()

	req, _ = http.NewRequest(http.MethodGet, base+"/tasks?channel=deals&viewer_slug=fe", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get tasks as non-member failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member task access, got %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/requests?channel=deals&viewer_slug=fe", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get requests as non-member failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member request access, got %d", resp.StatusCode)
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
	teleResp, teleErr := http.DefaultClient.Do(req)
	if teleErr != nil {
		t.Fatalf("telemetry post failed: %v", teleErr)
	}
	teleResp.Body.Close()
	if teleResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from usage ingest, got %d", teleResp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/usage", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	usageResp, usageErr := http.DefaultClient.Do(req)
	if usageErr != nil {
		t.Fatalf("usage request failed: %v", usageErr)
	}
	defer usageResp.Body.Close()
	var usage teamUsageState
	if err := json.NewDecoder(usageResp.Body).Decode(&usage); err != nil {
		t.Fatalf("decode usage response: %v", err)
	}
	if usage.Total.TotalTokens != 1000 {
		t.Fatalf("expected 1000 total tokens, got %d", usage.Total.TotalTokens)
	}
	if usage.Session.TotalTokens != 1000 {
		t.Fatalf("expected 1000 session tokens, got %d", usage.Session.TotalTokens)
	}
	if usage.Agents["be"].CostUsd != 0.18 {
		t.Fatalf("expected backend cost 0.18, got %+v", usage.Agents["be"])
	}
}

func TestBrokerActionsAndSchedulerEndpoints(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.appendActionLocked("request_created", "office", "general", "ceo", "Asked for approval", "request-1")
	b.mu.Unlock()
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:            "nex-insights",
		Label:           "Nex insights",
		IntervalMinutes: 15,
		Status:          "sleeping",
		NextRun:         "2026-03-24T10:15:00Z",
	}); err != nil {
		t.Fatalf("SetSchedulerJob failed: %v", err)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	for _, path := range []string{"/actions", "/scheduler"} {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		req.Header.Set("Authorization", "Bearer "+b.Token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s request failed: %v", path, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200 on %s, got %d: %s", path, resp.StatusCode, body)
		}
	}
}

func TestSchedulerDueOnlyFiltersFutureJobs(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:            "task-follow-up:general:task-1",
		Kind:            "task_follow_up",
		Label:           "Follow up",
		TargetType:      "task",
		TargetID:        "task-1",
		Channel:         "general",
		IntervalMinutes: 15,
		DueAt:           time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
		NextRun:         time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339),
		Status:          "scheduled",
	}); err != nil {
		t.Fatalf("SetSchedulerJob failed: %v", err)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodGet, base+"/scheduler?due_only=true", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scheduler request failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Jobs []schedulerJob `json:"jobs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode scheduler list: %v", err)
	}
	if len(listing.Jobs) != 0 {
		t.Fatalf("expected future job to be filtered out, got %+v", listing.Jobs)
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

func TestBrokerTaskLifecycle(t *testing.T) {
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
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	created := post(map[string]any{
		"action":     "create",
		"title":      "Own the landing page",
		"details":    "Frontend only",
		"created_by": "ceo",
		"owner":      "fe",
		"thread_id":  "msg-1",
	})
	if created.Status != "in_progress" || created.Owner != "fe" {
		t.Fatalf("unexpected created task: %+v", created)
	}
	if created.FollowUpAt == "" || created.ReminderAt == "" || created.RecheckAt == "" {
		t.Fatalf("expected follow-up timestamps on task create, got %+v", created)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/queue", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("queue request failed: %v", err)
	}
	defer resp.Body.Close()
	var queue struct {
		Actions   []officeActionLog `json:"actions"`
		Scheduler []schedulerJob    `json:"scheduler"`
		Due       []schedulerJob    `json:"due"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
		t.Fatalf("decode queue response: %v", err)
	}
	if len(queue.Scheduler) == 0 {
		t.Fatalf("expected queue to expose scheduler state, got %+v", queue)
	}

	completed := post(map[string]any{
		"action": "complete",
		"id":     created.ID,
	})
	if completed.Status != "done" {
		t.Fatalf("expected done task, got %+v", completed)
	}
	if completed.FollowUpAt != "" || completed.ReminderAt != "" || completed.RecheckAt != "" {
		t.Fatalf("expected completion to clear follow-up timestamps, got %+v", completed)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("tasks get failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Tasks []teamTask `json:"tasks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode tasks list: %v", err)
	}
	if len(listing.Tasks) != 0 {
		t.Fatalf("expected done task to be hidden by default, got %+v", listing.Tasks)
	}
}

func TestBrokerTaskCreateReusesExistingOpenTask(t *testing.T) {
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
	post := func(payload map[string]any) teamTask {
		body, _ := json.Marshal(payload)
		req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+b.Token())
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("task post failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status %d: %s", resp.StatusCode, raw)
		}
		var result struct {
			Task teamTask `json:"task"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("decode task response: %v", err)
		}
		return result.Task
	}

	first := post(map[string]any{
		"action":     "create",
		"title":      "Own the landing page",
		"details":    "Initial FE pass",
		"created_by": "ceo",
		"owner":      "fe",
		"thread_id":  "msg-1",
	})
	second := post(map[string]any{
		"action":     "create",
		"title":      "Own the landing page",
		"details":    "Updated details",
		"created_by": "ceo",
		"owner":      "fe",
		"thread_id":  "msg-1",
	})

	if first.ID != second.ID {
		t.Fatalf("expected task reuse, got %s and %s", first.ID, second.ID)
	}
	if second.Details != "Updated details" {
		t.Fatalf("expected task details to update, got %+v", second)
	}
	if got := len(b.ChannelTasks("general")); got != 1 {
		t.Fatalf("expected one open task after reuse, got %d", got)
	}
}

func TestBrokerStoresLedgerAndReviewLifecycle(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	signals, err := b.RecordSignals([]officeSignal{{
		ID:         "nex-1",
		Source:     "nex_insights",
		Kind:       "risk",
		Title:      "Nex insight",
		Content:    "Signup conversion is slipping.",
		Channel:    "general",
		Owner:      "fe",
		Confidence: "high",
		Urgency:    "high",
	}})
	if err != nil || len(signals) != 1 {
		t.Fatalf("record signals: %v %v", err, signals)
	}
	decision, err := b.RecordDecision("create_task", "general", "Open a frontend follow-up.", "High-signal conversion risk.", "fe", []string{signals[0].ID}, false, false)
	if err != nil {
		t.Fatalf("record decision: %v", err)
	}
	task, reused, err := b.EnsurePlannedTask(plannedTaskInput{
		Channel:          "general",
		Title:            "Build signup conversion fix",
		Details:          "Own the CTA and onboarding flow.",
		Owner:            "fe",
		CreatedBy:        "ceo",
		ThreadID:         "msg-1",
		TaskType:         "feature",
		SourceSignalID:   signals[0].ID,
		SourceDecisionID: decision.ID,
	})
	if err != nil || reused {
		t.Fatalf("ensure planned task: %v reused=%v", err, reused)
	}
	if task.PipelineStage != "implement" || task.ExecutionMode != "local_worktree" || task.SourceDecisionID != decision.ID {
		t.Fatalf("expected structured task metadata, got %+v", task)
	}

	base := fmt.Sprintf("http://%s", b.Addr())
	body, _ := json.Marshal(map[string]any{
		"action":     "complete",
		"channel":    "general",
		"id":         task.ID,
		"created_by": "you",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/tasks", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("complete task: %v", err)
	}
	defer resp.Body.Close()
	var result struct {
		Task teamTask `json:"task"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode completed task: %v", err)
	}
	if result.Task.Status != "review" || result.Task.ReviewState != "ready_for_review" {
		t.Fatalf("expected review-ready task, got %+v", result.Task)
	}

	if _, _, err := b.CreateWatchdogAlert("task_stalled", "general", "task", task.ID, "fe", "Task is waiting for movement."); err != nil {
		t.Fatalf("create watchdog: %v", err)
	}
	if len(b.Decisions()) != 1 || len(b.Signals()) != 1 || len(b.Watchdogs()) != 1 {
		t.Fatalf("expected ledger state, got signals=%d decisions=%d watchdogs=%d", len(b.Signals()), len(b.Decisions()), len(b.Watchdogs()))
	}
}

func TestBrokerBridgeEndpointRecordsVisibleBridge(t *testing.T) {
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
	createChannelBody, _ := json.Marshal(map[string]any{
		"action":      "create",
		"slug":        "launch",
		"name":        "Launch",
		"description": "Launch planning and messaging.",
		"members":     []string{"pm", "cmo"},
		"created_by":  "ceo",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/channels", bytes.NewReader(createChannelBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	resp.Body.Close()

	bridgeBody, _ := json.Marshal(map[string]any{
		"actor":          "ceo",
		"source_channel": "general",
		"target_channel": "launch",
		"summary":        "Use the stronger product narrative from #general in this launch channel before drafting the landing page.",
		"tagged":         []string{"cmo"},
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/bridges", bytes.NewReader(bridgeBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("bridge request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected bridge success, got %d: %s", resp.StatusCode, string(body))
	}

	messages := b.ChannelMessages("launch")
	if len(messages) != 1 {
		t.Fatalf("expected one bridge message in launch, got %d", len(messages))
	}
	if messages[0].Source != "ceo_bridge" || !strings.Contains(messages[0].Content, "#general") {
		t.Fatalf("unexpected bridge message: %+v", messages[0])
	}
	if got := len(b.Signals()); got != 1 {
		t.Fatalf("expected 1 bridge signal, got %d", got)
	}
	if got := len(b.Decisions()); got != 1 || b.Decisions()[0].Kind != "bridge_channel" {
		t.Fatalf("unexpected bridge decisions: %+v", b.Decisions())
	}
	if got := len(b.Actions()); got == 0 || b.Actions()[len(b.Actions())-1].Kind != "bridge_channel" {
		t.Fatalf("expected bridge action, got %+v", b.Actions())
	}
}

func TestBrokerRequestsLifecycle(t *testing.T) {
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
	body, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approval needed",
		"question": "Should we proceed?",
		"blocking": true,
		"required": true,
		"reply_to": "msg-1",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request create failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating request, got %d: %s", resp.StatusCode, raw)
	}

	req, _ = http.NewRequest(http.MethodGet, base+"/requests?channel=general", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request list failed: %v", err)
	}
	defer resp.Body.Close()
	var listing struct {
		Requests []humanInterview `json:"requests"`
		Pending  *humanInterview  `json:"pending"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listing); err != nil {
		t.Fatalf("decode requests: %v", err)
	}
	if len(listing.Requests) != 1 || listing.Pending == nil {
		t.Fatalf("expected one pending request, got %+v", listing)
	}
	if listing.Requests[0].ReminderAt == "" || listing.Requests[0].FollowUpAt == "" || listing.Requests[0].RecheckAt == "" {
		t.Fatalf("expected reminder timestamps on request create, got %+v", listing.Requests[0])
	}

	answerBody, _ := json.Marshal(map[string]any{
		"id":          listing.Requests[0].ID,
		"choice_text": "Yes",
	})
	req, _ = http.NewRequest(http.MethodPost, base+"/requests/answer", bytes.NewReader(answerBody))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request answer failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 answering request, got %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, base+"/queue", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("queue request failed: %v", err)
	}
	defer resp.Body.Close()
	var queue struct {
		Actions   []officeActionLog `json:"actions"`
		Scheduler []schedulerJob    `json:"scheduler"`
		Due       []schedulerJob    `json:"due"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
		t.Fatalf("decode queue response: %v", err)
	}
	for _, job := range queue.Scheduler {
		if job.TargetType == "request" && job.TargetID == listing.Requests[0].ID && !strings.EqualFold(job.Status, "done") {
			t.Fatalf("expected answered request scheduler jobs to complete, got %+v", job)
		}
	}

	if b.HasBlockingRequest() {
		t.Fatal("expected blocking request to clear after answer")
	}
}

func TestBrokerDecisionRequestsDefaultToBlocking(t *testing.T) {
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
	body, _ := json.Marshal(map[string]any{
		"kind":     "approval",
		"from":     "ceo",
		"channel":  "general",
		"title":    "Approval needed",
		"question": "Should we proceed?",
	})
	req, _ := http.NewRequest(http.MethodPost, base+"/requests", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+b.Token())
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request create failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 creating request, got %d: %s", resp.StatusCode, raw)
	}

	var created struct {
		Request humanInterview `json:"request"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	if !created.Request.Blocking || !created.Request.Required {
		t.Fatalf("expected approval to default to blocking+required, got %+v", created.Request)
	}
}

func TestQueueEndpointShowsDueJobs(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	if err := b.SetSchedulerJob(schedulerJob{
		Slug:       "request-follow-up:general:request-1",
		Kind:       "request_follow_up",
		Label:      "Follow up on approval",
		TargetType: "request",
		TargetID:   "request-1",
		Channel:    "general",
		DueAt:      time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339),
		NextRun:    time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339),
		Status:     "scheduled",
	}); err != nil {
		t.Fatalf("SetSchedulerJob failed: %v", err)
	}
	if err := b.StartOnPort(0); err != nil {
		t.Fatalf("failed to start broker: %v", err)
	}
	defer b.Stop()

	base := fmt.Sprintf("http://%s", b.Addr())
	req, _ := http.NewRequest(http.MethodGet, base+"/queue", nil)
	req.Header.Set("Authorization", "Bearer "+b.Token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("queue request failed: %v", err)
	}
	defer resp.Body.Close()
	var queue struct {
		Due []schedulerJob `json:"due"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&queue); err != nil {
		t.Fatalf("decode queue response: %v", err)
	}
	if len(queue.Due) != 1 {
		t.Fatalf("expected due scheduler job to surface, got %+v", queue.Due)
	}
}

func TestResolveTaskIntervalsRespectMinimumFloor(t *testing.T) {
	t.Setenv("WUPHF_TASK_FOLLOWUP_MINUTES", "1")
	t.Setenv("WUPHF_TASK_REMINDER_MINUTES", "1")
	t.Setenv("WUPHF_TASK_RECHECK_MINUTES", "1")

	if got := config.ResolveTaskFollowUpInterval(); got != 2 {
		t.Fatalf("expected follow-up interval floor of 2, got %d", got)
	}
	if got := config.ResolveTaskReminderInterval(); got != 2 {
		t.Fatalf("expected reminder interval floor of 2, got %d", got)
	}
	if got := config.ResolveTaskRecheckInterval(); got != 2 {
		t.Fatalf("expected recheck interval floor of 2, got %d", got)
	}
}

func TestParseSkillProposalFromMessage(t *testing.T) {
	b := &Broker{}
	msg := channelMessage{
		ID:      "msg-1",
		From:    "ceo",
		Channel: "general",
		Content: "I noticed a pattern.\n\n[SKILL PROPOSAL]\nName: deploy-verify\nTitle: Deploy Verification\nDescription: Post-deploy checks\nTrigger: after deploy\nTags: deploy, ops\n---\n1. Check health\n2. Check errors\n[/SKILL PROPOSAL]",
	}
	b.parseSkillProposalLocked(msg)
	if len(b.skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(b.skills))
	}
	s := b.skills[0]
	if s.Name != "deploy-verify" {
		t.Fatalf("expected name 'deploy-verify', got %q", s.Name)
	}
	if s.Title != "Deploy Verification" {
		t.Fatalf("expected title 'Deploy Verification', got %q", s.Title)
	}
	if s.Status != "proposed" {
		t.Fatalf("expected status 'proposed', got %q", s.Status)
	}
	if s.Description != "Post-deploy checks" {
		t.Fatalf("expected description 'Post-deploy checks', got %q", s.Description)
	}
}

func TestLastTaggedAtSetOnPost(t *testing.T) {
	b := &Broker{}
	b.channels = []teamChannel{{Slug: "general", Members: []string{"ceo", "pm"}}}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO"}, {Slug: "pm", Name: "PM"}}

	// Post a message tagging ceo
	msg := channelMessage{
		ID:      "msg-1",
		From:    "you",
		Channel: "general",
		Content: "@ceo what should we do?",
		Tagged:  []string{"ceo"},
	}

	if b.lastTaggedAt == nil {
		b.lastTaggedAt = make(map[string]time.Time)
	}

	// Simulate what handlePostMessage does
	if len(msg.Tagged) > 0 && (msg.From == "you" || msg.From == "human") {
		for _, slug := range msg.Tagged {
			b.lastTaggedAt[slug] = time.Now()
		}
	}

	if _, ok := b.lastTaggedAt["ceo"]; !ok {
		t.Fatal("expected ceo to be in lastTaggedAt")
	}
	if _, ok := b.lastTaggedAt["pm"]; ok {
		t.Fatal("did not expect pm to be in lastTaggedAt")
	}
}

func TestBrokerSurfaceMetadataPersists(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "tg-ops",
		Name:    "tg-ops",
		Members: []string{"ceo"},
		Surface: &channelSurface{
			Provider:    "telegram",
			RemoteID:    "-100999",
			RemoteTitle: "Ops Group",
			Mode:        "supergroup",
			BotTokenEnv: "MY_BOT_TOKEN",
		},
		CreatedBy: "test",
		CreatedAt: "2026-01-01T00:00:00Z",
		UpdatedAt: "2026-01-01T00:00:00Z",
	})
	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		t.Fatalf("saveLocked: %v", err)
	}
	b.mu.Unlock()

	reloaded := NewBroker()
	var found *teamChannel
	for _, ch := range reloaded.channels {
		if ch.Slug == "tg-ops" {
			found = &ch
			break
		}
	}
	if found == nil {
		t.Fatal("expected tg-ops channel after reload")
	}
	if found.Surface == nil {
		t.Fatal("expected surface metadata to persist")
	}
	if found.Surface.Provider != "telegram" {
		t.Fatalf("expected provider=telegram, got %q", found.Surface.Provider)
	}
	if found.Surface.RemoteID != "-100999" {
		t.Fatalf("expected remote_id=-100999, got %q", found.Surface.RemoteID)
	}
	if found.Surface.RemoteTitle != "Ops Group" {
		t.Fatalf("expected remote_title=Ops Group, got %q", found.Surface.RemoteTitle)
	}
	if found.Surface.Mode != "supergroup" {
		t.Fatalf("expected mode=supergroup, got %q", found.Surface.Mode)
	}
	if found.Surface.BotTokenEnv != "MY_BOT_TOKEN" {
		t.Fatalf("expected bot_token_env=MY_BOT_TOKEN, got %q", found.Surface.BotTokenEnv)
	}
}

func TestBrokerSurfaceChannelsFilter(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.channels = append(b.channels,
		teamChannel{
			Slug:    "tg-ch",
			Name:    "tg-ch",
			Members: []string{"ceo"},
			Surface: &channelSurface{Provider: "telegram", RemoteID: "-100"},
		},
		teamChannel{
			Slug:    "slack-ch",
			Name:    "slack-ch",
			Members: []string{"ceo"},
			Surface: &channelSurface{Provider: "slack", RemoteID: "C123"},
		},
		teamChannel{
			Slug:    "native-ch",
			Name:    "native-ch",
			Members: []string{"ceo"},
		},
	)
	b.mu.Unlock()

	tgChannels := b.SurfaceChannels("telegram")
	if len(tgChannels) != 1 {
		t.Fatalf("expected 1 telegram channel, got %d", len(tgChannels))
	}
	if tgChannels[0].Slug != "tg-ch" {
		t.Fatalf("expected tg-ch, got %q", tgChannels[0].Slug)
	}

	slackChannels := b.SurfaceChannels("slack")
	if len(slackChannels) != 1 {
		t.Fatalf("expected 1 slack channel, got %d", len(slackChannels))
	}

	nativeChannels := b.SurfaceChannels("")
	if len(nativeChannels) != 0 {
		t.Fatalf("expected 0 native surface channels, got %d", len(nativeChannels))
	}
}

func TestBrokerExternalQueueDeduplication(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "ext",
		Name:    "ext",
		Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "telegram", RemoteID: "-100"},
	})
	b.mu.Unlock()

	// Post two messages
	b.PostMessage("ceo", "ext", "msg one", nil, "")
	b.PostMessage("ceo", "ext", "msg two", nil, "")

	queue1 := b.ExternalQueue("telegram")
	if len(queue1) != 2 {
		t.Fatalf("expected 2 messages in first drain, got %d", len(queue1))
	}

	// Second drain should be empty
	queue2 := b.ExternalQueue("telegram")
	if len(queue2) != 0 {
		t.Fatalf("expected 0 messages in second drain, got %d", len(queue2))
	}

	// Post one more
	b.PostMessage("ceo", "ext", "msg three", nil, "")
	queue3 := b.ExternalQueue("telegram")
	if len(queue3) != 1 {
		t.Fatalf("expected 1 new message, got %d", len(queue3))
	}
	if queue3[0].Content != "msg three" {
		t.Fatalf("expected 'msg three', got %q", queue3[0].Content)
	}
}

func TestBrokerPostInboundSurfaceMessage(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "surf",
		Name:    "surf",
		Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "telegram", RemoteID: "-100"},
	})
	b.mu.Unlock()

	msg, err := b.PostInboundSurfaceMessage("alice", "surf", "hello surface", "telegram")
	if err != nil {
		t.Fatalf("PostInboundSurfaceMessage: %v", err)
	}
	if msg.Kind != "surface" {
		t.Fatalf("expected kind=surface, got %q", msg.Kind)
	}
	if msg.Source != "telegram" {
		t.Fatalf("expected source=telegram, got %q", msg.Source)
	}

	// Inbound should not appear in the external queue
	queue := b.ExternalQueue("telegram")
	if len(queue) != 0 {
		t.Fatalf("inbound message should not appear in external queue, got %d", len(queue))
	}

	// But it should appear in channel messages
	msgs := b.ChannelMessages("surf")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 channel message, got %d", len(msgs))
	}
}
