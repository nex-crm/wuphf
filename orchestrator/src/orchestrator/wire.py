"""The Go <-> Python wire contract (protocol-grade — golden-tested).

Secrets never cross in the body: the broker token is passed to the orchestrator
process via env and named (not valued) here, mirroring SlackProviderBinding.BotTokenEnv.
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field

SCHEMA_VERSION = 1


class McpServer(BaseModel):
    command: str
    args: list[str] = Field(default_factory=list)
    # ENV VAR NAMES to pass through to the teammcp subprocess (never values).
    env_passthrough: list[str] = Field(default_factory=list)


class DispatchRequest(BaseModel):
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
