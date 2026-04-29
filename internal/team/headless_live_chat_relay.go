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
	msg, _, posted, err := r.l.broker.ReportAgentIssue(r.slug, r.channel, r.replyTo, detail)
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
	return classifyAgentIssue(text).Visible
}
