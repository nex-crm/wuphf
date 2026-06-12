package team

// slack_transport_hardening_test.go covers the fixes from the PR-C verification
// pass: outbound control-sequence escaping (F1), the self/bot drop without a
// resolved bot user id (F2), the Ack-after-success decision (F3), and caching a
// bot user as non-human (F6).

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	teamTransport "github.com/nex-crm/wuphf/internal/team/transport"
)

// F1: dynamic fields are escaped so the office outbound path cannot inject Slack
// control sequences, while the function's own markup stays literal.
func TestFormatSlackOutboundEscapesControlSequences(t *testing.T) {
	out := formatSlackOutbound(channelMessage{
		From:    "<!channel>",
		Title:   "<!here>",
		Content: "see <http://evil|trusted label>",
	})
	for _, raw := range []string{"<!channel>", "<!here>", "<http://evil|trusted label>"} {
		if strings.Contains(out, raw) {
			t.Fatalf("control sequence %q must be escaped, got %q", raw, out)
		}
	}
	if !strings.Contains(out, "&lt;!channel&gt;") {
		t.Fatalf("expected escaped From, got %q", out)
	}
	if !strings.HasPrefix(out, "*&lt;!channel&gt;*: ") {
		t.Fatalf("structural bold-name attribution should remain literal, got %q", out)
	}
}

// F2: bot/self messages are dropped via bot_id + subtype even when auth.test
// never resolved a bot user id, and a genuine human message still flows.
func TestSlackRouteInboundDropsBotsWithoutBotUserID(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	tr.botUserID = "" // simulate auth.test failure
	host := &brokerTransportHost{broker: b}
	ctx := context.Background()

	_ = tr.routeInbound(ctx, host, &slackevents.MessageEvent{SubType: "bot_message", Channel: "C0123", Text: "x", TimeStamp: "1.1"})
	_ = tr.routeInbound(ctx, host, &slackevents.MessageEvent{BotID: "B1", User: "U1", Channel: "C0123", Text: "y", TimeStamp: "1.2"})
	if msgs := b.ChannelMessages("slack-general"); len(msgs) != 0 {
		t.Fatalf("bot messages must be dropped even without botUserID, got %d", len(msgs))
	}

	api.users["U7"] = &slack.User{ID: "U7", RealName: "Alice"}
	if err := tr.routeInbound(ctx, host, &slackevents.MessageEvent{User: "U7", Channel: "C0123", Text: "real", TimeStamp: "1.3"}); err != nil {
		t.Fatalf("routeInbound human: %v", err)
	}
	if msgs := b.ChannelMessages("slack-general"); len(msgs) != 1 {
		t.Fatalf("human message should flow, got %d", len(msgs))
	}
}

// failingHost rejects every ReceiveMessage so handleEvent must report "do not
// Ack" (so Slack redelivers).
type failingHost struct{}

func (failingHost) ReceiveMessage(context.Context, teamTransport.Message) error {
	return errors.New("broker unavailable")
}
func (failingHost) UpsertParticipant(context.Context, teamTransport.Participant, teamTransport.Binding) error {
	return nil
}
func (failingHost) DetachParticipant(context.Context, string, string) error { return nil }
func (failingHost) RevokeParticipant(context.Context, string, string) error { return nil }

// F3: a routable message whose Host write fails must NOT be Ack'd (handleEvent
// returns false) so Slack redelivers; a successful one is Ack'd (true).
func TestSlackHandleEventAckDecision(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U7"] = &slack.User{ID: "U7", RealName: "Alice"}
	tr, b := newTestSlackTransport(t, "C0123", api)
	ctx := context.Background()

	evt := socketmode.Event{
		Type: socketmode.EventTypeEventsAPI,
		Data: slackevents.EventsAPIEvent{
			InnerEvent: slackevents.EventsAPIInnerEvent{
				Data: &slackevents.MessageEvent{User: "U7", Channel: "C0123", Text: "hi", TimeStamp: "1.1"},
			},
		},
	}

	if ack := tr.handleEvent(ctx, failingHost{}, evt); ack {
		t.Fatal("a failed Host write must return ack=false so Slack redelivers")
	}
	if ack := tr.handleEvent(ctx, &brokerTransportHost{broker: b}, evt); !ack {
		t.Fatal("a successfully-handled message must return ack=true")
	}
}

// F6: a bot user resolved from users.info is cached as non-human and stays
// non-human on the cached path.
func TestSlackResolveUserCachesBotAsNonHuman(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["UBOT2"] = &slack.User{ID: "UBOT2", RealName: "Notetaker", IsBot: true}
	tr, _ := newTestSlackTransport(t, "C0123", api)
	ctx := context.Background()

	name, human := tr.resolveUser(ctx, "UBOT2")
	if human {
		t.Fatal("bot user must resolve to human=false")
	}
	if name != "Notetaker" {
		t.Fatalf("name = %q, want Notetaker", name)
	}
	if _, human2 := tr.resolveUser(ctx, "UBOT2"); human2 {
		t.Fatal("cached bot user must stay non-human")
	}
}

// A foreign bot REGISTERED via /slack/agents flows inbound attributed to its
// office slug — the ingress half of multi-agent coordination — while
// unregistered bots keep dropping (the registry is a fail-closed allowlist).
func TestSlackRouteInboundRegisteredForeignAgent(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	tr.botUserID = "UBOT"
	host := &brokerTransportHost{broker: b}
	ctx := context.Background()

	if _, err := b.RegisterSlackAgent("claude-bot", "Claude Bot", "U777"); err != nil {
		t.Fatalf("RegisterSlackAgent: %v", err)
	}

	// Modern app post: bot_id + user set, no subtype.
	if err := tr.routeInbound(ctx, host, &slackevents.MessageEvent{
		User: "U777", BotID: "B7", Channel: "C0123", Text: "analysis done", TimeStamp: "2.1",
	}); err != nil {
		t.Fatalf("routeInbound registered agent: %v", err)
	}
	// bot_message subtype variant with a user id also flows.
	if err := tr.routeInbound(ctx, host, &slackevents.MessageEvent{
		SubType: "bot_message", User: "U777", BotID: "B7", Channel: "C0123", Text: "follow-up", TimeStamp: "2.2",
	}); err != nil {
		t.Fatalf("routeInbound bot_message registered agent: %v", err)
	}

	msgs := b.ChannelMessages("slack-general")
	if len(msgs) != 2 {
		t.Fatalf("registered agent messages should flow, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.From != "claude-bot" {
			t.Fatalf("agent message must be attributed to the registered slug, got From=%q", m.From)
		}
	}

	// An unregistered bot still drops.
	_ = tr.routeInbound(ctx, host, &slackevents.MessageEvent{
		User: "U888", BotID: "B8", Channel: "C0123", Text: "noise", TimeStamp: "2.3",
	})
	if got := len(b.ChannelMessages("slack-general")); got != 2 {
		t.Fatalf("unregistered bot must stay dropped, got %d messages", got)
	}
}

// Even a (mis)registration of our OWN bot user id must not open an echo loop:
// the self-drop wins over the registry.
func TestSlackRouteInboundSelfDropBeatsRegistration(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	tr.botUserID = "UBOT"
	host := &brokerTransportHost{broker: b}

	if _, err := b.RegisterSlackAgent("self-echo", "Self", "UBOT"); err != nil {
		t.Fatalf("RegisterSlackAgent: %v", err)
	}
	_ = tr.routeInbound(context.Background(), host, &slackevents.MessageEvent{
		User: "UBOT", BotID: "B0", Channel: "C0123", Text: "echo", TimeStamp: "3.1",
	})
	if got := len(b.ChannelMessages("slack-general")); got != 0 {
		t.Fatalf("own bot id must drop even when registered, got %d messages", got)
	}
}

// An office message that tags a registered foreign agent is rendered with a
// real <@U…> mention (built from the registry, never from text) so the foreign
// bot actually wakes; unregistered tags stay escaped plain text.
func TestSlackFormatOutboundLinksRegisteredAgentMentions(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	if _, err := b.RegisterSlackAgent("claude-bot", "Claude Bot", "U777"); err != nil {
		t.Fatalf("RegisterSlackAgent: %v", err)
	}

	out, ok := tr.FormatOutbound(channelMessage{
		From:    "ceo",
		Channel: "slack-general",
		Content: "@claude-bot please review the draft; @claude-bot-2 and @pm stay put. <!channel>",
		Tagged:  []string{"claude-bot", "pm"},
	})
	if !ok {
		t.Fatal("FormatOutbound should map slack-general")
	}
	if !strings.Contains(out.Text, "<@U777>") {
		t.Fatalf("registered tag should become a real mention, got %q", out.Text)
	}
	if strings.Contains(out.Text, "@claude-bot-2 ") == false || strings.Contains(out.Text, "<@U777>-2") {
		t.Fatalf("longer slug must not be partially rewritten, got %q", out.Text)
	}
	if !strings.Contains(out.Text, "@pm") {
		t.Fatalf("unregistered tag must stay plain text, got %q", out.Text)
	}
	if strings.Contains(out.Text, "<!channel>") {
		t.Fatalf("escaping must still neutralize control sequences, got %q", out.Text)
	}
}

// Office-internal identities are NOT taggable in Slack: "@ceo"/"@human"/"@you"
// must render as plain display names, while registered foreign agents keep
// their real <@U…> pings. Sender attribution resolves to the display name with
// no "@" theater, and gate actors (human:<slack id>) resolve via the user cache.
func TestSlackRenderOfficeTagsForRealSlackReaders(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	if _, err := b.RegisterSlackAgent("claude-bot", "Claude Bot", "U777"); err != nil {
		t.Fatalf("RegisterSlackAgent: %v", err)
	}

	out, ok := tr.FormatOutbound(channelMessage{
		From:    "ceo",
		Channel: "slack-general",
		Content: "Approved @ceo's request. @claude-bot take it from here, cc @human and @you.",
		Tagged:  []string{"ceo", "claude-bot"},
	})
	if !ok {
		t.Fatal("FormatOutbound should map slack-general")
	}
	if !strings.HasPrefix(out.Text, "*CEO*: ") {
		t.Fatalf("sender must render as plain display name, got %q", out.Text)
	}
	if strings.Contains(out.Text, "@ceo") || strings.Contains(out.Text, "@human") || strings.Contains(out.Text, "@you") {
		t.Fatalf("office-internal tags must not survive as fake @tags, got %q", out.Text)
	}
	if !strings.Contains(out.Text, "Approved CEO's request") {
		t.Fatalf("member tag should become its display name, got %q", out.Text)
	}
	if !strings.Contains(out.Text, "<@U777>") {
		t.Fatalf("registered foreign agent must keep its real mention, got %q", out.Text)
	}
	if !strings.Contains(out.Text, "cc Human and Human") {
		t.Fatalf("@human/@you should render as Human, got %q", out.Text)
	}

	// Gate actor attribution: human:<slack id> resolves through the user cache.
	tr.UserMap["U08TVALKJ86"] = slackUserInfo{name: "Naj", human: true}
	if got := tr.displayNameForOffice("human:u08tvalkj86"); got != "Naj" {
		t.Fatalf("gate actor should resolve to slack display name, got %q", got)
	}
	if got := tr.displayNameForOffice("human:UNKNOWNID1"); got != "UNKNOWNID1" {
		t.Fatalf("uncached gate actor falls back to the id, got %q", got)
	}
}

func TestReplaceMentionToken(t *testing.T) {
	cases := []struct {
		name, text, token, repl, want string
	}{
		{"whole token", "ping @bot now", "@bot", "<@U1>", "ping <@U1> now"},
		{"token at end", "ping @bot", "@bot", "<@U1>", "ping <@U1>"},
		{"longer slug untouched", "ping @bot-2", "@bot", "<@U1>", "ping @bot-2"},
		{"email-like untouched", "mail@bot down", "@bot", "<@U1>", "mail@bot down"},
		{"multiple", "@bot and @bot", "@bot", "<@U1>", "<@U1> and <@U1>"},
		{"absent", "nothing here", "@bot", "<@U1>", "nothing here"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := replaceMentionToken(tc.text, tc.token, tc.repl); got != tc.want {
				t.Fatalf("replaceMentionToken(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

// Only request-bearing payload envelopes may be Acked. Acking a connection-
// lifecycle event like "hello" makes Slack drop the Socket Mode connection,
// which caused a ~10s reconnect loop where no event ever landed (caught live).
func TestSocketEventNeedsAck(t *testing.T) {
	for _, et := range []socketmode.EventType{
		socketmode.EventTypeEventsAPI,
		socketmode.EventTypeInteractive,
		socketmode.EventTypeSlashCommand,
	} {
		if !socketEventNeedsAck(et) {
			t.Fatalf("%q should be acked", et)
		}
	}
	for _, et := range []socketmode.EventType{
		socketmode.EventTypeHello,
		socketmode.EventTypeConnected,
		socketmode.EventTypeConnecting,
		socketmode.EventTypeDisconnect,
		socketmode.EventTypeIncomingError,
	} {
		if socketEventNeedsAck(et) {
			t.Fatalf("%q must NOT be acked (acking it drops the Socket Mode connection)", et)
		}
	}
}

// TestShouldAckEvent exercises the FULL ack decision the socket loop makes — the
// behavior that had no coverage (the loop was hidden behind the socketRunner
// seam), which let an Ack on the connection-handshake "hello" ship and trigger a
// ~10s reconnect loop in which no message or interaction was ever delivered.
func TestShouldAckEvent(t *testing.T) {
	req := &socketmode.Request{}
	cases := []struct {
		name    string
		evt     socketmode.Event
		handled bool
		want    bool
	}{
		// The exact bug: a hello envelope carries a Request, but acking it makes
		// Slack drop the connection. Must NOT ack.
		{"hello with request", socketmode.Event{Type: socketmode.EventTypeHello, Request: req}, true, false},
		{"connected with request", socketmode.Event{Type: socketmode.EventTypeConnected, Request: req}, true, false},
		{"events_api handled", socketmode.Event{Type: socketmode.EventTypeEventsAPI, Request: req}, true, true},
		{"events_api not handled (host failure → redeliver)", socketmode.Event{Type: socketmode.EventTypeEventsAPI, Request: req}, false, false},
		{"interactive handled", socketmode.Event{Type: socketmode.EventTypeInteractive, Request: req}, true, true},
		{"slash_command handled", socketmode.Event{Type: socketmode.EventTypeSlashCommand, Request: req}, true, true},
		{"events_api no request envelope", socketmode.Event{Type: socketmode.EventTypeEventsAPI, Request: nil}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAckEvent(tc.evt, tc.handled); got != tc.want {
				t.Fatalf("shouldAckEvent(%s, handled=%v) = %v, want %v", tc.evt.Type, tc.handled, got, tc.want)
			}
		})
	}
}
