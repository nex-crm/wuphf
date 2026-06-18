package team

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestSlackPaneStream_StartsAppendsCloses verifies the live stream lifecycle:
// the first append starts the stream (threaded under the pane root), later
// appends extend it, and close appends the disclaimer then stops.
func TestSlackPaneStream_StartsAppendsCloses(t *testing.T) {
	api := newFakeSlackAPI()
	s := &slackPaneStream{api: api, channelID: "D777", threadTS: "900.1"}

	if s.Started() {
		t.Fatal("stream must not be started before the first append")
	}
	if err := s.Append("First sentence."); err != nil {
		t.Fatalf("first append must start the stream: %v", err)
	}
	if !s.Started() {
		t.Fatal("stream must be started after the first append")
	}
	if err := s.Append("Second sentence."); err != nil {
		t.Fatalf("second append: %v", err)
	}
	s.Close(slackPaneDisclaimer)
	if s.Started() {
		t.Fatal("stream must be closed after Close")
	}

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.streamStarts) != 1 {
		t.Fatalf("want exactly one StartStream, got %d", len(api.streamStarts))
	}
	if api.streamStarts[0].ChannelID != "D777" || api.streamStarts[0].ThreadTS != "900.1" {
		t.Fatalf("StartStream must target D777 threaded under 900.1, got %+v", api.streamStarts[0])
	}
	if !strings.Contains(api.streamStarts[0].Chunks, "First sentence.") {
		t.Fatalf("first chunk must carry the opening text, got %q", api.streamStarts[0].Chunks)
	}
	// Second sentence + the disclaimer are two appends.
	if len(api.streamAppends) != 2 {
		t.Fatalf("want two appends (second sentence + disclaimer), got %d: %+v", len(api.streamAppends), api.streamAppends)
	}
	if !strings.Contains(api.streamAppends[1].Chunks, "mistakes") {
		t.Fatalf("the closing append must carry the disclaimer, got %q", api.streamAppends[1].Chunks)
	}
	if len(api.streamStops) != 1 || api.streamStops[0].ThreadTS != "stream.1" {
		t.Fatalf("want one StopStream on the stream ts, got %+v", api.streamStops)
	}
}

// TestSlackPaneStream_StartFailureSignalsFallback verifies a failed StartStream
// reports the error (so the relay falls back to normal posting) and never marks
// the stream started.
func TestSlackPaneStream_StartFailureSignalsFallback(t *testing.T) {
	api := newFakeSlackAPI()
	api.startStreamErr = errors.New("not_authed: assistant feature missing")
	s := &slackPaneStream{api: api, channelID: "D777", threadTS: "900.1"}

	if err := s.Append("Hello there, this is a real reply."); err == nil {
		t.Fatal("a failed StartStream must return an error so the caller falls back")
	}
	if s.Started() {
		t.Fatal("a failed start must not mark the stream started")
	}
	// A second append after failure is a quiet no-op, not a retry storm.
	if err := s.Append("more"); err != nil {
		t.Fatalf("post-failure append must be a quiet no-op, got %v", err)
	}
	// Close after a failed start does nothing (no StopStream on a stream that
	// never opened).
	s.Close(slackPaneDisclaimer)
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.streamStops) != 0 {
		t.Fatalf("a stream that never started must not be stopped, got %+v", api.streamStops)
	}
}

// TestOpenTurnStream_Gating verifies streaming is offered only for an agent's
// own open Slack pane.
func TestOpenTurnStream_Gating(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.slackTransport = tr
	lead := b.OfficeLeadSlug()

	// A shared channel never streams.
	if sink := b.openTurnStream(lead, "slack-general"); sink != nil {
		t.Fatal("a non-DM channel must not stream")
	}
	// The lead's DM, but no pane open yet.
	if sink := b.openTurnStream(lead, DMSlugFor(lead)); sink != nil {
		t.Fatal("a DM with no open pane must not stream")
	}

	// Open the lead's pane.
	tr.seedAssistantThread(context.Background(), "D900", "900.1")
	if sink := b.openTurnStream(lead, DMSlugFor(lead)); sink == nil {
		t.Fatal("the lead's open pane must stream")
	}
	// Another agent's slug talking in the lead's DM does not stream (wrong owner).
	if sink := b.openTurnStream("someone-else", DMSlugFor(lead)); sink != nil {
		t.Fatal("only the DM's own agent may stream into it")
	}
}

// TestRelayStreamsPaneReply is the end-to-end plumbing check: a pane turn streams
// (no separate posts), records exactly one history message, and that message is
// marked already-delivered so the outbound dispatcher will not re-post it.
func TestRelayStreamsPaneReply(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.slackTransport = tr
	lead := b.OfficeLeadSlug()
	dm := DMSlugFor(lead)
	tr.seedAssistantThread(context.Background(), "D900", "900.1")

	l := &Launcher{broker: b}
	root, err := b.PostMessage("you", dm, "What is the status?", nil, "")
	if err != nil {
		t.Fatalf("seed pane message: %v", err)
	}
	relay := newHeadlessLiveChatRelay(l, lead, dm, fmt.Sprintf(`reply_to_id "%s"`, root.ID), nil)

	relay.OnText("Here is the current office status, in detail.")
	relay.Flush()
	if !relay.closeStream("Here is the current office status, in detail.") {
		t.Fatal("a pane turn must report it streamed live")
	}

	api.mu.Lock()
	starts, appends, stops, posts := len(api.streamStarts), len(api.streamAppends), len(api.streamStops), len(api.posts)
	api.mu.Unlock()
	if starts != 1 {
		t.Fatalf("want one StartStream, got %d", starts)
	}
	if stops != 1 {
		t.Fatalf("want one StopStream, got %d", stops)
	}
	_ = appends
	if posts != 0 {
		t.Fatalf("a streamed reply must NOT go through chat.postMessage, got %d posts", posts)
	}

	// Exactly one history message recorded (the human root + the streamed reply).
	msgs := b.ChannelMessages(dm)
	var agentMsgs []channelMessage
	for _, m := range msgs {
		if m.From == lead {
			agentMsgs = append(agentMsgs, m)
		}
	}
	if len(agentMsgs) != 1 {
		t.Fatalf("want exactly one recorded streamed reply, got %d: %+v", len(agentMsgs), agentMsgs)
	}
	// And it must be marked delivered, so the outbound dispatcher skips it.
	for _, m := range b.ExternalQueue("slack") {
		if m.ID == agentMsgs[0].ID {
			t.Fatal("the streamed reply must be marked already-delivered, not re-queued for Slack")
		}
	}
}
