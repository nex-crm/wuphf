package team

// slack_integration_cards.go renders a `connect` decision (the action-resolve
// gate raised because work needs an integration that is missing/not-connected)
// as a polished Block Kit card in the Slack thread, mirroring the web's
// ConnectIntegrationCard:
//
//   • a header section with the integration NAME + a one-line "WUPHF needs <X>
//     to do this" ask, the integration LOGO as the section accessory image
//     (image_url from the Composio catalog metadata — never from agent text);
//   • a context block with the connection STATUS (Not connected / Connecting… /
//     Connected) from the connection registry, plus the masked action/verb;
//   • an actions block with a primary "Connect <X>" button and a "Not now"
//     dismiss.
//
// On a Connect click the existing interactive machinery (handleInteractive,
// slack_gate.go) routes to handleConnectClick, which starts the Composio OAuth
// flow server-side and rewrites the card in place to a "Connecting…" state with
// a URL button the human opens to finish OAuth. The existing connect-status
// fan-out (handleIntegrationConnectStatus → fanOutConnected) auto-answers the
// parked connect card the moment the connection goes live, so the card never has
// to poll. A "Not now" click answers the card with the gate's canonical `skip`
// option through the same answer path the generic gate uses.
//
// Every dynamic field is escaped (slackEscape) and the logo url is validated as
// an http(s) URL before it becomes an image_url, so a hostile catalog/registry
// value can neither smuggle Slack control sequences nor point an image at a
// non-web scheme.

import (
	"context"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/slack-go/slack"

	"github.com/nex-crm/wuphf/internal/action"
)

// slackConnectActionBlock is the block_id carried on the connect card's action
// block. It is distinct from slackGateActionBlock so the inbound handler routes a
// connect click to the Composio connect flow (start OAuth + rewrite) rather than
// the generic answer path. handleInteractive recognises both block ids.
const slackConnectActionBlock = "wuphf_connect_options"

// slackConnectOptionConnect / slackConnectOptionSkip are the two button option
// ids on a connect card. "connect" starts the OAuth flow; "skip" answers the
// gate's canonical connect option to abandon the parked action. Both round-trip
// through slackGateValue exactly like a generic gate button.
const (
	slackConnectOptionConnect = "connect"
	slackConnectOptionSkip    = "skip"
)

// isSlackConnectInterview reports whether a humanInterview should render as a
// connect card: a `connect`-kind request anchored to a concrete platform. The
// platform check guards against a malformed connect request with no integration
// to connect (which would render a card with no actionable target).
func isSlackConnectInterview(req humanInterview) bool {
	return normalizeRequestKind(req.Kind) == "connect" && strings.TrimSpace(req.Platform) != ""
}

// slackConnectImageURL returns the catalog logo URL when it is a usable http(s)
// image url, else "". Block Kit images need a hosted URL (no inline SVG), and a
// non-web scheme (data:, javascript:, file:) must never reach image_url — so a
// missing or unsafe logo degrades to no image, never a broken thumbnail.
func slackConnectImageURL(logoURL string) string {
	logoURL = strings.TrimSpace(logoURL)
	if logoURL == "" {
		return ""
	}
	u, err := url.Parse(logoURL)
	if err != nil || u.Host == "" {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return logoURL
	default:
		return ""
	}
}

// slackConnectDisplayName resolves the integration's display name for the card.
// It prefers the request title (minus a leading "Connect "), then the platform's
// canonical display name, never agent free text. The result is the human-facing
// integration name the card headlines.
func slackConnectDisplayName(req humanInterview) string {
	title := strings.TrimSpace(req.Title)
	if len(title) >= len("connect ") && strings.EqualFold(title[:len("connect ")], "connect ") {
		title = strings.TrimSpace(title[len("connect "):])
	}
	if title != "" {
		return title
	}
	if name := action.DisplayPlatformName(req.Platform); name != "" && name != "Unknown" {
		return name
	}
	return "the integration"
}

// formatSlackConnectBlocks renders a connect decision as the Block Kit connect
// card. statusLabel is the live connection status ("Not connected" /
// "Connecting…" / "Connected"); canConnect gates the live Connect button (false
// when Composio is not configured → informational card with guidance, no broken
// button). All dynamic fields are escaped; the logo is validated before use.
func formatSlackConnectBlocks(req humanInterview, statusLabel string, canConnect bool) []slack.Block {
	name := slackConnectDisplayName(req)
	var blocks []slack.Block

	ask := "🔌 *WUPHF needs " + slackEscape(name) + " to do this.*"
	headerText := slack.NewTextBlockObject(slack.MarkdownType, ask, false, false)
	var accessory *slack.Accessory
	if img := slackConnectImageURL(req.LogoURL); img != "" {
		accessory = slack.NewAccessory(slack.NewImageBlockElement(img, name+" logo"))
	}
	blocks = append(blocks, slack.NewSectionBlock(headerText, nil, accessory))

	question := strings.TrimSpace(req.Question)
	if question == "" {
		question = strings.TrimSpace(req.Title)
	}
	if question != "" {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, slackEscape(question), false, false), nil, nil))
	}

	// Context: connection status + the masked action/verb being requested.
	ctxParts := []string{"Status: *" + slackEscape(strings.TrimSpace(statusLabel)) + "*"}
	if summary := slackActionSummary(req.Action); summary != "" {
		ctxParts = append(ctxParts, "Action: "+summary)
	} else if from := strings.TrimSpace(req.From); from != "" {
		ctxParts = append(ctxParts, "Requested by "+slackEscape(from))
	}
	blocks = append(blocks, slack.NewContextBlock("",
		slack.NewTextBlockObject(slack.MarkdownType, strings.Join(ctxParts, "  ·  "), false, false)))

	if !canConnect {
		// Composio is not configured: no live button would resolve, so render the
		// card informationally with a clear next step instead of a dead button.
		blocks = append(blocks, slack.NewContextBlock("",
			slack.NewTextBlockObject(slack.MarkdownType,
				"Connect "+slackEscape(name)+" from the WUPHF web app (Integrations) to continue. Composio is not set up for in-Slack connect yet.", false, false)))
		return blocks
	}

	connectBtn := slack.NewButtonBlockElement(
		slackConnectOptionConnect,
		slackGateValue(req.ID, slackConnectOptionConnect),
		slack.NewTextBlockObject(slack.PlainTextType, "Connect "+name, true, false),
	).WithStyle(slack.StylePrimary)
	skipBtn := slack.NewButtonBlockElement(
		slackConnectOptionSkip,
		slackGateValue(req.ID, slackConnectOptionSkip),
		slack.NewTextBlockObject(slack.PlainTextType, "Not now", true, false),
	)
	blocks = append(blocks, slack.NewActionBlock(slackConnectActionBlock, connectBtn, skipBtn))
	return blocks
}

// formatSlackConnectFallback is the plain-text notification fallback for the
// connect card (the text Slack shows in notifications and accessibility readers).
func formatSlackConnectFallback(req humanInterview) string {
	return "Connect " + slackConnectDisplayName(req) + " to continue"
}

// slackConnectConnectingBlocks rewrites a connect card to its "Connecting…" state
// after the human clicks Connect: the OAuth flow has started, so the card shows a
// status line and (when Composio returned one) a URL button the human opens to
// finish the connection in the browser. The card is no longer a block_actions
// surface — the Connect/skip buttons are gone — because the fan-out auto-answers
// the parked card once the connection goes live.
func slackConnectConnectingBlocks(req humanInterview, authURL string) []slack.Block {
	name := slackConnectDisplayName(req)
	var blocks []slack.Block

	head := "🔌 *Connecting " + slackEscape(name) + "…*"
	headerText := slack.NewTextBlockObject(slack.MarkdownType, head, false, false)
	var accessory *slack.Accessory
	if img := slackConnectImageURL(req.LogoURL); img != "" {
		accessory = slack.NewAccessory(slack.NewImageBlockElement(img, name+" logo"))
	}
	blocks = append(blocks, slack.NewSectionBlock(headerText, nil, accessory))

	blocks = append(blocks, slack.NewContextBlock("",
		slack.NewTextBlockObject(slack.MarkdownType,
			"Finish signing in to "+slackEscape(name)+" in the browser. This resumes automatically once the connection is live.", false, false)))

	// A validated http(s) auth url becomes a Slack URL button: the human clicks it
	// to open the Composio OAuth page. An unusable/empty url degrades to a text-only
	// status (no broken button), and the human can connect from the web app.
	if safe := slackConnectImageURL(authURL); safe != "" {
		open := slack.NewButtonBlockElement(
			slackConnectOptionConnect,
			slackGateValue(req.ID, slackConnectOptionConnect),
			slack.NewTextBlockObject(slack.PlainTextType, "Open "+name+" sign-in", true, false),
		).WithStyle(slack.StylePrimary).WithURL(safe)
		blocks = append(blocks, slack.NewActionBlock(slackConnectActionBlock, open))
	}
	return blocks
}

// handleConnectClick processes a click on a connect card's Connect button: it
// starts the Composio OAuth flow server-side and rewrites the card in place to
// the "Connecting…" state (with the auth-url open button). It reuses the same
// channel-binding + human-only guarantees handleInteractive already applied
// before dispatching here. Best-effort on the Slack side: a failed start surfaces
// an ephemeral to the clicker; a failed rewrite is logged but never re-errors
// (the OAuth flow has already begun).
func (t *SlackTransport) handleConnectClick(ctx context.Context, channelID, messageTS, userID, actorName string, req humanInterview) {
	if t.Broker == nil {
		return
	}
	actorSlug := slackHumanActorSlug(userID, actorName)
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result, err := t.Broker.startSlackIntegrationConnect(cctx, req.Platform, "human:"+actorSlug)
	if err != nil {
		t.postGateEphemeral(ctx, channelID, userID,
			"Could not start the "+slackConnectDisplayName(req)+" connection. Try connecting from the WUPHF web app.")
		return
	}
	t.rewriteConnectCard(ctx, channelID, messageTS, req, result.AuthURL)
}

// rewriteConnectCard updates the connect card in place to its "Connecting…"
// state. Best-effort: a failed chat.update is swallowed by updateConnectMessage's
// caller logging, never fatal (the connection flow already started).
func (t *SlackTransport) rewriteConnectCard(ctx context.Context, channelID, messageTS string, req humanInterview, authURL string) {
	if channelID == "" || messageTS == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	blocks := slackConnectConnectingBlocks(req, authURL)
	_, _, _, _ = t.api.UpdateMessageContext(cctx, channelID, messageTS,
		slack.MsgOptionText("Connecting "+slackConnectDisplayName(req)+"…", false),
		slack.MsgOptionBlocks(blocks...),
	)
}

// updateConnectResolvedMessage rewrites a connect card in place to its terminal
// skipped state after the human clicks "Not now". The buttons are dropped (the
// action block is gone), mirroring updateGateMessage. Best-effort: a failed
// chat.update is logged but never re-errors (the answer already landed).
func (t *SlackTransport) updateConnectResolvedMessage(ctx context.Context, channelID, messageTS string, req humanInterview, byName string) {
	if channelID == "" || messageTS == "" {
		return
	}
	by := strings.TrimSpace(byName)
	if by == "" {
		by = "a teammate"
	}
	text := "🚫 " + slackEscape(slackConnectDisplayName(req)) + " connection skipped by " + slackEscape(by)
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, _, _, err := t.api.UpdateMessageContext(cctx, channelID, messageTS,
		slack.MsgOptionBlocks(slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, text, false, false), nil, nil)),
		slack.MsgOptionText(slackStripMarkdown(text), false),
	); err != nil {
		log.Printf("[slack] connect: update message %s/%s: %v", channelID, messageTS, err)
	}
}

// connectInterviewByID returns the active connect interview with id in the given
// channel, or ok=false. Used by the interactive handler to re-fetch the typed
// connect request (platform, logo, title) a click references, scoped to the
// channel the click came from. Mirrors activeDecisionForChannel's channel-binding
// guarantee but keys on the clicked id so the card and the click cannot drift.
func (t *SlackTransport) connectInterviewByID(channelSlug, interviewID string) (humanInterview, bool) {
	if t.Broker == nil {
		return humanInterview{}, false
	}
	for _, req := range t.Broker.Requests(channelSlug, false) {
		if req.ID == interviewID && isSlackConnectInterview(req) {
			return req, true
		}
	}
	return humanInterview{}, false
}
