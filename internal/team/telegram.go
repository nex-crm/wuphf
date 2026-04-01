package team

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	telegramAPIBase    = "https://api.telegram.org"
	telegramPollTimeout = 30 // seconds for long-poll
)

// telegramUpdate represents a single update from the Telegram Bot API.
type telegramUpdate struct {
	UpdateID int64           `json:"update_id"`
	Message  *telegramMsg    `json:"message,omitempty"`
}

type telegramMsg struct {
	MessageID int64          `json:"message_id"`
	Chat      telegramChat   `json:"chat"`
	From      *telegramUser  `json:"from,omitempty"`
	Text      string         `json:"text"`
	Date      int64          `json:"date"`
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
	OK     bool              `json:"ok"`
	Result json.RawMessage   `json:"result,omitempty"`
	Desc   string            `json:"description,omitempty"`
}

// TelegramTransport bridges Telegram chats with the office broker.
// Each mapped Telegram chat corresponds to an office channel with a
// "telegram" surface. Inbound Telegram messages are posted to the broker;
// outbound broker messages on surface channels are sent to Telegram.
type TelegramTransport struct {
	BotToken  string
	Broker    *Broker
	// ChatMap maps telegram chat_id (as string) -> office channel slug
	ChatMap   map[string]string
	// DMChannel is the office channel slug for direct messages (private chats).
	// When set, any private message to the bot routes to this channel.
	DMChannel string
	// UserMap maps telegram username (lowercase) -> office member slug.
	// If empty, display names are used verbatim as the "from" field.
	UserMap   map[string]string
	client    *http.Client
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
		BotToken:  botToken,
		Broker:    broker,
		ChatMap:   chatMap,
		DMChannel: dmChannel,
		UserMap:   make(map[string]string),
		client:    &http.Client{Timeout: time.Duration(telegramPollTimeout+10) * time.Second},
	}
}

// Start begins the bidirectional bridge: polling Telegram for inbound messages
// and draining the broker's external queue for outbound delivery.
// It blocks until ctx is cancelled.
func (t *TelegramTransport) Start(ctx context.Context) error {
	if t.BotToken == "" {
		return fmt.Errorf("telegram bot token is empty")
	}
	if len(t.ChatMap) == 0 && t.DMChannel == "" {
		return fmt.Errorf("no telegram channels configured")
	}

	errCh := make(chan error, 2)
	go func() { errCh <- t.pollInbound(ctx) }()
	go func() { errCh <- t.drainOutbound(ctx) }()
	go t.typingLoop(ctx)

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// pollInbound long-polls Telegram for new messages and routes them to the broker.
func (t *TelegramTransport) pollInbound(ctx context.Context) error {
	var offset int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		updates, err := t.getUpdates(ctx, offset)
		if err != nil {
			fmt.Printf("[telegram] poll error: %v\n", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}

		for _, upd := range updates {
			if upd.UpdateID >= offset {
				offset = upd.UpdateID + 1
			}
			if upd.Message == nil || upd.Message.Text == "" {
				continue
			}
			fmt.Printf("[telegram] inbound: chat=%d type=%s from=%s text=%q\n",
				upd.Message.Chat.ID, upd.Message.Chat.Type,
				upd.Message.From.FirstName, upd.Message.Text[:min(len(upd.Message.Text), 50)])
			if err := t.HandleInbound(upd.Message.Chat.ID, upd.Message.Chat.Type, upd.Message.From, upd.Message.Text); err != nil {
				fmt.Printf("[telegram] inbound error: %v\n", err)
				continue
			}
		}
	}
}

// drainOutbound periodically checks the broker's external queue and sends
// messages to the appropriate Telegram chats.
func (t *TelegramTransport) drainOutbound(ctx context.Context) error {
	// Reverse map: channel slug -> chat_id
	slugToChat := make(map[string]string, len(t.ChatMap))
	for chatID, slug := range t.ChatMap {
		slugToChat[slug] = chatID
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}

		msgs := t.Broker.ExternalQueue("telegram")
		if len(msgs) > 0 {
			fmt.Printf("[telegram] outbound queue: %d messages, slugToChat=%v\n", len(msgs), slugToChat)
		}
		for _, msg := range msgs {
			ch := normalizeChannelSlug(msg.Channel)
			chatID, ok := slugToChat[ch]
			if !ok {
				fmt.Printf("[telegram] outbound skip: no chat for channel %q\n", ch)
				continue
			}
			// Send typing indicator before the message
			if chatIDInt, err := strconv.ParseInt(chatID, 10, 64); err == nil {
				_ = SendTypingAction(t.BotToken, chatIDInt)
			}
			if err := t.SendToTelegram(chatID, msg); err != nil {
				// Transient send failure — message was already dequeued,
				// so we log and move on. In a future version we could
				// implement retry with dead-letter semantics.
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
		for chatIDStr := range t.ChatMap {
			chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
			if err != nil {
				continue
			}
			_ = SendTypingAction(t.BotToken, chatID)
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
			t.ChatMap[chatIDStr] = t.DMChannel
		} else {
			return fmt.Errorf("unmapped telegram chat: %s", chatIDStr)
		}
	}

	fromName := t.resolveUser(from)
	_, err := t.Broker.PostInboundSurfaceMessage(fromName, channel, text, "telegram")
	return err
}

// SendToTelegram sends a broker message to the specified Telegram chat with HTML formatting.
func (t *TelegramTransport) SendToTelegram(chatID string, msg channelMessage) error {
	text := formatTelegramOutbound(msg)
	return t.sendMessageHTML(chatID, text)
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
func (t *TelegramTransport) sendMessage(chatID, text string) error {
	return t.sendMessageWithMode(chatID, text, "")
}

// sendMessageHTML calls the Telegram sendMessage endpoint with HTML parse mode.
func (t *TelegramTransport) sendMessageHTML(chatID, text string) error {
	return t.sendMessageWithMode(chatID, text, "HTML")
}

// sendMessageWithMode calls the Telegram sendMessage endpoint with an optional parse_mode.
func (t *TelegramTransport) sendMessageWithMode(chatID, text, parseMode string) error {
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

	resp, err := t.client.Post(url, "application/json", bytes.NewReader(data))
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
func SendTypingAction(token string, chatID int64) error {
	url := fmt.Sprintf("%s/bot%s/sendChatAction", telegramAPIBase, token)

	data, err := json.Marshal(map[string]any{
		"chat_id": chatID,
		"action":  "typing",
	})
	if err != nil {
		return err
	}

	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
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
	resp, err := http.Get(url)
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
	resp, err := http.Get(url)
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
	resp, err := http.Post(url, "application/json", bytes.NewReader(payload))
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
	resp, err := http.Get(url)
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
