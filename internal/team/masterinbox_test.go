package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestBrokerWithMasterInboxChannel(t *testing.T, remoteID string) *Broker {
	t.Helper()
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "mi-inbox",
		Name:    "Master Inbox",
		Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "masterinbox", RemoteID: remoteID},
	})
	b.mu.Unlock()
	return b
}

func TestNewMasterInboxTransport(t *testing.T) {
	b := newTestBrokerWithMasterInboxChannel(t, "ch-123")
	cfg := masterInboxConfig{APIKey: "test-key"}
	transport := NewMasterInboxTransport(b, cfg)

	if transport.DefaultChannel != "mi-inbox" {
		t.Fatalf("expected default channel mi-inbox, got %q", transport.DefaultChannel)
	}
	if slug, ok := transport.ChannelMap["ch-123"]; !ok || slug != "mi-inbox" {
		t.Fatalf("expected channel map ch-123 -> mi-inbox, got %v", transport.ChannelMap)
	}
}

func TestMasterInboxHandleInbound(t *testing.T) {
	b := newTestBrokerWithMasterInboxChannel(t, "ch-123")
	cfg := masterInboxConfig{APIKey: "test-key"}
	transport := NewMasterInboxTransport(b, cfg)

	event := MasterInboxWebhookEvent{
		EventType:  "message_incoming",
		ChannelID:  "ch-123",
		FromName:   "Jane Doe",
		Email:      "jane@acme.com",
		ProspectID: "234_abc",
		Subject:    "Re: Your proposal",
		Text:       "Thanks for the proposal. We need to discuss pricing.",
	}

	err := transport.HandleInbound(event)
	if err != nil {
		t.Fatalf("HandleInbound: %v", err)
	}

	msgs := b.Messages()
	if len(msgs) == 0 {
		t.Fatal("expected at least one message in broker")
	}
	last := msgs[len(msgs)-1]
	if last.Source != "masterinbox" {
		t.Fatalf("expected source=masterinbox, got %q", last.Source)
	}
	if last.Kind != "surface" {
		t.Fatalf("expected kind=surface, got %q", last.Kind)
	}
	if last.From != "Jane Doe" {
		t.Fatalf("expected from=Jane Doe, got %q", last.From)
	}
}

func TestMasterInboxHandleInboundDefaultChannel(t *testing.T) {
	b := newTestBrokerWithMasterInboxChannel(t, "ch-123")
	cfg := masterInboxConfig{APIKey: "test-key"}
	transport := NewMasterInboxTransport(b, cfg)

	event := MasterInboxWebhookEvent{
		EventType: "message_incoming",
		ChannelID: "unknown-channel",
		FromName:      "test@example.com",
		Text:      "Hello",
	}

	err := transport.HandleInbound(event)
	if err != nil {
		t.Fatalf("HandleInbound with unknown channel should use default: %v", err)
	}
}

func TestMasterInboxExternalQueueSkipsInbound(t *testing.T) {
	b := newTestBrokerWithMasterInboxChannel(t, "ch-123")
	cfg := masterInboxConfig{APIKey: "test-key"}
	transport := NewMasterInboxTransport(b, cfg)

	err := transport.HandleInbound(MasterInboxWebhookEvent{
		EventType: "message_incoming",
		ChannelID: "ch-123",
		FromName:      "prospect@example.com",
		Text:      "inbound msg",
	})
	if err != nil {
		t.Fatalf("HandleInbound: %v", err)
	}

	queue := b.ExternalQueue("masterinbox")
	if len(queue) != 0 {
		t.Fatalf("expected empty external queue for inbound messages, got %d", len(queue))
	}
}

func TestMasterInboxExternalQueueIncludesOutbound(t *testing.T) {
	b := newTestBrokerWithMasterInboxChannel(t, "ch-123")

	_, err := b.PostMessage("ceo", "mi-inbox", "outbound draft", nil, "")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	queue := b.ExternalQueue("masterinbox")
	if len(queue) != 1 {
		t.Fatalf("expected 1 outbound message, got %d", len(queue))
	}
	if queue[0].Content != "outbound draft" {
		t.Fatalf("expected outbound content, got %q", queue[0].Content)
	}
}

func TestMasterInboxGetProspectByEmail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/get-prospects-by-email" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing auth header")
		}

		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["email"] != "test@example.com" {
			t.Fatalf("expected email test@example.com, got %q", body["email"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(masterInboxAPIResponse{
			Success: true,
			Data:    json.RawMessage(`[{"id":"p-1","email":"test@example.com","first_name":"Jane","last_name":"Doe","company":"Acme Corp"}]`),
		})
	}))
	defer server.Close()

	b := newTestBrokerWithMasterInboxChannel(t, "ch-123")
	cfg := masterInboxConfig{APIKey: "test-key", APIBase: server.URL}
	transport := NewMasterInboxTransport(b, cfg)

	prospect, err := transport.GetProspectByEmail(context.Background(), "test@example.com")
	if err != nil {
		t.Fatalf("GetProspectByEmail: %v", err)
	}
	if prospect == nil {
		t.Fatal("expected prospect, got nil")
	}
	if prospect.FirstName != "Jane" {
		t.Fatalf("expected Jane, got %q", prospect.FirstName)
	}
	if prospect.Company != "Acme Corp" {
		t.Fatalf("expected Acme Corp, got %q", prospect.Company)
	}
}

func TestMasterInboxDraftReply(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/draft-message" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(masterInboxAPIResponse{Success: true})
	}))
	defer server.Close()

	b := newTestBrokerWithMasterInboxChannel(t, "ch-123")
	cfg := masterInboxConfig{APIKey: "test-key", APIBase: server.URL}
	transport := NewMasterInboxTransport(b, cfg)

	prospectID := "30_abc123"
	message := "Hi Jane, thanks for your interest. Let me address your pricing question."

	err := transport.DraftReply(context.Background(), prospectID, message)
	if err != nil {
		t.Fatalf("DraftReply: %v", err)
	}
	if receivedBody["prospect_id"] != prospectID {
		t.Fatalf("expected prospect_id %q, got %q", prospectID, receivedBody["prospect_id"])
	}
	if receivedBody["message"] != message {
		t.Fatalf("expected message %q, got %q", message, receivedBody["message"])
	}
}

func TestMasterInboxAddProspectLabel(t *testing.T) {
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/add-prospect-label" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(masterInboxAPIResponse{Success: true})
	}))
	defer server.Close()

	b := newTestBrokerWithMasterInboxChannel(t, "ch-123")
	cfg := masterInboxConfig{APIKey: "test-key", APIBase: server.URL}
	transport := NewMasterInboxTransport(b, cfg)

	err := transport.AddProspectLabel(context.Background(), "p-1", "label-hot")
	if err != nil {
		t.Fatalf("AddProspectLabel: %v", err)
	}
	if receivedBody["prospect_id"] != "p-1" {
		t.Fatalf("expected prospect_id p-1, got %v", receivedBody["prospect_id"])
	}
	if receivedBody["label_id"] != "label-hot" {
		t.Fatalf("expected label_id label-hot, got %v", receivedBody["label_id"])
	}
}

func TestMasterInboxConfigBaseURL(t *testing.T) {
	cfg := masterInboxConfig{}
	if cfg.baseURL() != masterInboxDefaultAPIBase {
		t.Fatalf("expected default base URL, got %q", cfg.baseURL())
	}

	cfg.APIBase = "https://custom.api.com/v1/"
	if cfg.baseURL() != "https://custom.api.com/v1" {
		t.Fatalf("expected trimmed custom URL, got %q", cfg.baseURL())
	}
}

func TestFormatInboundMessage(t *testing.T) {
	event := MasterInboxWebhookEvent{
		Subject: "Meeting follow-up",
		Text:    "Let's schedule a demo next week.",
	}
	result := formatInboundMessage(event)
	if result == "" {
		t.Fatal("expected non-empty message")
	}
	if !contains(result, "Meeting follow-up") {
		t.Fatal("expected subject in message")
	}
	if !contains(result, "Let's schedule a demo") {
		t.Fatal("expected body in message")
	}
}

func TestFormatInboundMessageEmpty(t *testing.T) {
	event := MasterInboxWebhookEvent{}
	result := formatInboundMessage(event)
	if result != "" {
		t.Fatalf("expected empty message for empty event, got %q", result)
	}
}

func TestMasterInboxProspectDisplayName(t *testing.T) {
	tests := []struct {
		prospect MasterInboxProspect
		expected string
	}{
		{MasterInboxProspect{FirstName: "Jane", LastName: "Doe"}, "Jane Doe"},
		{MasterInboxProspect{FirstName: "Jane"}, "Jane"},
		{MasterInboxProspect{Email: "jane@example.com"}, "jane@example.com"},
		{MasterInboxProspect{}, ""},
	}
	for _, tt := range tests {
		got := tt.prospect.displayName()
		if got != tt.expected {
			t.Errorf("displayName() = %q, want %q", got, tt.expected)
		}
	}
}

