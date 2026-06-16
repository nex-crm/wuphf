package team

import "strings"

// slack_assistant.go wires WUPHF into Slack's native Agents & AI Apps surface:
// the Assistant pane. Mirrors the Hermes agent's deliberately minimal native
// integration — it handles the assistant lifecycle events and treats the pane as
// a well-scoped 1:1 DM with the office, rather than bolting on suggested-prompt /
// title chrome (which Hermes itself does not ship).
//
// When a user opens the pane, Slack fires assistant_thread_started (and
// assistant_thread_context_changed when they switch the channel context). We map
// that Assistant IM channel to the office LEAD's DM, so a message the user types
// in the pane routes to the lead through the ordinary inbound path, and the
// lead's reply posts straight back into the pane. The native "is thinking…"
// status (slack_thinking_status.go) plays on top for task work.
//
// The mapping is in-memory only (ChannelMap), which is correct: the lifecycle
// event fires again every time the pane is opened, so the binding is always
// re-established after a restart without persisting Slack-channel ids.

// seedAssistantThread binds an Assistant-pane IM channel to the office lead's DM
// so messages typed in the pane reach the office and replies come back, and
// records the pane's thread root so the office's replies thread under it.
// Channel-agnostic and idempotent; best-effort (a missing lead or broker just
// no-ops). threadTS is the Assistant thread root from the lifecycle event (may
// be empty on some events; inbound messages refine it).
func (t *SlackTransport) seedAssistantThread(channelID, threadTS string) {
	channelID = strings.TrimSpace(channelID)
	if channelID == "" || t.Broker == nil {
		return
	}
	lead := t.Broker.OfficeLeadSlug()
	if lead == "" {
		return
	}
	dmSlug := DMSlugFor(lead)
	if dmSlug == "" {
		return
	}
	t.Broker.RecordAssistantThread(dmSlug, threadTS)
	// Create the DM and bind its Slack surface to the pane's IM channel, so the
	// lead's replies are queued for delivery (ExternalQueue only returns messages
	// for channels that carry a slack surface) and post straight back into the
	// pane. THEN map the IM channel to the DM for inbound. Order matters: a
	// message could arrive immediately after the pane opens.
	t.Broker.BindSlackDMSurface(dmSlug, channelID)
	t.mapsMu.Lock()
	if t.ChannelMap == nil {
		t.ChannelMap = map[string]string{}
	}
	t.ChannelMap[channelID] = dmSlug
	t.mapsMu.Unlock()
}

// BindSlackDMSurface idempotently creates the 1:1 DM channel for slug and binds
// its surface to a Slack IM channel id, so office messages in that DM are
// relayed out to the Assistant pane (and survive a restart, re-bound on the next
// pane open regardless).
func (b *Broker) BindSlackDMSurface(slug, imChannelID string) {
	if b == nil {
		return
	}
	slug = normalizeChannelSlug(slug)
	imChannelID = strings.TrimSpace(imChannelID)
	if slug == "" || imChannelID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := b.ensureDMConversationLocked(slug)
	if ch == nil {
		return
	}
	if ch.Surface == nil || ch.Surface.Provider != "slack" || ch.Surface.RemoteID != imChannelID {
		ch.Surface = &channelSurface{Provider: "slack", RemoteID: imChannelID}
		_ = b.saveLocked()
	}
}

// RecordAssistantThread remembers the Slack Assistant thread root for a DM, so
// the office's replies in that DM thread under the same root (keeping the pane a
// single conversation). Latest non-empty root wins; empty is ignored. In-memory:
// the pane re-announces its thread on open, so this is re-established as needed.
func (b *Broker) RecordAssistantThread(dmSlug, threadTS string) {
	threadTS = strings.TrimSpace(threadTS)
	dmSlug = normalizeChannelSlug(dmSlug)
	if b == nil || dmSlug == "" || threadTS == "" {
		return
	}
	b.mu.Lock()
	if b.assistantThreads == nil {
		b.assistantThreads = map[string]string{}
	}
	b.assistantThreads[dmSlug] = threadTS
	b.mu.Unlock()
}

// AssistantThreadFor returns the recorded Assistant thread root for a DM, or ""
// if none — in which case the office reply posts at the DM top level.
func (b *Broker) AssistantThreadFor(dmSlug string) string {
	dmSlug = normalizeChannelSlug(dmSlug)
	if b == nil || dmSlug == "" {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.assistantThreads[dmSlug]
}
