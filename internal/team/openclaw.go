package team

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	SessionsCreate(ctx context.Context, agentID, label string) (string, error)
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

	// lastChannelByKey remembers where the most recent human-authored send for
	// each session came from, so when the assistant reply arrives via the
	// async event stream we can post it back to the same channel (DM, #general,
	// thread, etc.) instead of always falling back to #general. Guarded by mu.
	lastChannelByKey map[string]string

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
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.keyBySlug[slug]
	return ok
}

// AttachSlug subscribes the bridge to a session and registers the slug→key
// mapping so inbound events route to the right member. Called at startup for
// every openclaw-bound office member and at runtime from handleOfficeMembers
// when a new openclaw agent is created. Idempotent: attaching an already-
// attached slug is a no-op. Callers should hold broker.mu to serialize vs
// other member mutations; this method takes only the bridge's own mu.
func (b *OpenclawBridge) AttachSlug(ctx context.Context, slug, sessionKey string) error {
	if b == nil {
		return fmt.Errorf("openclaw: nil bridge")
	}
	slug = strings.TrimSpace(slug)
	sessionKey = strings.TrimSpace(sessionKey)
	if slug == "" || sessionKey == "" {
		return fmt.Errorf("openclaw: AttachSlug requires non-empty slug and sessionKey")
	}
	b.mu.RLock()
	existingKey, already := b.keyBySlug[slug]
	b.mu.RUnlock()
	if already && existingKey == sessionKey {
		return nil
	}
	client := b.getClient()
	if client != nil {
		if err := client.SessionsMessagesSubscribe(ctx, sessionKey); err != nil {
			return fmt.Errorf("openclaw: subscribe %q: %w", slug, err)
		}
	}
	b.mu.Lock()
	if already && existingKey != sessionKey {
		delete(b.slugByKey, existingKey)
		delete(b.lastChannelByKey, existingKey)
	}
	b.slugByKey[sessionKey] = slug
	b.keyBySlug[slug] = sessionKey
	b.mu.Unlock()
	return nil
}

// DetachSlug unsubscribes and removes the slug from bridge maps. Best-effort
// on the network call: if the gateway is unreachable we still clear local
// state so the slug frees up; the returned error informs the caller that the
// remote session may be leaked. Used by handleOfficeMembers on member remove
// and by provider-switch flows when a member migrates off openclaw.
func (b *OpenclawBridge) DetachSlug(ctx context.Context, slug string) error {
	if b == nil {
		return nil
	}
	slug = strings.TrimSpace(slug)
	b.mu.Lock()
	sessionKey := b.keyBySlug[slug]
	if sessionKey == "" {
		b.mu.Unlock()
		return nil
	}
	delete(b.keyBySlug, slug)
	delete(b.slugByKey, sessionKey)
	delete(b.lastChannelByKey, sessionKey)
	b.mu.Unlock()

	client := b.getClient()
	if client == nil {
		return nil
	}
	if err := client.SessionsMessagesUnsubscribe(ctx, sessionKey); err != nil {
		return fmt.Errorf("openclaw: unsubscribe %q: %w", slug, err)
	}
	return nil
}

// CreateSession calls sessions.create on the gateway and returns the new key.
// Used by handleOfficeMembers when a user hires a new openclaw agent without
// supplying an existing session key (the "auto-create" path).
func (b *OpenclawBridge) CreateSession(ctx context.Context, agentID, label string) (string, error) {
	if b == nil {
		return "", fmt.Errorf("openclaw: nil bridge")
	}
	client := b.getClient()
	if client == nil {
		return "", fmt.Errorf("openclaw: no active client")
	}
	return client.SessionsCreate(ctx, agentID, label)
}

// SnapshotBindings returns a copy of the current slug→sessionKey mapping.
// Used by runOnce on reconnect to re-subscribe every attached slug.
func (b *OpenclawBridge) SnapshotBindings() map[string]string {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[string]string, len(b.keyBySlug))
	for slug, key := range b.keyBySlug {
		out[slug] = key
	}
	return out
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
		broker:           broker,
		client:           client,
		bindings:         bindings,
		slugByKey:        slugByKey,
		keyBySlug:        keyBySlug,
		lastChannelByKey: make(map[string]string, len(bindings)),
		done:             make(chan struct{}),
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
	// Re-subscribe every currently-attached slug. On first run the set is
	// seeded by StartOpenclawBridgeFromConfig via AttachSlug. On reconnect it
	// contains whatever AttachSlug / DetachSlug have accumulated since.
	//
	// b.bindings is the legacy input for tests that construct the bridge with
	// a static array. When present we also subscribe those, which is what the
	// existing openclaw_test.go cases expect.
	seedKeys := make(map[string]struct{})
	for _, bind := range b.bindings {
		seedKeys[bind.SessionKey] = struct{}{}
		if err := client.SessionsMessagesSubscribe(b.ctx, bind.SessionKey); err != nil {
			_ = client.Close()
			b.setClient(nil)
			return err
		}
		// Seed bridge maps from static bindings the first time runOnce runs.
		// Idempotent on reconnect because AttachSlug short-circuits when the
		// pair is already set.
		b.mu.Lock()
		if _, ok := b.slugByKey[bind.SessionKey]; !ok {
			b.slugByKey[bind.SessionKey] = bind.Slug
			b.keyBySlug[bind.Slug] = bind.SessionKey
		}
		b.mu.Unlock()
	}
	for slug, sessionKey := range b.SnapshotBindings() {
		if _, alreadySeeded := seedKeys[sessionKey]; alreadySeeded {
			continue
		}
		if err := client.SessionsMessagesSubscribe(b.ctx, sessionKey); err != nil {
			_ = client.Close()
			b.setClient(nil)
			return fmt.Errorf("resubscribe %q: %w", slug, err)
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
	// lookupSlug grabs the slug for a session key under RLock so we never
	// hold b.mu while calling broker methods (which take their own mu).
	lookupSlug := func(sessionKey string) (string, bool) {
		b.mu.RLock()
		defer b.mu.RUnlock()
		slug, ok := b.slugByKey[sessionKey]
		return slug, ok
	}
	switch evt.Kind {
	case openclaw.EventKindMessage:
		if evt.SessionMessage == nil {
			return
		}
		slug, ok := lookupSlug(evt.SessionMessage.SessionKey)
		if !ok {
			return // not a bridged session, ignore
		}
		// Skip user/system echoes — only publish agent replies.
		if evt.SessionMessage.Role != "" && evt.SessionMessage.Role != "assistant" {
			return
		}
		if text := evt.SessionMessage.MessageText; text != "" {
			b.mu.RLock()
			channel := b.lastChannelByKey[evt.SessionMessage.SessionKey]
			b.mu.RUnlock()
			if channel == "" {
				channel = "general"
			}
			b.postBridgeMessage(slug, channel, text)
		}
	case openclaw.EventKindChanged:
		if evt.SessionsChanged != nil && evt.SessionsChanged.Reason == "ended" {
			if slug, ok := lookupSlug(evt.SessionsChanged.SessionKey); ok {
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
		if slug, ok := lookupSlug(evt.Gap.SessionKey); ok {
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

// OnOfficeMessage sends a human-authored message to the OpenClaw agent
// identified by slug. The channel argument is where the reply should land
// (e.g. "general" for @mentions, a DM slug like "human__pm-bot" for DMs).
// Retries on transient errors with a SINGLE reused idempotency key so the
// gateway can deduplicate.
//
// Reply routing: OpenClaw streams the assistant reply via an async event.
// We remember this channel here, keyed by the session key; handleClientEvent
// reads it when the reply arrives. If channel is empty we fall back to
// "general" so older callers and probes keep working.
func (b *OpenclawBridge) OnOfficeMessage(ctx context.Context, slug, channel, message string) error {
	key, ok := b.keyBySlug[slug]
	if !ok {
		return fmt.Errorf("openclaw: unknown bridged slug %q", slug)
	}
	if channel == "" {
		channel = "general"
	}
	b.mu.Lock()
	if b.lastChannelByKey == nil {
		b.lastChannelByKey = make(map[string]string)
	}
	b.lastChannelByKey[key] = channel
	b.mu.Unlock()
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

// postBridgeMessage posts a bridged-agent chat message into the given channel
// via the same broker entrypoint telegram.go uses for incoming chat.
func (b *OpenclawBridge) postBridgeMessage(slug, channel, text string) {
	if b.broker == nil {
		return
	}
	if channel == "" {
		channel = "general"
	}
	_, _ = b.broker.PostInboundSurfaceMessage(slug, channel, text, "openclaw")
}

// postSystemMessage posts a `system`-authored notice into #general.
func (b *OpenclawBridge) postSystemMessage(text string) {
	if b.broker == nil {
		return
	}
	b.broker.PostSystemMessage("general", "[openclaw] "+text, "openclaw")
}
