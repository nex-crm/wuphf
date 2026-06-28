package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// shared_test_helpers_test.go holds small test helpers shared across the team
// package tests. They were previously colocated with the (now removed)
// notebook/promotion test files; they live here so the surviving tests keep
// compiling.

// authReq builds an authorized JSON request.
func authReq(method, url string, body io.Reader, token string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// postMessage posts a channel message through the broker HTTP handler and
// returns the stored message.
func postMessage(t *testing.T, b *Broker, from, channel, content string, tagged []string) channelMessage {
	t.Helper()
	body := map[string]any{
		"from":    from,
		"channel": channel,
		"content": content,
	}
	if tagged != nil {
		body["tagged"] = tagged
	}
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/messages", bytes.NewReader(buf))
	req.Header.Set("Authorization", "Bearer "+b.token)
	rec := httptest.NewRecorder()
	b.handlePostMessage(rec, req)
	if rec.Code != http.StatusOK {
		resBody, _ := io.ReadAll(rec.Result().Body)
		t.Fatalf("post message status=%d body=%s", rec.Code, string(resBody))
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, m := range b.messages {
		if m.ID == resp.ID {
			return m
		}
	}
	t.Fatalf("message %s not found", resp.ID)
	return channelMessage{}
}

// newStartedWikiWorkerForTest spins a real WikiWorker on a temp git repo and
// starts its drain goroutine. The returned cancel stops the worker and waits
// for it to drain.
func newStartedWikiWorkerForTest(t *testing.T) (*WikiWorker, context.CancelFunc) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wiki")
	backup := filepath.Join(t.TempDir(), "wiki.bak")
	repo := NewRepoAt(root, backup)
	if err := repo.Init(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	worker := NewWikiWorker(repo, noopPublisher{})
	ctx, cancel := context.WithCancel(context.Background())
	worker.Start(ctx)
	return worker, func() {
		cancel()
		<-worker.Done()
	}
}
