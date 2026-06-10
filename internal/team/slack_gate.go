package team

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// slack_gate.go renders the office's human approval/decision requests
// (humanInterview) as Slack Block Kit messages with one button per option, and
// turns a button click back into an answer through the broker's canonical
// answer path (Broker.answerRequestFromActor). This makes the first-egress /
// external-action approval gate — the same data the web ExternalActionApprovalCard
// renders — natively actionable inside a Slack-bridged channel.
//
// Outbound (render): formatSlackInterviewBlocks builds the blocks; the transport's
// Send looks up the active decision for the target channel and posts them.
//
// Inbound (click): handleInteractive parses a block_actions InteractionCallback,
// resolves the clicking Slack user to a human requestActor, answers the interview,
// and rewrites the original message in place to show the resolved decision and drop
// the buttons. The gate is human-only by construction: a human pressed a button in
// Slack, so the actor is always a human.

// slackGateActionBlock is the block_id carried on the approval action block so the
// inbound handler can recognise (and, if needed, ignore) clicks that originate
// from this gate vs any other future interactive surface.
const slackGateActionBlock = "wuphf_interview_options"

// slackGateValueSep separates the interview id from the option id in a button's
// value (e.g. "request-7|approve"). Option ids are normalised to [a-z0-9-_] by
// normalizeRequestOptionID, and interview ids are broker-minted "request-N", so a
// single "|" never collides with either side.
const slackGateValueSep = "|"

// slackGateValue encodes an interview id + option id into a button value. The
// value round-trips through Slack untouched and is the only state the click
// callback needs to answer the right request with the right choice.
func slackGateValue(interviewID, optionID string) string {
	return strings.TrimSpace(interviewID) + slackGateValueSep + strings.TrimSpace(optionID)
}

// parseSlackGateValue inverts slackGateValue. ok is false when the value is not a
// well-formed "<interviewID>|<optionID>" pair (both halves non-empty).
func parseSlackGateValue(value string) (interviewID, optionID string, ok bool) {
	idx := strings.Index(value, slackGateValueSep)
	if idx < 0 {
		return "", "", false
	}
	interviewID = strings.TrimSpace(value[:idx])
	optionID = strings.TrimSpace(value[idx+len(slackGateValueSep):])
	if interviewID == "" || optionID == "" {
		return "", "", false
	}
	return interviewID, optionID, true
}

// formatSlackInterviewBlocks renders a humanInterview as Slack Block Kit: a header
// section with the question and (optional) context, a masked one-line action
// summary when the request carries a structured external action, a warning when
// the connection is unverified, and one button per option. The recommended option
// is styled primary. Every dynamic field is escaped with slackEscape so hostile
// agent-authored text cannot smuggle Slack control sequences into the rendered
// card.
func formatSlackInterviewBlocks(req humanInterview) []slack.Block {
	var blocks []slack.Block

	header := "📋 *Decision needed*"
	if from := strings.TrimSpace(req.From); from != "" {
		header += " from @" + slackEscape(from)
	}
	blocks = append(blocks, slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, header, false, false), nil, nil))

	question := strings.TrimSpace(req.Question)
	if question == "" {
		question = strings.TrimSpace(req.Title)
	}
	if question != "" {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, slackEscape(question), false, false), nil, nil))
	}

	if ctx := strings.TrimSpace(req.Context); ctx != "" {
		blocks = append(blocks, slack.NewContextBlock("",
			slack.NewTextBlockObject(slack.MarkdownType, slackEscape(ctx), false, false)))
	}

	if summary := slackActionSummary(req.Action); summary != "" {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "🔌 *Action*: "+summary, false, false), nil, nil))
	}

	if req.ConnectionUnverified {
		blocks = append(blocks, slack.NewContextBlock("",
			slack.NewTextBlockObject(slack.MarkdownType,
				"⚠️ Connection unverified — the office could not confirm this integration is connected.", false, false)))
	}

	if buttons := slackInterviewButtons(req); len(buttons) > 0 {
		blocks = append(blocks, slack.NewActionBlock(slackGateActionBlock, buttons...))
	}

	return blocks
}

// slackInterviewButtons builds one button per option, capped at the Slack action
// block limit (5 elements per action block). Buttons beyond the cap are dropped
// rather than split across blocks: the gate's canonical kinds (approval/connect/
// fallback/confirm/choice) all define ≤5 options, so the cap only ever bites a
// malformed request. The recommended option is styled primary; a "reject"/"skip"
// option is styled danger so the destructive choice reads as such.
func slackInterviewButtons(req humanInterview) []slack.BlockElement {
	const maxButtons = 5
	var buttons []slack.BlockElement
	for _, opt := range req.Options {
		id := strings.TrimSpace(opt.ID)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			label = id
		}
		btn := slack.NewButtonBlockElement(
			id,
			slackGateValue(req.ID, id),
			slack.NewTextBlockObject(slack.PlainTextType, slackEscape(label), true, false),
		)
		switch {
		case id == strings.TrimSpace(req.RecommendedID):
			btn = btn.WithStyle(slack.StylePrimary)
		case id == "reject" || id == "skip" || strings.HasPrefix(id, "reject"):
			btn = btn.WithStyle(slack.StyleDanger)
		}
		buttons = append(buttons, btn)
		if len(buttons) >= maxButtons {
			break
		}
	}
	return buttons
}

// slackActionSummary renders a one-line, already-masked summary of a structured
// external action for the approval card. It reads only the typed, non-secret
// fields (verb/name/platform + the envelope method/url); the broker re-masks the
// envelope on store (sanitizeApprovalActionPayload), so no secret reaches here.
// Returns "" when there is no action to summarise.
func slackActionSummary(action *approvalActionPayload) string {
	if action == nil {
		return ""
	}
	label := strings.TrimSpace(action.Name)
	if label == "" {
		label = strings.TrimSpace(action.Verb)
	}
	if label == "" {
		label = strings.TrimSpace(action.ActionID)
	}
	platform := strings.TrimSpace(action.Platform)

	var parts []string
	if label != "" {
		parts = append(parts, slackEscape(label))
	}
	if platform != "" {
		parts = append(parts, "on "+slackEscape(platform))
	}
	if env := action.RawEnvelope; env != nil {
		method := strings.TrimSpace(env.Method)
		url := strings.TrimSpace(env.URL)
		switch {
		case method != "" && url != "":
			parts = append(parts, "("+slackEscape(strings.ToUpper(method))+" "+slackEscape(url)+")")
		case url != "":
			parts = append(parts, "("+slackEscape(url)+")")
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// activeDecisionForChannel returns the most-recently-created active human
// decision for the given office channel slug, or ok=false when none is pending.
// "Most recent" matches the message the office just posted: a decision message
// and its underlying request are created together, so the newest active decision
// in the channel is the one the outbound message is rendering. Requests with no
// options (nothing to click) are skipped so Send falls back to plain text.
func (t *SlackTransport) activeDecisionForChannel(slug string) (humanInterview, bool) {
	if t.Broker == nil {
		return humanInterview{}, false
	}
	reqs := t.Broker.Requests(slug, false)
	var best humanInterview
	var found bool
	for _, req := range reqs {
		if !isHumanDecisionKind(req.Kind) || len(req.Options) == 0 {
			continue
		}
		if !found || req.CreatedAt > best.CreatedAt {
			best = req
			found = true
		}
	}
	return best, found
}

// handleInteractive processes one block_actions interaction: it resolves the
// clicking Slack user to a human actor, answers the matching interview through
// the broker's canonical answer path, and on success rewrites the original Slack
// message to show the resolved decision (dropping the buttons). It always reports
// true so the socket runner Acks the interaction — a failed answer is surfaced to
// the clicker via an ephemeral message, never via a Slack retry (retrying a click
// would just re-hit the same terminal/invalid state).
func (t *SlackTransport) handleInteractive(ctx context.Context, callback slack.InteractionCallback) bool {
	if callback.Type != slack.InteractionTypeBlockActions {
		return true
	}
	actions := callback.ActionCallback.BlockActions
	if len(actions) == 0 {
		return true
	}

	channelID := callback.Channel.ID
	messageTS := callback.Message.Timestamp
	if messageTS == "" {
		messageTS = callback.MessageTs
	}

	for _, action := range actions {
		if action == nil || action.BlockID != slackGateActionBlock {
			continue
		}
		interviewID, optionID, ok := parseSlackGateValue(action.Value)
		if !ok {
			log.Printf("[slack] gate: malformed action value %q", action.Value)
			t.postGateEphemeral(ctx, channelID, callback.User.ID, "Sorry — that button is no longer valid.")
			continue
		}

		// The gate is human-only. Reject the bot's own id and any confirmed
		// bot/app user so a non-human is never recorded as the approving actor.
		name, human := t.resolveUser(ctx, callback.User.ID)
		if callback.User.ID == "" || callback.User.ID == t.botUserID || !human {
			log.Printf("[slack] gate: rejecting non-human approval actor %q", callback.User.ID)
			t.postGateEphemeral(ctx, channelID, callback.User.ID, "Only a human can approve this request.")
			return true
		}

		// Bind the click to its channel: the interview being answered must be the
		// ACTIVE decision in the channel the click came from. This stops a crafted
		// or replayed interaction from resolving another channel's gate by id
		// (request ids are predictable), independent of option validation.
		officeSlug := t.channelSlugForID(channelID)
		decision, found := t.activeDecisionForChannel(officeSlug)
		if officeSlug == "" || !found || decision.ID != interviewID {
			t.postGateEphemeral(ctx, channelID, callback.User.ID, "This decision is no longer available in this channel.")
			return true
		}

		actorSlug := slackHumanActorSlug(callback.User.ID, name)
		answerActor := humanMessageSender(actorSlug)

		status, msg := t.Broker.answerRequestFromActor(answerActor, interviewID, optionID, "", "")
		if status != http.StatusOK {
			reason := strings.TrimSpace(msg)
			if reason == "" {
				reason = "could not record your answer"
			}
			log.Printf("[slack] gate: answer %s/%s failed (%d): %s", interviewID, optionID, status, reason)
			t.postGateEphemeral(ctx, channelID, callback.User.ID,
				"This request is no longer active: "+reason+".")
			return true
		}

		t.updateGateMessage(ctx, channelID, messageTS, optionID, name)
		return true
	}
	return true
}

// updateGateMessage rewrites the original Block Kit message in place to show the
// resolved decision and remove the buttons. Best-effort: a failed chat.update is
// logged but never fails the interaction (the answer already landed).
func (t *SlackTransport) updateGateMessage(ctx context.Context, channelID, messageTS, optionID, byName string) {
	if channelID == "" || messageTS == "" {
		return
	}
	resolved := slackGateResolutionText(optionID, byName)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, _, _, err := t.api.UpdateMessageContext(ctx, channelID, messageTS,
		slack.MsgOptionBlocks(slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, resolved, false, false), nil, nil)),
		slack.MsgOptionText(slackStripMarkdown(resolved), false),
	); err != nil {
		log.Printf("[slack] gate: update message %s/%s: %v", channelID, messageTS, err)
	}
}

// postGateEphemeral sends a transient, only-visible-to-the-clicker notice. Used
// when a click cannot be honoured (expired/unknown/terminal request). Best-effort.
func (t *SlackTransport) postGateEphemeral(ctx context.Context, channelID, userID, text string) {
	if channelID == "" || userID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// escapeText=true: the text can include request-derived reasons (e.g. an
	// option's TextHint), so neutralize any injected Slack control sequences.
	if _, err := t.api.PostEphemeralContext(ctx, channelID, userID, slack.MsgOptionText(text, true)); err != nil {
		log.Printf("[slack] gate: ephemeral to %s in %s: %v", userID, channelID, err)
	}
}

// slackGateResolutionText renders the "resolved" headline a decision message is
// rewritten to after a click. A reject/skip choice reads as a stop; everything
// else reads as an approval/answer. byName is the friendly clicker name.
func slackGateResolutionText(optionID, byName string) string {
	by := strings.TrimSpace(byName)
	if by == "" {
		by = "a teammate"
	}
	by = slackEscape(by)
	id := strings.TrimSpace(optionID)
	if id == "reject" || id == "skip" || strings.HasPrefix(id, "reject") {
		return fmt.Sprintf("🚫 Rejected by @%s", by)
	}
	return fmt.Sprintf("✅ Approved by @%s", by)
}

// slackHumanActorSlug derives a stable human actor slug for the answer path from a
// Slack user. It prefers the resolved display name (so audit entries read
// "human:mira"), falling back to the Slack user id so the actor is never empty.
func slackHumanActorSlug(userID, name string) string {
	if slug := normalizeHumanSessionSlug(name); slug != "" {
		return slug
	}
	if slug := normalizeHumanSessionSlug(userID); slug != "" {
		return slug
	}
	return "team-member"
}

// slackStripMarkdown reduces a small mrkdwn string to plain text for the
// notification/fallback text of an updated message. It only strips the markers
// this gate emits (*bold*); it is not a general mrkdwn parser.
func slackStripMarkdown(s string) string {
	return strings.ReplaceAll(s, "*", "")
}
