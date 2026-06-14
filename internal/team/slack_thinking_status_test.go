package team

import (
	"context"
	"testing"
	"time"
)

func TestIsThinkingActivity(t *testing.T) {
	if !isThinkingActivity(agentActivitySnapshot{Status: "active"}) {
		t.Fatal("active must be thinking")
	}
	for _, s := range []string{"idle", "error", ""} {
		if isThinkingActivity(agentActivitySnapshot{Status: s}) {
			t.Fatalf("%q must not be thinking", s)
		}
	}
}

func TestActiveSlackTaskThreadsForOwner(t *testing.T) {
	b := newTestBrokerWithSlackChannel(t, "C0123")
	b.mu.Lock()
	b.tasks = append(b.tasks,
		teamTask{ID: "OFFICE-1", Owner: "ceo", LifecycleState: LifecycleStateRunning},   // active + carded
		teamTask{ID: "OFFICE-2", Owner: "ceo", LifecycleState: LifecycleStateArchived},  // terminal
		teamTask{ID: "OFFICE-3", Owner: "scout", LifecycleState: LifecycleStateRunning}, // other owner
		teamTask{ID: "OFFICE-4", Owner: "ceo", LifecycleState: LifecycleStateRunning},   // active but NO card
	)
	b.slackTaskCards = map[string]slackTaskCardRecord{
		"OFFICE-1": {ChannelID: "C0123", Timestamp: "10.0"},
		"OFFICE-2": {ChannelID: "C0123", Timestamp: "20.0"},
		"OFFICE-3": {ChannelID: "C0123", Timestamp: "30.0"},
	}
	b.mu.Unlock()

	refs := b.ActiveSlackTaskThreadsForOwner("ceo")
	if len(refs) != 1 || refs[0].TaskID != "OFFICE-1" || refs[0].ThreadTS != "10.0" {
		t.Fatalf("want only OFFICE-1 (active+carded+owned), got %+v", refs)
	}
}

func TestRunAgentThinkingStatus_SetsAndClears(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	ensureTestMemberAccess(b, "slack-general", "ceo", "CEO")
	b.mu.Lock()
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-1", Owner: "ceo", LifecycleState: LifecycleStateRunning})
	b.slackTaskCards = map[string]slackTaskCardRecord{"OFFICE-1": {ChannelID: "C0123", Timestamp: "10.0"}}
	b.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = tr.runAgentThinkingStatus(ctx) }()

	// Wait for the loop to register its activity subscription before publishing —
	// publishActivityLocked drops events when there is no subscriber yet.
	for i := 0; i < 300; i++ {
		b.mu.Lock()
		n := len(b.activitySubscribers)
		b.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Drive the activity stream: CEO goes active → status set; then idle → cleared.
	waitFor := func(pred func() bool) bool {
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			api.mu.Lock()
			ok := pred()
			api.mu.Unlock()
			if ok {
				return true
			}
			time.Sleep(15 * time.Millisecond)
		}
		return false
	}

	pushActivity := func(snap agentActivitySnapshot) {
		b.mu.Lock()
		b.publishActivityLocked(snap)
		b.mu.Unlock()
	}

	pushActivity(agentActivitySnapshot{Slug: "ceo", Status: "active", Activity: "thinking"})
	if !waitFor(func() bool {
		for _, s := range api.statuses {
			if s.ThreadTS == "10.0" && s.Status != "" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected a non-empty thinking status on thread 10.0, got %+v", api.statuses)
	}

	pushActivity(agentActivitySnapshot{Slug: "ceo", Status: "idle", Activity: "idle"})
	if !waitFor(func() bool {
		// last status set on the thread should be the clear ("")
		last := ""
		seen := false
		for _, s := range api.statuses {
			if s.ThreadTS == "10.0" {
				last = s.Status
				seen = true
			}
		}
		return seen && last == ""
	}) {
		t.Fatalf("expected the status cleared on idle, got %+v", api.statuses)
	}
}
