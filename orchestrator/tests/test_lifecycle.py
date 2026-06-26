"""Lifecycle model tests — the P4 spike findings, now real regression tests."""

import pytest

from orchestrator import lifecycle as lc
from orchestrator.lifecycle import GateKind, HumanDecision, Route, State, TurnOutcome


def test_carry_state_is_lossless_for_all_canonical_states():
    for s in lc.CANONICAL:
        rec = {"task_id": f"t-{s.value}", "lifecycle_state": s.value, **dict(
            zip(("pipeline_stage", "review_state", "status", "blocked"), lc.FORWARD[s]))}
        state, how = lc.migrate_record(rec)
        assert state is s
        assert how == "carried"


def test_4tuple_derivation_collapses_exactly_three_states():
    collapses = {}
    for s in lc.CANONICAL:
        rec = dict(zip(("pipeline_stage", "review_state", "status", "blocked"), lc.FORWARD[s]))
        derived = lc.derive_from_legacy(*lc.FORWARD[s])
        if derived is not s:
            collapses[s] = derived
    assert collapses == {
        State.INTAKE: State.READY,
        State.DECISION: State.REVIEW,
        State.CHANGES_REQUESTED: State.RUNNING,
    }


@pytest.mark.parametrize("rec", [
    {"pipeline_stage": "implement", "review_state": "pending_review", "status": "in_progress", "blocked": True},
    {"pipeline_stage": "act", "review_state": "verifying", "status": "verifying", "blocked": False},
    {"pipeline_stage": "qux", "review_state": "zonk", "status": "frobnicate", "blocked": False},
])
def test_contradictory_tuples_fail_loud_to_unknown(rec):
    state, how = lc.migrate_record(rec)
    assert state is State.UNKNOWN
    assert how == "unknown"


@pytest.mark.parametrize("name,expected", [
    ("merged", State.APPROVED),
    ("blocked_on_pr_merge", State.BLOCKED),
])
def test_legacy_alias_names_normalize(name, expected):
    state, how = lc.migrate_record({"lifecycle_state": name})
    assert state is expected
    assert how == "carried"


def test_bare_status_fallback():
    # Full tuple unmapped (future 'act' pipeline stage), but the bare status rescues
    # it — mirrors migrateLifecycleStatesLocked's fallback for newer template stages.
    state, how = lc.migrate_record(
        {"pipeline_stage": "act", "review_state": "verifying", "status": "in_progress", "blocked": False}
    )
    assert state is State.RUNNING
    assert how == "bare_status_fallback"


def test_bare_status_direct_derivation():
    # A record with only a status maps directly via the bare tuple (derived, not fallback).
    state, how = lc.migrate_record({"status": "in_progress"})
    assert state is State.RUNNING
    assert how == "derived"


def test_routing_policy():
    assert lc.route(State.RUNNING) is Route.DISPATCH
    assert lc.route(State.PLANNING) is Route.DISPATCH
    assert lc.route(State.REVIEW) is Route.HUMAN
    assert lc.route(State.DECISION) is Route.HUMAN
    assert lc.route(State.APPROVED) is Route.IDLE
    assert lc.route(State.BLOCKED) is Route.IDLE


def test_gate_classification_and_decisions():
    assert lc.gate_for_outcome(State.PLANNING, TurnOutcome.PLAN_READY) is GateKind.PLAN
    assert lc.gate_for_outcome(State.RUNNING, TurnOutcome.SUBMITTED_FOR_REVIEW) is GateKind.REVIEW
    assert lc.gate_for_outcome(State.RUNNING, TurnOutcome.CONTINUE) is None

    assert lc.apply_human_decision(GateKind.PLAN, HumanDecision.APPROVE) is State.RUNNING
    assert lc.apply_human_decision(GateKind.REVIEW, HumanDecision.APPROVE) is State.APPROVED
    assert lc.apply_human_decision(GateKind.REVIEW, HumanDecision.REQUEST_CHANGES) is State.CHANGES_REQUESTED
    assert lc.apply_human_decision(GateKind.REVIEW, HumanDecision.REJECT) is State.REJECTED


def test_continue_preserves_working_state():
    # A continuation keeps the state it was in for planning AND changes_requested;
    # everything else collapses to running.
    assert lc.apply_turn_outcome(State.PLANNING, TurnOutcome.CONTINUE) is State.PLANNING
    assert lc.apply_turn_outcome(State.CHANGES_REQUESTED, TurnOutcome.CONTINUE) is State.CHANGES_REQUESTED
    assert lc.apply_turn_outcome(State.RUNNING, TurnOutcome.CONTINUE) is State.RUNNING


def test_gate_pending_state_maps_to_human_states():
    assert lc.gate_pending_state(GateKind.PLAN) is State.DECISION
    assert lc.gate_pending_state(GateKind.REVIEW) is State.REVIEW
    # both park the task in a non-executable, human-routed state
    for s in (State.DECISION, State.REVIEW):
        assert not lc.is_executable(s)
        assert lc.route(s) is Route.HUMAN


def test_derived_fields_total_on_unknown():
    # derived_fields must be total: UNKNOWN returns a fail-loud sentinel, never a
    # KeyError (reachable from coordination when a dependency is unmappable).
    ps, rs, status, blocked = lc.derived_fields(State.UNKNOWN)
    assert status == "unknown"
    assert blocked is False
