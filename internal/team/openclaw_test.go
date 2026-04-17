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
	mu            sync.Mutex
	sentKeys      []string
	subscribed    []string
	unsubscribed  []string
	createdAgents []string // agentID values passed to SessionsCreate
	createdLabels []string
	createNextKey string // key returned by the next SessionsCreate call; auto-incremented if empty
	createCounter int
	createErr     error
	events        chan openclaw.ClientEvent
	sendErr       error
	nextSendErrs  []error // drained FIFO if non-empty
	closed        bool
}

func newFakeOC() *fakeOCClient {
	return &fakeOCClient{events: make(chan openclaw.ClientEvent, 8)}
}

func (f *fakeOCClient) SessionsList(ctx context.Context, _ openclaw.SessionsListFilter) ([]openclaw.SessionRow, error) {
	return nil, nil
}

func (f *fakeOCClient) SessionsSend(ctx context.Context, key, msg, idem string) (*openclaw.SessionsSendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sentKeys = append(f.sentKeys, key+"|"+msg+"|"+idem)
	if len(f.nextSendErrs) > 0 {
		err := f.nextSendErrs[0]
		f.nextSendErrs = f.nextSendErrs[1:]
		if err != nil {
			return nil, err
		}
	}
	if f.sendErr != nil {
		return nil, f.sendErr
	}
	return &openclaw.SessionsSendResult{RunID: "run-" + idem, Status: "started"}, nil
}

func (f *fakeOCClient) SessionsMessagesSubscribe(ctx context.Context, key string) error {
	f.mu.Lock()
	f.subscribed = append(f.subscribed, key)
	f.mu.Unlock()
	return nil
}

func (f *fakeOCClient) SessionsMessagesUnsubscribe(ctx context.Context, key string) error {
	f.mu.Lock()
	f.unsubscribed = append(f.unsubscribed, key)
	f.mu.Unlock()
	return nil
}

func (f *fakeOCClient) SessionsCreate(ctx context.Context, agentID, label string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return "", f.createErr
	}
	f.createdAgents = append(f.createdAgents, agentID)
	f.createdLabels = append(f.createdLabels, label)
	if f.createNextKey != "" {
		key := f.createNextKey
		f.createNextKey = ""
		return key, nil
	}
	f.createCounter++
	return "fake-session-key-" + strconvItoa(f.createCounter), nil
}

// strconvItoa is a file-local helper so we don't need to import strconv here.
func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
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
	time.Sleep(50 * time.Millisecond)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.subscribed) != 2 {
		t.Fatalf("expected 2 subscriptions, got %d: %v", len(fake.subscribed), fake.subscribed)
	}
}

// TestHandleClientEventForwardsAssistantMessage confirms an assistant reply
// emitted on sessions.messages.subscribe is posted into #general under the
// bridged slug's name. The real OpenClaw daemon sends every transcript update
// as a single session.message event — there is no delta/final split.
func TestHandleClientEventForwardsAssistantMessage(t *testing.T) {
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

	seq := int64(2)
	fake.events <- openclaw.ClientEvent{
		Kind: openclaw.EventKindMessage,
		SessionMessage: &openclaw.SessionMessageEvent{
			SessionKey:  "k",
			MessageSeq:  &seq,
			Role:        "assistant",
			MessageText: "complete response",
		},
	}
	time.Sleep(30 * time.Millisecond)
	found := false
	for _, m := range broker.AllMessages() {
		if m.From == "openclaw-a" && m.Content == "complete response" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected assistant message posted to broker from openclaw-a; got %+v", broker.AllMessages())
	}
}

// TestHandleClientEventSkipsUserRole confirms the bridge does NOT re-post
// user-role echoes — otherwise every message the user sends via OnOfficeMessage
// would boomerang back into #general as though the bridged agent had spoken.
//
// We snapshot the broker state before emitting the event and assert the delta
// is zero, not that openclaw-a never appears, because NewBroker() rehydrates
// from persisted state that may contain messages from prior runs or tests.
func TestHandleClientEventSkipsUserRole(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := newFakeOC()
	broker := NewBroker()
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k", Slug: "openclaw-a"}}
	b := NewOpenclawBridge(broker, fake, bindings)
	_ = b.Start(context.Background())
	defer b.Stop()

	before := len(broker.AllMessages())

	seq := int64(0)
	fake.events <- openclaw.ClientEvent{
		Kind: openclaw.EventKindMessage,
		SessionMessage: &openclaw.SessionMessageEvent{
			SessionKey:  "k",
			MessageSeq:  &seq,
			Role:        "user",
			MessageText: "hello from office",
		},
	}
	time.Sleep(60 * time.Millisecond)

	after := broker.AllMessages()
	if len(after) != before {
		for _, m := range after[before:] {
			t.Fatalf("bridge must not echo user-role messages; got %+v", m)
		}
	}
}

func TestOnOfficeMessageSuccess(t *testing.T) {
	fake := newFakeOC()
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k", Slug: "openclaw-a"}}
	b := NewOpenclawBridge(NewBroker(), fake, bindings)
	_ = b.Start(context.Background())
	defer b.Stop()
	err := b.OnOfficeMessage(context.Background(), "openclaw-a", "general", "hello")
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
	b.SetRetryDelaysForTest([]time.Duration{10 * time.Millisecond, 10 * time.Millisecond})
	_ = b.Start(context.Background())
	defer b.Stop()
	err := b.OnOfficeMessage(context.Background(), "openclaw-a", "general", "hello")
	if err != nil {
		t.Fatalf("expected retry to succeed: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.sentKeys) != 3 {
		t.Fatalf("expected 3 send attempts, got %d: %v", len(fake.sentKeys), fake.sentKeys)
	}
	// All three attempts MUST reuse the same idempotency key (last field).
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

// TestGapEventPostsSystemNotice replaces the prior catch-up-replay test. The
// real OpenClaw daemon does not expose sessions.history, so the bridge can't
// reconstruct missed messages — it now surfaces a system notice so a human
// can re-prompt.
func TestGapEventPostsSystemNotice(t *testing.T) {
	fake := newFakeOC()
	broker := NewBroker()
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k-gap", Slug: "openclaw-gap"}}
	b := NewOpenclawBridge(broker, fake, bindings)
	_ = b.Start(context.Background())
	defer b.Stop()

	before := 0
	for _, m := range broker.AllMessages() {
		if m.From == "system" && strings.Contains(m.Content, "daemon event gap") {
			before++
		}
	}

	fake.events <- openclaw.ClientEvent{
		Kind: openclaw.EventKindGap,
		Gap:  &openclaw.GapEvent{SessionKey: "k-gap", FromSeq: 5, ToSeq: 7},
	}
	time.Sleep(100 * time.Millisecond)

	after := 0
	for _, m := range broker.AllMessages() {
		if m.From == "system" && strings.Contains(m.Content, "daemon event gap") {
			after++
		}
	}
	if after-before != 1 {
		t.Fatalf("expected exactly 1 new gap notice, got %d (before=%d after=%d)", after-before, before, after)
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

	time.Sleep(40 * time.Millisecond)
	mu.Lock()
	first := clients[0]
	mu.Unlock()

	_ = first.Close()

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

func TestStartOpenclawBridgeFromConfigNoBindings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	broker := NewBroker()
	bridge, err := StartOpenclawBridgeFromConfig(context.Background(), broker)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if bridge != nil {
		t.Fatalf("expected nil bridge when no bindings are configured, got %+v", bridge)
	}
}

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

	// Bridged session must be registered as an office member, otherwise
	// users can't see it in the sidebar or autocomplete @mentions against it.
	var member *officeMember
	for _, m := range broker.OfficeMembers() {
		m := m
		if m.Slug == "openclaw-boot" {
			member = &m
			break
		}
	}
	if member == nil {
		t.Fatal("bound slug should be registered as an office member")
	}
	if member.Name != "Boot" {
		t.Fatalf("member name: got %q want %q", member.Name, "Boot")
	}
	if member.CreatedBy != "openclaw" {
		t.Fatalf("member CreatedBy: got %q want %q", member.CreatedBy, "openclaw")
	}
	// And #general should list the slug so routing + mention autocomplete work.
	broker.mu.Lock()
	var generalHasBridged bool
	for _, ch := range broker.channels {
		if ch.Slug == "general" {
			for _, s := range ch.Members {
				if s == "openclaw-boot" {
					generalHasBridged = true
					break
				}
			}
			break
		}
	}
	broker.mu.Unlock()
	if !generalHasBridged {
		t.Fatal("bridged slug should be a member of #general")
	}
}

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

	go routeOpenclawMentionsLoop(ctx, broker, bridge)
	time.Sleep(20 * time.Millisecond)

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

func TestRouteOpenclawMentionsLoopForwardsDMPost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := newFakeOC()
	broker := NewBroker()
	// Register the bridged agent as a real office member so a DM channel can
	// be opened against its slug.
	if err := broker.EnsureBridgedMember("openclaw-dm", "DM Bot", "openclaw"); err != nil {
		t.Fatalf("ensure bridged member: %v", err)
	}
	dmSlug, err := broker.ChannelStore().GetOrCreateDirect("human", "openclaw-dm")
	if err != nil {
		t.Fatalf("open DM channel: %v", err)
	}
	// Mirror the DM into the broker's channel table so findChannelLocked
	// resolves it — ensureDMConversationLocked does the same when a surface
	// post lands first.
	broker.mu.Lock()
	broker.channels = append(broker.channels, teamChannel{
		Slug:    dmSlug.Slug,
		Name:    dmSlug.Slug,
		Type:    "dm",
		Members: []string{"human", "openclaw-dm"},
	})
	broker.mu.Unlock()

	bindings := []config.OpenclawBridgeBinding{{SessionKey: "sk-dm", Slug: "openclaw-dm"}}
	bridge := NewOpenclawBridge(broker, fake, bindings)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("bridge start: %v", err)
	}
	defer bridge.Stop()

	go routeOpenclawMentionsLoop(ctx, broker, bridge)
	time.Sleep(20 * time.Millisecond)

	broker.mu.Lock()
	broker.counter++
	msg := channelMessage{
		ID:        "msg-dm-1",
		From:      "human",
		Channel:   dmSlug.Slug,
		Content:   "hey, quick q",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	broker.appendMessageLocked(msg)
	broker.mu.Unlock()

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
		t.Fatalf("expected 1 forwarded DM send, got %d: %v", len(fake.sentKeys), fake.sentKeys)
	}
	if !strings.HasPrefix(fake.sentKeys[0], "sk-dm|hey, quick q|") {
		t.Fatalf("forwarded DM send has unexpected shape: %q", fake.sentKeys[0])
	}
}

func TestRouteOpenclawMentionsLoopDedupesDMAndMention(t *testing.T) {
	// Guards against a regression where a DM post that also happens to
	// @mention the partner would fire OnOfficeMessage twice.
	t.Setenv("HOME", t.TempDir())
	fake := newFakeOC()
	broker := NewBroker()
	if err := broker.EnsureBridgedMember("openclaw-both", "Both Bot", "openclaw"); err != nil {
		t.Fatalf("ensure bridged member: %v", err)
	}
	dm, err := broker.ChannelStore().GetOrCreateDirect("human", "openclaw-both")
	if err != nil {
		t.Fatalf("open DM: %v", err)
	}
	broker.mu.Lock()
	broker.channels = append(broker.channels, teamChannel{
		Slug: dm.Slug, Type: "dm", Members: []string{"human", "openclaw-both"},
	})
	broker.mu.Unlock()

	bindings := []config.OpenclawBridgeBinding{{SessionKey: "sk-both", Slug: "openclaw-both"}}
	bridge := NewOpenclawBridge(broker, fake, bindings)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := bridge.Start(ctx); err != nil {
		t.Fatalf("bridge start: %v", err)
	}
	defer bridge.Stop()

	go routeOpenclawMentionsLoop(ctx, broker, bridge)
	time.Sleep(20 * time.Millisecond)

	broker.mu.Lock()
	broker.counter++
	broker.appendMessageLocked(channelMessage{
		ID:        "msg-dedupe-1",
		From:      "human",
		Channel:   dm.Slug,
		Content:   "@openclaw-both hey",
		Tagged:    []string{"openclaw-both"},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	})
	broker.mu.Unlock()

	time.Sleep(150 * time.Millisecond)
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.sentKeys) != 1 {
		t.Fatalf("expected 1 forwarded send (deduped), got %d: %v", len(fake.sentKeys), fake.sentKeys)
	}
}

func TestSuperviseOfflineNoticeDeduped(t *testing.T) {
	broker := NewBroker()
	dialer := func(ctx context.Context) (openclawClient, error) {
		return nil, errors.New("dial refused")
	}
	bindings := []config.OpenclawBridgeBinding{{SessionKey: "k", Slug: "openclaw-dead"}}
	bridge := NewOpenclawBridgeWithDialer(broker, nil, dialer, bindings)
	bridge.backoff = NewBridgeBackoff(1*time.Millisecond, 2*time.Millisecond)
	bridge.breaker = NewCircuitBreaker(2, 5*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	_ = bridge.Start(ctx)

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
	err := b.OnOfficeMessage(context.Background(), "openclaw-a", "general", "hello")
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
