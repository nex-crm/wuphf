package team

// slack_inbound_thread_test.go covers the inbound half of one-task-one-thread:
// a Slack reply whose thread_ts matches a task's root card is folded into that
// task (ReplyTo + SourceTaskID), so a non-owner foreign agent's reply stays
// scoped to the task instead of leaking into shared channel context.

import (
	"strings"
	"testing"
)

func TestInboundThreadReplyFoldsIntoTask(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "slack-office", "ceo", "CEO")
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug: "slack-office", Name: "slack-office", Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "slack", RemoteID: "C0123"},
	})
	// A task with an internal thread root + a Slack root card at ts "171.5".
	b.tasks = append(b.tasks, teamTask{
		ID: "OFFICE-7", Channel: "slack-office", Title: "Compare plans",
		Owner: "ceo", ThreadID: "msg-root-internal", LifecycleState: LifecycleStateRunning,
	})
	b.slackTaskCards = map[string]slackTaskCardRecord{
		"OFFICE-7": {ChannelID: "C0123", Timestamp: "171.5", State: "running"},
	}
	b.mu.Unlock()

	// hermes (a foreign agent, NOT the task owner) replies inside the task's
	// thread (thread_ts = the root card ts).
	msg, err := b.PostInboundSurfaceMessageInThread("hermes", "slack-office", "Plan B totals $5,220", "slack", "171.5")
	if err != nil {
		t.Fatalf("inbound: %v", err)
	}
	if msg.SourceTaskID != "OFFICE-7" {
		t.Fatalf("reply not scoped to the task: SourceTaskID=%q", msg.SourceTaskID)
	}
	if msg.ReplyTo != "msg-root-internal" {
		t.Fatalf("reply not folded into the task thread: ReplyTo=%q", msg.ReplyTo)
	}
}

func TestInboundOutsideThreadIsNotTaskScoped(t *testing.T) {
	b := newTestBroker(t)
	ensureTestMemberAccess(b, "slack-office", "ceo", "CEO")
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug: "slack-office", Name: "slack-office", Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "slack", RemoteID: "C0123"},
	})
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-7", Channel: "slack-office", Owner: "ceo", ThreadID: "msg-root-internal"})
	b.slackTaskCards = map[string]slackTaskCardRecord{"OFFICE-7": {ChannelID: "C0123", Timestamp: "171.5"}}
	b.mu.Unlock()

	// A fresh top-level human message (no thread_ts) must NOT inherit a task.
	msg, err := b.PostInboundSurfaceMessageInThread("human:u1", "slack-office", "hey team", "slack", "")
	if err != nil {
		t.Fatalf("inbound: %v", err)
	}
	if msg.SourceTaskID != "" || msg.ReplyTo != "" {
		t.Fatalf("top-level message wrongly scoped: SourceTaskID=%q ReplyTo=%q", msg.SourceTaskID, msg.ReplyTo)
	}
}

// The bot must never relay a human-authored message back into Slack — doing so
// posts it under the human's name ("nazz: …"), impersonating them.
func TestSlackOutboundNeverImpersonatesHuman(t *testing.T) {
	tr, _ := newTestSlackTransport(t, "C0123", newFakeSlackAPI())
	for _, from := range []string{"human:u08tvalkj86", "human", "you", ""} {
		if _, ok := tr.FormatOutbound(channelMessage{
			From: from, Channel: "slack-general", Content: "kick off the task",
		}); ok {
			t.Fatalf("human-authored message (from=%q) must not relay to Slack", from)
		}
	}
	// Agent messages still relay.
	if _, ok := tr.FormatOutbound(channelMessage{From: "ceo", Channel: "slack-general", Content: "on it"}); !ok {
		t.Fatal("agent message should still relay to Slack")
	}
}

func TestSlackConventionNoteThreadAndQuoteRules(t *testing.T) {
	b := newTestBroker(t)
	b.mu.Lock()
	b.channels = append(b.channels, teamChannel{
		Slug: "slack-office", Name: "slack-office", Members: []string{"ceo"},
		Surface: &channelSurface{Provider: "slack", RemoteID: "C0123"},
	})
	b.mu.Unlock()
	l := &Launcher{broker: b}
	note := l.slackChannelConventionNote("slack-office")
	for _, want := range []string{"OWN Slack thread", "inside that thread", "paste its Slack message link"} {
		if !strings.Contains(note, want) {
			t.Errorf("convention note missing %q", want)
		}
	}
}
