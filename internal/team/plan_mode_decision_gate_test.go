package team

// plan_mode_decision_gate_test.go covers the Plan-mode human-only gate on the
// DECISION path (recordTaskDecisionInternal) — the "Approve plan & Start"
// button, but also any broker-token caller, since the local UI and agents
// share the broker token. The gate in MutateTask alone left this path open
// (multi-agent review HIGH/CRITICAL: agents could POST action=approve to
// /tasks/{id}/decision and start a plan without human approval).

import "testing"

func TestPlanMode_DecisionPathRejectsNonHumanApprove(t *testing.T) {
	b := newTestBroker(t)
	seedPlanningTask(t, b, "OFFICE-D1", "executor")

	// "ceo": an internal RecordTaskDecision(id, "approve", "ceo") caller.
	if err := b.RecordTaskDecision("OFFICE-D1", "approve", "ceo"); err == nil {
		t.Fatalf("decision approve by non-human (ceo) on a Planning task must be forbidden; got nil")
	}
	if got := lifecycleStateOf(t, b, "OFFICE-D1"); got != LifecycleStatePlanning {
		t.Fatalf("task must stay in Planning after a blocked approve; got %q", got)
	}

	// "owner": what handleTaskDecision stamps for a broker-token caller that
	// did NOT self-attribute as human (i.e. an agent hitting the endpoint).
	if err := b.RecordTaskDecision("OFFICE-D1", "approve", "owner"); err == nil {
		t.Fatalf("decision approve by 'owner' (agent via broker token) must be forbidden; got nil")
	}
	if got := lifecycleStateOf(t, b, "OFFICE-D1"); got != LifecycleStatePlanning {
		t.Fatalf("task must stay in Planning; got %q", got)
	}
}

func TestPlanMode_DecisionPathAllowsHumanApprove(t *testing.T) {
	b := newTestBroker(t)
	seedPlanningTask(t, b, "OFFICE-D2", "executor")

	// A human-attributed decision is what handleTaskDecision passes for the
	// local UI (created_by:"human") and for a remote human session
	// ("human:<slug>"). It must clear the gate and start the work.
	if err := b.RecordTaskDecisionWithComment("OFFICE-D2", "approve", "", "human"); err != nil {
		t.Fatalf("human approve on a Planning task must succeed; got %v", err)
	}
	if got := lifecycleStateOf(t, b, "OFFICE-D2"); got == LifecycleStatePlanning {
		t.Fatalf("human approve must advance the task out of Planning; still %q", got)
	}
}

func TestPlanMode_DecisionPathAllowsRemoteHumanSession(t *testing.T) {
	b := newTestBroker(t)
	seedPlanningTask(t, b, "OFFICE-D3", "executor")

	// humanMessageSender("nazz") == "human:nazz" — the attribution
	// handleTaskDecision uses for a remote human-session (share-cookie) actor.
	if err := b.RecordTaskDecisionWithComment("OFFICE-D3", "approve", "", humanMessageSender("nazz")); err != nil {
		t.Fatalf("remote human session approve must succeed; got %v", err)
	}
	if got := lifecycleStateOf(t, b, "OFFICE-D3"); got == LifecycleStatePlanning {
		t.Fatalf("remote human approve must advance the task out of Planning; still %q", got)
	}
}
