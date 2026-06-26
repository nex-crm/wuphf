"""WUPHF task lifecycle — production port.

Source of truth: internal/team/broker_lifecycle_transition.go @ origin/main ec467159.
This is the canonical Python copy (the spike under spikes/langgraph-runstate is
superseded by this module). The migration round-trip is validated by the P4 spike:
13/13 states round-trip; carry `lifecycle_state` (lossless); fail loud to `unknown`.

Two responsibilities:
  1. State data + migration (mirrors the Go maps): FORWARD, MIGRATION, migrate_record.
  2. Orchestration policy (P1): route(), apply_turn_outcome(), apply_human_decision().
     Policy intentionally simplified for P1; deviations from the Go transitions are
     documented inline and revisited in P2/P3.
"""

from __future__ import annotations

from enum import Enum


class State(str, Enum):
    UNKNOWN = "unknown"
    DRAFTING = "drafting"
    INTAKE = "intake"
    READY = "ready"
    PLANNING = "planning"
    RUNNING = "running"
    REVIEW = "review"
    DECISION = "decision"
    BLOCKED = "blocked"
    QUEUED_BEHIND_OWNER = "queued_behind_owner"
    CHANGES_REQUESTED = "changes_requested"
    APPROVED = "approved"
    REJECTED = "rejected"
    ARCHIVED = "archived"


CANONICAL: tuple[State, ...] = (
    State.DRAFTING, State.INTAKE, State.READY, State.PLANNING, State.RUNNING,
    State.REVIEW, State.DECISION, State.BLOCKED, State.QUEUED_BEHIND_OWNER,
    State.CHANGES_REQUESTED, State.APPROVED, State.REJECTED, State.ARCHIVED,
)

# state -> (pipeline_stage, review_state, status, blocked)   [lifecycleDerivedFields]
FORWARD: dict[State, tuple[str, str, str, bool]] = {
    # UNKNOWN is not a canonical broker state; it carries the fail-loud signal so
    # derived_fields / to_projection stay total (no KeyError) and the Go side still
    # detects it via status/lifecycle_state == "unknown" and refuses to dispatch.
    State.UNKNOWN:             ("", "", "unknown", False),
    State.DRAFTING:            ("draft", "pending_review", "open", False),
    State.PLANNING:            ("plan", "pending_review", "in_progress", False),
    State.INTAKE:              ("triage", "pending_review", "open", False),
    State.READY:               ("triage", "pending_review", "open", False),
    State.RUNNING:             ("implement", "pending_review", "in_progress", False),
    State.REVIEW:              ("review", "ready_for_review", "in_progress", False),
    State.DECISION:            ("review", "ready_for_review", "in_progress", False),
    State.BLOCKED:             ("review", "ready_for_review", "blocked", True),
    State.QUEUED_BEHIND_OWNER: ("triage", "pending_review", "open", True),
    State.CHANGES_REQUESTED:   ("implement", "pending_review", "in_progress", False),
    State.APPROVED:            ("ship", "approved", "done", False),
    State.REJECTED:            ("review", "rejected", "rejected", True),
    State.ARCHIVED:            ("archived", "approved", "archived", False),
}

# legacy 4-tuple -> state   [lifecycleMigrationMap]; keys lower-cased.
MIGRATION: dict[tuple[str, str, str, bool], State] = {
    ("draft", "pending_review", "open", False): State.DRAFTING,
    ("triage", "pending_review", "open", False): State.READY,
    ("implement", "pending_review", "in_progress", False): State.RUNNING,
    ("review", "ready_for_review", "in_progress", False): State.REVIEW,
    ("review", "ready_for_review", "blocked", True): State.BLOCKED,
    ("triage", "pending_review", "open", True): State.QUEUED_BEHIND_OWNER,
    ("ship", "approved", "done", False): State.APPROVED,
    ("review", "rejected", "rejected", True): State.REJECTED,
    ("plan", "pending_review", "in_progress", False): State.PLANNING,
    ("implement", "changes_requested", "in_progress", False): State.CHANGES_REQUESTED,
    ("", "", "blocked", True): State.BLOCKED,
    ("", "", "blocked", False): State.BLOCKED,
    ("implement", "pending_review", "blocked", True): State.BLOCKED,
    ("implement", "pending_review", "blocked", False): State.BLOCKED,
    ("review", "ready_for_review", "blocked", False): State.BLOCKED,
    ("", "", "open", False): State.READY,
    ("", "", "open", True): State.QUEUED_BEHIND_OWNER,
    ("", "", "in_progress", False): State.RUNNING,
    ("", "", "review", False): State.REVIEW,
    ("", "", "done", False): State.APPROVED,
    ("", "", "completed", False): State.APPROVED,
    ("", "", "canceled", False): State.APPROVED,
    ("", "", "cancelled", False): State.APPROVED,
    ("", "", "archived", False): State.ARCHIVED,
    ("archived", "approved", "archived", False): State.ARCHIVED,
}

_LEGACY_NAME_ALIAS = {"merged": State.APPROVED, "blocked_on_pr_merge": State.BLOCKED}
_TERMINAL = frozenset({State.APPROVED, State.REJECTED, State.ARCHIVED})
_EXECUTABLE = frozenset({State.RUNNING, State.PLANNING, State.APPROVED})  # isExecutableTeamTaskStatus
_PRE_EXECUTION = frozenset({
    State.DRAFTING, State.INTAKE, State.READY, State.QUEUED_BEHIND_OWNER, State.PLANNING,
})


def normalize_legacy_name(s: str) -> str:
    alias = _LEGACY_NAME_ALIAS.get((s or "").strip().lower())
    return alias.value if alias is not None else s


def _norm(s: str | None) -> str:
    return (s or "").strip().lower()


def derive_from_legacy(pipeline_stage, review_state, status, blocked) -> State:
    """Mirror deriveLifecycleStateFromLegacy: legacy 4-tuple -> state, else UNKNOWN."""
    return MIGRATION.get(
        (_norm(pipeline_stage), _norm(review_state), _norm(status), bool(blocked)),
        State.UNKNOWN,
    )


def migrate_record(record: dict) -> tuple[State, str]:
    """Resolve a broker-state.json task record to a lifecycle State.

    Returns (state, how). how in {carried, derived, bare_status_fallback, unknown}.
    Carrying lifecycle_state is lossless (preferred); derivation is the legacy-only
    fallback; unmapped tuples fail loud to UNKNOWN for operator triage.
    """
    carried = _norm(record.get("lifecycle_state"))
    if carried:
        alias = _LEGACY_NAME_ALIAS.get(carried)
        if alias is not None:
            return alias, "carried"
        try:
            return State(carried), "carried"
        except ValueError:
            pass  # fall through to legacy derivation
    derived = derive_from_legacy(
        record.get("pipeline_stage"), record.get("review_state"),
        record.get("status"), record.get("blocked", False),
    )
    if derived is not State.UNKNOWN:
        return derived, "derived"
    by_status = derive_from_legacy("", "", record.get("status"), record.get("blocked", False))
    if by_status is not State.UNKNOWN:
        return by_status, "bare_status_fallback"
    return State.UNKNOWN, "unknown"


def derived_fields(state: State) -> tuple[str, str, str, bool]:
    return FORWARD[state]


def is_executable(state: State) -> bool:
    return state in _EXECUTABLE


def is_pre_execution(state: State) -> bool:
    return state in _PRE_EXECUTION


def is_terminal(state: State) -> bool:
    return state in _TERMINAL


# --------------------------------------------------------------------------- #
# Orchestration policy (P1). Documented simplifications vs the Go transitions.
# --------------------------------------------------------------------------- #
class Route(str, Enum):
    DISPATCH = "dispatch"      # wake the owner: a plan turn (planning) or work turn (running)
    HUMAN = "human"            # awaiting a reviewer decision
    IDLE = "idle"             # nothing for the orchestrator to do this tick


class TurnOutcome(str, Enum):
    PLAN_READY = "plan_ready"                  # planning turn produced a plan to approve
    SUBMITTED_FOR_REVIEW = "submitted_for_review"
    COMPLETED = "completed"
    CONTINUE = "continue"                      # needs another work turn
    BLOCKED = "blocked"
    DECOMPOSED = "decomposed"                  # the turn created child tasks (a decomposition)


class HumanDecision(str, Enum):
    APPROVE = "approve"
    REQUEST_CHANGES = "request_changes"
    REJECT = "reject"


class GateKind(str, Enum):
    PLAN = "plan"      # approving a plan -> Running
    REVIEW = "review"  # approving delivered work -> Approved


def route(state: State) -> Route:
    """P1 routing. drafting/intake/ready/queued/blocked are IDLE — activation and
    unblock stay the broker's job in P1 (revisited in P2)."""
    if is_terminal(state):
        return Route.IDLE
    if state in (State.REVIEW, State.DECISION):
        return Route.HUMAN
    if state in (State.RUNNING, State.PLANNING, State.CHANGES_REQUESTED):
        return Route.DISPATCH
    return Route.IDLE


def gate_for_outcome(state: State, outcome: TurnOutcome) -> GateKind | None:
    """Does this turn outcome require a human gate, and of what kind?"""
    if outcome is TurnOutcome.PLAN_READY:
        return GateKind.PLAN
    if outcome in (TurnOutcome.SUBMITTED_FOR_REVIEW, TurnOutcome.COMPLETED):
        return GateKind.REVIEW
    return None


def gate_pending_state(gate: GateKind) -> State:
    """The lifecycle state a task parks in while a human gate is pending. Both are
    non-executable and route HUMAN, so projecting one stops the dispatch loop and
    lets the broker's existing approval path resolve the gate (re-hydrate model).
    A plan awaiting approval is a DECISION; delivered work awaiting review is REVIEW."""
    return State.DECISION if gate is GateKind.PLAN else State.REVIEW


def apply_turn_outcome(state: State, outcome: TurnOutcome) -> State:
    """Non-gated outcomes only (CONTINUE/BLOCKED/DECOMPOSED). Gated outcomes go
    through a human gate first (see apply_human_decision)."""
    if outcome is TurnOutcome.BLOCKED:
        return State.BLOCKED
    if outcome is TurnOutcome.DECOMPOSED:
        # The goal created its children; it now coordinates them rather than running
        # more turns of its own. RUNNING (non-gated): on the next tick the broker
        # sees children and routes the goal to the coordinate path (P2-ii). The
        # children carry their own review gates, so the decomposition is not
        # separately gated here (the broker already gated plan approval before
        # sub-task creation was allowed).
        return State.RUNNING
    if outcome is TurnOutcome.CONTINUE:
        # A continuation keeps the working state it was in: a planning turn stays
        # PLANNING, a changes-requested turn stays CHANGES_REQUESTED (so a later
        # turn still knows changes are outstanding), everything else is RUNNING.
        if state in (State.PLANNING, State.CHANGES_REQUESTED):
            return state
        return State.RUNNING
    raise ValueError(f"{outcome} is a gated outcome; route through a human gate")


def apply_human_decision(gate: GateKind, decision: HumanDecision) -> State:
    if decision is HumanDecision.REJECT:
        return State.REJECTED
    if decision is HumanDecision.REQUEST_CHANGES:
        return State.CHANGES_REQUESTED
    # APPROVE
    return State.RUNNING if gate is GateKind.PLAN else State.APPROVED
