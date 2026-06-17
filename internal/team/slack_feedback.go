package team

// slack_feedback.go adds the agent-quality feedback channel Slack recommends for
// AI apps, plus the LLM-output disclaimer, scoped to the 1:1 Assistant pane.
//
// Two capture paths, both pointing at Broker.RecordSlackFeedback:
//   - explicit 👍 / 👎 buttons rendered under each pane reply (handleInteractive)
//   - native emoji reactions on the office's pane messages (reaction_added)
//
// The pane is the right and only place for this chrome: it is a private 1:1 where
// a guiding, gradable agent is exactly what the user wants. Shared channels stay
// quiet — no buttons, no disclaimer footer — preserving the deliberate restraint
// of the passivity gate and the silence filter.

import (
	"context"
	"log"
	"strings"

	"github.com/slack-go/slack"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

const (
	// slackFeedbackActionBlock is the block_id of the thumbs feedback actions on a
	// pane reply; handleInteractive routes clicks on it here.
	slackFeedbackActionBlock = "wuphf_feedback"
	slackFeedbackActionUp    = "wuphf_feedback_up"
	slackFeedbackActionDown  = "wuphf_feedback_down"
	slackFeedbackValueUp     = "up"
	slackFeedbackValueDown   = "down"

	// slackPaneDisclaimer is the LLM-output footer Slack's governance guidance asks
	// for. Kept to one subtle context line so the pane stays uncluttered.
	slackPaneDisclaimer = "_The office can make mistakes. Double-check anything important._"

	// slackPaneReplyTextLimit is the practical ceiling for a Block Kit section's
	// mrkdwn text (the hard limit is 3000). A longer reply posts as plain text with
	// no feedback chrome rather than being rejected by Slack.
	slackPaneReplyTextLimit = 2900
)

// slackPaneReplyBlocks renders an office pane reply as Block Kit: the message, a
// quiet LLM disclaimer, and 👍 / 👎 feedback buttons. Returns ok=false when the
// text is empty or too long for a section block, in which case the caller posts
// plain text instead.
func slackPaneReplyBlocks(text string) ([]slack.Block, bool) {
	text = strings.TrimSpace(text)
	if text == "" || len(text) > slackPaneReplyTextLimit {
		return nil, false
	}
	return []slack.Block{
		slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil),
		slack.NewContextBlock("", slack.NewTextBlockObject(slack.MarkdownType, slackPaneDisclaimer, false, false)),
		slack.NewActionBlock(slackFeedbackActionBlock,
			slack.NewButtonBlockElement(slackFeedbackActionUp, slackFeedbackValueUp,
				slack.NewTextBlockObject(slack.PlainTextType, "👍 Helpful", false, false)),
			slack.NewButtonBlockElement(slackFeedbackActionDown, slackFeedbackValueDown,
				slack.NewTextBlockObject(slack.PlainTextType, "👎 Not helpful", false, false)),
		),
	}, true
}

// isAssistantPaneReply reports whether an outbound is a plain office reply in the
// 1:1 Assistant pane — the messages that get feedback chrome. Task work (it lives
// in its own thread) and decision cards (they carry their own buttons) are
// excluded.
func (t *SlackTransport) isAssistantPaneReply(msg transport.Outbound) bool {
	return msg.SourceTaskID == "" &&
		IsDMSlug(msg.Binding.ChannelSlug) &&
		!isSlackDecisionText(msg.Text)
}

// slackPositiveReactions / slackNegativeReactions are the emoji names that count
// as feedback when added to one of the office's pane messages. Other reactions
// are ignored (not every emoji is a verdict).
var slackPositiveReactions = map[string]bool{
	"+1": true, "thumbsup": true, "white_check_mark": true, "heavy_check_mark": true,
	"tada": true, "raised_hands": true, "100": true,
}

var slackNegativeReactions = map[string]bool{
	"-1": true, "thumbsdown": true, "x": true, "no_entry": true, "no_entry_sign": true,
}

// slackFeedbackReaction maps an emoji name to a feedback verdict. ok=false for
// emoji that are not a thumbs-style verdict.
func slackFeedbackReaction(emoji string) (positive, ok bool) {
	emoji = strings.TrimSpace(strings.ToLower(emoji))
	if slackPositiveReactions[emoji] {
		return true, true
	}
	if slackNegativeReactions[emoji] {
		return false, true
	}
	return false, false
}

// handleFeedbackClick records a 👍 / 👎 button press on a pane reply and thanks
// the clicker ephemerally. Always reports handled=true: a click is answered once,
// never retried. Returns false from the matched-but-unhandled path only so the
// caller's loop can keep its single-return discipline.
func (t *SlackTransport) handleFeedbackClick(ctx context.Context, channelID, messageTS, userID, value string) {
	positive := value == slackFeedbackValueUp
	if t.Broker != nil {
		t.Broker.RecordSlackFeedback(positive, "pane_button", channelID+"/"+messageTS, userID)
	}
	thanks := "Thanks for the feedback."
	if !positive {
		thanks = "Thanks — noted. The office will use this to improve."
	}
	t.postGateEphemeral(ctx, channelID, userID, thanks)
}

// handleReactionAdded captures an emoji reaction on one of the office's pane
// messages as feedback. It ignores reactions on other authors' messages, the
// office's own reactions, non-verdict emoji, and reactions outside a bound pane.
func (t *SlackTransport) handleReactionAdded(channelID, itemUser, reactor, emoji, messageTS string) {
	if t.Broker == nil {
		return
	}
	// Only the office's own messages are gradable, and only by someone else.
	if itemUser == "" || t.botUserID == "" || itemUser != t.botUserID {
		return
	}
	if reactor == "" || reactor == t.botUserID {
		return
	}
	// Only inside a bound pane (a DM mapped to an office DM slug).
	slug := t.channelSlugForID(channelID)
	if slug == "" || !IsDMSlug(slug) {
		return
	}
	positive, ok := slackFeedbackReaction(emoji)
	if !ok {
		return
	}
	t.Broker.RecordSlackFeedback(positive, "pane_reaction", channelID+"/"+messageTS, reactor)
}

// slackFeedbackEvent is one captured thumbs verdict on an office reply.
type slackFeedbackEvent struct {
	Positive bool   `json:"positive"`
	Source   string `json:"source"` // "pane_button" | "pane_reaction"
	Ref      string `json:"ref"`    // channel/message it targets
	ByUser   string `json:"by_user"`
}

// slackFeedbackBufferMax bounds the in-memory recent-feedback ring so a noisy
// pane can't grow it without limit.
const slackFeedbackBufferMax = 200

// RecordSlackFeedback records a thumbs verdict on an office reply. It both logs a
// structured, greppable line and keeps a bounded in-memory buffer the UI or the
// learning loop can read — a real, auditable capture point. source is
// "pane_button" or "pane_reaction"; ref is the channel/message it targets;
// byUser is the Slack user id who gave it.
func (b *Broker) RecordSlackFeedback(positive bool, source, ref, byUser string) {
	if b == nil {
		return
	}
	verdict := "positive"
	if !positive {
		verdict = "negative"
	}
	ref = strings.TrimSpace(ref)
	byUser = strings.TrimSpace(byUser)
	log.Printf("[slack] feedback: verdict=%s source=%s ref=%s by=%s", verdict, source, ref, byUser)

	b.mu.Lock()
	defer b.mu.Unlock()
	b.slackFeedback = append(b.slackFeedback, slackFeedbackEvent{
		Positive: positive, Source: source, Ref: ref, ByUser: byUser,
	})
	if over := len(b.slackFeedback) - slackFeedbackBufferMax; over > 0 {
		b.slackFeedback = append(b.slackFeedback[:0], b.slackFeedback[over:]...)
	}
}

// SlackFeedbackEvents returns a copy of the recent captured feedback (most recent
// last), for tests and any UI that wants to surface the signal.
func (b *Broker) SlackFeedbackEvents() []slackFeedbackEvent {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]slackFeedbackEvent(nil), b.slackFeedback...)
}
