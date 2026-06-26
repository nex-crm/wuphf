package team

import "testing"

// TestEnsureSlackTransportRunningNoTokensIsNoop verifies the hot-start gate: with
// no Slack tokens configured, EnsureSlackTransportRunning starts nothing, and
// stopSlackTransport is a safe no-op (the RegisterTransports cleanup runs even
// when the transport never started).
func TestEnsureSlackTransportRunningNoTokensIsNoop(t *testing.T) {
	t.Setenv("SLACK_BOT_TOKEN", "")
	t.Setenv("SLACK_APP_TOKEN", "")
	b := newTestBroker(t)

	if b.slackTransportRunning() {
		t.Fatal("transport should not be running before any start")
	}
	b.EnsureSlackTransportRunning()
	if b.slackTransportRunning() {
		t.Fatal("EnsureSlackTransportRunning with no tokens must not start a transport")
	}
	// Must not panic when nothing is running.
	b.stopSlackTransport()
	if b.slackTransportRunning() {
		t.Fatal("transport should remain stopped after stopSlackTransport no-op")
	}
}

// TestRefreshChannelMapMergesNewSurfaceChannels verifies the live-merge path: a
// channel connected after the transport started is picked up by refreshChannelMap
// without a restart, while existing entries are preserved. This is the mechanism
// EnsureSlackTransportRunning uses when called again on an already-running
// transport (e.g. a second /slack/connect).
func TestRefreshChannelMapMergesNewSurfaceChannels(t *testing.T) {
	b := newTestBroker(t)
	if _, err := b.createSlackChannel("C0001", "general"); err != nil {
		t.Fatalf("connect first channel: %v", err)
	}

	// Transport built against the broker's surface channels at "start" time.
	st := newSlackTransport(b, "bot-token-test", "app-token-test", newFakeSlackAPI())
	if got := st.ChannelMap["C0001"]; got != "slack-general" {
		t.Fatalf("initial ChannelMap[C0001] = %q, want slack-general", got)
	}
	if _, ok := st.ChannelMap["C0002"]; ok {
		t.Fatal("C0002 should not be mapped before it is connected")
	}

	// A second channel is connected after the transport is live.
	if _, err := b.createSlackChannel("C0002", "revops"); err != nil {
		t.Fatalf("connect second channel: %v", err)
	}
	st.refreshChannelMap()

	if got := st.ChannelMap["C0001"]; got != "slack-general" {
		t.Fatalf("after refresh ChannelMap[C0001] = %q, want slack-general (preserved)", got)
	}
	if got := st.ChannelMap["C0002"]; got != "slack-revops" {
		t.Fatalf("after refresh ChannelMap[C0002] = %q, want slack-revops (merged)", got)
	}
}
