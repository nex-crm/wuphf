package team

import (
	"strings"
	"time"
)

// Read-only accessors over broker state. Each method takes b.mu, copies
// the relevant slice/struct (so callers can't mutate the broker via the
// returned reference), and returns. None mutate state.
//
// Side-effecting "ensure" methods (EnsureBridgedMember,
// EnsureDirectChannel, PostInboundSurfaceMessage) stay in broker.go
// because they may write — they're queries in name only.

// Messages returns all channel messages (for the Go TUI channel view).
func (b *Broker) Messages() []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]channelMessage, len(b.messages))
	copy(out, b.messages)
	return out
}

func (b *Broker) HasPendingInterview() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, req := range b.requests {
		if requestIsHumanInterview(req) && requestIsActive(req) {
			return true
		}
	}
	return false
}

func (b *Broker) HasBlockingRequest() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, req := range b.requests {
		if requestBlocksMessages(req) {
			return true
		}
	}
	return false
}

// HasRecentlyTaggedAgents returns true if any agent was @mentioned within
// the given duration and has not yet replied (i.e. is presumably "typing").
func (b *Broker) HasRecentlyTaggedAgents(within time.Duration) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.lastTaggedAt) == 0 {
		return false
	}
	cutoff := time.Now().Add(-within)
	for _, t := range b.lastTaggedAt {
		if t.After(cutoff) {
			return true
		}
	}
	return false
}

func (b *Broker) EnabledMembers(channel string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sessionMode == SessionModeOneOnOne {
		return []string{b.oneOnOneAgent}
	}
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	if ch := b.findChannelLocked(channel); ch != nil {
		return b.enabledChannelMembersLocked(channel, ch.Members)
	}
	return nil
}

// DisabledMembers returns the slugs explicitly disabled for a channel —
// members who were present in ch.Members at some point but have been muted
// for this channel. Callers use this to distinguish "never added" (which an
// explicit @-tag can bypass) from "deliberately muted" (which an @-tag must
// respect — muting an agent is the user's explicit intent to silence them).
func (b *Broker) DisabledMembers(channel string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	ch := b.findChannelLocked(channel)
	if ch == nil || len(ch.Disabled) == 0 {
		return nil
	}
	return append([]string(nil), ch.Disabled...)
}

func (b *Broker) OfficeMembers() []officeMember {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeMember, len(b.members))
	copy(out, b.members)
	return out
}

func (b *Broker) ChannelMessages(channel string) []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	out := make([]channelMessage, 0, len(b.messages))
	for _, msg := range b.messages {
		if normalizeChannelSlug(msg.Channel) == channel {
			out = append(out, msg)
		}
	}
	return out
}

// AllMessages returns a copy of all messages across all channels, ordered by
// creation time. Use this when the caller needs to search across channels rather
// than in a single known channel.
func (b *Broker) AllMessages() []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]channelMessage, len(b.messages))
	copy(out, b.messages)
	return out
}

// SurfaceChannels returns all channels that have a surface configured for the given provider.
func (b *Broker) SurfaceChannels(provider string) []teamChannel {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []teamChannel
	for _, ch := range b.channels {
		if ch.Surface != nil && ch.Surface.Provider == provider {
			cp := ch
			cp.Members = append([]string(nil), ch.Members...)
			cp.Disabled = append([]string(nil), ch.Disabled...)
			s := *ch.Surface
			cp.Surface = &s
			out = append(out, cp)
		}
	}
	return out
}

// DMPartner returns the non-human member slug of a 1:1 DM channel. Returns
// "" if the channel is not a DM, does not exist, or is a group DM. Used by
// surface bridges (OpenClaw, Slack, etc.) to resolve "who is the human
// talking to" when routing DM posts to the right agent without requiring an
// @mention.
func (b *Broker) DMPartner(channelSlug string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := b.findChannelLocked(normalizeChannelSlug(channelSlug))
	if ch == nil || !ch.isDM() {
		return ""
	}
	if len(ch.Members) != 2 {
		return ""
	}
	for _, m := range ch.Members {
		if m != "human" && m != "you" {
			return m
		}
	}
	return ""
}

func (b *Broker) ChannelTasks(channel string) []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	out := make([]teamTask, 0, len(b.tasks))
	for _, task := range b.tasks {
		if normalizeChannelSlug(task.Channel) == channel {
			out = append(out, task)
		}
	}
	return out
}

// AllTasks returns a copy of all tasks across all channels. Use this when the
// caller needs to search across channels rather than in a single known channel.
func (b *Broker) AllTasks() []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]teamTask, len(b.tasks))
	copy(out, b.tasks)
	return out
}

// InFlightTasks returns tasks that have an assigned owner and a non-terminal
// status (anything except "done", "completed", "canceled", or "cancelled").
func (b *Broker) InFlightTasks() []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]teamTask, 0)
	for _, task := range b.tasks {
		if task.Owner == "" {
			continue
		}
		s := strings.ToLower(strings.TrimSpace(task.Status))
		if s == "done" || s == "completed" || s == "canceled" || s == "cancelled" {
			continue
		}
		out = append(out, task)
	}
	return out
}

// RecentHumanMessages returns up to limit messages sent by a human or external
// sender ("you", "human", or "nex"). The returned slice contains the most
// recent messages in chronological order (earliest first).
func (b *Broker) RecentHumanMessages(limit int) []channelMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	var human []channelMessage
	for _, msg := range b.messages {
		f := strings.ToLower(strings.TrimSpace(msg.From))
		if f == "you" || f == "human" || f == "nex" {
			human = append(human, msg)
		}
	}
	if len(human) <= limit {
		return human
	}
	return human[len(human)-limit:]
}

// UnackedTasks returns in_progress tasks with an owner that have not been acked
// and were created more than the given duration ago.
func (b *Broker) UnackedTasks(timeout time.Duration) []teamTask {
	b.mu.Lock()
	defer b.mu.Unlock()
	cutoff := time.Now().UTC().Add(-timeout)
	out := make([]teamTask, 0)
	for _, task := range b.tasks {
		if task.Status != "in_progress" || task.Owner == "" || task.AckedAt != "" {
			continue
		}
		created, err := time.Parse(time.RFC3339, task.CreatedAt)
		if err != nil {
			continue
		}
		if created.Before(cutoff) {
			out = append(out, task)
		}
	}
	return out
}

func (b *Broker) Requests(channel string, includeResolved bool) []humanInterview {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel = normalizeChannelSlug(channel)
	if channel == "" {
		channel = "general"
	}
	out := make([]humanInterview, 0, len(b.requests))
	for _, req := range b.requests {
		reqChannel := normalizeChannelSlug(req.Channel)
		if reqChannel == "" {
			reqChannel = "general"
		}
		if reqChannel != channel {
			continue
		}
		if !includeResolved && !requestIsActive(req) {
			continue
		}
		out = append(out, req)
	}
	return out
}

func (b *Broker) Actions() []officeActionLog {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeActionLog, len(b.actions))
	copy(out, b.actions)
	return out
}

func (b *Broker) Signals() []officeSignalRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeSignalRecord, len(b.signals))
	copy(out, b.signals)
	return out
}

func (b *Broker) Decisions() []officeDecisionRecord {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]officeDecisionRecord, len(b.decisions))
	copy(out, b.decisions)
	return out
}

func (b *Broker) Watchdogs() []watchdogAlert {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]watchdogAlert, len(b.watchdogs))
	copy(out, b.watchdogs)
	return out
}

func (b *Broker) Scheduler() []schedulerJob {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]schedulerJob, len(b.scheduler))
	copy(out, b.scheduler)
	return out
}

type queueSnapshot struct {
	Actions   []officeActionLog      `json:"actions"`
	Signals   []officeSignalRecord   `json:"signals,omitempty"`
	Decisions []officeDecisionRecord `json:"decisions,omitempty"`
	Watchdogs []watchdogAlert        `json:"watchdogs,omitempty"`
	Scheduler []schedulerJob         `json:"scheduler"`
	Due       []schedulerJob         `json:"due,omitempty"`
}

func (b *Broker) QueueSnapshot() queueSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return queueSnapshot{
		Actions:   append([]officeActionLog(nil), b.actions...),
		Signals:   append([]officeSignalRecord(nil), b.signals...),
		Decisions: append([]officeDecisionRecord(nil), b.decisions...),
		Watchdogs: append([]watchdogAlert(nil), b.watchdogs...),
		Scheduler: append([]schedulerJob(nil), b.scheduler...),
		Due:       append([]schedulerJob(nil), b.dueSchedulerJobsLocked(time.Now().UTC())...),
	}
}
