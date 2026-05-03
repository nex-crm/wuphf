package teammcp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

func normalizeChannelInput(input string) string {
	channel := strings.TrimSpace(input)
	if channel == "" {
		return ""
	}
	channel = strings.ToLower(strings.ReplaceAll(channel, " ", "-"))
	return channel
}

func resolveChannelHint(input string) string {
	channel := normalizeChannelInput(input)
	if channel == "" {
		channel = normalizeChannelInput(os.Getenv("WUPHF_CHANNEL"))
	}
	if channel == "" {
		channel = normalizeChannelInput(os.Getenv("NEX_CHANNEL"))
	}
	return channel
}

func resolveChannel(input string) string {
	channel := resolveChannelHint(input)
	if channel == "" {
		channel = "general"
	}
	return channel
}

func resolveConversationChannel(ctx context.Context, slug string, requestedChannel string) string {
	return resolveConversationContext(ctx, slug, requestedChannel, "").Channel
}

func resolveConversationContext(ctx context.Context, slug, requestedChannel, requestedReplyTo string) conversationContext {
	channel := resolveChannelHint(requestedChannel)
	replyTo := strings.TrimSpace(requestedReplyTo)
	if channel != "" {
		if replyTo == "" {
			replyTo = defaultReplyTargetForChannel(ctx, slug, channel)
		}
		return conversationContext{Channel: channel, ReplyToID: replyTo, Source: "explicit_channel"}
	}

	if replyTo != "" {
		if located := findMessageContextByID(ctx, slug, replyTo); located.Channel != "" {
			located.ReplyToID = replyTo
			located.Source = "explicit_reply"
			return located
		}
	}

	if isOneOnOneMode() {
		channel = resolveChannel("")
		if replyTo == "" {
			replyTo = inferDirectReplyTarget(ctx, slug, channel)
		}
		return conversationContext{Channel: channel, ReplyToID: replyTo, Source: "direct_session"}
	}

	if inferred := inferRecentConversationContext(ctx, slug); inferred.Channel != "" {
		if replyTo != "" {
			inferred.ReplyToID = replyTo
		}
		if inferred.ReplyToID == "" {
			inferred.ReplyToID = defaultReplyTargetForChannel(ctx, slug, inferred.Channel)
		}
		return inferred
	}

	if inferred := inferTaskConversationContext(ctx, slug); inferred.Channel != "" {
		if replyTo != "" {
			inferred.ReplyToID = replyTo
		}
		if inferred.ReplyToID == "" {
			inferred.ReplyToID = defaultReplyTargetForChannel(ctx, slug, inferred.Channel)
		}
		return inferred
	}

	channel = resolveChannel("")
	if replyTo == "" {
		replyTo = defaultReplyTargetForChannel(ctx, slug, channel)
	}
	return conversationContext{Channel: channel, ReplyToID: replyTo, Source: "fallback"}
}

func fetchAccessibleChannels(ctx context.Context, slug string) []brokerChannelSummary {
	var result brokerChannelsResponse
	if err := brokerGetJSON(ctx, "/channels", &result); err != nil {
		return nil
	}
	slug = strings.TrimSpace(slug)
	if slug == "" || slug == "ceo" {
		return result.Channels
	}
	out := make([]brokerChannelSummary, 0, len(result.Channels))
	for _, ch := range result.Channels {
		if !contains(ch.Members, slug) || contains(ch.Disabled, slug) {
			continue
		}
		out = append(out, ch)
	}
	return out
}

func fetchChannelMessages(ctx context.Context, channel, slug, scope string, limit int) []brokerMessage {
	values := url.Values{}
	values.Set("channel", channel)
	if limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", limit))
	}
	slug = strings.TrimSpace(slug)
	if slug != "" {
		values.Set("my_slug", slug)
		values.Set("viewer_slug", slug)
		if strings.TrimSpace(scope) != "" {
			values.Set("scope", strings.TrimSpace(scope))
		}
	}
	var result brokerMessagesResponse
	path := "/messages?" + values.Encode()
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return nil
	}
	return result.Messages
}

func inferRecentConversationContext(ctx context.Context, slug string) conversationContext {
	channels := fetchAccessibleChannels(ctx, slug)
	var (
		best      conversationContext
		bestStamp time.Time
	)
	for _, ch := range channels {
		messages := fetchChannelMessages(ctx, ch.Slug, slug, "inbox", 40)
		if len(messages) == 0 {
			continue
		}
		candidate, stamp := latestRelevantMessageContext(messages, slug, ch.Slug)
		if candidate.Channel == "" || stamp.IsZero() {
			continue
		}
		if best.Channel == "" || stamp.After(bestStamp) {
			best = candidate
			bestStamp = stamp
		}
	}
	return best
}

func latestRelevantMessageContext(messages []brokerMessage, slug, fallbackChannel string) (conversationContext, time.Time) {
	byID := make(map[string]brokerMessage, len(messages))
	for _, msg := range messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			byID[id] = msg
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.TrimSpace(msg.From) == strings.TrimSpace(slug) {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(msg.Content), "[STATUS]") {
			continue
		}
		stamp, err := time.Parse(time.RFC3339, strings.TrimSpace(msg.Timestamp))
		if err != nil {
			continue
		}
		channel := normalizeChannelInput(msg.Channel)
		if channel == "" {
			channel = normalizeChannelInput(fallbackChannel)
		}
		if channel == "" {
			channel = "general"
		}
		return conversationContext{
			Channel:   channel,
			ReplyToID: threadTargetForMessage(msg, byID),
			Source:    "recent_message",
		}, stamp
	}
	return conversationContext{}, time.Time{}
}

func threadTargetForMessage(msg brokerMessage, byID map[string]brokerMessage) string {
	current := strings.TrimSpace(msg.ID)
	parent := strings.TrimSpace(msg.ReplyTo)
	if parent == "" {
		return current
	}
	seen := map[string]bool{}
	for parent != "" {
		if seen[parent] {
			return parent
		}
		seen[parent] = true
		next, ok := byID[parent]
		if !ok || strings.TrimSpace(next.ReplyTo) == "" {
			return parent
		}
		parent = strings.TrimSpace(next.ReplyTo)
	}
	return current
}

func inferTaskConversationContext(ctx context.Context, slug string) conversationContext {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return conversationContext{}
	}
	channels := fetchAccessibleChannels(ctx, slug)
	var (
		best      conversationContext
		bestStamp time.Time
	)
	for _, ch := range channels {
		values := url.Values{}
		values.Set("channel", ch.Slug)
		values.Set("viewer_slug", slug)
		values.Set("my_slug", slug)
		var result brokerTasksResponse
		if err := brokerGetJSON(ctx, "/tasks?"+values.Encode(), &result); err != nil {
			continue
		}
		for _, task := range result.Tasks {
			if !taskCountsAsRunning(task) {
				continue
			}
			stamp := parseLatestTaskTime(task)
			if best.Channel != "" && !stamp.After(bestStamp) {
				continue
			}
			best = conversationContext{
				Channel:   normalizeChannelInput(task.Channel),
				ReplyToID: strings.TrimSpace(task.ThreadID),
				Source:    "owned_task",
			}
			bestStamp = stamp
		}
	}
	return best
}

func parseLatestTaskTime(task brokerTaskSummary) time.Time {
	for _, raw := range []string{strings.TrimSpace(task.UpdatedAt), strings.TrimSpace(task.CreatedAt)} {
		if raw == "" {
			continue
		}
		if stamp, err := time.Parse(time.RFC3339, raw); err == nil {
			return stamp
		}
	}
	return time.Time{}
}

func findMessageContextByID(ctx context.Context, slug, messageID string) conversationContext {
	messageID = strings.TrimSpace(messageID)
	if messageID == "" {
		return conversationContext{}
	}
	for _, ch := range fetchAccessibleChannels(ctx, slug) {
		messages := fetchChannelMessages(ctx, ch.Slug, slug, "", 100)
		byID := make(map[string]brokerMessage, len(messages))
		for _, msg := range messages {
			if id := strings.TrimSpace(msg.ID); id != "" {
				byID[id] = msg
			}
		}
		if msg, ok := byID[messageID]; ok {
			return conversationContext{
				Channel:   ch.Slug,
				ReplyToID: threadTargetForMessage(msg, byID),
				Source:    "message_lookup",
			}
		}
	}
	return conversationContext{}
}

func findTaskContextByID(ctx context.Context, slug, taskID string) conversationContext {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return conversationContext{}
	}
	channels := fetchAccessibleChannels(ctx, slug)
	for _, ch := range channels {
		values := url.Values{}
		values.Set("channel", ch.Slug)
		if strings.TrimSpace(slug) != "" {
			values.Set("viewer_slug", strings.TrimSpace(slug))
		}
		values.Set("include_done", "true")
		var result brokerTasksResponse
		if err := brokerGetJSON(ctx, "/tasks?"+values.Encode(), &result); err != nil {
			continue
		}
		for _, task := range result.Tasks {
			if strings.TrimSpace(task.ID) == taskID {
				return conversationContext{
					Channel:   ch.Slug,
					ReplyToID: strings.TrimSpace(task.ThreadID),
					Source:    "task_lookup",
				}
			}
		}
	}
	return conversationContext{}
}

func defaultReplyTargetForChannel(ctx context.Context, slug, channel string) string {
	channel = resolveChannel(channel)
	if isOneOnOneMode() {
		return inferDirectReplyTarget(ctx, slug, channel)
	}
	if replyTo, err := inferReplyTarget(ctx, slug, channel); err == nil && strings.TrimSpace(replyTo) != "" {
		return strings.TrimSpace(replyTo)
	}
	if replyTo, err := inferDefaultThreadTarget(ctx, slug, channel); err == nil && strings.TrimSpace(replyTo) != "" {
		return strings.TrimSpace(replyTo)
	}
	return ""
}

func inferDirectReplyTarget(ctx context.Context, slug, channel string) string {
	messages := fetchChannelMessages(ctx, channel, slug, "", 40)
	if len(messages) == 0 {
		return ""
	}
	byID := make(map[string]brokerMessage, len(messages))
	for _, msg := range messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			byID[id] = msg
		}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.TrimSpace(msg.From) == strings.TrimSpace(slug) {
			continue
		}
		return threadTargetForMessage(msg, byID)
	}
	return ""
}

func inferReplyTarget(ctx context.Context, slug string, channel string) (string, error) {
	var result brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?channel="+url.QueryEscape(channel)+"&my_slug="+url.QueryEscape(slug)+"&limit=25", &result); err != nil {
		return "", err
	}
	byID := make(map[string]brokerMessage, len(result.Messages))
	for _, msg := range result.Messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			byID[id] = msg
		}
	}
	for i := len(result.Messages) - 1; i >= 0; i-- {
		msg := result.Messages[i]
		if msg.From == slug {
			continue
		}
		if !contains(msg.Tagged, slug) {
			continue
		}
		if !isRecentEnough(msg.Timestamp, 15*time.Minute) {
			continue
		}
		return threadTargetForMessage(msg, byID), nil
	}
	return "", nil
}

func inferDefaultThreadTarget(ctx context.Context, slug string, channel string) (string, error) {
	var result brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?channel="+url.QueryEscape(channel)+"&my_slug="+url.QueryEscape(slug)+"&limit=40", &result); err != nil {
		return "", err
	}
	byID := make(map[string]brokerMessage, len(result.Messages))
	for _, msg := range result.Messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			byID[id] = msg
		}
	}
	for i := len(result.Messages) - 1; i >= 0; i-- {
		msg := result.Messages[i]
		if msg.From == slug {
			continue
		}
		if strings.HasPrefix(msg.Content, "[STATUS]") {
			continue
		}
		if !isRecentEnough(msg.Timestamp, 20*time.Minute) {
			continue
		}
		return threadTargetForMessage(msg, byID), nil
	}
	return "", nil
}

func isRecentEnough(ts string, maxAge time.Duration) bool {
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	return time.Since(parsed) <= maxAge
}
