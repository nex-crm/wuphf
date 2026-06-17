package team

import (
	"context"
	"strings"
	"testing"
)

// TestSeedAssistantThread_BindsPaneToLeadDM verifies the native Assistant pane is
// wired to the office lead's DM: a message typed in the pane (which arrives on
// the IM channel) must route to the lead through the ordinary inbound path.
func TestSeedAssistantThread_BindsPaneToLeadDM(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)

	lead := b.OfficeLeadSlug()
	if lead == "" {
		t.Fatal("precondition: office needs a lead")
	}
	want := DMSlugFor(lead)
	if want == "" || !IsDMSlug(want) {
		t.Fatalf("lead DM slug must be a DM slug, got %q", want)
	}

	tr.seedAssistantThread(context.Background(), "D999", "1700000000.5")

	tr.mapsMu.RLock()
	got := tr.ChannelMap["D999"]
	tr.mapsMu.RUnlock()
	if got != want {
		t.Fatalf("assistant pane IM channel D999 must map to the lead DM %q, got %q", want, got)
	}

	// The pane's thread root is recorded so the office's reply threads under it.
	if root := b.AssistantThreadFor(want); root != "1700000000.5" {
		t.Fatalf("assistant thread root for %q = %q, want 1700000000.5", want, root)
	}

	// The DM conversation must exist AND carry a slack surface bound to the IM
	// channel, or the lead's replies would never be queued for delivery back to
	// the pane (ExternalQueue only returns slack-surface channels).
	b.mu.Lock()
	ch := b.findChannelLocked(want)
	b.mu.Unlock()
	if ch == nil {
		t.Fatalf("BindSlackDMSurface must have created the lead DM channel %q", want)
	}
	if ch.Surface == nil || ch.Surface.Provider != "slack" || ch.Surface.RemoteID != "D999" {
		t.Fatalf("lead DM must carry a slack surface bound to IM channel D999, got %+v", ch.Surface)
	}

	// Idempotent: opening the pane again just re-affirms the same binding.
	tr.seedAssistantThread(context.Background(), "D999", "1700000000.5")
	tr.mapsMu.RLock()
	got2 := tr.ChannelMap["D999"]
	tr.mapsMu.RUnlock()
	if got2 != want {
		t.Fatalf("re-seeding must keep the binding stable, got %q", got2)
	}

	// Empty / brokerless inputs are no-ops, never a panic.
	tr.seedAssistantThread(context.Background(), "", "")
}

// TestFormatOutbound_ThreadsAssistantPaneReply verifies the office's DM reply is
// threaded under the pane's recorded conversation root (not posted loose).
func TestFormatOutbound_ThreadsAssistantPaneReply(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	lead := b.OfficeLeadSlug()
	dm := DMSlugFor(lead)
	// Open the pane (binds the IM channel in the transport ChannelMap + the DM
	// surface) and record the conversation root.
	tr.seedAssistantThread(context.Background(), "D777", "1700000000.9")

	out, ok := tr.FormatOutbound(channelMessage{From: lead, Channel: dm, Content: "Here's the answer."})
	if !ok {
		t.Fatal("expected the DM reply to format")
	}
	if out.ThreadKey != "1700000000.9" {
		t.Fatalf("assistant pane reply must thread under the pane root; ThreadKey=%q", out.ThreadKey)
	}
}

// TestSeedAssistantThread_SeedsSuggestedPrompts verifies opening the pane offers
// the office's native starter prompts on the pane's thread root.
func TestSeedAssistantThread_SeedsSuggestedPrompts(t *testing.T) {
	api := newFakeSlackAPI()
	tr, _ := newTestSlackTransport(t, "C0123", api)

	tr.seedAssistantThread(context.Background(), "D999", "1700000000.5")

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.suggestedPrompts) != 1 {
		t.Fatalf("opening the pane must seed suggested prompts exactly once, got %d", len(api.suggestedPrompts))
	}
	got := api.suggestedPrompts[0]
	if got.ChannelID != "D999" || got.ThreadTS != "1700000000.5" {
		t.Fatalf("prompts must target the pane root D999/1700000000.5, got %s/%s", got.ChannelID, got.ThreadTS)
	}
	if len(got.Prompts) == 0 || len(got.Prompts) > 4 {
		t.Fatalf("want 1-4 prompts (Slack renders up to four), got %d", len(got.Prompts))
	}
	for i, p := range got.Prompts {
		if p.Title == "" || p.Message == "" {
			t.Fatalf("prompt %d must carry a title and a message, got %+v", i, p)
		}
	}
}

// TestSeedAssistantThread_NoPromptsWithoutRoot verifies a lifecycle event without
// a thread root (some context-changed events) does not push prompts to nowhere.
func TestSeedAssistantThread_NoPromptsWithoutRoot(t *testing.T) {
	api := newFakeSlackAPI()
	tr, _ := newTestSlackTransport(t, "C0123", api)

	tr.seedAssistantThread(context.Background(), "D999", "")

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.suggestedPrompts) != 0 {
		t.Fatalf("no thread root → no suggested-prompts call, got %+v", api.suggestedPrompts)
	}
}

// TestAssistantPaneRef resolves the lead's open pane and refuses everyone else.
func TestAssistantPaneRef(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	lead := b.OfficeLeadSlug()

	// No pane open yet.
	if _, _, ok := b.AssistantPaneRef(lead); ok {
		t.Fatal("no pane should resolve before one is opened")
	}

	tr.seedAssistantThread(context.Background(), "D999", "1700000000.5")

	channelID, threadTS, ok := b.AssistantPaneRef(lead)
	if !ok || channelID != "D999" || threadTS != "1700000000.5" {
		t.Fatalf("lead pane must resolve to D999/1700000000.5, got %s/%s ok=%v", channelID, threadTS, ok)
	}
	// A non-lead agent has no pane.
	if _, _, ok := b.AssistantPaneRef("definitely-not-an-agent"); ok {
		t.Fatal("a non-lead slug must not resolve a pane")
	}
}

// TestClaimAssistantTitle_Once verifies the set-once guard so only the first user
// message titles a pane conversation.
func TestClaimAssistantTitle_Once(t *testing.T) {
	b := newTestBrokerWithSlackChannel(t, "C0123")
	if !b.claimAssistantTitle("100.1") {
		t.Fatal("first claim must succeed")
	}
	if b.claimAssistantTitle("100.1") {
		t.Fatal("second claim on the same thread must fail (already titled)")
	}
	if !b.claimAssistantTitle("200.2") {
		t.Fatal("a different thread must still be claimable")
	}
	if b.claimAssistantTitle("") {
		t.Fatal("an empty thread root must never be claimable")
	}
}

// TestMaybeSetAssistantTitle_FromFirstMessage verifies the pane conversation is
// titled from the first message, mentions stripped, and only once.
func TestMaybeSetAssistantTitle_FromFirstMessage(t *testing.T) {
	api := newFakeSlackAPI()
	tr, _ := newTestSlackTransport(t, "C0123", api)

	tr.maybeSetAssistantTitle(context.Background(), "D999", "1700000000.5", "<@UBOT> please draft the Q3 RevOps plan")
	tr.maybeSetAssistantTitle(context.Background(), "D999", "1700000000.5", "a different second message")

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.titles) != 1 {
		t.Fatalf("title must be set exactly once per thread, got %d", len(api.titles))
	}
	got := api.titles[0]
	if got.ChannelID != "D999" || got.ThreadTS != "1700000000.5" {
		t.Fatalf("title must target the pane root, got %s/%s", got.ChannelID, got.ThreadTS)
	}
	if got.Title != "please draft the Q3 RevOps plan" {
		t.Fatalf("title must derive from the first message with the mention stripped, got %q", got.Title)
	}
}

func TestAssistantThreadTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<@UBOT> hello there", "hello there"},
		{"  spaced   out   words  ", "spaced out words"},
		{"<@UBOT>", ""},
		{strings.Repeat("x", 100), strings.Repeat("x", 72) + "..."},
	}
	for _, c := range cases {
		if got := assistantThreadTitle(c.in); got != c.want {
			t.Fatalf("assistantThreadTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
