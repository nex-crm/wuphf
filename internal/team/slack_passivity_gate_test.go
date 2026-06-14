package team

import (
	"context"
	"strings"
	"testing"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
)

// gateFixture: a transport over a bridged channel C0123 (slug slack-general),
// bot id UBOT, with a human user U7 resolvable.
func gateFixture(t *testing.T) (*SlackTransport, *Broker, *brokerTransportHost) {
	t.Helper()
	api := newFakeSlackAPI()
	api.users["U7"] = &slack.User{ID: "U7", RealName: "Alice", IsBot: false}
	tr, b := newTestSlackTransport(t, "C0123", api)
	return tr, b, &brokerTransportHost{broker: b}
}

func TestPassivityGate_UntaggedHumanRecordedButSuppressed(t *testing.T) {
	tr, b, host := gateFixture(t)
	err := tr.routeInbound(context.Background(), host, &slackevents.MessageEvent{
		User: "U7", Channel: "C0123", Text: "just chatting with the team", TimeStamp: "1.0",
	})
	if err != nil {
		t.Fatalf("routeInbound: %v", err)
	}
	// Recorded for context...
	msgs := b.ChannelMessages("slack-general")
	if len(msgs) != 1 {
		t.Fatalf("untagged human chatter must be recorded for context, got %d", len(msgs))
	}
	// ...but it wakes no one: the message carries no tag and no task, in a Slack
	// channel, so the delivery path suppresses it.
	if !b.slackSuppressesWake(msgs[0]) {
		t.Fatalf("untagged ambient Slack message must suppress the wake: %+v", msgs[0])
	}
}

func TestSlackSuppressesWake(t *testing.T) {
	b := newTestBrokerWithSlackChannel(t, "C0123") // slug slack-general
	cases := []struct {
		name string
		msg  channelMessage
		want bool
	}{
		{"untagged human in slack", channelMessage{From: "human:u7", Channel: "slack-general"}, true},
		{"tagged human in slack", channelMessage{From: "human:u7", Channel: "slack-general", Tagged: []string{"ceo"}}, false},
		{"task-thread reply in slack", channelMessage{From: "human:u7", Channel: "slack-general", SourceTaskID: "OFFICE-1"}, false},
		{"foreign agent in slack", channelMessage{From: "claude-bot", Channel: "slack-general"}, false},
		{"untagged human in non-slack", channelMessage{From: "human:u7", Channel: "general"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.slackSuppressesWake(tc.msg); got != tc.want {
				t.Fatalf("slackSuppressesWake = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPassivityGate_TaggedHumanActsAndRecordsOrigin(t *testing.T) {
	tr, b, host := gateFixture(t)
	// Tagged at channel root → origin is the tag message's own ts.
	err := tr.routeInbound(context.Background(), host, &slackevents.MessageEvent{
		User: "U7", Channel: "C0123", Text: "<@UBOT> reconcile June invoices", TimeStamp: "2.5",
	})
	if err != nil {
		t.Fatalf("routeInbound: %v", err)
	}
	if msgs := b.ChannelMessages("slack-general"); len(msgs) != 1 {
		t.Fatalf("tagged human message must ingress, got %d", len(msgs))
	}
	if got := b.consumeSlackTagOrigin("C0123"); got != "2.5" {
		t.Fatalf("tag origin = %q, want 2.5 (the tag message ts)", got)
	}
}

func TestPassivityGate_TaggedInThreadRecordsThreadOrigin(t *testing.T) {
	tr, b, host := gateFixture(t)
	err := tr.routeInbound(context.Background(), host, &slackevents.MessageEvent{
		User: "U7", Channel: "C0123", Text: "<@UBOT> can you take this", TimeStamp: "3.5", ThreadTimeStamp: "3.0",
	})
	if err != nil {
		t.Fatalf("routeInbound: %v", err)
	}
	if got := b.consumeSlackTagOrigin("C0123"); got != "3.0" {
		t.Fatalf("tag origin = %q, want 3.0 (the thread it was tagged in)", got)
	}
}

func TestPassivityGate_UntaggedReplyInTaskThreadFlows(t *testing.T) {
	tr, b, host := gateFixture(t)
	// A task with a Slack root card at ts 9.0.
	b.mu.Lock()
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-1", Channel: "slack-general", Owner: "ceo", ThreadID: "msg-root"})
	b.slackTaskCards = map[string]slackTaskCardRecord{"OFFICE-1": {ChannelID: "C0123", Timestamp: "9.0", State: "running"}}
	b.mu.Unlock()

	// An untagged human follow-up INSIDE that task thread still flows (it
	// continues work WUPHF already owns).
	err := tr.routeInbound(context.Background(), host, &slackevents.MessageEvent{
		User: "U7", Channel: "C0123", Text: "any update?", TimeStamp: "9.1", ThreadTimeStamp: "9.0",
	})
	if err != nil {
		t.Fatalf("routeInbound: %v", err)
	}
	if msgs := b.ChannelMessages("slack-general"); len(msgs) != 1 {
		t.Fatalf("task-thread reply must flow even untagged, got %d", len(msgs))
	}
}

func TestPassivityGate_ForeignAgentExempt(t *testing.T) {
	tr, b, host := gateFixture(t)
	if _, err := b.RegisterSlackAgent("claude-bot", "Claude Bot", "U777"); err != nil {
		t.Fatalf("register: %v", err)
	}
	// A registered foreign agent posting untagged at channel root still ingresses
	// — it's WUPHF's own delegate, not ambient chatter.
	err := tr.routeInbound(context.Background(), host, &slackevents.MessageEvent{
		User: "U777", BotID: "B7", Channel: "C0123", Text: "analysis complete", TimeStamp: "5.0",
	})
	if err != nil {
		t.Fatalf("routeInbound: %v", err)
	}
	msgs := b.ChannelMessages("slack-general")
	if len(msgs) != 1 || msgs[0].From != "claude-bot" {
		t.Fatalf("foreign agent must be exempt from the gate, got %+v", msgs)
	}
}

// TestTaskThreadBacklink_PostsIntoOrigin verifies that creating a task's Slack
// thread root posts a backlink into the conversation where WUPHF was tagged.
func TestTaskThreadBacklink_PostsIntoOrigin(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.mu.Lock()
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-42", Channel: "slack-general", Owner: "ceo", Title: "Reconcile invoices"})
	b.mu.Unlock()
	// A tag origin recorded for the channel (as the gate would on a tag in thread "7.0").
	b.recordSlackTagOrigin("C0123", "7.0")

	ts := tr.ensureTaskThreadRoot(context.Background(), "OFFICE-42")
	if ts == "" {
		t.Fatal("ensureTaskThreadRoot returned empty ts")
	}

	// Two posts: the task card (root, no thread) and the backlink (threaded under
	// the origin 7.0, carrying the task id + a permalink to the new thread).
	var card, backlink *fakePost
	for i := range api.posts {
		p := &api.posts[i]
		if p.ThreadTS == "7.0" {
			backlink = p
		} else if p.ThreadTS == "" {
			card = p
		}
	}
	if card == nil {
		t.Fatal("expected the task card posted at channel root")
	}
	if backlink == nil {
		t.Fatalf("expected a backlink threaded under the origin 7.0, posts=%+v", api.posts)
	}
	if !strings.Contains(backlink.Text, "OFFICE-42") {
		t.Fatalf("backlink should name the task, got %q", backlink.Text)
	}
	if !strings.Contains(backlink.Text, "slack.example") {
		t.Fatalf("backlink should carry the permalink, got %q", backlink.Text)
	}
	// Origin is consumed exactly once.
	if got := b.consumeSlackTagOrigin("C0123"); got != "" {
		t.Fatalf("tag origin should be consumed by the backlink, still got %q", got)
	}
}

// TestTaskThreadBacklink_NoOriginNoBacklink: without a recorded tag origin (e.g.
// a scheduled task), only the card is posted, no backlink.
func TestTaskThreadBacklink_NoOriginNoBacklink(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.mu.Lock()
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-9", Channel: "slack-general", Owner: "ceo"})
	b.mu.Unlock()

	if ts := tr.ensureTaskThreadRoot(context.Background(), "OFFICE-9"); ts == "" {
		t.Fatal("ensureTaskThreadRoot returned empty ts")
	}
	for _, p := range api.posts {
		if p.ThreadTS != "" {
			t.Fatalf("no backlink expected without a tag origin, got threaded post %+v", p)
		}
	}
}
