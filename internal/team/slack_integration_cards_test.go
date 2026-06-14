package team

import (
	"context"
	"strings"
	"testing"

	"github.com/slack-go/slack"

	"github.com/nex-crm/wuphf/internal/action"
)

// seedSlackConnect appends an active connect decision (the shape
// ensureConnectRequest mints) to the broker on the given channel and returns its
// id. It mirrors the resolver's connect card: a "connect" kind anchored to a
// platform with a logo url and the canonical connect/skip options.
func seedSlackConnect(t *testing.T, b *Broker, channel, platform, logoURL string) string {
	t.Helper()
	options, recommended := requestOptionDefaults("connect")
	req := humanInterview{
		ID:            "request-connect-1",
		Kind:          "connect",
		Status:        "pending",
		From:          "revops",
		Channel:       channel,
		Title:         "Connect " + platform,
		Question:      "Connect " + platform + " so the team can run this action.",
		Options:       options,
		RecommendedID: recommended,
		Blocking:      true,
		Required:      true,
		Platform:      platform,
		LogoURL:       logoURL,
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
	b.mu.Lock()
	b.requests = append(b.requests, req)
	b.mu.Unlock()
	return req.ID
}

func connectRequestFixture(platform, logoURL string) humanInterview {
	options, recommended := requestOptionDefaults("connect")
	display := action.DisplayPlatformName(platform)
	return humanInterview{
		ID:            "request-connect-1",
		Kind:          "connect",
		Status:        "pending",
		From:          "revops",
		Channel:       "slack-general",
		Title:         "Connect " + display,
		Question:      "Connect " + display + " so the team can run this action.",
		Options:       options,
		RecommendedID: recommended,
		Blocking:      true,
		Required:      true,
		Platform:      platform,
		LogoURL:       logoURL,
	}
}

// --- Block Kit render ---

func TestFormatSlackConnectBlocksRendersCard(t *testing.T) {
	req := connectRequestFixture("gmail", "https://logo.example/gmail.png")
	blocks := formatSlackConnectBlocks(req, "Not connected", true)

	var (
		sawLogo    bool
		sawStatus  bool
		actionBlk  *slack.ActionBlock
		sectionTxt string
	)
	for _, blk := range blocks {
		switch b := blk.(type) {
		case *slack.SectionBlock:
			if b.Text != nil {
				sectionTxt += b.Text.Text + "\n"
			}
			if b.Accessory != nil && b.Accessory.ImageElement != nil {
				if url := b.Accessory.ImageElement.ImageURL; url != nil && *url == "https://logo.example/gmail.png" {
					sawLogo = true
				}
			}
		case *slack.ContextBlock:
			for _, el := range b.ContextElements.Elements {
				if txt, ok := el.(*slack.TextBlockObject); ok && strings.Contains(txt.Text, "Not connected") {
					sawStatus = true
				}
			}
		case *slack.ActionBlock:
			actionBlk = b
		}
	}

	if !strings.Contains(sectionTxt, "Gmail") {
		t.Fatalf("card header missing integration name; sections=%q", sectionTxt)
	}
	if !sawLogo {
		t.Fatalf("card section did not carry the catalog logo as an accessory image")
	}
	if !sawStatus {
		t.Fatalf("card context block did not carry the connection status")
	}
	if actionBlk == nil || actionBlk.BlockID != slackConnectActionBlock {
		t.Fatalf("expected an action block with id %q, got %+v", slackConnectActionBlock, actionBlk)
	}
	if got := len(actionBlk.Elements.ElementSet); got != 2 {
		t.Fatalf("connect card should have 2 buttons (connect + not now), got %d", got)
	}
	connect, ok := actionBlk.Elements.ElementSet[0].(*slack.ButtonBlockElement)
	if !ok || connect.Style != slack.StylePrimary {
		t.Fatalf("first button should be a primary Connect button, got %+v", actionBlk.Elements.ElementSet[0])
	}
	if _, opt, ok := parseSlackGateValue(connect.Value); !ok || opt != slackConnectOptionConnect {
		t.Fatalf("connect button value %q does not round-trip to the connect option", connect.Value)
	}
}

func TestFormatSlackConnectBlocksOmitsUnsafeLogo(t *testing.T) {
	for _, logo := range []string{"", "javascript:alert(1)", "data:image/png;base64,xx", "not a url"} {
		req := connectRequestFixture("notion", logo)
		blocks := formatSlackConnectBlocks(req, "Not connected", true)
		for _, blk := range blocks {
			if sb, ok := blk.(*slack.SectionBlock); ok && sb.Accessory != nil && sb.Accessory.ImageElement != nil {
				t.Fatalf("unsafe/empty logo %q must not become an image accessory", logo)
			}
		}
	}
}

func TestFormatSlackConnectBlocksInformationalWhenUnconfigured(t *testing.T) {
	req := connectRequestFixture("gmail", "https://logo.example/gmail.png")
	blocks := formatSlackConnectBlocks(req, "Not connected", false)
	for _, blk := range blocks {
		if _, ok := blk.(*slack.ActionBlock); ok {
			t.Fatalf("an unconfigured Composio must render no live button (informational card only)")
		}
	}
	var sawGuidance bool
	for _, blk := range blocks {
		if cb, ok := blk.(*slack.ContextBlock); ok {
			for _, el := range cb.ContextElements.Elements {
				if txt, ok := el.(*slack.TextBlockObject); ok && strings.Contains(txt.Text, "web app") {
					sawGuidance = true
				}
			}
		}
	}
	if !sawGuidance {
		t.Fatalf("informational card should guide the human to connect from the web app")
	}
}

func TestSlackConnectConnectingBlocksAddsAuthURLButton(t *testing.T) {
	req := connectRequestFixture("gmail", "")
	blocks := slackConnectConnectingBlocks(req, "https://auth.example/oauth?x=1")

	var urlBtn *slack.ButtonBlockElement
	for _, blk := range blocks {
		if ab, ok := blk.(*slack.ActionBlock); ok {
			for _, el := range ab.Elements.ElementSet {
				if btn, ok := el.(*slack.ButtonBlockElement); ok {
					urlBtn = btn
				}
			}
		}
	}
	if urlBtn == nil || urlBtn.URL != "https://auth.example/oauth?x=1" {
		t.Fatalf("connecting card did not carry an auth-url open button, got %+v", urlBtn)
	}

	// An unusable auth url degrades to a text-only connecting card (no button).
	for _, bad := range []string{"", "javascript:bad", "ftp://x/y"} {
		for _, blk := range slackConnectConnectingBlocks(req, bad) {
			if _, ok := blk.(*slack.ActionBlock); ok {
				t.Fatalf("unsafe auth url %q must not become a URL button", bad)
			}
		}
	}
}

func TestSlackConnectDisplayName(t *testing.T) {
	cases := []struct {
		title    string
		platform string
		want     string
	}{
		{"Connect Gmail", "gmail", "Gmail"},
		{"connect HubSpot", "hubspot", "HubSpot"},
		{"", "googlecalendar", "Google Calendar"},
		{"", "", "the integration"},
	}
	for _, tc := range cases {
		got := slackConnectDisplayName(humanInterview{Title: tc.title, Platform: tc.platform})
		if got != tc.want {
			t.Fatalf("slackConnectDisplayName(title=%q,platform=%q) = %q, want %q", tc.title, tc.platform, got, tc.want)
		}
	}
}

// --- Outbound dispatch (Send renders the connect card) ---

func TestSlackSendConnectDecisionRendersConnectCard(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Composio unconfigured → informational card, deterministic + no network.
	t.Setenv("WUPHF_COMPOSIO_API_KEY", "")
	t.Setenv("COMPOSIO_API_KEY", "")
	t.Setenv("WUPHF_COMPOSIO_USER_ID", "")
	t.Setenv("COMPOSIO_USER_ID", "")

	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	seedSlackConnect(t, b, "slack-general", "gmail", "https://logo.example/gmail.png")

	// A connect decision surfaces as a "human_request_raised" chat message, which
	// FormatOutbound renders as a decision (the 📋 prefix) → Send upgrades to blocks.
	out, ok := tr.FormatOutbound(channelMessage{
		Channel: "slack-general",
		From:    "revops",
		Kind:    "human_request_raised",
		Content: "Connect gmail so the team can run this action.",
	})
	if !ok {
		t.Fatalf("FormatOutbound returned ok=false")
	}
	if !isSlackDecisionText(out.Text) {
		t.Fatalf("connect outbound text not recognised as a decision: %q", out.Text)
	}
	if err := tr.Send(context.Background(), out); err != nil {
		t.Fatalf("Send: %v", err)
	}
	posts := api.snapshotPosts()
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if !strings.Contains(posts[0].Blocks, slackConnectActionBlock) && !strings.Contains(posts[0].Blocks, "web app") {
		t.Fatalf("connect post did not render the connect card: %s", posts[0].Blocks)
	}
}

// --- Inbound: connect button starts OAuth (unconfigured → ephemeral) ---

func TestSlackConnectClickUnconfiguredIsEphemeral(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WUPHF_COMPOSIO_API_KEY", "")
	t.Setenv("COMPOSIO_API_KEY", "")
	t.Setenv("WUPHF_COMPOSIO_USER_ID", "")
	t.Setenv("COMPOSIO_USER_ID", "")

	api := newFakeSlackAPI()
	api.users["U999"] = &slack.User{ID: "U999", Profile: slack.UserProfile{DisplayName: "Mira"}}
	tr, b := newTestSlackTransport(t, "C0123", api)
	id := seedSlackConnect(t, b, "slack-general", "gmail", "")

	cb := connectActionsCallback("C0123", "1700000000.0001", "U999", slackGateValue(id, slackConnectOptionConnect))
	if !tr.handleInteractive(context.Background(), cb) {
		t.Fatalf("handleInteractive returned false")
	}

	// Composio is not configured → no card rewrite, a single guidance ephemeral.
	if up := api.snapshotUpdates(); len(up) != 0 {
		t.Fatalf("a failed connect start should not rewrite the card, got %d updates", len(up))
	}
	eph := api.snapshotEphemerals()
	if len(eph) != 1 || !strings.Contains(eph[0].Text, "web app") {
		t.Fatalf("expected one 'connect from the web app' ephemeral, got %+v", eph)
	}
	// The connect card must stay unanswered (start failed, not skipped).
	if got := findRequestByID(b, id); got.Answered != nil {
		t.Fatalf("a failed connect start must not answer the card: %+v", got.Answered)
	}
}

// --- Inbound: "Not now" answers the connect gate with skip ---

func TestSlackConnectClickSkipAnswersGate(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U999"] = &slack.User{ID: "U999", Profile: slack.UserProfile{DisplayName: "Mira"}}
	tr, b := newTestSlackTransport(t, "C0123", api)
	id := seedSlackConnect(t, b, "slack-general", "gmail", "")

	cb := connectActionsCallback("C0123", "1700000000.0001", "U999", slackGateValue(id, slackConnectOptionSkip))
	if !tr.handleInteractive(context.Background(), cb) {
		t.Fatalf("handleInteractive returned false")
	}

	got := findRequestByID(b, id)
	if got.Answered == nil || got.Answered.ChoiceID != slackConnectOptionSkip {
		t.Fatalf("skip did not answer the connect gate: %+v", got.Answered)
	}
	updates := api.snapshotUpdates()
	if len(updates) != 1 || !strings.Contains(updates[0].Text, "skipped") {
		t.Fatalf("skip did not rewrite the card to a skipped state: %+v", updates)
	}
	if strings.Contains(updates[0].Blocks, slackConnectActionBlock) {
		t.Fatalf("resolved connect card still carries the action block (buttons not removed): %s", updates[0].Blocks)
	}
}

func TestSlackConnectClickWrongChannelIsEphemeral(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U999"] = &slack.User{ID: "U999", Profile: slack.UserProfile{DisplayName: "Mira"}}
	tr, _ := newTestSlackTransport(t, "C0123", api)

	// No connect request seeded in this channel → the click references a request
	// that is not the active connect decision here.
	cb := connectActionsCallback("C0123", "1700000000.0001", "U999", slackGateValue("request-missing", slackConnectOptionConnect))
	if !tr.handleInteractive(context.Background(), cb) {
		t.Fatalf("handleInteractive returned false")
	}
	eph := api.snapshotEphemerals()
	if len(eph) != 1 || !strings.Contains(eph[0].Text, "no longer available in this channel") {
		t.Fatalf("expected a channel-binding ephemeral, got %+v", eph)
	}
}

// --- helpers ---

func connectActionsCallback(channelID, messageTS, userID, value string) slack.InteractionCallback {
	cb := slack.InteractionCallback{Type: slack.InteractionTypeBlockActions}
	cb.Channel.ID = channelID
	cb.Message.Timestamp = messageTS
	cb.User.ID = userID
	cb.ActionCallback.BlockActions = []*slack.BlockAction{
		{BlockID: slackConnectActionBlock, Value: value},
	}
	return cb
}
