"""FastAPI service: the thin HTTP/SSE API the operator FE talks to (no broker).

  GET  /health        liveness
  GET  /providers     which inference backends are available (BYOK status)
  POST /build/stream  chat -> the agent assembles a WorkflowSpec (SSE: start/step/spec)
  POST /run           execute a compiled WorkflowSpec deterministically

The SSE shape + schema_version handshake + extra="forbid" decode are carried from
the dead stack's hardening. See docs/specs/operator-harness-clean-start.md.
"""

from __future__ import annotations

import json
from collections.abc import Callable

from fastapi import FastAPI, HTTPException
from sse_starlette.sse import EventSourceResponse

from .build_agent import BuildAgent, build_agent
from .executor import run_workflow
from .providers import providers_payload
from .wire import SCHEMA_VERSION, BuildRequest, RunRequest, RunResult


def _check_schema_version(got: int) -> None:
    if got != SCHEMA_VERSION:
        raise HTTPException(
            status_code=400,
            detail=f"schema_version mismatch: harness speaks {SCHEMA_VERSION}, got {got}",
        )


AgentFactory = Callable[[], BuildAgent]


def create_app(agent_factory: AgentFactory = build_agent) -> FastAPI:
    app = FastAPI(title="wuphf-harness", version="0.0.1")

    @app.get("/health")
    def health() -> dict:
        return {"status": "ok", "version": "0.0.1"}

    @app.get("/providers")
    def providers() -> dict:
        return providers_payload()

    @app.post("/build/stream")
    async def build_stream(req: BuildRequest) -> EventSourceResponse:
        _check_schema_version(req.schema_version)
        agent = agent_factory()

        async def generate():
            yield {"event": "start", "data": json.dumps({"message": req.message})}
            async for ev in agent.stream(req.message, req.tool_id):
                name = "spec" if ev.get("type") == "spec" else "step"
                yield {"event": name, "data": json.dumps(ev)}

        return EventSourceResponse(generate())

    @app.post("/run", response_model=RunResult)
    def run(req: RunRequest) -> RunResult:
        _check_schema_version(req.schema_version)
        approved = {s for s in req.input.get("approved", [])} if isinstance(req.input.get("approved"), list) else set()
        return run_workflow(req.spec, data=req.input, approved=approved)

    return app


app = create_app()
