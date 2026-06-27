"""Deterministic executor: run a compiled WorkflowSpec, step by step.

This is the DETERMINISTIC half of the spine — it runs the compiled spec, it does
not reason. S0 simulates each step (no live integrations yet); slices S3+ replace
the per-step simulation with real API-first replay -> UI replay -> bounded CUA-heal
while keeping this control flow: a `gated` step (external mutation) HALTS the run
with status=needs_approval and the pending step surfaced to the human approval card
(CQ1). Approve -> the FE re-runs with that step's id in `approved`.
"""

from __future__ import annotations

from .wire import RunResult, RunStep, WorkflowSpec


def run_workflow(spec: WorkflowSpec, data: dict | None = None, approved: set[str] | None = None) -> RunResult:
    """Execute spec deterministically over `data`. Steps already in `approved` are
    allowed to mutate; the first unapproved gated step halts the run for the human."""
    _ = data or {}
    approved = approved or set()
    steps: list[RunStep] = []
    for step in spec.steps:
        if step.gated and step.id not in approved:
            steps.append(RunStep(step_id=step.id, status="awaiting_approval",
                                 detail=f"{step.title} mutates {step.integration or 'an external system'} — needs approval."))
            return RunResult(
                status="needs_approval",
                steps=steps,
                digest=f"Paused at {step.title}: external mutation needs the human approval card.",
                pending_approval={
                    "step_id": step.id,
                    "title": step.title,
                    "integration": step.integration,
                    "detail": step.detail,
                },
            )
        steps.append(RunStep(step_id=step.id, status="ok", detail=f"{step.title} ran."))
    return RunResult(status="done", steps=steps, digest=f"Ran {len(steps)} steps to completion.")
