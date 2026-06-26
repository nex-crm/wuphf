"""FastAPI service: the Go broker dispatches orchestration steps here.

  POST /run        — re-hydrate from the record, run one step, return StepResult
  POST /resume     — resolve a pending human gate, continue, return StepResult
  POST /coordinate — re-hydrate a goal's children, return the per-child action plan
  GET  /health     — liveness

The harness is injected via a factory so tests override it with FakeHarness and the
process default builds a ClaudeAgentHarness when keys/SDK are present. The Go-side
dispatch client, the broker-state projection write-back, and SSE streaming are the
next increments; this skeleton is the contract surface they target.
"""

from __future__ import annotations

from typing import Callable

from fastapi import FastAPI, HTTPException

from .coordination import TaskGraph, coordinate
from .graph import build_graph, drive
from .harness import FakeHarness, Harness, build_harness
from .lifecycle import State
from .runstate import from_broker_record, to_projection
from .wire import (
    SCHEMA_VERSION,
    CoordinateRequest,
    CoordinationPlan,
    DispatchRequest,
    ResumeRequest,
    StepResult,
)


def _check_schema_version(got: int) -> None:
    """Fail loud on a wire-contract mismatch instead of silently misreading a
    request shaped for a different schema. The version field is otherwise
    decorative — a mismatched sidecar must 400, not best-effort parse."""
    if got != SCHEMA_VERSION:
        raise HTTPException(
            status_code=400,
            detail=f"schema_version mismatch: orchestrator speaks {SCHEMA_VERSION}, got {got}",
        )

# Per-process harness factory; overridable for tests / wiring.
HarnessFactory = Callable[[DispatchRequest], Harness]


def _default_harness_factory(req: DispatchRequest) -> Harness:
    # P2: drive Claude Code via the Claude Agent SDK when it's installed (passing
    # the request's model + teammcp config); otherwise degrade to FakeHarness so
    # the service stays runnable key-free. Tests inject FakeHarness explicitly.
    return build_harness(req.model, req.mcp)


def create_app(harness_factory: HarnessFactory = _default_harness_factory) -> FastAPI:
    app = FastAPI(title="wuphf-orchestrator", version="0.1.0")
    # One shared in-memory checkpointer across requests so /resume finds the thread.
    from langgraph.checkpoint.memory import MemorySaver

    checkpointer = MemorySaver()

    @app.get("/health")
    def health() -> dict:
        return {"status": "ok", "version": "0.1.0"}

    @app.post("/run", response_model=StepResult)
    def run(req: DispatchRequest) -> StepResult:
        _check_schema_version(req.schema_version)
        run_state = from_broker_record(req.record)
        run_state["system_prompt"] = req.system_prompt or run_state.get("system_prompt", "")
        if req.messages:
            run_state["messages"] = req.messages
        if run_state["lifecycle_state"] == State.UNKNOWN.value:
            # Fail loud: never dispatch an unmappable task. Emit the full projection
            # shape (status "unknown") so the Go broker decodes every field and
            # detects the signal via IsUnknown(), not a half-populated record.
            return StepResult(
                status="done",
                thread_id=req.task_id,
                projection=to_projection(run_state),
            )
        graph = build_graph(harness_factory(req), checkpointer=checkpointer)
        result = drive(graph, run_state, thread_id=req.task_id)
        return StepResult(
            status=result["status"],
            thread_id=req.task_id,
            projection=to_projection(result["state"]),
            interrupt=result.get("interrupt"),
        )

    @app.post("/coordinate", response_model=CoordinationPlan)
    def coordinate_goal(req: CoordinateRequest) -> CoordinationPlan:
        # Re-hydrate the goal's task graph from the child records and return the
        # per-child action plan. Pure: no harness, no checkpointer — a dependency
        # cycle comes back as `cycle` (every child BLOCKed) so the broker fails loud.
        _check_schema_version(req.schema_version)
        result = coordinate(TaskGraph.from_broker_records(req.children))
        return CoordinationPlan(
            goal_id=req.goal_id,
            actions=result.actions,
            ready=result.ready,
            cycle=result.cycle,
        )

    @app.post("/resume", response_model=StepResult)
    def resume(req: ResumeRequest) -> StepResult:
        _check_schema_version(req.schema_version)
        # The harness is not invoked on resume (the graph continues at human_gate),
        # so a FakeHarness placeholder is fine; the shared checkpointer holds the thread.
        graph = build_graph(FakeHarness(), checkpointer=checkpointer)
        result = drive(graph, None, thread_id=req.thread_id, resume={"decision": req.decision})
        return StepResult(
            status=result["status"],
            thread_id=req.thread_id,
            projection=to_projection(result["state"]),
            interrupt=result.get("interrupt"),
        )

    return app


app = create_app()
