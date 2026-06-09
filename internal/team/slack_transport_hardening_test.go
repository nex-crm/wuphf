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
	if !strings.Contains(out, "*@") {
		t.Fatalf("structural markup should remain literal, got %q", out)
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
