package team

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/openclaw"
)

type fakeOCClient struct {
	mu           sync.Mutex
	sentKeys     []string
	subscribed   []string
	events       chan openclaw.ClientEvent
	sendErr      error
	nextSendErrs []error // drained FIFO if non-empty
	historyByKey map[string][]openclaw.HistoricMessage
	closed       bool
}

func newFakeOC() *fakeOCClient {
	return &fakeOCClient{events: make(chan openclaw.ClientEvent, 8)}
}

func (f *fakeOCClient) SessionsList(ctx context.Context, _ openclaw.SessionsListFilter) ([]openclaw.SessionRow, error) {
	return nil, nil
}

func (f *fakeOCClient) SessionsSend(ctx context.Context, key, msg, idem string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentKeys = append(f.sentKeys, key+"|"+msg+"|"+idem)
	if len(f.nextSendErrs) > 0 {
		err := f.nextSendErrs[0]
		f.nextSendErrs = f.nextSendErrs[1:]
		return err
	}
	return f.sendErr
}

func (f *fakeOCClient) SessionsMessagesSubscribe(ctx context.Context, key string) error {
	f.mu.Lock()
	f.subscribed = append(f.subscribed, key)
	f.mu.Unlock()
	return nil
}

func (f *fakeOCClient) SessionsMessagesUnsubscribe(ctx context.Context, key string) error {
	return nil
}

func (f *fakeOCClient) SessionsHistory(ctx context.Context, key string, sinceSeq int64) ([]openclaw.HistoricMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.historyByKey[key], nil
}

func (f *fakeOCClient) Events() <-chan openclaw.ClientEvent { return f.events }
func (f *fakeOCClient) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.events)
	return nil
}

func TestBridgeStartSubscribesAllBindings(t *testing.T) {
	fake := newFakeOC()
	bindings := []config.OpenclawBridgeBinding{
		{SessionKey: "k1", Slug: "openclaw-a", DisplayName: "A"},
		{SessionKey: "k2", Slug: "openclaw-b", DisplayName: "B"},
	}
	b := NewOpenclawBridge(nil /* broker */, fake, bindings)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Give the subscribe loop a moment.
	time.Sleep(50 * time.Millisecond)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.subscribed) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d: %v", len(fake.subscribed), fake.subscribed)
	}
	_ = errors.New
}

func TestHandleClientEventSplitsDeltaAndFinal(t *testing.T) {
	fake := newFakeOC()
	broker := NewBroker()
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k", Slug: "openclaw-a", DisplayName: "A"}}
	b := NewOpenclawBridge(broker, fake, bindings)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer b.Stop()

	// Push a delta event.
	seq := int64(1)
	fake.events <- openclaw.ClientEvent{
		Kind: openclaw.EventKindMessage,
		SessionMessage: &openclaw.SessionMessageEvent{
			SessionKey:   "k",
			MessageSeq:   &seq,
			MessageState: "delta",
			MessageText:  "partial chunk",
		},
	}
	// Give event loop a tick.
	time.Sleep(30 * time.Millisecond)
	// agentStreamBuffer has an unexported recent() usable from within internal/team tests.
	if buf := broker.AgentStream("openclaw-a"); buf == nil || len(buf.recent()) == 0 {
		t.Fatalf("expected delta in AgentStream for openclaw-a")
	}

	// Push a final event.
	seq2 := int64(2)
	fake.events <- openclaw.ClientEvent{
		Kind: openclaw.EventKindMessage,
		SessionMessage: &openclaw.SessionMessageEvent{
			SessionKey:   "k",
			MessageSeq:   &seq2,
			MessageState: "final",
			MessageText:  "complete response",
		},
	}
	time.Sleep(30 * time.Millisecond)
	msgs := broker.AllMessages()
	found := false
	for _, m := range msgs {
		if m.From == "openclaw-a" && m.Content == "complete response" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected final message posted to broker from openclaw-a; got %+v", msgs)
	}
}

func TestOnOfficeMessageSuccess(t *testing.T) {
	fake := newFakeOC()
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k", Slug: "openclaw-a"}}
	b := NewOpenclawBridge(NewBroker(), fake, bindings)
	_ = b.Start(context.Background())
	defer b.Stop()
	err := b.OnOfficeMessage(context.Background(), "openclaw-a", "hello")
	if err != nil {
		t.Fatalf("OnOfficeMessage: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.sentKeys) != 1 {
		t.Fatalf("expected 1 send, got %v", fake.sentKeys)
	}
}

func TestOnOfficeMessageRetriesTransient(t *testing.T) {
	fake := newFakeOC()
	fake.nextSendErrs = []error{errors.New("transient 1"), errors.New("transient 2")} // first two fail, third succeeds
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k", Slug: "openclaw-a"}}
	b := NewOpenclawBridge(NewBroker(), fake, bindings)
	// Shrink retry delays for the test.
	b.SetRetryDelaysForTest([]time.Duration{10 * time.Millisecond, 10 * time.Millisecond})
	_ = b.Start(context.Background())
	defer b.Stop()
	err := b.OnOfficeMessage(context.Background(), "openclaw-a", "hello")
	if err != nil {
		t.Fatalf("expected retry to succeed: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.sentKeys) != 3 {
		t.Fatalf("expected 3 send attempts, got %d: %v", len(fake.sentKeys), fake.sentKeys)
	}
	// All three attempts MUST reuse the same idempotency key (last field).
	// Each entry is "key|message|idempotencyKey".
	prev := ""
	for _, entry := range fake.sentKeys {
		parts := strings.Split(entry, "|")
		if len(parts) != 3 {
			t.Fatalf("malformed sentKeys entry: %q", entry)
		}
		if prev == "" {
			prev = parts[2]
			continue
		}
		if parts[2] != prev {
			t.Fatalf("idempotency key changed across retries: %v", fake.sentKeys)
		}
	}
}

func TestGapEventTriggersHistoryReplay(t *testing.T) {
	fake := newFakeOC()
	fake.historyByKey = map[string][]openclaw.HistoricMessage{
		"k": {
			{SessionKey: "k", Message: []byte(`{"state":"final","content":"missed 1"}`)},
			{SessionKey: "k", Message: []byte(`{"state":"final","content":"missed 2"}`)},
		},
	}
	broker := NewBroker()
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k", Slug: "openclaw-a"}}
	b := NewOpenclawBridge(broker, fake, bindings)
	_ = b.Start(context.Background())
	defer b.Stop()

	fake.events <- openclaw.ClientEvent{
		Kind: openclaw.EventKindGap,
		Gap:  &openclaw.GapEvent{SessionKey: "k", FromSeq: 5, ToSeq: 7},
	}
	time.Sleep(100 * time.Millisecond)
	msgs := broker.AllMessages()
	catchupCount := 0
	for _, m := range msgs {
		if m.From == "openclaw-a" && strings.Contains(m.Content, "[catch-up]") {
			catchupCount++
		}
	}
	if catchupCount != 2 {
		t.Fatalf("expected 2 catch-up messages, got %d: %v", catchupCount, msgs)
	}
}

func TestReconnectOnClientClose(t *testing.T) {
	var mu sync.Mutex
	dialCount := 0
	clients := []*fakeOCClient{}
	dialer := func(ctx context.Context) (openclawClient, error) {
		mu.Lock()
		defer mu.Unlock()
		dialCount++
		c := newFakeOC()
		clients = append(clients, c)
		return c, nil
	}
	broker := NewBroker()
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k", Slug: "openclaw-a"}}
	b := NewOpenclawBridgeWithDialer(broker, nil, dialer, bindings)
	b.backoff = NewBridgeBackoff(5*time.Millisecond, 50*time.Millisecond)
	_ = b.Start(context.Background())
	defer b.Stop()

	// Let supervise dial + subscribe the first client.
	time.Sleep(40 * time.Millisecond)
	mu.Lock()
	first := clients[0]
	mu.Unlock()

	// Force the first client's event channel to close, simulating a drop.
	_ = first.Close()

	// Allow reconnect.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if dialCount < 2 {
		t.Fatalf("expected reconnect, dialCount=%d", dialCount)
	}
	latest := clients[len(clients)-1]
	latest.mu.Lock()
	subs := len(latest.subscribed)
	latest.mu.Unlock()
	if subs == 0 {
		t.Fatal("reconnected client was not re-subscribed")
	}
}

func TestOnOfficeMessagePermanentFailurePostsSystemMessage(t *testing.T) {
	fake := newFakeOC()
	fake.sendErr = errors.New("forever broken")
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k", Slug: "openclaw-a"}}
	broker := NewBroker()
	b := NewOpenclawBridge(broker, fake, bindings)
	b.SetRetryDelaysForTest([]time.Duration{5 * time.Millisecond, 5 * time.Millisecond})
	_ = b.Start(context.Background())
	defer b.Stop()
	err := b.OnOfficeMessage(context.Background(), "openclaw-a", "hello")
	if err == nil {
		t.Fatal("expected permanent failure error")
	}
	msgs := broker.AllMessages()
	sysFound := false
	for _, m := range msgs {
		if m.From == "system" {
			sysFound = true
			break
		}
	}
	if !sysFound {
		t.Fatal("expected system message posted on permanent failure")
	}
}
