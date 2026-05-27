package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestSlackAgentManifestUsesAgentsAndAIAppSurface(t *testing.T) {
	manifest := slackAgentManifestForMember(officeMember{Slug: "qa", Name: "Quality Agent", Role: "Checks WUPHF work"})
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	for _, want := range []string{
		`"agent_view"`,
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
