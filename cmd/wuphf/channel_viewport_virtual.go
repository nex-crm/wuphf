package main

import "strings"

var renderOfficeMessageBlockFn = renderOfficeMessageBlock

func buildVirtualizedOfficeViewport(messages []brokerMessage, expanded map[string]bool, contentWidth, msgH, scroll int, threadsDefaultExpand bool, unreadAnchorID string, unreadCount int, tail []renderedLine) []renderedLine {
	limit := msgH + scroll
	if limit < 1 {
		limit = 1
	}

	if len(messages) == 0 {
		lines := append(buildOfficeMessageLines(messages, expanded, contentWidth, threadsDefaultExpand, unreadAnchorID, unreadCount), tail...)
		if len(lines) > limit {
			return cloneRenderedLines(lines[len(lines)-limit:])
		}
		return lines
	}

	threaded := cachedThreadedMessages(messages, expanded, threadsDefaultExpand)
	collected := trimRenderedTail(tail, limit)
	if len(collected) >= limit {
		return collected
	}

	selected := make([][]renderedLine, 0, minInt(len(threaded), limit))
	total := len(collected)
	for i := len(threaded) - 1; i >= 0 && total < limit; i-- {
		block := cachedThreadedMessageBlock(threaded[i], contentWidth, unreadAnchorID, unreadCount)
		selected = append(selected, block)
		total += len(block)
	}
	if total < limit {
		selected = append(selected, cachedViewportTextBlock(contentWidth, "Today"))
	}

	out := make([]renderedLine, 0, total)
	for i := len(selected) - 1; i >= 0; i-- {
		out = append(out, selected[i]...)
	}
	out = append(out, collected...)
	if len(out) > limit {
		return cloneRenderedLines(out[len(out)-limit:])
	}
	return out
}

func cachedThreadedMessages(messages []brokerMessage, expanded map[string]bool, threadsDefaultExpand bool) []threadedMessage {
	ensureCollapsedThreadDefaults(messages, expanded, threadsDefaultExpand)
	h := newStateHasher()
	h.add("threaded-messages")
	h.addBool(threadsDefaultExpand)
	h.addMessages(messages)
	h.addExpandedThreads(expanded)
	key := h.sum()
	if cached, ok := channelRenderCache.getThreaded(key); ok {
		return cached
	}
	threaded := flattenThreadMessages(messages, expanded)
	channelRenderCache.putThreaded(key, threaded)
	return cloneThreadedMessages(threaded)
}

func ensureCollapsedThreadDefaults(messages []brokerMessage, expanded map[string]bool, threadsDefaultExpand bool) {
	if threadsDefaultExpand {
		return
	}
	for _, msg := range messages {
		if msg.ReplyTo != "" || !hasThreadReplies(messages, msg.ID) {
			continue
		}
		if _, ok := expanded[msg.ID]; !ok {
			expanded[msg.ID] = false
		}
	}
}

func cachedThreadedMessageBlock(tm threadedMessage, contentWidth int, unreadAnchorID string, unreadCount int) []renderedLine {
	h := newStateHasher()
	h.add("threaded-block")
	h.addInt(contentWidth)
	msg := tm.Message
	h.add(msg.ID, msg.From, msg.Kind, msg.Source, msg.Title, msg.ReplyTo, msg.Timestamp, msg.Content)
	h.add(strings.Join(msg.Tagged, ","))
	for _, reaction := range msg.Reactions {
		h.add(reaction.Emoji, reaction.From)
	}
	h.addInt(tm.Depth)
	h.add(tm.ParentLabel)
	h.addBool(tm.Collapsed)
	h.addInt(tm.HiddenReplies)
	h.add(strings.Join(tm.ThreadParticipants, ","))
	if msg.ID == unreadAnchorID {
		h.add(unreadAnchorID)
		h.addInt(unreadCount)
	}
	key := h.sum()
	if cached, ok := channelRenderCache.getViewportBlock(key); ok {
		return cached
	}
	lines := renderOfficeMessageBlockFn(tm, contentWidth, unreadAnchorID, unreadCount)
	channelRenderCache.putViewportBlock(key, lines)
	return cloneRenderedLines(lines)
}

func cachedViewportTextBlock(contentWidth int, label string) []renderedLine {
	h := newStateHasher()
	h.add("viewport-text-block", label)
	h.addInt(contentWidth)
	key := h.sum()
	if cached, ok := channelRenderCache.getViewportBlock(key); ok {
		return cached
	}
	lines := []renderedLine{{Text: renderDateSeparator(contentWidth, label)}}
	channelRenderCache.putViewportBlock(key, lines)
	return cloneRenderedLines(lines)
}

func trimRenderedTail(lines []renderedLine, limit int) []renderedLine {
	if len(lines) == 0 {
		return nil
	}
	if limit < 1 {
		limit = 1
	}
	if len(lines) <= limit {
		return cloneRenderedLines(lines)
	}
	return cloneRenderedLines(lines[len(lines)-limit:])
}
