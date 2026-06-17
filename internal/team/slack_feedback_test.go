package team

import (
	"context"
	"strings"
	"testing"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

func TestSlackPaneReplyBlocks(t *testing.T) {
	blocks, ok := slackPaneReplyBlocks("Here is the answer.")
	if !ok {
		t.Fatal("a normal reply must render blocks")
	}
	if len(blocks) != 3 {
		t.Fatalf("want section + disclaimer + actions (3 blocks), got %d", len(blocks))
	}
	// Empty and over-long replies fall back to plain text (ok=false).
	if _, ok := slackPaneReplyBlocks("   "); ok {
		t.Fatal("empty reply must not render blocks")
	}
	if _, ok := slackPaneReplyBlocks(strings.Repeat("x", slackPaneReplyTextLimit+1)); ok {
		t.Fatal("over-long reply must fall back to plain text")
	}
}

// TestSend_PaneReplyCarriesFeedbackChrome verifies a 1:1 pane reply posts with the
// disclaimer + feedback buttons, while task work and decision cards do not.
func TestSend_PaneReplyCarriesFeedbackChrome(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	lead := b.OfficeLeadSlug()
	dm := DMSlugFor(lead)
	tr.seedAssistantThread(context.Background(), "D777", "900.1")

	if err := tr.Send(context.Background(), transport.Outbound{
		Binding: transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: dm},
		Text:    "Here is the answer.",
	}); err != nil {
		t.Fatalf("send pane reply: %v", err)
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.posts) != 1 {
		t.Fatalf("want one post, got %d", len(api.posts))
	}
	blocks := api.posts[0].Blocks
	if !strings.Contains(blocks, slackFeedbackActionBlock) {
		t.Fatalf("pane reply must carry the feedback action block, got blocks=%s", blocks)
	}
	if !strings.Contains(blocks, "mistakes") {
		t.Fatalf("pane reply must carry the LLM disclaimer, got blocks=%s", blocks)
	}
	// The plain text remains as the notification fallback.
	if api.posts[0].Text != "Here is the answer." {
		t.Fatalf("notification fallback text must be preserved, got %q", api.posts[0].Text)
	}
}

func TestSend_NonPaneReplyHasNoFeedbackChrome(t *testing.T) {
	api := newFakeSlackAPI()
	tr, _ := newTestSlackTransport(t, "C0123", api)

	// A shared-channel (non-DM) reply: quiet, no feedback chrome.
	if err := tr.Send(context.Background(), transport.Outbound{
		Binding: transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: "slack-general"},
		Text:    "Status update.",
	}); err != nil {
		t.Fatalf("send channel reply: %v", err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.posts) != 1 {
		t.Fatalf("want one post, got %d", len(api.posts))
	}
	if strings.Contains(api.posts[0].Blocks, slackFeedbackActionBlock) {
		t.Fatalf("a shared-channel reply must NOT carry feedback chrome, got blocks=%s", api.posts[0].Blocks)
	}
}

func TestSlackFeedbackReaction(t *testing.T) {
	cases := []struct {
		emoji         string
		wantPositive  bool
		wantRecognize bool
	}{
		{"+1", true, true},
		{"thumbsup", true, true},
		{"-1", false, true},
		{"thumbsdown", false, true},
		{"eyes", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		pos, ok := slackFeedbackReaction(c.emoji)
		if ok != c.wantRecognize || (ok && pos != c.wantPositive) {
			t.Fatalf("slackFeedbackReaction(%q) = (%v,%v), want (%v,%v)", c.emoji, pos, ok, c.wantPositive, c.wantRecognize)
		}
	}
}

// TestHandleReactionAdded_OnlyGradesOfficeMessagesInPane locks the capture rules:
// a verdict emoji on the office's own message inside a bound pane is recorded;
// reactions on other authors' messages, the office's own reactions, non-verdict
// emoji, and reactions outside a pane are all ignored.
func TestHandleReactionAdded_OnlyGradesOfficeMessagesInPane(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	tr.botUserID = "UBOT"
	tr.seedAssistantThread(context.Background(), "D777", "900.1") // binds D777 → lead DM

	// Recorded: 👍 by a human on the office's message in the pane.
	tr.handleReactionAdded("D777", "UBOT", "UHUMAN", "+1", "950.0")
	if got := b.SlackFeedbackEvents(); len(got) != 1 || !got[0].Positive || got[0].Source != "pane_reaction" {
		t.Fatalf("a verdict on the office message in a pane must record one positive event, got %+v", got)
	}

	before := len(b.SlackFeedbackEvents())
	// Ignored: reaction on a non-office message.
	tr.handleReactionAdded("D777", "UHUMAN", "UOTHER", "+1", "951.0")
	// Ignored: the office reacting to itself.
	tr.handleReactionAdded("D777", "UBOT", "UBOT", "+1", "952.0")
	// Ignored: a non-verdict emoji.
	tr.handleReactionAdded("D777", "UBOT", "UHUMAN", "eyes", "953.0")
	// Ignored: reaction in an unmapped / non-pane channel.
	tr.handleReactionAdded("C0123", "UBOT", "UHUMAN", "+1", "954.0")
	if got := len(b.SlackFeedbackEvents()); got != before {
		t.Fatalf("ignored reactions must not record; count moved from %d to %d", before, got)
	}
}

func TestRecordSlackFeedback_BoundedBuffer(t *testing.T) {
	b := newTestBrokerWithSlackChannel(t, "C0123")
	for i := 0; i < slackFeedbackBufferMax+50; i++ {
		b.RecordSlackFeedback(true, "pane_button", "D1/1.0", "UHUMAN")
	}
	if got := len(b.SlackFeedbackEvents()); got != slackFeedbackBufferMax {
		t.Fatalf("buffer must cap at %d, got %d", slackFeedbackBufferMax, got)
	}
}
