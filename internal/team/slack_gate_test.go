package team

import (
	"context"
	"strings"
	"testing"

	"github.com/slack-go/slack"
)

// seedSlackDecision appends an active approval interview (with canonical options)
// to the broker on the given office channel and returns its id. It mirrors the
// shape the action gate produces: an "approval" kind with normalized options and
// a recommended id.
func seedSlackDecision(t *testing.T, b *Broker, channel string) string {
	t.Helper()
	options, recommended := normalizeRequestOptions("approval", "approve", nil)
	req := humanInterview{
		ID:            "request-gate-1",
		Kind:          "approval",
		Status:        "pending",
		From:          "ceo",
		Channel:       channel,
		Title:         "Send the launch email",
		Question:      "Approve sending the launch email to the list?",
		Context:       "Drafted by RevOps; 4,200 recipients.",
		Options:       options,
		RecommendedID: recommended,
		Blocking:      true,
		CreatedAt:     "2026-01-01T00:00:00Z",
		UpdatedAt:     "2026-01-01T00:00:00Z",
	}
	b.mu.Lock()
	b.requests = append(b.requests, req)
	b.mu.Unlock()
	return req.ID
}

// --- Outbound render ---

func TestFormatSlackInterviewBlocksOneButtonPerOption(t *testing.T) {
	options, recommended := normalizeRequestOptions("approval", "approve", nil)
	req := humanInterview{
		ID:            "request-7",
		Kind:          "approval",
		From:          "ceo",
		Question:      "Ship it?",
		Context:       "context line",
		Options:       options,
		RecommendedID: recommended,
		Action: &approvalActionPayload{
			Platform: "gmail",
			Verb:     "send",
			Name:     "Send email",
			RawEnvelope: &approvalActionEnvelope{
				Method: "post",
				URL:    "https://gmail.example/send",
			},
		},
		ConnectionUnverified: true,
	}

	blocks := formatSlackInterviewBlocks(req)

	var action *slack.ActionBlock
	for _, blk := range blocks {
		if ab, ok := blk.(*slack.ActionBlock); ok {
			action = ab
		}
	}
	if action == nil {
		t.Fatalf("expected an action block in %d blocks", len(blocks))
	}
	if action.BlockID != slackGateActionBlock {
		t.Fatalf("action block id = %q, want %q", action.BlockID, slackGateActionBlock)
	}
	if got, want := len(action.Elements.ElementSet), len(options); got != want {
		t.Fatalf("button count = %d, want %d (one per option)", got, want)
	}

	var sawRecommendedPrimary bool
	for i, el := range action.Elements.ElementSet {
		btn, ok := el.(*slack.ButtonBlockElement)
		if !ok {
			t.Fatalf("element %d is not a button: %T", i, el)
		}
		wantValue := slackGateValue(req.ID, options[i].ID)
		if btn.Value != wantValue {
			t.Fatalf("button %d value = %q, want %q", i, btn.Value, wantValue)
		}
		if _, optID, ok := parseSlackGateValue(btn.Value); !ok || optID != options[i].ID {
			t.Fatalf("button %d value %q does not round-trip to option %q", i, btn.Value, options[i].ID)
		}
		if options[i].ID == recommended && btn.Style == slack.StylePrimary {
			sawRecommendedPrimary = true
		}
	}
	if !sawRecommendedPrimary {
		t.Fatalf("recommended option %q was not styled primary", recommended)
	}
}

func TestSlackSendDecisionRendersBlocks(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	seedSlackDecision(t, b, "slack-general")

	out, ok := tr.FormatOutbound(channelMessage{
		Channel: "slack-general",
		From:    "ceo",
		Kind:    "approval",
		Content: "Approve sending the launch email to the list?",
	})
	if !ok {
		t.Fatalf("FormatOutbound returned ok=false")
	}
	if !isSlackDecisionText(out.Text) {
		t.Fatalf("decision outbound text not recognised as decision: %q", out.Text)
	}
	if err := tr.Send(context.Background(), out); err != nil {
		t.Fatalf("Send: %v", err)
	}

	posts := api.snapshotPosts()
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if posts[0].Blocks == "" {
		t.Fatalf("decision post carried no blocks")
	}
	if !strings.Contains(posts[0].Blocks, slackGateActionBlock) {
		t.Fatalf("decision post blocks missing action block id: %s", posts[0].Blocks)
	}
}

func TestSlackSendNonDecisionStaysPlainText(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	// An active decision exists in the channel, but this is an ordinary chat
	// message — it must NOT be upgraded to buttons.
	seedSlackDecision(t, b, "slack-general")

	out, ok := tr.FormatOutbound(channelMessage{
		Channel: "slack-general",
		From:    "pm",
		Content: "just a normal message",
	})
	if !ok {
		t.Fatalf("FormatOutbound returned ok=false")
	}
	if err := tr.Send(context.Background(), out); err != nil {
		t.Fatalf("Send: %v", err)
	}
	posts := api.snapshotPosts()
	if len(posts) != 1 {
		t.Fatalf("expected 1 post, got %d", len(posts))
	}
	if posts[0].Blocks != "" {
		t.Fatalf("ordinary message should not carry blocks, got: %s", posts[0].Blocks)
	}
}

// --- Inbound click → answer ---

func blockActionsCallback(channelID, messageTS, userID, value string) slack.InteractionCallback {
	cb := slack.InteractionCallback{
		Type: slack.InteractionTypeBlockActions,
	}
	cb.Channel.ID = channelID
	cb.Message.Timestamp = messageTS
	cb.User.ID = userID
	cb.ActionCallback.BlockActions = []*slack.BlockAction{
		{BlockID: slackGateActionBlock, Value: value},
	}
	return cb
}

func TestSlackGateClickApprovesInterview(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U999"] = &slack.User{ID: "U999", Profile: slack.UserProfile{DisplayName: "Mira"}}
	tr, b := newTestSlackTransport(t, "C0123", api)
	id := seedSlackDecision(t, b, "slack-general")

	cb := blockActionsCallback("C0123", "1700000000.0001", "U999", slackGateValue(id, "approve"))
	if !tr.handleInteractive(context.Background(), cb) {
		t.Fatalf("handleInteractive returned false (should always ack)")
	}

	// The interview flipped to answered with the right choice + human actor.
	got := findRequestByID(b, id)
	if got.Status != "answered" || got.Answered == nil {
		t.Fatalf("interview not answered: status=%q answered=%v", got.Status, got.Answered)
	}
	if got.Answered.ChoiceID != "approve" {
		t.Fatalf("answer choice = %q, want approve", got.Answered.ChoiceID)
	}
	assertAnswerActor(t, b, id, "human:mira")

	// The original message was rewritten to the resolved state with no buttons.
	updates := api.snapshotUpdates()
	if len(updates) != 1 {
		t.Fatalf("expected 1 message update, got %d", len(updates))
	}
	if updates[0].ChannelID != "C0123" || updates[0].Timestamp != "1700000000.0001" {
		t.Fatalf("update targeted wrong message: %+v", updates[0])
	}
	if !strings.Contains(updates[0].Text, "Approved by") || !strings.Contains(updates[0].Text, "Mira") {
		t.Fatalf("update text not the resolved approval: %q", updates[0].Text)
	}
	if strings.Contains(updates[0].Blocks, slackGateActionBlock) {
		t.Fatalf("resolved message still carries the action block (buttons not removed): %s", updates[0].Blocks)
	}
	if eph := api.snapshotEphemerals(); len(eph) != 0 {
		t.Fatalf("successful click should post no ephemeral, got %d", len(eph))
	}
}

func TestSlackGateClickRejectsInterview(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U999"] = &slack.User{ID: "U999", Profile: slack.UserProfile{DisplayName: "Mira"}}
	tr, b := newTestSlackTransport(t, "C0123", api)
	id := seedSlackDecision(t, b, "slack-general")

	cb := blockActionsCallback("C0123", "1700000000.0001", "U999", slackGateValue(id, "reject"))
	if !tr.handleInteractive(context.Background(), cb) {
		t.Fatalf("handleInteractive returned false")
	}

	got := findRequestByID(b, id)
	if got.Answered == nil || got.Answered.ChoiceID != "reject" {
		t.Fatalf("interview not rejected: %+v", got.Answered)
	}
	updates := api.snapshotUpdates()
	if len(updates) != 1 || !strings.Contains(updates[0].Text, "Rejected by") {
		t.Fatalf("reject did not rewrite message to rejected state: %+v", updates)
	}
}

func TestSlackGateClickUnknownInterviewIsEphemeral(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U999"] = &slack.User{ID: "U999", Profile: slack.UserProfile{DisplayName: "Mira"}}
	tr, _ := newTestSlackTransport(t, "C0123", api)

	// No interview seeded → the value references a request that does not exist.
	cb := blockActionsCallback("C0123", "1700000000.0001", "U999", slackGateValue("request-missing", "approve"))
	if !tr.handleInteractive(context.Background(), cb) {
		t.Fatalf("handleInteractive returned false (should ack even on failure)")
	}

	if up := api.snapshotUpdates(); len(up) != 0 {
		t.Fatalf("unknown interview should not rewrite any message, got %d updates", len(up))
	}
	eph := api.snapshotEphemerals()
	if len(eph) != 1 {
		t.Fatalf("expected 1 ephemeral notice, got %d", len(eph))
	}
	if eph[0].UserID != "U999" || !strings.Contains(eph[0].Text, "no longer active") {
		t.Fatalf("ephemeral notice wrong: %+v", eph[0])
	}
}

func TestSlackGateClickExpiredInterviewIsEphemeral(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U999"] = &slack.User{ID: "U999", Profile: slack.UserProfile{DisplayName: "Mira"}}
	tr, b := newTestSlackTransport(t, "C0123", api)
	id := seedSlackDecision(t, b, "slack-general")

	// First click answers it; the second click hits a terminal (no-longer-active)
	// request and must degrade to an ephemeral notice, not a second answer.
	first := blockActionsCallback("C0123", "1700000000.0001", "U999", slackGateValue(id, "approve"))
	tr.handleInteractive(context.Background(), first)

	second := blockActionsCallback("C0123", "1700000000.0001", "U999", slackGateValue(id, "reject"))
	if !tr.handleInteractive(context.Background(), second) {
		t.Fatalf("second click returned false")
	}

	// Still answered with the FIRST choice (approve), not overwritten by reject.
	got := findRequestByID(b, id)
	if got.Answered == nil || got.Answered.ChoiceID != "approve" {
		t.Fatalf("second click overwrote terminal state: %+v", got.Answered)
	}
	if up := api.snapshotUpdates(); len(up) != 1 {
		t.Fatalf("second click should not rewrite the message again, got %d updates", len(up))
	}
	eph := api.snapshotEphemerals()
	if len(eph) != 1 || !strings.Contains(eph[0].Text, "no longer active") {
		t.Fatalf("expected one 'no longer active' ephemeral, got %+v", eph)
	}
}

func TestSlackGateMalformedValueIsEphemeral(t *testing.T) {
	api := newFakeSlackAPI()
	api.users["U999"] = &slack.User{ID: "U999"}
	tr, b := newTestSlackTransport(t, "C0123", api)
	id := seedSlackDecision(t, b, "slack-general")

	cb := blockActionsCallback("C0123", "1700000000.0001", "U999", "garbage-no-separator")
	if !tr.handleInteractive(context.Background(), cb) {
		t.Fatalf("handleInteractive returned false")
	}
	// Interview untouched.
	if got := findRequestByID(b, id); got.Answered != nil {
		t.Fatalf("malformed click answered the interview: %+v", got.Answered)
	}
	if eph := api.snapshotEphemerals(); len(eph) != 1 {
		t.Fatalf("malformed click should post one ephemeral, got %d", len(eph))
	}
}

func TestParseSlackGateValue(t *testing.T) {
	cases := []struct {
		in      string
		wantID  string
		wantOpt string
		wantOK  bool
	}{
		{"request-7|approve", "request-7", "approve", true},
		{"request-7|reject_with_steer", "request-7", "reject_with_steer", true},
		{"noseparator", "", "", false},
		{"|approve", "", "", false},
		{"request-7|", "", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		id, opt, ok := parseSlackGateValue(tc.in)
		if ok != tc.wantOK || id != tc.wantID || opt != tc.wantOpt {
			t.Fatalf("parseSlackGateValue(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.in, id, opt, ok, tc.wantID, tc.wantOpt, tc.wantOK)
		}
	}
}

// --- helpers ---

func findRequestByID(b *Broker, id string) humanInterview {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, req := range b.requests {
		if req.ID == id {
			return req
		}
	}
	return humanInterview{}
}

func assertAnswerActor(t *testing.T, b *Broker, id, wantActor string) {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, action := range b.actions {
		if action.Kind == "request_answered" && action.RelatedID == id {
			if action.Actor != wantActor {
				t.Fatalf("answer actor = %q, want %q", action.Actor, wantActor)
			}
			return
		}
	}
	t.Fatalf("no request_answered audit entry for %s", id)
}
