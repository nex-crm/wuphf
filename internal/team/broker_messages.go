package team

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (b *Broker) handleMessages(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		b.handlePostMessage(w, r)
	case http.MethodGet:
		b.handleGetMessages(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (b *Broker) handleNexNotifications(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Channel     string   `json:"channel"`
		EventID     string   `json:"event_id"`
		Title       string   `json:"title"`
		Content     string   `json:"content"`
		Tagged      []string `json:"tagged"`
		ReplyTo     string   `json:"reply_to"`
		Source      string   `json:"source"`
		SourceLabel string   `json:"source_label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	msg, duplicate, err := b.PostAutomationMessage("nex", body.Channel, body.Title, body.Content, body.EventID, body.Source, body.SourceLabel, body.Tagged, body.ReplyTo)
	if err != nil {
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":        msg.ID,
		"duplicate": duplicate,
	})
}

func (b *Broker) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		From    string   `json:"from"`
		Channel string   `json:"channel"`
		Kind    string   `json:"kind"`
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tagged  []string `json:"tagged"`
		ReplyTo string   `json:"reply_to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	if firstBlockingRequest(b.requests) != nil {
		b.mu.Unlock()
		http.Error(w, "request pending; answer required before chat resumes", http.StatusConflict)
		return
	}

	b.counter++
	channel := normalizeChannelSlug(body.Channel)
	if channel == "" {
		channel = "general"
	}
	// Auto-create DM conversations on first message (like Slack's conversations.open)
	if b.findChannelLocked(channel) == nil {
		if IsDMSlug(channel) {
			if dm := b.ensureDMConversationLocked(channel); dm != nil {
				channel = dm.Slug
			}
		} else if b.channelStore != nil {
			if _, ok := b.channelStore.GetBySlug(channel); !ok {
				b.mu.Unlock()
				http.Error(w, "channel not found", http.StatusNotFound)
				return
			}
		} else {
			b.mu.Unlock()
			http.Error(w, "channel not found", http.StatusNotFound)
			return
		}
	}
	if !b.canAccessChannelLocked(body.From, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	// Auto-promote @slug mentions in the body into the tagged array. If a
	// user or agent typed `@pm`, treat it as a tag — `extractMentionedSlugs`
	// already restricts to registered agent slugs, so conversational use of
	// an @ that doesn't match an agent is untouched. Previously this ran for
	// agent posts only, on the theory that humans might want @ to be merely
	// conversational. In practice humans expect every @agent to notify, and
	// the web composer does not always commit typed @-text into an explicit
	// tag chip.
	//
	// Senders allowed to auto-promote: empty / "you" / "human" (humans) and
	// any registered agent slug. Everything else — "system", "nex", future
	// synthetic senders — is excluded by default so automation posts do not
	// accidentally wake agents on every @-reference they quote.
	//
	// Exception: when the human explicitly tags the lead (CEO), do not
	// auto-promote OTHER agents mentioned in the body. Example:
	// "@ceo ask @reviewer to ..." — the human's intent is for CEO to route,
	// not for the broker to fan out in parallel. Without this guard the
	// reviewer gets notified twice (by auto-promote AND later by CEO's
	// explicit tag), spawning two turns with nearly identical answers.
	tagged := uniqueSlugs(body.Tagged)
	sender := normalizeActorSlug(body.From)
	isHuman := sender == "" || sender == "you" || sender == "human"
	leadSlug := officeLeadSlugFrom(b.members)
	mentionedSlugs := extractMentionedSlugs(body.Content)
	leadExplicitlyTagged := leadSlug != "" && containsString(tagged, leadSlug)
	if isHuman && !leadExplicitlyTagged && leadSlug != "" && containsString(mentionedSlugs, leadSlug) && b.findMemberLocked(leadSlug) != nil {
		tagged = append(tagged, leadSlug)
		leadExplicitlyTagged = true
	}
	suppressAutoPromote := isHuman && leadExplicitlyTagged
	if b.senderMayAutoPromoteLocked(sender) && !suppressAutoPromote {
		for _, slug := range mentionedSlugs {
			if slug == sender {
				continue
			}
			if b.findMemberLocked(slug) == nil {
				continue
			}
			if !containsString(tagged, slug) {
				tagged = append(tagged, slug)
			}
		}
	}
	for _, taggedSlug := range tagged {
		switch taggedSlug {
		case "you", "human", "system":
			continue
		}
		if b.findMemberLocked(taggedSlug) == nil {
			b.mu.Unlock()
			http.Error(w, "unknown tagged member", http.StatusBadRequest)
			return
		}
	}

	// Thread auto-tagging: when a HUMAN replies in a thread, notify all
	// other agents who have already participated. This keeps the team
	// aligned without requiring the human to re-tag on every reply.
	// Agent-to-agent auto-tagging is intentionally skipped: focus mode
	// routing (specialist → lead only) already handles that path, and
	// auto-tagging agent replies causes broadcast loops.
	replyTo := strings.TrimSpace(body.ReplyTo)
	isHumanSender := sender == "" || sender == "you" || sender == "human"
	if replyTo != "" && isHumanSender {
		threadRoot := replyTo
		threadParticipants := []string{}
		for _, existing := range b.messages {
			inThread := existing.ID == threadRoot || existing.ReplyTo == threadRoot
			if inThread && existing.From != body.From {
				// Include agents (skip "you"/"human" — they see via the web UI poll)
				if existing.From != "you" && existing.From != "human" && b.findMemberLocked(existing.From) != nil {
					threadParticipants = append(threadParticipants, existing.From)
				}
			}
		}
		tagged = uniqueSlugs(append(tagged, threadParticipants...))
	}

	// Dedup near-identical consecutive broadcasts from the same agent in the
	// same channel + thread within a short window. Observed symptom: a single
	// CEO turn emits 2-3 team_broadcast calls with the same content in
	// slightly different wording, each costing a full round-trip downstream.
	// The prompt tells the model "at most one broadcast per turn", but that
	// rule is routinely ignored; this is the broker-side safety net.
	//
	// Humans and system senders are exempt — this only fires for agent posts.
	if !isHuman && sender != "" && sender != "system" && sender != "nex" {
		if b.isDuplicateAgentBroadcastLocked(sender, channel, replyTo, body.Content) {
			b.counter--
			b.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":         "",
				"deduped":    true,
				"total":      len(b.messages),
				"suppressed": "duplicate broadcast from the same agent in the same thread within the dedup window",
			})
			return
		}
	}

	if humanSenderMayCancelInterviews(body.From) {
		b.cancelActiveHumanInterviewsLocked(body.From, "Human sent a new message; unanswered interview canceled.", channel, replyTo)
	}

	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      body.From,
		Channel:   channel,
		Kind:      strings.TrimSpace(body.Kind),
		Title:     strings.TrimSpace(body.Title),
		Content:   body.Content,
		Tagged:    tagged,
		ReplyTo:   replyTo,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)
	total := len(b.messages)

	// Track which agents were tagged — they should show "typing" immediately
	if len(msg.Tagged) > 0 && (msg.From == "you" || msg.From == "human") {
		if b.lastTaggedAt == nil {
			b.lastTaggedAt = make(map[string]time.Time)
		}
		for _, slug := range msg.Tagged {
			b.lastTaggedAt[slug] = time.Now()
		}
	}

	// Clear typing indicator when an agent posts a reply
	if msg.From != "you" && msg.From != "human" && b.lastTaggedAt != nil {
		delete(b.lastTaggedAt, msg.From)
	}

	if err := b.saveLocked(); err != nil {
		b.mu.Unlock()
		http.Error(w, "failed to persist broker state", http.StatusInternalServerError)
		return
	}
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":    msg.ID,
		"total": total,
	})
}

func (b *Broker) handleReactions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		MessageID string `json:"message_id"`
		Emoji     string `json:"emoji"`
		From      string `json:"from"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if body.MessageID == "" || body.Emoji == "" || body.From == "" {
		http.Error(w, "message_id, emoji, and from are required", http.StatusBadRequest)
		return
	}

	b.mu.Lock()
	found := false
	for i := range b.messages {
		if b.messages[i].ID == body.MessageID {
			// Don't duplicate: same emoji from same agent
			for _, r := range b.messages[i].Reactions {
				if r.Emoji == body.Emoji && r.From == body.From {
					b.mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "duplicate": true})
					return
				}
			}
			b.messages[i].Reactions = append(b.messages[i].Reactions, messageReaction{
				Emoji: body.Emoji,
				From:  body.From,
			})
			found = true
			break
		}
	}
	if !found {
		b.mu.Unlock()
		http.Error(w, "message not found", http.StatusNotFound)
		return
	}
	_ = b.saveLocked()
	b.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// RecordTelegramGroup saves a group chat ID and title seen by the transport.
func (b *Broker) RecordTelegramGroup(chatID int64, title string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seenTelegramGroups == nil {
		b.seenTelegramGroups = make(map[int64]string)
	}
	b.seenTelegramGroups[chatID] = title
}

// SeenTelegramGroups returns all group chats the transport has seen.
func (b *Broker) SeenTelegramGroups() map[int64]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seenTelegramGroups == nil {
		return nil
	}
	out := make(map[int64]string, len(b.seenTelegramGroups))
	for k, v := range b.seenTelegramGroups {
		out[k] = v
	}
	return out
}

// MarkRoutingTargets records implicit routing recipients as active so the UI
// can show typing/thinking state without persisting a routing banner message.
func (b *Broker) MarkRoutingTargets(slugs []string) {
	if len(slugs) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.lastTaggedAt == nil {
		b.lastTaggedAt = make(map[string]time.Time)
	}
	now := time.Now()
	for _, slug := range slugs {
		slug = strings.TrimSpace(slug)
		if slug == "" {
			continue
		}
		b.lastTaggedAt[slug] = now
	}
}

// PostSystemMessage posts a lightweight system message that shows progress without blocking.
func (b *Broker) PostSystemMessage(channel, content, kind string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counter++
	if channel == "" {
		channel = "general"
	}
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      "system",
		Channel:   normalizeChannelSlug(channel),
		Kind:      kind,
		Content:   content,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)
}

func (b *Broker) PostMessage(from, channel, content string, tagged []string, replyTo string) (channelMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if firstBlockingRequest(b.requests) != nil {
		return channelMessage{}, fmt.Errorf("request pending; answer required before chat resumes")
	}
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	if b.findChannelLocked(channel) == nil {
		if IsDMSlug(channel) {
			if dm := b.ensureDMConversationLocked(channel); dm != nil {
				channel = dm.Slug
			}
		}
		if b.findChannelLocked(channel) == nil {
			return channelMessage{}, fmt.Errorf("channel not found")
		}
	}
	if !b.canAccessChannelLocked(from, channel) {
		return channelMessage{}, fmt.Errorf("channel access denied")
	}
	if humanSenderMayCancelInterviews(from) {
		b.cancelActiveHumanInterviewsLocked(from, "Human sent a new message; unanswered interview canceled.", channel, replyTo)
	}
	b.counter++
	msg := channelMessage{
		ID:        fmt.Sprintf("msg-%d", b.counter),
		From:      from,
		Channel:   channel,
		Kind:      "",
		Title:     "",
		Content:   strings.TrimSpace(content),
		Tagged:    uniqueSlugs(tagged),
		ReplyTo:   strings.TrimSpace(replyTo),
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	b.appendMessageLocked(msg)
	// Clear typing indicator — agent has replied
	if b.lastTaggedAt != nil {
		delete(b.lastTaggedAt, msg.From)
	}
	b.appendActionLocked("automation", msg.Source, channel, msg.From, truncateSummary(msg.Title+" "+msg.Content, 140), msg.ID)
	if err := b.saveLocked(); err != nil {
		return channelMessage{}, err
	}
	return msg, nil
}

func (b *Broker) PostAutomationMessage(from, channel, title, content, eventID, source, sourceLabel string, tagged []string, replyTo string) (channelMessage, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if strings.TrimSpace(eventID) != "" {
		for _, existing := range b.messages {
			if existing.EventID != "" && existing.EventID == strings.TrimSpace(eventID) {
				return existing, true, nil
			}
		}
	}

	b.counter++
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	msg := channelMessage{
		ID:          fmt.Sprintf("msg-%d", b.counter),
		From:        from,
		Channel:     channel,
		Kind:        "automation",
		Source:      strings.TrimSpace(source),
		SourceLabel: strings.TrimSpace(sourceLabel),
		EventID:     strings.TrimSpace(eventID),
		Title:       strings.TrimSpace(title),
		Content:     strings.TrimSpace(content),
		Tagged:      tagged,
		ReplyTo:     strings.TrimSpace(replyTo),
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
	}
	if msg.Source == "" {
		msg.Source = "context_graph"
	}
	if msg.SourceLabel == "" {
		msg.SourceLabel = "Nex"
	}
	if msg.From == "" {
		msg.From = "nex"
	}

	b.appendMessageLocked(msg)
	if err := b.saveLocked(); err != nil {
		return channelMessage{}, false, err
	}
	return msg, false, nil
}

func (b *Broker) CreateRequest(req humanInterview) (humanInterview, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel := normalizeChannelSlug(req.Channel)
	if channel == "" {
		channel = "general"
	}
	if b.findChannelLocked(channel) == nil {
		return humanInterview{}, fmt.Errorf("channel not found")
	}
	b.counter++
	now := time.Now().UTC().Format(time.RFC3339)
	req.ID = fmt.Sprintf("request-%d", b.counter)
	req.Channel = channel
	req.CreatedAt = now
	req.UpdatedAt = now
	req.Kind = normalizeRequestKind(req.Kind)
	req.Options, req.RecommendedID = normalizeRequestOptions(req.Kind, req.RecommendedID, req.Options)
	if requestIsHumanInterview(req) {
		req.Blocking = false
		req.Required = false
	}
	if strings.TrimSpace(req.Status) == "" {
		req.Status = "pending"
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = "Request"
	}
	b.requests = append(b.requests, req)
	b.pendingInterview = firstBlockingRequest(b.requests)
	b.appendActionLocked("request_created", "office", channel, req.From, truncateSummary(req.Title+" "+req.Question, 140), req.ID)
	if err := b.saveLocked(); err != nil {
		return humanInterview{}, err
	}
	return req, nil
}

func (b *Broker) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 10
	if l, err := strconv.Atoi(q.Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if limit > 100 {
		limit = 100
	}

	sinceID := q.Get("since_id")
	mySlug := strings.TrimSpace(q.Get("my_slug"))
	viewerSlug := strings.TrimSpace(q.Get("viewer_slug"))
	threadID := strings.TrimSpace(q.Get("thread_id"))
	if threadID == "" {
		threadID = strings.TrimSpace(q.Get("reply_to"))
	}
	scope := normalizeMessageScope(q.Get("scope"))
	if rawScope := strings.TrimSpace(q.Get("scope")); rawScope != "" && scope == "" {
		http.Error(w, "invalid message scope", http.StatusBadRequest)
		return
	}
	channel := normalizeChannelSlug(q.Get("channel"))
	if channel == "" {
		channel = "general"
	}
	accessSlug := mySlug
	if accessSlug == "" {
		accessSlug = viewerSlug
	}

	b.mu.Lock()
	// Auto-create DM conversation on read (user opens DM before sending)
	if IsDMSlug(channel) && b.findChannelLocked(channel) == nil {
		if dm := b.ensureDMConversationLocked(channel); dm != nil {
			channel = dm.Slug
		}
	}
	if !b.canAccessChannelLocked(accessSlug, channel) {
		b.mu.Unlock()
		http.Error(w, "channel access denied", http.StatusForbidden)
		return
	}
	channelMessages := make([]channelMessage, 0, len(b.messages))
	for _, msg := range b.messages {
		if normalizeChannelSlug(msg.Channel) != channel {
			continue
		}
		channelMessages = append(channelMessages, msg)
	}
	messageIndex := make(map[string]channelMessage, len(channelMessages))
	for _, msg := range channelMessages {
		if id := strings.TrimSpace(msg.ID); id != "" {
			messageIndex[id] = msg
		}
	}
	messages := make([]channelMessage, 0, len(channelMessages))
	for _, msg := range channelMessages {
		if b.sessionMode == SessionModeOneOnOne && !b.isOneOnOneDMMessage(msg) {
			continue
		}
		if threadID != "" && !messageInThread(msg, threadID) {
			continue
		}
		if scope != "" && viewerSlug != "" && !messageMatchesViewerScope(msg, viewerSlug, scope, messageIndex) {
			continue
		}
		messages = append(messages, msg)
	}
	if sinceID != "" {
		for i, m := range messages {
			if m.ID == sinceID {
				messages = messages[i+1:]
				break
			}
		}
	}
	if len(messages) > limit {
		messages = messages[len(messages)-limit:]
	}
	// Copy to avoid race
	result := make([]channelMessage, len(messages))
	copy(result, messages)
	b.mu.Unlock()

	taggedCount := 0
	taggedSlug := mySlug
	if taggedSlug == "" {
		taggedSlug = viewerSlug
	}
	if taggedSlug != "" {
		for _, m := range result {
			for _, t := range m.Tagged {
				if t == taggedSlug {
					taggedCount++
					break
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"channel":      channel,
		"messages":     result,
		"tagged_count": taggedCount,
	})
}

func messageInThread(msg channelMessage, threadID string) bool {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return true
	}
	return strings.TrimSpace(msg.ID) == threadID || strings.TrimSpace(msg.ReplyTo) == threadID
}

func normalizeMessageScope(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", "all", "channel":
		return ""
	case "agent", "inbox", "outbox":
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return ""
	}
}

func messageMatchesViewerScope(msg channelMessage, viewerSlug, scope string, messagesByID map[string]channelMessage) bool {
	scope = normalizeMessageScope(scope)
	switch scope {
	case "inbox":
		return messageBelongsToViewerInbox(msg, viewerSlug, messagesByID)
	case "outbox":
		return messageBelongsToViewerOutbox(msg, viewerSlug)
	case "agent":
		return messageVisibleToViewer(msg, viewerSlug, messagesByID)
	default:
		return true
	}
}

func messageVisibleToViewer(msg channelMessage, viewerSlug string, messagesByID map[string]channelMessage) bool {
	return messageBelongsToViewerOutbox(msg, viewerSlug) || messageBelongsToViewerInbox(msg, viewerSlug, messagesByID)
}

func messageBelongsToViewerOutbox(msg channelMessage, viewerSlug string) bool {
	viewerSlug = strings.TrimSpace(viewerSlug)
	if viewerSlug == "" || viewerSlug == "ceo" {
		return true
	}
	return strings.TrimSpace(msg.From) == viewerSlug
}

func messageBelongsToViewerInbox(msg channelMessage, viewerSlug string, messagesByID map[string]channelMessage) bool {
	viewerSlug = strings.TrimSpace(viewerSlug)
	if viewerSlug == "" || viewerSlug == "ceo" {
		return true
	}
	from := strings.TrimSpace(msg.From)
	switch from {
	case viewerSlug:
		return false
	case "you", "human", "ceo":
		return true
	}
	for _, tagged := range msg.Tagged {
		tagged = strings.TrimSpace(tagged)
		if tagged == viewerSlug || tagged == "all" {
			return true
		}
	}
	return messageRepliesToViewerThread(msg, viewerSlug, messagesByID)
}

func messageRepliesToViewerThread(msg channelMessage, viewerSlug string, messagesByID map[string]channelMessage) bool {
	replyTo := strings.TrimSpace(msg.ReplyTo)
	if replyTo == "" || viewerSlug == "" {
		return false
	}
	seen := map[string]bool{}
	for replyTo != "" {
		if seen[replyTo] {
			return false
		}
		seen[replyTo] = true
		parent, ok := messagesByID[replyTo]
		if !ok {
			return false
		}
		if strings.TrimSpace(parent.From) == viewerSlug {
			return true
		}
		replyTo = strings.TrimSpace(parent.ReplyTo)
	}
	return false
}

// isOneOnOneDMMessage returns true if msg belongs in the 1:1 DM conversation.
// Only messages exclusively between the human and the 1:1 agent pass through.
// Caller must hold b.mu.
func (b *Broker) isOneOnOneDMMessage(msg channelMessage) bool {
	agent := b.oneOnOneAgent

	switch msg.From {
	case "you", "human":
		// Human messages: only if untagged (direct conversation) or
		// explicitly tagging the 1:1 agent.
		if len(msg.Tagged) == 0 {
			return true
		}
		for _, t := range msg.Tagged {
			if t == agent {
				return true
			}
		}
		return false

	case agent:
		// Agent messages: only if untagged (direct reply to human) or
		// explicitly tagging the human.
		if len(msg.Tagged) == 0 {
			return true
		}
		for _, t := range msg.Tagged {
			if t == "you" || t == "human" {
				return true
			}
		}
		return false

	case "system":
		// System messages: only if they mention the 1:1 agent or human,
		// or are general system announcements (no routing indicators).
		if msg.Kind == "routing" {
			return false
		}
		return true

	default:
		// Messages from any other agent do not belong in this DM.
		return false
	}
}

// capturePaneActivity captures tmux pane content for each agent and detects
// activity by comparing with the previous snapshot. If content changed,
// the agent is active and we return the last 5 non-empty lines as a stream.

func FormatChannelView(messages []channelMessage) string {
	if len(messages) == 0 {
		return "  No messages yet. The team is getting set up..."
	}

	var sb strings.Builder
	for _, m := range messages {
		ts := m.Timestamp
		if len(ts) > 19 {
			ts = ts[11:19]
		}

		prefix := m.From
		if m.Kind == "automation" || m.From == "nex" {
			source := m.Source
			if source == "" {
				source = "context_graph"
			}
			title := m.Title
			if title != "" {
				title += ": "
			}
			sb.WriteString(fmt.Sprintf("  %s  Nex/%s: %s%s\n", ts, source, title, m.Content))
			continue
		}
		if strings.HasPrefix(m.Content, "[STATUS]") {
			sb.WriteString(fmt.Sprintf("  %s  @%s %s%s\n", ts, prefix, m.Content, formatMessageUsageSuffix(m.Usage)))
		} else {
			thread := ""
			if m.ReplyTo != "" {
				thread = fmt.Sprintf(" ↳ %s", m.ReplyTo)
			}
			sb.WriteString(fmt.Sprintf("  %s%s  @%s: %s%s\n", ts, thread, prefix, m.Content, formatMessageUsageSuffix(m.Usage)))
		}
	}
	return sb.String()
}

func formatMessageUsageSuffix(usage *messageUsage) string {
	if usage == nil {
		return ""
	}
	total := usage.TotalTokens
	if total == 0 {
		total = usage.InputTokens + usage.OutputTokens + usage.CacheReadTokens + usage.CacheCreationTokens
	}
	if total == 0 {
		return ""
	}
	return fmt.Sprintf(" [%d tok]", total)
}

// --------------- Skills ---------------

// dirExists returns true if path exists and is a directory.
