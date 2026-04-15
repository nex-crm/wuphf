package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/openclaw"
)

var defaultOpenclawRetryDelays = []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second}

// openclawClient is the subset of internal/openclaw.Client the bridge uses.
// Having it here (not in the openclaw package) keeps the mock test-local.
type openclawClient interface {
	SessionsList(ctx context.Context, f openclaw.SessionsListFilter) ([]openclaw.SessionRow, error)
	SessionsSend(ctx context.Context, key, message, idempotencyKey string) error
	SessionsMessagesSubscribe(ctx context.Context, key string) error
	SessionsMessagesUnsubscribe(ctx context.Context, key string) error
	SessionsHistory(ctx context.Context, key string, sinceSeq int64) ([]openclaw.HistoricMessage, error)
	Events() <-chan openclaw.ClientEvent
	Close() error
}

// openclawDialer produces a fresh openclawClient for each reconnect attempt.
type openclawDialer func(ctx context.Context) (openclawClient, error)

// OpenclawBridge adapts OpenClaw Gateway sessions into WUPHF office members.
type OpenclawBridge struct {
	broker   *Broker
	bindings []config.OpenclawBridgeBinding

	slugByKey map[string]string // sessionKey -> slug
	keyBySlug map[string]string // slug -> sessionKey

	retryDelays []time.Duration // nil = use defaults

	// Reconnect supervisor fields.
	dialer  openclawDialer
	backoff *BridgeBackoff
	breaker *CircuitBreaker

	// client, ctx, and cancel are guarded by mu.
	mu     sync.RWMutex
	client openclawClient
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// NewOpenclawBridge constructs a bridge with a single preconstructed client.
// It does not dial until Start is called. For supervised reconnects, use
// NewOpenclawBridgeWithDialer.
func NewOpenclawBridge(broker *Broker, client openclawClient, bindings []config.OpenclawBridgeBinding) *OpenclawBridge {
	slugByKey := make(map[string]string, len(bindings))
	keyBySlug := make(map[string]string, len(bindings))
	for _, b := range bindings {
		slugByKey[b.SessionKey] = b.Slug
		keyBySlug[b.Slug] = b.SessionKey
	}
	return &OpenclawBridge{
		broker:    broker,
		client:    client,
		bindings:  bindings,
		slugByKey: slugByKey,
		keyBySlug: keyBySlug,
		done:      make(chan struct{}),
	}
}

// NewOpenclawBridgeWithDialer constructs a bridge that supervises reconnects.
// If initial is non-nil, that client is used for the first session; subsequent
// sessions use the dialer. If initial is nil, dialer must be non-nil.
func NewOpenclawBridgeWithDialer(broker *Broker, initial openclawClient, dialer openclawDialer, bindings []config.OpenclawBridgeBinding) *OpenclawBridge {
	b := NewOpenclawBridge(broker, initial, bindings)
	b.dialer = dialer
	b.backoff = NewBridgeBackoff(time.Second, time.Minute)
	b.breaker = NewCircuitBreaker(10, 5*time.Minute)
	return b
}

// setClient and getClient are race-safe accessors.
func (b *OpenclawBridge) setClient(c openclawClient) {
	b.mu.Lock()
	b.client = c
	b.mu.Unlock()
}

func (b *OpenclawBridge) getClient() openclawClient {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.client
}

// Start launches the supervised reconnect loop.
func (b *OpenclawBridge) Start(ctx context.Context) error {
	b.mu.Lock()
	b.ctx, b.cancel = context.WithCancel(ctx)
	b.mu.Unlock()
	go b.supervise()
	return nil
}

// Stop cancels the bridge context, closes the client, and waits for the event loop to drain.
func (b *OpenclawBridge) Stop() {
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	b.mu.Unlock()
	if c := b.getClient(); c != nil {
		_ = c.Close()
	}
	<-b.done
}

// supervise is the reconnect loop. It repeatedly calls runOnce, honoring the
// circuit breaker and backoff on error, and exits cleanly when ctx is cancelled.
func (b *OpenclawBridge) supervise() {
	defer close(b.done)
	for {
		if b.ctx.Err() != nil {
			return
		}
		if b.breaker != nil && b.breaker.Open() {
			b.postSystemMessage("openclaw gateway offline")
			select {
			case <-time.After(5 * time.Minute):
				continue
			case <-b.ctx.Done():
				return
			}
		}
		err := b.runOnce()
		if err != nil && !errors.Is(err, context.Canceled) {
			if b.breaker != nil {
				b.breaker.RecordFailure()
			}
			if b.backoff != nil {
				if werr := b.backoff.Wait(b.ctx); werr != nil {
					return
				}
			} else {
				select {
				case <-time.After(time.Second):
				case <-b.ctx.Done():
					return
				}
			}
			continue
		}
		if b.ctx.Err() != nil {
			return
		}
	}
}

// runOnce establishes a session + subscribes + drains events until close.
// Returns nil on clean ctx cancel, error on dial/subscribe/channel-close failure.
func (b *OpenclawBridge) runOnce() error {
	client := b.getClient()
	if client == nil {
		if b.dialer == nil {
			return fmt.Errorf("openclaw: no dialer configured and no initial client")
		}
		c, err := b.dialer(b.ctx)
		if err != nil {
			return err
		}
		b.setClient(c)
		client = c
	}
	for _, bind := range b.bindings {
		if err := client.SessionsMessagesSubscribe(b.ctx, bind.SessionKey); err != nil {
			_ = client.Close()
			b.setClient(nil)
			return err
		}
	}
	// Successful subscribe => breaker reset (pre-impl decision 5).
	if b.breaker != nil {
		b.breaker.RecordSuccess()
	}
	if b.backoff != nil {
		b.backoff.Reset()
	}

	events := client.Events()
	for {
		select {
		case <-b.ctx.Done():
			return nil
		case evt, ok := <-events:
			if !ok {
				b.setClient(nil)
				return fmt.Errorf("openclaw: connection closed")
			}
			b.handleClientEvent(evt)
		}
	}
}

// handleClientEvent dispatches one event from the OpenClaw Client into the
// appropriate broker surface: delta chunks flow into the per-agent stream so
// they render as a live typing indicator, while finals (and bare messages
// without an explicit state) become chat messages authored by the bridged
// slug. Error/aborted states and "session ended" changes fan out as system
// notices. Gap kinds trigger history catch-up; Close kinds are handled by
// the supervise loop via the events channel close.
func (b *OpenclawBridge) handleClientEvent(evt openclaw.ClientEvent) {
	switch evt.Kind {
	case openclaw.EventKindMessage:
		if evt.SessionMessage == nil {
			return
		}
		slug, ok := b.slugByKey[evt.SessionMessage.SessionKey]
		if !ok {
			return // not a bridged session, ignore
		}
		switch evt.SessionMessage.MessageState {
		case "delta":
			if stream := b.broker.AgentStream(slug); stream != nil && evt.SessionMessage.MessageText != "" {
				stream.Push(evt.SessionMessage.MessageText)
			}
		case "final", "":
			// Treat empty state as a complete message (some servers omit state).
			if text := evt.SessionMessage.MessageText; text != "" {
				b.postBridgeMessage(slug, text)
			}
		case "error":
			b.postSystemMessage(fmt.Sprintf("openclaw agent %q reported an error", slug))
		case "aborted":
			b.postSystemMessage(fmt.Sprintf("openclaw agent %q aborted the turn", slug))
		}
	case openclaw.EventKindChanged:
		if evt.SessionsChanged != nil && evt.SessionsChanged.Reason == "ended" {
			if slug, ok := b.slugByKey[evt.SessionsChanged.SessionKey]; ok {
				b.postSystemMessage(fmt.Sprintf("openclaw agent %q is no longer active", slug))
			}
		}
	case openclaw.EventKindGap:
		if evt.Gap == nil {
			return
		}
		slug, ok := b.slugByKey[evt.Gap.SessionKey]
		if !ok {
			return
		}
		go b.catchUp(slug, evt.Gap.SessionKey, evt.Gap.FromSeq)
	case openclaw.EventKindClose:
		// Handled by supervise loop via events-channel close.
	}
}

// catchUp fetches historic messages since fromSeq and replays final messages
// as bridged "[catch-up] ..." chat messages in #general.
func (b *OpenclawBridge) catchUp(slug, sessionKey string, sinceSeq int64) {
	client := b.getClient()
	if client == nil {
		return
	}
	msgs, err := client.SessionsHistory(b.ctx, sessionKey, sinceSeq)
	if err != nil {
		b.postSystemMessage(fmt.Sprintf("openclaw catch-up failed for @%s: %v", slug, err))
		return
	}
	for _, m := range msgs {
		var inner struct {
			State   string `json:"state"`
			Content string `json:"content"`
			Text    string `json:"text"`
		}
		_ = json.Unmarshal(m.Message, &inner)
		text := inner.Content
		if text == "" {
			text = inner.Text
		}
		if text == "" || (inner.State != "" && inner.State != "final") {
			continue
		}
		b.postBridgeMessage(slug, "[catch-up] "+text)
	}
}

// retryDelaysList returns the configured retry delays, falling back to the
// package default when nothing has been injected.
func (b *OpenclawBridge) retryDelaysList() []time.Duration {
	if b.retryDelays != nil {
		return b.retryDelays
	}
	return defaultOpenclawRetryDelays
}

// SetRetryDelaysForTest is only used by tests.
func (b *OpenclawBridge) SetRetryDelaysForTest(d []time.Duration) { b.retryDelays = d }

// OnOfficeMessage sends an office message from user/@mention to the OpenClaw
// agent identified by slug. Retries on transient errors with a SINGLE reused
// idempotency key (per-call, per pre-implementation decision 3).
func (b *OpenclawBridge) OnOfficeMessage(ctx context.Context, slug, message string) error {
	key, ok := b.keyBySlug[slug]
	if !ok {
		return fmt.Errorf("openclaw: unknown bridged slug %q", slug)
	}
	idem := uuid.NewString()
	delays := b.retryDelaysList()
	var lastErr error
	for attempt := 0; attempt <= len(delays); attempt++ {
		client := b.getClient()
		if client == nil {
			lastErr = fmt.Errorf("openclaw: no active client")
		} else {
			err := client.SessionsSend(ctx, key, message, idem)
			if err == nil {
				return nil
			}
			lastErr = err
		}
		if ctx.Err() != nil {
			break
		}
		if attempt >= len(delays) {
			break
		}
		t := time.NewTimer(delays[attempt])
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
	b.postSystemMessage(fmt.Sprintf("failed to reach @%s: %v", slug, lastErr))
	return lastErr
}

// postBridgeMessage posts a bridged-agent chat message into #general via the
// same broker entrypoint telegram.go uses for incoming chat.
func (b *OpenclawBridge) postBridgeMessage(slug, text string) {
	if b.broker == nil {
		return
	}
	_, _ = b.broker.PostInboundSurfaceMessage(slug, "general", text, "openclaw")
}

// postSystemMessage posts a `system`-authored notice into #general.
// PostSystemMessage already uses sender="system", which the tests rely on.
func (b *OpenclawBridge) postSystemMessage(text string) {
	if b.broker == nil {
		return
	}
	b.broker.PostSystemMessage("general", "[openclaw] "+text, "openclaw")
}
