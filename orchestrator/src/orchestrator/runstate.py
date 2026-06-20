"""TaskRun: the LangGraph run-state, re-hydrated from the authoritative Go record.

Per the P4 spike decision (re-hydrate variant): the Go broker record is the single
source of durable truth. The orchestrator rebuilds TaskRun from it on every dispatch;
the LangGraph checkpoint is a within-run cache, never authoritative. `to_projection`
emits the status shape the Go store persists for the web (one-way projection).
"""

from __future__ import annotations

from typing import Any, TypedDict

from . import lifecycle as lc


class TaskRun(TypedDict, total=False):
    task_id: str
    lifecycle_state: str          # canonical State value
    owner: str
    system_prompt: str
    messages: list[dict]          # [{role, content}]
    # turn / gate bookkeeping
    gate_kind: str                # set before a human interrupt
    last_outcome: str
    last_text: str
    history: list[str]            # lifecycle states visited this run


def from_broker_record(record: dict) -> TaskRun:
    """Re-hydrate TaskRun from a broker-state.json-shaped task record.

    Carries lifecycle_state losslessly; falls back to legacy derivation. An
    unmappable record yields lifecycle_state='unknown' (the caller must surface
    it for operator triage, never dispatch it)."""
    state, _how = lc.migrate_record(record)
    return TaskRun(
        task_id=str(record.get("task_id") or record.get("id") or ""),
        lifecycle_state=state.value,
        owner=str(record.get("owner") or ""),
        system_prompt=str(record.get("system_prompt") or ""),
        messages=list(record.get("messages") or []),
        history=[],
    )


def to_projection(run: TaskRun) -> dict[str, Any]:
    """One-way projection back to the Go store: the typed state + its derived
    4-tuple, so the existing web renders unchanged."""
    state = lc.State(run["lifecycle_state"])
    ps, rs, st, blocked = lc.derived_fields(state)
    return {
        "task_id": run.get("task_id", ""),
        "lifecycle_state": state.value,
        "pipeline_stage": ps,
        "review_state": rs,
        "status": st,
        "blocked": blocked,
    }
