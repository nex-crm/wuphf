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
// so messages typed in the pane reach the office and replies come back. Channel-
// agnostic and idempotent; best-effort (a missing lead or broker just no-ops).
func (t *SlackTransport) seedAssistantThread(channelID string) {
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
