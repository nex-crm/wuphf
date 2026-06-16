package team

import (
	"context"
	"strings"
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

	// Active → a "thinking…" indicator is POSTED into the task thread (10.0).
	pushActivity(agentActivitySnapshot{Slug: "ceo", Status: "active", Activity: "thinking"})
	if !waitFor(func() bool {
		for _, p := range api.posts {
			if p.ThreadTS == "10.0" && strings.Contains(p.Text, "thinking") {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected a thinking indicator posted in thread 10.0, got posts=%+v", api.posts)
	}

	// Idle → the indicator is DELETED (the real reply posts separately).
	pushActivity(agentActivitySnapshot{Slug: "ceo", Status: "idle", Activity: "idle"})
	if !waitFor(func() bool {
		for _, d := range api.deletes {
			if d.ChannelID == "C0123" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected the thinking indicator deleted on idle, got deletes=%+v", api.deletes)
	}
}
