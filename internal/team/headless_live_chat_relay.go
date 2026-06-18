package team

import (
	"strings"
)

const (
	headlessLiveChatMinFlushChars = 16
	headlessLiveChatMaxFlushChars = 480
)

type headlessLiveChatRelay struct {
	l            *Launcher
	slug         string
	channel      string
	replyTo      string
	logf         func(string)
	buf          strings.Builder
	lastPosted   string
	postFailures int

	// Live pane streaming: when the target is the agent's own open Slack pane,
	// each flush appends to a single streaming message that builds in place
	// instead of posting a separate message. Resolved lazily on first flush;
	// nil means this turn delivers via the normal post path (every non-pane turn,
	// and any pane turn where the stream could not start). streamBuf accumulates
	// what was streamed so the full reply can be recorded to history at close.
	stream         turnStreamSink
	streamResolved bool
	streamClosed   bool
	streamBuf      strings.Builder
}

func newHeadlessLiveChatRelay(l *Launcher, slug string, targetChannel string, notification string, logf func(string)) *headlessLiveChatRelay {
	if l == nil || l.broker == nil {
		return nil
	}
	targetChannel = normalizeChannelSlug(targetChannel)
	if targetChannel == "" {
		targetChannel = "general"
	}
	if IsDMSlug(targetChannel) {
		if targetAgent := DMTargetAgent(targetChannel); targetAgent != "" {
			targetChannel = DMSlugFor(targetAgent)
		}
	}
	return &headlessLiveChatRelay{
		l:       l,
		slug:    strings.TrimSpace(slug),
		channel: targetChannel,
		replyTo: headlessReplyToID(notification),
		logf:    logf,
	}
}

func (r *headlessLiveChatRelay) OnText(chunk string) {
	if r == nil || chunk == "" {
		return
	}
	r.buf.WriteString(chunk)
	text := r.buf.String()
	if len(strings.TrimSpace(text)) >= headlessLiveChatMaxFlushChars || headlessLiveChatShouldFlush(text) {
		r.Flush()
	}
}

func (r *headlessLiveChatRelay) Flush() {
	if r == nil || r.l == nil || r.l.broker == nil {
		return
	}
	text := strings.TrimSpace(r.buf.String())
	r.buf.Reset()
	r.postText(text)
}

func (r *headlessLiveChatRelay) ReportIssue(detail string) {
	if r == nil || r.l == nil || r.l.broker == nil {
		return
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return
	}
	r.Flush()
	msg, _, posted, err := r.l.broker.ReportIncident(r.slug, r.channel, r.replyTo, detail)
	if err != nil {
		r.postFailures++
		if r.logf != nil && r.postFailures <= 3 {
			r.logf("live-chat-relay-issue-error: " + err.Error())
		}
		return
	}
	if posted {
		r.lastPosted = msg.Content
		if r.logf != nil {
			r.logf("live-chat-relay-post: posted streamed issue to #" + msg.Channel + " as " + msg.ID)
		}
	}
}

func (r *headlessLiveChatRelay) postText(text string) {
	if r == nil || r.l == nil || r.l.broker == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" || text == r.lastPosted || looksUnparsedToolCall(text) {
		return
	}
	// Live pane streaming: append this flush to the in-place streaming message
	// rather than posting a new one. A silent marker (NO_REPLY) is never shown.
	if sink := r.streamTo(); sink != nil && !r.streamClosed {
		if slackOutboundIsSilent(text) {
			r.lastPosted = text
			return
		}
		if err := sink.Append(text); err == nil {
			if r.streamBuf.Len() > 0 {
				r.streamBuf.WriteString("\n\n")
			}
			r.streamBuf.WriteString(text)
			r.lastPosted = text
			return
		}
		// The stream could not start (e.g. the app lacks the Assistant feature):
		// disable streaming and fall back to normal posting for the rest of the
		// turn, so the reply is never lost.
		r.stream = nil
		if r.logf != nil {
			r.logf("live-chat-relay-stream: start failed, falling back to normal posting")
		}
	}
	msg, err := r.l.broker.PostMessage(r.slug, r.channel, text, nil, r.replyTo)
	if err != nil {
		r.postFailures++
		if r.logf != nil && r.postFailures <= 3 {
			r.logf("live-chat-relay-post-error: " + err.Error())
		}
		return
	}
	r.lastPosted = text
	if r.logf != nil {
		r.logf("live-chat-relay-post: posted streamed output to #" + msg.Channel + " as " + msg.ID)
	}
}

// streamTo lazily resolves the live pane stream for this turn: a sink when the
// target is the agent's own open Slack pane, nil otherwise. Resolved once.
func (r *headlessLiveChatRelay) streamTo() turnStreamSink {
	if r == nil {
		return nil
	}
	if !r.streamResolved {
		r.streamResolved = true
		if r.l != nil && r.l.broker != nil {
			r.stream = r.l.broker.openTurnStream(r.slug, r.channel)
		}
	}
	return r.stream
}

// closeStream finalizes a live-streamed pane reply and records the full text to
// the message store (marked already-delivered so the outbound dispatcher does
// not re-post it). Returns true when the reply was delivered live — the caller
// then skips its normal final-message fallback. Returns false when nothing was
// streamed (every non-pane turn, or a pane turn that fell back to posting), in
// which case the caller delivers the final message the usual way.
func (r *headlessLiveChatRelay) closeStream(finalText string) bool {
	if r == nil {
		return false
	}
	sink := r.streamTo()
	if sink == nil || !sink.Started() {
		return false
	}
	sink.Close(slackPaneDisclaimer)
	r.streamClosed = true
	record := strings.TrimSpace(r.streamBuf.String())
	if record == "" {
		record = strings.TrimSpace(finalText)
	}
	if record != "" && r.l != nil && r.l.broker != nil {
		if msg, err := r.l.broker.PostMessage(r.slug, r.channel, record, nil, r.replyTo); err == nil {
			r.l.broker.MarkExternalDelivered(msg.ID)
			if r.logf != nil {
				r.logf("live-chat-relay-stream: finalized streamed pane reply, recorded " + msg.ID)
			}
		}
	}
	return true
}

func headlessLiveChatShouldFlush(text string) bool {
	if strings.Contains(text, "\n\n") {
		return len(strings.TrimSpace(text)) >= headlessLiveChatMinFlushChars
	}
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < headlessLiveChatMinFlushChars {
		return false
	}
	switch trimmed[len(trimmed)-1] {
	case '.', '!', '?':
		return true
	default:
		return false
	}
}

func headlessLiveChatLooksIssue(text string) bool {
	return classifyIncident(text).Visible
}
