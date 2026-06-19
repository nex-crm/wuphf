package team

import (
	"reflect"
	"testing"

	"github.com/nex-crm/wuphf/internal/workflow"
)

// digestLikeSpec mirrors the live demo contract after the builder extended it:
// a linear chain start →(run)→ done →(digest_ready)→ emailed →(announce_ready)→
// announced, with run as the only entry event.
func digestLikeSpec() *workflow.Spec {
	return &workflow.Spec{
		Initial:  "start",
		Terminal: []string{"announced"},
		States: []workflow.State{
			{ID: "start"}, {ID: "done"}, {ID: "emailed"}, {ID: "announced"},
		},
		Events: []workflow.Event{
			{ID: "run", Label: "Run the workflow"},
			{ID: "digest_ready", Label: "Digest Ready"},
			{ID: "announce_ready", Label: "Announce Ready"},
		},
		Transitions: []workflow.Transition{
			{From: "start", To: "done", On: "run"},
			{From: "done", To: "emailed", On: "digest_ready"},
			{From: "emailed", To: "announced", On: "announce_ready"},
		},
	}
}

// Fix #1: only events that leave the initial state are triggers; internal
// transition events (digest_ready, announce_ready) are excluded.
func TestEntryTriggerEventsExcludesInternal(t *testing.T) {
	entry := entryTriggerEvents(digestLikeSpec())
	if !entry["run"] {
		t.Errorf("entry trigger must include 'run' (leaves initial state)")
	}
	for _, internal := range []string{"digest_ready", "announce_ready"} {
		if entry[internal] {
			t.Errorf("internal event %q must NOT be a trigger", internal)
		}
	}
}

// Fix #2: a manual run drives the whole chain to the terminal state, not just
// the first hop.
func TestRunToCompletionEventsWalksWholeChain(t *testing.T) {
	got := runToCompletionEvents(digestLikeSpec())
	want := []workflow.ScenarioEvent{
		{Event: "run"}, {Event: "digest_ready"}, {Event: "announce_ready"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("run-to-completion events = %v, want %v", got, want)
	}
}

// Fix #2 guard: a branch (state with >1 outgoing transition) stops the walk —
// the runner can't auto-pick a branch without run data.
func TestRunToCompletionStopsAtBranch(t *testing.T) {
	s := digestLikeSpec()
	s.Transitions = append(s.Transitions, workflow.Transition{From: "done", To: "skipped", On: "skip"})
	got := runToCompletionEvents(s)
	// start has one exit (run) -> done; done now branches -> stop after `run`.
	want := []workflow.ScenarioEvent{{Event: "run"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("branch walk = %v, want %v (should stop at the branch)", got, want)
	}
}

// Fix #2 guard: a cyclic contract terminates (no infinite event stream).
func TestRunToCompletionGuardsCycle(t *testing.T) {
	s := &workflow.Spec{
		Initial: "a",
		States:  []workflow.State{{ID: "a"}, {ID: "b"}},
		Transitions: []workflow.Transition{
			{From: "a", To: "b", On: "x"},
			{From: "b", To: "a", On: "y"},
		},
	}
	got := runToCompletionEvents(s)
	if len(got) > len(s.States) {
		t.Fatalf("cycle produced %d events, want <= %d", len(got), len(s.States))
	}
}

// Fix #4: a deterministic step that names a send is reported as a draft, never a
// silent success — the audit must not claim a send that did not happen.
func TestLooksLikeSendAction(t *testing.T) {
	for _, id := range []string{"email_digest", "post_slack_general", "notify_owner", "publish_report"} {
		if !looksLikeSendAction(workflow.Action{ID: id}) {
			t.Errorf("looksLikeSendAction(%q) = false, want true", id)
		}
	}
	// Non-send deterministic steps that could legitimately hit the default
	// branch. (gmail_fetch_emails contains "email" but never reaches this branch
	// — it's handled by execDomainAction first — so it is not tested here.)
	for _, id := range []string{"summarize_threads", "compose_digest", "compute_totals", "fetch_records"} {
		if looksLikeSendAction(workflow.Action{ID: id}) {
			t.Errorf("looksLikeSendAction(%q) = true, want false", id)
		}
	}
}

// Triggers must be the canonical set only — no raw state-machine event chips.
func TestClassifyTriggerEvent(t *testing.T) {
	cases := map[string]string{
		"run":              "",        // generic manual mechanism → no chip
		"start":            "",        //
		"email_received":   "webhook", // inbound
		"webhook_incoming": "webhook",
		"context_updated":  "context",
		"data_changed":     "context",
		"new_lead":         "context",
	}
	for id, want := range cases {
		if got := classifyTriggerEvent(workflow.Event{ID: id}); got != want {
			t.Errorf("classifyTriggerEvent(%q) = %q, want %q", id, got, want)
		}
	}
}
