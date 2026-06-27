"""The FE <-> harness wire contract.

Mirrors the operator FE's existing shapes (web/src/operator/mock/data.ts
WorkflowStep + builder/planWorkflow.ts WorkflowPlan) so the prototype FE can point
its build/run calls straight at this harness. snake_case JSON; extra="forbid" so a
FE/harness drift fails loud; a schema_version handshake so a mismatched client 400s
instead of being silently misread. (Both disciplines carried from the dead stack.)
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field

SCHEMA_VERSION = 1

# Mirror of web/src/operator/mock/data.ts WorkflowStepKind.
WorkflowStepKind = Literal["trigger", "enrich", "ai", "decision", "action", "branch"]
ClarifyField = Literal["threshold", "channel"]


class WorkflowStep(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    kind: WorkflowStepKind
    title: str
    detail: str
    integration: str | None = None  # e.g. "HubSpot", "Slack"
    gated: bool = False  # external mutation -> human approval card (CQ1)


class ClarifyQuestion(BaseModel):
    model_config = ConfigDict(extra="forbid")

    field: ClarifyField
    prompt: str  # the single sharp follow-up the agent asks
    step_id: str  # the step this answer refines, in place


class WorkflowSpec(BaseModel):
    """The compiled, deterministic workflow the BUILD agent emits and the EXECUTE
    path runs. The FE renders it step-by-step as it assembles."""

    model_config = ConfigDict(extra="forbid")

    name: str
    tool_id: str
    steps: list[WorkflowStep] = Field(default_factory=list)
    narration: str = ""  # the agent's reflect-back line
    clarify: ClarifyQuestion | None = None


class BuildRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = SCHEMA_VERSION
    message: str  # the operator's plain-language description
    tool_id: str | None = None  # refine an existing tool, if any


class RunRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = SCHEMA_VERSION
    spec: WorkflowSpec
    input: dict[str, Any] = Field(default_factory=dict)  # test/real data for the run


class RunStep(BaseModel):
    step_id: str
    status: Literal["ok", "skipped", "awaiting_approval"]
    detail: str = ""


class RunResult(BaseModel):
    """Deterministic execution result. status=needs_approval when a gated step
    requires the human approval card before the run can proceed (CQ1)."""

    status: Literal["done", "needs_approval"]
    steps: list[RunStep] = Field(default_factory=list)
    digest: str = ""
    pending_approval: dict[str, Any] | None = None
