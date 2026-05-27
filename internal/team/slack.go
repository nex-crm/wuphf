package team

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nex-crm/wuphf/internal/team/transport"
)

const slackAdapterName = "slack"
const slackRestrictedChannelReply = "WUPHF is only enabled in Slack channels mapped from Settings > Slack. Use a private mapped channel to access this office."

var _ transport.Transport = (*SlackTransport)(nil)

type SlackTransport struct {
	BotToken   string
	AppToken   string
	BotUserID  string
	Broker     *Broker
	ChannelMap map[string]string // Slack channel ID -> WUPHF channel slug.
	client     *slackAPIClient

	mu               sync.Mutex
	healthState      transport.HealthState
	lastSuccessAt    time.Time
	lastErr          error
	assistantContext map[string]slackAssistantThreadContext
}

func NewSlackTransport(broker *Broker, botToken, appToken, botUserID string) *SlackTransport {
	channelMap := make(map[string]string)
	for _, ch := range broker.SurfaceChannels(slackAdapterName) {
		if ch.Surface == nil || strings.TrimSpace(ch.Surface.RemoteID) == "" {
			continue
		}
		channelMap[ch.Surface.RemoteID] = ch.Slug
	}
	return &SlackTransport{
		BotToken:         strings.TrimSpace(botToken),
		AppToken:         strings.TrimSpace(appToken),
		BotUserID:        strings.TrimSpace(botUserID),
		Broker:           broker,
		ChannelMap:       channelMap,
		client:           newSlackAPIClient(botToken, appToken),
		healthState:      transport.HealthDisconnected,
		assistantContext: make(map[string]slackAssistantThreadContext),
	}
}

func (s *SlackTransport) Name() string { return slackAdapterName }

func (s *SlackTransport) Binding() transport.Binding { return transport.Binding{} }

func (s *SlackTransport) Health() transport.Health {
	s.mu.Lock()
	defer s.mu.Unlock()
	return transport.Health{State: s.healthState, LastSuccessAt: s.lastSuccessAt, LastError: s.lastErr}
}

func (s *SlackTransport) Run(ctx context.Context, host transport.Host) error {
	if s.BotToken == "" {
		return fmt.Errorf("slack bot token is empty")
	}
	if s.AppToken == "" {
		return fmt.Errorf("slack app token is empty")
	}
	backoff := NewBridgeBackoff(time.Second, 30*time.Second)
	for {
		if ctx.Err() != nil {
			return nil
		}
		if err := s.runSocket(ctx, host); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.markHealth(transport.HealthDegraded, err)
			log.Printf("[slack] socket loop error: %v", err)
			if waitErr := backoff.Wait(ctx); waitErr != nil {
				return nil
			}
			continue
		}
		backoff.Reset()
	}
}

func (s *SlackTransport) Send(ctx context.Context, msg transport.Outbound) error {
	channelID := strings.TrimSpace(msg.Binding.ChannelSlug)
	if channelID == "" {
		return fmt.Errorf("slack: missing Slack channel id")
	}
	payload := map[string]any{
		"channel": channelID,
		"text":    msg.Text,
	}
	if len(msg.Blocks) > 0 {
		payload["blocks"] = msg.Blocks
	} else {
		payload["blocks"] = slackBlocksForText(msg.Text)
	}
	if msg.Participant.DisplayName != "" {
		payload["username"] = msg.Participant.DisplayName
	}
	if msg.IconURL != "" {
		payload["icon_url"] = msg.IconURL
	} else if msg.IconEmoji != "" {
		payload["icon_emoji"] = msg.IconEmoji
	}
	if msg.ThreadKey != "" {
		payload["thread_ts"] = msg.ThreadKey
	}
	resp, err := s.client.postMessage(ctx, payload)
	if err != nil {
		return err
	}
	if s.Broker != nil {
		s.Broker.recordSlackOutbound(msg.Participant.Key, resp.Channel, resp.TS)
	}
	return nil
}

func (s *SlackTransport) FormatOutbound(msg channelMessage) (transport.Outbound, bool) {
	slackChannelID, threadTS := s.slackOutboundTarget(msg)
	if slackChannelID == "" {
		return transport.Outbound{}, false
	}
	if msg.Source == slackAdapterName {
		return transport.Outbound{}, false
	}
	text := formatSlackOutbound(msg)
	identity := s.slackOutboundIdentity(msg)
	return transport.Outbound{
		Participant: transport.Participant{AdapterName: s.Name(), Key: msg.ID, DisplayName: identity.Username},
		Binding:     transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: slackChannelID},
		Text:        text,
		Blocks:      slackBlocksForMessage(msg),
		IconEmoji:   identity.IconEmoji,
		IconURL:     identity.IconURL,
		ThreadKey:   threadTS,
	}, true
}

func (s *SlackTransport) slackChannelSlug(channelID string) string {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ChannelMap[channelID]
}

func (s *SlackTransport) assistantContextChannelSlug(c slackAssistantThreadContext) string {
	return s.slackChannelSlug(c.ChannelID)
}

func (s *SlackTransport) slackChannelIDForSlug(channelSlug string) string {
	channelSlug = normalizeChannelSlug(channelSlug)
	if channelSlug == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for remoteID, slug := range s.ChannelMap {
		if slug == channelSlug {
			return remoteID
		}
	}
	return ""
}

func (s *SlackTransport) setSlackChannelMap(channelID, channelSlug string) {
	channelID = strings.TrimSpace(channelID)
	channelSlug = normalizeChannelSlug(channelSlug)
	if channelID == "" || channelSlug == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ChannelMap == nil {
		s.ChannelMap = make(map[string]string)
	}
	s.ChannelMap[channelID] = channelSlug
}

func (s *SlackTransport) slackChannelCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ChannelMap)
}

func (s *SlackTransport) slackOutboundThread(replyTo string) string {
	if s.Broker == nil {
		return ""
	}
	return s.Broker.slackOutboundTimestamp(replyTo)
}

func (s *SlackTransport) slackOutboundTarget(msg channelMessage) (channelID, threadTS string) {
	if s.Broker != nil && strings.TrimSpace(msg.ReplyTo) != "" {
		channelID, threadTS = s.Broker.slackReceiptForMessage(msg.ReplyTo)
		if channelID != "" {
			return channelID, threadTS
		}
	}
	return s.slackChannelIDForSlug(msg.Channel), s.slackOutboundThread(msg.ReplyTo)
}

type slackOutboundIdentity struct {
	Username  string
	IconEmoji string
	IconURL   string
}

func (s *SlackTransport) slackOutboundIdentity(msg channelMessage) slackOutboundIdentity {
	slug := normalizeActorSlug(msg.From)
	if slug == "" || slug == "system" || slug == "nex" || isHumanMessageSender(slug) {
		return slackOutboundIdentity{}
	}
	if s.Broker != nil && s.Broker.officeMemberBySlug(slug) == nil {
		if msg.Kind == "automation" {
			return slackOutboundIdentity{}
		}
	}
	username := "WUPHF @" + slug
	return slackOutboundIdentity{
		Username:  truncateSlackText(username, 80),
		IconEmoji: slackAgentIconEmoji(slug),
	}
}

func (s *SlackTransport) runSocket(ctx context.Context, host transport.Host) error {
	socketURL, err := s.client.openSocketModeURL(ctx)
	if err != nil {
		return err
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, socketURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	s.markHealth(transport.HealthConnected, nil)
	for {
		var env slackSocketEnvelope
		if err := conn.ReadJSON(&env); err != nil {
			return err
		}
		if env.EnvelopeID != "" {
			if err := conn.WriteJSON(map[string]string{"envelope_id": env.EnvelopeID}); err != nil {
				return err
			}
		}
		if env.Type == "disconnect" {
			return fmt.Errorf("slack requested disconnect")
		}
		s.markHealth(transport.HealthConnected, nil)
		s.handleEnvelope(ctx, host, env)
	}
}

type slackSocketEnvelope struct {
	EnvelopeID string          `json:"envelope_id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
}

type slackEventPayload struct {
	Type      string          `json:"type"`
	EventID   string          `json:"event_id"`
	EventTime int64           `json:"event_time"`
	Event     json.RawMessage `json:"event"`
}

type slackMessageEvent struct {
	Type        string `json:"type"`
	Subtype     string `json:"subtype,omitempty"`
	Channel     string `json:"channel"`
	ChannelType string `json:"channel_type,omitempty"`
	User        string `json:"user,omitempty"`
	BotID       string `json:"bot_id,omitempty"`
	Text        string `json:"text"`
	Timestamp   string `json:"ts"`
	ThreadTS    string `json:"thread_ts,omitempty"`
}

type slackAssistantEvent struct {
	Type            string               `json:"type"`
	AssistantThread slackAssistantThread `json:"assistant_thread"`
}

type slackAppHomeOpenedEvent struct {
	Type string `json:"type"`
	User string `json:"user"`
	Tab  string `json:"tab,omitempty"`
}

type slackAssistantThread struct {
	UserID    string                      `json:"user_id"`
	Context   slackAssistantThreadContext `json:"context"`
	ChannelID string                      `json:"channel_id"`
	ThreadTS  string                      `json:"thread_ts"`
}

type slackAssistantThreadContext struct {
	ChannelID    string `json:"channel_id,omitempty"`
	TeamID       string `json:"team_id,omitempty"`
	EnterpriseID string `json:"enterprise_id,omitempty"`
}

type slackSlashCommandPayload struct {
	Type        string `json:"type"`
	Command     string `json:"command"`
	Text        string `json:"text"`
	UserID      string `json:"user_id"`
	UserName    string `json:"user_name"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	ResponseURL string `json:"response_url"`
}

func (s *SlackTransport) handleEnvelope(ctx context.Context, host transport.Host, env slackSocketEnvelope) {
	if env.EnvelopeID != "" && s.Broker != nil && s.Broker.slackEventSeenOrMark("envelope:"+env.EnvelopeID) {
		return
	}
	var eventPayload slackEventPayload
	if err := json.Unmarshal(env.Payload, &eventPayload); err == nil && eventPayload.Type == "event_callback" {
		if eventPayload.EventID != "" && s.Broker != nil && s.Broker.slackEventSeenOrMark("event:"+eventPayload.EventID) {
			return
		}
		s.handleEventCallback(ctx, host, eventPayload)
		return
	}
	var command slackSlashCommandPayload
	if err := json.Unmarshal(env.Payload, &command); err == nil && command.Command != "" {
		s.handleSlashCommand(ctx, command)
	}
}

func (s *SlackTransport) handleEventCallback(ctx context.Context, host transport.Host, payload slackEventPayload) {
	var assistantEvent slackAssistantEvent
	if err := json.Unmarshal(payload.Event, &assistantEvent); err == nil {
		switch assistantEvent.Type {
		case "assistant_thread_started":
			s.handleAssistantThreadStarted(ctx, assistantEvent.AssistantThread)
			return
		case "assistant_thread_context_changed":
			s.handleAssistantThreadContextChanged(ctx, assistantEvent.AssistantThread)
			return
		}
	}
	var appHomeEvent slackAppHomeOpenedEvent
	if err := json.Unmarshal(payload.Event, &appHomeEvent); err == nil && appHomeEvent.Type == "app_home_opened" {
		s.handleAppHomeOpened(ctx, appHomeEvent)
		return
	}
	var event slackMessageEvent
	if err := json.Unmarshal(payload.Event, &event); err != nil {
		return
	}
	if event.Type != "message" && event.Type != "app_mention" {
		return
	}
	if event.Subtype != "" || event.BotID != "" || (s.BotUserID != "" && event.User == s.BotUserID) {
		return
	}
	if event.Type == "message" && event.ChannelType == "im" {
		s.handleAssistantMessage(ctx, event)
		return
	}
	channelSlug := s.slackChannelSlug(event.Channel)
	text := normalizeSlackInboundText(event.Text, s.BotUserID)
	if event.Type != "app_mention" && strings.HasPrefix(strings.TrimSpace(strings.ToLower(text)), "wuphf ") {
		s.handleCommandText(ctx, slackCommandContext{
			UserID:      event.User,
			ChannelID:   event.Channel,
			ChannelSlug: channelSlug,
			ThreadTS:    firstNonEmpty(event.ThreadTS, event.Timestamp),
		}, strings.TrimSpace(text))
		return
	}
	if channelSlug == "" {
		return
	}
	replyTo := s.slackReplyToForEvent(event)
	p := transport.Participant{AdapterName: s.Name(), Key: event.User, DisplayName: "slack:" + event.User, Human: true}
	_ = host.UpsertParticipant(ctx, p, transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: channelSlug})
	_ = host.ReceiveMessage(ctx, transport.Message{
		Participant:       p,
		Binding:           transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: channelSlug},
		Text:              text,
		ExternalID:        event.Timestamp,
		ExternalChannelID: event.Channel,
		Tagged:            s.taggedOfficeMembers(text),
		ReplyTo:           replyTo,
		ThreadKey:         firstNonEmpty(event.ThreadTS, event.Timestamp),
	})
}

func (s *SlackTransport) handleAppHomeOpened(ctx context.Context, event slackAppHomeOpenedEvent) {
	if strings.TrimSpace(event.User) == "" {
		return
	}
	if err := s.client.publishHomeView(ctx, event.User, s.slackAppHomeBlocks(event.User)); err != nil {
		log.Printf("[slack] publish app home failed: %v", err)
	}
}

func (s *SlackTransport) handleAssistantThreadStarted(ctx context.Context, thread slackAssistantThread) {
	if thread.ChannelID == "" || thread.ThreadTS == "" {
		return
	}
	if s.assistantContextChannelSlug(thread.Context) == "" {
		_ = s.postRestrictedChannelReply(ctx, thread.ChannelID, thread.ThreadTS)
		return
	}
	s.rememberAssistantContext(thread.ChannelID, thread.ThreadTS, thread.Context)
	prompts := []slackAssistantPrompt{
		{Title: "List agents", Message: "agent list"},
		{Title: "Mirror this channel", Message: "channel mirror"},
		{Title: "Search the wiki", Message: "wiki search project decisions"},
		{Title: "Show open work", Message: "inbox"},
	}
	if err := s.client.setSuggestedPrompts(ctx, thread.ChannelID, thread.ThreadTS, "Start with WUPHF", prompts); err != nil {
		log.Printf("[slack] set suggested prompts failed: %v", err)
	}
	if err := s.client.setAssistantTitle(ctx, thread.ChannelID, thread.ThreadTS, "WUPHF"); err != nil {
		log.Printf("[slack] set assistant title failed: %v", err)
	}
	_, _ = s.client.postMessage(ctx, map[string]any{
		"channel":   thread.ChannelID,
		"thread_ts": thread.ThreadTS,
		"text":      "Ask WUPHF to list agents, mirror the current channel, search wiki content, or show active work.",
		"blocks":    slackBlocksForText("Ask WUPHF to list agents, mirror the current channel, search wiki content, or show active work."),
	})
}

func (s *SlackTransport) slackReplyToForEvent(event slackMessageEvent) string {
	if s.Broker == nil || strings.TrimSpace(event.ThreadTS) == "" || event.ThreadTS == event.Timestamp {
		return ""
	}
	return s.Broker.slackMessageIDForTimestamp(event.Channel, event.ThreadTS)
}

func (s *SlackTransport) taggedOfficeMembers(text string) []string {
	if s.Broker == nil {
		return nil
	}
	mentioned := extractMentionedSlugs(text)
	if len(mentioned) == 0 {
		return nil
	}
	s.Broker.mu.Lock()
	defer s.Broker.mu.Unlock()
	tagged := make([]string, 0, len(mentioned))
	for _, slug := range mentioned {
		if s.Broker.findMemberLocked(slug) != nil {
			tagged = append(tagged, slug)
		}
	}
	return tagged
}

func (s *SlackTransport) handleAssistantThreadContextChanged(ctx context.Context, thread slackAssistantThread) {
	if thread.ChannelID == "" || thread.ThreadTS == "" {
		return
	}
	if s.assistantContextChannelSlug(thread.Context) == "" {
		_ = s.postRestrictedChannelReply(ctx, thread.ChannelID, thread.ThreadTS)
		return
	}
	s.rememberAssistantContext(thread.ChannelID, thread.ThreadTS, thread.Context)
	prompts := []slackAssistantPrompt{
		{Title: "Mirror this Slack channel", Message: "channel mirror"},
		{Title: "List WUPHF channels", Message: "channel list"},
		{Title: "Search wiki from this context", Message: "wiki search " + firstNonEmpty(thread.Context.ChannelID, "current channel")},
	}
	if err := s.client.setSuggestedPrompts(ctx, thread.ChannelID, thread.ThreadTS, "Context-aware actions", prompts); err != nil {
		log.Printf("[slack] refresh suggested prompts failed: %v", err)
	}
}

func (s *SlackTransport) rememberAssistantContext(channelID, threadTS string, c slackAssistantThreadContext) {
	key := assistantContextKey(channelID, threadTS)
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assistantContext[key] = c
}

func (s *SlackTransport) getAssistantContext(channelID, threadTS string) slackAssistantThreadContext {
	key := assistantContextKey(channelID, threadTS)
	if key == "" {
		return slackAssistantThreadContext{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.assistantContext[key]
}

func assistantContextKey(channelID, threadTS string) string {
	channelID = strings.TrimSpace(channelID)
	threadTS = strings.TrimSpace(threadTS)
	if channelID == "" || threadTS == "" {
		return ""
	}
	return channelID + ":" + threadTS
}

func (s *SlackTransport) handleAssistantMessage(ctx context.Context, event slackMessageEvent) {
	threadTS := firstNonEmpty(event.ThreadTS, event.Timestamp)
	if event.Channel == "" || threadTS == "" {
		return
	}
	if err := s.client.setAssistantStatus(ctx, event.Channel, threadTS, "Checking WUPHF..."); err != nil {
		log.Printf("[slack] set assistant status failed: %v", err)
	}
	context := s.getAssistantContext(event.Channel, threadTS)
	channelSlug := s.assistantContextChannelSlug(context)
	if channelSlug == "" {
		s.handleAppChatMessage(ctx, event)
		_ = s.client.setAssistantStatus(ctx, event.Channel, threadTS, "")
		return
	}
	reply := s.dispatchSlackbotCommand(ctx, slackCommandContext{
		UserID:    event.User,
		ChannelID: event.Channel,
		ThreadTS:  threadTS,
		// For AI app DMs, ChannelSlug is the referring Slack channel when Slack
		// supplies one, but only if that channel is mapped in Settings > Slack.
		ChannelSlug: channelSlug,
	}, normalizeSlackInboundText(event.Text, s.BotUserID))
	if title := assistantTitleForText(event.Text); title != "" {
		if err := s.client.setAssistantTitle(ctx, event.Channel, threadTS, title); err != nil {
			log.Printf("[slack] set assistant title failed: %v", err)
		}
	}
	if err := s.streamAssistantReply(ctx, event.Channel, threadTS, reply); err != nil {
		log.Printf("[slack] assistant stream failed: %v", err)
		_, _ = s.client.postMessage(ctx, map[string]any{
			"channel":   event.Channel,
			"thread_ts": threadTS,
			"text":      reply,
			"blocks":    slackBlocksForText(reply),
		})
	}
	if err := s.client.setAssistantStatus(ctx, event.Channel, threadTS, ""); err != nil {
		log.Printf("[slack] clear assistant status failed: %v", err)
	}
}

func (s *SlackTransport) handleAppChatMessage(ctx context.Context, event slackMessageEvent) {
	if s.Broker == nil {
		_ = s.postTextReply(ctx, event.Channel, firstNonEmpty(event.ThreadTS, event.Timestamp), "WUPHF broker is not available.")
		return
	}
	text := normalizeSlackInboundText(event.Text, s.BotUserID)
	if strings.TrimSpace(text) == "" {
		return
	}
	msg, err := s.Broker.PostInboundSurfaceMessageWithOptions(
		"slack:"+event.User,
		"general",
		text,
		slackAdapterName,
		inboundSurfaceMessageOptions{
			Tagged:  s.taggedOfficeMembers(text),
			ReplyTo: s.slackReplyToForEvent(event),
		},
	)
	if err != nil {
		_ = s.postTextReply(ctx, event.Channel, firstNonEmpty(event.ThreadTS, event.Timestamp), "WUPHF message failed: "+err.Error())
		return
	}
	s.Broker.recordSlackOutbound(msg.ID, event.Channel, event.Timestamp)
}

func (s *SlackTransport) streamAssistantReply(ctx context.Context, channelID, threadTS, text string) error {
	chunks := splitSlackStreamText(text, 11000)
	if len(chunks) == 0 {
		chunks = []string{"Done."}
	}
	resp, err := s.client.startStream(ctx, map[string]any{
		"channel":   channelID,
		"thread_ts": threadTS,
		"chunks": []map[string]any{{
			"type":          "markdown_text",
			"markdown_text": chunks[0],
		}},
	})
	if err != nil {
		return err
	}
	streamTS := firstNonEmpty(resp.MessageTS, resp.TS)
	if streamTS == "" {
		return slackAPIError{Method: "chat.startStream", Code: "missing_message_ts"}
	}
	for _, chunk := range chunks[1:] {
		if err := s.client.appendStream(ctx, map[string]any{
			"channel":    channelID,
			"message_ts": streamTS,
			"thread_ts":  threadTS,
			"chunks": []map[string]any{{
				"type":          "markdown_text",
				"markdown_text": chunk,
			}},
		}); err != nil {
			return err
		}
	}
	return s.client.stopStream(ctx, map[string]any{
		"channel":    channelID,
		"message_ts": streamTS,
		"thread_ts":  threadTS,
	})
}

func (s *SlackTransport) handleSlashCommand(ctx context.Context, payload slackSlashCommandPayload) {
	channelSlug := s.slackChannelSlug(payload.ChannelID)
	if channelSlug == "" {
		_ = s.postRestrictedChannelReply(ctx, payload.ChannelID, "")
		return
	}
	s.handleSlashChannelMessage(ctx, payload, channelSlug)
}

func (s *SlackTransport) handleSlashChannelMessage(ctx context.Context, payload slackSlashCommandPayload, channelSlug string) {
	if s.Broker == nil {
		_ = s.postTextReply(ctx, payload.ChannelID, "", "WUPHF broker is not available.")
		return
	}
	text := strings.TrimSpace(payload.Text)
	if text == "" {
		text = "wuphf"
	}
	from := "slack:" + firstNonEmpty(payload.UserName, payload.UserID)
	msg, err := s.Broker.PostInboundSurfaceMessageWithOptions(
		from,
		channelSlug,
		text,
		slackAdapterName,
		inboundSurfaceMessageOptions{Tagged: s.taggedOfficeMembers(text)},
	)
	if err != nil {
		_ = s.postTextReply(ctx, payload.ChannelID, "", "WUPHF message failed: "+err.Error())
		return
	}
	echoText := formatSlackSlashEcho(payload, channelSlug, text)
	resp, err := s.client.postMessage(ctx, map[string]any{
		"channel": payload.ChannelID,
		"text":    echoText,
		"blocks":  slackSlashEchoBlocks(payload, channelSlug, text),
	})
	if err != nil {
		log.Printf("[slack] slash channel echo failed: %v", err)
		return
	}
	s.Broker.recordSlackOutbound(msg.ID, resp.Channel, resp.TS)
}

func (s *SlackTransport) handleCommandText(ctx context.Context, c slackCommandContext, text string) {
	if strings.TrimSpace(c.ChannelSlug) == "" {
		if c.ChannelID != "" {
			_ = s.postRestrictedChannelReply(ctx, c.ChannelID, c.ThreadTS)
		}
		return
	}
	reply := s.dispatchSlackbotCommand(ctx, c, text)
	if strings.TrimSpace(reply) == "" {
		return
	}
	payload := map[string]any{
		"channel": c.ChannelID,
		"text":    reply,
		"blocks":  slackBlocksForText(reply),
	}
	if c.ThreadTS != "" {
		payload["thread_ts"] = c.ThreadTS
	}
	if _, err := s.client.postMessage(ctx, payload); err != nil {
		log.Printf("[slack] command response failed: %v", err)
	}
}

func (s *SlackTransport) postRestrictedChannelReply(ctx context.Context, channelID, threadTS string) error {
	return s.postTextReply(ctx, channelID, threadTS, slackRestrictedChannelReply)
}

func (s *SlackTransport) postTextReply(ctx context.Context, channelID, threadTS, text string) error {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil
	}
	payload := map[string]any{
		"channel": channelID,
		"text":    text,
		"blocks":  slackBlocksForText(text),
	}
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	_, err := s.client.postMessage(ctx, payload)
	return err
}

func (s *SlackTransport) markHealth(state transport.HealthState, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.healthState = state
	if err == nil {
		s.lastSuccessAt = time.Now().UTC()
		s.lastErr = nil
		return
	}
	s.lastErr = err
}

func formatSlackOutbound(msg channelMessage) string {
	var sb strings.Builder
	switch {
	case msg.Kind == "automation":
		source := msg.SourceLabel
		if source == "" {
			source = msg.Source
		}
		if source == "" {
			source = "automation"
		}
		sb.WriteString("*")
		sb.WriteString(escapeSlackText(source))
		sb.WriteString("*: ")
	case isHumanDecisionKind(msg.Kind):
		sb.WriteString("*Decision needed*")
		if msg.From != "" {
			sb.WriteString(" from @")
			sb.WriteString(escapeSlackText(msg.From))
		}
		sb.WriteString("\n")
	case msg.From == "system":
		sb.WriteString("_")
		sb.WriteString(escapeSlackText(msg.Content))
		sb.WriteString("_")
		appendSlackChannelFootnote(&sb, msg)
		return sb.String()
	default:
		if msg.From != "" {
			sb.WriteString("*@")
			sb.WriteString(escapeSlackText(msg.From))
			sb.WriteString("*: ")
		}
	}
	if msg.Title != "" {
		sb.WriteString("[")
		sb.WriteString(escapeSlackText(msg.Title))
		sb.WriteString("] ")
	}
	sb.WriteString(escapeSlackText(msg.Content))
	appendSlackChannelFootnote(&sb, msg)
	return sb.String()
}

func slackBlocksForMessage(msg channelMessage) []map[string]any {
	var blocks []map[string]any
	if header := slackHeaderForMessage(msg); header != "" {
		blocks = append(blocks, slackHeaderBlock(header))
	}
	if fields := slackFieldsForMessage(msg); len(fields) > 0 {
		blocks = append(blocks, map[string]any{
			"type":   "section",
			"fields": fields,
		})
	}
	content := slackMrkdwnFromContent(msg.Content)
	if content != "" {
		for _, chunk := range splitSlackStreamText(content, 2900) {
			blocks = append(blocks, slackSectionBlock(chunk, true))
			if len(blocks) >= 48 {
				break
			}
		}
	}
	if msg.Payload != nil && len(msg.Payload) > 0 {
		if summary := slackPayloadSummary(msg.Payload); summary != "" && len(blocks) < 48 {
			blocks = append(blocks, slackSectionBlock(summary, false))
		}
	}
	if footnote := slackChannelFootnote(msg); footnote != "" {
		blocks = append(blocks, slackContextBlock(footnote))
	}
	if len(blocks) == 0 {
		return slackBlocksForText(formatSlackOutbound(msg))
	}
	return blocks
}

func (s *SlackTransport) slackAppHomeBlocks(userID string) []map[string]any {
	blocks := []map[string]any{
		slackHeaderBlock("WUPHF"),
		slackSectionBlock("*Chat*\nUse the Messages tab to talk to WUPHF `#general`. Use `/wuphf @agent ...` in mapped channels to route directly to agents.", true),
		slackDividerBlock(),
	}
	blocks = append(blocks, s.slackIssuesHomeBlocks()...)
	blocks = append(blocks, slackDividerBlock())
	blocks = append(blocks, s.slackWikiHomeBlocks()...)
	blocks = append(blocks, slackDividerBlock())
	blocks = append(blocks, s.slackSettingsHomeBlocks(userID)...)
	if len(blocks) > 100 {
		return blocks[:100]
	}
	return blocks
}

func (s *SlackTransport) slackIssuesHomeBlocks() []map[string]any {
	if s.Broker == nil {
		return []map[string]any{slackHeaderBlock("Issues"), slackSectionBlock("Broker unavailable.", false)}
	}
	tasks := s.Broker.AllTasks()
	requests := s.Broker.ActiveRequests()
	blocks := []map[string]any{
		slackHeaderBlock("Issues"),
		slackSectionBlock(fmt.Sprintf("*Tasks:* %d\n*Open requests:* %d", len(tasks), len(requests)), false),
	}
	for i, task := range tasks {
		if i >= 5 {
			blocks = append(blocks, slackContextBlock(fmt.Sprintf("_%d more tasks in WUPHF_", len(tasks)-i)))
			break
		}
		status := task.Status()
		if status == "" {
			status = "pending"
		}
		owner := firstNonEmpty(task.Owner, "unassigned")
		blocks = append(blocks, slackSectionBlock(fmt.Sprintf("*%s* `%s`\n%s\n_Owner: @%s · Status: %s_", escapeSlackText(task.Title), escapeSlackText(task.ID), escapeSlackText(truncateSlackText(task.Details, 220)), escapeSlackText(owner), escapeSlackText(status)), false))
	}
	for i, req := range requests {
		if i >= 3 {
			blocks = append(blocks, slackContextBlock(fmt.Sprintf("_%d more requests in WUPHF_", len(requests)-i)))
			break
		}
		title := firstNonEmpty(req.Title, req.Kind, "Request")
		blocks = append(blocks, slackSectionBlock(fmt.Sprintf("*%s* `%s`\n%s", escapeSlackText(title), escapeSlackText(req.ID), escapeSlackText(truncateSlackText(req.Question, 220))), false))
	}
	return blocks
}

func (s *SlackTransport) slackWikiHomeBlocks() []map[string]any {
	blocks := []map[string]any{slackHeaderBlock("Wiki")}
	if s.Broker == nil || s.Broker.WikiIndex() == nil {
		return append(blocks, slackSectionBlock("Wiki index is not active. Use the WUPHF web UI to configure memory and wiki settings.", false))
	}
	hits, err := s.Broker.WikiIndex().Search(context.Background(), "project decisions team context", 5)
	if err != nil {
		return append(blocks, slackSectionBlock("Wiki search failed: "+escapeSlackText(err.Error()), false))
	}
	if len(hits) == 0 {
		return append(blocks, slackSectionBlock("No wiki hits yet. As WUPHF learns, relevant wiki facts will appear here.", false))
	}
	for _, hit := range hits {
		label := firstNonEmpty(hit.Entity, hit.FactID, "wiki")
		snippet := firstNonEmpty(hit.Snippet, "No snippet.")
		blocks = append(blocks, slackSectionBlock(fmt.Sprintf("*%s* · %.2f\n%s", escapeSlackText(label), hit.Score, escapeSlackText(truncateSlackText(snippet, 240))), false))
	}
	return blocks
}

func (s *SlackTransport) slackSettingsHomeBlocks(userID string) []map[string]any {
	if s.Broker == nil {
		return []map[string]any{slackHeaderBlock("Settings"), slackSectionBlock("Broker unavailable.", false)}
	}
	channels := s.Broker.Channels()
	mirrors := s.Broker.SurfaceChannels(slackAdapterName)
	members := s.Broker.OfficeMembers()
	fields := []map[string]any{
		slackMrkdwnFieldText(fmt.Sprintf("*Slack user*\n<@%s>", escapeSlackText(userID))),
		slackMrkdwnFieldText(fmt.Sprintf("*WUPHF channels*\n%d", len(channels))),
		slackMrkdwnFieldText(fmt.Sprintf("*Slack mirrors*\n%d", len(mirrors))),
		slackMrkdwnFieldText(fmt.Sprintf("*Agents*\n%d", countSlackVisibleAgents(members))),
	}
	blocks := []map[string]any{
		slackHeaderBlock("Settings"),
		{
			"type":   "section",
			"fields": fields,
		},
	}
	if len(mirrors) == 0 {
		blocks = append(blocks, slackSectionBlock("No Slack channels are mapped yet. Map a private channel in WUPHF Settings > Slack before using channel bridge features.", false))
		return blocks
	}
	var lines []string
	for _, ch := range mirrors {
		if ch.Surface == nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("• `#%s` → `%s`", escapeSlackText(ch.Slug), escapeSlackText(firstNonEmpty(ch.Surface.RemoteTitle, ch.Surface.RemoteID))))
	}
	blocks = append(blocks, slackSectionBlock("*Mapped channels*\n"+strings.Join(lines, "\n"), false))
	return blocks
}

func countSlackVisibleAgents(members []officeMember) int {
	count := 0
	for _, member := range members {
		if member.Slug == "" || isHumanMessageSender(member.Slug) {
			continue
		}
		count++
	}
	return count
}

var slackAgentIconPalette = []string{
	":large_green_circle:",
	":large_blue_circle:",
	":large_yellow_circle:",
	":large_purple_circle:",
	":large_orange_circle:",
	":white_circle:",
	":black_circle:",
}

func slackAgentIconEmoji(slug string) string {
	slug = normalizeActorSlug(slug)
	if slug == "" {
		return ""
	}
	sum := 0
	for _, r := range slug {
		sum += int(r)
	}
	return slackAgentIconPalette[sum%len(slackAgentIconPalette)]
}

func slackHeaderForMessage(msg channelMessage) string {
	switch {
	case isHumanDecisionKind(msg.Kind):
		return "Decision needed"
	case msg.Kind == "skill_invocation":
		return "Skill invoked"
	case msg.Kind == "skill_proposal":
		return "Skill proposed"
	case msg.Kind == "automation":
		source := firstNonEmpty(msg.SourceLabel, msg.Source, "Automation")
		return source
	case msg.Title != "":
		return msg.Title
	default:
		return ""
	}
}

func slackFieldsForMessage(msg channelMessage) []map[string]any {
	var fields []map[string]any
	if msg.From != "" && msg.From != "system" {
		fields = append(fields, slackMrkdwnFieldText("*From*\n@"+escapeSlackText(msg.From)))
	}
	if msg.Kind != "" {
		fields = append(fields, slackMrkdwnFieldText("*Type*\n"+escapeSlackText(msg.Kind)))
	}
	if msg.Title != "" && msg.Title != slackHeaderForMessage(msg) {
		fields = append(fields, slackMrkdwnFieldText("*Title*\n"+escapeSlackText(msg.Title)))
	}
	if len(msg.Tagged) > 0 {
		fields = append(fields, slackMrkdwnFieldText("*Tagged*\n"+escapeSlackText("@"+strings.Join(msg.Tagged, " @"))))
	}
	if len(fields) > 10 {
		return fields[:10]
	}
	return fields
}

func slackBlocksForText(text string) []map[string]any {
	text = strings.TrimSpace(text)
	if text == "" {
		text = "Done."
	}
	chunks := splitSlackStreamText(text, 2900)
	if len(chunks) == 0 {
		chunks = []string{text}
	}
	blocks := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		blocks = append(blocks, slackSectionBlock(chunk, true))
		if len(blocks) >= 50 {
			break
		}
	}
	return blocks
}

func slackSectionBlock(text string, expand bool) map[string]any {
	block := map[string]any{
		"type": "section",
		"text": slackMrkdwnText(truncateSlackText(text, 3000)),
	}
	if expand {
		block["expand"] = true
	}
	return block
}

func slackHeaderBlock(text string) map[string]any {
	return map[string]any{
		"type": "header",
		"text": map[string]any{
			"type": "plain_text",
			"text": truncateSlackText(strings.TrimSpace(text), 150),
		},
	}
}

func slackDividerBlock() map[string]any {
	return map[string]any{"type": "divider"}
}

func slackContextBlock(text string) map[string]any {
	return map[string]any{
		"type":     "context",
		"elements": []map[string]any{slackMrkdwnText(text)},
	}
}

func slackMrkdwnText(text string) map[string]any {
	return map[string]any{
		"type": "mrkdwn",
		"text": truncateSlackText(text, 3000),
	}
}

func slackMrkdwnFieldText(text string) map[string]any {
	return map[string]any{
		"type": "mrkdwn",
		"text": truncateSlackText(text, 2000),
	}
}

func formatSlackSlashEcho(payload slackSlashCommandPayload, channelSlug, text string) string {
	name := firstNonEmpty(payload.UserName, payload.UserID, "Slack")
	return fmt.Sprintf("*%s via /wuphf*: %s\n\n_From WUPHF #%s_", escapeSlackText(name), slackMrkdwnFromContent(text), escapeSlackText(normalizeChannelSlug(channelSlug)))
}

func slackSlashEchoBlocks(payload slackSlashCommandPayload, channelSlug, text string) []map[string]any {
	name := firstNonEmpty(payload.UserName, payload.UserID, "Slack")
	return []map[string]any{
		slackSectionBlock(fmt.Sprintf("*%s via /wuphf*\n%s", escapeSlackText(name), slackMrkdwnFromContent(text)), true),
		slackContextBlock("_Sent to WUPHF #" + escapeSlackText(normalizeChannelSlug(channelSlug)) + "_"),
	}
}

func slackMrkdwnFromContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if looksLikeKnownHTML(content) {
		content = htmlToSlackText(content)
	}
	return escapeSlackText(content)
}

var knownHTMLTagRe = regexp.MustCompile(`(?i)</?(p|br|strong|b|em|i|code|pre|ul|ol|li|blockquote|h[1-6]|div|span)\b`)
var htmlReplaceRules = []struct {
	re   *regexp.Regexp
	with string
}{
	{regexp.MustCompile(`(?i)<br\s*/?>`), "\n"},
	{regexp.MustCompile(`(?i)</p\s*>`), "\n\n"},
	{regexp.MustCompile(`(?i)<p\b[^>]*>`), ""},
	{regexp.MustCompile(`(?i)</?strong\b[^>]*>`), "*"},
	{regexp.MustCompile(`(?i)</?b\b[^>]*>`), "*"},
	{regexp.MustCompile(`(?i)</?em\b[^>]*>`), "_"},
	{regexp.MustCompile(`(?i)</?i\b[^>]*>`), "_"},
	{regexp.MustCompile("(?is)<pre\\b[^>]*>"), "```"},
	{regexp.MustCompile(`(?i)</pre\s*>`), "```"},
	{regexp.MustCompile("(?is)<code\\b[^>]*>"), "`"},
	{regexp.MustCompile(`(?i)</code\s*>`), "`"},
	{regexp.MustCompile(`(?i)<li\b[^>]*>`), "\n• "},
	{regexp.MustCompile(`(?i)</li\s*>`), ""},
	{regexp.MustCompile(`(?i)</h[1-6]\s*>`), "*\n"},
	{regexp.MustCompile(`(?i)<h[1-6]\b[^>]*>`), "*"},
	{regexp.MustCompile(`(?i)</(div|ul|ol|blockquote)\s*>`), "\n"},
	{regexp.MustCompile(`(?i)<(div|ul|ol|blockquote)\b[^>]*>`), ""},
	{regexp.MustCompile(`(?i)</?span\b[^>]*>`), ""},
}
var anyHTMLTagRe = regexp.MustCompile(`(?s)<[^>]+>`)

func looksLikeKnownHTML(content string) bool {
	return knownHTMLTagRe.MatchString(content)
}

func htmlToSlackText(content string) string {
	for _, rule := range htmlReplaceRules {
		content = rule.re.ReplaceAllString(content, rule.with)
	}
	content = anyHTMLTagRe.ReplaceAllString(content, "")
	content = html.UnescapeString(content)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func slackPayloadSummary(payload json.RawMessage) string {
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		return ""
	}
	object, ok := value.(map[string]any)
	if !ok || len(object) == 0 {
		return ""
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := []string{"*Card details*"}
	for _, key := range keys {
		if len(lines) >= 7 {
			break
		}
		rendered := slackRenderPayloadValue(object[key])
		if rendered == "" {
			continue
		}
		lines = append(lines, "• *"+escapeSlackText(key)+"*: "+escapeSlackText(rendered))
	}
	if len(lines) == 1 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func slackRenderPayloadValue(value any) string {
	switch v := value.(type) {
	case string:
		return truncateSlackText(strings.TrimSpace(v), 140)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprintf("%g", v)
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if s := slackRenderPayloadValue(item); s != "" {
				parts = append(parts, s)
			}
			if len(parts) >= 3 {
				break
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		if label, ok := v["label"].(string); ok {
			return truncateSlackText(strings.TrimSpace(label), 140)
		}
		if title, ok := v["title"].(string); ok {
			return truncateSlackText(strings.TrimSpace(title), 140)
		}
	}
	return ""
}

func appendSlackChannelFootnote(sb *strings.Builder, msg channelMessage) {
	footnote := slackChannelFootnote(msg)
	if footnote == "" {
		return
	}
	sb.WriteString("\n\n")
	sb.WriteString(footnote)
}

func slackChannelFootnote(msg channelMessage) string {
	channel := normalizeChannelSlug(msg.Channel)
	if channel == "" {
		return ""
	}
	if strings.TrimSpace(msg.ReplyTo) != "" {
		return "_Reply from WUPHF #" + escapeSlackText(channel) + "_"
	}
	return "_From WUPHF #" + escapeSlackText(channel) + "_"
}

func escapeSlackText(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func truncateSlackText(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func splitSlackStreamText(text string, max int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if max <= 0 {
		max = 11000
	}
	runes := []rune(text)
	out := make([]string, 0, (len(runes)/max)+1)
	for len(runes) > max {
		cut := max
		for i := max; i > max-500 && i > 0; i-- {
			if runes[i-1] == '\n' || runes[i-1] == ' ' {
				cut = i
				break
			}
		}
		out = append(out, strings.TrimSpace(string(runes[:cut])))
		runes = runes[cut:]
	}
	if tail := strings.TrimSpace(string(runes)); tail != "" {
		out = append(out, tail)
	}
	return out
}

func assistantTitleForText(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\n", " "))
	if text == "" {
		return ""
	}
	return truncateSlackText(text, 80)
}

var slackMentionRe = regexp.MustCompile(`<@[^>]+>`)

func normalizeSlackInboundText(text, botUserID string) string {
	text = strings.TrimSpace(text)
	if botUserID != "" {
		text = strings.ReplaceAll(text, "<@"+botUserID+">", "")
	}
	text = slackMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		return match
	})
	return strings.TrimSpace(text)
}

func sortedOfficeMembers(members []officeMember) []officeMember {
	out := append([]officeMember(nil), members...)
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}
