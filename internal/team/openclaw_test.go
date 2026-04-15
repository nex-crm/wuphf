package team

import (
	"context"
	"errors"
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
	return nil, nil
}

func (f *fakeOCClient) Events() <-chan openclaw.ClientEvent { return f.events }
func (f *fakeOCClient) Close() error                        { close(f.events); return nil }

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
