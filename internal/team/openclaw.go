package team

import (
	"context"
	"fmt"
	"sync"

	"github.com/nex-crm/wuphf/internal/config"
	"github.com/nex-crm/wuphf/internal/openclaw"
)

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

// handleClientEvent is implemented in Task 9.
func (b *OpenclawBridge) handleClientEvent(_ openclaw.ClientEvent) {
	// placeholder — Task 9 fills in
}
