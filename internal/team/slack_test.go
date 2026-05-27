package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

func TestSlackConnectCreatesMirroredChannel(t *testing.T) {
	b := newTestBroker(t)
	body := strings.NewReader(`{"channel_id":"C123","channel_name":"eng-updates","channel_slug":"engineering"}`)
	req := httptest.NewRequest(http.MethodPost, "/slack/connect", body)
	rec := httptest.NewRecorder()

	b.handleSlackConnect(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	channels := b.SurfaceChannels(slackAdapterName)
	if len(channels) != 1 {
		t.Fatalf("surface channels = %d, want 1", len(channels))
	}
	ch := channels[0]
	if ch.Slug != "engineering" || ch.Surface == nil || ch.Surface.RemoteID != "C123" {
		t.Fatalf("unexpected channel: %+v", ch)
	}
}

func TestSlackAgentCreateCommandCreatesOfficeMember(t *testing.T) {
	b := newTestBroker(t)
	slack := NewSlackTransport(b, "xoxb-test", "xapp-test", "Ubot")

	reply := slack.dispatchSlackbotCommand(context.Background(), slackCommandContext{}, "wuphf agent create qa Quality Agent")

	if !strings.Contains(reply, "Created agent `@qa`") {
		t.Fatalf("reply = %q", reply)
	}
	var found bool
	for _, member := range b.OfficeMembers() {
		if member.Slug == "qa" && member.Name == "Quality Agent" && member.CreatedBy == "slack" {
			found = true
		}
	}
	if !found {
		t.Fatalf("created member not found: %+v", b.OfficeMembers())
	}
}

func TestSlackSlashCommandRequiresMappedChannel(t *testing.T) {
	b := newTestBroker(t)
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode slack request: %v", err)
		}
		if text, _ := body["text"].(string); text != "" {
			posted = text
		}
		_, _ = w.Write([]byte(`{"ok":true,"channel":"Cpublic","ts":"1.0"}`))
	}))
	defer srv.Close()

	slack := NewSlackTransport(b, "xoxb-test", "xapp-test", "Ubot")
	slack.client.baseURL = srv.URL
	slack.handleSlashCommand(context.Background(), slackSlashCommandPayload{
		Text:        "agent create qa Quality Agent",
		ChannelID:   "Cpublic",
		ChannelName: "new-channel",
		UserID:      "U1",
	})

	if !strings.Contains(posted, "only enabled in Slack channels mapped") {
		t.Fatalf("posted reply = %q", posted)
	}
	for _, member := range b.OfficeMembers() {
		if member.Slug == "qa" {
			t.Fatalf("unmapped Slack channel created member: %+v", member)
		}
	}
}

func TestSlackSlashCommandPostsMappedChannelMessageAndTagsAgent(t *testing.T) {
	b := newTestBroker(t)
	var posted map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
			t.Fatalf("decode slack request: %v", err)
		}
		_, _ = w.Write([]byte(`{"ok":true,"channel":"Cprivate","ts":"333.000"}`))
	}))
	defer srv.Close()

	slack := NewSlackTransport(b, "xoxb-test", "xapp-test", "Ubot")
	slack.client.baseURL = srv.URL
	slack.setSlackChannelMap("Cprivate", "general")
	slack.handleSlashCommand(context.Background(), slackSlashCommandPayload{
		Text:        "@ceo please triage this",
		ChannelID:   "Cprivate",
		ChannelName: "wuphf-office-private",
		UserID:      "U1",
		UserName:    "najm",
	})

	messages := b.ChannelMessages("general")
	got := messages[len(messages)-1]
	if got.From != "slack:najm" || got.Content != "@ceo please triage this" {
		t.Fatalf("message = %+v", got)
	}
	if !containsString(got.Tagged, "ceo") {
		t.Fatalf("tagged = %+v, want ceo", got.Tagged)
	}
	if posted["channel"] != "Cprivate" {
		t.Fatalf("posted payload = %+v", posted)
	}
	if gotID := b.slackMessageIDForTimestamp("Cprivate", "333.000"); gotID != got.ID {
		t.Fatalf("slack receipt resolved %q, want %q", gotID, got.ID)
	}
}

func TestSlackAppMentionPostsNormalMappedChannelMessage(t *testing.T) {
	b := newTestBroker(t)
	slack := NewSlackTransport(b, "xoxb-test", "xapp-test", "Ubot")
	slack.setSlackChannelMap("Cprivate", "general")
	host := newFakeHost()

	slack.handleEventCallback(context.Background(), host, slackEventPayload{
		Event: json.RawMessage(`{
			"type":"app_mention",
			"channel":"Cprivate",
			"user":"U1",
			"text":"<@Ubot> agent create qa Quality Agent",
			"ts":"111.000"
		}`),
	})

	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(host.messages))
	}
	msg := host.messages[0]
	if msg.Text != "agent create qa Quality Agent" {
		t.Fatalf("message text = %q", msg.Text)
	}
	if msg.Binding.ChannelSlug != "general" || msg.ExternalChannelID != "Cprivate" {
		t.Fatalf("message binding/external = %+v", msg)
	}
	for _, member := range b.OfficeMembers() {
		if member.Slug == "qa" {
			t.Fatalf("app mention dispatched as command and created member: %+v", member)
		}
	}
}

func TestSlackInboundMentionsAgentsAndResolvesThreadReply(t *testing.T) {
	b := newTestBroker(t)
	b.recordSlackOutbound("msg-root", "Cprivate", "111.000")
	slack := NewSlackTransport(b, "xoxb-test", "xapp-test", "Ubot")
	slack.setSlackChannelMap("Cprivate", "general")
	host := newFakeHost()

	slack.handleEventCallback(context.Background(), host, slackEventPayload{
		Event: json.RawMessage(`{
			"type":"message",
			"channel":"Cprivate",
			"user":"U1",
			"text":"@ceo please take this",
			"ts":"222.000",
			"thread_ts":"111.000"
		}`),
	})

	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(host.messages))
	}
	msg := host.messages[0]
	if msg.ReplyTo != "msg-root" {
		t.Fatalf("reply_to = %q, want msg-root", msg.ReplyTo)
	}
	if !containsString(msg.Tagged, "ceo") {
		t.Fatalf("tagged = %+v, want ceo", msg.Tagged)
	}
	if msg.ThreadKey != "111.000" {
		t.Fatalf("thread key = %q, want Slack thread root", msg.ThreadKey)
	}
}

func TestSlackAgentManifestUsesAgentsAndAIAppSurface(t *testing.T) {
	manifest := slackAgentManifestForMember(officeMember{Slug: "qa", Name: "Quality Agent", Role: "Checks WUPHF work"})
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	for _, want := range []string{
		`"assistant_view"`,
		`"assistant_description"`,
		`"suggested_prompts"`,
		`"assistant:write"`,
		`"chat:write"`,
		`"im:history"`,
		`"assistant_thread_started"`,
		`"assistant_thread_context_changed"`,
		`"message.im"`,
		`"socket_mode_enabled":true`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("manifest missing %s: %s", want, raw)
		}
	}
}

func TestSlackOutboundEscapesMarkup(t *testing.T) {
	got := formatSlackOutbound(channelMessage{
		From:    "qa",
		Content: "review <this> & report",
	})
	if strings.Contains(got, "<this>") || !strings.Contains(got, "&lt;this&gt; &amp; report") {
		t.Fatalf("escaped output = %q", got)
	}
}

func TestSlackOutboundIncludesWUPHFChannelFootnote(t *testing.T) {
	got := formatSlackOutbound(channelMessage{
		From:    "qa",
		Channel: "wuphf-office",
		ReplyTo: "msg-root",
		Content: "done",
	})
	if !strings.Contains(got, "_Reply from WUPHF #wuphf-office_") {
		t.Fatalf("missing channel footnote: %q", got)
	}
}

func TestSlackBlocksRenderDecisionAndHTMLContent(t *testing.T) {
	blocks := slackBlocksForMessage(channelMessage{
		From:    "ceo",
		Channel: "wuphf-office",
		Kind:    "interview",
		Title:   "Launch approval",
		Content: "<p><strong>Ship?</strong></p><ul><li>Run checks</li><li>Notify team</li></ul>",
		ReplyTo: "msg-root",
	})
	if len(blocks) < 4 {
		t.Fatalf("blocks = %+v", blocks)
	}
	header, _ := blocks[0]["text"].(map[string]any)
	if header["text"] != "Decision needed" {
		t.Fatalf("header = %+v", blocks[0])
	}
	raw, err := json.Marshal(blocks)
	if err != nil {
		t.Fatal(err)
	}
	rendered := string(raw)
	for _, want := range []string{"*Ship?*", "• Run checks", "_Reply from WUPHF #wuphf-office_", `"expand":true`} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("blocks missing %q: %s", want, rendered)
		}
	}
}

func TestBrokerTransportRecordsSlackInboundThreadReceipt(t *testing.T) {
	b := newTestBroker(t)
	host := &brokerTransportHost{broker: b}
	err := host.ReceiveMessage(context.Background(), transport.Message{
		Participant: transport.Participant{
			AdapterName: slackAdapterName,
			Key:         "U1",
			DisplayName: "slack:U1",
			Human:       true,
		},
		Binding:           transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: "general"},
		Text:              "@ceo thread this",
		ExternalID:        "222.000",
		ExternalChannelID: "Cprivate",
		Tagged:            []string{"ceo"},
		ReplyTo:           "msg-root",
	})
	if err != nil {
		t.Fatalf("ReceiveMessage: %v", err)
	}
	messages := b.ChannelMessages("general")
	got := messages[len(messages)-1]
	if got.ReplyTo != "msg-root" || !containsString(got.Tagged, "ceo") {
		t.Fatalf("message = %+v", got)
	}
	if gotID := b.slackMessageIDForTimestamp("Cprivate", "222.000"); gotID != got.ID {
		t.Fatalf("slack receipt resolved %q, want %q", gotID, got.ID)
	}
}
