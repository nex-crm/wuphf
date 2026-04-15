package team

import (
	"context"
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
	SessionsSend(ctx context.Context, key, message, idempotencyKey string) (*openclaw.SessionsSendResult, error)
	SessionsMessagesSubscribe(ctx context.Context, key string) error
	SessionsMessagesUnsubscribe(ctx context.Context, key string) error
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

	// noticedOffline is true while the circuit breaker has been reported as
	// open via a system message; reset to false when the breaker closes so
	// each offline episode posts exactly one notice (not one per 5-minute
	// supervise tick).
	noticedOffline bool
}

// HasSlug reports whether the given slug is bound to a bridged OpenClaw
// session. Used by the launcher's mention dispatcher to decide whether to
// route a tagged message through the bridge instead of (or in addition to)
// the normal agent-spawn path.
func (b *OpenclawBridge) HasSlug(slug string) bool {
	if b == nil {
		return false
	}
	_, ok := b.keyBySlug[slug]
	return ok
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
			if !b.noticedOffline {
				b.postSystemMessage("openclaw gateway offline")
				b.noticedOffline = true
			}
			select {
			case <-time.After(5 * time.Minute):
				continue
			case <-b.ctx.Done():
				return
			}
		}
		b.noticedOffline = false
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
// broker. session.message events arrive as finalized transcript entries (there
// is no separate streaming path on the daemon's sessions.messages.subscribe).
// We only forward role=assistant messages — user messages in the stream are
// echoes of what we just sent, and forwarding them would double-post.
//
// sessions.changed events with reason=ended post a system notice so humans
// know the agent shut down that conversation.
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
		// Skip user/system echoes — only publish agent replies.
		if evt.SessionMessage.Role != "" && evt.SessionMessage.Role != "assistant" {
			return
		}
		if text := evt.SessionMessage.MessageText; text != "" {
			b.postBridgeMessage(slug, text)
		}
	case openclaw.EventKindChanged:
		if evt.SessionsChanged != nil && evt.SessionsChanged.Reason == "ended" {
			if slug, ok := b.slugByKey[evt.SessionsChanged.SessionKey]; ok {
				b.postSystemMessage(fmt.Sprintf("openclaw agent %q is no longer active", slug))
			}
		}
	case openclaw.EventKindGap:
		// The real OpenClaw daemon does not expose sessions.history, so we have
		// no authoritative catch-up source. Log a system notice instead so the
		// user knows an event was dropped — they can re-prompt if needed.
		if evt.Gap == nil {
			return
		}
		if slug, ok := b.slugByKey[evt.Gap.SessionKey]; ok {
			b.postSystemMessage(fmt.Sprintf("missed %d message(s) from @%s — daemon event gap", evt.Gap.ToSeq-evt.Gap.FromSeq-1, slug))
		}
	case openclaw.EventKindClose:
		// Handled by supervise loop via events-channel close.
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
// idempotency key so the gateway can deduplicate.
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
			_, err := client.SessionsSend(ctx, key, message, idem)
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
func (b *OpenclawBridge) postSystemMessage(text string) {
	if b.broker == nil {
		return
	}
	b.broker.PostSystemMessage("general", "[openclaw] "+text, "openclaw")
}
