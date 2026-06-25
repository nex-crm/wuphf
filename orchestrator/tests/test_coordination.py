"""P2 coordination-kernel tests: the dependency/sequencing rule the broker owns
today, rebuilt as a pure re-hydratable model. No graph wiring, no agent, no key."""

from __future__ import annotations

import pytest

from orchestrator.coordination import (
    CoordAction,
    TaskGraph,
    TaskNode,
    coordination_action,
    dependency_resolved,
    detect_cycle,
    plan,
    ready_to_dispatch,
    topological_layers,
    unresolved_dependencies,
)
from orchestrator.lifecycle import State


def graph(*nodes: TaskNode) -> TaskGraph:
    return TaskGraph(nodes={n.task_id: n for n in nodes})


def node(tid: str, state: State, *deps: str) -> TaskNode:
    return TaskNode(task_id=tid, lifecycle_state=state.value, depends_on=tuple(deps))


# --- dependency_resolved: only terminal-SUCCESS statuses release -------------- #


@pytest.mark.parametrize(
    "state, resolved",
    [
        (State.APPROVED, True),    # -> status "done"
        (State.ARCHIVED, True),    # -> status "archived"
        (State.REJECTED, False),   # -> status "rejected" (terminal but NOT a release)
        (State.RUNNING, False),
        (State.REVIEW, False),
        (State.BLOCKED, False),
        (State.DRAFTING, False),
    ],
)
def test_dependency_resolved_matches_broker_terminal_status(state, resolved):
    assert dependency_resolved(state) is resolved


# --- unresolved_dependencies + blocking --------------------------------------- #


def test_independent_tasks_both_dispatch_in_parallel():
    g = graph(node("a", State.RUNNING), node("b", State.RUNNING))
    assert plan(g) == {"a": CoordAction.DISPATCH, "b": CoordAction.DISPATCH}
    assert ready_to_dispatch(g) == ["a", "b"]


def test_dependent_task_blocks_until_upstream_done():
    # b depends on a; a is still running -> b blocked, only a dispatches.
    g = graph(node("a", State.RUNNING), node("b", State.READY, "a"))
    assert coordination_action(g, "b") == CoordAction.BLOCK
    assert ready_to_dispatch(g) == ["a"]

    # a reaches approved (status done) -> b is released and STARTs.
    g2 = graph(node("a", State.APPROVED), node("b", State.READY, "a"))
    assert coordination_action(g2, "b") == CoordAction.START
    assert ready_to_dispatch(g2) == ["b"]  # a is terminal -> idle, not dispatched


def test_rejected_upstream_keeps_dependents_blocked():
    g = graph(node("a", State.REJECTED), node("b", State.READY, "a"))
    assert unresolved_dependencies(g, "b") == ["a"]
    assert coordination_action(g, "b") == CoordAction.BLOCK


def test_missing_dependency_is_unresolved():
    # b depends on a task not in the graph -> fail-safe block.
    g = graph(node("b", State.READY, "ghost"))
    assert unresolved_dependencies(g, "b") == ["ghost"]
    assert coordination_action(g, "b") == CoordAction.BLOCK


def test_running_task_with_satisfied_dep_dispatches():
    g = graph(node("a", State.APPROVED), node("b", State.RUNNING, "a"))
    assert coordination_action(g, "b") == CoordAction.DISPATCH


# --- per-state action mapping ------------------------------------------------- #


def test_action_mapping_by_state():
    assert coordination_action(graph(node("t", State.REVIEW)), "t") == CoordAction.AWAIT
    assert coordination_action(graph(node("t", State.DECISION)), "t") == CoordAction.AWAIT
    assert coordination_action(graph(node("t", State.APPROVED)), "t") == CoordAction.IDLE
    assert coordination_action(graph(node("t", State.ARCHIVED)), "t") == CoordAction.IDLE
    assert coordination_action(graph(node("t", State.UNKNOWN)), "t") == CoordAction.UNKNOWN
    assert coordination_action(graph(node("t", State.READY)), "t") == CoordAction.START
    assert coordination_action(graph(node("t", State.CHANGES_REQUESTED)), "t") == CoordAction.DISPATCH


# --- re-hydrate from broker records ------------------------------------------- #


def test_from_broker_records_carries_state_and_deps():
    g = TaskGraph.from_broker_records(
        [
            {"id": "a", "lifecycle_state": "approved"},
            {"task_id": "b", "lifecycle_state": "ready", "depends_on": ["a"]},
        ]
    )
    assert g.nodes["a"].lifecycle_state == "approved"
    assert g.nodes["b"].depends_on == ("a",)
    # The dependency is satisfied (a is done) so b is ready to start.
    assert coordination_action(g, "b") == CoordAction.START


def test_from_broker_records_unmappable_is_unknown():
    g = TaskGraph.from_broker_records([{"id": "x", "pipeline_stage": "nonsense", "status": "???"}])
    assert coordination_action(g, "x") == CoordAction.UNKNOWN


# --- topological layers + cycle detection ------------------------------------- #


def test_topological_layers_express_parallel_then_serial():
    # a, b independent (layer 0); c depends on both (layer 1); d depends on c (layer 2).
    g = graph(
        node("a", State.READY),
        node("b", State.READY),
        node("c", State.READY, "a", "b"),
        node("d", State.READY, "c"),
    )
    assert topological_layers(g) == [["a", "b"], ["c"], ["d"]]


def test_detect_cycle_returns_path():
    g = graph(node("a", State.READY, "b"), node("b", State.READY, "a"))
    cycle = detect_cycle(g)
    assert cycle is not None
    assert cycle[0] == cycle[-1]  # closes the loop
    assert set(cycle) == {"a", "b"}


def test_topological_layers_raises_on_cycle():
    g = graph(node("a", State.READY, "b"), node("b", State.READY, "a"))
    with pytest.raises(ValueError, match="cycle"):
        topological_layers(g)


def test_no_cycle_returns_none():
    g = graph(node("a", State.READY), node("b", State.READY, "a"))
    assert detect_cycle(g) is None
