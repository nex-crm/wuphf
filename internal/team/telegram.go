package team

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

const (
	telegramAPIBase     = "https://api.telegram.org"
	telegramPollTimeout = 30 // seconds for long-poll
)

// telegramUpdate represents a single update from the Telegram Bot API.
type telegramUpdate struct {
	UpdateID int64        `json:"update_id"`
	Message  *telegramMsg `json:"message,omitempty"`
}

type telegramMsg struct {
	MessageID int64         `json:"message_id"`
	Chat      telegramChat  `json:"chat"`
	From      *telegramUser `json:"from,omitempty"`
	Text      string        `json:"text"`
	Date      int64         `json:"date"`
}

type telegramChat struct {
	ID    int64  `json:"id"`
	Title string `json:"title,omitempty"`
	Type  string `json:"type"` // "private", "group", "supergroup", "channel"
}

type telegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name,omitempty"`
	Username  string `json:"username,omitempty"`
}

type telegramAPIResponse struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Desc   string          `json:"description,omitempty"`
}

// compile-time assertion: TelegramTransport must satisfy transport.Transport.
var _ transport.Transport = (*TelegramTransport)(nil)

// TelegramTransport bridges Telegram chats with the office broker.
// Each mapped Telegram chat corresponds to an office channel with a
// "telegram" surface. Inbound Telegram messages are posted to the broker
// via transport.Host; outbound broker messages on surface channels are
// sent to Telegram via Send.
type TelegramTransport struct {
	BotToken string
	Broker   *Broker
	// ChatMap maps telegram chat_id (as string) -> office channel slug.
	ChatMap map[string]string
	// DMChannel is the office channel slug for direct messages (private chats).
	// When set, any private message to the bot routes to this channel.
	DMChannel string
	// UserMap maps telegram username (lowercase) -> office member slug.
	// If empty, display names are used verbatim as the "from" field.
	UserMap map[string]string
	client  *http.Client

	// chatMapMu protects ChatMap against concurrent reads from drainOutbound /
	// typingLoop and writes from routeInbound (learning new DM chats).
	chatMapMu sync.RWMutex

	// health fields — written by pollInbound, read by Health(). Protected by mu.
	mu            sync.Mutex
	healthState   transport.HealthState
	lastSuccessAt time.Time
	lastErr       error
}

// NewTelegramTransport creates a transport from the broker's surface channels.
// It reads TELEGRAM_BOT_TOKEN from the environment by default, but individual
// channels can override via their Surface.BotTokenEnv field.
func NewTelegramTransport(broker *Broker, botToken string) *TelegramTransport {
	chatMap := make(map[string]string)
	dmChannel := ""
	for _, ch := range broker.SurfaceChannels("telegram") {
		if ch.Surface == nil {
			continue
		}
		if ch.Surface.Mode == "private" || ch.Surface.RemoteID == "0" {
			dmChannel = ch.Slug
		} else if ch.Surface.RemoteID != "" {
			chatMap[ch.Surface.RemoteID] = ch.Slug
		}
	}
	return &TelegramTransport{
		BotToken:    botToken,
		Broker:      broker,
		ChatMap:     chatMap,
		DMChannel:   dmChannel,
		UserMap:     make(map[string]string),
		client:      &http.Client{Timeout: time.Duration(telegramPollTimeout+10) * time.Second},
		healthState: transport.HealthDisconnected,
	}
}

// Name returns "telegram" — the stable adapter name used as AdapterName in
// every Participant value this transport creates.
func (t *TelegramTransport) Name() string { return "telegram" }

// Binding returns an empty binding because a single TelegramTransport instance
// covers multiple channels via ChatMap. There is no single static ChannelSlug
// to declare here; the per-message channel is carried in each
// transport.Message.Binding constructed by routeInbound.
func (t *TelegramTransport) Binding() transport.Binding {
	return transport.Binding{}
}

// Health returns a point-in-time snapshot of adapter connectivity. O(1) — reads
// from cached fields updated by pollInbound.
func (t *TelegramTransport) Health() transport.Health {
	t.mu.Lock()
	defer t.mu.Unlock()
	return transport.Health{
		State:         t.healthState,
		LastSuccessAt: t.lastSuccessAt,
		LastError:     t.lastErr,
	}
}

// Run starts the bidirectional bridge and blocks until ctx is cancelled. Inbound
// Telegram messages are delivered to the office via host; outbound broker messages
// are polled from the broker queue and sent via Send. Implements transport.Transport.
func (t *TelegramTransport) Run(ctx context.Context, host transport.Host) error {
	if t.BotToken == "" {
		return fmt.Errorf("telegram bot token is empty")
	}
	if len(t.ChatMap) == 0 && t.DMChannel == "" {
		return fmt.Errorf("no telegram channels configured")
	}

	ctx2, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 2)
	go func() { errCh <- t.pollInbound(ctx2, host) }()
	go func() { errCh <- t.drainOutbound(ctx2) }()
	go t.typingLoop(ctx2)

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		cancel() // stop siblings
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

// Start is a compatibility shim for callers that predate the transport.Transport
// contract. It creates a brokerTransportHost and delegates to Run.
func (t *TelegramTransport) Start(ctx context.Context) error {
	host := &brokerTransportHost{broker: t.Broker}
	return t.Run(ctx, host)
}

// Send delivers one outbound message to the Telegram chat mapped to
// msg.Binding.ChannelSlug. Returns an error if no chat is mapped for that slug
// or if the Telegram API call fails. Implements transport.Transport.
func (t *TelegramTransport) Send(ctx context.Context, msg transport.Outbound) error {
	chatID := t.chatIDForSlug(msg.Binding.ChannelSlug)
	if chatID == "" {
		return fmt.Errorf("telegram: no chat mapped for channel %q", msg.Binding.ChannelSlug)
	}
	return t.sendMessageHTML(ctx, chatID, msg.Text)
}

// chatIDForSlug returns the Telegram chat_id string for the given office channel
// slug, or "" if no mapping exists.
func (t *TelegramTransport) chatIDForSlug(slug string) string {
	t.chatMapMu.RLock()
	defer t.chatMapMu.RUnlock()
	for chatID, s := range t.ChatMap {
		if s == slug && chatID != "0" {
			return chatID
		}
	}
	return ""
}

// pollInbound long-polls Telegram for new messages and routes them to the office
// via host. Updates health state on each successful or failed poll cycle.
func (t *TelegramTransport) pollInbound(ctx context.Context, host transport.Host) error {
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, err := t.getUpdates(ctx, offset)
		if err != nil {
			t.mu.Lock()
			t.healthState = transport.HealthDegraded
			t.lastErr = err
			t.mu.Unlock()
			log.Printf("[telegram] poll error: %v", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}

		t.mu.Lock()
		t.healthState = transport.HealthConnected
		t.lastSuccessAt = time.Now()
		t.lastErr = nil
		t.mu.Unlock()

		for _, upd := range updates {
			if upd.UpdateID >= offset {
				offset = upd.UpdateID + 1
			}
			if upd.Message == nil {
				continue
			}
			// Record every group/supergroup we see for /connect discovery.
			if upd.Message.Chat.Type == "group" || upd.Message.Chat.Type == "supergroup" {
				t.Broker.RecordTelegramGroup(upd.Message.Chat.ID, upd.Message.Chat.Title)
			}
			if upd.Message.Text == "" {
				continue
			}
			log.Printf("[telegram] inbound: chat=%d type=%s text=%q",
				upd.Message.Chat.ID, upd.Message.Chat.Type,
				upd.Message.Text[:min(len(upd.Message.Text), 50)])
			if err := t.routeInbound(ctx, host, upd.Message); err != nil {
				return err
			}
		}
	}
}

// routeInbound resolves the channel for msg, then delivers it to the office via
// host.UpsertParticipant + host.ReceiveMessage. Returns a non-nil error when
// the host signals a contract-level failure (e.g. ErrBindingChannelMissing);
// the caller (pollInbound) propagates this to Run which exits the transport.
func (t *TelegramTransport) routeInbound(ctx context.Context, host transport.Host, msg *telegramMsg) error {
	chatIDStr := strconv.FormatInt(msg.Chat.ID, 10)

	t.chatMapMu.Lock()
	channel, ok := t.ChatMap[chatIDStr]
	if !ok && msg.Chat.Type == "private" && t.DMChannel != "" {
		channel = t.DMChannel
		t.ChatMap[chatIDStr] = t.DMChannel
		ok = true
	}
	t.chatMapMu.Unlock()

	if !ok {
		log.Printf("[telegram] inbound: unmapped chat %s", chatIDStr)
		return nil
	}

	fromName := t.resolveUser(msg.From)
	key := "0"
	if msg.From != nil {
		key = strconv.FormatInt(msg.From.ID, 10)
	}

	p := transport.Participant{
		AdapterName: "telegram",
		Key:         key,
		DisplayName: fromName,
		Human:       true,
	}
	b := transport.Binding{
		Scope:       transport.ScopeChannel,
		ChannelSlug: channel,
	}

	if err := host.UpsertParticipant(ctx, p, b); err != nil {
		return fmt.Errorf("telegram upsert participant: %w", err)
	}
	if err := host.ReceiveMessage(ctx, transport.Message{
		Participant: p,
		Binding:     b,
		Text:        msg.Text,
		ExternalID:  strconv.FormatInt(msg.MessageID, 10),
	}); err != nil {
		return fmt.Errorf("telegram receive message: %w", err)
	}
	return nil
}

// drainOutbound periodically checks the broker's external queue and sends
// messages to the appropriate Telegram chats.
func (t *TelegramTransport) drainOutbound(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}

		// Rebuild reverse map each cycle (picks up dynamically added DM chats)
		t.chatMapMu.RLock()
		slugToChat := make(map[string]string, len(t.ChatMap))
		for chatID, slug := range t.ChatMap {
			if chatID == "0" {
				continue // skip the placeholder DM entry
			}
			slugToChat[slug] = chatID
		}
		t.chatMapMu.RUnlock()

		msgs := t.Broker.ExternalQueue("telegram")
		if len(msgs) > 0 {
			log.Printf("[telegram] outbound queue: %d message(s)", len(msgs))
		}
		for _, msg := range msgs {
			ch := normalizeChannelSlug(msg.Channel)
			chatID, ok := slugToChat[ch]
			if !ok {
				log.Printf("[telegram] outbound skip: no chat for channel %q", ch)
				continue
			}
			// Send typing indicator before the message (Telegram-specific UX).
			if chatIDInt, err := strconv.ParseInt(chatID, 10, 64); err == nil {
				_ = SendTypingAction(ctx, t.BotToken, chatIDInt)
			}
			out := transport.Outbound{
				Binding: transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: ch},
				Text:    formatTelegramOutbound(msg),
			}
			if err := t.Send(ctx, out); err != nil {
				// Transient send failure — message was already dequeued,
				// so we log and move on.
				log.Printf("[telegram] outbound send error for %q: %v", ch, err)
				continue
			}
		}
	}
}

// typingLoop periodically sends "typing" actions to Telegram chats when
// agents are actively processing (recently tagged and haven't replied yet).
func (t *TelegramTransport) typingLoop(ctx context.Context) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		// Check if any agents are "typing" (tagged within last 30s, no reply yet)
		if !t.Broker.HasRecentlyTaggedAgents(30 * time.Second) {
			continue
		}

		// Send typing to all mapped Telegram chats
		t.chatMapMu.RLock()
		chatIDs := make([]string, 0, len(t.ChatMap))
		for chatIDStr := range t.ChatMap {
			chatIDs = append(chatIDs, chatIDStr)
		}
		t.chatMapMu.RUnlock()
		for _, chatIDStr := range chatIDs {
			chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
			if err != nil {
				continue
			}
			_ = SendTypingAction(ctx, t.BotToken, chatID)
		}
	}
}

// HandleInbound processes an incoming Telegram message and posts it to the broker.
func (t *TelegramTransport) HandleInbound(chatID int64, chatType string, from *telegramUser, text string) error {
	chatIDStr := strconv.FormatInt(chatID, 10)
	channel, ok := t.ChatMap[chatIDStr]
	if !ok {
		// Check if this is a private/DM message
		if (chatType == "private") && t.DMChannel != "" {
			channel = t.DMChannel
			// Store the chat ID so we can reply to this user
			t.chatMapMu.Lock()
			t.ChatMap[chatIDStr] = t.DMChannel
			t.chatMapMu.Unlock()
		} else {
			return fmt.Errorf("unmapped telegram chat: %s", chatIDStr)
		}
	}

	fromName := t.resolveUser(from)
	_, err := t.Broker.PostInboundSurfaceMessage(fromName, channel, text, "telegram")
	return err
}

// SendToTelegram sends a broker message to the specified Telegram chat with HTML formatting.
func (t *TelegramTransport) SendToTelegram(ctx context.Context, chatID string, msg channelMessage) error {
	text := formatTelegramOutbound(msg)
	return t.sendMessageHTML(ctx, chatID, text)
}

// resolveUser maps a Telegram user to an office member slug.
func (t *TelegramTransport) resolveUser(user *telegramUser) string {
	if user == nil {
		return "unknown"
	}
	if user.Username != "" {
		lower := strings.ToLower(user.Username)
		if slug, ok := t.UserMap[lower]; ok {
			return slug
		}
	}
	// Fallback: use display name as-is
	name := strings.TrimSpace(user.FirstName)
	if user.LastName != "" {
		name += " " + strings.TrimSpace(user.LastName)
	}
	if name == "" {
		return "unknown"
	}
	return name
}

// formatTelegramOutbound formats a broker message as Telegram HTML.
func formatTelegramOutbound(msg channelMessage) string {
	switch {
	case msg.Kind == "skill_invocation":
		return fmt.Sprintf("⚡ <b>@%s</b> invoked a skill", escapeTelegramHTML(msg.From))

	case msg.Kind == "skill_proposal":
		return fmt.Sprintf("💡 <b>Skill proposed</b>: %s", escapeTelegramHTML(msg.Content))

	case msg.Kind == "automation":
		source := msg.Source
		if msg.SourceLabel != "" {
			source = msg.SourceLabel
		}
		if source == "" {
			source = "automation"
		}
		return fmt.Sprintf("🤖 <b>[%s]</b>: %s", escapeTelegramHTML(source), escapeTelegramHTML(msg.Content))

	case isHumanDecisionKind(msg.Kind):
		return formatTelegramDecision(msg)

	case msg.From == "system":
		return fmt.Sprintf("→ <i>%s</i>", escapeTelegramHTML(msg.Content))

	default:
		// Regular agent message
		var sb strings.Builder
		if msg.From != "" {
			sb.WriteString("<b>@")
			sb.WriteString(escapeTelegramHTML(msg.From))
			sb.WriteString("</b>: ")
		}
		if msg.Title != "" {
			sb.WriteString("[")
			sb.WriteString(escapeTelegramHTML(msg.Title))
			sb.WriteString("] ")
		}
		sb.WriteString(escapeTelegramHTML(msg.Content))
		return sb.String()
	}
}

// isHumanDecisionKind returns true for interview/decision message kinds.
func isHumanDecisionKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "interview", "approval", "confirm", "choice":
		return true
	}
	return strings.Contains(kind, "human")
}

// formatTelegramDecision formats a human decision/interview message.
func formatTelegramDecision(msg channelMessage) string {
	var sb strings.Builder
	sb.WriteString("📋 <b>Decision needed</b>")
	if msg.From != "" {
		sb.WriteString(" from @")
		sb.WriteString(escapeTelegramHTML(msg.From))
	}
	sb.WriteString("\n\n")
	sb.WriteString(escapeTelegramHTML(msg.Content))
	if msg.Title != "" {
		sb.WriteString("\n\n<i>")
		sb.WriteString(escapeTelegramHTML(msg.Title))
		sb.WriteString("</i>")
	}
	return sb.String()
}

// escapeTelegramHTML escapes characters that are special in Telegram HTML parse mode.
func escapeTelegramHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// getUpdates calls the Telegram getUpdates endpoint with long-polling.
func (t *TelegramTransport) getUpdates(ctx context.Context, offset int64) ([]telegramUpdate, error) {
	url := fmt.Sprintf("%s/bot%s/getUpdates?offset=%d&timeout=%d",
		telegramAPIBase, t.BotToken, offset, telegramPollTimeout)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var apiResp telegramAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("telegram json decode: %w", err)
	}
	if !apiResp.OK {
		return nil, fmt.Errorf("telegram api error: %s", apiResp.Desc)
	}

	var updates []telegramUpdate
	if err := json.Unmarshal(apiResp.Result, &updates); err != nil {
		return nil, fmt.Errorf("telegram updates decode: %w", err)
	}
	return updates, nil
}

// sendMessage calls the Telegram sendMessage endpoint (plain text).
func (t *TelegramTransport) sendMessage(ctx context.Context, chatID, text string) error {
	return t.sendMessageWithMode(ctx, chatID, text, "")
}

// sendMessageHTML calls the Telegram sendMessage endpoint with HTML parse mode.
func (t *TelegramTransport) sendMessageHTML(ctx context.Context, chatID, text string) error {
	return t.sendMessageWithMode(ctx, chatID, text, "HTML")
}

// sendMessageWithMode calls the Telegram sendMessage endpoint with an optional parse_mode.
//
// The 30s deadline is derived from the caller's ctx (typically the
// transport drainOutbound goroutine's ctx) so a transport shutdown
// cancels any in-flight send instead of letting it ride out the full 30s.
func (t *TelegramTransport) sendMessageWithMode(ctx context.Context, chatID, text, parseMode string) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", telegramAPIBase, t.BotToken)

	payload := map[string]string{
		"chat_id": chatID,
		"text":    text,
	}
	if parseMode != "" {
		payload["parse_mode"] = parseMode
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("telegram send read response: %w", err)
	}

	var apiResp telegramAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("telegram send decode: %w", err)
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram send error: %s", apiResp.Desc)
	}
	return nil
}

// SendTypingAction sends a "typing" chat action to a Telegram chat.
//
// The 30s deadline is derived from the caller's ctx — transport drain and
// typing loops pass their parent ctx so a transport shutdown cancels any
// in-flight chat-action call.
func SendTypingAction(ctx context.Context, token string, chatID int64) error {
	url := fmt.Sprintf("%s/bot%s/sendChatAction", telegramAPIBase, token)

	data, err := json.Marshal(map[string]any{
		"chat_id": chatID,
		"action":  "typing",
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("telegram typing: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram typing: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("telegram typing read: %w", err)
	}

	var apiResp telegramAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("telegram typing decode: %w", err)
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram typing error: %s", apiResp.Desc)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Exported helpers for the /connect telegram onboarding flow
// ---------------------------------------------------------------------------

// TelegramGroup represents a Telegram group discovered via getUpdates.
type TelegramGroup struct {
	ChatID int64
	Title  string
	Type   string // "group" or "supergroup"
}

// VerifyBot checks the bot token by calling getMe and returns the bot's display name.
func VerifyBot(token string) (string, error) {
	url := fmt.Sprintf("%s/bot%s/getMe", telegramAPIBase, token)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("telegram getMe: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("telegram getMe: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("telegram getMe read: %w", err)
	}

	var apiResp telegramAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("telegram getMe decode: %w", err)
	}
	if !apiResp.OK {
		return "", fmt.Errorf("telegram getMe error: %s", apiResp.Desc)
	}

	var bot struct {
		FirstName string `json:"first_name"`
		Username  string `json:"username"`
	}
	if err := json.Unmarshal(apiResp.Result, &bot); err != nil {
		return "", fmt.Errorf("telegram getMe result decode: %w", err)
	}
	name := bot.FirstName
	if name == "" {
		name = bot.Username
	}
	return name, nil
}

// DiscoverGroups calls getUpdates and extracts unique groups/supergroups
// the bot has received messages from.
func DiscoverGroups(token string) ([]TelegramGroup, error) {
	// Use offset=-100 to peek at recent updates without consuming them.
	// This way the transport's pollInbound doesn't lose messages.
	url := fmt.Sprintf("%s/bot%s/getUpdates?timeout=0&offset=-100", telegramAPIBase, token)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("telegram getUpdates: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("telegram getUpdates: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("telegram getUpdates read: %w", err)
	}

	var apiResp telegramAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("telegram getUpdates decode: %w", err)
	}
	if !apiResp.OK {
		return nil, fmt.Errorf("telegram getUpdates error: %s", apiResp.Desc)
	}

	var updates []telegramUpdate
	if err := json.Unmarshal(apiResp.Result, &updates); err != nil {
		return nil, fmt.Errorf("telegram updates decode: %w", err)
	}

	seen := make(map[int64]bool)
	var groups []TelegramGroup
	for _, upd := range updates {
		if upd.Message == nil {
			continue
		}
		chat := upd.Message.Chat
		if chat.Type != "group" && chat.Type != "supergroup" {
			continue
		}
		if seen[chat.ID] {
			continue
		}
		seen[chat.ID] = true
		groups = append(groups, TelegramGroup{
			ChatID: chat.ID,
			Title:  chat.Title,
			Type:   chat.Type,
		})
	}
	return groups, nil
}

// SendTelegramMessage sends a text message to a Telegram chat using the given bot token.
func SendTelegramMessage(token string, chatID int64, text string) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", telegramAPIBase, token)
	payload, err := json.Marshal(map[string]any{
		"chat_id": chatID,
		"text":    text,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("telegram send: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("telegram send read: %w", err)
	}

	var apiResp telegramAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("telegram send decode: %w", err)
	}
	if !apiResp.OK {
		return fmt.Errorf("telegram send error: %s", apiResp.Desc)
	}
	return nil
}

// VerifyChat checks if a chat ID is valid and returns its title.
func VerifyChat(token string, chatID int64) (string, error) {
	url := fmt.Sprintf("%s/bot%s/getChat?chat_id=%d", telegramAPIBase, token, chatID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("telegram getChat: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("telegram getChat: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var apiResp telegramAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("telegram getChat decode: %w", err)
	}
	if !apiResp.OK {
		return "", fmt.Errorf("chat not found: %s", apiResp.Desc)
	}
	var chat struct {
		Title string `json:"title"`
		Type  string `json:"type"`
	}
	if err := json.Unmarshal(apiResp.Result, &chat); err != nil {
		return "", nil
	}
	return chat.Title, nil
}

var telegramSlugRegexp = regexp.MustCompile(`[^a-z0-9]+`)

// SlugifyTelegramTitle is the canonical slug rule for Telegram-bridged channels.
// Both the TUI's `/connect telegram` and the web wizard route through this so
// the two paths can never produce different slugs for the same chat title.
func SlugifyTelegramTitle(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = telegramSlugRegexp.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "telegram"
	}
	return "tg-" + slug
}

// DiscoverGroupsFromBroker returns groups the transport has seen during polling.
// This is more reliable than getUpdates because the transport records every
// group it encounters, even after the updates are consumed.
func DiscoverGroupsFromBroker(broker *Broker) []TelegramGroup {
	seen := broker.SeenTelegramGroups()
	if len(seen) == 0 {
		return nil
	}
	groups := make([]TelegramGroup, 0, len(seen))
	for chatID, title := range seen {
		groups = append(groups, TelegramGroup{
			ChatID: chatID,
			Title:  title,
			Type:   "group",
		})
	}
	return groups
}
