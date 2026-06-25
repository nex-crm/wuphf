"""Multi-task coordination — the dependency/sequencing kernel WUPHF's broker owns
today (internal/team), rebuilt as a pure, re-hydratable model (P2).

A goal decomposes into several tasks with dependencies. This module answers, for
a re-hydrated set of task records: which tasks are ready to run now, which are
blocked on an upstream, and in what order — independent tasks run in parallel,
dependent tasks serialize. The graph is rebuilt from the Go records each tick
(re-hydrate, like a single task); nothing here is authoritative state.

Faithful to the broker's real rule (broker_tasks_lifecycle.go):
  - A dependency releases its dependents only when its derived STATUS is terminal
    (done/completed/canceled/cancelled/archived). An approval click or a rejection
    does NOT release — so a REJECTED upstream (lifecycle-terminal, status
    "rejected") keeps its dependents blocked, exactly as the broker intends.
  - A dependency the graph hasn't seen yet counts as UNRESOLVED (fail-safe: block
    rather than dispatch work whose prerequisite may not exist).
"""

from __future__ import annotations

from dataclasses import dataclass, field
from enum import Enum

from . import lifecycle as lc

# Derived-status values that mark a dependency satisfied — mirrors the broker's
# isTerminalTeamTaskStatus. In lifecycle terms only APPROVED (-> "done") and
# ARCHIVED (-> "archived") reach these; REJECTED (-> "rejected") does not.
_RESOLVED_STATUSES = frozenset({"done", "completed", "canceled", "cancelled", "archived"})


def dependency_resolved(state: lc.State) -> bool:
    """Whether a task in this state satisfies a dependency that points at it."""
    _ps, _rs, status, _blocked = lc.derived_fields(state)
    return status in _RESOLVED_STATUSES


class CoordAction(str, Enum):
    IDLE = "idle"          # terminal: nothing to do
    AWAIT = "await"        # review/decision: waiting on a human reviewer
    BLOCK = "block"        # one or more dependencies unresolved
    START = "start"        # pre-execution + deps satisfied -> activate to running
    DISPATCH = "dispatch"  # already executing (running/planning/changes) -> run a turn
    UNKNOWN = "unknown"    # unmappable lifecycle: operator triage, never dispatch


@dataclass(frozen=True)
class TaskNode:
    task_id: str
    lifecycle_state: str
    depends_on: tuple[str, ...] = ()


@dataclass
class TaskGraph:
    nodes: dict[str, TaskNode] = field(default_factory=dict)

    @classmethod
    def from_broker_records(cls, records: list[dict]) -> "TaskGraph":
        """Re-hydrate the goal's task graph from broker task records. Carries
        lifecycle_state directly (lossless); an unmappable record yields
        State.UNKNOWN, surfaced as CoordAction.UNKNOWN, never dispatched."""
        nodes: dict[str, TaskNode] = {}
        for rec in records:
            state, _how = lc.migrate_record(rec)
            tid = str(rec.get("task_id") or rec.get("id") or "")
            deps = tuple(str(d) for d in (rec.get("depends_on") or []) if str(d))
            nodes[tid] = TaskNode(task_id=tid, lifecycle_state=state.value, depends_on=deps)
        return cls(nodes=nodes)


def unresolved_dependencies(graph: TaskGraph, task_id: str) -> list[str]:
    """Dependency ids that are not yet satisfied: those missing from the graph
    (treated as unresolved) and those whose state is not terminal-success."""
    node = graph.nodes[task_id]
    out: list[str] = []
    for dep in node.depends_on:
        dep_node = graph.nodes.get(dep)
        if dep_node is None or not dependency_resolved(lc.State(dep_node.lifecycle_state)):
            out.append(dep)
    return out


def is_blocked(graph: TaskGraph, task_id: str) -> bool:
    return bool(unresolved_dependencies(graph, task_id))


def coordination_action(graph: TaskGraph, task_id: str) -> CoordAction:
    """The orchestrator's decision for one task within the goal this tick."""
    state = lc.State(graph.nodes[task_id].lifecycle_state)
    if state is lc.State.UNKNOWN:
        return CoordAction.UNKNOWN
    if lc.is_terminal(state):
        return CoordAction.IDLE
    if state in (lc.State.REVIEW, lc.State.DECISION):
        return CoordAction.AWAIT
    if unresolved_dependencies(graph, task_id):
        return CoordAction.BLOCK
    # Dependencies satisfied:
    if state in (lc.State.RUNNING, lc.State.PLANNING, lc.State.CHANGES_REQUESTED):
        return CoordAction.DISPATCH
    return CoordAction.START


def plan(graph: TaskGraph) -> dict[str, CoordAction]:
    """The whole-goal coordination decision: one action per task. Independent,
    unblocked tasks all come back START/DISPATCH (run in parallel); a task with an
    unresolved upstream comes back BLOCK (serialized behind it)."""
    return {tid: coordination_action(graph, tid) for tid in graph.nodes}


def ready_to_dispatch(graph: TaskGraph) -> list[str]:
    """Tasks the orchestrator should act on this tick (START or DISPATCH), sorted
    for determinism. This is the parallel batch — they have no unresolved deps."""
    return sorted(t for t, a in plan(graph).items() if a in (CoordAction.START, CoordAction.DISPATCH))


def detect_cycle(graph: TaskGraph) -> list[str] | None:
    """Return a dependency cycle as a path (…-> x -> … -> x) or None. Only
    intra-graph edges count. Callers must fail loud on a cycle — it's a deadlocked
    decomposition, never a runnable plan."""
    WHITE, GRAY, BLACK = 0, 1, 2
    color = {t: WHITE for t in graph.nodes}
    stack: list[str] = []

    def dfs(u: str) -> list[str] | None:
        color[u] = GRAY
        stack.append(u)
        for dep in graph.nodes[u].depends_on:
            if dep not in graph.nodes:
                continue
            if color[dep] == GRAY:
                return stack[stack.index(dep):] + [dep]
            if color[dep] == WHITE:
                found = dfs(dep)
                if found:
                    return found
        color[u] = BLACK
        stack.pop()
        return None

    for t in sorted(graph.nodes):
        if color[t] == WHITE:
            found = dfs(t)
            if found:
                return found
    return None


def topological_layers(graph: TaskGraph) -> list[list[str]]:
    """Group tasks into execution layers: layer 0 has no intra-graph prerequisites,
    each later layer depends only on earlier ones. Tasks WITHIN a layer are mutually
    independent — the orchestrator may run them in parallel (plan §7). Raises on a
    dependency cycle."""
    cycle = detect_cycle(graph)
    if cycle:
        raise ValueError(f"dependency cycle: {' -> '.join(cycle)}")

    indegree = {t: 0 for t in graph.nodes}
    children: dict[str, list[str]] = {t: [] for t in graph.nodes}
    for tid, node in graph.nodes.items():
        for dep in node.depends_on:
            if dep in graph.nodes:
                indegree[tid] += 1
                children[dep].append(tid)

    layers: list[list[str]] = []
    frontier = sorted(t for t, d in indegree.items() if d == 0)
    while frontier:
        layers.append(frontier)
        nxt: list[str] = []
        for t in frontier:
            for c in children[t]:
                indegree[c] -= 1
                if indegree[c] == 0:
                    nxt.append(c)
        frontier = sorted(nxt)
    return layers
