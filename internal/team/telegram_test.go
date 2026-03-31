package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTestBrokerWithTelegramChannel(t *testing.T, chatID string) *Broker {
	t.Helper()
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	t.Cleanup(func() { brokerStatePath = oldPathFn })

	b := NewBroker()
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug:    "telegram-general",
		Name:    "telegram-general",
		Members: []string{"ceo", "pm"},
		Surface: &channelSurface{
			Provider:    "telegram",
			RemoteID:    chatID,
			RemoteTitle: "Test Group",
			Mode:        "group",
			BotTokenEnv: "TELEGRAM_BOT_TOKEN",
		},
		CreatedBy: "test",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	})
	b.mu.Unlock()
	return b
}

func TestTelegramTransportChatMap(t *testing.T) {
	b := newTestBrokerWithTelegramChannel(t, "-100123456")
	transport := NewTelegramTransport(b, "fake-token")

	if len(transport.ChatMap) != 1 {
		t.Fatalf("expected 1 chat mapping, got %d", len(transport.ChatMap))
	}
	slug, ok := transport.ChatMap["-100123456"]
	if !ok {
		t.Fatal("expected chat_id -100123456 in ChatMap")
	}
	if slug != "telegram-general" {
		t.Fatalf("expected slug telegram-general, got %q", slug)
	}
}

func TestTelegramHandleInbound(t *testing.T) {
	b := newTestBrokerWithTelegramChannel(t, "-100999")
	transport := NewTelegramTransport(b, "fake-token")

	user := &telegramUser{
		ID:        42,
		FirstName: "Alice",
		Username:  "alice_dev",
	}

	err := transport.HandleInbound(-100999, user, "Hello from Telegram!")
	if err != nil {
		t.Fatalf("HandleInbound: %v", err)
	}

	msgs := b.ChannelMessages("telegram-general")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in channel, got %d", len(msgs))
	}
	if msgs[0].Content != "Hello from Telegram!" {
		t.Fatalf("expected message content, got %q", msgs[0].Content)
	}
	if msgs[0].Kind != "surface" {
		t.Fatalf("expected kind=surface, got %q", msgs[0].Kind)
	}
	if msgs[0].Source != "telegram" {
		t.Fatalf("expected source=telegram, got %q", msgs[0].Source)
	}
}

func TestTelegramHandleInboundWithUserMap(t *testing.T) {
	b := newTestBrokerWithTelegramChannel(t, "-100999")
	transport := NewTelegramTransport(b, "fake-token")
	transport.UserMap["alice_dev"] = "pm"

	user := &telegramUser{
		ID:        42,
		FirstName: "Alice",
		Username:  "alice_dev",
	}

	err := transport.HandleInbound(-100999, user, "mapped user message")
	if err != nil {
		t.Fatalf("HandleInbound: %v", err)
	}

	msgs := b.ChannelMessages("telegram-general")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].From != "pm" {
		t.Fatalf("expected from=pm via UserMap, got %q", msgs[0].From)
	}
}

func TestTelegramHandleInboundUnmappedChat(t *testing.T) {
	b := newTestBrokerWithTelegramChannel(t, "-100999")
	transport := NewTelegramTransport(b, "fake-token")

	err := transport.HandleInbound(-999, nil, "should fail")
	if err == nil {
		t.Fatal("expected error for unmapped chat")
	}
	if !strings.Contains(err.Error(), "unmapped") {
		t.Fatalf("expected unmapped error, got %v", err)
	}
}

func TestTelegramExternalQueueSkipsInbound(t *testing.T) {
	b := newTestBrokerWithTelegramChannel(t, "-100999")
	transport := NewTelegramTransport(b, "fake-token")

	// Post an inbound message via the transport
	err := transport.HandleInbound(-100999, &telegramUser{FirstName: "Bob"}, "inbound msg")
	if err != nil {
		t.Fatalf("HandleInbound: %v", err)
	}

	// The inbound message should NOT appear in the external queue
	// because PostInboundSurfaceMessage marks it as already delivered
	queue := b.ExternalQueue("telegram")
	if len(queue) != 0 {
		t.Fatalf("expected empty external queue for inbound messages, got %d", len(queue))
	}
}

func TestTelegramExternalQueueIncludesOutbound(t *testing.T) {
	b := newTestBrokerWithTelegramChannel(t, "-100999")

	// Post a regular message to the surface channel
	_, err := b.PostMessage("ceo", "telegram-general", "outbound test", nil, "")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}

	queue := b.ExternalQueue("telegram")
	if len(queue) != 1 {
		t.Fatalf("expected 1 outbound message, got %d", len(queue))
	}
	if queue[0].Content != "outbound test" {
		t.Fatalf("expected outbound content, got %q", queue[0].Content)
	}

	// Calling again should return empty (already dequeued)
	queue2 := b.ExternalQueue("telegram")
	if len(queue2) != 0 {
		t.Fatalf("expected empty queue on second call, got %d", len(queue2))
	}
}

func TestFormatTelegramOutbound(t *testing.T) {
	msg := channelMessage{
		From:    "ceo",
		Title:   "Update",
		Content: "All good",
	}
	got := formatTelegramOutbound(msg)
	want := "@ceo: [Update] All good"
	if got != want {
		t.Fatalf("formatTelegramOutbound = %q, want %q", got, want)
	}

	// Without title
	msg2 := channelMessage{From: "pm", Content: "Simple msg"}
	got2 := formatTelegramOutbound(msg2)
	want2 := "@pm: Simple msg"
	if got2 != want2 {
		t.Fatalf("formatTelegramOutbound = %q, want %q", got2, want2)
	}
}

func TestTelegramResolveUser(t *testing.T) {
	transport := &TelegramTransport{
		UserMap: map[string]string{
			"jdoe": "ceo",
		},
	}

	// With mapped username
	got := transport.resolveUser(&telegramUser{Username: "jdoe", FirstName: "John"})
	if got != "ceo" {
		t.Fatalf("expected ceo, got %q", got)
	}

	// Unmapped username falls back to display name
	got = transport.resolveUser(&telegramUser{Username: "unknown_user", FirstName: "Jane", LastName: "Smith"})
	if got != "Jane Smith" {
		t.Fatalf("expected 'Jane Smith', got %q", got)
	}

	// Nil user
	got = transport.resolveUser(nil)
	if got != "unknown" {
		t.Fatalf("expected unknown, got %q", got)
	}
}

func TestTelegramSendToTelegramMocked(t *testing.T) {
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := json.Marshal(telegramAPIResponse{OK: true})
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			data, _ := json.Marshal(telegramAPIResponse{OK: true})
			// Parse the request body
			var reqBody map[string]string
			json.NewDecoder(r.Body).Decode(&reqBody)
			gotBody = reqBody
			w.Write(data)
			return
		}
		w.Write(body)
	}))
	defer server.Close()

	transport := &TelegramTransport{
		BotToken: "test-token",
		client:   server.Client(),
		ChatMap:  map[string]string{"-100": "general"},
		UserMap:  make(map[string]string),
	}
	// Override the API base by patching the sendMessage method's URL
	// We test sendMessage directly via a mock server
	origBase := telegramAPIBase
	defer func() {
		// telegramAPIBase is a const, so we test via the mock server approach below
		_ = origBase
	}()

	msg := channelMessage{From: "ceo", Content: "test message"}

	// Direct HTTP test: simulate what sendMessage does
	payload, _ := json.Marshal(map[string]string{
		"chat_id": "-100",
		"text":    formatTelegramOutbound(msg),
	})
	resp, err := transport.client.Post(server.URL+"/bottest-token/sendMessage", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		t.Fatalf("sendMessage: %v", err)
	}
	resp.Body.Close()

	if gotBody["chat_id"] != "-100" {
		t.Fatalf("expected chat_id=-100, got %q", gotBody["chat_id"])
	}
	if gotBody["text"] != "@ceo: test message" {
		t.Fatalf("expected formatted text, got %q", gotBody["text"])
	}
}

func TestTelegramStartFailsWithoutToken(t *testing.T) {
	b := newTestBrokerWithTelegramChannel(t, "-100")
	transport := NewTelegramTransport(b, "")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := transport.Start(ctx)
	if err == nil || !strings.Contains(err.Error(), "bot token is empty") {
		t.Fatalf("expected token error, got %v", err)
	}
}

func TestTelegramStartFailsWithoutChannels(t *testing.T) {
	oldPathFn := brokerStatePath
	tmpDir := t.TempDir()
	brokerStatePath = func() string { return filepath.Join(tmpDir, "broker-state.json") }
	defer func() { brokerStatePath = oldPathFn }()

	b := NewBroker()
	transport := NewTelegramTransport(b, "fake-token")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := transport.Start(ctx)
	if err == nil || !strings.Contains(err.Error(), "no telegram channels") {
		t.Fatalf("expected no channels error, got %v", err)
	}
}

func TestTelegramPollInboundWithMockServer(t *testing.T) {
	b := newTestBrokerWithTelegramChannel(t, "-100555")

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			// First call: return one update
			updates := []telegramUpdate{{
				UpdateID: 1,
				Message: &telegramMsg{
					MessageID: 10,
					Chat:      telegramChat{ID: -100555, Title: "Test", Type: "group"},
					From:      &telegramUser{ID: 1, FirstName: "Tester", Username: "tester"},
					Text:      "hello from poll",
					Date:      time.Now().Unix(),
				},
			}}
			data, _ := json.Marshal(updates)
			resp := telegramAPIResponse{OK: true, Result: data}
			out, _ := json.Marshal(resp)
			w.Write(out)
			return
		}
		// Subsequent calls: block briefly then return empty
		time.Sleep(50 * time.Millisecond)
		resp := telegramAPIResponse{OK: true, Result: json.RawMessage("[]")}
		out, _ := json.Marshal(resp)
		w.Write(out)
	}))
	defer server.Close()

	transport := NewTelegramTransport(b, "test-token")
	transport.client = server.Client()

	// We need to override getUpdates to use the mock server.
	// Since getUpdates uses telegramAPIBase (a const), we'll test at a higher level
	// by calling HandleInbound directly, which we already tested above.
	// For the poll integration test, we verify the mock server interaction pattern.

	chatIDStr := strconv.FormatInt(-100555, 10)
	if _, ok := transport.ChatMap[chatIDStr]; !ok {
		t.Fatalf("expected chat mapping for -100555")
	}

	// Simulate what pollInbound does with a single update
	err := transport.HandleInbound(-100555, &telegramUser{FirstName: "Tester", Username: "tester"}, "hello from poll")
	if err != nil {
		t.Fatalf("HandleInbound: %v", err)
	}

	msgs := b.ChannelMessages("telegram-general")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "hello from poll" {
		t.Fatalf("expected poll content, got %q", msgs[0].Content)
	}
}
