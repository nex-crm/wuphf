package team

import (
	"context"
	"strings"
	"testing"
)

// TestDetectWorkflowAppRaisesProposal is the core of the post-task discovery
// rewrite: the BROKER (not an agent) judges a completed task and raises a real,
// non-blocking propose_app card — so there is no "next turn" deferral and no
// phantom card. Idempotent: a second pass dedupes onto the same card.
func TestDetectWorkflowAppRaisesProposal(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	var sawTranscript bool
	withFakeAppsLLM(t, func(_ context.Context, _, prompt string) (string, error) {
		sawTranscript = strings.Contains(prompt, "every Monday")
		return `{"worth_building":true,"name":"Lead Scorer","summary":"Score inbound leads against the ICP","description":"Scores leads against the ICP gates every Monday and reports fit","related_app_id":"","reason":"human runs it weekly"}`, nil
	})

	b := newTestBroker(t)
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-1", Owner: "ceo", Channel: "task-1", Title: "Score leads", status: "done"})
	b.messages = append(b.messages,
		channelMessage{From: "you", Channel: "task-1", Content: "Score these 3 leads against our ICP — I run this every Monday."},
		channelMessage{From: "ceo", Channel: "task-1", Content: "Scored: Acme 7/10, BetaLabs 3/10, Gamma 4/10."},
	)

	b.detectWorkflowAppForTask("OFFICE-1")

	if !sawTranscript {
		t.Error("judge prompt should carry the task transcript")
	}
	var proposal *humanInterview
	for i := range b.requests {
		if b.requests[i].AppProposal != nil {
			proposal = &b.requests[i]
			break
		}
	}
	if proposal == nil {
		t.Fatal("expected a detected app proposal card")
	}
	if proposal.AppProposal.Name != "Lead Scorer" {
		t.Fatalf("proposal name = %q, want Lead Scorer", proposal.AppProposal.Name)
	}
	if proposal.Blocking || proposal.Required {
		t.Fatal("a detected proposal must be non-blocking and not required")
	}

	// Idempotent: a second detection for the same workflow dedupes.
	b.detectWorkflowAppForTask("OFFICE-1")
	n := 0
	for i := range b.requests {
		if b.requests[i].AppProposal != nil {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("dedupe failed: %d proposal cards, want 1", n)
	}
}

// TestDetectWorkflowAppSkipsWhenNotWorth: a one-off task yields no card.
func TestDetectWorkflowAppSkipsWhenNotWorth(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		return `{"worth_building":false,"reason":"one-off, not repeatable"}`, nil
	})
	b := newTestBroker(t)
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-2", Owner: "ceo", Channel: "task-2", Title: "One-off", status: "done"})
	b.messages = append(b.messages, channelMessage{From: "you", Channel: "task-2", Content: "do this odd one-time thing"})

	b.detectWorkflowAppForTask("OFFICE-2")
	for i := range b.requests {
		if b.requests[i].AppProposal != nil {
			t.Fatal("no proposal expected when the judge says worth_building=false")
		}
	}
}

// TestDetectWorkflowAppSkipsAppBuilderOwnTasks: the App Builder's own build/edit
// tasks ARE app work, not a workflow to convert — detection must not even call
// the judge for them.
func TestDetectWorkflowAppSkipsAppBuilderOwnTasks(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	called := false
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		called = true
		return `{"worth_building":true,"name":"x","description":"y"}`, nil
	})
	b := newTestBroker(t)
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-3", Owner: appBuilderSlug, Channel: "task-3", Title: "Build app: X", status: "done"})
	b.messages = append(b.messages, channelMessage{From: appBuilderSlug, Channel: "task-3", Content: "built it"})

	b.detectWorkflowAppForTask("OFFICE-3")
	if called {
		t.Fatal("detection must skip the App Builder's own tasks without calling the judge")
	}
}

// TestQueueWorkflowDetectionGatedByFlag: the queue is a no-op unless enabled, so
// the unit suite never fires a live judge on task completion.
func TestQueueWorkflowDetectionGatedByFlag(t *testing.T) {
	b := newTestBroker(t)
	if b.workflowDetectionEnabled {
		t.Fatal("detection must default OFF for test brokers")
	}
	// No panic / no work when disabled.
	b.queueWorkflowAppDetection("OFFICE-404")
}
