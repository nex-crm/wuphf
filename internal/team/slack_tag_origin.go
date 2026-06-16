package team

import (
	"strings"
	"time"
)

// slack_tag_origin.go remembers WHERE a fresh @wuphf tag came from so the task
// it spins up can link its own thread back into that conversation. The inbound
// gate records an origin when the office is tagged outside any task thread; the
// task-card creator (ensureTaskThreadRoot) consumes it once and posts a
// backlink, so a human who tagged WUPHF sees a pointer to the new task thread.
//
// This is reliable precisely because of the passivity gate: with WUPHF acting
// only on tags, a fresh task created in a channel almost always traces to the
// most recent tag there. The TTL guards the rare case where a tag produced no
// task (a plain question) and a later, unrelated card would otherwise consume a
// stale origin.

// slackTagOriginTTL bounds how long a recorded tag origin stays consumable.
const slackTagOriginTTL = 15 * time.Minute

type slackTagOrigin struct {
	threadTS   string // Slack thread to reply into (the thread tagged, or the tag message itself)
	recordedAt time.Time
}

// recordSlackTagOrigin notes that the office was tagged in channelID at
// threadTS, replacing any prior unconsumed origin for that channel.
func (b *Broker) recordSlackTagOrigin(channelID, threadTS string) {
	channelID = strings.TrimSpace(channelID)
	threadTS = strings.TrimSpace(threadTS)
	if channelID == "" || threadTS == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.slackTagOrigins == nil {
		b.slackTagOrigins = make(map[string]slackTagOrigin)
	}
	b.slackTagOrigins[channelID] = slackTagOrigin{threadTS: threadTS, recordedAt: b.clockNow()}
}

// consumeSlackTagOrigin returns and clears the pending tag origin for channelID,
// or "" when there is none or it has aged past the TTL.
func (b *Broker) consumeSlackTagOrigin(channelID string) string {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.slackTagOrigins[channelID]
	if !ok {
		return ""
	}
	delete(b.slackTagOrigins, channelID)
	if b.clockNow().Sub(o.recordedAt) > slackTagOriginTTL {
		return ""
	}
	return o.threadTS
}

// clockNow is the broker's clock, honoring the injectable nowFn used by tests.
func (b *Broker) clockNow() time.Time {
	if b.nowFn != nil {
		return b.nowFn()
	}
	return time.Now()
}

// slackSuppressesWake reports whether a message must be recorded for context but
// NOT wake anyone — an untagged, non-task ambient HUMAN message in a Slack
// channel. This is the "act only when tagged" half of WUPHF's Slack passivity:
// the message is still appended (so the office has channel context), but no
// agent turn is dispatched for it. A tag lands the office lead in Tagged; a
// task-thread reply carries SourceTaskID; either makes this return false so the
// message wakes normally. Non-human (foreign agents) and non-Slack channels are
// never suppressed.
func (b *Broker) slackSuppressesWake(msg channelMessage) bool {
	if !isHumanMessageSender(msg.From) {
		return false
	}
	if len(msg.Tagged) > 0 || strings.TrimSpace(msg.SourceTaskID) != "" {
		return false
	}
	// A 1:1 DM (the Assistant pane / direct message) is the human addressing the
	// office directly: always respond, exactly like the main channel's @-mention.
	// Passivity is only for the SHARED channel, where ambient chatter is not for us.
	if IsDMSlug(msg.Channel) {
		return false
	}
	return b.ChannelHasSurface(msg.Channel, "slack")
}
