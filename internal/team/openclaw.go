package team

import (
	"context"
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

// OpenclawBridge adapts OpenClaw Gateway sessions into WUPHF office members.
type OpenclawBridge struct {
	broker   *Broker
	client   openclawClient
	bindings []config.OpenclawBridgeBinding

	slugByKey map[string]string // sessionKey -> slug
	keyBySlug map[string]string // slug -> sessionKey

	retryDelays []time.Duration // nil = use defaults

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// NewOpenclawBridge constructs a bridge. It does not dial until Start is called.
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

// Start subscribes to every bound session and launches the event loop.
func (b *OpenclawBridge) Start(ctx context.Context) error {
	b.mu.Lock()
	b.ctx, b.cancel = context.WithCancel(ctx)
	b.mu.Unlock()

	for _, bind := range b.bindings {
		if err := b.client.SessionsMessagesSubscribe(b.ctx, bind.SessionKey); err != nil {
			return fmt.Errorf("openclaw subscribe %q: %w", bind.SessionKey, err)
		}
	}
	go b.eventLoop()
	return nil
}

// Stop cancels the bridge context, closes the client, and waits for the event loop to drain.
func (b *OpenclawBridge) Stop() {
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	b.mu.Unlock()
	_ = b.client.Close()
	<-b.done
}

func (b *OpenclawBridge) eventLoop() {
	defer close(b.done)
	events := b.client.Events()
	for {
		select {
		case <-b.ctx.Done():
			return
		case evt, ok := <-events:
			if !ok {
				return
			}
			b.handleClientEvent(evt) // implemented in Task 9
		}
	}
}

// handleClientEvent dispatches one event from the OpenClaw Client into the
// appropriate broker surface: delta chunks flow into the per-agent stream so
// they render as a live typing indicator, while finals (and bare messages
// without an explicit state) become chat messages authored by the bridged
// slug. Error/aborted states and "session ended" changes fan out as system
// notices. Gap/Close kinds are intentionally no-ops here; Task 11 will wire
// them up to catch-up + reconnect.
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
		// Task 11 handles catch-up via SessionsHistory; no-op here.
	case openclaw.EventKindClose:
		// Task 11 handles reconnect; no-op here.
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
		err := b.client.SessionsSend(ctx, key, message, idem)
		if err == nil {
			return nil
		}
		lastErr = err
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
