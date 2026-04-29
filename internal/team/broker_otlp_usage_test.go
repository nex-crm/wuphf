package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/provider"
)

// TestHandleOTLPLogs_RejectsOversizedBody pins the 4 MiB body cap.
// Without this an authenticated agent emitting a runaway batch could
// grow broker memory unbounded; with it the broker rejects before
// json.Decoder finishes reading. We use a payload that LOOKS like
// JSON so the decoder reads past the start byte and actually trips
// the MaxBytesReader cap.
//
// Accept either 413 or 400 — same tolerance PAM uses (pam_test.go:691).
// The cap is enforced either way; the test pins "cap is enforced".
func TestHandleOTLPLogs_RejectsOversizedBody(t *testing.T) {
	b := newTestBroker(t)
	srv := httptest.NewServer(http.HandlerFunc(b.handleOTLPLogs))
	defer srv.Close()

	// Wrap the bulk inside a JSON string so the decoder doesn't bail at
	// the very first byte before the cap has a chance to fire.
	body := append([]byte(`{"x":"`), bytes.Repeat([]byte("a"), otlpLogsMaxBodyBytes+1)...)
	body = append(body, []byte(`"}`)...)
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 413 or 400, got %d", resp.StatusCode)
	}
}

// TestHandleOTLPLogs_RejectsNonPOST locks the method gate. The endpoint
// mutates broker state (records usage, persists) so any drift to allow
// GET/PUT/DELETE could expose it to log-tailing GETs or CSRF.
func TestHandleOTLPLogs_RejectsNonPOST(t *testing.T) {
	b := newTestBroker(t)
	srv := httptest.NewServer(http.HandlerFunc(b.handleOTLPLogs))
	defer srv.Close()

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req, _ := http.NewRequest(method, srv.URL, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: expected 405, got %d", method, resp.StatusCode)
		}
	}
}

// TestApplyUsageEvent_AccumulatesAcrossCalls pins the accumulation
// semantics: every field adds, request count increments by exactly 1,
// and TotalTokens is the sum of token fields (NOT the count of calls).
func TestApplyUsageEvent_AccumulatesAcrossCalls(t *testing.T) {
	var dst usageTotals
	applyUsageEvent(&dst, usageEvent{
		InputTokens: 100, OutputTokens: 50, CacheReadTokens: 10, CacheCreationTokens: 5, CostUsd: 0.10,
	})
	applyUsageEvent(&dst, usageEvent{
		InputTokens: 200, OutputTokens: 100, CacheReadTokens: 20, CacheCreationTokens: 10, CostUsd: 0.20,
	})

	if dst.Requests != 2 {
		t.Errorf("Requests: want 2, got %d", dst.Requests)
	}
	if dst.InputTokens != 300 {
		t.Errorf("InputTokens: want 300, got %d", dst.InputTokens)
	}
	if dst.OutputTokens != 150 {
		t.Errorf("OutputTokens: want 150, got %d", dst.OutputTokens)
	}
	if dst.CacheReadTokens != 30 {
		t.Errorf("CacheReadTokens: want 30, got %d", dst.CacheReadTokens)
	}
	if dst.CacheCreationTokens != 15 {
		t.Errorf("CacheCreationTokens: want 15, got %d", dst.CacheCreationTokens)
	}
	if dst.TotalTokens != 495 {
		t.Errorf("TotalTokens: want 495 (sum of all four token fields), got %d", dst.TotalTokens)
	}
	// Tighten tolerance to 1e-12 — sums of two cents-scale floats fit
	// in ~5e-17 ULP, so a regression that drops to float32 (~1e-7) or
	// flips to integer rounding will trip immediately.
	if diff := dst.CostUsd - 0.30; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("CostUsd: want ~0.30, got %v", dst.CostUsd)
	}
}

// TestMessageIsWithinUsageAttachWindow_BoundaryCases pins the four
// branches: empty timestamp returns true (pre-attach window), parseable
// RFC3339 < window returns true, parseable RFC3339 >= window returns
// false, and unparseable timestamps return true (defensive — the helper
// elsewhere walks backwards stopping at the first out-of-window match,
// so unparseable means "treat as in-window so the walk continues").
func TestMessageIsWithinUsageAttachWindow_BoundaryCases(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		timestamp string
		want      bool
	}{
		{"empty", "", true},
		{"whitespace", "   ", true},
		{"in-window 1m ago", now.Add(-time.Minute).Format(time.RFC3339), true},
		{"on-boundary exactly 15m", now.Add(-messageUsageAttachMaxAge).Format(time.RFC3339), true},
		{"out-of-window 16m ago", now.Add(-16 * time.Minute).Format(time.RFC3339), false},
		{"future timestamp", now.Add(time.Minute).Format(time.RFC3339), true},
		{"unparseable", "not-a-time", true},
		// RFC3339Nano fallback — second strict-RFC3339 parse fails but
		// the Nano variant succeeds.
		{"rfc3339nano in-window", now.Add(-30 * time.Second).Format(time.RFC3339Nano), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := messageIsWithinUsageAttachWindow(tc.timestamp, now); got != tc.want {
				t.Errorf("messageIsWithinUsageAttachWindow(%q): want %v, got %v", tc.timestamp, tc.want, got)
			}
		})
	}
}

func TestRecordAgentUsageAttachesToCurrentTurnMessagesOnly(t *testing.T) {
	b := newTestBroker(t)
	now := time.Now().UTC()
	b.mu.Lock()
	b.messages = []channelMessage{
		{
			ID:        "msg-1",
			From:      "ceo",
			Content:   "older turn",
			Timestamp: now.Add(-2 * time.Minute).Format(time.RFC3339),
			Usage:     &messageUsage{TotalTokens: 111},
		},
		{
			ID:        "msg-2",
			From:      "pm",
			Content:   "interleaved",
			Timestamp: now.Add(-30 * time.Second).Format(time.RFC3339),
		},
		{
			ID:        "msg-3",
			From:      "ceo",
			Content:   "current turn kickoff",
			Timestamp: now.Add(-10 * time.Second).Format(time.RFC3339),
		},
		{
			ID:        "msg-4",
			From:      "system",
			Content:   "routing",
			Timestamp: now.Add(-5 * time.Second).Format(time.RFC3339),
		},
		{
			ID:        "msg-5",
			From:      "ceo",
			Content:   "current turn answer",
			Timestamp: now.Format(time.RFC3339),
		},
	}
	b.mu.Unlock()

	b.RecordAgentUsage("ceo", "claude-sonnet-4-6", provider.ClaudeUsage{
		InputTokens:         800,
		OutputTokens:        200,
		CacheReadTokens:     50,
		CacheCreationTokens: 25,
	})

	msgs := b.Messages()
	if msgs[0].Usage == nil || msgs[0].Usage.TotalTokens != 111 {
		t.Fatalf("expected older turn usage to remain untouched, got %+v", msgs[0].Usage)
	}
	if msgs[2].Usage == nil || msgs[2].Usage.TotalTokens != 1075 {
		t.Fatalf("expected msg-3 to receive usage, got %+v", msgs[2].Usage)
	}
	if msgs[4].Usage == nil || msgs[4].Usage.TotalTokens != 1075 {
		t.Fatalf("expected msg-5 to receive usage, got %+v", msgs[4].Usage)
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
	b := newTestBroker(t)
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
