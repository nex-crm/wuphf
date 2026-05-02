package teammcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nex-crm/wuphf/internal/team"
)

func handleTeamBroadcast(ctx context.Context, _ *mcp.CallToolRequest, args TeamBroadcastArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	location := resolveConversationContext(ctx, slug, args.Channel, args.ReplyToID)
	channel := location.Channel
	replyTo := strings.TrimSpace(args.ReplyToID)
	if replyTo == "" && !args.NewTopic {
		replyTo = location.ReplyToID
	}

	if !isOneOnOneMode() {
		if messages, tasks, err := fetchBroadcastContext(ctx, channel, slug); err == nil {
			if reason := suppressBroadcastReason(slug, args.Content, replyTo, messages, tasks); reason != "" {
				return textResult(fmt.Sprintf("Held reply for @%s: %s. Poll again if the thread changes or if the CEO tags you in.", slug, reason)), nil, nil
			}
		}
	}

	// Auto-promote @-mentions in the body into the tagged array. Agents
	// routinely write "@operator please handle X" without setting tagged,
	// and the old path posted anyway + printed a warning nobody acted on
	// (so @operator never woke up). The intent is unambiguous — honor it.
	// The resulting post still passes through the broker's normal tagged
	// pipeline, so lastTaggedAt, typing indicator, and notification fanout
	// all fire correctly.
	effectiveTagged := args.Tagged
	var autoTagged []string
	if !isOneOnOneMode() {
		autoTagged = detectUntaggedMentions(args.Content, args.Tagged)
		if len(autoTagged) > 0 {
			effectiveTagged = append(append([]string{}, args.Tagged...), autoTagged...)
		}
	}

	var result struct {
		ID string `json:"id"`
	}
	err = brokerPostJSON(ctx, "/messages", map[string]any{
		"channel":  channel,
		"from":     slug,
		"content":  args.Content,
		"tagged":   effectiveTagged,
		"reply_to": replyTo,
	}, &result)
	if err != nil {
		return toolError(err), nil, nil
	}

	text := fmt.Sprintf("Posted to #%s as @%s", channel, slug)
	if isOneOnOneMode() {
		text = fmt.Sprintf("Sent direct reply to the human as @%s", slug)
	}
	if result.ID != "" {
		text += " (" + result.ID + ")"
	}
	if replyTo != "" {
		text += " in reply to " + replyTo
	}
	text += "."

	if len(autoTagged) > 0 {
		displayTagged := make([]string, 0, len(autoTagged))
		for _, tag := range autoTagged {
			displayTagged = append(displayTagged, "@"+strings.TrimLeft(tag, "@"))
		}
		text += fmt.Sprintf(
			" Auto-tagged %s from the body so they get woken; pass them explicitly in `tagged` next time to avoid this note.",
			strings.Join(displayTagged, ", "),
		)
	}

	return textResult(text), nil, nil
}

// detectUntaggedMentions returns @-slugs found in content that are not in the
// tagged list. Only slug-like words (alphanumeric + hyphen, 2-20 chars) are
// flagged to avoid false positives from conversational @-references.
func detectUntaggedMentions(content string, tagged []string) []string {
	taggedSet := make(map[string]struct{}, len(tagged))
	for _, t := range tagged {
		taggedSet[strings.ToLower(strings.TrimSpace(t))] = struct{}{}
	}
	seen := map[string]struct{}{}
	var out []string
	parts := strings.Fields(content)
	for _, p := range parts {
		if !strings.HasPrefix(p, "@") {
			continue
		}
		// Strip trailing punctuation
		raw := strings.TrimLeft(p, "@")
		raw = strings.TrimRight(raw, ".,;:!?)")
		raw = strings.ToLower(raw)
		if len(raw) < 2 || len(raw) > 20 {
			continue
		}
		// Only flag slug-like strings: alphanumeric + hyphens
		valid := true
		for _, r := range raw {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
				valid = false
				break
			}
		}
		if !valid {
			continue
		}
		// Skip common non-agent references
		switch raw {
		case "you", "human", "nex", "system", "everyone", "all", "team", "channel":
			continue
		}
		if _, inTagged := taggedSet[raw]; inTagged {
			continue
		}
		if _, already := seen[raw]; already {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func handleTeamReact(ctx context.Context, _ *mcp.CallToolRequest, args TeamReactArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	if args.MessageID == "" || args.Emoji == "" {
		return toolError(fmt.Errorf("message_id and emoji are required")), nil, nil
	}
	var result struct {
		OK        bool `json:"ok"`
		Duplicate bool `json:"duplicate"`
	}
	if err := brokerPostJSON(ctx, "/reactions", map[string]any{
		"message_id": args.MessageID,
		"emoji":      args.Emoji,
		"from":       slug,
	}, &result); err != nil {
		return toolError(err), nil, nil
	}
	if result.Duplicate {
		return textResult(fmt.Sprintf("Already reacted %s to %s.", args.Emoji, args.MessageID)), nil, nil
	}
	return textResult(fmt.Sprintf("Reacted %s to %s as @%s.", args.Emoji, args.MessageID, slug)), nil, nil
}

func fetchBroadcastContext(ctx context.Context, channel, mySlug string) ([]brokerMessage, []brokerTaskSummary, error) {
	msgValues := url.Values{}
	msgValues.Set("channel", channel)
	msgValues.Set("limit", "40")
	if mySlug != "" {
		msgValues.Set("my_slug", mySlug)
	}
	var messages brokerMessagesResponse
	if err := brokerGetJSON(ctx, "/messages?"+msgValues.Encode(), &messages); err != nil {
		return nil, nil, err
	}
	// Fetch tasks across ALL channels so ownsRelevantTask can find tasks that live
	// in dedicated channels (e.g. "engineering") even when the specialist broadcasts
	// into "general". Without all_channels=true, a specialist who completes work
	// cross-channel would be incorrectly suppressed.
	var tasks brokerTasksResponse
	if err := brokerGetJSON(ctx, "/tasks?all_channels=true", &tasks); err != nil {
		return messages.Messages, nil, err
	}
	return messages.Messages, tasks.Tasks, nil
}

func suppressBroadcastReason(slug, content, replyTo string, messages []brokerMessage, tasks []brokerTaskSummary) string {
	if strings.TrimSpace(slug) == "" || slug == "ceo" {
		return ""
	}
	threadRoot := threadRootForReply(replyTo, messages)
	myDomain := inferOfficeAgentDomain(slug)
	latest := latestRelevantMessage(messages, replyTo)
	latestDomain := inferOfficeTextDomain(content)
	if latestDomain == "general" && latest != nil {
		latestDomain = inferOfficeTextDomain(latest.Title + " " + latest.Content)
	}
	// An agent is explicitly needed if it was tagged in the latest relevant
	// message OR in any message in the thread (e.g. the root human message that
	// originally requested work from multiple agents in parallel).
	explicitNeed := latest != nil && containsSlug(latest.Tagged, slug)
	if !explicitNeed && replyTo != "" {
		for _, msg := range messages {
			if (msg.ID == replyTo || strings.TrimSpace(msg.ReplyTo) == replyTo) && containsSlug(msg.Tagged, slug) {
				explicitNeed = true
				break
			}
		}
	}
	ownsTask := ownsRelevantTask(slug, replyTo, threadRoot, latestDomain, tasks)

	if explicitNeed || ownsTask {
		return ""
	}
	// Safety net: only block hard domain mismatches
	if latestDomain != "" && latestDomain != "general" && myDomain != latestDomain {
		return "this is outside your domain"
	}
	return ""
}

func latestRelevantMessage(messages []brokerMessage, replyTo string) *brokerMessage {
	replyTo = strings.TrimSpace(replyTo)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if strings.HasPrefix(strings.TrimSpace(msg.Content), "[STATUS]") {
			continue
		}
		if replyTo != "" {
			if msg.ID != replyTo && strings.TrimSpace(msg.ReplyTo) != replyTo {
				continue
			}
		} else if strings.TrimSpace(msg.ReplyTo) != "" {
			continue
		}
		return &messages[i]
	}
	return nil
}

func threadRootForReply(replyTo string, messages []brokerMessage) string {
	replyTo = strings.TrimSpace(replyTo)
	if replyTo == "" {
		return ""
	}
	byID := make(map[string]brokerMessage, len(messages))
	for _, msg := range messages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			byID[id] = msg
		}
	}
	current := replyTo
	seen := map[string]struct{}{}
	for {
		if _, ok := seen[current]; ok {
			return current
		}
		seen[current] = struct{}{}
		msg, ok := byID[current]
		if !ok {
			return current
		}
		parent := strings.TrimSpace(msg.ReplyTo)
		if parent == "" {
			return current
		}
		current = parent
	}
}

func ownsRelevantTask(slug, replyTo, threadRoot, domain string, tasks []brokerTaskSummary) bool {
	slug = strings.TrimSpace(slug)
	replyTo = strings.TrimSpace(replyTo)
	threadRoot = strings.TrimSpace(threadRoot)
	now := time.Now()
	for _, task := range tasks {
		if strings.TrimSpace(task.Owner) != slug {
			continue
		}
		if !taskAllowsFollowUpBroadcast(task, replyTo, domain, now) {
			continue
		}
		if replyTo != "" {
			if strings.TrimSpace(task.ThreadID) == replyTo || (threadRoot != "" && strings.TrimSpace(task.ThreadID) == threadRoot) {
				return true
			}
			continue
		}
		taskDomain := inferOfficeTextDomain(task.Title + " " + task.Details)
		if taskDomain == domain || taskDomain == "general" || domain == "" {
			return true
		}
	}
	return false
}

const completedTaskBroadcastGrace = 15 * time.Minute

func taskAllowsFollowUpBroadcast(task brokerTaskSummary, replyTo, domain string, now time.Time) bool {
	status := strings.TrimSpace(task.Status)
	if !strings.EqualFold(status, "done") {
		return true
	}
	if replyTo != "" && strings.TrimSpace(task.ThreadID) == replyTo {
		return true
	}
	updatedAt := strings.TrimSpace(task.UpdatedAt)
	if updatedAt == "" {
		return false
	}
	updated, err := time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return false
	}
	if now.Sub(updated) > completedTaskBroadcastGrace {
		return false
	}
	taskDomain := inferOfficeTextDomain(task.Title + " " + task.Details)
	return taskDomain == domain || taskDomain == "general" || domain == ""
}

// inferOfficeAgentDomain and inferOfficeTextDomain are canonical wrappers around
// team.InferAgentDomain / team.InferTextDomain. All domain classification lives in
// team/domains.go — update keywords there and both packages stay in sync.

func inferOfficeAgentDomain(slug string) string { return team.InferAgentDomain(slug) }
func inferOfficeTextDomain(text string) string  { return team.InferTextDomain(text) }

func containsSlug(items []string, want string) bool {
	want = strings.TrimSpace(strings.ToLower(want))
	for _, item := range items {
		if strings.TrimSpace(strings.ToLower(item)) == want {
			return true
		}
	}
	return false
}

func handleTeamPoll(ctx context.Context, _ *mcp.CallToolRequest, args TeamPollArgs) (*mcp.CallToolResult, any, error) {
	channel := resolveConversationChannel(ctx, resolveSlugOptional(args.MySlug), args.Channel)
	values := url.Values{}
	values.Set("channel", channel)
	scope, err := normalizePollScope(args.Scope)
	if err != nil {
		return toolError(err), nil, nil
	}
	if slug := strings.TrimSpace(resolveSlugOptional(args.MySlug)); slug != "" {
		values.Set("my_slug", slug)
		applyAgentMessageScope(values, slug, scope)
	} else if scope != "" {
		values.Set("scope", scope)
	}
	if since := strings.TrimSpace(args.SinceID); since != "" {
		values.Set("since_id", since)
	}
	if args.Limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", args.Limit))
	}

	var result brokerMessagesResponse
	path := "/messages"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := brokerGetJSON(ctx, path, &result); err != nil {
		return toolError(err), nil, nil
	}

	summary := formatMessages(result.Messages, resolveSlugOptional(args.MySlug))
	if isOneOnOneMode() {
		if strings.TrimSpace(summary) == "" {
			return textResult("The 1:1 is quiet right now."), nil, nil
		}
		focus := latestHumanRequestSummary(result.Messages)
		if focus != "" {
			return textResult("Direct conversation\n\nLatest human request to answer now:\n" + focus + "\n\nOlder messages are background unless the latest request depends on them.\n\nRecent messages:\n" + summary), nil, nil
		}
		return textResult("Direct conversation\n\n" + summary), nil, nil
	}
	if scope == "inbox" || scope == "outbox" {
		scopeTitle := strings.ToUpper(scope[:1]) + scope[1:]
		if slug := strings.TrimSpace(resolveSlugOptional(args.MySlug)); slug != "" {
			return textResult(fmt.Sprintf("%s for @%s in #%s\n\n%s", scopeTitle, slug, channel, summary)), nil, nil
		}
		return textResult(fmt.Sprintf("%s in #%s\n\n%s", scopeTitle, channel, summary)), nil, nil
	}
	taskSummary := formatTaskSummary(ctx, resolveSlugOptional(args.MySlug), channel)
	requestSummary := formatRequestSummary(ctx, channel)
	return textResult(fmt.Sprintf("Channel #%s\n\n%s\n\nTagged messages for you: %d\n\n%s\n\n%s", channel, summary, result.TaggedCount, taskSummary, requestSummary)), nil, nil
}

func handleTeamInbox(ctx context.Context, req *mcp.CallToolRequest, args TeamPollArgs) (*mcp.CallToolResult, any, error) {
	args.Scope = "inbox"
	return handleTeamPoll(ctx, req, args)
}

func handleTeamOutbox(ctx context.Context, req *mcp.CallToolRequest, args TeamPollArgs) (*mcp.CallToolResult, any, error) {
	args.Scope = "outbox"
	return handleTeamPoll(ctx, req, args)
}

func handleTeamStatus(ctx context.Context, _ *mcp.CallToolRequest, args TeamStatusArgs) (*mcp.CallToolResult, any, error) {
	slug, err := resolveSlug(args.MySlug)
	if err != nil {
		return toolError(err), nil, nil
	}
	channel := resolveConversationChannel(ctx, slug, args.Channel)
	if err := brokerPostJSON(ctx, "/messages", map[string]any{
		"channel": channel,
		"from":    slug,
		"content": "[STATUS] " + args.Status,
		"tagged":  []string{},
	}, nil); err != nil {
		return toolError(err), nil, nil
	}
	return textResult(fmt.Sprintf("Updated #%s status for @%s: %s", channel, slug, args.Status)), nil, nil
}

func normalizePollScope(value string) (string, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "all", "channel":
		return "", nil
	case "agent", "inbox", "outbox":
		return strings.TrimSpace(strings.ToLower(value)), nil
	default:
		return "", fmt.Errorf("invalid scope %q", value)
	}
}

func applyAgentMessageScope(values url.Values, slug, scope string) {
	slug = strings.TrimSpace(slug)
	if slug == "" || slug == "ceo" || isOneOnOneMode() {
		return
	}
	values.Set("viewer_slug", slug)
	if scope == "" {
		scope = "agent"
	}
	values.Set("scope", scope)
}

func formatMessages(messages []brokerMessage, mySlug string) string {
	if len(messages) == 0 {
		return "No recent team messages."
	}
	lines := make([]string, 0, len(messages))
	for _, msg := range messages {
		ts := msg.Timestamp
		if len(ts) > 19 {
			ts = ts[11:19]
		}
		tagNote := ""
		if mySlug != "" && contains(msg.Tagged, mySlug) {
			tagNote = " [tagged you]"
		}
		threadNote := ""
		if msg.ReplyTo != "" {
			threadNote = " ↳ " + msg.ReplyTo
		}
		// Truncate content to avoid token explosion when agents return long code
		// blocks or reports. 800 chars is enough for context without burning tokens.
		// team_poll is background context; agents who need the full output can read
		// it directly from the thread via a targeted team_poll with thread_id.
		const pollContentLimit = 800
		if msg.Kind == "automation" || msg.From == "wuphf" || msg.From == "nex" {
			source := msg.Source
			if source == "" {
				source = "context_graph"
			}
			label := msg.SourceLabel
			if label == "" {
				label = "WUPHF"
			}
			title := ""
			if msg.Title != "" {
				title = msg.Title + ": "
			}
			content := msg.Content
			if len(content) > pollContentLimit {
				content = content[:pollContentLimit] + "…"
			}
			lines = append(lines, fmt.Sprintf("%s %s%s [%s/%s]: %s%s%s", ts, msg.ID, threadNote, label, source, title, content, tagNote))
			continue
		}
		content := msg.Content
		if len(content) > pollContentLimit {
			content = content[:pollContentLimit] + "…"
		}
		lines = append(lines, fmt.Sprintf("%s %s%s @%s: %s%s", ts, msg.ID, threadNote, msg.From, content, tagNote))
	}
	return strings.Join(lines, "\n")
}

func latestHumanRequestSummary(messages []brokerMessage) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		from := strings.TrimSpace(strings.ToLower(msg.From))
		if from != "you" && from != "human" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		ts := msg.Timestamp
		if len(ts) > 19 {
			ts = ts[11:19]
		}
		return fmt.Sprintf("%s %s @%s: %s", ts, msg.ID, msg.From, content)
	}
	return ""
}
