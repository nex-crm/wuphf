"""
Faithful port of WUPHF's lifecycle state machine for the P4 run-state spike.

Source of truth: internal/team/broker_lifecycle_transition.go @ origin/main ec467159.
We mirror the REAL data so the spike tests the actual migration, not a toy:
  - the 13 canonical lifecycle states (+ "unknown" fail-loud fallback)
  - FORWARD: state -> (pipeline_stage, review_state, status, blocked) 4-tuple
  - MIGRATION: legacy 4-tuple -> state (incl. legacy aliases / bare statuses)
  - derive_lifecycle_state_from_legacy(): the migration shim's core
  - bare-status fallback (migrateLifecycleStatesLocked)

If broker_lifecycle_transition.go changes these maps, re-port them here.
"""

UNKNOWN = "unknown"

CANONICAL_STATES = [
    "drafting", "intake", "ready", "planning", "running", "review",
    "decision", "blocked", "queued_behind_owner", "changes_requested",
    "approved", "rejected", "archived",
]

# state -> (pipeline_stage, review_state, status, blocked)   [lifecycleDerivedFields]
FORWARD = {
    "drafting":            ("draft", "pending_review", "open", False),
    "planning":            ("plan", "pending_review", "in_progress", False),
    "intake":              ("triage", "pending_review", "open", False),
    "ready":               ("triage", "pending_review", "open", False),
    "running":             ("implement", "pending_review", "in_progress", False),
    "review":              ("review", "ready_for_review", "in_progress", False),
    "decision":            ("review", "ready_for_review", "in_progress", False),
    "blocked":             ("review", "ready_for_review", "blocked", True),
    "queued_behind_owner": ("triage", "pending_review", "open", True),
    "changes_requested":   ("implement", "pending_review", "in_progress", False),
    "approved":            ("ship", "approved", "done", False),
    "rejected":            ("review", "rejected", "rejected", True),
    "archived":            ("archived", "approved", "archived", False),
}

# legacy 4-tuple -> state   [lifecycleMigrationMap]; keys are lower-cased.
MIGRATION = {
    ("draft", "pending_review", "open", False): "drafting",
    ("triage", "pending_review", "open", False): "ready",
    ("implement", "pending_review", "in_progress", False): "running",
    ("review", "ready_for_review", "in_progress", False): "review",
    ("review", "ready_for_review", "blocked", True): "blocked",
    ("triage", "pending_review", "open", True): "queued_behind_owner",
    ("ship", "approved", "done", False): "approved",
    ("review", "rejected", "rejected", True): "rejected",
    ("plan", "pending_review", "in_progress", False): "planning",
    ("implement", "changes_requested", "in_progress", False): "changes_requested",
    # blocked variants
    ("", "", "blocked", True): "blocked",
    ("", "", "blocked", False): "blocked",
    ("implement", "pending_review", "blocked", True): "blocked",
    ("implement", "pending_review", "blocked", False): "blocked",
    ("review", "ready_for_review", "blocked", False): "blocked",
    # bare statuses
    ("", "", "open", False): "ready",
    ("", "", "open", True): "queued_behind_owner",
    ("", "", "in_progress", False): "running",
    ("", "", "review", False): "review",
    ("", "", "done", False): "approved",
    ("", "", "completed", False): "approved",
    ("", "", "canceled", False): "approved",
    ("", "", "cancelled", False): "approved",
    ("", "", "archived", False): "archived",
    ("archived", "approved", "archived", False): "archived",
}

# normalizeLegacyLifecycleStateName
LEGACY_NAME_ALIAS = {"merged": "approved", "blocked_on_pr_merge": "blocked"}


def normalize_legacy_state_name(s: str) -> str:
    return LEGACY_NAME_ALIAS.get((s or "").strip().lower(), s)


def derive_lifecycle_state_from_legacy(pipeline_stage, review_state, status, blocked) -> str:
    key = (
        (pipeline_stage or "").strip().lower(),
        (review_state or "").strip().lower(),
        (status or "").strip().lower(),
        bool(blocked),
    )
    return MIGRATION.get(key, UNKNOWN)


_TERMINAL = {"done", "approved", "archived", "completed", "canceled", "cancelled"}


def migrate_state(record: dict) -> tuple[str, str]:
    """Mirror migrateLifecycleStatesLocked. Returns (state, how).

    how ∈ {carried, derived, bare_status_fallback, unknown}.
    Modern broker-state.json carries lifecycle_state directly (lossless);
    legacy snapshots derive from the 4-tuple; unmapped tuples fail loud.
    """
    carried = (record.get("lifecycle_state") or "").strip()
    if carried:
        return normalize_legacy_state_name(carried), "carried"
    ps = record.get("pipeline_stage", "")
    rs = record.get("review_state", "")
    st = record.get("status", "")
    bl = bool(record.get("blocked", False))
    derived = derive_lifecycle_state_from_legacy(ps, rs, st, bl)
    if derived != UNKNOWN:
        return derived, "derived"
    by_status = derive_lifecycle_state_from_legacy("", "", st, bl)
    if by_status != UNKNOWN:
        return by_status, "bare_status_fallback"
    return UNKNOWN, "unknown"


def make_broker_record(state: str, *, carry_state: bool = True, task_id: str = None) -> dict:
    """A broker-state.json-shaped task at `state`. carry_state=False simulates a
    pre-Lane-A snapshot that has only the legacy 4-tuple, no lifecycle_state."""
    ps, rs, st, bl = FORWARD[state]
    rec = {
        "task_id": task_id or f"task-{state}",
        "pipeline_stage": ps, "review_state": rs, "status": st, "blocked": bl,
    }
    if carry_state:
        rec["lifecycle_state"] = state
    return rec
