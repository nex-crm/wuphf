package team

// slack_task_cards_test.go covers the pinned lifecycle-card sync: post+pin on
// an active task, in-place update on state change, final update + unpin on a
// terminal state, restart-safety via the persisted card record, and the
// task-link footnote on outbound chat.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedCardTask(b *Broker, id string, state LifecycleState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tasks = append(b.tasks, teamTask{
		ID:             id,
		Channel:        "slack-general",
		Title:          "Ship the pricing page",
		Owner:          "ceo",
		LifecycleState: state,
	})
}

func setCardTaskState(b *Broker, id string, state LifecycleState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := range b.tasks {
		if b.tasks[i].ID == id {
			b.tasks[i].LifecycleState = state
		}
	}
}

func TestSlackTaskCardPostPinUpdateUnpin(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.SetWebURL("http://127.0.0.1:7905")
	seedCardTask(b, "OFFICE-7", LifecycleStateRunning)

	// Active task → card posted + pinned.
	tr.syncTaskCardsOnce(context.Background())
	if len(api.posts) != 1 {
		t.Fatalf("expected 1 card post, got %d", len(api.posts))
	}
	post := api.posts[0]
	if post.ChannelID != "C0123" {
		t.Fatalf("card posted to wrong channel: %q", post.ChannelID)
	}
	for _, want := range []string{"OFFICE-7", "Ship the pricing page", "running", "http://127.0.0.1:7905/tasks/OFFICE-7"} {
		if !strings.Contains(post.Blocks, want) {
			t.Errorf("card blocks missing %q: %s", want, post.Blocks)
		}
	}
	if len(api.pins) != 1 || api.pins[0].ChannelID != "C0123" {
		t.Fatalf("expected the card pinned in C0123, got %+v", api.pins)
	}
	rec, ok := b.SlackTaskCard("OFFICE-7")
	if !ok || !rec.Pinned || rec.State != "running" {
		t.Fatalf("card record not persisted correctly: %+v ok=%v", rec, ok)
	}

	// Same state → no Slack traffic.
	tr.syncTaskCardsOnce(context.Background())
	if len(api.posts) != 1 || len(api.snapshotUpdates()) != 0 {
		t.Fatalf("steady state must be silent: posts=%d updates=%d", len(api.posts), len(api.snapshotUpdates()))
	}

	// State change → in-place update of the SAME message, still pinned.
	setCardTaskState(b, "OFFICE-7", LifecycleStateReview)
	tr.syncTaskCardsOnce(context.Background())
	updates := api.snapshotUpdates()
	if len(api.posts) != 1 || len(updates) != 1 {
		t.Fatalf("state change must update not re-post: posts=%d updates=%d", len(api.posts), len(updates))
	}
	if updates[0].Timestamp != rec.Timestamp {
		t.Fatalf("update touched a different message: %q != %q", updates[0].Timestamp, rec.Timestamp)
	}
	if !strings.Contains(updates[0].Blocks, "review") {
		t.Fatalf("updated card missing new state: %s", updates[0].Blocks)
	}
	if len(api.unpins) != 0 {
		t.Fatalf("active state change must not unpin: %+v", api.unpins)
	}

	// Terminal state → final update + unpin.
	setCardTaskState(b, "OFFICE-7", LifecycleStateApproved)
	tr.syncTaskCardsOnce(context.Background())
	if len(api.unpins) != 1 {
		t.Fatalf("terminal state must unpin the card: %+v", api.unpins)
	}
	rec, _ = b.SlackTaskCard("OFFICE-7")
	if rec.Pinned || rec.State != "approved" {
		t.Fatalf("terminal record wrong: %+v", rec)
	}

	// Terminal steady state → silent.
	tr.syncTaskCardsOnce(context.Background())
	if len(api.snapshotUpdates()) != 2 || len(api.unpins) != 1 {
		t.Fatalf("terminal steady state must be silent: updates=%d unpins=%d", len(api.snapshotUpdates()), len(api.unpins))
	}
}

func TestSlackTaskCardNeverCardsInactiveOrUnbridgedTasks(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	// Old finished work and not-yet-started work get no card.
	seedCardTask(b, "OFFICE-1", LifecycleStateApproved)
	seedCardTask(b, "OFFICE-2", LifecycleStateIntake)
	// Active work in a channel with no Slack surface gets no card either.
	b.mu.Lock()
	b.tasks = append(b.tasks, teamTask{
		ID: "OFFICE-3", Channel: "general", Title: "internal", Owner: "ceo",
		LifecycleState: LifecycleStateRunning,
	})
	b.mu.Unlock()

	tr.syncTaskCardsOnce(context.Background())
	if len(api.posts) != 0 || len(api.pins) != 0 {
		t.Fatalf("nothing should be carded: posts=%+v pins=%+v", api.posts, api.pins)
	}
}

func TestSlackTaskCardRecordSurvivesRestart(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	seedCardTask(b, "OFFICE-9", LifecycleStateRunning)
	tr.syncTaskCardsOnce(context.Background())
	if len(api.posts) != 1 {
		t.Fatalf("expected 1 card post, got %d", len(api.posts))
	}

	// Simulate a restart: a fresh transport over the same broker (whose card
	// registry persists in broker state) must NOT post a duplicate card.
	api2 := newFakeSlackAPI()
	tr2 := newSlackTransport(b, "xoxb-test", "xapp-test", api2)
	tr2.syncTaskCardsOnce(context.Background())
	if len(api2.posts) != 0 {
		t.Fatalf("restart re-posted the card: %+v", api2.posts)
	}
}

func TestSlackTaskCardRegistryPersistsToDisk(t *testing.T) {
	withDiskLoad(t)
	state := brokerState{
		SlackTaskCards: map[string]slackTaskCardRecord{
			"OFFICE-5": {ChannelID: "C0123", Timestamp: "1700.1", State: "running", Pinned: true},
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "broker-state.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	b := NewBrokerAt(path)
	rec, ok := b.SlackTaskCard("OFFICE-5")
	if !ok || rec.Timestamp != "1700.1" || !rec.Pinned {
		t.Fatalf("card registry did not survive a state reload: %+v ok=%v", rec, ok)
	}
}

// Task messages no longer carry a per-message footnote — the task is conveyed
// by the thread (root card has the definition + link), keeping replies clean.
func TestSlackOutboundTaskMessageHasNoFootnote(t *testing.T) {
	tr, b := newTestSlackTransport(t, "C0123", newFakeSlackAPI())
	b.SetWebURL("http://127.0.0.1:7905")
	out, ok := tr.FormatOutbound(channelMessage{
		From: "ceo", Channel: "slack-general",
		Content: "Numbers are in: $488.04/yr.", SourceTaskID: "OFFICE-41",
	})
	if !ok {
		t.Fatal("expected outbound to format")
	}
	if strings.Contains(out.Text, "↳ task") {
		t.Fatalf("task footnote should be gone (threading conveys the task): %q", out.Text)
	}
	if out.SourceTaskID != "OFFICE-41" {
		t.Fatalf("SourceTaskID must still be carried for threading: %q", out.SourceTaskID)
	}
}
