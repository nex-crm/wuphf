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


class ApiCall(BaseModel):
    """An executable API call a step replays deterministically (the EXECUTE half).
    Built by discovery (browsersniff: HAR -> ApiCall) and replayed by the executor.
    auth_ref is a NAMED credential reference, never a secret value (operator-mlp
    A3/A4) — the executor resolves it from the credential store at run time."""

    model_config = ConfigDict(extra="forbid")

    method: str
    url: str
    query: dict[str, str] | None = None
    headers: dict[str, str] | None = None
    body: Any | None = None
    auth_ref: str | None = None  # NAMED credential reference, never a secret value


class WorkflowStep(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str
    kind: WorkflowStepKind
    title: str
    detail: str
    integration: str | None = None  # e.g. "HubSpot", "Slack"
    gated: bool = False  # external mutation -> human approval card (CQ1)
    api: ApiCall | None = None  # present -> executor replays a real call; absent -> simulated


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


ToolInputType = Literal["string", "number", "record"]


class ToolInput(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    type: ToolInputType = "string"


class Tool(BaseModel):
    """A callable capability the chat agent AUTHORED for an app — a workflow the
    operator taught it, saved as something the agent can call later by `name`.
    Mirrors the FE's operator/tools/mockTools.ts Tool. The chat agent creates these
    by calling its own `create_tool` tool (see harness/tools.py)."""

    model_config = ConfigDict(extra="forbid")

    name: str  # callable id, e.g. "score_and_route_lead"
    title: str  # plain-language, e.g. "Score & route a lead"
    purpose: str  # one line: what running it does
    inputs: list[ToolInput] = Field(default_factory=list)
    code: str = ""  # the agent-written implementation (empty until real authoring)


class ToolBuildRequest(BaseModel):
    """The operator's chat message; the tool agent may call create_tool for it."""

    model_config = ConfigDict(extra="forbid")

    schema_version: int = SCHEMA_VERSION
    message: str
    app: str | None = None  # the app the tool is for (copy only)


class ToolBuildResult(BaseModel):
    """What the tool agent produced for one chat turn: the tool it created (if it
    decided to make one) plus its reflect-back line."""

    model_config = ConfigDict(extra="forbid")

    tool: Tool | None = None
    narration: str = ""


class RunRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    schema_version: int = SCHEMA_VERSION
    spec: WorkflowSpec
    input: dict[str, Any] = Field(default_factory=dict)  # test/real data for the run


class RunStep(BaseModel):
    model_config = ConfigDict(extra="forbid")

    step_id: str
    status: Literal["ok", "skipped", "awaiting_approval", "error"]
    detail: str = ""
    http_status: int | None = None  # present for replayed API steps


class PendingApproval(BaseModel):
    model_config = ConfigDict(extra="forbid")

    step_id: str
    title: str
    integration: str | None = None
    detail: str


class RunResult(BaseModel):
    """Deterministic execution result. status=needs_approval when a gated step
    requires the human approval card before the run can proceed (CQ1)."""

    model_config = ConfigDict(extra="forbid")

    status: Literal["done", "needs_approval", "error"]
    steps: list[RunStep] = Field(default_factory=list)
    digest: str = ""
    pending_approval: PendingApproval | None = None
