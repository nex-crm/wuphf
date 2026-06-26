"""Orchestration graph tests — re-hydrate, dispatch, transitions, HITL, idle."""

from orchestrator.graph import build_graph, drive
from orchestrator.harness import FakeHarness
from orchestrator.lifecycle import State, TurnOutcome
from orchestrator.runstate import from_broker_record


def _run(record, harness):
    g = build_graph(harness)
    run = from_broker_record(record)
    return drive(g, run, thread_id=record["task_id"])


def test_running_submit_interrupts_then_approve_to_approved():
    g = build_graph(FakeHarness(TurnOutcome.SUBMITTED_FOR_REVIEW))
    run = from_broker_record({"task_id": "t1", "lifecycle_state": "running"})
    step = drive(g, run, thread_id="t1")
    assert step["status"] == "interrupted"
    assert step["interrupt"]["gate_kind"] == "review"

    resumed = drive(g, None, thread_id="t1", resume={"decision": "approve"})
    assert resumed["status"] == "done"
    assert resumed["state"]["lifecycle_state"] == State.APPROVED.value


def test_planning_plan_ready_interrupts_then_approve_to_running():
    g = build_graph(FakeHarness(TurnOutcome.PLAN_READY))
    run = from_broker_record({"task_id": "t2", "lifecycle_state": "planning"})
    step = drive(g, run, thread_id="t2")
    assert step["status"] == "interrupted"
    assert step["interrupt"]["gate_kind"] == "plan"

    resumed = drive(g, None, thread_id="t2", resume={"decision": "approve"})
    assert resumed["state"]["lifecycle_state"] == State.RUNNING.value


def test_review_request_changes_routes_to_changes_requested():
    g = build_graph(FakeHarness(TurnOutcome.SUBMITTED_FOR_REVIEW))
    run = from_broker_record({"task_id": "t3", "lifecycle_state": "running"})
    drive(g, run, thread_id="t3")
    resumed = drive(g, None, thread_id="t3", resume={"decision": "request_changes"})
    assert resumed["state"]["lifecycle_state"] == State.CHANGES_REQUESTED.value


def test_continue_outcome_stays_running_no_interrupt():
    step = _run({"task_id": "t4", "lifecycle_state": "running"}, FakeHarness(TurnOutcome.CONTINUE))
    assert step["status"] == "done"
    assert step["state"]["lifecycle_state"] == State.RUNNING.value


def test_blocked_outcome_transitions_to_blocked():
    step = _run({"task_id": "t5", "lifecycle_state": "running"}, FakeHarness(TurnOutcome.BLOCKED))
    assert step["state"]["lifecycle_state"] == State.BLOCKED.value


def test_decomposed_outcome_stays_running_no_gate():
    # A goal that decomposed runs to the coordinate path on the next tick; the
    # decompose turn itself is non-gated and leaves the goal RUNNING.
    step = _run({"task_id": "t5b", "lifecycle_state": "running"}, FakeHarness(TurnOutcome.DECOMPOSED))
    assert step["status"] == "done"
    assert step["state"]["lifecycle_state"] == State.RUNNING.value


def test_terminal_task_is_idle():
    step = _run({"task_id": "t6", "lifecycle_state": "approved"}, FakeHarness(TurnOutcome.CONTINUE))
    assert step["status"] == "done"
    assert step["state"]["lifecycle_state"] == State.APPROVED.value


def test_already_in_review_interrupts_for_decision():
    g = build_graph(FakeHarness())
    run = from_broker_record({"task_id": "t7", "lifecycle_state": "review"})
    step = drive(g, run, thread_id="t7")
    assert step["status"] == "interrupted"
    assert step["interrupt"]["gate_kind"] == "review"


def test_review_gate_projects_review_state_on_interrupt():
    # Regression (headline): an interrupted gate must leave the run in the GATE
    # state, not the pre-gate executable state. If this is "running" the broker
    # never surfaces the gate and re-dispatches the task forever.
    g = build_graph(FakeHarness(TurnOutcome.SUBMITTED_FOR_REVIEW))
    run = from_broker_record({"task_id": "g1", "lifecycle_state": "running"})
    step = drive(g, run, thread_id="g1")
    assert step["status"] == "interrupted"
    assert step["state"]["lifecycle_state"] == State.REVIEW.value


def test_plan_gate_projects_decision_state_on_interrupt():
    g = build_graph(FakeHarness(TurnOutcome.PLAN_READY))
    run = from_broker_record({"task_id": "g2", "lifecycle_state": "planning"})
    step = drive(g, run, thread_id="g2")
    assert step["status"] == "interrupted"
    assert step["state"]["lifecycle_state"] == State.DECISION.value


def test_changes_requested_continue_stays_changes_requested():
    # A continuation turn while changes are outstanding must NOT silently reset to
    # RUNNING — a later turn still needs to know changes were requested.
    step = _run(
        {"task_id": "g3", "lifecycle_state": "changes_requested"},
        FakeHarness(TurnOutcome.CONTINUE),
    )
    assert step["status"] == "done"
    assert step["state"]["lifecycle_state"] == State.CHANGES_REQUESTED.value
