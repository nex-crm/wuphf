package team

// Regression test for the F1 snapshot-then-write conversion in
// handlePostMessage (broker_messages.go): the state-file write now happens
// AFTER b.mu is released, so this pins that (a) the message is still durably
// on disk by the time the handler returns 200, and (b) a flood of concurrent
// posts converges to a state file containing every message — the seq guard
// in writeBrokerState must drop stale writes, never a newer one.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func postTestMessage(t *testing.T, b *Broker, content string) {
	t.Helper()
	body := strings.NewReader(fmt.Sprintf(`{"from":"you","channel":"general","content":%q}`, content))
	req, err := http.NewRequest(http.MethodPost, "/messages", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	b.handlePostMessage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handlePostMessage: expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestHandlePostMessagePersistsToDiskBeforeReturning(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = []teamChannel{{Slug: "general", Members: []string{"ceo"}}}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO"}}
	b.rebuildMemberIndexLocked()
	b.mu.Unlock()

	postTestMessage(t, b, "persist me to disk")

	state, err := loadBrokerStateFile(b.statePath)
	if err != nil {
		t.Fatalf("state file must exist after the handler returns: %v", err)
	}
	found := false
	for _, m := range state.Messages {
		if m.Content == "persist me to disk" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("posted message missing from persisted state (%d messages)", len(state.Messages))
	}
}

func TestHandlePostMessageConcurrentPostsConvergeOnDisk(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = []teamChannel{{Slug: "general", Members: []string{"ceo"}}}
	b.members = []officeMember{{Slug: "ceo", Name: "CEO"}}
	b.rebuildMemberIndexLocked()
	b.mu.Unlock()

	const posts = 20
	var wg sync.WaitGroup
	wg.Add(posts)
	for i := 0; i < posts; i++ {
		go func(i int) {
			defer wg.Done()
			postTestMessage(t, b, fmt.Sprintf("concurrent message %d", i))
		}(i)
	}
	wg.Wait()

	state, err := loadBrokerStateFile(b.statePath)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool, posts)
	for _, m := range state.Messages {
		got[m.Content] = true
	}
	for i := 0; i < posts; i++ {
		want := fmt.Sprintf("concurrent message %d", i)
		if !got[want] {
			t.Fatalf("state file lost %q under concurrent posts (have %d messages)", want, len(state.Messages))
		}
	}
}
