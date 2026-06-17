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

// TestActiveSlackTaskThreadsForOwner_SortedByRecency locks the ordering the
// thinking indicator relies on to stay bounded: most-recently-updated first, so
// the loop posts one indicator in refs[0] (the thread the owner is most likely
// working) instead of fanning out across every executable task it still owns and
// blowing Slack's rate limit.
func TestActiveSlackTaskThreadsForOwner_SortedByRecency(t *testing.T) {
	b := newTestBrokerWithSlackChannel(t, "C0123")
	b.mu.Lock()
	b.tasks = append(b.tasks,
		teamTask{ID: "OLD", Owner: "ceo", LifecycleState: LifecycleStateRunning, UpdatedAt: "2026-06-16T10:00:00Z"},
		teamTask{ID: "NEW", Owner: "ceo", LifecycleState: LifecycleStateRunning, UpdatedAt: "2026-06-16T12:00:00Z"},
		teamTask{ID: "MID", Owner: "ceo", LifecycleState: LifecycleStateRunning, UpdatedAt: "2026-06-16T11:00:00Z"},
	)
	b.slackTaskCards = map[string]slackTaskCardRecord{
		"OLD": {ChannelID: "C0123", Timestamp: "1.0"},
		"NEW": {ChannelID: "C0123", Timestamp: "2.0"},
		"MID": {ChannelID: "C0123", Timestamp: "3.0"},
	}
	b.mu.Unlock()

	refs := b.ActiveSlackTaskThreadsForOwner("ceo")
	if len(refs) != 3 {
		t.Fatalf("want 3 active threads, got %d: %+v", len(refs), refs)
	}
	if refs[0].TaskID != "NEW" {
		t.Fatalf("most-recently-updated task must sort first; got %s", refs[0].TaskID)
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

	// Active → native assistant status is SET on the task thread (10.0). No
	// channel message is posted (that would be the notification-overwhelm trap).
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
	if len(api.posts) != 0 {
		t.Fatalf("the thinking indicator must NOT post a channel message, got posts=%+v", api.posts)
	}

	// Idle → the status is CLEARED ("").
	pushActivity(agentActivitySnapshot{Slug: "ceo", Status: "idle", Activity: "idle"})
	if !waitFor(func() bool {
		last, seen := "", false
		for _, s := range api.statuses {
			if s.ThreadTS == "10.0" {
				last, seen = s.Status, true
			}
		}
		return seen && last == ""
	}) {
		t.Fatalf("expected the status cleared on idle, got %+v", api.statuses)
	}
}

// TestRunAgentThinkingStatus_LightsPane verifies the lead's thinking status also
// renders in the open Assistant pane (the 1:1 DM) — the surface where native
// status actually shows for a user chatting with the office — and clears on idle.
func TestRunAgentThinkingStatus_LightsPane(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	lead := b.OfficeLeadSlug()
	ensureTestMemberAccess(b, "slack-general", lead, "Lead")
	// Open the lead's Assistant pane (binds the IM channel D900 + records the
	// conversation root) so AssistantPaneRef(lead) resolves.
	tr.seedAssistantThread(context.Background(), "D900", "900.1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = tr.runAgentThinkingStatus(ctx) }()

	for i := 0; i < 300; i++ {
		b.mu.Lock()
		n := len(b.activitySubscribers)
		b.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

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

	// Lead active → status lit on the pane root (900.1 on IM channel D900).
	pushActivity(agentActivitySnapshot{Slug: lead, Status: "active", Activity: "thinking"})
	if !waitFor(func() bool {
		for _, s := range api.statuses {
			if s.ChannelID == "D900" && s.ThreadTS == "900.1" && s.Status != "" {
				return true
			}
		}
		return false
	}) {
		t.Fatalf("expected a thinking status in the pane (D900/900.1), got %+v", api.statuses)
	}
	if len(api.posts) != 0 {
		t.Fatalf("the pane indicator must NOT post a message, got %+v", api.posts)
	}

	// Idle → the pane status is cleared.
	pushActivity(agentActivitySnapshot{Slug: lead, Status: "idle", Activity: "idle"})
	if !waitFor(func() bool {
		last, seen := "", false
		for _, s := range api.statuses {
			if s.ChannelID == "D900" && s.ThreadTS == "900.1" {
				last, seen = s.Status, true
			}
		}
		return seen && last == ""
	}) {
		t.Fatalf("expected the pane status cleared on idle, got %+v", api.statuses)
	}
}
