package team

import "testing"

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

	tr.seedAssistantThread("D999", "1700000000.5")

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
	tr.seedAssistantThread("D999", "1700000000.5")
	tr.mapsMu.RLock()
	got2 := tr.ChannelMap["D999"]
	tr.mapsMu.RUnlock()
	if got2 != want {
		t.Fatalf("re-seeding must keep the binding stable, got %q", got2)
	}

	// Empty / brokerless inputs are no-ops, never a panic.
	tr.seedAssistantThread("", "")
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
	tr.seedAssistantThread("D777", "1700000000.9")

	out, ok := tr.FormatOutbound(channelMessage{From: lead, Channel: dm, Content: "Here's the answer."})
	if !ok {
		t.Fatal("expected the DM reply to format")
	}
	if out.ThreadKey != "1700000000.9" {
		t.Fatalf("assistant pane reply must thread under the pane root; ThreadKey=%q", out.ThreadKey)
	}
}
