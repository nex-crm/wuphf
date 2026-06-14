package team

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/slackutilsx"
	"github.com/slack-go/slack/socketmode"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// slack_transport.go bridges a Slack workspace with the office broker. It mirrors
// the shape of telegram.go: a channel-id → office-slug map (built from the
// broker's "slack" surface channels), a user-id → member-slug map, cached health
// fields under a mutex, and the same transport.Transport method set.
//
// Inbound uses Socket Mode (no public URL required): an app-level "xapp-" token
// opens a WebSocket; message events are routed to the office via the Host
// contract (UpsertParticipant then ReceiveMessage). Outbound uses the Web API
// (chat.postMessage) via the bot "xoxb-" token.
//
// All Web API calls go through the slackAPI seam so the transport (and the
// SlackBridge in slack_bridge.go) are unit-testable against a fake with no
// network. The Socket Mode event loop is kept thin and lives behind runSocketMode
// so the routing logic (routeInbound) can be tested directly.

// slackAPI is the narrow Web API surface this package depends on. The real
// implementation wraps *slack.Client; tests supply a fake. Keeping it small
// (the four calls actually used) follows "accept interfaces, return structs".
type slackAPI interface {
	// PostMessageContext posts a message and returns (channelID, messageTS, err).
	PostMessageContext(ctx context.Context, channelID string, opts ...slack.MsgOption) (string, string, error)
	// GetUserInfoContext resolves a Slack user id to its profile (for display
	// name + IsBot classification).
	GetUserInfoContext(ctx context.Context, userID string) (*slack.User, error)
	// AuthTestContext identifies the bot's own user id so inbound self-messages
	// can be dropped.
	AuthTestContext(ctx context.Context) (*slack.AuthTestResponse, error)
	// GetUsersInConversationContext lists the member ids of a channel, used to
	// pre-warm the user → member-slug map.
	GetUsersInConversationContext(ctx context.Context, params *slack.GetUsersInConversationParameters) ([]string, string, error)
	// UpdateMessageContext edits a previously-posted message in place (chat.update),
	// used by the approval gate to rewrite a decision card to its resolved state.
	UpdateMessageContext(ctx context.Context, channelID, timestamp string, opts ...slack.MsgOption) (string, string, string, error)
	// PostEphemeralContext posts a message visible only to one user (chat.postEphemeral),
	// used by the approval gate to tell a clicker that a request is no longer active.
	PostEphemeralContext(ctx context.Context, channelID, userID string, opts ...slack.MsgOption) (string, error)
	// PublishViewContext publishes a user's App Home tab view (views.publish),
	// used to render the office overview when the Home tab is opened.
	PublishViewContext(ctx context.Context, userID string, view slack.HomeTabViewRequest) error
	// AddPinContext pins a message in a channel (pins.add), used to keep each
	// ongoing task's lifecycle card surfaced. Requires the pins:write scope.
	AddPinContext(ctx context.Context, channelID string, item slack.ItemRef) error
	// RemovePinContext unpins a message (pins.remove), used when a task's
	// lifecycle card reaches a terminal state.
	RemovePinContext(ctx context.Context, channelID string, item slack.ItemRef) error
}

// slackUserInfo is the cached resolution of a Slack user id.
type slackUserInfo struct {
	name  string
	human bool
	// Profile fields captured for the entity-wiki pass (users:read scope):
	// who this person/bot is, in their own workspace's words.
	realName string
	title    string
	tz       string
}

// slackClient is the real slackAPI backed by *slack.Client.
type slackClient struct {
	api *slack.Client
}

func (c *slackClient) PostMessageContext(ctx context.Context, channelID string, opts ...slack.MsgOption) (string, string, error) {
	return c.api.PostMessageContext(ctx, channelID, opts...)
}

func (c *slackClient) GetUserInfoContext(ctx context.Context, userID string) (*slack.User, error) {
	return c.api.GetUserInfoContext(ctx, userID)
}

func (c *slackClient) AuthTestContext(ctx context.Context) (*slack.AuthTestResponse, error) {
	return c.api.AuthTestContext(ctx)
}

func (c *slackClient) GetUsersInConversationContext(ctx context.Context, params *slack.GetUsersInConversationParameters) ([]string, string, error) {
	return c.api.GetUsersInConversationContext(ctx, params)
}

func (c *slackClient) UpdateMessageContext(ctx context.Context, channelID, timestamp string, opts ...slack.MsgOption) (string, string, string, error) {
	return c.api.UpdateMessageContext(ctx, channelID, timestamp, opts...)
}

func (c *slackClient) PostEphemeralContext(ctx context.Context, channelID, userID string, opts ...slack.MsgOption) (string, error) {
	return c.api.PostEphemeralContext(ctx, channelID, userID, opts...)
}

func (c *slackClient) PublishViewContext(ctx context.Context, userID string, view slack.HomeTabViewRequest) error {
	_, err := c.api.PublishViewContext(ctx, slack.PublishViewContextRequest{UserID: userID, View: view})
	return err
}

func (c *slackClient) AddPinContext(ctx context.Context, channelID string, item slack.ItemRef) error {
	return c.api.AddPinContext(ctx, channelID, item)
}

func (c *slackClient) RemovePinContext(ctx context.Context, channelID string, item slack.ItemRef) error {
	return c.api.RemovePinContext(ctx, channelID, item)
}

// socketRunner is the inbound seam: it blocks reading Socket Mode events and
// hands each to handle until ctx is cancelled. handle returns whether the event
// should be Ack'd; returning false (a Host write failed) leaves the event
// un-Ack'd so Slack redelivers it. The real implementation wraps
// *socketmode.Client; tests drive routeInbound/handleEvent directly and do not
// need a fake socket connection.
type socketRunner interface {
	Run(ctx context.Context, handle func(socketmode.Event) bool) error
}

// socketModeRunner is the real socketRunner over *socketmode.Client.
type socketModeRunner struct {
	client *socketmode.Client
}

// socketEventNeedsAck reports whether a Socket Mode envelope expects an Ack.
// Only the request-bearing payload types do; connection-lifecycle events
// (connecting/connected/hello/disconnect/incoming_error) must NOT be Acked —
// acking them makes Slack drop the connection.
func socketEventNeedsAck(t socketmode.EventType) bool {
	switch t {
	case socketmode.EventTypeEventsAPI,
		socketmode.EventTypeInteractive,
		socketmode.EventTypeSlashCommand:
		return true
	default:
		return false
	}
}

// shouldAckEvent is the full Ack decision the socket loop makes for one event:
// Ack only when the event carries a request envelope, the handler reports it was
// handled, AND the type is one Slack expects an Ack for. Extracted as a pure
// function so the loop's Ack behavior is testable without a live WebSocket — the
// gap that let a "hello" Ack (which drops the connection) ship undetected.
func shouldAckEvent(evt socketmode.Event, handled bool) bool {
	return evt.Request != nil && handled && socketEventNeedsAck(evt.Type)
}

// Run starts the Socket Mode WebSocket loop in a sibling goroutine, drains the
// Events channel and dispatches each event to handle, and blocks until ctx is
// cancelled or RunContext returns. RunContext owns reconnection internally; a
// returned error means the connection gave up and the caller (Run) should
// surface it for supervised restart.
func (r *socketModeRunner) Run(ctx context.Context, handle func(socketmode.Event) bool) error {
	errCh := make(chan error, 1)
	go func() { errCh <- r.client.RunContext(ctx) }()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case evt, ok := <-r.client.Events:
			if !ok {
				// Events closed: wait for RunContext to report why.
				select {
				case <-ctx.Done():
					return ctx.Err()
				case err := <-errCh:
					return err
				}
			}
			// Ack AFTER handling and only when handle reports success. A failed
			// Host write returns false → no Ack → Slack redelivers (the broker
			// dedupes by ExternalID = the message ts). This makes inbound
			// at-least-once instead of silently at-most-once.
			//
			// Only Ack the envelope types Slack expects an ack for (events_api,
			// interactive, slash_command). Acking a connection-lifecycle envelope
			// like "hello" makes Slack drop the connection — that caused a ~10s
			// reconnect loop where no event ever landed.
			if shouldAckEvent(evt, handle(evt)) {
				_ = r.client.Ack(*evt.Request)
			}
		}
	}
}

// compile-time assertion: SlackTransport must satisfy transport.Transport.
var _ transport.Transport = (*SlackTransport)(nil)

// SlackTransport bridges Slack channels with the office broker. Each mapped
// Slack channel corresponds to an office channel with a "slack" surface.
type SlackTransport struct {
	BotToken string
	AppToken string
	Broker   *Broker
	// ChannelMap maps slack channel id (e.g. "C0123") -> office channel slug.
	ChannelMap map[string]string
	// UserMap maps slack user id (e.g. "U0123") -> resolved identity (display
	// name + humanity). Populated lazily from users.info on first contact and
	// eagerly from conversation membership at startup. Misses fall back to the
	// raw user id. Caching humanity (not just the name) means a warmed bot/app
	// user stays classified non-human even if a later message lacks bot_id.
	UserMap map[string]slackUserInfo

	api    slackAPI
	socket socketRunner

	// botUserID is this bot's own Slack user id, resolved via auth.test at the
	// start of Run. Inbound messages from this id are dropped (no echo loops).
	botUserID string
	// botUserName is this bot's own Slack display name (auth.test "user").
	// Outbound rendering uses it as the coordinator's public voice: internal
	// office agents never present as separate Slack actors — WUPHF is one bot
	// coordinating, so references to the office lead render as the bot name.
	botUserName string

	// mapsMu protects ChannelMap and UserMap against concurrent reads from Send /
	// FormatOutbound and writes from routeInbound (learning new users).
	mapsMu sync.RWMutex

	// spawnedClients caches per-spawned-agent Web API clients keyed by office
	// slug — written once per slug, read on every Send. See
	// slack_spawned_agents.go for the lookup and construction.
	spawnedClients sync.Map

	// cardLocks serializes thread-root card posting per task id, so a Send
	// (first task message) and the 15s card-sync loop can't both post a root
	// card for the same task. Keyed by task id → *sync.Mutex.
	cardLocks sync.Map

	// health fields — written by the inbound loop, read by Health(). Protected by mu.
	mu            sync.Mutex
	healthState   transport.HealthState
	lastSuccessAt time.Time
	lastErr       error
}

// NewSlackTransport creates a transport from the broker's "slack" surface
// channels, wiring the real Web API and Socket Mode clients from the configured
// tokens. The app token enables the Socket Mode inbound half; the bot token
// drives the Web API outbound half.
func NewSlackTransport(broker *Broker, botToken, appToken string) *SlackTransport {
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))
	t := newSlackTransport(broker, botToken, appToken, &slackClient{api: api})
	if appToken != "" {
		t.socket = &socketModeRunner{client: socketmode.New(api)}
	}
	return t
}

// newSlackTransport is the injectable constructor used by tests: it builds the
// channel map from the broker's surface channels and accepts a fake slackAPI.
// The socket runner is left nil (tests exercise routeInbound directly).
func newSlackTransport(broker *Broker, botToken, appToken string, api slackAPI) *SlackTransport {
	channelMap := make(map[string]string)
	if broker != nil {
		for _, ch := range broker.SurfaceChannels("slack") {
			if ch.Surface == nil || ch.Surface.RemoteID == "" {
				continue
			}
			channelMap[ch.Surface.RemoteID] = ch.Slug
		}
	}
	return &SlackTransport{
		BotToken:    botToken,
		AppToken:    appToken,
		Broker:      broker,
		ChannelMap:  channelMap,
		UserMap:     make(map[string]slackUserInfo),
		api:         api,
		healthState: transport.HealthDisconnected,
	}
}

// refreshChannelMap merges the broker's current "slack" surface channels into the
// live ChannelMap so a channel connected after the transport started begins
// bridging without a restart. Socket Mode already receives every workspace event;
// inbound routing (routeInbound) filters by ChannelMap and outbound Send looks up
// the same map, so adding an entry is sufficient for both directions. Existing
// entries are preserved. The surface snapshot is taken before mapsMu is held so
// the broker lock (SurfaceChannels → b.mu) is never acquired under mapsMu.
func (t *SlackTransport) refreshChannelMap() {
	if t.Broker == nil {
		return
	}
	surfaces := t.Broker.SurfaceChannels("slack")
	t.mapsMu.Lock()
	defer t.mapsMu.Unlock()
	for _, ch := range surfaces {
		if ch.Surface == nil || ch.Surface.RemoteID == "" {
			continue
		}
		t.ChannelMap[ch.Surface.RemoteID] = ch.Slug
	}
}

// Name returns "slack" — the stable adapter name used as AdapterName in every
// Participant value this transport creates.
func (t *SlackTransport) Name() string { return "slack" }

// Binding returns an empty binding because a single SlackTransport instance
// covers multiple channels via ChannelMap. The per-message channel is carried in
// each transport.Message.Binding constructed by routeInbound.
func (t *SlackTransport) Binding() transport.Binding {
	return transport.Binding{}
}

// Health returns a point-in-time snapshot of adapter connectivity. O(1): reads
// cached fields updated by the inbound loop.
func (t *SlackTransport) Health() transport.Health {
	t.mu.Lock()
	defer t.mu.Unlock()
	return transport.Health{
		State:         t.healthState,
		LastSuccessAt: t.lastSuccessAt,
		LastError:     t.lastErr,
	}
}

// Run starts the bidirectional bridge and blocks until ctx is cancelled.
// Inbound Slack events are delivered to the office via host; outbound delivery
// is driven by the Host-side dispatcher started alongside this Run in
// launcher_transports.go (which calls FormatOutbound + Send). Implements
// transport.Transport.
func (t *SlackTransport) Run(ctx context.Context, host transport.Host) error {
	if t.BotToken == "" {
		return fmt.Errorf("slack bot token is empty")
	}
	if t.AppToken == "" {
		return fmt.Errorf("slack app token is empty (Socket Mode requires an xapp- token)")
	}
	if t.socket == nil {
		return fmt.Errorf("slack: socket runner not configured")
	}
	if len(t.ChannelMap) == 0 {
		return fmt.Errorf("no slack channels configured")
	}

	// Resolve our own bot user id (with a short retry) so we can drop
	// self-authored messages. If it never resolves we still start: the bot_id /
	// subtype guards in routeInbound are the primary self/bot drop, and this id
	// is a belt-and-suspenders third check.
	t.botUserID, t.botUserName = t.resolveBotIdentity(ctx)
	if t.botUserID == "" {
		log.Printf("[slack] auth.test failed after retries — self-echo guard relies on bot_id/subtype only")
	}

	// Pre-warm the user map from each mapped channel's membership. Best-effort:
	// a failure here only means names resolve lazily on first message.
	t.warmUserMap(ctx)

	t.setHealth(transport.HealthConnected, nil)

	// Run the Socket Mode loop in a sibling goroutine and select on ctx so a
	// context cancellation returns nil (intentional shutdown — the Host must not
	// reconnect) while a genuine connection failure surfaces for supervised
	// restart. This mirrors telegram.go's Run/select shape.
	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- t.socket.Run(ctx2, func(evt socketmode.Event) bool {
			return t.handleEvent(ctx2, host, evt)
		})
	}()

	select {
	case <-ctx.Done():
		// Stop the socket loop and wait (bounded) for it to actually return, so
		// the Host is not used after Run returns and the launcher proceeds with
		// shutdown.
		cancel()
		select {
		case <-errCh:
		case <-time.After(5 * time.Second):
			log.Printf("[slack] socket loop did not stop within 5s of cancellation")
		}
		return nil
	case err := <-errCh:
		cancel()
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

// resolveBotIdentity calls auth.test with a short bounded retry, returning the
// bot's own Slack user id and display name, or empties if it never resolves.
func (t *SlackTransport) resolveBotIdentity(ctx context.Context) (userID, userName string) {
	for attempt := 0; attempt < 3; attempt++ {
		if auth, err := t.api.AuthTestContext(ctx); err == nil && auth != nil && auth.UserID != "" {
			return auth.UserID, strings.TrimSpace(auth.User)
		}
		select {
		case <-ctx.Done():
			return "", ""
		case <-time.After(time.Duration(attempt+1) * 200 * time.Millisecond):
		}
	}
	return "", ""
}

// Start is a compatibility shim for callers that predate the transport.Transport
// contract. It creates a brokerTransportHost and delegates to Run.
func (t *SlackTransport) Start(ctx context.Context) error {
	host := &brokerTransportHost{broker: t.Broker}
	return t.Run(ctx, host)
}

// handleEvent dispatches one Socket Mode event and reports whether it should be
// Ack'd. Only EventsAPI message events are routed inbound; connection lifecycle
// events update health. It returns false ONLY when a routable message failed to
// reach the Host (a transient broker error) so the event is left un-Ack'd and
// Slack redelivers it; everything else (handled, ignored, or non-routable) is
// Ack'd.
func (t *SlackTransport) handleEvent(ctx context.Context, host transport.Host, evt socketmode.Event) bool {
	switch evt.Type {
	case socketmode.EventTypeConnected, socketmode.EventTypeHello:
		t.setHealth(transport.HealthConnected, nil)
	case socketmode.EventTypeConnectionError, socketmode.EventTypeInvalidAuth, socketmode.EventTypeIncomingError:
		t.setHealth(transport.HealthDegraded, fmt.Errorf("slack socket event: %s", evt.Type))
	case socketmode.EventTypeEventsAPI:
		apiEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
		if !ok {
			return true
		}
		if home, isHome := apiEvent.InnerEvent.Data.(*slackevents.AppHomeOpenedEvent); isHome {
			// Best-effort render of the office overview; always Ack'd.
			t.publishHomeTab(ctx, home.User)
			return true
		}
		msg, ok := apiEvent.InnerEvent.Data.(*slackevents.MessageEvent)
		if !ok {
			return true
		}
		if err := t.routeInbound(ctx, host, msg); err != nil {
			t.setHealth(transport.HealthDegraded, err)
			log.Printf("[slack] route inbound error (leaving un-acked for redelivery): %v", err)
			return false
		}
	case socketmode.EventTypeInteractive:
		// Block Kit interactions (a human clicking an approval/decision button).
		// handleInteractive always reports handled=true: a click is answered
		// once and any failure is surfaced to the clicker ephemerally, never
		// retried, so the interaction is always Ack'd.
		callback, ok := evt.Data.(slack.InteractionCallback)
		if !ok {
			return true
		}
		return t.handleInteractive(ctx, callback)
	}
	return true
}

// routeInbound resolves the office channel for a Slack message event and delivers
// it to the office via host.UpsertParticipant + host.ReceiveMessage. Subtyped
// events (edits/joins/etc.), the bot's own messages, and messages on unmapped
// channels are skipped. Bot-authored messages are dropped UNLESS the author's
// Slack user id is registered as a foreign agent via /slack/agents — those flow
// inbound attributed to the registered office slug as non-human participants,
// which is the ingress half of multi-agent coordination (the egress half is the
// packer's @-mention delegation). Returns a non-nil error only on a Host
// contract failure (e.g. ErrBindingChannelMissing), matching telegram's
// routeInbound so the caller can surface it for supervised restart.
func (t *SlackTransport) routeInbound(ctx context.Context, host transport.Host, msg *slackevents.MessageEvent) error {
	if msg == nil {
		return nil
	}
	// Skip non-plain messages: edits, deletes, joins all carry a SubType. The
	// one exception is "bot_message" — that is how some foreign bots' posts
	// arrive, so it must reach the registry check below instead of dropping.
	if msg.SubType != "" && msg.SubType != "bot_message" {
		return nil
	}
	// Drop our own posts to avoid echo loops with the office's outbound relay.
	if t.botUserID != "" && msg.User == t.botUserID {
		return nil
	}
	// Echo guard for SPAWNED office agents (broker_slack_spawn.go): their
	// Slack posts are office-originated (Send posts them with the agent's own
	// token), so they must never re-ingress as new inbound. Foreign agents
	// (ingress allowed) resolve via foreignAgentSlug below instead.
	if t.Broker != nil && t.Broker.IsSpawnedSlackAgentUserID(msg.User) {
		return nil
	}
	if strings.TrimSpace(msg.Text) == "" {
		return nil
	}

	t.mapsMu.RLock()
	channel, ok := t.ChannelMap[msg.Channel]
	t.mapsMu.RUnlock()
	if !ok {
		log.Printf("[slack] inbound: unmapped channel %s", msg.Channel)
		return nil
	}

	// Resolve the sender. A registered foreign agent is attributed to its
	// office slug and marked non-human; every OTHER bot-authored message —
	// unregistered bot users, legacy bot_message posts without a user id, and
	// our own posts when auth.test never resolved botUserID — drops here.
	// Registration is the ingress allowlist: fail-closed by default.
	var fromName string
	var human bool
	if agentSlug := t.foreignAgentSlug(msg.User); agentSlug != "" {
		fromName, human = agentSlug, false
	} else if msg.BotID != "" || msg.SubType == "bot_message" || msg.User == "" {
		return nil
	} else {
		fromName, human = t.resolveUser(ctx, msg.User)
		if !human {
			// resolveUser classified this user as a bot (users.info IsBot) but
			// it is not a registered foreign agent — fail closed, exactly like
			// the BotID/subtype guard above. A bot whose message happens to
			// arrive in plain-user shape must not slip past the /slack/agents
			// allowlist. Only humans and allowlisted agents ingress.
			return nil
		}
	}

	p := transport.Participant{
		AdapterName: "slack",
		Key:         msg.User,
		DisplayName: fromName,
		Human:       human,
	}
	b := transport.Binding{
		Scope:       transport.ScopeChannel,
		ChannelSlug: channel,
	}

	if err := host.UpsertParticipant(ctx, p, b); err != nil {
		return fmt.Errorf("slack upsert participant: %w", err)
	}
	if err := host.ReceiveMessage(ctx, transport.Message{
		Participant: p,
		Binding:     b,
		Text:        t.translateInboundMentions(ctx, msg.Text),
		ExternalID:  msg.TimeStamp,
		ThreadKey:   msg.ThreadTimeStamp,
	}); err != nil {
		return fmt.Errorf("slack receive message: %w", err)
	}
	t.setHealth(transport.HealthConnected, nil)
	return nil
}

// slackInboundMentionRE matches Slack's wire mention syntax <@U…> / <@W…>
// (optionally with a |label suffix).
var slackInboundMentionRE = regexp.MustCompile(`<@([UW][A-Z0-9]+)(?:\|[^>]*)?>`)

// translateInboundMentions rewrites Slack wire mentions into office tokens so
// the broker's mention machinery understands who is addressed: a mention of
// OUR bot becomes "@<lead>" (tagging the wuphf bot is how a Slack human talks
// to the office — without this the lead never wakes); a registered foreign
// agent becomes "@<slug>"; any other user becomes its cached display name
// (people are referenced, not office actors).
func (t *SlackTransport) translateInboundMentions(ctx context.Context, text string) string {
	if !strings.Contains(text, "<@") {
		return text
	}
	return slackInboundMentionRE.ReplaceAllStringFunc(text, func(m string) string {
		sub := slackInboundMentionRE.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		userID := sub[1]
		if t.botUserID != "" && userID == t.botUserID && t.Broker != nil {
			if lead := t.Broker.OfficeLeadSlug(); lead != "" {
				return "@" + lead
			}
		}
		if t.Broker != nil {
			if slug := t.Broker.SlackAgentSlugByUserID(userID); slug != "" {
				return "@" + slug
			}
		}
		name, _ := t.resolveUser(ctx, userID)
		return name
	})
}

// foreignAgentSlug resolves a Slack user id to a registered foreign agent's
// office slug via the /slack/agents registry. Returns "" for empty ids,
// unregistered ids, and — as an echo guard even against a misconfigured
// registration of our own bot — the transport's own bot user id.
func (t *SlackTransport) foreignAgentSlug(userID string) string {
	if userID == "" || t.Broker == nil {
		return ""
	}
	if t.botUserID != "" && userID == t.botUserID {
		return ""
	}
	return t.Broker.SlackAgentSlugByUserID(userID)
}

// Send delivers one outbound message to the Slack channel mapped to
// msg.Binding.ChannelSlug via chat.postMessage. Returns an error if no channel is
// mapped for that slug or if the Slack API call fails. Implements
// transport.Transport. A ThreadKey, when present, threads the reply.
func (t *SlackTransport) Send(ctx context.Context, msg transport.Outbound) error {
	channelID := t.channelIDForSlug(msg.Binding.ChannelSlug)
	if channelID == "" {
		return fmt.Errorf("slack: no channel mapped for %q", msg.Binding.ChannelSlug)
	}
	// Post with the spawned agent's own client when the outbound carries a
	// spawned-sender participant (slack_spawned_agents.go), so the message
	// appears in Slack as that agent; the main bot client otherwise.
	api := t.postClientFor(msg.Participant)
	opts := []slack.MsgOption{slack.MsgOptionText(msg.Text, false)}
	// A decision/interview message renders as an interactive Block Kit card with
	// one button per option, so a human can approve/reject natively in Slack.
	// transport.Outbound is text-only (shared kernel type, not Slack-specific), so
	// the decision is re-derived here: only an outbound that formatSlackOutbound
	// already rendered as a decision (the "📋 Decision needed" prefix from
	// formatSlackDecision) is upgraded to blocks — ordinary chat posted while a
	// decision is pending is left as plain text. When matched, the blocks REPLACE
	// the plain text, keeping msg.Text as the notification fallback. No match (no
	// active decision, or the request carries no options) falls through unchanged.
	if isSlackDecisionText(msg.Text) {
		if decision, ok := t.activeDecisionForChannel(msg.Binding.ChannelSlug); ok {
			decision.From = t.displayNameForOffice(decision.From)
			opts = append(opts, slack.MsgOptionBlocks(formatSlackInterviewBlocks(decision)...))
		}
	}
	// Thread anchoring: a task message threads under its task's single root
	// card (one task = one thread). The root is ensured to exist first, so the
	// very first task message can't land before the thread is opened. A task
	// anchor wins over any echoed inbound ThreadKey — task work belongs in the
	// task thread, not wherever the trigger happened to sit.
	threadTS := msg.ThreadKey
	if msg.SourceTaskID != "" {
		// A task message belongs in its task's ONE thread, full stop. Anchor on
		// the task root — and if the root can't be ensured, post to the channel
		// (threadTS = ""), NOT the inbound ThreadKey: threading task work under
		// some unrelated inbound thread would cross-wire task context and break
		// the one-task-one-thread contract.
		threadTS = t.ensureTaskThreadRoot(ctx, msg.SourceTaskID)
	}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if _, _, err := api.PostMessageContext(ctx, channelID, opts...); err != nil {
		return fmt.Errorf("slack send: %w", err)
	}
	return nil
}

// FormatOutbound converts a broker channelMessage to a transport.Outbound for the
// per-transport dispatcher. Returns ok=false when no Slack channel is mapped for
// the message's channel slug — a missing mapping is a routine skip, not a send
// failure. Pure mapping: no side effects (the API call happens in Send).
func (t *SlackTransport) FormatOutbound(msg channelMessage) (transport.Outbound, bool) {
	ch := normalizeChannelSlug(msg.Channel)
	if t.channelIDForSlug(ch) == "" {
		log.Printf("[slack] outbound skip: no channel for %q", ch)
		return transport.Outbound{}, false
	}
	// Never relay a human-authored message out to Slack: the bot would post it
	// under the human's name ("nazz: …"), impersonating them. A human's message
	// either already lives in Slack (typed there, marked delivered on inbound)
	// or came from another surface (web/API), where WUPHF speaking AS that human
	// in Slack is wrong — the office is one coordinating bot, never a person.
	// Decision/interview cards are agent-authored (From is the raising agent),
	// so the approval-gate path is unaffected.
	if isHumanMessageSender(msg.From) {
		return transport.Outbound{}, false
	}
	// A spawned Slack agent posts as ITSELF: capture the sender BEFORE the
	// display-name rewrite below and carry it to Send via the otherwise-unused
	// Participant field (see slack_spawned_agents.go).
	spawnedSender := t.spawnedSenderParticipant(msg.From)
	// Resolve the office-internal sender to a Slack-facing display name BEFORE
	// rendering: "@ceo" means nothing to real Slack users, so attribution is a
	// plain bold name, never a fake tag.
	addressed := leadingMentionSlugs(msg.Content)
	msg.From = t.displayNameForOffice(msg.From)
	text := t.renderOfficeTags(formatSlackOutbound(msg), addressed)
	// No per-message task footnote: task messages now thread under the task's
	// single root card (which carries the task definition + web link), so a
	// repeated "↳ task" line on every reply would just clutter the thread.
	return transport.Outbound{
		Participant:  spawnedSender,
		Binding:      transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: ch},
		Text:         text,
		SourceTaskID: strings.TrimSpace(msg.SourceTaskID),
	}, true
}

// leadingMentionSlugs parses the ADDRESSING position of a message: the run of
// "@slug" tokens (separated by spaces/commas) at the very start of the content.
// Only these are messages TO an agent — a real Slack ping is appropriate and a
// response is expected. "@slug" anywhere later is a reference, not an address;
// pinging on references makes bots answer messages that merely talk ABOUT them.
func leadingMentionSlugs(content string) map[string]bool {
	out := map[string]bool{}
	rest := strings.TrimSpace(content)
	for strings.HasPrefix(rest, "@") {
		rest = rest[1:]
		end := 0
		for end < len(rest) && isSlugByte(rest[end]) {
			end++
		}
		if end == 0 {
			break
		}
		out[strings.ToLower(rest[:end])] = true
		rest = strings.TrimLeft(rest[end:], " \t,")
	}
	return out
}

// displayNameForOffice maps an office sender identity to the name real Slack
// users should see. WUPHF presents as ONE coordinating bot in Slack: internal
// office agents (ceo, planner, …) are an implementation detail, so their
// messages carry NO sender attribution — Slack already shows the bot as the
// speaker. Humans keep a minimal attribution (they are a genuinely different
// speaker), and gate actors (human:<slack user id>) resolve to their cached
// Slack display name. "system" passes through (formatSlackOutbound styles it).
func (t *SlackTransport) displayNameForOffice(from string) string {
	from = strings.TrimSpace(from)
	if from == "" || from == "system" {
		return from
	}
	if id, ok := strings.CutPrefix(from, "human:"); ok {
		// Gate clicks attribute to human:<slack user id>. Show the cached
		// Slack display name — attribution, not a mention, so no ping.
		uid := strings.ToUpper(strings.TrimSpace(id))
		t.mapsMu.RLock()
		info, cached := t.UserMap[uid]
		t.mapsMu.RUnlock()
		if cached {
			return info.name
		}
		return uid
	}
	if isHumanMessageSender(from) {
		return "Human"
	}
	if t.Broker != nil {
		if _, isMember := t.Broker.MemberDisplayNames()[normalizeActorSlug(from)]; isMember {
			// Internal agent → the bot speaks as itself, no role theater.
			return ""
		}
	}
	return from
}

// renderOfficeTags rewrites office-internal "@slug" tokens for SLACK READERS:
// a REGISTERED foreign agent becomes a real <@U…> mention so the bot is
// actually pinged; the office LEAD becomes the bot's own Slack name (WUPHF is
// one coordinating bot — there is no public "CEO"); every other member token
// becomes its plain display name, because a tag that cannot be tagged in
// Slack is just noise. Two properties keep this injection-safe: replacement
// values come exclusively from the roster/registry/auth.test (never from
// message text), and the rewrite runs AFTER formatSlackOutbound has escaped
// every dynamic field, so surrounding text cannot smuggle its own control
// sequences. Unknown "@whatever" tokens are left as escaped plain text.
func (t *SlackTransport) renderOfficeTags(text string, addressed map[string]bool) string {
	if t.Broker == nil {
		return text
	}
	leadSlug := t.Broker.OfficeLeadSlug()
	for slug, name := range t.Broker.MemberDisplayNames() {
		if slug == "" {
			continue
		}
		if userID := t.Broker.SlackAgentUserIDBySlug(slug); userID != "" {
			// Ping ONLY when the message addresses the agent (leading-mention
			// position) — a ping is a request to respond. References render
			// as the plain name so bots don't answer messages about them.
			if addressed[slug] {
				text = replaceMentionToken(text, "@"+slug, "<@"+slackEscape(userID)+">")
			} else {
				text = replaceMentionToken(text, "@"+slug, slackEscape(name))
			}
			continue
		}
		if slug == leadSlug && t.botUserName != "" {
			name = t.botUserName
		}
		text = replaceMentionToken(text, "@"+slug, slackEscape(name))
	}
	// Universal human identities never map to one Slack user; render plainly.
	text = replaceMentionToken(text, "@human", "Human")
	text = replaceMentionToken(text, "@you", "Human")
	return text
}

// replaceMentionToken replaces whole-token occurrences of token in text with
// replacement. An occurrence only counts when neither the byte before nor the
// byte after could extend a slug, so "@bot" never rewrites inside "@bot-2" or
// "mail@bot".
func replaceMentionToken(text, token, replacement string) string {
	var sb strings.Builder
	for {
		i := strings.Index(text, token)
		if i < 0 {
			sb.WriteString(text)
			return sb.String()
		}
		end := i + len(token)
		boundary := (i == 0 || !isSlugByte(text[i-1])) &&
			(end >= len(text) || !isSlugByte(text[end]))
		sb.WriteString(text[:i])
		if boundary {
			sb.WriteString(replacement)
		} else {
			sb.WriteString(token)
		}
		text = text[end:]
	}
}

// isSlugByte reports whether c can be part of an office member slug.
func isSlugByte(c byte) bool {
	return c == '-' || c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// channelIDForSlug returns the Slack channel id for the given office channel
// slug, or "" if no mapping exists.
func (t *SlackTransport) channelIDForSlug(slug string) string {
	t.mapsMu.RLock()
	defer t.mapsMu.RUnlock()
	for channelID, s := range t.ChannelMap {
		if s == slug {
			return channelID
		}
	}
	return ""
}

// channelSlugForID returns the office channel slug bound to a Slack channel id,
// or "" if the channel is not bridged. Used by the gate handler to bind a click
// to the channel it came from.
func (t *SlackTransport) channelSlugForID(channelID string) string {
	t.mapsMu.RLock()
	defer t.mapsMu.RUnlock()
	return t.ChannelMap[channelID]
}

// resolveUser maps a Slack user id to an office member slug (or display name
// fallback) and reports whether the user is a human. The result is cached in
// UserMap so repeat senders skip the users.info round-trip. A lookup failure
// falls back to the raw user id and treats the sender as human.
func (t *SlackTransport) resolveUser(ctx context.Context, userID string) (name string, human bool) {
	if userID == "" {
		return "unknown", true
	}

	t.mapsMu.RLock()
	cached, ok := t.UserMap[userID]
	t.mapsMu.RUnlock()
	if ok {
		return cached.name, cached.human
	}

	if t.api == nil {
		return userID, true
	}
	user, err := t.api.GetUserInfoContext(ctx, userID)
	if err != nil || user == nil {
		return userID, true
	}
	info := slackUserInfo{
		name:     slackDisplayName(user),
		human:    !user.IsBot,
		realName: strings.TrimSpace(user.Profile.RealName),
		title:    strings.TrimSpace(user.Profile.Title),
		tz:       strings.TrimSpace(user.TZ),
	}
	t.mapsMu.Lock()
	t.UserMap[userID] = info
	t.mapsMu.Unlock()
	return info.name, info.human
}

// warmUserMap pre-populates UserMap from the membership of every mapped channel.
// Best-effort: per-channel and per-user failures are logged and skipped so a
// single bad lookup never blocks startup.
func (t *SlackTransport) warmUserMap(ctx context.Context) {
	t.mapsMu.RLock()
	channelIDs := make([]string, 0, len(t.ChannelMap))
	for channelID := range t.ChannelMap {
		channelIDs = append(channelIDs, channelID)
	}
	t.mapsMu.RUnlock()

	for _, channelID := range channelIDs {
		members, _, err := t.api.GetUsersInConversationContext(ctx, &slack.GetUsersInConversationParameters{
			ChannelID: channelID,
			Limit:     200,
		})
		if err != nil {
			log.Printf("[slack] warm user map: channel %s: %v", channelID, err)
			continue
		}
		for _, userID := range members {
			if userID == "" || userID == t.botUserID {
				continue
			}
			t.mapsMu.RLock()
			_, known := t.UserMap[userID]
			t.mapsMu.RUnlock()
			if known {
				continue
			}
			// resolveUser caches under mapsMu; ignore the result here.
			_, _ = t.resolveUser(ctx, userID)
		}
	}
}

// setHealth updates the cached health snapshot. On a connected state it stamps
// LastSuccessAt and clears the error; on a degraded/disconnected state it records
// the error and leaves LastSuccessAt untouched.
func (t *SlackTransport) setHealth(state transport.HealthState, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.healthState = state
	if state == transport.HealthConnected {
		t.lastSuccessAt = time.Now()
		t.lastErr = nil
		return
	}
	if err != nil {
		t.lastErr = err
	}
}

// slackDisplayName picks the friendliest non-empty name for a Slack user.
func slackDisplayName(user *slack.User) string {
	if user == nil {
		return "unknown"
	}
	for _, candidate := range []string{
		strings.TrimSpace(user.Profile.DisplayName),
		strings.TrimSpace(user.RealName),
		strings.TrimSpace(user.Profile.RealName),
		strings.TrimSpace(user.Name),
	} {
		if candidate != "" {
			return candidate
		}
	}
	if user.ID != "" {
		return user.ID
	}
	return "unknown"
}

// formatSlackOutbound renders a broker message as Slack mrkdwn. It mirrors the
// kind taxonomy of formatTelegramOutbound but emits Slack-flavored markup
// (*bold* / _italic_) instead of Telegram HTML.
//
// Every DYNAMIC field (From, Title, Content, SourceLabel) is run through
// slackEscape before composition, so a broker message that carries hostile text
// cannot inject Slack control sequences (<!channel>/<!here> mass-pings, <@U…>
// pings, or fake <url|label> links). The structural markup this function adds
// (*, _, [, ]) is intentionally left literal so it still renders. Posts then use
// escapeText=false (in Send) because escaping already happened here at field
// granularity.
func formatSlackOutbound(msg channelMessage) string {
	from := slackEscape(msg.From)
	content := slackEscape(msg.Content)
	title := slackEscape(msg.Title)
	switch {
	case msg.Kind == "skill_invocation":
		if from == "" {
			return "⚡ Skill invoked"
		}
		return fmt.Sprintf("⚡ *%s* invoked a skill", from)
	case msg.Kind == "skill_proposal":
		return fmt.Sprintf("💡 *Skill proposed*: %s", content)
	case msg.Kind == "automation":
		source := msg.Source
		if msg.SourceLabel != "" {
			source = msg.SourceLabel
		}
		if source == "" {
			source = "automation"
		}
		return fmt.Sprintf("🤖 *[%s]*: %s", slackEscape(source), content)
	case isHumanDecisionKind(msg.Kind):
		return formatSlackDecision(msg)
	case msg.From == "system":
		return fmt.Sprintf("→ _%s_", content)
	default:
		var sb strings.Builder
		if from != "" {
			// Attribution is a plain bold name — office identities are not
			// taggable in Slack, so no "@" theater.
			sb.WriteString("*")
			sb.WriteString(from)
			sb.WriteString("*: ")
		}
		if title != "" {
			sb.WriteString("[")
			sb.WriteString(title)
			sb.WriteString("] ")
		}
		sb.WriteString(content)
		return sb.String()
	}
}

// slackDecisionPrefix is the leading marker every decision/interview message
// carries. Send uses it (via isSlackDecisionText) to recognise the one outbound
// message that should be upgraded from plain text to an interactive Block Kit
// card, leaving ordinary chat posted during a pending decision as plain text.
const slackDecisionPrefix = "📋 *Decision needed*"

// isSlackDecisionText reports whether an outbound body was rendered as a decision
// by formatSlackDecision (and therefore should be upgraded to interactive blocks).
func isSlackDecisionText(text string) bool {
	return strings.HasPrefix(text, slackDecisionPrefix)
}

// formatSlackDecision renders a human decision/interview message as Slack mrkdwn.
// Dynamic fields are escaped (see formatSlackOutbound).
func formatSlackDecision(msg channelMessage) string {
	var sb strings.Builder
	sb.WriteString(slackDecisionPrefix)
	if msg.From != "" {
		sb.WriteString(" from ")
		sb.WriteString(slackEscape(msg.From))
	}
	sb.WriteString("\n\n")
	sb.WriteString(slackEscape(msg.Content))
	if msg.Title != "" {
		sb.WriteString("\n\n_")
		sb.WriteString(slackEscape(msg.Title))
		sb.WriteString("_")
	}
	return sb.String()
}

// slackEscape neutralizes Slack control characters (& < >) in a dynamic field so
// composed mrkdwn cannot smuggle pings or fake links. Wraps slack-go's canonical
// EscapeMessage.
func slackEscape(s string) string {
	return slackutilsx.EscapeMessage(s)
}
