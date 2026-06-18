package team

import (
	"context"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

// slack_assistant.go wires WUPHF into Slack's native Agents & AI Apps surface:
// the Assistant pane. It handles the assistant lifecycle events and treats the
// pane as a well-scoped 1:1 DM with the office, plus the native agent
// affordances Slack recommends for an entry point: a "is thinking…" status that
// renders in the pane (slack_thinking_status.go), tappable suggested prompts on
// open, and a conversation title set from the first message. These live ONLY in
// the 1:1 pane — a private surface where being a responsive, guiding agent is
// exactly what the user wants. Shared channels stay quiet (see slack_silence.go
// and the passivity gate); the restraint there is intentional.
//
// When a user opens the pane, Slack fires assistant_thread_started (and
// assistant_thread_context_changed when they switch the channel context). We map
// that Assistant IM channel to the office LEAD's DM, so a message the user types
// in the pane routes to the lead through the ordinary inbound path, and the
// lead's reply posts straight back into the pane.
//
// The mapping is in-memory only (ChannelMap), which is correct: the lifecycle
// event fires again every time the pane is opened, so the binding is always
// re-established after a restart without persisting Slack-channel ids.

// assistantAPICallTimeout bounds each native Assistant Web API call so a slow
// Slack response can't stall the socket event loop these run inside.
const assistantAPICallTimeout = 8 * time.Second

// seedAssistantThread binds an Assistant-pane IM channel to the office lead's DM
// so messages typed in the pane reach the office and replies come back, records
// the pane's thread root so the office's replies thread under it, and seeds the
// pane with context-aware suggested prompts. Channel-agnostic and idempotent;
// best-effort (a missing lead or broker just no-ops). threadTS is the Assistant
// thread root from the lifecycle event (may be empty on some events; inbound
// messages refine it).
func (t *SlackTransport) seedAssistantThread(ctx context.Context, channelID, threadTS string) {
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

	// Native entry-point affordance: offer tappable starter prompts. Needs the
	// thread root (present on assistant_thread_started / _context_changed).
	t.setAssistantSuggestedPrompts(ctx, channelID, threadTS)
}

// setAssistantSuggestedPrompts pushes the office's starter prompts into the pane.
// Best-effort: a missing thread root or an API error (e.g. the app lacks the
// Assistant feature) is logged at most and never propagates.
func (t *SlackTransport) setAssistantSuggestedPrompts(ctx context.Context, channelID, threadTS string) {
	threadTS = strings.TrimSpace(threadTS)
	if threadTS == "" || t.api == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, assistantAPICallTimeout)
	defer cancel()
	_ = t.api.SetAssistantThreadsSuggestedPromptsContext(cctx, slack.AssistantThreadsSetSuggestedPromptsParameters{
		Title:     "How can the office help?",
		ChannelID: channelID,
		ThreadTS:  threadTS,
		Prompts:   assistantSuggestedPrompts(),
	})
}

// assistantSuggestedPrompts returns the office's starter prompts for the pane.
// They map to what WUPHF actually does — run tasks, report status, surface the
// team wiki — so a first-time user sees concrete entry points rather than a
// blank composer. Slack renders up to four.
func assistantSuggestedPrompts() []slack.AssistantThreadsPrompt {
	return []slack.AssistantThreadsPrompt{
		{Title: "What's the office working on?", Message: "What is the office working on right now?"},
		{Title: "Start a new task", Message: "I'd like to start a new task: "},
		{Title: "Catch me up", Message: "Catch me up on what's shipped recently."},
		{Title: "What can you help with?", Message: "What can this office help me with?"},
	}
}

// maybeSetAssistantTitle names the pane conversation from its first user message,
// once per thread, so it is findable in the user's pane history. Best-effort and
// guarded by the broker's set-once flag, so repeated messages don't re-title.
func (t *SlackTransport) maybeSetAssistantTitle(ctx context.Context, channelID, threadTS, firstMessage string) {
	threadTS = strings.TrimSpace(threadTS)
	channelID = strings.TrimSpace(channelID)
	if threadTS == "" || channelID == "" || t.api == nil || t.Broker == nil {
		return
	}
	if !t.Broker.claimAssistantTitle(threadTS) {
		return
	}
	title := assistantThreadTitle(firstMessage)
	if title == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, assistantAPICallTimeout)
	defer cancel()
	_ = t.api.SetAssistantThreadsTitleContext(cctx, slack.AssistantThreadsSetTitleParameters{
		ChannelID: channelID,
		ThreadTS:  threadTS,
		Title:     title,
	})
}

// assistantThreadTitle renders a short, single-line conversation title from a
// user's first message: mentions stripped, whitespace collapsed, clipped to
// Slack's practical title length.
func assistantThreadTitle(message string) string {
	cleaned := strings.TrimSpace(slackInboundMentionRE.ReplaceAllString(message, ""))
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	return truncate(cleaned, 72)
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

// AssistantPaneRef locates the Slack Assistant pane (the 1:1 DM) bound to an
// agent slug: the pane's IM channel id and its conversation thread root. Returns
// ok=false when the slug has no open pane — no recorded thread root or no bound
// Slack IM surface — which is the common case for any agent that is not the
// office lead. This is the surface where assistant.threads.setStatus actually
// renders (the native pane composer), unlike a shared-channel task thread, so
// the thinking-status loop targets it for the lead.
func (b *Broker) AssistantPaneRef(slug string) (channelID, threadTS string, ok bool) {
	slug = strings.TrimSpace(slug)
	if b == nil || slug == "" {
		return "", "", false
	}
	dmSlug := normalizeChannelSlug(DMSlugFor(slug))
	if dmSlug == "" {
		return "", "", false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	threadTS = strings.TrimSpace(b.assistantThreads[dmSlug])
	if threadTS == "" {
		return "", "", false
	}
	ch := b.findChannelLocked(dmSlug)
	if ch == nil || ch.Surface == nil || ch.Surface.Provider != "slack" || strings.TrimSpace(ch.Surface.RemoteID) == "" {
		return "", "", false
	}
	return ch.Surface.RemoteID, threadTS, true
}

// claimAssistantTitle reports whether the Assistant thread rooted at threadTS
// still needs a title, marking it claimed so only the first caller (the first
// user message) sets it. In-memory and best-effort: after a restart a thread may
// be re-titled once, which is harmless (Slack just overwrites with the same
// shape of title).
func (b *Broker) claimAssistantTitle(threadTS string) bool {
	threadTS = strings.TrimSpace(threadTS)
	if b == nil || threadTS == "" {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.assistantTitled[threadTS] {
		return false
	}
	if b.assistantTitled == nil {
		b.assistantTitled = map[string]bool{}
	}
	b.assistantTitled[threadTS] = true
	return true
}
