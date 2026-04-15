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
		"k-gaptest": {
			{SessionKey: "k-gaptest", Message: []byte(`{"state":"final","content":"missed 1"}`)},
			{SessionKey: "k-gaptest", Message: []byte(`{"state":"final","content":"missed 2"}`)},
		},
	}
	broker := NewBroker()
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k-gaptest", Slug: "openclaw-gaptest"}}
	b := NewOpenclawBridge(broker, fake, bindings)
	_ = b.Start(context.Background())
	defer b.Stop()

	// The broker may have persisted messages from prior runs. Snapshot the catch-up
	// count BEFORE pushing the gap event and count only the delta.
	before := countCatchups(broker, "openclaw-gaptest")

	fake.events <- openclaw.ClientEvent{
		Kind: openclaw.EventKindGap,
		Gap:  &openclaw.GapEvent{SessionKey: "k-gaptest", FromSeq: 5, ToSeq: 7},
	}
	time.Sleep(100 * time.Millisecond)
	delta := countCatchups(broker, "openclaw-gaptest") - before
	if delta != 2 {
		t.Fatalf("expected 2 NEW catch-up messages, got %d (broker state may be polluted)", delta)
	}
}

func countCatchups(broker *Broker, slug string) int {
	n := 0
	for _, m := range broker.AllMessages() {
		if m.From == slug && strings.Contains(m.Content, "[catch-up]") {
			n++
		}
	}
	return n
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

// TestStartOpenclawBridgeFromConfigNoBindings confirms the bootstrap is a
// no-op when config has no bridges — the integration is opt-in and must not
// dial the gateway or spin up a supervise goroutine.
func TestStartOpenclawBridgeFromConfigNoBindings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// No config file written; Load() will return zero-value Config.
	broker := NewBroker()
	bridge, err := StartOpenclawBridgeFromConfig(context.Background(), broker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bridge != nil {
		t.Fatalf("expected nil bridge when no bindings are configured, got %+v", bridge)
	}
}

// TestStartOpenclawBridgeFromConfigWithBindings confirms the bootstrap builds
// and starts a supervised bridge when bindings are present. We inject a fake
// dialer so this test never touches the real gateway.
func TestStartOpenclawBridgeFromConfigWithBindings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	cfg := config.Config{
		OpenclawBridges: []config.OpenclawBridgeBinding{
			{SessionKey: "boot-k", Slug: "openclaw-boot", DisplayName: "Boot"},
		},
	}
	if err := config.Save(cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	fake := newFakeOC()
	openclawBootstrapDialer = func(ctx context.Context) (openclawClient, error) { return fake, nil }
	defer func() { openclawBootstrapDialer = nil }()

	broker := NewBroker()
	bridge, err := StartOpenclawBridgeFromConfig(context.Background(), broker)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if bridge == nil {
		t.Fatal("expected non-nil bridge when bindings are configured")
	}
	defer bridge.Stop()

	// Give supervise a tick to dial + subscribe.
	time.Sleep(80 * time.Millisecond)
	fake.mu.Lock()
	subs := len(fake.subscribed)
	fake.mu.Unlock()
	if subs == 0 {
		t.Fatal("expected bootstrap to subscribe the bound session")
	}
	if !bridge.HasSlug("openclaw-boot") {
		t.Fatal("HasSlug should report true for bound slug")
	}
	if bridge.HasSlug("not-bridged") {
		t.Fatal("HasSlug should report false for unknown slug")
	}
}

// TestRouteOpenclawMentionsLoopForwardsHumanMention confirms the mention
// dispatcher invokes OnOfficeMessage when a human posts an @mention that
// matches a bridged slug — the missing runtime link this PR closes.
func TestRouteOpenclawMentionsLoopForwardsHumanMention(t *testing.T) {
	fake := newFakeOC()
	broker := NewBroker()
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "mk", Slug: "openclaw-mentions"}}
	bridge := NewOpenclawBridge(broker, fake, bindings)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("bridge start: %v", err)
	}
	defer bridge.Stop()

	// Kick off the mention-routing loop.
	go routeOpenclawMentionsLoop(ctx, broker, bridge)
	// Let the subscriber register before we publish.
	time.Sleep(20 * time.Millisecond)

	// Simulate a human posting into #general with the bridged slug tagged.
	broker.mu.Lock()
	broker.counter++
	msg := channelMessage{
		ID:        "msg-mention-1",
		From:      "human",
		Channel:   "general",
		Content:   "ping from the office",
		Tagged:    []string{"openclaw-mentions"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	broker.appendMessageLocked(msg)
	broker.mu.Unlock()

	// Allow the subscriber + OnOfficeMessage goroutine to run.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		fake.mu.Lock()
		got := len(fake.sentKeys)
		fake.mu.Unlock()
		if got >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.sentKeys) != 1 {
		t.Fatalf("expected 1 forwarded send, got %d: %v", len(fake.sentKeys), fake.sentKeys)
	}
	if !strings.HasPrefix(fake.sentKeys[0], "mk|ping from the office|") {
		t.Fatalf("forwarded send has unexpected shape: %q", fake.sentKeys[0])
	}
}

// TestRouteOpenclawMentionsLoopIgnoresUnrelated confirms the dispatcher is
// narrow: it does not forward system messages, agent-to-agent chatter, or
// mentions of non-bridged slugs. Without this the bridge would double-post
// every office message.
func TestRouteOpenclawMentionsLoopIgnoresUnrelated(t *testing.T) {
	fake := newFakeOC()
	broker := NewBroker()
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "mk", Slug: "openclaw-only"}}
	bridge := NewOpenclawBridge(broker, fake, bindings)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = bridge.Start(ctx)
	defer bridge.Stop()

	go routeOpenclawMentionsLoop(ctx, broker, bridge)
	time.Sleep(20 * time.Millisecond)

	// Three negatives: system author, agent author, and mention of
	// a non-bridged slug. None should reach the gateway.
	cases := []channelMessage{
		{ID: "msg-neg-1", From: "system", Channel: "general", Content: "sys", Tagged: []string{"openclaw-only"}},
		{ID: "msg-neg-2", From: "ceo", Channel: "general", Content: "agent", Tagged: []string{"openclaw-only"}},
		{ID: "msg-neg-3", From: "human", Channel: "general", Content: "wrong", Tagged: []string{"someone-else"}},
	}
	for _, m := range cases {
		broker.mu.Lock()
		broker.counter++
		m.Timestamp = time.Now().UTC().Format(time.RFC3339)
		broker.appendMessageLocked(m)
		broker.mu.Unlock()
	}
	time.Sleep(150 * time.Millisecond)

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.sentKeys) != 0 {
		t.Fatalf("expected 0 forwarded sends for unrelated messages, got %v", fake.sentKeys)
	}
}

// TestSuperviseOfflineNoticeDeduped drives the supervise loop into
// breaker-open state and confirms the "openclaw gateway offline" system
// message is posted at most once per episode — not every 5-minute tick
// (reviewer Important issue 5).
func TestSuperviseOfflineNoticeDeduped(t *testing.T) {
	broker := NewBroker()
	dialer := func(ctx context.Context) (openclawClient, error) {
		return nil, errors.New("dial refused")
	}
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k", Slug: "openclaw-dead"}}
	bridge := NewOpenclawBridgeWithDialer(broker, nil, dialer, bindings)
	// Fast-fail backoff + low threshold so the breaker trips quickly.
	bridge.backoff = NewBridgeBackoff(1*time.Millisecond, 2*time.Millisecond)
	bridge.breaker = NewCircuitBreaker(2, 5*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	_ = bridge.Start(ctx)

	// Give supervise time to fail twice (trips breaker) and iterate the
	// breaker-open branch several times. Without the noticedOffline guard
	// each iteration would post another system message.
	time.Sleep(200 * time.Millisecond)
	cancel()
	bridge.Stop()

	n := 0
	for _, m := range broker.AllMessages() {
		if m.From == "system" && strings.Contains(m.Content, "openclaw gateway offline") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 offline notice per breaker-open episode, got %d", n)
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
