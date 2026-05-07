package team

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/team/transport"
)

// seedSurfaceMessage posts a system message to a channel that's marked as a
// telegram surface so b.ExternalQueue("telegram") will return it on the next
// poll. Using "telegram" as the provider keeps the dispatcher under test
// independent of the real adapter.
func seedSurfaceMessage(t *testing.T, b *Broker, channel, content string) {
	t.Helper()
	b.mu.Lock()
	for i := range b.channels {
		if b.channels[i].Slug == channel {
			b.channels[i].Surface = &channelSurface{Provider: "telegram"}
			break
		}
	}
	b.counter++
	b.messages = append(b.messages, channelMessage{
		ID:        "msg-test-" + content,
		From:      "system",
		Channel:   channel,
		Kind:      "system",
		Content:   content,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	b.mu.Unlock()
}

// TestOutboundDispatcherFormatsAndSendsEachQueuedMessage asserts the dispatcher
// loop calls formatter+sender once per queued message and applies the
// formatter's ok=false skip semantics. Uses a channel-rendezvous instead of
// a sleep so the test is deterministic.
func TestOutboundDispatcherFormatsAndSendsEachQueuedMessage(t *testing.T) {
	b := newTestBroker(t)
	seedSurfaceMessage(t, b, "general", "alpha")
	seedSurfaceMessage(t, b, "general", "beta")
	// Skip-shaped: formatter returns ok=false for messages whose content is
	// "skip-me". Verifies the dispatcher honors the skip and does not call
	// sender for that message.
	seedSurfaceMessage(t, b, "general", "skip-me")

	var sentMu sync.Mutex
	sent := []string{}
	done := make(chan struct{})
	var sendCalls atomic.Int32

	formatter := func(msg channelMessage) (transport.Outbound, bool) {
		if msg.Content == "skip-me" {
			return transport.Outbound{}, false
		}
		return transport.Outbound{
			Binding: transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: msg.Channel},
			Text:    msg.Content,
		}, true
	}
	sender := func(_ context.Context, out transport.Outbound) error {
		sentMu.Lock()
		sent = append(sent, out.Text)
		sentMu.Unlock()
		// Signal as soon as we've seen both expected messages so the test
		// can stop the dispatcher without waiting on the next ticker tick.
		if sendCalls.Add(1) == 2 {
			close(done)
		}
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { _ = runOutboundDispatcher(ctx, b, "telegram", formatter, sender) }()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("dispatcher did not deliver expected messages within timeout (sent=%v)", sent)
	}
	cancel()

	sentMu.Lock()
	defer sentMu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("sent count: got %d (%v), want 2 (skip-me filtered)", len(sent), sent)
	}
	want := map[string]bool{"alpha": true, "beta": true}
	for _, s := range sent {
		if !want[s] {
			t.Errorf("unexpected sent text: %q", s)
		}
	}
}

// TestOutboundDispatcherLogsButContinuesOnSendError asserts a transient
// sender error does not stop the dispatcher loop — subsequent messages on
// later ticks are still delivered. Mirrors the at-least-once-with-drop
// semantics the prior in-adapter loop had.
func TestOutboundDispatcherLogsButContinuesOnSendError(t *testing.T) {
	b := newTestBroker(t)
	seedSurfaceMessage(t, b, "general", "fail-me")
	// Second message arrives on a later poll cycle.

	var deliveredOK atomic.Int32
	formatter := func(msg channelMessage) (transport.Outbound, bool) {
		return transport.Outbound{
			Binding: transport.Binding{Scope: transport.ScopeChannel, ChannelSlug: msg.Channel},
			Text:    msg.Content,
		}, true
	}
	sender := func(_ context.Context, out transport.Outbound) error {
		if out.Text == "fail-me" {
			return errors.New("simulated transient send failure")
		}
		deliveredOK.Add(1)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loopDone := make(chan struct{})
	go func() {
		_ = runOutboundDispatcher(ctx, b, "telegram", formatter, sender)
		close(loopDone)
	}()

	// Wait for the first poll-and-fail cycle, then queue a deliverable
	// message. The dispatcher must still be alive to pick it up.
	time.Sleep(outboundDispatchInterval + 200*time.Millisecond)
	seedSurfaceMessage(t, b, "general", "deliver-me")

	deadline := time.After(3 * outboundDispatchInterval)
	for deliveredOK.Load() != 1 {
		select {
		case <-deadline:
			t.Fatalf("dispatcher stopped after a send error (deliveredOK=%d)", deliveredOK.Load())
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	cancel()
	<-loopDone
}

// TestOutboundDispatcherNilBrokerIsNoop asserts a nil broker returns
// immediately rather than spinning forever — defensive guard for misconfigured
// callers (the launcher today always passes a real broker).
func TestOutboundDispatcherNilBrokerIsNoop(t *testing.T) {
	err := runOutboundDispatcher(context.Background(), nil, "telegram", nil, nil)
	if err != nil {
		t.Errorf("nil broker should return nil, got %v", err)
	}
}
