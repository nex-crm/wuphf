package team

import (
	"context"
	"encoding/json"
	"fmt"
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
		"blocks":  slackBlocksForText(msg.Text),
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
	slackChannelID := s.slackChannelIDForSlug(msg.Channel)
	if slackChannelID == "" {
		return transport.Outbound{}, false
	}
	if msg.Source == slackAdapterName {
		return transport.Outbound{}, false
	}
	text := formatSlackOutbound(msg)
	return transport.Outbound{
		Participant: transport.Participant{AdapterName: s.Name(), Key: msg.ID},
		Binding:     transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: slackChannelID},
		Text:        text,
		ThreadKey:   s.slackOutboundThread(msg.ReplyTo),
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
		reply := slackRestrictedChannelReply
		if err := s.streamAssistantReply(ctx, event.Channel, threadTS, reply); err != nil {
			log.Printf("[slack] assistant restricted reply failed: %v", err)
			_ = s.postRestrictedChannelReply(ctx, event.Channel, threadTS)
		}
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
	text := "wuphf " + strings.TrimSpace(payload.Text)
	s.handleCommandText(ctx, slackCommandContext{
		UserID:      payload.UserID,
		UserName:    payload.UserName,
		ChannelID:   payload.ChannelID,
		ChannelName: payload.ChannelName,
		ChannelSlug: channelSlug,
	}, text)
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
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return nil
	}
	payload := map[string]any{
		"channel": channelID,
		"text":    slackRestrictedChannelReply,
		"blocks":  slackBlocksForText(slackRestrictedChannelReply),
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

func appendSlackChannelFootnote(sb *strings.Builder, msg channelMessage) {
	channel := normalizeChannelSlug(msg.Channel)
	if channel == "" {
		return
	}
	if strings.TrimSpace(msg.ReplyTo) != "" {
		sb.WriteString("\n\n_Reply from WUPHF #")
	} else {
		sb.WriteString("\n\n_From WUPHF #")
	}
	sb.WriteString(escapeSlackText(channel))
	sb.WriteString("_")
}

func slackBlocksForText(text string) []map[string]any {
	return []map[string]any{{
		"type": "section",
		"text": map[string]any{
			"type": "mrkdwn",
			"text": truncateSlackText(text, 2800),
		},
	}}
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
