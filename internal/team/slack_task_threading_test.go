package team

// slack_task_threading_test.go covers one-task-one-thread: a task message
// carries SourceTaskID, Send ensures a single root card and threads the
// message under it, the root is created exactly once under concurrency, and
// the root card renders the task definition.

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/slack-go/slack"
)

func TestFormatOutboundCarriesSourceTaskID(t *testing.T) {
	tr, _ := newTestSlackTransport(t, "C0123", newFakeSlackAPI())
	out, ok := tr.FormatOutbound(channelMessage{
		From: "ceo", Channel: "slack-general", Content: "delegating", SourceTaskID: "OFFICE-7",
	})
	if !ok {
		t.Fatal("expected outbound to format")
	}
	if out.SourceTaskID != "OFFICE-7" {
		t.Fatalf("SourceTaskID not carried: %q", out.SourceTaskID)
	}
}

func TestSendThreadsTaskMessageUnderRootCard(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	b.SetWebURL("http://127.0.0.1:7905")
	seedCardTask(b, "OFFICE-7", LifecycleStateRunning)

	out, _ := tr.FormatOutbound(channelMessage{
		From: "ceo", Channel: "slack-general", Content: "ask @hermes for the totals", SourceTaskID: "OFFICE-7",
	})
	if err := tr.Send(context.Background(), out); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// Two posts: the root card (first, no thread_ts) then the message threaded
	// under the card's ts.
	if len(api.posts) != 2 {
		t.Fatalf("want 2 posts (root + threaded message), got %d: %+v", len(api.posts), api.posts)
	}
	card, msg := api.posts[0], api.posts[1]
	if card.ThreadTS != "" {
		t.Fatalf("root card must not be threaded: %+v", card)
	}
	if card.Blocks == "" || !strings.Contains(card.Blocks, "OFFICE-7") {
		t.Fatalf("first post should be the task root card: %+v", card)
	}
	if msg.ThreadTS != card.ThreadTS && msg.ThreadTS == "" {
		t.Fatalf("task message was not threaded: %+v", msg)
	}
	rec, ok := b.SlackTaskCard("OFFICE-7")
	if !ok {
		t.Fatal("root card record not stored")
	}
	if msg.ThreadTS != rec.Timestamp {
		t.Fatalf("message threaded under %q, want root ts %q", msg.ThreadTS, rec.Timestamp)
	}
}

func TestEnsureTaskThreadRootIdempotentUnderConcurrency(t *testing.T) {
	api := newFakeSlackAPI()
	tr, b := newTestSlackTransport(t, "C0123", api)
	seedCardTask(b, "OFFICE-9", LifecycleStateRunning)

	var wg sync.WaitGroup
	tss := make([]string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tss[i] = tr.ensureTaskThreadRoot(context.Background(), "OFFICE-9")
		}(i)
	}
	wg.Wait()

	// Exactly one card posted, and every caller saw the same root ts.
	if len(api.posts) != 1 {
		t.Fatalf("want exactly 1 root card across concurrent callers, got %d", len(api.posts))
	}
	for i, ts := range tss {
		if ts == "" || ts != tss[0] {
			t.Fatalf("caller %d got root ts %q, want stable %q", i, ts, tss[0])
		}
	}
}

func TestTaskRootCardRendersDefinition(t *testing.T) {
	task := &teamTask{
		ID: "OFFICE-7", Title: "Recommend the cheaper plan", Owner: "ceo",
		Definition: &TaskDefinition{
			Goal:            "Pick the cheaper 3-year cloud plan",
			Deliverables:    []TaskDeliverable{{Name: "recommendation", Format: "chat post"}},
			SuccessCriteria: []string{"both agents independently agree on the totals"},
		},
	}
	blocks := buildSlackTaskCardBlocks(task, "running", "http://127.0.0.1:7905")
	raw := blocksText(t, blocks)
	for _, want := range []string{
		"Pick the cheaper 3-year cloud plan", "recommendation (chat post)",
		"both agents independently agree", "Open task in WUPHF", "lives in this thread",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("root card missing %q:\n%s", want, raw)
		}
	}

	// Falls back to Details when no Definition is set.
	plain := &teamTask{ID: "OFFICE-8", Title: "x", Owner: "ceo", Details: "TESTING run — compare plans."}
	if got := blocksText(t, buildSlackTaskCardBlocks(plain, "running", "")); !strings.Contains(got, "TESTING run") {
		t.Errorf("no-definition card should show Details: %s", got)
	}
}

// blocksText renders a card's section text for assertions.
func blocksText(t *testing.T, blocks []slack.Block) string {
	t.Helper()
	var sb strings.Builder
	for _, b := range blocks {
		if sec, ok := b.(*slack.SectionBlock); ok && sec.Text != nil {
			sb.WriteString(sec.Text.Text)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}
