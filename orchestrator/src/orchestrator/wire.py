"""The Go <-> Python wire contract (protocol-grade — golden-tested).

Secrets never cross in the body: the broker token is passed to the orchestrator
process via env and named (not valued) here, mirroring SlackProviderBinding.BotTokenEnv.
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

SCHEMA_VERSION = 1


class McpServer(BaseModel):
    # extra="forbid" so a field added on the Go side but forgotten here fails loud
    # at decode time instead of being silently dropped (matters once P2 extends the
    # contract with the goal-coordination message shape).
    model_config = ConfigDict(extra="forbid")

    command: str
    args: list[str] = Field(default_factory=list)
    # ENV VAR NAMES to pass through to the teammcp subprocess (never values).
    env_passthrough: list[str] = Field(default_factory=list)


class DispatchRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = SCHEMA_VERSION
    task_id: str
    # The authoritative broker record (re-hydrate source). Carries lifecycle_state
    # plus the legacy 4-tuple; the orchestrator never trusts a derived state when
    # the field is present.
    record: dict[str, Any]
    model: str = ""
    system_prompt: str = ""
    messages: list[dict] = Field(default_factory=list)
    mcp: dict[str, McpServer] = Field(default_factory=dict)


class ResumeRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = SCHEMA_VERSION
    task_id: str
    thread_id: str
    decision: Literal["approve", "request_changes", "reject"]


class StepResult(BaseModel):
    """Non-streaming result of one orchestration step (the SSE stream carries the
    same information incrementally; this is the terminal summary the broker persists)."""

    status: Literal["done", "interrupted"]
    thread_id: str
    projection: dict[str, Any]            # one-way -> Go store (task status shape)
    interrupt: dict[str, Any] | None = None


class CoordinateRequest(BaseModel):
    """Goal-level coordination input: the goal's CHILD records, re-hydrated from the
    broker each tick (like a single task). Each child carries task_id +
    lifecycle_state + depends_on (the broker sends DependsOn ∪ BlockedOn so the
    kernel's release rule matches the broker's unblock cascade)."""

    model_config = ConfigDict(extra="forbid")

    schema_version: int = SCHEMA_VERSION
    goal_id: str
    children: list[dict[str, Any]] = Field(default_factory=list)


class CoordinationPlan(BaseModel):
    """The per-child action plan the broker applies: start / dispatch each child in
    `ready`, leave the rest. `cycle` (a dependency path) is a deadlocked
    decomposition — the broker fails loud and dispatches nothing."""

    goal_id: str
    actions: dict[str, str]               # task_id -> idle|await|block|start|dispatch|unknown
    ready: list[str]                      # the parallel batch to act on this tick
    cycle: list[str] | None = None
