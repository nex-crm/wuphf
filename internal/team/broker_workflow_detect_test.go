package team

import (
	"context"
	"strings"
	"testing"
)

// seedRecurringShape writes manifests so the deterministic miner yields a
// candidate for taskID: the same multi-tool shape run by `agent` across taskID
// plus `priors` sibling tasks. With >= appWorkflowRecurrenceFloor total runs the
// read-only shape surfaces. Requires WUPHF_RUNTIME_HOME to be set so
// EventSinkPath resolves to the test's temp dir.
func seedRecurringShape(t *testing.T, agent, taskID string, priors []string, tools ...string) {
	t.Helper()
	path := EventSinkPath()
	if path == "" {
		t.Fatal("EventSinkPath empty — set WUPHF_RUNTIME_HOME before seeding")
	}
	for _, id := range append(append([]string{}, priors...), taskID) {
		if err := appendTurnManifest(path, manifestFor(id, agent, tools...)); err != nil {
			t.Fatalf("seed manifest: %v", err)
		}
	}
}

// TestDetectWorkflowAppRaisesProposal is the core of the post-task discovery
// rewrite: gated on the deterministic miner (the shape must have actually
// recurred), the BROKER (not an agent) judges the completed task and raises a
// real, non-blocking propose_app card — so there is no "next turn" deferral and
// no phantom card. Idempotent: a second pass dedupes onto the same card.
func TestDetectWorkflowAppRaisesProposal(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	var sawTranscript, sawShape bool
	withFakeAppsLLM(t, func(_ context.Context, _, prompt string) (string, error) {
		sawTranscript = strings.Contains(prompt, "every Monday")
		sawShape = strings.Contains(prompt, "OBSERVED WORKFLOW SHAPE") && strings.Contains(prompt, "score_leads")
		return `{"worth_building":true,"name":"Lead Scorer","summary":"Score inbound leads against the ICP","description":"Scores leads against the ICP gates every Monday and reports fit","related_app_id":"","reason":"human runs it weekly"}`, nil
	})

	b := newTestBroker(t)
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-1", Owner: "ceo", Channel: "task-1", Title: "Score leads", status: "done"})
	b.messages = append(b.messages,
		channelMessage{From: "you", Channel: "task-1", Content: "Score these 3 leads against our ICP — I run this every Monday."},
		channelMessage{From: "ceo", Channel: "task-1", Content: "Scored: Acme 7/10, BetaLabs 3/10, Gamma 4/10."},
	)
	// The shape recurred: OFFICE-1 plus a prior run by the same agent.
	seedRecurringShape(t, "ceo", "OFFICE-1", []string{"OFFICE-0"}, "crm_fetch_leads", "score_leads")

	b.detectWorkflowAppForTask("OFFICE-1")

	if !sawTranscript {
		t.Error("judge prompt should carry the task transcript")
	}
	if !sawShape {
		t.Error("judge prompt should carry the mined OBSERVED WORKFLOW SHAPE")
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

// TestDetectWorkflowAppSkipsWhenNotWorth: even a recurring shape yields no card
// when the judge decides an App is not the right surface (worth_building=false).
func TestDetectWorkflowAppSkipsWhenNotWorth(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	judged := false
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		judged = true
		return `{"worth_building":false,"reason":"better as unattended automation"}`, nil
	})
	b := newTestBroker(t)
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-2", Owner: "ceo", Channel: "task-2", Title: "Recurring", status: "done"})
	b.messages = append(b.messages, channelMessage{From: "you", Channel: "task-2", Content: "run this again"})
	seedRecurringShape(t, "ceo", "OFFICE-2", []string{"OFFICE-1b"}, "crm_fetch_leads", "score_leads")

	b.detectWorkflowAppForTask("OFFICE-2")
	if !judged {
		t.Fatal("judge should be reached when the shape recurred")
	}
	for i := range b.requests {
		if b.requests[i].AppProposal != nil {
			t.Fatal("no proposal expected when the judge says worth_building=false")
		}
	}
}

// TestDetectWorkflowAppSkipsWithoutRecurrenceEvidence is the precision win: a
// completed task whose shape neither recurred nor reached an outcome never even
// reaches the judge — the deterministic miner gates it out, killing the per-task
// LLM noise the old transcript-only judge produced.
func TestDetectWorkflowAppSkipsWithoutRecurrenceEvidence(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	called := false
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		called = true
		return `{"worth_building":true,"name":"x","description":"y"}`, nil
	})
	b := newTestBroker(t)
	b.tasks = append(b.tasks, teamTask{ID: "OFFICE-9", Owner: "ceo", Channel: "task-9", Title: "One-off", status: "done"})
	b.messages = append(b.messages, channelMessage{From: "you", Channel: "task-9", Content: "do this odd one-time thing"})
	// A single non-outcome run: below the recurrence floor, so no candidate.
	seedRecurringShape(t, "ceo", "OFFICE-9", nil, "crm_fetch_leads", "score_leads")

	b.detectWorkflowAppForTask("OFFICE-9")
	if called {
		t.Fatal("judge must not be called without recurrence/outcome evidence")
	}
	for i := range b.requests {
		if b.requests[i].AppProposal != nil {
			t.Fatal("no proposal expected without recurrence evidence")
		}
	}
}

// TestDetectInlineWorkflowAppRaisesProposal: repeated INLINE (task-less) work the
// CEO did answering chat — which no task-completion ever triggers — clusters into
// a recurring workflow and surfaces a proposal in the channel it happened in.
// Bounded: while a proposal is on the board, a second pass doesn't re-judge.
func TestDetectInlineWorkflowAppRaisesProposal(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	judgeCalls := 0
	withFakeAppsLLM(t, func(_ context.Context, _, prompt string) (string, error) {
		judgeCalls++
		if !strings.Contains(prompt, "INLINE WORK") {
			t.Errorf("inline judge prompt should mark the work as inline, got: %.80s", prompt)
		}
		return `{"worth_building":true,"name":"Lead Scorer","summary":"Score leads","description":"Scores inbound leads against the ICP every time","related_app_id":"","reason":"done repeatedly inline"}`, nil
	})
	b := newTestBroker(t)
	// Two task-less inline turns with the same work shape (pseudo-tasks).
	path := EventSinkPath()
	for _, id := range []string{inlineTurnScopePrefix + "a", inlineTurnScopePrefix + "b"} {
		if err := appendTurnManifest(path, manifestFor(id, "ceo", "crm_fetch_leads", "score_leads")); err != nil {
			t.Fatalf("seed inline manifest: %v", err)
		}
	}

	b.detectInlineWorkflowApp("ceo", "general")

	var proposal *humanInterview
	for i := range b.requests {
		if b.requests[i].AppProposal != nil {
			proposal = &b.requests[i]
			break
		}
	}
	if proposal == nil {
		t.Fatal("expected an inline-detected app proposal")
	}
	if proposal.AppProposal.Name != "Lead Scorer" {
		t.Fatalf("proposal name = %q", proposal.AppProposal.Name)
	}
	if len(proposal.AppProposal.ObservedSteps) == 0 {
		t.Error("inline proposal should carry the observed shape")
	}

	// A second pass is bounded: a proposal is already on the board, so the judge
	// is not consulted again.
	before := judgeCalls
	b.detectInlineWorkflowApp("ceo", "general")
	if judgeCalls != before {
		t.Errorf("judge re-consulted while a proposal was already pending (%d -> %d)", before, judgeCalls)
	}
}

// TestDetectInlineWorkflowAppSkipsAlreadyProposedShape: a shape the human already
// saw (an existing proposal, even one they answered/rejected) is not re-pitched
// or re-judged — the per-turn inline path must not nag.
func TestDetectInlineWorkflowAppSkipsAlreadyProposedShape(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	judgeCalls := 0
	withFakeAppsLLM(t, func(_ context.Context, _, _ string) (string, error) {
		judgeCalls++
		return `{"worth_building":true,"name":"Lead Scorer","description":"x"}`, nil
	})
	b := newTestBroker(t)
	path := EventSinkPath()
	for _, id := range []string{inlineTurnScopePrefix + "a", inlineTurnScopePrefix + "b"} {
		if err := appendTurnManifest(path, manifestFor(id, "ceo", "crm_fetch_leads", "score_leads")); err != nil {
			t.Fatal(err)
		}
	}
	// The shape was already proposed and the human DECIDED (rejected) it.
	b.requests = append(b.requests, humanInterview{
		ID: "request-x", Kind: "approval",
		AppProposal: &appProposalSpec{Name: "Lead Scorer", Fingerprint: "crm_fetch_leads>score_leads"},
	})

	b.detectInlineWorkflowApp("ceo", "general")

	if judgeCalls != 0 {
		t.Errorf("an already-proposed shape must not be re-judged, judgeCalls=%d", judgeCalls)
	}
	n := 0
	for i := range b.requests {
		if b.requests[i].AppProposal != nil {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("must not raise a second card for an already-proposed shape, got %d", n)
	}
}

// TestDetectionLaneIsolation: an inline pseudo-task must NOT merge into a real
// task's cluster (which fuzzy+cross-agent would otherwise do) — that would
// inflate the task's recurrence count and mislabel it. Each lane sees only its
// own manifests.
func TestDetectionLaneIsolation(t *testing.T) {
	t.Setenv("WUPHF_RUNTIME_HOME", t.TempDir())
	path := EventSinkPath()
	// One real task and one inline pseudo-task with the SAME read-only shape.
	if err := appendTurnManifest(path, manifestFor("OFFICE-1", "ceo", "crm_fetch_leads", "score_leads")); err != nil {
		t.Fatal(err)
	}
	if err := appendTurnManifest(path, manifestFor(inlineTurnScopePrefix+"a", "ceo", "crm_fetch_leads", "score_leads")); err != nil {
		t.Fatal(err)
	}
	// Task lane: the real task is a single read-only run; the inline pseudo-task is
	// excluded, so nothing inflates it to the floor → no candidate.
	if cand := detectionCandidateForTask("OFFICE-1"); cand != nil {
		t.Fatalf("task lane must not be inflated by an inline pseudo-task: %+v", cand)
	}
	// Inline lane: a single inline run is below the inline floor (>= 2) → none.
	cands, err := detectAppCandidates(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 0 {
		t.Fatalf("a single inline run must not surface, got %d", len(cands))
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
