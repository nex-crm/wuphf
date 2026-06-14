package team

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/nex-crm/wuphf/internal/packer"
)

// slack_bridge.go implements packer.SlackBridge over the same slackAPI seam used
// by SlackTransport, so the inbound context-packer can deliver a packed
// delegation to a Slack thread without importing slack-go directly. The packer's
// Deliver() owns idempotency, the egress audit, and the final redaction scan;
// this bridge is the thin "put these exact bytes on Slack" step.

// compile-time assertion: SlackBridge must satisfy packer.SlackBridge.
var _ packer.SlackBridge = (*SlackBridge)(nil)

// SlackBridge posts packed delegations to Slack. It is constructed with a
// slackAPI (real or fake) and carries no broker state — the destination travels
// on each PackedDelegation's InjectionRecord.
type SlackBridge struct {
	api slackAPI
}

// NewSlackBridge builds a SlackBridge from a configured bot token. The bot token
// (xoxb-) is sufficient for chat.postMessage; Socket Mode is not needed for the
// outbound delivery path.
func NewSlackBridge(botToken string) *SlackBridge {
	return &SlackBridge{api: &slackClient{api: slack.New(botToken)}}
}

// newSlackBridge is the injectable constructor used by tests.
func newSlackBridge(api slackAPI) *SlackBridge {
	return &SlackBridge{api: api}
}

// Post posts d.MentionText to the delegation's channel/thread (carried on
// d.Injection.ChannelID / d.Injection.ThreadTS) and, when d.ThreadContext is
// non-empty, posts it as a threaded follow-up under the mention. It returns the
// Slack ts of the mention message — the anchor the packer records as proof of
// delivery. The idempotencyKey is forwarded so a future idempotent transport can
// dedupe; the current Web API has no native idempotency token, so the packer's
// sink remains the dedupe authority (it short-circuits a re-Post of a SENT key).
//
// The thread-context follow-up is BEST-EFFORT: if it fails, Post still returns
// (mentionTS, nil). The mention carries the essentials (ask + plan) and is the
// audit anchor; returning an error here would make the packer's Deliver mark the
// whole delegation FAILED, and because a FAILED record does not short-circuit
// idempotency, a retry would DUPLICATE the mention. A dropped thread block is a
// degraded — never an over- — delivery, so it is logged loudly rather than
// surfaced as a failure. (Only the mention failing is a real delivery failure.)
func (b *SlackBridge) Post(ctx context.Context, d packer.PackedDelegation, idempotencyKey string) (string, error) {
	channelID := strings.TrimSpace(d.Injection.ChannelID)
	if channelID == "" {
		return "", fmt.Errorf("slack bridge: empty channel id (idempotency key %q)", idempotencyKey)
	}
	mention := strings.TrimSpace(d.MentionText)
	if mention == "" {
		return "", fmt.Errorf("slack bridge: empty mention text (idempotency key %q)", idempotencyKey)
	}

	mentionCtx, cancelMention := context.WithTimeout(ctx, 30*time.Second)
	defer cancelMention()

	// Post the mention. escapeText=true is deliberate and security-relevant: this
	// is the foreign-facing egress boundary, and the packer redacts SECRETS, not
	// Slack control sequences. Escaping &<> neutralizes injected mrkdwn like
	// <!channel>/<!here> mass-pings and fake <url|text> links that a tainted Ask
	// could otherwise smuggle into the workspace. If the delegation targets an
	// existing thread, anchor the mention to it; otherwise the mention starts a
	// new thread whose ts we reuse for the follow-up.
	mentionOpts := []slack.MsgOption{slack.MsgOptionText(mention, true)}
	if ts := strings.TrimSpace(d.Injection.ThreadTS); ts != "" {
		mentionOpts = append(mentionOpts, slack.MsgOptionTS(ts))
	}
	_, mentionTS, err := b.api.PostMessageContext(mentionCtx, channelID, mentionOpts...)
	if err != nil {
		return "", fmt.Errorf("slack bridge: post mention: %w", err)
	}

	// Thread the remaining context under the mention, when present. Prefer the
	// delegation's own thread anchor; fall back to the mention's ts so the
	// follow-up always lands in the same thread as the mention.
	if thread := strings.TrimSpace(d.ThreadContext); thread != "" {
		threadTS := strings.TrimSpace(d.Injection.ThreadTS)
		if threadTS == "" {
			threadTS = mentionTS
		}
		// Fresh timeout: the mention call above may have consumed most of its
		// budget, and the follow-up is an independent network call that deserves
		// its own deadline rather than the mention's leftovers.
		followCtx, cancelFollow := context.WithTimeout(ctx, 30*time.Second)
		defer cancelFollow()
		_, _, ferr := b.api.PostMessageContext(followCtx, channelID,
			slack.MsgOptionText(thread, true),
			slack.MsgOptionTS(threadTS),
		)
		if ferr != nil {
			log.Printf("[slack-bridge] thread-context follow-up failed (key %q, channel %s): %v — mention delivered, thread block dropped", idempotencyKey, channelID, ferr)
		}
	}

	return mentionTS, nil
}
